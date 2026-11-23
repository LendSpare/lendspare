package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LendSpare/lendspare/internal/engine"
	"github.com/LendSpare/lendspare/internal/proc"
)

// --- the curated ComfyUI container (started only while the GPU is idle) ----------

func (d *daemon) comfyContainerRunning() bool {
	out, err := proc.Command(d.cfg.docker, "inspect", "-f", "{{.State.Running}}", d.cfg.containerName).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// The forwarder that is the curated ComfyUI's only door: socat, pinned by digest, listening on
// a published loopback port and reaching ComfyUI over an --internal network that has no route
// out. So ComfyUI - and any workflow graph or custom node it runs, even from a compromised
// server - can reach neither the internet nor the owner's LAN (docs/features/29, threat model).
const comfyForwarderImage = "alpine/socat:1.8.0.3@sha256:beb4a68d9e4fe6b0f21ea774a0fde6c31f580dde6368939ed70100c5385b015e"

func (d *daemon) comfyNetworks() (internal, public, forwarder string) {
	return d.cfg.containerName + "-net", d.cfg.containerName + "-door", d.cfg.containerName + "-fwd"
}

func (d *daemon) startComfyContainer() {
	image := d.comfyImage()
	log.Printf("[lendspare] GPU idle — starting %s", image)
	d.stopComfyContainer()
	// Hardened run: GPU access, auto-removed, memory cap, no host mounts, no Linux
	// capabilities beyond the default-dropped set, no privilege escalation, a process-count
	// cap (the same hardening pipeline/grader containers get in container.go), and - unless
	// the owner chose --comfy-network=bridge - no network but the internal one.
	args := []string{
		"run", "-d", "--name", d.cfg.containerName, "--rm",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "512",
	}
	args = append(args, engine.GPUArgs(d.cfg.docker)...)
	args = append(args, "--memory", "16g")
	isolated := d.cfg.comfyNetwork != "bridge"
	if isolated {
		internal, public, _ := d.comfyNetworks()
		for _, n := range [][]string{{"--internal", internal}, {public}} {
			if err := proc.Command(d.cfg.docker, append([]string{"network", "create"}, n...)...).Run(); err != nil &&
				proc.Command(d.cfg.docker, "network", "inspect", n[len(n)-1]).Run() != nil {
				log.Printf("[lendspare] cannot create network %s: %v - not starting ComfyUI", n[len(n)-1], err)
				return
			}
		}
		args = append(args, "--network", internal, image)
	} else {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:8188", d.cfg.comfyPort), image)
	}
	if out, err := proc.Command(d.cfg.docker, args...).CombinedOutput(); err != nil {
		// docker's own words ("Conflict. The container name ... is already in use", "could not
		// select device driver"); the exit status alone says nothing.
		log.Printf("[lendspare] docker run failed: %v: %s", err, lastLine(out))
		return
	}
	if isolated {
		if err := d.startComfyForwarder(); err != nil {
			log.Printf("[lendspare] the ComfyUI forwarder did not start (%v) - stopping ComfyUI rather than exposing it", err)
			d.stopComfyContainer()
		}
	}
}

// startComfyForwarder: created on the publishing network, joined to the internal one, then
// started - so it has Docker's resolver for the ComfyUI container's name from its first second.
func (d *daemon) startComfyForwarder() error {
	internal, public, fwd := d.comfyNetworks()
	create := []string{"create", "--name", fwd, "--rm", "--network", public,
		"-p", fmt.Sprintf("127.0.0.1:%d:8188", d.cfg.comfyPort),
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--memory", "64m", "--pids-limit", "64",
		comfyForwarderImage, "TCP-LISTEN:8188,fork,reuseaddr", "TCP:" + d.cfg.containerName + ":8188"}
	if out, err := proc.Command(d.cfg.docker, create...).CombinedOutput(); err != nil {
		return fmt.Errorf("%w %s", err, truncate(string(out), 200))
	}
	if out, err := proc.Command(d.cfg.docker, "network", "connect", internal, fwd).CombinedOutput(); err != nil {
		return fmt.Errorf("%w %s", err, truncate(string(out), 200))
	}
	if out, err := proc.Command(d.cfg.docker, "start", fwd).CombinedOutput(); err != nil {
		return fmt.Errorf("%w %s", err, truncate(string(out), 200))
	}
	return nil
}

func (d *daemon) stopComfyContainer() {
	_, _, fwd := d.comfyNetworks()
	_ = proc.Command(d.cfg.docker, "rm", "-f", fwd).Run()
	_ = proc.Command(d.cfg.docker, "rm", "-f", d.cfg.containerName).Run()
}

func (d *daemon) comfyReady() bool {
	resp, err := d.http.Get(d.comfyURL + "/system_stats")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// --- a comfy job -------------------------------------------------------------

type comfyPayload struct {
	Prompt string          `json:"prompt"`
	Seed   *int            `json:"seed"`
	Batch  int             `json:"batch"`
	Params json.RawMessage `json:"params"` // an imported workflow: the server already wrote the values in
}

// typed: the graph arrives filled in by parameter; only input images remain to place.
func (p comfyPayload) typed() bool { return len(p.Params) > 0 && string(p.Params) != "null" }

func (d *daemon) runComfyJob(j *job) error {
	var p comfyPayload
	_ = json.Unmarshal(j.Payload, &p)
	var names map[string]string
	if len(j.Inputs) > 0 {
		var err error
		if names, err = d.uploadInputs(j.Inputs); err != nil {
			return err
		}
	}
	images, err := d.runComfy(j.WorkflowJSON, p, names)
	if err != nil {
		return err
	}
	b64 := make([]string, 0, len(images))
	for _, img := range images {
		b64 = append(b64, base64.StdEncoding.EncodeToString(img))
	}
	return d.api.submit(map[string]any{"job": j.GUID, "images": b64})
}

// runComfy fills the curated template's prompt/seed (an imported graph arrives filled in),
// places uploaded input images, submits it to ComfyUI, waits for completion, and returns
// the rendered PNGs.
func (d *daemon) runComfy(graph string, p comfyPayload, inputs map[string]string) ([][]byte, error) {
	if graph == "" || graph == "null" {
		return nil, fmt.Errorf("job has no workflow graph")
	}
	if !p.typed() {
		graph = strings.ReplaceAll(graph, "{{prompt}}", jsonEscape(p.Prompt))
		seed := int(time.Now().UnixNano() & 0x7fffffff)
		if p.Seed != nil {
			seed = *p.Seed
		}
		graph = strings.ReplaceAll(graph, "{{seed}}", strconv.Itoa(seed))
	}

	var prompt map[string]any
	if err := decodeNumbers([]byte(graph), &prompt); err != nil {
		return nil, fmt.Errorf("bad workflow graph after substitution: %w", err)
	}
	fillPlaceholders(prompt, inputs)
	body, _ := json.Marshal(map[string]any{"prompt": prompt, "client_id": "lendspare"})
	resp, err := d.http.Post(d.comfyURL+"/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var pr struct {
		PromptID string `json:"prompt_id"`
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfy /prompt %d: %s", resp.StatusCode, truncate(string(b), 300))
	}
	if err := json.Unmarshal(b, &pr); err != nil || pr.PromptID == "" {
		return nil, fmt.Errorf("comfy /prompt: no prompt_id (%s)", truncate(string(b), 200))
	}
	deadline := time.Now().Add(time.Duration(d.cfg.jobTimeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		imgs, done, err := d.pollHistory(pr.PromptID)
		if err != nil {
			return nil, err
		}
		if done {
			return imgs, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("comfy render timed out after %ds", d.cfg.jobTimeoutSec)
}

func (d *daemon) pollHistory(promptID string) ([][]byte, bool, error) {
	resp, err := d.http.Get(d.comfyURL + "/history/" + promptID)
	if err != nil {
		return nil, false, err
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var hist map[string]struct {
		Outputs map[string]struct {
			Images []struct {
				Filename  string `json:"filename"`
				Subfolder string `json:"subfolder"`
				Type      string `json:"type"`
			} `json:"images"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(b, &hist); err != nil {
		return nil, false, nil // not ready yet / transient
	}
	entry, ok := hist[promptID]
	if !ok {
		return nil, false, nil
	}
	var images [][]byte
	for _, node := range entry.Outputs {
		for _, img := range node.Images {
			if img.Type != "output" {
				continue
			}
			data, err := d.fetchImage(img.Filename, img.Subfolder, img.Type)
			if err == nil && len(data) > 0 {
				images = append(images, data)
			}
		}
	}
	return images, true, nil
}

func (d *daemon) fetchImage(filename, subfolder, typ string) ([]byte, error) {
	u := fmt.Sprintf("%s/view?filename=%s&subfolder=%s&type=%s",
		d.comfyURL, urlEscape(filename), urlEscape(subfolder), urlEscape(typ))
	resp, err := d.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("view %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1]) // strip the surrounding quotes
}

func urlEscape(s string) string {
	r := strings.NewReplacer(" ", "%20", "&", "%26", "?", "%3F", "#", "%23", "+", "%2B")
	return r.Replace(s)
}

// lastLine is the last non-empty line of a command's output, cut to 300 bytes: docker prints
// its error last, after any progress lines of a pull.
func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(out), "\r", "")), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if len(l) > 300 {
		l = l[:300]
	}
	return l
}
