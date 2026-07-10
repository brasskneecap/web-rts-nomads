<template>
  <button type="button" class="cg-back" :style="vars" @click="$emit('click')">
    <span class="cg-back__label"><slot>Back</slot></span>
  </button>
</template>

<script setup lang="ts">
// Shared Back button for the Custom Game tabs and lobby — the dark
// inactive-button art with the standard hover/press feedback. Emits `click`;
// inherits --s from its container for sizing.
import inactiveBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-inactive-button.png'

defineEmits<{ (e: 'click'): void }>()

const vars = { '--cg-back-img': `url(${inactiveBtnUrl})` }
</script>

<style scoped>
.cg-back {
  flex: 0 0 auto;
  min-width: calc(var(--s) * 150);
  padding: calc(var(--s) * 4) calc(var(--s) * 16);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: calc(var(--s) * 16) solid transparent;
  border-image-source: var(--cg-back-img);
  border-image-slice: 14 fill;
  border-image-width: calc(var(--s) * 16);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

.cg-back__label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #e0cfa4;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
}

.cg-back:hover {
  filter: brightness(1.12);
}

.cg-back:active {
  filter: brightness(0.9);
  transform: translateY(1px);
}
</style>
