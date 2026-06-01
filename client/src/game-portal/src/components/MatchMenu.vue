<template>
  <div
    class="match-menu"
    role="dialog"
    aria-label="Match menu"
    :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }"
  >
    <UiPanel class="match-menu__panel" :padding="24">
      <button
        type="button"
        class="match-menu__close"
        aria-label="Close menu"
        @click="emit('close')"
      >×</button>
      <div class="match-menu__body">
        <div class="match-menu__tabs" role="tablist">
          <template v-for="slot in tabSlots" :key="slot.index">
            <UiButton
              v-if="slot.tab"
              class="match-menu__tab"
              :selected="slot.tab.id === activeTabId"
              role="tab"
              :aria-selected="slot.tab.id === activeTabId"
              :aria-controls="`match-menu-panel-${slot.tab.id}`"
              @click="setActiveTab(slot.tab.id)"
            >
              {{ slot.tab.label }}
            </UiButton>
            <div v-else class="match-menu__tab-spacer" aria-hidden="true" />
          </template>
        </div>

        <div
          :id="`match-menu-panel-${activeTabId}`"
          class="match-menu__tab-content"
          role="tabpanel"
          :aria-labelledby="`match-menu-tab-${activeTabId}`"
        >
          <div v-if="activeTabId === 'shop'" class="match-menu__grid">
            <button
              v-for="cell in shopCells"
              :key="cell.key"
              type="button"
              class="match-menu__slot ui-hover-glow"
              :class="{
                'match-menu__slot--filled': !!cell.entry,
                'match-menu__slot--sold-out': !!cell.entry && cell.entry.quantity <= 0,
              }"
              :disabled="!cell.entry || cell.entry.quantity <= 0"
              :aria-label="cell.entry ? (cell.entry.quantity > 0 ? `Buy ${cell.entry.displayName} for ${cell.entry.costGold} gold` : `${cell.entry.displayName} (sold out)`) : undefined"
              @click="cell.entry && cell.entry.quantity > 0 ? onPurchase(cell.entry) : undefined"
            >
              <template v-if="cell.entry">
                <ActionIcon
                  class="match-menu__slot-icon"
                  :action="{
                    id: cell.entry.itemId,
                    label: cell.entry.displayName,
                    iconDef: { kind: 'item', type: cell.entry.itemId },
                  }"
                />
                <div class="match-menu__tooltip">
                  <div class="match-menu__tooltip-title">{{ cell.entry.displayName }}</div>
                  <div v-if="cell.entry.description" class="match-menu__tooltip-desc">{{ cell.entry.description }}</div>
                  <div class="match-menu__tooltip-cost">{{ cell.entry.costGold }}g</div>
                  <div v-if="cell.entry.quantity <= 0" class="match-menu__tooltip-sold-out">Sold out</div>
                  <div v-else-if="cell.entry.quantity < 99" class="match-menu__tooltip-stock">Stock: {{ cell.entry.quantity }}</div>
                </div>
              </template>
            </button>
          </div>

          <div v-else-if="activeTabId === 'vault'" class="match-menu__vault">
            <VaultPanel
              embedded
              :vault="vault"
              :vault-capacity="vaultCapacity"
              :vault-selected-instance-id="vaultSelectedInstanceId"
              :units="units"
              :on-select-vault-item="onSelectVaultItem"
              :on-equip-item="onEquipItem"
              :on-unequip-item="onUnequipItem"
              :on-use-consumable="onUseConsumable"
              :on-transfer-item="onTransferItem"
            />
          </div>

          <div v-else class="match-menu__grid">
            <div
              v-for="i in SLOTS_PER_TAB"
              :key="`${activeTabId}-${i}`"
              class="match-menu__slot"
            />
          </div>
        </div>
      </div>
    </UiPanel>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import ActionIcon from '@/components/ActionIcon.vue'
import VaultPanel from '@/components/VaultPanel.vue'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'
import type { ShopCatalogEntry, Unit } from '@/game/core/GameState'
import type { VaultItemSnapshot } from '@/game/network/protocol'

interface TabDef {
  id: string
  label: string
}

const TABS: TabDef[] = [
  { id: 'shop', label: 'Shop' },
  { id: 'upgrades', label: 'Upgrades' },
  { id: 'vault', label: 'Vault' },
]

const TAB_ROW_SLOTS = 4
const SLOTS_PER_TAB = 18

const props = withDefaults(defineProps<{
  shopCatalog?: ShopCatalogEntry[]
  vault?: VaultItemSnapshot[]
  vaultCapacity?: number
  vaultSelectedInstanceId?: number | null
  units?: Unit[]
  onSelectVaultItem?: (instanceId: number | null) => void
  onEquipItem?: (unitId: number, slotIndex: number, instanceId: number) => void
  onUnequipItem?: (unitId: number, slotIndex: number) => void
  onUseConsumable?: (unitId: number, slotIndex: number) => void
  onTransferItem?: (fromUnitId: number, fromSlotIdx: number, toUnitId: number, toSlotIdx: number) => void
}>(), {
  shopCatalog: () => [],
  vault: () => [],
  vaultCapacity: 0,
  vaultSelectedInstanceId: null,
  units: () => [],
  onSelectVaultItem: () => {},
  onEquipItem: () => {},
  onUnequipItem: () => {},
  onUseConsumable: () => {},
  onTransferItem: () => {},
})

