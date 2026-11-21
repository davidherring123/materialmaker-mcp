// Package hostcmd provides a CLI over app.Service. MCP is one of its transports.
package hostcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/davidherring123/materialmaker-mcp/go/internal/adapter/materialmaker"
	"github.com/davidherring123/materialmaker-mcp/go/internal/agent/mcp"
	"github.com/davidherring123/materialmaker-mcp/go/internal/app"
	"github.com/davidherring123/materialmaker-mcp/go/internal/core/material"
)

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("materialmaker-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", ".", "Godot project or material workspace directory")
	mmProject := fs.String("mm-project", "", "Material Maker source checkout (one global installation)")
	nodeDir := fs.String("nodes", "", "directory of Material Maker .mmg node definitions")
	templates := fs.String("templates", "", "path to cookbook templates (optional)")
	godot := fs.String("godot", "", "Godot executable used to run Material Maker's export CLI")
    previewProject:=fs.String("preview-project","","trusted Godot preview rig directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("expected: mcp | list | catalog [query] | inspect NAME | create NAME [CATEGORY TEMPLATE] | validate NAME | edit NAME OPS.json REVISION | render NAME [SIZE] | preview NAME [SIZE]")
	}
	w, err := material.OpenWorkspace(*workspace)
	if err != nil {
		return err
	}
	resolvedNodeDir := *nodeDir
	if resolvedNodeDir == "" && *mmProject != "" {
		resolvedNodeDir = filepath.Join(*mmProject, "addons", "material_maker", "nodes")
	}
	cat := material.Catalog{}
	if resolvedNodeDir != "" {
		cat, err = material.LoadCatalog(resolvedNodeDir)
		if err != nil {
			return err
		}
	}
	svc := &app.Service{Workspace: w, Catalog: cat, Templates: *templates, Renderer: &materialmaker.Renderer{Binary: *godot, Project: *mmProject, PreviewProject:*previewProject}}
	printJSON := func(v any) error { return json.NewEncoder(stdout).Encode(v) }
	argsRequired := func(n int) error {
		if len(rest) < n {
			return fmt.Errorf("%s requires %d argument(s)", rest[0], n-1)
		}
		return nil
	}
	switch rest[0] {
	case "mcp":
		return mcp.New(svc).Run(ctx, stdin, stdout)
	case "list":
		v, e := svc.List()
		if e != nil {
			return e
		}
		return printJSON(v)
	case "catalog":
		query := ""
		if len(rest) > 1 {
			query = rest[1]
		}
		return printJSON(svc.CatalogSearch(query, 100))
	case "inspect":
		if e := argsRequired(2); e != nil {
			return e
		}
		v, e := svc.Inspect(rest[1])
		if e != nil {
			return e
		}
		return printJSON(v)
	case "validate":
		if e := argsRequired(2); e != nil {
			return e
		}
		v, e := svc.Validate(rest[1])
		if e != nil {
			return e
		}
		return printJSON(v)
	case "create":
		if e := argsRequired(2); e != nil {
			return e
		}
		category, template := "", ""
		if len(rest) > 2 {
			category = rest[2]
		}
		if len(rest) > 3 {
			template = rest[3]
		}
		v, p, e := svc.Create(rest[1], category, template)
		if e != nil {
			return fmt.Errorf("%w (problems: %v)", e, p)
		}
		return printJSON(v)
	case "edit":
		if e := argsRequired(4); e != nil {
			return e
		}
		raw, e := os.ReadFile(rest[2])
		if e != nil {
			return e
		}
		var ops []material.Operation
		if e := json.Unmarshal(raw, &ops); e != nil {
			return e
		}
		v, p, e := svc.Edit(rest[1], rest[3], ops)
		if e != nil {
			return fmt.Errorf("%w (problems: %v)", e, p)
		}
		return printJSON(v)
    case "preview":
        if e:=argsRequired(2);e!=nil{return e}
        size:=256
        if len(rest)>2{var parseErr error;size,parseErr=strconv.Atoi(rest[2]);if parseErr!=nil{return parseErr}}
        v,e:=svc.Preview(ctx,rest[1],size)
        if e!=nil{return e}
        return printJSON(v)
	case "render":
		if e := argsRequired(2); e != nil {
			return e
		}
		size := 256
		if len(rest) > 2 {
			var parseErr error
			size, parseErr = strconv.Atoi(rest[2])
			if parseErr != nil {
				return parseErr
			}
		}
		v, e := svc.Render(ctx, rest[1], size)
		if e != nil {
			return e
		}
		return printJSON(v)
	default:
		return fmt.Errorf("unknown command %q", rest[0])
	}
}
