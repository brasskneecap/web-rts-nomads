<template>
  <canvas ref="canvasEl" :width="size" :height="size" class="anim-ref-canvas" />
</template>

<script setup lang="ts">
// AnimationRefCanvas: plays an animation reference (effect / projectile / beam /
// object@state) on a RAF loop via the SHARED drawAnimationDecal — the same
// resolver the in-game decal renderer uses — so a picker thumbnail / preview
// shows exactly what will render in the match. Auto-fits the frame into the
// canvas. Stops its RAF loop on unmount.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { drawAnimationDecal, resolveAnimationFrames } from '@/game/rendering/animationRef'

const props = withDefaults(
  defineProps<{
    /** Animation scheme string, e.g. "object:caltrops@electrified". */
    animation?: string
    /** Canvas backing-store size in px. CSS scales it to the container. */
    size?: number
    padding?: number
  }>(),
  { size: 64, padding: 4 },
)

const canvasEl = ref<HTMLCanvasElement | null>(null)
let raf = 0

function render() {
  const c = canvasEl.value
  if (!c) return
  const ctx = c.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, props.size, props.size)
  if (!props.animation) return
  const frames = resolveAnimationFrames(props.animation)
  if (!frames) return
  // Fit the frame into the padded box; drawAnimationDecal scales frameWidth by
  // this factor, so derive it from the frame geometry (0 while still decoding —
  // drawAnimationDecal registers its own onReady redraw in that case).
  const box = props.size - props.padding * 2
  const longest = Math.max(frames.frameWidth, frames.frameHeight)
  const scale = longest > 0 ? box / longest : 1
  drawAnimationDecal(ctx, props.animation, props.size / 2, props.size / 2, scale, performance.now(), render)
}

function loop() {
  render()
  raf = requestAnimationFrame(loop)
}

onMounted(() => {
  // jsdom (unit tests) has no 2D context — stay inert rather than spinning RAF.
  if (!canvasEl.value?.getContext('2d')) return
  loop()
})
onBeforeUnmount(() => cancelAnimationFrame(raf))
watch(() => [props.animation, props.size], render)
</script>

<style scoped>
.anim-ref-canvas {
  display: block;
  width: 100%;
  height: 100%;
  image-rendering: pixelated;
}
</style>
