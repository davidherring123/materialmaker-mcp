// Package materialmaker adapts Material Maker's existing export CLI to the Go core.
// It executes one fixed binary with explicit args (never a shell) and publishes
// complete renders only after confirming the exported maps are decodable.
package materialmaker

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/davidherring123/materialmaker-mcp/internal/core/material"
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

// PreviewResult describes a stable, local screenshot and its exported maps.
// A supporting MCP client can attach the PNG as image content.
type PreviewResult struct {
	Name          string   `json:"name"`
	ImagePath     string   `json:"image_path"`
	Directory     string   `json:"directory"`
	Files         []string `json:"files"`
	RequestedSize int      `json:"requested_size"`
}

func (r *Renderer) Render(ctx context.Context, workspace *material.Workspace, name string, size int) (RenderResult, error) {
	return r.render(ctx, workspace, name, size, nil)
}

// Preview renders fresh PBR maps and the repository's 3D preview rig into one
// private directory. Neither the textures nor the screenshot are published
// until both subprocesses have succeeded and their PNGs were verified.
// This avoids showing an old image of an edited material.
func (r *Renderer) Preview(ctx context.Context, workspace *material.Workspace, name string, size int) (PreviewResult, error) {
	var screenshot string
	result, err := r.render(ctx, workspace, name, size, func(stage string) (string, error) {
		file, err := r.renderScreenshot(ctx, stage, name)
		if err != nil {
			return "", err
		}
		screenshot = file
		return file, nil
	})
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{
		Name: name, ImagePath: filepath.Join(result.Directory, screenshot),
		Directory: result.Directory, Files: result.Files, RequestedSize: size,
	}, nil
}

// afterExport runs against the private render stage and returns the filename
// of an additional artifact. It must not return paths or operate outside stage.
func (r *Renderer) render(ctx context.Context, workspace *material.Workspace, name string, size int, afterExport func(string) (string, error)) (RenderResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	input, generated, err := r.prepareRender(workspace, name, size)
	if err != nil {
		return RenderResult{}, err
	}
	stage, err := os.MkdirTemp(generated, "."+name+"-render-")
	if err != nil {
		return RenderResult{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	output, err := r.exportMaps(ctx, input, stage, size)
	if err != nil {
		return RenderResult{}, err
	}
	files, err := collectRenderFiles(stage, size, output)
	if err != nil {
		return RenderResult{}, err
	}
	if afterExport != nil {
		additional, err := afterExport(stage)
		if err != nil {
			return RenderResult{}, err
		}
		if additional != filepath.Base(additional) || !strings.HasSuffix(additional, ".png") {
			return RenderResult{}, errors.New("invalid generated preview filename")
		}
		files = append(files, additional)
	}
	return publishRender(generated, stage, name, size, files)
}
func (r *Renderer) prepareRender(workspace *material.Workspace, name string, size int) (input, generated string, err error) {
	if size < 16 || size > 2048 {
		return "", "", errors.New("size must be 16-2048")
	}
	if r.Binary == "" || r.Project == "" {
		return "", "", errors.New("material maker requires --godot and --material-maker-project")
	}
	if _, err := os.Stat(r.Binary); err != nil {
		return "", "", fmt.Errorf("godot binary: %w", err)
	}
	if _, err := os.Stat(filepath.Join(r.Project, "project.godot")); err != nil {
		return "", "", fmt.Errorf("material maker checkout: %w", err)
	}
	input, err = workspace.SourcePath(name)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(input); err != nil {
		return "", "", err
	}
	generated, err = workspace.GeneratedRoot()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(generated, 0o755); err != nil {
		return "", "", err
	}
	return input, generated, nil
}
func (r *Renderer) exportMaps(ctx context.Context, input, stage string, size int) ([]byte, error) {
	target := r.Target
	if target == "" {
		target = "Godot/Godot 4 Standard"
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, r.Binary, "--path", r.Project, "--export-material", input, "--target", target, "-o", stage, "--size", fmt.Sprint(size))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("render failed: %w: %s", err, trimLog(out))
	}
	return out, nil
}
func collectRenderFiles(stage string, size int, output []byte) ([]string, error) {
	entries, err := os.ReadDir(stage)
	if err != nil {
		return nil, err
	}
	files := []string{}
	foundPNG := false
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("unexpected file type in render output")
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".png") {
			if err := validateRenderPNG(filepath.Join(stage, entry.Name()), size); err != nil {
				return nil, err
			}
			foundPNG = true
		}
		files = append(files, entry.Name())
	}
	if !foundPNG {
		return nil, fmt.Errorf("export produced no PNG maps: %s", trimLog(output))
	}
	return files, nil
}
func validateRenderPNG(path string, size int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	cfg, decodeErr := png.DecodeConfig(f)
	closeErr := f.Close()
	if decodeErr != nil {
		return fmt.Errorf("invalid PNG %q: %w", filepath.Base(path), decodeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid PNG %q", filepath.Base(path))
	}
	if cfg.Width != size || cfg.Height != size {
		return fmt.Errorf("%s is %dx%d, expected %dx%d; this Material Maker version may ignore --size", filepath.Base(path), cfg.Width, cfg.Height, size, size)
	}
	return nil
}
func publishRender(generated, stage, name string, size int, files []string) (RenderResult, error) {
	if err := material.ValidName(name); err != nil {
		return RenderResult{}, err
	}
	dest := filepath.Join(generated, name)
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
			if rollbackErr := os.Rename(backup, dest); rollbackErr != nil {
				return RenderResult{}, errors.Join(err, fmt.Errorf("restoring previous render: %w", rollbackErr))
			}
		}
		return RenderResult{}, err
	}
	if previous {
		if err := os.RemoveAll(backup); err != nil {
			return RenderResult{}, fmt.Errorf("render published but old backup cleanup failed: %w", err)
		}
	}
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = filepath.Join(dest, file)
	}
	return RenderResult{Name: name, Files: paths, RequestedSize: size, Directory: dest}, nil
}

