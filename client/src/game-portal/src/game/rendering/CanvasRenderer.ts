// src/game/rendering/CanvasRenderer.ts
import { GameState } from '../core/GameState'
import {
  DEFAULT_GRASS_COLOR,
  getBuildingColor,
  getObstacleColor,
  getTerrainColor,
} from '../maps/mapConfig'
import { BUILDING_DEF_MAP, getResolvedBuildingAttackVisual } from '../maps/buildingDefs'
import { getBuildingSprite, getTintedBuildingSprite } from './buildingSprites'
import { getObstacleSprite } from './obstacleSprites'
import { OBSTACLE_DEF_MAP } from '../maps/obstacleDefs'
import {
  drawTerrainTile,
  GROUND_TILE_COORDS,
  TERRAIN_TILE_COORDS,
  isTerrainTilesetReady,
} from './terrainTileset'
import { getResolvedUnitAttackVisual, getUnitRenderBounds, UNIT_DEF_MAP } from '../maps/unitDefs'
import type { UnitDef, UnitRenderDef } from '../maps/unitDefs'
import type { BannerSnapshot, BuildingTile, TrapSnapshot } from '../network/protocol'
import { Camera } from './Camera'
import { getRankToneColor } from './rankColors'
import { ACTION_ICON_MAP } from '../maps/actionIconDefs'
import { getPerkAuraRadius, PERK_DEF_MAP } from '../maps/perkDefs'
import { getUnitFrame, getUnitSpriteSet } from './unitSprites'
import { getObjectSpriteSet } from './objectSprites'
import { UnitAnimationController } from './unitAnimation'

// Multiplier applied to each unit sprite's native size at draw time. Bump
// until sprites read clearly at common zoom-outs without swamping the UI.
const UNIT_SPRITE_SCALE = 1.25
// PixelLab exports center a ~48px character in a ~68px canvas, so roughly
// 15% of the sprite height on each side is transparent padding. Used to
// anchor overhead UI (health bar, chevrons, buffs) to the visible head and
// to sit the selection ring under the visible feet instead of the canvas
// bottom.
const UNIT_SPRITE_TOP_PADDING = 0.15
const UNIT_SPRITE_BOTTOM_PADDING = 0.15

export type MinimapBounds = {
  x: number
  y: number
  width: number
  height: number
}

export function getMinimapBounds(
  canvasWidth: number,
  _canvasHeight: number,
  mapWidth: number,
  mapHeight: number,
): MinimapBounds {
  const padding = 18
  const maxWidth = 220
  const aspectRatio = mapWidth / mapHeight
  const width = Math.min(maxWidth, canvasWidth * 0.2)
  const height = width / aspectRatio

  return {
    x: canvasWidth - width - padding,
    y: padding,
    width,
    height,
  }
}

export class CanvasRenderer {
  private ctx: CanvasRenderingContext2D
  private canvas: HTMLCanvasElement
  private state: GameState
  private camera: Camera
  private resizeObserver: ResizeObserver | null = null
  private renderTime = 0
  private bannerInitialDurations = new Map<number, number>()
  // Post-expiry fade-out queue. Traps render at full opacity during their
  // lifetime; when they vanish from the snapshot (expired, detonated, or
  // culled), we retain the last-known TrapSnapshot and render it with linear
  // alpha fade for TRAP_FADE_MS, then drop it. Fading traps are NOT selectable
  // — they are gone from state.traps, which getTrapAtPosition iterates.
  private lastSeenTraps = new Map<string, TrapSnapshot>()
  private fadingOutTraps = new Map<string, { snapshot: TrapSnapshot; startedAt: number }>()
  private readonly TRAP_FADE_MS = 450

  // Per-trap sprite animation state. Only sprite-backed trap types use this;
  // others continue to run through the procedural render path. An entry is
  // removed once an exploding animation finishes or an idle-state trap leaves
  // the snapshot.
  private trapAnimStates = new Map<string, {
    animation: 'idle' | 'exploding'
    startedAt: number
    x: number
    y: number
    radius: number
    /** Inner trigger radius (explosive_trap). Falls back to `radius` when
     *  absent — for trap types whose single radius is the active zone. */
    triggerRadius: number | undefined
    /** Visual-variant tag from the server (e.g. "electrified"). When set and
     *  the sprite set defines a matching animation, it's played instead of
     *  idle. Used for perk-driven visual swaps like ascendant_infusion. */
    variant: string | undefined
    spriteKey: string
  }>()
  // Per-trap-type frame timings.
  private readonly TRAP_IDLE_FRAME_MS = 150
  private readonly TRAP_EXPLODING_FRAME_MS = 55
  // Base render scale for object sprites (native 32px * 2 = 64px ≈ one cell).
  // The authored explosion art already depicts the full blast visually, so we
  // deliberately don't scale it to match the server's damage radius — that
  // made the explosion read as ~5× the barrel. Both animations draw at the
  // same scale; bump this if objects read too small on screen.
  private readonly OBJECT_SPRITE_SCALE = 2
  private unitAnim = new UnitAnimationController()
  private terrainCache: HTMLCanvasElement | null = null
  private terrainCacheKey: unknown[] = []

  constructor(canvas: HTMLCanvasElement, state: GameState, camera: Camera) {
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas not supported')

    this.canvas = canvas
    this.ctx = ctx
    this.state = state
    this.camera = camera

    this.resize()
    window.addEventListener('resize', this.resize)

    if (typeof ResizeObserver !== 'undefined') {
      this.resizeObserver = new ResizeObserver(() => {
        this.resize()
      })
      this.resizeObserver.observe(this.canvas)
    }
  }

  private resize = () => {
    this.canvas.width = this.canvas.clientWidth
    this.canvas.height = this.canvas.clientHeight

    this.camera.clamp(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
  }

  render() {
    const ctx = this.ctx
    const renderTime = performance.now()
    this.renderTime = renderTime
    const units = this.state.getInterpolatedUnits(renderTime)

    ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)

    // Background outside the map
    ctx.fillStyle = '#0a0a0a'
    ctx.fillRect(0, 0, this.canvas.width, this.canvas.height)

    ctx.save()
    ctx.scale(this.camera.zoom, this.camera.zoom)
    ctx.translate(-this.camera.x, -this.camera.y)

    this.drawMapBounds()
    this.drawMapBackground()
    this.drawMoveMarkers()
    this.drawBuildingSpawnMarkers()
    this.drawTraps(this.state.traps)
    this.drawBanners(this.state.banners)
    this.drawUnits(units)
    this.drawBuildPlacementGhost()
    this.drawSelectionBox()

    ctx.restore()

    this.drawMinimap(units)
  }

  destroy() {
    window.removeEventListener('resize', this.resize)
    this.resizeObserver?.disconnect()
    this.resizeObserver = null
  }

  // Rebuilds the offscreen terrain canvas when any input that feeds it has
  // changed. Terrain is static after a map load, so baking it once and
  // blitting avoids the per-tile drawImage seams that appear at non-integer
  // zoom/camera positions.
  private ensureTerrainCache() {
    const { cellSize, terrain, tiles, defaultTile, gridCols, gridRows } = this.state.mapConfig
    const tilesetReady = isTerrainTilesetReady()
    const mapWidth = this.state.mapWidth
    const mapHeight = this.state.mapHeight
    const key: unknown[] = [tilesetReady, cellSize, gridCols, gridRows, mapWidth, mapHeight, defaultTile, tiles, terrain]

    if (this.terrainCache && this.terrainCacheKey.length === key.length && key.every((v, i) => v === this.terrainCacheKey[i])) {
      return
    }

    const cache = this.terrainCache ?? document.createElement('canvas')
    if (cache.width !== mapWidth || cache.height !== mapHeight) {
      cache.width = mapWidth
      cache.height = mapHeight
    }
    const cctx = cache.getContext('2d')
    if (!cctx) return

    cctx.imageSmoothingEnabled = false
    cctx.clearRect(0, 0, mapWidth, mapHeight)

    const groundCoord = defaultTile ?? GROUND_TILE_COORDS
    if (tilesetReady) {
      for (let gy = 0; gy < gridRows; gy++) {
        for (let gx = 0; gx < gridCols; gx++) {
          drawTerrainTile(cctx, groundCoord, gx * cellSize, gy * cellSize, cellSize)
        }
      }
    } else {
      cctx.fillStyle = DEFAULT_GRASS_COLOR
      cctx.fillRect(0, 0, mapWidth, mapHeight)
    }

    if (tilesetReady && tiles) {
      for (const tile of tiles) {
        drawTerrainTile(
          cctx,
          { sheet: tile.sheet, sx: tile.sx, sy: tile.sy },
          tile.x * cellSize,
          tile.y * cellSize,
          cellSize,
        )
      }
    }

    for (const tile of terrain) {
      const coords = TERRAIN_TILE_COORDS[tile.terrain]
      if (tilesetReady && coords) {
        drawTerrainTile(cctx, coords, tile.x * cellSize, tile.y * cellSize, cellSize)
      } else {
        cctx.fillStyle = getTerrainColor(tile.terrain)
        cctx.fillRect(tile.x * cellSize, tile.y * cellSize, cellSize, cellSize)
      }
    }

    this.terrainCache = cache
    this.terrainCacheKey = key
  }

  private drawGrid() {
    const ctx = this.ctx
    const gridSize = this.state.mapConfig.cellSize

    const worldWidth = this.canvas.width / this.camera.zoom
    const worldHeight = this.canvas.height / this.camera.zoom

    const viewStartX = this.camera.x
    const viewEndX = this.camera.x + worldWidth
    const viewStartY = this.camera.y
    const viewEndY = this.camera.y + worldHeight

    const startX = Math.max(0, Math.floor(viewStartX / gridSize) * gridSize)
    const endX = Math.min(this.state.mapWidth, viewEndX + gridSize)
    const startY = Math.max(0, Math.floor(viewStartY / gridSize) * gridSize)
    const endY = Math.min(this.state.mapHeight, viewEndY + gridSize)

    ctx.strokeStyle = '#1f1f1f'
    ctx.lineWidth = 1 / this.camera.zoom

    for (let x = startX; x < endX; x += gridSize) {
      ctx.beginPath()
      ctx.moveTo(x, startY)
      ctx.lineTo(x, endY)
      ctx.stroke()
    }

    for (let y = startY; y < endY; y += gridSize) {
      ctx.beginPath()
      ctx.moveTo(startX, y)
      ctx.lineTo(endX, y)
      ctx.stroke()
    }
  }

  private drawMapBackground() {
    const ctx = this.ctx
    const { cellSize, obstacles, buildings } = this.state.mapConfig

    this.ensureTerrainCache()
    if (this.terrainCache) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(this.terrainCache, 0, 0)
    }

    if (this.state.isBuildPlacementActive()) {
      this.drawGrid()
    }

