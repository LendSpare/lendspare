//go:build windows

package app

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Desktop questions on Windows: a top-most message box that gives up after ten minutes (the
// question stays on the settings page). Yes / No / Cancel are the only buttons Windows offers
// here, so the text says what each one means.

var (
	user32Dlg          = syscall.NewLazyDLL("user32.dll")
	pMessageBoxTimeout = user32Dlg.NewProc("MessageBoxTimeoutW")
	desktopNotify      func(title, text string) // the tray's balloon, when the tray runs
)

const (
	mbYesNoCancel   = 0x3
	mbIconQuestion  = 0x20
	mbTopmost       = 0x40000
	mbSetForeground = 0x10000
	idYes           = 6
	idNo            = 7
)

func desktopDialog(title, text, kind string) string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	yes, no := answerAllow, answerDeny
	legend := "\n\nYes = Allow    No = Deny    Cancel = decide later (Shared folders)"
	if kind == "conflict" {
		yes, no = answerReplace, answerKeep
		legend = "\n\nYes = Replace    No = Keep the file here    Cancel = decide later (Shared folders)"
	}
	t, _ := syscall.UTF16PtrFromString(text + legend)
	c, _ := syscall.UTF16PtrFromString("LendSpare - " + title)
	r, _, _ := pMessageBoxTimeout.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)),
		mbYesNoCancel|mbIconQuestion|mbTopmost|mbSetForeground, 0, 10*60*1000)
	switch r {
	case idYes:
		return yes
	case idNo:
		return no
	}
	return ""
}

func notifyDesktop(title, text string) {
	if desktopNotify != nil {
		desktopNotify(title, text)
	}
}
