// Package app is the LendSpare program itself: the compute daemon, the connections to providers,
// shared folders, the settings page and the tray. cmd/lendspare calls Main.
package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/LendSpare/lendspare/internal/egress"
	"github.com/LendSpare/lendspare/internal/engine"
)

// What the settings page shows in its header: the running daemon's state, once there is one.
var pageDaemon atomic.Pointer[daemon]

const (
	textReady        = "LendSpare is ready"
	textReadyHint    = "Connect from the website where you use LendSpare."
	daemonVersion    = "0.0.3"
	envDeprecatedFmt = "%s is no longer used: LendSpare has no default server and takes no token from the environment. Connect from the website where you use LendSpare, or run `lendspare connect`."
)

func pageStatus() map[string]any {
	out := map[string]any{"status": textReady, "hint": textReadyHint}
	if d := pageDaemon.Load(); d != nil {
		_, text, jobs := d.st.snapshot()
		out["status"], out["jobs"], out["paused"] = text, jobs, d.st.isPaused()
	}
	return out
}

type config struct {
	image         string // curated ComfyUI image (enables the comfy kind)
	comfyURL      string // the owner's own ComfyUI (enables comfy without a container)
	containerName string
	comfyPort     int
	pollSec       int
	idleGraceSec  int
	minFreeMB     int
	foreignBusyMB int
	jobTimeoutSec int
	gpuGate       bool
	llmURL        string // "" = detect Ollama (:11434) then LM Studio (:1234); "off" = none
	llmUsesGPU    bool
	kinds         string // optional allow-list of kinds to offer
	pipelineEnv   string // local env file with the seller's API keys (pipelines only)
	pipelineTrust string // image digests that may receive pipelineEnv (also on the settings page)
	comfyNetwork  string // "isolated" (default: no internet, no LAN) or "bridge"
	settings      bool   // open the settings page (of the running instance, if one runs)
	workDir       string
	docker        string
	devOrigins    []string // --dev-origin, repeatable

	// egress proxy mode (run inside a sidecar container by the program itself)
	proxyMode   bool
	proxyListen string
	proxyAllow  string

	// an own relay for shared folders: `--relay`
	relay     bool
	relayAddr string
	relayDir  string
	relayCert string
	relayKey  string

	tray   bool // run as a tray icon (the default on a Windows double-click)
	noTray bool
}

