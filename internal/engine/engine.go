// Package engine wraps the container CLI (docker or podman) and the GPU probes.
package engine

import (
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/LendSpare/lendspare/internal/proc"
)

// The container engine is whatever CLI answers: docker, or podman (rootless, or inside a
// `podman machine` on Windows/macOS). Everything else in the daemon only builds argument
// lists, so the few places the two differ are collected here.

// Resolve turns the --docker flag into a CLI to call. "auto" (the default) prefers
// docker when its server answers, then podman, and otherwise returns "docker" so the usual
// "no container runtime" reporting applies. Any other value is a CLI name or path.
func Resolve(flagValue string) string {
	if flagValue != "" && flagValue != "auto" {
		return flagValue
	}
	for _, bin := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		if engineAnswers(bin) {
			return bin
		}
	}
	return "docker"
}

func engineAnswers(bin string) bool {
	if proc.Command(bin, "version", "--format", "{{.Server.Version}}").Run() == nil {
		return true
	}
	return proc.Command(bin, "info", "--format", "{{.ID}}").Run() == nil
}

func isPodman(bin string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(bin, `\`, "/")))
	return strings.HasPrefix(base, "podman")
}

// GPUArgs is how a container is handed the GPU: Docker's --gpus, or the CDI device that
// nvidia-container-toolkit registers for Podman (`nvidia-ctk cdi generate`).
func GPUArgs(bin string) []string {
	if isPodman(bin) {
		return []string{"--device", "nvidia.com/gpu=all"}
	}
	return []string{"--gpus", "all"}
}

// MountOpt appends the options a bind mount needs on this engine. Podman on an SELinux
// host (Fedora, RHEL) refuses a mount it has not relabelled; `z` is harmless elsewhere.
func MountOpt(bin, opts string) string {
	if isPodman(bin) && runtime.GOOS == "linux" {
		if opts == "" {
			return ":z"
		}
		return ":" + opts + ",z"
	}
	if opts == "" {
		return ""
	}
	return ":" + opts
}

// InfoFormat is the `info --format` template for "CPUs|memory bytes".
func InfoFormat(bin string) string {
	if isPodman(bin) {
		return "{{.Host.CPUs}}|{{.Host.MemTotal}}"
	}
	return "{{.NCPU}}|{{.MemTotal}}"
}

// topArgs lists the container's host PIDs.
func topArgs(bin, container string) []string {
	if isPodman(bin) {
		return []string{"top", container, "pid"}
	}
	return []string{"top", container, "-eo", "pid"}
}

// SelfImage is the image the daemon itself runs from when it is a container (set by the
// compose file): the egress sidecar is started from it, because a path inside this
// container means nothing to the host's engine.
func SelfImage() string {
	if v := os.Getenv("LENDSPARE_SELF_IMAGE"); v != "" {
		return v
	}
	return os.Getenv("MESH_SELF_IMAGE") // deprecated name
}
