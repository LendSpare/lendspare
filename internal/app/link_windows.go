//go:build windows

package app

import (
	"fmt"
	"syscall"
	"unsafe"
)

// The lendspare:// URL scheme for this user only (HKCU\Software\Classes\lendspare): no admin rights,
// no other user affected. The link starts `lendspare.exe "<url>"`, which hands it to the running
// instance (app.go) - it can only open the confirmation.

var (
	pRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	pRegDeleteTree  = advapi32.NewProc("RegDeleteTreeW")
)

const linkKey = `Software\Classes\lendspare`

func setRegString(key syscall.Handle, name, value string) error {
	v, _ := syscall.UTF16FromString(value)
	var namePtr *uint16
	if name != "" {
		namePtr = utf16(name)
	}
	r, _, _ := pRegSetValueEx.Call(uintptr(key), uintptr(unsafe.Pointer(namePtr)), 0, regSZ, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	if r != 0 {
		return fmt.Errorf("registry: error %d", r)
	}
	return nil
}

func createKey(path string) (syscall.Handle, error) {
	var key syscall.Handle
	r, _, _ := pRegCreateKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(utf16(path))), 0, 0, 0, keySetValue, 0,
		uintptr(unsafe.Pointer(&key)), 0)
	if r != 0 {
		return 0, fmt.Errorf("registry: error %d", r)
	}
	return key, nil
}

func registerLink() error {
	if isPackaged() {
		return nil // declared in the package manifest
	}
	exe := installSelf()
	if exe == "" {
		return fmt.Errorf("cannot find this program's path")
	}
	for _, e := range []struct{ path, name, value string }{
		{linkKey, "", "URL:LendSpare"},
		{linkKey, "URL Protocol", ""},
		{linkKey + `\shell\open\command`, "", `"` + exe + `" "%1"`},
	} {
		k, err := createKey(e.path)
		if err != nil {
			return err
		}
		err = setRegString(k, e.name, e.value)
		pRegCloseKey.Call(uintptr(k))
		if err != nil {
			return err
		}
	}
	return nil
}

func unregisterLink() error {
	if isPackaged() {
		return nil // the switch is honoured in fromDeepLink instead
	}
	pRegDeleteTree.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(utf16(linkKey))))
	return nil
}

func linkRegistered() bool {
	if isPackaged() {
		return true
	}
	var key syscall.Handle
	r, _, _ := pRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(utf16(linkKey))), 0, keyQueryValue, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return false
	}
	pRegCloseKey.Call(uintptr(key))
	return true
}