// envFirst reads the first variable that is set: LENDSPARE_* wins over the deprecated MESH_*.
func envFirst(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// startupLink is a lendspare:// link this process was started with and no running instance could
// take (LendSpare was not running yet).
var startupLink string

type devFlag struct{ list *[]string }

func (d devFlag) String() string { return "" }
func (d devFlag) Set(v string) error {
	*d.list = append(*d.list, v)
	return nil
}

// Main runs LendSpare with os.Args; it returns only for one-shot commands and exits otherwise.
func Main() {
	args := os.Args[1:]
	if len(args) > 0 && strings.HasPrefix(args[0], "lendspare://") {
		if handOffLink(args[0]) {
			return
		}
		startupLink = args[0]
		os.Args = os.Args[:1] // continue as a normal start; the link is confirmed once up
	} else if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		os.Exit(runSubcommand(args[0], args[1:]))
	}
	for _, v := range []string{"MESH_SERVER", "MESH_TOKEN"} {
		if os.Getenv(v) != "" {
			fmt.Fprintf(os.Stderr, envDeprecatedFmt+"\n", v)
		}
	}

	cfg := config{}
	var showVersion bool
	flag.StringVar(&cfg.image, "image", envFirst("LENDSPARE_IMAGE", "MESH_IMAGE"), "ComfyUI image to run (default: the one a connected provider names)")
	flag.StringVar(&cfg.comfyURL, "comfy-url", "", "drive your own ComfyUI at this URL instead of the curated container")
	flag.StringVar(&cfg.containerName, "container", "lendspare-comfy", "container name for the curated ComfyUI")
	flag.IntVar(&cfg.comfyPort, "comfy-port", 8188, "host port mapped to the curated ComfyUI")
	flag.IntVar(&cfg.pollSec, "poll", 3, "seconds between GPU/claim polls")
	flag.IntVar(&cfg.idleGraceSec, "idle-grace", 60, "seconds the GPU must stay idle before borrowing it")
	flag.IntVar(&cfg.minFreeMB, "min-free-mb", 6000, "minimum free VRAM (MB) required to borrow the GPU")
	flag.IntVar(&cfg.foreignBusyMB, "foreign-busy-mb", 800, "foreign VRAM (MB) that counts as 'owner busy'")
	flag.IntVar(&cfg.jobTimeoutSec, "job-timeout", 600, "max seconds for one job (a pipeline's own cap may be lower)")
	flag.BoolVar(&cfg.gpuGate, "gpu-gate", true, "step aside while another process uses the GPU")
	flag.StringVar(&cfg.llmURL, "llm-url", "", "local OpenAI-compatible server (default: detect Ollama, then LM Studio; 'off' to disable)")
	flag.BoolVar(&cfg.llmUsesGPU, "llm-gpu", true, "the local model server uses the GPU (so chat jobs follow GPU gating)")
	flag.StringVar(&cfg.kinds, "kinds", "", "only offer these kinds (comma-separated: comfy,llm,pipeline,grader,features,tts)")
	flag.StringVar(&cfg.pipelineEnv, "pipeline-env", "", "env file with your API keys for pipelines; stays on this machine")
	flag.StringVar(&cfg.pipelineTrust, "pipeline-trust", "", "comma-separated image digests (sha256:...) that may receive --pipeline-env")
	flag.StringVar(&cfg.comfyNetwork, "comfy-network", "isolated", "isolated: the curated ComfyUI reaches neither the internet nor your LAN; bridge: the container engine's default network")
	flag.BoolVar(&cfg.settings, "settings", false, "open the settings page (shared folders, connections, activity)")
	flag.StringVar(&cfg.workDir, "work-dir", "", "per-job directories (default: system temp)")
	flag.StringVar(&cfg.docker, "docker", envFirst("LENDSPARE_ENGINE", "MESH_ENGINE"), "container CLI: auto (docker, then podman), docker, podman, or a path")
	flag.BoolVar(&cfg.proxyMode, "egress-proxy", false, "internal: run as the allow-list egress proxy")
	flag.StringVar(&cfg.proxyListen, "listen", ":3128", "internal: egress proxy listen address")
	flag.StringVar(&cfg.proxyAllow, "allow", "", "internal: comma-separated hosts the egress proxy lets through")
	flag.BoolVar(&cfg.relay, "relay", false, "run only as a relay for shared folders (sites and computers that cannot reach each other)")
	flag.StringVar(&cfg.relayAddr, "relay-listen", ":8443", "relay: listen address")
	flag.StringVar(&cfg.relayDir, "relay-dir", filepath.Join(os.TempDir(), "lendspare-relay"), "relay: where chunks wait (a few MiB per transfer)")
	flag.StringVar(&cfg.relayCert, "relay-cert", "", "relay: TLS certificate file (or put it behind a TLS proxy)")
	flag.StringVar(&cfg.relayKey, "relay-key", "", "relay: TLS key file")
	flag.BoolVar(&cfg.tray, "tray", false, "run as a tray icon")
	flag.BoolVar(&cfg.noTray, "no-tray", false, "run in the console even on a double-click")
	flag.BoolVar(&showVersion, "version", false, "print the version and exit")
	flag.Var(devFlag{&cfg.devOrigins}, "dev-origin", "development only: also allow this provider origin, e.g. http://localhost:8080 (repeatable)")
	for _, gone := range []string{"server", "token", "pair", "forget"} {
		name := gone
		flag.Var(removedFlag(name), name, "removed")
	}
	flag.Parse()
	if cfg.docker == "" {
		cfg.docker = "auto"
	}

	if showVersion {
		fmt.Println("LendSpare", daemonVersion)
		return
	}
	if cfg.relay {
		log.Fatal(runRelay(cfg.relayAddr, cfg.relayDir, cfg.relayCert, cfg.relayKey))
	}
	if cfg.proxyMode {
		allow := splitList(cfg.proxyAllow)
		log.Printf("[egress] listening on %s, allowing %v", cfg.proxyListen, allow)
		log.Fatal(http.ListenAndServe(cfg.proxyListen, egress.NewProxy(allow)))
	}
	if cfg.workDir == "" {
		cfg.workDir = os.TempDir()
	}

	if cfg.settings && openRunningSettings() {
		return // the running instance's page is open
	}
	a, err := openApp(cfg)
	if err != nil {
		log.Fatalf("lendspare: %v", err)
	}
	// Shared folders and the settings page work from the first start, with no account.
	settingsURL, err := startSettings(a, pageStatus)
	if err != nil {
		log.Printf("[settings] %v", err)
	}
	if cfg.settings && settingsURL != "" {
		_ = openLocalPage(settingsURL)
	}

	if wantTray(&cfg) {
		runTray(cfg)
		return
	}
	attachConsole()
	cfg.docker = engine.Resolve(cfg.docker)
	log.Printf("[lendspare] container engine: %s", cfg.docker)
	d := newDaemon(cfg, a.mgr)
	d.openStartupLink()
	d.run()
}

