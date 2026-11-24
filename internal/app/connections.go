package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LendSpare/lendspare/internal/secrets"
)

// The connections this device holds, and the one place that changes them. Pairing adds or
// updates one connection; removing, pausing or losing one never touches another's token,
// settings, folder grants or history.

type clientKey struct{ id, origin string }

type manager struct {
	mu      sync.Mutex
	secrets secrets.Store
	policy  originPolicy
	cfg     appConfig
	mtime   time.Time
	clients map[clientKey]*apiClient

	onChange func()          // something about the connections changed: report again, repaint
	onRemove func(id string) // a connection is gone: forget what was granted to it
	onHint   func()          // a provider says a site waits on the shared folders
}

// openManager loads (and if needed migrates) the config and makes sure the device has its
// identity. Nothing here touches the network.
func openManager(store secrets.Store, policy originPolicy) (*manager, error) {
	cfg, err := loadConfig(store)
	if err != nil {
		return nil, err
	}
	m := &manager{secrets: store, policy: policy, cfg: cfg, clients: map[clientKey]*apiClient{}}
	created, err := ensureDevice(&m.cfg, store)
	if err != nil {
		return nil, fmt.Errorf("cannot create this device's identity: %w", err)
	}
	if _, statErr := os.Stat(configPath()); created || errors.Is(statErr, os.ErrNotExist) {
		if err := m.saveLocked(); err != nil {
			return nil, err
		}
	}
	m.noteMtime()
	return m, nil
}

func (m *manager) noteMtime() {
	if fi, err := os.Stat(configPath()); err == nil {
		m.mtime = fi.ModTime()
	}
}

// saveLocked writes config.json; the caller holds m.mu.
func (m *manager) saveLocked() error {
	m.cfg.SchemaVersion = configSchema
	if m.cfg.Connections == nil {
		m.cfg.Connections = []connection{}
	}
	b, _ := json.MarshalIndent(m.cfg, "", "  ")
	err := writeFileAtomic(configPath(), b)
	m.noteMtime()
	return err
}

// reloadLocked picks up a change another process made (`lendspare connect` while the tray runs).
func (m *manager) reloadLocked() {
	fi, err := os.Stat(configPath())
	if err != nil || !fi.ModTime().After(m.mtime) {
		return
	}
	b, err := os.ReadFile(configPath())
	if err != nil {
		return
	}
	var cfg appConfig
	if json.Unmarshal(b, &cfg) != nil || cfg.SchemaVersion < configSchema || cfg.Device.ID == "" {
		return
	}
	m.cfg = cfg
	m.mtime = fi.ModTime()
	m.clients = map[clientKey]*apiClient{}
	log.Printf("[config] reloaded after a change made elsewhere")
	if m.onChange != nil {
		go m.onChange()
	}
}

func (m *manager) changed() {
	if m.onChange != nil {
		go m.onChange()
	}
}

func (m *manager) device() deviceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Device
}

func (m *manager) deepLinkEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadLocked()
	return !m.cfg.NoDeepLink
}

func (m *manager) setDeepLink(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.NoDeepLink = !on
	return m.saveLocked()
}

func copyConn(c connection) connection {
	c.Scopes = append([]string{}, c.Scopes...)
	return c
}

// list returns the connections, oldest first. Tokens are never part of it.
func (m *manager) list() []connection {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadLocked()
	out := make([]connection, 0, len(m.cfg.Connections))
	for _, c := range m.cfg.Connections {
		out = append(out, copyConn(c))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Added.Before(out[j].Added) })
	return out
}

func (m *manager) get(id string) (connection, bool) {
	for _, c := range m.list() {
		if c.ID == id {
			return c, true
		}
	}
	return connection{}, false
}

// usable: connected, and its origin is still allowed (a development origin only while the
// flag is given, a legacy one only for a migrated connection).
func (m *manager) usable(c connection) bool {
	return c.State == stateConnected && m.policy.allowed(c.Origin, c.Legacy) == nil
}

// blockReason explains why a connection that is not disconnected still does not run.
func (m *manager) blockReason(c connection) string {
	if err := m.policy.allowed(c.Origin, c.Legacy); err != nil {
		return err.Error() + " (a development origin needs --dev-origin)"
	}
	return ""
}

// apiFor returns the client of one connection, made once per (connection, origin).
func (m *manager) apiFor(id string) (*apiClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadLocked()
	for _, c := range m.cfg.Connections {
		if c.ID != id {
			continue
		}
		if c.State != stateConnected {
			return nil, errors.New("this connection is disconnected")
		}
		if err := m.policy.allowed(c.Origin, c.Legacy); err != nil {
			return nil, err
		}
		key := clientKey{id, c.Origin}
		if a := m.clients[key]; a != nil {
			return a, nil
		}
		tok, err := m.secrets.Get(secretToken(id))
		if err != nil {
			return nil, fmt.Errorf("no token for this connection: %w", err)
		}
		a := &apiClient{server: c.Origin, token: tok, http: sameOriginClient(c.Origin, 120*time.Second), onFilesHint: m.onHint}
		m.clients[key] = a
		return a, nil
	}
	return nil, errors.New("no such connection")
}

func (m *manager) tokenOf(id string) string {
	a, err := m.apiFor(id)
	if err != nil {
		return ""
	}
	return a.token
}

// computeConns: connections whose worker may run now - the scope is granted, the local switch
// is on, nobody paused it and its origin is allowed.
func (m *manager) computeConns() []connection {
	out := []connection{}
	for _, c := range m.list() {
		if m.usable(c) && !c.Paused && c.Compute && c.has(scopeCompute) {
			out = append(out, c)
		}
	}
	return out
}

