//go:build linux

package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LendSpare/lendspare/internal/proc"
)

// "Start when I log in" on Ubuntu and other systemd desktops: a per-user unit (no root),
// started with the user's session. The program is copied to ~/.local/bin first, so moving
// the download does not break it.

func unitPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = configDir()
	}
	return filepath.Join(base, "systemd", "user", "lendspare.service")
}

func autostartEnabled() bool {
	_, err := os.Stat(unitPath())
	return err == nil
}

func installedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "lendspare")
}

func installSelf() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	dst := installedPath()
	if filepath.Clean(self) == filepath.Clean(dst) {
		return self
	}
	if os.MkdirAll(filepath.Dir(dst), 0o755) != nil {
		return self
	}
	in, err := os.Open(self)
	if err != nil {
		return self
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return self
	}
	_, cerr := io.Copy(out, in)
	if out.Close() != nil || cerr != nil {
		return self
	}
	return dst
}

func unitContent(path string) string {
	return fmt.Sprintf(`[Unit]
Description=LendSpare (shared folders and spare compute)
After=network-online.target

[Service]
ExecStart=%s
Restart=on-failure
RestartSec=10
NoNewPrivileges=yes

[Install]
WantedBy=default.target
`, path)
}

func setAutostart(on bool) error {
	if !on {
		_ = proc.Command("systemctl", "--user", "disable", "--now", "lendspare.service").Run()
		if err := os.Remove(unitPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	path := installSelf()
	if path == "" {
		return fmt.Errorf("cannot find this program's path")
	}
	if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath(), []byte(unitContent(path)), 0o644); err != nil {
		return err
	}
	_ = proc.Command("systemctl", "--user", "daemon-reload").Run()
	return proc.Command("systemctl", "--user", "enable", "lendspare.service").Run()
}
