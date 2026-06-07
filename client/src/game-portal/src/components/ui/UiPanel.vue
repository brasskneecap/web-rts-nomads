<template>
  <div class="ui-panel" :style="panelStyle">
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import uiPanelUrl from '@/assets/ui/themes/default/ui_panel.png'
import parchmentPanelUrl from '@/assets/ui/themes/default/ui_parchment_panel.png'
import footerPanelUrl from '@/assets/ui/themes/default/footer_panel.png'
import theme from '@/assets/ui/themes/default/theme.json'

const props = withDefaults(defineProps<{
  padding?: number
  variant?: 'default' | 'parchment' | 'footer'
}>(), {
  padding: 12,
  variant: 'default',
})

const variants = {
  default: { image: uiPanelUrl, slice: theme.uiPanel.slice },
  parchment: { image: parchmentPanelUrl, slice: theme.parchmentPanel.slice },
  footer: { image: footerPanelUrl, slice: theme.footerPanel.slice },
}

const panelStyle = computed(() => {
  const v = variants[props.variant]
  return {
    '--ui-panel-image': `url(${v.image})`,
    '--ui-panel-slice': String(v.slice),
    padding: `${props.padding}px`,
  }
})
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
