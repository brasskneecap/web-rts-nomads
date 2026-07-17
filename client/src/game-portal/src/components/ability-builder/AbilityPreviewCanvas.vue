<template>
  <div class="ab-preview-canvas" data-test="ability-preview-canvas">
    <div class="ab-preview-canvas__stage">
      <canvas ref="canvasEl" class="ab-preview-canvas__canvas" />
      <canvas ref="overlayCanvasEl" class="ab-preview-canvas__overlay" />
      <p v-if="!frames.length" class="ab-preview-canvas__empty" data-test="preview-canvas-empty">
        Run a preview to see how this ability executes.
      </p>
    </div>

    <div class="ab-preview-canvas__controls" data-test="preview-canvas-controls">
      <button
        type="button"
        class="ab-preview-canvas__btn"
        :disabled="!frames.length"
        data-test="preview-play-toggle"
        @click="togglePlaying"
      >{{ playing ? 'Pause' : 'Play' }}</button>

      <button
        type="button"
        class="ab-preview-canvas__btn"
        :disabled="!frames.length"
        data-test="preview-restart"
        @click="onRestart"
      >Restart</button>

      <input
        type="range"
        class="ab-preview-canvas__scrub"
        min="0"
        :max="Math.max(0, frames.length - 1)"
        :value="currentTick"
        :disabled="!frames.length"
        data-test="preview-scrub"
        @input="onScrub"
      >

      <span class="ab-preview-canvas__time" data-test="preview-time-readout">
        {{ (currentTick * PREVIEW_FRAME_DT_SECONDS).toFixed(2) }}s / {{ maxTimeSeconds.toFixed(2) }}s
      </span>

      <div class="ab-preview-canvas__speeds" role="group" aria-label="Playback speed">
        <button
          v-for="s in SPEED_OPTIONS"
          :key="s"
          type="button"
          class="ab-preview-canvas__btn ab-preview-canvas__btn--speed"
          :class="{ 'ab-preview-canvas__btn--active': speed === s }"
          :disabled="!frames.length"
          :data-test="`preview-speed-${s}`"
          @click="setSpeed(s)"
        >{{ s }}&times;</button>
      </div>
    </div>

    <div class="ab-preview-canvas__overlay-toggles" data-test="preview-overlay-toggles">
      <label class="ed-check">
        <input v-model="showCastRange" type="checkbox" :disabled="!frames.length">
        Cast range
      </label>
      <label class="ed-check">
        <input v-model="showAoe" type="checkbox" :disabled="!frames.length">
        AoE radius
      </label>
    </div>
  </div>
</template>