    for (const tile of obstacles) {
      // Grid footprint: the cells the obstacle physically occupies. Used
      // for the selection/hover ring so players see what they actually
      // clicked, even when the sprite spills into neighbouring cells.
      const gridW = tile.width ?? 1
      const gridH = tile.height ?? 1
      const footprintX = tile.x * cellSize
      const footprintY = tile.y * cellSize
      const footprintW = gridW * cellSize
      const footprintH = gridH * cellSize

      // Render bounds: may extend beyond the footprint (e.g. tree canopies
      // reaching up into the row above). Falls back to the footprint when
      // no render override is defined for this obstacle type.
      const renderDef = OBSTACLE_DEF_MAP.get(tile.obstacle)?.render
      const renderX = footprintX + (renderDef?.offsetX ?? 0) * cellSize
      const renderY = footprintY + (renderDef?.offsetY ?? 0) * cellSize
      const renderW = (renderDef?.width ?? gridW) * cellSize
      const renderH = (renderDef?.height ?? gridH) * cellSize

      // Selection ring is drawn *before* the sprite so the sprite body
      // covers the top half of the ellipse — mimicking the unit ring,
      // which looks like it's wrapping around the base of the entity.
      const isSelected = tile.id && tile.id === this.state.selectedObstacleId
      const isHovered = tile.id && tile.id === this.state.hoveredInteractableObstacleId
      if (isSelected || isHovered) {
        const ringDef = OBSTACLE_DEF_MAP.get(tile.obstacle)?.selectionRing
        const override = ringDef ? { ...ringDef, cellSize } : undefined
        this.drawFootprintSelectionEllipse(
          footprintX, footprintY, footprintW, footprintH,
          isSelected ? 'selected' : 'hover',
          'bottom',
          override,
        )
      }

      const sprite = getObstacleSprite(tile.obstacle)

      if (sprite) {
        ctx.imageSmoothingEnabled = false
        ctx.drawImage(sprite, renderX, renderY, renderW, renderH)
      } else {
        const inset = cellSize * 0.14
        ctx.fillStyle = getObstacleColor(tile.obstacle)
        ctx.fillRect(
          renderX + inset,
          renderY + inset,
          renderW - inset * 2,
          renderH - inset * 2,
        )

        ctx.strokeStyle = 'rgba(15, 23, 42, 0.75)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.strokeRect(
          renderX + inset,
          renderY + inset,
          renderW - inset * 2,
          renderH - inset * 2,
        )
      }
    }

    for (const building of buildings) {
      if (!building.visible) continue
      if (building.buildingType === 'enemy-spawnpoint') continue

      // Footprint — the grid cells the building physically occupies. Used
      // for HP bars, construction overlays, and selection hit-testing.
      const worldX = building.x * cellSize
      const worldY = building.y * cellSize
      const width = building.width * cellSize
      const height = building.height * cellSize
      const ownerColor =
        building.occupied && building.ownerId
          ? this.state.getPlayerColor(building.ownerId)
          : null

      const buildingDef = BUILDING_DEF_MAP.get(building.buildingType)
      const renderDef = buildingDef?.render
      const spriteRenderDef = buildingDef?.spriteRender
      const inset = renderDef ? renderDef.inset * cellSize : cellSize * 0.18

      // Sprite bounds may extend beyond the footprint when spriteRender
      // overrides are configured (e.g. a barracks with a roof taller than
      // its footprint). Falls back to the footprint when no overrides are set.
      const spriteX = worldX + (spriteRenderDef?.offsetX ?? 0) * cellSize
      const spriteY = worldY + (spriteRenderDef?.offsetY ?? 0) * cellSize
      const spriteW = (spriteRenderDef?.width ?? building.width) * cellSize
      const spriteH = (spriteRenderDef?.height ?? building.height) * cellSize

      const playerFill = ownerColor ?? buildingDef?.color ?? getBuildingColor(building.buildingType, building.occupied, ownerColor)

      const sprite = getBuildingSprite(building.buildingType)

      // Selection / hover ring drawn *before* the sprite so the sprite
      // covers the top half, mimicking the wrap-around look of the unit ring.
      if (this.state.selectedBuildingId === building.id) {
        this.drawFootprintSelectionEllipse(worldX, worldY, width, height, 'selected', 'center')
      } else if (this.state.hoveredInteractableBuildingId === building.id) {
        this.drawFootprintSelectionEllipse(worldX, worldY, width, height, 'hover', 'center')
      }

      if (sprite) {
        ctx.imageSmoothingEnabled = false
        const tinted = ownerColor
          ? getTintedBuildingSprite(building.buildingType, ownerColor)
          : null
        ctx.drawImage(tinted ?? sprite, spriteX, spriteY, spriteW, spriteH)
      } else if (!renderDef) {
        ctx.fillStyle = playerFill
        ctx.fillRect(worldX, worldY, width, height)
      } else {
        // Draw every layer explicitly; 'player' color is substituted with the owner color.
        // No base fill: unpainted areas are transparent so terrain shows through.
        for (const layer of renderDef.layers) {
          ctx.fillStyle = layer.color === 'player' ? playerFill : layer.color
          if (!('kind' in layer) || layer.kind === 'rect') {
            ctx.fillRect(
              worldX + layer.x * cellSize,
              worldY + layer.y * cellSize,
              layer.w * cellSize,
              layer.h * cellSize,
            )
          } else if (layer.kind === 'tri') {
            const s = cellSize / 6
            const tlX = worldX + layer.cx * cellSize + layer.sc * s
            const tlY = worldY + layer.cy * cellSize + layer.sr * s
            const bslash = (layer.sc + layer.sr) % 2 === 1
            ctx.beginPath()
            if (!bslash) {
              if (layer.h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX,     tlY + s) }
              else               { ctx.moveTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s); ctx.lineTo(tlX, tlY + s) }
            } else {
              if (layer.h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s) }
              else               { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX,     tlY + s); ctx.lineTo(tlX + s, tlY + s) }
            }
            ctx.closePath()
            ctx.fill()
            ctx.strokeStyle = ctx.fillStyle as string
            ctx.lineWidth = 1
            ctx.stroke()
          }
        }
      }

      const isUnderConstruction = building.metadata?.['underConstruction'] === true

      if (isUnderConstruction) {
        ctx.save()
        ctx.globalAlpha = 0.45
        ctx.fillStyle = '#0f172a'
        ctx.fillRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
        ctx.restore()

        ctx.save()
        ctx.strokeStyle = '#fbbf24'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([8 / this.camera.zoom, 5 / this.camera.zoom])
        ctx.strokeRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
        ctx.restore()

        // Construction progress bar above the building
        const hp = building.metadata?.['hp'] as number | undefined
        const maxHp = building.metadata?.['maxHp'] as number | undefined
        if (hp !== undefined && maxHp !== undefined && maxHp > 0) {
          const progress = Math.max(0, Math.min(1, hp / maxHp))
          const barH = 6 / this.camera.zoom
          const barX = worldX + inset
          const barY = worldY + inset - barH - 4 / this.camera.zoom
          const barW = width - inset * 2

          ctx.save()
          ctx.fillStyle = '#1e293b'
          ctx.fillRect(barX, barY, barW, barH)
          ctx.fillStyle = '#fbbf24'
          ctx.fillRect(barX, barY, barW * progress, barH)
          ctx.strokeStyle = 'rgba(251,191,36,0.5)'
          ctx.lineWidth = 1 / this.camera.zoom
          ctx.setLineDash([])
          ctx.strokeRect(barX, barY, barW, barH)
          ctx.restore()
        }
      } else {
        ctx.strokeStyle = 'rgba(15, 23, 42, 0.85)'
        ctx.lineWidth = 2 / this.camera.zoom
        if (!sprite) {
          ctx.strokeRect(worldX, worldY, width, height)
        }

        const hp = building.metadata?.['hp'] as number | undefined
        const maxHp = building.metadata?.['maxHp'] as number | undefined
        if (hp !== undefined && maxHp !== undefined && maxHp > 0 && hp < maxHp) {
          const healthPercent = Math.max(0, Math.min(1, hp / maxHp))
          const barH = 6 / this.camera.zoom
          const barX = worldX + inset
          const barY = worldY + inset - barH - 4 / this.camera.zoom
          const barW = width - inset * 2

          let fillColor = '#22c55e'
          if (healthPercent <= 0.35) fillColor = '#ef4444'
          else if (healthPercent <= 0.7) fillColor = '#eab308'

          ctx.save()
          ctx.fillStyle = 'rgba(2, 6, 23, 0.85)'
          ctx.fillRect(barX - 1, barY - 1, barW + 2, barH + 2)
          ctx.fillStyle = '#0f172a'
          ctx.fillRect(barX, barY, barW, barH)
          ctx.fillStyle = fillColor
          ctx.fillRect(barX, barY, barW * healthPercent, barH)
          ctx.strokeStyle = 'rgba(248, 250, 252, 0.8)'
          ctx.lineWidth = 1 / this.camera.zoom
          ctx.setLineDash([])
          ctx.strokeRect(barX, barY, barW, barH)
          ctx.restore()
        }
      }

      if (!isUnderConstruction) {
        this.drawBuildingAttackEffect(building)
      }
    }
  }

  // Draws a flat ground-ring at the base of a rectangular footprint, matching
  // the green ellipse rendered under selected units. `mode` controls the
  // stroke style: 'selected' is a solid yellow ring; 'hover' is a softer
  // dashed ring used for interactable (gatherable / repairable) hover.
  // `anchor` controls vertical placement within the footprint:
  //   'bottom' sits the ring at the base of the footprint (units, trees).
  //   'center' lifts it toward the vertical middle (multi-cell buildings).
  // `override` lets per-def configs (e.g. ObstacleSelectionRingDef) nudge
  // the center / radii in cell units without changing this default logic.
  //
  // Ring position is driven by the grid footprint, not the sprite render
  // box — this keeps the ring on the physical tile even when sprite
  // overflow pushes the visible art outside the footprint.
  private drawFootprintSelectionEllipse(
    worldX: number,
    worldY: number,
    footprintW: number,
    footprintH: number,
    mode: 'selected' | 'hover',
    anchor: 'bottom' | 'center' = 'bottom',
    override?: {
      offsetX?: number
      offsetY?: number
      radiusX?: number
      radiusY?: number
      cellSize?: number
    },
  ) {
    const ctx = this.ctx
    const cellSize = override?.cellSize ?? 0

    const defaultRadiusX = Math.max(12, footprintW * 0.55)
    const defaultRadiusY = Math.max(6, defaultRadiusX * 0.34)

    const radiusX = override?.radiusX !== undefined && cellSize > 0
      ? override.radiusX * cellSize
      : defaultRadiusX
    const radiusY = override?.radiusY !== undefined && cellSize > 0
      ? override.radiusY * cellSize
      : defaultRadiusY

    const defaultCenterX = worldX + footprintW / 2
    const defaultCenterY = anchor === 'center'
      ? worldY + footprintH * 0.78
      : worldY + footprintH - radiusY * 0.35

    const centerX = override?.offsetX !== undefined && cellSize > 0
      ? worldX + override.offsetX * cellSize
      : defaultCenterX
    const centerY = override?.offsetY !== undefined && cellSize > 0
      ? worldY + override.offsetY * cellSize
      : defaultCenterY

    ctx.save()
    if (mode === 'selected') {
      ctx.strokeStyle = '#22c55e'
      ctx.lineWidth = 3 / this.camera.zoom
    } else {
      ctx.strokeStyle = 'rgba(253, 224, 71, 0.9)'
      ctx.lineWidth = 2 / this.camera.zoom
      ctx.setLineDash([5 / this.camera.zoom, 4 / this.camera.zoom])
    }
    ctx.beginPath()
    ctx.ellipse(centerX, centerY, radiusX, radiusY, 0, 0, Math.PI * 2)
    ctx.stroke()
    ctx.restore()
  }

  private drawMapBounds() {
    const ctx = this.ctx

    ctx.strokeStyle = '#444'
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.strokeRect(0, 0, this.state.mapWidth, this.state.mapHeight)
  }

  // Dashed ring drawn around unit-centered aura perks (Guardian Aura, etc.).
  // Color is the unit's player color so allies and enemies read at a glance;
  // alpha is intentionally low so stacks of aura units don't overwhelm.
  private drawAuraRing(centerX: number, centerY: number, radius: number, color: string) {
    const ctx = this.ctx
    ctx.save()
    ctx.strokeStyle = color
    ctx.globalAlpha = 0.55
    ctx.lineWidth = 1.5 / this.camera.zoom
    ctx.setLineDash([6 / this.camera.zoom, 5 / this.camera.zoom])
    ctx.beginPath()
    ctx.arc(centerX, centerY, radius, 0, Math.PI * 2)
    ctx.stroke()
    ctx.restore()
  }

  private drawMoveMarkers() {
    const ctx = this.ctx
    const now = performance.now()

    for (const marker of this.state.moveMarkers) {
      const elapsed = now - marker.createdAt
      const progress = Math.max(0, Math.min(elapsed / marker.durationMs, 1))
      const alpha = 1 - progress

      const radius = 10 + progress * 16

      ctx.save()
      ctx.globalAlpha = alpha
      ctx.strokeStyle = '#7fdcff'
      ctx.lineWidth = 2 / this.camera.zoom

      ctx.beginPath()
      ctx.arc(marker.x, marker.y, radius, 0, Math.PI * 2)
      ctx.stroke()

      ctx.beginPath()
      ctx.arc(marker.x, marker.y, 4, 0, Math.PI * 2)
      ctx.stroke()

      ctx.restore()
    }
  }

  private drawBuildingSpawnMarkers() {
    const selectedBuilding = this.state.getSelectedBuilding()
    if (!selectedBuilding) return

    const spawnPoint = this.state.getBuildingSpawnPoint(selectedBuilding)
    if (!spawnPoint) return

    this.drawSpawnPointMarker(spawnPoint.x, spawnPoint.y, '#93c5fd')
  }

  private drawSpawnPointMarker(x: number, y: number, color: string) {
    const ctx = this.ctx

    ctx.save()
    ctx.strokeStyle = color
    ctx.fillStyle = 'rgba(15, 23, 42, 0.92)'
    ctx.lineWidth = 2 / this.camera.zoom

    ctx.beginPath()
    ctx.arc(x, y, 9, 0, Math.PI * 2)
    ctx.fill()
    ctx.stroke()

    ctx.beginPath()
    ctx.moveTo(x - 13, y)
    ctx.lineTo(x + 13, y)
    ctx.moveTo(x, y - 13)
    ctx.lineTo(x, y + 13)
    ctx.stroke()
    ctx.restore()
  }

  // ──────────────────────────────────────────────────────────────────────────
  // Trap rendering
  //
  // Each trap type renders a distinctive ground marker at its world position:
  //   - caltrops:       scattered dot cluster, grey/iron tint, dashed radius ring
  //   - fire_pit:       radial gradient fill (orange→red), pulsing outer ring
  //   - explosive_trap: dashed red ring + center pip; triggered=true draws a
  //                     one-frame burst flash at the explosion radius
  //   - marker_trap:    purple dashed ring + crossed-line sigil at center
  //
  // Alpha fades linearly from 1 → 0.15 as remainingSeconds → 0, using the
  // same per-id initial-duration cache pattern as drawBanners.
  // ──────────────────────────────────────────────────────────────────────────
  private drawTraps(traps: TrapSnapshot[]) {
    const ctx = this.ctx
    const now = performance.now()
    const currentIds = new Set<string>()
    for (const t of traps) currentIds.add(t.id)

    // ── Sprite-backed trap animation states ──────────────────────────────────
    // Kept separate from the procedural fade-out path so the animation can
    // outlive the snapshot (exploding continues for its full duration even
    // after the server drops the trap).
    for (const trap of traps) {
      const spriteKey = this.spriteKeyForTrapType(trap.type)
      if (!spriteKey) continue
      // Only switch to 'exploding' when the sprite set actually defines that
      // animation. Simple object sheets (marker_trap, etc.) only have idle —
      // without this guard a stray `triggered` tick would wipe the state.
      const canExplode = !!getObjectSpriteSet(spriteKey)?.animations.has('exploding')
      const shouldExplode = trap.triggered && canExplode
      const state = this.trapAnimStates.get(trap.id)
      if (!state) {
        this.trapAnimStates.set(trap.id, {
          animation: shouldExplode ? 'exploding' : 'idle',
          startedAt: now,
          x: trap.x,
          y: trap.y,
          radius: trap.radius,
          triggerRadius: trap.triggerRadius,
          variant: trap.variant,
          spriteKey,
        })
      } else {
        state.x = trap.x
        state.y = trap.y
        state.radius = trap.radius
        state.triggerRadius = trap.triggerRadius
        state.variant = trap.variant
        if (shouldExplode && state.animation !== 'exploding') {
          state.animation = 'exploding'
          state.startedAt = now
        }
      }
    }

    // Diff current vs last-seen: any trap that existed last frame but isn't in
    // this frame's snapshot has been removed by the server (expired, detonated,
    // or culled). Enqueue a fade-out using its last-known snapshot so we can
    // animate the disappearance for TRAP_FADE_MS. Sprite-backed types skip
    // this — their anim-state map handles disappearance directly.
    for (const [id, snap] of this.lastSeenTraps) {
      if (this.hasTrapSprites(snap.type)) continue
      if (!currentIds.has(id) && !this.fadingOutTraps.has(id)) {
        this.fadingOutTraps.set(id, { snapshot: snap, startedAt: now })
      }
    }
    // Refresh the last-seen cache for the next-frame diff.
    this.lastSeenTraps.clear()
    for (const t of traps) this.lastSeenTraps.set(t.id, t)

    // ── Render live traps (procedural path) ──────────────────────────────────
    for (const trap of traps) {
      if (this.hasTrapSprites(trap.type)) continue
      ctx.save()
      ctx.globalAlpha = 1
      this.renderTrapBody(ctx, trap)
      if (this.state.selectedTrapId === trap.id) {
        this.renderTrapSelectionRing(ctx, trap)
      }
      ctx.restore()
    }

    // ── Render fading-out traps with linear alpha fade ──────────────────────
    for (const id of Array.from(this.fadingOutTraps.keys())) {
      const entry = this.fadingOutTraps.get(id)!
      const t = (now - entry.startedAt) / this.TRAP_FADE_MS
      if (t >= 1) {
        this.fadingOutTraps.delete(id)
        continue
      }
      ctx.save()
      ctx.globalAlpha = 1 - t
      this.renderTrapBody(ctx, entry.snapshot)
      ctx.restore()
    }

    // ── Render sprite-backed traps via animation states ─────────────────────
    for (const id of Array.from(this.trapAnimStates.keys())) {
      const state = this.trapAnimStates.get(id)!
      const done = this.renderTrapSpriteState(id, state, currentIds.has(id), now)
      if (done) this.trapAnimStates.delete(id)
    }
  }

  // Returns true when the state has ended and should be removed (idle-state
  // trap left the snapshot, or exploding animation finished).
  private renderTrapSpriteState(
    id: string,
    state: { animation: 'idle' | 'exploding'; startedAt: number; x: number; y: number; radius: number; triggerRadius: number | undefined; variant: string | undefined; spriteKey: string },
    stillInSnapshot: boolean,
    now: number,
  ): boolean {
    const spriteSet = getObjectSpriteSet(state.spriteKey)
    if (!spriteSet) return !stillInSnapshot

    const scale = spriteSet.scale ?? this.OBJECT_SPRITE_SCALE
    // Positional nudge, authored in native sprite pixels. Scaled by the
    // effective render scale so the shift stays proportional when an object
    // also sets a `scale` override.
    const offX = (spriteSet.offsetX ?? 0) * scale
    const offY = (spriteSet.offsetY ?? 0) * scale

    if (state.animation === 'exploding') {
      const anim = spriteSet.animations.get('exploding')
      if (!anim) return true
      const elapsed = now - state.startedAt
      const frameMs = anim.frameDurationMs ?? this.TRAP_EXPLODING_FRAME_MS
      const frameIndex = Math.floor(elapsed / frameMs)
      if (frameIndex >= anim.frameCount) return true
      this.drawObjectFrame(anim, frameIndex, state.x + offX, state.y + offY, scale)
      return false
    }

    if (!stillInSnapshot) return true
    // Variant wins over idle when present and defined for this sprite set —
    // lets perk-driven visuals (electrified caltrops, etc.) swap in without
    // renderer-side branching on perk ids.
    const variantAnim = state.variant ? spriteSet.animations.get(state.variant) : undefined
    const anim = variantAnim ?? spriteSet.animations.get('idle')
    if (!anim) return true
    const elapsed = now - state.startedAt
    const frameMs = anim.frameDurationMs ?? this.TRAP_IDLE_FRAME_MS
    const frameIndex = anim.loop
      ? Math.floor(elapsed / frameMs) % anim.frameCount
      : Math.min(Math.floor(elapsed / frameMs), anim.frameCount - 1)

    // Dotted radius indicators — drawn BEFORE the barrel so the sprite sits
    // on top. Outer ring = blast/effect radius (orange), inner ring = trigger
    // radius (red). For trap types without a distinct trigger zone only the
    // single outer ring renders.
    if (state.triggerRadius && state.triggerRadius !== state.radius) {
      this.drawTrapRadiusRing(state.x, state.y, state.radius, 'rgba(251, 146, 60, 0.75)')
      this.drawTrapRadiusRing(state.x, state.y, state.triggerRadius, 'rgba(239, 68, 68, 0.75)')
    } else {
      this.drawTrapRadiusRing(state.x, state.y, state.radius, 'rgba(251, 146, 60, 0.75)')
    }

    this.drawObjectFrame(anim, frameIndex, state.x + offX, state.y + offY, scale)

    if (this.state.selectedTrapId === id) {
      const ctx = this.ctx
      ctx.save()
      ctx.globalAlpha = 1
      ctx.beginPath()
      ctx.arc(state.x, state.y, state.radius + 3, 0, Math.PI * 2)
      ctx.strokeStyle = '#ffffff'
      ctx.lineWidth = 2 / this.camera.zoom
      ctx.setLineDash([])
      ctx.stroke()
      ctx.restore()
    }
    return false
  }

  private drawTrapRadiusRing(x: number, y: number, radius: number, color: string) {
    const ctx = this.ctx
    ctx.save()
    ctx.strokeStyle = color
    ctx.lineWidth = 2 / this.camera.zoom
    // Tiny dash pattern reads as dots rather than dashes. Length scales with
    // zoom so the rhythm stays consistent whether zoomed in or out.
    ctx.setLineDash([1 / this.camera.zoom, 4 / this.camera.zoom])
    ctx.lineCap = 'round'
    ctx.beginPath()
    ctx.arc(x, y, radius, 0, Math.PI * 2)
    ctx.stroke()
    ctx.restore()
  }

  private drawObjectFrame(
    anim: { sheet: HTMLImageElement; frameWidth: number; frameHeight: number },
    frameIndex: number,
    centerX: number,
    centerY: number,
    scale: number,
  ) {
    if (!anim.sheet.complete || anim.sheet.naturalWidth === 0) return
    const ctx = this.ctx
    ctx.imageSmoothingEnabled = false
    const destW = anim.frameWidth * scale
    const destH = anim.frameHeight * scale
    ctx.drawImage(
      anim.sheet,
      frameIndex * anim.frameWidth, 0,
      anim.frameWidth, anim.frameHeight,
      centerX - destW / 2, centerY - destH / 2,
      destW, destH,
    )
  }

  // Maps a trap type to the asset key under assets/objects/. Returns '' when
  // the type has no sprite set registered (falls through to procedural).
  // Extend the switch as more trap types gain sprite treatment.
  private spriteKeyForTrapType(type: TrapSnapshot['type']): string {
    switch (type) {
      case 'explosive_trap':
        return getObjectSpriteSet('explosive_trap') ? 'explosive_trap' : ''
      case 'marker_trap':
        return getObjectSpriteSet('marker_trap') ? 'marker_trap' : ''
      case 'fire_pit':
        return getObjectSpriteSet('fire_pit') ? 'fire_pit' : ''
      case 'caltrops':
        return getObjectSpriteSet('caltrops') ? 'caltrops' : ''
      default:
        return ''
    }
  }

  private hasTrapSprites(type: TrapSnapshot['type']): boolean {
    return this.spriteKeyForTrapType(type) !== ''
  }

  // Trap body rendering — shared between live and fading-out paths. Caller
  // owns ctx.save/restore and globalAlpha so the same snapshot can be drawn
  // at different opacities without per-type duplication.
  private renderTrapBody(ctx: CanvasRenderingContext2D, trap: TrapSnapshot) {
    const ownerColor = this.state.getPlayerColor(trap.ownerId) ?? '#94a3b8'
    const { x, y } = trap
    const r = trap.radius
    switch (trap.type) {
      case 'caltrops':
        this.drawTrapCaltrops(ctx, x, y, r, ownerColor)
        break
      case 'fire_pit':
        this.drawTrapFirePit(ctx, x, y, r, ownerColor)
        break
      case 'explosive_trap':
        this.drawTrapExplosive(ctx, x, y, r, ownerColor, trap.triggered ?? false)
        break
      case 'marker_trap':
        this.drawTrapMarker(ctx, x, y, r, ownerColor)
        break
    }
  }

  // Selection highlight: bright white ring just outside the trap radius, at
  // full alpha. Only drawn for live (selectable) traps — fading-out traps
  // have already been cleared from selection.
  private renderTrapSelectionRing(ctx: CanvasRenderingContext2D, trap: TrapSnapshot) {
    ctx.globalAlpha = 1
    ctx.beginPath()
    ctx.arc(trap.x, trap.y, trap.radius + 3, 0, Math.PI * 2)
    ctx.strokeStyle = '#ffffff'
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.setLineDash([])
    ctx.stroke()
  }

  // ── caltrops ─────────────────────────────────────────────────────────────
  // Subtle cluster of iron-grey dots inside a dashed radius ring, with the
  // owner color blended into the ring stroke.
  private drawTrapCaltrops(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    r: number,
    ownerColor: string,
  ) {
    // Dashed radius ring
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.strokeStyle = this.withAlpha(ownerColor, 0.45)
    ctx.lineWidth = 1.2 / this.camera.zoom
    ctx.setLineDash([5 / this.camera.zoom, 5 / this.camera.zoom])
    ctx.stroke()
    ctx.setLineDash([])

    // Scattered dot cluster — a fixed pseudo-random pattern seeded by trap id
    // (just use a static offset list so there's zero runtime randomness).
    const dots: [number, number][] = [
      [0, 0], [-14, -8], [12, -10], [-8, 12], [16, 6],
      [-18, 4], [6, 18], [-10, -20], [20, -4], [0, -16],
    ]
    ctx.fillStyle = this.withAlpha(ownerColor, 0.55)
    const dotR = Math.max(1.5, 2.5 / this.camera.zoom)
    for (const [dx, dy] of dots) {
      const px = x + dx * (r / 60)
      const py = y + dy * (r / 60)
      ctx.beginPath()
      ctx.arc(px, py, dotR, 0, Math.PI * 2)
      ctx.fill()
    }
  }

  // ── fire_pit ──────────────────────────────────────────────────────────────
  // Radial gradient (orange core → red edge → transparent) with an
  // owner-colored outer ring stroke.
  private drawTrapFirePit(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    r: number,
    ownerColor: string,
  ) {
    // Radial fill
    const grad = ctx.createRadialGradient(x, y, 0, x, y, r)
    grad.addColorStop(0, 'rgba(255, 180, 60, 0.35)')
    grad.addColorStop(0.55, 'rgba(220, 60, 10, 0.22)')
    grad.addColorStop(1, 'rgba(200, 30, 0, 0)')
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.fillStyle = grad
    ctx.fill()

    // Owner-tinted outer ring
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.strokeStyle = this.withAlpha(ownerColor, 0.5)
    ctx.lineWidth = 1.5 / this.camera.zoom
    ctx.setLineDash([8 / this.camera.zoom, 4 / this.camera.zoom])
    ctx.stroke()
    ctx.setLineDash([])
  }

  // ── explosive_trap ────────────────────────────────────────────────────────
  // Armed state: red dashed ring + small center pip.
  // Triggered state: one-frame burst — bright radial flash at explosion radius.
  private drawTrapExplosive(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    r: number,
    ownerColor: string,
    triggered: boolean,
  ) {
    if (triggered) {
      // One-frame detonation burst: radial gradient from bright yellow/white
      // core to transparent edge, drawn at full explosion radius.
      const burst = ctx.createRadialGradient(x, y, 0, x, y, r)
      burst.addColorStop(0, 'rgba(255, 255, 200, 0.9)')
      burst.addColorStop(0.3, 'rgba(255, 160, 0, 0.75)')
      burst.addColorStop(0.7, 'rgba(220, 40, 0, 0.45)')
      burst.addColorStop(1, 'rgba(180, 20, 0, 0)')
      ctx.beginPath()
      ctx.arc(x, y, r, 0, Math.PI * 2)
      ctx.fillStyle = burst
      ctx.globalAlpha = 0.85 // Override the faded alpha — burst should punch through
      ctx.fill()
      return
    }

    // Armed: dashed red ring
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.strokeStyle = 'rgba(239, 68, 68, 0.75)'
    ctx.lineWidth = 1.5 / this.camera.zoom
    ctx.setLineDash([6 / this.camera.zoom, 3 / this.camera.zoom])
    ctx.stroke()
    ctx.setLineDash([])

    // Owner-color fill hint (very subtle)
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.fillStyle = 'rgba(239, 68, 68, 0.05)'
    ctx.fill()

    // Center pip — a small filled circle indicating placement point
    const pipR = Math.max(3, 5 / this.camera.zoom)
    ctx.beginPath()
    ctx.arc(x, y, pipR, 0, Math.PI * 2)
    ctx.fillStyle = this.withAlpha(ownerColor, 0.8)
    ctx.fill()
    ctx.strokeStyle = 'rgba(239, 68, 68, 0.9)'
    ctx.lineWidth = 1 / this.camera.zoom
    ctx.stroke()
  }

  // ── marker_trap ───────────────────────────────────────────────────────────
  // Purple dashed ring + a small diamond/cross sigil at center — looks like a
  // debuff marker, not a weapon.
  private drawTrapMarker(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    r: number,
    ownerColor: string,
  ) {
    // Subtle purple fill
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.fillStyle = 'rgba(139, 92, 246, 0.07)'
    ctx.fill()

    // Dashed ring in owner color blended toward purple
    ctx.beginPath()
    ctx.arc(x, y, r, 0, Math.PI * 2)
    ctx.strokeStyle = this.withAlpha(ownerColor, 0.5)
    ctx.lineWidth = 1.2 / this.camera.zoom
    ctx.setLineDash([7 / this.camera.zoom, 4 / this.camera.zoom])
    ctx.stroke()
    ctx.setLineDash([])

    // Center sigil: two crossed lines + small diamond outline
    const sigilSize = Math.max(5, 8 / this.camera.zoom)
    ctx.strokeStyle = 'rgba(167, 139, 250, 0.85)'
    ctx.lineWidth = 1 / this.camera.zoom

    // Horizontal + vertical cross
    ctx.beginPath()
    ctx.moveTo(x - sigilSize, y)
    ctx.lineTo(x + sigilSize, y)
    ctx.moveTo(x, y - sigilSize)
    ctx.lineTo(x, y + sigilSize)
    ctx.stroke()

    // Diamond (45° square)
    const d = sigilSize * 0.65
    ctx.beginPath()
    ctx.moveTo(x, y - d)
    ctx.lineTo(x + d, y)
    ctx.lineTo(x, y + d)
    ctx.lineTo(x - d, y)
    ctx.closePath()
    ctx.stroke()
  }

  // ──────────────────────────────────────────────────────────────────────────
  // Rallying banner rendering
  //
  // Banners render BELOW units so they never occlude gameplay.  Each banner:
  //   1. Radius circle — soft fill + owner-colored outline, distinguishable
  //      from selection ellipses (which are green) and attack ranges.
  //   2. Flag sprite  — a small pole + triangular flag drawn in world space
  //      at the plant position; the flag face is filled with the owner color.
  //   3. Alpha fade   — globalAlpha scales linearly from 1 → 0 as
  //      remainingSeconds → 0, providing a smooth decay indicator with no
  //      extra UI clutter.
  //
  // The maximum banner lifetime is read from the first snapshot that carries
  // the banner; we clamp alpha between 0.15 and 1.0 so the banner is still
  // visually present until it actually expires rather than fading to nothing
  // while a few seconds remain.
  // ──────────────────────────────────────────────────────────────────────────
  private drawBanners(banners: BannerSnapshot[]) {
    if (banners.length === 0) {
      if (this.bannerInitialDurations.size > 0) this.bannerInitialDurations.clear()
      return
    }

    const ctx = this.ctx
    const seen = new Set<number>()

    for (const banner of banners) {
      seen.add(banner.id)
      const ownerColor = this.state.getPlayerColor(banner.ownerId) ?? '#a78bfa'
      // Server doesn't ship the original duration, so cache the largest
      // remainingSeconds we've ever seen for this banner id and use that as
      // the fade denominator. Auto-adapts to any server-side tuning of
      // bannerDurationSeconds without a client-side constant.
      const recorded = this.bannerInitialDurations.get(banner.id) ?? 0
      if (banner.remainingSeconds > recorded) {
        this.bannerInitialDurations.set(banner.id, banner.remainingSeconds)
      }
      const initial = this.bannerInitialDurations.get(banner.id) ?? banner.remainingSeconds
      const alpha = Math.max(0.15, Math.min(1, banner.remainingSeconds / initial))

      ctx.save()
      ctx.globalAlpha = alpha

      // ── 1. Radius fill + outline ──────────────────────────────────────────
      ctx.beginPath()
      ctx.arc(banner.x, banner.y, banner.radius, 0, Math.PI * 2)
      ctx.fillStyle = this.withAlpha(ownerColor, 0.08)
      ctx.fill()

      ctx.strokeStyle = this.withAlpha(ownerColor, 0.55)
      ctx.lineWidth = 1.5 / this.camera.zoom
      ctx.setLineDash([6 / this.camera.zoom, 4 / this.camera.zoom])
      ctx.stroke()
      ctx.setLineDash([])

      // ── 2. Flag sprite ────────────────────────────────────────────────────
      // All measurements in world pixels; scale with camera zoom via lineWidth
      // conventions already used in this file.
      const poleH = 18   // pole height in world px
      const poleW = 1.5 / this.camera.zoom
      const flagW = 10   // flag width (horizontal)
      const flagH = 7    // flag height (vertical)
      const poleX = banner.x
      const poleTopY = banner.y - poleH
      const poleBotY = banner.y

      // Dark outline pass so the sprite reads over any terrain color.
      ctx.save()
      ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'
      ctx.lineWidth = poleW + 2 / this.camera.zoom
      ctx.lineCap = 'round'
      ctx.beginPath()
      ctx.moveTo(poleX, poleBotY)
      ctx.lineTo(poleX, poleTopY)
      ctx.stroke()
      ctx.restore()

      // Pole
      ctx.strokeStyle = '#cbd5e1'
      ctx.lineWidth = poleW
      ctx.lineCap = 'round'
      ctx.beginPath()
      ctx.moveTo(poleX, poleBotY)
      ctx.lineTo(poleX, poleTopY)
      ctx.stroke()

      // Flag face — filled triangle pointing right
      ctx.fillStyle = ownerColor
      ctx.strokeStyle = 'rgba(15, 23, 42, 0.7)'
      ctx.lineWidth = 1 / this.camera.zoom
      ctx.lineJoin = 'round'
      ctx.beginPath()
      ctx.moveTo(poleX,          poleTopY)           // top-left (at pole)
      ctx.lineTo(poleX + flagW,  poleTopY + flagH / 2) // right tip
      ctx.lineTo(poleX,          poleTopY + flagH)   // bottom-left (at pole)
      ctx.closePath()
      ctx.fill()
      ctx.stroke()

      ctx.restore()
    }

    // Drop cached durations for banners no longer in the snapshot so the
    // map doesn't grow unbounded across many planted-then-expired banners.
    for (const id of this.bannerInitialDurations.keys()) {
      if (!seen.has(id)) this.bannerInitialDurations.delete(id)
    }
  }

  private drawUnits(
    units: Array<{
      id: number
      unitType?: string
      rank?: string
      recentRankUpSeconds?: number
      path?: string
      status?: string
      x: number
      y: number
      targetX?: number
      targetY?: number
      moving?: boolean
      hp?: number
      maxHp?: number
      shield?: number
      maxShield?: number
      attackSpeed?: number
      activeBuffs?: { id: string; stacks?: number }[]
      activeDebuffs?: { id: string; stacks?: number }[]
      color?: string
      visible?: boolean
      ownerId?: string
      carriedResourceType?: string
      perkIds?: string[]
    }>,
  ) {
    const ctx = this.ctx
    const activeUnitIds = new Set<number>()

    for (const unit of units) {
      if (unit.visible === false) {
        continue
      }
      activeUnitIds.add(unit.id)

      const selected = this.state.selectedUnitIds.has(unit.id)
      const isInspected = this.state.inspectedEnemyUnitId === unit.id
      const isHoveredEnemy = this.state.hoveredEnemyUnitId === unit.id
      const unitDef = UNIT_DEF_MAP.get(unit.unitType ?? '')
      const unitBounds = getUnitRenderBounds(unitDef)
      const halfWidth = unitBounds
        ? Math.max(Math.abs(unitBounds.minX), Math.abs(unitBounds.maxX))
        : 13
      const bottomOffset = unitBounds?.maxY ?? 12
      const spriteSet = getUnitSpriteSet(unit.path, unit.unitType)
      // PixelLab canvas has transparent padding below the feet — shift the
      // ring up by that amount so it sits under the visible feet, not the
      // canvas edge. No-op for units rendering procedurally.
      const ringLift = spriteSet
        ? spriteSet.size.height * UNIT_SPRITE_SCALE * UNIT_SPRITE_BOTTOM_PADDING
        : 0
      const selectionRadiusX = Math.max(15, halfWidth + 2)
      const selectionRadiusY = Math.max(8, Math.min(12, selectionRadiusX * 0.52))
      const selectionCenterY = unit.y + bottomOffset - selectionRadiusY * 0.35 - ringLift

      // Perk aura rings — dashed circle around the unit for each unit-centered
      // aura perk it carries (e.g. Guardian Aura). Triggered perks (Last Stand)
      // only render while their id is present in activeBuffs. Drawn before the
      // selection ellipse so the sprite can sit cleanly on top.
      if (unit.perkIds && unit.perkIds.length > 0) {
        const activeBuffIds = unit.activeBuffs && unit.activeBuffs.length > 0
          ? new Set(unit.activeBuffs.map((b) => b.id))
          : undefined
        for (const perkId of unit.perkIds) {
          const radius = getPerkAuraRadius(perkId, activeBuffIds)
          if (radius == null) continue
          this.drawAuraRing(unit.x, selectionCenterY, radius, unit.color || '#fef08a')
        }
      }

      if (selected) {
        ctx.strokeStyle = '#22c55e'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.beginPath()
        ctx.ellipse(unit.x, selectionCenterY, selectionRadiusX, selectionRadiusY, 0, 0, Math.PI * 2)
        ctx.stroke()
      }

      // Enemy hover ring (orange dashed)
      if (isHoveredEnemy && !isInspected) {
        ctx.save()
        ctx.strokeStyle = 'rgba(251, 146, 60, 0.9)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([5 / this.camera.zoom, 4 / this.camera.zoom])
        ctx.beginPath()
        ctx.ellipse(unit.x, selectionCenterY, selectionRadiusX + 1, selectionRadiusY + 1, 0, 0, Math.PI * 2)
        ctx.stroke()
        ctx.restore()
      }

      // Inspected enemy ring (red solid)
      if (isInspected) {
        ctx.strokeStyle = '#ef4444'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.beginPath()
        ctx.ellipse(unit.x, selectionCenterY, selectionRadiusX, selectionRadiusY, 0, 0, Math.PI * 2)
        ctx.stroke()
      }

      // Visible head-top Y — anchor for all overhead UI (health bar,
      // chevrons, buffs, debuffs, rank-up text). Sprite-aware so big sprites
      // push the UI clear of the head instead of floating over the chest.
      const headTopY = spriteSet
        ? unit.y + bottomOffset - spriteSet.size.height * UNIT_SPRITE_SCALE * (1 - UNIT_SPRITE_TOP_PADDING)
        : unit.y - (unitBounds ? Math.abs(unitBounds.minY) : 14)

      // Health bar always visible for all units
      const isEnemy = unit.ownerId !== this.state.localPlayerId
      this.drawSelectedUnitHealthBar(unit, isEnemy, headTopY)
      this.drawUnitRankChevrons(unit, headTopY)

      if (unit.status === 'Attacking') {
        this.drawConfiguredUnitAttackEffect(unit)
      }

      this.drawUnitRankUpBurst(unit, headTopY)

      // Active-buff indicators (momentum, relentless, whirlwind, berserk_state, …).
      // Populated by server activeBuffIconsLocked(); icons come from action-icons.json.
      if (unit.activeBuffs && unit.activeBuffs.length > 0) {
        this.drawUnitActiveBuffs(unit.x, headTopY, unit.activeBuffs)
      }
      if (unit.activeDebuffs && unit.activeDebuffs.length > 0) {
        this.drawUnitActiveDebuffs(unit.x, headTopY, unit.activeDebuffs)
      }

      const unitColor = unit.color || 'lime'
      const unitRankColor = this.getRankColor(unit.rank)
      const unitRenderDef = resolveUnitRenderDef(unitDef, unit.path)
      if (spriteSet) {
        const attackFacing =
          unit.status === 'Attacking'
            ? findAttackFacing(
                unit,
                unitDef,
                units,
                this.state.mapConfig.buildings,
                this.state.mapConfig.cellSize,
              )
            : null
        // Sync attack-animation speed to the unit's effective attackSpeed
        // (attacks/sec, includes rank/perk bonuses from the server). One
        // full animation cycle = one attack cooldown.
        let attackFrameDurationMs: number | undefined
        const attackingAnim = spriteSet.animations.get('attacking')
        const effectiveAttackSpeed = unit.attackSpeed ?? unitDef?.attackSpeed
        if (attackingAnim && effectiveAttackSpeed && effectiveAttackSpeed > 0) {
          attackFrameDurationMs = 1000 / effectiveAttackSpeed / attackingAnim.frameCount
        }
        const anim = this.unitAnim.sample(
          unit.id,
          unit.x,
          unit.y,
          unit.status,
          unit.moving,
          attackFacing,
          attackFrameDurationMs,
          this.renderTime,
          unit.carriedResourceType,
        )
        const frame = getUnitFrame(spriteSet, anim.animation, anim.direction, anim.frameIndex)
        if (frame) {
          const w = spriteSet.size.width * UNIT_SPRITE_SCALE
          const h = spriteSet.size.height * UNIT_SPRITE_SCALE
          const dx = unit.x - w / 2
          const dy = unit.y + bottomOffset - h
          const prevSmoothing = ctx.imageSmoothingEnabled
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(
            frame.image,
            frame.srcX, frame.srcY, frame.srcW, frame.srcH,
            dx, dy, w, h,
          )
          ctx.imageSmoothingEnabled = prevSmoothing
          continue
        }
      }

      if (unitRenderDef) {
        for (const layer of unitRenderDef.layers) {
          ctx.fillStyle =
            layer.color === 'player' ? unitColor :
            layer.color === 'rank'   ? unitRankColor :
            layer.color === 'rankLight' ? this.getRankColor(unit.rank, 'light') :
            layer.color === 'rankDark' ? this.getRankColor(unit.rank, 'dark') :
            layer.color
          if (layer.kind === 'circle') {
            ctx.beginPath()
            ctx.arc(unit.x + layer.cx, unit.y + layer.cy, layer.r, 0, Math.PI * 2)
            ctx.fill()
          } else if (layer.kind === 'poly') {
            ctx.beginPath()
            ctx.moveTo(unit.x + layer.points[0][0], unit.y + layer.points[0][1])
            for (let i = 1; i < layer.points.length; i++) {
              ctx.lineTo(unit.x + layer.points[i][0], unit.y + layer.points[i][1])
            }
            ctx.closePath()
            ctx.fill()
          }
        }
      } else {
        ctx.fillStyle = unitColor
        ctx.beginPath()
        ctx.arc(unit.x, unit.y, 10, 0, Math.PI * 2)
        ctx.fill()
      }
    }

    this.unitAnim.prune(activeUnitIds)
  }

  private drawConfiguredUnitAttackEffect(unit: {
    id: number
    x: number
    y: number
    unitType?: string
    ownerId?: string
  }) {
    const unitDef = unit.unitType ? UNIT_DEF_MAP.get(unit.unitType) : undefined
    const attackVisual = getResolvedUnitAttackVisual(unitDef)
    if (attackVisual.kind === 'projectile') {
      this.drawProjectileAttackEffect(unit, attackVisual)
      return
    }
    this.drawMeleeAttackEffect(unit, attackVisual)
  }

  private drawUnitRankUpBurst(
    unit: { x: number; y: number; recentRankUpSeconds?: number },
    headTopY: number,
  ) {
    if (!(unit.recentRankUpSeconds && unit.recentRankUpSeconds > 0)) {
      return
    }

    const ctx = this.ctx
    ctx.save()
    ctx.font = `${Math.max(10, 12 / this.camera.zoom)}px sans-serif`
    ctx.textAlign = 'center'
    ctx.lineWidth = 3 / this.camera.zoom
    ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'

    const alpha = Math.max(0, Math.min(unit.recentRankUpSeconds / 1.4, 1))
    ctx.fillStyle = `rgba(250, 204, 21, ${alpha})`
    const textY = headTopY - 18 - 8 / this.camera.zoom
    ctx.strokeText('RANK UP!', unit.x, textY)
    ctx.fillText('RANK UP!', unit.x, textY)

    ctx.restore()
  }

  private drawUnitRankChevrons(
    unit: { x: number; y: number; rank?: string },
    headTopY: number,
  ) {
    const count = unit.rank === 'bronze' ? 1 : unit.rank === 'silver' ? 2 : unit.rank === 'gold' ? 3 : 0
    if (count === 0) return

    const ctx = this.ctx
    const barWidth = 26
    const barHeight = 4
    const barX = unit.x - barWidth / 2
    const barY = headTopY - 8

    const halfWidth = 3.5
    const height = 2.5
    const spacing = 3
    const stackHeight = height + (count - 1) * spacing
    const cx = barX - 2 - halfWidth
    const topPeakY = barY + barHeight / 2 - stackHeight / 2
    const color = this.getRankColor(unit.rank)

    ctx.save()
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'

    for (let i = 0; i < count; i++) {
      const peakY = topPeakY + i * spacing
      const baseY = peakY + height

      ctx.strokeStyle = 'rgba(15, 23, 42, 0.95)'
      ctx.lineWidth = 2.5 / this.camera.zoom
      ctx.beginPath()
      ctx.moveTo(cx - halfWidth, baseY)
      ctx.lineTo(cx, peakY)
      ctx.lineTo(cx + halfWidth, baseY)
      ctx.stroke()

      ctx.strokeStyle = color
      ctx.lineWidth = 1.25 / this.camera.zoom
      ctx.stroke()
    }
    ctx.restore()
  }

  private getRankColor(rank?: string, tone: 'base' | 'light' | 'dark' = 'base') {
    return getRankToneColor(rank, tone)
  }

  private scaleBuildingAttackVisual(
    attackVisual: { originX: number; originY: number; effectLength: number },
  ) {
    const designPixelsPerCell = 100
    const scale = this.state.mapConfig.cellSize / designPixelsPerCell

    return {
      originX: attackVisual.originX * scale,
      originY: attackVisual.originY * scale,
      effectLength: attackVisual.effectLength * scale,
    }
  }

  private drawMeleeAttackEffect(
    unit: { id: number; x: number; y: number; unitType?: string; ownerId?: string },
    attackVisual: { originX: number; originY: number; effectLength: number },
  ) {
    const ctx = this.ctx
    const direction = this.getAttackDirection(unit) ?? { x: 1, y: 0 }
    const isEnemy = !!unit.ownerId && unit.ownerId !== this.state.localPlayerId
    const beamColor = isEnemy
      ? '#ef4444'
      : unit.ownerId
        ? (this.state.getPlayerColor(unit.ownerId) ?? '#ffffff')
        : '#ffffff'
    const unitDef = unit.unitType ? UNIT_DEF_MAP.get(unit.unitType) : undefined
    const attackSpeed = Math.max(unitDef?.attackSpeed ?? 1, 0.1)
    const cycleMs = 1000 / attackSpeed
    const t = (this.renderTime % cycleMs) / cycleMs
    const alpha = 1 - t
    const originX = unit.x + attackVisual.originX
    const originY = unit.y + attackVisual.originY
    const centerX = originX + direction.x * (attackVisual.effectLength + t * 4)
    const centerY = originY + direction.y * (attackVisual.effectLength + t * 4)
    const majorAngle = Math.atan2(direction.y, direction.x)
    const arcRadius = attackVisual.effectLength + t * 5
    const arcWidth = 0.95

    ctx.save()
    ctx.strokeStyle = this.withAlpha(beamColor, 0.95 * alpha)
    ctx.lineWidth = 3.5 / this.camera.zoom
    ctx.lineCap = 'round'
    ctx.beginPath()
    ctx.arc(centerX, centerY, arcRadius, majorAngle - arcWidth, majorAngle + arcWidth)
    ctx.stroke()

    if (t < 0.25) {
      const flashAlpha = (1 - t / 0.25) * 0.8
      ctx.fillStyle = this.withAlpha(beamColor, flashAlpha)
      ctx.beginPath()
      ctx.arc(centerX, centerY, 3 + t * 5, 0, Math.PI * 2)
      ctx.fill()
    }

    ctx.restore()
  }

  private drawProjectileAttackEffect(
    unit: { id: number; x: number; y: number; unitType?: string; ownerId?: string },
    attackVisual: { originX: number; originY: number; effectLength: number },
  ) {
    const ctx = this.ctx
    const direction = this.getAttackDirection(unit)
    if (!direction) return

    const unitDef = unit.unitType ? UNIT_DEF_MAP.get(unit.unitType) : undefined
    const attackSpeed = Math.max(unitDef?.attackSpeed ?? 1, 0.1)
    const cycleMs = 1000 / attackSpeed
    const blinkOn = (this.renderTime % cycleMs) < cycleMs * 0.4
    if (!blinkOn) return

    const lineLength = attackVisual.effectLength
    const beamColor = unit.ownerId ? (this.state.getPlayerColor(unit.ownerId) ?? '#ffffff') : '#ffffff'

    const startX = unit.x + attackVisual.originX
    const startY = unit.y + attackVisual.originY
    const endX = startX + direction.x * lineLength
    const endY = startY + direction.y * lineLength

    ctx.save()
    ctx.strokeStyle = this.withAlpha(beamColor, 0.28)
    ctx.lineWidth = 6 / this.camera.zoom
    ctx.lineCap = 'round'
    ctx.beginPath()
    ctx.moveTo(startX, startY)
    ctx.lineTo(endX, endY)
    ctx.stroke()

    ctx.strokeStyle = this.withAlpha(beamColor, 0.95)
    ctx.lineWidth = 2.5 / this.camera.zoom
    ctx.beginPath()
    ctx.moveTo(startX, startY)
    ctx.lineTo(endX, endY)
    ctx.stroke()
    ctx.restore()
  }

  private drawBuildingAttackEffect(building: BuildingTile) {
    const buildingDef = BUILDING_DEF_MAP.get(building.buildingType)
    const attackSpeed = Math.max(buildingDef?.attackSpeed ?? 0, 0)
    const attackRange = Math.max(buildingDef?.attackRange ?? 0, 0)
    if (attackSpeed <= 0 || attackRange <= 0 || !building.ownerId) return

    const cooldown = typeof building.metadata?.['attackCooldown'] === 'number'
      ? building.metadata['attackCooldown']
      : undefined
    if (cooldown === undefined || cooldown <= 0) return

    const cycleSeconds = 1 / attackSpeed
    const progress = 1 - Math.max(0, Math.min(cooldown / cycleSeconds, 1))
    if (progress > 0.4) return

      const attackVisual = getResolvedBuildingAttackVisual(buildingDef)
      const scaledAttackVisual = this.scaleBuildingAttackVisual(attackVisual)
      const direction = this.getBuildingAttackDirection(building, attackRange, attackVisual)
      if (!direction) return

      const cellSize = this.state.mapConfig.cellSize
      const worldX = building.x * cellSize
      const worldY = building.y * cellSize
      const startX = worldX + scaledAttackVisual.originX
      const startY = worldY + scaledAttackVisual.originY
      const endX = startX + direction.x * scaledAttackVisual.effectLength
      const endY = startY + direction.y * scaledAttackVisual.effectLength
      const beamColor = this.state.getPlayerColor(building.ownerId) ?? '#ffffff'
      const alpha = 1 - progress / 0.4

      const ctx = this.ctx
      if (attackVisual.kind === 'projectile') {
        ctx.save()
        ctx.strokeStyle = this.withAlpha(beamColor, 0.28 * alpha)
        ctx.lineWidth = 6 / this.camera.zoom
        ctx.lineCap = 'round'
        ctx.beginPath()
        ctx.moveTo(startX, startY)
        ctx.lineTo(endX, endY)
        ctx.stroke()

        ctx.strokeStyle = this.withAlpha(beamColor, 0.95 * alpha)
        ctx.lineWidth = 2.5 / this.camera.zoom
        ctx.beginPath()
        ctx.moveTo(startX, startY)
        ctx.lineTo(endX, endY)
        ctx.stroke()
        ctx.restore()
        return
      }

      const majorAngle = Math.atan2(direction.y, direction.x)
      ctx.save()
      ctx.strokeStyle = this.withAlpha(beamColor, 0.95 * alpha)
      ctx.lineWidth = 3.5 / this.camera.zoom
      ctx.lineCap = 'round'
      ctx.beginPath()
      ctx.arc(startX, startY, scaledAttackVisual.effectLength, majorAngle - 0.95, majorAngle + 0.95)
      ctx.stroke()
      ctx.restore()
    }

  private getBuildingAttackDirection(
    building: BuildingTile,
    attackRange: number,
    attackVisual?: { originX: number; originY: number },
  ) {
      if (!building.ownerId) return null

      const cellSize = this.state.mapConfig.cellSize
      const scaledAttackVisual = attackVisual
        ? this.scaleBuildingAttackVisual({
            originX: attackVisual.originX,
            originY: attackVisual.originY,
            effectLength: 0,
          })
        : null
      const fallbackOriginX = (building.width * cellSize) / 2
      const fallbackOriginY = (building.height * cellSize) * 0.28
      const originX = building.x * cellSize + (scaledAttackVisual?.originX ?? fallbackOriginX)
      const originY = building.y * cellSize + (scaledAttackVisual?.originY ?? fallbackOriginY)

    let bestDirection: { x: number; y: number } | null = null
    let bestDistanceSq = attackRange * attackRange

    for (const target of this.state.units) {
      if (!target.visible || !target.ownerId || target.ownerId === building.ownerId) continue

      const dx = target.x - originX
      const dy = target.y - originY
      const distanceSq = dx * dx + dy * dy
      if (distanceSq === 0 || distanceSq > bestDistanceSq) continue

      const distance = Math.sqrt(distanceSq)
      bestDistanceSq = distanceSq
      bestDirection = { x: dx / distance, y: dy / distance }
    }

    return bestDirection
  }

  private getAttackDirection(
    unit: { id: number; x: number; y: number; unitType?: string; ownerId?: string },
    maxRange?: number,
  ) {
    const attackRange = maxRange ?? (unit.unitType ? UNIT_DEF_MAP.get(unit.unitType)?.attackRange ?? 0 : 0)
    if (attackRange <= 0) return null

    let bestDirection: { x: number; y: number } | null = null
    let bestDistanceSq = Infinity

    for (const target of this.state.units) {
      if (!target.visible) continue
      if (target.id === unit.id) continue
      if (!unit.ownerId || !target.ownerId || target.ownerId === unit.ownerId) continue

      const dx = target.x - unit.x
      const dy = target.y - unit.y
      const distanceSq = dx * dx + dy * dy
      if (distanceSq > attackRange * attackRange || distanceSq >= bestDistanceSq || distanceSq === 0) continue

      const distance = Math.sqrt(distanceSq)
      bestDistanceSq = distanceSq
      bestDirection = { x: dx / distance, y: dy / distance }
    }

    for (const building of this.state.mapConfig.buildings) {
      if (!building.visible) continue
      if (!unit.ownerId || !building.ownerId || building.ownerId === unit.ownerId) continue

      const cellSize = this.state.mapConfig.cellSize
      const centerX = building.x * cellSize + (building.width * cellSize) / 2
      const centerY = building.y * cellSize + (building.height * cellSize) / 2
      const dx = centerX - unit.x
      const dy = centerY - unit.y
      const distanceSq = dx * dx + dy * dy
      if (distanceSq > attackRange * attackRange || distanceSq >= bestDistanceSq || distanceSq === 0) continue

      const distance = Math.sqrt(distanceSq)
      bestDistanceSq = distanceSq
      bestDirection = { x: dx / distance, y: dy / distance }
    }

    return bestDirection
  }

  private withAlpha(color: string, alpha: number) {
    const normalized = color.trim()
    if (normalized.startsWith('#')) {
      let hex = normalized.slice(1)
      if (hex.length === 3) {
        hex = hex.split('').map((char) => char + char).join('')
      }
      if (hex.length === 6) {
        const r = Number.parseInt(hex.slice(0, 2), 16)
        const g = Number.parseInt(hex.slice(2, 4), 16)
        const b = Number.parseInt(hex.slice(4, 6), 16)
        return `rgba(${r}, ${g}, ${b}, ${alpha})`
      }
    }
    return color
  }

  private drawSelectedUnitHealthBar(
    unit: {
      x: number
      y: number
      hp?: number
      maxHp?: number
      shield?: number
      maxShield?: number
    },
    isEnemy: boolean,
    headTopY: number,
  ) {
    const ctx = this.ctx
    const maxHp = Math.max(unit.maxHp ?? unit.hp ?? 100, 1)
    const hp = Math.max(0, Math.min(unit.hp ?? maxHp, maxHp))
    const healthPercent = hp / maxHp
    const barWidth = 26
    const barHeight = 4
    const barX = unit.x - barWidth / 2
    const barY = headTopY - 8

    let fillColor = '#22c55e'
    if (isEnemy) {
      fillColor = '#ef4444'
    } else if (healthPercent <= 0.35) {
      fillColor = '#ef4444'
    } else if (healthPercent <= 0.7) {
      fillColor = '#eab308'
    }

    ctx.save()
    ctx.fillStyle = 'rgba(2, 6, 23, 0.85)'
    ctx.fillRect(barX - 1, barY - 1, barWidth + 2, barHeight + 2)

    ctx.fillStyle = '#0f172a'
    ctx.fillRect(barX, barY, barWidth, barHeight)

    ctx.fillStyle = fillColor
    ctx.fillRect(barX, barY, barWidth * healthPercent, barHeight)

    // Shield overlay: a cyan bar stacked directly above the HP bar, scaled
    // against max-shield. Only drawn when the unit actually carries shield.
    const shield = Math.max(0, unit.shield ?? 0)
    const maxShield = Math.max(0, unit.maxShield ?? 0)
    if (maxShield > 0 && shield > 0) {
      const shieldY = barY - barHeight - 1
      ctx.fillStyle = '#0f172a'
      ctx.fillRect(barX, shieldY, barWidth, barHeight)
      ctx.fillStyle = '#38bdf8'
      ctx.fillRect(barX, shieldY, barWidth * Math.min(1, shield / maxShield), barHeight)
      ctx.strokeStyle = 'rgba(248, 250, 252, 0.6)'
      ctx.lineWidth = 1 / this.camera.zoom
      ctx.strokeRect(barX, shieldY, barWidth, barHeight)
    }

    ctx.strokeStyle = 'rgba(248, 250, 252, 0.8)'
    ctx.lineWidth = 1 / this.camera.zoom
    ctx.strokeRect(barX, barY, barWidth, barHeight)
    ctx.restore()
  }

  // ──────────────────────────────────────────────────────────────────────────
  // Active-buff and active-debuff indicator icons.
  //
  // Buffs (drawUnitActiveBuffs):
  //   UnitSnapshot.activeBuffs is a list of perk ids whose timed or
  //   conditional buff is currently active (populated by activeBuffIconsLocked
  //   in perks.go). Each id is looked up in PERK_DEF_MAP to find its icon id,
  //   which is then rendered as an SVG path from ACTION_ICON_MAP.
  //   EXTENSION POINT: add the perk id to activeBuffIconsLocked on the server.
  //
  // Debuffs (drawUnitActiveDebuffs):
  //   UnitSnapshot.activeDebuffs is a list of raw icon ids (not perk ids) —
  //   debuffs can land on units that don't own the causing perk, so there is
  //   no PERK_DEF_MAP indirection. Icons are looked up directly in
  //   ACTION_ICON_MAP. Currently exposed: debuff-taunted, debuff-weakened,
  //   debuff-marked. Rendered in a separate row above buffs with red tinting.
  // ──────────────────────────────────────────────────────────────────────────
  private drawUnitActiveBuffs(
    x: number,
    headTopY: number,
    buffs: { id: string; stacks?: number }[],
  ) {
    const ctx = this.ctx
    const iconSize = 12
    const gap = 2
    const totalWidth = buffs.length * iconSize + Math.max(0, buffs.length - 1) * gap
    // Sits one row above the health bar (bar at headTopY - 8, buffs at -24).
    const baseY = headTopY - 24
    let cursorX = x - totalWidth / 2

    for (const buff of buffs) {
      const def = PERK_DEF_MAP.get(buff.id)
      const iconId = def?.icon
      const iconPath = iconId ? ACTION_ICON_MAP.get(iconId) : undefined
      if (!iconPath) {
        cursorX += iconSize + gap
        continue
      }

      ctx.save()
      // Background pill so the icon is readable over any terrain/sprite.
      ctx.fillStyle = 'rgba(15, 23, 42, 0.8)'
      ctx.beginPath()
      ctx.arc(cursorX + iconSize / 2, baseY + iconSize / 2, iconSize / 2 + 1, 0, Math.PI * 2)
      ctx.fill()
      ctx.strokeStyle = 'rgba(251, 191, 36, 0.9)'
      ctx.lineWidth = 1 / this.camera.zoom
      ctx.stroke()

      // SVG paths in action-icons.json are authored in a 0..24 viewBox.
      // Map that into our icon-size square centered at (cursorX, baseY).
      ctx.translate(cursorX, baseY)
      ctx.scale(iconSize / 24, iconSize / 24)
      ctx.strokeStyle = '#fde68a'
      ctx.lineWidth = 2
      ctx.lineCap = 'round'
      ctx.lineJoin = 'round'
      ctx.stroke(new Path2D(iconPath))
      ctx.restore()

      // Stack count badge — only drawn when stacks >= 2 so single-instance
      // effects stay uncluttered. Amber fill to echo the buff-row border.
      if ((buff.stacks ?? 1) >= 2) {
        this.drawStackBadge(cursorX + iconSize, baseY, buff.stacks!, 'rgba(251, 191, 36, 0.95)')
      }

      cursorX += iconSize + gap
    }
  }

  // ──────────────────────────────────────────────────────────────────────────
  // Debuff icons — Option A: separate row placed 12 px above the buff row so
  // the two rows never crowd each other horizontally when a unit carries both.
  // baseY offset is -38 vs -26 for buffs (12 px row height + 0 px gap).
  // ──────────────────────────────────────────────────────────────────────────
  private drawUnitActiveDebuffs(
    x: number,
    headTopY: number,
    debuffs: { id: string; stacks?: number }[],
  ) {
    const ctx = this.ctx
    const iconSize = 12
    const gap = 2
    const totalWidth = debuffs.length * iconSize + Math.max(0, debuffs.length - 1) * gap
    // Debuffs sit one row above buffs (buffs at -24, debuffs at -36).
    const baseY = headTopY - 36
    let cursorX = x - totalWidth / 2

    for (const debuff of debuffs) {
      // Debuff ids are raw icon ids — look up directly in ACTION_ICON_MAP,
      // no PERK_DEF_MAP indirection needed.
      const iconPath = ACTION_ICON_MAP.get(debuff.id)
      if (!iconPath) {
        cursorX += iconSize + gap
        continue
      }

      ctx.save()
      // Background pill — same dark navy as buffs for visual consistency.
      ctx.fillStyle = 'rgba(15, 23, 42, 0.8)'
      ctx.beginPath()
      ctx.arc(cursorX + iconSize / 2, baseY + iconSize / 2, iconSize / 2 + 1, 0, Math.PI * 2)
      ctx.fill()
      // Red-400 border distinguishes debuffs from the amber buff border.
      ctx.strokeStyle = 'rgba(248, 113, 113, 0.9)'
      ctx.lineWidth = 1 / this.camera.zoom
      ctx.stroke()

      // SVG paths in action-icons.json are authored in a 0..24 viewBox.
      // Map that into our icon-size square centered at (cursorX, baseY).
      ctx.translate(cursorX, baseY)
      ctx.scale(iconSize / 24, iconSize / 24)
      // Red-200 icon stroke — clearly red, not amber.
      ctx.strokeStyle = '#fecaca'
      ctx.lineWidth = 2
      ctx.lineCap = 'round'
      ctx.lineJoin = 'round'
      ctx.stroke(new Path2D(iconPath))
      ctx.restore()

      if ((debuff.stacks ?? 1) >= 2) {
        this.drawStackBadge(cursorX + iconSize, baseY, debuff.stacks!, 'rgba(248, 113, 113, 0.95)')
      }

      cursorX += iconSize + gap
    }
  }

  // drawStackBadge renders a small circular number tag at the top-right of
  // an icon at (iconRightX, iconTopY). Shared between buff and debuff rows so
  // both use identical geometry; only the fill color differs (amber for
  // buffs, red for debuffs).
  private drawStackBadge(iconRightX: number, iconTopY: number, stacks: number, fill: string) {
    const ctx = this.ctx
    const r = 4
    // Anchor the badge centered on the icon's top-right corner so it clearly
    // reads as an overlay rather than a sibling icon.
    const cx = iconRightX
    const cy = iconTopY

    ctx.save()
    ctx.fillStyle = fill
    ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'
    ctx.lineWidth = 0.75
    ctx.beginPath()
    ctx.arc(cx, cy, r, 0, Math.PI * 2)
    ctx.fill()
    ctx.stroke()

    ctx.fillStyle = '#0f172a'
    ctx.font = 'bold 6px system-ui, -apple-system, Segoe UI, sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    // Render "N" for 2..9 and cap at "9+" for future higher stack caps so
    // the badge stays single-glyph even if maxDebuffStacks grows.
    ctx.fillText(stacks > 9 ? '9+' : String(stacks), cx, cy + 0.5)
    ctx.restore()
  }


  private drawBuildPlacementGhost() {
    const placement = this.state.buildPlacement
    if (!placement) return

    const ctx = this.ctx
    const { cellSize } = this.state.mapConfig
    const { cursorGridX, cursorGridY, gridW, gridH, valid } = placement

    const worldX = cursorGridX * cellSize
    const worldY = cursorGridY * cellSize
    const buildingDef = BUILDING_DEF_MAP.get(placement.buildingType)
    const renderDef = buildingDef?.render
    const sprite = getBuildingSprite(placement.buildingType)
    const footW = gridW * cellSize
    const footH = gridH * cellSize

    ctx.save()
    ctx.globalAlpha = 0.6

    const playerFill = valid ? (buildingDef?.color ?? '#1e40af') : '#dc2626'

    if (sprite) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(sprite, worldX, worldY, footW, footH)
      if (!valid) {
        ctx.globalAlpha = 0.35
        ctx.fillStyle = '#dc2626'
        ctx.fillRect(worldX, worldY, footW, footH)
      }
    } else if (!renderDef) {
      // No render def — solid fill fallback
      ctx.fillStyle = playerFill
      ctx.fillRect(worldX, worldY, footW, footH)
    } else {
      // Draw every layer explicitly, substituting 'player' with the valid/invalid tint color.
      for (const layer of renderDef.layers) {
        ctx.fillStyle = layer.color === 'player' ? playerFill : layer.color
        if (!('kind' in layer) || layer.kind === 'rect') {
          ctx.fillRect(
            worldX + layer.x * cellSize,
            worldY + layer.y * cellSize,
            layer.w * cellSize,
            layer.h * cellSize,
          )
        } else if (layer.kind === 'tri') {
          const s = cellSize / 6
          const tlX = worldX + layer.cx * cellSize + layer.sc * s
          const tlY = worldY + layer.cy * cellSize + layer.sr * s
          const bslash = (layer.sc + layer.sr) % 2 === 1
          ctx.beginPath()
          if (!bslash) {
            if (layer.h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX,     tlY + s) }
            else               { ctx.moveTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s); ctx.lineTo(tlX, tlY + s) }
          } else {
            if (layer.h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s) }
            else               { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX,     tlY + s); ctx.lineTo(tlX + s, tlY + s) }
          }
          ctx.closePath()
          ctx.fill()
          ctx.strokeStyle = ctx.fillStyle as string
          ctx.lineWidth = 1
          ctx.stroke()
        }
      }
    }

    ctx.globalAlpha = 0.9
    ctx.strokeStyle = valid ? '#93c5fd' : '#fca5a5'
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.setLineDash([8 / this.camera.zoom, 4 / this.camera.zoom])
    ctx.strokeRect(worldX, worldY, gridW * cellSize, gridH * cellSize)
    ctx.restore()
  }

  private drawSelectionBox() {
    if (!this.state.selectionBox.active) return

    const ctx = this.ctx
    const { left, top, right, bottom } = this.state.getSelectionBounds()
    const width = right - left
    const height = bottom - top

    ctx.fillStyle = 'rgba(80, 160, 255, 0.2)'
    ctx.strokeStyle = 'rgba(80, 160, 255, 0.9)'
    ctx.lineWidth = 1 / this.camera.zoom

    ctx.fillRect(left, top, width, height)
    ctx.strokeRect(left, top, width, height)
  }

  private drawMinimap(
    units: Array<{
      id: number
      x: number
      y: number
      ownerId?: string
      color?: string
      visible?: boolean
    }>,
  ) {
    const ctx = this.ctx
    const bounds = getMinimapBounds(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
    const { x, y, width: minimapWidth, height: minimapHeight } = bounds

    ctx.save()

    ctx.fillStyle = 'rgba(5, 10, 18, 0.82)'
    ctx.fillRect(x, y, minimapWidth, minimapHeight)

    for (const tile of this.state.mapConfig.terrain) {
      ctx.fillStyle = getTerrainColor(tile.terrain)
      ctx.fillRect(
        x + (tile.x / this.state.mapConfig.gridCols) * minimapWidth,
        y + (tile.y / this.state.mapConfig.gridRows) * minimapHeight,
        minimapWidth / this.state.mapConfig.gridCols,
        minimapHeight / this.state.mapConfig.gridRows,
      )
    }

    for (const tile of this.state.mapConfig.obstacles) {
      ctx.fillStyle = getObstacleColor(tile.obstacle)
      const tileW = tile.width ?? 1
      const tileH = tile.height ?? 1
      ctx.fillRect(
        x + (tile.x / this.state.mapConfig.gridCols) * minimapWidth,
        y + (tile.y / this.state.mapConfig.gridRows) * minimapHeight,
        (tileW / this.state.mapConfig.gridCols) * minimapWidth,
        (tileH / this.state.mapConfig.gridRows) * minimapHeight,
      )
    }

    for (const building of this.state.mapConfig.buildings) {
      if (!building.visible) continue

      const isLocalPlayerBuilding =
        building.occupied && building.ownerId === this.state.localPlayerId
      const ownerColor =
        building.occupied && building.ownerId
          ? this.state.getPlayerColor(building.ownerId)
          : null

      ctx.fillStyle = isLocalPlayerBuilding
        ? '#f8fafc'
        : getBuildingColor(building.buildingType, building.occupied, ownerColor)
      ctx.fillRect(
        x + (building.x / this.state.mapConfig.gridCols) * minimapWidth,
        y + (building.y / this.state.mapConfig.gridRows) * minimapHeight,
        (building.width / this.state.mapConfig.gridCols) * minimapWidth,
        (building.height / this.state.mapConfig.gridRows) * minimapHeight,
      )
    }

    ctx.strokeStyle = 'rgba(166, 191, 255, 0.35)'
    ctx.lineWidth = 1
    ctx.strokeRect(x, y, minimapWidth, minimapHeight)

    for (const unit of units) {
      if (unit.visible === false) {
        continue
      }

      const dotX = x + (unit.x / this.state.mapWidth) * minimapWidth
      const dotY = y + (unit.y / this.state.mapHeight) * minimapHeight
      const isLocalPlayerUnit =
        !!this.state.localPlayerId && unit.ownerId === this.state.localPlayerId

      ctx.fillStyle = isLocalPlayerUnit ? '#f8fafc' : (unit.color ?? '#94a3b8')
      ctx.beginPath()
      ctx.arc(dotX, dotY, isLocalPlayerUnit ? 2.8 : 2.2, 0, Math.PI * 2)
      ctx.fill()
    }

    const worldWidth = this.canvas.width / this.camera.zoom
    const worldHeight = this.canvas.height / this.camera.zoom
    const viewX = x + (this.camera.x / this.state.mapWidth) * minimapWidth
    const viewY = y + (this.camera.y / this.state.mapHeight) * minimapHeight
    const viewWidth = (worldWidth / this.state.mapWidth) * minimapWidth
    const viewHeight = (worldHeight / this.state.mapHeight) * minimapHeight

    ctx.strokeStyle = 'rgba(125, 211, 252, 0.95)'
    ctx.lineWidth = 1.5
    ctx.strokeRect(viewX, viewY, viewWidth, viewHeight)

    ctx.restore()
  }
}

