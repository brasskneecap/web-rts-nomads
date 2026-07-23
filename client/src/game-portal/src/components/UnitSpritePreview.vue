<template>
  <div class="sprite-preview">
    <template v-if="hasArt">
      <div class="sprite-preview__main">
        <canvas
          ref="mainCanvas"
          :width="MAIN_BOX"
          :height="MAIN_BOX"
          class="sprite-preview__canvas"
          @mousedown="onCanvasMouseDown"
          @mousemove="onCanvasMouseMove"
          @mouseleave="onCanvasMouseLeave"
        />

        <div class="sprite-preview__controls">
          <label>
            Animation
            <select v-model="animation">
              <option v-for="name in animationOptions" :key="name" :value="name">{{ name }}</option>
            </select>
          </label>

          <label>
            Facing
            <select v-model="direction">
              <option v-for="d in UNIT_DIRECTIONS" :key="d" :value="d">{{ directionLabel(d) }}</option>
            </select>
          </label>

          <button type="button" class="sprite-preview__play" @click="playing = !playing">
            {{ playing ? 'Pause' : 'Play' }}
          </button>

          <label class="sprite-preview__scrub">
            Frame {{ frame + 1 }} / {{ frameCount }}
            <input
              type="range"
              min="0"
              :max="Math.max(0, frameCount - 1)"
              v-model.number="frame"
            />
          </label>

          <label class="sprite-preview__fps">
            FPS
            <input type="number" min="1" max="30" v-model.number="fps" />
          </label>

          <button
            v-if="hasChannelAbility"
            type="button"
            class="sprite-preview__channel"
            :class="{ 'is-active': channelActive }"
            @click="toggleChannel"
          >
            Channel Loop {{ channelActive ? '■' : '▶' }}
          </button>
        </div>

        <p v-if="fallbackNote" class="sprite-preview__note">{{ fallbackNote }}</p>
        <p v-if="channelActive" class="sprite-preview__note sprite-preview__note--channel">
          Channelling <strong>{{ channelAbility?.id }}</strong> — looping casting frames
          {{ channelRangeLabel }}, firing from the attack origin every
          {{ channelAbility?.tickIntervalSeconds ? channelAbility.tickIntervalSeconds + 's' : 'tick' }}.
        </p>

        <div class="sprite-preview__origin">
          <button
            type="button"
            class="sprite-preview__origin-toggle"
            :class="{ 'is-active': showAttackOrigin }"
            @click="showAttackOrigin = !showAttackOrigin"
          >
            Attack Origin {{ showAttackOrigin ? '▾' : '▸' }}
          </button>

          <div v-if="showAttackOrigin" class="sprite-preview__origin-body">
            <p class="sprite-preview__hint">
              Drag the marker on the sprite (or click anywhere on it) to set where
              <strong>{{ directionLabel(direction) }}</strong>-facing projectiles/spells leave the body.
              {{ isFacingAuthored ? 'Authored for this facing.' : 'Showing the default launch point — nothing moves until you drag.' }}
            </p>

            <div class="sprite-preview__origin-inputs">
              <label>
                X
                <input
                  type="number"
                  :value="Math.round(currentOrigin.x)"
                  @input="onOriginXInput(($event.target as HTMLInputElement).value)"
                />
              </label>
              <label>
                Y
                <input
                  type="number"
                  :value="Math.round(currentOrigin.y)"
                  @input="onOriginYInput(($event.target as HTMLInputElement).value)"
                />
              </label>
              <button type="button" @click="applyToAllFacings">Apply to all facings</button>
              <button type="button" @click="fireTestProjectile">Fire test projectile</button>
            </div>
          </div>
        </div>

        <!-- Anchors & bounds authoring: draw + drag the render box, foot anchor,
             selection ring, and ground shadow directly on the sprite. -->
        <div class="sprite-preview__origin">
          <button
            type="button"
            class="sprite-preview__origin-toggle"
            :class="{ 'is-active': showGizmos }"
            data-test="unit-preview-gizmos-toggle"
            @click="showGizmos = !showGizmos"
          >
            Anchors &amp; bounds {{ showGizmos ? '▾' : '▸' }}
          </button>

          <div v-if="showGizmos" class="sprite-preview__origin-body">
            <p class="sprite-preview__hint">
              Drag the coloured handles on the sprite: the <strong>gold</strong> foot anchor,
              the <strong>blue</strong> selection ring (centre + right edge), and the
              <strong>violet</strong> ground shadow (centre + right edge). Grey dashed = the
              read-only render box.
            </p>

            <div class="sprite-preview__gizmo-layers">
              <label><input type="checkbox" v-model="gizmoLayers.renderBox" /> Render box</label>
              <label><input type="checkbox" v-model="gizmoLayers.foot" /> Foot anchor</label>
              <label><input type="checkbox" v-model="gizmoLayers.ring" /> Selection ring</label>
              <label><input type="checkbox" v-model="gizmoLayers.shadow" /> Shadow</label>
            </div>

            <div class="sprite-preview__gizmo-group">
              <span class="sprite-preview__gizmo-title">Footprint / anchor</span>
              <div class="sprite-preview__origin-inputs">
                <label>Half width
                  <input type="number" :value="Math.round(bounds.halfWidth)"
                    @input="setBound('halfWidth', ($event.target as HTMLInputElement).value)" /></label>
                <label>Top
                  <input type="number" :value="Math.round(bounds.top)"
                    @input="setBound('top', ($event.target as HTMLInputElement).value)" /></label>
                <label>Bottom
                  <input type="number" :value="Math.round(bounds.bottom)"
                    @input="setBound('bottom', ($event.target as HTMLInputElement).value)" /></label>
                <label>Ring X
                  <input type="number" :value="Math.round(bounds.ringOffsetX ?? 0)"
                    @input="setBound('ringOffsetX', ($event.target as HTMLInputElement).value)" /></label>
                <label>Ring Y
                  <input type="number" :value="Math.round(bounds.ringOffsetY ?? 0)"
                    @input="setBound('ringOffsetY', ($event.target as HTMLInputElement).value)" /></label>
              </div>
            </div>

            <div class="sprite-preview__gizmo-group">
              <span class="sprite-preview__gizmo-title">
                <label class="sprite-preview__gizmo-enable">
                  <input type="checkbox" :checked="shadowEnabled"
                    @change="setShadowEnabled(($event.target as HTMLInputElement).checked)" />
                  Ground shadow
                </label>
              </span>
              <div v-if="shadowEnabled && effShadow" class="sprite-preview__origin-inputs">
                <label>Off X
                  <input type="number" :value="Math.round(effShadow.offsetX)"
                    @input="setShadowNum('offsetX', ($event.target as HTMLInputElement).value)" /></label>
                <label>Off Y
                  <input type="number" :value="Math.round(effShadow.offsetY)"
                    @input="setShadowNum('offsetY', ($event.target as HTMLInputElement).value)" /></label>
                <label>Radius X
                  <input type="number" :value="round1(effShadow.radiusX)"
                    @input="setShadowNum('radiusX', ($event.target as HTMLInputElement).value)" /></label>
                <label>Radius Y
                  <input type="number" :value="round1(effShadow.radiusY)"
                    @input="setShadowNum('radiusY', ($event.target as HTMLInputElement).value)" /></label>
                <label>Opacity
                  <input type="number" min="0" max="1" step="0.05" :value="round1(effShadow.opacity)"
                    @input="setShadowNum('opacity', ($event.target as HTMLInputElement).value)" /></label>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>

    <div v-else class="sprite-preview__empty">
      No packed art for <code>{{ displayKey }}</code> — it renders as a placeholder in game.
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  getSpritePaddingFrac, getUnitFrame, getUnitSpriteSet, UNIT_DIRECTIONS, UNIT_SPRITE_SCALE,
  type UnitDirection, type UnitSpriteSet,
} from '@/game/rendering/unitSprites'
import { channelLoopFrameIndex } from '@/game/rendering/unitAnimation'
import { channelTickPhase, type ChannelAbilityInfo } from '@/game/units/channelPreview'
import {
  getUnitBoundsFor,
  type UnitAttackOrigin, type UnitBounds, type UnitOriginPoint, type UnitShadow,
} from '@/game/maps/unitDefs'
import { resolveUnitShadow } from '@/game/maps/unitShadow'
import {
  canvasToOrigin, deriveDefaultOrigin, originToCanvas, resolveFacingOrigin, unitAnchorCanvas,
  type PreviewDrawGeometry,
} from '@/game/units/attackOriginPreviewMath'
import {
  applyGizmoDrag, drawGizmos, hitTestGizmo,
  type GizmoContext, type GizmoHandle, type GizmoOptions,
} from '@/game/units/spriteGizmos'
import { drawProjectileForVariant } from '@/game/rendering/projectileSprites'
import type { ProjectileSnapshot } from '@/game/network/protocol'

