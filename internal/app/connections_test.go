package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LendSpare/lendspare/internal/secrets"
)

const testIntentA = "intent-aaaaaaaaaaaaaaaaaaaaaaaa"

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// pairByIntent runs the whole intent flow against p: deep link, confirmation, Allow.
func pairByIntent(t *testing.T, m *manager, p *fakeProvider, intent string) connection {
	t.Helper()
	pr := newPairer(m)
	p.open(intent)
	v, err := pr.fromDeepLink(p.link(intent))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.allow(v["id"].(string)); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.list() {
		if c.Origin == p.origin() {
			return c
		}
	}
	t.Fatal("no connection after Allow")
	return connection{}
}

func newTestDaemon(t *testing.T, m *manager) *daemon {
	d := newDaemon(config{pollSec: 0, docker: "no-such-docker-binary"}, m)
	d.lastLLM = time.Now()
	d.lastImageCheck = time.Now()
	return d
}

// --- a fresh start ---------------------------------------------------------------------------

type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, errors.New("no network in this test")
}

func TestFreshStartContactsNoProviderAndOffersNothing(t *testing.T) {
	ct := &countingTransport{}
	roundTripper = ct
	t.Cleanup(func() { roundTripper = nil })
	m := newTestManager(t)
	d := newTestDaemon(t, m)
	for i := 0; i < 3; i++ {
		d.step()
	}
	d.reportAll()
	if ct.n.Load() != 0 {
		t.Fatalf("a fresh start made %d requests", ct.n.Load())
	}
	if len(m.list()) != 0 || len(m.computeConns()) != 0 || len(m.filesConns()) != 0 {
		t.Fatal("a fresh start must hold no connection")
	}
	if _, text, _ := d.st.snapshot(); text != "LendSpare is ready" {
		t.Fatalf("status = %q", text)
	}
	if st := pageStatus()["status"]; st != "LendSpare is ready" {
		t.Fatalf("page status = %v", st)
	}
	dev := m.device()
	if len(dev.ID) != 36 || dev.ID[14] != '4' || dev.PublicKey == "" {
		t.Fatalf("device identity = %+v", dev)
	}
	if _, err := loadDeviceKey(m.secrets); err != nil {
		t.Fatalf("device key: %v", err)
	}
	raw, _ := os.ReadFile(configPath())
	if strings.Contains(string(raw), "seed") || strings.Contains(string(raw), "token") {
		t.Fatalf("config.json holds a secret: %s", raw)
	}
}

// --- pairing ---------------------------------------------------------------------------------

func TestIntentPairingHappyPath(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	pr := newPairer(m)
	p.open(testIntentA)

	v, err := pr.fromDeepLink(p.link(testIntentA))
	if err != nil {
		t.Fatal(err)
	}
	if v["operator"] != "Operator a" || v["host"] != p.host() || v["account"] != "a***@a.example" || len(v["scopes"].([]string)) != 2 {
		t.Fatalf("confirmation shows %v", v)
	}
	if len(m.list()) != 0 || p.saw("pair/complete") {
		t.Fatal("nothing may be created or completed before Allow")
	}
	if _, err := pr.allow(v["id"].(string)); err != nil {
		t.Fatal(err)
	}
	if !p.sigOK {
		t.Fatal("the provider did not verify the Ed25519 signature over share-pair-v2")
	}
	list := m.list()
	if len(list) != 1 || list[0].ID != "conn-a" || list[0].Origin != p.origin() || list[0].WorkerID != 7 || list[0].State != stateConnected {
		t.Fatalf("connections = %+v", list)
	}
	if m.tokenOf("conn-a") != p.token {
		t.Fatal("the token is not stored for the connection")
	}
	raw, _ := os.ReadFile(configPath())
	if strings.Contains(string(raw), p.token) {
		t.Fatal("the token is in config.json")
	}
	// A scope alone never turns computing on.
	if list[0].Compute || len(m.computeConns()) != 0 {
		t.Fatal("computing must be off for a new connection even with compute:offer")
	}
	if len(m.filesConns()) != 1 {
		t.Fatal("files:serve connection missing")
	}
	// The same intent again: used.
	if _, err := pr.fromDeepLink(p.link(testIntentA)); err == nil || !strings.Contains(err.Error(), "not valid any more") {
		t.Fatalf("a used intent must be refused, got %v", err)
	}
}

