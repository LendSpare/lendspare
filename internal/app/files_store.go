package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What this computer shares and with whom (docs/features/29-computer-files.md). This file is
// the only place a share or a site's access can come from: it is written by the local settings
// page and never by anything the server sends, so a compromised server cannot add a folder or
// let itself in.

const (
	conflictAsk   = "ask"   // ask the person at the computer when a newer file would replace one
	conflictNewer = "newer" // a newer incoming file replaces an older one, an older one never does
	conflictNever = "never" // never replace an existing file
	accessRead    = "read"
	accessWrite   = "write"
	defaultDirect = 47321
)

type shareConfig struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Enabled       bool   `json:"enabled"`
	Writable      bool   `json:"writable"`
	AllowDelete   bool   `json:"allowDelete"`
	AllowPrograms bool   `json:"allowPrograms"` // .exe, .sh, .lnk ... may be written
	AllowHidden   bool   `json:"allowHidden"`   // dot files and folders are visible
	OnConflict    string `json:"onConflict"`
	QuotaBytes    int64  `json:"quotaBytes"` // always set: sites fill the folder up to it and no further
	// Connections lists the connection ids this folder is granted to; nobody else is told it exists.
	Connections []string `json:"connections"`
}

// siteGrant: a site key the person approved, and for which shares.
type siteGrant struct {
	Key         string            `json:"key"` // base64url X25519 public key
	Fingerprint string            `json:"fingerprint"`
	Name        string            `json:"name"` // what it said it was when approved (not proof of anything)
	Shares      map[string]string `json:"shares"`
	Added       time.Time         `json:"added"`
	LastUsed    time.Time         `json:"lastUsed,omitempty"`
}

type directConfig struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
	// Addresses the owner made reachable from outside (a forwarded port, a DNS name), e.g.
	// "home.example.net:47321". Sites try them after the LAN ones; the seal still decides.
	Advertise []string `json:"advertise,omitempty"`
}

type filesSettings struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Shares        []shareConfig        `json:"shares"`
	Sites         []siteGrant          `json:"sites"`
	Direct        directConfig         `json:"direct"`
	PipelineTrust []string             `json:"pipelineTrust"` // image digests that may receive --pipeline-env
	Denied        map[string]time.Time `json:"denied"`        // site key + share: do not ask again until
}

type filesStore struct {
	mu       sync.Mutex
	path     string
	s        filesSettings
	onChange func() // the daemon reports what it shares again
}

func filesSettingsPath() string { return filepath.Join(configDir(), "files.json") }

func openFilesStore(path string) (*filesStore, error) {
	st := &filesStore{path: path, s: filesSettings{Direct: directConfig{Port: defaultDirect}, Denied: map[string]time.Time{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &st.s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if st.s.Denied == nil {
		st.s.Denied = map[string]time.Time{}
	}
	if st.s.Direct.Port == 0 {
		st.s.Direct.Port = defaultDirect
	}
	return st, nil
}

// save writes the settings readable by this user only, through a temp file.
func (st *filesStore) save() error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	st.s.SchemaVersion = 2
	b, _ := json.MarshalIndent(st.s, "", "  ")
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

func (st *filesStore) changed() error {
	err := st.save()
	if st.onChange != nil {
		go st.onChange()
	}
	return err
}

func (st *filesStore) snapshot() filesSettings {
	st.mu.Lock()
	defer st.mu.Unlock()
	b, _ := json.Marshal(st.s)
	var out filesSettings
	_ = json.Unmarshal(b, &out)
	return out
}

func (st *filesStore) share(name string) (shareConfig, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.s.Shares {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return shareConfig{}, false
}

// --- shares ------------------------------------------------------------------------------

// cleanShareName: what a site and an agent call the folder. No slashes, no dots alone.
func cleanShareName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return -1
		}
		return r
	}, name))
	if name == "." || name == ".." {
		return ""
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	return name
}

