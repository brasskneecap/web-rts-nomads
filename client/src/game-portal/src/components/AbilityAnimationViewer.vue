<template>
  <div class="ability-anim-viewer">
    <canvas ref="canvasEl" class="ability-anim-viewer__canvas" />
    <div class="ability-anim-viewer__controls">
      <button
        type="button"
        class="ability-anim-viewer__cast"
        :disabled="casting"
        @click="play"
      >
        {{ casting ? 'Casting…' : 'Cast' }}
      </button>
      <label class="ability-anim-viewer__loop">
        <input v-model="loop" type="checkbox" />
        Loop
      </label>
    </div>
  </div>
</template>

<script setup lang="ts">
// In-editor preview: plays a single ability cast (adept -> raider) on a small
// synthetic scene. Reuses the REAL game renderer (CanvasRenderer) fed a
// standalone GameState — there is no server/network involved, everything
// here is authored client-side and never touches match/simulation state.
//
// Only a subset of AuthoredAbilityDef is consumed:
//   projectile, effectOnTarget, effectAtPoint, burnEffectAtPoint,
//   effectScale, projectileScale, casterAnimation-adjacent fields
//   (castTime, impactDelaySeconds, burnDurationSeconds).
// Fields with no client-visual hook today (healAmount, damageAmount,
// casterAnimation itself, targetsPoint) don't change the timeline directly —
// see the component's summary notes for why casterAnimation can't be wired.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { GameState, type Unit } from '@/game/core/GameState'
import { Camera } from '@/game/rendering/Camera'
import { CanvasRenderer } from '@/game/rendering/CanvasRenderer'
import { ENEMY_PLAYER_ID, type BeamSnapshot, type EffectSnapshot, type ProjectileSnapshot } from '@/game/network/protocol'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'

const props = defineProps<{
  /** The ability being previewed. */
  def: AuthoredAbilityDef
}>()

// --- scene layout constants -------------------------------------------------
const ADEPT_ID = 9001
const RAIDER_ID = 9002
const UNIT_SEPARATION = 300 // world px between the two units
// One-time nudge (world px) used only during the initial facing-priming
// renders — see the comment in onMounted. Never visible on screen.
const PRIME_OFFSET = 3
// World-space viewport the camera frames — wide/tall enough to hold both
// units, the space between them, and effect sprites blooming off the raider.
const VIEW_WORLD_WIDTH = 720
const VIEW_WORLD_HEIGHT = 440
const MIN_ZOOM = 0.35
const MAX_ZOOM = 2.5

// --- timeline constants ------------------------------------------------------
const CAST_TIME_DEFAULT = 0.6
const PROJECTILE_FLIGHT_SECONDS = 0.35
// A point-target traveling vortex (Arcane Orb) flies straight past the target
// rather than homing into it. It flies for longer and continues this far (world
// px) beyond the raider so it visibly passes through and exits the frame.
const FLYPAST_FLIGHT_SECONDS = 0.9
const FLYPAST_EXTRA_WORLD = 220
// A chaining ability (chain_lightning) flashes a momentary lightning beam for
// this long after the cast, then the scene resets.
const BEAM_FLASH_SECONDS = 0.35
const EFFECT_LIFETIME_SECONDS = 1.0
const BURN_LIFETIME_CAP_SECONDS = 3
const POST_EFFECT_HOLD_SECONDS = 0.2
const LOOP_GAP_MS = 2500

const ON_TARGET_EFFECT_ID = 1
const AT_POINT_EFFECT_ID = 2
const BURN_EFFECT_ID = 3

const canvasEl = ref<HTMLCanvasElement | null>(null)
const casting = ref(false)
const loop = ref(false)

let state: GameState | null = null
let camera: Camera | null = null
let renderer: CanvasRenderer | null = null
let raf = 0
let loopTimer: number | undefined

type CastTimeline = {
  startedAtMs: number
  /** Cast finishes; projectile launch / instant impact begins. */
  castEnd: number
  /** Impact moment — effects spawn here (== castEnd when no projectile). */
  impactAt: number
  /** Everything (effects + burn) has finished; scene resets to idle. */
  totalEnd: number
  hasProjectile: boolean
  /** Point-target traveling projectile (Arcane Orb): flies straight PAST the
   *  target instead of homing into it, and has no on-target impact effect. */
  flypast: boolean
  /** Chaining ability (chain_lightning): rendered as a momentary lightning
   *  BEAM rather than a projectile. */
  beam: boolean
  beamVariant: string
  projectileVariant: string
  effectOnTarget?: string
  effectAtPoint?: string
  burnEffectAtPoint?: string
  effectScale?: number
  projectileScale?: number
  burnLifetime: number
}
let timeline: CastTimeline | null = null

