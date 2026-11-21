---
name: material-authoring
description: Build and iterate editable Material Maker PBR materials from user-provided visual references or descriptions using deterministic MCP graph operations.
---

# Reference-driven Material Maker material authoring

Use this skill when a user asks to create, match, or revise a texture/material. **Prefer the user's own supplied image, mesh, color palette, or existing `.ptex` to anonymous canned recipes.** There is intentionally no cookbook or static preset library.

## Inputs and decomposition

Read the reference(s) that the MCP client is authorized to access. Do not invent what unseen images contain, pull private images from unrelated locations, or upload files through this material tool. When several references conflict, prioritize explicit user constraints and identify which property is taken from each.

Before authoring, extract:
- **Palette:** dominant colors and their value/saturation spread; which changes are spatial rather than overall.
- **Topology:** continuous crack network, discrete packed cells, parallel grain/streaks, overlapping scattered shapes, woven grid, or smooth broad variation. *Choose generators by topology, not by habit.*
- **Spatial scale:** size and repeat count of the visible pattern, tileability and intended UV density.
- **Physical response:** dielectric/metallic, rough/matte versus glossy, AO, micro-relief versus actual large height variations. Do not infer physical depth from high-contrast pixels alone.
- **Styling limits:** required texture resolution, hard-pixel edges/limited palette versus smooth stylized shading, target game engine, must-have and must-not-have features.

If an image reference is unavailable to the material tool, inspect it through the AI client's permitted image/file facilities, then translate the findings into graph parameters. This MCP tool does **not** accept arbitrary filesystem paths to private images.

## Tool sequence

1. `material_list`/`material_inspect` to find prior **user-owned** materials for reuse. For new work, `material_create` optionally accepts `source_material` (another existing material *in the same workspace*), otherwise create blank.
2. `material_catalog` for exact node types and parameters before adding them. Enums use their **ordinal indexes**, not human-facing labels or internal literal values.
3. Keep the editable `.ptex` as the source of truth. `material_inspect` returns a revision. Use `material_edit` with that `expected_revision` and **small, deliberate batches** of operations. Failed batches do not publish partial changes.
4. `material_validate` and fix errors. Warnings about numeric limits can be intentional but should be acknowledged and visually tested.
5. `material_preview` returns a stable screenshot path and optional image content for review. Inspect the result and compare to the user's reference; do not declare success based only on successful node validation.
6. Use `material_render` when maps, not a 3D screenshot, are needed. Keep all source `.ptex` files versioned and avoid overwriting unrelated assets.

## Graph readability and editability

- Prefer a small, readable graph with clear stages (BaseShape → Detail/Mask → Color / Height → Roughness/Normal → Material).
- Give nodes **descriptive role names** (`MossMask`, `StoneLayout`, `GrainNoise`, `WearColor`, `SurfaceNormal`) instead of anonymous `node_2` or duplicated generic node names.
- Group related stages in Material Maker native `graph` subgraphs when helpful; expose meaningful artist controls for scale, wear, palette, roughness and seed.
- Lay nodes out left to right with short connections. Preserve downstream geometry and random seed when making parameter-only revisions.
- When editing one feature, modify only its parameters/nodes unless a dependency requires another change; tell the user about those dependencies.

## Material/texture heuristics

- Favor smooth noise/Perlin/fBM for broad, soft fields; cellular/Voronoi for packed cells and fractured surfaces; anisotropic/directional noise for wood grain, cloth and brushed metal; Truchet for line/circuit/weave patterns. Search the actual catalog rather than assuming any node exists.
- Recolor **albedo** using its color ramp; don't accidentally recolor grayscale height/roughness masks.
- For weathering, use an explicit base-plus-overlay mask. In Material Maker's blend semantics, effective overlay opacity depends on the blend's `amount` *and* port-2 mask. A mid-gray mask is not a hard cutout. Use separate hard coverage masks and color ramps where needed.
- Normal/height should follow physically plausible surface structure, not arbitrary independent noise. Historically, some Material Maker headless revisions produced flat normals from buffered `normal_map` mode (`param4=1`); try the direct mode (`param4=0`) for analytic sources and inspect the result rather than assuming the issue is universally fixed.
- Some buffer/compute-driven nodes that validate correctly may still render black in headless export; when that happens try a simpler analytic node before retrying endlessly.
- For intentionally crisp pixel textures, design at the visual grid scale, avoid excessive fine detail, limit the palette where appropriate, and prefer nearest-neighbor sampling over smoothed downsampling. Rendering at 64×64 does not automatically create good pixel art.
- Ensure tiling edges match and preview the pattern at the intended physical repeat density, not only magnified on a flat texture.

## Quality gate

Before presenting a finished material, check that the requested shape, palette, scale, surface response and tileability match the reference; all required output maps exist and decode; normal/ORM/height channels are not accidentally flat; and the material reads correctly on **lit 3D geometry**. Preview lighting can exaggerate or understate gloss, so verify in the target game engine when material response matters. Be honest when the preview rig cannot show an exported channel such as emission or translucency.

**Don't confuse a generated preview PNG with the editable source.** Always retain the `.ptex` graph and give the user the preview path for inspection.
