package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// llmServer is a local OpenAI-compatible model server the owner already runs.
type llmServer struct {
	kind   string // "ollama" | "lmstudio" | "openai"
	url    string
	models []llmModel
}

type llmModel struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Server string `json:"server"`
}

// detectLLM finds the local model server: the --llm-url given, else Ollama on :11434,
// else LM Studio on :1234. Returns nil when none answers. Only Ollama reports weights
// digests, so only Ollama models can be confirmed and offered (docs/features/15).
func detectLLM(client *http.Client, configured string) *llmServer {
	if configured == "off" {
		return nil
	}
	candidates := []string{"http://127.0.0.1:11434", "http://127.0.0.1:1234"}
	if configured != "" {
		candidates = []string{strings.TrimRight(configured, "/")}
	}
	short := &http.Client{Timeout: 3 * time.Second, Transport: client.Transport}
	for _, base := range candidates {
		if models, ok := ollamaTags(short, base); ok {
			return &llmServer{kind: "ollama", url: base, models: models}
		}
		if models, ok := openAIModels(short, base); ok {
			return &llmServer{kind: "lmstudio", url: base, models: models}
		}
	}
	return nil
}

// ollamaTags reads GET /api/tags: every model with the digest of its manifest.
func ollamaTags(client *http.Client, base string) ([]llmModel, bool) {
	resp, err := client.Get(base + "/api/tags")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, false
	}
	out := []llmModel{}
	for _, m := range tags.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		out = append(out, llmModel{Name: name, Digest: strings.TrimPrefix(m.Digest, "sha256:"), Server: "ollama"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true
}

// openAIModels reads GET /v1/models (LM Studio): names, but no digest to confirm them by.
func openAIModels(client *http.Client, base string) ([]llmModel, bool) {
	resp, err := client.Get(base + "/v1/models")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, false
	}
	out := []llmModel{}
	for _, m := range list.Data {
		out = append(out, llmModel{Name: m.ID, Server: "lmstudio"})
	}
	return out, true
}

func modelKey(s *llmServer) string {
	if s == nil {
		return ""
	}
	parts := []string{s.url}
	for _, m := range s.models {
		parts = append(parts, m.Name+"@"+m.Digest)
	}
	return strings.Join(parts, ",")
}

// runLLMJob proxies a chat job to the local server and submits the OpenAI-shaped answer.
func (d *daemon) runLLMJob(j *job) error {
	if d.llm == nil {
		return fmt.Errorf("no local model server")
	}
	model := ""
	if j.LLM != nil {
		model = j.LLM.Model
	}
	if model == "" {
		return fmt.Errorf("this machine does not hold the weights for %s", j.Workflow)
	}
	answer, err := chatCompletion(&http.Client{Timeout: time.Duration(d.cfg.jobTimeoutSec) * time.Second}, d.llm.url, model, j.Payload)
	if err != nil {
		return err
	}
	return d.api.submit(map[string]any{"job": j.GUID, "json": answer})
}

// chatCompletion sends the buyer's request (only the forwarded fields, as the server
// built it) to the local /v1/chat/completions under the local model name, and returns
// the answer with `usage` normalised to OpenAI's prompt/completion token counts.
func chatCompletion(client *http.Client, base, model string, payload json.RawMessage) (map[string]any, error) {
	req := map[string]any{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("bad payload: %w", err)
		}
	}
	req["model"] = model
	req["stream"] = false
	if v, ok := req["max_completion_tokens"]; ok {
		if _, has := req["max_tokens"]; !has {
			req["max_tokens"] = v
		}
		delete(req, "max_completion_tokens")
	}
	body, _ := json.Marshal(req)
	resp, err := client.Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local model %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, fmt.Errorf("local model: not JSON")
	}
	answer["usage"] = normaliseUsage(answer)
	return answer, nil
}

// normaliseUsage accepts OpenAI's usage and Ollama's native counters
// (prompt_eval_count / eval_count) and returns OpenAI's shape.
func normaliseUsage(answer map[string]any) map[string]int {
	num := func(v any) int {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
		return 0
	}
	in, out := 0, 0
	if u, ok := answer["usage"].(map[string]any); ok {
		in, out = num(u["prompt_tokens"]), num(u["completion_tokens"])
	}
	if in == 0 {
		in = num(answer["prompt_eval_count"])
	}
	if out == 0 {
		out = num(answer["eval_count"])
	}
	return map[string]int{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out}
}
