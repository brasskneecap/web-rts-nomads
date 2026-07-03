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
import { drawMinimapBase, drawMinimapPOIs } from './minimapLayers'
import { BUILDING_DEF_MAP, getResolvedBuildingAttackVisual, resolveBuildingShadow } from '../maps/buildingDefs'
import { getBuildingFallbackRender } from '../maps/buildingFallbackRender'
import {
  CONSTRUCTION_FRAME_COUNT,
  DAMAGED_TIER_COUNT,
  TRAINING_FRAME_COUNT,
  getBuildingSprite,
  getConstructionFrameIndex,
  getConstructionSprite,
  getDamagedFrameIndex,
  getDamagedFramesPerTier,
  getDamagedSprite,
  getDamagedTier,
  getRecipeShopStyleSprite,
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
import { resolveUnitShadow, SHADOW_LIGHT_DX, SHADOW_LIGHT_DY, SHADOW_LIGHT_SHIFT } from '../maps/unitShadow'
import type { BannerSnapshot, BeamSnapshot, BuildingTile, EffectSnapshot, ObstacleTile, ProjectileSnapshot, TrapSnapshot, Zone, ZoneSnapshot } from '../network/protocol'
import { ENEMY_PLAYER_ID, ZONE_TEAM_OWNER } from '../network/protocol'
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
import { getBeamSprite } from './beamSprites'
import { getResourceIconImage } from './resourceSprites'
import { getActionIconImage } from './actionIconSprites'
import { getItemAssetImage } from './itemAssets'
import { ITEM_DEF_MAP } from '../maps/itemDefs'
import { UnitAnimationController } from './unitAnimation'
import { cellSetBoundaryEdges, cellKey, zoneBoundaryEdges } from '../maps/zoneGeometry'

// Cache of item-icon HTMLImageElements keyed by itemId. Built lazily on first
// request. Resolves the item def's iconKey through the same fallback chain
// ActionIcon.vue uses: action-icon sprites first (covers icons authored
// alongside abilities), then assets/items/**/<iconKey>.png (where the
// actual item PNGs live — broad_sword, scimitar, potions, etc.).
const itemIconCache = new Map<string, HTMLImageElement | null>()

/** Returns a preloaded HTMLImageElement for a catalog item id, or null when
 *  no PNG is available. Caches the result so repeat calls within a frame are
 *  free. Cached null is intentionally NOT stored — items load asynchronously
 *  from the asset glob, and a too-early miss would poison the cache. */
function resolveItemIconImage(itemId: string): HTMLImageElement | null {
  const cached = itemIconCache.get(itemId)
  if (cached) return cached
  const def = ITEM_DEF_MAP.get(itemId)
  if (!def) return null
  const img = getActionIconImage(def.iconKey) ?? getItemAssetImage(def.iconKey)
  if (img) itemIconCache.set(itemId, img)
  return img
}

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

// Path2D objects require SVG string parsing on construction. Since all icon
// paths come from ACTION_ICON_MAP (loaded once at startup, never mutated),
// we cache them at module level so each unique path is only parsed once.
const path2DCache = new Map<string, Path2D>()
function getCachedPath2D(svgPath: string): Path2D {
  let p = path2DCache.get(svgPath)
  if (!p) {
    p = new Path2D(svgPath)
    path2DCache.set(svgPath, p)
  }
  return p
}

// Shared damage-variant → color palette used by BOTH the minor (side-falling)
// popup and the major (floating-up) popup. Mirrors the server's
// damageTypeColorVariant in damage_type_hints.go so a damage type the server
// is willing to tag is always paired with a color here.
//
// Returns `fallback` when variant is undefined or unrecognised so callers
// can supply different defaults — minor popups default to fire/orange (the
// historical Reactive Flames look), major popups default to enemy-white /
// friendly-red. Both branches stay in sync by using this one map.
//
// EXTENSION POINT: add a case here when adding a new damage type color on
// the server; client behaviour then comes online automatically.
function colorForDamageVariant(variant: string | undefined, fallback: string): string {
  switch (variant) {
    case 'electric':
      return '#c084fc' // tailwind purple-400 — light/lavender
    case 'holy':
      return '#fcd34d' // tailwind amber-300 — gold
    case 'shadow':
      return '#7e22ce' // tailwind purple-700 — dark, necrotic
    case 'fire':
      return '#fb923c' // tailwind orange-400
    default:
      return fallback
  }
}

export class CanvasRenderer {
  private ctx: CanvasRenderingContext2D
  private canvas: HTMLCanvasElement
  private state: GameState
  private camera: Camera
  private resizeObserver: ResizeObserver | null = null
  private renderTime = 0
  // Cached unit-circle radial gradient (opaque black center → transparent
  // edge) reused for every ground shadow (units, buildings, loot drops). Built
  // once against the stable `this.ctx`; per-entity size/opacity come from a
  // transform + global alpha at draw time (see drawGroundShadow), so one
  // gradient serves everything.
  private groundShadowGradient: CanvasGradient | null = null
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
  // Cached last non-zero attack facing per unit id. Prevents findAttackFacing
  // (O(N) scan) from running every frame when the server sends 0,0 (not
  // mid-swing). Updated each frame the server sends a non-zero facing;
  // re-used on frames where the server sends 0,0.
  private unitAttackFacingCache = new Map<number, { dx: number; dy: number }>()
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
     * dominating the main damage popup. 'heal' renders a light-green "+N"
     * floating up (intentional healing, e.g. the heal ability).
     */
    kind: 'normal' | 'combined' | 'crit' | 'minor' | 'heal' | 'manaRestore'
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
    /**
     * Sub-flavour for 'normal' / 'crit' popups: shares the same palette
     * as minorVariant ("shadow" → dark purple, "fire" → orange, "holy" →
     * gold, "electric" → light purple). When absent the popup falls back
     * to the default friendly-red / enemy-white. Mirrors the server
     * DamageTypeHintSnapshot.variant.
     */
    damageType?: string
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

  // Floating loot-pickup indicators — icon + label floating up above the
  // collecting unit when a chest is picked up. Spawned by NetworkClient via
  // spawnLootPickupFloater(). Visually mirrors floatingResourceNumbers but
  // uses a longer duration since pickup is a discrete notable event.
  private floatingLootPickups: Array<{
    x: number
    y: number
    kind: 'resource' | 'item'
    resourceId?: string
    itemId?: string
    label: string
    color: string
    startedAt: number
  }> = []
  private readonly FLOATING_LOOT_DURATION_MS = 1300
  private readonly FLOATING_LOOT_RISE_PX = 40

  // Loot-chest "open-then-disappear" animation state. When a drop leaves
  // state.lootDropsById (collected by a unit, or any other removal path) we
  // snapshot its last-known position here and play the chest's opening sheet
  // through one cycle at that spot, then drop it. Driven entirely from the
  // render loop's diff against state — no notification plumbing needed.
  private lastLootDropPositions: Map<string, { x: number; y: number }> = new Map()
  private openingLootDrops: Map<string, { x: number; y: number; startedAt: number }> =
    new Map()
  private readonly LOOT_OPENING_FRAME_MS = 110
  private readonly LOOT_OPENING_DURATION_MS = this.LOOT_OPENING_FRAME_MS * 4

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
          damageType: evt.damageType,
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
    this.drawZoneOverlay()
    this.drawBuildingSpawnMarkers()
    this.drawTraps(this.state.traps)
    this.drawBanners(this.state.banners)
    // Loot-drop chests render after banners but before units so the collecting
    // unit visually passes over the chest (unit z-order wins).
    this.drawLootDrops()
    this.drawUnits(units)
    // Effects sit on top of the caster body but underneath projectiles so
    // arrows and bolts always read clearly over the VFX layer.
    this.drawEffects(this.state.effects, units)
    // Drawn after units so arrows render on top of the firing unit's body.
    this.drawProjectiles(this.state.projectiles)
    // Channeled beams render at the same z-layer as projectiles so they sit
    // above unit sprites but below the FOW and UI overlays.
    this.drawBeams(this.state.beams)

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
    this.drawCommanderTargetingAoE()
    this.drawSelectionBox()
    this.drawFloatingDamageNumbers(renderTime)
    this.drawFloatingResourceNumbers(renderTime)
    this.drawFloatingLootPickups()

    ctx.restore()

    // Screen-space HUD (identity transform restored above).
    this.drawCaptureProgressBar()
    this.drawMinimap(units)
  }

  // Top-of-screen capture progress bar. Appears while any zone has an in-flight
  // capture/defend timer (0 < progress < 1), showing the most-progressed one as
  // a single filling bar. Screen space — call after the world transform is
  // restored. Replaces the old per-cell zone "fill up" visual.
  private drawCaptureProgressBar() {
    const zones = this.state.mapConfig.zones
    if (!zones || zones.length === 0) return

    let bestZone: Zone | null = null
    let bestProgress = 0
    let bestContested = false
    let bestSnap: ZoneSnapshot | null = null
    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      if (!snap || snap.progress === undefined) continue
      const p = snap.progress
      if (p > 0 && p < 1 && p > bestProgress) {
        bestProgress = p
        bestZone = zone
        bestContested = snap.contested === true
        bestSnap = snap
      }
    }
    if (!bestZone) return

    const ctx = this.ctx
    const W = 360
    const H = 24
    const x = Math.round((this.canvas.width - W) / 2)
    const y = 16
    const verb = bestZone.capture?.type === 'claim' ? 'Defending' : 'Capturing'
    let suffix = ''
    if (bestZone.capture?.type === 'claim') {
      const pts = bestSnap?.claimPoints
      if (pts && pts.length > 1) {
        const held = pts.filter((pt) => pt.captured).length
        suffix = ` (${held}/${pts.length} points)`
      }
    }
    const label = `${bestZone.name || bestZone.id} — ${verb}${suffix}`

    ctx.save()
    ctx.setLineDash([])
    // Track.
    ctx.fillStyle = 'rgba(15,23,42,0.85)'
    ctx.fillRect(x, y, W, H)
    // Fill (amber when contested, blue otherwise).
    ctx.fillStyle = bestContested ? 'rgba(251,191,36,0.95)' : 'rgba(96,165,250,0.95)'
    ctx.fillRect(x + 2, y + 2, Math.max(0, (W - 4) * bestProgress), H - 4)
    // Border.
    ctx.strokeStyle = 'rgba(148,163,184,0.65)'
    ctx.lineWidth = 1
    ctx.strokeRect(x + 0.5, y + 0.5, W - 1, H - 1)
    // Label.
    ctx.fillStyle = '#f1f5f9'
    ctx.font = 'bold 12px system-ui, -apple-system, Segoe UI, sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText(label, x + W / 2, y + H / 2 + 0.5)
    ctx.restore()
  }

  /** Spawn a floating icon + label above a world position when a unit picks
   *  up a loot chest. Called by NetworkClient.handleLootCollected. One call
   *  per resource line or item line — stacking is the caller's responsibility. */
  spawnLootPickupFloater(opts: {
    x: number
    y: number
    kind: 'resource' | 'item'
    resourceId?: string
    itemId?: string
    label: string
    color: string
  }): void {
    this.floatingLootPickups.push({
      ...opts,
      startedAt: performance.now(),
    })
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
      const renderDef = getBuildingFallbackRender(building.buildingType)
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

      // Recipe shops render a per-instance "shopStyle" art override when one is
      // set (and loaded); otherwise the shared building-type sprite is used.
      const styleSprite =
        building.buildingType === 'recipe-shop'
          ? getRecipeShopStyleSprite(building.metadata?.['shopStyle'] as string | undefined)
          : null
      const sprite = styleSprite ?? getBuildingSprite(building.buildingType)
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
      // metadata while the head of the production queue is in flight. A
      // blacksmith researching an upgrade reuses the same training animation —
      // the server stamps upgradeInProgress while research is in flight.
      const isTraining =
        !isUnderConstruction &&
        !isPendingStart &&
        ((typeof building.metadata?.['producingUnitType'] === 'string' &&
          (building.metadata?.['producingUnitType'] as string).length > 0) ||
          building.metadata?.['upgradeInProgress'] === true)
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

      // Ground shadow — lowest layer under the building, so the selection ring
      // and sprite draw on top. Skipped for the placement preview
      // (pendingStart), which renders as a translucent ghost rather than a real
      // structure on the ground. Size/center default from the footprint; an
      // optional `shadow` block in the building catalog tunes or disables it.
      if (!isPendingStart) {
        const shadow = resolveBuildingShadow(
          buildingDef?.shadow,
          building.width,
          building.height,
          cellSize,
        )
        if (shadow) {
          this.drawGroundShadow(
            worldX + shadow.centerX,
            worldY + shadow.centerY,
            shadow.radiusX,
            shadow.radiusY,
            shadow.opacity,
          )
        }
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
        const framesPerTier = getDamagedFramesPerTier(building.buildingType)
        const frameW = sheetW / framesPerTier
        const tierH = sheetH / DAMAGED_TIER_COUNT
        const frameCol = getDamagedFrameIndex(this.renderTime, framesPerTier)
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
        // A style override is drawn as-is; tinting keys by building type and
        // would return the wrong (shared) sprite for a styled recipe shop.
        const tinted = ownerColor && !styleSprite
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
  // ─── Zone overlay ─────────────────────────────────────────────────────────
  // Renders the in-match zone overlay from ZoneSnapshot + static MapConfig.zones
  // geometry. Tints each zone by ownership (ally=blue, enemy=red, neutral=grey),
  // marks contested zones with a pulsing outline, and renders a progress fill
  // for presence zones. Zones capturable by the viewing team (adjacent to an
  // ally-owned zone) are rendered with a distinct capturable tint.
  //
  // IMPORTANT: this is DISPLAY ONLY. No capture simulation is done client-side.
  // The owner/contested/progress values come solely from the server snapshots.
  // ──────────────────────────────────────────────────────────────────────────

  // True when any building footprint overlaps the 2x2 claim slot whose top-left
  // is (px, py). Used to suppress the ghost tower once a real tower is placed.
  private claimSlotHasBuilding(px: number, py: number): boolean {
    for (const b of this.state.mapConfig.buildings) {
      if (px < b.x + b.width && px + 2 > b.x && py < b.y + b.height && py + 2 > b.y) return true
    }
    return false
  }

  private drawZoneOverlay() {
    const zones = this.state.mapConfig.zones
    if (!zones || zones.length === 0) return
    if (this.state.zoneSnapshotsById.size === 0) return

    const ctx = this.ctx
    const cellSize = this.state.mapConfig.cellSize
    const localPlayerId = this.state.localPlayerId

    // Build a set of zone ids owned by the local player (or an ally).
    const allyOwnedIds = new Set<string>()
    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      if (!snap) continue
      // A zone is allied when the owner is the team sentinel (every capture and
      // every locked home zone resolves to "team") or matches the local player.
      // ENEMY_PLAYER_ID and 'neutral' are never allied.
      if (snap.owner === ZONE_TEAM_OWNER || snap.owner === localPlayerId) {
        allyOwnedIds.add(zone.id)
      }
    }

    // Index every owned cell to its owner color. Used to drop the shared border
    // between two adjacent zones with the SAME owner so they read as one large
    // continuous zone. Unowned / neutral zones (no owner color) are not indexed,
    // so their borders are always kept.
    const cellOwnerColor = new Map<string, string>()
    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      if (!snap) continue
      const oc = snap.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null
      if (!oc) continue
      for (const [x, y] of zone.cells) cellOwnerColor.set(cellKey(x, y), oc)
    }

    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      if (!snap) continue

      const edges = zoneBoundaryEdges(zone)
      const isAlly = allyOwnedIds.has(zone.id)
      const isContested = snap.contested === true

      // Perimeter color = the controlling player's color (server-resolved;
      // team-owned → the lowest-slot player's color). Unowned/neutral → grey.
      const ownerColor = snap.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null
      const perimColor = ownerColor ? this.withAlpha(ownerColor, 0.6) : 'rgba(70,70,70,0.75)'

      ctx.save()

      // Trace only the OUTER edges of the zone — the cell sides that border a
      // non-member — so zones read as a thin outline, not a band of filled
      // perimeter squares. The interior keeps the normal ground color.
      //
      // The outline is inset slightly INWARD (along each edge's inward normal)
      // so two adjacent zones that share a boundary each draw their own line
      // just inside their own cells, instead of overpainting the same pixels
      // (which would hide one zone's color behind the other's).
      const inset = Math.min(2 / this.camera.zoom, cellSize * 0.25)
      const trace = () => {
        ctx.beginPath()
        for (const e of edges) {
          // Drop the shared border with an adjacent same-owner zone: the cell on
          // the outside of this edge belongs to another zone with our owner
          // color, so the two should look like one merged region.
          if (ownerColor && cellOwnerColor.get(cellKey(e.nbx, e.nby)) === ownerColor) continue
          const ox = e.nx * inset
          const oy = e.ny * inset
          ctx.moveTo(e.x1 * cellSize + ox, e.y1 * cellSize + oy)
          ctx.lineTo(e.x2 * cellSize + ox, e.y2 * cellSize + oy)
        }
      }

      ctx.strokeStyle = perimColor
      ctx.lineWidth = 2 / this.camera.zoom
      ctx.setLineDash([])
      trace()
      ctx.stroke()

      // Contested: pulsing dashed outline on the boundary (time-driven blink).
      if (isContested) {
        const blinkAlpha = 0.5 + 0.5 * Math.sin(this.renderTime / 300)
        ctx.strokeStyle = `rgba(251,191,36,${blinkAlpha.toFixed(2)})`
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.setLineDash([6 / this.camera.zoom, 4 / this.camera.zoom])
        trace()
        ctx.stroke()
        ctx.setLineDash([])
      }

      // Capture progress is shown by the top-screen bar (drawCaptureProgressBar),
      // not by filling the zone — see that method.

      // Presence capture sub-zone: a dashed cyan outline (with a faint fill)
      // marking exactly where the player's units must stand to capture. Shown
      // until the zone is team-owned. Only drawn when an explicit capture
      // sub-zone is authored; without one the whole zone is the capture area and
      // the zone outline above already shows it.
      if (
        zone.capture?.type === 'presence' &&
        !isAlly &&
        zone.captureCells &&
        zone.captureCells.length > 0
      ) {
        const capCells = zone.captureCells
        const pulse = 0.55 + 0.45 * Math.sin(this.renderTime / 350)
        ctx.fillStyle = 'rgba(34,211,238,0.12)' // faint cyan "stand here" area
        for (const [x, y] of capCells) {
          ctx.fillRect(x * cellSize, y * cellSize, cellSize, cellSize)
        }
        ctx.strokeStyle = `rgba(34,211,238,${pulse.toFixed(2)})`
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([6 / this.camera.zoom, 4 / this.camera.zoom])
        ctx.beginPath()
        for (const e of cellSetBoundaryEdges(capCells)) {
          const ox = e.nx * inset
          const oy = e.ny * inset
          ctx.moveTo(e.x1 * cellSize + ox, e.y1 * cellSize + oy)
          ctx.lineTo(e.x2 * cellSize + ox, e.y2 * cellSize + oy)
        }
        ctx.stroke()
        ctx.setLineDash([])
      }

      // Claim mechanic: highlight each 2x2 capture-point slot so the player
      // knows where to build towers. Captured points render green; outstanding
      // points pulse cyan. Whole block hidden once the zone is team-owned.
      if (zone.capture?.type === 'claim' && !isAlly) {
        const points: [number, number][] =
          zone.claimPoints && zone.claimPoints.length > 0
            ? zone.claimPoints
            : [[zone.anchor.x, zone.anchor.y]]
        const slot = 2 * cellSize
        const pulse = 0.6 + 0.4 * Math.sin(this.renderTime / 350)
        points.forEach((p, i) => {
          const sx = p[0] * cellSize
          const sy = p[1] * cellSize
          const captured = snap.claimPoints?.[i]?.captured ?? false
          if (captured) {
            ctx.fillStyle = 'rgba(74,222,128,0.20)' // green: point held
            ctx.fillRect(sx, sy, slot, slot)
            ctx.strokeStyle = 'rgba(74,222,128,0.9)'
            ctx.lineWidth = 2.5 / this.camera.zoom
            ctx.setLineDash([])
            ctx.strokeRect(sx, sy, slot, slot)
          } else {
            // Ghost tower: translucent preview of what to build here, until a
            // real building occupies the slot.
            if (!this.claimSlotHasBuilding(p[0], p[1])) {
              const towerType = (zone.capture?.config?.['towerType'] as string | undefined) ?? 'tower'
              const ghost = getBuildingSprite(towerType)
              if (ghost) {
                ctx.save()
                ctx.globalAlpha = 0.35
                ctx.drawImage(ghost, sx, sy, slot, slot)
                ctx.restore()
              }
            }
            ctx.fillStyle = 'rgba(34,211,238,0.16)' // cyan build slot
            ctx.fillRect(sx, sy, slot, slot)
            ctx.strokeStyle = `rgba(34,211,238,${pulse.toFixed(2)})`
            ctx.lineWidth = 2.5 / this.camera.zoom
            ctx.setLineDash([6 / this.camera.zoom, 4 / this.camera.zoom])
            ctx.strokeRect(sx, sy, slot, slot)
            ctx.setLineDash([])
          }
        })
      }

      ctx.restore()
    }
  }

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

  // Draws ground-loot chests from state.lootDropsById. Live drops render the
  // chest sprite's idle frame; drops that disappeared since the previous frame
  // get a short "opening" animation played at their last-known position before
  // vanishing. Falls back to a procedural rounded-rect when the sprite set
  // hasn't loaded yet (first frames after page load).
  private drawLootDrops() {
    const drops = this.state.lootDropsById
    const ctx = this.ctx
    const now = performance.now()

    const spriteSet = getObjectSpriteSet('chest')
    const idleAnim = spriteSet?.animations.get('idle') ?? null
    const openingAnim = spriteSet?.animations.get('opening') ?? null

    // Detect drops that left state since the previous render frame and queue
    // an opening animation at their last-known position. Captures the player-
    // pickup path AND any other removal (expiration, etc.) uniformly.
    if (openingAnim) {
      for (const [id, pos] of this.lastLootDropPositions) {
        if (!drops.has(id) && !this.openingLootDrops.has(id)) {
          this.openingLootDrops.set(id, { x: pos.x, y: pos.y, startedAt: now })
        }
      }
    }

    // Refresh the position snapshot for next frame's diff.
    this.lastLootDropPositions.clear()
    for (const drop of drops.values()) {
      this.lastLootDropPositions.set(drop.id, { x: drop.x, y: drop.y })
    }

    // Expire opening animations once they've played through.
    for (const [id, anim] of this.openingLootDrops) {
      if (now - anim.startedAt >= this.LOOT_OPENING_DURATION_MS) {
        this.openingLootDrops.delete(id)
      }
    }

    if (drops.size === 0 && this.openingLootDrops.size === 0) return

    const fallbackSize = 24 // world px — matches InputManager's click radius
    const spriteReady =
      spriteSet != null &&
      idleAnim != null &&
      idleAnim.sheet.complete &&
      idleAnim.sheet.naturalWidth > 0
    const scale = spriteSet?.scale ?? this.OBJECT_SPRITE_SCALE
    const offX = (spriteSet?.offsetX ?? 0) * scale
    const offY = (spriteSet?.offsetY ?? 0) * scale

    // A small soft shadow grounds each chest. Sized from the chest's world
    // footprint and seated just below its center so the box reads as sitting on
    // the terrain. Same gradient + global light direction as unit/building
    // shadows (see drawGroundShadow).
    const chestShadowRadiusX = fallbackSize * 0.46
    const chestShadowRadiusY = chestShadowRadiusX * 0.4
    const chestShadowCenterDy = fallbackSize * 0.33

    // Live (un-collected) chests — idle frame.
    for (const drop of drops.values()) {
      this.drawGroundShadow(
        drop.x,
        drop.y + chestShadowCenterDy,
        chestShadowRadiusX,
        chestShadowRadiusY,
        0.4,
      )
      if (spriteReady && idleAnim) {
        this.drawObjectFrame(idleAnim, 0, drop.x + offX, drop.y + offY, scale)
      } else {
        this.drawLootDropFallback(drop.x, drop.y, fallbackSize)
      }

      if (this.state.hoveredLootDropId === drop.id) {
        ctx.save()
        ctx.strokeStyle = 'rgba(245, 180, 0, 0.9)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([4 / this.camera.zoom, 3 / this.camera.zoom])
        ctx.beginPath()
        ctx.arc(drop.x, drop.y, fallbackSize * 0.72, 0, Math.PI * 2)
        ctx.stroke()
        ctx.setLineDash([])
        ctx.restore()
      }
    }

    // Collected chests — play the opening sheet once at their last position.
    if (
      spriteSet &&
      openingAnim &&
      openingAnim.sheet.complete &&
      openingAnim.sheet.naturalWidth > 0
    ) {
      for (const opening of this.openingLootDrops.values()) {
        const elapsed = now - opening.startedAt
        const frameIndex = Math.min(
          Math.floor(elapsed / this.LOOT_OPENING_FRAME_MS),
          openingAnim.frameCount - 1,
        )
        this.drawObjectFrame(
          openingAnim,
          frameIndex,
          opening.x + offX,
          opening.y + offY,
          scale,
        )
      }
    }
  }

  // Procedural chest shape used while the sprite sheet is still loading or
  // when the asset is missing entirely. Mirrors the original placeholder so
  // a brand-new client never sees a blank loot drop.
  private drawLootDropFallback(cx: number, cy: number, size: number) {
    const ctx = this.ctx
    ctx.save()

    const x = cx - size / 2
    const y = cy - size / 2
    const r = 4

    ctx.shadowColor = 'rgba(0, 0, 0, 0.55)'
    ctx.shadowBlur = 4 / this.camera.zoom

    ctx.fillStyle = '#8b6914'
    ctx.strokeStyle = '#3d2c0a'
    ctx.lineWidth = 1.5 / this.camera.zoom
    ctx.beginPath()
    ctx.moveTo(x + r, y)
    ctx.lineTo(x + size - r, y)
    ctx.arcTo(x + size, y, x + size, y + r, r)
    ctx.lineTo(x + size, y + size - r)
    ctx.arcTo(x + size, y + size, x + size - r, y + size, r)
    ctx.lineTo(x + r, y + size)
    ctx.arcTo(x, y + size, x, y + size - r, r)
    ctx.lineTo(x, y + r)
    ctx.arcTo(x, y, x + r, y, r)
    ctx.closePath()
    ctx.fill()

    ctx.shadowColor = 'transparent'
    ctx.stroke()

    ctx.strokeStyle = '#f5b400'
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.beginPath()
    ctx.moveTo(x + 3, y + size * 0.4)
    ctx.lineTo(x + size - 3, y + size * 0.4)
    ctx.stroke()

    ctx.restore()
  }

  // Lazily builds (and caches) the unit-circle radial gradient used for every
  // ground shadow: opaque black at the center fading to transparent at radius
  // 1. Per-entity ellipse size comes from a scale() transform and per-entity
  // opacity from globalAlpha at draw time, so this single gradient serves all
  // entities. Tied to the stable `this.ctx`, which is never reassigned.
  private getGroundShadowGradient(): CanvasGradient {
    if (!this.groundShadowGradient) {
      const g = this.ctx.createRadialGradient(0, 0, 0, 0, 0, 1)
      g.addColorStop(0, 'rgba(0, 0, 0, 1)')
      g.addColorStop(0.65, 'rgba(0, 0, 0, 0.85)')
      g.addColorStop(1, 'rgba(0, 0, 0, 0)')
      this.groundShadowGradient = g
    }
    return this.groundShadowGradient
  }

  // Draws one soft elliptical ground shadow centered at (centerX, centerY) with
  // the given pixel radii and peak opacity. Applies the global scene lighting:
  // the shadow is nudged toward the north-west (light from the south-east),
  // proportional to its size so taller entities cast longer shadows. Shared by
  // units, buildings, and loot drops so the implied light source is consistent.
  private drawGroundShadow(
    centerX: number,
    centerY: number,
    radiusX: number,
    radiusY: number,
    opacity: number,
  ) {
    if (opacity <= 0 || radiusX <= 0 || radiusY <= 0) return
    const shift = radiusX * SHADOW_LIGHT_SHIFT
    const cx = centerX + SHADOW_LIGHT_DX * shift
    const cy = centerY + SHADOW_LIGHT_DY * shift
    const ctx = this.ctx
    ctx.save()
    ctx.globalAlpha = opacity
    ctx.translate(cx, cy)
    ctx.scale(radiusX, radiusY)
    ctx.beginPath()
    ctx.arc(0, 0, 1, 0, Math.PI * 2)
    ctx.fillStyle = this.getGroundShadowGradient()
    ctx.fill()
    ctx.restore()
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
      mana?: number
      maxMana?: number
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
      flyer?: boolean
      focusTargetId?: number
      channelLoopStart?: number
      channelLoopEnd?: number
    }>,
  ) {
    const ctx = this.ctx
    const { cellSize } = this.state.mapConfig
    const activeUnitIds = new Set<number>()

    // Pre-index effect names by anchor unit so the per-unit perk-aura check
    // below is O(1) instead of O(effects) for every unit with perk ids.
    const effectNamesByUnit = new Map<number, Set<string>>()
    for (const e of this.state.effects) {
      if (e.anchorUnitId == null) continue
      let names = effectNamesByUnit.get(e.anchorUnitId)
      if (!names) {
        names = new Set<string>()
        effectNamesByUnit.set(e.anchorUnitId, names)
      }
      names.add(e.name)
    }

    // Pre-index unit IDs that are the Focus Target of the currently-selected
    // unit, so the per-unit render can draw a glow ring under them. The ring
    // is intentionally gated on EXACTLY ONE selected unit: showing rings for
    // every Cleric's focus in a multi-selection would clutter the world. So
    // the ring reads as "this is the ally that the unit you have selected is
    // focused on" — clean 1:1 visual mapping. Matches the autocast button
    // glow color so "Focus Target button is active" and "this is the
    // focused unit" are obviously the same idea.
    const localPlayerId = this.state.localPlayerId
    const focusTargetIds = new Set<number>()
    if (localPlayerId && this.state.selectedUnitIds.size === 1) {
      // Single-selection branch: read the focus from whatever unit is solely
      // selected (must be local-player-owned and have an active focus).
      const onlySelectedId = this.state.selectedUnitIds.values().next().value
      for (const u of units) {
        if (u.id !== onlySelectedId) continue
        if (u.ownerId !== localPlayerId) break
        if (!u.focusTargetId) break
        focusTargetIds.add(u.focusTargetId)
        break
      }
    }

    // Pre-resolve the active commander targeting AoE so the per-unit loop
    // below can do an O(1) inside-radius test instead of looking the ability
    // up for every unit. Null whenever no commander targeting is active or
    // the ability has no usable radius.
    const commanderAoE = this.getActiveCommanderAoE()

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

      // Ground shadow — a soft blob under the feet so the sprite reads as
      // standing ON the terrain instead of floating as a flat sticker. Drawn
      // first (lowest layer) so selection rings, perk auras, and the sprite
      // all sit on top. Size/opacity default from the unit's bounds; an
      // optional `shadow` block in the catalog tunes or disables it. Flyers
      // get an offset, larger, fainter shadow (see resolveUnitShadow). Skipped
      // for sprite-less placeholder blobs, which already read as grounded.
      if (spriteSet) {
        const shadow = resolveUnitShadow(unitDef?.shadow, unitBounds, !!unit.flyer)
        if (shadow) {
          const feetX = unit.x + ringOffsetX + shadow.offsetX
          const feetY = unit.y + bottomOffset - ringLift + ringOffsetY + shadow.offsetY
          this.drawGroundShadow(feetX, feetY, shadow.radiusX, shadow.radiusY, shadow.opacity)
        }
      }

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
        const activeEffectNames = effectNamesByUnit.get(unit.id)
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

      // Focus Target marker — sky-blue glow ring around any unit currently
      // assigned as a Cleric's focus. Same hue as the action-bar autocast
      // glow so "Focus Target button is active" and "this is the focused
      // unit" read as one connected visual idea. Stacks below the selection
      // ring if both apply (a player can focus on a unit they've selected).
      if (focusTargetIds.has(unit.id)) {
        ctx.save()
        ctx.shadowColor = 'rgba(90, 190, 255, 0.85)'
        ctx.shadowBlur = 9 / this.camera.zoom
        ctx.strokeStyle = 'rgba(90, 190, 255, 0.95)'
        ctx.lineWidth = 2.5 / this.camera.zoom
        ctx.beginPath()
        ctx.ellipse(
          selectionCenterX,
          selectionCenterY,
          selectionRadiusX + 1.5,
          selectionRadiusY + 1.5,
          0,
          0,
          Math.PI * 2,
        )
        ctx.stroke()
        ctx.restore()
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

      // Friendly hover ring (sky-blue dashed). Mirrors the enemy hover ring
      // visually but in the autocast / focus-target hue so the family of
      // ally-targeting cues stays cohesive: the cursor is a target reticle,
      // the action button glows blue, and the unit under the cursor lights
      // up in matching blue. Only set while the player is in a cast-ability
      // or focus-target cursor mode (see InputManager.updateHoverCursor).
      if (this.state.hoveredFriendlyUnitId === unit.id) {
        ctx.save()
        ctx.strokeStyle = 'rgba(90, 190, 255, 0.95)'
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

      // Commander AoE highlight: if the player is currently aiming Smite /
      // Blessing and this unit's body sits inside the cursor's AoE AND the
      // unit matches the ability's affect team (hostile for damage, friendly
      // for heal), paint a solid ring in the ability's tint so the player
      // can see at a glance which units the cast will actually hit.
      if (commanderAoE) {
        const dx = unit.x - commanderAoE.centerX
        const dy = unit.y - commanderAoE.centerY
        if (
          dx * dx + dy * dy <= commanderAoE.radiusSq &&
          this.unitMatchesCommanderAoETeam(unit, commanderAoE.affectsTeam) &&
          (commanderAoE.eligible?.(unit) ?? true)
        ) {
          ctx.save()
          ctx.strokeStyle = commanderAoE.unitRingColor
          ctx.lineWidth = 2.5 / this.camera.zoom
          ctx.beginPath()
          ctx.ellipse(selectionCenterX, selectionCenterY, selectionRadiusX + 1, selectionRadiusY + 1, 0, 0, Math.PI * 2)
          ctx.stroke()
          ctx.restore()
        }
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
          // Protocol now always sends actionFacingDx/Dy (no omitempty), so
          // 0,0 explicitly means "not mid-swing this tick" and non-zero means
          // "server is firing in this direction."
          //
          // Strategy: use the server value when non-zero (update cache);
          // when zero (windup / cooldown tick), re-use the cached facing from
          // the last firing tick so the sprite stays oriented at the target
          // instead of snapping away. Only run the expensive findAttackFacing
          // scan for units that have never had a server-supplied facing.
          const sdx = unit.actionFacingDx ?? 0
          const sdy = unit.actionFacingDy ?? 0
          if (sdx !== 0 || sdy !== 0) {
            const facing = { dx: sdx, dy: sdy }
            this.unitAttackFacingCache.set(unit.id, facing)
            actionFacing = facing
          } else {
            const cached = this.unitAttackFacingCache.get(unit.id)
            if (cached) {
              actionFacing = cached
            } else {
              // No cached facing yet — run the search once and cache it.
              const found = findAttackFacing(
                unit,
                unitDef,
                units,
                this.state.mapConfig.buildings,
                this.state.mapConfig.cellSize,
              )
              if (found) {
                this.unitAttackFacingCache.set(unit.id, found)
                actionFacing = found
              }
            }
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
          unit.path,
          unit.ownerId,
          this.state.localPlayerId,
          unit.flyer,
          unit.channelLoopStart,
          unit.channelLoopEnd,
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
    for (const id of this.unitAttackFacingCache.keys()) {
      if (!activeUnitIds.has(id)) this.unitAttackFacingCache.delete(id)
    }
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

  /**
   * Render all active channeled beams. Each beam is a sustained line from the
   * caster to the target for the duration of the channel. Endpoints are
   * derived from the current unit positions each frame — the server does not
   * send coords; that is intentional.
   *
   * Per-variant dispatch: add cases to the inner switch for future beam types.
   * Defaults to the siphon_life look for any unrecognised variant.
   */
  private drawBeams(beams: BeamSnapshot[]) {
    if (beams.length === 0) return

    const unitsById = new Map<number, Unit>()
    for (const u of this.state.units) unitsById.set(u.id, u)

    const originLiftCache = new Map<string, { x: number; y: number }>()
    const targetLiftCache = new Map<string, { x: number; y: number }>()

    for (const beam of beams) {
      const caster = unitsById.get(beam.casterUnitId)
      const target = unitsById.get(beam.targetUnitId)
      // Defensive: skip if either unit is missing from the current frame.
      if (!caster || !target) continue

      const originLift = this.getProjectileOriginLift(caster, originLiftCache)
      const targetLift = this.getProjectileTargetLift(target, targetLiftCache)

      // Per-unit nudge for the beam source — sprites whose chest anchor
      // doesn't fit (tall hood lands the default on the head; staff hand is
      // off to one side) declare a `beamOrigin` in their sprites.json.
      // Screen-space delta — does not rotate with facing.
      const casterSpriteSet = getUnitSpriteSet(caster.path, caster.unitType)
      const beamOrigin = casterSpriteSet?.beamOrigin ?? { x: 0, y: 0 }

      const originX = caster.x + originLift.x + beamOrigin.x
      const originY = caster.y + originLift.y + beamOrigin.y
      const endX = target.x + targetLift.x
      const endY = target.y + targetLift.y

      switch (beam.variant) {
        case 'siphon_life':
          this.drawSiphonLifeBeam(originX, originY, endX, endY)
          break
        case 'chain_siphon':
          // Secondary beam fired by the chain_siphon perk: primary target ->
          // chain target. Reuses the siphon_life look for now (the beam
          // emerges from the primary victim's body and "leaps" to the chain
          // victim, so the same necrotic green tendril reads correctly). If
          // we later want to differentiate (thinner, dimmer, different tint)
          // this is the seam — author a dedicated drawChainSiphonBeam.
          this.drawSiphonLifeBeam(originX, originY, endX, endY)
          break
        default:
          this.drawSiphonLifeBeam(originX, originY, endX, endY)
          break
      }
    }
  }

  /**
   * Draw a single Siphon Life drain beam — a sustained necrotic green energy
   * tendril from the caster's hand to the target.
   *
   * Hybrid render:
   *   1. Animated sprite body — frame strip from assets/beams/siphon_life/,
   *      stretched along the caster→target vector. Frame index advances from
   *      performance.now() so the beam wiggles continuously while channeling.
   *      Sprite supplies its own outline + core; manifest's axisRotation
   *      compensates for art painted on a diagonal.
   *   2. Caster-side puff   — soft radial gradient at origin (energy emanating
   *      from the hand). Sells the "this unit is the source" relationship.
   *   3. Target-side ember  — brighter radial gradient at endpoint. Sells the
   *      "being drained" relationship at the absorption point.
   *
   * If the sprite hasn't decoded yet (first-frame race) we still draw the
   * end-glows so the beam isn't visually missing.
   */
  private drawSiphonLifeBeam(originX: number, originY: number, endX: number, endY: number) {
    const ctx = this.ctx

    const dx = endX - originX
    const dy = endY - originY
    const length = Math.hypot(dx, dy)
    if (length < 0.5) return // caster and target coincident — nothing meaningful to draw
    const angle = Math.atan2(dy, dx)

    // Pulse used by the end-glows so they breathe in time with the channel.
    const pulseT = (Math.sin((performance.now() / 700) * Math.PI * 2) + 1) / 2 // 0..1
    const pulseFactor = 0.85 + 0.15 * pulseT

    const sprite = getBeamSprite('siphon_life')
    if (sprite && sprite.image && sprite.image.complete && sprite.image.naturalWidth > 0) {
      const { image, frameWidth, frameHeight, frames, frameDurationMs, axisRotation, headOnRight, displayHeight } = sprite
      const frameIndex = Math.floor(performance.now() / frameDurationMs) % frames
      const sx = frameIndex * frameWidth
      const axisRad = (axisRotation * Math.PI) / 180

      ctx.save()
      const prevSmoothing = ctx.imageSmoothingEnabled
      ctx.imageSmoothingEnabled = false
      ctx.translate(originX, originY)
      // Rotate so the painted axis ends up parallel to caster→target.
      ctx.rotate(angle - axisRad)
      if (!headOnRight) {
        // Mirror horizontally so the painted "head" (impact end) lands on the
        // target side after the stretch.
        ctx.translate(length, 0)
        ctx.scale(-1, 1)
      }
      ctx.drawImage(image, sx, 0, frameWidth, frameHeight, 0, -displayHeight / 2, length, displayHeight)
      ctx.imageSmoothingEnabled = prevSmoothing
      ctx.restore()
    }

    // Caster-side puff: soft green radial gradient at the origin.
    const puffRadius = Math.max(3, 5 / this.camera.zoom)
    const puffGradient = ctx.createRadialGradient(originX, originY, 0, originX, originY, puffRadius)
    puffGradient.addColorStop(0, `rgba(200, 255, 215, ${0.75 * pulseFactor})`)
    puffGradient.addColorStop(0.55, `rgba(50, 200, 100, ${0.35 * pulseFactor})`)
    puffGradient.addColorStop(1, 'rgba(0, 100, 50, 0)')
    ctx.save()
    ctx.fillStyle = puffGradient
    ctx.beginPath()
    ctx.arc(originX, originY, puffRadius, 0, Math.PI * 2)
    ctx.fill()
    ctx.restore()

    // Target-side ember: slightly brighter and more saturated to sell the
    // absorption point ("energy being drained here").
    const emberRadius = Math.max(3.5, 6 / this.camera.zoom)
    const emberAlpha = 0.80 + 0.10 * pulseFactor
    const emberGradient = ctx.createRadialGradient(endX, endY, 0, endX, endY, emberRadius)
    emberGradient.addColorStop(0, `rgba(160, 255, 180, ${emberAlpha})`)
    emberGradient.addColorStop(0.45, `rgba(30, 180, 80, ${0.55 * emberAlpha})`)
    emberGradient.addColorStop(1, 'rgba(0, 80, 30, 0)')
    ctx.save()
    ctx.fillStyle = emberGradient
    ctx.beginPath()
    ctx.arc(endX, endY, emberRadius, 0, Math.PI * 2)
    ctx.fill()
    ctx.restore()
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

    // All damage popup font sizes bumped up a few points for readability.
    // Crit and combined are scaled in lockstep to preserve the size
    // hierarchy (combined > crit > base > minor).
    const baseFontPx = Math.max(15, 17 / this.camera.zoom)
    const combinedFontPx = Math.max(19, 22 / this.camera.zoom)
    const critFontPx = Math.max(17, 20 / this.camera.zoom)
    // Minor (Reactive Flames splash, Divine Judgement, etc.) — still smaller
    // than base so it reads as ancillary damage.
    const minorFontPx = Math.max(14, 15 / this.camera.zoom)

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
        // from the trap itself. Color shared with major popups via
        // colorForDamageVariant; minor's fallback (no variant) is the
        // legacy fire / orange used by Reactive Flames.
        ctx.font = `bold ${minorFontPx}px sans-serif`
        ctx.strokeText(text, drawX, drawY)
        ctx.fillStyle = colorForDamageVariant(num.minorVariant, '#fb923c')
        ctx.fillText(text, drawX, drawY)
      } else if (num.kind === 'heal') {
        // Intentional healing — light-green "+N" floating up, distinct from
        // the (red/white) damage numbers and the green HP bar.
        const healText = `+${text}`
        ctx.font = `bold ${baseFontPx}px sans-serif`
        ctx.strokeText(healText, drawX, drawY)
        ctx.fillStyle = '#4ade80' // tailwind green-400
        ctx.fillText(healText, drawX, drawY)
      } else if (num.kind === 'manaRestore') {
        // Intentional mana grant (Repurposed Life, future cleric mana
        // abilities) — blue "+N" floating up, matching the mana-bar's
        // visual identity. Passive regen does not emit, so this only
        // fires for perk / ability grants.
        const manaText = `+${text}`
        ctx.font = `bold ${baseFontPx}px sans-serif`
        ctx.strokeText(manaText, drawX, drawY)
        ctx.fillStyle = '#60a5fa' // tailwind blue-400
        ctx.fillText(manaText, drawX, drawY)
      } else {
        // Major (floating-up) popup — color by damage type when the server
        // has tagged this HP-loss via damageTypeHints; otherwise default
        // friendly-red / enemy-white. Shares the colorForDamageVariant
        // palette with the minor branch above so a "shadow" Siphon Life
        // primary tick and a "shadow" Shared Suffering echo read as the
        // same hue, just at different sizes / animation styles.
        ctx.font = `bold ${baseFontPx}px sans-serif`
        ctx.strokeText(text, drawX, drawY)
        const defaultFill = num.isFriendly ? '#ef4444' : '#ffffff'
        ctx.fillStyle = colorForDamageVariant(num.damageType, defaultFill)
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

  // Floating loot-pickup labels — icon + label that rises and fades above the
  // collecting unit for each resource line and item the unit just picked up.
  // Mirrors drawFloatingResourceNumbers; duration is longer (1300 ms) because
  // pickup is a discrete event the player wants to read fully.
  private drawFloatingLootPickups() {
    if (this.floatingLootPickups.length === 0) return
    const ctx = this.ctx
    const now = performance.now()

    // Drop expired entries first so we only iterate live ones.
    this.floatingLootPickups = this.floatingLootPickups.filter(
      (f) => now - f.startedAt < this.FLOATING_LOOT_DURATION_MS,
    )
    if (this.floatingLootPickups.length === 0) return

    ctx.save()
    ctx.textAlign = 'left'
    ctx.textBaseline = 'middle'
    ctx.lineWidth = 3 / this.camera.zoom
    ctx.strokeStyle = 'rgba(15, 23, 42, 0.9)'
    ctx.font = `bold ${Math.max(12, 14 / this.camera.zoom)}px sans-serif`

    const iconPx = Math.max(12, 16 / this.camera.zoom)
    const gap = Math.max(2, 3 / this.camera.zoom)

    for (const f of this.floatingLootPickups) {
      const elapsed = now - f.startedAt
      const t = elapsed / this.FLOATING_LOOT_DURATION_MS
      const drawY = f.y - this.FLOATING_LOOT_RISE_PX * t
      // Ease-out fade: starts sharp, softens toward end.
      ctx.globalAlpha = Math.max(0, 1 - t * t)

      const text = f.label
      const textWidth = ctx.measureText(text).width
      const totalWidth = iconPx + gap + textWidth
      const groupX = f.x - totalWidth / 2

      let icon: HTMLImageElement | null = null
      if (f.kind === 'resource' && f.resourceId) {
        icon = getResourceIconImage(f.resourceId)
      } else if (f.kind === 'item' && f.itemId) {
        icon = resolveItemIconImage(f.itemId)
      }

      if (icon && icon.complete && icon.naturalWidth > 0) {
        const prevSmoothing = ctx.imageSmoothingEnabled
        ctx.imageSmoothingEnabled = false
        ctx.drawImage(icon, groupX, drawY - iconPx / 2, iconPx, iconPx)
        ctx.imageSmoothingEnabled = prevSmoothing
      } else {
        // Fallback dot when the asset hasn't loaded yet.
        ctx.fillStyle = f.color
        ctx.beginPath()
        ctx.arc(groupX + iconPx / 2, drawY, Math.max(4, 5 / this.camera.zoom), 0, Math.PI * 2)
        ctx.fill()
      }

      const textX = groupX + iconPx + gap
      ctx.strokeText(text, textX, drawY)
      ctx.fillStyle = f.color
      ctx.fillText(text, textX, drawY)
    }

    ctx.restore()
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
      mana?: number
      maxMana?: number
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

    // Shield overlay: an orange bar stacked directly above the HP bar, scaled
    // against max-shield. Only drawn when the unit actually carries shield.
    // Orange (not blue) so it can't be confused with the blue mana bar below.
    const shield = Math.max(0, unit.shield ?? 0)
    const maxShield = Math.max(0, unit.maxShield ?? 0)
    if (maxShield > 0 && shield > 0) {
      const shieldY = barY - barHeight - 1
      ctx.fillStyle = '#0f172a'
      ctx.fillRect(barX, shieldY, barWidth, barHeight)
      ctx.fillStyle = '#f97316'
      ctx.fillRect(barX, shieldY, barWidth * Math.min(1, shield / maxShield), barHeight)
      ctx.strokeStyle = 'rgba(248, 250, 252, 0.6)'
      ctx.lineWidth = 1 / this.camera.zoom
      ctx.strokeRect(barX, shieldY, barWidth, barHeight)
    }

    // Mana bar: a blue bar stacked directly below the HP bar, scaled against
    // max-mana. Drawn for any spellcaster (maxMana > 0) even when the pool is
    // empty, so the depleted state stays visible (e.g. acolyte).
    const maxMana = Math.max(0, unit.maxMana ?? 0)
    if (maxMana > 0) {
      const mana = Math.max(0, Math.min(unit.mana ?? 0, maxMana))
      const manaY = barY + barHeight + 1
      ctx.fillStyle = '#0f172a'
      ctx.fillRect(barX, manaY, barWidth, barHeight)
      ctx.fillStyle = '#3b82f6'
      ctx.fillRect(barX, manaY, barWidth * (mana / maxMana), barHeight)
      ctx.strokeStyle = 'rgba(248, 250, 252, 0.6)'
      ctx.lineWidth = 1 / this.camera.zoom
      ctx.strokeRect(barX, manaY, barWidth, barHeight)
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
      ctx.stroke(getCachedPath2D(iconPath))
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
      ctx.stroke(getCachedPath2D(iconPath))
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
    const renderDef = getBuildingFallbackRender(placement.buildingType)
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

  // Per-ability targeting metadata. The server's commander_abilities.go is
  // the source of truth for which team each ability affects (Damage > 0 ⇒
  // hostiles, Heal > 0 ⇒ allies); we mirror that mapping here so the AoE
  // preview tints + per-unit ring colors match the cast outcome. Add a new
  // entry whenever a commander ability is introduced.
  private static readonly COMMANDER_AOE_THEMES: Record<
    string,
    {
      affectsTeam: 'enemy' | 'friendly'
      fill: string
      stroke: string
      unitRing: string
    }
  > = {
    smite: {
      affectsTeam: 'enemy',
      fill: 'rgba(239, 68, 68, 0.14)',
      stroke: 'rgba(239, 68, 68, 0.85)',
      unitRing: 'rgba(248, 113, 113, 0.95)',
    },
    blessing: {
      affectsTeam: 'friendly',
      fill: 'rgba(74, 222, 128, 0.14)',
      stroke: 'rgba(74, 222, 128, 0.85)',
      unitRing: 'rgba(134, 239, 172, 0.95)',
    },
  }

  // Theme for the consumable-item AoE preview. Items always target allies
  // (heals, XP), so it reuses the friendly-green language of Blessing with a
  // warmer gold ring to read as "item" rather than "ability".
  private static readonly ITEM_AOE_THEME = {
    affectsTeam: 'friendly' as const,
    fill: 'rgba(250, 204, 21, 0.12)',
    stroke: 'rgba(250, 204, 21, 0.85)',
    unitRing: 'rgba(253, 224, 71, 0.95)',
  }

  // Resolve the active ground-target AoE (commander ability OR consumable
  // item) if any. Returns the cursor center (in world space), the squared
  // radius (so the per-unit hit-test can skip the sqrt), the affected team,
  // and the mode-specific colors — or null when no AoE targeting is active /
  // the ability is unknown / has no usable radius.
  private getActiveCommanderAoE():
    | {
        centerX: number
        centerY: number
        radius: number
        radiusSq: number
        affectsTeam: 'enemy' | 'friendly'
        fillColor: string
        strokeColor: string
        unitRingColor: string
        // Optional per-unit eligibility on top of the team check. Mirrors
        // the server's consumableTargetEligibleLocked so the preview rings
        // only mark units that will actually receive (and split) the effect
        // — e.g. workers are skipped for XP potions.
        eligible?: (unit: { unitType?: string }) => boolean
      }
    | null {
    const itemTargeting = this.state.itemTargeting
    if (itemTargeting && itemTargeting.radius > 0) {
      const theme = CanvasRenderer.ITEM_AOE_THEME
      const effectType = ITEM_DEF_MAP.get(itemTargeting.itemId)?.consumable?.type
      return {
        centerX: this.state.cursorWorldX,
        centerY: this.state.cursorWorldY,
        radius: itemTargeting.radius,
        radiusSq: itemTargeting.radius * itemTargeting.radius,
        affectsTeam: theme.affectsTeam,
        fillColor: theme.fill,
        strokeColor: theme.stroke,
        unitRingColor: theme.unitRing,
        eligible:
          effectType === 'grant_xp'
            ? (unit) => unit.unitType !== 'worker'
            : undefined,
      }
    }

    const abilityId = this.state.commanderTargetingAbilityId
    if (!abilityId) return null
    const ability = this.state.localPlayerCommanderAbilities.find((a) => a.id === abilityId)
    const radius = ability?.radius ?? 0
    if (!ability || radius <= 0) return null
    const key = ability.id.replace(/^commander_/, '').toLowerCase()
    const theme = CanvasRenderer.COMMANDER_AOE_THEMES[key]
    if (!theme) return null
    return {
      centerX: this.state.cursorWorldX,
      centerY: this.state.cursorWorldY,
      radius,
      radiusSq: radius * radius,
      affectsTeam: theme.affectsTeam,
      fillColor: theme.fill,
      strokeColor: theme.stroke,
      unitRingColor: theme.unitRing,
    }
  }

  // Returns true when the unit is on the team the active commander ability
  // would actually affect. Mirrors the server's playersAreHostileLocked /
  // playersAreFriendlyLocked gates in applyCommanderAbilityLocked.
  private unitMatchesCommanderAoETeam(
    unit: { ownerId?: string },
    affectsTeam: 'enemy' | 'friendly',
  ): boolean {
    if (affectsTeam === 'enemy') {
      return this.state.isHostileToLocalPlayer(unit.ownerId)
    }
    // Friendly: must have an owner, must not be hostile to the local player.
    // Unowned / wave-enemy units are excluded by the ownerId guard.
    if (!unit.ownerId) return false
    return !this.state.isHostileToLocalPlayer(unit.ownerId)
  }

  // World-space AoE indicator drawn at the cursor while the player is
  // aiming Smite / Blessing. Sized to the ability's actual radius so the
  // visual previews exactly what the server will hit.
  private drawCommanderTargetingAoE() {
    const aoe = this.getActiveCommanderAoE()
    if (!aoe) return

    const ctx = this.ctx
    ctx.save()
    ctx.beginPath()
    ctx.arc(aoe.centerX, aoe.centerY, aoe.radius, 0, Math.PI * 2)
    ctx.fillStyle = aoe.fillColor
    ctx.fill()
    ctx.strokeStyle = aoe.strokeColor
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.setLineDash([8 / this.camera.zoom, 4 / this.camera.zoom])
    ctx.stroke()
    ctx.restore()
  }

  // Draws each zone's perimeter cells on the minimap, tinted by the controlling
  // player's color (grey when unowned) — a glanceable map of territorial
  // control. Mirrors the in-world owner-color logic, including merging the
  // shared border between two adjacent same-owner zones so they read as one
  // large territory.
  private drawMinimapZones(bounds: { x: number; y: number; width: number; height: number }) {
    const zones = this.state.mapConfig.zones
    if (!zones || zones.length === 0) return

    const ctx = this.ctx
    const { x, y, width: mmW, height: mmH } = bounds
    const { cellSize } = this.state.mapConfig
    const sx = mmW / this.state.mapWidth
    const sy = mmH / this.state.mapHeight
    const cw = Math.max(1, cellSize * sx)
    const ch = Math.max(1, cellSize * sy)

    // Group key per cell: same-owner zones share a key (their seam merges away),
    // while each neutral/unowned zone keeps its own key (borders preserved).
    // A cell is drawn only when it sits on the OUTER edge of its group.
    const cellGroup = new Map<string, string>()
    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      const oc = snap?.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null
      const key = oc ?? `z:${zone.id}`
      for (const [cx, cy] of zone.cells) cellGroup.set(cellKey(cx, cy), key)
    }

    for (const zone of zones) {
      const snap = this.state.zoneSnapshotsById.get(zone.id)
      const ownerColor = snap?.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null
      const key = ownerColor ?? `z:${zone.id}`
      ctx.fillStyle = ownerColor ?? 'rgba(160,160,160,0.85)'
      for (const [cx, cy] of zone.cells) {
        const onGroupEdge =
          cellGroup.get(cellKey(cx - 1, cy)) !== key ||
          cellGroup.get(cellKey(cx + 1, cy)) !== key ||
          cellGroup.get(cellKey(cx, cy - 1)) !== key ||
          cellGroup.get(cellKey(cx, cy + 1)) !== key
        if (!onGroupEdge) continue
        ctx.fillRect(x + cx * cellSize * sx, y + cy * cellSize * sy, cw, ch)
      }
    }
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
    if (panelRect) {
      ctx.fillStyle = '#000'
      ctx.fillRect(panelRect.x, panelRect.y, panelRect.width, panelRect.height)
    }

    // Static base layer (terrain + obstacles + buildings + border) — shared
    // with the lobby minimap preview via minimapLayers.ts so a new map
    // element type only has to be wired in once.
    drawMinimapBase(ctx, this.state.mapConfig, bounds, this.terrainCache, {
      localPlayerId: this.state.localPlayerId,
      getOwnerColor: (id) => this.state.getPlayerColor(id),
    })

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

    // Zone perimeters, owner-coloured — drawn above fog so the player can read
    // their territorial control across the whole map at a glance.
    this.drawMinimapZones(bounds)

    // Neutral camp POIs, shop markers, and loot-drop chest dots. All drawn
    // AFTER fog of war so they remain visible regardless of scouting state,
    // and AFTER the zone perimeters so a POI sitting on a zone border (e.g.
    // the forest-1 merchant between the four zones) isn't obscured by the
    // lines. Shop POIs come from the persisted welcome-time list — the live
    // mapConfig.buildings is FOW-filtered and omits unscouted shops.
    drawMinimapPOIs(
      ctx,
      this.state.mapConfig,
      bounds,
      this.state.neutralCampSnapshotsById,
      this.state.lootDropsById,
      this.state.neutralShopPOIs,
    )

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
