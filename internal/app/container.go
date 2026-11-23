package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LendSpare/lendspare/internal/engine"
	"github.com/LendSpare/lendspare/internal/proc"
)

// The container contract (docs/features/07), file-based so it works with --network none
// and on an --internal network alike:
//
//	in : /task/job.json  {kind, guid, payload, inputs: [{name, role, path}]}
//	     /task/inputs/<name>   the job's input files (read-only)
//	out: /out/result.json      pipeline: {summary, decline?, reason?, failed?}
//	                           grader:   {tests: {id: pass|fail}, exitCode, durationSec, timedOut?, infraError?}
//	                           features: {op, fields: {...}, missing: [...], notes: [...]}
//	                           tts:      {durationSec, sampleRate, characters}, plus /out/speech.wav
//	     /out/<any other file> pipeline artifacts (a patch, files, a report)
//
// The container exits when done. The daemon enforces the time cap, memory and CPU
// limits, keeps the lease alive with heartbeats, and removes everything afterwards.

const (
	maxOutputs     = 20
	maxOutputBytes = 50 << 20
)

var errStopped = errors.New("stopped by the server")

func (d *daemon) runContainerJob(j *job) error {
	var payload map[string]any
	_ = json.Unmarshal(j.Payload, &payload)
	ref, err := ensureImage(d.cfg.docker, j.DockerImage, j.DockerDigest)
	if err != nil {
		if j.Kind == "grader" {
			// Our own grading infrastructure failed: say so, it is not the seller's fault.
			return d.api.submit(map[string]any{"job": j.GUID, "json": map[string]any{"infraError": err.Error()}})
		}
		return err
	}

	dir, err := os.MkdirTemp(d.cfg.workDir, "lendspare-job-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	taskDir := filepath.Join(dir, "task")
	outDir := filepath.Join(dir, "out")
	for _, p := range []string{filepath.Join(taskDir, "inputs"), outDir} {
		if err := os.MkdirAll(p, 0o777); err != nil {
			return err
		}
	}
	// The container may run as any user; let it write its outputs.
	_ = os.Chmod(outDir, 0o777)

	inputs := []map[string]any{}
	for _, in := range j.Inputs {
		name := filepath.Base(in.Name)
		f, err := os.Create(filepath.Join(taskDir, "inputs", name))
		if err != nil {
			return err
		}
		_, err = d.api.download(in.URL, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("input %s: %w", name, err)
		}
		inputs = append(inputs, map[string]any{"name": name, "role": in.Role, "path": "/task/inputs/" + name})
	}
	jobJSON, _ := json.MarshalIndent(map[string]any{"kind": j.Kind, "guid": j.GUID, "payload": payload, "inputs": inputs}, "", "  ")
	if err := os.WriteFile(filepath.Join(taskDir, "job.json"), jobJSON, 0o644); err != nil {
		return err
	}

	limits := containerLimits(j, payload, d.cfg.jobTimeoutSec).fitTo(dockerResources(d.cfg.docker))
	network := "none"
	env := []string{}
	if j.Kind == "pipeline" && len(j.EgressHosts) > 0 {
		net, proxy, cleanup, err := setupEgress(d.cfg.docker, short(j.GUID), j.EgressHosts)
		if err != nil {
			return fmt.Errorf("egress proxy: %w", err)
		}
		defer cleanup()
		network = net
		proxyURL := "http://" + proxy
		env = append(env, "HTTPS_PROXY="+proxyURL, "HTTP_PROXY="+proxyURL, "https_proxy="+proxyURL,
			"http_proxy="+proxyURL, "NO_PROXY=localhost,127.0.0.1")
	}

	name := "lendspare-job-" + short(j.GUID)
	args := []string{"run", "-d", "--name", name,
		"--network", network,
		"--memory", fmt.Sprintf("%dm", limits.memoryMB), "--cpus", fmt.Sprintf("%g", limits.cpus),
		"--pids-limit", "512", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"-v", taskDir + ":/task" + engine.MountOpt(d.cfg.docker, "ro"), "-v", outDir + ":/out" + engine.MountOpt(d.cfg.docker, ""),
		"-e", "MESH_KIND=" + j.Kind, "-e", "MESH_JOB=/task/job.json",
		"-e", fmt.Sprintf("MESH_TIME_LIMIT_SEC=%d", limits.timeSec)}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	// The seller's API keys: handed to docker as a file path, never read, logged or sent.
	// Only to an image the owner trusts by digest: a compromised server could otherwise route a
	// job with its own image here and read the keys back out of the result.
	if j.Kind == "pipeline" && d.cfg.pipelineEnv != "" {
		if d.files != nil && d.files.store.trustsPipelineImage(j.DockerDigest, splitList(d.cfg.pipelineTrust)) {
			args = append(args, "--env-file", d.cfg.pipelineEnv)
		} else {
			log.Printf("[lendspare] job %s: image %s is not on the trusted list - running it without your API keys", short(j.GUID), j.DockerDigest)
		}
	}
	args = append(args, ref)

	_ = proc.Command(d.cfg.docker, "rm", "-f", name).Run()
	if out, err := proc.Command(d.cfg.docker, args...).CombinedOutput(); err != nil {
		msg := fmt.Sprintf("docker run: %v %s", err, truncate(string(out), 300))
		if j.Kind == "grader" {
			return d.api.submit(map[string]any{"job": j.GUID, "json": map[string]any{"infraError": msg}})
		}
		return errors.New(msg)
	}
	defer func() { _ = proc.Command(d.cfg.docker, "rm", "-f", name).Run() }()

	started := time.Now()
	exitCode, timedOut, oom, err := d.waitContainer(name, j.GUID, time.Duration(limits.timeSec)*time.Second)
	if errors.Is(err, errStopped) {
		log.Printf("[lendspare] job %s stopped by the server", short(j.GUID))
		return nil
	}
	if err != nil {
		if j.Kind == "grader" {
			return d.api.submit(map[string]any{"job": j.GUID, "json": map[string]any{"infraError": err.Error()}})
		}
		return err
	}
	duration := time.Since(started).Seconds()

	result := map[string]any{}
	if b, err := os.ReadFile(filepath.Join(outDir, "result.json")); err == nil {
		_ = json.Unmarshal(b, &result)
	}
	metrics := map[string]any{"durationSec": duration, "exitCode": exitCode}

	if j.Kind == "grader" {
		if timedOut {
			result["timedOut"] = true
		}
		if oom {
			result["oom"] = true
		}
		if len(result) == 0 {
			result["infraError"] = fmt.Sprintf("grader wrote no report (exit %d)", exitCode)
		}
		if _, ok := result["durationSec"]; !ok {
			result["durationSec"] = duration
		}
		return d.api.submit(map[string]any{"job": j.GUID, "json": result, "metrics": metrics})
	}

	if timedOut {
		result = map[string]any{"failed": true, "reason": fmt.Sprintf("time cap of %ds exceeded", limits.timeSec)}
	} else if oom {
		result = map[string]any{"failed": true, "reason": "out of memory"}
	} else if len(result) == 0 && exitCode != 0 {
		result = map[string]any{"failed": true, "reason": fmt.Sprintf("pipeline exited with %d", exitCode)}
	}
	outputs, err := collectOutputs(outDir)
	if err != nil {
		return err
	}
	return d.api.submit(map[string]any{"job": j.GUID, "outputs": outputs, "json": result, "metrics": metrics})
}

type limits struct {
	timeSec  int
	memoryMB int
	cpus     float64
}

// containerLimits: the grader's and the features workflow's limits come in the payload; a
// pipeline gets the listing's time cap, capped by the daemon's own --job-timeout.
func containerLimits(j *job, payload map[string]any, maxSec int) limits {
	l := limits{timeSec: maxSec, memoryMB: 4096, cpus: 2}
	if j.Kind == "pipeline" && j.TimeCapSec > 0 && j.TimeCapSec < l.timeSec {
		l.timeSec = j.TimeCapSec
	}
	if j.Kind == "grader" || j.Kind == "features" || j.Kind == "tts" {
		if lim, ok := payload["limits"].(map[string]any); ok {
			if v, ok := lim["timeSec"].(float64); ok && v > 0 {
				// The command's own limit plus room to unpack and report.
				l.timeSec = int(v) + 60
			}
			if v, ok := lim["memoryMb"].(float64); ok && v > 0 {
				l.memoryMB = int(v)
			}
			if v, ok := lim["cpus"].(float64); ok && v > 0 {
				l.cpus = v
			}
		}
	}
	return l
}

// fitTo keeps a limit inside what docker has: `--cpus 4` on a 3-CPU Docker Desktop VM is
// refused outright ("range of CPUs is from 0.01 to 3.00"), and the job would fail on every
// attempt. Zero means unknown and changes nothing.
func (l limits) fitTo(ncpu int, memBytes int64) limits {
	if ncpu > 0 && l.cpus > float64(ncpu) {
		l.cpus = float64(ncpu)
	}
	if mb := int(memBytes / (1024 * 1024)); mb > 0 && l.memoryMB > mb {
		l.memoryMB = mb
	}
	return l
}

var (
	resourcesOnce sync.Once
	resourcesCPU  int
	resourcesMem  int64
)

// dockerResources asks docker once how many CPUs and how much memory it can give a container.
func dockerResources(docker string) (int, int64) {
	resourcesOnce.Do(func() {
		out, err := proc.Command(docker, "info", "--format", engine.InfoFormat(docker)).Output()
		if err != nil {
			return
		}
		parts := strings.Split(strings.TrimSpace(string(out)), "|")
		if len(parts) == 2 {
			resourcesCPU, _ = strconv.Atoi(parts[0])
			resourcesMem, _ = strconv.ParseInt(parts[1], 10, 64)
		}
	})
	return resourcesCPU, resourcesMem
}

// waitContainer waits for the container to exit, heart-beating the lease every minute.
// A server that says stop (cancelled, declined) ends it at once.
func (d *daemon) waitContainer(name, guid string, timeout time.Duration) (exitCode int, timedOut, oom bool, err error) {
	deadline := time.Now().Add(timeout)
	lastBeat := time.Now()
	for {
		out, err := proc.Command(d.cfg.docker, "inspect", "-f", "{{.State.Running}}|{{.State.ExitCode}}|{{.State.OOMKilled}}", name).Output()
		if err != nil {
			return 0, false, false, fmt.Errorf("container vanished: %w", err)
		}
		parts := strings.Split(strings.TrimSpace(string(out)), "|")
		if len(parts) == 3 && parts[0] == "false" {
			code := 0
			_, _ = fmt.Sscanf(parts[1], "%d", &code)
			return code, false, parts[2] == "true", nil
		}
		if time.Now().After(deadline) {
			_ = proc.Command(d.cfg.docker, "kill", name).Run()
			return 137, true, false, nil
		}
		if time.Since(lastBeat) > time.Minute {
			lastBeat = time.Now()
			if !d.api.heartbeat(guid) {
				_ = proc.Command(d.cfg.docker, "kill", name).Run()
				return 0, false, false, errStopped
			}
		}
		time.Sleep(time.Second)
	}
}

// collectOutputs: every file the container left in /out except result.json, base64.
func collectOutputs(outDir string) ([]map[string]any, error) {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	outputs := []map[string]any{}
	total := 0
	for _, e := range entries {
		if e.IsDir() || e.Name() == "result.json" || len(outputs) >= maxOutputs {
			continue
		}
		b, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			continue
		}
		total += len(b)
		if total > maxOutputBytes {
			break
		}
		outputs = append(outputs, map[string]any{"name": e.Name(), "contentType": contentTypeOf(e.Name()),
			"base64": base64.StdEncoding.EncodeToString(b)})
	}
	return outputs, nil
}

func contentTypeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".diff", ".patch":
		return "text/x-diff"
	case ".md":
		return "text/markdown"
	case ".txt", ".log":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".zip":
		return "application/zip"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	}
	return "application/octet-stream"
}

// ensureImage returns what to `docker run`: the image when its local id or a repo
// digest matches the pinned digest (or nothing is pinned and it is present), else the
// image pulled by digest. A local build is pinned by its image id (`sha256:…`).
func ensureImage(docker, image, digest string) (string, error) {
	if image == "" {
		return "", fmt.Errorf("job names no image")
	}
	out, err := proc.Command(docker, "image", "inspect", "-f", "{{.Id}}|{{join .RepoDigests \",\"}}", image).Output()
	present := err == nil
	if present {
		if digest == "" || strings.Contains(string(out), digest) {
			return image, nil
		}
	}
	repo := image
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	ref := image
	if digest != "" {
		ref = repo + "@" + digest
	}
	if b, err := proc.Command(docker, "pull", ref).CombinedOutput(); err != nil {
		if present {
			return "", fmt.Errorf("local %s does not match the pinned digest %s", image, digest)
		}
		return "", fmt.Errorf("pull %s: %w %s", ref, err, truncate(string(b), 200))
	}
	return ref, nil
}

// setupEgress creates an internal network for one job and an allow-list proxy on it
// that is also attached to the default bridge: the job container's only way out.
func setupEgress(docker, id string, hosts []string) (network, proxy string, cleanup func(), err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", nil, err
	}
	network = "lendspare-net-" + id
	proxyName := "lendspare-egress-" + id
	cleanup = func() {
		_ = proc.Command(docker, "rm", "-f", proxyName).Run()
		_ = proc.Command(docker, "network", "rm", network).Run()
	}
	cleanup()
	if b, err := proc.Command(docker, "network", "create", "--internal", network).CombinedOutput(); err != nil {
		return "", "", nil, fmt.Errorf("network: %w %s", err, truncate(string(b), 200))
	}
	// Default network (docker's "bridge", podman's "podman"): the one with a way out.
	args := []string{"run", "-d", "--name", proxyName, "--memory", "128m",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges"}
	proxyArgs := []string{"--egress-proxy", "--listen=:3128", "--allow=" + strings.Join(hosts, ",")}
	if img := engine.SelfImage(); img != "" {
		// Containerised daemon: the sidecar is this very image.
		args = append(args, img, "/lendspare")
	} else {
		args = append(args, "-v", exe+":/lendspare"+engine.MountOpt(docker, "ro"), "busybox:1.36", "/lendspare")
	}
	args = append(args, proxyArgs...)
	if b, err := proc.Command(docker, args...).CombinedOutput(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("proxy: %w %s", err, truncate(string(b), 200))
	}
	if b, err := proc.Command(docker, "network", "connect", network, proxyName).CombinedOutput(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("connect: %w %s", err, truncate(string(b), 200))
	}
	return network, proxyName + ":3128", cleanup, nil
}
