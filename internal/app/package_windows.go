//go:build windows

package app

import "unsafe"

var pGetCurrentPackageFullName = kernel32.NewProc("GetCurrentPackageFullName")

const appmodelErrorNoPackage = 15700

func runningPackaged() bool {
	if pGetCurrentPackageFullName.Find() != nil {
		return false
	}
	var n uint32
	r, _, _ := pGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&n)), 0)
	return r != appmodelErrorNoPackage // packaged: ERROR_INSUFFICIENT_BUFFER for the empty buffer
}

// openStartupSettings shows Windows' Startup apps page, where a packaged app's start-at-login
// is switched.
func openStartupSettings() error {
	shellOpen("open", "ms-settings:startupapps", "")
	return nil
}
