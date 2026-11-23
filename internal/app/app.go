package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LendSpare/lendspare/internal/secrets"
)

// app is what every front end (tray, settings page, command line) works on: the connections,
// the pairing flow and the shared folders. Opening it contacts nothing.
type app struct {
	cfg   config
	mgr   *manager
	pair  *pairer
	files *filesService
}

// appInst is the process's one app, for the tray and the settings page.
var appInst *app

func openApp(cfg config) (*app, error) {
	policy, err := newOriginPolicy(cfg.devOrigins)
	if err != nil {
		return nil, err
	}
	// The secret store and the config come first: loadConfig may migrate files.json before
	// the files store reads it.
	store := secrets.Open(configDir())
	mgr, err := openManager(store, policy)
	if err != nil {
		return nil, err
	}
	a := &app{cfg: cfg, mgr: mgr, pair: newPairer(mgr)}
	a.files, _ = sharedFiles()
	if a.files != nil {
		mgr.onRemove = a.files.store.forgetConnection
		a.files.provider = func(conn string) string {
			if c, ok := mgr.get(conn); ok {
				return hostOf(c.Origin)
			}
			return ""
		}
	}
	appInst = a
	return a, nil
}

// handOffLink gives a lendspare:// link to the running instance through its loopback settings
// server, authenticated like every other local call (the token in the private settings-url
// file). False when no instance answers.
func handOffLink(link string) bool {
	b, err := os.ReadFile(settingsURLPath())
	if err != nil {
		return false
	}
	u := strings.TrimSpace(string(b))
	parts := strings.SplitN(u, "#t=", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "http://127.0.0.1:") {
		return false
	}
	client := &http.Client{Timeout: 15 * time.Second}
	if resp, err := client.Get(parts[0] + "health"); err != nil {
		return false
	} else {
		resp.Body.Close()
	}
	body, _ := json.Marshal(map[string]string{"url": link})
	req, _ := http.NewRequest(http.MethodPost, parts[0]+"api/deeplink", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LendSpare-Token", parts[1])
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		fmt.Fprintln(os.Stderr, "LendSpare could not use this link:", e.Error)
	}
	return true // an instance took it, whether or not it accepted the link
}

// openStartupLink confirms the link LendSpare was started with, once the app is up.
func (d *daemon) openStartupLink() {
	if startupLink == "" || appInst == nil {
		return
	}
	if _, err := appInst.pair.fromDeepLink(startupLink); err != nil {
		fmt.Fprintln(os.Stderr, "LendSpare could not use this link:", err)
		return
	}
	startupLink = ""
	openSettingsPage()
}
