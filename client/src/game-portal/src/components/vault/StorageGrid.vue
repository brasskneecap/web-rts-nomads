<template>
  <div class="storage" :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }">
    <div class="storage__label">Storage {{ items.length }} / {{ capacity }}</div>
    <!-- The grid is also a drop zone: dragging an equipped item back here
         unequips it into the vault. -->
    <div
      class="storage__grid"
      :class="{ 'storage__grid--drop-active': dragActive }"
      @dragover="onGridDragOver"
      @drop="onGridDrop"
    >
      <button
        v-for="cell in cells"
        :key="cell.key"
        type="button"
        class="storage__cell"
        :class="{
          'storage__cell--empty': !cell.item,
          'storage__cell--selected': cell.item && cell.item.instanceId === selectedInstanceId,
        }"
        :style="cell.item ? { '--tier-color': cell.item.tierColor } : {}"
        :disabled="!cell.item"
        :draggable="!!cell.item"
        :aria-label="cell.item ? cell.item.displayName : undefined"
        @click="cell.item ? emit('select', cell.item.instanceId) : undefined"
        @dragstart="cell.item ? onCellDragStart($event, cell.item) : undefined"
        @dragend="emit('item-dragend')"
      >
        <template v-if="cell.item">
          <ActionIcon
            class="storage__cell-icon"
            :action="{ id: cell.item.itemId, label: cell.item.displayName, iconDef: { kind: 'item', type: cell.item.itemId } }"
          />
          <span v-if="(cell.item.stacks ?? 1) > 1" class="storage__cell-stack">{{ cell.item.stacks }}</span>
        </template>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ActionIcon from '@/components/ActionIcon.vue'
import type { VaultStorageItem } from './types'

const props = defineProps<{
  items: VaultStorageItem[]
  capacity: number
  selectedInstanceId: number | null
  /** True while an equipped item is being dragged — the grid lights up as an
   *  unequip drop target. */
  dragActive: boolean
  iconContainerUrl: string
}>()

const emit = defineEmits<{
  select: [instanceId: number]
  'item-dragstart': [instanceId: number, itemId: string]
  'item-dragend': []
  'storage-drop': []
}>()

function onCellDragStart(e: DragEvent, item: VaultStorageItem) {
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

function onGridDragOver(e: DragEvent) {
  // Only relevant when unequipping (an equipped item dragged back here); the
  // parent ignores the drop unless the drag source is a unit slot.
  e.preventDefault()
}

function onGridDrop(e: DragEvent) {
  e.preventDefault()
  emit('storage-drop')
}

// Pad up to a stable grid so the column keeps its shape even when nearly empty.
const cells = computed(() => {
  const total = Math.max(props.capacity, props.items.length, 6)
  return Array.from({ length: total }, (_, i) => {
    const item = props.items[i] ?? null
    return { key: item ? `item-${item.instanceId}` : `empty-${i}`, item }
  })
})
</script>

<style scoped>
.storage {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.storage__label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(212, 168, 79, 0.7);
}

.storage__grid {
  display: grid;
  grid-template-columns: repeat(4, 64px);
  gap: 8px;
  border-radius: 6px;
}

.storage__grid--drop-active {
  outline: 2px dashed rgba(96, 165, 250, 0.5);
  outline-offset: 4px;
}

.storage__cell {
  position: relative;
  width: 64px;
  height: 64px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  border: 2px solid transparent;
  border-radius: 0;
  padding: 0;
  box-sizing: border-box;
  transition: box-shadow 0.15s;
}

.storage__cell:not(.storage__cell--empty) {
  box-shadow: inset 0 0 0 2px var(--tier-color, transparent);
}

.storage__cell:not(.storage__cell--empty):hover {
  box-shadow:
    inset 0 0 0 2px var(--tier-color, transparent),
    var(--ui-hover-glow);
}

.storage__cell--selected {
  box-shadow:
    inset 0 0 0 2px var(--tier-color, #9ca3af),
    0 0 10px var(--tier-color, #9ca3af);
}

.storage__cell--empty {
  cursor: inherit;
}

.storage__cell:disabled {
  opacity: 1;
}

.storage__cell-icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.storage__cell-stack {
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

</style>
