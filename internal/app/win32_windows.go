//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

// The few Win32 calls the tray icon needs, through syscall alone: the daemon has no third-party
// module, and a tray icon is one window class, one notify icon and one popup menu.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pPostMessage         = user32.NewProc("PostMessageW")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenu          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetTimer            = user32.NewProc("SetTimer")
	pCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	pRegisterWindowMsg   = user32.NewProc("RegisterWindowMessageW")
	pShellNotifyIcon     = shell32.NewProc("Shell_NotifyIconW")
	pShellExecute        = shell32.NewProc("ShellExecuteW")
	pCreateBitmap        = gdi32.NewProc("CreateBitmap")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	pCreateMutex         = kernel32.NewProc("CreateMutexW")
	pAttachConsole       = kernel32.NewProc("AttachConsole")
	pRegOpenKeyEx        = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueEx       = advapi32.NewProc("RegSetValueExW")
	pRegQueryValueEx     = advapi32.NewProc("RegQueryValueExW")
	pRegDeleteValue      = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey         = advapi32.NewProc("RegCloseKey")
)

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmCommand     = 0x0111
	wmTimer       = 0x0113
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	wmTray        = 0x0400 + 1

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2
	nifMsg    = 0x1
	nifIcon   = 0x2
	nifTip    = 0x4
	nifInfo   = 0x10
	niifInfo  = 0x1

	mfString    = 0x0
	mfGrayed    = 0x1
	mfChecked   = 0x8
	mfSeparator = 0x800

	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100
	tpmNoNotify    = 0x80

	swShowNormal = 1
)

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	pt      point
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type notifyIconData struct {
	size            uint32
	hwnd            uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	timeout         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        [16]byte
	balloonIcon     uintptr
}

type iconInfo struct {
	isIcon   uint32
	xHotspot uint32
	yHotspot uint32
	mask     uintptr
	color    uintptr
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func fillUTF16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

// shellOpen asks Windows to open a file or address with whatever handles it.
func shellOpen(verb, target, params string) {
	var p *uint16
	if params != "" {
		p = utf16(params)
	}
	pShellExecute.Call(0, uintptr(unsafe.Pointer(utf16(verb))), uintptr(unsafe.Pointer(utf16(target))),
		uintptr(unsafe.Pointer(p)), 0, swShowNormal)
}
