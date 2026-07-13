<template>
  <div class="playtest-bar">
    <UiButton size="sm" @click="emit('togglePause')">{{ paused ? '▶ Resume' : '⏸ Pause' }}</UiButton>
    <UiButton size="sm" @click="emit('reset')">↺ Reset</UiButton>
  </div>
</template>
<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
defineProps<{ paused: boolean }>()
const emit = defineEmits<{ togglePause: []; reset: [] }>()
</script>
<style scoped>
/* Sits below the top resources bar (MatchHud, ~58px tall). No background or
   border — the Pause/Reset buttons are self-explanatory. */
.playtest-bar {
  position: absolute;
  top: 68px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 30;
  display: flex;
  align-items: center;
  gap: 10px;
}
</style>
