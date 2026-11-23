package app

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Shared folders (docs/features/29-computer-files.md): the seal, what a share lets through,
// and the conversation with a site, played over an in-memory transport.

// fakeTransport is the site on the other end: it holds the upload, records every answer and
// every chunk sent back.
type fakeTransport struct {
	mu      sync.Mutex
	up      [][]byte
	upLast  int
	replies [][]byte
	done    []bool
	down    [][]byte
}

func (f *fakeTransport) reply(sealed []byte, done bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, sealed)
	f.done = append(f.done, done)
	return nil
}

func (f *fakeTransport) nextUp(n uint64) ([]byte, bool, error) {
	if int(n) >= len(f.up) {
		return nil, false, fmt.Errorf("no chunk %d", n)
	}
	return f.up[n], int(n) == f.upLast, nil
}

func (f *fakeTransport) putDown(n uint64, data []byte, last bool) error {
	f.down = append(f.down, data)
	return nil
}

// site plays one aMount installation: its static key, and the mirror of the computer's seal.
type site struct {
	t      *testing.T
	static *ecdh.PrivateKey
	name   string
}

func newSite(t *testing.T, name string) *site {
	k, _ := ecdh.X25519().GenerateKey(randReader{})
	return &site{t: t, static: k, name: name}
}

type randReader struct{}

// Read is crypto/rand: the old /dev/urandom read fell back to the clock on Windows, where two
// quick requests then got the same id and the second was refused as a replay.
func (randReader) Read(p []byte) (int, error) { return cryptorand.Read(p) }

type call struct {
	keys *sessionKeys
	id   string
	t    *fakeTransport
}

func (s *site) request(computer *ecdh.PublicKey, req map[string]any) (*call, []byte) {
	id := make([]byte, 16)
	_, _ = randReader{}.Read(id)
	req["id"] = hex.EncodeToString(id)
	if _, ok := req["ts"]; !ok {
		req["ts"] = time.Now().Unix()
	}
	req["site"] = map[string]any{"name": s.name}
	plain, _ := json.Marshal(req)
	eph, _ := ecdh.X25519().GenerateKey(randReader{})
	keys, env, err := siteSeal(s.static, eph, computer, plain)
	if err != nil {
		s.t.Fatal(err)
	}
	_ = keys.bindRequestID(req["id"].(string))
	return &call{keys: keys, id: req["id"].(string), t: &fakeTransport{}}, env
}

// answers opens every reply the computer sent, in order, checking each counter.
func (c *call) answers(t *testing.T) []map[string]any {
	out := []map[string]any{}
	for i, r := range c.t.replies {
		if binary.BigEndian.Uint64(r[:8]) != uint64(i) {
			t.Fatalf("reply %d carries counter %d", i, binary.BigEndian.Uint64(r[:8]))
		}
		plain, err := c.keys.toSite.Open(nil, nonce("rsp\x00", uint64(i)), r[8:], []byte(filesAADResp))
		if err != nil {
			t.Fatalf("reply %d does not open: %v", i, err)
		}
		var m map[string]any
		_ = json.Unmarshal(plain, &m)
		out = append(out, m)
	}
	return out
}

func (c *call) last(t *testing.T) map[string]any {
	a := c.answers(t)
	if len(a) == 0 {
		t.Fatal("no answer")
	}
	return a[len(a)-1]
}

func (c *call) upload(data []byte, chunk int) {
	if len(data) == 0 {
		c.t.up = [][]byte{c.keys.toComputer.Seal(nil, nonce("up\x00\x00", 0), nil, c.keys.chunkAAD(0, true))}
		return
	}
	for n := 0; n*chunk < len(data); n++ {
		end := min((n+1)*chunk, len(data))
		last := end == len(data)
		c.t.up = append(c.t.up, c.keys.toComputer.Seal(nil, nonce("up\x00\x00", uint64(n)), data[n*chunk:end], c.keys.chunkAAD(uint64(n), last)))
		if last {
			c.t.upLast = n
		}
	}
}

