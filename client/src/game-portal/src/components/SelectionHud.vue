<template>
  <footer
    class="selection-hud"
    :style="{
      '--ui-panel-image': `url(${uiPanelUrl})`,
      '--ui-panel-slice': String(theme.footerPanel.slice),
      '--ui-icon-container-image': `url(${iconContainerUrl})`,
    }"
  >
    <div class="selection-main">
      <section
        ref="minimapPanelEl"
        class="selection-panel selection-panel--minimap"
        aria-label="Minimap"
      >
        <!-- The actual minimap is rendered onto the main game canvas; this
             panel just provides the framed slot it draws into. -->
      </section>
      <!-- Transparent spacer above the details panel so its frame sits lower
           than the minimap/actions panels. Reserved for future content; for
           now it just creates the visual offset. -->
      <div class="selection-details-spacer" aria-hidden="true"></div>
      <section class="selection-panel selection-panel--details">
        <div class="details-body">
        <!-- Header: title/subtitle/rank shown only for single-unit / building.
              Multi-unit selection shows the cards instead. -->
        <header v-if="unitCards.length <= 1" class="selection-header">
          <div class="selection-header__copy">
            <div class="selection-header__name">
              <div class="selection-title">
                {{ ui.selection.title }}<span
                  v-if="ui.selection.kind === 'unit' && ui.selection.pathLabel"
                  class="selection-title__path"
                > ({{ ui.selection.pathLabel }})</span><span
                  v-if="buildingDurability"
                  class="selection-title__durability"
                  :title="buildingDurability.label"
                > {{ buildingDurability.value }}</span>
              </div>
              <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
              <div
                v-if="buildingResourceStock"
                class="selection-resource"
                :title="buildingResourceStock.label"
              >
                {{ buildingResourceStock.label }}: {{ buildingResourceStock.value }}
              </div>
              <div
                v-if="focusTargetLabel"
                class="selection-focus"
                :title="focusTargetLabel"
              >
                Focusing: {{ focusTargetLabel }}
              </div>
            </div>
            <div
              v-if="ui.selection.kind === 'unit' && (ui.selection.rankLabel || ui.selection.xpLabel)"
              class="selection-progression"
            >
              <div v-if="ui.selection.rankLabel" class="selection-progression__rank-group">
                <div class="selection-progression__label">Rank</div>
                <div class="selection-progression__rank">{{ ui.selection.rankLabel }}</div>
              </div>
              <span v-if="ui.selection.xpLabel" class="selection-progression__xp">{{ ui.selection.xpLabel }}</span>
            </div>
          </div>
          <!-- Top-right header slot: production leading-card + progress bar
               for buildings that are training. Anchored here (instead of in
               the production-card below) so the queue row gets the full
               body width to render larger unit cards. -->
          <div
            v-if="ui.selection.production"
            class="selection-header__production"
          >
            <div class="production-leading">
              <img
                v-if="getUnitPortraitUrl(undefined, ui.selection.production.queuedUnitTypes[0])"
                :src="getUnitPortraitUrl(undefined, ui.selection.production.queuedUnitTypes[0])!"
                :alt="ui.selection.production.queuedUnitTypes[0]"
                draggable="false"
              />
            </div>
            <div class="production-bar">
              <div
                class="production-bar__fill"
                :style="{ width: `${Math.max(0, Math.min(ui.selection.production.progress * 100, 100))}%` }"
              />
              <div class="production-bar__time">{{ ui.selection.production.timeLabel }}</div>
              <button
                v-if="ui.selection.production.cancelable !== false"
                class="production-bar__cancel"
                type="button"
                aria-label="Cancel"
                title="Cancel"
                @click="$emit('action', ui.selection.production.cancelActionId ?? 'cancel-training')"
              >
                x
              </button>
            </div>
          </div>
          <div v-if="iconDetails.length > 0 && ui.selection.kind !== 'building'" class="detail-stats">
            <div
              v-for="detail in iconDetails"
              :key="detail.id"
              class="stat-row"
              :class="{ 'stat-row--has-tooltip': !!detail.tooltipTitle }"
              :title="detail.tooltip"
              :aria-label="detail.value ? `${detail.label} ${detail.value}` : detail.label"
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
                class="stat-row__icon"
              >
                <path :d="detail.icon" />
              </svg>
              <strong v-if="detail.value" class="stat-row__value">{{ detail.value }}</strong>
              <div v-if="detail.tooltipTitle" class="stat-tooltip">
                <div class="stat-tooltip__title">{{ detail.tooltipTitle }}</div>
                <div v-if="detail.tooltipBody" class="stat-tooltip__body">{{ detail.tooltipBody }}</div>
              </div>
            </div>
          </div>
        </header>

        <div v-if="unitCards.length > 1" class="unit-cards">
          <button
            v-for="card in unitCards"
            :key="card.id"
            type="button"
            class="unit-card"
            :title="card.title"
            @click="onUnitCardClick(card.id, $event)"
          >
            <div class="unit-card__hp">
              <div
                class="unit-card__hp-fill"
                :class="{
                  'unit-card__hp-fill--low': card.hpFraction > 0 && card.hpFraction < 0.34,
                  'unit-card__hp-fill--mid': card.hpFraction >= 0.34 && card.hpFraction < 0.67,
                }"
                :style="{ width: `${card.hpFraction * 100}%` }"
              />
            </div>
            <div class="unit-card__portrait">
              <img
                v-if="card.portraitUrl"
                :src="card.portraitUrl"
                :alt="card.title"
                draggable="false"
              />
              <span v-else class="unit-card__portrait-fallback">{{ card.initials }}</span>
              <div
                v-if="card.rankChevrons > 0"
                class="unit-card__rank"
                :style="{ color: card.rankColor }"
                :aria-label="`Rank ${card.rank}`"
              >
                <svg
                  v-for="n in card.rankChevrons"
                  :key="n"
                  viewBox="0 0 10 6"
                  class="unit-card__rank-chevron"
                  aria-hidden="true"
                >
                  <polyline
                    points="1.2,5 5,1.2 8.8,5"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.6"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
              </div>
            </div>
          </button>
        </div>

        <div v-if="ui.selection.kind === 'building' && ui.selection.construction" class="construction-card">
          <div class="construction-bar">
            <div
              class="construction-bar__fill"
              :style="{ width: `${Math.max(0, Math.min(ui.selection.construction.progress * 100, 100))}%` }"
            />
            <div class="construction-bar__time">{{ ui.selection.construction.timeLabel }}</div>
          </div>
          <div class="construction-workers">
            Assigned Workers {{ ui.selection.construction.builderCount }}/3
          </div>
        </div>

        <!-- Townhall in-progress bar. The upgrade button itself lives in the
             action grid (bottom-left slot); this section only shows progress
             while an upgrade is underway. -->
        <div
          v-if="ui.selection.kind === 'building' && ui.selectedBuildingType === 'townhall' && townhallUpgradeInProgress"
          class="townhall-tier"
        >
          <div class="townhall-upgrade-bar">
            <div
              class="townhall-upgrade-bar__fill"
              :style="{ width: `${townhallUpgradeProgress * 100}%` }"
            />
            <div class="townhall-upgrade-bar__label">
              {{ (ui.townHallTier || 1) === 1 ? 'Upgrading to Keep...' : 'Upgrading to Castle...' }}
            </div>
          </div>
        </div>

        <!-- Production queue. Always renders 7 slots whenever training is
             active (matching the max queue depth of 8 = 1 leading + 7
             queued). Empty slots show the icon-container frame so the
             player can see at a glance how much queue capacity is left. -->
        <div
          v-if="ui.selection.production"
          class="production-queue"
        >
          <button
            v-for="i in 7"
            :key="i"
            type="button"
            class="production-queue__slot"
            :class="{ 'production-queue__slot--empty': !ui.selection.production.queuedUnitTypes[i] }"
            :disabled="!ui.selection.production.queuedUnitTypes[i]"
            :title="ui.selection.production.queuedUnitTypes[i]
              ? `${ui.selection.production.queuedUnitTypes[i]} — click to remove from queue`
              : 'Empty queue slot'"
            @click="$emit('action', `cancel-queue-${i}`)"
          >
            <img
              v-if="ui.selection.production.queuedUnitTypes[i] && getUnitPortraitUrl(undefined, ui.selection.production.queuedUnitTypes[i])"
              :src="getUnitPortraitUrl(undefined, ui.selection.production.queuedUnitTypes[i])!"
              :alt="ui.selection.production.queuedUnitTypes[i]"
              draggable="false"
            />
          </button>
        </div>
        <div v-if="inlineDetails.length > 0 && ui.selection.kind !== 'building'" class="detail-inline">
          <template v-for="(detail, index) in inlineDetails" :key="detail.id">
            <span class="detail-entry" :title="detail.tooltip">
              <span>{{ detail.label }}</span>
              <strong v-if="detail.value">{{ detail.value }}</strong>
            </span>
            <span v-if="index < inlineDetails.length - 1" class="detail-separator">,</span>
          </template>
        </div>
        </div>

        <!-- Right side: inventory slots for items the unit can hold. Only
             rendered when exactly one unit with an inventory is selected —
             multi-select, building selections, and inventory-less units
             (workers) hide the strip entirely. All possible slots render;
             ones above unit.inventory.size overlay the lock icon with their
             unlock rank. Occupied slots are interactive (click to unequip or
             use consumable); empty unlocked slots are interactive when a
             vault item is selected (click to equip). -->
        <div
          v-if="ui.selectedUnits.length === 1 && inventorySlots.length > 0"
          class="details-inventory"
          aria-label="Inventory"
        >
          <component
            :is="slot.locked ? 'div' : 'button'"
            v-for="slot in inventorySlots"
            :key="slot.index"
            class="inventory-slot"
            :class="{
              'inventory-slot--locked': slot.locked,
              'inventory-slot--equip-target': !slot.locked && !slot.occupied && ui.vaultSelectedInstanceId !== null,
            }"
            :title="slot.tooltip ? undefined : slot.title"
            :aria-label="slot.title"
            :type="slot.locked ? undefined : 'button'"
            :disabled="slot.locked || undefined"
            @click="!slot.locked ? onInventorySlotClick(slot) : undefined"
            @mouseenter="slot.tooltip ? onInventorySlotEnter($event, slot) : undefined"
            @mouseleave="onInventorySlotLeave"
          >
            <img
              v-if="slot.iconUrl"
              :src="slot.iconUrl"
              :alt="slot.title"
              class="inventory-slot__icon"
              draggable="false"
            />
            <div v-else-if="slot.occupied && slot.itemId" class="inventory-slot__item-icon">
              <ActionIcon :action="{ id: slot.itemId, label: slot.title, iconDef: { kind: 'item', type: slot.itemId } }" />
            </div>
          </component>
        </div>
        <!-- Item hover tooltip for occupied inventory slots — the same styled
             game tooltip used by the vault storage grid and unit cards. -->
        <ItemHoverTooltip :item="hoveredInventoryTooltip" :anchor="inventoryAnchorRect" />
      </section>

      <section class="selection-panel selection-panel--actions">
        <div class="action-grid">
        <template v-for="i in GRID_SIZE" :key="i">
          <template v-if="ui.selection.actions[i - 1]">
            <!-- Perk display cell (bottom row: bronze → silver → gold) -->
            <div
              v-if="ui.selection.actions[i - 1].kind === 'perk'"
              class="action-cell action-cell--perk"
              :class="[
                `action-cell--perk-${ui.selection.actions[i - 1].perkRank}`,
                { 'action-cell--perk-cooldown': perkCooldownFraction(ui.selection.actions[i - 1]) > 0 },
              ]"
            >
              <ActionIcon :action="ui.selection.actions[i - 1]" />
              <!-- Clock-wipe cooldown overlay: a conic gradient covers the
                   fraction of the icon equal to remaining/total, with a
                   seconds-remaining label in the center. The overlay and
                   label are absent when the perk is ready. -->
              <div
                v-if="perkCooldownFraction(ui.selection.actions[i - 1]) > 0"
                class="perk-cooldown-overlay"
                :style="{ '--perk-cooldown-cleared': `${(1 - perkCooldownFraction(ui.selection.actions[i - 1])) * 360}deg` }"
                aria-hidden="true"
              >
                <span class="perk-cooldown-number">
                  {{ Math.max(1, Math.ceil(ui.selection.actions[i - 1].cooldownRemaining ?? 0)) }}
                </span>
              </div>
              <div
                v-if="ui.selection.actions[i - 1].tooltipTitle"
                class="perk-tooltip"
              >
                <div class="perk-tooltip__title">{{ ui.selection.actions[i - 1].tooltipTitle }}</div>
                <div
                  v-if="ui.selection.actions[i - 1].tooltipBody"
                  class="perk-tooltip__body"
                >{{ ui.selection.actions[i - 1].tooltipBody }}</div>
              </div>
            </div>
            <!-- Invisible padding cell that holds the slot between regular actions and perks -->
            <div
              v-else-if="ui.selection.actions[i - 1].id === ''"
              class="action-cell action-cell--empty"
            />
            <!-- Regular interactive action button -->
            <button
              v-else
              class="action-cell"
              :class="{
                'action-cell--active': ui.selection.actions[i - 1].active,
                'action-cell--autocast': ui.selection.actions[i - 1].autoCast,
                'action-cell--channeling': ui.selection.actions[i - 1].channeling,
                'action-cell--cooldown': perkCooldownFraction(ui.selection.actions[i - 1]) > 0,
              }"
              :disabled="ui.selection.actions[i - 1].disabled"
              type="button"
              @click="$emit('action', ui.selection.actions[i - 1].id)"
              @contextmenu.prevent="$emit('action', 'autocast-toggle-' + ui.selection.actions[i - 1].id)"
            >
              <ActionIcon :action="ui.selection.actions[i - 1]" />
              <!-- Remaining shop stock, bottom-right — same visual language
                   as the Match Menu shop cards' corner badge. -->
              <span
                v-if="ui.selection.actions[i - 1].stockCount"
                class="action-cell__stock"
              >{{ ui.selection.actions[i - 1].stockCount }}</span>
              <!-- Ability cooldown clock-wipe overlay. Shares the same
                   conic-gradient + countdown-number visual language as the
                   perk cooldown overlay so cooldowns read consistently
                   across both perk slots and ability buttons. pointer-events
                   none so the button itself still gets the click. -->
              <div
                v-if="perkCooldownFraction(ui.selection.actions[i - 1]) > 0"
                class="perk-cooldown-overlay"
                :style="{ '--perk-cooldown-cleared': `${(1 - perkCooldownFraction(ui.selection.actions[i - 1])) * 360}deg` }"
                aria-hidden="true"
              >
                <span class="perk-cooldown-number">
                  {{ Math.max(1, Math.ceil(ui.selection.actions[i - 1].cooldownRemaining ?? 0)) }}
                </span>
              </div>
              <!-- Styled action tooltip — shared frame with the stat /
                   perk tooltips. Always shows the label; cost rows are
                   only rendered for actions that have a cost (training,
                   building). -->
              <div
                v-if="ui.selection.actions[i - 1].label"
                class="action-tooltip"
              >
                <div class="action-tooltip__title">{{ parseActionLabel(ui.selection.actions[i - 1].label).name }}</div>
                <div
                  v-if="actionHotkey(ui.selection.actions[i - 1])"
                  class="action-tooltip__hotkey"
                >Hotkey: {{ actionHotkey(ui.selection.actions[i - 1]) }}</div>
                <div
                  v-if="ui.selection.actions[i - 1].cost?.length"
                  class="action-tooltip__body"
                >
                  <div
                    v-for="c in ui.selection.actions[i - 1].cost"
                    :key="c.resourceId"
                    class="action-tooltip__row"
                  >
                    <img
                      v-if="getResourceIconUrl(c.resourceId)"
                      :src="getResourceIconUrl(c.resourceId)!"
                      :alt="resourceDisplayName(c.resourceId)"
                      class="action-tooltip__icon"
                      draggable="false"
                    />
                    <span
                      v-else
                      class="action-tooltip__gem"
                      :style="{ background: `linear-gradient(180deg, ${c.accent}, rgba(0,0,0,0.55))` }"
                    />
                    <span class="action-tooltip__name">{{ resourceDisplayName(c.resourceId) }}</span>
                    <span class="action-tooltip__amount">{{ c.amount }}</span>
                  </div>
                </div>
                <div
                  v-if="ui.selection.actions[i - 1].tooltipBody"
                  class="action-tooltip__stat-preview"
                >{{ ui.selection.actions[i - 1].tooltipBody }}</div>
              </div>
            </button>
          </template>
          <div v-else class="action-cell action-cell--empty" />
        </template>
      </div>
      </section>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { formatUnitPath, type ActionItem } from '@/game/core/GameState'
