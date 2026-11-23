package app

import (
	"errors"
	"testing"
)

func setPackaged(t *testing.T, v bool) {
	t.Helper()
	old := isPackaged
	isPackaged = func() bool { return v }
	t.Cleanup(func() { isPackaged = old })
}

// A package's lendspare:// handler stays registered, so the off switch must refuse the link itself.
func TestPackagedShareRefusesLinksWhenTheSwitchIsOff(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	if err := m.setDeepLink(false); err != nil {
		t.Fatal(err)
	}
	p.open(testIntentA)

	setPackaged(t, true)
	if _, err := newPairer(m).fromDeepLink(p.link(testIntentA)); !errors.Is(err, errLinksOff) {
		t.Fatalf("packaged, links off: err = %v, want errLinksOff", err)
	}
	if err := m.setDeepLink(true); err != nil {
		t.Fatal(err)
	}
	if _, err := newPairer(m).fromDeepLink(p.link(testIntentA)); err != nil {
		t.Fatalf("packaged, links on: %v", err)
	}
}

// Unpackaged, the switch works by (un)registering the handler, so a link that arrives is used.
func TestUnpackagedShareIgnoresTheSwitchOnArrival(t *testing.T) {
	p := newFakeProvider(t, "a")
	m := newTestManager(t, p.origin())
	if err := m.setDeepLink(false); err != nil {
		t.Fatal(err)
	}
	p.open(testIntentA)

	setPackaged(t, false)
	if _, err := newPairer(m).fromDeepLink(p.link(testIntentA)); err != nil {
		t.Fatalf("unpackaged: %v", err)
	}
}
