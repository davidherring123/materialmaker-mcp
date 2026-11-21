// Package integration provides opt-in real-engine tests, separate from hermetic unit tests.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidherring123/materialmaker-mcp/internal/adapter/materialmaker"
	"github.com/davidherring123/materialmaker-mcp/internal/core/material"
)

// TestRealMaterialMakerExport is an opt-in GPU-backed smoke test. It does not
// download or launch anything unless all three environment variables are set:
// MM_SMOKE_GODOT, MM_SMOKE_PROJECT, MM_SMOKE_PTEX. The user supplies the
// executable, trusted Material Maker checkout, and saved material to test.
func TestRealMaterialMakerExport(t *testing.T) {
	godot := os.Getenv("MM_SMOKE_GODOT")
	project := os.Getenv("MM_SMOKE_PROJECT")
	source := os.Getenv("MM_SMOKE_PTEX")
	if godot == "" || project == "" || source == "" {
		t.Skip("set MM_SMOKE_GODOT, MM_SMOKE_PROJECT, MM_SMOKE_PTEX for real GPU-backed export")
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := material.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := workspace.SourcePath("smoke")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	renderer := &materialmaker.Renderer{Binary: godot, Project: project}
	result, err := renderer.Render(ctx, workspace, "smoke", 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("renderer produced no maps: %+v", result)
	}
}
