package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LendSpare/lendspare/internal/proc"
)

// Connecting to a provider (docs/PROTOCOL.md sections 1-3). Two ways in:
//
//   - from a website: a deep link or a typed code names a one-time intent; the person confirms
//     on this computer (operator, host, service, scopes) before any credential exists;
//   - from LendSpare: choose a provider, the device-code flow, the person approves on the
//     provider's page.
//
// Either way pairing adds or updates one connection.

type providerDesc struct {
	API      int      `json:"api"`
	Provider string   `json:"provider"`
	Operator string   `json:"operator"`
	Origin   string   `json:"origin"`
	Services []string `json:"services"`
	Terms    termsRef `json:"terms"`
}

type termsRef struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

type initAnswer struct {
	Challenge string   `json:"challenge"`
	ExpiresAt string   `json:"expiresAt"`
	Provider  string   `json:"provider"`
	Service   string   `json:"service"`
	Scopes    []string `json:"scopes"`
	Account   string   `json:"account"`
	Terms     termsRef `json:"terms"`
	Code      string   `json:"code"` // optional: the code the website shows, to compare
}

type completeAnswer struct {
	ConnectionID string   `json:"connectionId"`
	Token        string   `json:"token"`
	WorkerID     int      `json:"workerId"`
	Service      string   `json:"service"`
	Scopes       []string `json:"scopes"`
	Account      string   `json:"account"`
}

// providerClient makes the calls that need no token. It follows no redirect off its origin.
type providerClient struct {
	origin string
	http   *http.Client
}

func newProviderClient(origin string) *providerClient {
	return &providerClient{origin: origin, http: sameOriginClient(origin, 30*time.Second)}
}

func (c *providerClient) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.origin+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = truncate(string(data), 200)
		}
		return &apiError{path, resp.StatusCode, e.Error}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// describe reads the provider's public description and refuses one that names another origin.
func (c *providerClient) describe() (providerDesc, error) {
	var d providerDesc
	if err := c.do("GET", "/.well-known/share-provider", nil, &d); err != nil {
		return d, err
	}
	n, err := normalizeOrigin(d.Origin)
	if err != nil || n != c.origin {
		return providerDesc{}, errors.New("the provider's description names a different origin")
	}
	return d, nil
}

func pairErrorText(err error) string {
	var ae *apiError
	if errors.As(err, &ae) {
		switch ae.status {
		case 404:
			return "That link or code is not valid any more. Start again on the website."
		case 429:
			return "Too many attempts. Wait a few minutes and try again."
		}
	}
	return "Could not connect: " + err.Error()
}

// --- deep link and code syntax ---------------------------------------------------------------

var (
	intentRe = regexp.MustCompile(`^[A-Za-z0-9_-]{20,200}$`)
	codeRe   = regexp.MustCompile(`^[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{8}$`)
)

// parseDeepLink accepts exactly lendspare://connect?origin=<host[:port]>&intent=<nonce>. Any other
// parameter, a path, credentials or a fragment makes the whole link invalid: it must not be
// possible to smuggle a scope, a token, a path or a URL through it.
func parseDeepLink(raw string) (hostParam, intent string, err error) {
	u, perr := url.Parse(strings.TrimSpace(raw))
	if perr != nil || u.Scheme != "lendspare" || u.Host != "connect" || (u.Path != "" && u.Path != "/") ||
		u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return "", "", errors.New("that is not a LendSpare connection link")
	}
	q := u.Query()
	if len(q) != 2 || len(q["origin"]) != 1 || len(q["intent"]) != 1 {
		return "", "", errors.New("that is not a LendSpare connection link")
	}
	if !intentRe.MatchString(q.Get("intent")) {
		return "", "", errors.New("the link carries an invalid intent")
	}
	return q.Get("origin"), q.Get("intent"), nil
}

// normalizeCode turns "k7qm-2xwd" into "K7QM2XWD" and refuses anything that is not a code.
func normalizeCode(s string) (string, error) {
	c := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if !codeRe.MatchString(c) {
		return "", errors.New("a code has 8 letters and digits, like K7QM-2XWD")
	}
	return c, nil
}

