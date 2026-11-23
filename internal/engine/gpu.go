package engine

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LendSpare/lendspare/internal/proc"
)

// GPUState returns (foreignUsedMB, freeMB). foreignUsedMB is VRAM used by compute
// processes that are NOT part of our ComfyUI container — the owner's own use. freeMB is
// the card's free memory (-1 if unknown). Without nvidia-smi both are "nothing known":
// (0, -1), so a machine without an NVIDIA card is never considered busy.
func GPUState(docker, container string) (int, int) {
	free := -1
	if out, err := proc.Command("nvidia-smi", "--query-gpu=memory.free", "--format=csv,noheader,nounits").Output(); err == nil {
		if v, err := strconv.Atoi(firstLine(string(out))); err == nil {
			free = v
		}
	}
	ours := containerPIDs(docker, container)
	foreign := 0
	if out, err := proc.Command("nvidia-smi", "--query-compute-apps=pid,used_memory", "--format=csv,noheader,nounits").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.Split(line, ",")
			if len(parts) != 2 {
				continue
			}
			pid := strings.TrimSpace(parts[0])
			mem, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			if pid == "" || ours[pid] {
				continue
			}
			foreign += mem
		}
	}
	return foreign, free
}

// containerPIDs returns the host PIDs inside our container so their GPU use is not
// mistaken for the owner's.
func containerPIDs(docker, container string) map[string]bool {
	pids := map[string]bool{}
	out, err := proc.Command(docker, topArgs(docker, container)...).Output()
	if err != nil {
		return pids
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "PID" || !isNumeric(line) {
			continue
		}
		pids[line] = true
	}
	return pids
}

// GPUName is the first NVIDIA card's name, or "" without nvidia-smi.
func GPUName() string {
	out, err := proc.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	return firstLine(string(out))
}

var (
	dockerOK bool
	dockerAt time.Time
	dockerMu sync.Mutex
)

// Available reports whether a container runtime answers (re-checked every minute).
func Available(docker string) bool {
	dockerMu.Lock()
	defer dockerMu.Unlock()
	if time.Since(dockerAt) < time.Minute {
		return dockerOK
	}
	dockerAt = time.Now()
	dockerOK = proc.Command(docker, "version", "--format", "{{.Server.Version}}").Run() == nil
	return dockerOK
}

// EgressProxyAvailable reports whether the egress proxy can run: the allow-list proxy runs this very binary inside a sidecar
// container, so it needs a Linux build of the daemon and a Linux container runtime.
func EgressProxyAvailable(docker string) bool {
	return runtime.GOOS == "linux" && Available(docker)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
