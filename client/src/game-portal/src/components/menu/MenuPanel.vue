<template>
  <UiPanel class="menu-panel" :padding="16">
    <slot />
  </UiPanel>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'
</script>

<style scoped>
.menu-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 220px;
}
</style>
