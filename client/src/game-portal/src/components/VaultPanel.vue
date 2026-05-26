<template>
  <div
    class="vault-panel"
    :class="{
      'vault-panel--collapsed': collapsed,
      'vault-panel--dragging': !embedded && drag.dragging.value,
      'vault-panel--embedded': embedded,
    }"
    :style="embedded ? undefined : drag.style.value"
    role="dialog"
    aria-label="Vault"
  >
    <!-- Drag handle / header (hidden when embedded inside another panel) -->
    <header
      v-if="!embedded"
      class="vault-head"
      :class="{ 'vault-head--dragging': drag.dragging.value }"
      v-bind="drag.handleBindings"
    >
      <span class="vault-grip" aria-hidden="true">⋮⋮</span>
      <button
        class="vault-toggle"
        type="button"
        :aria-expanded="!collapsed"
        :title="collapsed ? 'Expand Vault' : 'Collapse Vault'"
        @click="collapsed = !collapsed"
      >
        <span class="vault-chevron" :class="{ open: !collapsed }">▾</span>
        <span class="vault-title">Vault ({{ vaultCount }}/{{ vaultCapacity }})</span>
      </button>
      <button
        v-if="onClose"
        class="vault-close"
        type="button"
        title="Close Vault"
        @click="onClose && onClose()"
      >✕</button>
    </header>

    <div v-if="embedded || !collapsed" class="vault-body">
      <div class="vault-columns">
        <!-- Left column: vault item grid -->
        <div class="vault-grid-col">
          <div class="vault-col-label">Storage {{ vaultCount }} / {{ vaultCapacity }}</div>
          <div
            class="vault-grid"
            :class="{ 'vault-grid--drop-active': dragSource?.kind === 'unit-slot' && vaultGridDragOver }"
            @dragover.prevent="onVaultGridDragOver"
            @dragleave="onVaultGridDragLeave"
            @drop="onVaultGridDrop"
          >
            <button
              v-for="cell in vaultCells"
              :key="cell.key"
              type="button"
              class="vault-cell"
              :class="{
                'vault-cell--empty': !cell.item,
                'vault-cell--selected': cell.item && cell.item.instanceId === vaultSelectedInstanceId,
                'vault-cell--drag-source': dragSource?.kind === 'vault' && cell.item !== null && dragSource.instanceId === cell.item.instanceId,
              }"
              :style="cell.item ? { '--tier-color': tierColor(cell.item.tier) } : {}"
              :disabled="!cell.item"
              :draggable="cell.item !== null ? 'true' : 'false'"
              @click="cell.item ? onVaultCellClick(cell.item.instanceId) : undefined"
              @dragstart="cell.item ? onVaultCellDragStart($event, cell.item.instanceId, cell.item.itemId) : undefined"
              @dragend="onDragEnd"
            >
              <template v-if="cell.item">
                <ActionIcon
                  class="vault-cell__icon"
                  :action="{ id: cell.item.itemId, label: cell.item.displayName, iconDef: { kind: 'item', type: cell.item.itemId } }"
                />
                <span
                  v-if="(cell.item.stacks ?? 1) > 1"
                  class="vault-cell__stack"
                >{{ cell.item.stacks }}</span>
                <div class="vault-cell-tooltip">
                  <div class="vault-cell-tooltip__title">{{ cell.item.displayName }}</div>
                  <div v-if="cell.item.tier" class="vault-cell-tooltip__tier">{{ cell.item.tier.charAt(0).toUpperCase() + cell.item.tier.slice(1) }}</div>
                  <div v-if="cell.item.tooltipBody" class="vault-cell-tooltip__body">{{ cell.item.tooltipBody }}</div>
                </div>
              </template>
            </button>
          </div>
          <div v-if="vaultSelectedInstanceId !== null" class="vault-selection-hint">
            Click a unit slot to equip
          </div>
        </div>

        <!-- Right column: unit list -->
        <div class="vault-units-col">
          <div class="vault-col-label">Units</div>
          <div v-if="inventoryUnits.length === 0" class="vault-empty-units">
            No units with inventory slots.
          </div>
          <div v-else class="vault-units-list">
            <div
              v-for="unitRow in inventoryUnits"
              :key="unitRow.id"
              class="vault-unit-row"
            >
              <div class="vault-unit-header">
                <div class="vault-unit-portrait">
                  <img
                    v-if="unitRow.portraitUrl"
                    :src="unitRow.portraitUrl"
                    :alt="unitRow.name"
                    draggable="false"
                  />
                  <span v-else class="vault-unit-portrait__fallback">{{ unitRow.initials }}</span>
                </div>
                <div class="vault-unit-info">
                  <span class="vault-unit-name">{{ unitRow.name }}</span>
                  <span class="vault-unit-hp">{{ unitRow.hp }} / {{ unitRow.maxHp }}</span>
                </div>
              </div>
              <div class="vault-unit-slots">
                <button
                  v-for="slot in unitRow.slots"
                  :key="slot.index"
                  type="button"
                  class="vault-unit-slot"
                  :class="{
                    'vault-unit-slot--occupied': slot.occupied,
                    'vault-unit-slot--equip-target': !slot.occupied && vaultSelectedInstanceId !== null && dragSource === null,
                    'vault-unit-slot--drag-source': dragSource?.kind === 'unit-slot' && dragSource.unitId === unitRow.id && dragSource.slotIndex === slot.index,
                    'vault-unit-slot--drop-valid': dragSource !== null && !slot.occupied && isDropValid(unitRow.unitType),
                    'vault-unit-slot--drop-invalid': dragSource !== null && slot.occupied && !(dragSource.kind === 'unit-slot' && dragSource.unitId === unitRow.id && dragSource.slotIndex === slot.index),
                  }"
                  :style="slot.occupied && slot.tierColor ? { '--tier-color': slot.tierColor } : {}"
                  :title="slot.tooltip"
                  :draggable="slot.occupied && slot.itemId !== null ? 'true' : 'false'"
                  @click="onUnitSlotClick(unitRow.id, slot)"
                  @dragstart="slot.occupied && slot.itemId !== null && slot.instanceId !== null ? onUnitSlotDragStart($event, unitRow.id, slot.index, slot.instanceId, slot.itemId!) : undefined"
                  @dragend="onDragEnd"
                  @dragover.prevent
                  @drop="onUnitSlotDrop($event, unitRow.id, slot.index, slot)"
                >
                  <template v-if="slot.occupied && slot.itemId">
                    <ActionIcon
                      class="vault-unit-slot__icon"
                      :action="{ id: slot.itemId, label: slot.displayName, iconDef: { kind: 'item', type: slot.itemId } }"
                    />
                  </template>
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { VaultItemSnapshot } from '@/game/network/protocol'
import type { Unit } from '@/game/core/GameState'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'
import { TIER_COLORS, buildItemTooltipBody } from '@/game/items/itemRules'
import { getUnitPortraitUrl } from '@/game/rendering/unitSprites'
import { useDraggablePanel } from '@/composables/useDraggablePanel'
import ActionIcon from '@/components/ActionIcon.vue'

