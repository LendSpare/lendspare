package app

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LendSpare/lendspare/internal/proc"
)

// The settings page: shared folders, the sites allowed into them, the questions waiting for
// the person, the activity log, direct transfers and the images trusted with API keys
// (docs/features/29-computer-files.md). Served on 127.0.0.1 only, on a random port, behind a
// random token that is in the address the tray (or `--settings`) opens. The Host header must
// be that loopback address (a web page cannot rebind a name onto it) and changes need JSON
// with a matching Origin - so no other page in the browser can drive it.
//
// This page is the only way a folder or a site's access comes into being. The server cannot
// reach it.

//go:embed settings.html
var settingsHTML string

type settingsServer struct {
	token string
	port  int
	app   *app
	files *filesService
	state func() map[string]any // the daemon's status and version, for the header
}

var (
	settingsOnce sync.Once
	settingsSrv  *settingsServer
)

func settingsURLPath() string { return filepath.Join(configDir(), "settings-url") }

// startSettings starts the page once per process and returns its address with the token.
func startSettings(a *app, state func() map[string]any) (string, error) {
	var err error
	settingsOnce.Do(func() {
		var ln net.Listener
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return
		}
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		s := &settingsServer{token: hex.EncodeToString(b), port: ln.Addr().(*net.TCPAddr).Port, app: a, files: a.files, state: state}
		srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		settingsSrv = s
		_ = os.MkdirAll(configDir(), 0o700)
		_ = os.WriteFile(settingsURLPath(), []byte(s.url()), 0o600)
		log.Printf("[settings] shared folders and settings: %s", s.url())
	})
	if settingsSrv == nil {
		if err == nil {
			err = errors.New("the settings page did not start")
		}
		return "", err
	}
	return settingsSrv.url(), nil
}

func (s *settingsServer) url() string {
	return fmt.Sprintf("http://127.0.0.1:%d/#t=%s", s.port, s.token)
}

