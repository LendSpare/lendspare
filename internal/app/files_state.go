package app

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"runtime"
	"syscall"
)

// shareState is what the settings page, the server and sites are told about a folder
// (asked again on every request: once the cause is fixed, the next one works):
//
//	ok        readable
//	missing   not there (a disk unplugged, a network share no longer mounted)
//	denied    macOS privacy settings have not let this program in yet (EPERM from TCC)
//	noaccess  the file permissions refuse the account this program runs as - on a network
//	          share, the account the share was connected with
//	offline   a network share that is mounted but does not answer
func shareState(dir string) string {
	f, err := os.Open(dir)
	if err != nil {
		return shareErrState(err)
	}
	defer f.Close()
	if _, err := f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return shareErrState(err)
	}
	return "ok"
}

// shareErrState sorts an error from opening or listing a shared folder. Every cause used to
// read as the macOS privacy block, so a folder on a Windows share mounted on a Mac sent the
// person to Privacy & Security for what was a share permission or a sleeping PC.
func shareErrState(err error) string {
	for _, e := range offlineErrnos {
		if errors.Is(err, e) {
			return "offline"
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "missing"
	case runtime.GOOS == "darwin" && errors.Is(err, syscall.EPERM):
		return "denied" // TCC answers EPERM ("operation not permitted"), never EACCES
	case errors.Is(err, fs.ErrPermission):
		return "noaccess"
	}
	return "offline" // any other I/O failure on the folder itself: nothing to read, retry later
}

// offlineErrnos are what a mounted network share answers while its server is asleep, gone
// or the connection dropped (on Windows the ERROR_* codes of a lost network path).
var offlineErrnos = append([]syscall.Errno{
	syscall.EIO, syscall.ENOTCONN, syscall.ETIMEDOUT, syscall.EHOSTDOWN, syscall.EHOSTUNREACH,
	syscall.ENETDOWN, syscall.ENETUNREACH, syscall.ESTALE, syscall.ECONNRESET, syscall.ECONNREFUSED,
}, windowsNetErrnos...)
