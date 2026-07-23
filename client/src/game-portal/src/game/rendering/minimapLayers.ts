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

import type { BuildingTile, LootDropSnapshot, MapConfig, NeutralCampSnapshot } from '../network/protocol'
import {
  DEFAULT_GRASS_COLOR,
  getBuildingColor,
  getNeutralSpawnTierColor,
  getObstacleColor,
  getTerrainColor,
} from '../maps/mapConfig'
import { drawAutoTiledTerrain, isTerrainTilesetReady } from './terrainTileset'
import shopHouseUrl from '../../assets/minimap/shop-house.svg?url'

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
  | 'elevation'
  | 'cliffTileset'
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
  const { gridCols, gridRows, cellSize, terrain, tiles, defaultTile, elevation, cliffTileset } = mapConfig
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
      elevation,
      cliffTileset,
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

// A neutral shop's minimap point of interest: the building footprint (grid
// cells) plus its type for color resolution. Captured from the full authored
// map so it survives FOW filtering of the live building list.
export type ShopPOI = Pick<BuildingTile, 'id' | 'buildingType' | 'x' | 'y' | 'width' | 'height'>

// A building is a shop POI when it sells something. Capability-based (rather
// than a building-type list) so a future shop kind is picked up for free —
// tiles carry capabilities in both the authored catalog files and live
// snapshots, so this works with or without building defs loaded.
const SHOP_POI_CAPABILITIES = ['item-purchase', 'recipe-purchase']

// One bright light-yellow for every shop kind — spotting "there's a shop
// here" matters more at minimap scale than telling shop kinds apart. Keep in
// sync with the fill in assets/minimap/shop-house.svg.
export const SHOP_POI_COLOR = '#fef08a'
const SHOP_POI_OUTLINE = '#3b2f0b'

// The SVG house icon, lazily loaded on first draw. Until it finishes loading
// (and in non-browser test environments where it never does) the fallback
// path in drawShopHouseFallback draws an equivalent house silhouette so the
// marker is never missing for a frame.
let shopHouseIcon: HTMLImageElement | null = null
let shopHouseIconReady = false

function ensureShopHouseIcon(): HTMLImageElement | null {
  if (!shopHouseIcon && typeof Image !== 'undefined') {
    shopHouseIcon = new Image()
    shopHouseIcon.onload = () => {
      shopHouseIconReady = true
    }
    shopHouseIcon.src = shopHouseUrl
  }
  return shopHouseIconReady ? shopHouseIcon : null
}

// Path-drawn stand-in for the SVG: roof with eaves over a squat body, same
// colors, same footprint as the drawImage call.
function drawShopHouseFallback(
  ctx: CanvasRenderingContext2D,
  cx: number,
  cy: number,
  r: number,
): void {
  const bodyHalfW = r * 0.62
  ctx.fillStyle = SHOP_POI_COLOR
  ctx.beginPath()
  ctx.moveTo(cx, cy - r) // roof apex
  ctx.lineTo(cx + r, cy - r * 0.05) // right eave
  ctx.lineTo(cx + bodyHalfW, cy - r * 0.05)
  ctx.lineTo(cx + bodyHalfW, cy + r) // body bottom-right
  ctx.lineTo(cx - bodyHalfW, cy + r) // body bottom-left
  ctx.lineTo(cx - bodyHalfW, cy - r * 0.05)
  ctx.lineTo(cx - r, cy - r * 0.05) // left eave
  ctx.closePath()
  ctx.fill()
  ctx.lineWidth = 1
  ctx.strokeStyle = SHOP_POI_OUTLINE
  ctx.stroke()
}

export function getShopPOIs(buildings: BuildingTile[] | null | undefined): ShopPOI[] {
  const pois: ShopPOI[] = []
  for (const b of buildings ?? []) {
    if (b.visible === false) continue
    if (!b.capabilities?.some((c) => SHOP_POI_CAPABILITIES.includes(c))) continue
    pois.push({
      id: b.id,
      buildingType: b.buildingType,
      x: b.x,
      y: b.y,
      width: b.width,
      height: b.height,
    })
  }
  return pois
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
  // Neutral shop markers. Omit (undefined) to derive from
  // mapConfig.buildings — correct for the lobby preview and map editor,
  // whose map models hold the full authored building list. The in-game
  // renderer must pass GameState.neutralShopPOIs instead, because its live
  // building list is FOW-filtered and unscouted shops are simply absent.
  shopPOIs?: ShopPOI[],
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

  // Neutral shop markers — a light-yellow house icon, larger than camp dots,
  // so a "place to spend" reads differently from a "place to fight". Always
  // visible regardless of FOW: shops are static map landmarks, same
  // treatment as camp POIs above.
  const shops = shopPOIs ?? getShopPOIs(mapConfig.buildings)
  if (shops.length > 0) {
    const cellMinimapW = width / gridCols
    const cellMinimapH = height / gridRows
    const r = 5.5 // half-size of the icon in minimap pixels
    const icon = ensureShopHouseIcon()
    for (const shop of shops) {
      const cx = x + (shop.x + shop.width / 2) * cellMinimapW
      const cy = y + (shop.y + shop.height / 2) * cellMinimapH
      if (icon) {
        ctx.drawImage(icon, cx - r, cy - r, r * 2, r * 2)
      } else {
        drawShopHouseFallback(ctx, cx, cy, r)
      }
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
