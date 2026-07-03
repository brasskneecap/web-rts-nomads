<template>
  <div
    class="match-menu"
    role="dialog"
    aria-label="Match menu"
    :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }"
  >
    <div ref="panelEl" class="match-menu__panel">
      <button
        type="button"
        class="match-menu__close"
        aria-label="Close menu"
        @click="emit('close')"
      >×</button>
      <div class="match-menu__body">
        <div class="match-menu__tabs" role="tablist">
          <template v-for="slot in tabSlots" :key="slot.index">
            <button
              v-if="slot.tab"
              type="button"
              class="match-menu__tab"
              :class="{ 'match-menu__tab--active': slot.tab.id === activeTabId }"
              role="tab"
              :aria-selected="slot.tab.id === activeTabId"
              :aria-controls="`match-menu-panel-${slot.tab.id}`"
              @click="setActiveTab(slot.tab.id)"
            >
              {{ slot.tab.label }}
            </button>
            <div v-else class="match-menu__tab-spacer" aria-hidden="true" />
          </template>
        </div>

        <div
          :id="`match-menu-panel-${activeTabId}`"
          class="match-menu__tab-content"
          role="tabpanel"
          :aria-labelledby="`match-menu-tab-${activeTabId}`"
        >
          <div v-if="activeTabId === 'shop'" class="match-menu__shop">
            <div v-if="shopGroups.length === 0" class="match-menu__shop-empty">
              No shops available.
            </div>
            <!-- One card per selling building, one per row: shop icon + name,
                 then a fixed 12-slot grid, with the refresh button to its right. -->
            <div
              v-for="group in shopGroups"
              :key="group.buildingId"
              class="shop-card"
            >
              <div class="shop-card__head">
                <div class="shop-card__icon">
                  <ActionIcon
                    class="shop-card__icon-img"
                    :action="{ id: group.buildingType, label: group.buildingName, iconDef: { kind: 'building', type: group.buildingType } }"
                  />
                </div>
                <div class="shop-card__name">{{ group.buildingName }}</div>
              </div>
              <div class="shop-card__body">
                <div class="shop-card__slots">
                  <button
                    v-for="cell in group.cells"
                    :key="cell.key"
                    type="button"
                    class="shop-slot"
                    :class="{
                      'shop-slot--filled': !!cell.entry,
                      'shop-slot--sold-out': !!cell.entry && cell.entry.quantity <= 0,
                    }"
                    :disabled="!cell.entry || cell.entry.quantity <= 0"
                    :aria-label="cell.entry ? (cell.entry.quantity > 0 ? `Buy ${cell.entry.displayName} for ${cell.entry.costGold} gold` : `${cell.entry.displayName} (sold out)`) : undefined"
                    @click="cell.entry && cell.entry.quantity > 0 ? onPurchase(cell.entry) : undefined"
                    @mouseenter="cell.entry ? onSlotEnter($event, cell.entry) : undefined"
                    @mouseleave="onSlotLeave"
                  >
                    <template v-if="cell.entry">
                      <ActionIcon
                        class="shop-slot__icon"
                        :action="{ id: cell.entry.itemId, label: cell.entry.displayName, iconDef: { kind: 'item', type: cell.entry.itemId } }"
                      />
                      <span v-if="cell.entry.quantity > 0" class="shop-slot__stock">{{ cell.entry.quantity }}</span>
                    </template>
                  </button>
                </div>
                <button
                  type="button"
                  class="shop-refresh"
                  :disabled="!group.canReroll"
                  :title="group.canReroll ? `Refresh this shop (${shopRerollsRemaining} left)` : 'Refresh not available for this shop'"
                  aria-label="Refresh shop"
                  @click="group.canReroll ? emit('reroll', group.buildingId) : undefined"
                >
                  <ActionIcon
                    class="shop-refresh__icon"
                    :action="{ id: 'reroll', label: 'Refresh', iconDef: { kind: 'item', type: 'reroll' } }"
                  />
                </button>
              </div>
            </div>
          </div>

          <div v-else-if="activeTabId === 'vault'" class="match-menu__vault">
            <VaultPanel
              embedded
              :vault="vault"
              :vault-selected-instance-id="vaultSelectedInstanceId"
              :units="units"
              :on-select-vault-item="onSelectVaultItem"
              :on-equip-item="onEquipItem"
              :on-unequip-item="onUnequipItem"
              :on-use-consumable="onUseConsumable"
              :on-transfer-item="onTransferItem"
              :on-use-item-on-unit="onUseItemOnUnit"
              :on-focus-unit="handleFocusUnit"
            />
          </div>

          <!-- Craft: one card per craftable recipe (recipes are unlocked via
               Recipe Shop purchases, tracked server-side). Same card shell as
               the Shop/Upgrades tabs. -->
          <div v-else-if="activeTabId === 'craft'" class="match-menu__shop">
            <div v-if="!hasArtificer" class="match-menu__shop-empty">
              Build an Artificer to craft items.
            </div>
            <div v-else-if="craftCatalog.length === 0" class="match-menu__shop-empty">
              Buy a recipe at a Recipe Shop to craft it here.
            </div>
            <template v-else>
              <div
                v-for="entry in craftCatalog"
                :key="entry.recipeId"
                class="shop-card"
              >
                <div class="shop-card__head">
                  <div class="shop-card__icon">
                    <ActionIcon
                      class="shop-card__icon-img"
                      :action="{ id: entry.output, label: entry.name, iconDef: { kind: 'item', type: entry.output } }"
                    />
                  </div>
                  <div class="shop-card__name">{{ entry.name }}</div>
                  <div class="craft-row__cost">{{ entry.costGold }} gold</div>
                </div>
                <ul class="craft-row__ingredients">
                  <li
                    v-for="ing in entry.ingredients"
                    :key="ing.itemId"
                    class="craft-row__ingredient"
                    :class="{ 'craft-row__ingredient--short': ing.have < ing.need }"
                  >{{ ing.itemId }} {{ ing.have }}/{{ ing.need }}</li>
                </ul>
                <button
                  type="button"
                  class="craft-row__btn"
                  :disabled="!entry.craftable"
                  :title="entry.craftable ? `Craft ${entry.name}` : 'Missing ingredients'"
                  @click="entry.craftable ? emit('craft', entry.recipeId) : undefined"
                >Craft</button>
              </div>
            </template>
          </div>

          <!-- Upgrades: one card per upgrade-providing building (currently the
               Blacksmith). Same card shell as the Shop, minus the reroll. -->
          <div v-else class="match-menu__shop">
            <div v-if="!hasUpgradeBuilding" class="match-menu__shop-empty">
              Build a Blacksmith to research unit upgrades.
            </div>
            <div v-else class="shop-card">
              <div class="shop-card__head">
                <div class="shop-card__icon">
                  <ActionIcon
                    class="shop-card__icon-img"
                    :action="{ id: 'blacksmith', label: 'Blacksmith', iconDef: { kind: 'building', type: 'blacksmith' } }"
                  />
                </div>
                <div class="shop-card__name">Blacksmith</div>
              </div>
              <div class="upgrade-list">
                <UpgradeRow
                  v-for="upgrade in upgrades"
                  :key="upgrade.track"
                  :upgrade="upgrade"
                  :on-purchase="onPurchaseUpgrade"
                />
              </div>
              <UpgradeQueue :upgrades="upgrades" :on-cancel="onCancelUpgrade" />
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Shop item tooltip, teleported so the scroll container can't clip it. -->
    <Teleport to="body">
      <div v-if="hoveredShopItem" class="shop-tooltip" :style="shopTooltipStyle">
        <div class="shop-tooltip__title">{{ hoveredShopItem.displayName }}</div>
        <div v-if="hoveredShopItem.description" class="shop-tooltip__desc">{{ hoveredShopItem.description }}</div>
        <div class="shop-tooltip__cost">{{ hoveredShopItem.costGold }}g</div>
        <!-- Remaining stock lives in the slot's corner badge (shop-slot__stock),
             not here — the tooltip only calls out the sold-out state. -->
        <div v-if="hoveredShopItem.quantity <= 0" class="shop-tooltip__out">Sold out</div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import ActionIcon from '@/components/ActionIcon.vue'
