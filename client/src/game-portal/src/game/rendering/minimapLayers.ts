// Shared rendering primitives for the minimap. The in-game HUD minimap and
// the create-lobby map preview both call into these so a map's static
// content (terrain, obstacles, buildings, neutral-camp POIs) renders the
// same way everywhere. Live-state layers (units, fog of war, camera
// viewport) stay inline at each call site because they only exist in one
// of the two contexts.
//
// Adding a new map element type (a new POI category, a new building skip
// rule, etc.) should happen here once; both renderers pick it up
// automatically.

import type { LootDropSnapshot, MapConfig, NeutralCampSnapshot } from '../network/protocol'
import {
  DEFAULT_GRASS_COLOR,
  getBuildingColor,
  getNeutralSpawnTierColor,
  getObstacleColor,
  getTerrainColor,
} from '../maps/mapConfig'
import { drawAutoTiledTerrain, isTerrainTilesetReady } from './terrainTileset'

export type MinimapRect = { x: number; y: number; width: number; height: number }

// The structural subset of MapConfig that the minimap renderers actually
// need. Declared narrowly so both MapConfig (in-game live state) and
// MapCatalogMapPayload (lobby preview's catalog file) can be passed
// without casts.
export type MinimapMapInput = Pick<
  MapConfig,
  | 'gridCols'
  | 'gridRows'
  | 'cellSize'
  | 'terrain'
  | 'tiles'
  | 'defaultTile'
  | 'obstacles'
  | 'buildings'
  | 'neutralSpawns'
>

// Builds an offscreen full-map-resolution canvas containing the rendered
// terrain. When the sprite tileset is loaded the canvas holds real sprite
// tiles (matches the in-game look exactly); otherwise it falls back to
// category-color fills so the minimap still renders something usable.
//
// Returns null if the canvas API is unavailable or the map dimensions are
// invalid. Callers should treat null as "no terrain layer" — drawMinimapBase
// gracefully degrades by painting category-color cells directly into the
// minimap rect.
export function buildTerrainSurface(mapConfig: MinimapMapInput): HTMLCanvasElement | null {
  const { gridCols, gridRows, cellSize, terrain, tiles, defaultTile } = mapConfig
  const mapWidth = gridCols * cellSize
  const mapHeight = gridRows * cellSize
  if (mapWidth <= 0 || mapHeight <= 0) return null

  const cache = document.createElement('canvas')
  cache.width = mapWidth
  cache.height = mapHeight
  const cctx = cache.getContext('2d')
  if (!cctx) return null
  cctx.imageSmoothingEnabled = false

  if (isTerrainTilesetReady()) {
    drawAutoTiledTerrain(cctx, {
      gridCols,
      gridRows,
      cellSize,
      defaultTile,
      terrain: terrain ?? [],
      tiles,
    })
  } else {
    cctx.fillStyle = DEFAULT_GRASS_COLOR
    cctx.fillRect(0, 0, mapWidth, mapHeight)
    for (const tile of terrain ?? []) {
      cctx.fillStyle = getTerrainColor(tile.terrain)
      cctx.fillRect(tile.x * cellSize, tile.y * cellSize, cellSize, cellSize)
    }
  }

  return cache
}

export type MinimapBaseOpts = {
  // Highlight this player's buildings in white (matches in-game behavior).
  // Lobby preview leaves this null since there is no local player.
  localPlayerId?: string | null
  // Resolves a building owner's color when occupied. Lobby preview leaves
  // this null because the static catalog file carries no owner state.
  getOwnerColor?: ((ownerId: string) => string | null) | null
}

