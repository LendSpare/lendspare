//go:build !windows && !darwin

package app

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// Desktop questions on Linux (Ubuntu and friends): zenity when a desktop session is there,
// notify-send for notifications. A headless server has neither - the settings page and the
// log are where the questions and failures are, and that is enough.

func hasDesktop() bool { return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" }

func desktopDialog(title, text, kind string) string {
	if !hasDesktop() {
		return ""
	}
	if _, err := exec.LookPath("zenity"); err != nil {
		return ""
	}
	ok, cancel, extra := "Allow", "Deny", "Decide later"
	if kind == "conflict" {
		ok, cancel, extra = "Replace", "Keep the file here", "Replace all from this site (10 min)"
	}
	cmd := proc.Command("zenity", "--question", "--title=LendSpare - "+title, "--text="+text, "--ok-label="+ok,
		"--cancel-label="+cancel, "--extra-button="+extra, "--timeout=600", "--width=480")
	out, err := cmd.Output()
	switch {
	case err == nil:
		if kind == "conflict" {
			return answerReplace
		}
		return answerAllow
	case strings.TrimSpace(string(out)) == extra:
		if kind == "conflict" {
			return answerReplaceAll
		}
		return ""
	}
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 {
		if kind == "conflict" {
			return answerKeep
		}
		return answerDeny
	}
	return "" // timed out (5) or no display
}

func notifyDesktop(title, text string) {
	if !hasDesktop() {
		return
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = proc.Command("notify-send", "--app-name=LendSpare", "LendSpare - "+title, text).Run()
	}
}
