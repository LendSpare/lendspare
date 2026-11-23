//go:build windows

package app

import "testing"

// The registry layer of "Start with Windows", on a value name of its own so a developer's
// real autostart entry is never touched.
func TestRunValueRoundTrip(t *testing.T) {
	const name = "LendSpareTest"
	deleteRunValue(name)
	defer deleteRunValue(name)

	if hasRunValue(name) {
		t.Fatal("value present before it was written")
	}
	if err := writeRunValue(name, `"C:\Some Dir\lendspare.exe"`); err != nil {
		t.Fatal(err)
	}
	if !hasRunValue(name) {
		t.Fatal("value missing after write")
	}
	deleteRunValue(name)
	if hasRunValue(name) {
		t.Fatal("value still present after delete")
	}
}

func TestMakeIconReturnsAHandle(t *testing.T) {
	if makeIcon(46, 160, 67) == 0 {
		t.Fatal("CreateIconIndirect failed")
	}
}

func TestInstalledPathIsPerUser(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\x\AppData\Local`)
	if got := installedPath(); got != `C:\Users\x\AppData\Local\LendSpare\lendspare.exe` {
		t.Errorf("installedPath = %s", got)
	}
}
