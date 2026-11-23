package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiClient talks to one connection's provider with that connection's bearer token. There is
// one per (connection, origin); its token is never sent to any other origin.
type apiClient struct {
	server      string
	token       string
	http        *http.Client
	onFilesHint func() // /pending said a site waits on the shared folders
}

// apiError is a non-success answer, kept typed so a 401/403 can be told from a hiccup.
type apiError struct {
	path   string
	status int
	body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("%s %d: %s", e.path, e.status, e.body) }

// roundTripper replaces the network transport for every client this program makes; tests set
// it to count or intercept requests. Nil means the default transport.
var roundTripper http.RoundTripper

// sameOriginClient follows a redirect only within the server's own origin: an answer from a
// compromised or impersonated server must not send this machine, with its token, anywhere
// else - not to another site, not into the owner's LAN.
func sameOriginClient(server string, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: roundTripper, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !sameOrigin(server, req.URL.String()) {
			return fmt.Errorf("refusing a redirect away from %s", server)
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
}

// sameOrigin: does rawURL point at the server itself (scheme, host and port)?
func sameOrigin(server, rawURL string) bool {
	a, err1 := url.Parse(server)
	b, err2 := url.Parse(rawURL)
	if err1 != nil || err2 != nil || a.Host == "" {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

type pendingResponse struct {
	Job *job `json:"job"`
}

type job struct {
	GUID         string          `json:"guid"`
	Kind         string          `json:"kind"`
	Workflow     string          `json:"workflow"`
	DockerImage  string          `json:"docker_image"`
	DockerDigest string          `json:"docker_digest"`
	WorkflowJSON string          `json:"workflow_json"` // comfy: the curated TEMPLATE with {{prompt}}/{{seed}}, or an imported graph already filled in
	Payload      json.RawMessage `json:"payload"`
	LLM          *jobLLM         `json:"llm"`
	TimeCapSec   int             `json:"time_cap_sec"`
	EgressHosts  []string        `json:"egress_hosts"`
	Inputs       []jobInput      `json:"inputs"`
}

type jobLLM struct {
	Digest string `json:"digest"`
	Model  string `json:"model"`
}

type jobInput struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

func (a *apiClient) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
}

// claim leases one job of these kinds, or returns nil when the queue is empty.
func (a *apiClient) claim(kinds []string) (*job, error) {
	req, _ := http.NewRequest(http.MethodGet, a.server+"/api/v1/mesh/pending?kinds="+url.QueryEscape(strings.Join(kinds, ",")), nil)
	a.auth(req)
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Mesh-Files") == "1" && a.onFilesHint != nil {
		a.onFilesHint()
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, &apiError{"pending", resp.StatusCode, truncate(string(b), 300)}
	}
	var pr pendingResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return pr.Job, nil
}

// getJSON reads a daemon endpoint; found is false on 204 (nothing to do).
func (a *apiClient) getJSON(path string, out any) (found bool, err error) {
	req, _ := http.NewRequest(http.MethodGet, a.server+path, nil)
	a.auth(req)
	resp, err := a.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return false, &apiError{path, resp.StatusCode, truncate(string(b), 300)}
	}
	return true, json.NewDecoder(resp.Body).Decode(out)
}

func (a *apiClient) submit(body map[string]any) error {
	return a.postJSON("/api/v1/mesh/submit", body, nil)
}

// heartbeat keeps a long job's lease; false means the server wants the job stopped
// (cancelled, declined, or no longer ours).
func (a *apiClient) heartbeat(guid string) bool {
	var out struct {
		Continue bool `json:"continue"`
	}
	if err := a.postJSON("/api/v1/mesh/heartbeat", map[string]any{"job": guid}, &out); err != nil {
		return !strings.Contains(err.Error(), "409")
	}
	return out.Continue
}

func (a *apiClient) postJSON(path string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, a.server+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	a.auth(req)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &apiError{path, resp.StatusCode, truncate(string(data), 300)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// download fetches one signed input URL (the bearer token is required as well).
func (a *apiClient) download(rawURL string, w io.Writer) (int64, error) {
	// Job inputs come from the server and nowhere else: a URL pointing elsewhere (another
	// host, the owner's router) is refused before anything is sent - with or without the token.
	if !sameOrigin(a.server, rawURL) {
		return 0, fmt.Errorf("refusing an input that is not on %s", a.server)
	}
	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	a.auth(req)
	req.Header.Set("Accept", "*/*")
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("input %d", resp.StatusCode)
	}
	return io.Copy(w, resp.Body)
}
