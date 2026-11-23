package app

import "sync"

// scheduler decides whose turn it is. The device has one GPU, so compute runs one job at a
// time across all connections (the compute loop is single-threaded and runs a job to its end
// before claiming the next); the scheduler only makes the claiming fair: after a connection
// is served, the others are asked first.
type scheduler struct {
	mu   sync.Mutex
	last string
}

// order returns ids rotated so the one after the last served comes first.
func (s *scheduler) order(ids []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := 0
	for i, id := range ids {
		if id == s.last {
			start = i + 1
		}
	}
	out := make([]string, 0, len(ids))
	for i := range ids {
		out = append(out, ids[(start+i)%len(ids)])
	}
	return out
}

func (s *scheduler) served(id string) {
	s.mu.Lock()
	s.last = id
	s.mu.Unlock()
}
