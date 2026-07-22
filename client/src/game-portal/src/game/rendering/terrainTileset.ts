import type { TerrainTile, TerrainType, TileCoord, TileInstance, TilesetDef } from '../network/protocol'

export type { TileCoord }

// Data-driven terrain tileset registry (Tileset Editor plan). Tileset defs
// are server-authored (GET /catalog/tilesets) and initialized once at
// startup via initTilesetDefs — see GameClient.start(). Each def's `image`
// field is a key resolved against GET /tilesets/images/{key}. Tile
// coordinates stored in map data (TileCoord: {tileset, col, row}) are
// resolved against the matching def's grid geometry (offsetX/offsetY,
// tileWidth/tileHeight, spacingX/spacingY) at draw time — no more hardcoded
// pixel sx/sy or bundled PNG imports.

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export function tilesetImageUrl(def: TilesetDef): string {
  return `${API_BASE}/tilesets/images/${def.image}`
}

const tilesetsById: Record<string, TilesetDef> = {}
const images: Record<string, HTMLImageElement> = {}

// initTilesetDefs populates the registry and kicks off image loads for any
// newly-seen def. Safe to call more than once (e.g. a future catalog
// reload) — existing images are not re-requested.
export function initTilesetDefs(defs: TilesetDef[]): void {
  for (const def of defs) {
    tilesetsById[def.id] = def
    if (!images[def.id]) {
      const img = new Image()
      img.src = tilesetImageUrl(def)
      images[def.id] = img
    }
  }
}

export function getTilesetDef(id: string): TilesetDef | undefined {
  return tilesetsById[id]
}

export function getTilesetImage(id: string): HTMLImageElement | null {
  const img = images[id]
  if (!img || !img.complete || img.naturalWidth === 0) return null
  return img
}

export function onTilesetReady(id: string, cb: () => void): void {
  const img = images[id]
  if (!img) return
  if (img.complete && img.naturalWidth > 0) {
    cb()
    return
  }
  img.addEventListener('load', cb, { once: true })
}

export function isTerrainTilesetReady(): boolean {
  return getTilesetDef('tileset') !== undefined && getTilesetImage('tileset') !== null
}

// Fallback grid geometry used when a tileset's def hasn't loaded yet (e.g.
// the tile picker rendering before the catalog fetch resolves). Matches the
// stock 4×4 Wang sheets' original 32px logical tile size.
const FALLBACK_TILE_SIZE = 32
const FALLBACK_GRID = 4

// Per-tileset source tile size in pixels, derived from the loaded def.
export function getSheetTileSize(tilesetId: string): number {
  const def = getTilesetDef(tilesetId)
  return def ? def.tileWidth : FALLBACK_TILE_SIZE
}

// Tiles per row/column for a tileset, derived from the loaded def.
export function getSheetGrid(tilesetId: string): number {
  const def = getTilesetDef(tilesetId)
  return def ? def.cols : FALLBACK_GRID
}

// Logical extent of a tileset (grid tiles × tile size). The tile picker
// renders and snaps in this space.
export function getSheetLogicalExtent(tilesetId: string): number {
  return getSheetGrid(tilesetId) * getSheetTileSize(tilesetId)
}

// Builds a pool of every tile in a cols×rows sheet — for sheets that are a
// full grid of interchangeable tiles of a single terrain type.
function fullSheetVariantPool(tileset: string, cols: number, rows: number): TileCoord[] {
  const pool: TileCoord[] = []
  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < cols; col++) pool.push({ tileset, col, row })
  }
  return pool
}

