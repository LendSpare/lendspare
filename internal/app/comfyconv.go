package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Workflows from anywhere (docs/features/03): what this machine's ComfyUI can run, the
// conversion of a UI-format workflow into the API format with this ComfyUI's own
// /object_info (only a machine with the nodes installed knows their input order), and
// the input images of a typed job.

// --- capabilities ------------------------------------------------------------------

// comfyCaps is the part of the capability report the lease matches a workflow against:
// node classes (/object_info) and model files (/models/<folder>).
type comfyCaps struct {
	Nodes  []string `json:"nodes"`
	Models []string `json:"models"`
}

func comfyCapabilities(client *http.Client, base string) (*comfyCaps, error) {
	var info map[string]json.RawMessage
	if err := getJSON(client, base+"/object_info", &info); err != nil {
		return nil, err
	}
	caps := &comfyCaps{Nodes: make([]string, 0, len(info)), Models: []string{}}
	for name := range info {
		caps.Nodes = append(caps.Nodes, name)
	}
	sort.Strings(caps.Nodes)
	var folders []string
	if err := getJSON(client, base+"/models", &folders); err != nil {
		// An old ComfyUI without /models: nodes only; imported workflows naming models
		// will not be leased to it, which is the safe side.
		return caps, nil
	}
	seen := map[string]bool{}
	for _, folder := range folders {
		var files []string
		if err := getJSON(client, base+"/models/"+url.PathEscape(folder), &files); err != nil {
			continue
		}
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				caps.Models = append(caps.Models, f)
			}
		}
	}
	sort.Strings(caps.Models)
	return caps, nil
}

