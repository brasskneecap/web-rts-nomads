# Tileset Editor — SP1: Data-driven tilesets + slice editor

Status: design (approved to spec) · Date: 2026-07-20 · Branch: `UI-Terrain-Editor`

## Context & goal

Terrain sheets are currently **hardcoded** in several places that must stay in
sync: `client/.../game/rendering/terrainTileset.ts` (image imports, per-sheet
tile size, grid count, variant pools, and the slice math in `drawTerrainTile`),
the mirrored walkability logic in `isWalkableGroundTile` (client) and
`pathing.go` (server), and the `TileSheet` string union.

The user wants a **Tileset Editor** so terrain art can be authored, not coded —
in three sub-projects:

- **SP1 (this doc)** — tilesets become editable server-catalog data with a
  visual **slice editor** (tile size / grid / **offset** / spacing), replacing
  the hardcoded config. Foundation for the rest.
- **SP2** (later) — generate individual tiles from arbitrary image regions.
- **SP3** (later) — author per-tile walkability & cliff tagging (pathing reads
  authored flags instead of hardcoded coords).

This spec covers **SP1 only**. Decisions locked in brainstorming:
- Full **data-driven catalog** (like Items/Abilities): CRUD + persistence.
- Placed tiles switch from pixel coords `{sheet, sx, sy}` to **grid indices**
  `{tileset, col, row}`, with automatic migration of existing maps.
- Images are **uploaded in the editor** and stored server-side.

## Non-goals (SP1)

- Per-tile walkability/cliff *authoring* UI (SP3). SP1 keeps the current
  hardcoded walkability, re-keyed to col/row so it still works.
- Carving tiles out of arbitrary images (SP2).
- Changing the Wang auto-tiler behavior. The `tileset` (grass↔dirt) auto-tiling
  keeps working; its coord table is converted from pixels to col/row.

## Data model

### `TilesetDef` (new catalog type)

```ts
interface TilesetDef {
  id: string          // slug, e.g. 'grass-grass-8x8' (stable; referenced by maps)
  name: string        // display label
  image: string       // uploaded PNG key, served at a stable URL
  cols: number        // tiles across
  rows: number        // tiles down
  offsetX: number     // px from the image's left where the first tile begins
  offsetY: number     // px from the top
  tileWidth: number   // source px per tile
  tileHeight: number  // source px per tile (usually == tileWidth)
  spacingX: number    // px gap between adjacent tiles (0 for edge-to-edge)
  spacingY: number
}
// Source rect of tile (col,row):
//   srcX = offsetX + col * (tileWidth  + spacingX)
//   srcY = offsetY + row * (tileHeight + spacingY)
```

Rendering is unchanged in spirit: a tile's source rect is blitted to the map
cell at `cellSize` (smoothing on when downscaling a hi-res source, per the
existing `drawTerrainTile`). The def's tile size is *source* px; on-screen size
stays `cellSize × zoom`.

Fields deferred to later sub-projects (reserved, not built in SP1): per-tile
`walkable`/`cliff` flags (SP3), variant-group tagging (SP3 — SP1 keeps pools in
code).

### `TileInstance` change — pixels → grid indices

```
before:  { x, y, sheet: TileSheet, sx: number, sy: number }
after:   { x, y, tileset: string,  col: number, row: number }
```

`TileCoord` (`{sheet, sx, sy}`) → `{tileset, col, row}`. Placed tiles become
independent of the pixel layout, so re-slicing a tileset (nudging offset / tile
size) never invalidates already-painted tiles — the whole point of the editor.

The `terrain[]` (grass/dirt semantic auto-tile) layer is unchanged.

## Migration

**Existing maps** store `tiles[]` as `{sheet, sx, sy}` where `sx,sy` are 32px
multiples. On load (both server `maps.go` and client `catalog.ts`), a shim
converts legacy entries:

```
tileset = sheet
col = sx / 32   ;  row = sy / 32       // 32 = current logical tile size
```

- Read path accepts both shapes; write path emits only the new shape.
- The wire (grouped) form in `terrainTileGroups.ts` +
  `map_terrain_tile_groups.go` gains the same shim and groups by
  `(tileset, col, row)`.
- The 5 current hardcoded sheets ship as **embedded `TilesetDef`s** with their
  known config, so their ids resolve and every existing map keeps rendering:

  | id | cols×rows | tileWidth | source |
  |----|-----------|-----------|--------|
  | `tileset` (grass↔dirt Wang) | 4×4 | 32 | grass-dirt-elevation-25.png |
  | `grass-grass-25` | 4×4 | 160 | new-grass-grass-elavation-640.png |
  | `dirt-dirt-25` | 4×4 | 160 | new-dirt-dirt-elavation-640.png |
  | `grass-dirt-0` | 4×4 | 32 | grass-dirt-elevation-0.png |
  | `grass-grass-8x8` | 8×8 | 128 | 8x8-grass-grass-1024.png |

  (offsets/spacing 0 for all; tileHeight == tileWidth.)