// removedFlag is a flag of the previous program that no longer exists; using it explains why.
type removedFlag string

func (r removedFlag) String() string { return "" }
func (r removedFlag) Set(string) error {
	switch r {
	case "forget":
		fmt.Fprintln(os.Stderr, "--forget is gone: use `lendspare connections` and `lendspare disconnect <id>`.")
	default:
		fmt.Fprintf(os.Stderr, "--%s is gone: LendSpare has no default server and no pairing on start. Connect from the website where you use LendSpare, or run `lendspare connect`.\n", string(r))
	}
	os.Exit(2)
	return nil
}
func (r removedFlag) IsBoolFlag() bool { return r == "pair" || r == "forget" }

type daemon struct {
	cfg        config
	mgr        *manager
	http       *http.Client
	api        *apiClient // the client of the connection whose work the compute loop runs now (see step)
	llm        *llmServer
	comfyURL   string
	comfyOwned bool // we start/stop the curated container
	lastLLM    time.Time

	comfyCaps      *comfyCaps // last known node classes + model files (kept while the container is down)
	serverImageRef string     // the image a connected provider says its listings need (no --image)
	lastImageCheck time.Time
	lastConvert    time.Time
	triedConvert   map[int]bool

	st       *status // what the tray shows; never nil
	sched    scheduler
	lastBusy time.Time

	// test hook: replaces the GPU and engine probing with a fixed list of kinds, and the job runner.
	kindsHook func() []string
	runHook   func(c connection, j *job)

	// Shared folders: the service, the poller's wake-up and the LAN listener.
	files     *filesService
	filesWake chan struct{}
	direct    *directServer
	reportMu  sync.Mutex
	reports   map[string]time.Time // connection id -> last capability report
	filesSig  string
	lastFiles time.Time
}

func newDaemon(cfg config, mgr *manager) *daemon {
	d := &daemon{cfg: cfg, mgr: mgr, http: &http.Client{Timeout: 60 * time.Second, Transport: roundTripper},
		triedConvert: map[int]bool{}, st: newStatus(), filesWake: make(chan struct{}, 1), reports: map[string]time.Time{},
		lastBusy: time.Now()}
	mgr.onHint = func() {
		select {
		case d.filesWake <- struct{}{}:
		default:
		}
	}
	d.files, _ = sharedFiles()
	d.direct = &directServer{d: d}
	switch {
	case cfg.comfyURL != "":
		d.comfyURL = strings.TrimRight(cfg.comfyURL, "/")
	case cfg.image != "":
		d.comfyURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.comfyPort)
		d.comfyOwned = true
	}
	return d
}