const tabSlots = computed(() =>
  Array.from({ length: TAB_ROW_SLOTS }, (_, index) => ({
    index,
    tab: TABS[index] ?? null,
  })),
)

const shopCells = computed(() => {
  // Pad the visible entries up to SLOTS_PER_TAB so the grid keeps its shape
  // even with a small catalog. Overflow beyond SLOTS_PER_TAB is dropped for
  // now — paginate in a follow-up when the catalog outgrows 18 entries.
  const entries = props.shopCatalog.slice(0, SLOTS_PER_TAB)
  const cells: Array<{ key: string; entry: ShopCatalogEntry | null }> = entries.map((e) => ({
    key: `item-${e.itemId}`,
    entry: e,
  }))
  for (let i = cells.length; i < SLOTS_PER_TAB; i++) {
    cells.push({ key: `empty-${i}`, entry: null })
  }
  return cells
})

const emit = defineEmits<{
  close: []
  purchase: [payload: { itemId: string; buildingId: string }]
}>()

// Active tab is exposed as v-model so parents can drive it from hotkeys
// (U → upgrades, V → vault) or programmatic flows. Default is hardcoded
// because defineModel() is hoisted outside setup() and cannot reference
// the local TABS constant — keep this in sync with TABS[0].id.
const activeTabId = defineModel<string>('activeTab', { default: 'shop' })

function setActiveTab(id: string) {
  if (activeTabId.value === id) return
  activeTabId.value = id
}

function onPurchase(entry: ShopCatalogEntry) {
  if (!entry.purchaseBuildingId || entry.quantity <= 0) return
  emit('purchase', { itemId: entry.itemId, buildingId: entry.purchaseBuildingId })
}
</script>

<style scoped>
.match-menu {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  z-index: 30;
  pointer-events: auto;
  user-select: none;
}

.match-menu__panel {
  position: relative;
  width: 750px;
  height: 475px;
  box-sizing: border-box;
}

.match-menu__close {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  background: transparent;
  border: 0;
  color: #f5ead2;
  font-size: 22px;
  font-weight: 700;
  line-height: 1;
  cursor: pointer;
  z-index: 1;
}

.match-menu__close:hover {
  color: #fff;
  filter: brightness(1.15);
}

.match-menu__close:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
  border-radius: 4px;
}

.match-menu__body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
  height: 100%;
}

.match-menu__tabs {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.match-menu__tab {
  width: 150px;
  height: 56px;
  min-width: 0;
  min-height: 0;
  font-size: 14px;
}

.match-menu__tab-spacer {
  width: 150px;
  height: 56px;
}

.match-menu__tab-content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.match-menu__grid {
  display: grid;
  grid-template-columns: repeat(6, 75px);
  grid-template-rows: repeat(3, 75px);
  gap: 8px;
  justify-content: center;
  align-content: center;
  flex: 1;
  min-height: 0;
}

.match-menu__vault {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.match-menu__slot {
  position: relative;
  width: 75px;
  height: 75px;
  padding: 0;
  border: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  color: inherit;
  font: inherit;
  /* Empty slots inherit the game default cursor (set on <html> by main.ts).
     Explicit `inherit` beats the UA stylesheet's button default and the
     global :is(button…) rule's :not(:disabled) exclusion. */
  cursor: inherit;
}

/* Filled slots show the game hover cursor since they have a tooltip. */
.match-menu__slot--filled {
  cursor: var(--cursor-hover, pointer);
}

.match-menu__slot:disabled {
  opacity: 1; /* override user-agent disabled opacity on empty slots */
}

/* Sold-out slot: still visible (tooltip still readable) but greyed so the
   player can see what the shop used to stock without being able to click. */
.match-menu__slot--sold-out .match-menu__slot-icon {
  filter: grayscale(0.85) brightness(0.55);
}
.match-menu__slot--sold-out {
  cursor: var(--cursor-disabled, default);
}

/* Shop item art rendered inside the icon-container frame at 70% so the
   container's outer edge stays visible — same idiom as .ability-slot__icon. */
.match-menu__slot-icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.match-menu__tooltip-sold-out {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
  font-size: 11px;
  font-weight: 700;
  color: #f5a3a3;
  line-height: 1.5;
}

.match-menu__tooltip-stock {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
}

/* Shop-item hover tooltip — mirrors the action-tooltip language used by
   SelectionHud so all in-match tooltips read as one visual family. */
.match-menu__tooltip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  min-width: 160px;
  max-width: 240px;
  padding: 7px 10px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(34, 22, 10, 0.98), rgba(20, 12, 4, 0.98));
  border: 1px solid rgba(200, 164, 106, 0.45);
  color: #f5ead2;
  text-align: left;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 100;
}

.match-menu__slot--filled:hover .match-menu__tooltip {
  opacity: 1;
  visibility: visible;
}

.match-menu__tooltip-title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
}

.match-menu__tooltip-desc {
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
}

.match-menu__tooltip-cost {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
  font-size: 12px;
  font-weight: 700;
  color: #ffe9a0;
  line-height: 1.5;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.9);
}

</style>
