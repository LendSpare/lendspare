package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Shared folders through the gateway's sealed mailbox (docs/features/29-computer-files.md).
// The daemon asks for waiting sessions - every 5 s while quiet, every second for a minute after
// any activity, and at once when /pending answers with X-Mesh-Files - and serves each in its
// own goroutine, a few at a time.

const maxFileSessions = 4

var errSessionClosed = errors.New("the site closed the session")

type relayView struct {
	Session string   `json:"session"`
	State   string   `json:"state"`
	Mode    string   `json:"mode"`
	Request string   `json:"request"`
	From    fromInfo `json:"from"`
}

// runFiles is the poller; it never returns. It asks each connection that may be asked (scope
// files:serve, not paused, at least one folder granted to it) for waiting sessions.
func (d *daemon) runFiles() {
	slots := make(chan struct{}, maxFileSessions)
	lastActivity := time.Time{}
	for {
		busy := false
		asked := false
		for _, c := range d.mgr.filesConns() {
			if !d.files.sharing(c.ID) {
				continue
			}
			api, err := d.mgr.apiFor(c.ID)
			if err != nil {
				continue
			}
			asked = true
			var view relayView
			found, err := api.getJSON("/api/v1/mesh/files/next", &view)
			if err != nil {
				if isAuthError(err) {
					d.mgr.markDisconnected(c.ID, "the provider refused this connection")
				} else {
					log.Printf("[files] cannot ask %s for sessions: %v", hostOf(c.Origin), err)
				}
				continue
			}
			if found && view.Session != "" {
				lastActivity = time.Now()
				busy = true
				slots <- struct{}{}
				go func(v relayView, id string, api *apiClient) {
					defer func() { <-slots }()
					d.serveRelay(id, api, v)
				}(view, c.ID, api)
			}
		}
		switch {
		case !asked:
			d.waitFiles(15 * time.Second)
		case busy || time.Since(lastActivity) < time.Minute:
			d.waitFiles(time.Second)
		default:
			d.waitFiles(5 * time.Second)
		}
	}
}

// waitFiles sleeps, or less when the server hinted that a site is waiting.
func (d *daemon) waitFiles(max time.Duration) {
	select {
	case <-d.filesWake:
	case <-time.After(max):
	}
}

func (d *daemon) serveRelay(conn string, api *apiClient, v relayView) {
	envelope, err := base64.StdEncoding.DecodeString(v.Request)
	t := &relayTransport{api: api, session: v.Session}
	if err == nil {
		err = d.files.handle(conn, t, v.Mode, envelope, v.From)
	}
	if err != nil {
		code := "transfer_failed"
		if errors.Is(err, errBadSeal) {
			code = "bad_seal"
		}
		if !errors.Is(err, errSessionClosed) {
			log.Printf("[files] session %s: %v", short(v.Session), err)
		}
		_ = api.postJSON("/api/v1/mesh/files/"+v.Session+"/fail", map[string]any{"error": code}, nil)
	}
}

type relayTransport struct {
	api     *apiClient
	session string
}

func (r *relayTransport) path(rest string) string { return "/api/v1/mesh/files/" + r.session + rest }

func (r *relayTransport) reply(sealed []byte, done bool) error {
	err := r.api.postJSON(r.path("/reply"), map[string]any{"response": base64.StdEncoding.EncodeToString(sealed), "done": done}, nil)
	if err != nil && bytes.Contains([]byte(err.Error()), []byte(" 409")) {
		return errSessionClosed
	}
	return err
}

// nextUp waits for chunk n of what the site uploads.
func (r *relayTransport) nextUp(n uint64) ([]byte, bool, error) {
	idleSince := time.Now()
	for failures := 0; ; {
		req, _ := http.NewRequest(http.MethodGet, r.api.server+r.path(fmt.Sprintf("/up/%d?wait=15", n)), nil)
		r.api.auth(req)
		resp, err := r.api.http.Do(req)
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
			var view struct {
				State string `json:"state"`
			}
			if _, err := r.api.getJSON(r.path(""), &view); err == nil && view.State != "ACTIVE" {
				return nil, false, errSessionClosed
			}
			if time.Since(idleSince) > 10*time.Minute {
				return nil, false, errors.New("the site stopped sending")
			}
		case http.StatusNotFound, http.StatusConflict, http.StatusGone:
			return nil, false, errSessionClosed
		default:
			if failures++; failures > 5 {
				return nil, false, fmt.Errorf("chunk %d: %d", n, resp.StatusCode)
			}
			time.Sleep(time.Second)
		}
	}
}

// putDown sends chunk n to the site, waiting while the site is more than a window behind.
func (r *relayTransport) putDown(n uint64, data []byte, last bool) error {
	started := time.Now()
	for failures := 0; ; {
		req, _ := http.NewRequest(http.MethodPut, r.api.server+r.path(fmt.Sprintf("/down/%d?last=%s", n, boolDigit(last))), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = int64(len(data))
		r.api.auth(req)
		resp, err := r.api.http.Do(req)
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
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusConflict:
			return errSessionClosed
		default:
			if failures++; failures > 5 {
				return fmt.Errorf("chunk %d: %d", n, resp.StatusCode)
			}
			time.Sleep(time.Second)
		}
	}
}

func boolDigit(b bool) string { return strconv.Itoa(map[bool]int{false: 0, true: 1}[b]) }
