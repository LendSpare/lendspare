//go:build windows

package app

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/LendSpare/lendspare/internal/engine"
)

// The Windows front end: a tray icon instead of a console. Double-click the program and it
// pairs through the browser (first run), then sits in the tray - a coloured dot for what it
// is doing, Pause / Resume, "Start with Windows", the log, and Quit.

const (
	idStatus = 1 + iota
	idPause
	idShares
	idAutostart
	idLog
	idQuit
)

type trayApp struct {
	cfg      config
	st       *status
	hwnd     uintptr
	icons    map[level]uintptr
	shown    level
	shownTxt string
	created  uintptr // the "TaskbarCreated" broadcast, sent when Explorer restarts
	d        atomic.Pointer[daemon]
}

// wantTray: a double-click (no arguments) or --tray runs the tray; --no-tray, and any other
// flags, keep the console behaviour.
func wantTray(cfg *config) bool {
	if cfg.noTray || cfg.proxyMode {
		return false
	}
	return cfg.tray || defaultTray()
}

func runTray(cfg config) {
	runtime.LockOSThread()
	if !acquireSingleInstance() {
		return
	}
	logToFile()
	a := &trayApp{cfg: cfg, st: newStatus(), icons: map[level]uintptr{}, shown: -1}
	a.st.set(levelIdle, "Starting")
	// Failures of shared-folder transfers come up as a balloon from the tray icon.
	desktopNotify = func(title, text string) { a.balloon(title, text) }
	go a.backend()
	a.loop()
}

// backend runs the daemon (which never returns: Quit exits the process). Starting it contacts
// no provider; connections are made from the settings page, a link or `lendspare connect`.
func (a *trayApp) backend() {
	cfg := a.cfg
	cfg.docker = engine.Resolve(cfg.docker)
	log.Printf("[lendspare] container engine: %s", cfg.docker)
	d := newDaemon(cfg, appInst.mgr)
	d.st = a.st
	a.d.Store(d)
	ensureLink(appInst)
	d.openStartupLink()
	d.run()
}

// --- the window ------------------------------------------------------------------------

func (a *trayApp) loop() {
	instance, _, _ := pGetModuleHandle.Call(0)
	class := utf16("LendSpareTray")
	wc := wndClassEx{size: uint32(unsafe.Sizeof(wndClassEx{})), instance: instance, className: class,
		wndProc: syscall.NewCallback(a.wndProc)}
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	// A hidden top-level window (not message-only: those never get "TaskbarCreated").
	a.hwnd, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(utf16("LendSpare"))),
		0, 0, 0, 0, 0, 0, 0, instance, 0)
	if a.hwnd == 0 {
		log.Printf("[lendspare] cannot create the tray window")
		a.backendOnly()
		return
	}
	a.created, _, _ = pRegisterWindowMsg.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	for l, c := range map[level][3]byte{
		levelIdle: {46, 160, 67}, levelWorking: {37, 99, 235}, levelPaused: {120, 120, 130},
		levelBusy: {217, 150, 20}, levelError: {200, 50, 50}, levelPairing: {124, 58, 237},
	} {
		a.icons[l] = makeIcon(c[0], c[1], c[2])
	}
	a.refresh(true)
	pSetTimer.Call(a.hwnd, 1, 1000, 0)
	a.firstRunTip()

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	a.notify(nimDelete)
}

// backendOnly keeps the daemon alive when no tray could be made.
func (a *trayApp) backendOnly() { select {} }

func (a *trayApp) wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	switch {
	case message == wmTray:
		switch lparam & 0xFFFF {
		case wmRButtonUp, wmLButtonUp, wmContextMenu:
			a.showMenu()
		}
		return 0
	case message == wmTimer:
		a.refresh(false)
		return 0
	case message == wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	case a.created != 0 && message == a.created:
		a.refresh(true) // Explorer restarted: the icon is gone, put it back
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, message, wparam, lparam)
	return r
}

func (a *trayApp) notify(action uintptr) {
	lvl, text, _ := a.st.snapshot()
	nid := notifyIconData{hwnd: a.hwnd, id: 1, flags: nifMsg | nifIcon | nifTip, callbackMessage: wmTray,
		icon: a.icons[lvl]}
	nid.size = uint32(unsafe.Sizeof(nid))
	fillUTF16(nid.tip[:], "LendSpare - "+text)
	pShellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&nid)))
}

// firstRunTip: Windows 11 puts a program's icon in the "hidden icons" overflow until the user
// drags it out, and offers no supported way for a program to pin itself. A balloon from the
// icon is still shown, so say once where it is and how to keep it visible.
func (a *trayApp) firstRunTip() {
	marker := filepath.Join(configDir(), "tray-tip-shown")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	_ = os.WriteFile(marker, []byte("1"), 0o600)
	a.balloon("LendSpare is running",
		"Its icon is in the hidden icons (the ^ arrow by the clock). Drag it onto the taskbar to keep it visible.")
}