func (c *call) downloaded(t *testing.T) []byte {
	var out []byte
	for n, d := range c.t.down {
		plain, err := c.keys.toSite.Open(nil, nonce("dn\x00\x00", uint64(n)), d, c.keys.chunkAAD(uint64(n), n == len(c.t.down)-1))
		if err != nil {
			t.Fatalf("chunk %d does not open: %v", n, err)
		}
		out = append(out, plain...)
	}
	return out
}

const testConn = "c1" // the connection the test folders are granted to

// testFiles is a files service in a temp config dir, with one share and no desktop.
func testFiles(t *testing.T) (*filesService, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)
	t.Setenv("AppData", cfg)
	// On Windows every t.TempDir() is inside %LOCALAPPDATA%\Temp, which may only be shared
	// for reading (riskyForWriting): point it elsewhere, as AppData is above.
	t.Setenv("LOCALAPPDATA", filepath.Join(cfg, "Local"))
	store, err := openFilesStore(filepath.Join(cfg, "files.json"))
	if err != nil {
		t.Fatal(err)
	}
	key, _ := loadFilesKey(filepath.Join(cfg, "files.key"))
	shareDir := filepath.Join(t.TempDir(), "sqlbackups")
	_ = os.MkdirAll(filepath.Join(shareDir, "2025", "08"), 0o755)
	_ = os.WriteFile(filepath.Join(shareDir, "2025", "08", "db.sql.gz"), bytes.Repeat([]byte("x"), 10), 0o644)
	if _, err := store.addShare(shareDir, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.setGrant("sqlbackups", testConn, true); err != nil {
		t.Fatal(err)
	}
	fsv := &filesService{store: store, key: key, asker: newAsker(store, nil), activity: newActivityLog(""), usage: newUsageTracker()}
	return fsv, shareDir
}

func serve(t *testing.T, fsv *filesService, s *site, mode string, req map[string]any, upload []byte) *call {
	t.Helper()
	c, env := s.request(fsv.key.PublicKey(), req)
	if upload != nil {
		c.upload(upload, 4)
	}
	if err := fsv.handle(testConn, c.t, mode, env, fromInfo{Site: "https://site.com"}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	return c
}

func approve(t *testing.T, fsv *filesService) {
	t.Helper()
	qs := fsv.asker.open()
	if len(qs) == 0 {
		t.Fatal("nobody was asked")
	}
	if !fsv.asker.answer(qs[0].ID, answerAllow) {
		t.Fatal("answer refused")
	}
}

func TestAnUnknownSiteWaitsForThePersonThenReads(t *testing.T) {
	fsv, _ := testFiles(t)
	s := newSite(t, "site.com")

	got := serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups", "path": "2025", "recursive": true}, nil).last(t)
	if got["error"] != "approval_pending" || got["fingerprint"] != fingerprint(s.static.PublicKey().Bytes()) {
		t.Fatalf("answer = %v", got)
	}
	q := fsv.asker.open()[0]
	if q.Approval.Fingerprint != fingerprint(s.static.PublicKey().Bytes()) || q.Approval.Share != "sqlbackups" || q.Approval.SiteName != "site.com" {
		t.Fatalf("question = %+v", q.Approval)
	}
	// Asking again does not ask again.
	serve(t, fsv, s, "read", map[string]any{"op": "stat", "share": "sqlbackups", "path": "2025"}, nil)
	if len(fsv.asker.open()) != 1 {
		t.Fatalf("asked %d times", len(fsv.asker.open()))
	}
	approve(t, fsv)

	got = serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups", "path": "2025", "recursive": true}, nil).last(t)
	entries, _ := got["entries"].([]any)
	if got["ok"] != true || len(entries) != 2 {
		t.Fatalf("list = %v", got)
	}
	c := serve(t, fsv, s, "read", map[string]any{"op": "read", "share": "sqlbackups", "path": "2025/08/db.sql.gz"}, nil)
	if string(c.downloaded(t)) != "xxxxxxxxxx" || c.last(t)["complete"] != true {
		t.Fatalf("read %q / %v", c.downloaded(t), c.last(t))
	}

	// Another site is a stranger, whatever it calls itself.
	other := newSite(t, "site.com")
	if got := serve(t, fsv, other, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil).last(t); got["error"] != "approval_pending" {
		t.Fatalf("an unapproved key got %v", got)
	}
}

func TestADeniedSiteIsNotAskedAgainForADay(t *testing.T) {
	fsv, _ := testFiles(t)
	s := newSite(t, "evil")
	serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil)
	fsv.asker.answer(fsv.asker.open()[0].ID, answerDeny)
	if got := serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil).last(t); got["error"] != "denied" {
		t.Fatalf("got %v", got)
	}
	if len(fsv.asker.open()) != 0 {
		t.Fatal("asked again")
	}
}