import VaultPanel from '@/components/VaultPanel.vue'
import UpgradeRow from '@/components/vault/UpgradeRow.vue'
import UpgradeQueue from '@/components/vault/UpgradeQueue.vue'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'
import type { ShopCatalogEntry, Unit, CraftCatalogEntry } from '@/game/core/GameState'
import type { PlayerUpgradeSnapshot, VaultItemSnapshot } from '@/game/network/protocol'

interface TabDef {
  id: string
  label: string
}

const TABS: TabDef[] = [
  { id: 'shop', label: 'Shop' },
  { id: 'upgrades', label: 'Upgrades' },
  { id: 'vault', label: 'Vault' },
  { id: 'craft', label: 'Craft' },
]

const TAB_ROW_SLOTS = 4
// Every shop card shows a fixed grid of slots, padded with empties.
const SHOP_SLOTS = 12

const props = withDefaults(defineProps<{
  shopCatalog?: ShopCatalogEntry[]
  shopRerollsRemaining?: number
  upgrades?: PlayerUpgradeSnapshot[]
  onPurchaseUpgrade?: (track: string) => void
  onCancelUpgrade?: (buildingId: string) => void
  vault?: VaultItemSnapshot[]
  vaultSelectedInstanceId?: number | null
  units?: Unit[]
  onSelectVaultItem?: (instanceId: number | null) => void
  onEquipItem?: (unitId: number, slotIndex: number, instanceId: number) => void
  onUnequipItem?: (unitId: number, slotIndex: number) => void
  onUseConsumable?: (unitId: number, slotIndex: number) => void
  onTransferItem?: (fromUnitId: number, fromSlotIdx: number, toUnitId: number, toSlotIdx: number) => void
  onUseItemOnUnit?: (unitId: number, instanceId: number) => void
  craftCatalog?: CraftCatalogEntry[]
  hasArtificer?: boolean
  onFocusUnit?: (unitId: number, menuRightPx?: number) => void
}>(), {
  shopCatalog: () => [],
  shopRerollsRemaining: 0,
  upgrades: () => [],
  onPurchaseUpgrade: () => {},
  onCancelUpgrade: () => {},
  vault: () => [],
  vaultSelectedInstanceId: null,
  units: () => [],
  onSelectVaultItem: () => {},
  onEquipItem: () => {},
  onUnequipItem: () => {},
  onUseConsumable: () => {},
  onTransferItem: () => {},
  onUseItemOnUnit: () => {},
  onFocusUnit: () => {},
  craftCatalog: () => [],
  hasArtificer: false,
})

