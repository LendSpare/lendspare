//go:build windows

package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// "Start with Windows": a value under HKCU\...\Run (per user, no admin rights, the same
// place every tray program uses). The program is first copied to
// %LOCALAPPDATA%\LendSpare, so moving or deleting the download does not break it.

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "LendSpare"

	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x1
	keySetValue     = 0x2
	regSZ           = 1
)

func openRunKey(access uint32) (syscall.Handle, error) {
	var key syscall.Handle
	r, _, _ := pRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(utf16(runKeyPath))), 0,
		uintptr(access), uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return 0, fmt.Errorf("registry: error %d", r)
	}
	return key, nil
}

func autostartEnabled() bool {
	if isPackaged() {
		return false // the package's StartupTask, switched in Windows Settings
	}
	return hasRunValue(runValueName)
}

func hasRunValue(name string) bool {
	key, err := openRunKey(keyQueryValue)
	if err != nil {
		return false
	}
	defer pRegCloseKey.Call(uintptr(key))
	var size uint32
	r, _, _ := pRegQueryValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(utf16(name))), 0, 0, 0,
		uintptr(unsafe.Pointer(&size)))
	return r == 0 && size > 0
}

func writeRunValue(name, command string) error {
	key, err := openRunKey(keySetValue)
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(uintptr(key))
	value, _ := syscall.UTF16FromString(command)
	r, _, _ := pRegSetValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(utf16(name))), 0, regSZ,
		uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)*2))
	if r != 0 {
		return fmt.Errorf("registry: error %d", r)
	}
	return nil
}

func deleteRunValue(name string) {
	key, err := openRunKey(keySetValue)
	if err != nil {
		return
	}
	defer pRegCloseKey.Call(uintptr(key))
	pRegDeleteValue.Call(uintptr(key), uintptr(unsafe.Pointer(utf16(name))))
}

func installedPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = configDir()
	}
	return filepath.Join(base, "LendSpare", "lendspare.exe")
}

// installSelf copies this program to its permanent place and returns that path (or this
// program's own path when it already is there, or the copy is impossible).
func installSelf() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	dst := installedPath()
	if filepath.Clean(self) == filepath.Clean(dst) {
		return self
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return self
	}
	in, err := os.Open(self)
	if err != nil {
		return self
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return self // the installed copy is running, or not writable
	}
	_, cerr := io.Copy(out, in)
	if out.Close() != nil || cerr != nil {
		return self
	}
	return dst
}

func setAutostart(on bool) error {
	if isPackaged() {
		return openStartupSettings()
	}
	if !on {
		deleteRunValue(runValueName)
		return nil
	}
	path := installSelf()
	if path == "" {
		return fmt.Errorf("cannot find this program's path")
	}
	return writeRunValue(runValueName, `"`+path+`"`)
}