<script setup lang="ts">
// AbilityPreviewCanvas: replays a captured PreviewResult.frames sequence
// (Task 5's per-tick server snapshots) through the REAL game renderer —
// same pattern AbilityAnimationViewer.vue established: a standalone
// GameState + Camera fed into CanvasRenderer, driven by a requestAnimationFrame
// loop, jsdom-safe bail when there's no 2D context.
//
// Unlike AbilityAnimationViewer (which authors its own timeline client-side),
// this component's visuals are 100% server-authoritative: wall-clock time only
// selects WHICH captured frame index to display (see previewPlayback.ts's
// frameIndexAt) — every field on screen comes straight from that frame's
// snapshot.
//
// Snapshot -> GameState apply strategy: DIRECT-ASSIGN, not GameState.applySnapshot().
// applySnapshot() is the live network path — it's entangled with an
// interpolation ring buffer, wall-clock-keyed damage-popup synthesis (crit /
// minor / damage-type / hit pools), sound-trigger bookkeeping, and end-of-match
// roster freezing, all keyed off "now" and "the previous snapshot". Replaying
// a preview scrubs and rewinds through frames, which would replay/duplicate
// those side effects. The renderer itself reads state.units/projectiles/
// beams/effects directly (see CanvasRenderer's render()), so the live path's
// own final step for projectiles/beams/effects IS already a direct assignment
// (`this.projectiles = message.projectiles ?? []`, etc.) — only `units` needs
// a small field-mapping (UnitSnapshot.progressionPath -> Unit.path), shared
// with applySnapshot's own per-tick unit mapping via GameState's exported
// mapUnitSnapshot() so the two paths can never silently diverge.
//
// Playback controls (Task 7) follow the SAME controlled-with-fallback shape
// `currentTick` already established: `playing` and `speed` are optional
// props with defaults, mirrored into local refs (`playing`/`speed` below) so
// the component drives its own play/pause/speed when used standalone, while
// still emitting `update:playing`/`update:speed` so a parent CAN take over
// (v-model). The existing `lastEmittedTick` echo-guard pattern is reused
// verbatim for both (`lastEmittedPlaying`/`lastEmittedSpeed`).
//
// IDLE STATE (Task 5): the panel now mounts this component unconditionally,
// even before any preview has run, so `frames: []` is a real, always-first,
// load-bearing render — not just a transient gap between runs. Every code
// path below was already defensive against it (computeSceneBBox([]) falls
// back to FALLBACK_BBOX, frameIndexAt clamps frameCount<=0 to 0, applyFrame
// treats an out-of-range/undefined frame as "clear to idle", and every
// playback control already binds `:disabled="!frames.length"`), so mounting
// idle needed no new guards — only the parent template's `result?.frames`
// null-safety and this note that the path is now exercised on first paint.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { GameState, mapUnitSnapshot } from '@/game/core/GameState'
import { Camera } from '@/game/rendering/Camera'
import { CanvasRenderer } from '@/game/rendering/CanvasRenderer'
import type { PreviewFrame } from '@/game/abilities/program/programPreview'
import { FALLBACK_BBOX, PREVIEW_FRAME_DT_SECONDS, computeCameraFit, computeSceneBBox, frameIndexAt, type SceneBBox } from './previewPlayback'
import { overlayCircles } from './PreviewOverlays'

interface Props {
  frames: PreviewFrame[]
  /**
   * Controlled frame index — v-model'd by the parent (Task 7/8's transport
   * controls). REQUIRED and always controlled: unlike `playing`/`speed`
   * below (which fall back to a local default so this component works
   * standalone), the parent MUST v-model `currentTick` — there is no
   * standalone fallback for it.
   */
  currentTick: number
  /** Parent owns play/pause. Defaults to auto-play so this component is usable standalone. */
  playing?: boolean
  /** Playback speed multiplier. Defaults to real-time. */
  speed?: number
  /** World-space cast range radius, centered on the caster. Task 8 supplies this from the ability def. */
  castRange?: number
  /** World-space AoE radius, centered on the cast/impact point. Task 8 supplies this from the ability def. */
  aoeRadius?: number
  /** Caster world position — Task 8 supplies this from the preview request's caster coords. */
  casterX?: number
  casterY?: number
  /** Cast/impact world position — Task 8 supplies this from the preview request's cast coords. */
  castX?: number
  castY?: number
}

const props = withDefaults(defineProps<Props>(), {
  playing: true,
  speed: 1,
})

const emit = defineEmits<{
  'update:currentTick': [tick: number]
  'update:playing': [playing: boolean]
  'update:speed': [speed: number]
}>()

// ── camera framing ──────────────────────────────────────────────────────
// computeSceneBBox/FALLBACK_BBOX/SceneBBox/computeCameraFit all live in
// previewPlayback.ts (DOM-free, unit-testable) — this component only owns
// the zoom clamp range and wiring the fit into the Camera.
const MIN_ZOOM = 0.35
const MAX_ZOOM = 2.5

const sceneBBox = ref<SceneBBox>(FALLBACK_BBOX)

// refreshCamera sets the camera's zoom/pan DIRECTLY (not via Camera.centerOn,
// which clamps against GameState.mapWidth/mapHeight using overscan padding
// tuned for the live in-game HUD — see computeCameraFit's doc comment for
// why that fights a small preview scene). CanvasRenderer's own resize
// handler still calls camera.clamp() internally on ResizeObserver/
// window-resize events, but since this runs every rendered frame (see
// tick() below) it simply overwrites any such side effect before the next
// render() call — no visible glitch results.
function refreshCamera(canvas: HTMLCanvasElement, cam: Camera, bbox: SceneBBox, mapWidth: number, mapHeight: number) {
  if (canvas.width <= 0 || canvas.height <= 0) return
  const fit = computeCameraFit(bbox, canvas.width, canvas.height, mapWidth, mapHeight, MIN_ZOOM, MAX_ZOOM)
  cam.zoom = fit.zoom
  cam.x = fit.x
  cam.y = fit.y
}

