<template>
  <button
    type="button"
    class="exit-button"
    :style="{ backgroundImage: `url(${exitUrl})` }"
    :aria-label="ariaLabel"
    @click="$emit('click', $event)"
  ></button>
</template>

<script setup lang="ts">
import exitUrl from '@/assets/ui/buttons/exit.png'

withDefaults(
  defineProps<{
    ariaLabel?: string
  }>(),
  { ariaLabel: 'Back' },
)

defineEmits<{ click: [MouseEvent] }>()
</script>

<style scoped>
.exit-button {
  width: 56px;
  height: 56px;
  padding: 0;
  border: 0;
  background-color: transparent;
  background-repeat: no-repeat;
  background-position: center;
  background-size: contain;
  image-rendering: pixelated;
  filter:
    drop-shadow(0 0 6px rgba(255, 255, 255, 0.85))
    drop-shadow(0 1px 1px rgba(0, 0, 0, 0.75))
    drop-shadow(0 3px 4px rgba(0, 0, 0, 0.5));
  transition: transform 120ms ease, filter 120ms ease;
}

.exit-button:hover,
.exit-button:focus-visible {
  transform: scale(1.08);
  filter:
    drop-shadow(0 0 8px rgba(255, 255, 255, 0.95))
    drop-shadow(0 1px 1px rgba(0, 0, 0, 0.75))
    drop-shadow(0 4px 5px rgba(0, 0, 0, 0.55))
    drop-shadow(0 0 10px rgba(255, 220, 140, 0.7));
  outline: none;
}

.exit-button:active {
  transform: scale(0.98);
}
</style>
