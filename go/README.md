# Material Maker MCP — Go foundation

A new, experimental Go implementation alongside the original Python prototype. The original code remains untouched. **The domain core does not import MCP and can be reused by a CLI, Godot editor plugin, or other client.**

This first milestone supports workspace-scoped `.ptex` storage, a Material Maker node catalog, non-destructive template creation, graph inspection, deterministic multi-operation edits, validation, a Material Maker CLI export adapter, and a lightweight **stdio MCP server** with eight tools. There is no model generation or cloud service. Graph authoring is performed by the client using structured edits.

## Architecture

```text
cmd/materialmaker-mcp -> internal/hostcmd ----> internal/app
                               |                   |
                         internal/agent/mcp        +--> internal/core/material
                                                   +--> internal/adapter/materialmaker
```

The core is pure Go and has no protocol or editor dependencies. App services are called identically from CLI and MCP. The Material Maker adapter owns subprocess execution and render publication. Version 0.1 uses a dependency-free MCP stdio transport; replacing it with the official Go MCP SDK later won't affect the core.

## Build and run

```bash
cd go
go test ./...
go build -o materialmaker-mcp ./cmd/materialmaker-mcp

# One shared Material Maker checkout and Godot binary, any number of project workspaces:
./materialmaker-mcp --workspace /path/to/my-godot-project \
  --mm-project /path/to/material-maker \
  --godot /path/to/godot \
  --preview-project ../src/mm_mcp/preview_project \
  --templates ../cookbook \
  mcp
```

Or `list`, `catalog [substring]`, `create NAME [CATEGORY TEMPLATE]`, `inspect NAME`, `validate NAME`, `edit NAME operations.json REVISION`, `render NAME [SIZE]`, and `preview NAME [SIZE]`. The executable writes only under `assets/materials/{source,generated}` within the chosen workspace. Client configuration should launch it with the `mcp` subcommand via stdio. MCP tools are `material_list`, `material_catalog`, `material_inspect`, `material_create`, `material_edit`, `material_validate`, `material_render`, and `material_preview`. A `material_edit` call requires the revision from `material_inspect`.

Example deterministic operations file:

```json
[
  {"op":"set_param", "node":"Material", "param":"roughness", "value":0.7},
  {"op":"move_node", "node":"Material", "x":400, "y":100}
]
```

Supported operations: `set_param`, `add_node`, `remove_node`, `move_node`, `connect`, `disconnect`; use `scope:"subgraph_name"` for a nested graph, or `scope:"outer/inner"` for deeper nesting. All changes are applied to an in-memory clone; on validation failure **nothing is saved**. Snapshot the existing source graph in Git for version history.

## Intentional restrictions

- Material names are validated; the client cannot select arbitrary file paths.
- Pre-existing symlinks in managed workspace paths are rejected. Export runs a configured local binary directly with `exec.CommandContext`, not through a shell.
- Render results are staged and checked before replacing the previous output. A 180-second process timeout limits hanging renders.
- The renderer requires **actual** output dimensions to match `--size`. Older Material Maker versions that ignore `--size` fail explicitly rather than silently returning huge images. Direct low-resolution rendering is a compatibility requirement to test against your installed Material Maker version.
- CLI and MCP process locking is per process. Two processes editing the same project simultaneously are **not** yet safe. Do not start multiple writers against one workspace.
- No live-GUI socket bridge, automatic Godot `.tres` importer, node canvas, preview server, cross-platform smoke test, or official SDK transport is included yet. These are follow-up milestones.
- Keep your Python prototype until parity tests prove the Go implementation meets your needs.

## Material screenshots

Run `materialmaker-mcp --workspace /path/to/game --mm-project /path/to/material-maker --godot /path/to/godot --preview-project /path/to/this-repo/src/mm_mcp/preview_project preview stone 256`, or call the `material_preview` MCP tool with `{ "name": "stone", "size": 256 }`. The tool first exports fresh maps, then uses the repository's existing Godot preview rig to render a lit sphere, rounded cube, and rook. It writes a deterministic screenshot path:

`assets/materials/generated/stone/stone_preview.png`

The result includes an absolute `image_path` for local file tools and an inline MCP PNG image attachment (when 8 MiB or smaller) so compatible AI clients can show/inspect it without searching the directory. The image is a Godot preview, **not a capture of the Material Maker application's window**. It uses the same source asset and no model-based image generation. The source graph stays unchanged. Both maps and screenshot are staged before replacing the previous set; failed previews leave the previous output untouched.

`--preview-project` points to the existing `src/mm_mcp/preview_project` shipped by this repository; it is a trusted host-side path rather than an agent-provided argument. Screenshot generation is unavailable until that flag and a compatible GUI-capable Godot executable are configured. It does **not** work with Godot's `--headless` mode. An actual GPU-backed rendering smoke test remains necessary.