// Tilesets that support randomized "variant" painting: a pool of
// interchangeable tiles (col/row) the editor scatters across painted cells so
// a field reads as varied rather than one repeating stamp. The flat (-0)
// elevation sheets are full 4×4 grids of one terrain type, so every tile is an
// interchangeable variant.
const TILESET_VARIANT_POOLS: Record<string, ReadonlyArray<TileCoord>> = {
  'corrupt-corrupt-elevation-0': fullSheetVariantPool('corrupt-corrupt-elevation-0', 4, 4),
  'dirt-dirt-elevation-0': fullSheetVariantPool('dirt-dirt-elevation-0', 4, 4),
  'grass-grass-elevation-0': fullSheetVariantPool('grass-grass-elevation-0', 4, 4),
  'snow-snow-elevation-0': fullSheetVariantPool('snow-snow-elevation-0', 4, 4),
}

export function getSheetVariantPool(tilesetId: string): ReadonlyArray<TileCoord> | null {
  return TILESET_VARIANT_POOLS[tilesetId] ?? null
}

// Flat (-0) elevation sheets are uniform single-terrain (or a flat blend) with
// no cliffs — every tile is walkable ground. Mirrors the server's
// isWalkableGroundTile in pathing.go.
const FLAT_WALKABLE_TILESETS = new Set([
  'corrupt-corrupt-elevation-0',
  'dirt-dirt-elevation-0',
  'dirt-grass-elevation-0',
  'grass-grass-elevation-0',
  'snow-snow-elevation-0',
])

// isWalkableGroundTile mirrors the server's isWalkableGroundTile
// (server/internal/game/pathing.go) — keep the two in sync. A tiles[] override
// renders flat walkable ground (vs a cliff/edge/decoration that blocks) when:
//   - its tileset is a flat (-0) sheet (all tiles walkable); or
//   - it's one of the two pure-interior Wang slots — (col2,row1) grass,
//     (col0,row3) dirt — on any other (Wang 4×4) tileset.
export function isWalkableGroundTile(coord: TileCoord): boolean {
  if (FLAT_WALKABLE_TILESETS.has(coord.tileset)) return true
  if (coord.col === 2 && coord.row === 1) return true
  if (coord.col === 0 && coord.row === 3) return true
  return false
}

// Named presets for the map editor's "Default Ground" dropdown. Each points at
// a flat (-0) single-terrain sheet; a map's unpainted ground is filled with
// randomized variants from that sheet (see drawAutoTiledTerrain).
export type DefaultGroundName = 'grass' | 'dirt' | 'snow' | 'corrupt'
export const DEFAULT_TILE_PRESETS: Record<DefaultGroundName, TileCoord> = {
  grass: { tileset: 'grass-grass-elevation-0', col: 0, row: 0 },
  dirt: { tileset: 'dirt-dirt-elevation-0', col: 0, row: 0 },
  snow: { tileset: 'snow-snow-elevation-0', col: 0, row: 0 },
  corrupt: { tileset: 'corrupt-corrupt-elevation-0', col: 0, row: 0 },
}

// Legacy grass/dirt Wang coords on the base `tileset` sheet — still used to
// render maps whose defaultTile is the base Wang sheet (existing maps) and by
// the grass/dirt terrain brush's auto-tiler.
export const TERRAIN_TILE_COORDS: Record<TerrainType, TileCoord> = {
  grass: { tileset: 'tileset', col: 2, row: 1 },
  dirt: { tileset: 'tileset', col: 0, row: 3 },
}

// Fallback "ground" tile when a map has no defaultTile — flat grass.
export const GROUND_TILE_COORDS: TileCoord = { tileset: 'grass-grass-elevation-0', col: 0, row: 0 }

// Deterministic per-cell variant index for the randomized ground fill — a
// stable hash of the cell coords so the pattern never flickers between frames
// and renders identically in the editor and in-game.
function groundVariantIndex(x: number, y: number, n: number): number {
  let h = Math.imul(x | 0, 374761393) + Math.imul(y | 0, 668265263)
  h = Math.imul(h ^ (h >>> 13), 1274126177)
  return ((h ^ (h >>> 16)) >>> 0) % n
}