func (a *trayApp) balloon(title, text string) {
	nid := notifyIconData{hwnd: a.hwnd, id: 1, flags: nifInfo, infoFlags: niifInfo}
	nid.size = uint32(unsafe.Sizeof(nid))
	fillUTF16(nid.infoTitle[:], title)
	fillUTF16(nid.info[:], text)
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// refresh redraws the icon when what it shows changed (or force, to add it again).
func (a *trayApp) refresh(force bool) {
	lvl, text, _ := a.st.snapshot()
	if force {
		a.notify(nimAdd)
	} else if lvl != a.shown || text != a.shownTxt {
		a.notify(nimModify)
	}
	a.shown, a.shownTxt = lvl, text
}

func (a *trayApp) showMenu() {
	_, text, jobs := a.st.snapshot()
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu)
	add := func(flags, id uintptr, label string) {
		pAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(utf16(label))))
	}
	add(mfString|mfGrayed, idStatus, text)
	if jobs > 0 {
		add(mfString|mfGrayed, 0, fmt.Sprintf("Jobs done this session: %d", jobs))
	}
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	conns := trayConnections()
	if len(conns) == 0 {
		add(mfString|mfGrayed, 0, "Connect from the website where you use LendSpare.")
	}
	for i, c := range conns {
		add(mfString|mfGrayed, 0, c.label)
		if c.canPause {
			verb := "Pause "
			if c.paused {
				verb = "Resume "
			}
			add(mfString, uintptr(idConnBase+i), verb+truncate(c.label, 40))
		}
	}
	if len(conns) > 0 {
		pAppendMenu.Call(menu, mfSeparator, 0, 0)
	}
	if a.st.isPaused() {
		add(mfString, idPause, "Resume sharing")
	} else {
		add(mfString, idPause, "Pause sharing")
	}
	add(mfString, idShares, sharesMenuLabel())
	flags := uintptr(mfString)
	if autostartEnabled() {
		flags |= mfChecked
	}
	if isPackaged() {
		add(flags, idAutostart, "Start with Windows...") // opens Windows' Startup apps page
	} else {
		add(flags, idAutostart, "Start with Windows")
	}
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	add(mfString, idLog, "Show the log")
	add(mfString, idQuit, "Quit")

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(a.hwnd) // or the menu would not close when the user clicks away
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(pt.x), uintptr(pt.y), 0, a.hwnd, 0)
	pPostMessage.Call(a.hwnd, wmNull, 0, 0)
	a.handle(int(cmd))
}

func (a *trayApp) handle(cmd int) {
	switch cmd {
	case idPause:
		a.st.setPaused(!a.st.isPaused())
		if !a.st.isPaused() {
			a.st.set(levelIdle, "Resuming")
		}
	case idShares:
		openSettingsPage()
	case idAutostart:
		if err := setAutostart(!autostartEnabled()); err != nil {
			log.Printf("[lendspare] start with Windows: %v", err)
		}
	case idLog:
		shellOpen("open", "notepad.exe", logPath())
	case idQuit:
		a.quit()
	default:
		toggleConnection(cmd)
	}
}

// quit frees the GPU (stops our ComfyUI container) and exits.
func (a *trayApp) quit() {
	a.notify(nimDelete)
	if d := a.d.Load(); d != nil {
		d.shutdown()
	}
	os.Exit(0) // not paired yet: nothing of ours is running
}

// --- single instance, log file, icon ---------------------------------------------------

// acquireSingleInstance: one tray per user session. A relaunch (Pair again) may start a
// moment before the old process has exited, so it waits a few seconds for the name.
func acquireSingleInstance() bool {
	name := utf16(`Local\LendSpareTray`)
	for i := 0; i < 20; i++ {
		_, _, err := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
		if !errors.Is(err, syscall.Errno(183)) { // ERROR_ALREADY_EXISTS
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func logPath() string { return filepath.Join(configDir(), "daemon.log") }

// logToFile sends the log to a file (the tray has no console), keeping one old copy.
func logToFile() {
	_ = os.MkdirAll(configDir(), 0o700)
	if fi, err := os.Stat(logPath()); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(logPath(), logPath()+".old")
	}
	if f, err := os.OpenFile(logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(f)
		os.Stderr = f
	}
}

// makeIcon draws a 32x32 anti-aliased dot with a darker rim, so the tray shows state by colour.
func makeIcon(r, g, b byte) uintptr {
	const size = 32
	pix := make([]byte, size*size*4) // BGRA
	cx, cy, radius := 15.5, 15.5, 14.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			cover := math.Max(0, math.Min(1, radius+0.5-d))
			if cover == 0 {
				continue
			}
			cr, cg, cb := float64(r), float64(g), float64(b)
			if d > radius-2.5 { // the rim
				cr, cg, cb = cr*0.55, cg*0.55, cb*0.55
			}
			i := (y*size + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(cb), byte(cg), byte(cr), byte(cover*255)
		}
	}
	mask := make([]byte, size*size/8) // all zero: the alpha channel decides
	color, _, _ := pCreateBitmap.Call(size, size, 1, 32, uintptr(unsafe.Pointer(&pix[0])))
	hmask, _, _ := pCreateBitmap.Call(size, size, 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	defer pDeleteObject.Call(color)
	defer pDeleteObject.Call(hmask)
	ii := iconInfo{isIcon: 1, mask: hmask, color: color}
	icon, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}
