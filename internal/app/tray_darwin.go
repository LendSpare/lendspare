//go:build darwin && cgo

package app

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "tray_darwin.h"
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/LendSpare/lendspare/internal/engine"
)

// The macOS front end: a menu-bar icon instead of a console, mirroring tray_windows.go - a
// coloured dot for what it is doing, Pause/Resume, "Start at Login", the log, and Quit.
// Built on Cocoa directly through cgo (no third-party module, matching the Go-stdlib-only
// goal for the daemon itself: this file is the one exception, and only when cgo is enabled -
// see tray_other.go for the console-only fallback used by CGO_ENABLED=0 cross-builds).

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
	shown    level
	shownTxt string
	d        atomic.Pointer[daemon]
}

// activeTray is process-global because the Cocoa callbacks come back through plain C
// function pointers, which cannot carry a Go closure - one tray per process, same as Windows.
var activeTray *trayApp

var lockFile *os.File

func wantTray(cfg *config) bool {
	if cfg.noTray || cfg.proxyMode {
		return false
	}
	return cfg.tray || defaultTray()
}

func runTray(cfg config) {
	runtime.LockOSThread() // AppKit's run loop must run on the process's real main thread
	if !acquireSingleInstance() {
		return
	}
	logToFile()
	a := &trayApp{cfg: cfg, st: newStatus(), shown: -1}
	activeTray = a
	a.st.set(levelIdle, "Starting")
	C.lendspare_tray_init()
	a.refresh(true)
	a.rebuildMenu()
	go a.backend()
	C.lendspare_tray_run()
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

var trayColors = map[level][3]byte{
	levelIdle: {46, 160, 67}, levelWorking: {37, 99, 235}, levelPaused: {120, 120, 130},
	levelBusy: {217, 150, 20}, levelError: {200, 50, 50}, levelPairing: {124, 58, 237},
}

// refresh redraws the icon when what it shows changed (or force, to draw it the first time).
func (a *trayApp) refresh(force bool) {
	lvl, text, _ := a.st.snapshot()
	if !force && lvl == a.shown && text == a.shownTxt {
		return
	}
	a.shown, a.shownTxt = lvl, text
	c := trayColors[lvl]
	tip := C.CString("LendSpare - " + text)
	defer C.free(unsafe.Pointer(tip))
	C.lendspare_tray_set_icon(C.int(c[0]), C.int(c[1]), C.int(c[2]), tip)
}

func (a *trayApp) rebuildMenu() {
	C.lendspare_tray_clear_menu()
	_, text, jobs := a.st.snapshot()
	addMenuItem(text, idStatus, false, false)
	if jobs > 0 {
		addMenuItem(fmt.Sprintf("Jobs done this session: %d", jobs), 0, false, false)
	}
	conns := trayConnections()
	if len(conns) == 0 {
		addMenuItem("Connect from the website where you use LendSpare.", 0, false, false)
	}
	for i, c := range conns {
		addMenuItem(c.label, 0, false, false)
		if c.canPause {
			verb := "Pause "
			if c.paused {
				verb = "Resume "
			}
			addMenuItem(verb+truncate(c.label, 40), idConnBase+i, true, false)
		}
	}
	C.lendspare_tray_add_separator()
	if a.st.isPaused() {
		addMenuItem("Resume sharing", idPause, true, false)
	} else {
		addMenuItem("Pause sharing", idPause, true, false)
	}
	addMenuItem(sharesMenuLabel(), idShares, true, false)
	addMenuItem("Start at Login", idAutostart, true, autostartEnabled())
	C.lendspare_tray_add_separator()
	addMenuItem("Show the log", idLog, true, false)
	addMenuItem("Quit", idQuit, true, false)
}

func addMenuItem(label string, tag int, enabled, checked bool) {
	l := C.CString(label)
	defer C.free(unsafe.Pointer(l))
	e, c := C.int(0), C.int(0)
	if enabled {
		e = 1
	}
	if checked {
		c = 1
	}
	C.lendspare_tray_add_item(l, C.int(tag), e, c)
}

//export goLendSpareTrayClick
func goLendSpareTrayClick(id C.int) {
	if activeTray != nil {
		activeTray.handle(int(id))
	}
}

//export goLendSpareTrayTick
func goLendSpareTrayTick() {
	if activeTray != nil {
		activeTray.refresh(false)
	}
}

//export goLendSpareMenuNeedsUpdate
func goLendSpareMenuNeedsUpdate() {
	if activeTray != nil {
		activeTray.rebuildMenu()
	}
}

func (a *trayApp) handle(cmd int) {
	switch cmd {
	case idPause:
		a.st.setPaused(!a.st.isPaused())
		if !a.st.isPaused() {
			a.st.set(levelIdle, "Resuming")
		}
		a.refresh(true)
	case idShares:
		openSettingsPage()
	case idAutostart:
		if err := setAutostart(!autostartEnabled()); err != nil {
			log.Printf("[lendspare] start at login: %v", err)
		}
	case idLog:
		_ = exec.Command("open", logPath()).Start()
	case idQuit:
		a.quit()
	default:
		toggleConnection(cmd)
	}
}

// quit frees the GPU (stops our ComfyUI container) and exits.
func (a *trayApp) quit() {
	if d := a.d.Load(); d != nil {
		d.shutdown()
	}
	os.Exit(0) // not paired yet: nothing of ours is running
}

func attachConsole() {} // the daemon is launched from a Terminal already attached to one

// acquireSingleInstance: one tray per user session, held with an flock on a lock file (a
// relaunch for "Pair again" may start a moment before the old process has exited).
func acquireSingleInstance() bool {
	path := filepath.Join(configDir(), "tray.lock")
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return true // don't block startup over a lock-file problem
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	for i := 0; i < 20; i++ {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			lockFile = f // keep open for the life of the process - that holds the lock
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	f.Close()
	return false
}

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

func logPath() string { return filepath.Join(configDir(), "daemon.log") }

// linkRegistered: macOS registers lendspare:// only from an app bundle, which LendSpare is not yet.
func linkRegistered() bool { return false }