export function drawTerrainTile(
  ctx: CanvasRenderingContext2D,
  coord: TileCoord,
  destX: number,
  destY: number,
  destSize: number,
) {
  const def = getTilesetDef(coord.tileset)
  const img = getTilesetImage(coord.tileset)
  if (!def || !img) return
  const sx = def.offsetX + coord.col * (def.tileWidth + def.spacingX)
  const sy = def.offsetY + coord.row * (def.tileHeight + def.spacingY)
  // Hi-res tiles are downscaled to the cell — smooth for a clean result, and
  // inset the source rect a half source-pixel so the interpolation can't
  // sample the neighbouring atlas tile and bleed a seam. Low-res pixel-art
  // tiles keep nearest-neighbour (no smoothing, no inset) so they stay crisp.
  const hiRes = def.tileWidth > destSize
  const inset = hiRes ? 0.5 : 0
  ctx.imageSmoothingEnabled = hiRes
  ctx.drawImage(
    img,
    sx + inset,
    sy + inset,
    def.tileWidth - inset * 2,
    def.tileHeight - inset * 2,
    destX,
    destY,
    destSize,
    destSize,
  )
}

// Wang 2-corner lookup for the grass/dirt tileset. The 4-bit mask encodes
// which of the cell's 4 corners are "grass": bit 0 = TL, bit 1 = TR,
// bit 2 = BL, bit 3 = BR. Index 0 = all dirt corners, index 15 = all grass.
// Coordinates were derived by sampling each 32×32 cell in tileset.png (now
// expressed as grid col/row instead of pixel sx/sy).
const WANG_GRASS_DIRT_COORDS: ReadonlyArray<TileCoord> = [
  { tileset: 'tileset', col: 0, row: 3 }, // 0  ----
  { tileset: 'tileset', col: 3, row: 3 }, // 1  G---
  { tileset: 'tileset', col: 0, row: 2 }, // 2  -G--
  { tileset: 'tileset', col: 1, row: 2 }, // 3  GG--
  { tileset: 'tileset', col: 0, row: 0 }, // 4  --G-
  { tileset: 'tileset', col: 3, row: 2 }, // 5  G-G-
  { tileset: 'tileset', col: 2, row: 3 }, // 6  -GG-
  { tileset: 'tileset', col: 3, row: 1 }, // 7  GGG-
  { tileset: 'tileset', col: 1, row: 3 }, // 8  ---G
  { tileset: 'tileset', col: 0, row: 1 }, // 9  G--G
  { tileset: 'tileset', col: 1, row: 0 }, // 10 -G-G
  { tileset: 'tileset', col: 2, row: 2 }, // 11 GG-G
  { tileset: 'tileset', col: 3, row: 0 }, // 12 --GG
  { tileset: 'tileset', col: 2, row: 0 }, // 13 G-GG
  { tileset: 'tileset', col: 1, row: 1 }, // 14 -GGG
  { tileset: 'tileset', col: 2, row: 1 }, // 15 GGGG
]

export function getWangGrassDirtCoord(mask: number): TileCoord {
  return WANG_GRASS_DIRT_COORDS[mask & 0b1111]
}

// Reverse-lookup TerrainType from a TileCoord. Used to figure out which
// semantic terrain a `defaultTile` represents so the auto-tiler knows
// which terrain is the "background" vs the painted overlay.
export function inferTerrainFromCoord(coord: TileCoord): TerrainType | null {
  for (const key of Object.keys(TERRAIN_TILE_COORDS) as TerrainType[]) {
    const tc = TERRAIN_TILE_COORDS[key]
    if (tc.tileset === coord.tileset && tc.col === coord.col && tc.row === coord.row) {
      return key
    }
  }
  return null
}

// Shared spec consumed by drawAutoTiledTerrain. Loose-typed so it can accept
// either a full MapConfig or an editor draft without coupling.
export interface AutoTiledTerrainSpec {
  gridCols: number
  gridRows: number
  cellSize: number
  defaultTile?: TileCoord
  terrain: ReadonlyArray<TerrainTile>
  tiles?: ReadonlyArray<TileInstance>
}

