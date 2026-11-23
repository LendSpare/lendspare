package app

import (
	"testing"

	"github.com/LendSpare/lendspare/internal/secrets"
)

// isolateConfig points the config directory at a temp directory, whatever the OS.
func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	t.Setenv("LENDSPARE_CONFIG_DIR", "")
	t.Setenv("LENDSPARE_SECRETS", "file")
}

// newTestManager opens a manager on an isolated config with the file secret store, allowing
// the given development origins (the httptest servers).
func newTestManager(t *testing.T, dev ...string) *manager {
	t.Helper()
	isolateConfig(t)
	policy, err := newOriginPolicy(dev)
	if err != nil {
		t.Fatal(err)
	}
	m, err := openManager(secrets.NewFile(configDir()), policy)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// connect adds a connection with a token straight to the manager (no pairing), compute on.
func connect(t *testing.T, m *manager, origin, token string, scopes ...string) connection {
	t.Helper()
	c, err := m.upsert(connection{Origin: origin, Provider: hostOf(origin), Service: "ai", Scopes: scopes}, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.setCompute(c.ID, true); err != nil {
		t.Fatal(err)
	}
	c, _ = m.get(c.ID)
	return c
}