// Returns the render definition to use for a unit, preferring a path-specific
// variant when one exists and has layers. Falls back to the base render.
function resolveUnitRenderDef(
  unitDef: UnitDef | undefined | null,
  path?: string,
): UnitRenderDef | undefined {
  if (!unitDef) return undefined
  if (path && path !== 'none') {
    const variant = unitDef.renderVariants?.[path]
    if (variant?.layers?.length) return variant
  }
  return unitDef.render
}

// Scans units and enemy buildings for the nearest target within (a slightly
// padded) attack range. Used to give attacking sprites the right facing when
// the server hasn't sent an explicit attack-target id.
function findAttackFacing(
  attacker: {
    x: number
    y: number
    ownerId?: string
  },
  attackerDef: UnitDef | undefined,
  units: Array<{
    id: number
    x: number
    y: number
    visible?: boolean
    ownerId?: string
    hp?: number
  }>,
  buildings: BuildingTile[],
  cellSize: number,
): { dx: number; dy: number } | null {
  const baseRange = attackerDef?.attackRange ?? 40
  const searchRange = baseRange + 16
  const rangeSq = searchRange * searchRange

  let bestSq = Infinity
  let bestDx = 0
  let bestDy = 0

  for (const other of units) {
    if (other.ownerId === attacker.ownerId) continue
    if (other.visible === false) continue
    if (other.hp !== undefined && other.hp <= 0) continue
    const dx = other.x - attacker.x
    const dy = other.y - attacker.y
    const d2 = dx * dx + dy * dy
    if (d2 < bestSq && d2 <= rangeSq) {
      bestSq = d2
      bestDx = dx
      bestDy = dy
    }
  }

  for (const building of buildings) {
    if (building.ownerId === attacker.ownerId) continue
    if (building.visible === false) continue
    const cx = building.x * cellSize + (building.width * cellSize) / 2
    const cy = building.y * cellSize + (building.height * cellSize) / 2
    const dx = cx - attacker.x
    const dy = cy - attacker.y
    const d2 = dx * dx + dy * dy
    if (d2 < bestSq && d2 <= rangeSq) {
      bestSq = d2
      bestDx = dx
      bestDy = dy
    }
  }

  return bestSq === Infinity ? null : { dx: bestDx, dy: bestDy }
}
