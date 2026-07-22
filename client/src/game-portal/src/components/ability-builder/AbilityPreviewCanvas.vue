<template>
  <div class="ab-preview-canvas" data-test="ability-preview-canvas">
    <div class="ab-preview-canvas__stage">
      <canvas ref="canvasEl" class="ab-preview-canvas__canvas" />
      <canvas ref="overlayCanvasEl" class="ab-preview-canvas__overlay" />
      <!-- Drag-to-place layer (Phase 6b): the only pointer-interactive
           element in the stage, and only while there's no result to replay
           yet (frames: []). Mounted/unmounted with edit mode itself so a
           replay never has a stray hit-testable layer sitting over it. -->
      <div
        v-if="!frames.length"
        class="ab-preview-canvas__drag-layer"
        data-test="preview-drag-layer"
        @pointerdown="onDragPointerDown"
        @pointermove="onDragPointerMove"
        @pointerup="onDragPointerUp"
        @pointercancel="onDragPointerUp"
      />
      <p v-if="!frames.length && !sceneUnits.length" class="ab-preview-canvas__empty" data-test="preview-canvas-empty">
        Run a preview to see how this ability executes.
      </p>
    </div>

    <div class="ab-preview-canvas__controls" data-test="preview-canvas-controls">
      <!-- Leading slot: the panel injects Run / Edit here so they sit in the
           same toolbar as the playback controls, same icon-button format. -->
      <slot name="controls-lead" />

      <PreviewControlButton
        :icon="playing && frames.length ? 'pause' : 'play'"
        :label="playing && frames.length ? 'Pause' : 'Play'"
        :disabled="!frames.length"
        data-test="preview-play-toggle"
        @click="togglePlaying"
      />

      <PreviewControlButton
        icon="restart"
        label="Restart"
        :disabled="!frames.length"
        data-test="preview-restart"
        @click="onRestart"
      />

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
// Render clock (N3): CanvasRenderer's frame-cycling cosmetics (unit sprite
// idle/walk/attack cycling, looping effect/beam frames, floating-number
// fade) all free-run on a wall clock by default — correct for a LIVE match,
// wrong here: DIRECT-ASSIGN freezes state.units/projectiles/beams/effects
// exactly at the displayed frame's snapshot while paused/scrubbed, but those
// cosmetics would keep aging on real elapsed time regardless (the unit
// visibly keeps mid-stride-cycling, a damage number keeps fading out, after
// hitting pause). We inject a DETERMINISTIC clock instead — see
// `previewClock` below — derived from the frame index actually on screen,
// via `previewClockMs` (previewPlayback.ts), so pausing genuinely freezes
// everything and scrubbing is idempotent (frame N looks the same every time
// it's displayed). The SEPARATE playback clock above (`seekBase`/
// `startedAtMs`, driven by real performance.now()) still decides WHICH frame
// index to show — that part is deliberately still wall-clock-real-time so
// speed/play/pause read naturally; only the RENDERER's own cosmetics clock
// is swapped for a frame-index-derived one.
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
import type { UnitSnapshot } from '@/game/network/protocol'
import type { PreviewFrame, PreviewSceneUnit } from '@/game/abilities/program/programPreview'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import {
  FALLBACK_BBOX,
  PREVIEW_FRAME_DT_SECONDS,
  computeCameraFit,
  computeSceneBBox,
  computeSceneBBoxFromPoints,
  frameIndexAt,
  previewClockMs,
  type SceneBBox,
} from './previewPlayback'
import { damageNumbersForFrameIndex } from './previewDamageNumbers'
import { overlayCircles, screenToWorld } from './PreviewOverlays'
import { getUnitSpriteSet, isPointInUnitBody, UNIT_SPRITE_SCALE } from '@/game/rendering/unitSprites'
import { getUnitBoundsFor } from '@/game/maps/unitDefs'
import PreviewControlButton from './PreviewControlButton.vue'

