//go:build windows

package app

import (
	"log"
	"os"
)

// The Windows build is a GUI program (no console window when double-clicked). Started from
// a terminal with flags it attaches to that terminal, so its log lines are still seen.
func attachConsole() {
	if r, _, _ := pAttachConsole.Call(^uintptr(0)); r == 0 { // ATTACH_PARENT_PROCESS
		return // no parent console (or this is a console build, already attached)
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = out, out
		log.SetOutput(out)
	}
}