func TestTypedCodePairing(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	pr := newPairer(m)
	p.open("K7QM2XWD")
	if _, err := pr.fromCode(p.host(), "ZZZZ-ZZZZ"); err == nil {
		t.Fatal("a code with letters outside the alphabet must be refused before any request")
	}
	if _, err := pr.fromCode(p.host(), "AAAA-AAAA"); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("an unknown code: %v", err)
	}
	v, err := pr.fromCode(p.host(), "k7qm-2xwd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.allow(v["id"].(string)); err != nil {
		t.Fatal(err)
	}
	if len(m.list()) != 1 || m.tokenOf("conn-a") != p.token {
		t.Fatal("typed-code pairing did not store the connection")
	}
}

func TestDeclineKeepsNothingAndTellsTheProvider(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	pr := newPairer(m)
	p.open(testIntentA)
	v, _ := pr.fromDeepLink(p.link(testIntentA))
	if err := pr.cancel(v["id"].(string)); err != nil {
		t.Fatal(err)
	}
	if !p.declined || len(m.list()) != 0 || p.saw("pair/complete") {
		t.Fatalf("declined=%v connections=%d", p.declined, len(m.list()))
	}
	if _, err := pr.allow(v["id"].(string)); err == nil {
		t.Fatal("a cancelled request must not be allowed afterwards")
	}
}

func TestExpiredOrUnknownIntentIsOneAnswer(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	_, err := newPairer(m).fromDeepLink(p.link("intent-never-existed-0000000000"))
	if err == nil || !strings.Contains(err.Error(), "not valid any more") {
		t.Fatalf("err = %v", err)
	}
}

func TestPairingAddsOrUpdatesNeverReplacesTheList(t *testing.T) {
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	ca := pairByIntent(t, m, a, testIntentA)
	if err := m.setCompute(ca.ID, true); err != nil {
		t.Fatal(err)
	}
	pairByIntent(t, m, b, "intent-bbbbbbbbbbbbbbbbbbbbbbbb")
	if len(m.list()) != 2 {
		t.Fatalf("pairing a second provider left %d connections", len(m.list()))
	}
	a.token = "tok-a-renewed"
	pairByIntent(t, m, a, "intent-cccccccccccccccccccccccc") // same account again: an update
	list := m.list()
	if len(list) != 2 {
		t.Fatalf("re-pairing the same account added a record: %d", len(list))
	}
	got, _ := m.get("conn-a")
	if !got.Compute || m.tokenOf("conn-a") != "tok-a-renewed" {
		t.Fatal("the update must keep the local switch and replace the token")
	}
}

func TestDeviceCodeFlowAgainstAV1Server(t *testing.T) {
	p := newFakeProvider(t, "old")
	p.v1 = true
	m := newTestManager(t, p.origin())
	pr := newPairer(m)
	opened := ""
	pr.openURL = func(u string) error { opened = u; return nil }
	v, err := pr.startDevice(p.host())
	if err != nil {
		t.Fatal(err)
	}
	if p.saw("/api/v1/mesh/pair") {
		t.Fatal("nothing but the description may be requested before Allow")
	}
	if _, err := pr.allow(v["id"].(string)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for len(m.list()) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	list := m.list()
	if len(list) != 1 {
		t.Fatal("the device-code flow did not finish")
	}
	c := list[0]
	if !c.has(scopeCompute) || !c.has(scopeFiles) || c.Compute || c.Service != "ai" || c.WorkerID != 9 {
		t.Fatalf("v1 connection = %+v", c)
	}
	if p.startReq["deviceId"] == "" || p.startReq["publicKey"] == "" {
		t.Fatalf("the start call must carry deviceId and publicKey: %v", p.startReq)
	}
	if opened != p.origin()+"/mesh/pair?code=K7QM2XWD" {
		t.Fatalf("opened %q", opened)
	}
}

func TestDeviceCodeOnlyOpensTheProvidersOwnPage(t *testing.T) {
	p := newFakeProvider(t, "old")
	p.v1 = true
	m := newTestManager(t, p.origin())
	pr := newPairer(m)
	openedCh := make(chan string, 1)
	pr.openURL = func(u string) error { openedCh <- u; return nil }
	pr.client = func(origin string) *providerClient {
		c := newProviderClient(origin)
		c.http = &http.Client{Transport: rewriteVerifyURL{http.DefaultTransport}}
		return c
	}
	v, _ := pr.startDevice(p.host())
	_, _ = pr.allow(v["id"].(string))
	var opened string
	select {
	case opened = <-openedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("the page was never opened")
	}
	if !strings.HasPrefix(opened, p.origin()+"/") {
		t.Fatalf("opened %q", opened)
	}
}

// rewriteVerifyURL makes the provider's start answer name a page on another origin.
type rewriteVerifyURL struct{ next http.RoundTripper }

func (r rewriteVerifyURL) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil && strings.HasSuffix(req.URL.Path, "/api/v1/mesh/pair") {
		resp.Body = nopCloser{strings.NewReader(`{"userCode":"AAAA-BBBB","device":"d","verifyUrl":"https://evil.example/x","expiresIn":30,"interval":1}`)}
		resp.ContentLength = -1
	}
	return resp, err
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }

// --- origins and links -----------------------------------------------------------------------

func TestOriginAllowList(t *testing.T) {
	pol, err := newOriginPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{"https://lendspare.com", "https://share.brainybrain.com", "https://LENDSPARE.com:443", "https://lendspare.com/"}
	for _, o := range ok {
		if err := pol.allowed(o, false); err != nil {
			t.Errorf("%s refused: %v", o, err)
		}
	}
	bad := []string{"http://lendspare.com", "https://lendspare.com:8443", "https://lendspare.com.evil.com", "https://evil.example",
		"https://user@lendspare.com", "https://lendspare.com/path", "https://ai.artur.work", "https://share.artur.work", "", "javascript:alert(1)", "https://lendspare.com?x=1"}
	for _, o := range bad {
		if pol.allowed(o, false) == nil {
			t.Errorf("%q accepted", o)
		}
	}
	if pol.allowed("https://ai.artur.work", true) != nil {
		t.Error("a legacy origin must be allowed for a migrated connection")
	}
	if pol.allowed("https://evil.example", true) == nil {
		t.Error("the legacy flag must not open other origins")
	}
	dev, _ := newOriginPolicy([]string{"http://localhost:8080"})
	if dev.allowed("http://localhost:8080", false) != nil || dev.allowed("http://localhost:8081", false) == nil {
		t.Error("a dev origin is allowed exactly, with scheme and port")
	}
	if _, err := dev.fromParam("localhost:8080"); err != nil {
		t.Error(err)
	}
}

func TestDeepLinksWithExtrasOrURLsAreRefusedBeforeAnyRequest(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	pr := newPairer(m)
	good := "abcdefghijklmnopqrstuvwx"
	for _, link := range []string{
		"lendspare://connect?origin=" + p.host() + "&intent=" + good + "&scope=files:serve",
		"lendspare://connect?origin=" + p.host() + "&intent=" + good + "&return=https://evil.example",
		"lendspare://connect?origin=https://" + p.host() + "&intent=" + good,
		"lendspare://connect?origin=" + p.host() + "/x&intent=" + good,
		"lendspare://connect?origin=user@" + p.host() + "&intent=" + good,
		"lendspare://connect?origin=evil.example&intent=" + good,
		"lendspare://connect?origin=lendspare.com&intent=" + good, // allow-listed but not a dev origin here: no request may be made
		"lendspare://connect/path?origin=" + p.host() + "&intent=" + good,
		"lendspare://connect?origin=" + p.host() + "&intent=short",
		"lendspare://connect?origin=" + p.host() + "&origin=evil.example&intent=" + good,
		"lendspare://connect?intent=" + good,
		"lendspare://connect?origin=" + p.host() + "&intent=" + good + "#frag",
		"https://connect?origin=" + p.host() + "&intent=" + good,
		"lendspare://token?origin=" + p.host() + "&intent=" + good,
	} {
		if p.requests() != nil && len(p.requests()) > 0 && !strings.Contains(link, "origin=lendspare.com") {
			t.Fatalf("a request was made before %q", link)
		}
		if _, err := pr.fromDeepLink(link); err == nil {
			t.Errorf("%q accepted", link)
		}
	}
	if len(p.requests()) != 0 {
		t.Fatalf("refused links reached the provider: %v", p.requests())
	}
}

