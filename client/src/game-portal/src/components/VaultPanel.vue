<template>
  <div
    class="vault-panel"
    :class="{
      'vault-panel--collapsed': collapsed,
      'vault-panel--dragging': !embedded && drag.dragging.value,
      'vault-panel--embedded': embedded,
    }"
    :style="rootStyle"
    role="dialog"
    aria-label="Vault"
  >
    <!-- Drag handle / header (hidden when embedded inside the MatchMenu tab) -->
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
        <span class="vault-title">Vault ({{ storageItems.length }}/{{ vaultCapacity }})</span>
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
      <div class="vault-layout">
        <!-- Left: storage grid + selected-item details -->
        <div class="vault-left">
          <StorageGrid
            :items="storageItems"
            :capacity="vaultCapacity"
            :selected-instance-id="vaultSelectedInstanceId"
            :drag-active="dragSource?.kind === 'unit-slot'"
            :icon-container-url="iconContainerUrl"
            @select="onSelectStorage"
            @item-dragstart="onStorageDragStart"
            @item-dragend="onDragEnd"
            @storage-drop="onStorageDrop"
          />
          <SelectedItemPanel :item="selectedItem" :icon-container-url="iconContainerUrl" />
        </div>

        <!-- Right: eligible unit cards -->
        <div class="vault-right">
          <div class="vault-right__head">
            <span class="vault-right__title">Eligible Units ({{ unitCards.length }})</span>
            <span v-if="vaultSelectedInstanceId !== null" class="vault-right__hint">
              Click a unit's empty slot to equip
            </span>
          </div>
          <div class="vault-right__list">
            <EligibleUnitCard
              v-for="card in unitCards"
              :key="card.id"
              :card="card"
              :has-selected-item="vaultSelectedInstanceId !== null"
              :accepts-drop="acceptsDropForUnit(card.id)"
              :icon-container-url="iconContainerUrl"
              @focus="onCardFocus"
              @slot-dragstart="onCardSlotDragStart"
              @slot-dragend="onDragEnd"
              @slot-drop="onCardSlotDrop"
            />
            <div v-if="unitCards.length === 0" class="vault-right__empty">
              No units can hold items.
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
import { formatUnitPath, type Unit } from '@/game/core/GameState'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'
import { PERK_DEF_MAP } from '@/game/maps/perkDefs'
import { formatPerkTooltip } from '@/game/core/perkTooltip'
import { TIER_COLORS, buildItemTooltipBody } from '@/game/items/itemRules'
import { getUnitPortraitUrl } from '@/game/rendering/unitSprites'
import { getRankToneColor } from '@/game/rendering/rankColors'
import { useDraggablePanel } from '@/composables/useDraggablePanel'
import StorageGrid from '@/components/vault/StorageGrid.vue'
import SelectedItemPanel from '@/components/vault/SelectedItemPanel.vue'
import EligibleUnitCard from '@/components/vault/EligibleUnitCard.vue'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'
import type {
  VaultInventorySlot,
  VaultPerkChip,
  VaultRank,
  VaultSelectedItem,
  VaultStorageItem,
  VaultUnitCardData,
} from '@/components/vault/types'

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
  onFocusUnit?: (unitId: number) => void
  onClose?: () => void
  /**
   * When true, render only the body (no drag handle / header chrome / absolute
   * positioning / panel background). Used when the vault is embedded inside the
   * MatchMenu Vault tab.
   */
  embedded?: boolean
}>(), {
  embedded: false,
  onFocusUnit: () => {},
})

// ── Panel drag (floating mode only) ─────────────────────────────────────────
const collapsed = ref(false)
const drag = useDraggablePanel('vault-panel')

const rootStyle = computed(() => {
  if (props.embedded) return {}
  return drag.style.value
})

// Rank slots are positional: index 0 = bronze, 1 = silver, 2 = gold. Mirrors
// the perk-rank ordering used by getPerkActionItems.
const RANK_SLOTS: VaultRank[] = ['bronze', 'silver', 'gold']

