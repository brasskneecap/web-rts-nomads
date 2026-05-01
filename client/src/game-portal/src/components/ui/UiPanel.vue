<template>
  <div class="ui-panel" :style="panelStyle">
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import uiPanelUrl from '@/assets/ui/ui_panel_56x56_slice17.png'

const props = withDefaults(defineProps<{
  padding?: number
}>(), {
  padding: 12,
})

const panelStyle = computed(() => ({
  '--ui-panel-image': `url(${uiPanelUrl})`,
  padding: `${props.padding}px`,
}))
</script>

<style scoped>
.ui-panel {
  background: none;
  border: 17px solid transparent;
  border-image-source: var(--ui-panel-image);
  border-image-slice: 17 fill;
  border-image-width: 17px;
  border-image-repeat: round;
  image-rendering: pixelated;
  box-sizing: border-box;
}
</style>
