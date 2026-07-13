<template>
  <div class="sprite-preview">
    <template v-if="hasArt">
      <div class="sprite-preview__main">
        <canvas
          ref="mainCanvas"
          :width="MAIN_BOX"
          :height="MAIN_BOX"
          class="sprite-preview__canvas"
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

const props = defineProps<{ unitKey?: string; pathKey?: string }>()

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
  drawMain()
}
raf = requestAnimationFrame(tick)
onBeforeUnmount(() => cancelAnimationFrame(raf))

// Draws one animation/direction/frame into a square canvas box, fit at the
// largest integer scale and centered. Never reimplements srcX/srcY math —
// that lives entirely in getUnitFrame. A null return (art missing / not yet
// decoded) just leaves the canvas cleared instead of throwing or drawing
// stale content.
function drawFrame(canvas: HTMLCanvasElement | null, animName: string, dir: UnitDirection, frameIndex: number, box: number) {
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, box, box)
  const s = set.value
  if (!s) return
  const drawable = getUnitFrame(s, animName, dir, frameIndex)
  if (!drawable) return
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
}

function drawMain() {
  // For the rotations option, pass '' so getUnitFrame falls straight through to
  // the idle rotation sheet regardless of what the animation is named. For a
  // real animation, pass its name.
  const animArg = isRotations.value ? '' : animation.value
  drawFrame(mainCanvas.value, animArg, direction.value, frame.value, MAIN_BOX)
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
</style>