// The visible window element; measured so the camera can frame a focused unit
// just clear of the window's right edge (see handleFocusUnit).
const panelEl = ref<HTMLElement | null>(null)

// Forward Vault unit focus to the parent, attaching the window's current
// right edge (viewport CSS px) so the camera can place the unit a fixed gap
// to its right regardless of screen size.
function handleFocusUnit(unitId: number) {
  const menuRightPx = panelEl.value?.getBoundingClientRect().right
  props.onFocusUnit(unitId, menuRightPx)
}

const tabSlots = computed(() =>
  Array.from({ length: TAB_ROW_SLOTS }, (_, index) => ({
    index,
    tab: TABS[index] ?? null,
  })),
)

// Group the flat shop catalog into one card per selling building. Each item is
// emitted once (deduped server-side), so a building owns a stable set of items.
interface ShopCell {
  key: string
  entry: ShopCatalogEntry | null
}
interface ShopGroup {
  buildingId: string
  buildingType: string
  buildingName: string
  cells: ShopCell[]
  /** Whether this shop can be refreshed right now (neutral-shop with rerolls). */
  canReroll: boolean
}

const shopGroups = computed<ShopGroup[]>(() => {
  const byBuilding = new Map<string, { type: string; name: string; items: ShopCatalogEntry[] }>()
  for (const entry of props.shopCatalog) {
    let group = byBuilding.get(entry.purchaseBuildingId)
    if (!group) {
      group = { type: entry.purchaseBuildingType, name: entry.purchaseBuildingName, items: [] }
      byBuilding.set(entry.purchaseBuildingId, group)
    }
    group.items.push(entry)
  }

  return Array.from(byBuilding.entries()).map(([buildingId, g]) => {
    // Always render SHOP_SLOTS cells, padding with empties past the catalog.
    const cells: ShopCell[] = g.items.slice(0, SHOP_SLOTS).map((entry) => ({
      key: `item-${entry.itemId}`,
      entry,
    }))
    for (let i = cells.length; i < SHOP_SLOTS; i++) {
      cells.push({ key: `empty-${i}`, entry: null })
    }
    // Refresh only applies to neutral-shop buildings and needs reroll budget.
    const canReroll = g.type === 'neutral-shop' && props.shopRerollsRemaining > 0
    return { buildingId, buildingType: g.type, buildingName: g.name, cells, canReroll }
  })
})

