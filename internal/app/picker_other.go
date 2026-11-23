//go:build !windows && !darwin

package app

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// pickFolder uses zenity or kdialog when a desktop has one; otherwise the page's own browser
// and a pasted path are the way (a headless Ubuntu server has no dialog to show).
func pickFolder() (string, error) {
	if !hasDesktop() {
		return "", errors.New("no desktop here - use the folder browser or paste the path")
	}
	for _, try := range [][]string{
		{"zenity", "--file-selection", "--directory", "--title=Choose a folder to share"},
		{"kdialog", "--getexistingdirectory", "."},
	} {
		if _, err := exec.LookPath(try[0]); err != nil {
			continue
		}
		out, err := proc.Command(try[0], try[1:]...).Output()
		if err != nil {
			return "", nil // cancelled
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", errors.New("no folder dialog installed (zenity or kdialog) - use the folder browser or paste the path")
}

func openPrivacySettings(string) error { return nil }
