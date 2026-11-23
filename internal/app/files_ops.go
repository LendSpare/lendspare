package app

import (
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Serving one sealed request against the shared folders (docs/features/29-computer-files.md).
//
// Every file operation goes through an os.Root opened at the share: the walk refuses to leave
// it, so `..`, an absolute path, a symlink or junction pointing elsewhere cannot reach the rest
// of the disk however the request is worded - by a site, a server, or an agent talked into it.

// filesTransport is how a session's bytes move: through the gateway (relay) or straight over
// the LAN (direct). The request has already arrived; this is the rest of the conversation.
type filesTransport interface {
	reply(sealed []byte, done bool) error
	nextUp(n uint64) (data []byte, last bool, err error) // blocks until chunk n of the site's upload
	putDown(n uint64, data []byte, last bool) error      // blocks while the site is behind
}

type fromInfo struct {
	Site          string `json:"site"`
	Key           string `json:"key"`
	App           string `json:"app"`
	SharedAccount string `json:"sharedAccount"`
}

type opRequest struct {
	ID         string `json:"id"`
	TS         int64  `json:"ts"`
	Op         string `json:"op"`
	Share      string `json:"share"`
	Path       string `json:"path"`
	Recursive  bool   `json:"recursive"`
	Limit      int    `json:"limit"`
	Size       int64  `json:"size"`
	Mtime      int64  `json:"mtime"`
	OnConflict string `json:"onConflict"` // the site's side: "newer" only when it may be destructive
	// Mailboxes the site offers for this session (its own endpoint, its own relay), in order of
	// preference. Inside the seal, so only the approved site can name them.
	Paths []mailboxPath `json:"paths,omitempty"`
	// The quota of the site folder a read goes into (aMount sends it): logged and shown here,
	// so both sides know each other's limits. The site enforces its own.
	Dest *struct {
		Folder string `json:"folder"`
		Quota  int64  `json:"quota"`
		Used   int64  `json:"used"`
	} `json:"dest,omitempty"`
	Site struct {
		Name string `json:"name"`
	} `json:"site"`
}

type fileEntry struct {
	Path  string `json:"path"`
	Dir   bool   `json:"dir,omitempty"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// opError is an answer, not a failure of the program: it goes back sealed to the site.
type opError struct {
	Code    string
	Message string
	Extra   map[string]any
}

func (e *opError) Error() string { return e.Code + ": " + e.Message }

func refuse(code, msg string) *opError { return &opError{Code: code, Message: msg} }

type filesService struct {
	store    *filesStore
	key      *ecdh.PrivateKey
	asker    *asker
	activity *activityLog
	usage    *usageTracker
	provider func(conn string) string // host of a connection, for the activity log
	seen     sync.Map                 // request id -> time, against a replayed request
}

const requestMaxAge = 10 * time.Minute

// handle opens one sealed request and serves it to the end. `mode` is what the gateway (or
// the direct ticket) says the session may do; the request must not ask for more.
func (fsv *filesService) handle(conn string, t filesTransport, mode string, envelope []byte, from fromInfo) error {
	keys, plain, err := openRequest(fsv.key, envelope)
	if err != nil {
		return err
	}
	var req opRequest
	if err := json.Unmarshal(plain, &req); err != nil || keys.bindRequestID(req.ID) != nil {
		return t.reply(keys.sealResponse(errorJSON(refuse("bad_request", "the request could not be read"))), true)
	}
	if age := time.Since(time.Unix(req.TS, 0)); age > requestMaxAge || age < -requestMaxAge {
		return t.reply(keys.sealResponse(errorJSON(refuse("too_old", "the request is too old (or the clocks disagree)"))), true)
	}
	if _, dup := fsv.seen.LoadOrStore(req.ID, time.Now()); dup {
		return t.reply(keys.sealResponse(errorJSON(refuse("replayed", "this request was already served"))), true)
	}
	fsv.forgetOld()
	site := b64url(keys.sitePub)
	// A peer that can be a server is one: the site's own mailbox (or its own relay) when this
	// computer reaches it, and the gateway only when nothing closer answers.
	via := "relay"
	if len(req.Paths) > 0 {
		var chosen filesTransport
		if chosen, via = pickTransport(t, req.Paths); chosen != t {
			_ = t.reply(keys.sealResponse(okJSON(map[string]any{"status": "moved", "via": "mailbox"})), true)
			t = chosen
		}
	}
	answer, done, err := fsv.serve(conn, t, keys, mode, site, &req, from)
	entry := activityEntry{Provider: fsv.providerOf(conn), Site: siteLabel(req.Site.Name, keys.sitePub), Op: req.Op, Share: req.Share, Path: req.Path, Result: "ok", Via: via}
	if oe := (*opError)(nil); errors.As(err, &oe) {
		entry.Result, entry.Message = oe.Code, oe.Message
		answer, done, err = errorJSON(oe), true, nil
	} else if err != nil {
		// The transfer broke (the site went away, the network): still not a silent failure.
		entry.Result, entry.Message = "transfer_failed", err.Error()
	}
	fsv.activity.add(entry)
	if err != nil {
		return err
	}
	if answer != nil {
		return t.reply(keys.sealResponse(answer), done)
	}
	return nil
}

func (fsv *filesService) forgetOld() {
	fsv.seen.Range(func(k, v any) bool {
		if time.Since(v.(time.Time)) > 2*requestMaxAge {
			fsv.seen.Delete(k)
		}
		return true
	})
}

func siteLabel(name string, pub []byte) string {
	name = strings.TrimSpace(name)
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "a site"
	}
	return name + " (" + fingerprint(pub) + ")"
}

func errorJSON(e *opError) []byte {
	out := map[string]any{"ok": false, "error": e.Code, "message": e.Message}
	for k, v := range e.Extra {
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return b
}

func okJSON(fields map[string]any) []byte {
	fields["ok"] = true
	b, _ := json.Marshal(fields)
	return b
}

// serve does the operation; it returns the final answer (nil when it already replied itself).
func (fsv *filesService) serve(conn string, t filesTransport, keys *sessionKeys, mode, site string, req *opRequest, from fromInfo) ([]byte, bool, error) {
	if req.Op == "shares" {
		return fsv.listShares(site, conn), true, nil
	}
	needs := accessRead
	switch req.Op {
	case "list", "stat", "read":
	case "write", "mkdir", "delete":
		needs = accessWrite
	default:
		return nil, true, refuse("bad_op", "unknown operation")
	}
	if needs == accessWrite && mode != accessWrite {
		return nil, true, refuse("read_only_session", "this session may only read")
	}
	share, ok := fsv.store.shareFor(req.Share, conn)
	if !ok || !share.Enabled {
		return nil, true, refuse("not_shared", "this computer shares no folder by that name")
	}
	if needs == accessWrite && !share.Writable {
		return nil, true, refuse("read_only", "this folder is shared for reading only")
	}
	if err := fsv.authorise(site, keys.sitePub, req, share, needs, from); err != nil {
		return nil, true, err
	}
	fsv.store.touchSite(site)
	rel, err := cleanRel(req.Path, share.AllowHidden)
	if err != nil {
		return nil, true, err
	}
	root, err := openShareRoot(share)
	if err != nil {
		return nil, true, err
	}
	defer root.Close()
	switch req.Op {
	case "list":
		return fsv.list(root, share, rel, req)
	case "stat":
		fi, err := root.Stat(osRel(rel))
		if err != nil {
			return nil, true, ioRefusal(err)
		}
		return okJSON(map[string]any{"entry": entryOf(rel, fi)}), true, nil
	case "read":
		if req.Dest != nil {
			log.Printf("[files] %s reads %s/%s into its folder %q (quota %s, %s used)", siteLabel(req.Site.Name, keys.sitePub),
				share.Name, rel, req.Dest.Folder, humanSize(req.Dest.Quota), humanSize(req.Dest.Used))
		}
		return nil, true, fsv.read(t, keys, root, rel)
	case "mkdir":
		if err := checkTargetName(root, rel, share); err != nil {
			return nil, true, err
		}
		if err := root.MkdirAll(osRel(rel), 0o755); err != nil {
			return nil, true, ioRefusal(err)
		}
		return okJSON(map[string]any{"created": rel}), true, nil
	case "delete":
		if !share.AllowDelete {
			return nil, true, refuse("delete_disabled", "deleting is not allowed in this folder (the computer's settings)")
		}
		if rel == "." {
			return nil, true, refuse("bad_path", "the shared folder itself cannot be deleted")
		}
		var size int64
		if fi, err := root.Stat(osRel(rel)); err == nil && !fi.IsDir() {
			size = fi.Size()
		}
		if err := root.Remove(osRel(rel)); err != nil {
			return nil, true, ioRefusal(err)
		}
		fsv.usage.adjust(share, -size)
		return okJSON(map[string]any{"deleted": rel}), true, nil
	default: // write
		return nil, true, fsv.write(t, keys, root, share, site, rel, req, from)
	}
}

// authorise: an approved site goes on; an unknown one is put to the person at the computer
// and told to come back once they decided. The question carries the fingerprint, the only
// thing about the site the person can trust.
func (fsv *filesService) authorise(site string, sitePub []byte, req *opRequest, share shareConfig, needs string, from fromInfo) error {
	have := fsv.store.access(site, share.Name)
	if have == accessWrite || (have == accessRead && needs == accessRead) {
		return nil
	}
	if fsv.store.denied(site, share.Name) {
		return refuse("denied", "the person at this computer did not allow this site")
	}
	q := fsv.asker.askApproval(approvalQuestion{SiteKey: site, Fingerprint: fingerprint(sitePub), SiteName: req.Site.Name,
		Share: share.Name, Mode: needs, From: from})
	return &opError{Code: "approval_pending", Message: "the person at this computer has been asked to allow this site; try again once they have",
		Extra: map[string]any{"fingerprint": q.Fingerprint, "question": q.ID}}
}

func (fsv *filesService) listShares(site, conn string) []byte {
	out := []map[string]any{}
	for _, s := range fsv.store.snapshot().Shares {
		if !s.Enabled || !s.grantedTo(conn) {
			continue
		}
		out = append(out, map[string]any{"name": s.Name, "writable": s.Writable, "access": fsv.store.access(site, s.Name),
			"onConflict": s.OnConflict, "state": shareState(s.Path), "quota": quotaOf(s), "used": fsv.usage.used(s)})
	}
	return okJSON(map[string]any{"shares": out})
}

// --- paths ------------------------------------------------------------------------------

// cleanRel turns a site's path into one inside the share, or refuses: no absolute paths, no
// drive letters, no `..`, no alternate data streams, no hidden files unless the share allows.
func cleanRel(p string, allowHidden bool) (string, error) {
	// Not trimmed: "report " is not "report", and quietly writing another name than the one
	// asked for is exactly the silent failure this must not have.
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.ContainsAny(p, "\x00:") {
		return "", refuse("bad_path", "that path cannot be in a shared folder")
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return ".", nil
	}
	clean := path.Clean(p)
	for _, part := range strings.Split(clean, "/") {
		if part == ".." || part == "" {
			return "", refuse("bad_path", "that path leaves the shared folder")
		}
		if strings.HasPrefix(part, ".") && !allowHidden {
			return "", refuse("hidden", "hidden files are not shared from this folder")
		}
	}
	return clean, nil
}

func osRel(rel string) string { return filepath.FromSlash(rel) }

func openShareRoot(share shareConfig) (*os.Root, error) {
	switch shareState(share.Path) {
	case "missing":
		return nil, refuse("share_missing", "the shared folder is not there (a disk unplugged or a network share disconnected?)")
	case "offline":
		return nil, refuse("share_offline", "the shared folder is on a network share that does not answer right now")
	case "noaccess":
		return nil, refuse("share_noaccess", "the account this program runs as may not read the shared folder")
	case "denied":
		msg := "this program may not read the shared folder"
		if runtime.GOOS == "darwin" {
			msg = "macOS has not allowed this program to read the shared folder yet (Privacy & Security, Files and Folders)"
		}
		return nil, refuse("share_denied", msg)
	}
	root, err := os.OpenRoot(share.Path)
	if err != nil {
		return nil, ioRefusal(err)
	}
	return root, nil
}

func quotaOf(s shareConfig) int64 {
	if s.QuotaBytes <= 0 {
		return defaultQuota
	}
	return s.QuotaBytes
}

func ioRefusal(err error) *opError {
	switch {
	case isDiskFull(err):
		return refuse("disk_full", "the disk of this computer is full")
	case errors.Is(err, fs.ErrNotExist):
		return refuse("not_found", "no such file or folder")
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EPERM):
		return refuse("no_permission", "this program may not read or write that")
	case strings.Contains(err.Error(), "path escapes"):
		return refuse("bad_path", "that path leaves the shared folder")
	default:
		return refuse("io_error", "the file could not be read or written")
	}
}

func entryOf(rel string, fi fs.FileInfo) fileEntry {
	e := fileEntry{Path: rel, Mtime: fi.ModTime().Unix()}
	if fi.IsDir() {
		e.Dir = true
	} else {
		e.Size = fi.Size()
	}
	return e
}

// --- list -------------------------------------------------------------------------------

func (fsv *filesService) list(root *os.Root, share shareConfig, rel string, req *opRequest) ([]byte, bool, error) {
	limit := req.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	entries := []fileEntry{}
	truncated := false
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		f, err := root.Open(osRel(dir))
		if err != nil {
			return err
		}
		items, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })
		for _, it := range items {
			name := it.Name()
			if (strings.HasPrefix(name, ".") && !share.AllowHidden) || strings.Contains(name, ".lendspare-part-") || strings.Contains(name, ".share-part-") || strings.Contains(name, ".mesh-part-") {
				continue
			}
			child := name
			if dir != "." {
				child = dir + "/" + name
			}
			fi, err := root.Stat(osRel(child)) // follows a link only while it stays inside the share
			if err != nil {
				continue
			}
			if len(entries) >= limit {
				truncated = true
				return nil
			}
			entries = append(entries, entryOf(child, fi))
			if fi.IsDir() && req.Recursive && depth < 32 {
				if err := walk(child, depth+1); err != nil {
					continue
				}
			}
		}
		return nil
	}
	if err := walk(rel, 0); err != nil {
		return nil, true, ioRefusal(err)
	}
	return okJSON(map[string]any{"entries": entries, "truncated": truncated}), true, nil
}

// --- read -------------------------------------------------------------------------------

func (fsv *filesService) read(t filesTransport, keys *sessionKeys, root *os.Root, rel string) error {
	f, err := root.Open(osRel(rel))
	if err != nil {
		return ioRefusal(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ioRefusal(err)
	}
	if fi.IsDir() {
		return refuse("is_directory", "that is a folder - list it, then read its files")
	}
	chunks := (fi.Size() + filesChunkMax - 1) / filesChunkMax
	if chunks == 0 {
		chunks = 1
	}
	if err := t.reply(keys.sealResponse(okJSON(map[string]any{"size": fi.Size(), "mtime": fi.ModTime().Unix(),
		"chunks": chunks})), false); err != nil {
		return err
	}
	buf := make([]byte, filesChunkMax)
	for n := uint64(0); n < uint64(chunks); n++ {
		got, err := io.ReadFull(f, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return err
		}
		last := n == uint64(chunks)-1
		if err := t.putDown(n, keys.sealDown(n, last, buf[:got]), last); err != nil {
			return err
		}
	}
	return t.reply(keys.sealResponse(okJSON(map[string]any{"size": fi.Size(), "mtime": fi.ModTime().Unix(),
		"chunks": chunks, "complete": true})), true)
}

// --- write ------------------------------------------------------------------------------

// Programs and files that run by themselves: refused unless the share allows programs.
var programExts = map[string]bool{
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".ps1": true, ".psm1": true, ".psd1": true, ".vbs": true,
	".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".msi": true, ".msp": true, ".msix": true,
	".appx": true, ".scr": true, ".pif": true, ".cpl": true, ".lnk": true, ".url": true, ".reg": true, ".hta": true,
	".jar": true, ".dll": true, ".sys": true, ".drv": true, ".ocx": true, ".inf": true, ".sh": true, ".bash": true,
	".zsh": true, ".fish": true, ".command": true, ".tool": true, ".app": true, ".pkg": true, ".dmg": true,
	".desktop": true, ".appimage": true, ".run": true, ".deb": true, ".rpm": true, ".py": true, ".pyw": true,
	".pl": true, ".rb": true, ".scpt": true, ".applescript": true, ".workflow": true, ".action": true,
	".library-ms": true, ".settingcontent-ms": true, ".iso": true, ".img": true, ".vhd": true, ".vhdx": true,
}

var programNames = map[string]bool{"autorun.inf": true, "desktop.ini": true, ".bashrc": true, ".profile": true,
	".zshrc": true, ".bash_profile": true}

func checkWritableName(rel string, share shareConfig) error {
	if share.AllowPrograms {
		return nil
	}
	for _, part := range strings.Split(rel, "/") {
		lower := strings.ToLower(part)
		if programNames[lower] || programExts[strings.ToLower(path.Ext(lower))] {
			return refuse("program_refused", "programs and scripts cannot be written into this folder (the computer's settings)")
		}
	}
	return nil
}

const askTimeout = 10 * time.Minute

func (fsv *filesService) write(t filesTransport, keys *sessionKeys, root *os.Root, share shareConfig, site, rel string, req *opRequest, from fromInfo) error {
	if rel == "." {
		return refuse("bad_path", "name a file to write")
	}
	if err := checkTargetName(root, rel, share); err != nil {
		return err
	}
	if req.Size < 0 {
		return refuse("bad_request", "bad size")
	}
	incoming := time.Unix(req.Mtime, 0)
	replace, err := fsv.decideConflict(t, keys, root, share, site, rel, req, incoming, from)
	if err != nil {
		return err
	}
	if replace == decideUnchanged {
		return t.reply(keys.sealResponse(okJSON(map[string]any{"unchanged": true})), true)
	}
	var replacing int64
	if fi, err := root.Stat(osRel(rel)); err == nil && replace == decideReplace {
		replacing = fi.Size()
	}
	if used := fsv.usage.usedNow(share); !fits(quotaOf(share), used, req.Size, replacing) {
		return quotaRefusal(quotaOf(share), used)
	}
	if dir := path.Dir(rel); dir != "." {
		if err := root.MkdirAll(osRel(dir), 0o755); err != nil {
			return ioRefusal(err)
		}
	}
	tmp := path.Join(path.Dir(rel), "."+path.Base(rel)+".lendspare-part-"+keysuffix(keys))
	f, err := root.OpenFile(osRel(tmp), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return ioRefusal(err)
	}
	cleanup := func() { f.Close(); _ = root.Remove(osRel(tmp)) }
	// Ready: the site uploads only now, and over the path this answer came by.
	if err := t.reply(keys.sealResponse(okJSON(map[string]any{"status": "ready"})), false); err != nil {
		cleanup()
		return err
	}
	var written int64
	for n := uint64(0); ; n++ {
		data, last, err := t.nextUp(n)
		if err != nil {
			cleanup()
			return err
		}
		plain, err := keys.openUp(n, last, data)
		if err != nil {
			cleanup()
			return refuse("integrity", err.Error())
		}
		if written+int64(len(plain)) > req.Size {
			cleanup()
			return refuse("size_mismatch", "more bytes arrived than announced")
		}
		if _, err := f.Write(plain); err != nil {
			cleanup()
			return ioRefusal(err)
		}
		written += int64(len(plain))
		if last {
			break
		}
	}
	if err := f.Close(); err != nil || written != req.Size {
		_ = root.Remove(osRel(tmp))
		return refuse("size_mismatch", "the file arrived incomplete")
	}
	if req.Mtime > 0 {
		_ = root.Chtimes(osRel(tmp), incoming, incoming)
	}
	// Somebody may have changed the file while it was arriving: decide again, quietly.
	if fi, err := root.Stat(osRel(rel)); err == nil && replace == decideCreate {
		_ = root.Remove(osRel(tmp))
		return &opError{Code: "conflict", Message: "a file appeared there while this one was arriving",
			Extra: map[string]any{"existingMtime": fi.ModTime().Unix(), "existingSize": fi.Size()}}
	}
	if err := root.Rename(osRel(tmp), osRel(rel)); err != nil {
		_ = root.Remove(osRel(tmp))
		return ioRefusal(err)
	}
	fsv.usage.adjust(share, written-replacing)
	markDownloaded(filepath.Join(share.Path, osRel(rel)), from)
	log.Printf("[files] %s wrote %s/%s (%d bytes)", siteLabel(req.Site.Name, keys.sitePub), share.Name, rel, written)
	return t.reply(keys.sealResponse(okJSON(map[string]any{"written": true, "replaced": replace == decideReplace,
		"size": written})), true)
}

func keysuffix(k *sessionKeys) string { return fmt.Sprintf("%x", k.requestID[:6]) }

type decision int

const (
	decideCreate decision = iota
	decideReplace
	decideUnchanged
)

// decideConflict: create when nothing is there; the same file again is "unchanged"; otherwise
// a newer incoming file replaces the old one only if the site may be destructive (it says
// "newer") AND the computer agrees - its folder setting, or the person when it is "ask".
func (fsv *filesService) decideConflict(t filesTransport, keys *sessionKeys, root *os.Root, share shareConfig, site, rel string, req *opRequest, incoming time.Time, from fromInfo) (decision, error) {
	fi, err := root.Stat(osRel(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return decideCreate, nil
	}
	if err != nil {
		return 0, ioRefusal(err)
	}
	if fi.IsDir() {
		return 0, refuse("is_directory", "a folder of that name is there")
	}
	if fi.Size() == req.Size && fi.ModTime().Unix() == req.Mtime {
		return decideUnchanged, nil
	}
	conflict := func(why string) (decision, error) {
		return 0, &opError{Code: "conflict", Message: why, Extra: map[string]any{
			"existingMtime": fi.ModTime().Unix(), "existingSize": fi.Size()}}
	}
	if !incoming.After(fi.ModTime()) {
		return conflict("the file on this computer is newer (or as new) - it was kept")
	}
	if req.OnConflict != conflictNewer {
		return conflict("a file is already there and the site did not ask to replace it (destructive actions are not allowed for it)")
	}
	switch share.OnConflict {
	case conflictNewer:
		return decideReplace, nil
	case conflictNever:
		return conflict("this computer never replaces files in this folder")
	}
	if fsv.asker.replaceAllActive(site, share.Name) {
		return decideReplace, nil
	}
	_ = t.reply(keys.sealResponse(okJSON(map[string]any{"status": "asking",
		"message": "the person at this computer is asked whether to replace the file"})), false)
	switch fsv.asker.askConflict(conflictQuestion{SiteKey: site, SiteName: req.Site.Name, Fingerprint: fingerprint(keys.sitePub),
		Share: share.Name, Path: rel, ExistingMtime: fi.ModTime().Unix(), ExistingSize: fi.Size(),
		IncomingMtime: req.Mtime, IncomingSize: req.Size, From: from}, askTimeout) {
	case answerReplace, answerReplaceAll:
		return decideReplace, nil
	default:
		return conflict("the person at this computer kept the existing file")
	}
}

func (fsv *filesService) providerOf(conn string) string {
	if fsv.provider == nil {
		return ""
	}
	return fsv.provider(conn)
}
