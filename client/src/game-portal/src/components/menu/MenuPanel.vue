<template>
  <UiPanel class="menu-panel" :padding="16" :variant="variant">
    <slot />
  </UiPanel>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'

withDefaults(defineProps<{
  variant?: 'default' | 'parchment'
}>(), {
  variant: 'default',
})
</script>

<style scoped>
.menu-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 220px;
}
</style>
