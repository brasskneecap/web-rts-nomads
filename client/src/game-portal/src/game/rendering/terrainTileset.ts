import tilesetUrl from '../../assets/terrain/grass-dirt-elevation-25.png?url'
import grassGrass25Url from '../../assets/terrain/grass-grass-elevation-25.png?url'
import dirtDirt25Url from '../../assets/terrain/dirt-dirt-elevation-25.png?url'
import grassDirt0Url from '../../assets/terrain/grass-dirt-elevation-0.png?url'
import type { TerrainTile, TerrainType, TileCoord, TileInstance, TileSheet } from '../network/protocol'

export type { TileCoord }

// Default tile size for sheets that don't override it. Per-sheet overrides
// live in SHEET_TILE_SIZE below.
export const TILE_SIZE = 16

const sheetUrls: Record<TileSheet, string> = {
  tileset: tilesetUrl,
  'grass-grass-25': grassGrass25Url,
  'dirt-dirt-25': dirtDirt25Url,
  'grass-dirt-0': grassDirt0Url,
}

// Per-sheet source tile size in pixels. Sheets not listed use TILE_SIZE (16).
const SHEET_TILE_SIZE: Partial<Record<TileSheet, number>> = {
  tileset: 32,
  'grass-grass-25': 32,
  'dirt-dirt-25': 32,
  'grass-dirt-0': 32,
}

export function getSheetTileSize(name: TileSheet): number {
  return SHEET_TILE_SIZE[name] ?? TILE_SIZE
}

export const TILE_SHEET_NAMES: TileSheet[] = [
  'tileset',
  'grass-grass-25',
  'dirt-dirt-25',
  'grass-dirt-0',
]

const images = {} as Record<TileSheet, HTMLImageElement>
for (const name of TILE_SHEET_NAMES) {
  const img = new Image()
  img.src = sheetUrls[name]
  images[name] = img
}

// Named presets for the map editor's "Default Ground" dropdown.
export const DEFAULT_TILE_PRESETS: Record<'grass' | 'dirt', TileCoord> = {
  grass: { sheet: 'tileset', sx: 64, sy: 32 },
  dirt: { sheet: 'tileset', sx: 0, sy: 96 },
}

export const TERRAIN_TILE_COORDS: Record<TerrainType, TileCoord> = {
  grass: { sheet: 'tileset', sx: 64, sy: 32 },
  dirt: { sheet: 'tileset', sx: 0, sy: 96 },
}

// Fallback "ground" tile tiled across the map when no specific terrain is set
// and the map doesn't override via defaultTile. Defaults to grass.
export const GROUND_TILE_COORDS: TileCoord = { sheet: 'tileset', sx: 64, sy: 32 }

export function getSheetImage(name: TileSheet): HTMLImageElement | null {
  const img = images[name]
  if (!img || !img.complete || img.naturalWidth === 0) return null
  return img
}

export function onSheetReady(name: TileSheet, cb: () => void) {
  const img = images[name]
  if (!img) return
  if (img.complete && img.naturalWidth > 0) {
    cb()
    return
  }
  img.addEventListener('load', cb, { once: true })
}

function getSheet(name: TileSheet): HTMLImageElement | null {
  return getSheetImage(name)
}

export function isTerrainTilesetReady(): boolean {
  return getSheet('tileset') !== null
}

export function drawTerrainTile(
  ctx: CanvasRenderingContext2D,
  coord: TileCoord,
  destX: number,
  destY: number,
  destSize: number,
) {
  const img = getSheet(coord.sheet)
  if (!img) return
  const srcSize = getSheetTileSize(coord.sheet)
  ctx.imageSmoothingEnabled = false
  ctx.drawImage(img, coord.sx, coord.sy, srcSize, srcSize, destX, destY, destSize, destSize)
}

// Wang 2-corner lookup for the grass/dirt tileset. The 4-bit mask encodes
// which of the cell's 4 corners are "grass": bit 0 = TL, bit 1 = TR,
// bit 2 = BL, bit 3 = BR. Index 0 = all dirt corners, index 15 = all grass.
// Coordinates were derived by sampling each 32×32 cell in tileset.png.
const WANG_GRASS_DIRT_COORDS: ReadonlyArray<readonly [number, number]> = [
  [0, 96],   // 0  ----
  [96, 96],  // 1  G---
  [0, 64],   // 2  -G--
  [32, 64],  // 3  GG--
  [0, 0],    // 4  --G-
  [96, 64],  // 5  G-G-
  [64, 96],  // 6  -GG-
  [96, 32],  // 7  GGG-
  [32, 96],  // 8  ---G
  [0, 32],   // 9  G--G
  [32, 0],   // 10 -G-G
  [64, 64],  // 11 GG-G
  [96, 0],   // 12 --GG
  [64, 0],   // 13 G-GG
  [32, 32],  // 14 -GGG
  [64, 32],  // 15 GGGG
]

export function getWangGrassDirtCoord(mask: number): TileCoord {
  const [sx, sy] = WANG_GRASS_DIRT_COORDS[mask & 0b1111]
  return { sheet: 'tileset', sx, sy }
}

// Reverse-lookup TerrainType from a TileCoord. Used to figure out which
// semantic terrain a `defaultTile` represents so the auto-tiler knows
// which terrain is the "background" vs the painted overlay.
export function inferTerrainFromCoord(coord: TileCoord): TerrainType | null {
  for (const key of Object.keys(TERRAIN_TILE_COORDS) as TerrainType[]) {
    const tc = TERRAIN_TILE_COORDS[key]
    if (tc.sheet === coord.sheet && tc.sx === coord.sx && tc.sy === coord.sy) {
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
// pixel-coord overrides for decorations that bypass auto-tiling.
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
    // defaultTile isn't a known TerrainType — fall back to the legacy path:
    // tile the default everywhere, then stamp terrain[] overrides as direct
    // (non-auto-tiled) lookups. Keeps custom-default maps working unchanged.
    for (let gy = 0; gy < gridRows; gy++) {
      for (let gx = 0; gx < gridCols; gx++) {
        drawTerrainTile(ctx, groundCoord, gx * cellSize, gy * cellSize, cellSize)
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
        { sheet: tile.sheet, sx: tile.sx, sy: tile.sy },
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
