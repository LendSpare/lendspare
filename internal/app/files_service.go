package app

import (
	"log"
	"sync"
)

// The shared-folders subsystem as one value (docs/features/29-computer-files.md): what is
// shared and with whom (filesStore), the computer's key, the questions for the person, the
// activity log and the folder usage. It exists before the computer is paired, so the settings
// page works from the first start; the daemon picks it up once it runs.

var (
	filesOnce sync.Once
	filesSvc  *filesService
	filesErr  error
)

// sharedFiles returns the process's one files service.
func sharedFiles() (*filesService, error) {
	filesOnce.Do(func() {
		store, err := openFilesStore(filesSettingsPath())
		if err != nil {
			filesErr = err
			return
		}
		key, err := loadFilesKey(filesKeyPath())
		if err != nil {
			filesErr = err
			return
		}
		activity := newActivityLog(activityPath())
		activity.notify = notifyDesktop
		filesSvc = &filesService{store: store, key: key, asker: newAsker(store, desktopDialog), activity: activity,
			usage: newUsageTracker()}
	})
	if filesErr != nil {
		log.Printf("[files] shared folders are off: %v", filesErr)
	}
	return filesSvc, filesErr
}

// sharing: anything granted to this connection to serve? (No poll for sessions otherwise.)
func (fsv *filesService) sharing(conn string) bool {
	if fsv == nil {
		return false
	}
	for _, s := range fsv.store.snapshot().Shares {
		if s.Enabled && s.grantedTo(conn) {
			return true
		}
	}
	return false
}

// advertised: the owner's outside addresses, while the listener runs.
func (fsv *filesService) advertised(listening bool) []string {
	if !listening {
		return []string{}
	}
	return append([]string{}, fsv.store.snapshot().Direct.Advertise...)
}

// capabilities is what the server is told: the public key, the shares by name (never a
// path), their state and quota, and where sites on the same network may try.
func (fsv *filesService) capabilities(direct []string, conn string) map[string]any {
	if fsv == nil {
		return nil
	}
	shares := []map[string]any{}
	for _, s := range fsv.store.snapshot().Shares {
		if !s.Enabled || !s.grantedTo(conn) {
			continue
		}
		shares = append(shares, map[string]any{"name": s.Name, "writable": s.Writable, "state": shareState(s.Path),
			"quota": quotaOf(s), "used": fsv.usage.used(s)})
	}
	return map[string]any{"publicKey": b64url(fsv.key.PublicKey().Bytes()), "shares": shares, "direct": direct,
		"advertised": fsv.advertised(len(direct) > 0)}
}
