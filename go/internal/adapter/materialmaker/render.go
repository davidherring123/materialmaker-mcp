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
	// PreviewProject points to the trusted repository preview rig, never an agent-provided path.
	PreviewProject string
	mu      sync.Mutex
}
type RenderResult struct {
	Name          string   `json:"name"`
	Files         []string `json:"files"`
	RequestedSize int      `json:"requested_size"`
	Directory     string   `json:"directory"`
}

// PreviewResult returns a stable screenshot path and all resulting files.
type PreviewResult struct {
    Name string `json:"name"`
    ImagePath string `json:"image_path"`
    Directory string `json:"directory"`
    Files []string `json:"files"`
    RequestedSize int `json:"requested_size"`
}
func (r *Renderer) Render(ctx context.Context, workspace *material.Workspace, name string, size int) (RenderResult, error) {
    return r.render(ctx,workspace,name,size,nil)
}
// Preview renders fresh maps and a 3D screenshot in a private staging directory,
// then publishes the complete result. Failed previews preserve previous output.
func (r *Renderer) Preview(ctx context.Context, workspace *material.Workspace, name string, size int) (PreviewResult,error) {
    var screenshot string
    result,err:=r.render(ctx,workspace,name,size,func(stage string)(string,error){
        file,err:=r.renderScreenshot(ctx,stage,name)
        if err!=nil{return "",err}
        screenshot=file
        return file,nil
    })
    if err!=nil{return PreviewResult{},err}
    return PreviewResult{Name:name,ImagePath:filepath.Join(result.Directory,screenshot),Directory:result.Directory,Files:result.Files,RequestedSize:size},nil
}
func (r *Renderer) render(ctx context.Context,workspace *material.Workspace,name string,size int,afterExport func(string)(string,error))(RenderResult,error){
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
    if afterExport!=nil {
        extra,err:=afterExport(stage)
        if err!=nil {return RenderResult{},err}
        if extra!=filepath.Base(extra)||!strings.HasSuffix(extra,".png") {return RenderResult{},errors.New("invalid preview filename")}
        files=append(files,extra)
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
// renderScreenshot drives the existing Godot preview scene with staged PBR maps.
func (r *Renderer) renderScreenshot(ctx context.Context,stage,name string)(string,error){
    if r.PreviewProject=="" {return "",errors.New("preview requires --preview-project pointing to src/mm_mcp/preview_project")}
    if _,err:=os.Stat(filepath.Join(r.PreviewProject,"project.godot"));err!=nil{return "",fmt.Errorf("preview project: %w",err)}
    paths:=map[string]string{}
    entries,err:=os.ReadDir(stage)
    if err!=nil{return "",err}
    for _,entry:=range entries {
        if entry.IsDir()||entry.Type()&os.ModeSymlink!=0 {continue}
        for _,kind:=range []string{"albedo","normal","orm"} {
            if strings.HasSuffix(strings.ToLower(entry.Name()),"_"+kind+".png"){
                if paths[kind]!="" {return "",fmt.Errorf("multiple %s maps",kind)}
                paths[kind]=filepath.Join(stage,entry.Name())
            }
        }
    }
    for _,kind:=range []string{"albedo","normal","orm"} {
        if paths[kind]=="" {return "",fmt.Errorf("preview requires a %s map",kind)}
    }
    file:=name+"_preview.png"
    output:=filepath.Join(stage,file)
    timeoutCtx,cancel:=context.WithTimeout(ctx,60*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(timeoutCtx,r.Binary,"--path",r.PreviewProject,"--",
      "--albedo="+paths["albedo"],"--normal="+paths["normal"],"--orm="+paths["orm"],"--out="+output)
    log,err:=cmd.CombinedOutput()
    if err!=nil{return "",fmt.Errorf("preview screenshot failed: %w: %s",err,trimLog(log))}
    info,err:=os.Lstat(output)
    if err!=nil{return "",fmt.Errorf("preview screenshot not written: %w",err)}
    if !info.Mode().IsRegular(){return "",errors.New("preview screenshot must be a regular PNG")}
    f,err:=os.Open(output)
    if err!=nil{return "",err}
    defer f.Close()
    img,err:=png.Decode(f)
    if err!=nil{return "",fmt.Errorf("invalid screenshot: %w",err)}
    if img.Bounds().Dx()<1||img.Bounds().Dy()<1{return "",errors.New("empty screenshot")}
    return file,nil
}
func trimLog(data []byte) string {
	s := string(data)
	if len(s) > 2500 {
		s = s[len(s)-2500:]
	}
	return s
}