// Renders the full ground layer with Wang 2-corner auto-tiling. Used by both
// the in-game CanvasRenderer (baked into terrainCache) and the map editor
// (per-frame), so painted terrain looks identical in both views.
//
// Caller must verify isTerrainTilesetReady() before calling — otherwise
// drawTerrainTile is a no-op and you'll get a transparent canvas.
//
// Layering: auto-tiled ground (defaultTile + terrain[]) → tiles[] decorative
// overrides on top. terrain[] is consumed by the auto-tiler; tiles[] is raw
// tileset-coord overrides for decorations that bypass auto-tiling.
export function drawAutoTiledTerrain(
  ctx: CanvasRenderingContext2D,
  spec: AutoTiledTerrainSpec,
): void {
  const { gridCols, gridRows, cellSize, terrain, tiles } = spec
  const groundCoord = spec.defaultTile ?? GROUND_TILE_COORDS
  const defaultTerrain = inferTerrainFromCoord(groundCoord)

  ctx.imageSmoothingEnabled = false

  if (defaultTerrain) {
    // Build per-cell terrain grid: defaultTerrain everywhere, with sparse
    // overrides from terrain[]. Out-of-bounds reads return defaultTerrain.
    const grid: TerrainType[][] = new Array(gridRows)
    for (let y = 0; y < gridRows; y++) {
      grid[y] = new Array(gridCols).fill(defaultTerrain)
    }
    for (const t of terrain) {
      if (t.x >= 0 && t.x < gridCols && t.y >= 0 && t.y < gridRows) {
        grid[t.y][t.x] = t.terrain
      }
    }
    const getTerrain = (x: number, y: number): TerrainType => {
      if (x < 0 || x >= gridCols || y < 0 || y >= gridRows) return defaultTerrain
      return grid[y][x]
    }
    for (let cy = 0; cy < gridRows; cy++) {
      for (let cx = 0; cx < gridCols; cx++) {
        const mask = computeWangMask(cx, cy, defaultTerrain, getTerrain)
        const coord = getWangGrassDirtCoord(mask)
        drawTerrainTile(ctx, coord, cx * cellSize, cy * cellSize, cellSize)
      }
    }
  } else {
    // defaultTile isn't a base-tileset Wang terrain — flat/custom default. When
    // the sheet is a full-grid variant pool (the flat -0 sheets), scatter a
    // randomized variant per cell so the ground reads as varied; otherwise tile
    // the single default coord. terrain[] overrides are stamped directly after.
    const pool = getSheetVariantPool(groundCoord.tileset)
    for (let gy = 0; gy < gridRows; gy++) {
      for (let gx = 0; gx < gridCols; gx++) {
        const coord = pool ? pool[groundVariantIndex(gx, gy, pool.length)] : groundCoord
        drawTerrainTile(ctx, coord, gx * cellSize, gy * cellSize, cellSize)
      }
    }
    for (const tile of terrain) {
      const coords = TERRAIN_TILE_COORDS[tile.terrain]
      if (coords) {
        drawTerrainTile(ctx, coords, tile.x * cellSize, tile.y * cellSize, cellSize)
      }
    }
  }

  if (tiles) {
    for (const tile of tiles) {
      drawTerrainTile(
        ctx,
        { tileset: tile.tileset, col: tile.col, row: tile.row },
        tile.x * cellSize,
        tile.y * cellSize,
        cellSize,
      )
    }
  }
}

