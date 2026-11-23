//go:build linux

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LendSpare/lendspare/internal/proc"
)

// The lendspare:// scheme through a per-user .desktop file with x-scheme-handler/lendspare, made the
// default with xdg-mime. No root, no other user affected.

const desktopFileName = "lendspare-url-handler.desktop"

func desktopFilePath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "applications", desktopFileName)
}

func registerLink() error {
	exe := installSelf()
	if exe == "" {
		return fmt.Errorf("cannot find this program's path")
	}
	content := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=LendSpare\nExec=%q %%u\nNoDisplay=true\nTerminal=false\nMimeType=x-scheme-handler/lendspare;\n", exe)
	if err := os.MkdirAll(filepath.Dir(desktopFilePath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(desktopFilePath(), []byte(content), 0o644); err != nil {
		return err
	}
	_ = proc.Command("xdg-mime", "default", desktopFileName, "x-scheme-handler/lendspare").Run()
	return nil
}

func unregisterLink() error {
	if err := os.Remove(desktopFilePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
