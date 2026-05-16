// src/game/rendering/CanvasRenderer.ts
import { GameState } from '../core/GameState'
import type { Unit } from '../core/GameState'
import type { FogOfWar } from '../core/FogOfWar'
import {
  DEFAULT_GRASS_COLOR,
  getBuildingColor,
  getObstacleColor,
  getTerrainColor,
} from '../maps/mapConfig'
import { BUILDING_DEF_MAP, getResolvedBuildingAttackVisual } from '../maps/buildingDefs'
import {
  CONSTRUCTION_FRAME_COUNT,
  DAMAGED_FRAMES_PER_TIER,
  DAMAGED_TIER_COUNT,
  TRAINING_FRAME_COUNT,
  getBuildingSprite,
  getConstructionFrameIndex,
  getConstructionSprite,
  getDamagedFrameIndex,
  getDamagedSprite,
  getDamagedTier,
  getTintedBuildingSprite,
  getTintedConstructionSprite,
  getTintedDamagedSprite,
  getTintedTrainingSprite,
  getTrainingFrameIndex,
  getTrainingSprite,
} from './buildingSprites'
import { getObstacleAnimationFrame, getObstacleSprite } from './obstacleSprites'
import { OBSTACLE_DEF_MAP } from '../maps/obstacleDefs'
import {
  drawAutoTiledTerrain,
  isTerrainTilesetReady,
} from './terrainTileset'
import { getResolvedUnitAttackVisual, getUnitBounds, getUnitBoundsFor, UNIT_DEF_MAP } from '../maps/unitDefs'
import type { UnitDef } from '../maps/unitDefs'
import type { BannerSnapshot, BuildingTile, EffectSnapshot, ObstacleTile, ProjectileSnapshot, TrapSnapshot } from '../network/protocol'
import { ENEMY_PLAYER_ID } from '../network/protocol'
import { drawProjectileForVariant } from './projectileSprites'
import { Camera } from './Camera'
import { getRankToneColor } from './rankColors'
import { ACTION_ICON_MAP } from '../maps/actionIconDefs'
import { getPerkAuraRadius, PERK_DEF_MAP } from '../maps/perkDefs'
import {
  getUnitFrame,
  getUnitSpriteSet,
  UNIT_SPRITE_SCALE,
  UNIT_SPRITE_TOP_PADDING,
  UNIT_SPRITE_BOTTOM_PADDING,
} from './unitSprites'
import { getObjectSpriteSet } from './objectSprites'
import { getEffectSprite } from './effectSprites'
import { getResourceIconImage } from './resourceSprites'
import { UnitAnimationController } from './unitAnimation'

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
  targetRect?: { x: number; y: number; width: number; height: number } | null,
): MinimapBounds {
  const aspectRatio = mapWidth / mapHeight

  // When the HUD provides a panel rect, fit the minimap inside it preserving
  // the map's aspect ratio (centered within the rect). Falls back to the
  // top-right corner placement when no panel rect is supplied.
  if (targetRect && targetRect.width > 0 && targetRect.height > 0) {
    const targetAspect = targetRect.width / targetRect.height
    let width: number
    let height: number
    if (targetAspect > aspectRatio) {
      height = targetRect.height
      width = height * aspectRatio
    } else {
      width = targetRect.width
      height = width / aspectRatio
    }
    return {
      x: targetRect.x + (targetRect.width - width) / 2,
      y: targetRect.y + (targetRect.height - height) / 2,
      width,
      height,
    }
  }

  const padding = 18
  const maxWidth = 220
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
  // Per-banner animation start timestamps (performance.now ms). Lets each
  // banner's idle loop hold its own phase instead of every banner on screen
  // strobing in lockstep.
  private bannerAnimStartedAt = new Map<number, number>()
  private readonly BANNER_IDLE_FRAME_MS = 120
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
    /** Extra render-scale factor from the server (perk-driven visual inflate).
     *  undefined / 0 → 1×. Multiplied onto the sprite set's base scale. */
    scaleMultiplier: number | undefined
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
  private fogCanvas: HTMLCanvasElement
  private fogCtx: CanvasRenderingContext2D
  private lastFogRevTick: number = -1

  // Floating damage numbers — client-side transient overlays world-locked to
  // the victim's position at damage-time. Populated by draining
  // GameState.damageEvents at the start of render(); fade + drift upward over
  // FLOATING_DAMAGE_DURATION_MS, then drop off the list.
  private floatingDamageNumbers: Array<{
    x: number
    y: number
    amount: number
    isFriendly: boolean
    startedAt: number
    /**
     * 'normal' renders white (enemy) / red (friendly) — the default. 'combined'
     * is the Marksman Double Shot yellow sum, drawn larger and slightly higher
     * than the normal numbers so it reads as the "totalled" hit on top of the
     * two individual white numbers. 'crit' draws a red circle behind the
     * number to mark a critical hit. 'minor' renders smaller and orange to
     * communicate ancillary splash damage (Reactive Flames etc.) without
     * dominating the main damage popup.
     */
    kind: 'normal' | 'combined' | 'crit' | 'minor'
    /**
     * Horizontal drift direction (±1) for 'minor' popups so they spray out
     * sideways and fall, distinguishing them from the upward-drifting normal
     * popups. Set at spawn time and held constant for the popup's lifetime.
     * Unused by other kinds.
     */
    xDriftSign?: -1 | 1
    /**
     * Sub-flavour for 'minor' popups: "fire" → orange (default),
     * "electric" → purple. Mirrors the server MinorDamageEventSnapshot.variant.
     */
    minorVariant?: string
  }> = []
  private readonly FLOATING_DAMAGE_DURATION_MS = 900
  private readonly FLOATING_DAMAGE_RISE_PX = 32
  // Minor popups: horizontal drift in either direction + accelerating fall.
  // Distinct trajectory so the eye picks them out as ancillary damage even
  // when interleaved with regular popups.
  private readonly FLOATING_DAMAGE_MINOR_X_PX = 28
  private readonly FLOATING_DAMAGE_MINOR_FALL_PX = 26

  // Floating resource numbers — spawned when a worker deposits at the
  // townhall (carriedAmount drops to 0). Mirrors the damage-number lifecycle
  // but renders an icon + amount in green (full-credit) or yellow/red
  // (reduced-credit) based on the event's capacityFraction.
  private floatingResourceNumbers: Array<{
    x: number
    y: number
    amount: number
    resourceId: string
    capacityFraction: number
    startedAt: number
  }> = []
  private readonly FLOATING_RESOURCE_DURATION_MS = 1100
  private readonly FLOATING_RESOURCE_RISE_PX = 36

  constructor(canvas: HTMLCanvasElement, state: GameState, camera: Camera) {
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas not supported')

    this.canvas = canvas
    this.ctx = ctx
    this.state = state
    this.camera = camera

    this.fogCanvas = document.createElement('canvas')
    this.fogCtx = this.fogCanvas.getContext('2d')!

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
    const sortedUnits = units.slice().sort((a, b) => a.y - b.y)

    // Drain new damage events (populated in GameState.applySnapshot) into
    // the floating-number list. World-lock each at the victim's head-top so
    // the number stays put even if the unit moves or dies mid-animation.
    if (this.state.damageEvents.length > 0) {
      for (const evt of this.state.damageEvents) {
        const fow = this.state.fow
        if (fow.cols > 0 && !fow.isClear(evt.x, evt.y, this.state.mapConfig.cellSize)) continue
        const bounds = getUnitBounds(UNIT_DEF_MAP.get(evt.unitType))
        // Combined Double Shot numbers float higher than the individual
        // shot numbers so they sit visually above them.
        const yOffset = evt.kind === 'combined' ? bounds.top - 14 : bounds.top
        const kind = evt.kind ?? 'normal'
        // Minor popups spray left/right + fall. Random sign per popup so a
        // burst of minor hits on the same unit fans out instead of stacking.
        const xDriftSign = kind === 'minor' ? (Math.random() < 0.5 ? -1 : 1) : undefined
        this.floatingDamageNumbers.push({
          x: evt.x,
          y: evt.y + yOffset,
          amount: evt.amount,
          isFriendly: evt.isFriendly,
          startedAt: evt.createdAt,
          kind,
          xDriftSign,
          minorVariant: evt.minorVariant,
        })
      }
      this.state.damageEvents.length = 0
    }

    // Drain resource deposit events into the floating-resource list. World-
    // locked at the deposit position (the worker's xy when carriedAmount
    // hit 0) so the number stays put even after the worker walks away.
    if (this.state.resourceDepositEvents.length > 0) {
      for (const evt of this.state.resourceDepositEvents) {
        this.floatingResourceNumbers.push({
          x: evt.x,
          y: evt.y - 16,
          amount: evt.amount,
          resourceId: evt.resourceId,
          capacityFraction: evt.capacityFraction,
          startedAt: evt.createdAt,
        })
      }
      this.state.resourceDepositEvents.length = 0
    }

    ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)

    // Background outside the map
    ctx.fillStyle = '#0a0a0a'
    ctx.fillRect(0, 0, this.canvas.width, this.canvas.height)

    ctx.save()
    ctx.scale(this.camera.zoom, this.camera.zoom)
    ctx.translate(-this.camera.x, -this.camera.y)

    this.drawMapBounds()
    this.drawMapBackground()
    this.drawBuildingSpawnMarkers()
    this.drawTraps(this.state.traps)
    this.drawBanners(this.state.banners)
    this.drawUnits(sortedUnits)
    // Effects sit on top of the caster body but underneath projectiles so
    // arrows and bolts always read clearly over the VFX layer.
    this.drawEffects(this.state.effects, units)
    // Drawn after units so arrows render on top of the firing unit's body.
    this.drawProjectiles(this.state.projectiles)

    if (this.state.fow.cols > 0) {
      const cs = this.state.mapConfig.cellSize
      this.updateFogCanvas(this.state.fow, cs)
      const marginPx = CanvasRenderer.FOG_MARGIN * cs
      ctx.filter = `blur(${CanvasRenderer.FOG_BLUR_PX}px)`
      ctx.drawImage(this.fogCanvas, -marginPx, -marginPx)
      ctx.filter = 'none'
    }

    // Move markers render above the fog so players always see where they
    // commanded their units to go, even into unexplored territory.
    this.drawMoveMarkers()
    this.drawBuildPlacementGhost()
    this.drawSelectionBox()
    this.drawFloatingDamageNumbers(renderTime)
    this.drawFloatingResourceNumbers(renderTime)

    ctx.restore()

    this.drawMinimap(units)
  }

  destroy() {
    window.removeEventListener('resize', this.resize)
    this.resizeObserver?.disconnect()
    this.resizeObserver = null
  }

  // Cells of fully-dark fog added outside the playable map on every side.
  // Large enough to cover sprite overflow and absorb the blur kernel so the
  // edge of the blur never hits the fog canvas boundary.
  private static readonly FOG_MARGIN = 4
  // Gaussian blur radius applied when compositing the fog onto the main
  // canvas. Softens hard cell edges into a smooth circular reveal shape.
  private static readonly FOG_BLUR_PX = 24

  private updateFogCanvas(fow: FogOfWar, cellSize: number): void {
    if (fow.revTick === this.lastFogRevTick) return
    this.lastFogRevTick = fow.revTick

    const margin = CanvasRenderer.FOG_MARGIN
    const cs = cellSize
    const canvasW = (fow.cols + 2 * margin) * cs
    const canvasH = (fow.rows + 2 * margin) * cs
    if (this.fogCanvas.width !== canvasW || this.fogCanvas.height !== canvasH) {
      this.fogCanvas.width = canvasW
      this.fogCanvas.height = canvasH
    }

    const ctx = this.fogCtx
    const mx = margin * cs
    const pw = fow.cols * cs
    const ph = fow.rows * cs

    ctx.clearRect(0, 0, canvasW, canvasH)

    // Dark borders outside the playable area — always fully opaque.
    ctx.fillStyle = 'rgba(0, 0, 0, 1.0)'
    ctx.fillRect(0, 0, canvasW, mx)           // top
    ctx.fillRect(0, canvasH - mx, canvasW, mx) // bottom
    ctx.fillRect(0, mx, mx, ph)               // left
    ctx.fillRect(canvasW - mx, mx, mx, ph)    // right

    // Playable cells — shifted inward by the margin.
    for (let gy = 0; gy < fow.rows; gy++) {
      for (let gx = 0; gx < fow.cols; gx++) {
        const state = fow.cellAt(gx, gy)
        if (state === 3) continue  // clear — leave transparent
        ctx.fillStyle = state === 0
          ? 'rgba(0, 0, 0, 1.0)'
          : 'rgba(0, 0, 0, 0.55)'
        ctx.fillRect((gx + margin) * cs, (gy + margin) * cs, cs, cs)
      }
    }
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

    if (tilesetReady) {
      drawAutoTiledTerrain(cctx, {
        gridCols,
        gridRows,
        cellSize,
        defaultTile,
        terrain,
        tiles,
      })
    } else {
      cctx.fillStyle = DEFAULT_GRASS_COLOR
      cctx.fillRect(0, 0, mapWidth, mapHeight)
      for (const tile of terrain) {
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

    this.ensureTerrainCache()
    if (this.terrainCache) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(this.terrainCache, 0, 0)
    }

    if (this.state.isBuildPlacementActive()) {
      this.drawGrid()
    }
  }

  // Renders a single obstacle (tree, rock, goldmine crate, etc). Extracted
  // from drawMapBackground so the main render loop can Y-sort obstacles
  // alongside buildings and units — e.g. a unit standing behind a tree
  // canopy is correctly occluded by the tree sprite.
  private drawSingleObstacle(tile: ObstacleTile) {
    const ctx = this.ctx
    const { cellSize } = this.state.mapConfig

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
    const shakeFrame = this.getTreeShakeFrame(tile)

    if (shakeFrame) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(
        shakeFrame.image,
        shakeFrame.srcX, shakeFrame.srcY, shakeFrame.srcW, shakeFrame.srcH,
        renderX, renderY, renderW, renderH,
      )
    } else if (sprite) {
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

  // Returns the current shake frame for a tree being chopped, or null when
  // the tree should render its static sprite. Slaved to the chopping worker's
  // animation cycle so the shake's impact aligns with the axe swing — the
  // shake plays during the trailing portion of each chop cycle and freezes
  // on frame 0 outside that window.
  private getTreeShakeFrame(tile: ObstacleTile) {
    if (tile.obstacle !== 'tree' || !tile.id) return null

    const CHOPPING_FRAMES = 4
    // Shake occupies the back half of the chop cycle, lining the impact and
    // recoil frames up with the axe-strike portion of the worker animation.
    const SHAKE_WINDOW_START = 0.5

    let chopper: number | null = null
    for (const u of this.state.units) {
      if (u.status === 'Chopping Wood' && u.workTargetId === tile.id) {
        chopper = u.id
        break
      }
    }
    if (chopper === null) return null

    const peek = this.unitAnim.peekAnimation(chopper)
    if (!peek || peek.animation !== 'chopping') return null

    const cycleMs = peek.frameDurationMs * CHOPPING_FRAMES
    const elapsed = Math.max(0, this.renderTime - peek.animStartedAt)
    const phase01 = (elapsed % cycleMs) / cycleMs
    if (phase01 < SHAKE_WINDOW_START) return null

    const shakeProgress = (phase01 - SHAKE_WINDOW_START) / (1 - SHAKE_WINDOW_START)
    const probe = getObstacleAnimationFrame(tile.obstacle, 'shaking', 0)
    if (!probe) return null
    const frameIndex = Math.min(probe.frameCount - 1, Math.floor(shakeProgress * probe.frameCount))
    return getObstacleAnimationFrame(tile.obstacle, 'shaking', frameIndex)
  }

  // Renders a single building (sprite / construction preview / procedural
  // fallback / overlays / health bar / attack effect). Extracted from
  // drawMapBackground so the main render loop can Y-sort buildings with
  // units and let units in front of a building occlude the building sprite.
  private drawSingleBuilding(building: BuildingTile) {
    if (!building.visible) return
    if (building.buildingType === 'enemy-spawnpoint') return

    const ctx = this.ctx
    const { cellSize } = this.state.mapConfig

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
      const isUnderConstruction = building.metadata?.['underConstruction'] === true
      // pendingStart: placed but no worker has arrived yet — render as a
      // transparent preview of the finished building. The construction
      // animation + progress bar only kick in once a worker arrives and the
      // server clears this flag.
      const isPendingStart = building.metadata?.['pendingStart'] === true
      const hp = building.metadata?.['hp'] as number | undefined
      const maxHp = building.metadata?.['maxHp'] as number | undefined
      const constructionProgress =
        hp !== undefined && maxHp !== undefined && maxHp > 0
          ? Math.max(0, Math.min(1, hp / maxHp))
          : 0
      const constructionSprite =
        isUnderConstruction && !isPendingStart ? getConstructionSprite(building.buildingType) : null
      // Training animation plays only on finished, owned, non-preview buildings
      // that are actively producing a unit. Server sets producingUnitType in
      // metadata while the head of the production queue is in flight.
      const isTraining =
        !isUnderConstruction &&
        !isPendingStart &&
        typeof building.metadata?.['producingUnitType'] === 'string' &&
        (building.metadata?.['producingUnitType'] as string).length > 0
      const trainingSprite = isTraining ? getTrainingSprite(building.buildingType) : null
      // Damage tier only applies to finished, non-preview buildings. Under
      // 90% HP picks a row from damaged.png (tier 0..3); above that the
      // normal sprite.png is used unchanged.
      const damagedTier =
        isUnderConstruction || isPendingStart ? -1 : getDamagedTier(constructionProgress)
      const damagedSprite =
        damagedTier >= 0 && !trainingSprite ? getDamagedSprite(building.buildingType) : null

      if (building.ghost) {
        ctx.save()
        ctx.globalAlpha = 0.5
        if (constructionSprite) {
          const tinted = ownerColor
            ? getTintedConstructionSprite(building.buildingType, ownerColor)
            : null
          const source: CanvasImageSource = tinted ?? constructionSprite
          const frameIndex = getConstructionFrameIndex(constructionProgress)
          const sheetW = constructionSprite.naturalWidth
          const sheetH = constructionSprite.naturalHeight
          const frameW = sheetW / CONSTRUCTION_FRAME_COUNT
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(source, frameIndex * frameW, 0, frameW, sheetH, spriteX, spriteY, spriteW, spriteH)
        } else if (sprite) {
          const tinted = ownerColor
            ? getTintedBuildingSprite(building.buildingType, ownerColor)
            : null
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(tinted ?? sprite, spriteX, spriteY, spriteW, spriteH)
        } else {
          ctx.fillStyle = playerFill
          ctx.fillRect(spriteX, spriteY, spriteW, spriteH)
        }
        ctx.globalCompositeOperation = 'saturation'
        ctx.fillStyle = 'rgba(128, 128, 128, 1)'
        ctx.fillRect(spriteX, spriteY, spriteW, spriteH)
        ctx.restore()
        return
      }

      // Selection / hover ring drawn *before* the sprite so the sprite
      // covers the top half, mimicking the wrap-around look of the unit ring.
      if (
        this.state.selectedBuildingId === building.id ||
        this.state.hoveredInteractableBuildingId === building.id
      ) {
        const isSelected = this.state.selectedBuildingId === building.id
        const ringDef = buildingDef?.selectionRing
        const ringOverride = ringDef ? { ...ringDef, cellSize } : undefined
        this.drawFootprintSelectionEllipse(
          worldX, worldY, width, height,
          isSelected ? 'selected' : 'hover',
          'center',
          ringOverride,
        )
      }

      if (isPendingStart) {
        ctx.save()
        ctx.globalAlpha = 0.4
      }

      if (constructionSprite) {
        ctx.imageSmoothingEnabled = false
        const tinted = ownerColor
          ? getTintedConstructionSprite(building.buildingType, ownerColor)
          : null
        const source: CanvasImageSource = tinted ?? constructionSprite
        const frameIndex = getConstructionFrameIndex(constructionProgress)
        const sheetW = constructionSprite.naturalWidth
        const sheetH = constructionSprite.naturalHeight
        const frameW = sheetW / CONSTRUCTION_FRAME_COUNT
        ctx.drawImage(
          source,
          frameIndex * frameW,
          0,
          frameW,
          sheetH,
          spriteX,
          spriteY,
          spriteW,
          spriteH,
        )
      } else if (trainingSprite) {
        ctx.imageSmoothingEnabled = false
        const tinted = ownerColor
          ? getTintedTrainingSprite(building.buildingType, ownerColor)
          : null
        const source: CanvasImageSource = tinted ?? trainingSprite
        const sheetW = trainingSprite.naturalWidth
        const sheetH = trainingSprite.naturalHeight
        const frameW = sheetW / TRAINING_FRAME_COUNT
        const frameIndex = getTrainingFrameIndex(this.renderTime)
        ctx.drawImage(
          source,
          frameIndex * frameW,
          0,
          frameW,
          sheetH,
          spriteX,
          spriteY,
          spriteW,
          spriteH,
        )
      } else if (damagedSprite) {
        ctx.imageSmoothingEnabled = false
        const tinted = ownerColor
          ? getTintedDamagedSprite(building.buildingType, ownerColor)
          : null
        const source: CanvasImageSource = tinted ?? damagedSprite
        const sheetW = damagedSprite.naturalWidth
        const sheetH = damagedSprite.naturalHeight
        const frameW = sheetW / DAMAGED_FRAMES_PER_TIER
        const tierH = sheetH / DAMAGED_TIER_COUNT
        const frameCol = getDamagedFrameIndex(this.renderTime)
        ctx.drawImage(
          source,
          frameCol * frameW,
          damagedTier * tierH,
          frameW,
          tierH,
          spriteX,
          spriteY,
          spriteW,
          spriteH,
        )
      } else if (sprite) {
        ctx.imageSmoothingEnabled = false
        const tinted = ownerColor
          ? getTintedBuildingSprite(building.buildingType, ownerColor)
          : null
        ctx.drawImage(tinted ?? sprite, spriteX, spriteY, spriteW, spriteH)
      } else if (!renderDef) {
        ctx.fillStyle = playerFill
        ctx.fillRect(spriteX, spriteY, spriteW, spriteH)
      } else {
        // Draw every layer explicitly; 'player' color is substituted with the owner color.
        // No base fill: unpainted areas are transparent so terrain shows through.
        // Layer coords are relative to the sprite-render box (so a townhall
        // with a 3x2 footprint + 3x3 sprite draws its tower one cell above
        // the footprint, matching the sprite.png).
        for (const layer of renderDef.layers) {
          ctx.fillStyle = layer.color === 'player' ? playerFill : layer.color
          if (!('kind' in layer) || layer.kind === 'rect') {
            ctx.fillRect(
              spriteX + layer.x * cellSize,
              spriteY + layer.y * cellSize,
              layer.w * cellSize,
              layer.h * cellSize,
            )
          } else if (layer.kind === 'tri') {
            const s = cellSize / 6
            const tlX = spriteX + layer.cx * cellSize + layer.sc * s
            const tlY = spriteY + layer.cy * cellSize + layer.sr * s
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

      if (isPendingStart) {
        ctx.restore()
      }

      if (isUnderConstruction && !isPendingStart) {
        // No construction spritesheet yet for this building type → fall back
        // to the procedural "dark overlay + dashed yellow border" treatment
        // so the under-construction state is still legible.
        if (!constructionSprite) {
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
        }

        // Construction progress bar above the building
        if (hp !== undefined && maxHp !== undefined && maxHp > 0) {
          const barH = 6 / this.camera.zoom
          const barX = worldX + inset
          const barY = worldY + inset - barH - 4 / this.camera.zoom
          const barW = width - inset * 2

          ctx.save()
          ctx.fillStyle = '#1e293b'
          ctx.fillRect(barX, barY, barW, barH)
          ctx.fillStyle = '#fbbf24'
          ctx.fillRect(barX, barY, barW * constructionProgress, barH)
          ctx.strokeStyle = 'rgba(251,191,36,0.5)'
          ctx.lineWidth = 1 / this.camera.zoom
          ctx.setLineDash([])
          ctx.strokeRect(barX, barY, barW, barH)
          ctx.restore()
        }
      } else if (!isPendingStart) {
        ctx.strokeStyle = 'rgba(15, 23, 42, 0.85)'
        ctx.lineWidth = 2 / this.camera.zoom
        if (!sprite) {
          ctx.strokeRect(worldX, worldY, width, height)
        }

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
      ? worldY + footprintH * 0.62
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
    const spriteSet = getObjectSpriteSet('rally_point')
    const idleAnim = spriteSet?.animations.get('idle')
    if (spriteSet && idleAnim) {
      const frameMs = idleAnim.frameDurationMs ?? this.BANNER_IDLE_FRAME_MS
      const elapsed = performance.now()
      const frameIndex = idleAnim.loop
        ? Math.floor(elapsed / frameMs) % idleAnim.frameCount
        : Math.min(Math.floor(elapsed / frameMs), idleAnim.frameCount - 1)
      const scale = spriteSet.scale ?? this.OBJECT_SPRITE_SCALE
      const offX = (spriteSet.offsetX ?? 0) * scale
      const offY = (spriteSet.offsetY ?? 0) * scale
      this.drawObjectFrame(idleAnim, frameIndex, x + offX, y + offY, scale)
      return
    }

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
          scaleMultiplier: trap.scaleMultiplier,
          spriteKey,
        })
      } else {
        state.x = trap.x
        state.y = trap.y
        state.radius = trap.radius
        state.triggerRadius = trap.triggerRadius
        state.variant = trap.variant
        state.scaleMultiplier = trap.scaleMultiplier
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
    state: { animation: 'idle' | 'exploding'; startedAt: number; x: number; y: number; radius: number; triggerRadius: number | undefined; variant: string | undefined; scaleMultiplier: number | undefined; spriteKey: string },
    stillInSnapshot: boolean,
    now: number,
  ): boolean {
    const spriteSet = getObjectSpriteSet(state.spriteKey)
    if (!spriteSet) return !stillInSnapshot

    // Base scale from the sprite manifest (or the renderer default), then
    // multiplied by any server-driven inflate (e.g. overload_protocol → 2×
    // for explosive_trap). scaleMultiplier is treated as 1 when absent or 0.
    const baseScale = spriteSet.scale ?? this.OBJECT_SPRITE_SCALE
    const mult = state.scaleMultiplier && state.scaleMultiplier > 0 ? state.scaleMultiplier : 1
    const scale = baseScale * mult
    // Positional nudge, authored in native sprite pixels. Scaled by the
    // effective render scale so the shift stays proportional when an object
    // also sets a `scale` override or a perk inflates it.
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
  // Banners render BELOW units so they never occlude gameplay. Each banner:
  //   1. Radius circle — soft fill + owner-colored outline, distinguishable
  //      from selection ellipses (which are green) and attack ranges.
  //   2. Flag sprite  — animated idle sprite (or procedural fallback) drawn
  //      in world space at the plant position.
  //
  // No alpha fade: the banner reads at full opacity for its entire lifetime
  // and disappears when the server drops it from the snapshot.
  // ──────────────────────────────────────────────────────────────────────────
  private drawBanners(banners: BannerSnapshot[]) {
    if (banners.length === 0) {
      if (this.bannerAnimStartedAt.size > 0) this.bannerAnimStartedAt.clear()
      return
    }

    const ctx = this.ctx
    const seen = new Set<number>()
    const spriteSet = getObjectSpriteSet('rallying_banner')
    const idleAnim = spriteSet?.animations.get('idle')
    const now = performance.now()

    for (const banner of banners) {
      seen.add(banner.id)
      const ownerColor = this.state.getPlayerColor(banner.ownerId) ?? '#a78bfa'

      ctx.save()

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

      // ── 2. Banner visual ──────────────────────────────────────────────────
      if (spriteSet && idleAnim) {
        let startedAt = this.bannerAnimStartedAt.get(banner.id)
        if (startedAt === undefined) {
          startedAt = now
          this.bannerAnimStartedAt.set(banner.id, startedAt)
        }
        const frameMs = idleAnim.frameDurationMs ?? this.BANNER_IDLE_FRAME_MS
        const elapsed = now - startedAt
        const frameIndex = idleAnim.loop
          ? Math.floor(elapsed / frameMs) % idleAnim.frameCount
          : Math.min(Math.floor(elapsed / frameMs), idleAnim.frameCount - 1)
        const scale = spriteSet.scale ?? this.OBJECT_SPRITE_SCALE
        const offX = (spriteSet.offsetX ?? 0) * scale
        const offY = (spriteSet.offsetY ?? 0) * scale
        this.drawObjectFrame(idleAnim, frameIndex, banner.x + offX, banner.y + offY, scale)
      } else {
        // Procedural fallback: simple pole + triangular flag face.
        const poleH = 18
        const poleW = 1.5 / this.camera.zoom
        const flagW = 10
        const flagH = 7
        const poleX = banner.x
        const poleTopY = banner.y - poleH
        const poleBotY = banner.y

        ctx.save()
        ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'
        ctx.lineWidth = poleW + 2 / this.camera.zoom
        ctx.lineCap = 'round'
        ctx.beginPath()
        ctx.moveTo(poleX, poleBotY)
        ctx.lineTo(poleX, poleTopY)
        ctx.stroke()
        ctx.restore()

        ctx.strokeStyle = '#cbd5e1'
        ctx.lineWidth = poleW
        ctx.lineCap = 'round'
        ctx.beginPath()
        ctx.moveTo(poleX, poleBotY)
        ctx.lineTo(poleX, poleTopY)
        ctx.stroke()

        ctx.fillStyle = ownerColor
        ctx.strokeStyle = 'rgba(15, 23, 42, 0.7)'
        ctx.lineWidth = 1 / this.camera.zoom
        ctx.lineJoin = 'round'
        ctx.beginPath()
        ctx.moveTo(poleX,          poleTopY)
        ctx.lineTo(poleX + flagW,  poleTopY + flagH / 2)
        ctx.lineTo(poleX,          poleTopY + flagH)
        ctx.closePath()
        ctx.fill()
        ctx.stroke()
      }

      ctx.restore()
    }

    // Drop animation phases for banners no longer in the snapshot so the
    // map doesn't grow unbounded across many planted-then-expired banners.
    for (const id of this.bannerAnimStartedAt.keys()) {
      if (!seen.has(id)) this.bannerAnimStartedAt.delete(id)
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
      workTargetId?: string
      actionFacingDx?: number
      actionFacingDy?: number
    }>,
  ) {
    const ctx = this.ctx
    const { cellSize } = this.state.mapConfig
    const activeUnitIds = new Set<number>()

    // Y-sort obstacles, buildings, and units together so a unit standing
    // behind a tree canopy / building (smaller feet-Y than the entity's
    // bottom edge) renders first and gets visually occluded by that sprite.
    // Anchor Y = bottom of the grid footprint for static entities; feet
    // position for units.
    type Entry = {
      anchorY: number
      obstacle?: ObstacleTile
      building?: BuildingTile
      unit?: (typeof units)[number]
    }
    const entries: Entry[] = []
    for (const tile of this.state.mapConfig.obstacles) {
      const gridH = tile.height ?? 1
      entries.push({
        anchorY: (tile.y + gridH) * cellSize,
        obstacle: tile,
      })
    }
    for (const building of this.state.mapConfig.buildings) {
      if (!building.visible) continue
      if (building.buildingType === 'enemy-spawnpoint') continue
      entries.push({
        anchorY: (building.y + building.height) * cellSize,
        building,
      })
    }
    for (const unit of units) {
      if (unit.visible === false) continue
      activeUnitIds.add(unit.id)
      entries.push({ anchorY: unit.y, unit })
    }
    entries.sort((a, b) => a.anchorY - b.anchorY)

    for (const entry of entries) {
      if (entry.obstacle) {
        this.drawSingleObstacle(entry.obstacle)
        continue
      }
      if (entry.building) {
        this.drawSingleBuilding(entry.building)
        continue
      }
      const unit = entry.unit!

      const selected = this.state.selectedUnitIds.has(unit.id)
      const isInspected = this.state.inspectedEnemyUnitId === unit.id
      const isHoveredEnemy = this.state.hoveredEnemyUnitId === unit.id
      const unitDef = UNIT_DEF_MAP.get(unit.unitType ?? '')
      const unitBounds = getUnitBoundsFor({ path: unit.path, unitType: unit.unitType })
      const halfWidth = unitBounds.halfWidth
      const bottomOffset = unitBounds.bottom
      const spriteSet = getUnitSpriteSet(unit.path, unit.unitType)
      // PixelLab canvas has transparent padding below the feet — shift the
      // ring up by that amount so it sits under the visible feet, not the
      // canvas edge.
      const ringLift = spriteSet
        ? spriteSet.size.height * UNIT_SPRITE_SCALE * UNIT_SPRITE_BOTTOM_PADDING
        : 0
      const selectionRadiusX = Math.max(15, halfWidth + 2)
      const selectionRadiusY = Math.max(8, Math.min(12, selectionRadiusX * 0.52))
      const ringOffsetX = unitBounds.ringOffsetX ?? 0
      const ringOffsetY = unitBounds.ringOffsetY ?? 0
      const selectionCenterX = unit.x + ringOffsetX
      const selectionCenterY = unit.y + bottomOffset - selectionRadiusY * 0.35 - ringLift + ringOffsetY

      // Perk aura rings — dashed circle around the unit for each unit-centered
      // aura perk it carries (e.g. Guardian Aura). Triggered perks (Last Stand)
      // only render while their id is present in activeBuffs. Drawn before the
      // selection ellipse so the sprite can sit cleanly on top.
      if (unit.perkIds && unit.perkIds.length > 0) {
        const activeBuffIds = unit.activeBuffs && unit.activeBuffs.length > 0
          ? new Set(unit.activeBuffs.map((b) => b.id))
          : undefined
        // Effects anchored to this unit gate effect-driven aura rings (e.g.
        // whirlwind_core's flash ring follows the EffectSnapshot, not buffs).
        const activeEffectNames = this.state.effects.length > 0
          ? new Set(
              this.state.effects
                .filter((e) => e.anchorUnitId === unit.id)
                .map((e) => e.name),
            )
          : undefined
        for (const perkId of unit.perkIds) {
          const radius = getPerkAuraRadius(perkId, activeBuffIds, activeEffectNames)
          if (radius == null) continue
          this.drawAuraRing(selectionCenterX, selectionCenterY, radius, unit.color || '#fef08a')
        }
      }

      if (selected) {
        ctx.strokeStyle = '#22c55e'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.beginPath()
        ctx.ellipse(selectionCenterX, selectionCenterY, selectionRadiusX, selectionRadiusY, 0, 0, Math.PI * 2)
        ctx.stroke()
      }

      // Drag-select candidate ring — shown while the selection box is active
      // for any friendly unit whose body is inside the box. Matches the
      // selection box color so it reads as "will be selected on release".
      if (this.state.selectionBox.active && !selected && this.state.isUnitInSelectionBox(unit)) {
        ctx.save()
        ctx.strokeStyle = 'rgba(80, 160, 255, 0.9)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([5 / this.camera.zoom, 4 / this.camera.zoom])
        ctx.beginPath()
        ctx.ellipse(selectionCenterX, selectionCenterY, selectionRadiusX, selectionRadiusY, 0, 0, Math.PI * 2)
        ctx.stroke()
        ctx.restore()
      }

      // Enemy hover ring (orange dashed)
      if (isHoveredEnemy && !isInspected) {
        ctx.save()
        ctx.strokeStyle = 'rgba(251, 146, 60, 0.9)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([5 / this.camera.zoom, 4 / this.camera.zoom])
        ctx.beginPath()
        ctx.ellipse(selectionCenterX, selectionCenterY, selectionRadiusX + 1, selectionRadiusY + 1, 0, 0, Math.PI * 2)
        ctx.stroke()
        ctx.restore()
      }

      // Inspected enemy ring (red solid)
      if (isInspected) {
        ctx.strokeStyle = '#ef4444'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.beginPath()
        ctx.ellipse(selectionCenterX, selectionCenterY, selectionRadiusX, selectionRadiusY, 0, 0, Math.PI * 2)
        ctx.stroke()
      }

      // Visible head-top Y — anchor for all overhead UI (health bar,
      // chevrons, buffs, debuffs, rank-up text). Sprite-aware so big sprites
      // push the UI clear of the head instead of floating over the chest.
      const headTopY = spriteSet
        ? unit.y + bottomOffset - spriteSet.size.height * UNIT_SPRITE_SCALE * (1 - UNIT_SPRITE_TOP_PADDING)
        : unit.y + unitBounds.top

      // Health bar always visible for all units. Color rules:
      //   - Hostile (wave enemies): always red.
      //   - Allied other player: that player's color, regardless of HP.
      //     Bar width still conveys HP percentage.
      //   - Own units: HP-based green→yellow→red (computed inside the helper).
      const isEnemy = this.state.isHostileToLocalPlayer(unit.ownerId)
      const isAlly =
        !isEnemy && !!unit.ownerId && unit.ownerId !== this.state.localPlayerId
      const allyColor = isAlly ? this.state.getPlayerColor(unit.ownerId) ?? null : null
      this.drawSelectedUnitHealthBar(unit, isEnemy, allyColor, headTopY)
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
      if (spriteSet) {
        let actionFacing: { dx: number; dy: number } | null = null
        if (unit.status === 'Attacking') {
          // Prefer the server-authoritative attack facing — it points at the
          // exact target the server is firing at. Only fall back to a local
          // nearest-enemy search when the server didn't supply one (old
          // server, or a tick where the field is absent).
          const sdx = unit.actionFacingDx
          const sdy = unit.actionFacingDy
          if (
            sdx !== undefined &&
            sdy !== undefined &&
            (sdx !== 0 || sdy !== 0)
          ) {
            actionFacing = { dx: sdx, dy: sdy }
          } else {
            actionFacing = findAttackFacing(
              unit,
              unitDef,
              units,
              this.state.mapConfig.buildings,
              this.state.mapConfig.cellSize,
            )
          }
        } else if (unit.workTargetId) {
          actionFacing = findWorkFacing(
            unit,
            unit.workTargetId,
            this.state.mapConfig.buildings,
            this.state.mapConfig.obstacles,
            this.state.mapConfig.cellSize,
          )
        }
        // Sync attack-animation timing to the unit's effective attackSpeed
        // (attacks/sec; includes rank/perk bonuses from the server). One
        // full animation cycle = one attack cooldown. The animation window
        // is capped at 1s: slow attackers (cooldown > 1s) play the swing
        // briskly and idle until the next swing instead of stretching one
        // animation grotesquely across multiple seconds. Faster than 1
        // attack/sec → animation duration shrinks to fit the cooldown.
        let attackTiming: { frameDurationMs: number; animDurationMs: number; cycleMs: number } | undefined
        const attackingAnim = spriteSet.animations.get('attacking')
        const effectiveAttackSpeed = unit.attackSpeed ?? unitDef?.attackSpeed
        if (attackingAnim && effectiveAttackSpeed && effectiveAttackSpeed > 0) {
          const cycleMs = 1000 / effectiveAttackSpeed
          const animDurationMs = Math.min(1000, cycleMs)
          attackTiming = {
            frameDurationMs: animDurationMs / attackingAnim.frameCount,
            animDurationMs,
            cycleMs,
          }
        }
        const anim = this.unitAnim.sample(
          unit.id,
          unit.x,
          unit.y,
          unit.status,
          unit.moving,
          actionFacing,
          attackTiming,
          this.renderTime,
          unit.carriedResourceType,
          unit.unitType,
          unit.ownerId,
          this.state.localPlayerId,
          unit.flyer,
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

      // Placeholder while a sprite is still decoding, or for units without a
      // registered sprite set. Sized from unitBounds so it matches the body
      // footprint roughly.
      ctx.fillStyle = unitColor
      ctx.beginPath()
      ctx.ellipse(
        unit.x,
        unit.y + (unitBounds.top + unitBounds.bottom) / 2,
        halfWidth,
        (unitBounds.bottom - unitBounds.top) / 2,
        0, 0, Math.PI * 2,
      )
      ctx.fill()
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
    // Ranged shots are rendered by drawProjectiles.
    if (attackVisual.kind === 'projectile') {
      return
    }
    this.drawMeleeAttackEffect(unit, attackVisual)
  }

  // Aim 30% down from the visible top of a unit — lands at chest/upper-torso
  // for all current humanoid sprites. Tune here if sprite proportions change.
  private static readonly TARGET_BODY_CENTER_FRACTION = 0.3
  // Fallback body-center lift when the referenced unit is not in client state.
  private static readonly DEFAULT_BODY_CENTER_OFFSET_Y = -16

  /**
   * Draw transient AoE explosion VFX from server snapshots. Marksman gold
   * explosive_tips queues these on every primary hit; the renderer expands
   * an orange-red ring (and a softer glow) over the lifetime, fading out
   * as `progress` approaches 1. Placeholder visual — slated for replacement
   * with a sprite-sheet animation later. Variant-keyed so future perks
   * (frost, holy, etc.) can override colours without changing this loop.
   */
  private drawEffects(effects: EffectSnapshot[], interpolatedUnits: Unit[]) {
    if (effects.length === 0) return

    // Build a per-frame id→unit index so anchor lookups are O(1).
    const unitsById = new Map<number, Unit>()
    for (const u of interpolatedUnits) unitsById.set(u.id, u)

    const ctx = this.ctx
    // Track names that had an unloaded/missing image this frame so we only
    // warn once per missing effect name across all frames, not per-effect.
    for (const effect of effects) {
      const sprite = getEffectSprite(effect.name)
      if (!sprite) continue // warn-once already handled inside getEffectSprite

      const { image, frameWidth, frameHeight, frames } = sprite
      if (!image || !image.complete || image.naturalWidth === 0) continue

      // Prefer the anchor unit's interpolated position so the VFX rides the
      // unit smoothly. Fall back to the server-resolved x/y when the unit is
      // absent from this interpolated frame (e.g. it died mid-effect).
      let wx: number
      let wy: number
      if (effect.anchorUnitId) {
        const anchor = unitsById.get(effect.anchorUnitId)
        wx = anchor?.x ?? effect.x
        wy = anchor?.y ?? effect.y
        // Anchor placement relative to the unit's bounds. Empty/"center"
        // keeps the historical origin placement (so existing perk-queued
        // effects are pixel-unchanged); only "feet"/"head" shift vertically.
        if (anchor && (effect.anchor === 'feet' || effect.anchor === 'head')) {
          const b = getUnitBoundsFor({ path: anchor.path, unitType: anchor.unitType })
          wy = anchor.y + (effect.anchor === 'head' ? b.top : b.bottom)
        }
      } else {
        wx = effect.x
        wy = effect.y
      }

      // Effects play once from frame 0 to frames-1 as progress goes 0→1.
      const frameIndex = Math.min(frames - 1, Math.floor(effect.progress * frames))
      const sx = frameIndex * frameWidth
      const sy = 0

      const scale = effect.sizeScale ?? 1.0
      const dw = frameWidth * scale
      const dh = frameHeight * scale
      // Center the frame on the anchor world position, then nudge by the
      // sprite-authored offset (scaled by sizeScale so the visual relationship
      // holds when a perk over/undersizes the effect).
      const dx = wx - dw / 2 + sprite.offsetX * scale
      const dy = wy - dh / 2 + sprite.offsetY * scale

      const prevSmoothing = ctx.imageSmoothingEnabled
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(image, sx, sy, frameWidth, frameHeight, dx, dy, dw, dh)
      ctx.imageSmoothingEnabled = prevSmoothing
    }
  }

  private drawProjectiles(projectiles: ProjectileSnapshot[]) {
    if (projectiles.length === 0) return

    // Build a per-frame id→unit index so origin/target lift lookups are O(1)
    // instead of O(N) linear scans per projectile.
    const unitsById = new Map<number, Unit>()
    for (const u of this.state.units) unitsById.set(u.id, u)

    // Memoize lift results by (unitType, path) within the frame so repeated
    // shots from the same unit type don't redo sprite/bounds resolution.
    const originLiftCache = new Map<string, { x: number; y: number }>()
    const targetLiftCache = new Map<string, { x: number; y: number }>()

    const ctx = this.ctx
    for (const proj of projectiles) {
      const originLift = this.getProjectileOriginLift(unitsById.get(proj.ownerUnitId), originLiftCache)
      const targetLift = this.getProjectileTargetLift(
        proj.targetUnitId ? unitsById.get(proj.targetUnitId) : undefined,
        targetLiftCache,
      )

      const originX = proj.originX + originLift.x
      const originY = proj.originY + originLift.y
      // Pierce shots fly to a fixed-line endpoint, not a homing target — skip
      // the target-unit lift so the streak ends exactly where the server
      // chose, even if the original target moved.
      const targetX = proj.pierce ? proj.targetX : proj.targetX + targetLift.x
      const targetY = proj.pierce ? proj.targetY : proj.targetY + targetLift.y

      const t = Math.max(0, Math.min(1, proj.progress))

      // Marksman silver pierce — render as a quick green wind streak from
      // origin out to the current head position rather than an arrow sprite.
      // Bypasses drawProjectileForVariant entirely.
      if (proj.pierce) {
        this.drawPierceWindStreak(originX, originY, targetX, targetY, t)
        continue
      }

      const x = originX + (targetX - originX) * t
      const y = originY + (targetY - originY) * t

      const dx = targetX - originX
      const dy = targetY - originY
      const angle = dx === 0 && dy === 0 ? 0 : Math.atan2(dy, dx)

      ctx.save()
      ctx.translate(x, y)
      ctx.rotate(angle)
      drawProjectileForVariant(ctx, {
        zoom: this.camera.zoom,
        projectile: proj,
      })
      ctx.restore()
    }
  }

  /**
   * Render a Marksman pierce shot as a fast green wind streak. Designed for
   * high contrast against grass / green terrain (which would otherwise eat
   * a pure-green effect): the core is bright white, surrounded by a vivid
   * green-cyan glow, with a faint dim trail along the entire flight path so
   * the trajectory reads even between snapshots.
   *
   * Layered passes (drawn back-to-front):
   *   1. Faint full-path trail — origin → endpoint, very dim, shows the
   *      whole shot line so the player sees the trajectory at a glance.
   *   2. Outer green glow — wide, vivid, follows the active tail→head
   *      segment so the streak reads as a moving blade of wind.
   *   3. Bright white core — narrow, high-alpha, on top of the glow.
   *   4. Bright leading head — radial gradient at the front of the streak.
   *   5. Origin puff — small green burst at the bow when the shot fires
   *      (early-progress only) for the "the marksman just released" tell.
   */
  private drawPierceWindStreak(originX: number, originY: number, endX: number, endY: number, progress: number) {
    const ctx = this.ctx
    // The tail trails 50% of the path behind the head — the visible streak
    // is [headT - TAIL_FRACTION, headT] clamped to [0,1].
    const TAIL_FRACTION = 0.50

    const headT = progress
    const tailT = Math.max(0, headT - TAIL_FRACTION)

    const dx = endX - originX
    const dy = endY - originY

    const headX = originX + dx * headT
    const headY = originY + dy * headT
    const tailX = originX + dx * tailT
    const tailY = originY + dy * tailT

    // No alpha fade across the streak's lifetime — the projectile is removed
    // by the server the same tick it lands, and a fading tail before that
    // makes the actual reach hard to judge. Holding full alpha until the
    // server drops the projectile keeps the maximum distance unambiguous.

    // Width budget: total visible streak ≈ 6px wide. The glow sets the
    // outer envelope at 6px, the white core sits at 2px inside it, and
    // the head dot caps a similar diameter so the whole effect reads as a
    // thin blade rather than a bar.
    const glowWidth = Math.max(4, 6 / this.camera.zoom)
    const coreWidth = Math.max(1.25, 2 / this.camera.zoom)
    const headRadius = Math.max(2.5, 4 / this.camera.zoom)
    const trailWidth = Math.max(0.75, 1.25 / this.camera.zoom)

    // Pass 1 — faint full-path trail (origin → endpoint). Very low alpha;
    // exists so the player sees the trajectory line during the whole flight,
    // even when the moving streak segment isn't covering a given pixel.
    ctx.save()
    ctx.lineCap = 'round'
    ctx.strokeStyle = 'rgba(150, 255, 200, 0.18)'
    ctx.lineWidth = trailWidth
    ctx.beginPath()
    ctx.moveTo(originX, originY)
    ctx.lineTo(endX, endY)
    ctx.stroke()
    ctx.restore()

    // Pass 2 — vivid green-cyan glow on the active streak segment. Sets
    // the outer ~6px envelope of the blade.
    ctx.save()
    ctx.lineCap = 'round'
    ctx.strokeStyle = 'rgba(80, 255, 180, 0.6)'
    ctx.lineWidth = glowWidth
    ctx.beginPath()
    ctx.moveTo(tailX, tailY)
    ctx.lineTo(headX, headY)
    ctx.stroke()
    ctx.restore()

    // Pass 3 — bright white core, ~2px wide. White rather than green so the
    // streak pops against grass / green terrain.
    ctx.save()
    ctx.lineCap = 'round'
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.95)'
    ctx.lineWidth = coreWidth
    ctx.beginPath()
    ctx.moveTo(tailX, tailY)
    ctx.lineTo(headX, headY)
    ctx.stroke()
    ctx.restore()

    // Pass 4 — leading head: small radial gradient that caps the front of
    // the blade. Same diameter band as the glow so the whole effect reads
    // as one consistent ~6px-wide thread.
    const headGradient = ctx.createRadialGradient(headX, headY, 0, headX, headY, headRadius)
    headGradient.addColorStop(0, 'rgba(255, 255, 255, 1)')
    headGradient.addColorStop(0.5, 'rgba(160, 255, 200, 1)')
    headGradient.addColorStop(1, 'rgba(0, 120, 60, 0)')
    ctx.save()
    ctx.fillStyle = headGradient
    ctx.beginPath()
    ctx.arc(headX, headY, headRadius, 0, Math.PI * 2)
    ctx.fill()
    ctx.restore()

    // Pass 5 — small origin puff at the bow when the shot first fires.
    // Sized to the same band so the firing tell doesn't dwarf the streak.
    if (progress < 0.5) {
      const puffAlpha = (1 - progress / 0.5) * 0.85
      const puffRadius = Math.max(3, 5 / this.camera.zoom)
      const puffGradient = ctx.createRadialGradient(originX, originY, 0, originX, originY, puffRadius)
      puffGradient.addColorStop(0, `rgba(220, 255, 230, ${puffAlpha})`)
      puffGradient.addColorStop(0.6, `rgba(80, 230, 160, ${0.55 * puffAlpha})`)
      puffGradient.addColorStop(1, 'rgba(0, 120, 60, 0)')
      ctx.save()
      ctx.fillStyle = puffGradient
      ctx.beginPath()
      ctx.arc(originX, originY, puffRadius, 0, Math.PI * 2)
      ctx.fill()
      ctx.restore()
    }
  }

  // spriteBodyCenterLift back-computes the visible body from the sprite draw
  // math (see sprite blit at drawImage call above): the sprite's bottom edge
  // sits at unit.y + bottomOffset, with transparent pads trimming both ends.
  // Returns null for procedural-only units so callers can fall back.
  private spriteBodyCenterLift(unitType: string, path: string | undefined): { x: number; y: number } | null {
    const spriteSet = getUnitSpriteSet(path, unitType)
    if (!spriteSet) return null
    const bounds = getUnitBoundsFor({ path, unitType })
    const bottomOffset = bounds.bottom
    const h = spriteSet.size.height * UNIT_SPRITE_SCALE
    const visibleBottom = bottomOffset - h * UNIT_SPRITE_BOTTOM_PADDING
    const visibleTop = bottomOffset - h * (1 - UNIT_SPRITE_TOP_PADDING)
    return {
      x: 0,
      y: visibleTop + (visibleBottom - visibleTop) * CanvasRenderer.TARGET_BODY_CENTER_FRACTION,
    }
  }

  // Origin lift: bow position for sprite-rendered units (same chest anchor as
  // targets), attackVisual offsets for procedural units.
  private getProjectileOriginLift(
    unit: Unit | undefined,
    cache: Map<string, { x: number; y: number }>,
  ): { x: number; y: number } {
    if (!unit?.unitType) return { x: 0, y: CanvasRenderer.DEFAULT_BODY_CENTER_OFFSET_Y }
    const key = `${unit.unitType}|${unit.path ?? ''}`
    const cached = cache.get(key)
    if (cached) return cached

    const sprite = this.spriteBodyCenterLift(unit.unitType, unit.path)
    const lift = sprite ?? (() => {
      const attackVisual = getResolvedUnitAttackVisual(UNIT_DEF_MAP.get(unit.unitType))
      return { x: attackVisual.originX, y: attackVisual.originY }
    })()
    cache.set(key, lift)
    return lift
  }

  // Target lift: chest for sprite-rendered units, 30%-from-top of procedural
  // bounds for everyone else.
  private getProjectileTargetLift(
    unit: Unit | undefined,
    cache: Map<string, { x: number; y: number }>,
  ): { x: number; y: number } {
    if (!unit?.unitType) return { x: 0, y: CanvasRenderer.DEFAULT_BODY_CENTER_OFFSET_Y }
    const key = `${unit.unitType}|${unit.path ?? ''}`
    const cached = cache.get(key)
    if (cached) return cached

    const sprite = this.spriteBodyCenterLift(unit.unitType, unit.path)
    let lift: { x: number; y: number }
    if (sprite) {
      lift = sprite
    } else {
      const bounds = getUnitBoundsFor({ path: unit.path, unitType: unit.unitType })
      lift = {
        x: 0,
        y: bounds.top + (bounds.bottom - bounds.top) * CanvasRenderer.TARGET_BODY_CENTER_FRACTION,
      }
    }
    cache.set(key, lift)
    return lift
  }

  private drawFloatingDamageNumbers(renderTime: number) {
    if (this.floatingDamageNumbers.length === 0) return
    const ctx = this.ctx
    ctx.save()
    ctx.textAlign = 'center'
    ctx.textBaseline = 'alphabetic'
    ctx.lineWidth = 3 / this.camera.zoom
    ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'

    const baseFontPx = Math.max(12, 14 / this.camera.zoom)
    const combinedFontPx = Math.max(16, 19 / this.camera.zoom)
    const critFontPx = Math.max(14, 17 / this.camera.zoom)
    // Minor (Reactive Flames splash, etc.) — smaller than base, drawn in
    // orange so it reads as ancillary damage without dominating the popup.
    const minorFontPx = Math.max(9, 10 / this.camera.zoom)

    const kept: typeof this.floatingDamageNumbers = []
    for (const num of this.floatingDamageNumbers) {
      const elapsed = renderTime - num.startedAt
      if (elapsed >= this.FLOATING_DAMAGE_DURATION_MS) continue
      const t = elapsed / this.FLOATING_DAMAGE_DURATION_MS

      // Minor popups: horizontal drift (linear) + accelerating downward
      // fall (t²) — reads like a spark scattering away from the impact.
      // All other kinds drift straight up (the standard floating-number
      // animation).
      let drawX = num.x
      let drawY: number
      if (num.kind === 'minor') {
        drawX = num.x + this.FLOATING_DAMAGE_MINOR_X_PX * t * (num.xDriftSign ?? 1)
        drawY = num.y + this.FLOATING_DAMAGE_MINOR_FALL_PX * t * t
      } else {
        drawY = num.y - this.FLOATING_DAMAGE_RISE_PX * t
      }
      ctx.globalAlpha = Math.max(0, 1 - t)
      const text = String(num.amount)

      if (num.kind === 'combined') {
        // Combined Double Shot total — drawn larger and yellow/gold.
        ctx.font = `bold ${combinedFontPx}px sans-serif`
        ctx.strokeText(text, drawX, drawY)
        ctx.fillStyle = '#fde047' // tailwind yellow-300
        ctx.fillText(text, drawX, drawY)
      } else if (num.kind === 'crit') {
        // Critical hit — red circle behind a slightly larger number. The
        // circle radius scales with text width so multi-digit numbers don't
        // poke past it; capped to keep small hits from drawing tiny dots.
        ctx.font = `bold ${critFontPx}px sans-serif`
        const measure = ctx.measureText(text)
        const circleR = Math.max(critFontPx * 0.85, measure.width * 0.65 + 4 / this.camera.zoom)
        // Center the circle vertically on the cap-height midline of the
        // text rather than the baseline, so the number sits inside the disc.
        const circleY = drawY - critFontPx * 0.35

        // Outer dimmer red ring for a subtle glow against bright terrain.
        ctx.save()
        ctx.fillStyle = `rgba(120, 10, 10, ${ctx.globalAlpha * 0.55})`
        ctx.beginPath()
        ctx.arc(num.x, circleY, circleR * 1.18, 0, Math.PI * 2)
        ctx.fill()
        ctx.restore()

        // Filled red disc.
        ctx.save()
        ctx.fillStyle = `rgba(220, 38, 38, ${ctx.globalAlpha * 0.92})`
        ctx.beginPath()
        ctx.arc(num.x, circleY, circleR, 0, Math.PI * 2)
        ctx.fill()
        // Crisp dark-red outline for definition against light terrain.
        ctx.lineWidth = Math.max(1, 1.5 / this.camera.zoom)
        ctx.strokeStyle = `rgba(80, 8, 8, ${ctx.globalAlpha * 0.95})`
        ctx.stroke()
        ctx.restore()

        // Number on top — white text with a thin dark stroke for legibility.
        ctx.strokeText(text, drawX, drawY)
        ctx.fillStyle = '#ffffff'
        ctx.fillText(text, drawX, drawY)
      } else if (num.kind === 'minor') {
        // Smaller popup so the splash reads as a side-effect, not a hit
        // from the trap itself. Color derives from variant: "electric" =
        // purple (Electrified Caltrops), default = orange (Reactive
        // Flames / generic fire splash).
        ctx.font = `bold ${minorFontPx}px sans-serif`
        ctx.strokeText(text, drawX, drawY)
        ctx.fillStyle = num.minorVariant === 'electric'
          ? '#c084fc' // tailwind purple-400
          : '#fb923c' // tailwind orange-400
        ctx.fillText(text, drawX, drawY)
      } else {
        ctx.font = `bold ${baseFontPx}px sans-serif`
        ctx.strokeText(text, drawX, drawY)
        ctx.fillStyle = num.isFriendly ? '#ef4444' : '#ffffff'
        ctx.fillText(text, drawX, drawY)
      }
      kept.push(num)
    }
    ctx.restore()
    this.floatingDamageNumbers = kept
  }

  // Floating resource deposit indicators — icon + "+amount" floating up over
  // the deposit site, mirroring the damage number lifecycle. Color is keyed
  // off capacityFraction: 1.0 = green (full credit), <1 = yellow/red (gain
  // reduced by capacity caps or perks).
  private drawFloatingResourceNumbers(renderTime: number) {
    if (this.floatingResourceNumbers.length === 0) return
    const ctx = this.ctx
    ctx.save()
    ctx.textAlign = 'left'
    ctx.textBaseline = 'middle'
    ctx.lineWidth = 3 / this.camera.zoom
    ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'
    ctx.font = `bold ${Math.max(12, 14 / this.camera.zoom)}px sans-serif`

    const iconPx = Math.max(12, 16 / this.camera.zoom)
    const gap = Math.max(2, 3 / this.camera.zoom)

    const kept: typeof this.floatingResourceNumbers = []
    for (const num of this.floatingResourceNumbers) {
      const elapsed = renderTime - num.startedAt
      if (elapsed >= this.FLOATING_RESOURCE_DURATION_MS) continue
      const t = elapsed / this.FLOATING_RESOURCE_DURATION_MS
      const drawY = num.y - this.FLOATING_RESOURCE_RISE_PX * t
      ctx.globalAlpha = Math.max(0, 1 - t)

      const text = `+${num.amount}`
      const textWidth = ctx.measureText(text).width
      const totalWidth = iconPx + gap + textWidth
      const groupX = num.x - totalWidth / 2

      // Color by capacity. Thresholds: ≥1 = full green, ≥0.66 = yellow,
      // <0.66 = red. capacityFraction is currently always 1, so this just
      // wires up the future yellow/red states.
      const fillColor =
        num.capacityFraction >= 1
          ? '#15803d'
          : num.capacityFraction >= 0.66
            ? '#ca8a04'
            : '#b91c1c'

      const icon = getResourceIconImage(num.resourceId)
      if (icon && icon.complete && icon.naturalWidth > 0) {
        const prevSmoothing = ctx.imageSmoothingEnabled
        ctx.imageSmoothingEnabled = false
        ctx.drawImage(icon, groupX, drawY - iconPx / 2, iconPx, iconPx)
        ctx.imageSmoothingEnabled = prevSmoothing
      }

      const textX = groupX + iconPx + gap
      ctx.strokeText(text, textX, drawY)
      ctx.fillStyle = fillColor
      ctx.fillText(text, textX, drawY)
      kept.push(num)
    }
    ctx.restore()
    this.floatingResourceNumbers = kept
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
    const isEnemy = this.state.isHostileToLocalPlayer(unit.ownerId)
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
      if (!target.visible || !this.state.ownersAreHostile(target.ownerId, building.ownerId)) continue

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
      if (!this.state.ownersAreHostile(target.ownerId, unit.ownerId)) continue

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
      if (!this.state.ownersAreHostile(building.ownerId, unit.ownerId)) continue

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
    allyColor: string | null,
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
    } else if (allyColor) {
      // Allied other player: bar is colored by their player color regardless
      // of HP. The bar width still conveys the HP percentage.
      fillColor = allyColor
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
    const spriteRenderDef = buildingDef?.spriteRender
    const sprite = getBuildingSprite(placement.buildingType)
    const footW = gridW * cellSize
    const footH = gridH * cellSize
    // Sprite box may extend beyond the footprint (townhall has a 3x2
    // footprint + 3x3 sprite that pokes one cell up). Preview the full
    // visual so players see what the finished building will look like.
    const spriteX = worldX + (spriteRenderDef?.offsetX ?? 0) * cellSize
    const spriteY = worldY + (spriteRenderDef?.offsetY ?? 0) * cellSize
    const spriteW = (spriteRenderDef?.width ?? gridW) * cellSize
    const spriteH = (spriteRenderDef?.height ?? gridH) * cellSize

    ctx.save()
    ctx.globalAlpha = 0.6

    const playerFill = valid ? (buildingDef?.color ?? '#1e40af') : '#dc2626'

    if (sprite) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(sprite, spriteX, spriteY, spriteW, spriteH)
      if (!valid) {
        ctx.globalAlpha = 0.35
        ctx.fillStyle = '#dc2626'
        ctx.fillRect(worldX, worldY, footW, footH)
      }
    } else if (!renderDef) {
      // No render def — solid fill fallback
      ctx.fillStyle = playerFill
      ctx.fillRect(spriteX, spriteY, spriteW, spriteH)
    } else {
      // Draw every layer explicitly, substituting 'player' with the valid/invalid tint color.
      for (const layer of renderDef.layers) {
        ctx.fillStyle = layer.color === 'player' ? playerFill : layer.color
        if (!('kind' in layer) || layer.kind === 'rect') {
          ctx.fillRect(
            spriteX + layer.x * cellSize,
            spriteY + layer.y * cellSize,
            layer.w * cellSize,
            layer.h * cellSize,
          )
        } else if (layer.kind === 'tri') {
          const s = cellSize / 6
          const tlX = spriteX + layer.cx * cellSize + layer.sc * s
          const tlY = spriteY + layer.cy * cellSize + layer.sr * s
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
      this.state.minimapPanelRect,
    )
    const { x, y, width: minimapWidth, height: minimapHeight } = bounds

    ctx.save()

    // Fill the full panel interior black first so the letterbox area (the
    // space inside the panel that the aspect-fit minimap doesn't cover) is
    // opaque. Without this, the canvas world view shows through the gap
    // between the minimap and the panel frame on the top/bottom (or sides).
    const panelRect = this.state.minimapPanelRect
    ctx.fillStyle = '#000'
    if (panelRect) {
      ctx.fillRect(panelRect.x, panelRect.y, panelRect.width, panelRect.height)
    } else {
      ctx.fillRect(x, y, minimapWidth, minimapHeight)
    }

    // Blit the offscreen terrain cache into the minimap area. The cache
    // already contains the rendered sprite tiles, custom-per-tile sprites,
    // and terrain overrides exactly as they appear in-world, so blitting it
    // gives the most faithful color match possible. Smoothing is left on so
    // the heavy downscale averages neighboring pixels into a clean color
    // rather than aliasing into individual sprite pixels.
    if (this.terrainCache) {
      const prevSmoothing = ctx.imageSmoothingEnabled
      ctx.imageSmoothingEnabled = true
      ctx.drawImage(this.terrainCache, x, y, minimapWidth, minimapHeight)
      ctx.imageSmoothingEnabled = prevSmoothing
    } else {
      // Fallback for the brief window before the terrain cache is built —
      // paint the explicit terrain overrides at their per-tile color.
      for (const tile of this.state.mapConfig.terrain) {
        ctx.fillStyle = getTerrainColor(tile.terrain)
        ctx.fillRect(
          x + (tile.x / this.state.mapConfig.gridCols) * minimapWidth,
          y + (tile.y / this.state.mapConfig.gridRows) * minimapHeight,
          minimapWidth / this.state.mapConfig.gridCols,
          minimapHeight / this.state.mapConfig.gridRows,
        )
      }
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
      // Enemy spawn points are logical spawners and must not appear on the
      // minimap. Mirrors the world-render skip in the main building loop.
      if (building.buildingType === 'enemy-spawnpoint') continue
      if (building.ghost) continue

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

    const fow = this.state.fow
    if (fow.cols > 0) {
      const { cellSize } = this.state.mapConfig
      const scaleX = minimapWidth / (fow.cols * cellSize)
      const scaleY = minimapHeight / (fow.rows * cellSize)
      for (let gy = 0; gy < fow.rows; gy++) {
        for (let gx = 0; gx < fow.cols; gx++) {
          const state = fow.cellAt(gx, gy)
          if (state === 3) continue
          ctx.fillStyle = state === 0
            ? 'rgba(0,0,0,1.0)'
            : 'rgba(0,0,0,0.6)'
          ctx.fillRect(
            x + gx * cellSize * scaleX,
            y + gy * cellSize * scaleY,
            Math.ceil(cellSize * scaleX),
            Math.ceil(cellSize * scaleY),
          )
        }
      }
    }

    const worldWidth = this.canvas.width / this.camera.zoom
    const worldHeight = this.canvas.height / this.camera.zoom
    const viewX = x + (this.camera.x / this.state.mapWidth) * minimapWidth
    const viewY = y + (this.camera.y / this.state.mapHeight) * minimapHeight
    const viewWidth = (worldWidth / this.state.mapWidth) * minimapWidth
    const viewHeight = (worldHeight / this.state.mapHeight) * minimapHeight

    // Clip the viewport rect to the minimap bounds so it can't extend past
    // the panel frame when the camera is panned to an edge or the world is
    // smaller than the on-screen viewport.
    ctx.save()
    ctx.beginPath()
    ctx.rect(x, y, minimapWidth, minimapHeight)
    ctx.clip()
    ctx.strokeStyle = 'rgba(125, 211, 252, 0.95)'
    ctx.lineWidth = 1.5
    ctx.strokeRect(viewX, viewY, viewWidth, viewHeight)
    ctx.restore()

    ctx.restore()
  }
}

// Hostility predicate for free-function helpers below that don't have access to
// GameState. Mirrors GameState.ownersAreHostile and the server's
// playersAreHostile: real players are allied with each other; only the
// wave-enemy faction is hostile to real players.
function ownersAreHostile(a: string | null | undefined, b: string | null | undefined): boolean {
  if (!a || !b || a === b) return false
  return a === ENEMY_PLAYER_ID || b === ENEMY_PLAYER_ID
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
    if (!ownersAreHostile(other.ownerId, attacker.ownerId)) continue
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
    if (!ownersAreHostile(building.ownerId, attacker.ownerId)) continue
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

// Orients a stationary worker toward the exact building or obstacle it is
// gathering from, constructing, or repairing. The server publishes the target
// id on the unit snapshot (WorkTargetID). Trees are obstacles, other work
// targets are buildings, so both collections are searched.
function findWorkFacing(
  worker: { x: number; y: number },
  workTargetId: string,
  buildings: BuildingTile[],
  obstacles: ObstacleTile[],
  cellSize: number,
): { dx: number; dy: number } | null {
  for (const building of buildings) {
    if (building.id !== workTargetId) continue
    const cx = building.x * cellSize + (building.width * cellSize) / 2
    const cy = building.y * cellSize + (building.height * cellSize) / 2
    return { dx: cx - worker.x, dy: cy - worker.y }
  }
  for (const obstacle of obstacles) {
    if (obstacle.id !== workTargetId) continue
    const w = obstacle.width ?? 1
    const h = obstacle.height ?? 1
    const cx = obstacle.x * cellSize + (w * cellSize) / 2
    const cy = obstacle.y * cellSize + (h * cellSize) / 2
    return { dx: cx - worker.x, dy: cy - worker.y }
  }
  return null
}
