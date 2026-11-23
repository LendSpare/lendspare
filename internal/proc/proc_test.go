package proc

import (
	"runtime"
	"slices"
	"testing"
)

func TestCommandKeepsArgsAndHidesWindowOnWindows(t *testing.T) {
	c := Command("docker", "version", "--format", "{{.Server.Version}}")
	if want := []string{"docker", "version", "--format", "{{.Server.Version}}"}; !slices.Equal(c.Args, want) {
		t.Fatalf("args = %q, want %q", c.Args, want)
	}
	if runtime.GOOS == "windows" {
		if c.SysProcAttr == nil {
			t.Fatal("no SysProcAttr: a console window would flash")
		}
	} else if c.SysProcAttr != nil {
		t.Fatalf("unexpected SysProcAttr %+v", c.SysProcAttr)
	}
}
