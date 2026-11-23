//go:build windows

package app

import (
	"errors"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// pickFolder shows Windows' own folder dialog (through PowerShell, which ships with Windows)
// and returns the chosen folder, or "" when the person cancelled.
func pickFolder() (string, error) {
	script := `[Console]::OutputEncoding=[Text.Encoding]::UTF8;Add-Type -AssemblyName System.Windows.Forms;` +
		`$f=New-Object System.Windows.Forms.FolderBrowserDialog;$f.Description='Choose a folder to share';` +
		`$o=New-Object System.Windows.Forms.Form -Property @{TopMost=$true};` +
		`if($f.ShowDialog($o) -eq 'OK'){[Console]::Out.Write($f.SelectedPath)}`
	out, err := proc.Command("powershell", "-NoProfile", "-STA", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", errors.New("the folder dialog could not open - paste the path instead")
	}
	return strings.TrimSpace(string(out)), nil
}

func openPrivacySettings(string) error { return nil }