// A blacksmith (the upgrade-providing building) exists for the local player.
// hasBlacksmith is per-track but reflects the player-level "a blacksmith
// exists" signal, so any track suffices.
const hasUpgradeBuilding = computed(() => props.upgrades.some((u) => u.hasBlacksmith))

// ── Shop item tooltip (teleported to body to avoid scroll-container clipping) ─
const hoveredShopItem = ref<ShopCatalogEntry | null>(null)
const shopAnchorRect = ref<DOMRect | null>(null)

function onSlotEnter(e: MouseEvent, entry: ShopCatalogEntry) {
  shopAnchorRect.value = (e.currentTarget as HTMLElement).getBoundingClientRect()
  hoveredShopItem.value = entry
}

function onSlotLeave() {
  hoveredShopItem.value = null
}

const shopTooltipStyle = computed(() => {
  const r = shopAnchorRect.value
  if (!r) return {}
  return {
    left: `${r.left + r.width / 2}px`,
    top: `${r.top - 8}px`,
    transform: 'translate(-50%, -100%)',
  }
})

const emit = defineEmits<{
  close: []
  purchase: [payload: { itemId: string; buildingId: string }]
  reroll: [buildingId: string]
  craft: [recipeId: string]
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
  /* Anchored toward the top-left rather than vertically centered, so the
     window starts higher up the screen. */
  top: 4vh;
  left: 4vw;
  z-index: 30;
  pointer-events: auto;
  user-select: none;
}

/* Raise the menu while a shop slot's inline tooltip is showing so it paints
   above MatchHud/overlays — see the tooltip layering convention in style.css. */
.match-menu:has(.match-menu__slot--filled:hover) {
  z-index: var(--z-panel-raised, 300);
}

/* Dark window matching the Vault mockup: near-black panel with a subtle warm
   top glow, a thin gold rim, rounded corners and a deep drop shadow. Replaces
   the old parchment 9-slice frame. */
.match-menu__panel {
  position: relative;
  width: 890px;
  max-width: 92vw;
  height: 760px;
  max-height: 92vh;
  box-sizing: border-box;
  padding: 24px;
  background:
    radial-gradient(120% 60% at 50% 0%, rgba(196, 158, 62, 0.12), transparent 60%),
    linear-gradient(180deg, rgba(26, 19, 11, 0.98), rgba(13, 9, 5, 0.99));
  border: 1px solid rgba(212, 168, 79, 0.45);
  border-radius: 12px;
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 200, 0.08),
    0 18px 48px rgba(0, 0, 0, 0.7);
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
  background: rgba(40, 26, 12, 0.55);
  border: 1px solid rgba(120, 86, 44, 0.7);
  border-radius: 6px;
  color: #f3e6c8;
  font-size: 20px;
  font-weight: 700;
  line-height: 1;
  z-index: 1;
}

.match-menu__close:hover {
  color: #fff;
  background: rgba(60, 40, 18, 0.85);
  border-color: rgba(214, 178, 110, 0.9);
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

/* CSS tab buttons (replacing the header-UI 9-slice button) styled to sit on
   the parchment panel: a warm wood-toned chip with a gold rim so the cream
   label stays readable on the lighter parchment background. */
.match-menu__tab {
  width: 150px;
  height: 56px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-family: inherit;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #f3e6c8;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.6);
  background: linear-gradient(180deg, rgba(74, 50, 26, 0.96), rgba(46, 30, 14, 0.96));
  border: 1px solid rgba(120, 86, 44, 0.9);
  border-radius: 6px;
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 200, 0.12),
    0 2px 4px rgba(0, 0, 0, 0.35);
  transition: filter 0.12s ease, border-color 0.12s ease, background 0.12s ease;
}

.match-menu__tab:hover {
  filter: brightness(1.12);
  border-color: rgba(214, 178, 110, 0.95);
}

.match-menu__tab:active {
  filter: brightness(0.9);
}

/* Selected tab: brighter fill, gold rim and a soft glow so the active section
   reads at a glance. */
