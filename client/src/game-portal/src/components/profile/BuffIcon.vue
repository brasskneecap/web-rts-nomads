<template>
  <div
    class="buff-icon"
    :class="{ 'buff-icon--locked': locked }"
    :title="label"
    :aria-label="label"
    aria-hidden="false"
  >
    <img
      v-if="imgUrl"
      :src="imgUrl"
      :alt="label"
      class="buff-icon__img"
      draggable="false"
    />
    <div v-else class="buff-icon__fallback" :style="{ background: fallbackColor }">
      {{ fallbackLetter }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getActionIconImage } from '@/game/rendering/actionIconSprites'

const props = defineProps<{
  iconKey: string
  label: string
  locked?: boolean
}>()

// Resolve through the same action-icon sprite loader used by ActionIcon.vue.
// Returns null when no PNG exists for the key — the fallback renders instead.
const imgUrl = computed<string | null>(() => {
  const img = getActionIconImage(props.iconKey)
  if (!img) return null
  return img.src || null
})

const FALLBACK_COLORS = [
  '#5a6fa8',
  '#7a8a6a',
  '#8a5a5a',
  '#6a7a8a',
  '#7a6a8a',
  '#8a7a5a',
]

function hashCode(str: string): number {
  let h = 0
  for (let i = 0; i < str.length; i++) {
    h = (Math.imul(31, h) + str.charCodeAt(i)) | 0
  }
  return Math.abs(h)
}

const fallbackColor = computed(
  () => FALLBACK_COLORS[hashCode(props.iconKey) % FALLBACK_COLORS.length],
)

const fallbackLetter = computed(() => props.label.charAt(0).toUpperCase())
</script>

<style scoped>
.buff-icon {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid rgba(200, 164, 106, 0.3);
}

.buff-icon--locked {
  filter: grayscale(0.7) brightness(0.7);
}

.buff-icon__img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  image-rendering: pixelated;
  display: block;
}

.buff-icon__fallback {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 700;
  color: rgba(245, 234, 210, 0.9);
}
</style>