const props = withDefaults(defineProps<{
  vault: VaultItemSnapshot[]
  vaultCapacity: number
  vaultSelectedInstanceId: number | null
  units: Unit[]
  onSelectVaultItem: (instanceId: number | null) => void
  onUnequipItem: (unitId: number, slotIndex: number) => void
  onEquipItem: (unitId: number, slotIndex: number, instanceId: number) => void
  onUseConsumable: (unitId: number, slotIndex: number) => void
  onTransferItem: (fromUnitId: number, fromSlotIdx: number, toUnitId: number, toSlotIdx: number) => void
  onClose?: () => void
  /**
   * When true, render only the body (no drag handle, no header chrome, no
   * absolute positioning, no panel background). Used when the vault is
   * embedded inside another container such as the MatchMenu Vault tab.
   */
  embedded?: boolean
}>(), {
  embedded: false,
})

// ── Panel drag (header) ────────────────────────────────────────────────────
const collapsed = ref(false)
const drag = useDraggablePanel('vault-panel')

// ── HTML5 drag-and-drop state ──────────────────────────────────────────────
type DragSource =
  | { kind: 'vault'; instanceId: number; itemId: string }
  | { kind: 'unit-slot'; unitId: number; slotIndex: number; instanceId: number; itemId: string }

const dragSource = ref<DragSource | null>(null)
const vaultGridDragOver = ref(false)

// ── Derived counts ─────────────────────────────────────────────────────────
const vaultCount = computed(() => props.vault.length)

function tierColor(tier: string | undefined): string {
  if (!tier) return TIER_COLORS.common
  return TIER_COLORS[tier as keyof typeof TIER_COLORS] ?? TIER_COLORS.common
}

