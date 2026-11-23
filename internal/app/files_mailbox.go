package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mailboxes: a peer that can be a server is one (docs/features/29-computer-files.md, "Direct
// paths"). A site reachable from this computer (public, on the LAN, in Docker here) serves a
// mailbox for its own session, or the owner runs one anywhere with `lendspare --relay` -
// the "own relay" a site can be configured with when both ends are behind NAT and the bytes
// should not pass through the provider. The site names its mailboxes **inside the sealed
// request**, so a compromised gateway can neither add nor change one; the computer tries them
// in order and falls back to the gateway. Everything in a mailbox is sealed the same way.
//
// One protocol, keyed by a 128-bit session token only the site and this computer know:
//
//	computer: POST {T}/hello · POST {T}/reply?done= (raw sealed) · GET {T}/up/{n}?wait= · PUT {T}/down/{n}?last=
//	site    : GET {T}/state?since=&wait= · GET {T}/response/{i} · PUT {T}/up/{n}?last= · GET {T}/down/{n}?wait=

type mailboxPath struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// usable: a mailbox this computer may call - http(s), a well-formed token, no credentials in
// the address.
func (m mailboxPath) usable() bool {
	u, err := url.Parse(m.URL)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil &&
		tokenPattern.MatchString(m.Token)
}

type mailboxTransport struct {
	base   string
	client *http.Client
}

func newMailboxTransport(m mailboxPath) *mailboxTransport {
	return &mailboxTransport{base: strings.TrimRight(m.URL, "/") + "/" + m.Token,
		client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("a mailbox does not redirect")
		}}}
}

// hello says this computer took the mailbox; a quick answer or none.
func (m *mailboxTransport) hello() error {
	c := &http.Client{Timeout: 3 * time.Second, CheckRedirect: m.client.CheckRedirect}
	resp, err := c.Post(m.base+"/hello", "application/octet-stream", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailbox hello: %d", resp.StatusCode)
	}
	return nil
}

func (m *mailboxTransport) reply(sealed []byte, done bool) error {
	resp, err := m.client.Post(m.base+"/reply?done="+boolDigit(done), "application/octet-stream", bytes.NewReader(sealed))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return errSessionClosed
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mailbox reply: %d", resp.StatusCode)
	}
	return nil
}

func (m *mailboxTransport) nextUp(n uint64) ([]byte, bool, error) {
	started := time.Now()
	for failures := 0; ; {
		resp, err := m.client.Get(fmt.Sprintf("%s/up/%d?wait=15", m.base, n))
		if err != nil {
			if failures++; failures > 5 {
				return nil, false, err
			}
			time.Sleep(time.Second)
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, filesChunkMax+4096))
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			return data, resp.Header.Get("X-Mesh-Last") == "1", nil
		case http.StatusNoContent:
			if time.Since(started) > 10*time.Minute {
				return nil, false, errors.New("the site stopped sending")
			}
		case http.StatusNotFound, http.StatusGone:
			return nil, false, errSessionClosed
		default:
			if failures++; failures > 5 {
				return nil, false, fmt.Errorf("mailbox chunk %d: %d", n, resp.StatusCode)
			}
			time.Sleep(time.Second)
		}
	}
}

func (m *mailboxTransport) putDown(n uint64, data []byte, last bool) error {
	started := time.Now()
	for failures := 0; ; {
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/down/%d?last=%s", m.base, n, boolDigit(last)), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := m.client.Do(req)
		if err != nil {
			if failures++; failures > 5 {
				return err
			}
			time.Sleep(time.Second)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode < 300:
			return nil
		case resp.StatusCode == http.StatusTooManyRequests:
			if time.Since(started) > 10*time.Minute {
				return errors.New("the site stopped reading")
			}
			time.Sleep(300 * time.Millisecond)
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			return errSessionClosed
		default:
			if failures++; failures > 5 {
				return fmt.Errorf("mailbox chunk %d: %d", n, resp.StatusCode)
			}
			time.Sleep(time.Second)
		}
	}
}