const props = defineProps<{
  unitKey?: string
  pathKey?: string
  attackOrigin?: UnitAttackOrigin
  // The unit's chosen projectile id (form.projectile). When set, the fire-test
  // draws that projectile's real visual instead of a placeholder dot — the
  // variant IS the projectile id (see projectile.go), so the same draw registry
  // the game uses renders it here.
  projectile?: string
  projectileScale?: number
  // Channel-loop preview: the [start,end] casting frames the unit loops while
  // channelling, and info about the channelling ability driving it. When
  // `channelAbility` is present the "Channel Loop" control appears so the
  // author can replay the exact in-game loop (and see it fire from the attack
  // origin). Both are undefined for units that don't channel.
  channelLoop?: { start: number; end: number }
  channelAbility?: ChannelAbilityInfo | null
  // Authored bounds/shadow from the editor form (partial — missing fields fall
  // back to catalog/defaults for display). Editing the anchors/bounds overlay
  // emits patches back through update:bounds / update:shadow.
  bounds?: Partial<UnitBounds>
  shadow?: UnitShadow
  flyer?: boolean
}>()
const emit = defineEmits<{
  'update:attackOrigin': [UnitAttackOrigin | undefined]
  'update:bounds': [Partial<UnitBounds> | undefined]
  'update:shadow': [UnitShadow | undefined]
}>()