## Server

New catalog mirroring the item/ability persistence pattern
(`item_persistence.go`, `campaign_persistence.go`):

- **Embedded baseline**: `catalog/tilesets/*.json` (the 5 defs above), embedded
  via `//go:embed`. Their images live under `catalog/tilesets/images/` (copies
  of the current terrain PNGs) so the def is self-contained.
- **Writable overlay**: `TILESET_CATALOG_DIR` env (fallback to the source dir),
  `LoadPersistedTilesetsIntoOverlay()` at startup, `SaveTilesetDef` /
  `DeleteTilesetDef`, `currentTilesetDefs()` merging embedded + overlay. Delete
  guarded: refuse if any map's `tiles[]` still references the tileset.
- **Endpoints**:
  - `GET  /catalog/tilesets` → `{ tilesets: TilesetDef[] }` (already proxied via `/catalog`).
  - `POST /tilesets` (create/update def) → 201; 400 on validation.
  - `DELETE /tilesets/{id}` (guarded).
  - `POST /tilesets/{id}/image` (PNG upload; validate PNG, size cap; stores to
    the images dir; sets `image` on the def). Mirrors item-icon upload.
  - `GET /tilesets/images/{key}` (serve the PNG; no-cache like editor art).
- **Pathing** (`pathing.go`): `isWalkableGroundTile` re-keyed to
  `(tileset, col, row)` — same rule, converted from pixels. Still hardcoded for
  the known tilesets in SP1; becomes def-driven in SP3.

Proxy: `/tilesets` must be added to `vite.config.ts` and the embedded-handler
`apiPrefixes` (like `/items`, `/units`).

## Client

- **`TilesetEditorPanel.vue`** (new toolbar tab `tilesets`, on `EditorShell`):
  - Sidebar: list of tilesets + "New Tileset" + duplicate.
  - Header: name, auto-slugged read-only id (editable while new), Save + Delete.
  - **Image upload** (drag/drop or file picker) → `POST /tilesets/{id}/image`.
  - **Slice controls**: cols, rows, offsetX/Y, tileWidth/Height, spacingX/Y
    (number inputs + optionally drag handles later).
  - **Live overlay canvas**: draws the uploaded image with the slice grid
    overlaid (cell rectangles) so the author drags the numbers and watches the
    grid snap onto the art. Highlights the hovered/first tile.
- **`tilesetEditorApi.ts`**: `saveTileset`, `deleteTileset`, `uploadTilesetImage`.
- **`terrainTileset.ts` refactor → data-driven**: `fetchTilesetDefs()` (from
  `catalog.ts`, GET `/catalog/tilesets`), `initTilesetDefs(defs)` seeding a
  runtime map (like `initBuildingDefs`). `getSheetTileSize`/`getSheetGrid`/
  `getSheetLogicalExtent`/`drawTerrainTile`/`sheetUrls` all read from the def.
  The Wang coord table + variant pools convert to col/row (still in code for
  SP1). `isTerrainTilesetReady()` gates on defs loaded + the `tileset` image.
- **Painting** (`WorldEditorPanel.vue`): the tile picker + `paintAtScreen`
  store `{tileset, col, row}`; the picker renders in the def's grid.
- Load order: `GameClient.start` / editor `onMounted` fetch tileset defs
  alongside the other catalogs.

## Testing

- **Server**: tileset CRUD + overlay round-trip; delete-guard when referenced;
  legacy `tiles[]` migration (sx/sy → col/row) on map load; pathing walkability
  with col/row (a `grass-grass-8x8` row-0 tile is walkable, a cliff tile blocks).
- **Client**: slice-rect math (`col,row + offset/spacing → src rect`); painting
  writes col/row; the picker snaps to the def grid; an existing map with legacy
  tiles still renders (migration shim). Typecheck + build.
- **E2E (manual)**: create a tileset, upload a PNG, align the grid, paint from
  it, confirm units path correctly.

## Rollout / sequencing within SP1

1. Protocol + migration shim (types, wire grouping) — client + server, keep old
   maps rendering.
2. Server tileset catalog + endpoints + embedded defs + image storage.
3. Client `terrainTileset.ts` data-driven refactor + painting col/row.
4. `TilesetEditorPanel.vue` + api + toolbar wiring + slice overlay.
5. Re-key walkability to col/row (client + server), verify pathing.

Each step keeps the build green and existing maps working.
