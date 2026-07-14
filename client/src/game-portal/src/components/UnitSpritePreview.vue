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
        </div>

        <p v-if="fallbackNote" class="sprite-preview__note">{{ fallbackNote }}</p>

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
  getUnitFrame, getUnitSpriteSet, UNIT_DIRECTIONS,
  type UnitDirection, type UnitSpriteSet,
} from '@/game/rendering/unitSprites'
import { getUnitBoundsFor, type UnitAttackOrigin, type UnitOriginPoint } from '@/game/maps/unitDefs'
import {
  canvasToOrigin, deriveDefaultOrigin, originToCanvas, resolveFacingOrigin, unitAnchorCanvas,
  type PreviewDrawGeometry,
} from '@/game/units/attackOriginPreviewMath'
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
}>()
const emit = defineEmits<{ 'update:attackOrigin': [UnitAttackOrigin | undefined] }>()

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

const bounds = computed(() => getUnitBoundsFor({ path: props.pathKey, unitType: props.unitKey }))

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
  if (!showAttackOrigin.value) return
  const geo = lastGeometry.value
  const pt = canvasPointFromEvent(e)
  if (!geo || !pt) return
  isDraggingOrigin.value = true
  applyCanvasPoint(pt, geo)
  window.addEventListener('mousemove', onWindowMouseMove)
  window.addEventListener('mouseup', onWindowMouseUp)
}

function onWindowMouseMove(e: MouseEvent) {
  if (!isDraggingOrigin.value) return
  const geo = lastGeometry.value
  const pt = canvasPointFromEvent(e)
  if (!geo || !pt) return
  applyCanvasPoint(pt, geo)
}

function stopDragListeners() {
  window.removeEventListener('mousemove', onWindowMouseMove)
  window.removeEventListener('mouseup', onWindowMouseUp)
}

function onWindowMouseUp() {
  isDraggingOrigin.value = false
  stopDragListeners()
}

// Hover-only proximity check (not dragging) — purely a visual affordance so
// the marker highlights before you click, without ever touching `cursor:`
// (banned project-wide; see AI_RULES.md).
const HOVER_RADIUS_CANVAS_PX = 10
function onCanvasMouseMove(e: MouseEvent) {
  if (isDraggingOrigin.value) return
  if (!showAttackOrigin.value) { hoveringOrigin.value = false; return }
  const geo = lastGeometry.value
  const pt = canvasPointFromEvent(e)
  if (!geo || !pt) { hoveringOrigin.value = false; return }
  const anchor = unitAnchorCanvas(geo, bounds.value)
  const originPt = originToCanvas(currentOrigin.value, anchor, geo.scale)
  hoveringOrigin.value = Math.hypot(pt.x - originPt.x, pt.y - originPt.y) <= HOVER_RADIUS_CANVAS_PX
}
function onCanvasMouseLeave() {
  hoveringOrigin.value = false
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
watch(animation, () => { frame.value = 0 })
watch(frameCount, (fc) => { frame.value = frame.value % Math.max(1, fc) })

let raf = 0
let lastStep = 0
function tick(now: number) {
  raf = requestAnimationFrame(tick)
  if (playing.value && now - lastStep >= 1000 / Math.max(1, fps.value)) {
    lastStep = now
    frame.value = (frame.value + 1) % Math.max(1, frameCount.value)
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
  if (showAttackOrigin.value) drawAttackOriginOverlay(ctx, geo)
  if (fireTestActive.value) drawFireTestGhost(ctx, geo, now)
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
</style>