// pickTransport: the first mailbox the site named that answers, else the transport the request
// came in on (the gateway, or the LAN connection the site opened itself).
func pickTransport(arrived filesTransport, paths []mailboxPath) (filesTransport, string) {
	if _, isDirect := arrived.(*directTransport); isDirect {
		return arrived, "direct"
	}
	for _, p := range paths {
		if !p.usable() {
			continue
		}
		m := newMailboxTransport(p)
		if err := m.hello(); err == nil {
			return m, "mailbox " + hostOf(p.URL)
		}
	}
	return arrived, "relay"
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return "?"
}

// --- the relay host: `lendspare --relay` -----------------------------------------------

// mailboxHost serves mailboxes for both sides. It holds sealed bytes it cannot read, a few
// chunks per session at a time, and forgets a session after ten idle minutes.
type mailboxHost struct {
	mu       sync.Mutex
	dir      string
	sessions map[string]*mailbox
	max      int
}

type mailbox struct {
	hello     bool
	responses [][]byte
	done      bool
	upCount   int
	upAcked   int
	upLast    bool
	downCount int
	downAcked int
	downLast  bool
	touched   time.Time
	changed   chan struct{}
	dir       string
}

func newMailboxHost(dir string) *mailboxHost {
	_ = os.MkdirAll(dir, 0o700)
	h := &mailboxHost{dir: dir, sessions: map[string]*mailbox{}, max: 256}
	go func() {
		for range time.Tick(time.Minute) {
			h.sweep()
		}
	}()
	return h
}

func (h *mailboxHost) sweep() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for t, m := range h.sessions {
		if time.Since(m.touched) > 10*time.Minute {
			_ = os.RemoveAll(m.dir)
			delete(h.sessions, t)
		}
	}
}

func (h *mailboxHost) get(token string, create bool) (*mailbox, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, ok := h.sessions[token]
	if !ok {
		if !create {
			return nil, errSessionClosed
		}
		if len(h.sessions) >= h.max {
			return nil, errors.New("busy")
		}
		m = &mailbox{dir: filepath.Join(h.dir, token), changed: make(chan struct{})}
		_ = os.MkdirAll(m.dir, 0o700)
		h.sessions[token] = m
	}
	m.touched = time.Now()
	return m, nil
}

func (h *mailboxHost) notify(m *mailbox) {
	close(m.changed)
	m.changed = make(chan struct{})
}

func (h *mailboxHost) wait(m *mailbox, ready func() bool, max time.Duration) {
	deadline := time.Now().Add(max)
	for {
		h.mu.Lock()
		ok, ch := ready(), m.changed
		h.mu.Unlock()
		if ok || time.Now().After(deadline) {
			return
		}
		select {
		case <-ch:
		case <-time.After(time.Until(deadline)):
		}
	}
}

var mailboxRoute = regexp.MustCompile(`^/([a-f0-9]{32})/(hello|reply|state|close|response/(\d+)|up/(\d+)|down/(\d+))$`)

func (h *mailboxHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := mailboxRoute.FindStringSubmatch(strings.TrimPrefix(r.URL.Path, "/mailbox"))
	if parts == nil {
		http.NotFound(w, r)
		return
	}
	token, action := parts[1], parts[2]
	waitFor := func() time.Duration {
		s, _ := strconv.Atoi(r.URL.Query().Get("wait"))
		return time.Duration(min(max(s, 0), 15)) * time.Second
	}
	m, err := h.get(token, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	switch {
	case action == "hello" && r.Method == http.MethodPost:
		h.mu.Lock()
		m.hello = true
		h.notify(m)
		h.mu.Unlock()
	case action == "reply" && r.Method == http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 9<<20))
		h.mu.Lock()
		m.responses = append(m.responses, body)
		m.done = m.done || r.URL.Query().Get("done") == "1"
		h.notify(m)
		h.mu.Unlock()
	case action == "close" && r.Method == http.MethodPost:
		h.mu.Lock()
		_ = os.RemoveAll(m.dir)
		delete(h.sessions, token)
		h.mu.Unlock()
	case action == "state":
		since := r.URL.Query().Get("since")
		h.wait(m, func() bool { return mailboxRev(m) != since }, waitFor())
		h.mu.Lock()
		state := map[string]any{"hello": m.hello, "responses": len(m.responses), "done": m.done, "rev": mailboxRev(m),
			"up":   map[string]any{"count": m.upCount, "acked": m.upAcked, "last": m.upLast},
			"down": map[string]any{"count": m.downCount, "acked": m.downAcked, "last": m.downLast}}
		h.mu.Unlock()
		writeJSON(w, state)
	case strings.HasPrefix(action, "response/"):
		i, _ := strconv.Atoi(parts[3])
		h.mu.Lock()
		var body []byte
		if i < len(m.responses) {
			body = m.responses[i]
		}
		h.mu.Unlock()
		if body == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write(body)
	default: // up/{n} or down/{n}
		dir, ns := "up", parts[4]
		if strings.HasPrefix(action, "down/") {
			dir, ns = "down", parts[5]
		}
		n, _ := strconv.Atoi(ns)
		if r.Method == http.MethodPut {
			h.put(w, r, m, dir, n)
		} else {
			h.take(w, m, dir, n, waitFor())
		}
		return
	}
}

