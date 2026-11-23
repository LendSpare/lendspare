//go:build windows || (darwin && cgo)

package app

import (
	"fmt"
	"os"
)

// What the tray menus show about connections, shared by the Windows and macOS front ends.
// Every connection is always listed, however many there are.

const idConnBase = 100 // menu ids from here up: one pause/resume item per connection

type trayConn struct {
	id, label string
	paused    bool
	canPause  bool
}

func trayConnections() []trayConn {
	a := appInst
	if a == nil {
		return nil
	}
	out := []trayConn{}
	for _, c := range a.mgr.list() {
		state := "connected"
		switch {
		case c.State != stateConnected:
			state = "disconnected"
		case a.mgr.blockReason(c) != "":
			state = "blocked"
		case c.Paused:
			state = "paused"
		}
		out = append(out, trayConn{id: c.ID, label: fmt.Sprintf("%s (%s) - %s", hostOf(c.Origin), serviceName(c.Service), state),
			paused: c.Paused, canPause: c.State == stateConnected})
	}
	return out
}

// toggleConnection pauses or resumes the connection behind menu item cmd.
func toggleConnection(cmd int) {
	conns := trayConnections()
	i := cmd - idConnBase
	if a := appInst; a != nil && i >= 0 && i < len(conns) && conns[i].canPause {
		_ = a.mgr.setPaused(conns[i].id, !conns[i].paused)
	}
}

// ensureLink registers the lendspare:// handler for this user unless the person turned it off.
func ensureLink(a *app) {
	if a != nil && a.mgr.deepLinkEnabled() && !linkRegistered() {
		_ = registerLink()
	}
}

// defaultTray: a double-click, or a double-click on a link, runs the tray.
func defaultTray() bool { return len(os.Args) == 1 || startupLink != "" }

// sharesMenuLabel: the tray item, with the questions and connections waiting for the person.
func sharesMenuLabel() string {
	n := 0
	if f := filesSvc; f != nil {
		n += len(f.asker.open())
	}
	if a := appInst; a != nil {
		n += a.pair.pendingCount()
	}
	switch n {
	case 0:
		return "Connections and folders..."
	case 1:
		return "Connections and folders... (1 waiting)"
	}
	return fmt.Sprintf("Connections and folders... (%d waiting)", n)
}

func (p *pairer) pendingCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	n := 0
	for _, e := range p.pend {
		if !e.done && !e.waiting {
			n++
		}
	}
	return n
}