function tierColor(tier: string | undefined): string {
  if (!tier) return TIER_COLORS.common
  return TIER_COLORS[tier as keyof typeof TIER_COLORS] ?? TIER_COLORS.common
}

// gold > silver > bronze > unranked, used for sorting.
function rankValue(rank: string | undefined): number {
  return rank === 'gold' ? 3 : rank === 'silver' ? 2 : rank === 'bronze' ? 1 : 0
}

// ── Fast lookup of raw units for the slot-click handler ─────────────────────
const unitsById = computed(() => {
  const m = new Map<number, Unit>()
  for (const u of props.units) m.set(u.id, u)
  return m
})

// ── Storage items ───────────────────────────────────────────────────────────
const storageItems = computed<VaultStorageItem[]>(() =>
  props.vault.map((snap) => {
    const def = ITEM_DEF_MAP.get(snap.itemId)
    return {
      instanceId: snap.instanceId,
      itemId: snap.itemId,
      displayName: def?.displayName ?? snap.itemId,
      tier: def?.tier,
      tierColor: tierColor(def?.tier),
      tooltipBody: def ? buildItemTooltipBody(def) : '',
      stacks: snap.stacks,
    }
  }),
)

// ── Selected item (details panel + eligibility source) ──────────────────────
const selectedSnapshot = computed<VaultItemSnapshot | null>(() => {
  if (props.vaultSelectedInstanceId === null) return null
  return props.vault.find((v) => v.instanceId === props.vaultSelectedInstanceId) ?? null
})

const selectedItem = computed<VaultSelectedItem | null>(() => {
  const snap = selectedSnapshot.value
  if (!snap) return null
  const def = ITEM_DEF_MAP.get(snap.itemId)
  return {
    itemId: snap.itemId,
    displayName: def?.displayName ?? snap.itemId,
    tier: def?.tier,
    tierColor: tierColor(def?.tier),
    description: def?.description,
    stats: def ? buildItemTooltipBody(def) : '',
  }
})

// Whether a specific item is allowed on a unit type. No restriction means any
// item-capable unit qualifies.
function itemTypeAllowsUnit(itemId: string, unitType: string): boolean {
  const def = ITEM_DEF_MAP.get(itemId)
  if (!def?.allowedUnitTypes || def.allowedUnitTypes.length === 0) return true
  return def.allowedUnitTypes.includes(unitType)
}

// Whether the currently-selected item is allowed on a unit type (drives card
// eligibility / sorting). True when nothing is selected.
function itemAllowsUnit(unitType: string): boolean {
  const snap = selectedSnapshot.value
  if (!snap) return true
  return itemTypeAllowsUnit(snap.itemId, unitType)
}

// ── Per-unit card data ──────────────────────────────────────────────────────
function buildInventory(unit: Unit): VaultInventorySlot[] {
  const size = unit.inventory?.size ?? 0
  const slots = unit.inventory?.slots ?? []
  return RANK_SLOTS.map((rank, index) => {
    const locked = index >= size
    const held = !locked ? slots[index] ?? null : null
    if (!held) {
      return { rank, slotIndex: index, locked, item: null }
    }
    const def = ITEM_DEF_MAP.get(held.itemId)
    return {
      rank,
      slotIndex: index,
      locked,
      item: {
        instanceId: held.instanceId,
        itemId: held.itemId,
        displayName: def?.displayName ?? held.itemId,
        tier: def?.tier,
        tierColor: tierColor(def?.tier),
        tooltipBody: def ? buildItemTooltipBody(def) : '',
        isConsumable: def?.kind === 'consumable',
      },
    }
  })
}

function buildPerks(unit: Unit): VaultPerkChip[] {
  const ids = unit.perkIds ?? []
  const chips: VaultPerkChip[] = []
  for (const perkId of ids) {
    const def = PERK_DEF_MAP.get(perkId)
    if (!def) continue
    chips.push({
      id: perkId,
      iconId: def.icon ?? 'attack',
      title: def.displayName,
      body: formatPerkTooltip(def, unit),
    })
  }
  return chips
}

