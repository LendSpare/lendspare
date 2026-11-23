package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Questions for the person at this computer (docs/features/29-computer-files.md): may a new
// site reach a shared folder, and may a newer incoming file replace one that is here. Asked
// in a desktop dialog where there is a desktop, and always on the settings page; whichever
// answers first wins. Only the person decides - nothing the server sends answers a question.

const (
	answerAllow      = "allow"
	answerDeny       = "deny"
	answerLater      = "later"
	answerReplace    = "replace"
	answerKeep       = "keep"
	answerReplaceAll = "replace-all" // this site, this folder, for the next ten minutes
)

const (
	replaceAllFor  = 10 * time.Minute
	maxOpenAsks    = 5
	approvalExpiry = 24 * time.Hour
)

type approvalQuestion struct {
	ID          string   `json:"id"`
	SiteKey     string   `json:"siteKey"`
	Fingerprint string   `json:"fingerprint"`
	SiteName    string   `json:"siteName"`
	Share       string   `json:"share"`
	Mode        string   `json:"mode"`
	From        fromInfo `json:"from"`
}

type conflictQuestion struct {
	ID            string   `json:"id"`
	SiteKey       string   `json:"siteKey"`
	SiteName      string   `json:"siteName"`
	Fingerprint   string   `json:"fingerprint"`
	Share         string   `json:"share"`
	Path          string   `json:"path"`
	ExistingMtime int64    `json:"existingMtime"`
	ExistingSize  int64    `json:"existingSize"`
	IncomingMtime int64    `json:"incomingMtime"`
	IncomingSize  int64    `json:"incomingSize"`
	From          fromInfo `json:"from"`
}

type question struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"` // approval | conflict
	Created  time.Time         `json:"created"`
	Approval *approvalQuestion `json:"approval,omitempty"`
	Conflict *conflictQuestion `json:"conflict,omitempty"`
	answered chan string
}

// dialogFunc shows a question on the desktop and returns the answer, or "" when there is no
// desktop or the person closed it without deciding.
type dialogFunc func(title, text string, kind string) string

type asker struct {
	mu         sync.Mutex
	store      *filesStore
	questions  map[string]*question
	replaceAll map[string]time.Time
	dialog     dialogFunc
	onChange   func()
}

func newAsker(store *filesStore, dialog dialogFunc) *asker {
	return &asker{store: store, questions: map[string]*question{}, replaceAll: map[string]time.Time{}, dialog: dialog}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *asker) changed() {
	if a.onChange != nil {
		go a.onChange()
	}
}

// askApproval puts a new site to the person, once per site and folder; the site is told to
// come back. More than a handful of open questions and the rest wait silently on the page, so
// a flood of requests cannot flood the desktop with dialogs.
func (a *asker) askApproval(q approvalQuestion) approvalQuestion {
	a.mu.Lock()
	a.expire()
	open := 0
	for _, existing := range a.questions {
		if existing.Kind != "approval" {
			continue
		}
		open++
		if existing.Approval.SiteKey == q.SiteKey && strings.EqualFold(existing.Approval.Share, q.Share) {
			if q.Mode == accessWrite {
				existing.Approval.Mode = accessWrite
			}
			a.mu.Unlock()
			return *existing.Approval
		}
	}
	q.ID = newID()
	qq := &question{ID: q.ID, Kind: "approval", Created: time.Now(), Approval: &q, answered: make(chan string, 1)}
	a.questions[q.ID] = qq
	a.mu.Unlock()
	a.changed()
	if open < maxOpenAsks && a.dialog != nil {
		go func() {
			if ans := a.dialog(approvalText(q)); ans == answerAllow || ans == answerDeny {
				a.answer(q.ID, ans)
			}
		}()
	}
	return q
}

