//go:build darwin

package app

import (
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// Desktop questions on macOS: an AppleScript dialog (any process may show one), gone after ten
// minutes; the question stays on the settings page.

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(s) + `"`
}

func desktopDialog(title, text, kind string) string {
	buttons := `{"Decide later", "Deny", "Allow"}`
	if kind == "conflict" {
		buttons = `{"Keep the file here", "Replace all from this site (10 min)", "Replace"}`
	}
	script := "display dialog " + appleString(text) + " with title " + appleString("LendSpare - "+title) +
		" buttons " + buttons + " default button 1 with icon caution giving up after 600"
	out, err := proc.Command("osascript", "-e", script).Output()
	if err != nil {
		return ""
	}
	res := string(out)
	switch {
	case strings.Contains(res, "gave up:true"):
		return ""
	case strings.Contains(res, "button returned:Allow"):
		return answerAllow
	case strings.Contains(res, "button returned:Deny"):
		return answerDeny
	case strings.Contains(res, "button returned:Replace all"):
		return answerReplaceAll
	case strings.Contains(res, "button returned:Replace"):
		return answerReplace
	case strings.Contains(res, "button returned:Keep"):
		return answerKeep
	}
	return ""
}

func notifyDesktop(title, text string) {
	_ = proc.Command("osascript", "-e", "display notification "+appleString(text)+" with title "+appleString("LendSpare - "+title)).Run()
}