.match-menu__tab--active {
  background: linear-gradient(180deg, rgba(150, 110, 52, 0.98), rgba(108, 76, 33, 0.98));
  border-color: rgba(247, 216, 142, 0.95);
  color: #fff6dd;
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 200, 0.2),
    0 0 10px rgba(247, 216, 142, 0.35);
}

.match-menu__tab:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 3px;
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

/* Shop: one card per row, stacked from the top-left of the space. */
.match-menu__shop {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 12px;
  padding-right: 4px;
  scrollbar-width: thin;
  scrollbar-color: rgba(212, 168, 79, 0.3) transparent;
}

.match-menu__shop-empty {
  font-size: 12px;
  color: rgba(232, 217, 184, 0.5);
}

.shop-card {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(212, 168, 79, 0.22);
  border-radius: 8px;
  box-sizing: border-box;
}

.shop-card__head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.shop-card__icon {
  position: relative;
  flex: 0 0 72px;
  width: 72px;
  height: 72px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
}

.shop-card__icon-img {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 82%;
  height: 82%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.shop-card__name {
  font-size: 15px;
  font-weight: 700;
  color: #f5e4c0;
  letter-spacing: 0.03em;
}

/* Upgrades card: compact upgrade rows laid out in a responsive grid so several
   stack per row. */
.upgrade-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 8px;
}

/* Craft card: cost sits at the right edge of the shop-card__head row
   (mirrors .shop-tooltip__cost's gold color), then the ingredient list and
   craft button stack below. */
.craft-row__cost {
  margin-left: auto;
  font-size: 12px;
  font-weight: 700;
  color: #ffe9a0;
}

.craft-row__ingredients {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 11px;
  color: #d4b87a;
}

.craft-row__ingredient--short {
  color: #f5a3a3;
}

.craft-row__btn {
  width: 100%;
  padding: 6px 8px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.85), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.03em;
  text-align: center;
  transition: background 0.12s, border-color 0.12s;
}

.craft-row__btn:not(:disabled):hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.95), rgba(83, 53, 28, 1));
  border-color: rgba(220, 180, 110, 0.6);
}

.craft-row__btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

/* Body: the 12-slot grid with the refresh button to its right. */
.shop-card__body {
  display: flex;
  align-items: center;
  gap: 12px;
}

.shop-card__slots {
  display: grid;
  grid-template-columns: repeat(6, 60px);
  grid-auto-rows: 60px;
  gap: 8px;
  flex: 0 0 auto;
}

.shop-slot {
  position: relative;
  width: 60px;
  height: 60px;
  padding: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  border: 0;
  box-sizing: border-box;
}

.shop-slot--filled:hover:not(:disabled) {
  box-shadow: var(--ui-hover-glow);
}

.shop-slot--sold-out .shop-slot__icon {
  filter: grayscale(0.85) brightness(0.55);
}

.shop-slot:disabled {
  opacity: 1;
}

.shop-slot__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.shop-slot__stock {
  position: absolute;
  bottom: 7px;
  right: 8px;
  font-size: 11px;
  font-weight: 700;
  color: #ffffff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.9);
  pointer-events: none;
  line-height: 1;
}

/* Refresh button on the right of the slots. */
.shop-refresh {
  position: relative;
  flex: 0 0 auto;
  width: 52px;
  height: 52px;
  margin-left: auto;
  padding: 0;
  background: rgba(0, 0, 0, 0.25);
  border: 1px solid rgba(212, 168, 79, 0.4);
  border-radius: 8px;
  transition: border-color 0.12s ease, filter 0.12s ease;
}

.shop-refresh:hover:not(:disabled) {
  filter: brightness(1.15);
  border-color: rgba(214, 178, 110, 0.9);
}

.shop-refresh:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.shop-refresh__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 66%;
  height: 66%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

/* Teleported shop item tooltip. */
.shop-tooltip {
  position: fixed;
  z-index: 1000;
  min-width: 160px;
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

.shop-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
}

.shop-tooltip__desc {
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.45;
}

.shop-tooltip__cost {
  margin-top: 5px;
  font-size: 12px;
  font-weight: 700;
  color: #ffe9a0;
}

.shop-tooltip__out {
  margin-top: 3px;
  font-size: 11px;
  font-weight: 700;
  color: #f5a3a3;
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