func TestNothingLeavesTheShare(t *testing.T) {
	fsv, dir := testFiles(t)
	s := newSite(t, "site.com")
	serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil)
	approve(t, fsv)
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	_ = os.WriteFile(secret, []byte("password"), 0o600)
	_ = os.Symlink(secret, filepath.Join(dir, "link.txt"))
	_ = os.Symlink(filepath.Dir(dir), filepath.Join(dir, "up"))
	_ = os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0o600)

	for _, p := range []string{"../secret.txt", "2025/../../secret.txt", "/etc/passwd", "link.txt", "up/secret.txt", ".hidden", "a:b"} {
		got := serve(t, fsv, s, "read", map[string]any{"op": "read", "share": "sqlbackups", "path": p}, nil).last(t)
		if got["ok"] == true {
			t.Errorf("%s was readable: %v", p, got)
		}
	}
	got := serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil).last(t)
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "hidden") || strings.Contains(string(b), "secret") {
		t.Errorf("the listing shows what it must not: %s", b)
	}
	if got := serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "other"}, nil).last(t); got["error"] != "not_shared" {
		t.Errorf("an unshared name: %v", got)
	}
}

func TestReplayedAndOldRequestsAreRefused(t *testing.T) {
	fsv, _ := testFiles(t)
	s := newSite(t, "site.com")
	c, env := s.request(fsv.key.PublicKey(), map[string]any{"op": "shares"})
	_ = fsv.handle(testConn, c.t, "read", env, fromInfo{})
	c2 := &call{keys: c.keys, t: &fakeTransport{}}
	_ = fsv.handle(testConn, c2.t, "read", env, fromInfo{})
	if c2.last(t)["error"] != "replayed" {
		t.Fatalf("replay: %v", c2.last(t))
	}
	old := serve(t, fsv, s, "read", map[string]any{"op": "shares", "ts": time.Now().Add(-time.Hour).Unix()}, nil)
	if old.last(t)["error"] != "too_old" {
		t.Fatalf("old: %v", old.last(t))
	}
	// Sealed for another computer: it cannot even be opened.
	otherKey, _ := ecdh.X25519().GenerateKey(randReader{})
	_, foreign := s.request(otherKey.PublicKey(), map[string]any{"op": "shares"})
	if err := fsv.handle(testConn, &fakeTransport{}, "read", foreign, fromInfo{}); !errors.Is(err, errBadSeal) {
		t.Fatalf("foreign seal: %v", err)
	}
}

func writableShare(t *testing.T, fsv *filesService, onConflict string) {
	t.Helper()
	sh, _ := fsv.store.share("sqlbackups")
	sh.Writable, sh.OnConflict = true, onConflict
	if _, err := fsv.store.updateShare("sqlbackups", sh); err != nil {
		t.Fatal(err)
	}
}

func writeReq(path string, data []byte, mtime time.Time, onConflict string) map[string]any {
	return map[string]any{"op": "write", "share": "sqlbackups", "path": path, "size": len(data), "mtime": mtime.Unix(), "onConflict": onConflict}
}

