//go:build !windows && !(darwin && cgo)

package app

// The tray icon is Windows-only and macOS-with-cgo (tray_darwin.go); everywhere else
// (Linux, or a macOS build with CGO_ENABLED=0) the daemon is a console program - a
// systemd unit or launchd agent keeps it running, see README.

func wantTray(*config) bool { return false }

func runTray(config) {}

func attachConsole() {}