function clamp01(n: number): number {
  return n < 0 ? 0 : n > 1 ? 1 : n
}

function clampRange(v: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, v))
}

function buildUnit(
  id: number,
  unitType: 'adept' | 'raider',
  name: string,
  x: number,
  y: number,
  ownerId: string,
): Unit {
  return {
    id,
    unitType,
    name,
    capabilities: [],
    visible: true,
    x,
    y,
    hp: 100,
    maxHp: 100,
    ownerId,
    moving: false,
  }
}

// Builds the fixed timeline breakpoints for one cast, read fresh from the
// CURRENT def each time play() is called — so editing the ability between
// casts always previews the latest authored values.
function buildTimeline(def: AuthoredAbilityDef, nowMs: number): CastTimeline {
  const castEnd = def.castTime && def.castTime > 0 ? def.castTime : CAST_TIME_DEFAULT
  const hasProjectileField = !!def.projectile
  // A chaining ability (chain_lightning) is delivered in-game as a bouncing
  // BEAM, not a flying projectile — its `projectile` id ("lightning_bolt") is
  // actually a beam variant (assets/beams/<variant>/). Render it as a beam.
  const beam = hasProjectileField && (def.chainCount ?? 0) > 0
  const hasProjectile = hasProjectileField && !beam
  // A point-target ability that also launches a projectile (Arcane Orb) is a
  // traveling vortex: it flies straight in the target's direction and passes
  // THROUGH/beyond it, rather than homing in and detonating. Any other
  // projectile is a homing bolt that impacts the target.
  const flypast = hasProjectile && !!def.targetsPoint
  const travel = !hasProjectile
    ? 0 // no projectile ⇒ skip straight to impact (instant / point spell)
    : flypast
      ? FLYPAST_FLIGHT_SECONDS
      : clampRange(
          def.impactDelaySeconds && def.impactDelaySeconds > 0
            ? def.impactDelaySeconds
            : PROJECTILE_FLIGHT_SECONDS,
          0.15,
          1.2,
        )
  const impactAt = castEnd + travel

  const burnLifetime = def.burnEffectAtPoint
    ? clampRange(def.burnDurationSeconds ?? EFFECT_LIFETIME_SECONDS, EFFECT_LIFETIME_SECONDS, BURN_LIFETIME_CAP_SECONDS)
    : 0
  const hasAnyEffect = !!(def.effectOnTarget || def.effectAtPoint || def.burnEffectAtPoint)
  const effectHold = hasAnyEffect ? Math.max(EFFECT_LIFETIME_SECONDS, burnLifetime) : 0.3
  const totalEnd = impactAt + effectHold + POST_EFFECT_HOLD_SECONDS

  return {
    startedAtMs: nowMs,
    castEnd,
    impactAt,
    totalEnd,
    hasProjectile,
    flypast,
    beam,
    beamVariant: beam ? (def.projectile ?? '') : '',
    projectileVariant: def.projectile ?? '',
    effectOnTarget: def.effectOnTarget || undefined,
    effectAtPoint: def.effectAtPoint || undefined,
    burnEffectAtPoint: def.burnEffectAtPoint || undefined,
    effectScale: def.effectScale,
    projectileScale: def.projectileScale,
    burnLifetime: burnLifetime || EFFECT_LIFETIME_SECONDS,
  }
}

function play() {
  if (!state || casting.value) return
  if (loopTimer !== undefined) {
    clearTimeout(loopTimer)
    loopTimer = undefined
  }
  casting.value = true
  timeline = buildTimeline(props.def, performance.now())
}

function finishCast() {
  timeline = null
  casting.value = false
  if (state) {
    state.projectiles = []
    state.effects = []
    state.beams = []
  }
  if (loop.value) {
    loopTimer = window.setTimeout(() => {
      loopTimer = undefined
      play()
    }, LOOP_GAP_MS)
  }
}

// Cancels any in-flight cast / pending loop replay and snaps the scene back
// to idle. Called when the previewed ability changes out from under an
// active animation, and on unmount.
function stopCast() {
  if (loopTimer !== undefined) {
    clearTimeout(loopTimer)
    loopTimer = undefined
  }
  timeline = null
  casting.value = false
  if (state) {
    const adept = state.units.find((u) => u.id === ADEPT_ID)
    if (adept) adept.status = undefined
    state.projectiles = []
    state.effects = []
    state.beams = []
  }
}

