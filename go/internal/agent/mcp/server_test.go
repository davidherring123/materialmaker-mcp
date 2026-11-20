package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/davidherring123/materialmaker-mcp/go/internal/app"
	"github.com/davidherring123/materialmaker-mcp/go/internal/core/material"
	"strings"
	"testing"
)

func TestInitializeToolsAndNoNotificationReply(t *testing.T) {
	w, err := material.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(&app.Service{Workspace: w, Catalog: material.Catalog{}})
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"material_list","arguments":{}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := s.Run(context.Background(), strings.NewReader(in), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three responses, got %d: %s", len(lines), output.String())
	}
	var init map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	if init["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal("wrong protocol version")
	}
	var list map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &list)
	if n := len(list["result"].(map[string]any)["tools"].([]any)); n != 7 {
		t.Fatalf("wrong number of tools: %d", n)
	}
	var called map[string]any
	_ = json.Unmarshal([]byte(lines[2]), &called)
	if called["result"].(map[string]any)["isError"] != false {
		t.Fatalf("call failed: %s", lines[2])
	}
}
func TestUnknownToolIsJSONRPCError(t *testing.T) {
	s := New(&app.Service{})
	r := s.Handle(context.Background(), request{ID: json.RawMessage("42"), Method: "tools/call", Params: json.RawMessage(`{"name":"not_a_tool","arguments":{}}`)})
	if r.Error == nil || r.Error.Code != -32602 {
		t.Fatalf("unexpected unknown tool response: %+v", r)
	}
}