// Fixed on-screen box (px) the canvas draws into. The sprite is fit inside at
// the largest integer scale that keeps pixel art crisp — no fractional scaling
// — then centered.
const MAIN_BOX = 240

// The idle rotation sheet isn't a packed animation, but it's presented as one
// selectable option so the author can inspect the static per-facing pose the
// same way they inspect walking/attacking. Selecting it draws the rotation for
// the current facing; the facing selector cycles through all 8.
const ROTATIONS_OPTION = 'rotations'

const set = ref<UnitSpriteSet | null>(null)
const animation = ref('walking')
const direction = ref<UnitDirection>('south')
const frame = ref(0)
const playing = ref(true)
const fps = ref(8)

const mainCanvas = ref<HTMLCanvasElement | null>(null)

// --- Attack-origin authoring (crosshair overlay) ---
// All coordinate math (screen-space offset <-> canvas pixel) lives in
// attackOriginPreviewMath.ts — this component only drives it: tracks the
// last drawn sprite geometry, resolves the current facing's point, and wires
// drag/click/numeric-input events to it.
const showAttackOrigin = ref(false)
const isDraggingOrigin = ref(false)
const hoveringOrigin = ref(false)
// Set every drawMain() call from drawFrame's return value. Never recomputed
// independently — hit-testing must use the SAME dx/dy/w/h/scale that just
// placed the sprite, or the crosshair and the sprite drift apart.
const lastGeometry = ref<PreviewDrawGeometry | null>(null)

// Effective bounds for display = the authored form values (props.bounds) layered
// over the catalog/default resolution, so the overlay reflects unsaved edits
// live. Editing emits patches back onto the authored block only (below).
const bounds = computed<UnitBounds>(() => {
  const fallback = getUnitBoundsFor({ path: props.pathKey, unitType: props.unitKey })
  return props.bounds ? { ...fallback, ...props.bounds } : fallback
})

// --- Anchors & bounds authoring (render box / foot anchor / selection ring / shadow) ---
// A debug overlay that draws — and lets you drag — the same geometry the game
// renderer uses, so aligning logic to the art is visual instead of guess-edited
// JSON. Off by default; nothing existing changes until it's toggled on.
const showGizmos = ref(false)
const gizmoLayers = ref<GizmoOptions>({ renderBox: true, foot: true, ring: true, shadow: true })
const activeHandle = ref<GizmoHandle | null>(null)
const draggingHandle = ref<GizmoHandle | null>(null)

// The sprite's transparent-bottom lift (1x px) — the exact term the renderer's
// ring/shadow math subtracts. 0 until art has loaded.
const ringLift = computed(() => {
  const s = set.value
  return s ? s.size.height * UNIT_SPRITE_SCALE * getSpritePaddingFrac(s).bottom : 0
})

const gizmoContext = computed<GizmoContext | null>(() => {
  const geo = lastGeometry.value
  if (!geo) return null
  return { geo, bounds: bounds.value, shadow: props.shadow, flyer: props.flyer, ringLift: ringLift.value }
})

// Emit patches onto the AUTHORED block only (props.bounds/props.shadow), leaving
// unedited fields to fall back to catalog/defaults — keeps the saved def lean.
function emitBoundsPatch(patch: Partial<UnitBounds>) {
  emit('update:bounds', { ...(props.bounds ?? {}), ...patch } as Partial<UnitBounds>)
}
function emitShadowPatch(patch: Partial<UnitShadow>) {
  emit('update:shadow', { ...(props.shadow ?? {}), ...patch })
}
function applyGizmo(handle: GizmoHandle, pt: { x: number; y: number }) {
  const c = gizmoContext.value
  if (!c) return
  const res = applyGizmoDrag(handle, pt, c)
  if (res.bounds) emitBoundsPatch(res.bounds)
  if (res.shadow) emitShadowPatch(res.shadow)
}

// Numeric-input setters mirror the drag edits for precise/keyboard adjustment.
function setBound(key: 'halfWidth' | 'top' | 'bottom' | 'ringOffsetX' | 'ringOffsetY', raw: string) {
  const n = Number(raw)
  if (!Number.isNaN(n)) emitBoundsPatch({ [key]: n })
}
function setShadowNum(key: 'radiusX' | 'radiusY' | 'opacity' | 'offsetX' | 'offsetY', raw: string) {
  const n = Number(raw)
  if (!Number.isNaN(n)) emitShadowPatch({ [key]: n })
}
function setShadowEnabled(enabled: boolean) {
  emitShadowPatch({ enabled })
}

// Resolved shadow (concrete defaults filled in) for the numeric inputs, so the
// author sees the values actually in effect. null when the shadow is disabled.
const effShadow = computed(() => resolveUnitShadow(props.shadow, bounds.value, props.flyer))
const shadowEnabled = computed(() => props.shadow?.enabled !== false)
const round1 = (n: number) => Math.round(n * 10) / 10