// Build vault cell data. Always show at least vaultCapacity cells.
const vaultCells = computed(() => {
  const totalCells = Math.max(props.vaultCapacity, props.vault.length, 6)
  return Array.from({ length: totalCells }, (_, i) => {
    const snapshot = props.vault[i] ?? null
    if (!snapshot) {
      return { key: `empty-${i}`, item: null }
    }
    const def = ITEM_DEF_MAP.get(snapshot.itemId)
    const displayName = def?.displayName ?? snapshot.itemId
    const tier = def?.tier
    const tooltipBody = def ? buildItemTooltipBody(def) : ''
    return {
      key: `item-${snapshot.instanceId}`,
      item: {
        instanceId: snapshot.instanceId,
        itemId: snapshot.itemId,
        stacks: snapshot.stacks,
        displayName,
        tier,
        tooltipBody,
      },
    }
  })
})

// Units that have at least one inventory slot.
const inventoryUnits = computed(() => {
  return props.units
    .filter((u) => (u.inventory?.size ?? 0) > 0)
    .map((u) => {
      const slots = Array.from({ length: u.inventory!.size }, (_, i) => {
        const held = u.inventory!.slots[i] ?? null
        if (!held) {
          return {
            index: i,
            occupied: false as const,
            itemId: null as string | null,
            instanceId: null as number | null,
            displayName: '',
            tierColor: null,
            tooltip: props.vaultSelectedInstanceId !== null
              ? 'Click to equip selected item'
              : 'Empty slot',
            isConsumable: false,
          }
        }
        const def = ITEM_DEF_MAP.get(held.itemId)
        const isConsumable = def?.kind === 'consumable'
        const tc = def?.tier ? tierColor(def.tier) : null
        const actionHint = isConsumable ? 'Click to use' : 'Click to unequip'
        return {
          index: i,
          occupied: true as const,
          itemId: held.itemId,
          instanceId: held.instanceId,
          displayName: def?.displayName ?? held.itemId,
          tierColor: tc,
          tooltip: def ? `${def.displayName} — ${actionHint}` : held.itemId,
          isConsumable,
        }
      })
      const maxHp = u.maxHp ?? u.hp ?? 0
      const hp = u.hp ?? 0
      return {
        id: u.id,
        name: u.name,
        unitType: u.unitType,
        portraitUrl: getUnitPortraitUrl(u.path, u.unitType),
        initials: (u.name || u.unitType || '?').slice(0, 2).toUpperCase(),
        hp,
        maxHp,
        slots,
      }
    })
})

// ── Drag validation ────────────────────────────────────────────────────────
/**
 * Returns true when the dragged item is allowed on unitType.
 * Falls back to true when no allowedUnitTypes restriction is set on the def.
 */
function isDropValid(unitType: string): boolean {
  const src = dragSource.value
  if (!src) return false
  const def = ITEM_DEF_MAP.get(src.itemId)
  if (!def || !def.allowedUnitTypes || def.allowedUnitTypes.length === 0) return true
  return def.allowedUnitTypes.includes(unitType)
}

// ── Drag handlers — vault cells ────────────────────────────────────────────
function onVaultCellDragStart(e: DragEvent, instanceId: number, itemId: string) {
  dragSource.value = { kind: 'vault', instanceId, itemId }
  e.dataTransfer!.effectAllowed = 'move'
  props.onSelectVaultItem(null)
}

// ── Drag handlers — unit slots ─────────────────────────────────────────────
function onUnitSlotDragStart(
  e: DragEvent,
  unitId: number,
  slotIndex: number,
  instanceId: number,
  itemId: string,
) {
  dragSource.value = { kind: 'unit-slot', unitId, slotIndex, instanceId, itemId }
  e.dataTransfer!.effectAllowed = 'move'
  props.onSelectVaultItem(null)
}

function onDragEnd() {
  dragSource.value = null
  vaultGridDragOver.value = false
}

// ── Drop zone — vault grid (unequip by dropping back) ─────────────────────
function onVaultGridDragOver(e: DragEvent) {
  e.preventDefault()
  vaultGridDragOver.value = true
}

function onVaultGridDragLeave() {
  vaultGridDragOver.value = false
}

function onVaultGridDrop(e: DragEvent) {
  e.preventDefault()
  vaultGridDragOver.value = false
  const src = dragSource.value
  dragSource.value = null
  if (!src) return
  if (src.kind === 'unit-slot') {
    props.onUnequipItem(src.unitId, src.slotIndex)
  }
}

// ── Drop zone — unit slots ─────────────────────────────────────────────────
function onUnitSlotDrop(
  e: DragEvent,
  unitId: number,
  slotIndex: number,
  slot: { occupied: boolean; instanceId: number | null },
) {
  e.preventDefault()
  const src = dragSource.value
  dragSource.value = null
  if (!src) return
  if (slot.occupied) return // reject occupied destination (Phase 1)

  if (src.kind === 'vault') {
    props.onEquipItem(unitId, slotIndex, src.instanceId)
  } else if (src.kind === 'unit-slot') {
    if (src.unitId === unitId && src.slotIndex === slotIndex) return // same slot, no-op
    props.onTransferItem(src.unitId, src.slotIndex, unitId, slotIndex)
  }
}

