// Command materialmaker-mcp runs the local stdio MCP server. No separate
// user-facing CLI is installed; clients launch this binary directly.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/davidherring123/materialmaker-mcp/internal/adapter/materialmaker"
	"github.com/davidherring123/materialmaker-mcp/internal/agent/mcp"
	"github.com/davidherring123/materialmaker-mcp/internal/app"
	"github.com/davidherring123/materialmaker-mcp/internal/core/material"
)

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("materialmaker-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", ".", "Godot project or material workspace directory")
	mmProject := fs.String("mm-project", "", "existing Material Maker source checkout")
	nodes := fs.String("nodes", "", "Material Maker .mmg node definitions directory")
	godot := fs.String("godot", "", "path to compatible Godot executable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return errors.New("unexpected command: this binary serves MCP over stdio; no standalone CLI subcommands")
	}
	w, err := material.OpenWorkspace(*workspace)
	if err != nil {
		return err
	}
	nodeDir := *nodes
	if nodeDir == "" && *mmProject != "" {
		nodeDir = filepath.Join(*mmProject, "addons", "material_maker", "nodes")
	}
	if nodeDir == "" {
		return errors.New("set --nodes or --mm-project to load the Material Maker node catalog")
	}
	catalog, err := material.LoadCatalog(nodeDir)
	if err != nil {
		return err
	}
	svc := &app.Service{Workspace: w, Catalog: catalog, Renderer: &materialmaker.Renderer{Binary: *godot, Project: *mmProject}}
	return mcp.New(svc).Run(ctx, stdin, stdout)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "materialmaker-mcp:", err)
		os.Exit(1)
	}
}
