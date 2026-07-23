# Smart Cliff Tool — Design Spec

**Status:** design (awaiting review)
**Date:** 2026-07-23

## Goal

Add a Warcraft 3-style **smart cliff painter** to the world editor's Paint
section. The user selects a `-25` elevation tileset and a Cliff brush, then
**left-drags to raise** a single-level plateau or **right-drags to lower** it.
The correct cliff tile for every affected cell (edges, outer/inner corners) is
selected automatically and neighbors are fixed up on every edit, because the
whole cliff layer is recomputed from an elevation grid. Includes **walkable
ramps** between levels.

## Decisions (settled during brainstorming)

- **Single elevation level**: a cell is either raised (plateau top) or ground.
- **Ramps included**: walkable slopes so units can traverse between levels.
- **Brush**: left-drag raises, right-drag lowers (matches the existing
  right-click-to-erase convention in the tile painter).
- **Tile source — approach C**: the structured cliff atlas is **generated
  programmatically** from the existing `-25` scene art (cut the usable pieces —
  rock face, grassy lips, corners — then mirror/rotate/composite into the
  canonical slot layout). No new artwork required from the user. Slots the
  scene can't supply cleanly (e.g. inner corners) get synthesized and may need
  touch-up; those are reviewed at a checkpoint.
- **Auto-tile set**: the 13-piece subset (flat top, 4 straight walls, 4 outer
  corners, 4 inner corners) plus ramp tiles. Not the full 47-blob; complex
  thin shapes are approximated, which is fine for plateau-shaped cliffs.
- **Shared layout**: all `-25` sheets already place cliffs in the same cells,
  so one canonical atlas layout + one auto-tiler serves every terrain.
- **Phasing**: (1) cliffs, then (2) ramps.

## Architecture

### Elevation grid as the single source of truth

Cliffs are **not** painted tile-by-tile into `tiles[]`. Instead the map stores
a sparse **elevation grid** and a pure function derives the cliff tiles from
it. This mirrors the existing terrain system exactly (`terrain[]` +
`defaultTile` → `computeWangMask` → grass/dirt tiles), so it introduces no new
architectural pattern and every edit recomputes cleanly (no stale corners).

New `MapConfig` fields (client `mapConfig.ts` + server `protocol.MapConfig`,
kept in sync):

- `elevation: GridCoord[]` — the set of raised cells (single level).
- `ramps: GridCoord[]` — cells marked as ramps (phase 2). Facing is inferred
  from which orthogonal side steps down to ground.
- `cliffTileset: string` — the `-25` tileset id whose atlas supplies cliff
  tiles (e.g. `grass-grass-elevation-25`).

All three are optional/absent on maps with no cliffs (backward compatible).

### Canonical cliff atlas

The generated (and any future hand-authored) cliff atlas uses a **fixed slot
layout** so the auto-tiler can index it by config. Required slots:

| Group | Slots | Meaning |
|-------|-------|---------|
| Interior | `FLAT` | plateau top; matches the sheet's terrain |
| Straight walls | `N`, `E`, `S`, `W` | `S` is the tall rock face; `N/E/W` are grassy lips |
| Outer corners | `NE`, `NW`, `SE`, `SW` | convex plateau corners |
| Inner corners | `NEi`, `NWi`, `SEi`, `SWi` | concave corners (notches) |
| Ramps (phase 2) | `RAMP_S`, `RAMP_N`, `RAMP_E`, `RAMP_W` + side transitions | walkable slopes |

The exact grid dimensions and `(col,row)` of each slot are defined by a
**template** committed alongside the generator (`cliffAtlasLayout`), shared by
client, server, and the generator. Phase 1 needs the 13 cliff slots; phase 2
adds the ramp slots (the authored/generated sheet grows accordingly — a
tileset def is just cols/rows, so a non-4×4 sheet is fine).

### Atlas generation (approach C)

A build-time/offline generator (`tools/cliffatlas` Go program, invoked when
`-25` art changes) reads a `-25` source scene and writes a canonical cliff
atlas PNG + updates the tileset def:

1. **Locate source pieces** in the scene by the known cliff geometry (the
   shared `-25` layout): the flat interior cell, a straight rock-face segment,
   the grassy-lip segments, and corner segments.