// filesConns: connections that may be asked for, and told about, shared folders.
func (m *manager) filesConns() []connection {
	out := []connection{}
	for _, c := range m.list() {
		if m.usable(c) && !c.Paused && c.has(scopeFiles) {
			out = append(out, c)
		}
	}
	return out
}

func sameAccount(a, b connection) bool {
	return a.Origin == b.Origin && a.Service == b.Service && strings.EqualFold(a.Account, b.Account)
}

// upsert adds a connection or updates the one it is the same as (same id from the provider,
// or the same origin, service and account). It never replaces the list, and what the person set
// locally on an existing connection (compute switch, pause) survives a re-pairing.
func (m *manager) upsert(c connection, token string) (connection, error) {
	if token == "" {
		return connection{}, errors.New("the provider sent no token")
	}
	if err := m.policy.allowed(c.Origin, false); err != nil {
		return connection{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadLocked()
	idx := -1
	for i, e := range m.cfg.Connections {
		if (c.ID != "" && e.ID == c.ID) || (c.ID == "" && sameAccount(e, c)) {
			idx = i
			break
		}
	}
	if c.ID == "" {
		c.ID = newUUIDv4()
	}
	c.State = stateConnected
	if idx >= 0 {
		old := m.cfg.Connections[idx]
		c.Compute, c.Paused, c.Legacy, c.Added = old.Compute, old.Paused, old.Legacy, old.Added
		c.ID = old.ID
		m.cfg.Connections[idx] = c
	} else {
		c.Compute = false // computing is off until the person switches it on
		c.Added = time.Now().UTC()
		m.cfg.Connections = append(m.cfg.Connections, c)
	}
	if err := m.secrets.Set(secretToken(c.ID), token); err != nil {
		return connection{}, fmt.Errorf("cannot keep the token: %w", err)
	}
	delete(m.clients, clientKey{c.ID, c.Origin})
	if err := m.saveLocked(); err != nil {
		return connection{}, err
	}
	m.changedLocked()
	return copyConn(c), nil
}

func (m *manager) changedLocked() {
	if m.onChange != nil {
		go m.onChange()
	}
}

func (m *manager) update(id string, f func(*connection)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadLocked()
	for i := range m.cfg.Connections {
		if m.cfg.Connections[i].ID == id {
			f(&m.cfg.Connections[i])
			err := m.saveLocked()
			m.changedLocked()
			return err
		}
	}
	return errors.New("no such connection")
}

func (m *manager) setPaused(id string, p bool) error {
	return m.update(id, func(c *connection) { c.Paused = p })
}

// setCompute is the local switch; the provider's scope alone never turns computing on.
func (m *manager) setCompute(id string, on bool) error {
	return m.update(id, func(c *connection) { c.Compute = on })
}

func (m *manager) setWorkerID(id string, n int) {
	if n <= 0 {
		return
	}
	// Every report answers with the worker id; an update would fire onChange, which reports again.
	m.mu.Lock()
	m.reloadLocked()
	same := false
	for _, c := range m.cfg.Connections {
		if c.ID == id && c.WorkerID == n {
			same = true
		}
	}
	m.mu.Unlock()
	if same {
		return
	}
	_ = m.update(id, func(c *connection) { c.WorkerID = n })
}

// markDisconnected is what a 401/403 means: the provider revoked this connection. Only its
// worker stops; the dead token is dropped; the record stays so the person sees it and can
// connect again.
func (m *manager) markDisconnected(id, why string) {
	err := m.update(id, func(c *connection) { c.State = stateDisconnected })
	if err != nil {
		return
	}
	m.mu.Lock()
	for k := range m.clients {
		if k.id == id {
			delete(m.clients, k)
		}
	}
	m.mu.Unlock()
	_ = m.secrets.Delete(secretToken(id))
	log.Printf("[connections] %s: %s - the connection is now disconnected", id, why)
}

// disconnect asks the provider to end the connection, then deletes this connection's token
// and record and nothing else. A provider that cannot be reached does not keep the person
// from removing it here; the notice says so.
func (m *manager) disconnect(id string) (notice string, err error) {
	c, ok := m.get(id)
	if !ok {
		return "", errors.New("no such connection")
	}
	if c.State == stateConnected && m.policy.allowed(c.Origin, c.Legacy) == nil {
		if a, aerr := m.apiFor(id); aerr == nil {
			req, _ := http.NewRequest(http.MethodDelete, a.server+"/api/v2/share/connection", nil)
			a.auth(req)
			resp, derr := a.http.Do(req)
			switch {
			case derr != nil:
				notice = "The provider could not be reached; remove this computer on its website as well."
			default:
				resp.Body.Close()
				if resp.StatusCode >= 500 {
					notice = "The provider answered with an error; remove this computer on its website as well."
				}
			}
		}
	}
	m.mu.Lock()
	m.reloadLocked()
	kept := m.cfg.Connections[:0:0]
	for _, e := range m.cfg.Connections {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	m.cfg.Connections = kept
	for k := range m.clients {
		if k.id == id {
			delete(m.clients, k)
		}
	}
	err = m.saveLocked()
	m.mu.Unlock()
	_ = m.secrets.Delete(secretToken(id))
	if m.onRemove != nil {
		m.onRemove(id)
	}
	m.changed()
	return notice, err
}

// view is what the settings page and `lendspare connections` show: no token, ever.
func (m *manager) view(c connection) map[string]any {
	state := c.State
	if r := m.blockReason(c); r != "" && state == stateConnected {
		state = "blocked"
	}
	return map[string]any{"id": c.ID, "provider": c.Provider, "operator": c.Operator, "host": hostOf(c.Origin),
		"origin": c.Origin, "service": c.Service, "account": c.Account, "scopes": c.Scopes, "state": state,
		"paused": c.Paused, "compute": c.Compute, "name": c.Name, "reason": m.blockReason(c)}
}
