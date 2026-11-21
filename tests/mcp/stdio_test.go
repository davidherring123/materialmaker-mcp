// Package mcp contains black-box tests of the shipped stdio MCP executable.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This contract intentionally runs the built Go entrypoint through stdin and
// stdout rather than calling internal handlers. It catches transport/CLI drift.
func TestMCPStdioContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/materialmaker-mcp", "--workspace", t.TempDir(), "--nodes", filepath.Join(root, "tests", "fixtures", "nodes"))
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	call := func(id int, method string, params any) map[string]any {
		t.Helper()
		data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(stdin, string(data)); err != nil {
			t.Fatalf("send %s: %v (stderr=%s)", method, err, stderr.String())
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read %s: %v (stderr=%s)", method, err, stderr.String())
		}
		var reply map[string]any
		if err := json.Unmarshal(line, &reply); err != nil {
			t.Fatal(err)
		}
		if reply["error"] != nil {
			t.Fatalf("unexpected RPC error for %s: %s", method, line)
		}
		return reply["result"].(map[string]any)
	}

	initialization := call(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}})
	if initialization["serverInfo"] == nil {
		t.Fatal("missing server info")
	}
	tools := call(2, "tools/list", map[string]any{})["tools"].([]any)
	if len(tools) != 8 {
		t.Fatalf("expected 8 tools, got %d", len(tools))
	}
	created := call(3, "tools/call", map[string]any{"name": "material_create", "arguments": map[string]any{"name": "stone"}})
	if created["isError"] != false {
		t.Fatalf("creation failed: %+v", created)
	}
	payload := created["structuredContent"].(map[string]any)
	revision := payload["material"].(map[string]any)["revision"].(string)
	invalid := call(4, "tools/call", map[string]any{"name": "material_edit", "arguments": map[string]any{
		"name": "stone", "expected_revision": revision,
		"operations": []map[string]any{{"op": "set_param", "node": "Missing", "param": "roughness", "value": 0.5}},
	}})
	if invalid["isError"] != true {
		t.Fatalf("invalid edit succeeded: %+v", invalid)
	}
	inspected := call(5, "tools/call", map[string]any{"name": "material_inspect", "arguments": map[string]any{"name": "stone"}})
	if got := inspected["structuredContent"].(map[string]any)["revision"].(string); got != revision {
		t.Fatalf("failed edit changed revision (%s != %s)", got, revision)
	}
	changed := call(6, "tools/call", map[string]any{"name": "material_edit", "arguments": map[string]any{
		"name": "stone", "expected_revision": revision,
		"operations": []map[string]any{{"op": "set_param", "node": "Material", "param": "roughness", "value": 0.5}},
	}})
	if changed["isError"] != false {
		t.Fatalf("valid edit failed: %+v", changed)
	}
	_ = stdin.Close()
	_, _ = io.Copy(io.Discard, reader)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("MCP process failed: %v (stderr=%s)", err, stderr.String())
	}
}