// ── Click handlers (existing flow — unchanged) ─────────────────────────────
function onVaultCellClick(instanceId: number) {
  if (props.vaultSelectedInstanceId === instanceId) {
    props.onSelectVaultItem(null)
  } else {
    props.onSelectVaultItem(instanceId)
  }
}

function onUnitSlotClick(
  unitId: number,
  slot: { index: number; occupied: boolean; isConsumable: boolean },
) {
  if (slot.occupied) {
    if (props.vaultSelectedInstanceId === null) {
      if (slot.isConsumable) {
        props.onUseConsumable(unitId, slot.index)
      } else {
        props.onUnequipItem(unitId, slot.index)
      }
    }
    // occupied + vault item selected: do nothing (prevent overwrite)
  } else {
    const instanceId = props.vaultSelectedInstanceId
    if (instanceId !== null) {
      props.onEquipItem(unitId, slot.index, instanceId)
      props.onSelectVaultItem(null)
    }
  }
}
</script>

<style scoped>
.vault-panel {
  position: absolute;
  bottom: 240px;
  /* Center horizontally without transform so the drag composable's
     transform: translate() works without conflicts. Calculate 50vw - half
     of the expected width (~240px) as a rough center anchor. The user can
     drag to reposition. */
  left: calc(50vw - 240px);
  z-index: 50;
  min-width: 480px;
  max-width: 680px;
  background:
    radial-gradient(circle at top, rgba(196, 158, 62, 0.12), transparent 52%),
    linear-gradient(180deg, rgba(30, 18, 8, 0.97), rgba(16, 10, 4, 0.98));
  border: 1px solid rgba(212, 168, 79, 0.35);
  border-radius: 10px;
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 200, 0.1),
    0 16px 40px rgba(0, 0, 0, 0.65);
  color: #e8d9b8;
  font-size: 13px;
  pointer-events: auto;
}

/* Embedded mode: drop all the floating-panel chrome so the host container
   (MatchMenu Vault tab) provides background, border, sizing, and position. */
.vault-panel--embedded {
  position: static;
  left: auto;
  bottom: auto;
  z-index: auto;
  min-width: 0;
  max-width: none;
  width: 100%;
  height: 100%;
  background: none;
  border: 0;
  border-radius: 0;
  box-shadow: none;
  transform: none !important;
}

.vault-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  cursor: grab;
  border-bottom: 1px solid rgba(212, 168, 79, 0.2);
  user-select: none;
}

.vault-head--dragging {
  cursor: grabbing;
}

.vault-grip {
  color: rgba(212, 168, 79, 0.5);
  font-size: 15px;
  letter-spacing: -2px;
  pointer-events: none;
}

.vault-toggle {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: #e8d9b8;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  padding: 0;
  letter-spacing: 0.04em;
}

.vault-close {
  margin-left: auto;
  background: none;
  border: none;
  color: rgba(232, 217, 184, 0.6);
  font-size: 14px;
  cursor: pointer;
  padding: 2px 6px;
  line-height: 1;
  border-radius: 3px;
}

.vault-close:hover {
  color: #e8d9b8;
  background: rgba(255, 255, 255, 0.08);
}

.vault-chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: rgba(212, 168, 79, 0.8);
  font-size: 14px;
}

.vault-chevron.open {
  transform: rotate(0deg);
}

.vault-chevron:not(.open) {
  transform: rotate(-90deg);
}

.vault-title {
  color: #f5e4c0;
}

.vault-body {
  padding: 12px;
}

.vault-columns {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.vault-col-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(212, 168, 79, 0.7);
  margin-bottom: 8px;
}

/* ── Vault grid (left column) ─────────────────────────────────────────── */

.vault-grid-col {
  flex: 0 0 auto;
}

.vault-grid {
  display: grid;
  grid-template-columns: repeat(4, 48px);
  gap: 4px;
}

.vault-cell {
  position: relative;
  width: 48px;
  height: 48px;
  background: rgba(0, 0, 0, 0.55);
  border: 2px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  padding: 0;
  cursor: default;
  transition: border-color 0.15s;
  box-sizing: border-box;
}