func getJSON(client *http.Client, u string, out any) error {
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %d", u, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- UI format -> API format ----------------------------------------------------------

// nodeSpec is one node class from /object_info, reduced to its inputs in order.
type nodeSpec struct {
	inputs []inputSpec
}

type inputSpec struct {
	name   string
	widget bool // has a value in widgets_values
	extra  int  // widget values that follow it and belong to the UI (seed control, upload button)
}

var widgetTypes = map[string]bool{"INT": true, "FLOAT": true, "STRING": true, "BOOLEAN": true, "COMBO": true}

// parseObjectInfo reads /object_info keeping each node's input order: Go maps do not,
// and widgets_values is positional.
func parseObjectInfo(raw []byte) (map[string]nodeSpec, error) {
	var nodes map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	out := make(map[string]nodeSpec, len(nodes))
	for class, body := range nodes {
		var node struct {
			Input      map[string]json.RawMessage `json:"input"`
			InputOrder map[string][]string        `json:"input_order"`
		}
		if json.Unmarshal(body, &node) != nil {
			continue
		}
		spec := nodeSpec{}
		for _, section := range []string{"required", "optional"} {
			rawSection, ok := node.Input[section]
			if !ok {
				continue
			}
			order := node.InputOrder[section]
			if len(order) == 0 {
				order = orderedKeys(rawSection)
			}
			var defs map[string]json.RawMessage
			if json.Unmarshal(rawSection, &defs) != nil {
				continue
			}
			for _, name := range order {
				spec.inputs = append(spec.inputs, describeInput(name, defs[name]))
			}
		}
		out[class] = spec
	}
	return out, nil
}

// describeInput: ["INT", {...}] or [["a","b"], {...}] or ["COMBO", {"options": [...]}] are
// widgets; ["MODEL"] is a socket.
func describeInput(name string, raw json.RawMessage) inputSpec {
	in := inputSpec{name: name}
	var def []json.RawMessage
	if json.Unmarshal(raw, &def) != nil || len(def) == 0 {
		return in
	}
	var typeName string
	if json.Unmarshal(def[0], &typeName) == nil {
		in.widget = widgetTypes[typeName]
	} else {
		var options []any
		in.widget = json.Unmarshal(def[0], &options) == nil
	}
	if len(def) > 1 {
		var opts map[string]any
		if json.Unmarshal(def[1], &opts) == nil {
			if v, ok := opts["control_after_generate"].(bool); ok && v {
				in.extra++
			}
			if v, ok := opts["image_upload"].(bool); ok && v {
				in.extra++
			}
			if v, ok := opts["forceInput"].(bool); ok && v {
				in.widget = false
			}
		}
	}
	// Older ComfyUI adds the "control after generate" widget to any seed without saying so.
	if in.widget && in.extra == 0 && (name == "seed" || name == "noise_seed") && typeName == "INT" {
		in.extra = 1
	}
	return in
}

// orderedKeys lists an object's keys in document order.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		key, _ := t.(string)
		keys = append(keys, key)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

type uiWorkflow struct {
	Nodes []uiNode          `json:"nodes"`
	Links []json.RawMessage `json:"links"`
}

type uiNode struct {
	ID            json.Number     `json:"id"`
	Type          string          `json:"type"`
	Mode          int             `json:"mode"`
	Title         string          `json:"title"`
	Inputs        []uiInput       `json:"inputs"`
	WidgetsValues json.RawMessage `json:"widgets_values"`
}

type uiInput struct {
	Name   string                 `json:"name"`
	Link   *int                   `json:"link"`
	Widget *struct{ Name string } `json:"widget"`
}

type uiLink struct {
	fromNode string
	fromSlot int
}

// Nodes that exist only in the editor.
var virtualNodes = map[string]bool{"Note": true, "MarkdownNote": true, "Reroute": true, "PrimitiveNode": true}

// convertUIWorkflow turns a UI-format workflow into what /prompt takes, using this
// machine's /object_info. It refuses rather than guesses: a node class this ComfyUI does
// not have, a bypassed or muted node, or a subgraph.
func convertUIWorkflow(ui []byte, info map[string]nodeSpec) (map[string]any, error) {
	var wf uiWorkflow
	dec := json.NewDecoder(bytes.NewReader(ui))
	dec.UseNumber()
	if err := dec.Decode(&wf); err != nil {
		return nil, fmt.Errorf("not a UI-format workflow: %w", err)
	}
	links := map[int]uiLink{}
	for _, raw := range wf.Links {
		// [id, from_node, from_slot, to_node, to_slot, type]
		var l []any
		if decodeNumbers(raw, &l) != nil || len(l) < 5 {
			continue
		}
		id, ok1 := l[0].(json.Number)
		from, ok2 := l[1].(json.Number)
		slot, ok3 := l[2].(json.Number)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		linkID, _ := id.Int64()
		fromSlot, _ := slot.Int64()
		links[int(linkID)] = uiLink{fromNode: from.String(), fromSlot: int(fromSlot)}
	}
	byID := map[string]uiNode{}
	for _, n := range wf.Nodes {
		byID[n.ID.String()] = n
	}

	// resolve follows a link through reroutes to a real node output, or to a primitive's value.
	var resolve func(linkID int, depth int) (any, error)
	resolve = func(linkID int, depth int) (any, error) {
		l, ok := links[linkID]
		if !ok || depth > 50 {
			return nil, fmt.Errorf("broken link %d", linkID)
		}
		src, ok := byID[l.fromNode]
		if !ok {
			return nil, fmt.Errorf("link %d comes from a missing node", linkID)
		}
		switch src.Type {
		case "Reroute":
			if len(src.Inputs) == 0 || src.Inputs[0].Link == nil {
				return nil, fmt.Errorf("an unconnected reroute (node %s)", src.ID)
			}
			return resolve(*src.Inputs[0].Link, depth+1)
		case "PrimitiveNode":
			var values []any
			if decodeNumbers(src.WidgetsValues, &values) != nil || len(values) == 0 {
				return nil, fmt.Errorf("an empty primitive (node %s)", src.ID)
			}
			return normaliseNumber(values[0]), nil
		}
		return []any{l.fromNode, l.fromSlot}, nil
	}

	out := map[string]any{}
	for _, n := range wf.Nodes {
		if virtualNodes[n.Type] {
			continue
		}
		if n.Mode == 2 || n.Mode == 4 {
			return nil, fmt.Errorf("node %s (%s) is muted or bypassed; enable or remove it and export again", n.ID, n.Type)
		}
		spec, ok := info[n.Type]
		if !ok {
			return nil, fmt.Errorf("this ComfyUI has no node %q; install its node pack first", n.Type)
		}
		inputs := map[string]any{}
		linked := map[string]int{}
		for _, in := range n.Inputs {
			if in.Link != nil {
				name := in.Name
				if in.Widget != nil && in.Widget.Name != "" {
					name = in.Widget.Name
				}
				linked[name] = *in.Link
			}
		}
		// Widget values are positional, except for nodes that store them by name.
		var positional []any
		var named map[string]any
		if decodeNumbers(n.WidgetsValues, &positional) != nil {
			_ = decodeNumbers(n.WidgetsValues, &named)
		}
		i := 0
		for _, in := range spec.inputs {
			if in.widget {
				if named != nil {
					if v, ok := named[in.name]; ok {
						inputs[in.name] = normaliseNumber(v)
					}
				} else if i < len(positional) {
					inputs[in.name] = normaliseNumber(positional[i])
				}
				i += 1 + in.extra
			}
			if linkID, ok := linked[in.name]; ok {
				v, err := resolve(linkID, 0)
				if err != nil {
					return nil, err
				}
				inputs[in.name] = v
			}
		}
		node := map[string]any{"class_type": n.Type, "inputs": inputs}
		if n.Title != "" {
			node["_meta"] = map[string]any{"title": n.Title}
		}
		out[n.ID.String()] = node
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the workflow has no nodes ComfyUI can run")
	}
	return out, nil
}

// decodeNumbers decodes keeping numbers exact: a 15-digit seed does not survive float64.
func decodeNumbers(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// normaliseNumber keeps whole numbers whole (a seed must stay an int).
func normaliseNumber(v any) any {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	if f, ok := v.(float64); ok && f == float64(int64(f)) && f < 1e15 && f > -1e15 {
		return int64(f)
	}
	return v
}

// convertPending converts, at most once per workflow per run, what the owner imported in
// UI format (GET /api/v1/mesh/convert → POST /api/v1/mesh/convert/{id}).
func (d *daemon) convertPending() {
	d.lastConvert = time.Now()
	var pending struct {
		Workflow *struct {
			ID    int             `json:"id"`
			Title string          `json:"title"`
			JSON  json.RawMessage `json:"json"`
		} `json:"workflow"`
	}
	found, err := d.api.getJSON("/api/v1/mesh/convert", &pending)
	if err != nil || !found || pending.Workflow == nil || d.triedConvert[pending.Workflow.ID] {
		return
	}
	id := pending.Workflow.ID
	d.triedConvert[id] = true
	resp, err := d.http.Get(d.comfyURL + "/object_info")
	if err != nil {
		return
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	info, err := parseObjectInfo(raw)
	var body map[string]any
	if err == nil {
		var api map[string]any
		api, err = convertUIWorkflow(pending.Workflow.JSON, info)
		body = map[string]any{"json": api}
	}
	if err != nil {
		log.Printf("[lendspare] cannot convert workflow %d (%s): %v", id, pending.Workflow.Title, err)
		body = map[string]any{"error": truncate(err.Error(), 300)}
	} else {
		log.Printf("[lendspare] converted workflow %d (%s)", id, pending.Workflow.Title)
	}
	if err := d.api.postJSON("/api/v1/mesh/convert/"+strconv.Itoa(id), body, nil); err != nil {
		log.Printf("[lendspare] conversion upload failed: %v", err)
	}
}

// --- input images of a typed job ----------------------------------------------------

// uploadInputs fetches each input image through its signed link and uploads it to the
// local ComfyUI, returning placeholder -> the name ComfyUI stored it under.
func (d *daemon) uploadInputs(inputs []jobInput) (map[string]string, error) {
	names := map[string]string{}
	for _, in := range inputs {
		var buf bytes.Buffer
		if _, err := d.api.download(in.URL, &buf); err != nil {
			return nil, fmt.Errorf("input %s: %w", in.Name, err)
		}
		stored, err := comfyUpload(d.http, d.comfyURL, "lendspare-"+in.Name+"-"+short(in.SHA256)+".png", buf.Bytes())
		if err != nil {
			return nil, err
		}
		names[fmt.Sprintf("{{input:%s}}", in.Name)] = stored
	}
	return names, nil
}

func comfyUpload(client *http.Client, base, filename string, data []byte) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("image", filename)
	_, _ = part.Write(data)
	_ = w.WriteField("overwrite", "true")
	_ = w.Close()
	resp, err := client.Post(base+"/upload/image", w.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil || out.Name == "" {
		return "", fmt.Errorf("comfy /upload/image %d", resp.StatusCode)
	}
	if out.Subfolder != "" {
		return out.Subfolder + "/" + out.Name, nil
	}
	return out.Name, nil
}

// fillPlaceholders replaces input values that are exactly a placeholder, at the value
// level, so a buyer's prompt that happens to contain the text is left alone.
func fillPlaceholders(graph map[string]any, names map[string]string) {
	for _, node := range graph {
		n, ok := node.(map[string]any)
		if !ok {
			continue
		}
		inputs, ok := n["inputs"].(map[string]any)
		if !ok {
			continue
		}
		for k, v := range inputs {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "{{input:") {
				if name, ok := names[s]; ok {
					inputs[k] = name
				}
			}
		}
	}
}
