<template>
  <div class="bag" :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }">
    <div class="bag__label">Items {{ totalCount }}</div>
    <!-- Consumable "bag" items. Drag one onto a unit card to apply it directly
         to that unit (full effect, one stack). Dropping anywhere else cancels
         and the item stays here. Not a drop target itself. -->
    <div class="bag__grid">
      <button
        v-for="item in items"
        :key="`bag-${item.instanceId}`"
        type="button"
        class="bag__cell"
        :style="{ '--tier-color': item.tierColor }"
        :aria-label="item.displayName"
        draggable="true"
        @dragstart="onCellDragStart($event, item)"
        @dragend="emit('item-dragend')"
        @mouseenter="onCellEnter($event, item)"
        @mouseleave="onCellLeave"
      >
        <ActionIcon
          class="bag__cell-icon"
          :action="{ id: item.itemId, label: item.displayName, iconDef: { kind: 'item', type: item.itemId } }"
        />
        <span v-if="(item.stacks ?? 1) > 1" class="bag__cell-stack">{{ item.stacks }}</span>
      </button>
      <div v-if="items.length === 0" class="bag__empty">No consumables in your bag.</div>
    </div>
  </div>

  <ItemHoverTooltip :item="hoveredTooltip" :anchor="anchorRect" />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import ActionIcon from '@/components/ActionIcon.vue'
import ItemHoverTooltip, { type ItemTooltipData } from '@/components/ItemHoverTooltip.vue'
import type { VaultStorageItem } from './types'

const props = defineProps<{
  items: VaultStorageItem[]
  iconContainerUrl: string
}>()

const emit = defineEmits<{
  'item-dragstart': [instanceId: number, itemId: string]
  'item-dragend': []
}>()

const totalCount = computed(() =>
  props.items.reduce((sum, it) => sum + (it.stacks ?? 1), 0),
)

// ── Floating tooltip (shared ItemHoverTooltip, teleported to body) ──────────
const hoveredItem = ref<VaultStorageItem | null>(null)
const anchorRect = ref<DOMRect | null>(null)

const hoveredTooltip = computed<ItemTooltipData | null>(() => {
  const item = hoveredItem.value
  if (!item) return null
  return {
    displayName: item.displayName,
    tier: item.tier,
    tierColor: item.tierColor,
    body: item.tooltipBody,
    hint: 'Drag onto a unit to use',
  }
})

function onCellEnter(e: MouseEvent, item: VaultStorageItem) {
  anchorRect.value = (e.currentTarget as HTMLElement).getBoundingClientRect()
  hoveredItem.value = item
}

function onCellLeave() {
  hoveredItem.value = null
}

function onCellDragStart(e: DragEvent, item: VaultStorageItem) {
  // Hide the tooltip while dragging — it would trail the pointer otherwise.
  hoveredItem.value = null
  e.dataTransfer?.setData('text/plain', String(item.instanceId))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  // Drag just the item icon (the ActionIcon canvas), not the whole cell frame.
  const canvas = (e.currentTarget as HTMLElement | null)?.querySelector('canvas')
  if (canvas && e.dataTransfer) {
    const r = canvas.getBoundingClientRect()
    e.dataTransfer.setDragImage(canvas, r.width / 2, r.height / 2)
  }
  emit('item-dragstart', item.instanceId, item.itemId)
}
</script>

<style scoped>
.bag {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.bag__label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(212, 168, 79, 0.7);
}

.bag__grid {
  display: grid;
  grid-template-columns: repeat(4, 64px);
  gap: 8px;
  border-radius: 6px;
}

.bag__cell {
  position: relative;
  width: 64px;
  height: 64px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  border: 2px solid transparent;
  border-radius: 0;
  padding: 0;
  box-sizing: border-box;
  box-shadow: inset 0 0 0 2px var(--tier-color, transparent);
  transition: box-shadow 0.15s;
}

.bag__cell:hover {
  box-shadow:
    inset 0 0 0 2px var(--tier-color, transparent),
    var(--ui-hover-glow);
}

.bag__cell-icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.bag__cell-stack {
  position: absolute;
  bottom: 2px;
  right: 3px;
  font-size: 10px;
  font-weight: 700;
  color: #fff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.8);
  pointer-events: none;
  line-height: 1;
}

.bag__empty {
  grid-column: 1 / -1;
  font-size: 11px;
  color: rgba(232, 217, 184, 0.5);
  padding: 6px 0;
}
</style>
