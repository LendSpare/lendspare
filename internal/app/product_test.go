package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// The product-photo workflow (docs/features/18) end to end through runComfyJob: the photo
// is fetched with the worker's token from its signed link, uploaded to ComfyUI, its name
// written into the LoadImage placeholder (and nowhere else), the custom nodes reach
// ComfyUI untouched, and the result is submitted for the job.
// testdata/comfy-product-composite.json is a copy of agents/mesh/workflows/ (a PHP unit
// test keeps the two identical).
func TestProductJobUploadsThePhotoAndSubmitsTheComposite(t *testing.T) {
	c := newFakeComfy(t)
	defer c.Close()
	photo := []byte("\x89PNG\r\n\x1a\nproduct")
	var submitted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/api/v1/mesh/input/g1/7":
			w.Write(photo)
		case "/api/v1/mesh/submit":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &submitted)
			fmt.Fprint(w, `{"state":"SUCCESS"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// What the server hands the daemon: the typed graph with the image parameter replaced
	// by its placeholder (MeshComfyGraph::build) and the batch written in.
	raw, err := os.ReadFile("testdata/comfy-product-composite.json")
	if err != nil {
		t.Fatal(err)
	}
	var graph map[string]map[string]any
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatal(err)
	}
	graph["1"]["inputs"].(map[string]any)["image"] = "{{input:image}}"
	graph["7"]["inputs"].(map[string]any)["text"] = "on a marble counter, keep {{input:image}} literally"
	built, _ := json.Marshal(graph)

	d := &daemon{http: c.Client(), comfyURL: c.URL, cfg: config{jobTimeoutSec: 5},
		api: &apiClient{server: server.URL, token: "tok", http: server.Client()}}
	j := &job{GUID: "g1", Kind: "comfy", WorkflowJSON: string(built),
		Payload: json.RawMessage(`{"params":{"prompt":"x","seed":42,"image":true},"prompt":"x","seed":42,"batch":1,"inputs":["image"]}`),
		Inputs:  []jobInput{{Name: "image", Role: "input", SHA256: "0123456789abcdef", URL: server.URL + "/api/v1/mesh/input/g1/7?exp=1&sig=s"}}}
	if err := d.runComfyJob(j); err != nil {
		t.Fatal(err)
	}

	if string(c.uploaded) != string(photo) {
		t.Fatal("the product photo was not uploaded to ComfyUI")
	}
	if inputsOf(c.prompt, "1")["image"] != "mesh-image.png" {
		t.Fatalf("LoadImage placeholder not filled: %v", c.prompt["1"])
	}
	if inputsOf(c.prompt, "7")["text"] != "on a marble counter, keep {{input:image}} literally" {
		t.Fatalf("only an exact placeholder value is replaced: %v", c.prompt["7"])
	}
	composite := c.prompt["14"].(map[string]any)
	if composite["class_type"] != "MeshProductComposite" ||
		fmt.Sprint(inputsOf(c.prompt, "14")["product"]) != "[1 0]" || fmt.Sprint(inputsOf(c.prompt, "14")["mask"]) != "[2 0]" {
		t.Fatalf("the composite must take the original photo (node 1) and its mask: %v", composite)
	}
	if fmt.Sprint(inputsOf(c.prompt, "6")["mask"]) != "[2 1]" {
		t.Fatalf("the background is generated outside the product: %v", c.prompt["6"])
	}
	if submitted["job"] != "g1" {
		t.Fatalf("submit: %v", submitted)
	}
	images, _ := submitted["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images: %v", submitted["images"])
	}
	got, _ := base64.StdEncoding.DecodeString(images[0].(string))
	if string(got) != "\x89PNG\r\n\x1a\nfake" {
		t.Fatalf("the rendered image was not submitted: %q", got)
	}
}