// --- what the person is shown ----------------------------------------------------------------

var serviceNames = map[string]string{"ai": "AI", "mount": "Mount", "sync": "Sync"}

func serviceName(s string) string {
	if n, ok := serviceNames[s]; ok {
		return n
	}
	return s
}

// confirmationText builds the local permission text from docs/terms/gui-permission-texts.md
// (ids connect.*). The partner-platforms clause is kept because the provider description has
// no field that says whether a provider's terms exclude partner jobs.
func confirmationText(host, operator, service, account string, scopes []string, termsHost, termsURL string, known bool) (title, summary string, details []string) {
	if operator == "" {
		operator = host
	}
	title = "Connect to " + host
	summary = fmt.Sprintf("%s, operated by %s, asks permission to use this computer.", serviceName(service), operator)
	if account != "" {
		summary += " Account: " + account + "."
	}
	summary += " Installing LendSpare alone never shares your resources."
	if !known || contains(scopes, scopeCompute) {
		details = append(details,
			"Offer computing capacity of this computer.",
			fmt.Sprintf("Allow computing tasks from customers of the connected service and permitted partner platforms, under the terms you accepted on %s. The customer behind each task is not shown to you. A task cannot see your files, passwords or network.", termsHost),
			"Computing stays off until you switch it on in LendSpare, and you can set limits there.",
			"Your own use comes first: LendSpare steps aside while another program uses the GPU. Cryptocurrency mining is never permitted.",
			"Running tasks uses power and may heat and wear your hardware. Earnings are credit in the service, not money.")
	}
	if !known || contains(scopes, scopeFiles) {
		details = append(details,
			fmt.Sprintf("Let %s use a folder on this computer.", operator),
			"This service will reach only the folder and operations listed below. Writing, deleting and replacing files each need your separate approval.",
			"By default the folder is read-only.",
			"The service passes your data through sealed. It cannot read your files.",
			"Your computer may be off or offline. Keep your own copies of anything important.")
	}
	if termsURL != "" {
		details = append(details, fmt.Sprintf("You accepted the terms on %s. You can read them at %s.", termsHost, termsURL))
	}
	details = append(details, "You can change or remove this connection at any time in LendSpare.")
	return
}

// --- the pairer --------------------------------------------------------------------------------

const (
	kindIntent = "intent"
	kindCode   = "code"
	kindDevice = "device"
	maxPending = 5
	pendingTTL = 15 * time.Minute
)

type pending struct {
	id      string
	kind    string
	origin  string
	desc    providerDesc
	known   bool // the provider answered with scopes (v2 initialize); a device-code start does not
	init    initAnswer
	secret  string // the intent or the normalised code; never shown, never logged
	created time.Time

	// device-code progress
	cancel    context.CancelFunc
	waiting   bool
	userCode  string
	verifyURL string
	errText   string
	done      bool
	connected *connection
}

type pairer struct {
	m       *manager
	mu      sync.Mutex
	pend    map[string]*pending
	openURL func(string) error
	name    func() string
	client  func(origin string) *providerClient
}

func newPairer(m *manager) *pairer {
	return &pairer{m: m, pend: map[string]*pending{}, openURL: openBrowser, name: machineName, client: newProviderClient}
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (p *pairer) gcLocked() {
	for id, e := range p.pend {
		if time.Since(e.created) > pendingTTL || (e.done && time.Since(e.created) > time.Minute) {
			if e.cancel != nil {
				e.cancel()
			}
			delete(p.pend, id)
		}
	}
}

func (p *pairer) add(e *pending) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	if len(p.pend) >= maxPending {
		return errors.New("too many connections are waiting for an answer - allow or cancel one first")
	}
	e.id, e.created = randomID(), time.Now()
	p.pend[e.id] = e
	return nil
}