interface Props {
  frames: PreviewFrame[]
  /**
   * The preview run's full execution trace (Task 9: floating damage/heal
   * numbers). Optional/defaults to [] so every existing caller (and every
   * test in AbilityPreviewCanvas.test.ts) that doesn't pass it keeps working
   * — it simply means no numbers spawn, same as before this prop existed.
   */
  trace?: AbilityExecutionTraceEvent[]
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
  /**
   * The live, user-draggable scene units (Phase 6b) — ally/enemy placements
   * the panel owns, edited by dragging on this canvas BEFORE any run.
   * Rendered as a synthetic scene whenever `frames` is empty (see the "edit
   * mode" section below); ignored once real frames exist (a replay is 100%
   * server-authoritative, same as always). Defaults to `[]` so every
   * existing caller/test that predates drag placement keeps working
   * unchanged — idle state stays idle with no scene units.
   */
  sceneUnits?: PreviewSceneUnit[]
}

const props = withDefaults(defineProps<Props>(), {
  playing: true,
  speed: 1,
  trace: () => [],
  sceneUnits: () => [],
})

const emit = defineEmits<{
  'update:currentTick': [tick: number]
  'update:playing': [playing: boolean]
  'update:speed': [speed: number]
  /** Phase 6b: a drag moved scene unit `index` (into the `sceneUnits` prop) to a new world position. */
  'update:scene-unit': [payload: { index: number; x: number; y: number }]
  /** Phase 6b: a drag moved the caster to a new world position. */
  'update:caster': [payload: { x: number; y: number }]
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

// previewFrameIndex is the frame index CURRENTLY on screen — updated right
// before every render()/spawnDamageNumbersForIndex call so both read the
// exact same value within one tick. `previewClock` (passed to CanvasRenderer
// as its injected timeSource) is a closure over this `let`, so it always
// reports previewClockMs(<whatever frame is displayed right now>), never a
// stale value from a previous tick. See the module doc comment's "Render
// clock (N3)" section above for why this exists.
let previewFrameIndex = 0
const previewClock = () => previewClockMs(previewFrameIndex)

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
    state.corpses = []
    state.projectiles = []
    state.beams = []
    state.effects = []
    state.traps = []
    return
  }
  const snap = frame.snapshot
  state.units = (snap.units ?? []).map(mapUnitSnapshot)
  // corpses: the preview must show what a match shows. A previewed kill leaves
  // a body in the captured frame exactly as it does live (the frames ARE
  // snapshotUnfilteredLocked output), so without this an ability that kills
  // something previews as the target simply blinking out of existence — which
  // is no longer what happens in a real match. Same reasoning, and the same
  // one-line fix, as traps below.
  state.corpses = snap.corpses ?? []
  state.projectiles = snap.projectiles ?? []
  state.beams = snap.beams ?? []
  state.effects = snap.effects ?? []
  // traps: same direct-assign treatment as projectiles/beams/effects (the live
  // path's own final step is likewise `this.traps = message.traps ?? []`, see
  // GameState.applySnapshot). Without this a place_trap ability (the Trapper's
  // caltrops / fire_pit / explosive_trap / marker_trap) previews as a cast that
  // visibly does nothing — the trap IS in every captured frame's snapshot, it
  // just never reached the renderer.
  state.traps = snap.traps ?? []
}

// ── edit-mode synthetic scene (Phase 6b: drag-to-place) ─────────────────
// Before any Run, `frames` is empty — instead of a blank stage, render the
// caster + live scene units so the user can see and drag them against real
// unit sprites/terrain. isEditMode is just `frames.length === 0`; it flips
// back to false the instant a run produces frames (or stays false with a
// non-empty frames array from a run that genuinely captured none — same
// "idle" fallback applyFrame already used).
const isEditMode = computed(() => props.frames.length === 0)

