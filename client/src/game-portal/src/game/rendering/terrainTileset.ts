import floorsUrl from '../../assets/terrain/Floors_Tiles.png?url'
import waterUrl from '../../assets/terrain/Water_tiles.png?url'
import tilesetUrl from '../../assets/terrain/tileset.png?url'
import type { TerrainType, TileCoord, TileSheet } from '../network/protocol'

export type { TileCoord }

// Default tile size for sheets that don't override it. Per-sheet overrides
// live in SHEET_TILE_SIZE below.
export const TILE_SIZE = 16

const sheetUrls: Record<TileSheet, string> = {
  floors: floorsUrl,
  water: waterUrl,
  tileset: tilesetUrl,
}

// Per-sheet source tile size in pixels. Sheets not listed use TILE_SIZE (16).
const SHEET_TILE_SIZE: Partial<Record<TileSheet, number>> = {
  tileset: 32,
}

export function getSheetTileSize(name: TileSheet): number {
  return SHEET_TILE_SIZE[name] ?? TILE_SIZE
}

export const TILE_SHEET_NAMES: TileSheet[] = ['floors', 'water', 'tileset']

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
  if (!img.complete || img.naturalWidth === 0) return null
  return img
}

export function onSheetReady(name: TileSheet, cb: () => void) {
  const img = images[name]
  if (img.complete && img.naturalWidth > 0) {
    cb()
    return
  }
  img.addEventListener('load', cb, { once: true })
}

function getSheet(name: TileSheet): HTMLImageElement | null {
  return getSheetImage(name)
}

// True when the base floors sheet is ready. Water is drawn only where needed
// and degrades gracefully if its sheet hasn't loaded yet.
export function isTerrainTilesetReady(): boolean {
  return getSheet('floors') !== null
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