const unitCards = computed<VaultUnitCardData[]>(() => {
  const hasSelection = props.vaultSelectedInstanceId !== null

  // Only units with an inventory capability can hold items.
  const capable = props.units.filter((u) => u.inventory != null)

  const cards = capable.map((u, originalIndex) => {
    const inventory = buildInventory(u)
    const eligible = itemAllowsUnit(u.unitType)
    const hasEmptyMatchingSlot =
      eligible && inventory.some((s) => !s.locked && !s.item)
    const pathLabel = u.path && u.path !== 'none' ? formatUnitPath(u.path) : ''
    const specializationName = pathLabel || u.name
    const rankChevrons =
      u.rank === 'bronze' ? 1 : u.rank === 'silver' ? 2 : u.rank === 'gold' ? 3 : 0
    return {
      card: {
        id: u.id,
        portraitUrl: getUnitPortraitUrl(u.path, u.unitType) ?? null,
        initials: (specializationName || u.unitType || '?').slice(0, 2).toUpperCase(),
        specializationName,
        rank: u.rank ?? '',
        rankChevrons,
        rankColor: getRankToneColor(u.rank, 'light'),
        xpInto: u.xpIntoCurrentRank ?? null,
        xpToNext: u.xpToNextRank ?? null,
        isMaxRank: u.rank === 'gold',
        perks: buildPerks(u),
        inventory,
        eligible,
        hasEmptyMatchingSlot,
      } as VaultUnitCardData,
      originalIndex,
    }
  })

  // Sort. With an item selected: eligible-with-empty-slot first, then eligible
  // (slots full), then ineligible; higher rank wins ties. Without a selection:
  // simply by rank then name.
  cards.sort((a, b) => {
    if (hasSelection) {
      const groupA = a.card.eligible ? (a.card.hasEmptyMatchingSlot ? 0 : 1) : 2
      const groupB = b.card.eligible ? (b.card.hasEmptyMatchingSlot ? 0 : 1) : 2
      if (groupA !== groupB) return groupA - groupB
    }
    const rankDiff = rankValue(b.card.rank) - rankValue(a.card.rank)
    if (rankDiff !== 0) return rankDiff
    const nameDiff = a.card.specializationName.localeCompare(b.card.specializationName)
    if (nameDiff !== 0) return nameDiff
    return a.originalIndex - b.originalIndex
  })

  return cards.map((c) => c.card)
})

// ── Drag-and-drop state ─────────────────────────────────────────────────────
type DragSource =
  | { kind: 'vault'; instanceId: number; itemId: string }
  | { kind: 'unit-slot'; unitId: number; slotIndex: number; itemId: string }

const dragSource = ref<DragSource | null>(null)

// Whether a currently-dragged item could be equipped on a given unit: the item
// must be allowed on that unit's type and the unit must have an empty unlocked
// slot. Drives the per-slot drop-target glow on each card.
function acceptsDropForUnit(unitId: number): boolean {
  const src = dragSource.value
  if (!src) return false
  const unit = unitsById.value.get(unitId)
  if (!unit) return false
  const def = ITEM_DEF_MAP.get(src.itemId)
  const allowed = !def?.allowedUnitTypes?.length || def.allowedUnitTypes.includes(unit.unitType)
  if (!allowed) return false
  const size = unit.inventory?.size ?? 0
  const slots = unit.inventory?.slots ?? []
  for (let i = 0; i < Math.min(RANK_SLOTS.length, size); i++) {
    if (!slots[i]) return true
  }
  return false
}

// ── Interactions ────────────────────────────────────────────────────────────
function onSelectStorage(instanceId: number) {
  // Click selects an item to view its stats. Toggle off on a second click.
  if (props.vaultSelectedInstanceId === instanceId) {
    props.onSelectVaultItem(null)
  } else {
    props.onSelectVaultItem(instanceId)
  }
}

function onCardFocus(unitId: number) {
  props.onFocusUnit(unitId)
}