func mailboxRev(m *mailbox) string {
	return fmt.Sprintf("%t%d%t%d%d%t%d%d%t", m.hello, len(m.responses), m.done, m.upCount, m.upAcked, m.upLast, m.downCount, m.downAcked, m.downLast)
}

func (h *mailboxHost) counters(m *mailbox, dir string) (*int, *int, *bool) {
	if dir == "up" {
		return &m.upCount, &m.upAcked, &m.upLast
	}
	return &m.downCount, &m.downAcked, &m.downLast
}

func (h *mailboxHost) put(w http.ResponseWriter, r *http.Request, m *mailbox, dir string, n int) {
	body, err := io.ReadAll(io.LimitReader(r.Body, filesChunkMax+4097))
	if err != nil || len(body) == 0 || len(body) > filesChunkMax+4096 {
		http.Error(w, "bad chunk", http.StatusRequestEntityTooLarge)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	count, acked, last := h.counters(m, dir)
	switch {
	case n < *count:
		w.WriteHeader(http.StatusAccepted)
		return
	case n > *count || *last:
		http.Error(w, "out of order", http.StatusConflict)
		return
	case n-*acked >= 4:
		http.Error(w, "window full", http.StatusTooManyRequests)
		return
	}
	if err := os.WriteFile(filepath.Join(m.dir, fmt.Sprintf("%s-%d", dir, n)), body, 0o600); err != nil {
		http.Error(w, "cannot store", http.StatusInternalServerError)
		return
	}
	*count = n + 1
	*last = r.URL.Query().Get("last") == "1"
	h.notify(m)
	w.WriteHeader(http.StatusAccepted)
}

func (h *mailboxHost) take(w http.ResponseWriter, m *mailbox, dir string, n int, wait time.Duration) {
	h.wait(m, func() bool { c, _, _ := h.counters(m, dir); return *c > n }, wait)
	h.mu.Lock()
	count, acked, last := h.counters(m, dir)
	if n < *acked {
		h.mu.Unlock()
		http.Error(w, "gone", http.StatusGone)
		return
	}
	for i := *acked; i < n; i++ {
		_ = os.Remove(filepath.Join(m.dir, fmt.Sprintf("%s-%d", dir, i)))
	}
	*acked = n
	isLast := *last && n == *count-1
	ready := n < *count
	h.notify(m)
	h.mu.Unlock()
	if !ready {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	body, err := os.ReadFile(filepath.Join(m.dir, fmt.Sprintf("%s-%d", dir, n)))
	if err != nil {
		http.Error(w, "gone", http.StatusGone)
		return
	}
	if isLast {
		w.Header().Set("X-Mesh-Last", "1")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(body)
}

// runRelay is `lendspare --relay`: an own relay for sites and computers that cannot reach
// each other, so the bytes need not pass through the provider. Put it behind TLS (or give it
// --relay-cert/--relay-key); what it carries is sealed either way.
func runRelay(listen, dir, cert, key string) error {
	h := newMailboxHost(dir)
	mux := http.NewServeMux()
	mux.Handle("/mailbox/", h)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("[relay] mailboxes on %s/mailbox (spool %s)", listen, dir)
	if cert != "" && key != "" {
		return srv.ListenAndServeTLS(cert, key)
	}
	return srv.ListenAndServe()
}