import type { GameUiSnapshot } from '@/game/core/GameClient'
import { getUnitPortraitUrl } from '@/game/rendering/unitSprites'
import { getRankToneColor } from '@/game/rendering/rankColors'
import ActionIcon from '@/components/ActionIcon.vue'
import uiPanelUrl from '@/assets/ui/themes/default/footer_panel.png'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'
import theme from '@/assets/ui/themes/default/theme.json'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'
import { TIER_COLORS, buildItemTooltipBody } from '@/game/items/itemRules'
import ItemHoverTooltip, { type ItemTooltipData } from '@/components/ItemHoverTooltip.vue'
import { getActionIconImage } from '@/game/rendering/actionIconSprites'
import { getResourceIconUrl } from '@/game/rendering/resourceSprites'

const emit = defineEmits<{
  action: [actionId: string]
  'select-unit': [unitId: number]
  'deselect-unit': [unitId: number]
  'minimap-rect': [rect: DOMRect | null]
  'use-consumable': [payload: { unitId: number; slotIndex: number }]
  'unequip-item': [payload: { unitId: number; slotIndex: number }]
  'equip-item': [payload: { unitId: number; slotIndex: number; instanceId: number }]
}>()

const props = defineProps<{
  ui: GameUiSnapshot
}>()

// ── Minimap panel rect tracking ────────────────────────────────────────────
// The canvas-rendered minimap reads its bounds from GameState; we push the
// panel's viewport rect up to Match.vue (and through GameClient → state) any
// time the layout shifts (resize, scroll, responsive breakpoint).
const minimapPanelEl = ref<HTMLElement | null>(null)
let rafId = 0
let lastEmittedRect: { x: number; y: number; w: number; h: number } | null = null

