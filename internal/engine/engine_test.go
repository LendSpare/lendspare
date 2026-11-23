package engine

import (
	"reflect"
	"runtime"
	"testing"
)

func TestResolveEngineHonoursAnExplicitChoice(t *testing.T) {
	for _, v := range []string{"podman", "docker", `C:\tools\podman.exe`} {
		if got := Resolve(v); got != v {
			t.Errorf("resolveEngine(%q) = %q", v, got)
		}
	}
}

func TestIsPodman(t *testing.T) {
	cases := map[string]bool{
		"podman": true, "/usr/bin/podman": true, `C:\Program Files\RedHat\Podman\podman.exe`: true,
		"podman-remote": true, "docker": false, "/usr/bin/docker": false, "docker.exe": false,
	}
	for bin, want := range cases {
		if got := isPodman(bin); got != want {
			t.Errorf("isPodman(%q) = %v, want %v", bin, got, want)
		}
	}
}

func TestGPUArgsPerEngine(t *testing.T) {
	if got := GPUArgs("docker"); !reflect.DeepEqual(got, []string{"--gpus", "all"}) {
		t.Errorf("docker: %v", got)
	}
	if got := GPUArgs("podman"); !reflect.DeepEqual(got, []string{"--device", "nvidia.com/gpu=all"}) {
		t.Errorf("podman: %v", got)
	}
}

func TestMountOptions(t *testing.T) {
	if got := MountOpt("docker", "ro"); got != ":ro" {
		t.Errorf("docker ro: %q", got)
	}
	if got := MountOpt("docker", ""); got != "" {
		t.Errorf("docker plain: %q", got)
	}
	wantRO, wantPlain := ":ro", ""
	if runtime.GOOS == "linux" { // SELinux relabel only where it exists
		wantRO, wantPlain = ":ro,z", ":z"
	}
	if got := MountOpt("podman", "ro"); got != wantRO {
		t.Errorf("podman ro: %q, want %q", got, wantRO)
	}
	if got := MountOpt("podman", ""); got != wantPlain {
		t.Errorf("podman plain: %q, want %q", got, wantPlain)
	}
}

func TestInfoAndTopTemplates(t *testing.T) {
	if InfoFormat("docker") != "{{.NCPU}}|{{.MemTotal}}" || InfoFormat("podman") != "{{.Host.CPUs}}|{{.Host.MemTotal}}" {
		t.Error("info templates")
	}
	if got := topArgs("podman", "c"); !reflect.DeepEqual(got, []string{"top", "c", "pid"}) {
		t.Errorf("podman top: %v", got)
	}
	if got := topArgs("docker", "c"); !reflect.DeepEqual(got, []string{"top", "c", "-eo", "pid"}) {
		t.Errorf("docker top: %v", got)
	}
}
