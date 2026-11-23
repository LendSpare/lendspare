package app

import (
	"errors"
	"strings"
)

// The settings page's calls about connections. They are reached only with the page's token and
// a matching Origin (settings.go), so no web page can make them. A lendspare:// link arrives here
// too (POST /api/deeplink, from a second LendSpare process) and can do one thing: open a
// confirmation the person must answer.

func (s *settingsServer) connectionsAPI(method, path string, body map[string]any) (out any, handled bool, err error) {
	a := s.app
	if a == nil {
		return nil, false, nil
	}
	switch method + " " + path {
	case "POST /api/deeplink":
		v, err := a.pair.fromDeepLink(strField(body, "url"))
		if err == nil {
			openSettingsPage()
		}
		return v, true, err
	case "POST /api/connect/start":
		v, err := a.pair.startDevice(strField(body, "provider"))
		return v, true, err
	case "POST /api/connect/code":
		v, err := a.pair.fromCode(strField(body, "provider"), strField(body, "code"))
		return v, true, err
	case "POST /api/connect/allow":
		v, err := a.pair.allow(strField(body, "id"))
		return v, true, err
	case "POST /api/connect/cancel":
		return map[string]any{"ok": true}, true, a.pair.cancel(strField(body, "id"))
	case "POST /api/connections/pause":
		return map[string]any{"ok": true}, true, a.mgr.setPaused(strField(body, "id"), boolField(body, "paused"))
	case "POST /api/connections/compute":
		c, ok := a.mgr.get(strField(body, "id"))
		if !ok || !c.has(scopeCompute) {
			return nil, true, errors.New("this connection has no computing permission")
		}
		return map[string]any{"ok": true}, true, a.mgr.setCompute(c.ID, boolField(body, "enabled"))
	case "POST /api/connections/disconnect":
		notice, err := a.mgr.disconnect(strField(body, "id"))
		return map[string]any{"ok": err == nil, "notice": notice}, true, err
	case "POST /api/shares/grant":
		c, ok := a.mgr.get(strField(body, "connection"))
		if !ok || !c.has(scopeFiles) {
			return nil, true, errors.New("this connection has no folder permission")
		}
		if s.files == nil {
			return nil, true, errors.New("shared folders could not start - see the log")
		}
		return map[string]any{"ok": true}, true, s.files.store.setGrant(strField(body, "name"), c.ID, boolField(body, "granted"))
	case "POST /api/deeplink-handler":
		on := boolField(body, "enabled")
		if err := a.mgr.setDeepLink(on); err != nil {
			return nil, true, err
		}
		if on {
			return map[string]any{"ok": true}, true, registerLink()
		}
		return map[string]any{"ok": true}, true, unregisterLink()
	}
	return nil, false, nil
}

// connectionsState is added to the page's state.
func (s *settingsServer) connectionsState(out map[string]any) {
	a := s.app
	if a == nil {
		return
	}
	conns := []map[string]any{}
	for _, c := range a.mgr.list() {
		conns = append(conns, a.mgr.view(c))
	}
	out["connections"] = conns
	out["pending"] = a.pair.list()
	provs := []string{}
	for _, o := range a.mgr.policy.providers() {
		provs = append(provs, strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://"))
	}
	out["providers"] = provs
	out["deepLink"] = a.mgr.deepLinkEnabled()
	out["device"] = a.mgr.device().ID
}
