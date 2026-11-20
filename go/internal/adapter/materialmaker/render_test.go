package materialmaker

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/davidherring123/materialmaker-mcp/go/internal/core/material"
)

func TestRenderSuccessAndFailedRenderPreservesPriorOutputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake renderer fixture")
	}
	root := t.TempDir()
	w, err := material.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	src, err := w.SourcePath("stone")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{"nodes":[],"connections":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "maker")
	_ = os.MkdirAll(project, 0755)
	_ = os.WriteFile(filepath.Join(project, "project.godot"), []byte(""), 0600)
	pngPath := filepath.Join(root, "tile.png")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Setenv("FAKE_PNG", pngPath)
	bin := filepath.Join(root, "fake-godot.sh")
	script := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-o" ]; then shift; out="$1"; fi
 shift
done
if [ "$FAIL_RENDER" = "1" ]; then exit 1; fi
cp "$FAKE_PNG" "$out/stone_albedo.png"
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	r := &Renderer{Binary: bin, Project: project}
	first, err := r.Render(context.Background(), w, "stone", 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != 1 {
		t.Fatalf("wrong output count: %+v", first)
	}
	t.Setenv("FAIL_RENDER", "1")
	if _, err := r.Render(context.Background(), w, "stone", 16); err == nil {
		t.Fatal("failed subprocess reported success")
	}
	if _, err := os.Stat(first.Files[0]); err != nil {
		t.Fatalf("previous good output was lost: %v", err)
	}
}