func (d *daemon) run() {
	log.Printf("[lendspare] up (v%s). poll=%ds gpu-gate=%v", daemonVersion, d.cfg.pollSec, d.cfg.gpuGate)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		d.shutdown()
	}()

	if d.comfyOwned {
		d.stopComfyContainer() // a leftover container holds VRAM
	}
	d.refreshLLM()
	d.mgr.onChange = func() { d.reportAll() }
	if d.files != nil {
		pageDaemon.Store(d)
		d.files.store.onChange = func() {
			d.direct.sync(d.files.store.snapshot().Direct)
			d.reportAll()
		}
		d.direct.sync(d.files.store.snapshot().Direct)
		go d.runFiles()
	}
	d.reportAll()
	for {
		d.step()
	}
}

// step is one pass of the compute loop: sweep housekeeping, then - if some connection may run
// work now - claim at most one job, from the connection whose turn it is.
func (d *daemon) step() {
	conns := d.mgr.computeConns()
	if time.Since(d.lastLLM) > 5*time.Minute && len(conns) > 0 {
		if d.refreshLLM() {
			d.reportAll()
		}
	}
	d.hourlyReports()
	if d.files != nil && time.Since(d.lastFiles) > time.Minute {
		d.lastFiles = time.Now()
		if sig := d.filesSignature(); sig != d.filesSig {
			d.reportAll()
		}
	}
	if d.st.isPaused() {
		if d.comfyOwned && d.comfyContainerRunning() {
			d.stopComfyContainer() // a paused machine does not hold the owner's VRAM
		}
		d.st.set(levelPaused, "Paused")
		d.sleep()
		return
	}
	if len(conns) == 0 {
		// Nothing to run for anybody: no GPU probing, no container, no request.
		if d.comfyOwned && d.comfyContainerRunning() {
			d.stopComfyContainer()
		}
		if len(d.mgr.list()) == 0 {
			d.st.set(levelIdle, textReady)
		} else {
			d.st.set(levelIdle, "Connected - computing is off")
		}
		d.sleep()
		return
	}
	if api, err := d.mgr.apiFor(conns[0].ID); err == nil {
		d.api = api // housekeeping below (workflow conversion) speaks to the first connection
	}
	if first := conns[0]; d.imageFromServer() && time.Since(d.lastImageCheck) > imageCheckEvery {
		if api, err := d.mgr.apiFor(first.ID); err == nil && d.syncServerImage(api) {
			d.reportAll()
		}
	}

	busy := false
	free := -1
	var kinds []string
	if d.kindsHook != nil {
		kinds = d.kindsHook()
	} else {
		if d.cfg.gpuGate {
			var foreign int
			foreign, free = engine.GPUState(d.cfg.docker, d.cfg.containerName)
			busy = foreign >= d.cfg.foreignBusyMB
		}
		if busy {
			d.lastBusy = time.Now()
		}
		kinds = d.availableKinds(busy, free)
	}
	if len(kinds) == 0 {
		switch {
		case busy:
			d.st.set(levelBusy, "You are using the GPU - stepping aside")
		case !engine.Available(d.cfg.docker) && d.llm == nil && d.comfyURL == "":
			d.st.set(levelError, "No Docker or Podman found - install one to share this GPU")
		default:
			d.st.set(levelIdle, "Getting ready")
		}
		d.sleep()
		return
	}

	ids := make([]string, len(conns))
	byID := map[string]connection{}
	for i, c := range conns {
		ids[i] = c.ID
		byID[c.ID] = c
	}
	for _, id := range d.sched.order(ids) {
		c := byID[id]
		api, err := d.mgr.apiFor(id)
		if err != nil {
			continue
		}
		d.api = api
		j, err := api.claim(kinds)
		if err != nil {
			log.Printf("[lendspare] claim error from %s: %v", hostOf(c.Origin), err)
			if isAuthError(err) {
				d.mgr.markDisconnected(id, "the provider refused this connection")
			}
			continue
		}
		if j == nil {
			continue
		}
		d.sched.served(id)
		d.st.set(levelWorking, "Working on a job")
		if d.runHook != nil {
			d.runHook(c, j)
		} else {
			d.process(j)
		}
		d.st.jobDone()
		return
	}
	d.st.set(levelIdle, "Ready - waiting for jobs")
	d.sleep()
}

