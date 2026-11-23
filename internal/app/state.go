package app

import "sync"

// What the daemon is doing right now, for whoever wants to show it (the tray icon, a log
// line). The run loop writes it; nothing in here changes behaviour except `paused`, the
// one thing a person can set from outside.

type level int

const (
	levelIdle    level = iota // connected, waiting for work
	levelWorking              // running a job
	levelPaused               // the person paused sharing
	levelBusy                 // the owner is using the GPU, so we stepped aside
	levelError                // cannot work: no engine, server away, no longer paired
	levelPairing              // waiting for the person to approve in the browser
)

type status struct {
	mu     sync.Mutex
	text   string
	lvl    level
	paused bool
	jobs   int
}

func newStatus() *status { return &status{text: "Starting", lvl: levelIdle} }

func (s *status) set(l level, text string) {
	s.mu.Lock()
	s.lvl, s.text = l, text
	s.mu.Unlock()
}

func (s *status) snapshot() (level, string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lvl, s.text, s.jobs
}

func (s *status) jobDone() {
	s.mu.Lock()
	s.jobs++
	s.mu.Unlock()
}

func (s *status) isPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// setPaused flips the pause switch and reports the new value.
func (s *status) setPaused(p bool) {
	s.mu.Lock()
	s.paused = p
	s.mu.Unlock()
}