// Polled per-frame so position changes (CSS var tweaks, layout shifts, HMR)
// propagate even though ResizeObserver only catches size changes. Only emits
// when the rect actually changed, so the parent handler is a no-op most
// frames. Cost: one getBoundingClientRect per frame.
function pollMinimapRect() {
  const el = minimapPanelEl.value
  if (!el) {
    if (lastEmittedRect !== null) {
      lastEmittedRect = null
      emit('minimap-rect', null)
    }
  } else {
    const r = el.getBoundingClientRect()
    if (
      !lastEmittedRect ||
      lastEmittedRect.x !== r.left ||
      lastEmittedRect.y !== r.top ||
      lastEmittedRect.w !== r.width ||
      lastEmittedRect.h !== r.height
    ) {
      lastEmittedRect = { x: r.left, y: r.top, w: r.width, h: r.height }
      emit('minimap-rect', r)
    }
  }
  rafId = requestAnimationFrame(pollMinimapRect)
}

onMounted(() => {
  rafId = requestAnimationFrame(pollMinimapRect)
})

onBeforeUnmount(() => {
  if (rafId) cancelAnimationFrame(rafId)
  rafId = 0
  emit('minimap-rect', null)
})

// Shift-click on a unit card removes that unit from the group selection.
// Plain click selects only that unit (matching the existing behavior).
function onUnitCardClick(unitId: number, event: MouseEvent) {
  if (event.shiftKey) {
    emit('deselect-unit', unitId)
  } else {
    emit('select-unit', unitId)
  }
}

const GRID_SIZE = 12

// Details are split by whether they have a stat icon: icon entries render as
// a vertical icon+value grid, everything else falls through to the inline row.
const iconDetails = computed(() => props.ui.selection.details.filter((d) => !!d.icon))
const inlineDetails = computed(() => props.ui.selection.details.filter((d) => !d.icon))

// Building durability is hoisted next to the title (e.g. "Townhall 100/100")
// so the rest of the building details can be hidden without losing HP info.
// Returns null for non-building selections or when no durability detail
// exists in the snapshot.
const buildingDurability = computed(() => {
  if (props.ui.selection.kind !== 'building') return null
  const detail = props.ui.selection.details.find(
    (d) => d.id === 'durability' || d.id === 'construction-health',
  )
  if (!detail || !detail.value) return null
  return { label: detail.label, value: detail.value }
})

// Remaining resource stock (e.g. "Gold Remaining") for resource buildings
// like the goldmine. The inline details row is suppressed for buildings
// (see the `kind !== 'building'` guard on `.detail-inline`), so this detail
// would otherwise never render — surface it in the header instead. Returns
// null for non-buildings or buildings that carry no resource-stock detail.
const buildingResourceStock = computed(() => {
  if (props.ui.selection.kind !== 'building') return null
  const detail = props.ui.selection.details.find((d) => d.id === 'resource-stock')
  if (!detail || !detail.value) return null
  return { label: detail.label, value: detail.value }
})

// Focus Target indicator — shown in the header when exactly one Cleric (or
// other heal-class support unit) is selected and has an active focus target.
// Resolves the focused ally's name + HP from the snapshot's selectedUnits when
// possible; falls back to the bare ID when the focus is out of vision and
// no longer present in the current snapshot.
const focusTargetLabel = computed(() => {
  if (props.ui.selectedUnits.length !== 1) return null
  const caster = props.ui.selectedUnits[0]
  const focusId = caster.focusTargetId ?? 0
  if (focusId === 0) return null
  const focusUnit = props.ui.selectedUnits.find((u) => u.id === focusId)
  if (focusUnit) {
    const name = focusUnit.name || focusUnit.unitType || `Unit ${focusUnit.id}`
    const hp = focusUnit.hp ?? 0
    const max = focusUnit.maxHp ?? hp
    return `${name} (${hp}/${max})`
  }
  return `Unit ${focusId}`
})