// The point projectiles leave an unauthored unit's body today (see
// deriveDefaultOrigin's doc comment). null while art hasn't loaded yet.
const derivedOrigin = computed<UnitOriginPoint | null>(() => {
  const s = set.value
  if (!s) return null
  return deriveDefaultOrigin(s, bounds.value)
})

// The authored point for the CURRENT facing, or null if this facing (and
// default) have nothing authored.
const authoredOrigin = computed<UnitOriginPoint | null>(() => resolveFacingOrigin(props.attackOrigin, direction.value))
const isFacingAuthored = computed(() => authoredOrigin.value !== null)

// The point actually drawn/edited: authored wins, falling back to the
// derived launch point (or (0,0) as a last resort before art has loaded).
const currentOrigin = computed<UnitOriginPoint>(() => authoredOrigin.value ?? derivedOrigin.value ?? { x: 0, y: 0 })

// Emits a NEW UnitAttackOrigin object (or undefined once nothing remains
// authored) so a v-model:attack-origin binding always fires reactively.
function emitOrigin(next: { default?: UnitOriginPoint; byFacing?: Partial<Record<UnitDirection, UnitOriginPoint>> }) {
  const cleaned: UnitAttackOrigin = {}
  if (next.default) cleaned.default = next.default
  if (next.byFacing && Object.keys(next.byFacing).length > 0) cleaned.byFacing = next.byFacing
  emit('update:attackOrigin', Object.keys(cleaned).length > 0 ? cleaned : undefined)
}

// Writes a new point for the CURRENT facing only. If the point lands back
// on the derived default (and there's no explicit `default` block authored)
// the byFacing override is dropped instead of stored — keeps unauthored
// units unauthored until the point actually moves.
function setFacingOrigin(point: UnitOriginPoint) {
  const rounded: UnitOriginPoint = { x: Math.round(point.x), y: Math.round(point.y) }
  const nextByFacing: Partial<Record<UnitDirection, UnitOriginPoint>> = { ...(props.attackOrigin?.byFacing ?? {}) }
  const derived = derivedOrigin.value
  const matchesDerived = derived && rounded.x === derived.x && rounded.y === derived.y
  if (matchesDerived && !props.attackOrigin?.default) {
    delete nextByFacing[direction.value]
  } else {
    nextByFacing[direction.value] = rounded
  }
  emitOrigin({ default: props.attackOrigin?.default, byFacing: nextByFacing })
}

function onOriginXInput(raw: string) {
  const n = Number(raw)
  if (Number.isNaN(n)) return
  setFacingOrigin({ x: n, y: currentOrigin.value.y })
}
function onOriginYInput(raw: string) {
  const n = Number(raw)
  if (Number.isNaN(n)) return
  setFacingOrigin({ x: currentOrigin.value.x, y: n })
}

// Sets `default` to the current point and clears every per-facing override —
// every facing falls back to `default` until individually re-authored.
function applyToAllFacings() {
  const point = currentOrigin.value
  emitOrigin({ default: { x: Math.round(point.x), y: Math.round(point.y) } })
}

// Translates a mouse event's client coordinates into canvas-internal pixel
// coordinates, accounting for any CSS scaling of the canvas element (its
// drawing buffer is fixed at MAIN_BOX, but layout could resize it).
function canvasPointFromEvent(e: MouseEvent): { x: number; y: number } | null {
  const canvas = mainCanvas.value
  if (!canvas) return null
  const rect = canvas.getBoundingClientRect()
  if (rect.width === 0 || rect.height === 0) return null
  return {
    x: (e.clientX - rect.left) * (canvas.width / rect.width),
    y: (e.clientY - rect.top) * (canvas.height / rect.height),
  }
}

function applyCanvasPoint(pt: { x: number; y: number }, geo: PreviewDrawGeometry) {
  const anchor = unitAnchorCanvas(geo, bounds.value)
  setFacingOrigin(canvasToOrigin(pt.x, pt.y, anchor, geo.scale))
}

function onCanvasMouseDown(e: MouseEvent) {
  const geo = lastGeometry.value
  const pt = canvasPointFromEvent(e)
  if (!geo || !pt) return
  // Anchors/bounds gizmos take priority over the attack-origin crosshair when a
  // handle is grabbed, so the two overlays never fight for the same drag.
  if (showGizmos.value && gizmoContext.value) {
    const handle = hitTestGizmo(pt, gizmoContext.value, gizmoLayers.value)
    if (handle) {
      draggingHandle.value = handle
      activeHandle.value = handle
      applyGizmo(handle, pt)
      window.addEventListener('mousemove', onWindowMouseMove)
      window.addEventListener('mouseup', onWindowMouseUp)
      return
    }
  }
  if (!showAttackOrigin.value) return
  isDraggingOrigin.value = true
  applyCanvasPoint(pt, geo)
  window.addEventListener('mousemove', onWindowMouseMove)
  window.addEventListener('mouseup', onWindowMouseUp)
}

