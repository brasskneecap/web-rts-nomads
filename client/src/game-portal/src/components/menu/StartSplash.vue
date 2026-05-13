<template>
  <div
    class="start-splash"
    :class="{ 'start-splash--dismissing': dismissing }"
    :style="{ backgroundImage: `url(${mainMenuBackgroundUrl})` }"
    role="button"
    tabindex="0"
    aria-label="Click or press any key to begin"
    @pointerdown="dismiss"
    @keydown="dismiss"
  >
    <div class="start-splash__prompt">
      <span class="start-splash__prompt-text">Click anywhere to begin</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import mainMenuBackgroundUrl from '@/assets/background-images/main-menu.png'

const emit = defineEmits<{
  (e: 'dismiss'): void
}>()

const dismissing = ref(false)

function dismiss() {
  if (dismissing.value) return
  // Emit synchronously so the parent can start audio inside this user-gesture
  // event tick (required by browser autoplay policy).
  emit('dismiss')
  dismissing.value = true
}
</script>

<style scoped>
.start-splash {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding-bottom: 12vh;
  cursor: pointer;
  background-color: #05080d;
  background-size: cover;
  background-position: center;
  background-repeat: no-repeat;
  outline: none;
  transition: opacity 400ms ease-out;
}

.start-splash--dismissing {
  opacity: 0;
  pointer-events: none;
}

.start-splash__prompt {
  padding: 14px 28px;
  border-radius: 4px;
  background: rgba(8, 10, 16, 0.55);
  border: 1px solid rgba(200, 164, 106, 0.35);
  animation: start-splash-pulse 1.8s ease-in-out infinite;
}

.start-splash__prompt-text {
  font-size: 18px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.18em;
  text-transform: uppercase;
}

@keyframes start-splash-pulse {
  0%, 100% { opacity: 0.6; }
  50%      { opacity: 1; }
}
</style>
