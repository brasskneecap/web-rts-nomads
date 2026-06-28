<template>
  <div class="inv" :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }">
    <div
      v-for="slot in slots"
      :key="slot.rank"
      class="inv__slot"
      :class="{
        'inv__slot--locked': slot.locked,
        'inv__slot--filled': !!slot.item,
        'inv__slot--drop-target': isDropTarget(slot),
      }"
      :aria-label="slotAriaLabel(slot)"
      :draggable="!slot.locked && !!slot.item"
      @dragstart="onDragStart($event, slot)"
      @dragend="onDragEnd(slot)"
      @dragover="onDragOver($event, slot)"
      @drop="onDrop($event, slot)"
      @mouseenter="onSlotEnter($event, slot)"
      @mouseleave="onSlotLeave"
    >
      <template v-if="slot.locked">
        <ActionIcon class="inv__lock" :action="{ id: 'lock', label: 'Locked' }" />
      </template>
      <template v-else-if="slot.item">
        <ActionIcon
          class="inv__icon"
          :action="{ id: slot.item.itemId, label: slot.item.displayName, iconDef: { kind: 'item', type: slot.item.itemId } }"
        />
      </template>
    </div>
  </div>

  <!-- Tooltip is teleported to <body> so it floats above every panel and is
       never clipped by the unit list's scroll container. Positioned over the
       hovered slot via its captured viewport rect. -->
  <Teleport to="body">
    <div v-if="hovered" class="inv-tooltip" :style="tooltipStyle">
      <div class="inv-tooltip__title">{{ hovered.item!.displayName }}</div>
      <div v-if="hovered.item!.tier" class="inv-tooltip__tier" :style="{ color: hovered.item!.tierColor }">
        {{ capitalize(hovered.item!.tier!) }}
      </div>
      <div v-if="hovered.item!.tooltipBody" class="inv-tooltip__body">{{ hovered.item!.tooltipBody }}</div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import ActionIcon from '@/components/ActionIcon.vue'
import type { VaultInventorySlot } from './types'

const props = defineProps<{
  slots: VaultInventorySlot[]
  /** True while a compatible item is being dragged over this unit — empty
   *  unlocked slots light up as drop targets. */
  acceptsDrop: boolean
  iconContainerUrl: string
}>()

const emit = defineEmits<{
  'slot-dragstart': [slotIndex: number]
  'slot-dragend': []
  'slot-drop': [slotIndex: number]
}>()

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

// ── Floating tooltip (teleported to body) ───────────────────────────────────
const hovered = ref<VaultInventorySlot | null>(null)
const anchorRect = ref<DOMRect | null>(null)

function onSlotEnter(e: MouseEvent, slot: VaultInventorySlot) {
  if (!slot.item) {
    hovered.value = null
    return
  }
  anchorRect.value = (e.currentTarget as HTMLElement).getBoundingClientRect()
  hovered.value = slot
}

function onSlotLeave() {
  hovered.value = null
}

const tooltipStyle = computed(() => {
  const r = anchorRect.value
  if (!r) return {}
  // Centered above the slot; translate up by its own height via transform.
  return {
    left: `${r.left + r.width / 2}px`,
    top: `${r.top - 8}px`,
    transform: 'translate(-50%, -100%)',
  }
})

function isDropTarget(slot: VaultInventorySlot): boolean {
  return props.acceptsDrop && !slot.locked && !slot.item
}

function slotAriaLabel(slot: VaultInventorySlot): string {
  if (slot.locked) return 'Locked slot'
  if (slot.item) return `Slot: ${slot.item.displayName}`
  return 'Empty slot'
}

function onDragStart(e: DragEvent, slot: VaultInventorySlot) {
  if (slot.locked || !slot.item) return
  e.dataTransfer?.setData('text/plain', String(slot.slotIndex))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  setIconDragImage(e)
  emit('slot-dragstart', slot.slotIndex)
}

// Use just the item icon (the ActionIcon canvas) as the drag image instead of
// the whole slot frame + tooltip.
function setIconDragImage(e: DragEvent) {
  const canvas = (e.currentTarget as HTMLElement | null)?.querySelector('canvas')
  if (canvas && e.dataTransfer) {
    const r = canvas.getBoundingClientRect()
    e.dataTransfer.setDragImage(canvas, r.width / 2, r.height / 2)
  }
}

function onDragEnd(slot: VaultInventorySlot) {
  if (slot.locked) return
  emit('slot-dragend')
}

function onDragOver(e: DragEvent, slot: VaultInventorySlot) {
  if (slot.locked) return
  // Permit a drop so @drop fires; the parent validates and rejects overwrites.
  e.preventDefault()
}

function onDrop(e: DragEvent, slot: VaultInventorySlot) {
  if (slot.locked) return
  e.preventDefault()
  emit('slot-drop', slot.slotIndex)
}
</script>

<style scoped>
.inv {
  display: flex;
  gap: 6px;
  flex: 0 0 auto;
}

/* Uniform slot frame — every slot looks identical regardless of rank. */
.inv__slot {
  position: relative;
  width: 52px;
  height: 52px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  border: 2px solid transparent;
  border-radius: 0;
  box-shadow: inset 0 0 0 2px rgba(212, 168, 79, 0.25);
  box-sizing: border-box;
  flex: 0 0 auto;
}

.inv__slot--filled {
  box-shadow: inset 0 0 0 2px rgba(212, 168, 79, 0.55);
}

.inv__slot--locked {
  filter: grayscale(0.7) brightness(0.55);
  box-shadow: inset 0 0 0 2px rgba(120, 120, 120, 0.35);
}

.inv__slot--drop-target {
  animation: inv-target-pulse 1.2s ease-in-out infinite;
}

@keyframes inv-target-pulse {
  0%, 100% { box-shadow: inset 0 0 0 2px rgba(96, 165, 250, 0.5), 0 0 4px rgba(96, 165, 250, 0.4); }
  50%      { box-shadow: inset 0 0 0 2px rgba(96, 165, 250, 0.95), 0 0 10px rgba(96, 165, 250, 0.95); }
}

.inv__icon,
.inv__lock {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

/* Teleported to <body>: fixed to the viewport, positioned over the hovered
   slot, floating above all panels. */
.inv-tooltip {
  position: fixed;
  z-index: 1000;
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

.inv-tooltip__title {
  font-size: 12px;
  font-weight: 700;
  color: #f5e4c0;
  margin-bottom: 2px;
}

.inv-tooltip__tier {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  margin-bottom: 3px;
}

.inv-tooltip__body {
  font-size: 11px;
  color: rgba(232, 217, 184, 0.75);
  line-height: 1.4;
}
</style>
