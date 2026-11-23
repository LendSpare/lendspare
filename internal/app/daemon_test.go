package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const llamaDigest = "a80c4f17acd55265feec403c7aef86be0c25983ab279d83f3bcd3abbcb5b8b72"

// A recorded Ollama: /api/tags with digests, and the OpenAI-compatible chat endpoint.
func fakeOllama(t *testing.T, seen *map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprintf(w, `{"models":[{"name":"llama3.2:3b","model":"llama3.2:3b","digest":"%s","size":2019393189}]}`, llamaDigest)
		case "/v1/chat/completions":
			if seen != nil {
				_ = json.NewDecoder(r.Body).Decode(seen)
			}
			fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Paris"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestDetectsOllamaWithDigests(t *testing.T) {
	srv := fakeOllama(t, nil)
	defer srv.Close()
	s := detectLLM(srv.Client(), srv.URL)
	if s == nil || s.kind != "ollama" {
		t.Fatalf("expected ollama, got %+v", s)
	}
	if len(s.models) != 1 || s.models[0].Digest != llamaDigest || s.models[0].Name != "llama3.2:3b" {
		t.Fatalf("models: %+v", s.models)
	}
}

func TestDetectsLMStudioWithoutDigests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"qwen2.5-7b-instruct"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s := detectLLM(srv.Client(), srv.URL)
	if s == nil || s.kind != "lmstudio" || s.models[0].Digest != "" {
		t.Fatalf("expected lmstudio without digest, got %+v", s)
	}
}

func TestNoServerAndOff(t *testing.T) {
	if detectLLM(http.DefaultClient, "http://127.0.0.1:1") != nil {
		t.Fatal("nothing listens on :1")
	}
	if detectLLM(http.DefaultClient, "off") != nil {
		t.Fatal("off means off")
	}
}

func TestChatProxiesUnderTheLocalNameAndNormalisesUsage(t *testing.T) {
	seen := map[string]any{}
	srv := fakeOllama(t, &seen)
	defer srv.Close()
	payload := json.RawMessage(`{"messages":[{"role":"user","content":"Capital of France?"}],"temperature":0,"seed":42,"max_completion_tokens":20,"stream":true}`)
	answer, err := chatCompletion(srv.Client(), srv.URL, "llama3.2:3b", payload)
	if err != nil {
		t.Fatal(err)
	}
	if seen["model"] != "llama3.2:3b" || seen["stream"] != false || seen["max_tokens"] != float64(20) {
		t.Fatalf("request sent: %+v", seen)
	}
	usage := answer["usage"].(map[string]int)
	if usage["prompt_tokens"] != 12 || usage["completion_tokens"] != 3 || usage["total_tokens"] != 15 {
		t.Fatalf("usage: %+v", usage)
	}
}

func TestNormalisesOllamaNativeCounters(t *testing.T) {
	u := normaliseUsage(map[string]any{"prompt_eval_count": float64(30), "eval_count": float64(7)})
	if u["prompt_tokens"] != 30 || u["completion_tokens"] != 7 || u["total_tokens"] != 37 {
		t.Fatalf("usage: %+v", u)
	}
}

func TestClaimSendsKindsAndBearer(t *testing.T) {
	var gotKinds, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKinds = r.URL.Query().Get("kinds")
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"job":{"guid":"g1","kind":"llm","workflow":"mesh-llama-3.2-3b","payload":{"messages":[]},"llm":{"model":"llama3.2:3b"}}}`)
	}))
	defer srv.Close()
	a := &apiClient{server: srv.URL, token: "tok", http: srv.Client()}
	j, err := a.claim([]string{"llm", "pipeline"})
	if err != nil || j == nil || j.Kind != "llm" || j.LLM.Model != "llama3.2:3b" {
		t.Fatalf("claim: %+v %v", j, err)
	}
	if gotKinds != "llm,pipeline" || gotAuth != "Bearer tok" {
		t.Fatalf("kinds=%q auth=%q", gotKinds, gotAuth)
	}
}

func TestEmptyQueueIsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	a := &apiClient{server: srv.URL, token: "t", http: srv.Client()}
	if j, err := a.claim([]string{"llm"}); j != nil || err != nil {
		t.Fatalf("got %+v %v", j, err)
	}
}

func TestFilterKinds(t *testing.T) {
	got := filterKinds([]string{"comfy", "llm", "pipeline"}, "llm, grader")
	if strings.Join(got, ",") != "llm" {
		t.Fatalf("got %v", got)
	}
	if len(filterKinds([]string{"a"}, "")) != 1 {
		t.Fatal("empty allow-list keeps everything")
	}
}

func TestCollectOutputsSkipsResultAndCaps(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "result.json"), []byte(`{"summary":"x"}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "patch.diff"), []byte("--- a\n+++ b\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "report.md"), []byte("# done"), 0o644)
	outs, err := collectOutputs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 || outs[0]["name"] != "patch.diff" || outs[0]["contentType"] != "text/x-diff" {
		t.Fatalf("outputs: %+v", outs)
	}
}