function onWindowMouseMove(e: MouseEvent) {
  const geo = lastGeometry.value
  const pt = canvasPointFromEvent(e)
  if (!geo || !pt) return
  if (draggingHandle.value) {
    applyGizmo(draggingHandle.value, pt)
    return
  }
  if (!isDraggingOrigin.value) return
  applyCanvasPoint(pt, geo)
}

function stopDragListeners() {
  window.removeEventListener('mousemove', onWindowMouseMove)
  window.removeEventListener('mouseup', onWindowMouseUp)
}

function onWindowMouseUp() {
  isDraggingOrigin.value = false
  draggingHandle.value = null
  stopDragListeners()
}

// Hover-only proximity check (not dragging) — purely a visual affordance so
// the marker highlights before you click, without ever touching `cursor:`
// (banned project-wide; see AI_RULES.md).
const HOVER_RADIUS_CANVAS_PX = 10
function onCanvasMouseMove(e: MouseEvent) {
  if (isDraggingOrigin.value || draggingHandle.value) return
  const pt = canvasPointFromEvent(e)
  // Gizmo hover highlight (which handle would be grabbed).
  activeHandle.value =
    showGizmos.value && gizmoContext.value && pt
      ? hitTestGizmo(pt, gizmoContext.value, gizmoLayers.value)
      : null
  // Attack-origin crosshair hover (unchanged).
  if (!showAttackOrigin.value) { hoveringOrigin.value = false; return }
  const geo = lastGeometry.value
  if (!geo || !pt) { hoveringOrigin.value = false; return }
  const anchor = unitAnchorCanvas(geo, bounds.value)
  const originPt = originToCanvas(currentOrigin.value, anchor, geo.scale)
  hoveringOrigin.value = Math.hypot(pt.x - originPt.x, pt.y - originPt.y) <= HOVER_RADIUS_CANVAS_PX
}
function onCanvasMouseLeave() {
  hoveringOrigin.value = false
  if (!draggingHandle.value) activeHandle.value = null
}

// --- Fire-test projectile (preview-only ghost tween; never touches
// combat/sim) ---
const FIRE_TEST_DURATION_MS = 550
// How far the ghost travels, as a fraction of the canvas box — enough to read
// as "fired that way" without needing to reach any particular target.
const FIRE_TEST_TRAVEL = MAIN_BOX * 0.45
const fireTestActive = ref(false)
let fireTestStartedAt = 0

// Screen-space unit vector for each facing (x = right, y = down) — the same
// convention the game renderer uses (east = 0°, south = 90°). The test ghost
// flies along the CURRENT facing's vector so it visibly leaves in the
// direction the unit is pointing, matching how its real projectiles would.
const FIRE_TEST_DIRECTION_VECTORS: Record<UnitDirection, { x: number; y: number }> = {
  north: { x: 0, y: -1 },
  'north-east': { x: Math.SQRT1_2, y: -Math.SQRT1_2 },
  east: { x: 1, y: 0 },
  'south-east': { x: Math.SQRT1_2, y: Math.SQRT1_2 },
  south: { x: 0, y: 1 },
  'south-west': { x: -Math.SQRT1_2, y: Math.SQRT1_2 },
  west: { x: -1, y: 0 },
  'north-west': { x: -Math.SQRT1_2, y: -Math.SQRT1_2 },
}

function fireTestProjectile() {
  if (!hasArt.value) return
  const attackAnim = animationOptions.value.find((o) => o === 'attacking')
  if (attackAnim) animation.value = attackAnim
  frame.value = 0
  playing.value = true
  fireTestActive.value = true
  fireTestStartedAt = performance.now()
}

// --- Channel-loop preview ---
// Replays the unit's channel exactly as it renders in game: the casting sheet
// looping over [channelLoop.start, channelLoop.end] (via the shared
// channelLoopFrameIndex the runtime controller uses), with a pulse firing from
// the attack origin on the ability's tick cadence. Only offered when the unit
// actually has a channelling ability.
const hasChannelAbility = computed(() => props.channelAbility != null)
const channelActive = ref(false)
// Integer frame-step counter, advanced once per fps tick while channelling —
// the phase fed to channelLoopFrameIndex. Independent of `frame` so a manual
// scrub can't corrupt the loop.
const channelStep = ref(0)
let channelStartedAt = 0

const channelRangeLabel = computed(() => {
  const loop = props.channelLoop
  if (!loop || channelLoopFrameIndex(loop.start, loop.end, 0) === null) return '(no loop range set)'
  return `${loop.start}–${loop.end}`
})

// The casting frame for a given loop step. Falls back to plain cycling of the
// casting sheet when no valid loop range is authored, so the author still sees
// the channel play (and can tell the loop is unset).
function channelFrameFor(step: number): number {
  const loop = props.channelLoop
  const idx = channelLoopFrameIndex(loop?.start, loop?.end, step)
  return idx ?? (step % Math.max(1, frameCount.value))
}

