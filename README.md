# Material Maker MCP (Go)

A local-first, deterministic [Material Maker](https://github.com/RodZill4/material-maker) material editor for AI agents. The MCP server is implemented in Go; the **material core is independent of MCP** and never makes network requests. Claude, or another MCP client, describes a material and modifies its saved, editable `.ptex` node graph through structured operations rather than regenerating an image.

This is a Go replacement of the forked Python prototype. The Python server, its separate CLI, cookbook, preset generator scripts, web playground, and old test suite have been removed. The small Godot 3D preview scene is embedded in the Go binary to retain the `material_preview` tool. The fork remains MIT-licensed; see `LICENSE` and `NOTICE.md`.

## What it does

- **Eight MCP tools:** `material_list`, `material_catalog`, `material_inspect`, `material_create`, `material_edit`, `material_validate`, `material_render`, `material_preview`.
- **Reference-driven authoring:** begin with a user-provided image or description; optionally clone an existing material in the same project using `material_create({"source_material":"stone"})`. There is **no bundled cookbook**.
- **Deterministic edits:** `material_edit` accepts up to 100 typed graph operations (`set_param`, `add_node`, `remove_node`, `move_node`, `connect`, `disconnect`) scoped to an optional nested graph. All operations apply to an in-memory copy and are published together after validation.
- **Stale-edit protection:** an edit requires the SHA-256 `expected_revision` from `material_inspect`.
- **Texture export:** the `material_render` tool invokes the installed Material Maker/Godot export command with explicit process arguments, stages outputs, and validates PNG dimensions before publishing.
- **3D screenshots:** `material_preview` renders the current maps on a sphere, bevelled cube, and rook. It saves `assets/materials/generated/<name>/<name>_preview.png`, returning the absolute path and (where supported) an inline MCP image.

## Prerequisites

- Go 1.23+ to build.
- A **single existing Material Maker project checkout** with `addons/material_maker/nodes/*.mmg` definitions, and a compatible Godot executable for rendering. This tool does not download either or install a copy per game project.
- A project/workspace directory in which to save materials.

```bash
# From this repository
 go test ./...
 go build -o materialmaker-mcp ./cmd/materialmaker-mcp
```

## Configure Claude Code / another local MCP client

The binary **starts MCP over stdio directly**. There are no separate human-facing material CLI subcommands. Adapt the paths to your machine:

```json
{
  "mcpServers": {
    "materialmaker": {
      "command": "/absolute/path/to/materialmaker-mcp",
      "args": [
        "--workspace", "/absolute/path/to/godot-project",
        "--mm-project", "/absolute/path/to/material-maker-checkout",
        "--godot", "/absolute/path/to/godot"
      ]
    }
  }
}
```

Use `--nodes /path/to/material-maker/addons/material_maker/nodes` if catalog definitions are outside the specified checkout. The **Material Maker CLI** is still an external renderer called by the Go adapter; it is *not* the removed user-facing CLI.

Material assets live in the workspace, not the MCP repository:

```text
my-game/assets/materials/
  source/stone.ptex
  generated/stone/stone_albedo.png
  generated/stone/stone_normal.png
  generated/stone/stone_orm.png
  generated/stone/stone_preview.png
```

### Example workflow

1. Give Claude a texture reference image or describe its color, pattern, physical relief, and target resolution. Use the authoring skill in [`skills/material-authoring/SKILL.md`](skills/material-authoring/SKILL.md).
2. `material_catalog` to discover the actual node types and parameter schemas; `material_create` creates a blank graph or clones a user-authored graph in the workspace.
3. `material_inspect` to obtain the graph and revision; use `material_edit` to make validated changes, then `material_validate`.
4. `material_preview` to render and inspect a new screenshot; `material_render` to export maps without the extra screenshot when desired.
5. Continue editing the same `.ptex`. Keep sources in Git for history and rollback.

## Architecture

```text
cmd/materialmaker-mcp              - stdio MCP entrypoint, installation config only
internal/agent/mcp                 - JSON-RPC tool schemas and transport
internal/app                       - shared, protocol-independent application services
internal/core/material             - graph, catalog, validation, workspace and edits
internal/adapter/materialmaker     - Godot process invocation and bundled 3D preview scene
skills/material-authoring          - prompt/reference-driven authoring guidance
```

The core does not import the MCP layer or depend on Claude. File paths exposed to the model are restricted to validated material names under `assets/materials/` in the selected workspace. The MCP does not upload any files. Only user-selected agent context may be sent to the AI client provider; do not give a client access to unrelated private files.

## Current limitations / safety

- **No live Material Maker GUI bridge**, no embedded Godot editor tab, no browser UI, and no direct mesh/texture import management in Godot. These are future features, not part of this Go replacement.
- No real GPU/Material Maker render smoke test in CI. Current tests exercise the adapter with controlled fake executables. The upstream fixed-size 2048 bake behavior may vary by installed Material Maker revision; this adapter explicitly rejects incorrect PNG dimensions instead of silently downsampling.
- The stdio MCP transport is a minimal implementation, not yet the official Go SDK. Its protocol conformance needs broader client testing.
- Graph transactions are atomic within one process; concurrent independent MCP processes pointed at the same workspace are not coordinated. Run one writer per workspace.
- The preview requires albedo, normal and ORM maps. Material channels such as emission, opacity and Deep Parallax are not exhaustively represented in the screenshot; verify exported maps in the target engine.
- Rendering starts a **local GPU-capable Godot process**. It needs a trusted installed Material Maker source checkout and executable; neither is downloaded automatically.

## License

MIT. Original project by Grayson Chalmers, forked and substantially rewritten; upstream copyright and license are retained.
