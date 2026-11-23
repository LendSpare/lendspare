package app

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// A plain readable folder is "ok", a gone one "missing" - on every OS.
func TestShareStateOfLocalFolders(t *testing.T) {
	dir := t.TempDir()
	if got := shareState(dir); got != "ok" {
		t.Fatalf("readable folder: %q", got)
	}
	if got := shareState(filepath.Join(dir, "gone")); got != "missing" {
		t.Fatalf("missing folder: %q", got)
	}
	if isNetworkVolume(dir) {
		t.Fatal("a temp folder is not a network volume")
	}
}

// Only macOS privacy (EPERM) is "denied"; a share's own permissions and a sleeping server
// are not, so a folder shared from a Windows PC is not sent to Privacy & Security.
func TestShareErrStateSortsTheCause(t *testing.T) {
	wrap := func(e error) error { return &fs.PathError{Op: "open", Path: "/Volumes/share", Err: e} }
	perm := "noaccess"
	if runtime.GOOS == "darwin" {
		perm = "denied"
	}
	cases := []struct {
		err  error
		want string
	}{
		{wrap(syscall.ENOENT), "missing"},
		{wrap(syscall.EACCES), "noaccess"},
		{wrap(syscall.EPERM), perm},
		{wrap(syscall.EIO), "offline"},
		{wrap(syscall.ETIMEDOUT), "offline"},
		{wrap(syscall.EHOSTDOWN), "offline"},
		{fmt.Errorf("readdir: %w", wrap(syscall.ENOTCONN)), "offline"},
		{os.ErrPermission, "noaccess"},
	}
	for _, c := range cases {
		if got := shareErrState(c.err); got != c.want {
			t.Errorf("%v: got %q, want %q", c.err, got, c.want)
		}
	}
}

// .row sets display:flex, which beats the browser's own [hidden] - the macOS hint showed
// under every folder, Windows included, until the page said so itself.
func TestSettingsPageHonoursHidden(t *testing.T) {
	page, err := os.ReadFile("settings.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "[hidden] { display: none !important; }") {
		t.Fatal("settings.html must force [hidden] to display:none")
	}
}

// A path typed on the settings page is shared only when the folder is there; one chosen in a
// dialog is not checked (settings.go), so only the typed case is tested here.
func TestTypedFolderMustExist(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	if err := typedFolderExists(dir); err != nil {
		t.Fatalf("existing folder refused: %v", err)
	}
	if err := typedFolderExists("  " + dir + "  "); err != nil {
		t.Fatalf("pasted with spaces: %v", err)
	}
	if err := typedFolderExists(filepath.Join(dir, "typo")); err == nil || !strings.Contains(err.Error(), "no such folder") {
		t.Fatalf("typo accepted: %v", err)
	}
	if err := typedFolderExists(file); err == nil || !strings.Contains(err.Error(), "file") {
		t.Fatalf("file accepted: %v", err)
	}
}

// The page has no "Stop sharing": unticking Shared is how a folder stops, and choosing the
// same folder again ticks it back with the settings it had instead of "already shared".
func TestChoosingAnUnsharedFolderSharesItAgain(t *testing.T) {
	fsv, dir := testFiles(t)
	sc := fsv.store.snapshot().Shares[0]
	sc.Enabled, sc.OnConflict = false, conflictNewer
	if _, err := fsv.store.updateShare(sc.Name, sc); err != nil {
		t.Fatal(err)
	}
	again, err := fsv.store.addShare(dir, "")
	if err != nil || !again.Enabled || again.OnConflict != conflictNewer || again.Name != sc.Name || len(fsv.store.snapshot().Shares) != 1 {
		t.Fatalf("re-share: %+v %v (%d shares)", again, err, len(fsv.store.snapshot().Shares))
	}
	if _, err := fsv.store.addShare(dir, ""); err == nil {
		t.Fatal("a folder that is shared was added twice")
	}
}

// A failed docker run is logged with docker's own last words, not just "exit status 125".
func TestLastLineOfDockerOutput(t *testing.T) {
	out := []byte("Unable to find image locally\r\nv2: Pulling from x\r\ndocker: Error response from daemon: Conflict. The container name \"/lendspare-comfy\" is already in use.\r\n\r\n")
	if got := lastLine(out); got != `docker: Error response from daemon: Conflict. The container name "/lendspare-comfy" is already in use.` {
		t.Fatalf("%q", got)
	}
	if lastLine(nil) != "" || len(lastLine(bytes.Repeat([]byte("x"), 1000))) != 300 {
		t.Fatal("empty or long output")
	}
}