//go:embed preview_project/*
var previewFiles embed.FS

// stagePreviewProject materializes the trusted preview scene embedded in the
// binary. It is an independent, transient Godot project; it never touches
// a caller's game project or a Material Maker installation.
func stagePreviewProject() (string, func(), error) {
	dir, err := os.MkdirTemp("", "materialmaker-preview-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, name := range []string{"project.godot", "preview.tscn", "preview.gd"} {
		data, err := previewFiles.ReadFile("preview_project/" + name)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return dir, cleanup, nil
}

// renderScreenshot drives the embedded Godot preview rig. It requires albedo,
// normal and ORM maps from one successful export. No user-supplied code or
// external asset path is passed to the preview project's script.
func (r *Renderer) renderScreenshot(ctx context.Context, stage, name string) (string, error) {
	previewProject, cleanup, err := stagePreviewProject()
	if err != nil {
		return "", fmt.Errorf("staging preview project: %w", err)
	}
	defer cleanup()
	paths := map[string]string{}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		for _, kind := range []string{"albedo", "normal", "orm"} {
			if strings.HasSuffix(strings.ToLower(entry.Name()), "_"+kind+".png") {
				if paths[kind] != "" {
					return "", fmt.Errorf("multiple %s maps found", kind)
				}
				paths[kind] = filepath.Join(stage, entry.Name())
			}
		}
	}
	for _, kind := range []string{"albedo", "normal", "orm"} {
		if paths[kind] == "" {
			return "", fmt.Errorf("preview needs a %s map; Material Maker's export profile did not produce one", kind)
		}
	}
	file := name + "_preview.png"
	out := filepath.Join(stage, file)
	previewCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(previewCtx, r.Binary, "--path", previewProject, "--",
		"--albedo="+paths["albedo"], "--normal="+paths["normal"],
		"--orm="+paths["orm"], "--out="+out)
	log, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("preview screenshot failed: %w: %s", err, trimLog(log))
	}
	info, err := os.Lstat(out)
	if err != nil {
		return "", fmt.Errorf("preview screenshot not written: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("preview screenshot must be a regular PNG")
	}
	f, err := os.Open(out)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		return "", fmt.Errorf("preview screenshot is not a complete PNG: %w", err)
	}
	if img.Bounds().Dx() < 1 || img.Bounds().Dy() < 1 {
		return "", errors.New("empty screenshot")
	}
	return file, nil
}

func trimLog(data []byte) string {
	s := string(data)
	if len(s) > 2500 {
		s = s[len(s)-2500:]
	}
	return s
}