function applyTimeline(nowMs: number) {
  if (!state) return
  const adept = state.units.find((u) => u.id === ADEPT_ID)
  const raider = state.units.find((u) => u.id === RAIDER_ID)
  if (!adept || !raider) return

  if (!timeline) {
    if (adept.status !== undefined) adept.status = undefined
    if (state.projectiles.length) state.projectiles = []
    if (state.effects.length) state.effects = []
    if (state.beams.length) state.beams = []
    return
  }

  const elapsed = (nowMs - timeline.startedAtMs) / 1000
  adept.status = elapsed < timeline.castEnd ? 'Casting' : undefined

  if (timeline.hasProjectile && elapsed >= timeline.castEnd && elapsed < timeline.impactAt) {
    const travelT = clamp01(
      (elapsed - timeline.castEnd) / Math.max(0.001, timeline.impactAt - timeline.castEnd),
    )
    // A homing bolt stops at the raider; a fly-past vortex continues straight
    // beyond it, so aim the endpoint past the raider along the adept→raider line.
    let targetX = raider.x
    let targetY = raider.y
    if (timeline.flypast) {
      const dx = raider.x - adept.x
      const dy = raider.y - adept.y
      const dist = Math.hypot(dx, dy) || 1
      targetX = raider.x + (dx / dist) * FLYPAST_EXTRA_WORLD
      targetY = raider.y + (dy / dist) * FLYPAST_EXTRA_WORLD
    }
    const snapshot: ProjectileSnapshot = {
      id: 'preview-cast',
      ownerUnitId: adept.id,
      ownerId: adept.ownerId ?? '',
      targetUnitId: raider.id,
      originX: adept.x,
      originY: adept.y,
      targetX,
      targetY,
      progress: travelT,
      variant: timeline.projectileVariant,
      // Render scale is ability-owned (the caster's projectileScale is only for
      // its basic attack) — matches the server's ability projectile spawn.
      // 0/undefined ⇒ the client's default 1×.
      scale: timeline.projectileScale,
    }
    state.projectiles = [snapshot]
  } else if (state.projectiles.length) {
    state.projectiles = []
  }

  // Chaining ability: flash a momentary lightning beam adept→raider (matches
  // the in-game beam-bounce visual) instead of a projectile.
  if (timeline.beam && elapsed >= timeline.castEnd && elapsed < timeline.castEnd + BEAM_FLASH_SECONDS) {
    const beamSnap: BeamSnapshot = {
      id: 'preview-beam',
      casterUnitId: adept.id,
      targetUnitId: raider.id,
      ownerId: adept.ownerId ?? '',
      variant: timeline.beamVariant,
      momentary: true,
      originX: adept.x,
      originY: adept.y,
      targetX: raider.x,
      targetY: raider.y,
    }
    state.beams = [beamSnap]
  } else if (state.beams.length) {
    state.beams = []
  }

  const nextEffects: EffectSnapshot[] = []
  // A fly-past vortex (Arcane Orb) has no on-target impact effect — the
  // traveling projectile itself is the spell's visual, so skip the effect layer.
  if (!timeline.flypast && elapsed >= timeline.impactAt) {
    const sinceImpact = elapsed - timeline.impactAt
    if (timeline.effectOnTarget && sinceImpact < EFFECT_LIFETIME_SECONDS) {
      nextEffects.push({
        id: ON_TARGET_EFFECT_ID,
        name: timeline.effectOnTarget,
        anchorUnitId: raider.id,
        x: raider.x,
        y: raider.y,
        progress: clamp01(sinceImpact / EFFECT_LIFETIME_SECONDS),
        sizeScale: timeline.effectScale,
      })
    }
    if (timeline.effectAtPoint && sinceImpact < EFFECT_LIFETIME_SECONDS) {
      nextEffects.push({
        id: AT_POINT_EFFECT_ID,
        name: timeline.effectAtPoint,
        x: raider.x,
        y: raider.y,
        progress: clamp01(sinceImpact / EFFECT_LIFETIME_SECONDS),
        sizeScale: timeline.effectScale,
      })
    }
    if (timeline.burnEffectAtPoint && sinceImpact < timeline.burnLifetime) {
      nextEffects.push({
        id: BURN_EFFECT_ID,
        name: timeline.burnEffectAtPoint,
        x: raider.x,
        y: raider.y,
        progress: clamp01(sinceImpact / timeline.burnLifetime),
        // Sized with effectScale so the crater lines up with the meteor's own
        // impact frames (matches the server's burn VFX sizing).
        sizeScale: timeline.effectScale,
      })
    }
  }
  state.effects = nextEffects

  if (elapsed >= timeline.totalEnd) {
    finishCast()
  }
}

// Keeps the camera framed on the two-unit formation every frame — cheap
// enough to recompute continuously, and keeps the scene correctly zoomed if
// the host panel resizes.
function refreshCamera(canvas: HTMLCanvasElement, cam: Camera, st: GameState) {
  if (canvas.width <= 0 || canvas.height <= 0) return
  const zoomX = canvas.width / VIEW_WORLD_WIDTH
  const zoomY = canvas.height / VIEW_WORLD_HEIGHT
  cam.zoom = clampRange(Math.min(zoomX, zoomY), MIN_ZOOM, MAX_ZOOM)
  cam.centerOn(st.mapWidth / 2, st.mapHeight / 2, canvas.width, canvas.height, st.mapWidth, st.mapHeight)
}

