package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeProvider plays a provider's control plane as docs/PROTOCOL.md describes it (or, with
// v1, one that predates v2): the description, intents, device initialize / complete / decline,
// the work API under a bearer, and the connection endpoints. It records every request so a test
// can say what was sent where.
type fakeProvider struct {
	t      *testing.T
	srv    *httptest.Server
	token  string
	connID string

	mu       sync.Mutex
	log      []string          // "METHOD path", every request
	bearers  []string          // Authorization values seen
	intents  map[string]string // intent or normalised code -> state: open | claimed | used
	devices  map[string]ed25519.PublicKey
	chall    map[string]string // intent -> challenge
	sigOK    bool
	declined bool
	revoked  bool
	deleted  bool
	caps     []map[string]any
	jobs     []string // queued job guids
	redirect string   // /pending answers 302 to this URL
	v1       bool

	scopes   []string
	operator string
	account  string
	pollsTil int // v1 device flow: pending polls before approval
	polls    int
	startReq map[string]string
}

func newFakeProvider(t *testing.T, name string) *fakeProvider {
	p := &fakeProvider{t: t, token: "tok-" + name + "-" + hex.EncodeToString(randBytes(6)), connID: "conn-" + name,
		intents: map[string]string{}, devices: map[string]ed25519.PublicKey{}, chall: map[string]string{},
		scopes: []string{scopeCompute, scopeFiles}, operator: "Operator " + name, account: "a***@" + name + ".example", pollsTil: 1}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func (p *fakeProvider) origin() string { return p.srv.URL }
func (p *fakeProvider) host() string   { return strings.TrimPrefix(p.srv.URL, "http://") }

// open registers an intent as the website would and returns the lendspare:// link for it.
func (p *fakeProvider) open(key string) {
	p.mu.Lock()
	p.intents[key] = "open"
	p.mu.Unlock()
}

func (p *fakeProvider) link(intent string) string {
	return "lendspare://connect?origin=" + p.host() + "&intent=" + intent
}

func (p *fakeProvider) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.log...)
}

func (p *fakeProvider) saw(substr string) bool {
	for _, r := range p.requests() {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return false
}

func (p *fakeProvider) bearerOK(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+p.token && !p.revoked && !p.deleted
}

func writeJ(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *fakeProvider) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, r.Method+" "+r.URL.Path)
	if a := r.Header.Get("Authorization"); a != "" {
		p.bearers = append(p.bearers, a)
	}
	var body map[string]any
	if r.Body != nil && r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	str := func(k string) string { s, _ := body[k].(string); return s }
	key := func() string { return str("intent") + str("code") }

	switch r.Method + " " + r.URL.Path {
	case "GET /.well-known/share-provider":
		if p.v1 {
			http.NotFound(w, r)
			return
		}
		writeJ(w, 200, map[string]any{"api": 2, "provider": "fake", "operator": p.operator, "origin": p.srv.URL,
			"services": []string{"ai"}, "terms": map[string]string{"version": "1", "url": "https://site.example/terms"}})
	case "POST /api/v2/share/devices/initialize":
		if st := p.intents[key()]; st != "open" && st != "claimed" {
			writeJ(w, 404, map[string]string{"error": "intent_invalid"})
			return
		}
		p.intents[key()] = "claimed"
		pub, _ := base64.StdEncoding.DecodeString(str("publicKey"))
		p.devices[key()] = ed25519.PublicKey(pub)
		p.chall[key()] = hex.EncodeToString(randBytes(16))
		if str("deviceId") == "" || str("name") == "" || str("platform") == "" {
			p.t.Errorf("initialize without device fields: %v", body)
		}
		writeJ(w, 200, map[string]any{"challenge": p.chall[key()], "expiresAt": "x", "provider": "fake", "service": "ai",
			"scopes": p.scopes, "account": p.account, "terms": map[string]string{"version": "1", "url": "https://site.example/terms"}})
	case "POST /api/v2/share/pair/complete":
		if p.intents[key()] != "claimed" {
			writeJ(w, 404, map[string]string{"error": "intent_invalid"})
			return
		}
		sig, _ := base64.StdEncoding.DecodeString(str("signature"))
		msg := "share-pair-v2\n" + p.chall[key()] + "\n" + p.srv.URL
		if !ed25519.Verify(p.devices[key()], []byte(msg), sig) {
			writeJ(w, 403, map[string]string{"error": "bad_signature"})
			return
		}
		p.sigOK = true
		p.intents[key()] = "used"
		writeJ(w, 200, map[string]any{"connectionId": p.connID, "token": p.token, "workerId": 7, "service": "ai", "scopes": p.scopes, "account": p.account})
	case "POST /api/v2/share/intents/decline":
		p.declined = true
		p.intents[key()] = "used"
		w.WriteHeader(http.StatusNoContent)
	case "POST /api/v1/mesh/pair":
		p.startReq = map[string]string{}
		for k, v := range body {
			p.startReq[k], _ = v.(string)
		}
		writeJ(w, 200, map[string]any{"userCode": "K7QM-2XWD", "device": strings.Repeat("a", 64),
			"verifyUrl": p.srv.URL + "/mesh/pair?code=K7QM2XWD", "expiresIn": 30, "interval": 1})
	case "POST /api/v1/mesh/pair/poll":
		p.polls++
		if p.polls <= p.pollsTil {
			writeJ(w, 200, map[string]any{"status": "pending"})
			return
		}
		writeJ(w, 200, map[string]any{"status": "approved", "token": p.token, "workerId": 9, "name": "Old PC", "server": p.srv.URL})
	case "GET /api/v1/mesh/pending":
		if p.redirect != "" {
			http.Redirect(w, r, p.redirect, http.StatusFound)
			return
		}
		if !p.bearerOK(r) {
			writeJ(w, 401, map[string]string{"error": "no"})
			return
		}
		if len(p.jobs) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		guid := p.jobs[0]
		p.jobs = p.jobs[1:]
		writeJ(w, 200, map[string]any{"job": map[string]any{"guid": guid, "kind": "llm", "workflow": "w"}})
	case "POST /api/v1/mesh/capabilities":
		if !p.bearerOK(r) {
			writeJ(w, 401, map[string]string{"error": "no"})
			return
		}
		p.caps = append(p.caps, body)
		writeJ(w, 200, map[string]any{"worker": map[string]int{"id": 7}})
	case "DELETE /api/v2/share/connection":
		if !p.bearerOK(r) {
			writeJ(w, 401, map[string]string{"error": "no"})
			return
		}
		p.deleted = true
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}
