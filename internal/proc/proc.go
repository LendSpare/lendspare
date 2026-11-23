// Package proc starts helper programs (docker, nvidia-smi, ...) without a console window.
package proc

import "os/exec"

// Command is exec.Command that never flashes a console window: the Windows build is a GUI
// program (tray icon), so every docker / nvidia-smi it starts would otherwise pop one up.
func Command(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	hideWindow(c)
	return c
}
