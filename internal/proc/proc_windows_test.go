//go:build windows

package proc

import "testing"

func TestCommandSetsNoWindowFlags(t *testing.T) {
	a := Command("cmd").SysProcAttr
	if !a.HideWindow || a.CreationFlags&createNoWindow == 0 {
		t.Fatalf("SysProcAttr = %+v, want HideWindow and CREATE_NO_WINDOW", a)
	}
}