// availableKinds is what this machine can start right now (GPU gate, containers, local models).
func (d *daemon) availableKinds(busy bool, free int) []string {
	kinds := []string{}
	if d.comfyURL != "" && kindAllowed(d.cfg.kinds, "comfy") {
		if d.comfyOwned {
			running := d.comfyContainerRunning()
			if busy && running {
				log.Printf("[lendspare] owner is using the GPU, releasing it")
				d.stopComfyContainer()
				running = false
			}
			idleFor := time.Since(d.lastBusy)
			if !busy && !running && idleFor >= time.Duration(d.cfg.idleGraceSec)*time.Second && (free < 0 || free >= d.cfg.minFreeMB) {
				d.startComfyContainer()
			}
		}
		if !busy && d.comfyReady() {
			kinds = append(kinds, "comfy")
			// First sight of this ComfyUI: tell the providers which nodes and models it has.
			if d.comfyCaps == nil {
				d.refreshComfyCaps()
				d.reportAll()
			}
			if time.Since(d.lastConvert) > time.Minute {
				d.convertPending()
			}
		}
	}
	if d.llm != nil && (!busy || !d.cfg.llmUsesGPU) {
		kinds = append(kinds, "llm")
	}
	if engine.Available(d.cfg.docker) {
		kinds = append(kinds, "pipeline", "grader", "features", "tts")
	}
	return filterKinds(kinds, d.cfg.kinds)
}

func (d *daemon) hourlyReports() {
	d.reportMu.Lock()
	var stale bool
	for _, c := range d.mgr.list() {
		if d.mgr.usable(c) && time.Since(d.reports[c.ID]) > time.Hour {
			stale = true
		}
	}
	d.reportMu.Unlock()
	if stale {
		d.reportAll()
	}
}

// shutdown frees the GPU and ends the process.
func (d *daemon) shutdown() {
	log.Printf("[lendspare] shutting down, freeing the GPU")
	if d.comfyOwned {
		d.stopComfyContainer()
	}
	os.Exit(0)
}

func (d *daemon) process(j *job) {
	log.Printf("[lendspare] job %s (%s, %s)", short(j.GUID), j.Kind, j.Workflow)
	var err error
	switch j.Kind {
	case "llm":
		err = d.runLLMJob(j)
	case "pipeline", "grader", "features", "tts":
		err = d.runContainerJob(j)
	default:
		err = d.runComfyJob(j)
	}
	if err != nil {
		log.Printf("[lendspare] job %s failed: %v — reporting", short(j.GUID), err)
		_ = d.api.submit(map[string]any{"job": j.GUID, "error": truncate(err.Error(), 500)})
		return
	}
	log.Printf("[lendspare] job %s done", short(j.GUID))
}

func (d *daemon) possibleKinds() []string {
	kinds := []string{}
	if d.comfyURL != "" {
		kinds = append(kinds, "comfy")
	}
	if d.llm != nil && len(d.llm.models) > 0 {
		kinds = append(kinds, "llm")
	}
	if engine.Available(d.cfg.docker) {
		// The server leases grader and features jobs only to rigs the platform trusts.
		kinds = append(kinds, "pipeline", "grader", "features", "tts")
	}
	return kinds
}

// --- helpers ---------------------------------------------------------------

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// kindAllowed: true when --kinds was not given, or it lists kind - gates whether the daemon
// even attempts a kind of work (e.g. starting the ComfyUI container), not just whether it is
// later offered. Without this, --kinds=llm on a machine that cannot run comfy (no GPU
// passthrough, as with Docker Desktop on macOS) still retries "docker run" every poll.
func kindAllowed(allow, kind string) bool {
	return len(filterKinds([]string{kind}, allow)) > 0
}