function toggleChannel() {
  channelActive.value = !channelActive.value
  if (!channelActive.value) return
  // Drive the casting sheet (matches pickAnimation's Channeling -> 'casting').
  // Set BEFORE writing `frame` so the animation-change watch — which is guarded
  // against clobbering the pinned frame while channelling — has already seen
  // channelActive === true.
  animation.value = 'casting'
  channelStep.value = 0
  channelStartedAt = performance.now()
  playing.value = true
  // The whole point of the mode is to see the loop leave the attack origin, so
  // surface the crosshair too.
  showAttackOrigin.value = true
  frame.value = channelFrameFor(0)
}

const hasArt = computed(() => set.value !== null)
const displayKey = computed(() => props.pathKey || props.unitKey || '(none)')
// rotations first, then the unit's real packed animations, sorted.
const animationOptions = computed(() =>
  set.value ? [ROTATIONS_OPTION, ...[...set.value.animations.keys()].sort()] : [],
)
const isRotations = computed(() => animation.value === ROTATIONS_OPTION)
const frameCount = computed(() =>
  isRotations.value ? 1 : (set.value?.animations.get(animation.value)?.frameCount ?? 1),
)

// The author must be TOLD when the selected animation has no dedicated sheet
// and is quietly playing a substitute (a fallback strip like casting ->
// attacking) — never leave them guessing why a caster is swinging a weapon
// instead of channelling. rotations is a real thing, not a substitute, so it
// never warns.
const fallbackNote = computed(() => {
  if (!set.value || !animation.value || isRotations.value) return ''
  if (set.value.animations.has(animation.value)) return ''
  return `No dedicated "${animation.value}" sheet — showing the idle rotation / substitute animation.`
})

const DIRECTION_LABELS: Record<UnitDirection, string> = {
  north: 'N',
  'north-east': 'NE',
  east: 'E',
  'south-east': 'SE',
  south: 'S',
  'south-west': 'SW',
  west: 'W',
  'north-west': 'NW',
}
function directionLabel(d: UnitDirection): string {
  return DIRECTION_LABELS[d]
}

function refresh() {
  set.value = getUnitSpriteSet(props.pathKey, props.unitKey)
  frame.value = 0
  const opts = animationOptions.value
  if (opts.length && !opts.includes(animation.value)) {
    // Default to the first real animation (more informative than the static
    // rotation pose); fall back to rotations if the unit has none.
    animation.value = opts.find((o) => o !== ROTATIONS_OPTION) ?? ROTATIONS_OPTION
  }
}
defineExpose({ refresh })
watch(() => [props.unitKey, props.pathKey], refresh, { immediate: true })

// Reset/clamp the frame whenever the selected animation (or its frame count)
// changes, so a scrubber left past a shorter animation's end doesn't break.
// While channelling, picking any non-casting animation exits channel mode; the
// frame reset is skipped for the casting sheet so the loop stays pinned.
watch(animation, (a) => {
  if (channelActive.value && a !== 'casting') channelActive.value = false
  if (!channelActive.value) frame.value = 0
})
watch(frameCount, (fc) => { frame.value = frame.value % Math.max(1, fc) })

// A unit swap that clears the channelling ability must drop channel mode —
// otherwise the casting sheet keeps looping on a unit that doesn't channel.
watch(() => props.channelAbility, (ca) => { if (!ca) channelActive.value = false })

let raf = 0
let lastStep = 0
function tick(now: number) {
  raf = requestAnimationFrame(tick)
  if (playing.value && now - lastStep >= 1000 / Math.max(1, fps.value)) {
    lastStep = now
    if (channelActive.value) {
      channelStep.value += 1
      frame.value = channelFrameFor(channelStep.value)
    } else {
      frame.value = (frame.value + 1) % Math.max(1, frameCount.value)
    }
  }
  drawMain(now)
}
raf = requestAnimationFrame(tick)
onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
  stopDragListeners()
})

// Draws one animation/direction/frame into a square canvas box, fit at the
// largest integer scale and centered. Never reimplements srcX/srcY math —
// that lives entirely in getUnitFrame. A null return (art missing / not yet
// decoded / no canvas) just leaves the canvas cleared instead of throwing or
// drawing stale content. Returns the exact draw geometry used, so the
// attack-origin overlay/hit-testing can share it instead of recomputing.
function drawFrame(
  canvas: HTMLCanvasElement | null,
  animName: string,
  dir: UnitDirection,
  frameIndex: number,
  box: number,
): PreviewDrawGeometry | null {
  if (!canvas) return null
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  ctx.clearRect(0, 0, box, box)
  const s = set.value
  if (!s) return null
  const drawable = getUnitFrame(s, animName, dir, frameIndex)
  if (!drawable) return null
  ctx.imageSmoothingEnabled = false
  const scale = Math.max(1, Math.floor(box / Math.max(drawable.srcW, drawable.srcH)))
  const w = drawable.srcW * scale
  const h = drawable.srcH * scale
  const x = (box - w) / 2
  const y = (box - h) / 2
  ctx.drawImage(
    drawable.image,
    drawable.srcX, drawable.srcY, drawable.srcW, drawable.srcH,
    x, y, w, h,
  )
  return { dx: x, dy: y, w, h, scale }
}