func approvalText(q approvalQuestion) (string, string, string) {
	verb := "read"
	if q.Mode == accessWrite {
		verb = "read and write"
	}
	name := q.SiteName
	if strings.TrimSpace(name) == "" {
		name = "(no name)"
	}
	via := ""
	if q.From.SharedAccount != "" {
		via = "\nAccount: " + q.From.SharedAccount + " (you shared this computer with it)"
	}
	return fmt.Sprintf("Allow a site to %s \"%s\"?", verb, q.Share),
		fmt.Sprintf("A site asks to %s the shared folder \"%s\" on this computer.\n\nIt calls itself: %s%s\nIts code: %s\n\n"+
			"Compare this code with the one on the site's aMount page - only the code proves which site this is. "+
			"If you did not expect this, deny it.", verb, q.Share, name, via, q.Fingerprint), "approval"
}

// askConflict waits for the person (up to timeout) and returns their answer; no answer keeps
// the file that is here.
func (a *asker) askConflict(q conflictQuestion, timeout time.Duration) string {
	q.ID = newID()
	qq := &question{ID: q.ID, Kind: "conflict", Created: time.Now(), Conflict: &q, answered: make(chan string, 1)}
	a.mu.Lock()
	a.questions[q.ID] = qq
	open := 0
	for _, existing := range a.questions {
		if existing.Kind == "conflict" {
			open++
		}
	}
	a.mu.Unlock()
	a.changed()
	if open <= maxOpenAsks && a.dialog != nil {
		go func() {
			if ans := a.dialog(conflictText(q)); ans != "" {
				a.answer(q.ID, ans)
			}
		}()
	}
	select {
	case ans := <-qq.answered:
		return ans
	case <-time.After(timeout):
		a.drop(q.ID)
		return answerKeep
	}
}

func conflictText(q conflictQuestion) (string, string, string) {
	when := func(t int64) string { return time.Unix(t, 0).Format("2006-01-02 15:04") }
	return fmt.Sprintf("Replace a file in \"%s\"?", q.Share),
		fmt.Sprintf("%s (code %s) sends a newer \"%s\".\n\nOn this computer: %s, %s\nIncoming: %s, %s\n\n"+
			"Replace it with the incoming one?", nonEmpty(q.SiteName, "A site"), q.Fingerprint, q.Path,
			humanSize(q.ExistingSize), when(q.ExistingMtime), humanSize(q.IncomingSize), when(q.IncomingMtime)), "conflict"
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// answer settles a question from the dialog or the settings page; false when it is gone.
func (a *asker) answer(id, ans string) bool {
	a.mu.Lock()
	q, ok := a.questions[id]
	if !ok {
		a.mu.Unlock()
		return false
	}
	if q.Kind == "approval" {
		switch ans {
		case answerAllow:
			delete(a.questions, id)
			a.mu.Unlock()
			_ = a.store.grant(q.Approval.SiteKey, q.Approval.Fingerprint, q.Approval.SiteName, q.Approval.Share, q.Approval.Mode)
		case answerDeny:
			delete(a.questions, id)
			a.mu.Unlock()
			a.store.deny(q.Approval.SiteKey, q.Approval.Share)
		default:
			a.mu.Unlock()
			return false
		}
		a.changed()
		return true
	}
	switch ans {
	case answerReplace, answerKeep:
	case answerReplaceAll:
		a.replaceAll[q.Conflict.SiteKey+"|"+strings.ToLower(q.Conflict.Share)] = time.Now().Add(replaceAllFor)
	default:
		a.mu.Unlock()
		return false
	}
	delete(a.questions, id)
	a.mu.Unlock()
	q.answered <- ans
	a.changed()
	return true
}

func (a *asker) drop(id string) {
	a.mu.Lock()
	delete(a.questions, id)
	a.mu.Unlock()
	a.changed()
}

func (a *asker) replaceAllActive(siteKey, share string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.replaceAll[siteKey+"|"+strings.ToLower(share)]
	return ok && time.Now().Before(until)
}

// expire forgets approval questions nobody answered in a day (the site asks again if it wants).
func (a *asker) expire() {
	for id, q := range a.questions {
		if q.Kind == "approval" && time.Since(q.Created) > approvalExpiry {
			delete(a.questions, id)
		}
	}
}

// open lists the questions waiting, oldest first, for the settings page and the tray.
func (a *asker) open() []question {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expire()
	out := []question{}
	for _, q := range a.questions {
		out = append(out, *q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