func TestWritesNeedAWritableShareAWriteSessionAndWriteAccess(t *testing.T) {
	fsv, dir := testFiles(t)
	s := newSite(t, "site.com")
	data := []byte("hello world!")
	if got := serve(t, fsv, s, "write", writeReq("new.txt", data, time.Now(), "never"), data).last(t); got["error"] != "read_only" {
		t.Fatalf("read-only share: %v", got)
	}
	writableShare(t, fsv, conflictAsk)
	if got := serve(t, fsv, s, "read", writeReq("new.txt", data, time.Now(), "never"), data).last(t); got["error"] != "read_only_session" {
		t.Fatalf("a read session wrote: %v", got)
	}
	serve(t, fsv, s, "write", writeReq("new.txt", data, time.Now(), "never"), data) // asks
	approve(t, fsv)
	mtime := time.Now().Add(-time.Hour).Truncate(time.Second)
	got := serve(t, fsv, s, "write", writeReq("out/new.txt", data, mtime, "never"), data).last(t)
	if got["written"] != true {
		t.Fatalf("write: %v", got)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "out", "new.txt"))
	fi, _ := os.Stat(filepath.Join(dir, "out", "new.txt"))
	if string(b) != string(data) || !fi.ModTime().Equal(mtime) {
		t.Fatalf("on disk: %q %v", b, fi.ModTime())
	}
	// The same file again changes nothing.
	if got := serve(t, fsv, s, "write", writeReq("out/new.txt", data, mtime, "never"), data).last(t); got["unchanged"] != true {
		t.Fatalf("again: %v", got)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "out", ".*mesh-part*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
}

func TestAnExistingFileIsReplacedOnlyByANewerOneAndOnlyWithBothSidesAgreeing(t *testing.T) {
	fsv, dir := testFiles(t)
	s := newSite(t, "site.com")
	writableShare(t, fsv, conflictAsk)
	target := filepath.Join(dir, "2025", "08", "db.sql.gz")
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	_ = os.Chtimes(target, old, old)
	serve(t, fsv, s, "write", writeReq("x", nil, time.Now(), "never"), []byte{})
	approve(t, fsv)
	newer := []byte("newer contents")
	now := time.Now().Truncate(time.Second)

	// The site may not be destructive: kept, said so.
	got := serve(t, fsv, s, "write", writeReq("2025/08/db.sql.gz", newer, now, "never"), newer).last(t)
	if got["error"] != "conflict" || got["existingMtime"] != float64(old.Unix()) {
		t.Fatalf("site said never: %v", got)
	}
	// An older file never replaces a newer one, whatever anybody says.
	got = serve(t, fsv, s, "write", writeReq("2025/08/db.sql.gz", newer, old.Add(-time.Hour), "newer"), newer).last(t)
	if got["error"] != "conflict" {
		t.Fatalf("older replaced newer: %v", got)
	}
	// Newer, the site may, the folder says ask: the person keeps it.
	done := make(chan *call)
	go func() { done <- serve(t, fsv, s, "write", writeReq("2025/08/db.sql.gz", newer, now, "newer"), newer) }()
	q := waitQuestion(t, fsv, "conflict")
	if q.Conflict.Path != "2025/08/db.sql.gz" || q.Conflict.ExistingSize != 10 {
		t.Fatalf("question: %+v", q.Conflict)
	}
	fsv.asker.answer(q.ID, answerKeep)
	c := <-done
	if a := c.answers(t); a[0]["status"] != "asking" || a[len(a)-1]["error"] != "conflict" {
		t.Fatalf("kept: %v", a)
	}
	// ... and now replaces it.
	go func() { done <- serve(t, fsv, s, "write", writeReq("2025/08/db.sql.gz", newer, now, "newer"), newer) }()
	fsv.asker.answer(waitQuestion(t, fsv, "conflict").ID, answerReplace)
	if got := (<-done).last(t); got["replaced"] != true {
		t.Fatalf("replace: %v", got)
	}
	if b, _ := os.ReadFile(target); string(b) != string(newer) {
		t.Fatalf("on disk: %q", b)
	}
	// The folder set to "newer": no question at all.
	writableShare(t, fsv, conflictNewer)
	newest := []byte("newest")
	if got := serve(t, fsv, s, "write", writeReq("2025/08/db.sql.gz", newest, now.Add(time.Minute), "newer"), newest).last(t); got["replaced"] != true {
		t.Fatalf("newer folder: %v", got)
	}
	writableShare(t, fsv, conflictNever)
	if got := serve(t, fsv, s, "write", writeReq("2025/08/db.sql.gz", newer, now.Add(time.Hour), "newer"), newer).last(t); got["error"] != "conflict" {
		t.Fatalf("never folder: %v", got)
	}
}