// fromDeepLink starts the confirmation for a lendspare:// link. It changes no setting and no
// folder: at most it makes the two reads of the protocol and asks the person.
func (p *pairer) fromDeepLink(raw string) (map[string]any, error) {
	if isPackaged() && !p.m.deepLinkEnabled() {
		return nil, errLinksOff // a package's lendspare:// handler cannot be unregistered
	}
	hostParam, intent, err := parseDeepLink(raw)
	if err != nil {
		return nil, err
	}
	origin, err := p.m.policy.fromParam(hostParam)
	if err != nil {
		return nil, err
	}
	return p.begin(kindIntent, origin, intent)
}

// fromCode starts the confirmation for a code typed by hand.
func (p *pairer) fromCode(providerHost, code string) (map[string]any, error) {
	origin, err := p.m.policy.byHost(providerHost)
	if err != nil {
		return nil, err
	}
	c, err := normalizeCode(code)
	if err != nil {
		return nil, err
	}
	return p.begin(kindCode, origin, c)
}

func (p *pairer) begin(kind, origin, secret string) (map[string]any, error) {
	if err := p.m.policy.allowed(origin, false); err != nil {
		return nil, err
	}
	pc := p.client(origin)
	desc, err := pc.describe()
	if err != nil {
		return nil, fmt.Errorf("%s did not answer as a LendSpare provider: %w", hostOf(origin), err)
	}
	dev := p.m.device()
	body := map[string]any{"deviceId": dev.ID, "publicKey": dev.PublicKey, "name": p.name(), "platform": runtime.GOOS}
	body[map[string]string{kindIntent: "intent", kindCode: "code"}[kind]] = secret
	var ans initAnswer
	if err := pc.do("POST", "/api/v2/share/devices/initialize", body, &ans); err != nil {
		return nil, errors.New(pairErrorText(err))
	}
	if ans.Challenge == "" || len(ans.Scopes) == 0 {
		return nil, errors.New("the provider's answer was incomplete")
	}
	for _, s := range ans.Scopes {
		if s != scopeCompute && s != scopeFiles {
			log.Printf("[connect] the provider asked for a scope this version does not know (%s); it will be ignored", truncate(s, 40))
		}
	}
	e := &pending{kind: kind, origin: origin, desc: desc, known: true, init: ans, secret: secret}
	if err := p.add(e); err != nil {
		return nil, err
	}
	return p.viewOf(e), nil
}

// startDevice starts the confirmation for connecting from LendSpare (no website first). Nothing
// but the provider's public description is read before the person allows it.
func (p *pairer) startDevice(providerHost string) (map[string]any, error) {
	origin, err := p.m.policy.byHost(providerHost)
	if err != nil {
		return nil, err
	}
	desc, derr := p.client(origin).describe()
	if derr != nil {
		log.Printf("[connect] %s has no provider description (%v); continuing with its host only", hostOf(origin), derr)
		desc = providerDesc{}
	}
	e := &pending{kind: kindDevice, origin: origin, desc: desc}
	if err := p.add(e); err != nil {
		return nil, err
	}
	return p.viewOf(e), nil
}

func (p *pairer) viewOf(e *pending) map[string]any {
	host := hostOf(e.origin)
	service, scopes, account, terms := e.init.Service, e.init.Scopes, e.init.Account, e.init.Terms
	if !e.known {
		service, terms = "ai", e.desc.Terms
		if len(e.desc.Services) > 0 {
			service = e.desc.Services[0]
		}
	}
	termsHost := host
	if u, err := url.Parse(terms.URL); err == nil && u.Host != "" {
		termsHost = u.Host
	}
	title, summary, details := confirmationText(host, e.desc.Operator, service, account, scopes, termsHost, terms.URL, e.known)
	v := map[string]any{"id": e.id, "kind": e.kind, "host": host, "operator": e.desc.Operator, "service": service,
		"serviceName": serviceName(service), "scopes": scopes, "account": account, "terms": terms, "title": title,
		"summary": summary, "details": details, "code": e.init.Code, "waiting": e.waiting, "userCode": e.userCode,
		"verifyUrl": e.verifyURL, "error": e.errText, "done": e.done}
	if e.connected != nil {
		v["connection"] = e.connected.ID
	}
	return v
}