func TestGraderLimitsComeFromThePayload(t *testing.T) {
	j := &job{Kind: "grader"}
	l := containerLimits(j, map[string]any{"limits": map[string]any{"timeSec": float64(120), "memoryMb": float64(1024), "cpus": float64(1)}}, 600)
	if l.timeSec != 180 || l.memoryMB != 1024 || l.cpus != 1 {
		t.Fatalf("limits: %+v", l)
	}
	p := containerLimits(&job{Kind: "pipeline", TimeCapSec: 300}, nil, 600)
	if p.timeSec != 300 {
		t.Fatalf("pipeline cap: %+v", p)
	}
}

// The features workflow (docs/features/11) takes its limits from the payload like the grader.
func TestFeaturesLimitsComeFromThePayload(t *testing.T) {
	l := containerLimits(&job{Kind: "features"}, map[string]any{"limits": map[string]any{"timeSec": float64(120), "memoryMb": float64(6144), "cpus": float64(4)}}, 600)
	if l.timeSec != 180 || l.memoryMB != 6144 || l.cpus != 4 {
		t.Fatalf("limits: %+v", l)
	}
}

// A payload asking for more than docker has is fitted to it instead of failing every attempt.
func TestLimitsFitWhatDockerHas(t *testing.T) {
	l := limits{timeSec: 180, memoryMB: 6144, cpus: 4}.fitTo(3, 2*1024*1024*1024)
	if l.cpus != 3 || l.memoryMB != 2048 || l.timeSec != 180 {
		t.Fatalf("fitted: %+v", l)
	}
	if got := (limits{memoryMB: 4096, cpus: 2}).fitTo(0, 0); got.cpus != 2 || got.memoryMB != 4096 {
		t.Fatalf("unknown resources must change nothing: %+v", got)
	}
}

// A machine with docker offers the features kind; the server decides whether it is trusted.
func TestFeaturesKindIsOfferedWithDocker(t *testing.T) {
	if got := filterKinds([]string{"pipeline", "grader", "features"}, "features"); len(got) != 1 || got[0] != "features" {
		t.Fatalf("filterKinds: %v", got)
	}
}

// The speech workflow (docs/features/17) is a container kind: limits from the payload, any
// machine with docker offers it, and its audio comes back as a typed output.
func TestSpeechIsAContainerKind(t *testing.T) {
	l := containerLimits(&job{Kind: "tts"}, map[string]any{"limits": map[string]any{"timeSec": float64(600), "memoryMb": float64(4096), "cpus": float64(2)}}, 120)
	if l.timeSec != 660 || l.memoryMB != 4096 || l.cpus != 2 {
		t.Fatalf("limits: %+v", l)
	}
	if got := filterKinds([]string{"pipeline", "grader", "features", "tts"}, "tts"); len(got) != 1 || got[0] != "tts" {
		t.Fatalf("filterKinds: %v", got)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "speech.wav"), []byte("RIFF0000WAVEfmt "), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "result.json"), []byte(`{"durationSec": 1.5}`), 0o644)
	outputs, err := collectOutputs(dir)
	if err != nil || len(outputs) != 1 || outputs[0]["name"] != "speech.wav" || outputs[0]["contentType"] != "audio/wav" {
		t.Fatalf("outputs: %v %v", outputs, err)
	}
}

// The seller's API keys are passed to docker as a file path and never reach a log line
// or the server: the daemon has no code path that reads the file.
func TestPipelineEnvIsNeverRead(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "keys.env")
	_ = os.WriteFile(env, []byte("ANTHROPIC_API_KEY=sk-ant-secret-value\n"), 0o600)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	d := newDaemon(config{pipelineEnv: env, workDir: dir, docker: "docker-that-does-not-exist"}, newTestManager(t))
	d.reportAll()
	if strings.Contains(logs.String(), "sk-ant-secret-value") {
		t.Fatal("the secret reached a log line")
	}
}