// Draws the crosshair marker for the current facing's attack origin. Purely
// a canvas overlay drawn AFTER the sprite each frame — never persisted,
// never affects the sprite bitmap itself.
function drawAttackOriginOverlay(ctx: CanvasRenderingContext2D, geo: PreviewDrawGeometry) {
  const anchor = unitAnchorCanvas(geo, bounds.value)
  const pt = originToCanvas(currentOrigin.value, anchor, geo.scale)
  const highlighted = isDraggingOrigin.value || hoveringOrigin.value
  const ringRadius = highlighted ? 8 : 6
  const color = isDraggingOrigin.value ? '#fde68a' : highlighted ? '#7dd3fc' : '#38bdf8'
  ctx.save()
  ctx.strokeStyle = color
  ctx.lineWidth = highlighted ? 2.5 : 2
  ctx.beginPath()
  ctx.arc(pt.x, pt.y, ringRadius, 0, Math.PI * 2)
  ctx.stroke()
  ctx.beginPath()
  ctx.moveTo(pt.x - ringRadius - 4, pt.y)
  ctx.lineTo(pt.x + ringRadius + 4, pt.y)
  ctx.moveTo(pt.x, pt.y - ringRadius - 4)
  ctx.lineTo(pt.x, pt.y + ringRadius + 4)
  ctx.stroke()
  ctx.restore()
}

// Ghost-tweens a projectile from the authored origin along the current facing,
// over FIRE_TEST_DURATION_MS. Preview-only confirmation that the authored origin
// "looks right" — never touches combat/sim state. When the unit has a projectile
// chosen, draws that projectile's ACTUAL visual (same draw registry the game
// uses); otherwise a plain placeholder dot.
function drawFireTestGhost(ctx: CanvasRenderingContext2D, geo: PreviewDrawGeometry, now: number) {
  const elapsed = now - fireTestStartedAt
  if (elapsed > FIRE_TEST_DURATION_MS) {
    fireTestActive.value = false
    return
  }
  const t = Math.min(1, Math.max(0, elapsed / FIRE_TEST_DURATION_MS))
  const anchor = unitAnchorCanvas(geo, bounds.value)
  const originPt = originToCanvas(currentOrigin.value, anchor, geo.scale)
  // Fly along the CURRENT facing's screen-space vector, so the ghost leaves in
  // the direction the unit is pointing rather than always to the right.
  const dir = FIRE_TEST_DIRECTION_VECTORS[direction.value]
  const x = originPt.x + dir.x * FIRE_TEST_TRAVEL * t
  const y = originPt.y + dir.y * FIRE_TEST_TRAVEL * t

  ctx.save()
  ctx.translate(x, y)
  if (props.projectile) {
    // Real projectile visual. Scale to the preview's sprite zoom so it reads
    // proportionally, and rotate to the flight heading (the draw fn renders
    // along +x). Only .variant and .scale are read, so a minimal object is safe.
    ctx.scale(geo.scale, geo.scale)
    ctx.rotate(Math.atan2(dir.y, dir.x))
    drawProjectileForVariant(ctx, {
      zoom: 1,
      projectile: {
        variant: props.projectile,
        scale: props.projectileScale && props.projectileScale > 0 ? props.projectileScale : 1,
      } as unknown as ProjectileSnapshot,
    })
  } else {
    ctx.fillStyle = '#fbbf24'
    ctx.beginPath()
    ctx.arc(0, 0, 4, 0, Math.PI * 2)
    ctx.fill()
  }
  ctx.restore()
}

function drawMain(now: number) {
  // For the rotations option, pass '' so getUnitFrame falls straight through to
  // the idle rotation sheet regardless of what the animation is named. For a
  // real animation, pass its name.
  const animArg = isRotations.value ? '' : animation.value
  const geo = drawFrame(mainCanvas.value, animArg, direction.value, frame.value, MAIN_BOX)
  lastGeometry.value = geo
  if (!geo) return
  const ctx = mainCanvas.value?.getContext('2d')
  if (!ctx) return
  // Channelling always shows the origin crosshair (the loop is meant to be read
  // against its launch point) even if the author collapsed the section.
  if (showAttackOrigin.value || channelActive.value) drawAttackOriginOverlay(ctx, geo)
  if (channelActive.value) drawChannelBeam(ctx, geo, now)
  if (fireTestActive.value) drawFireTestGhost(ctx, geo, now)
  if (showGizmos.value && gizmoContext.value) drawGizmos(ctx, gizmoContext.value, gizmoLayers.value, activeHandle.value)
}