// True when the server has set tierUpRemaining on the selected building's
// metadata, indicating an upgrade is in progress.
const townhallUpgradeInProgress = computed(() => {
  if (props.ui.selection.kind !== 'building') return false
  const detail = props.ui.selection.details.find((d) => d.id === 'tierup-remaining')
  return !!detail
})

// Progress fraction (0..1) for the in-progress tier-up bar.
// Drives the fill width. The detail value carries "remaining/total" or the
// server may send a dedicated metadata field. We use the metadata approach:
// tierUpRemaining and tierUpTotal are expected in the building metadata, which
// SelectionHud doesn't currently receive directly. As a fallback, if the
// detail carries a numeric value it is treated as the remaining fraction.
const townhallUpgradeProgress = computed(() => {
  if (props.ui.selection.kind !== 'building') return 0
  const detail = props.ui.selection.details.find((d) => d.id === 'tierup-progress')
  if (!detail?.value) return 0
  const v = parseFloat(detail.value)
  return Number.isNaN(v) ? 0 : Math.max(0, Math.min(1, v))
})

// One card per selected unit. Portrait prefers the unit's promoted path (e.g.
// a berserker-path soldier shows the berserker sprite) and falls back to the
// base unit type when the path has no dedicated sprite set.
const unitCards = computed(() => {
  const units = props.ui.selectedUnits
  if (units.length === 0) return []
  return units.map((u) => {
    const max = u.maxHp ?? u.hp ?? 0
    const hp = u.hp ?? 0
    const hpFraction = max > 0 ? Math.max(0, Math.min(1, hp / max)) : 0
    // Mirror the world rank visual: bronze=1, silver=2, gold=3 stacked chevrons,
    // tinted by the same rank palette used on unit overlays.
    const rankChevrons =
      u.rank === 'bronze' ? 1 : u.rank === 'silver' ? 2 : u.rank === 'gold' ? 3 : 0
    const pathLabel = u.path && u.path !== 'none' ? formatUnitPath(u.path) : ''
    const displayName = pathLabel || u.name
    return {
      id: u.id,
      title: `${displayName}  ${hp} / ${max}`,
      portraitUrl: getUnitPortraitUrl(u.path, u.unitType),
      initials: (displayName || u.unitType || '?').slice(0, 2).toUpperCase(),
      hpFraction,
      rank: u.rank ?? '',
      rankChevrons,
      rankColor: getRankToneColor(u.rank, 'light'),
    }
  })
})

// Inventory display is fully data-driven from unit.inventory. Every unit
// that has an inventory shows all MAX_INVENTORY_SLOTS frames (mirroring the
// vault panel) so the total possible is always visible: `size` slots are
// unlocked, the rest overlay the lock icon with the rank that unlocks them.
// Units with no inventory at all (workers) render no slot strip. Held items
// are interactive — click to use (consumable) or unequip (equipment). Empty
// unlocked slots glow as equip targets when a vault item is selected.
const MAX_INVENTORY_SLOTS = 3

// Index → rank that unlocks the slot; mirrors the server's
// setInventorySizeForRankLocked (1 at base, 2 at silver, 3 at gold).
const SLOT_UNLOCK_RANK = ['Base', 'Silver', 'Gold']

const inventorySlots = computed(() => {
  // Inventory only shows for single-unit selections.
  const unit = props.ui.selectedUnits.length === 1 ? props.ui.selectedUnits[0] : null
  const inventory = unit?.inventory
  const size = inventory?.size ?? 0
  const slots = inventory?.slots ?? []
  if (size === 0) return []
  const displayCount = Math.max(MAX_INVENTORY_SLOTS, size)

  return Array.from({ length: displayCount }, (_, index) => {
    const locked = index >= size
    if (locked) {
      const unlockRank = SLOT_UNLOCK_RANK[index]
      return {
        index,
        locked: true,
        occupied: false,
        iconUrl: getActionIconImage('lock')?.src ?? null,
        itemId: null as string | null,
        title: unlockRank ? `Locked — unlocks at ${unlockRank} rank` : 'Locked slot',
        instanceId: null as number | null,
        isConsumable: false,
        tooltip: null as ItemTooltipData | null,
      }
    }

    const held = slots[index] ?? null
    if (!held) {
      const vaultId = props.ui.vaultSelectedInstanceId
      return {
        index,
        locked: false,
        occupied: false,
        iconUrl: null,
        itemId: null as string | null,
        title: vaultId !== null ? 'Click to equip selected item' : 'Empty slot',
        instanceId: null as number | null,
        isConsumable: false,
        tooltip: null as ItemTooltipData | null,
      }
    }

    const def = ITEM_DEF_MAP.get(held.itemId)
    const isConsumable = def?.kind === 'consumable'
    const actionHint = isConsumable ? 'Click to use' : 'Click to unequip'
    return {
      index,
      locked: false,
      occupied: true,
      iconUrl: null,
      itemId: held.itemId,
      title: def ? `${def.displayName} — ${actionHint}` : held.itemId,
      instanceId: held.instanceId,
      isConsumable,
      // Styled hover tooltip (replaces the native title for occupied slots).
      tooltip: {
        displayName: def?.displayName ?? held.itemId,
        tier: def?.tier,
        tierColor: def?.tier
          ? (TIER_COLORS[def.tier as keyof typeof TIER_COLORS] ?? TIER_COLORS.common)
          : undefined,
        body: def ? buildItemTooltipBody(def) : '',
        hint: actionHint,
      } as ItemTooltipData,
    }
  })
})

// ── Inventory slot hover tooltip ────────────────────────────────────────────
const hoveredInventoryTooltip = ref<ItemTooltipData | null>(null)
const inventoryAnchorRect = ref<DOMRect | null>(null)

function onInventorySlotEnter(e: MouseEvent, slot: { tooltip: ItemTooltipData | null }) {
  if (!slot.tooltip) return
  inventoryAnchorRect.value = (e.currentTarget as HTMLElement).getBoundingClientRect()
  hoveredInventoryTooltip.value = slot.tooltip
}

function onInventorySlotLeave() {
  hoveredInventoryTooltip.value = null
}

function onInventorySlotClick(slot: {
  index: number
  locked: boolean
  occupied: boolean
  instanceId: number | null
  isConsumable: boolean
}) {
  // The click may consume/unequip the hovered item — don't leave a tooltip
  // describing a slot that just changed.
  hoveredInventoryTooltip.value = null
  const unit = props.ui.selectedUnits[0]
  if (!unit) return

  if (slot.occupied) {
    // No vault item selected: use consumable or unequip equipment.
    if (props.ui.vaultSelectedInstanceId === null) {
      if (slot.isConsumable) {
        emit('use-consumable', { unitId: unit.id, slotIndex: slot.index })
      } else {
        emit('unequip-item', { unitId: unit.id, slotIndex: slot.index })
      }
    }
    // If vault item is selected and the slot is occupied, do nothing
    // (prevent accidental overwrite — user should unequip first).
  } else {
    // Empty slot: equip the selected vault item if one is chosen.
    const instanceId = props.ui.vaultSelectedInstanceId
    if (instanceId !== null) {
      emit('equip-item', { unitId: unit.id, slotIndex: slot.index, instanceId })
    }
  }
}

// perkCooldownFraction returns the remaining/total ratio for a perk action,
// clamped to [0, 1]. 0 means the perk is ready (no overlay rendered); >0
// means the clock-wipe overlay should cover that fraction of the icon.
function perkCooldownFraction(action: ActionItem): number {
  const remaining = action.cooldownRemaining ?? 0
  const total = action.cooldownTotal ?? 0
  if (remaining <= 0 || total <= 0) return 0
  return Math.min(1, remaining / total)
}

