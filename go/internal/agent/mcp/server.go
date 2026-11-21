// Package mcp is a thin stdio JSON-RPC adapter over app.Service. It contains
// protocol-specific definitions; no MCP types enter the domain core.
// This deliberately small transport is intended for local single-user clients.
package mcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/davidherring123/materialmaker-mcp/go/internal/adapter/materialmaker"
	"github.com/davidherring123/materialmaker-mcp/go/internal/app"
	"github.com/davidherring123/materialmaker-mcp/go/internal/core/material"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
	Annotations any    `json:"annotations,omitempty"`
}

func schema(required []string, fields map[string]any) map[string]any {
	out := map[string]any{"type": "object", "properties": fields, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
func simple(t string) any { return map[string]any{"type": t} }

var toolset = []tool{
	{"material_list", "List editable materials in the workspace.", schema(nil, map[string]any{}), map[string]any{"readOnlyHint": true}},
	{"material_catalog", "Discover Material Maker node types or describe one known type. Use a search query to avoid returning a huge catalog.", schema(nil, map[string]any{"query": simple("string"), "node_type": simple("string"), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}), map[string]any{"readOnlyHint": true}},
	{"material_inspect", "Read a saved material graph and its SHA-256 revision. Use that revision with material_edit to prevent stale writes.", schema([]string{"name"}, map[string]any{"name": simple("string")}), map[string]any{"readOnlyHint": true}},
	{"material_create", "Create a new material without overwriting an existing one. Optionally clone a category/template from the configured cookbook.", schema([]string{"name"}, map[string]any{"name": simple("string"), "category": simple("string"), "template": simple("string")}), map[string]any{"destructiveHint": false}},
	{"material_edit", "Atomically apply up to 100 deterministic graph edits, validate the result, then save once. No partial patches are published.", schema([]string{"name", "operations", "expected_revision"}, map[string]any{"name": simple("string"), "expected_revision": simple("string"), "operations": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "object"}}}), map[string]any{"destructiveHint": true}},
	{"material_validate", "Validate a saved .ptex against the configured Material Maker node definitions.", schema([]string{"name"}, map[string]any{"name": simple("string")}), map[string]any{"readOnlyHint": true}},
	{"material_render", "Export PBR maps using the configured local Material Maker/ Godot CLI. Publishing replaces the previous complete output only after validation.", schema([]string{"name", "size"}, map[string]any{"name": simple("string"), "size": map[string]any{"type": "integer", "minimum": 16, "maximum": 2048}}), map[string]any{"destructiveHint": true}},
	{"material_preview", "Render a 3D screenshot using the current material and return a stable image path and inline PNG. Saves NAME_preview.png beside fresh generated maps.", schema([]string{"name"}, map[string]any{"name": simple("string"), "size": map[string]any{"type": "integer", "minimum": 16, "maximum": 2048}}), map[string]any{"destructiveHint": true}},
}

type Server struct{ Service *app.Service }

func New(service *app.Service) *Server { return &Server{Service: service} }

func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if e := encode(writer, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "invalid JSON"}}); e != nil {
				return e
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		} // protocol notifications never receive a response
		reply := s.Handle(ctx, req)
		if err := encode(writer, reply); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func encode(w *bufio.Writer, body response) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.Flush()
}
func (s *Server) Handle(ctx context.Context, r request) response {
	id := r.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	ok := func(v any) response { return response{JSONRPC: "2.0", ID: id, Result: v} }
	fail := func(code int, msg string) response {
		return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
	}
	switch r.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(r.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = "2025-06-18"
		}
		return ok(map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]string{"name": "materialmaker-mcp", "version": "0.1.0"}, "instructions": "All materials are stored in the configured workspace. Prefer reading a revision before editing; changes are atomic. Never use shell commands to edit .ptex when a tool covers the operation."})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": toolset})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return fail(-32602, "invalid tools/call parameters")
		}
		result, err := s.call(ctx, p.Name, p.Arguments)
		if errors.Is(err, errUnknownTool) {
			return fail(-32602, err.Error())
		}
		return ok(toolResult(result, err))
	default:
		return fail(-32601, "method not found: "+r.Method)
	}
}
func toolResult(value any, err error) any {
	result := map[string]any{"isError": err != nil}
	if err != nil {
		result["content"] = []any{map[string]any{"type": "text", "text": err.Error()}}
		return result
	}
	data, e := json.Marshal(value)
	if e != nil {
		result["isError"] = true
		result["content"] = []any{map[string]any{"type": "text", "text": e.Error()}}
		return result
	}
    content:=[]any{map[string]any{"type":"text","text":string(data)}}
    if preview,ok:=value.(materialmaker.PreviewResult);ok {
        // Inline images are capped at 8MiB; the path is always returned.
        if info,e:=os.Stat(preview.ImagePath);e==nil&&info.Mode().IsRegular()&&info.Size()<=8<<20 {
            if bytes,e:=os.ReadFile(preview.ImagePath);e==nil {
                content=append(content,map[string]any{"type":"image","mimeType":"image/png","data":base64.StdEncoding.EncodeToString(bytes)})
            }
        }
    }
    result["content"]=content
    result["structuredContent"] = value
	return result
}

var errUnknownTool = errors.New("unknown tool")

func (s *Server) call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var a struct {
		Name             string               `json:"name"`
		Query            string               `json:"query"`
		NodeType         string               `json:"node_type"`
		Limit            int                  `json:"limit"`
		Category         string               `json:"category"`
		Template         string               `json:"template"`
		ExpectedRevision string               `json:"expected_revision"`
		Operations       []material.Operation `json:"operations"`
		Size             int                  `json:"size"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	switch name {
	case "material_list":
		return s.Service.List()
	case "material_catalog":
		if a.NodeType != "" {
			return s.Service.Describe(a.NodeType)
		}
		return map[string]any{"node_types": s.Service.CatalogSearch(a.Query, a.Limit)}, nil
	case "material_inspect":
		return s.Service.Inspect(a.Name)
	case "material_create":
		m, p, e := s.Service.Create(a.Name, a.Category, a.Template)
		return map[string]any{"material": m, "problems": p}, e
	case "material_edit":
		if strings.TrimSpace(a.ExpectedRevision) == "" {
			return nil, errors.New("expected_revision is required; inspect material first")
		}
		m, p, e := s.Service.Edit(a.Name, a.ExpectedRevision, a.Operations)
		return map[string]any{"material": m, "problems": p}, e
	case "material_validate":
		return s.Service.Validate(a.Name)
	case "material_render":
		return s.Service.Render(ctx, a.Name, a.Size)
    case "material_preview":
        if a.Size==0{a.Size=256}
        return s.Service.Preview(ctx,a.Name,a.Size)
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownTool, name)
	}
}
