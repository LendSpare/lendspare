package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// A recorded ComfyUI: object_info, model folders, upload, prompt, history and view.
type fakeComfy struct {
	*httptest.Server
	prompt   map[string]any
	uploaded []byte
}

func newFakeComfy(t *testing.T) *fakeComfy {
	info, err := os.ReadFile("testdata/object_info.json")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nfake")
	f := &fakeComfy{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/object_info":
			w.Write(info)
		case "/models":
			fmt.Fprint(w, `["checkpoints","loras","vae"]`)
		case "/models/checkpoints":
			fmt.Fprint(w, `["sd_xl_base_1.0.safetensors"]`)
		case "/models/loras":
			fmt.Fprint(w, `["watercolor_xl.safetensors","sd_xl_base_1.0.safetensors"]`)
		case "/models/vae":
			fmt.Fprint(w, `[]`)
		case "/upload/image":
			file, _, err := r.FormFile("image")
			if err != nil {
				http.Error(w, "no file", http.StatusBadRequest)
				return
			}
			f.uploaded, _ = io.ReadAll(file)
			fmt.Fprint(w, `{"name":"mesh-image.png","subfolder":"","type":"input"}`)
		case "/prompt":
			var body struct {
				Prompt map[string]any `json:"prompt"`
			}
			dec := json.NewDecoder(r.Body)
			dec.UseNumber()
			_ = dec.Decode(&body)
			f.prompt = body.Prompt
			fmt.Fprint(w, `{"prompt_id":"p1"}`)
		case "/history/p1":
			fmt.Fprint(w, `{"p1":{"outputs":{"9":{"images":[{"filename":"out.png","subfolder":"","type":"output"}]}}}}`)
		case "/view":
			w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	return f
}

func loadObjectInfo(t *testing.T) map[string]nodeSpec {
	raw, err := os.ReadFile("testdata/object_info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := parseObjectInfo(raw)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func inputsOf(api map[string]any, id string) map[string]any {
	return api[id].(map[string]any)["inputs"].(map[string]any)
}

func TestComfyCapabilitiesListsNodesAndModelFiles(t *testing.T) {
	c := newFakeComfy(t)
	defer c.Close()
	caps, err := comfyCapabilities(c.Client(), c.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(caps.Nodes) != 7 || caps.Nodes[0] != "CLIPTextEncode" {
		t.Fatalf("nodes: %v", caps.Nodes)
	}
	want := []string{"sd_xl_base_1.0.safetensors", "watercolor_xl.safetensors"}
	if strings.Join(caps.Models, ",") != strings.Join(want, ",") {
		t.Fatalf("models (deduplicated, sorted): %v", caps.Models)
	}
}

func TestObjectInfoKeepsInputOrderAndSeesSeedControl(t *testing.T) {
	info := loadObjectInfo(t)
	names := []string{}
	for _, in := range info["KSampler"].inputs {
		names = append(names, in.name)
	}
	if strings.Join(names, ",") != "model,seed,steps,cfg,sampler_name,scheduler,positive,negative,latent_image,denoise" {
		t.Fatalf("order: %v", names)
	}
	seed := info["KSampler"].inputs[1]
	if !seed.widget || seed.extra != 1 {
		t.Fatalf("seed: %+v", seed)
	}
	if info["KSampler"].inputs[0].widget {
		t.Fatal("MODEL is a socket, not a widget")
	}
	if img := info["LoadImage"].inputs[0]; !img.widget || img.extra != 1 {
		t.Fatalf("LoadImage.image has an upload button after it: %+v", img)
	}
}

func TestConvertsAUIWorkflowWithThisMachinesObjectInfo(t *testing.T) {
	ui, _ := os.ReadFile("testdata/sdxl-ui.json")
	api, err := convertUIWorkflow(ui, loadObjectInfo(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := api["10"]; ok {
		t.Fatal("the Note node is editor-only and must not be sent")
	}
	ks := inputsOf(api, "3")
	if ks["seed"] != int64(156680208700286) || ks["steps"] != int64(20) || ks["cfg"] != int64(8) ||
		ks["sampler_name"] != "euler" || ks["scheduler"] != "normal" || ks["denoise"] != int64(1) {
		t.Fatalf("KSampler widgets (the seed control value must be skipped): %v", ks)
	}
	if fmt.Sprint(ks["positive"]) != "[6 0]" || fmt.Sprint(ks["latent_image"]) != "[5 0]" {
		t.Fatalf("links: %v", ks)
	}
	if inputsOf(api, "6")["text"] != "a cozy cabin, golden hour" {
		t.Fatalf("prompt: %v", api["6"])
	}
	if fmt.Sprint(inputsOf(api, "8")["vae"]) != "[4 2]" {
		t.Fatalf("vae link: %v", api["8"])
	}
}

func TestConversionRefusesWhatItCannotKnow(t *testing.T) {
	info := loadObjectInfo(t)
	unknown := `{"nodes":[{"id":1,"type":"FancyCustomNode","mode":0,"widgets_values":[]}],"links":[]}`
	if _, err := convertUIWorkflow([]byte(unknown), info); err == nil || !strings.Contains(err.Error(), "install its node pack") {
		t.Fatalf("unknown node: %v", err)
	}
	bypassed := `{"nodes":[{"id":1,"type":"SaveImage","mode":4,"widgets_values":["x"]}],"links":[]}`
	if _, err := convertUIWorkflow([]byte(bypassed), info); err == nil || !strings.Contains(err.Error(), "bypassed") {
		t.Fatalf("bypassed node: %v", err)
	}
}

func TestConversionResolvesRerouteAndPrimitive(t *testing.T) {
	ui := `{"nodes":[
	  {"id":1,"type":"PrimitiveNode","mode":0,"outputs":[{"links":[1]}],"widgets_values":[768,"fixed"]},
	  {"id":2,"type":"EmptyLatentImage","mode":0,"inputs":[{"name":"width","link":1,"widget":{"name":"width"}}],"widgets_values":[512,512,1]},
	  {"id":3,"type":"Reroute","mode":0,"inputs":[{"name":"","link":2}]},
	  {"id":4,"type":"VAEDecode","mode":0,"inputs":[{"name":"samples","link":3},{"name":"vae","link":null}]}
	],"links":[[1,1,0,2,0,"INT"],[2,2,0,3,0,"LATENT"],[3,3,0,4,0,"LATENT"]]}`
	api, err := convertUIWorkflow([]byte(ui), loadObjectInfo(t))
	if err != nil {
		t.Fatal(err)
	}
	latent := inputsOf(api, "2")
	if latent["width"] != int64(768) || latent["height"] != int64(512) {
		t.Fatalf("primitive value: %v", latent)
	}
	if fmt.Sprint(inputsOf(api, "4")["samples"]) != "[2 0]" {
		t.Fatalf("reroute should resolve to node 2: %v", api["4"])
	}
}

func TestTypedJobUploadsItsInputImageAndLeavesThePromptAlone(t *testing.T) {
	c := newFakeComfy(t)
	defer c.Close()
	input := []byte("\x89PNG\r\n\x1a\nbuyer")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		w.Write(input)
	}))
	defer server.Close()

	d := &daemon{http: c.Client(), comfyURL: c.URL, cfg: config{jobTimeoutSec: 5},
		api: &apiClient{server: server.URL, token: "tok", http: server.Client()}}
	names, err := d.uploadInputs([]jobInput{{Name: "image", URL: server.URL + "/in", SHA256: "abcdef0123456789"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(c.uploaded) != string(input) {
		t.Fatal("the input image was not uploaded to ComfyUI")
	}
	graph := `{"1":{"class_type":"LoadImage","inputs":{"image":"{{input:image}}"}},` +
		`"2":{"class_type":"CLIPTextEncode","inputs":{"text":"keep {{prompt}} and {{input:image}} as typed"}},` +
		`"3":{"class_type":"KSampler","inputs":{"seed":156680208700286}}}`
	p := comfyPayload{Params: json.RawMessage(`{"prompt":"x"}`), Prompt: "REPLACED"}
	images, err := d.runComfy(graph, p, names)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("images: %d", len(images))
	}
	if inputsOf(c.prompt, "1")["image"] != "mesh-image.png" {
		t.Fatalf("placeholder not filled: %v", c.prompt["1"])
	}
	if inputsOf(c.prompt, "2")["text"] != "keep {{prompt}} and {{input:image}} as typed" {
		t.Fatalf("a typed graph's text must be left as the buyer wrote it: %v", c.prompt["2"])
	}
	if fmt.Sprint(inputsOf(c.prompt, "3")["seed"]) != "156680208700286" {
		t.Fatalf("a large seed must survive exactly: %v", c.prompt["3"])
	}
}

func TestCuratedTemplateStillGetsPromptAndSeed(t *testing.T) {
	c := newFakeComfy(t)
	defer c.Close()
	d := &daemon{http: c.Client(), comfyURL: c.URL, cfg: config{jobTimeoutSec: 5}}
	seed := 7
	graph := `{"6":{"class_type":"CLIPTextEncode","inputs":{"text":"{{prompt}}"}},"13":{"class_type":"KSampler","inputs":{"seed":{{seed}}}}}`
	if _, err := d.runComfy(graph, comfyPayload{Prompt: `a "quoted" lake`, Seed: &seed}, nil); err != nil {
		t.Fatal(err)
	}
	if inputsOf(c.prompt, "6")["text"] != `a "quoted" lake` || fmt.Sprint(inputsOf(c.prompt, "13")["seed"]) != "7" {
		t.Fatalf("template: %v", c.prompt)
	}
}
