// Package materialmaker adapts Material Maker's existing export CLI to the Go core.
// It executes one fixed binary with explicit args (never a shell) and publishes
// complete renders only after confirming the exported maps are decodable.
package materialmaker

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/davidherring123/materialmaker-mcp/go/internal/core/material"
)

type Renderer struct {
	Binary  string
	Project string
	Target  string
	mu      sync.Mutex
}
type RenderResult struct {
	Name          string   `json:"name"`
	Files         []string `json:"files"`
	RequestedSize int      `json:"requested_size"`
	Directory     string   `json:"directory"`
}

func (r *Renderer) Render(ctx context.Context, workspace *material.Workspace, name string, size int) (RenderResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if size < 16 || size > 2048 {
		return RenderResult{}, errors.New("size must be 16-2048")
	}
	if r.Binary == "" || r.Project == "" {
		return RenderResult{}, errors.New("Material Maker requires --godot and --material-maker-project")
	}
	if _, err := os.Stat(r.Binary); err != nil {
		return RenderResult{}, fmt.Errorf("Godot binary: %w", err)
	}
	if _, err := os.Stat(filepath.Join(r.Project, "project.godot")); err != nil {
		return RenderResult{}, fmt.Errorf("Material Maker checkout: %w", err)
	}
	input, err := workspace.SourcePath(name)
	if err != nil {
		return RenderResult{}, err
	}
	if _, err := os.Stat(input); err != nil {
		return RenderResult{}, err
	}
	generated, err := workspace.GeneratedRoot()
	if err != nil {
		return RenderResult{}, err
	}
	if err := os.MkdirAll(generated, 0o755); err != nil {
		return RenderResult{}, err
	}
	stage, err := os.MkdirTemp(generated, "."+name+"-render-")
	if err != nil {
		return RenderResult{}, err
	}
	defer os.RemoveAll(stage)
	target := r.Target
	if target == "" {
		target = "Godot/Godot 4 Standard"
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, r.Binary, "--path", r.Project, "--export-material", input, "--target", target, "-o", stage, "--size", fmt.Sprint(size))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return RenderResult{}, fmt.Errorf("render failed: %w: %s", err, trimLog(out))
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return RenderResult{}, err
	}
	files := []string{}
	foundPNG := false
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return RenderResult{}, errors.New("unexpected file type in render output")
		}
		path := filepath.Join(stage, entry.Name())
		if strings.EqualFold(filepath.Ext(path), ".png") {
			f, err := os.Open(path)
			if err != nil {
				return RenderResult{}, err
			}
			cfg, decodeErr := png.DecodeConfig(f)
			f.Close()
			if decodeErr != nil || cfg.Width <= 0 || cfg.Height <= 0 {
				return RenderResult{}, fmt.Errorf("invalid PNG %q", entry.Name())
			}
			if cfg.Width != size || cfg.Height != size {
				return RenderResult{}, fmt.Errorf("%s is %dx%d, expected %dx%d; this Material Maker version may ignore --size", entry.Name(), cfg.Width, cfg.Height, size, size)
			}
			foundPNG = true
		}
		files = append(files, entry.Name())
	}
	if !foundPNG {
		return RenderResult{}, fmt.Errorf("export produced no PNG maps: %s", trimLog(out))
	}
	// Back up old exports before publishing, and restore them if publication fails.
	dest := filepath.Join(generated, name)
	if err := material.ValidName(name); err != nil {
		return RenderResult{}, err
	}
	if info, err := os.Lstat(dest); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return RenderResult{}, errors.New("refusing to replace symlinked material output")
	}
	backup := dest + ".backup"
	if _, err := os.Lstat(backup); err == nil {
		return RenderResult{}, fmt.Errorf("stale backup %s: resolve manually", backup)
	} else if !os.IsNotExist(err) {
		return RenderResult{}, err
	}
	previous := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return RenderResult{}, err
		}
		previous = true
	} else if !os.IsNotExist(err) {
		return RenderResult{}, err
	}
	if err := os.Rename(stage, dest); err != nil {
		if previous {
			_ = os.Rename(backup, dest)
		}
		return RenderResult{}, err
	}
	if previous {
		if err := os.RemoveAll(backup); err != nil {
			return RenderResult{}, fmt.Errorf("render published but old backup cleanup failed: %w", err)
		}
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(dest, f)
	}
	return RenderResult{Name: name, Files: paths, RequestedSize: size, Directory: dest}, nil
}
func trimLog(data []byte) string {
	s := string(data)
	if len(s) > 2500 {
		s = s[len(s)-2500:]
	}
	return s
}
