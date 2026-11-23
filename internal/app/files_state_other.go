//go:build !darwin && !windows

package app

import "syscall"

var windowsNetErrnos []syscall.Errno

// isNetworkVolume is not looked up on Linux and the BSDs: only the settings page's hint
// depends on it, and the state itself ("offline", "noaccess") does not.
func isNetworkVolume(string) bool { return false }