// ── snapshot -> GameState application ───────────────────────────────────
const canvasEl = ref<HTMLCanvasElement | null>(null)
const overlayCanvasEl = ref<HTMLCanvasElement | null>(null)

let state: GameState | null = null
let camera: Camera | null = null
let renderer: CanvasRenderer | null = null
let overlayCtx: CanvasRenderingContext2D | null = null
let raf = 0

// lastAppliedIndex guards applyFrame's per-unit remap against redundant work:
// frames advance at 20fps (PREVIEW_FRAME_DT_SECONDS) but tick() runs at
// display refresh rate (~60fps) and also re-runs continuously while paused,
// so most RAF ticks resolve to the SAME frame index. Reset to -1 whenever
// `frames` changes so the (possibly identical) index re-applies against the
// new scene data.
let lastAppliedIndex = -1

// applyFrame writes frames[i]'s snapshot onto the standalone GameState. Out-
// of-range/empty input clears the scene to idle rather than throwing.
function applyFrame(i: number) {
  if (!state) return
  const frame = props.frames[i]
  if (!frame) {
    state.units = []
    state.projectiles = []
    state.beams = []
    state.effects = []
    return
  }
  const snap = frame.snapshot
  state.units = (snap.units ?? []).map(mapUnitSnapshot)
  state.projectiles = snap.projectiles ?? []
  state.beams = snap.beams ?? []
  state.effects = snap.effects ?? []
}

// ── overlay rings (cast range / AoE) ────────────────────────────────────
// Pure screen-space geometry comes from PreviewOverlays.ts (unit-tested);
// this is just the canvas paint step, on a SEPARATE transparent canvas
// layered over the renderer's own so CanvasRenderer never needs to know
// about preview-only chrome. Toggles default ON.
const showCastRange = ref(true)
const showAoe = ref(true)

function drawOverlays(cam: Camera, canvas: HTMLCanvasElement) {
  const octx = overlayCtx
  const ocanvas = overlayCanvasEl.value
  if (!octx || !ocanvas) return
  if (ocanvas.width !== canvas.width) ocanvas.width = canvas.width
  if (ocanvas.height !== canvas.height) ocanvas.height = canvas.height
  octx.clearRect(0, 0, ocanvas.width, ocanvas.height)

  // Idle (no preview run yet): there's no cast/impact scene to ring, and
  // casterX/castX etc. still hold stale values from the LAST run (or their
  // 0-fallback), so drawing rings here would paint a cast-range/AoE circle
  // over an otherwise-empty stage — the exact "looks broken before any run"
  // regression this guards against. Only the idle placeholder text shows.
  if (props.frames.length === 0) return

  const { castRange, aoe } = overlayCircles(
    {
      castRange: props.castRange,
      aoeRadius: props.aoeRadius,
      casterX: props.casterX ?? 0,
      casterY: props.casterY ?? 0,
      castX: props.castX ?? 0,
      castY: props.castY ?? 0,
      showCastRange: showCastRange.value,
      showAoe: showAoe.value,
    },
    { x: cam.x, y: cam.y, zoom: cam.zoom },
    ocanvas.width,
    ocanvas.height,
  )

  if (castRange) {
    octx.beginPath()
    octx.arc(castRange.cx, castRange.cy, castRange.radius, 0, Math.PI * 2)
    octx.setLineDash([6, 5])
    octx.lineWidth = 2
    octx.strokeStyle = 'rgba(231, 200, 138, 0.8)'
    octx.stroke()
    octx.setLineDash([])
  }

  if (aoe) {
    octx.beginPath()
    octx.arc(aoe.cx, aoe.cy, aoe.radius, 0, Math.PI * 2)
    octx.fillStyle = 'rgba(224, 178, 88, 0.18)'
    octx.fill()
    octx.lineWidth = 2
    octx.strokeStyle = 'rgba(224, 178, 88, 0.75)'
    octx.stroke()
  }
}

