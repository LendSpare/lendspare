package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LendSpare/lendspare/internal/secrets"
)

// config.json, schema 2 (docs/PROTOCOL.md section 6): this installation's identity and the
// connections it holds. Secrets are not in it: the private key and every token live in the
// secret store (internal/secrets).

const configSchema = 2

const (
	scopeCompute = "compute:offer"
	scopeFiles   = "files:serve"

	stateConnected    = "connected"
	stateDisconnected = "disconnected"
)

type deviceInfo struct {
	ID        string `json:"id"`        // random UUIDv4: names this installation, authenticates nothing
	PublicKey string `json:"publicKey"` // standard base64 of the raw 32-byte Ed25519 key
}

// connection is one (provider, account, service) this device is connected to.
type connection struct {
	ID       string    `json:"id"`
	Provider string    `json:"provider"`           // provider id from /.well-known/share-provider, else the host
	Operator string    `json:"operator,omitempty"` // legal name shown in the confirmation
	Origin   string    `json:"origin"`
	Service  string    `json:"service"`
	Account  string    `json:"account,omitempty"` // a label the person recognises, never an id
	Scopes   []string  `json:"scopes"`
	WorkerID int       `json:"workerId,omitempty"`
	State    string    `json:"state"`
	Name     string    `json:"name,omitempty"`    // what this computer is called there
	Paused   bool      `json:"paused,omitempty"`  // stops this connection's workers, nothing else
	Compute  bool      `json:"compute,omitempty"` // the local switch: off for every new connection
	Legacy   bool      `json:"legacy,omitempty"`  // migrated from schema 1: may use a legacy origin
	Added    time.Time `json:"added"`
}

func (c connection) has(scope string) bool { return contains(c.Scopes, scope) }

type appConfig struct {
	SchemaVersion int          `json:"schemaVersion"`
	Device        deviceInfo   `json:"device"`
	Connections   []connection `json:"connections"`
	NoDeepLink    bool         `json:"noDeepLink,omitempty"` // the person turned the lendspare:// handler off
}

// configDir is where the config, the settings, the log and the installed copy live.
func configDir() string {
	if d := os.Getenv("LENDSPARE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(userConfigBase(), "LendSpare")
}

func userConfigBase() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return base
}

// legacyConfigDir is where the previous program (version 2.x of the daemon) kept its files.
func legacyConfigDir() string { return filepath.Join(userConfigBase(), "mesh-daemon") }

func configPath() string { return filepath.Join(configDir(), "config.json") }

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// --- device identity -------------------------------------------------------------------

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

const secretDeviceKey = "device-key"

func secretToken(connID string) string { return "token-" + connID }