.vault-cell:not(.vault-cell--empty) {
  border-color: var(--tier-color, #9ca3af);
  cursor: pointer;
}

.vault-cell:not(.vault-cell--empty):hover {
  border-color: color-mix(in srgb, var(--tier-color, #9ca3af), white 40%);
}

.vault-cell--selected {
  border-color: var(--tier-color, #9ca3af) !important;
  box-shadow: 0 0 8px var(--tier-color, #9ca3af);
}

.vault-cell__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 80%;
  height: 80%;
  transform: translate(-50%, -50%);
  pointer-events: none;
  display: flex;
  align-items: center;
  justify-content: center;
}

.vault-cell-tooltip {
  display: none;
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  z-index: 100;
  min-width: 120px;
  max-width: 200px;
  background: rgba(10, 12, 20, 0.96);
  border: 1px solid rgba(212, 168, 79, 0.4);
  border-radius: 6px;
  padding: 7px 10px;
  pointer-events: none;
  white-space: normal;
}

.vault-cell:hover .vault-cell-tooltip {
  display: block;
}

.vault-cell-tooltip__title {
  font-size: 12px;
  font-weight: 700;
  color: #f5e4c0;
  margin-bottom: 2px;
}

.vault-cell-tooltip__tier {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--tier-color, #9ca3af);
  margin-bottom: 3px;
}

.vault-cell-tooltip__body {
  font-size: 11px;
  color: rgba(232, 217, 184, 0.75);
  line-height: 1.4;
}

.vault-cell__stack {
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

.vault-selection-hint {
  margin-top: 8px;
  font-size: 11px;
  color: rgba(96, 165, 250, 0.9);
  text-align: center;
  animation: hint-pulse 1.4s ease-in-out infinite;
}

@keyframes hint-pulse {
  0%, 100% { opacity: 0.7; }
  50%       { opacity: 1; }
}

/* ── Unit list (right column) ────────────────────────────────────────── */

.vault-units-col {
  flex: 1 1 auto;
  min-width: 0;
}

.vault-empty-units {
  font-size: 12px;
  color: rgba(232, 217, 184, 0.5);
  padding: 8px 0;
}

.vault-units-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 220px;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 168, 79, 0.3) transparent;
}

.vault-unit-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 6px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(212, 168, 79, 0.15);
  border-radius: 6px;
}

.vault-unit-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.vault-unit-portrait {
  flex: 0 0 32px;
  width: 32px;
  height: 32px;
  border-radius: 4px;
  overflow: hidden;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}

.vault-unit-portrait img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  image-rendering: pixelated;
}

.vault-unit-portrait__fallback {
  font-size: 11px;
  font-weight: 700;
  color: rgba(232, 217, 184, 0.7);
}

.vault-unit-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.vault-unit-name {
  font-size: 12px;
  font-weight: 700;
  color: #f0e0c0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.vault-unit-hp {
  font-size: 11px;
  color: rgba(232, 217, 184, 0.6);
}

.vault-unit-slots {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.vault-unit-slot {
  position: relative;
  width: 40px;
  height: 40px;
  background: rgba(0, 0, 0, 0.55);
  border: 2px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  padding: 0;
  cursor: pointer;
  transition: border-color 0.15s;
  box-sizing: border-box;
}

.vault-unit-slot--occupied {
  border-color: var(--tier-color, #9ca3af);
}

.vault-unit-slot--occupied:hover {
  border-color: color-mix(in srgb, var(--tier-color, #9ca3af), white 40%);
}

.vault-unit-slot--equip-target {
  animation: unit-slot-pulse 1.2s ease-in-out infinite;
  cursor: pointer;
}

@keyframes unit-slot-pulse {
  0%, 100% { border-color: rgba(96, 165, 250, 0.35); }
  50%       { border-color: rgba(96, 165, 250, 0.9); }
}

.vault-unit-slot__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 78%;
  height: 78%;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
}

/* ── Drag-and-drop visual feedback ──────────────────────────────────── */

/* Item being dragged dims slightly to show it is "in motion" */
.vault-cell--drag-source,
.vault-unit-slot--drag-source {
  opacity: 0.45;
}

/* Valid drop target: pulse with the equip-target rhythm */
.vault-unit-slot--drop-valid {
  animation: unit-slot-pulse 0.8s ease-in-out infinite;
  border-color: rgba(96, 165, 250, 0.7);
}

/* Invalid drop target: red border hint */
.vault-unit-slot--drop-invalid {
  border-color: rgba(239, 68, 68, 0.5);
}

/* Vault grid accepts drops when a unit-slot item is being dragged */
.vault-grid--drop-active {
  outline: 2px dashed rgba(96, 165, 250, 0.4);
  outline-offset: 2px;
}
</style>