// ── playback clock ──────────────────────────────────────────────────────
// seekBase/startedAtMs anchor frameIndexAt(): seekBase is the frame index
// playback last (re)started from, startedAtMs is the wall-clock moment it
// did so. Both are rebased whenever `playing` toggles (freezing at the
// current position on pause, or resuming from it on play) and whenever an
// EXTERNAL currentTick change arrives (a scrub from the parent's controls) —
// but NOT when the change is this component's own emitted update, which
// `lastEmittedTick` guards against so playback doesn't re-anchor itself
// every frame it's driving.
const seekBase = ref(props.currentTick)
const startedAtMs = ref(performance.now())
let lastEmittedTick: number | null = null

function emitTick(tick: number) {
  if (tick === props.currentTick) return
  lastEmittedTick = tick
  emit('update:currentTick', tick)
}

watch(
  () => props.currentTick,
  (val) => {
    if (lastEmittedTick !== null && val === lastEmittedTick) {
      lastEmittedTick = null
      return
    }
    // External scrub/seek: resume (or stay paused) from exactly this tick.
    seekBase.value = val
    startedAtMs.value = performance.now()
  },
)

// playing: controlled-with-fallback (see module doc comment above). Local
// ref is the single source of truth tick()/the freeze-watch below read;
// props.playing only matters at mount (seed) and for a PARENT-driven change
// (guarded against echoing our own emitted update, same shape as currentTick).
const playing = ref(props.playing)
let lastEmittedPlaying: boolean | null = null

function setPlaying(next: boolean) {
  if (next === playing.value) return
  playing.value = next
  lastEmittedPlaying = next
  emit('update:playing', next)
}

watch(
  () => props.playing,
  (val) => {
    if (lastEmittedPlaying !== null && val === lastEmittedPlaying) {
      lastEmittedPlaying = null
      return
    }
    playing.value = val
  },
)

watch(playing, (isPlaying) => {
  // Freeze the anchor at the current displayed position, then (if
  // resuming) start the wall clock from now. Scrub/restart handlers below
  // set seekBase to their OWN target tick and call emitTick beforehand, so
  // by the time this (microtask-flushed) watcher runs, props.currentTick
  // already matches — this reassignment is then a no-op for those paths and
  // the intended "freeze in place" behavior for a plain play/pause toggle.
  seekBase.value = props.currentTick
  if (isPlaying) startedAtMs.value = performance.now()
})

// speed: same controlled-with-fallback shape as `playing`.
const SPEED_OPTIONS = [0.5, 1, 2] as const
const speed = ref(props.speed)
let lastEmittedSpeed: number | null = null

function setSpeed(next: number) {
  if (next === speed.value) return
  speed.value = next
  lastEmittedSpeed = next
  emit('update:speed', next)
}

watch(
  () => props.speed,
  (val) => {
    if (lastEmittedSpeed !== null && val === lastEmittedSpeed) {
      lastEmittedSpeed = null
      return
    }
    speed.value = val
  },
)

function togglePlaying() {
  setPlaying(!playing.value)
}

function onScrub(e: Event) {
  const value = Number((e.target as HTMLInputElement).value)
  setPlaying(false)
  seekBase.value = value
  startedAtMs.value = performance.now()
  emitTick(value)
}

function onRestart() {
  seekBase.value = 0
  startedAtMs.value = performance.now()
  emitTick(0)
  setPlaying(true)
}

const maxTimeSeconds = computed(() => Math.max(0, props.frames.length - 1) * PREVIEW_FRAME_DT_SECONDS)

// A new preview run replaces `frames` wholesale — snap back to frame 0 and
// recompute the camera framing for the new scene.
watch(
  () => props.frames,
  (frames) => {
    sceneBBox.value = computeSceneBBox(frames)
    seekBase.value = 0
    startedAtMs.value = performance.now()
    // New scene data for the same index (0) — force applyFrame's re-apply
    // guard in tick() to run again instead of treating 0 as already-applied.
    lastAppliedIndex = -1
    applyFrame(0)
    lastAppliedIndex = 0
    emitTick(0)
  },
)