const RESOURCE_LABELS: Record<string, string> = {
  gold: 'Gold',
  wood: 'Wood',
  food: 'Food',
}

function resourceDisplayName(resourceId: string): string {
  return RESOURCE_LABELS[resourceId] ?? resourceId.charAt(0).toUpperCase() + resourceId.slice(1)
}

// Action labels embed the hotkey inline as parens around a letter — e.g.
// "(M)ove", "E(x)it", "(B)uild". Split that into a clean display name and
// the hotkey letter so the tooltip can render them on separate lines.
// Returns the original label as the name when no hotkey markup is present.
function parseActionLabel(label: string): { name: string; hotkey: string | null } {
  const m = label.match(/\(([A-Za-z])\)/)
  if (!m) return { name: label, hotkey: null }
  return {
    name: label.replace(/\(([A-Za-z])\)/, '$1'),
    hotkey: m[1].toUpperCase(),
  }
}

// Tooltip hotkey for an action: prefer the explicit `hotkey` field (set on
// build-menu buildings so they show their key with clean labels), otherwise
// fall back to a "(X)" marker parsed out of the label (e.g. "E(x)it").
function actionHotkey(action: { hotkey?: string; label: string }): string | null {
  return action.hotkey ?? parseActionLabel(action.label).hotkey
}
</script>

<style scoped>
.selection-hud {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 5;
  /* Standardized fixed sizes — no clamp(), no flex distribution. The HUD
     stays a constant size regardless of viewport changes. text-align +
     inline-block on children is the non-flex equivalent of justify-content
     center; font-size: 0 here suppresses the inter-element whitespace and
     each panel resets its own font-size. */
  text-align: center;
  font-size: 0;
  /* +25% pass over the original (220/600/260/200/32/12) — UI was reading
     too small at 100% zoom. Scale all panel-defining variables together so
     internal layout stays proportional. */
  --minimap-panel-width: 275px;
  --details-panel-width: 750px;
  --actions-panel-width: 325px;
  /* All three panels share the same height so the frame reads as one
     continuous footer rather than two adjacent panels at different sizes. */
  --main-panel-height: 250px;
  --hud-height: 250px;
  /* The details panel is pushed down by this amount, leaving a transparent
     gap above its frame while minimap/actions stay at full height. */
  --details-top-spacer: 40px;
  /* Horizontal breathing room between the details frame and the minimap /
     actions panels on either side of it. */
  --panel-side-gap: 15px;
  pointer-events: none;
}

/* Raise the HUD while one of its inline tooltips is showing (action cells,
   perk cells, stat rows) so the tooltip paints above the launcher/ItemsBar
   strip that overlaps the space above the panels — see the tooltip layering
   convention in style.css. Scoped to the tooltip triggers, NOT a bare
   :hover on the root: the raise must release the instant the pointer leaves
   the trigger, or the raised HUD would shadow the launcher's buttons. */
.selection-hud:has(.action-cell:hover, .stat-row--has-tooltip:hover) {
  z-index: var(--z-panel-raised, 300);
}

.selection-main {
  position: relative;
  display: inline-block;
  vertical-align: bottom;
  width: calc(
    var(--minimap-panel-width) + var(--details-panel-width) + var(--actions-panel-width)
  );
  height: var(--main-panel-height);
  /* Wrapper passes pointer events through; each panel re-enables them
     individually. Without this, clicks that hit the minimap panel (which
     itself is pointer-events: none so the canvas-rendered minimap can be
     clicked) get caught here instead of reaching the canvas behind. */
  pointer-events: none;
}

/* Shared 9-slice panel frame: 56×56 source with 16px corners. The corners
   stay pixel-perfect, edges + center tile (round) to fill any panel size.
   --ui-panel-image is set on the .selection-hud root from the imported PNG.
   `box-sizing: border-box` is required so the 17px border doesn't add to the
   declared width — without it, panels overflow each other by 34px. */
.selection-panel {
  box-sizing: border-box;
  min-width: 0;
  padding: 12px 14px;
  background: none;
  border: calc(var(--ui-panel-slice) * 1px) solid transparent;
  border-radius: 0;
  border-image-source: var(--ui-panel-image);
  border-image-slice: var(--ui-panel-slice) fill;
  border-image-width: calc(var(--ui-panel-slice) * 1px);
  border-image-repeat: round;
  image-rendering: pixelated;
}

.selection-panel--minimap {
  position: absolute;
  top: 0;
  left: 0;
  width: var(--minimap-panel-width);
  height: var(--main-panel-height);
  font-size: 13px;
  /* No `fill` on the slice: the panel's interior must be transparent so the
     canvas-rendered minimap (which sits behind the HUD) shows through. The
     other panels keep `fill` because they have no canvas content underneath. */
  border-image-slice: var(--ui-panel-slice);
  pointer-events: none;
}

.selection-details-spacer {
  position: absolute;
  top: 0;
  left: var(--minimap-panel-width);
  width: var(--details-panel-width);
  height: var(--details-top-spacer);
  background: transparent;
  pointer-events: none;
}

.selection-panel--details {
  position: absolute;
  top: var(--details-top-spacer);
  left: var(--minimap-panel-width);
  width: var(--details-panel-width);
  height: calc(var(--main-panel-height) - var(--details-top-spacer));
  font-size: 13px;
  /* Row layout: left-aligned details body + right-aligned inventory grid. */
  display: flex;
  flex-direction: row;
  align-items: stretch;
  gap: 12px;
  pointer-events: auto;
}

/* Left side of the details panel — owns all the existing details content
   (title, subtitle, rank, stats, unit cards, production bars, etc). */
.details-body {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  /* overflow: visible so stat hover tooltips can extend above the panel
     frame. Sub-sections that legitimately need to scroll (unit-cards,
     detail-inline) own their own overflow handling. */
  overflow: visible;
  background: #000;
}

/* Right side: vertical column of inventory slots, right-aligned. */
.details-inventory {
  flex: 0 0 auto;
  align-self: stretch;
  margin-left: auto;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  padding: 4px;
  background: rgba(0, 0, 0, 0.6);
}

.inventory-slot {
  position: relative;
  width: 48px;
  height: 48px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  /* Default: interactive slots enable pointer events via button element */
  pointer-events: none;
  border: 2px solid transparent;
  box-sizing: border-box;
  cursor: default;
}

button.inventory-slot {
  pointer-events: auto;
  cursor: pointer;
  padding: 0;
  outline: none;
}

button.inventory-slot:hover {
  border-color: rgba(212, 168, 79, 0.5);
}

button.inventory-slot:focus-visible {
  border-color: rgba(212, 168, 79, 0.9);
}

/* Empty slot with a vault item selected — pulses to indicate it's a valid equip target. */
.inventory-slot--equip-target {
  animation: inventory-equip-pulse 1.2s ease-in-out infinite;
}

@keyframes inventory-equip-pulse {
  0%, 100% { border-color: rgba(96, 165, 250, 0.35); }
  50%       { border-color: rgba(96, 165, 250, 0.9); }
}

/* Held-item / lock icon overlay sits centered inside the icon-container
   frame. 70% of the slot leaves the container's outer edge visible so the
   slot still reads as a slot rather than just an icon. */
.inventory-slot__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  object-fit: contain;
  image-rendering: pixelated;
  pointer-events: none;
}

/* Item icon rendered via ActionIcon (canvas). Centered in the slot at 70%
   size to match the lock icon's visual weight. */