// list is every confirmation still open, for the settings page and the tray.
func (p *pairer) list() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	out := []map[string]any{}
	for _, e := range p.pend {
		out = append(out, p.viewOf(e))
	}
	return out
}

func (p *pairer) take(id string) (*pending, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.pend[id]
	return e, ok
}

// allow is the person's "Allow connection". Only now is a credential asked for.
func (p *pairer) allow(id string) (map[string]any, error) {
	e, ok := p.take(id)
	if !ok {
		return nil, errors.New("that request is no longer open")
	}
	if e.kind == kindDevice {
		return p.allowDevice(e)
	}
	priv, err := loadDeviceKey(p.m.secrets)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, []byte("share-pair-v2\n"+e.init.Challenge+"\n"+e.origin))
	dev := p.m.device()
	body := map[string]any{"deviceId": dev.ID, "signature": base64.StdEncoding.EncodeToString(sig)}
	body[map[string]string{kindIntent: "intent", kindCode: "code"}[e.kind]] = e.secret
	var ans completeAnswer
	if err := p.client(e.origin).do("POST", "/api/v2/share/pair/complete", body, &ans); err != nil {
		p.drop(id)
		return nil, errors.New(pairErrorText(err))
	}
	scopes := ans.Scopes
	if len(scopes) == 0 {
		scopes = e.init.Scopes
	}
	conn, err := p.m.upsert(connection{ID: ans.ConnectionID, Provider: firstNonEmpty(e.desc.Provider, hostOf(e.origin)),
		Operator: e.desc.Operator, Origin: e.origin, Service: firstNonEmpty(ans.Service, e.init.Service),
		Account: firstNonEmpty(ans.Account, e.init.Account), Scopes: knownScopes(scopes), WorkerID: ans.WorkerID,
		Name: p.name()}, ans.Token)
	p.drop(id)
	if err != nil {
		return nil, err
	}
	log.Printf("[connect] connected to %s (%s)", hostOf(e.origin), conn.Service)
	return map[string]any{"done": true, "connection": conn.ID}, nil
}