// ensureDevice creates the identity on first start. A config that names a device whose key is
// gone gets a new identity: tokens are bearer credentials and do not depend on the key.
func ensureDevice(cfg *appConfig, store secrets.Store) (bool, error) {
	if cfg.Device.ID != "" {
		if _, err := store.Get(secretDeviceKey); err == nil {
			return false, nil
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return false, err
	}
	if err := store.Set(secretDeviceKey, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		return false, err
	}
	cfg.Device = deviceInfo{ID: newUUIDv4(), PublicKey: base64.StdEncoding.EncodeToString(pub)}
	return true, nil
}

func loadDeviceKey(store secrets.Store) (ed25519.PrivateKey, error) {
	s, err := store.Get(secretDeviceKey)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("the device key is damaged")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// --- loading, with the one-time migration ------------------------------------------------

// v1Config is what the previous program kept: one server, one token.
type v1Config struct {
	Server   string `json:"server"`
	Token    string `json:"token"`
	WorkerID int    `json:"workerId"`
	Name     string `json:"name"`
}

// loadConfig reads config.json, migrating a schema-1 file (and the old program's directory)
// once. The originals stay as copies under backup-v1/, so the step can be undone by hand.
func loadConfig(store secrets.Store) (appConfig, error) {
	dir := configDir()
	if _, err := os.Stat(configPath()); errors.Is(err, os.ErrNotExist) {
		copyLegacyFiles(legacyConfigDir(), dir)
	}
	cfg := appConfig{SchemaVersion: configSchema}
	b, err := os.ReadFile(configPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		upgradeFilesJSON(dir, "")
		return cfg, nil
	case err != nil:
		return cfg, err
	}
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return cfg, fmt.Errorf("%s: %w", configPath(), err)
	}
	if probe.SchemaVersion >= configSchema {
		return cfg, json.Unmarshal(b, &cfg)
	}
	return migrateV1(dir, b, store)
}

// copyLegacyFiles copies (never moves) the previous program's files into a fresh directory.
func copyLegacyFiles(from, to string) {
	for _, name := range []string{"config.json", "files.json", "files.key", "files-activity.log"} {
		src, err := os.Open(filepath.Join(from, name))
		if err != nil {
			continue
		}
		if err := os.MkdirAll(to, 0o700); err == nil {
			if dst, err := os.OpenFile(filepath.Join(to, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
				_, _ = io.Copy(dst, src)
				dst.Close()
			}
		}
		src.Close()
	}
}

func migrateV1(dir string, raw []byte, store secrets.Store) (appConfig, error) {
	cfg := appConfig{SchemaVersion: configSchema}
	var old v1Config
	if err := json.Unmarshal(raw, &old); err != nil {
		return cfg, err
	}
	backup := filepath.Join(dir, "backup-v1")
	if err := os.MkdirAll(backup, 0o700); err != nil {
		return cfg, err
	}
	if _, err := os.Stat(filepath.Join(backup, "config.json")); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(filepath.Join(backup, "config.json"), raw, 0o600); err != nil {
			return cfg, err
		}
	}
	grantTo := ""
	origin, err := normalizeOrigin(old.Server)
	if err == nil && old.Token != "" {
		id := newUUIDv4()
		conn := connection{ID: id, Provider: hostOf(origin), Origin: origin, Service: "ai",
			Scopes: []string{scopeCompute, scopeFiles}, WorkerID: old.WorkerID, State: stateConnected,
			Name: old.Name, Compute: true, Legacy: contains(legacyOrigins, origin), Added: time.Now().UTC()}
		if err := store.Set(secretToken(id), old.Token); err != nil {
			return cfg, fmt.Errorf("cannot keep the old pairing: %w", err)
		}
		cfg.Connections = []connection{conn}
		grantTo = id
		log.Printf("[config] migrated the previous pairing with %s to a connection with the rights it had", conn.Provider)
	} else if old.Token != "" {
		log.Printf("[config] the previous pairing names no usable server; it was not migrated (a copy is in %s)", backup)
	}
	if _, err := ensureDevice(&cfg, store); err != nil {
		return cfg, err
	}
	upgradeFilesJSON(dir, grantTo)
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return cfg, writeFileAtomic(filepath.Join(dir, "config.json"), b)
}

// upgradeFilesJSON stamps files.json with schema 2 and grants its folders to the migrated
// connection only. It runs once (the stamp) and never widens anything: with no migrated
// connection no folder is granted to anybody.
func upgradeFilesJSON(dir, grantTo string) {
	path := filepath.Join(dir, "files.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return
	}
	var version int
	_ = json.Unmarshal(doc["schemaVersion"], &version)
	if version >= configSchema {
		return
	}
	backup := filepath.Join(dir, "backup-v1")
	_ = os.MkdirAll(backup, 0o700)
	if _, err := os.Stat(filepath.Join(backup, "files.json")); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(filepath.Join(backup, "files.json"), b, 0o600)
	}
	var shares []map[string]any
	_ = json.Unmarshal(doc["shares"], &shares)
	for _, s := range shares {
		s["connections"] = []string{}
		if grantTo != "" {
			s["connections"] = []string{grantTo}
		}
	}
	doc["shares"], _ = json.Marshal(shares)
	doc["schemaVersion"] = json.RawMessage("2")
	out, _ := json.MarshalIndent(doc, "", "  ")
	if err := writeFileAtomic(path, out); err != nil {
		log.Printf("[config] could not upgrade files.json: %v", err)
	}
}

// machineName is the suggestion shown to a provider ("Gaming PC").
func machineName() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return truncate(strings.TrimSpace(h), 60)
	}
	return ""
}
