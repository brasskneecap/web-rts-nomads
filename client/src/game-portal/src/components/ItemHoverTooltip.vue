<template>
  <!-- Teleported to <body> so it floats above every panel and is never
       clipped by a scroll container. Positioned centered above the hovered
       element via its captured viewport rect. Shared by the vault storage
       grid, the vault unit-card inventory slots, and the SelectionHud
       inventory so item hover reads identically everywhere. -->
  <Teleport to="body">
    <div v-if="item && anchor" class="item-tooltip" :style="style">
      <div class="item-tooltip__title">{{ item.displayName }}</div>
      <div
        v-if="item.tier"
        class="item-tooltip__tier"
        :style="{ color: item.tierColor }"
      >{{ capitalize(item.tier) }}</div>
      <div v-if="item.body" class="item-tooltip__body">{{ item.body }}</div>
      <div v-if="item.hint" class="item-tooltip__hint">{{ item.hint }}</div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'

export interface ItemTooltipData {
  displayName: string
  /** Item tier name ("common", "rare", …) shown under the title. */
  tier?: string
  /** Accent color for the tier line (see TIER_COLORS in itemRules). */
  tierColor?: string
  /** Stat / description block (buildItemTooltipBody output). */
  body?: string
  /** Interaction hint ("Click to use", "Click to unequip", …). */
  hint?: string
}

const props = defineProps<{
  /** Item to describe, or null when nothing is hovered. */
  item: ItemTooltipData | null
  /** Viewport rect of the hovered element the tooltip anchors above. */
  anchor: DOMRect | null
}>()

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

const style = computed(() => {
  const r = props.anchor
  if (!r) return {}
  return {
    left: `${r.left + r.width / 2}px`,
    top: `${r.top - 8}px`,
    transform: 'translate(-50%, -100%)',
  }
})
</script>

<style scoped>
.item-tooltip {
  position: fixed;
  z-index: var(--z-tooltip, 10000);
  min-width: 130px;
  max-width: 220px;
  background: rgba(10, 12, 20, 0.97);
  border: 1px solid rgba(212, 168, 79, 0.4);
  border-radius: 6px;
  padding: 7px 10px;
  pointer-events: none;
  white-space: normal;
  text-align: left;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
}

.item-tooltip__title {
  font-size: 12px;
  font-weight: 700;
  color: #f5e4c0;
  margin-bottom: 2px;
}

.item-tooltip__tier {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  margin-bottom: 3px;
}

.item-tooltip__body {
  font-size: 11px;
  color: rgba(232, 217, 184, 0.75);
  line-height: 1.4;
  white-space: pre-line;
}

.item-tooltip__hint {
  margin-top: 4px;
  font-size: 10px;
  font-style: italic;
  color: rgba(232, 217, 184, 0.55);
}
</style>