func TestAConnectionToADisallowedOriginNeverRuns(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.upsert(connection{Origin: "https://evil.example", Service: "ai", Scopes: []string{scopeCompute}}, "tok"); err == nil {
		t.Fatal("a new connection to a disallowed origin must be refused")
	}
	// A stored one (an old dev origin, a hand-edited file) is kept but does not run.
	m.mu.Lock()
	m.cfg.Connections = append(m.cfg.Connections, connection{ID: "x", Origin: "http://127.0.0.1:9", State: stateConnected, Scopes: []string{scopeCompute}, Compute: true})
	_ = m.saveLocked()
	m.mu.Unlock()
	_ = m.secrets.Set(secretToken("x"), "tok")
	if len(m.computeConns()) != 0 {
		t.Fatal("a dev origin without --dev-origin must not run")
	}
	if _, err := m.apiFor("x"); err == nil {
		t.Fatal("no client may be made for it")
	}
}

func TestARedirectToAnotherOriginIsRefusedAndTheBearerNeverSentThere(t *testing.T) {
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	c := connect(t, m, a.origin(), a.token, scopeCompute)
	a.redirect = b.origin() + "/api/v1/mesh/pending"
	api, err := m.apiFor(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.claim([]string{"llm"}); err == nil {
		t.Fatal("the redirect must fail the request")
	}
	if len(b.requests()) != 0 {
		t.Fatalf("the other origin received %v", b.requests())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.bearers) != 0 {
		t.Fatal("a bearer reached another origin")
	}
}

// --- several connections ----------------------------------------------------------------------

func TestDisconnectingOneLeavesTheOtherWorking(t *testing.T) {
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	ca := pairByIntent(t, m, a, testIntentA)
	cb := pairByIntent(t, m, b, "intent-bbbbbbbbbbbbbbbbbbbbbbbb")
	if ca.ID == cb.ID || m.tokenOf(ca.ID) == m.tokenOf(cb.ID) || m.tokenOf(ca.ID) != a.token || m.tokenOf(cb.ID) != b.token {
		t.Fatal("each connection needs its own token")
	}
	notice, err := m.disconnect(ca.ID)
	if err != nil || notice != "" {
		t.Fatalf("disconnect: %q %v", notice, err)
	}
	if !a.deleted {
		t.Fatal("the provider was not asked to end the connection (DELETE /api/v2/share/connection)")
	}
	if len(m.list()) != 1 || m.tokenOf(cb.ID) != b.token {
		t.Fatal("the other connection must be untouched")
	}
	if _, err := m.secrets.Get(secretToken(ca.ID)); err == nil {
		t.Fatal("the disconnected connection's token must be deleted")
	}
	api, _ := m.apiFor(cb.ID)
	b.jobs = []string{"j1"}
	if j, err := api.claim([]string{"llm"}); err != nil || j == nil {
		t.Fatalf("B no longer works: %v %v", j, err)
	}
	if b.deleted {
		t.Fatal("B was told to disconnect")
	}
}

func TestA401StopsOnlyThatConnection(t *testing.T) {
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	ca := connect(t, m, a.origin(), a.token, scopeCompute)
	cb := connect(t, m, b.origin(), b.token, scopeCompute)
	a.revoked = true
	b.jobs = []string{"jb"}
	d := newTestDaemon(t, m)
	d.kindsHook = func() []string { return []string{"llm"} }
	var ran []string
	d.runHook = func(c connection, j *job) { ran = append(ran, c.ID+":"+j.GUID) }
	d.step()
	gotA, _ := m.get(ca.ID)
	gotB, _ := m.get(cb.ID)
	if gotA.State != stateDisconnected || gotB.State != stateConnected {
		t.Fatalf("states: A=%s B=%s", gotA.State, gotB.State)
	}
	if len(ran) != 1 || ran[0] != cb.ID+":jb" {
		t.Fatalf("B's job did not run: %v", ran)
	}
	if _, err := m.secrets.Get(secretToken(ca.ID)); err == nil {
		t.Fatal("A's dead token must be dropped")
	}
	if m.tokenOf(cb.ID) != b.token {
		t.Fatal("B's token must be untouched")
	}
	if len(m.computeConns()) != 1 {
		t.Fatal("only B may run now")
	}
}

func TestOneComputeJobAtATimeAcrossConnections(t *testing.T) {
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	ca := connect(t, m, a.origin(), a.token, scopeCompute)
	cb := connect(t, m, b.origin(), b.token, scopeCompute)
	a.jobs = []string{"a1", "a2", "a3"}
	b.jobs = []string{"b1", "b2", "b3"}
	d := newTestDaemon(t, m)
	d.kindsHook = func() []string { return []string{"llm"} }
	var cur, peak atomic.Int32
	var mu sync.Mutex
	var order []string
	d.runHook = func(c connection, j *job) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		order = append(order, j.GUID)
		mu.Unlock()
		cur.Add(-1)
	}
	for i := 0; i < 6; i++ {
		d.step()
	}
	if peak.Load() != 1 {
		t.Fatalf("%d jobs ran at once", peak.Load())
	}
	if got := strings.Join(order, ","); got != "a1,b1,a2,b2,a3,b3" {
		t.Fatalf("not round-robin: %s", got)
	}
	_, _ = ca, cb
}

