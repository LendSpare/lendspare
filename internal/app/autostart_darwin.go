//go:build darwin

package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// "Start at Login": a per-user launchd agent (no admin rights, survives reboots and does
// not need the Login Items UI). The program is first copied to
// ~/Library/Application Support/LendSpare, so moving or deleting the download does not
// break it - the same reasoning as installSelf on Windows.

const launchAgentLabel = "com.lendspare.app"

func launchAgentPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(configDir(), launchAgentLabel+".plist")
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func autostartEnabled() bool {
	_, err := os.Stat(launchAgentPath())
	return err == nil
}

func installedPath() string {
	home, err := os.UserHomeDir()
	base := configDir()
	if err == nil && home != "" {
		base = filepath.Join(home, "Library", "Application Support", "LendSpare")
	}
	return filepath.Join(base, "lendspare")
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
	cerr2 := out.Close()
	if cerr != nil || cerr2 != nil {
		return self
	}
	return dst
}

func plistContent(path string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><false/>
	<key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`, launchAgentLabel, path)
}

func setAutostart(on bool) error {
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	if !on {
		_ = exec.Command("launchctl", "bootout", uid, launchAgentPath()).Run()
		if err := os.Remove(launchAgentPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	path := installSelf()
	if path == "" {
		return fmt.Errorf("cannot find this program's path")
	}
	if err := os.MkdirAll(filepath.Dir(launchAgentPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(launchAgentPath(), []byte(plistContent(path)), 0o644); err != nil {
		return err
	}
	return exec.Command("launchctl", "bootstrap", uid, launchAgentPath()).Run()
}