// Paints the static "base" of a minimap into the given rect: black panel
// fill, downscaled terrain blit, obstacles, buildings, and the subtle
// border ring. Does NOT draw any live layer (units, FOW, viewport) or
// POIs — POIs are a separate concern because they overlay FOW in the
// in-game pipeline.
export function drawMinimapBase(
  ctx: CanvasRenderingContext2D,
  mapConfig: MinimapMapInput,
  bounds: MinimapRect,
  terrainSurface: HTMLCanvasElement | null,
  opts: MinimapBaseOpts = {},
): void {
  const { x, y, width, height } = bounds
  const { gridCols, gridRows } = mapConfig

  ctx.fillStyle = '#000'
  ctx.fillRect(x, y, width, height)

  if (terrainSurface) {
    const prevSmoothing = ctx.imageSmoothingEnabled
    ctx.imageSmoothingEnabled = true
    ctx.drawImage(terrainSurface, x, y, width, height)
    ctx.imageSmoothingEnabled = prevSmoothing
  } else {
    ctx.fillStyle = DEFAULT_GRASS_COLOR
    ctx.fillRect(x, y, width, height)
    for (const tile of mapConfig.terrain ?? []) {
      ctx.fillStyle = getTerrainColor(tile.terrain)
      ctx.fillRect(
        x + (tile.x / gridCols) * width,
        y + (tile.y / gridRows) * height,
        width / gridCols,
        height / gridRows,
      )
    }
  }

  for (const tile of mapConfig.obstacles ?? []) {
    ctx.fillStyle = getObstacleColor(tile.obstacle)
    const tileW = tile.width ?? 1
    const tileH = tile.height ?? 1
    ctx.fillRect(
      x + (tile.x / gridCols) * width,
      y + (tile.y / gridRows) * height,
      (tileW / gridCols) * width,
      (tileH / gridRows) * height,
    )
  }

  for (const b of mapConfig.buildings ?? []) {
    // In-game state may set visible=false / ghost=true on buildings; treat
    // unset (catalog file) as visible+not-ghost.
    if (b.visible === false) continue
    if (b.ghost) continue
    if (b.buildingType === 'enemy-spawnpoint') continue

    const isLocal =
      !!opts.localPlayerId && b.occupied === true && b.ownerId === opts.localPlayerId

    let color: string
    if (isLocal) {
      color = '#f8fafc'
    } else {
      const ownerColor =
        opts.getOwnerColor && b.occupied === true && b.ownerId
          ? opts.getOwnerColor(b.ownerId)
          : null
      color = getBuildingColor(b.buildingType, b.occupied ?? true, ownerColor)
    }
    ctx.fillStyle = color
    ctx.fillRect(
      x + (b.x / gridCols) * width,
      y + (b.y / gridRows) * height,
      (b.width / gridCols) * width,
      (b.height / gridRows) * height,
    )
  }

  ctx.strokeStyle = 'rgba(166, 191, 255, 0.35)'
  ctx.lineWidth = 1
  ctx.strokeRect(x, y, width, height)
}

// Paints neutral-camp POI markers (and any other authored POIs we add
// later) into the given rect. Drawn ABOVE any FOW so they remain visible
// regardless of scouting state.
//
// When `snapshotsById` is provided (in-game), the dot uses live state:
//   - color from currentTier
//   - dot hidden when aliveUnitCount === 0 so cleared / wave-hidden camps
//     drop off the minimap until they respawn
//
// When `snapshotsById` is null (lobby preview, map editor — no live
// server data), every authored camp renders in its startingTier color.
export function drawMinimapPOIs(
  ctx: CanvasRenderingContext2D,
  mapConfig: MinimapMapInput,
  bounds: MinimapRect,
  snapshotsById: Map<string, NeutralCampSnapshot> | null,
  // Live ground-loot chests. Always-visible (no FOW gating). Pass null or
  // undefined in lobby/editor contexts where no live server data exists.
  lootDropsById?: Map<string, LootDropSnapshot> | null,
): void {
  const spawns = mapConfig.neutralSpawns
  const { x, y, width, height } = bounds
  const { gridCols, gridRows, cellSize } = mapConfig

  if (spawns && spawns.length > 0) {
    const cellMinimapW = width / gridCols
    const cellMinimapH = height / gridRows

    for (const ns of spawns) {
      const snap = snapshotsById?.get(ns.id)
      // Live-state camps with no units are invisible on the minimap; they
      // re-appear when spawnGroupForCampLocked next runs (wave clear).
      // Camps without a snapshot (no live data — lobby/editor) still render.
      if (snap && snap.aliveUnitCount === 0) continue

      const dotX = x + (ns.x + 0.5) * cellMinimapW
      const dotY = y + (ns.y + 0.5) * cellMinimapH
      const tier = snap?.currentTier ?? ns.startingTier ?? 1
      ctx.fillStyle = getNeutralSpawnTierColor(tier)
      ctx.beginPath()
      ctx.arc(dotX, dotY, 3, 0, Math.PI * 2)
      ctx.fill()
      ctx.lineWidth = 0.75
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.85)'
      ctx.stroke()
    }
  }

  // Loot-drop dots — amber, slightly smaller than camp dots so they
  // visually distinguish from camp POIs. Always visible regardless of FOW.
  if (lootDropsById && lootDropsById.size > 0) {
    const mapW = gridCols * cellSize
    const mapH = gridRows * cellSize
    for (const drop of lootDropsById.values()) {
      const dotX = x + (drop.x / mapW) * width
      const dotY = y + (drop.y / mapH) * height
      ctx.fillStyle = '#f5b400'
      ctx.beginPath()
      ctx.arc(dotX, dotY, 2.5, 0, Math.PI * 2)
      ctx.fill()
      ctx.lineWidth = 0.75
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.85)'
      ctx.stroke()
    }
  }
}