function tick() {
  raf = requestAnimationFrame(tick)
  const canvas = canvasEl.value
  if (!state || !camera || !renderer || !canvas) return
  applyTimeline(performance.now())
  refreshCamera(canvas, camera, state)
  renderer.render()
}

onMounted(() => {
  const canvas = canvasEl.value
  if (!canvas) return
  // Bail if the environment can't provide a 2D context (e.g. jsdom under unit
  // tests): CanvasRenderer's constructor throws without one. The component
  // then stays inert — no renderer, no RAF loop — and Cast is a no-op.
  if (!canvas.getContext('2d')) return

  const st = new GameState()
  // Collapses the renderer's built-in minimap HUD to a 1x1 no-op rect. With
  // no panel rect supplied it falls back to painting a full-map thumbnail in
  // the corner (see CanvasRenderer.drawMinimap) — irrelevant clutter for a
  // focused single-cast preview.
  st.minimapPanelRect = { x: 0, y: 0, width: 1, height: 1 }

  const centerX = st.mapWidth / 2
  const centerY = st.mapHeight / 2
  const adeptX = centerX - UNIT_SEPARATION / 2
  const raiderX = centerX + UNIT_SEPARATION / 2

  const adept = buildUnit(ADEPT_ID, 'adept', 'Adept', adeptX - PRIME_OFFSET, centerY, 'preview-caster')
  const raider = buildUnit(RAIDER_ID, 'raider', 'Raider', raiderX + PRIME_OFFSET, centerY, ENEMY_PLAYER_ID)
  st.units = [adept, raider]

  const cam = new Camera()
  const rend = new CanvasRenderer(canvas, st, cam)

  state = st
  camera = cam
  renderer = rend

  // Prime facing. The renderer's per-unit animation controller only derives
  // "sticky" facing from movement observed BETWEEN successive render() calls
  // (see unitAnimation.ts's sample()) — actionFacingDx/Dy is only consulted
  // while status === 'Attacking', never while 'Casting' or idle. Two
  // back-to-back render() calls (offset position -> resting position)
  // register as a one-frame "step" toward each other, which locks the sticky
  // facing to "toward each other" for the rest of the scene's life (idle AND
  // casting) — and since both calls land synchronously before the browser
  // paints, the priming step is never actually visible on screen.
  refreshCamera(canvas, cam, st)
  rend.render()
  adept.x = adeptX
  raider.x = raiderX
  refreshCamera(canvas, cam, st)
  rend.render()

  raf = requestAnimationFrame(tick)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
  if (loopTimer !== undefined) clearTimeout(loopTimer)
  renderer?.destroy()
})

watch(loop, (enabled) => {
  if (enabled) {
    if (!casting.value) play()
  } else if (loopTimer !== undefined) {
    clearTimeout(loopTimer)
    loopTimer = undefined
  }
})

// Any change to the fields the timeline actually consumes — whether from
// swapping to a different ability or live-editing the current one in the
// panel — snaps the preview back to idle so it never plays a stale mix of
// old/new projectile-effect visuals.
watch(
  () => [
    props.def?.id,
    props.def?.projectile,
    props.def?.effectOnTarget,
    props.def?.effectAtPoint,
    props.def?.burnEffectAtPoint,
    props.def?.effectScale,
    props.def?.projectileScale,
    props.def?.chainCount,
    props.def?.castTime,
    props.def?.impactDelaySeconds,
    props.def?.burnDurationSeconds,
  ],
  () => stopCast(),
)
</script>

<style scoped>
.ability-anim-viewer {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.ability-anim-viewer__canvas {
  width: 100%;
  height: 320px;
  display: block;
  background: rgba(8, 14, 24, 0.55);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 12px;
  image-rendering: pixelated;
}

.ability-anim-viewer__controls {
  display: flex;
  align-items: center;
  gap: 16px;
}

.ability-anim-viewer__cast {
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 10px;
  background: rgba(215, 187, 132, 0.16);
  color: #f8fafc;
  padding: 8px 18px;
  font-size: 0.82rem;
  font-weight: 700;
}

.ability-anim-viewer__cast:hover:not(:disabled) {
  border-color: rgba(215, 187, 132, 0.85);
  background: rgba(215, 187, 132, 0.26);
}

.ability-anim-viewer__cast:disabled {
  opacity: 0.55;
}

.ability-anim-viewer__loop {
  display: flex;
  align-items: center;
  gap: 6px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.8rem;
}
</style>
