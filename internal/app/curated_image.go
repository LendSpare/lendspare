package app

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// --- the ComfyUI image, from a provider (docs/features/05) ------------------------
//
// "Share my GPU" hands a novice one command with no --image. The daemon asks
// GET /api/v1/mesh/stats which curated workflows the machine's owner lists and runs that
// workflow's image, pinned by digest. When the server pins a new digest the reference
// changes, the running container is stopped, and the next start runs the new image
// (docker run pulls a digest it does not have). --image still wins when it is given.

// How often the daemon asks the server which image to run.
const imageCheckEvery = 10 * time.Minute

type serverWorkflow struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Digest string `json:"digest"`
}

type statsResponse struct {
	Workflows []serverWorkflow `json:"workflows"`
}

// imageRef is "repo:tag@sha256:<hex>" when a digest is pinned, else the image as given.
func imageRef(image, digest string) string {
	image = strings.TrimSpace(image)
	digest = strings.TrimSpace(digest)
	if image == "" || digest == "" || strings.Contains(image, "@") {
		return image
	}
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	return image + "@" + digest
}

// serverImage picks the image of the first workflow that names one ("" when none does).
func serverImage(workflows []serverWorkflow) string {
	for _, w := range workflows {
		if ref := imageRef(w.Image, w.Digest); ref != "" {
			return ref
		}
	}
	return ""
}

// chooseImage decides what to run: the --image flag always wins; otherwise the server's
// image. changed is true when that differs from what the daemon runs now.
func chooseImage(flagImage, current, fromServer string) (ref string, changed bool) {
	if flagImage != "" {
		return flagImage, flagImage != current
	}
	return fromServer, fromServer != current
}

// imageFromServer is true when neither --image nor --comfy-url was given.
func (d *daemon) imageFromServer() bool {
	return d.cfg.image == "" && d.cfg.comfyURL == ""
}

// syncServerImage asks the server which image to run and switches to it when it changed.
// True when something changed (the caller reports capabilities again).
func (d *daemon) syncServerImage(api *apiClient) bool {
	d.lastImageCheck = time.Now()
	var stats statsResponse
	found, err := api.getJSON("/api/v1/mesh/stats", &stats)
	if err != nil {
		log.Printf("[lendspare] cannot ask the server for the image: %v", err)
		return false
	}
	if !found {
		return false
	}
	return d.applyServerImage(serverImage(stats.Workflows))
}

// applyServerImage switches the curated container to ref ("" = no image work).
func (d *daemon) applyServerImage(ref string) bool {
	next, changed := chooseImage("", d.serverImageRef, ref)
	if !changed {
		return false
	}
	if d.comfyContainerRunning() { // also a leftover from an earlier run
		log.Printf("[lendspare] the server pins a new image, restarting ComfyUI")
		d.stopComfyContainer()
	}
	d.serverImageRef = next
	d.comfyCaps = nil
	if next == "" {
		d.comfyURL = ""
		d.comfyOwned = false
		log.Printf("[lendspare] no image work listed for this machine")
		return true
	}
	d.comfyURL = fmt.Sprintf("http://127.0.0.1:%d", d.cfg.comfyPort)
	d.comfyOwned = true
	log.Printf("[lendspare] image work: %s", next)
	return true
}

// comfyImage is what the curated container runs: --image, else the server's pick.
func (d *daemon) comfyImage() string {
	if d.cfg.image != "" {
		return d.cfg.image
	}
	return d.serverImageRef
}
