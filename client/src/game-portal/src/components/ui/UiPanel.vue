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
import worldMenuPanelUrl from '@/assets/ui/themes/updated/world-menu-panel.png'
import worldInnerPanelUrl from '@/assets/ui/themes/updated/world-inner-panel.png'
import warRoomInnerPanelUrl from '@/assets/ui/themes/updated/war-room/war-room-inner-panel.png'
import innerPanelUrl from '@/assets/ui/themes/updated/inner-panel.png'
import theme from '@/assets/ui/themes/default/theme.json'

const props = withDefaults(defineProps<{
  padding?: number
  variant?: 'default' | 'parchment' | 'footer' | 'worldMenu' | 'worldInner' | 'warRoomInner' | 'innerPanel'
  /**
   * How the 9-slice's edges and fill are laid down.
   * - `round` (default): tiled, so a wood grain or brass rivet keeps its scale
   *   however large the panel gets. Right for the framing panels.
   * - `stretch`: scaled to fit. Right when the fill is a continuous texture
   *   (parchment) and tiling would show a visible repeat seam.
   */
  repeat?: 'round' | 'stretch'
}>(), {
  padding: 12,
  variant: 'default',
  repeat: 'round',
})

const variants = {
  default: { image: uiPanelUrl, slice: theme.uiPanel.slice },
  parchment: { image: parchmentPanelUrl, slice: theme.parchmentPanel.slice },
  footer: { image: footerPanelUrl, slice: theme.footerPanel.slice },
  worldMenu: { image: worldMenuPanelUrl, slice: theme.worldMenuPanel.slice },
  worldInner: { image: worldInnerPanelUrl, slice: theme.worldInnerPanel.slice },
  warRoomInner: { image: warRoomInnerPanelUrl, slice: theme.warRoomInnerPanel.slice },
  innerPanel: { image: innerPanelUrl, slice: theme.innerPanel.slice },
}

const panelStyle = computed(() => {
  const v = variants[props.variant]
  return {
    '--ui-panel-image': `url(${v.image})`,
    '--ui-panel-slice': String(v.slice),
    '--ui-panel-repeat': props.repeat,
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
  border-image-repeat: var(--ui-panel-repeat, round);
  image-rendering: pixelated;
  box-sizing: border-box;
}
</style>
