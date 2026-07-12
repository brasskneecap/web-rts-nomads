<template>
  <div class="playtest-bar">
    <UiButton size="sm" @click="emit('stop')">■ Stop &amp; reset</UiButton>
    <span class="playtest-bar__label">Playtest (ephemeral — no rewards)</span>
  </div>
</template>
<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
const emit = defineEmits<{ stop: [] }>()
</script>
<style scoped>
.playtest-bar { position: absolute; top: 12px; left: 50%; transform: translateX(-50%);
  z-index: 30; display: flex; align-items: center; gap: 10px; padding: 6px 12px;
  background: rgba(3,8,14,0.92); border: 1px solid rgba(215,187,132,0.5); border-radius: 10px; color: #f4d27a; }
.playtest-bar__label { font-size: 0.75rem; }
</style>