// addShare shares a folder (read-only, asking on conflicts) under the given name or its own.
func (st *filesStore) addShare(path, name string) (shareConfig, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || strings.TrimSpace(path) == "" {
		return shareConfig{}, errors.New("give the full path of a folder")
	}
	abs = filepath.Clean(abs)
	if reason := forbiddenShare(abs); reason != "" {
		return shareConfig{}, errors.New(reason)
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		return shareConfig{}, errors.New("that is a file - share the folder it is in")
	}
	if name = cleanShareName(name); name == "" {
		name = cleanShareName(filepath.Base(abs))
	}
	if name == "" {
		name = "share"
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, s := range st.s.Shares {
		if samePath(s.Path, abs) {
			if s.Enabled {
				return shareConfig{}, fmt.Errorf("this folder is already shared as %q", s.Name)
			}
			// Unticked "Shared" is how a folder stops being shared (the page has no remove
			// button); choosing it again ticks it back, with the settings it had.
			st.s.Shares[i].Enabled = true
			return st.s.Shares[i], st.changed()
		}
	}
	name = st.uniqueShareName(name, "")
	sc := shareConfig{Name: name, Path: abs, Enabled: true, OnConflict: conflictAsk, QuotaBytes: defaultQuota}
	st.s.Shares = append(st.s.Shares, sc)
	return sc, st.changed()
}

func (st *filesStore) uniqueShareName(name, except string) string {
	taken := map[string]bool{}
	for _, s := range st.s.Shares {
		if !strings.EqualFold(s.Name, except) {
			taken[strings.ToLower(s.Name)] = true
		}
	}
	out := name
	for n := 2; taken[strings.ToLower(out)]; n++ {
		out = fmt.Sprintf("%s %d", name, n)
	}
	return out
}

// updateShare applies the settings page's form. Renaming carries the sites' access along.
func (st *filesStore) updateShare(name string, upd shareConfig) (shareConfig, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, s := range st.s.Shares {
		if !strings.EqualFold(s.Name, name) {
			continue
		}
		newName := cleanShareName(upd.Name)
		if newName == "" {
			newName = s.Name
		}
		if !strings.EqualFold(newName, s.Name) {
			for _, o := range st.s.Shares {
				if strings.EqualFold(o.Name, newName) {
					return shareConfig{}, fmt.Errorf("another folder is already called %q", newName)
				}
			}
			for gi := range st.s.Sites {
				if mode, ok := st.s.Sites[gi].Shares[s.Name]; ok {
					delete(st.s.Sites[gi].Shares, s.Name)
					st.s.Sites[gi].Shares[newName] = mode
				}
			}
		}
		if upd.Writable && riskyForWriting(s.Path) != "" {
			return shareConfig{}, errors.New(riskyForWriting(s.Path))
		}
		switch upd.OnConflict {
		case conflictAsk, conflictNewer, conflictNever:
		default:
			upd.OnConflict = conflictAsk
		}
		if upd.QuotaBytes <= 0 {
			upd.QuotaBytes = s.QuotaBytes // a folder always has a quota
		}
		if upd.QuotaBytes <= 0 {
			upd.QuotaBytes = defaultQuota
		}
		upd.Name, upd.Path = newName, s.Path
		st.s.Shares[i] = upd
		return upd, st.changed()
	}
	return shareConfig{}, errors.New("no such shared folder")
}

func (st *filesStore) removeShare(name string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, s := range st.s.Shares {
		if strings.EqualFold(s.Name, name) {
			st.s.Shares = append(st.s.Shares[:i], st.s.Shares[i+1:]...)
			for gi := range st.s.Sites {
				delete(st.s.Sites[gi].Shares, s.Name)
			}
			return st.changed()
		}
	}
	return errors.New("no such shared folder")
}

// --- sites -------------------------------------------------------------------------------

// access says what this site may do with this share: "", "read" or "write".
func (st *filesStore) access(siteKey, share string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, g := range st.s.Sites {
		if g.Key == siteKey {
			for name, mode := range g.Shares {
				if strings.EqualFold(name, share) {
					return mode
				}
			}
		}
	}
	return ""
}

func (st *filesStore) touchSite(siteKey string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.s.Sites {
		if st.s.Sites[i].Key == siteKey && time.Since(st.s.Sites[i].LastUsed) > time.Minute {
			st.s.Sites[i].LastUsed = time.Now()
			_ = st.save()
		}
	}
}

// grant is the person's Allow: this site, this share, read or write.
func (st *filesStore) grant(siteKey, fingerprint, siteName, share, mode string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.s.Denied, siteKey+"|"+strings.ToLower(share))
	for i := range st.s.Sites {
		if st.s.Sites[i].Key == siteKey {
			if st.s.Sites[i].Shares == nil {
				st.s.Sites[i].Shares = map[string]string{}
			}
			if st.s.Sites[i].Shares[share] != accessWrite {
				st.s.Sites[i].Shares[share] = mode
			}
			return st.changed()
		}
	}
	st.s.Sites = append(st.s.Sites, siteGrant{Key: siteKey, Fingerprint: fingerprint, Name: siteName,
		Shares: map[string]string{share: mode}, Added: time.Now()})
	return st.changed()
}

// setSiteAccess changes or removes ("" mode) one site's access to one share; the settings page.
func (st *filesStore) setSiteAccess(siteKey, share, mode string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.s.Sites {
		if st.s.Sites[i].Key != siteKey {
			continue
		}
		if mode == "" {
			delete(st.s.Sites[i].Shares, share)
		} else {
			st.s.Sites[i].Shares[share] = mode
		}
		if len(st.s.Sites[i].Shares) == 0 {
			st.s.Sites = append(st.s.Sites[:i], st.s.Sites[i+1:]...)
		}
		return st.changed()
	}
	return errors.New("no such site")
}

func (st *filesStore) revokeSite(siteKey string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.s.Sites {
		if st.s.Sites[i].Key == siteKey {
			st.s.Sites = append(st.s.Sites[:i], st.s.Sites[i+1:]...)
			return st.changed()
		}
	}
	return errors.New("no such site")
}

// deny remembers a Deny for a day, so a site (or a server pretending to be one) cannot make
// the computer ask again and again.
func (st *filesStore) deny(siteKey, share string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Denied[siteKey+"|"+strings.ToLower(share)] = time.Now().Add(24 * time.Hour)
	_ = st.save()
}