// Synthetic ownerId/color/unitType assignments MIRROR ability_preview.go's
// RunAbilityPreview spawn loop exactly (previewCasterOwner/previewEnemyOwner,
// "#3498db"/"#e74c3c", adept/raider/soldier) — see that file's doc comments.
// Matching it here means the caster/ally/enemy sprites the user drags around
// are colored/team-tinted IDENTICALLY to how the real preview run renders
// them once Run is clicked; no separate "editor-only" palette to keep in
// sync by hand. Both ally and enemy scene units share the same "#e74c3c"
// color server-side (only ownerId/team differ) — reproduced verbatim, not
// "fixed" to look more distinct, since a mismatch here would be the actual
// bug.
const EDIT_CASTER_OWNER = 'preview_caster'
const EDIT_ALLY_OWNER = 'preview_caster'
const EDIT_ENEMY_OWNER = 'preview_enemy'
const EDIT_CASTER_COLOR = '#3498db'
const EDIT_SCENE_UNIT_COLOR = '#e74c3c'
const EDIT_CASTER_UNIT_TYPE = 'adept'
const EDIT_ENEMY_UNIT_TYPE = 'raider'
const EDIT_ALLY_UNIT_TYPE = 'soldier'
// The caster is a single fixed synthetic id; scene units are numbered off
// their index in `props.sceneUnits`. These ids are internal-only — they
// never travel to the server and never coexist with real server-assigned
// ids in the same GameState (edit mode and replay mode are mutually
// exclusive, gated by isEditMode) — they only need to stay stable across
// this component's own re-renders of the SAME array, which indexing off
// array position already gives us.
const EDIT_CASTER_ID = -1

// buildEditModeUnits produces synthetic wire UnitSnapshots for the caster +
// every scene unit, positioned at their LIVE (possibly mid-drag) coordinates.
// Fed through the SAME mapUnitSnapshot() the real replay path uses (see
// applyFrame above) so edit mode can never silently diverge from how a real
// snapshot's units get mapped onto GameState.
function buildEditModeUnits(casterX: number, casterY: number, sceneUnits: PreviewSceneUnit[]): UnitSnapshot[] {
  const units: UnitSnapshot[] = [
    {
      id: EDIT_CASTER_ID,
      ownerId: EDIT_CASTER_OWNER,
      color: EDIT_CASTER_COLOR,
      unitType: EDIT_CASTER_UNIT_TYPE,
      name: 'Caster',
      visible: true,
      x: casterX,
      y: casterY,
      hp: 1,
      maxHp: 1,
      moving: false,
    },
  ]
  sceneUnits.forEach((su, index) => {
    const isEnemy = su.team === 'enemy'
    units.push({
      id: -(index + 2),
      ownerId: isEnemy ? EDIT_ENEMY_OWNER : EDIT_ALLY_OWNER,
      color: EDIT_SCENE_UNIT_COLOR,
      unitType: isEnemy ? EDIT_ENEMY_UNIT_TYPE : EDIT_ALLY_UNIT_TYPE,
      name: isEnemy ? `Enemy ${index + 1}` : `Ally ${index + 1}`,
      visible: true,
      x: su.x,
      y: su.y,
      hp: Math.max(0, su.hp),
      maxHp: Math.max(1, su.maxHp),
      moving: false,
    })
  })
  return units
}

// editModePoints: every world point currently on the edit-mode stage (caster
// + scene units), for computeSceneBBoxFromPoints to frame the camera around.
function editModePoints(): Array<{ x: number; y: number }> {
  const pts: Array<{ x: number; y: number }> = [{ x: props.casterX ?? 0, y: props.casterY ?? 0 }]
  for (const u of props.sceneUnits) pts.push({ x: u.x, y: u.y })
  return pts
}

// applyEditModeScene writes the synthetic caster+scene-unit snapshot onto
// the standalone GameState — the edit-mode equivalent of applyFrame above.
// No corpses/projectiles/beams/effects/traps exist before a cast is ever
// requested (clearing them also drops anything left over from a previous run's
// frames — a body from the last replay must not haunt the edit-mode scene).
function applyEditModeScene() {
  if (!state) return
  state.units = buildEditModeUnits(props.casterX ?? 0, props.casterY ?? 0, props.sceneUnits).map(mapUnitSnapshot)
  state.corpses = []
  state.projectiles = []
  state.beams = []
  state.effects = []
  state.traps = []
}

