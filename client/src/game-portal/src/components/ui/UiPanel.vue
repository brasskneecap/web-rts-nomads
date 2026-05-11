<template>
  <div class="ui-panel" :style="panelStyle">
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import uiPanelUrl from '@/assets/ui/themes/default/ui_panel.png'
import theme from '@/assets/ui/themes/default/theme.json'

const props = withDefaults(defineProps<{
  padding?: number
}>(), {
  padding: 12,
})

const panelStyle = computed(() => ({
  '--ui-panel-image': `url(${uiPanelUrl})`,
  '--ui-panel-slice': String(theme.uiPanel.slice),
  padding: `${props.padding}px`,
}))
</script>

<style scoped>
.ui-panel {
  background: none;
  border: calc(var(--ui-panel-slice) * 1px) solid transparent;
  border-image-source: var(--ui-panel-image);
  border-image-slice: var(--ui-panel-slice) fill;
  border-image-width: calc(var(--ui-panel-slice) * 1px);
  border-image-repeat: round;
  image-rendering: pixelated;
  box-sizing: border-box;
}
</style>