.inventory-slot__item-icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
}

/* Locked slots dim the container slightly so the lock icon reads as a
   "you can't use this yet" affordance rather than a regular item. */
.inventory-slot--locked {
  opacity: 0.85;
}

.inventory-slot--locked .inventory-slot__icon {
  opacity: 0.85;
}

.selection-header {
  display: flex;
  flex-direction: row;
  /* Top-aligned columns so name/rank don't drift down when the stats column
     is taller, and stats don't drift down when the copy column is taller. */
  align-items: flex-start;
  gap: 14px;
  flex: 0 0 auto;
}

.selection-header__copy {
  display: flex;
  flex-direction: column;
  /* Don't grow — let the stats column sit immediately to the right of the
     name/rank column instead of being pushed to the right edge of the body. */
  flex: 0 1 auto;
  min-width: 0;
  /* Stretch to the header's full height so space-between has room to push
     the name group to the top and progression to the bottom. */
  align-self: stretch;
  justify-content: space-between;
  padding: 10px;
  background: rgba(0, 0, 0, 0.6);
}

.selection-header__name {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  min-width: 0;
  text-align: left;
}

.selection-panel--actions {
  position: absolute;
  top: 0;
  right: 0;
  width: var(--actions-panel-width);
  height: var(--hud-height);
  font-size: 13px;
  /* overflow: visible so perk hover tooltips can extend above the panel. */
  overflow: visible;
  pointer-events: auto;
  padding: 0;
}

.selection-title {
  font-size: 17px;
  font-weight: 700;
  line-height: 1.15;
  overflow-wrap: anywhere;
  color: #f5ead2;
}

.selection-subtitle {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.35;
  overflow-wrap: anywhere;
  color: #cbb893;
}

/* Focus Target indicator — sits between the subtitle and the progression row
   for a single Cleric / support unit with an active focus. Color matches the
   auto-cast highlight (sky-blue) so the visual association between the
   Focus Target button glow and this indicator is clear. */
.selection-focus {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.35;
  overflow-wrap: anywhere;
  color: #9bd4ff;
  font-weight: 600;
}

.selection-resource {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.35;
  overflow-wrap: anywhere;
  color: #f2c94c;
  font-weight: 600;
}

.selection-title__path {
  font-size: 12px;
  font-weight: 600;
  color: #e9c77a;
}

/* Inline HP shown next to the building title (e.g. "Townhall 100/100").
   Tan-colored to subordinate it to the title without disappearing. */
.selection-title__durability {
  margin-left: 8px;
  font-size: 13px;
  font-weight: 700;
  color: #d4b87a;
  font-variant-numeric: tabular-nums;
}

.selection-progression {
  margin-top: 4px;
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 10px;
  font-size: 12px;
  color: #e7d7b6;
}

.selection-progression__rank-group {
  display: flex;
  flex-direction: column;
  line-height: 1.1;
}

.selection-progression__label {
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #a8946e;
}

.selection-progression__rank {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #fff2d6;
}

.selection-progression__xp {
  color: #cbb893;
}

.action-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  grid-template-rows: repeat(3, 1fr);
  gap: 0;
  width: 100%;
  height: 100%;
}

.detail-stats {
  display: grid;
  grid-auto-flow: column;
  grid-template-rows: repeat(4, auto);
  grid-auto-columns: max-content;
  row-gap: 8px;
  column-gap: 16px;
  flex: 0 0 auto;
  padding: 10px 4px;
  background: rgba(0, 0, 0, 0.6);
}

.stat-row {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #e7d7b6;
  font-size: 13px;
  line-height: 1.2;
}

.stat-row__icon {
  width: 16px;
  height: 16px;
  flex: 0 0 16px;
  color: #d2b376;
}

.stat-row__value {
  color: #fff2d6;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.stat-row--has-tooltip {
  position: relative;
  cursor: default;
}

.stat-tooltip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 0;
  min-width: 160px;
  max-width: 240px;
  padding: 7px 10px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(34, 22, 10, 0.98), rgba(20, 12, 4, 0.98));
  border: 1px solid rgba(200, 164, 106, 0.45);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  color: #f5ead2;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 10;
  white-space: pre-line;
}

.stat-row--has-tooltip:hover .stat-tooltip {
  opacity: 1;
  visibility: visible;
}

.stat-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
}

.stat-tooltip__body {
  font-size: 12px;
  line-height: 1.5;
  color: #d4b87a;
}

.detail-inline {
  margin-top: 8px;
  color: #e7d7b6;
  font-size: 13px;
  line-height: 1.45;
  overflow-y: auto;
}

.unit-cards {
  display: flex;
  flex-wrap: wrap;
  align-content: flex-start;
  gap: 6px;
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(210, 176, 113, 0.4) transparent;
}

.unit-cards::-webkit-scrollbar {
  width: 6px;
}

.unit-cards::-webkit-scrollbar-thumb {
  background: rgba(210, 176, 113, 0.4);
  border-radius: 3px;
}

.unit-card {
  display: flex;
  flex-direction: column;
  width: 52px;
  padding: 0;
  border: 1px solid rgba(210, 176, 113, 0.45);
  border-radius: 6px;
  background: linear-gradient(180deg, rgba(54, 34, 20, 0.9), rgba(36, 22, 12, 0.9));
  color: inherit;
  cursor: pointer;
  overflow: hidden;
  box-shadow: inset 0 1px 0 rgba(255, 235, 193, 0.08);
  transition: border-color 0.12s ease-out;
}

.unit-card:hover {
  border-color: rgba(251, 205, 120, 0.9);
}

.unit-card__hp {
  position: relative;
  height: 5px;
  background: rgba(20, 10, 4, 0.85);
  border-bottom: 1px solid rgba(70, 47, 24, 0.6);
  overflow: hidden;
}