// --- scopes and the local switch -------------------------------------------------------------

func TestCapabilitiesGoOnlyWhereScopeAndSwitchAllow(t *testing.T) {
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	d := newTestDaemon(t, m)

	dir := t.TempDir()
	store, err := openFilesStore(filepath.Join(dir, "files.json"))
	if err != nil {
		t.Fatal(err)
	}
	key, _ := loadFilesKey(filepath.Join(dir, "files.key"))
	d.files = &filesService{store: store, key: key, asker: newAsker(store, nil), activity: newActivityLog(""), usage: newUsageTracker()}
	docs, secret := filepath.Join(dir, "docs"), filepath.Join(dir, "private")
	_ = os.MkdirAll(docs, 0o755)
	_ = os.MkdirAll(secret, 0o755)
	if _, err := store.addShare(docs, "Docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.addShare(secret, "Private"); err != nil {
		t.Fatal(err)
	}

	ca, err := m.upsert(connection{ID: "ca", Origin: a.origin(), Service: "ai", Scopes: []string{scopeCompute}}, a.token)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := m.upsert(connection{ID: "cb", Origin: b.origin(), Service: "mount", Scopes: []string{scopeFiles}}, b.token)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.setGrant("Docs", cb.ID, true)

	// compute:offer but the local switch is off: A is told nothing.
	d.reportAll()
	if len(a.caps) != 0 {
		t.Fatalf("A received capabilities with compute off: %v", a.caps)
	}
	// B has files:serve only: folder names, granted ones only, and no compute facts.
	if len(b.caps) != 1 {
		t.Fatalf("B reports = %d", len(b.caps))
	}
	if _, has := b.caps[0]["kinds"]; has {
		t.Fatal("compute capabilities sent without the scope")
	}
	shares, _ := b.caps[0]["files"].(map[string]any)["shares"].([]any)
	if len(shares) != 1 || shares[0].(map[string]any)["name"] != "Docs" {
		t.Fatalf("B was told about %v", shares)
	}
	// Switching compute on makes A hear about kinds; the folder grant to B stays B's.
	_ = m.setCompute(ca.ID, true)
	d.reportAll()
	if len(a.caps) != 1 || a.caps[0]["kinds"] == nil {
		t.Fatalf("A after the switch: %v", a.caps)
	}
	if _, has := a.caps[0]["files"]; has {
		t.Fatal("A has no files scope and no folder granted to it")
	}
	// A paused connection hears nothing.
	_ = m.setPaused(ca.ID, true)
	before := len(a.caps)
	d.reportAll()
	if len(a.caps) != before {
		t.Fatal("a paused connection was reported to")
	}
	// B has compute:offer? No: setting compute on is refused at the page, and a compute switch
	// without the scope still does not make a worker.
	_ = m.setCompute(cb.ID, true)
	for _, c := range m.computeConns() {
		if c.ID == cb.ID {
			t.Fatal("a connection without compute:offer must never run compute")
		}
	}
}

func TestAFolderIsServedOnlyThroughTheConnectionItIsGrantedTo(t *testing.T) {
	fsv, _ := testFiles(t)
	s := newSite(t, "site.com")
	serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil)
	approve(t, fsv)
	if got := serve(t, fsv, s, "read", map[string]any{"op": "list", "share": "sqlbackups"}, nil).last(t); got["ok"] != true {
		t.Fatalf("granted connection: %v", got)
	}
	c, env := s.request(fsv.key.PublicKey(), map[string]any{"op": "list", "share": "sqlbackups"})
	if err := fsv.handle("another-connection", c.t, "read", env, fromInfo{}); err != nil {
		t.Fatal(err)
	}
	if got := c.last(t); got["error"] != "not_shared" {
		t.Fatalf("a connection without the grant got %v", got)
	}
	c, env = s.request(fsv.key.PublicKey(), map[string]any{"op": "shares"})
	_ = fsv.handle("another-connection", c.t, "read", env, fromInfo{})
	if got := c.last(t); len(got["shares"].([]any)) != 0 {
		t.Fatalf("the folder list leaked to another connection: %v", got)
	}
}