func (st *filesStore) denied(siteKey, share string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	until, ok := st.s.Denied[siteKey+"|"+strings.ToLower(share)]
	return ok && time.Now().Before(until)
}

// --- other settings ----------------------------------------------------------------------

func (st *filesStore) setDirect(enabled bool, port int, advertise []string) error {
	if port < 1024 || port > 65535 {
		port = defaultDirect
	}
	clean := []string{}
	for _, a := range advertise {
		if a = strings.TrimSpace(a); validHostPort(a) && len(clean) < 2 {
			clean = append(clean, a)
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Direct = directConfig{Enabled: enabled, Port: port, Advertise: clean}
	return st.changed()
}

// validHostPort: "name.example:47321" or "203.0.113.5:47321", a port of 1024 or more.
func validHostPort(a string) bool {
	host, port, err := net.SplitHostPort(a)
	if err != nil || host == "" || strings.ContainsAny(host, "/@ ") {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1024 && n <= 65535
}

// trustsPipelineImage: may this image receive the seller's API keys (--pipeline-env)?
func (st *filesStore) trustsPipelineImage(digest string, fromFlag []string) bool {
	digest = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(digest), "sha256:"))
	if digest == "" {
		return false
	}
	st.mu.Lock()
	list := append(append([]string{}, st.s.PipelineTrust...), fromFlag...)
	st.mu.Unlock()
	for _, d := range list {
		if strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "sha256:")) == digest {
			return true
		}
	}
	return false
}

func (st *filesStore) setPipelineTrust(digests []string) error {
	clean := []string{}
	for _, d := range digests {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "sha256:"))
		if len(d) == 64 && strings.Trim(d, "0123456789abcdef") == "" {
			clean = append(clean, "sha256:"+d)
		}
	}
	sort.Strings(clean)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.PipelineTrust = clean
	return st.changed()
}

// --- what may not be shared --------------------------------------------------------------

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// within: is p the same as dir or inside it?
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		rel2, err2 := filepath.Rel(strings.ToLower(dir), strings.ToLower(p))
		if err2 == nil {
			rel = rel2
		}
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// forbiddenShare: folders that may never be shared at all - this program's own directory
// (it holds the machine token and the private key) or anything containing it.
func forbiddenShare(abs string) string {
	cfg := configDir()
	if within(abs, cfg) || within(cfg, abs) {
		return "this folder holds (or is inside) this program's own settings, its token and its key - it cannot be shared"
	}
	return ""
}

// riskyForWriting: folders a site may read but never write into, because a file written
// there runs the next time somebody logs in or opens a terminal.
func riskyForWriting(abs string) string {
	home, _ := os.UserHomeDir()
	vol := filepath.VolumeName(abs)
	if abs == vol+string(filepath.Separator) || abs == "/" {
		return "a whole disk cannot be shared for writing - share a folder in it"
	}
	if home != "" && samePath(abs, home) {
		return "your whole home folder cannot be shared for writing - share a folder in it"
	}
	risky := []string{}
	switch runtime.GOOS {
	case "windows":
		risky = append(risky, os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"),
			os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramData"))
	case "darwin":
		risky = append(risky, "/System", "/Library", "/Applications", "/usr", "/bin", "/etc",
			filepath.Join(home, "Library"))
	default:
		risky = append(risky, "/etc", "/usr", "/bin", "/sbin", "/lib", "/boot", "/var", "/opt",
			filepath.Join(home, ".config"), filepath.Join(home, ".local"))
	}
	for _, r := range risky {
		if r != "" && (within(abs, r) || within(r, abs)) {
			return "this folder holds programs or settings that run by themselves - it can be shared for reading only"
		}
	}
	return ""
}

// --- grants to connections -----------------------------------------------------------------

// grantedTo: is this folder offered through this connection?
func (s shareConfig) grantedTo(conn string) bool { return conn != "" && contains(s.Connections, conn) }

// shareFor finds a folder by name only if it is granted to the connection asking.
func (st *filesStore) shareFor(name, conn string) (shareConfig, bool) {
	s, ok := st.share(name)
	if !ok || !s.grantedTo(conn) {
		return shareConfig{}, false
	}
	return s, true
}

// setGrant grants a folder to one connection or takes it back; other connections keep theirs.
func (st *filesStore) setGrant(name, conn string, on bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, s := range st.s.Shares {
		if !strings.EqualFold(s.Name, name) {
			continue
		}
		kept := []string{}
		for _, c := range s.Connections {
			if c != conn {
				kept = append(kept, c)
			}
		}
		if on {
			kept = append(kept, conn)
		}
		st.s.Shares[i].Connections = kept
		return st.changed()
	}
	return errors.New("no such shared folder")
}

// forgetConnection drops a removed connection from every folder's grants.
func (st *filesStore) forgetConnection(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.s.Shares {
		kept := []string{}
		for _, c := range st.s.Shares[i].Connections {
			if c != id {
				kept = append(kept, c)
			}
		}
		st.s.Shares[i].Connections = kept
	}
	_ = st.save()
}