// Wang 2-corner mask computation. The corner-fill rule depends on which
// terrain is the "background" (defaultTerrain): the *other* terrain is the
// one painted as overlays in terrain[], and overlays expand into adjacent
// corners so a single painted cell renders as a full pure tile, with
// transitions appearing in its 8 neighbors.
//
// Concretely: a corner is "grass" when either
//   (a) defaultTerrain === 'grass' AND all 4 cells touching that corner
//       are grass (i.e., no painted dirt nearby), OR
//   (b) defaultTerrain === 'dirt' AND any of the 4 cells touching that
//       corner is grass (i.e., grass is the overlay and expands).
function computeWangMask(
  cx: number,
  cy: number,
  defaultTerrain: TerrainType,
  getTerrain: (x: number, y: number) => TerrainType,
): number {
  const cornerIsGrass = (
    ax: number, ay: number,
    bx: number, by: number,
    ccx: number, ccy: number,
    dx: number, dy: number,
  ): boolean => {
    if (defaultTerrain === 'grass') {
      return getTerrain(ax, ay) === 'grass'
        && getTerrain(bx, by) === 'grass'
        && getTerrain(ccx, ccy) === 'grass'
        && getTerrain(dx, dy) === 'grass'
    }
    return getTerrain(ax, ay) === 'grass'
      || getTerrain(bx, by) === 'grass'
      || getTerrain(ccx, ccy) === 'grass'
      || getTerrain(dx, dy) === 'grass'
  }

  let mask = 0
  // TL corner at grid line (cx, cy): touched by cells (cx-1,cy-1) (cx,cy-1) (cx-1,cy) (cx,cy)
  if (cornerIsGrass(cx - 1, cy - 1, cx, cy - 1, cx - 1, cy, cx, cy)) mask |= 1
  // TR corner at (cx+1, cy): (cx,cy-1) (cx+1,cy-1) (cx,cy) (cx+1,cy)
  if (cornerIsGrass(cx, cy - 1, cx + 1, cy - 1, cx, cy, cx + 1, cy)) mask |= 2
  // BL corner at (cx, cy+1): (cx-1,cy) (cx,cy) (cx-1,cy+1) (cx,cy+1)
  if (cornerIsGrass(cx - 1, cy, cx, cy, cx - 1, cy + 1, cx, cy + 1)) mask |= 4
  // BR corner at (cx+1, cy+1): (cx,cy) (cx+1,cy) (cx,cy+1) (cx+1,cy+1)
  if (cornerIsGrass(cx, cy, cx + 1, cy, cx, cy + 1, cx + 1, cy + 1)) mask |= 8
  return mask
}

// Mirrors the server's addTerrainBlocks (server/internal/game/pathing.go):
// a cell renders as a Wang transition tile (cliff / slope) when its mask is
// neither 0 (all-dirt) nor 15 (all-grass). Those transition tiles are not
// walkable and the server rejects pathfinding through them, so the client
// must reject build placement on them too. tiles[] overrides decide outright
// via isWalkableGroundTile — a pure-Wang coord or a grass-variant tile makes
// the cell walkable, any other override (cliff/edge/decoration) blocks it.
// Returns false when the map's defaultTile isn't one of the two recognized
// canonical coords (custom default → server treats everything as walkable, so
// we do too).
export function isTerrainCellBlocked(
  spec: AutoTiledTerrainSpec,
  x: number,
  y: number,
): boolean {
  // tiles[] overrides always decide their own cell — a painted cliff blocks
  // even on a flat, otherwise-walkable default ground.
  if (spec.tiles) {
    for (const t of spec.tiles) {
      if (t.x === x && t.y === y) {
        return !isWalkableGroundTile({ tileset: t.tileset, col: t.col, row: t.row })
      }
    }
  }

  const groundCoord = spec.defaultTile ?? GROUND_TILE_COORDS
  const defaultTerrain = inferTerrainFromCoord(groundCoord)
  if (!defaultTerrain) return false // flat/custom default: unpainted is walkable

  const terrainAt = (cx: number, cy: number): TerrainType => {
    if (cx < 0 || cx >= spec.gridCols || cy < 0 || cy >= spec.gridRows) {
      return defaultTerrain
    }
    for (const t of spec.terrain) {
      if (t.x === cx && t.y === cy) return t.terrain
    }
    return defaultTerrain
  }

  const mask = computeWangMask(x, y, defaultTerrain, terrainAt)
  return mask !== 0 && mask !== 15
}
