package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Everything a site did with the shared folders, kept on this computer: the settings page
// shows it, a file keeps it across restarts, and a failure is never silent - a file that could
// not be written (a name Windows reserves, two names that differ only in case, a conflict, a
// broken transfer) is logged and, where the desktop allows, announced in a notification.
// Failures are gathered for a few seconds so a folder of 300 bad names is one notification.

type activityEntry struct {
	Time     time.Time `json:"time"`
	Site     string    `json:"site"`
	Op       string    `json:"op"`
	Share    string    `json:"share"`
	Path     string    `json:"path"`
	Result   string    `json:"result"`
	Message  string    `json:"message,omitempty"`
	Via      string    `json:"via,omitempty"`      // relay, direct, or mailbox <host>
	Provider string    `json:"provider,omitempty"` // the connection the request came through
}

// Results that are a decision, not a failure: nothing to announce.
var quietResults = map[string]bool{"ok": true, "approval_pending": true, "denied": true}

type activityLog struct {
	mu      sync.Mutex
	entries []activityEntry
	file    string
	notify  func(title, text string) // nil: the log and the settings page only
	pending []activityEntry
	timer   *time.Timer
	gather  time.Duration
}

func newActivityLog(file string) *activityLog {
	return &activityLog{file: file, gather: 15 * time.Second}
}

func activityPath() string { return filepath.Join(configDir(), "files-activity.log") }

func (a *activityLog) add(e activityEntry) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
	if len(a.entries) > 300 {
		a.entries = a.entries[len(a.entries)-300:]
	}
	line := fmt.Sprintf("%s %s %s %s/%s -> %s %s", e.Time.Format(time.RFC3339), e.Site, e.Op, e.Share, e.Path, e.Result, e.Message)
	if !quietResults[e.Result] {
		log.Printf("[files] %s", line)
	}
	if a.file != "" {
		if fi, err := os.Stat(a.file); err == nil && fi.Size() > 1<<20 {
			_ = os.Rename(a.file, a.file+".old")
		}
		if f, err := os.OpenFile(a.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}
	if quietResults[e.Result] || a.notify == nil {
		return
	}
	a.pending = append(a.pending, e)
	if a.timer == nil {
		a.timer = time.AfterFunc(a.gather, a.flush)
	}
}

// flush turns the failures gathered so far into one notification.
func (a *activityLog) flush() {
	a.mu.Lock()
	pending, notify := a.pending, a.notify
	a.pending, a.timer = nil, nil
	a.mu.Unlock()
	if len(pending) == 0 || notify == nil {
		return
	}
	title, text := summarise(pending)
	notify(title, text)
}

func summarise(failures []activityEntry) (string, string) {
	title := "A file could not be transferred"
	if len(failures) > 1 {
		title = fmt.Sprintf("%d files could not be transferred", len(failures))
	}
	lines := []string{}
	for i, f := range failures {
		if i == 3 {
			lines = append(lines, fmt.Sprintf("... and %d more - see Shared folders, Activity", len(failures)-3))
			break
		}
		lines = append(lines, fmt.Sprintf("%s/%s: %s", f.Share, f.Path, f.Message))
	}
	return title, strings.Join(lines, "\n")
}

func (a *activityLog) recent() []activityEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]activityEntry, len(a.entries))
	for i := range a.entries {
		out[len(a.entries)-1-i] = a.entries[i] // newest first
	}
	return out
}
