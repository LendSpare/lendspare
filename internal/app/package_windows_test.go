//go:build windows

package app

import "testing"

func TestATestBinaryIsNotPackaged(t *testing.T) {
	if runningPackaged() {
		t.Fatal("go test binaries run outside any MSIX package")
	}
}

func TestPackagedLinkRegistrationIsLeftToTheManifest(t *testing.T) {
	setPackaged(t, true)
	if !linkRegistered() {
		t.Fatal("the manifest declares lendspare://, so it counts as registered")
	}
	if err := registerLink(); err != nil {
		t.Fatal(err)
	}
}

func TestPackagedAutostartIsLeftToWindows(t *testing.T) {
	setPackaged(t, true)
	if autostartEnabled() {
		t.Fatal("a packaged LendSpare must not report its own autostart state")
	}
}