func knownScopes(in []string) []string {
	out := []string{}
	for _, s := range in {
		if (s == scopeCompute || s == scopeFiles) && !contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (p *pairer) drop(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pend, id)
}

// cancel is "Cancel": the provider is told so its page stops waiting, and nothing is kept.
func (p *pairer) cancel(id string) error {
	e, ok := p.take(id)
	if !ok {
		return nil
	}
	if e.cancel != nil {
		e.cancel()
	}
	if e.kind != kindDevice {
		body := map[string]any{"deviceId": p.m.device().ID}
		body[map[string]string{kindIntent: "intent", kindCode: "code"}[e.kind]] = e.secret
		if err := p.client(e.origin).do("POST", "/api/v2/share/intents/decline", body, nil); err != nil {
			log.Printf("[connect] could not tell %s the connection was declined: %v", hostOf(e.origin), err)
		}
	}
	p.drop(id)
	return nil
}

// --- the device-code flow (connecting from LendSpare) --------------------------------------------

type pairStart struct {
	UserCode  string `json:"userCode"`
	Device    string `json:"device"`
	VerifyURL string `json:"verifyUrl"`
	ExpiresIn int    `json:"expiresIn"`
	Interval  int    `json:"interval"`
}

type pairPoll struct {
	Status       string   `json:"status"` // pending | expired | approved
	Token        string   `json:"token"`
	WorkerID     int      `json:"workerId"`
	Name         string   `json:"name"`
	ConnectionID string   `json:"connectionId"`
	Service      string   `json:"service"`
	Scopes       []string `json:"scopes"`
	Account      string   `json:"account"`
}

var errLinksOff = errors.New("lendspare:// links are turned off in LendSpare's settings")

var errPairExpired = errors.New("the pairing code expired before it was approved")

func (p *pairer) allowDevice(e *pending) (map[string]any, error) {
	p.mu.Lock()
	if e.waiting || e.done {
		p.mu.Unlock()
		return p.viewOf(e), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel, e.waiting = cancel, true
	p.mu.Unlock()
	go func() {
		res, desc, err := p.runDevice(ctx, e)
		p.mu.Lock()
		defer p.mu.Unlock()
		e.waiting = false
		if err != nil {
			e.errText = pairErrorText(err)
			if errors.Is(err, errPairExpired) {
				e.errText = "The code expired before it was approved."
			}
			if ctx.Err() == nil {
				log.Printf("[connect] %s: %v", hostOf(e.origin), err)
			}
			return
		}
		e.done, e.connected = true, &res
		_ = desc
	}()
	return map[string]any{"waiting": true}, nil
}

// runDevice does the whole device-code round trip. The start call carries deviceId and
// publicKey; a provider that predates them answers without the v2 fields and the connection is
// recorded with the scopes such a provider always implied.
func (p *pairer) runDevice(ctx context.Context, e *pending) (connection, providerDesc, error) {
	pc := p.client(e.origin)
	dev := p.m.device()
	var start pairStart
	err := pc.do("POST", "/api/v1/mesh/pair", map[string]string{"name": p.name(), "platform": runtime.GOOS,
		"deviceId": dev.ID, "publicKey": dev.PublicKey}, &start)
	if err == nil && (start.Device == "" || start.UserCode == "") {
		err = errors.New("the provider's answer had no code")
	}
	if err != nil {
		return connection{}, e.desc, err
	}
	// Only the provider's own page is opened: an answer from an impersonated server must not
	// send the person's browser somewhere else.
	if !sameOrigin(e.origin, start.VerifyURL) {
		start.VerifyURL = e.origin + "/mesh/pair?code=" + url.QueryEscape(start.UserCode)
	}
	p.mu.Lock()
	e.userCode, e.verifyURL = start.UserCode, start.VerifyURL
	p.mu.Unlock()
	if p.openURL != nil {
		if err := p.openURL(start.VerifyURL); err != nil {
			log.Printf("[connect] could not open a browser (%v)", err)
		}
	}
	interval := time.Duration(start.Interval) * time.Second
	if interval < time.Second {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	failures := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return connection{}, e.desc, ctx.Err()
		case <-time.After(interval):
		}
		var res pairPoll
		if err := pc.do("POST", "/api/v1/mesh/pair/poll", map[string]string{"device": start.Device}, &res); err != nil {
			if failures++; failures >= 10 {
				return connection{}, e.desc, err
			}
			continue
		}
		failures = 0
		switch res.Status {
		case "approved":
			if res.Token == "" {
				return connection{}, e.desc, errors.New("the provider approved but sent no token")
			}
			scopes := knownScopes(res.Scopes)
			if len(scopes) == 0 {
				scopes = []string{scopeCompute, scopeFiles}
				log.Printf("[connect] %s predates protocol v2: recording the connection with %s and %s", hostOf(e.origin), scopeCompute, scopeFiles)
			}
			conn, err := p.m.upsert(connection{ID: res.ConnectionID, Provider: firstNonEmpty(e.desc.Provider, hostOf(e.origin)),
				Operator: e.desc.Operator, Origin: e.origin, Service: firstNonEmpty(res.Service, "ai"), Account: res.Account,
				Scopes: scopes, WorkerID: res.WorkerID, Name: firstNonEmpty(res.Name, p.name())}, res.Token)
			return conn, e.desc, err
		case "expired":
			return connection{}, e.desc, errPairExpired
		}
	}
	return connection{}, e.desc, errPairExpired
}

// browserCommand is the command that opens a URL in the default browser.
func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}

func openBrowser(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return fmt.Errorf("refusing to open %q", url)
	}
	name, args := browserCommand(runtime.GOOS, url)
	return proc.Command(name, args...).Start()
}
