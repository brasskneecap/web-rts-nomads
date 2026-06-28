<template>
  <div class="perk-row" :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }">
    <div
      v-for="perk in perks"
      :key="perk.id"
      class="perk-cell"
      @mouseenter="onEnter($event, perk)"
      @mouseleave="onLeave"
    >
      <ActionIcon class="perk-cell__icon" :action="{ id: perk.iconId, label: perk.title }" />
    </div>
    <div v-if="perks.length === 0" class="perk-row__empty">No perks yet</div>
  </div>

  <!-- Teleported to <body> so it floats above panels and is never clipped by
       the unit list's scroll container. -->
  <Teleport to="body">
    <div v-if="hovered" class="perk-tooltip" :style="tooltipStyle">
      <div class="perk-tooltip__title">{{ hovered.title }}</div>
      <div v-if="hovered.body" class="perk-tooltip__body">{{ hovered.body }}</div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import ActionIcon from '@/components/ActionIcon.vue'
import type { VaultPerkChip } from './types'

defineProps<{
  perks: VaultPerkChip[]
  iconContainerUrl: string
}>()

const hovered = ref<VaultPerkChip | null>(null)
const anchorRect = ref<DOMRect | null>(null)

function onEnter(e: MouseEvent, perk: VaultPerkChip) {
  anchorRect.value = (e.currentTarget as HTMLElement).getBoundingClientRect()
  hovered.value = perk
}

function onLeave() {
  hovered.value = null
}

const tooltipStyle = computed(() => {
  const r = anchorRect.value
  if (!r) return {}
  return {
    left: `${r.left + r.width / 2}px`,
    top: `${r.top - 8}px`,
    transform: 'translate(-50%, -100%)',
  }
})
</script>

<style scoped>
.perk-row {
  display: flex;
  gap: 5px;
  align-items: center;
}

.perk-cell {
  position: relative;
  width: 34px;
  height: 34px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  flex: 0 0 auto;
}

.perk-cell__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 72%;
  height: 72%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.perk-row__empty {
  font-size: 11px;
  color: rgba(232, 217, 184, 0.4);
  font-style: italic;
}

/* Teleported to <body>: fixed to the viewport, positioned over the hovered
   perk, floating above all panels (mirrors the SelectionHud perk tooltip). */
.perk-tooltip {
  position: fixed;
  z-index: 1000;
  min-width: 150px;
  max-width: 240px;
  padding: 7px 10px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(34, 22, 10, 0.98), rgba(20, 12, 4, 0.98));
  border: 1px solid rgba(200, 164, 106, 0.45);
  color: #f5ead2;
  text-align: left;
  pointer-events: none;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
}

.perk-tooltip__title {
  font-size: 12px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 3px;
  line-height: 1.45;
}

.perk-tooltip__body {
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.45;
  white-space: pre-line;
}
</style>