.unit-card__hp-fill {
  position: absolute;
  inset: 0 auto 0 0;
  background: linear-gradient(90deg, #4ade80, #22c55e);
  transition: width 0.15s ease-out;
}

.unit-card__hp-fill--mid {
  background: linear-gradient(90deg, #facc15, #eab308);
}

.unit-card__hp-fill--low {
  background: linear-gradient(90deg, #f87171, #dc2626);
}

.unit-card__portrait {
  position: relative;
  width: 100%;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  background:
    radial-gradient(circle at top, rgba(220, 165, 80, 0.18), transparent 65%),
    linear-gradient(180deg, rgba(72, 48, 22, 0.6), rgba(40, 24, 10, 0.6));
}

.unit-card__portrait img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  image-rendering: pixelated;
  pointer-events: none;
}

.unit-card__portrait-fallback {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.05em;
  color: #f5ead2;
}

/* Rank chevron stack: 1/2/3 stacked chevrons in the top-right of the
   portrait, mirroring the world overlay. Color is set inline from the
   shared rank palette. Drop shadow keeps the chevrons readable against
   bright portraits. */
.unit-card__rank {
  position: absolute;
  top: 2px;
  left: 2px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
  pointer-events: none;
  filter: drop-shadow(0 0 1.5px rgba(0, 0, 0, 0.95));
}

.unit-card__rank-chevron {
  width: 9px;
  height: 5px;
  display: block;
}

/* Production slot in the selection header — holds the leading-unit card +
   progress bar when a building is training. Sits immediately after the
   title column so the bar can stretch across the rest of the row and
   become the dominant element instead of being tucked into the corner. */
.selection-header__production {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

/* Leading card — the unit currently being trained. Shares the icon-
   container background used elsewhere in the HUD for consistency. */
.production-leading {
  flex: 0 0 auto;
  /* Nudge the current-unit image down so it sits 10px clear of the top. */
  margin-top: 10px;
  width: 70px;
  height: 70px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
}

.production-leading img {
  width: 80%;
  height: 80%;
  object-fit: contain;
  image-rendering: pixelated;
}

/* Queue row — exactly 7 slots, always rendered when training is active so
   queue capacity is always visible at a glance. Smaller than the leading
   portrait above to make the hierarchy obvious. */
.production-queue {
  display: flex;
  flex-wrap: nowrap;
  gap: 4px;
  min-width: 0;
  overflow: hidden;
  /* Shift the queue right so it visually aligns under the leading
     portrait + start of the progress bar above, rather than starting at
     the panel's hard-left edge. (Tracks the larger leading card.) */
  margin-left: 124px;
}

.production-queue__slot {
  flex: 0 1 55px;
  width: 55px;
  height: 55px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  /* Reset native button chrome so the slot reads as a styled cell. */
  border: 0;
  padding: 0;
  font: inherit;
  color: inherit;
  cursor: pointer;
  transition: box-shadow 0.08s ease-out;
}

/* Hover glow matches the shop/vault/upgrade idiom (the shared inset
   --ui-hover-glow from style.css). Replaces the old `filter: brightness()`
   hover, which churned a GPU compositing layer over the pixelated
   icon-container PNG and made the slot flicker. */
.production-queue__slot:not(:disabled):hover {
  box-shadow: var(--ui-hover-glow);
}

/* Empty slots stay rendered but dimmed so the panel always shows the full
   7-slot capacity even when nothing's queued behind the leader. Disabled
   reflects the fact that there's nothing to cancel here. */
.production-queue__slot--empty {
  opacity: 0.5;
  cursor: default;
}

.production-queue__slot img {
  width: 80%;
  height: 80%;
  object-fit: contain;
  image-rendering: pixelated;
}

.production-bar {
  position: relative;
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  height: 20px;
  margin-right: 8px;
  border-radius: 999px;
  border: 1px solid rgba(210, 176, 113, 0.28);
  background:
    linear-gradient(180deg, rgba(54, 34, 20, 0.96), rgba(36, 22, 12, 0.96));
  box-shadow:
    inset 0 1px 0 rgba(255, 235, 193, 0.08),
    inset 0 0 0 1px rgba(70, 47, 24, 0.45);
}

.production-bar__fill {
  position: absolute;
  inset: 0 auto 0 0;
  background:
    linear-gradient(90deg, rgba(187, 127, 48, 0.9), rgba(232, 185, 92, 0.92));
  box-shadow: inset 0 1px 0 rgba(255, 243, 211, 0.22);
}

.production-bar__time {
  position: absolute;
  inset: 0 28px 0 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff4dc;
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.04em;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
  pointer-events: none;
}

.production-bar__cancel {
  position: absolute;
  top: 50%;
  right: 4px;
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transform: translateY(-50%);
  border: 0;
  border-radius: 999px;
  background: rgba(46, 20, 10, 0.72);
  color: #fff4dc;
  font-size: 11px;
  font-weight: 800;
  line-height: 1;
  cursor: pointer;
  box-shadow: inset 0 0 0 1px rgba(229, 193, 132, 0.28);
}

.production-bar__cancel:hover {
  background: rgba(88, 36, 16, 0.86);
}

.construction-card {
  margin-top: 2px;
  /* Inset the progress bar 10px from each edge so it doesn't touch the
     panel sides. */
  padding: 0 10px;
}

.construction-bar {
  position: relative;
  overflow: hidden;
  height: 30px;
  border-radius: 999px;
  border: 1px solid rgba(251, 191, 36, 0.35);
  background: linear-gradient(180deg, rgba(54, 34, 20, 0.96), rgba(36, 22, 12, 0.96));
  box-shadow:
    inset 0 1px 0 rgba(255, 235, 193, 0.08),
    inset 0 0 0 1px rgba(70, 47, 24, 0.45);
}

.construction-bar__fill {
  position: absolute;
  inset: 0 auto 0 0;
  background: linear-gradient(90deg, rgba(161, 105, 20, 0.9), rgba(251, 191, 36, 0.92));
  box-shadow: inset 0 1px 0 rgba(255, 243, 211, 0.22);
}

.construction-bar__time {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff4dc;
  font-size: 13px;
  font-weight: 800;
  letter-spacing: 0.04em;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
  pointer-events: none;
}

/* Assigned-worker count on its own line below the bar, rather than crammed
   into the bar's right edge. */
.construction-workers {
  margin-top: 6px;
  text-align: center;
  color: rgba(255, 244, 220, 0.75);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.03em;
}

.detail-entry {
  display: inline;
}

.detail-entry strong {
  margin-left: 4px;
  color: #fff2d6;
  font-size: 13px;
}

.detail-separator {
  margin-right: 4px;
}

.action-cell {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  color: #f5ead2;
  padding: 0;
  cursor: pointer;
  image-rendering: pixelated;
}

/* Remaining shop stock in the button's bottom-right corner. Mirrors
   MatchMenu's .shop-slot__stock so stock reads identically in the Match Menu
   shop cards and the in-world building panel. */
.action-cell__stock {
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

/* ── Action hover tooltip (move/attack/patrol/build/train) ───────────────── */
/* Shares the stat-tooltip visual language: appears above the cell on hover, */
/* shows the action label as the title, optionally lists resource costs as a */
/* body block. pointer-events: none so the tooltip doesn't swallow clicks.   */
.action-tooltip {
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
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  color: #f5ead2;
  text-align: left;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 10;
}

.action-cell:hover .action-tooltip {
  opacity: 1;
  visibility: visible;
}

.action-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
}

/* Hotkey sub-line shown under the action name. Smaller, tan-colored to
   subordinate it to the action name above. Same hue as the stat-tooltip
   body so the tooltip family reads as one visual style. */
.action-tooltip__hotkey {
  font-size: 11px;
  line-height: 1.5;
  color: #d4b87a;
  letter-spacing: 0.04em;
}

.action-tooltip__body {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.action-tooltip__stat-preview {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
  letter-spacing: 0.02em;
  /* Preserve newlines so multi-line bodies (e.g. the Artificer recipe
     ingredient list) render one item per line. */
  white-space: pre-line;
}

.action-tooltip__row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: #d4b87a;
}

.action-tooltip__gem {
  display: inline-block;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex: 0 0 9px;
  box-shadow: 0 0 2px rgba(0, 0, 0, 0.6);
}

/* PNG-based resource icon for the action tooltip — used when
   assets/resources/<id>.png is available. Sized larger than the gem
   fallback because the art reads better at 14px. */
.action-tooltip__icon {
  width: 14px;
  height: 14px;
  flex: 0 0 14px;
  object-fit: contain;
  image-rendering: pixelated;
}

.action-tooltip__name {
  flex: 1 1 auto;
}

.action-tooltip__amount {
  font-weight: 700;
  color: #ffe9a0;
  font-variant-numeric: tabular-nums;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.9);
}

/* Hover / active highlight uses the shared inset glow (style.css
   --ui-hover-glow) instead of `filter: brightness()`, which flickered the
   pixelated icon-container background (same bug fixed in shop/vault/upgrade).
   The active state is now a persistent gold ring + glow, mirroring
   .ability-slot.is-active and the autocast / channeling ring idiom below.
   The per-state hover rules compose the glow on top of the cell's existing
   ring so a hovered active/autocast cell keeps its state indicator. */
.action-cell:not(:disabled):hover {
  box-shadow: var(--ui-hover-glow);
}

.action-cell--active {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 14px rgba(255, 200, 80, 0.45);
}

.action-cell--active:not(:disabled):hover {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 14px rgba(255, 200, 80, 0.45),
    var(--ui-hover-glow);
}

.action-cell--autocast:not(:disabled):hover {
  box-shadow:
    inset 0 0 0 2px rgba(90, 190, 255, 0.7),
    0 0 7px rgba(90, 190, 255, 0.45),
    var(--ui-hover-glow);
}

/* Auto-cast enabled: glowing border around the ability icon. Placeholder
   styling — TODO: dedicated visual asset / shader / animation for the glow
   border (per spec). Mirrors the perk-rank box-shadow idiom. */
.action-cell--autocast {
  box-shadow:
    inset 0 0 0 2px rgba(90, 190, 255, 0.7),
    0 0 7px rgba(90, 190, 255, 0.45);
}

/* Channeling in progress: pulsing green border. Distinct from auto-cast
   (blue) so the player can glance at the action bar and read "this unit is
   actively channeling this ability right now." The animation pulses between
   a dim and bright green glow over 0.7 s to match the beam's pulse rate. */
.action-cell--channeling {
  animation: ability-channeling-pulse 0.7s ease-in-out infinite;
}

@keyframes ability-channeling-pulse {
  0%, 100% {
    box-shadow:
      inset 0 0 0 2px rgba(50, 210, 100, 0.55),
      0 0 5px rgba(40, 190, 80, 0.30);
  }
  50% {
    box-shadow:
      inset 0 0 0 2px rgba(80, 255, 140, 0.90),
      0 0 10px rgba(60, 230, 110, 0.65);
  }
}

.action-cell:disabled {
  cursor: not-allowed;
}

/* Dim only the cell's content (icon, stock badge, cooldown overlay) when
   disabled — NOT the hover tooltip. Parent `opacity` cascades to every
   descendant and can't be undone by a child, so dimming the button as a
   whole made the tooltip unreadable. Excluding the tooltip keeps it at full
   opacity so a disabled action is still easy to read. */
.action-cell:disabled > :not(.action-tooltip) {
  opacity: 0.42;
}

/* Empty slots keep the icon-container background visible but contain no
   action icon. Acts as a disabled button: no hover effect, no click. */
.action-cell--empty {
  cursor: default;
  pointer-events: none;
}

/* ── Perk display cells ───────────────────────────────────────────────────── */
/* Shared base: display-only, not clickable, slightly darker background.      */
/* pointer-events: auto so the custom hover tooltip can trigger.              */
.action-cell--perk {
  position: relative;
  /* Perk cells are display-only <div>s, so the global :is(button…) hover
     cursor never applies. Inherit the game's default cursor from <html>
     (set inline in main.ts) instead of `cursor: default`, which would force
     the OS white arrow. Same idiom as the vault's inert slots. */
  cursor: inherit;
  pointer-events: auto;
  color: #d4b87a;
}

/* Rank-tinted outlines. The icon-container art is the cell background, so
   rank distinction is conveyed by an inset glow rather than a real border. */
.action-cell--perk-bronze {
  box-shadow: inset 0 0 0 2px rgba(200, 140, 60, 0.55);
}

.action-cell--perk-silver {
  box-shadow: inset 0 0 0 2px rgba(180, 195, 210, 0.50);
}

.action-cell--perk-gold {
  box-shadow:
    inset 0 0 0 2px rgba(240, 210, 80, 0.65),
    0 0 6px rgba(200, 165, 40, 0.30);
}

/* Locked / empty rank slot: dim the icon and border further. */
.action-cell--perk:has(.action-icon) .action-icon {
  opacity: 0.9;
}
.action-cell--perk-silver .action-icon,
.action-cell--perk-gold .action-icon {
  opacity: 0.35;
}

/* ── Perk cooldown overlay ───────────────────────────────────────────────── */
/* Conic gradient covers the "remaining" wedge of the icon and clears as time   */
/* elapses. --perk-cooldown-cleared is the already-elapsed angle (0deg at the   */
/* start of the cooldown, 360deg at the end); set per-element via :style.      */
/* The overlay is pointer-events: none so the hover tooltip still fires.       */
.perk-cooldown-overlay {
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  display: flex;
  align-items: center;
  justify-content: center;
  background: conic-gradient(
    from 0deg,
    transparent 0deg var(--perk-cooldown-cleared, 0deg),
    rgba(0, 0, 0, 0.68) var(--perk-cooldown-cleared, 0deg) 360deg
  );
}

.perk-cooldown-number {
  font-size: 18px;
  font-weight: 700;
  color: #fef4d3;
  text-shadow:
    0 0 3px rgba(0, 0, 0, 0.9),
    0 1px 2px rgba(0, 0, 0, 0.85);
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

/* Drop the icon's saturation/brightness while on cooldown for a "disabled"   */
/* readability cue beyond the dark overlay alone. Shared between perk cells   */
/* and regular ability buttons — both use the same conic-gradient overlay so  */
/* they should also share the underlying icon-dim treatment.                  */
.action-cell--perk-cooldown .action-icon,
.action-cell--cooldown .action-icon {
  opacity: 0.45;
  filter: grayscale(0.6);
}

/* ── Perk hover tooltip ──────────────────────────────────────────────────── */
.perk-tooltip {
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
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  color: #f5ead2;
  text-align: left;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 10;
}

.action-cell--perk:hover .perk-tooltip {
  opacity: 1;
  visibility: visible;
}

/* Edge-anchor tooltips so they don't get clipped at screen edges. The
   actions panel sits at the right of the HUD, so column 4 cells anchor the
   tooltip to the cell's right edge; column 1 cells anchor to the left. */
.action-grid > *:nth-child(4n) .perk-tooltip,
.action-grid > *:nth-child(4n) .action-tooltip {
  left: auto;
  right: 0;
  transform: none;
}

.action-grid > *:nth-child(4n + 1) .perk-tooltip,
.action-grid > *:nth-child(4n + 1) .action-tooltip {
  left: 0;
  right: auto;
  transform: none;
}

.perk-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
}

.perk-tooltip__body {
  font-size: 12px;
  line-height: 1.5;
  color: #d4b87a;
}

/* ── Townhall tier section ───────────────────────────────────────────────── */

.townhall-tier {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 2px;
  /* Inset the progress bar 10px from each edge so it doesn't touch the
     panel sides — matches the construction card. */
  padding: 0 10px;
}

/* In-progress upgrade bar — same visual language as the construction bar */
.townhall-upgrade-bar {
  position: relative;
  overflow: hidden;
  height: 26px;
  border-radius: 999px;
  border: 1px solid rgba(251, 191, 36, 0.35);
  background: linear-gradient(180deg, rgba(54, 34, 20, 0.96), rgba(36, 22, 12, 0.96));
  box-shadow:
    inset 0 1px 0 rgba(255, 235, 193, 0.08),
    inset 0 0 0 1px rgba(70, 47, 24, 0.45);
}

.townhall-upgrade-bar__fill {
  position: absolute;
  inset: 0 auto 0 0;
  background: linear-gradient(90deg, rgba(161, 105, 20, 0.9), rgba(251, 191, 36, 0.92));
  box-shadow: inset 0 1px 0 rgba(255, 243, 211, 0.22);
  transition: width 0.3s ease-out;
}

.townhall-upgrade-bar__label {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff4dc;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.04em;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
  pointer-events: none;
}

</style>
