package app

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Quotas are per shared folder, always (docs/features/29-computer-files.md): a site may fill a
// folder up to its quota and no further, and both sides see the figures before a transfer
// starts - the computer reports quota and use with each share, a site sends the quota of the
// folder it is downloading into. A full disk is just an error: how much space the disk has is
// not something a site needs to know, so it is never said.

const defaultQuota = 10 << 30 // 10 GiB, set when a folder is shared; the person can change it

type usageEntry struct {
	bytes    int64
	measured time.Time
}

type usageTracker struct {
	mu    sync.Mutex
	usage map[string]usageEntry // share path -> bytes in it
	busy  map[string]bool
}

func newUsageTracker() *usageTracker {
	return &usageTracker{usage: map[string]usageEntry{}, busy: map[string]bool{}}
}

// used returns the last measurement and starts a new one when it is old; -1 before the first.
func (u *usageTracker) used(share shareConfig) int64 {
	u.mu.Lock()
	e, ok := u.usage[share.Path]
	stale := !ok || time.Since(e.measured) > 10*time.Minute
	if stale && !u.busy[share.Path] {
		u.busy[share.Path] = true
		go u.measure(share)
	}
	u.mu.Unlock()
	if !ok {
		return -1
	}
	return e.bytes
}

// usedNow measures synchronously when nothing is known yet (a write must not guess).
func (u *usageTracker) usedNow(share shareConfig) int64 {
	if n := u.used(share); n >= 0 {
		return n
	}
	return u.measure(share)
}

func (u *usageTracker) measure(share shareConfig) int64 {
	var total int64
	if root, err := os.OpenRoot(share.Path); err == nil {
		_ = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				if info, err := d.Info(); err == nil {
					total += info.Size()
				}
			}
			return nil
		})
		root.Close()
	}
	u.mu.Lock()
	u.usage[share.Path] = usageEntry{bytes: total, measured: time.Now()}
	delete(u.busy, share.Path)
	u.mu.Unlock()
	return total
}

// adjust keeps the figure right between measurements (a write added, a delete removed).
func (u *usageTracker) adjust(share shareConfig, delta int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if e, ok := u.usage[share.Path]; ok {
		e.bytes += delta
		if e.bytes < 0 {
			e.bytes = 0
		}
		u.usage[share.Path] = e
	}
}

// fits: would `incoming` bytes (replacing `replacing` bytes) stay within the folder's quota?
func fits(quota, used, incoming, replacing int64) bool {
	if quota <= 0 {
		quota = defaultQuota
	}
	return used-replacing+incoming <= quota
}

func quotaRefusal(quota, used int64) *opError {
	return &opError{Code: "quota_exceeded", Message: "this would go over the shared folder's quota (" + humanSize(quota) +
		", " + humanSize(used) + " used)", Extra: map[string]any{"quota": quota, "used": used}}
}

// isDiskFull: the disk itself is full. Said as such, without figures.
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || strings.Contains(strings.ToLower(err.Error()), "not enough space") ||
		strings.Contains(strings.ToLower(err.Error()), "no space left")
}