func waitQuestion(t *testing.T, fsv *filesService, kind string) question {
	t.Helper()
	for i := 0; i < 200; i++ {
		for _, q := range fsv.asker.open() {
			if q.Kind == kind {
				return q
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no question")
	return question{}
}

func TestNamesThisComputerCannotHoldAreRefusedOutLoud(t *testing.T) {
	fsv, dir := testFiles(t)
	s := newSite(t, "site.com")
	writableShare(t, fsv, conflictAsk)
	serve(t, fsv, s, "write", writeReq("x", nil, time.Now(), "never"), []byte{})
	approve(t, fsv)
	defer func(o string, c bool) { targetOS, caseInsensitiveFS = o, c }(targetOS, caseInsensitiveFS)
	targetOS, caseInsensitiveFS = "windows", true

	for _, name := range []string{"CON", "con.txt", "Aux.tar.gz", "LPT1", "report.", "trailing ", "a<b", `q"x`} {
		got := serve(t, fsv, s, "write", writeReq(name, []byte("x"), time.Now(), "never"), []byte("x")).last(t)
		if got["error"] != "name_not_allowed" || !strings.Contains(got["message"].(string), "name") {
			t.Errorf("%q: %v", name, got)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "Photo.JPG"), []byte("mine"), 0o644)
	got := serve(t, fsv, s, "write", writeReq("photo.jpg", []byte("theirs"), time.Now(), "newer"), []byte("theirs")).last(t)
	if got["error"] != "case_collision" || got["existingName"] != "Photo.JPG" {
		t.Fatalf("case twin: %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Photo.JPG")); string(b) != "mine" {
		t.Fatal("the other file was replaced")
	}
	for _, name := range []string{"run.exe", "x/setup.BAT", "install.sh", "Tool.app/Contents/x", "autorun.inf"} {
		got := serve(t, fsv, s, "write", writeReq(name, []byte("x"), time.Now(), "never"), []byte("x")).last(t)
		if got["error"] != "program_refused" {
			t.Errorf("%q: %v", name, got)
		}
	}
	// Every refusal is in the activity log with its reason.
	failures := 0
	for _, a := range fsv.activity.recent() {
		if a.Result == "name_not_allowed" || a.Result == "case_collision" || a.Result == "program_refused" {
			failures++
			if a.Message == "" {
				t.Errorf("no reason logged: %+v", a)
			}
		}
	}
	if failures != 14 {
		t.Errorf("logged %d refusals, want 14", failures)
	}
}

func TestFailuresAreGatheredIntoOneNotification(t *testing.T) {
	a := newActivityLog("")
	a.gather = 20 * time.Millisecond
	got := make(chan [2]string, 2)
	a.notify = func(title, text string) { got <- [2]string{title, text} }
	a.add(activityEntry{Op: "write", Share: "docs", Path: "CON.txt", Result: "name_not_allowed", Message: "reserved on Windows"})
	a.add(activityEntry{Op: "write", Share: "docs", Path: "b", Result: "conflict", Message: "kept"})
	a.add(activityEntry{Op: "list", Share: "docs", Result: "ok"})
	n := <-got
	if n[0] != "2 files could not be transferred" || !strings.Contains(n[1], "docs/CON.txt: reserved on Windows") {
		t.Fatalf("notification %v", n)
	}
	select {
	case extra := <-got:
		t.Fatalf("second notification %v", extra)
	case <-time.After(60 * time.Millisecond):
	}
}

func TestTheFolderQuotaHoldsAndAFullDiskIsJustAnError(t *testing.T) {
	fsv, _ := testFiles(t)
	s := newSite(t, "site.com")
	sh, _ := fsv.store.share("sqlbackups")
	sh.Writable, sh.QuotaBytes = true, 20
	_, _ = fsv.store.updateShare("sqlbackups", sh)
	serve(t, fsv, s, "write", writeReq("x", nil, time.Now(), "never"), []byte{})
	approve(t, fsv)
	big := bytes.Repeat([]byte("y"), 11) // 10 already used
	got := serve(t, fsv, s, "write", writeReq("big.bin", big, time.Now(), "never"), big).last(t)
	if got["error"] != "quota_exceeded" || got["quota"] != float64(20) || got["used"] != float64(10) {
		t.Fatalf("quota: %v", got)
	}
	shares := serve(t, fsv, s, "read", map[string]any{"op": "shares"}, nil).last(t)["shares"].([]any)
	if q := shares[0].(map[string]any); q["quota"] != float64(20) {
		t.Fatalf("shares: %v", q)
	}
	e := ioRefusal(fmt.Errorf("write: %w", os.ErrInvalid))
	if e.Code == "disk_full" {
		t.Fatal("not a full disk")
	}
	full := ioRefusal(fmt.Errorf("write /x: no space left on device"))
	if full.Code != "disk_full" || strings.ContainsAny(full.Message, "0123456789") {
		t.Fatalf("disk full: %+v", full)
	}
}

func TestShareRulesOnThisComputer(t *testing.T) {
	fsv, dir := testFiles(t)
	if _, err := fsv.store.addShare(configDir(), ""); err == nil {
		t.Error("the program's own settings were shared")
	}
	if _, err := fsv.store.addShare(filepath.Dir(configDir()), ""); err == nil {
		t.Error("a folder holding the program's settings was shared")
	}
	if _, err := fsv.store.addShare(dir, ""); err == nil {
		t.Error("the same folder twice")
	}
	other := filepath.Join(t.TempDir(), "SQLBackups")
	_ = os.MkdirAll(other, 0o755)
	sc, err := fsv.store.addShare(other, "")
	if err != nil || sc.Name != "SQLBackups 2" || sc.QuotaBytes != defaultQuota || sc.Writable || sc.OnConflict != conflictAsk {
		t.Fatalf("second share: %+v %v", sc, err)
	}
	home, _ := os.UserHomeDir()
	// Settings that run by themselves live in a different place on each system.
	settings := filepath.Join(home, ".config")
	switch runtime.GOOS {
	case "windows":
		settings = filepath.Join(os.Getenv("APPDATA"), "Microsoft")
	case "darwin":
		settings = filepath.Join(home, "Library", "LaunchAgents")
	}
	if riskyForWriting(home) == "" || riskyForWriting("/") == "" || riskyForWriting(settings) == "" {
		t.Error("risky folders not flagged")
	}
	if riskyForWriting(dir) != "" {
		t.Error("a plain folder flagged")
	}
}

func TestTheCrossLanguageVector(t *testing.T) {
	// Fixed keys and messages: the same file is checked by aMount's PHP Seal test, so the
	// two implementations cannot drift. MESH_WRITE_VECTOR=1 writes it again.
	k := func(b byte) *ecdh.PrivateKey {
		p, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{b}, 32))
		return p
	}
	computer, siteKey, eph := k(1), k(2), k(3)
	plain := []byte(`{"id":"00112233445566778899aabbccddeeff","ts":1800000000,"op":"read","share":"sqlbackups","path":"2025/08/db.sql.gz","site":{"name":"site.com"}}`)
	keys, env, err := siteSeal(siteKey, eph, computer.PublicKey(), plain)
	if err != nil {
		t.Fatal(err)
	}
	ck, opened, err := openRequest(computer, env)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("open: %v", err)
	}
	_ = ck.bindRequestID("00112233445566778899aabbccddeeff")
	_ = keys.bindRequestID("00112233445566778899aabbccddeeff")
	resp := ck.sealResponse([]byte(`{"ok":true,"size":5}`))
	down := ck.sealDown(0, true, []byte("hello"))
	up := keys.toComputer.Seal(nil, nonce("up\x00\x00", 0), []byte("world"), keys.chunkAAD(0, true))
	if _, err := ck.openUp(0, true, up); err != nil {
		t.Fatal(err)
	}
	if _, err := ck.openUp(0, false, up); err == nil {
		t.Fatal("a chunk opened with the wrong last flag")
	}

	// The direct-path ticket, as the gateway's MeshFilesTicket makes it.
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"w":7,"u":12,"m":"read","exp":1800000300}`))
	sum := sha256.Sum256([]byte(token))
	hk, _ := hkdf.Key(sha256.New, sum[:], nil, ticketLabel, 32)
	mac := hmac.New(sha256.New, hk)
	mac.Write([]byte(body))
	ticket := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if c, err := verifyTicket(ticket, token, 7, time.Unix(1800000000, 0)); err != nil || c.U != 12 {
		t.Fatalf("ticket: %v", err)
	}
	if _, err := verifyTicket(ticket, token, 8, time.Unix(1800000000, 0)); err == nil {
		t.Fatal("a ticket for another computer passed")
	}
	if _, err := verifyTicket(ticket, token, 7, time.Unix(1800000301, 0)); err == nil {
		t.Fatal("an expired ticket passed")
	}

	vector := map[string]string{
		"computerPrivate": hex.EncodeToString(computer.Bytes()), "computerPublic": b64url(computer.PublicKey().Bytes()),
		"sitePrivate": hex.EncodeToString(siteKey.Bytes()), "ephPrivate": hex.EncodeToString(eph.Bytes()),
		"siteFingerprint": fingerprint(siteKey.PublicKey().Bytes()), "request": string(plain),
		"envelope": base64.StdEncoding.EncodeToString(env), "response": base64.StdEncoding.EncodeToString(resp),
		"responsePlain": `{"ok":true,"size":5}`, "down0": base64.StdEncoding.EncodeToString(down), "down0Plain": "hello",
		"up0": base64.StdEncoding.EncodeToString(up), "up0Plain": "world",
		"token": token, "tokenHash": tokenHashHex(token), "ticket": ticket,
	}
	path := filepath.Join("testdata", "files-vector.json")
	if os.Getenv("MESH_WRITE_VECTOR") == "1" {
		b, _ := json.MarshalIndent(vector, "", "  ")
		_ = os.WriteFile(path, append(b, '\n'), 0o644)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run once with MESH_WRITE_VECTOR=1)", err)
	}
	var pinned map[string]string
	_ = json.Unmarshal(b, &pinned)
	for key, want := range vector {
		if pinned[key] != want {
			t.Errorf("%s drifted:\n got %s\nwant %s", key, want, pinned[key])
		}
	}
}

func TestAMailboxCarriesTheSessionWhenThePeerCanBeAServer(t *testing.T) {
	fsv, _ := testFiles(t)
	s := newSite(t, "site.com")
	serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil)
	approve(t, fsv)
	host := newMailboxHost(t.TempDir())
	srv := httptest.NewServer(http.StripPrefix("", host))
	defer srv.Close()
	token := strings.Repeat("ab", 16)

	c, env := s.request(fsv.key.PublicKey(), map[string]any{"op": "read", "share": "sqlbackups", "path": "2025/08/db.sql.gz",
		"paths": []map[string]string{{"url": "http://127.0.0.1:1/mailbox", "token": token}, {"url": srv.URL + "/mailbox", "token": token}}})
	if err := fsv.handle(testConn, c.t, "read", env, fromInfo{}); err != nil {
		t.Fatal(err)
	}
	// The gateway only heard that the session moved; the bytes went through the mailbox.
	if a := c.answers(t); len(a) != 1 || a[0]["status"] != "moved" || len(c.t.down) != 0 {
		t.Fatalf("gateway saw %v and %d chunks", a, len(c.t.down))
	}
	get := func(p string) []byte {
		resp, err := http.Get(srv.URL + "/mailbox/" + token + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b
	}
	var state map[string]any
	_ = json.Unmarshal(get("/state"), &state)
	if state["hello"] != true || state["responses"] != float64(2) || state["done"] != true {
		t.Fatalf("mailbox state %v", state)
	}
	// The site reads it all back: the answers continue the counter, the chunk opens.
	c.t = &fakeTransport{replies: [][]byte{c.t.replies[0], get("/response/0"), get("/response/1")}, down: [][]byte{get("/down/0")}}
	if a := c.answers(t); a[2]["complete"] != true || string(c.downloaded(t)) != "xxxxxxxxxx" {
		t.Fatalf("site read %v / %q", a, c.downloaded(t))
	}
}