// --- migration -------------------------------------------------------------------------------

func writeV1(t *testing.T, server string) {
	t.Helper()
	dir := configDir()
	_ = os.MkdirAll(dir, 0o700)
	v1, _ := json.Marshal(map[string]any{"server": server, "token": "old-token-secret", "workerId": 4, "name": "Gaming PC"})
	_ = os.WriteFile(filepath.Join(dir, "config.json"), v1, 0o600)
	files := `{"shares":[{"name":"Docs","path":"/x","enabled":true,"quotaBytes":1},{"name":"Pics","path":"/y","enabled":true,"quotaBytes":1}],"sites":[],"direct":{"enabled":false,"port":47321},"pipelineTrust":null,"denied":{}}`
	_ = os.WriteFile(filepath.Join(dir, "files.json"), []byte(files), 0o600)
}

func TestSchemaOneMigration(t *testing.T) {
	isolateConfig(t)
	policy, _ := newOriginPolicy([]string{"http://127.0.0.1:5555"})
	writeV1(t, "http://127.0.0.1:5555/")
	logs := captureLog(t)
	secrets := secrets.NewFile(configDir())
	m, err := openManager(secrets, policy)
	if err != nil {
		t.Fatal(err)
	}
	list := m.list()
	if len(list) != 1 {
		t.Fatalf("connections = %d", len(list))
	}
	c := list[0]
	if c.Origin != "http://127.0.0.1:5555" || !c.has(scopeCompute) || !c.has(scopeFiles) || len(c.Scopes) != 2 || !c.Compute || c.WorkerID != 4 || c.State != stateConnected {
		t.Fatalf("migrated = %+v", c)
	}
	if m.tokenOf(c.ID) != "old-token-secret" {
		t.Fatal("the old token was not carried over")
	}
	for _, f := range []string{"config.json", "files.json"} {
		if _, err := os.Stat(filepath.Join(configDir(), "backup-v1", f)); err != nil {
			t.Fatalf("no backup of %s: %v", f, err)
		}
	}
	backup, _ := os.ReadFile(filepath.Join(configDir(), "backup-v1", "config.json"))
	if !strings.Contains(string(backup), `"server"`) {
		t.Fatal("the backup is not the schema-1 original")
	}
	raw, _ := os.ReadFile(configPath())
	if strings.Contains(string(raw), "old-token-secret") {
		t.Fatal("the token is in the new config.json")
	}
	store, err := openFilesStore(filepath.Join(configDir(), "files.json"))
	if err != nil {
		t.Fatal(err)
	}
	shares := store.snapshot().Shares
	if len(shares) != 2 || !shares[0].grantedTo(c.ID) || !shares[1].grantedTo(c.ID) {
		t.Fatalf("grants = %+v", shares)
	}
	other, _ := m.upsert(connection{Origin: "http://127.0.0.1:5555", Account: "someone else", Service: "mount", Scopes: []string{scopeFiles}}, "t2")
	if _, ok := store.shareFor("Docs", other.ID); ok {
		t.Fatal("existing folders must be granted to the migrated connection only")
	}
	if strings.Contains(logs.String(), "old-token-secret") {
		t.Fatal("the token reached a log line")
	}

	// Idempotent: a second start changes nothing.
	filesBefore, _ := os.ReadFile(filepath.Join(configDir(), "files.json"))
	m2, err := openManager(secrets, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.list(); len(got) != 2 || got[0].ID != c.ID {
		t.Fatalf("second start: %+v", got)
	}
	filesAfter, _ := os.ReadFile(filepath.Join(configDir(), "files.json"))
	if !bytes.Equal(filesBefore, filesAfter) {
		t.Fatal("files.json was rewritten by the second start")
	}
	if m2.device().ID != m.device().ID {
		t.Fatal("the device identity changed")
	}
}

func TestMigratedLegacyOriginKeepsWorkingButIsNotNew(t *testing.T) {
	isolateConfig(t)
	writeV1(t, "https://ai.artur.work")
	policy, _ := newOriginPolicy(nil)
	m, err := openManager(secrets.NewFile(configDir()), policy)
	if err != nil {
		t.Fatal(err)
	}
	c := m.list()[0]
	if !c.Legacy || c.Origin != "https://ai.artur.work" || !m.usable(c) {
		t.Fatalf("legacy connection = %+v usable=%v", c, m.usable(c))
	}
	if _, err := m.upsert(connection{Origin: "https://ai.artur.work", Service: "ai", Account: "x", Scopes: []string{scopeCompute}}, "t"); err == nil {
		t.Fatal("a new connection to a legacy origin must be refused")
	}
}

func TestMigrationCopiesThePreviousProgramsDirectory(t *testing.T) {
	isolateConfig(t)
	old := legacyConfigDir()
	_ = os.MkdirAll(old, 0o700)
	v1, _ := json.Marshal(map[string]any{"server": "https://ai.artur.work", "token": "t"})
	_ = os.WriteFile(filepath.Join(old, "config.json"), v1, 0o600)
	_ = os.WriteFile(filepath.Join(old, "files.key"), []byte("key"), 0o600)
	policy, _ := newOriginPolicy(nil)
	m, err := openManager(secrets.NewFile(configDir()), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.list()) != 1 {
		t.Fatal("the old pairing was not migrated")
	}
	if b, err := os.ReadFile(filepath.Join(configDir(), "files.key")); err != nil || string(b) != "key" {
		t.Fatal("files.key was not copied")
	}
	if _, err := os.Stat(filepath.Join(old, "config.json")); err != nil {
		t.Fatal("the old directory must be left as it was")
	}
}

// --- secrets ---------------------------------------------------------------------------------

func TestNoSecretReachesALog(t *testing.T) {
	logs := captureLog(t)
	a, b := newFakeProvider(t, "a"), newFakeProvider(t, "b")
	m := newTestManager(t, a.origin(), b.origin())
	ca := pairByIntent(t, m, a, testIntentA)
	pairByIntent(t, m, b, "intent-bbbbbbbbbbbbbbbbbbbbbbbb")
	_ = m.setCompute(ca.ID, true)
	d := newTestDaemon(t, m)
	d.kindsHook = func() []string { return []string{"llm"} }
	d.runHook = func(connection, *job) {}
	a.jobs = []string{"j"}
	d.step()
	a.revoked = true
	d.step()
	_, _ = m.disconnect(ca.ID)

	seed, _ := m.secrets.Get(secretDeviceKey)
	for _, secret := range []string{a.token, b.token, testIntentA, "intent-bbbbbbbbbbbbbbbbbbbbbbbb", seed} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("a secret reached the log: %q", secret)
		}
	}
}

