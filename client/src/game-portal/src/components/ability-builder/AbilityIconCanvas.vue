<template>
  <canvas ref="canvasEl" :width="size" :height="size" class="ability-icon-canvas" />
</template>

<script setup lang="ts">
// AbilityIconCanvas: renders an ability icon via the SHARED drawAbilityIcon —
// the same code the in-game action bar (ActionIcon.vue) uses — so the editor's
// preview and picker show exactly what the action bar will. Draws whatever the
// `icon` scheme resolves to (effect frame / projectile frame / bundled/uploaded
// key), re-rendering when the image decodes or any input changes.
import { onMounted, ref, watch } from 'vue'
import { drawAbilityIcon } from '@/game/rendering/abilityIconRender'

const props = withDefaults(
  defineProps<{
    icon?: string
    abilityId?: string
    projectile?: string
    /** Canvas backing-store size in px. CSS scales it to the container. */
    size?: number
  }>(),
  { size: 64 },
)

const canvasEl = ref<HTMLCanvasElement | null>(null)

function render() {
  const c = canvasEl.value
  if (!c) return
  const ctx = c.getContext('2d')
  if (!ctx) return
  // onReady === render so a still-decoding image repaints itself once loaded.
  drawAbilityIcon(ctx, props.size, { icon: props.icon, abilityId: props.abilityId, projectile: props.projectile }, render)
}

onMounted(render)
watch(() => [props.icon, props.abilityId, props.projectile, props.size], render)
</script>

<style scoped>
.ability-icon-canvas {
  display: block;
  width: 100%;
  height: 100%;
  image-rendering: pixelated;
}
</style>