2. **Normalize to the canonical slots**: crop each piece to one tile; derive
   the 4 wall directions and 4 outer corners by mirroring/rotating the cleanest
   source of each; synthesize inner corners by compositing lip pieces.
3. **Seam-check**: verify each slot tiles with its valid neighbors; flag slots
   that don't for touch-up.
4. **Output**: `<id>-cliffatlas.png` in the catalog images dir + a def with the
   canonical slot geometry.

**Checkpoint:** the first implementation task produces this atlas for
`grass-grass-elevation-25` and we review it before building the rest. If
extraction quality is unacceptable for specific slots, we either hand-touch
those slots or fall back to approach B (map raw scene tiles) for them. A
**placeholder atlas** (flat colored tiles labeled per slot) lets the tool and
auto-tiler be developed/tested independently of art quality.

### Auto-tiler

Pure function `cliffTileAt(elevation, x, y) -> TileCoord | null`:

- Ground cell (not raised, no raised neighbor contributing an edge) → `null`
  (base terrain shows through).
- Raised cell → choose the slot from its 8-neighbor raised/not pattern using
  the standard 13-piece blob lookup (edges from the 4 orthogonal neighbors;
  outer/inner corners from the diagonal neighbors when the adjacent edges
  agree). The exact 256→slot lookup table lives in the implementation and is
  mirrored client/server.
- Editing a cell recomputes that cell **and its 8 neighbors** (corners depend
  on diagonals), which is the "fix the tiles around it" behavior — free,
  because it is all derived from the grid.

### Brush & UX (world editor Paint section)

- New **Cliff** tool in the tool picker.
- Options: a **cliff-tileset dropdown** (the `-25` sheets) bound to
  `cliffTileset`; a **Raise / Ramp** sub-mode toggle (phase 2 adds Ramp).
- **Left-drag**: add the dragged cells to `elevation` (raise). **Right-drag**:
  remove them (lower). Each stroke updates `elevation` then the render/pathing
  layers recompute. Brush size applies (reuse the existing brush-size control).
- Ramp sub-mode (phase 2): click a cliff-edge cell to toggle it in `ramps`.

### Rendering

`drawAutoTiledTerrain` (or a sibling pass) gains a **cliff layer** drawn after
the base ground: for each cell, `cliffTileAt(...)` → `drawTerrainTile` from the
cliff atlas. Because the atlas tile already contains the terrain texture on its
top portion, the cliff tile fully overrides the base cell it occupies. Used by
in-game render, minimap, and both editors (all already share
`drawAutoTiledTerrain`).

### Walkability (client + server, mirrored)

Extends the existing `isTerrainCellBlocked` (client) / `addTerrainBlocks`
(server):

- Plateau **top** (raised interior + walkable edge lips): walkable.
- Cliff **faces/edges** that represent a vertical drop (the `S` face and the
  wall portions of corners): **block**.
- **Ramps** (phase 2): walkable (units traverse levels).
- Ground: unchanged.

The block/walk decision is derived from the same `cliffTileAt` slot, so client
and server agree by construction.

## Phasing

**Phase 1 — Cliffs (the core):**
1. Atlas generator + canonical layout; produce grass cliff atlas; **review
   checkpoint**. Placeholder atlas fallback available.
2. `MapConfig` fields (`elevation`, `cliffTileset`) client + server + protocol.
3. Auto-tiler (`cliffTileAt`) + 13-piece lookup, mirrored client/server.
4. Cliff render layer.
5. Walkability (client `isTerrainCellBlocked` + server `addTerrainBlocks`).
6. Cliff brush (tool + dropdown + raise/lower) wired to `elevation`.

**Phase 2 — Ramps:**
7. Ramp atlas slots + generation.
8. `ramps` field + Ramp sub-mode brush.
9. Ramp render + walkable traversal.

## Out of scope (v1)

- Multiple stacked cliff levels.
- Full 47-blob correctness for arbitrary thin/diagonal shapes.
- Per-terrain cliff art differences beyond swapping the `-25` sheet.
- Hand-authored atlases (approach A) — the generator (C) is the v1 source;
  authored atlases can drop into the same canonical layout later with no code
  change.

## Backward compatibility

Maps without `elevation`/`ramps`/`cliffTileset` render and path exactly as
today. The cliff layer is additive.