function onStorageDragStart(instanceId: number, itemId: string) {
  dragSource.value = { kind: 'vault', instanceId, itemId }
}

function onCardSlotDragStart(payload: { unitId: number; slotIndex: number }) {
  const unit = unitsById.value.get(payload.unitId)
  const held = unit?.inventory?.slots?.[payload.slotIndex] ?? null
  if (!held) return
  dragSource.value = {
    kind: 'unit-slot',
    unitId: payload.unitId,
    slotIndex: payload.slotIndex,
    itemId: held.itemId,
  }
}

function onDragEnd() {
  dragSource.value = null
}

// Drop onto a unit's inventory slot: equip from the vault, or transfer from
// another unit slot. Occupied targets are never overwritten.
function onCardSlotDrop(payload: { unitId: number; slotIndex: number }) {
  const src = dragSource.value
  dragSource.value = null
  if (!src) return

  const unit = unitsById.value.get(payload.unitId)
  if (!unit) return
  const size = unit.inventory?.size ?? 0
  if (payload.slotIndex >= size) return // locked slot
  const held = unit.inventory?.slots?.[payload.slotIndex] ?? null
  if (held) return // occupied — block, never overwrite

  if (!itemTypeAllowsUnit(src.itemId, unit.unitType)) return

  if (src.kind === 'vault') {
    props.onEquipItem(unit.id, payload.slotIndex, src.instanceId)
    if (src.instanceId === props.vaultSelectedInstanceId) props.onSelectVaultItem(null)
  } else {
    // Transfer between unit slots.
    if (src.unitId === unit.id && src.slotIndex === payload.slotIndex) return
    props.onTransferItem(src.unitId, src.slotIndex, unit.id, payload.slotIndex)
  }
}

// Drop onto the storage grid: unequip an item dragged out of a unit slot.
function onStorageDrop() {
  const src = dragSource.value
  dragSource.value = null
  if (src?.kind === 'unit-slot') {
    props.onUnequipItem(src.unitId, src.slotIndex)
  }
}
</script>

<style scoped>
.vault-panel {
  position: absolute;
  bottom: 240px;
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

/* Embedded mode: drop all floating-panel chrome so the host container provides
   background, border, sizing, and position. */
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
  padding: 0;
  letter-spacing: 0.04em;
}

.vault-close {
  margin-left: auto;
  background: none;
  border: none;
  color: rgba(232, 217, 184, 0.6);
  font-size: 14px;
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

.vault-chevron.open { transform: rotate(0deg); }
.vault-chevron:not(.open) { transform: rotate(-90deg); }

.vault-title {
  color: #f5e4c0;
}

/* Embedded body fills the tab; floating body just gets padding. */
.vault-body {
  padding: 12px;
  height: 100%;
  box-sizing: border-box;
}

.vault-panel--embedded .vault-body {
  padding: 0;
}

.vault-layout {
  display: flex;
  gap: 16px;
  align-items: stretch;
  height: 100%;
  min-height: 0;
}

/* Left column: storage grid + selected-item details. */
/* Fixed to the storage grid's exact width (4×64 + 3×8 gaps = 280px) so the
   left column never resizes between the empty and populated selected-item
   states — keeping the unit cards from shifting. */
.vault-left {
  flex: 0 0 280px;
  width: 280px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

/* Right column: scrollable eligible unit list. */
.vault-right {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.vault-right__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
  flex: 0 0 auto;
}

.vault-right__title {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #f5e4c0;
}

.vault-right__hint {
  font-size: 11px;
  color: rgba(96, 165, 250, 0.9);
}

.vault-right__list {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  /* Two unit cards per row; each column shrinks freely (minmax 0). */
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-content: start;
  gap: 8px;
  padding-right: 6px;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 168, 79, 0.3) transparent;
}

.vault-right__empty {
  grid-column: 1 / -1;
  font-size: 12px;
  color: rgba(232, 217, 184, 0.5);
  padding: 12px 0;
}
</style>