// Draws the channel beam: a faint line from the attack origin along the current
// facing, plus a pulse that travels outward once per ability tick — a
// preview-only visualization of where the channel emits and at what cadence.
// Never touches sim state.
function drawChannelBeam(ctx: CanvasRenderingContext2D, geo: PreviewDrawGeometry, now: number) {
  const anchor = unitAnchorCanvas(geo, bounds.value)
  const originPt = originToCanvas(currentOrigin.value, anchor, geo.scale)
  const dir = FIRE_TEST_DIRECTION_VECTORS[direction.value]
  const endX = originPt.x + dir.x * FIRE_TEST_TRAVEL
  const endY = originPt.y + dir.y * FIRE_TEST_TRAVEL

  ctx.save()
  ctx.strokeStyle = 'rgba(125, 211, 252, 0.35)'
  ctx.lineWidth = 2
  ctx.beginPath()
  ctx.moveTo(originPt.x, originPt.y)
  ctx.lineTo(endX, endY)
  ctx.stroke()

  // Pulse position along the beam = phase [0,1) through the current tick.
  const phase = channelTickPhase(now - channelStartedAt, props.channelAbility?.tickIntervalSeconds)
  const px = originPt.x + dir.x * FIRE_TEST_TRAVEL * phase
  const py = originPt.y + dir.y * FIRE_TEST_TRAVEL * phase
  ctx.fillStyle = `rgba(56, 189, 248, ${(0.4 + 0.6 * (1 - phase)).toFixed(3)})`
  ctx.beginPath()
  ctx.arc(px, py, 4, 0, Math.PI * 2)
  ctx.fill()
  ctx.restore()
}
</script>

<style scoped>
.sprite-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  box-sizing: border-box;
}

.sprite-preview__main {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.sprite-preview__canvas {
  background: rgba(8, 14, 24, 0.55);
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 10px;
  image-rendering: pixelated;
}

.sprite-preview__controls {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: center;
  gap: 10px;
  width: 100%;
}

.sprite-preview__controls label,
.sprite-preview__hint {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.sprite-preview__hint {
  align-self: center;
  opacity: 0.75;
}

.sprite-preview__controls select,
.sprite-preview__controls input[type='number'] {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.sprite-preview__controls input[type='number'] {
  width: 64px;
}

.sprite-preview__scrub {
  flex: 1 1 160px;
  min-width: 140px;
}

.sprite-preview__scrub input[type='range'] {
  width: 100%;
}

.sprite-preview__play {
  align-self: flex-end;
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 10px;
  background: rgba(215, 187, 132, 0.16);
  color: #f8fafc;
  padding: 7px 14px;
  font-size: 0.78rem;
  font-weight: 700;
}

.sprite-preview__play:hover {
  border-color: rgba(215, 187, 132, 0.85);
  background: rgba(215, 187, 132, 0.26);
}

.sprite-preview__note {
  margin: 0;
  color: #fcd34d;
  font-size: 0.72rem;
  text-align: center;
}

.sprite-preview__note--channel {
  color: #7dd3fc;
}

.sprite-preview__note--channel strong {
  color: #bae6fd;
}

.sprite-preview__channel {
  align-self: flex-end;
  border: 1px solid rgba(56, 189, 248, 0.5);
  border-radius: 10px;
  background: rgba(56, 189, 248, 0.14);
  color: #f8fafc;
  padding: 7px 14px;
  font-size: 0.78rem;
  font-weight: 700;
}

.sprite-preview__channel:hover {
  border-color: rgba(56, 189, 248, 0.85);
  background: rgba(56, 189, 248, 0.24);
}

.sprite-preview__channel.is-active {
  border-color: rgba(56, 189, 248, 0.9);
  background: rgba(56, 189, 248, 0.32);
}

.sprite-preview__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1;
  min-height: 120px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px dashed rgba(148, 163, 184, 0.3);
  border-radius: 16px;
  padding: 24px;
  color: rgba(226, 232, 240, 0.7);
  font-size: 0.82rem;
  text-align: center;
}

.sprite-preview__empty code {
  color: #d7bb84;
}

.sprite-preview__origin {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sprite-preview__origin-toggle {
  align-self: center;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.72);
  color: rgba(226, 232, 240, 0.86);
  padding: 5px 14px;
  font-size: 0.76rem;
  font-weight: 600;
}

.sprite-preview__origin-toggle.is-active {
  border-color: rgba(56, 189, 248, 0.6);
  background: rgba(56, 189, 248, 0.14);
  color: #f8fafc;
}

.sprite-preview__origin-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border: 1px solid rgba(56, 189, 248, 0.25);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  padding: 10px;
}

.sprite-preview__origin-inputs {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: center;
  gap: 8px;
}

.sprite-preview__origin-inputs label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.sprite-preview__origin-inputs input[type='number'] {
  width: 64px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.sprite-preview__origin-inputs button {
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 10px;
  background: rgba(215, 187, 132, 0.16);
  color: #f8fafc;
  padding: 7px 12px;
  font-size: 0.76rem;
  font-weight: 700;
}

.sprite-preview__origin-inputs button:hover {
  border-color: rgba(215, 187, 132, 0.85);
  background: rgba(215, 187, 132, 0.26);
}

.sprite-preview__gizmo-layers {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
}

.sprite-preview__gizmo-layers label,
.sprite-preview__gizmo-enable {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.74rem;
}

.sprite-preview__gizmo-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px solid rgba(148, 163, 184, 0.14);
}

.sprite-preview__gizmo-title {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: rgba(226, 232, 240, 0.72);
}
</style>
