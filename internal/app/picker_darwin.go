//go:build darwin

package app

import (
	"errors"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// pickFolder shows the macOS folder chooser.
func pickFolder() (string, error) {
	out, err := proc.Command("osascript", "-e", `POSIX path of (choose folder with prompt "Choose a folder to share")`).Output()
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			return "", nil // cancelled
		}
		return "", errors.New("the folder chooser could not open - paste the path instead")
	}
	return strings.TrimRight(strings.TrimSpace(string(out)), "/"), nil
}

// openPrivacySettings opens Privacy & Security at Files and Folders (or Full Disk Access),
// where the person lets this program into Desktop, Documents, Downloads or a disk.
func openPrivacySettings(pane string) error {
	anchor := "Privacy_FilesAndFolders"
	if pane == "full" {
		anchor = "Privacy_AllFiles"
	}
	return proc.Command("open", "x-apple.systempreferences:com.apple.preference.security?"+anchor).Start()
}
