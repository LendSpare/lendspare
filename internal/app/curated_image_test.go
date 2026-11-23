package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// docs/features/05: the one-button command carries no --image; the daemon takes the image
// from the server, keeps --image as an override, and switches when the digest changes.

func TestImageRefPinsTheDigest(t *testing.T) {
	cases := []struct{ image, digest, want string }{
		{"registry.example/comfy:v1", "sha256:abc", "registry.example/comfy:v1@sha256:abc"},
		{"registry.example/comfy:v1", "abc", "registry.example/comfy:v1@sha256:abc"},
		{"registry.example/comfy:v1", "", "registry.example/comfy:v1"},
		{"registry.example/comfy@sha256:old", "sha256:new", "registry.example/comfy@sha256:old"},
		{"", "sha256:abc", ""},
	}
	for _, c := range cases {
		if got := imageRef(c.image, c.digest); got != c.want {
			t.Errorf("imageRef(%q, %q) = %q, want %q", c.image, c.digest, got, c.want)
		}
	}
}

func TestServerImageTakesTheFirstWorkflowWithAnImage(t *testing.T) {
	got := serverImage([]serverWorkflow{{Name: "none"}, {Name: "flux", Image: "img/flux:v1", Digest: "sha256:d1"}, {Name: "b", Image: "img/b"}})
	if got != "img/flux:v1@sha256:d1" {
		t.Fatalf("serverImage = %q", got)
	}
	if serverImage(nil) != "" {
		t.Fatal("no workflows must mean no image")
	}
}

func TestChooseImage(t *testing.T) {
	// The flag always wins.
	if ref, _ := chooseImage("mine:1", "", "server:2"); ref != "mine:1" {
		t.Fatalf("flag override lost: %q", ref)
	}
	// Server-provided, first time: a change.
	if ref, changed := chooseImage("", "", "server@sha256:a"); ref != "server@sha256:a" || !changed {
		t.Fatalf("server image not taken: %q %v", ref, changed)
	}
	// Same digest again: nothing to do.
	if _, changed := chooseImage("", "server@sha256:a", "server@sha256:a"); changed {
		t.Fatal("an unchanged digest must not restart ComfyUI")
	}
	// New digest: a change.
	if ref, changed := chooseImage("", "server@sha256:a", "server@sha256:b"); ref != "server@sha256:b" || !changed {
		t.Fatalf("digest change missed: %q %v", ref, changed)
	}
}

func TestSyncServerImageFollowsTheDigest(t *testing.T) {
	digest := "sha256:first"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mesh/stats" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, `{"queued":0,"workflows":[{"name":"comfy-flux1-schnell","image":"img/flux:v1","digest":"%s"}]}`, digest)
	}))
	defer srv.Close()

	m := newTestManager(t, srv.URL)
	c := connect(t, m, srv.URL, "tok", scopeCompute)
	api, _ := m.apiFor(c.ID)
	d := newDaemon(config{comfyPort: 8188, docker: "no-such-docker-binary"}, m)
	if !d.imageFromServer() {
		t.Fatal("no --image and no --comfy-url: the image must come from the server")
	}
	if d.comfyURL != "" {
		t.Fatal("before asking the server the daemon offers no image work")
	}
	if !d.syncServerImage(api) || d.comfyImage() != "img/flux:v1@sha256:first" || !d.comfyOwned || d.comfyURL == "" {
		t.Fatalf("first sync: image=%q owned=%v url=%q", d.comfyImage(), d.comfyOwned, d.comfyURL)
	}
	if d.syncServerImage(api) {
		t.Fatal("same digest: no change expected")
	}
	digest = "sha256:second"
	if !d.syncServerImage(api) || d.comfyImage() != "img/flux:v1@sha256:second" {
		t.Fatalf("new digest not followed: %q", d.comfyImage())
	}
	if time.Since(d.lastImageCheck) > time.Minute {
		t.Fatal("the check time must be recorded")
	}
}

func TestImageFlagOverridesTheServer(t *testing.T) {
	d := newDaemon(config{image: "mine/comfy:9", comfyPort: 8188, docker: "no-such-docker-binary"}, newTestManager(t))
	if d.imageFromServer() {
		t.Fatal("--image given: the server must not be asked")
	}
	if d.comfyImage() != "mine/comfy:9" || !d.comfyOwned {
		t.Fatalf("flag image not used: %q", d.comfyImage())
	}
}

func TestNoListingMeansNoImageWork(t *testing.T) {
	d := newDaemon(config{comfyPort: 8188, docker: "no-such-docker-binary"}, newTestManager(t))
	d.applyServerImage("img/a@sha256:1")
	if !d.applyServerImage("") || d.comfyURL != "" || d.comfyOwned {
		t.Fatal("a listing switched off must stop image work")
	}
}
