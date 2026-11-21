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

func TestPreviewCreatesStableScreenshotAndPreservesOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" { t.Skip("POSIX fake renderer fixture") }
	root := t.TempDir()
	w, err := material.OpenWorkspace(root)
	if err != nil { t.Fatal(err) }
	path, err := w.SourcePath("granite")
	if err != nil { t.Fatal(err) }
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, []byte("{\"nodes\":[],\"connections\":[]}"), 0600); err != nil { t.Fatal(err) }
	for _, dir := range []string{"maker", "preview-rig"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil { t.Fatal(err) }
		if err := os.WriteFile(filepath.Join(root, dir, "project.godot"), []byte(""), 0600); err != nil { t.Fatal(err) }
	}
	pngPath := filepath.Join(root, "source.png")
	f, err := os.Create(pngPath)
	if err != nil { t.Fatal(err) }
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, G: 128, A: 255})
	if err := png.Encode(f, img); err != nil { t.Fatal(err) }
	if err := f.Close(); err != nil { t.Fatal(err) }
	t.Setenv("FAKE_PNG", pngPath)
	bin := filepath.Join(root, "fake-godot.sh")
	script := `#!/bin/sh
stage=""
preview=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then shift; stage="$1"; fi
  case "$1" in --out=*) preview="${1#--out=}";; esac
  shift
done
if [ -n "$preview" ]; then
  if [ "$FAIL_PREVIEW" = "1" ]; then exit 7; fi
  cp "$FAKE_PNG" "$preview"
else
  cp "$FAKE_PNG" "$stage/granite_albedo.png"
  cp "$FAKE_PNG" "$stage/granite_normal.png"
  cp "$FAKE_PNG" "$stage/granite_orm.png"
fi
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil { t.Fatal(err) }
	r := &Renderer{Binary: bin, Project: filepath.Join(root, "maker"), PreviewProject: filepath.Join(root, "preview-rig")}
	result, err := r.Preview(context.Background(), w, "granite", 16)
	if err != nil { t.Fatal(err) }
	wanted := filepath.Join(root, "assets", "materials", "generated", "granite", "granite_preview.png")
	if result.ImagePath != wanted { t.Fatalf("unexpected screenshot path: %s", result.ImagePath) }
	if len(result.Files) != 4 { t.Fatalf("unexpected screenshot/map file count: %+v", result.Files) }
	if _, err := os.Stat(wanted); err != nil { t.Fatal(err) }
	t.Setenv("FAIL_PREVIEW", "1")
	if _, err := r.Preview(context.Background(), w, "granite", 16); err == nil { t.Fatal("expected preview failure") }
	if _, err := os.Stat(wanted); err != nil { t.Fatalf("last good screenshot lost: %v", err) }
	if _, err := os.Stat(filepath.Join(result.Directory, "granite_orm.png")); err != nil { t.Fatalf("last good map lost: %v", err) }
}

func TestPreviewMissingPBRMapsDoesNotPublish(t *testing.T) {
	if runtime.GOOS == "windows" { t.Skip("POSIX fake renderer fixture") }
	root := t.TempDir()
	w, err := material.OpenWorkspace(root)
	if err != nil { t.Fatal(err) }
	src, err := w.SourcePath("stone")
	if err != nil { t.Fatal(err) }
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(src, []byte("{\"nodes\":[],\"connections\":[]}"), 0600); err != nil { t.Fatal(err) }
	for _, name := range []string{"maker", "preview-rig"} {
		if err := os.MkdirAll(filepath.Join(root,name), 0755); err != nil { t.Fatal(err) }
		if err := os.WriteFile(filepath.Join(root,name,"project.godot"), nil, 0600); err != nil { t.Fatal(err) }
	}
	pngPath := filepath.Join(root, "source.png")
	f, err := os.Create(pngPath)
	if err != nil { t.Fatal(err) }
	if err := png.Encode(f, image.NewRGBA(image.Rect(0,0,16,16))); err != nil { t.Fatal(err) }
	if err := f.Close(); err != nil { t.Fatal(err) }
	t.Setenv("FAKE_PNG", pngPath)
	fake := filepath.Join(root,"fake-godot.sh")
	script := `#!/bin/sh
stage=""
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-o" ]; then shift; stage="$1"; fi
 shift
done
cp "$FAKE_PNG" "$stage/stone_albedo.png"
`
	if err := os.WriteFile(fake, []byte(script), 0755); err != nil { t.Fatal(err) }
	r := &Renderer{Binary:fake,Project:filepath.Join(root,"maker"), PreviewProject:filepath.Join(root,"preview-rig")}
	if _, err := r.Preview(context.Background(),w,"stone",16); err == nil { t.Fatal("expected missing normal/orm map failure") }
	generated, err := w.GeneratedRoot()
	if err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(generated,"stone")); !os.IsNotExist(err) { t.Fatalf("partial output published: %v", err) }
}