function tick() {
  raf = requestAnimationFrame(tick)
  const canvas = canvasEl.value
  if (!state || !camera || !renderer || !canvas) return
  // Local aliases: narrowing on the module-scoped `let`s above doesn't
  // survive the frameIndexAt()/applyFrame() calls below (TS drops it for any
  // function call that could reassign a captured outer variable).
  const st = state
  const cam = camera
  const rend = renderer

  const index = frameIndexAt({
    playing: playing.value,
    startedAtMs: startedAtMs.value,
    nowMs: performance.now(),
    speed: speed.value,
    frameCount: props.frames.length,
    seekTick: seekBase.value,
  })

  // Frames advance at 20fps while RAF runs at display refresh (and this loop
  // also spins continuously while paused) — most ticks resolve to the same
  // index, so only re-map units when it actually changes. refreshCamera
  // still runs every tick regardless: it must keep counter-acting the
  // ResizeObserver's camera.clamp() side effect (see refreshCamera's comment
  // above) even on frames where the displayed index doesn't move.
  if (index !== lastAppliedIndex) {
    applyFrame(index)
    lastAppliedIndex = index
  }
  refreshCamera(canvas, cam, sceneBBox.value, st.mapWidth, st.mapHeight)
  rend.render()
  drawOverlays(cam, canvas)

  if (index !== props.currentTick) emitTick(index)
}

onMounted(() => {
  const canvas = canvasEl.value
  if (!canvas) return
  // jsdom (unit tests) has no real 2D context — CanvasRenderer's constructor
  // throws without one. Stay inert: no renderer, no RAF loop.
  if (!canvas.getContext('2d')) return

  const st = new GameState()
  // Collapse the renderer's built-in minimap HUD — irrelevant clutter for a
  // focused preview replay (same as AbilityAnimationViewer).
  st.minimapPanelRect = { x: 0, y: 0, width: 1, height: 1 }

  const cam = new Camera()
  const rend = new CanvasRenderer(canvas, st, cam)

  state = st
  camera = cam
  renderer = rend
  overlayCtx = overlayCanvasEl.value?.getContext('2d') ?? null

  sceneBBox.value = computeSceneBBox(props.frames)
  applyFrame(props.currentTick)
  lastAppliedIndex = props.currentTick
  refreshCamera(canvas, cam, sceneBBox.value, st.mapWidth, st.mapHeight)
  rend.render()
  drawOverlays(cam, canvas)

  raf = requestAnimationFrame(tick)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
  renderer?.destroy()
})
</script>

<style scoped>
.ab-preview-canvas {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ab-preview-canvas__stage {
  position: relative;
  width: 100%;
  height: 320px;
}

.ab-preview-canvas__canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
  background: rgba(8, 14, 24, 0.55);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  image-rendering: pixelated;
}

.ab-preview-canvas__overlay {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.ab-preview-canvas__empty {
  position: absolute;
  inset: 0;
  margin: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 0 20px;
  color: var(--ed-text-dim);
  font-family: var(--font-body);
  font-size: 0.86rem;
  font-style: italic;
  pointer-events: none;
}

.ab-preview-canvas__controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.ab-preview-canvas__btn {
  padding: 5px 10px;
  font-family: var(--font-body);
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--ed-text);
  background: rgba(15, 23, 42, 0.35);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.ab-preview-canvas__btn:hover:not(:disabled) {
  border-color: var(--ed-line-strong);
}

.ab-preview-canvas__btn:disabled {
  opacity: 0.4;
}

.ab-preview-canvas__btn--speed {
  min-width: 34px;
}

.ab-preview-canvas__btn--active {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
  background: rgba(212, 168, 71, 0.14);
}

.ab-preview-canvas__scrub {
  flex: 1 1 160px;
  min-width: 120px;
}

.ab-preview-canvas__time {
  font-family: var(--font-body);
  font-size: 0.76rem;
  font-variant-numeric: tabular-nums;
  color: var(--ed-text-dim);
  white-space: nowrap;
}

.ab-preview-canvas__speeds {
  display: flex;
  gap: 4px;
}

.ab-preview-canvas__overlay-toggles {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}
</style>