// openRunningSettings: `lendspare --settings` while another instance runs opens its page.
func openRunningSettings() bool {
	b, err := os.ReadFile(settingsURLPath())
	if err != nil {
		return false
	}
	u := strings.TrimSpace(string(b))
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(strings.SplitN(u, "#", 2)[0] + "health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return openLocalPage(u) == nil
}

// openSettingsPage opens this process's settings page (the tray's "Shared folders...").
func openSettingsPage() {
	if settingsSrv == nil {
		log.Printf("[settings] the settings page is not running")
		return
	}
	if err := openLocalPage(settingsSrv.url()); err != nil {
		log.Printf("[settings] open: %v", err)
	}
}

// openLocalPage opens the settings page; only our own loopback address, never another page.
func openLocalPage(u string) error {
	if !strings.HasPrefix(u, "http://127.0.0.1:") {
		return errors.New("not the settings page")
	}
	name, args := browserCommand(runtime.GOOS, u)
	return proc.Command(name, args...).Start()
}

func (s *settingsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allowedHost := map[string]bool{fmt.Sprintf("127.0.0.1:%d", s.port): true, fmt.Sprintf("localhost:%d", s.port): true}
	if !allowedHost[r.Host] {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/":
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(settingsHTML))
		return
	case "/health":
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-LendSpare-Token")), []byte(s.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodPost {
		origin := r.Header.Get("Origin")
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || (origin != "" && !allowedHost[strings.TrimPrefix(origin, "http://")]) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	var body map[string]any
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	}
	out, err := s.api(r, body)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func strField(body map[string]any, k string) string {
	v, _ := body[k].(string)
	return strings.TrimSpace(v)
}
func boolField(body map[string]any, k string) bool { v, _ := body[k].(bool); return v }

func (s *settingsServer) api(r *http.Request, body map[string]any) (any, error) {
	if out, handled, err := s.connectionsAPI(r.Method, r.URL.Path, body); handled {
		return out, err
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/state" {
		return s.stateJSON(), nil
	}
	if s.files == nil {
		return nil, errors.New("shared folders could not start - see the log")
	}
	st := s.files.store
	switch r.Method + " " + r.URL.Path {
	case "GET /api/browse":
		return browse(r.URL.Query().Get("path"))
	case "POST /api/pick":
		p, err := pickFolder()
		return map[string]any{"path": p}, err
	case "POST /api/shares":
		if boolField(body, "typed") { // typed or pasted; a folder chosen in a dialog or the browser is there
			if err := typedFolderExists(strField(body, "path")); err != nil {
				return nil, err
			}
		}
		sc, err := st.addShare(strField(body, "path"), strField(body, "name"))
		if err != nil {
			return nil, err
		}
		// Touch it now: on macOS this is when the privacy prompt appears, while the person
		// is looking at this page - not later, in the middle of a site's request.
		return map[string]any{"share": sc, "state": shareState(sc.Path), "network": isNetworkVolume(sc.Path)}, nil
	case "POST /api/shares/update":
		var upd shareConfig
		raw, _ := json.Marshal(body["share"])
		if err := json.Unmarshal(raw, &upd); err != nil {
			return nil, errors.New("bad settings")
		}
		return st.updateShare(strField(body, "name"), upd)
	case "POST /api/shares/remove":
		return map[string]any{"ok": true}, st.removeShare(strField(body, "name"))
	case "POST /api/answer":
		if !s.files.asker.answer(strField(body, "id"), strField(body, "answer")) {
			return nil, errors.New("that question is no longer open")
		}
		return map[string]any{"ok": true}, nil
	case "POST /api/sites/access":
		mode := strField(body, "mode")
		if mode != "" && mode != accessRead && mode != accessWrite {
			return nil, errors.New("bad access")
		}
		return map[string]any{"ok": true}, st.setSiteAccess(strField(body, "key"), strField(body, "share"), mode)
	case "POST /api/sites/revoke":
		return map[string]any{"ok": true}, st.revokeSite(strField(body, "key"))
	case "POST /api/direct":
		port, _ := body["port"].(float64)
		advertise := strings.FieldsFunc(strField(body, "advertise"), func(r rune) bool { return r == '\n' || r == ',' || r == ' ' })
		return map[string]any{"ok": true}, st.setDirect(boolField(body, "enabled"), int(port), advertise)
	case "POST /api/pipeline-trust":
		list := append([]string{}, strings.FieldsFunc(strField(body, "digests"), func(r rune) bool { return r == '\n' || r == ',' || r == ' ' })...)
		return map[string]any{"ok": true}, st.setPipelineTrust(list)
	case "POST /api/open-privacy":
		return map[string]any{"ok": true}, openPrivacySettings(strField(body, "pane"))
	case "POST /api/autostart":
		return map[string]any{"ok": true}, setAutostart(boolField(body, "enabled"))
	case "POST /api/restart":
		go restartSelf()
		return map[string]any{"ok": true}, nil
	}
	return nil, errors.New("unknown action")
}

func (s *settingsServer) stateJSON() map[string]any {
	if s.files == nil {
		out := map[string]any{"os": runtime.GOOS, "version": daemonVersion, "shares": []any{}, "sites": []any{}, "questions": []any{},
			"activity": []any{}, "direct": directConfig{}, "pipelineTrust": []string{}}
		s.connectionsState(out)
		for k, v := range s.state() {
			out[k] = v
		}
		return out
	}
	snap := s.files.store.snapshot()
	shares := []map[string]any{}
	for _, sh := range snap.Shares {
		shares = append(shares, map[string]any{"config": sh, "state": shareState(sh.Path), "used": s.files.usage.used(sh),
			"writeRisk": riskyForWriting(sh.Path), "network": isNetworkVolume(sh.Path)})
	}
	sort.Slice(snap.Sites, func(i, j int) bool { return snap.Sites[i].Added.Before(snap.Sites[j].Added) })
	out := map[string]any{
		"os": runtime.GOOS, "version": daemonVersion, "shares": shares, "sites": snap.Sites,
		"questions": s.files.asker.open(), "activity": s.files.activity.recent(), "direct": snap.Direct,
		"pipelineTrust": snap.PipelineTrust, "fingerprint": fingerprint(s.files.key.PublicKey().Bytes()),
		"autostart":        autostartEnabled(),
		"autostartManaged": isPackaged(),
	}
	if exe, err := os.Executable(); err == nil {
		out["exe"] = exe // what to add to Full Disk Access by hand on macOS
	}
	s.connectionsState(out)
	if s.state != nil {
		for k, v := range s.state() {
			out[k] = v
		}
	}
	return out
}

// typedFolderExists: a path typed or pasted on the settings page is shared only when it names
// a folder that is there - a typo must not become a share that reads "Folder not found".
func typedFolderExists(p string) error {
	fi, err := os.Stat(strings.TrimSpace(p))
	switch {
	case err == nil && !fi.IsDir():
		return errors.New("that is a file - share the folder it is in")
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("there is no such folder - check the path")
	}
	return nil // there but not readable yet (macOS privacy, permissions): share it, the card says why
}

// browse lists the folders in one folder for the page's own folder browser (never files:
// what is shared is always a folder). An empty path lists the starting places.
func browse(p string) (any, error) {
	if strings.TrimSpace(p) == "" {
		return map[string]any{"path": "", "dirs": browseRoots()}, nil
	}
	p = filepath.Clean(p)
	entries, err := os.ReadDir(p)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			return map[string]any{"path": p, "parent": parentOf(p), "dirs": []any{}, "denied": true}, nil
		}
		return nil, err
	}
	dirs := []map[string]string{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "$") {
			dirs = append(dirs, map[string]string{"name": e.Name(), "path": filepath.Join(p, e.Name())})
		}
	}
	return map[string]any{"path": p, "parent": parentOf(p), "dirs": dirs}, nil
}

func parentOf(p string) string {
	parent := filepath.Dir(p)
	if parent == p {
		return ""
	}
	return parent
}

func browseRoots() []map[string]string {
	out := []map[string]string{}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, map[string]string{"name": "Home (" + filepath.Base(home) + ")", "path": home})
		for _, sub := range []string{"Desktop", "Documents", "Downloads", "Pictures"} {
			if fi, err := os.Stat(filepath.Join(home, sub)); err == nil && fi.IsDir() {
				out = append(out, map[string]string{"name": sub, "path": filepath.Join(home, sub)})
			}
		}
	}
	switch runtime.GOOS {
	case "windows":
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if _, err := os.Stat(root); err == nil {
				out = append(out, map[string]string{"name": "Disk " + string(c) + ":", "path": root})
			}
		}
	case "darwin":
		out = append(out, map[string]string{"name": "Volumes", "path": "/Volumes"})
	default:
		for _, p := range []string{"/media", "/mnt", "/"} {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				out = append(out, map[string]string{"name": p, "path": p})
			}
		}
	}
	return out
}

// restartSelf starts this program again with the same arguments and leaves - what macOS
// sometimes needs before a newly granted folder permission applies.
func restartSelf() {
	time.Sleep(300 * time.Millisecond)
	self, err := os.Executable()
	if err != nil {
		return
	}
	args := os.Args[1:]
	cmd := proc.Command(self, args...)
	if cmd.Start() == nil {
		os.Exit(0)
	}
}