func filterKinds(kinds []string, allow string) []string {
	if strings.TrimSpace(allow) == "" {
		return kinds
	}
	allowed := map[string]bool{}
	for _, k := range splitList(allow) {
		allowed[k] = true
	}
	out := []string{}
	for _, k := range kinds {
		if allowed[k] {
			out = append(out, k)
		}
	}
	return out
}

func short(s string) string { return truncate(s, 8) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (d *daemon) filesSignature() string {
	var sigs []any
	for _, c := range d.mgr.filesConns() {
		sigs = append(sigs, c.ID, d.files.capabilities(d.direct.addresses(), c.ID)["shares"])
	}
	b, _ := json.Marshal(sigs)
	return string(b)
}

// capsFor is what one connection may be told: compute capabilities only with the scope AND the
// local switch, folder names only for folders granted to it. ok is false when there is
// nothing at all it may be told.
func (d *daemon) capsFor(c connection) (caps map[string]any, ok bool) {
	caps = map[string]any{"daemonVersion": daemonVersion}
	if c.has(scopeCompute) && c.Compute {
		ok = true
		caps["kinds"] = filterKinds(d.possibleKinds(), d.cfg.kinds)
		caps["containers"] = engine.Available(d.cfg.docker)
		caps["egressProxy"] = engine.EgressProxyAvailable(d.cfg.docker)
		caps["gpu"] = engine.GPUName()
		if d.llm != nil {
			caps["llm"] = d.llm.models
		}
		if d.comfyURL != "" && d.comfyReady() {
			d.refreshComfyCaps()
		}
		if d.comfyCaps != nil {
			caps["comfy"] = d.comfyCaps
		}
	}
	if c.has(scopeFiles) && d.files != nil && d.files.sharing(c.ID) {
		ok = true
		caps["files"] = d.files.capabilities(d.direct.addresses(), c.ID)
	}
	return caps, ok
}

// reportAll tells each connection what it may know (POST /capabilities). A connection that is
// paused, disconnected or has nothing it may be told hears nothing.
func (d *daemon) reportAll() {
	d.reportMu.Lock()
	defer d.reportMu.Unlock()
	for _, c := range d.mgr.list() {
		if !d.mgr.usable(c) || c.Paused {
			continue
		}
		caps, ok := d.capsFor(c)
		if !ok {
			continue
		}
		api, err := d.mgr.apiFor(c.ID)
		if err != nil {
			continue
		}
		var answer struct {
			Worker struct {
				ID int `json:"id"`
			} `json:"worker"`
		}
		if err := api.postJSON("/api/v1/mesh/capabilities", caps, &answer); err != nil {
			log.Printf("[lendspare] capability report to %s failed: %v", hostOf(c.Origin), err)
			if isAuthError(err) {
				d.mgr.markDisconnected(c.ID, "the provider refused this connection")
			}
			continue
		}
		d.mgr.setWorkerID(c.ID, answer.Worker.ID)
		d.reports[c.ID] = time.Now()
		log.Printf("[lendspare] reported to %s: kinds=%v", hostOf(c.Origin), caps["kinds"])
	}
	if d.files != nil {
		d.filesSig = d.filesSignature()
	}
}

// refreshComfyCaps asks the running ComfyUI what it has; the last answer is kept while it is down.
func (d *daemon) refreshComfyCaps() {
	caps, err := comfyCapabilities(d.http, d.comfyURL)
	if err != nil {
		log.Printf("[lendspare] cannot read ComfyUI capabilities: %v", err)
		return
	}
	d.comfyCaps = caps
}

// refreshLLM re-detects the local model server; true when what it serves changed.
func (d *daemon) refreshLLM() bool {
	d.lastLLM = time.Now()
	before := modelKey(d.llm)
	d.llm = detectLLM(d.http, d.cfg.llmURL)
	return modelKey(d.llm) != before
}

func (d *daemon) sleep() { time.Sleep(time.Duration(d.cfg.pollSec) * time.Second) }

// isAuthError: the provider no longer accepts this connection's token (revoked).
func isAuthError(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status == 401 || ae.status == 403
	}
	m := err.Error()
	return strings.Contains(m, "pending 401") || strings.Contains(m, "pending 403")
}