// --- the settings page: a link can only open a confirmation ---------------------------------

func TestADeepLinkHandedToThePageOnlyOpensAConfirmation(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	a := &app{mgr: m, pair: newPairer(m)}
	s := &settingsServer{token: "tok", port: 4242, app: a, state: pageStatus}
	call := func(method, path, token, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:4242"
		r.Header.Set("X-LendSpare-Token", token)
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	p.open(testIntentA)
	body := `{"url":"` + p.link(testIntentA) + `"}`
	if w := call("POST", "/api/deeplink", "wrong", "", body); w.Code != 403 {
		t.Fatalf("a call without the token: %d", w.Code)
	}
	if w := call("POST", "/api/deeplink", "tok", "http://evil.example", body); w.Code != 403 {
		t.Fatalf("a call from another page's origin: %d", w.Code)
	}
	if w := call("POST", "/api/deeplink", "tok", "", `{"url":"lendspare://connect?origin=`+p.host()+`&intent=`+testIntentA+`&x=1"}`); w.Code == 200 {
		t.Fatal("a link with extra parameters was accepted")
	}
	if w := call("POST", "/api/deeplink", "tok", "http://127.0.0.1:4242", body); w.Code != 200 {
		t.Fatalf("the link: %d %s", w.Code, w.Body)
	}
	if len(m.list()) != 0 || p.saw("pair/complete") {
		t.Fatal("a link must not create a connection or complete a pairing")
	}
	var state map[string]any
	_ = json.Unmarshal(call("GET", "/api/state", "tok", "", "").Body.Bytes(), &state)
	if pend, _ := state["pending"].([]any); len(pend) != 1 {
		t.Fatalf("pending = %v", state["pending"])
	}
	if _, has := state["connections"]; !has {
		t.Fatal("connections missing from the state")
	}
}

// The worker id the provider answers is stored, and storing it must not set off another report
// when it is the same id: onChange reports again, which once looped without end.
func TestACapabilityReportDoesNotLoopOnTheWorkerID(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	d := newTestDaemon(t, m)
	c, err := m.upsert(connection{ID: "ca", Origin: p.origin(), Service: "ai", Scopes: []string{scopeCompute}}, p.token)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.setCompute(c.ID, true); err != nil {
		t.Fatal(err)
	}
	m.onChange = func() { d.reportAll() }
	d.reportAll()
	time.Sleep(500 * time.Millisecond)
	p.mu.Lock()
	n := len(p.caps)
	p.mu.Unlock()
	if n == 0 || n > 2 {
		t.Fatalf("%d capability reports, want 1 or 2", n)
	}
}

// The page's header state must not replace the connection list with a count.
func TestStateListsConnectionsWhenThereAreSome(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	a := &app{mgr: m, pair: newPairer(m)}
	prev := appInst
	appInst = a
	t.Cleanup(func() { appInst = prev })
	p.open("K7QM2XWD")
	v, err := a.pair.fromCode(p.host(), "K7QM-2XWD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pair.allow(v["id"].(string)); err != nil {
		t.Fatal(err)
	}
	s := &settingsServer{token: "tok", port: 4242, app: a, state: pageStatus}
	r := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	r.Host = "127.0.0.1:4242"
	r.Header.Set("X-LendSpare-Token", "tok")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var state map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if conns, ok := state["connections"].([]any); !ok || len(conns) != 1 {
		t.Fatalf("connections = %#v, want a list of one", state["connections"])
	}
}

// --- small things the old tests covered ------------------------------------------------------

func TestParseDeepLinkAndCode(t *testing.T) {
	host, intent, err := parseDeepLink("lendspare://connect?origin=share.brainybrain.com&intent=abcdefghijklmnopqrstu")
	if err != nil || host != "share.brainybrain.com" || intent != "abcdefghijklmnopqrstu" {
		t.Fatalf("%q %q %v", host, intent, err)
	}
	if c, err := normalizeCode(" k7qm 2xwd "); err != nil || c != "K7QM2XWD" {
		t.Fatalf("%q %v", c, err)
	}
	for _, bad := range []string{"", "K7QM2XW", "K7QM2XWDD", "K7QM-2XW0", "K7QM-2XWI"} {
		if _, err := normalizeCode(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBrowserCommandPerOS(t *testing.T) {
	cases := map[string]string{"windows": "rundll32", "darwin": "open", "linux": "xdg-open"}
	for goos, want := range cases {
		name, args := browserCommand(goos, "https://x/")
		if name != want || args[len(args)-1] != "https://x/" {
			t.Errorf("%s: %s %v", goos, name, args)
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)"} {
		if err := openBrowser(bad); err == nil {
			t.Errorf("%s opened", bad)
		}
	}
}

func TestIsAuthError(t *testing.T) {
	if !isAuthError(&apiError{"pending", 403, "x"}) || !isAuthError(&apiError{"/api/v1/mesh/files/next", 401, "x"}) || !isAuthError(errors.New("pending 401: x")) {
		t.Error("401/403 means the connection was revoked")
	}
	if isAuthError(&apiError{"pending", 500, "x"}) || isAuthError(errors.New("dial tcp: refused")) {
		t.Error("other failures are retried")
	}
}

func TestStatusPauseAndSnapshot(t *testing.T) {
	st := newStatus()
	if st.isPaused() {
		t.Error("starts unpaused")
	}
	st.setPaused(true)
	st.set(levelWorking, "Working on a job")
	st.jobDone()
	lvl, text, jobs := st.snapshot()
	if !st.isPaused() || lvl != levelWorking || text != "Working on a job" || jobs != 1 {
		t.Errorf("snapshot %v %q %d paused=%v", lvl, text, jobs, st.isPaused())
	}
}

func TestSchedulerRotates(t *testing.T) {
	var s scheduler
	ids := []string{"a", "b", "c"}
	if got := strings.Join(s.order(ids), ""); got != "abc" {
		t.Fatal(got)
	}
	s.served("a")
	if got := strings.Join(s.order(ids), ""); got != "bca" {
		t.Fatal(got)
	}
	s.served("c")
	if got := strings.Join(s.order(ids), ""); got != "abc" {
		t.Fatal(got)
	}
	s.served("gone")
	if got := strings.Join(s.order(ids), ""); got != "abc" {
		t.Fatal(got)
	}
}