// ── floating damage/heal numbers (Task 9) ───────────────────────────────
// lastDamageFrameIndex guards spawnDamageNumbersForIndex the same way
// lastAppliedIndex guards applyFrame above, but is tracked SEPARATELY (not
// shared) — a reset here must not silently perturb the per-unit remap
// guard, or vice versa. -1 is never a valid frame index, so rearming it
// forces the very next call to (re)spawn whatever events belong to the
// currently-displayed frame even if that frame's index hasn't changed.
let lastDamageFrameIndex = -1

// clearDamageNumbers is the seek/restart/new-run reset point: wipes the
// renderer's in-flight floating numbers (via the minimal public seam added
// to CanvasRenderer for exactly this — see its doc comment) and rearms
// lastDamageFrameIndex so the destination frame's own numbers reliably
// (re)spawn next, instead of silently staying cleared until the index
// happens to change again.
function clearDamageNumbers() {
  renderer?.clearFloatingDamageNumbers()
  lastDamageFrameIndex = -1
}

// spawnDamageNumbersForIndex pushes frame `i`'s damage_applied/healing_applied
// trace events onto GameState.damageEvents — the SAME drain path
// CanvasRenderer.render() already uses for the live network path (see its
// "Drain new damage events" comment at the top of render()) — so the replay
// gets floating numbers from the renderer's existing paint logic with no
// further renderer changes. Guarded by lastDamageFrameIndex so a frame
// already spawned this run doesn't re-spawn every RAF tick while
// paused/idle on it (frames advance at 20fps, tick() runs at ~60fps and
// keeps spinning while paused — same cadence mismatch lastAppliedIndex's
// doc comment above describes).
function spawnDamageNumbersForIndex(i: number) {
  if (!state || i === lastDamageFrameIndex) return
  lastDamageFrameIndex = i
  const frame = props.frames[i]
  if (!frame) return
  const specs = damageNumbersForFrameIndex(
    props.trace ?? [],
    i,
    frame.snapshot.units ?? [],
    props.casterX ?? 0,
    props.casterY ?? 0,
  )
  if (specs.length === 0) return
  // createdAt lands on previewClockMs(i) — the SAME injected-clock axis
  // CanvasRenderer's render() reads via `previewClock` (this component's
  // injected timeSource) for this same frame index. Using performance.now()
  // here instead would stamp a number on the real wall clock while the
  // renderer's own renderTime is the frame-index clock — the two would
  // disagree the instant they're compared, so a number spawns already
  // "elapsed" (aged/faded, possibly past FLOATING_DAMAGE_DURATION_MS
  // entirely) instead of freshly popping in at elapsed===0. See N3 doc
  // comment at the top of this file and previewClockMs's own comment.
  const now = previewClockMs(i)
  for (const spec of specs) {
    state.damageEvents.push({
      unitId: spec.unitId,
      unitType: spec.unitType,
      x: spec.x,
      y: spec.y,
      amount: spec.amount,
      isFriendly: spec.isFriendly,
      createdAt: now,
      kind: spec.kind,
      damageType: spec.damageType,
    })
  }
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

  // Edit mode (frames: [], Phase 6b) used to early-return here: casterX/castX
  // etc. held STALE values from the last run (or their 0-fallback), so a
  // ring would paint over an otherwise-empty stage. That's no longer true —
  // the panel now feeds this component LIVE caster/cast coordinates while
  // editing (only freezing them at run time for an actual replay), so the
  // rings drawn below are exactly what dragging the caster/cast point around
  // would produce once Run is clicked. Showing them while placing units is
  // the whole point of Phase 6b (test range/radius against real geometry
  // before running), so this now falls through unconditionally.
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

// ── drag-to-place (Phase 6b, edit mode only) ─────────────────────────────
// The overlay/render canvases stay pointer-events:none (CanvasRenderer never
// needs to know a drag is happening); the `.ab-preview-canvas__drag-layer`
// div in the template is the ONLY interactive element, mounted/unmounted
// alongside edit mode itself (`v-if="!frames.length"`).
//
// Hit-testing converts the pointer to a WORLD position (screenToWorld) and
// tests it against each candidate's actual on-screen SPRITE BODY via
// isPointInUnitBody — the SAME world-space body-rect the in-match unit
// selection uses (game/rendering/unitSprites.ts), which accounts for the
// sprite being anchored at the unit's FEET with its body extending upward.
// A naive "radius around unit.x/unit.y" test would target the feet/shadow,
// not the visible model — and since the preview zooms in far, the gap between
// a unit's feet (its world y) and its body is large in screen space, so clicks
// on the model would miss. Using the sprite body rect makes clicks land on the
// visible character exactly as they do when selecting units in a real match.

type DragTarget = { kind: 'caster' } | { kind: 'unit'; index: number }
let dragTarget: DragTarget | null = null
let dragPointerId: number | null = null
// World-space offset from the grabbed point to the unit's anchor, captured at
// pointer-down. Preserved for the whole drag so the sprite stays exactly where
// you grabbed it under the cursor — without it, every move snaps the unit's
// anchor (unit.x/unit.y) to the cursor, which yanks the visible body (drawn
// well above the anchor) up and away from the pointer on the first move.
let dragGrabOffset = { x: 0, y: 0 }

function currentCam(): { x: number; y: number; zoom: number } | null {
  return camera ? { x: camera.x, y: camera.y, zoom: camera.zoom } : null
}

// unitFrameContains tests a WORLD point against the FULL sprite draw box the
// renderer blits — [x ± w/2] × [y + bounds.bottom - h, y + bounds.bottom],
// where w/h are the sheet frame size × UNIT_SPRITE_SCALE (see CanvasRenderer's
// unit draw: dx = unit.x - w/2, dy = unit.y + bounds.bottom - h). We use the
// FULL frame, not the padding-trimmed getUnitBodyRect, because a sprite's
// visible feet can sit above the trimmed rect's bottom (transparent padding
// varies per sheet), which made clicks land BELOW the visible model. The full
// frame always contains the whole sprite, so clicking the model always grabs
// it. Falls back to the body rect when the sprite sheet hasn't decoded yet.
function unitFrameContains(worldX: number, worldY: number, x: number, y: number, unitType: string): boolean {
  const spriteSet = getUnitSpriteSet(undefined, unitType)
  if (!spriteSet) return isPointInUnitBody(worldX, worldY, { x, y, unitType })
  const bounds = getUnitBoundsFor({ unitType })
  const w = spriteSet.size.width * UNIT_SPRITE_SCALE
  const h = spriteSet.size.height * UNIT_SPRITE_SCALE
  const maxY = y + bounds.bottom
  return worldX >= x - w / 2 && worldX <= x + w / 2 && worldY >= maxY - h && worldY <= maxY
}

function pointerToStageScreen(canvas: HTMLCanvasElement, e: PointerEvent): { x: number; y: number } {
  const rect = canvas.getBoundingClientRect()
  return { x: e.clientX - rect.left, y: e.clientY - rect.top }
}

// hitTestEditScene finds the draggable (caster or scene unit) whose sprite
// body contains the given screen position. On overlap it prefers the one drawn
// ON TOP (greater world y is painted later — see drawUnits' anchorY sort), so a
// click lands on the unit you can actually see. Returns null when the click is
// on empty stage (a pointerdown there is a no-op, not a drag). Uses the SAME
// unit types buildEditModeUnits renders with, and omits `path` identically, so
// the hit box matches the drawn sprite exactly.
function hitTestEditScene(screenX: number, screenY: number): DragTarget | null {
  const cam = currentCam()
  if (!cam) return null
  const world = screenToWorld(screenX, screenY, cam)

  const candidates: Array<{ target: DragTarget; x: number; y: number; unitType: string }> = [
    { target: { kind: 'caster' }, x: props.casterX ?? 0, y: props.casterY ?? 0, unitType: EDIT_CASTER_UNIT_TYPE },
    ...props.sceneUnits.map((u, index) => ({
      target: { kind: 'unit' as const, index },
      x: u.x,
      y: u.y,
      unitType: u.team === 'enemy' ? EDIT_ENEMY_UNIT_TYPE : EDIT_ALLY_UNIT_TYPE,
    })),
  ]

  let best: { target: DragTarget; y: number } | null = null
  for (const c of candidates) {
    if (!unitFrameContains(world.x, world.y, c.x, c.y, c.unitType)) continue
    if (!best || c.y > best.y) best = { target: c.target, y: c.y }
  }
  return best ? best.target : null
}

function onDragPointerDown(e: PointerEvent) {
  const canvas = canvasEl.value
  if (!canvas || !isEditMode.value) return
  const { x: screenX, y: screenY } = pointerToStageScreen(canvas, e)
  const hit = hitTestEditScene(screenX, screenY)
  if (!hit) return
  dragTarget = hit
  dragPointerId = e.pointerId
  // Capture the grab offset (anchor - grabbed world point) so the sprite
  // tracks the cursor from wherever it was clicked, instead of teleporting its
  // anchor onto the pointer.
  const cam = currentCam()
  if (cam) {
    const world = screenToWorld(screenX, screenY, cam)
    const anchor =
      hit.kind === 'caster'
        ? { x: props.casterX ?? 0, y: props.casterY ?? 0 }
        : { x: props.sceneUnits[hit.index]?.x ?? 0, y: props.sceneUnits[hit.index]?.y ?? 0 }
    dragGrabOffset = { x: anchor.x - world.x, y: anchor.y - world.y }
  } else {
    dragGrabOffset = { x: 0, y: 0 }
  }
  // Optional chaining: happy-dom/jsdom (unit tests) don't implement pointer
  // capture at all — harmless no-op there, real browsers get the intended
  // "a fast drag doesn't drop the target when the pointer leaves the layer"
  // behavior.
  ;(e.currentTarget as (HTMLElement & { setPointerCapture?: (id: number) => void }) | null)?.setPointerCapture?.(
    e.pointerId,
  )
}

function onDragPointerMove(e: PointerEvent) {
  if (!dragTarget || dragPointerId === null || e.pointerId !== dragPointerId) return
  const canvas = canvasEl.value
  const cam = currentCam()
  if (!canvas || !cam) return
  const { x: screenX, y: screenY } = pointerToStageScreen(canvas, e)
  const pointerWorld = screenToWorld(screenX, screenY, cam)
  // Apply the grab offset so the unit's anchor keeps the same relationship to
  // the cursor it had at pointer-down (no first-move jump).
  let world = { x: pointerWorld.x + dragGrabOffset.x, y: pointerWorld.y + dragGrabOffset.y }
  // Clamp to the map's actual bounds — dragging a unit off the terrain
  // entirely isn't a useful test of range/radius against real geometry.
  if (state) {
    world = {
      x: Math.min(Math.max(world.x, 0), state.mapWidth),
      y: Math.min(Math.max(world.y, 0), state.mapHeight),
    }
  }
  if (dragTarget.kind === 'caster') {
    emit('update:caster', { x: world.x, y: world.y })
  } else {
    emit('update:scene-unit', { index: dragTarget.index, x: world.x, y: world.y })
  }
}

function onDragPointerUp(e: PointerEvent) {
  if (dragPointerId === null || e.pointerId !== dragPointerId) return
  dragTarget = null
  dragPointerId = null
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
    // A parent-driven seek (e.g. clicking a trace-log row via onSeekEvent)
    // is exactly the "scrub" case Task 9 must not leave stale numbers
    // across — clear so only the destination frame's own numbers show.
    clearDamageNumbers()
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
  clearDamageNumbers()
}

function onRestart() {
  seekBase.value = 0
  startedAtMs.value = performance.now()
  emitTick(0)
  setPlaying(true)
  clearDamageNumbers()
}

const maxTimeSeconds = computed(() => Math.max(0, props.frames.length - 1) * PREVIEW_FRAME_DT_SECONDS)

// A new preview run replaces `frames` wholesale — snap back to frame 0 and
// recompute the camera framing for the new scene. Also fires on the reverse
// transition (a drag clears `result` back to edit mode — see
// AbilityPreviewPanel.vue's onUpdateSceneUnit/onUpdateCaster): frames goes
// from populated back to `[]`, and this branches into the edit-mode bbox/
// scene application instead of the replay one. tick() below re-derives both
// continuously anyway while actually IN edit mode (a live drag doesn't
// change `frames` at all, so this watcher alone can't track a drag — see
// tick()'s own edit-mode branch), so this handler only needs to get the
// FIRST frame of either mode right.
watch(
  () => props.frames,
  (frames) => {
    if (frames.length === 0) {
      sceneBBox.value = computeSceneBBoxFromPoints(editModePoints())
      applyEditModeScene()
    } else {
      sceneBBox.value = computeSceneBBox(frames)
      // New scene data for the same index (0) — force applyFrame's re-apply
      // guard in tick() to run again instead of treating 0 as already-applied.
      lastAppliedIndex = -1
      applyFrame(0)
      lastAppliedIndex = 0
    }
    seekBase.value = 0
    startedAtMs.value = performance.now()
    emitTick(0)
    // A re-run (or the error path clearing frames, or a drag returning to
    // edit mode) must not leave the PREVIOUS run's floating numbers on
    // screen fading out over the new scene — clear, then (replay only)
    // immediately (re)spawn whatever frame 0 of the fresh trace carries
    // (typically none; frame 0 is captured before the cast is even
    // requested — see ability_preview.go). Edit mode has no trace at all.
    clearDamageNumbers()
    if (frames.length > 0) spawnDamageNumbersForIndex(0)
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

  if (isEditMode.value) {
    // Static, user-editable scene: no frame index to advance — re-derive the
    // synthetic caster/scene-unit snapshot and camera framing from whatever
    // the panel currently holds every tick (cheap: a handful of objects),
    // so a live drag (which changes props.sceneUnits/casterX/Y but NOT
    // `frames` — the watcher above can't see it) is reflected immediately,
    // not just at mode-transition edges.
    previewFrameIndex = 0
    sceneBBox.value = computeSceneBBoxFromPoints(editModePoints())
    applyEditModeScene()
    refreshCamera(canvas, cam, sceneBBox.value, st.mapWidth, st.mapHeight)
    rend.render()
    drawOverlays(cam, canvas)
    return
  }

  const index = frameIndexAt({
    playing: playing.value,
    startedAtMs: startedAtMs.value,
    nowMs: performance.now(),
    speed: speed.value,
    frameCount: props.frames.length,
    seekTick: seekBase.value,
  })

  // Seed the injected render clock (previewClock, read by `rend.render()`
  // below via CanvasRenderer's timeSource seam) with the frame index this
  // tick is about to display — BEFORE applyFrame/spawnDamageNumbersForIndex
  // run, so a number spawned this tick and the renderer's own renderTime
  // agree on the exact same previewClockMs(index) value (see N3 doc comment
  // + previewClockMs's comment for why that equality matters).
  previewFrameIndex = index

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
  // Guarded independently by lastDamageFrameIndex (see its own doc comment)
  // — always called, not nested in the `if` above, so a clearDamageNumbers()
  // reset (which rearms lastDamageFrameIndex to -1 without touching
  // lastAppliedIndex) reliably respawns even when the displayed index itself
  // didn't change.
  spawnDamageNumbersForIndex(index)
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
  // previewClock (this component's injected clock — see the N3 doc comment
  // above) replaces CanvasRenderer's default real-clock timeSource so every
  // cosmetic it drives freezes/scrubs in lockstep with the displayed frame
  // instead of free-running on real elapsed time.
  const rend = new CanvasRenderer(canvas, st, cam, previewClock)

  state = st
  camera = cam
  renderer = rend
  overlayCtx = overlayCanvasEl.value?.getContext('2d') ?? null

  if (isEditMode.value) {
    sceneBBox.value = computeSceneBBoxFromPoints(editModePoints())
    applyEditModeScene()
  } else {
    sceneBBox.value = computeSceneBBox(props.frames)
    previewFrameIndex = props.currentTick
    applyFrame(props.currentTick)
    lastAppliedIndex = props.currentTick
    spawnDamageNumbersForIndex(props.currentTick)
  }
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

/* The only interactive layer in the stage (Phase 6b drag-to-place) — no
   `cursor:` declaration here: the app's custom game cursor already wins via
   the global rule in style.css, a draggable unit needs no cursor change. */
.ab-preview-canvas__drag-layer {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  touch-action: none;
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
