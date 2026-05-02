<template>
  <footer
    class="selection-hud"
    :style="{
      '--ui-panel-image': `url(${uiPanelUrl})`,
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
                class="production-bar__cancel"
                type="button"
                aria-label="Cancel Training"
                title="Cancel Training"
                @click="$emit('action', 'cancel-training')"
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
            <div class="construction-bar__builders">{{ ui.selection.construction.builderCount }}/3</div>
          </div>
        </div>

        <!-- Townhall tier badge + upgrade controls. Only shown when a townhall
             is selected (owned by the local player). Tier badge always visible;
             upgrade button shown when tier < 3 and no upgrade is in progress;
             progress bar shown while an upgrade is underway. -->
        <div
          v-if="ui.selection.kind === 'building' && ui.selectedBuildingType === 'townhall'"
          class="townhall-tier"
        >
          <div class="townhall-tier__badge">
            {{ townhallTierLabel }}
            <span class="townhall-tier__tier-num">Tier {{ ui.townHallTier || 1 }}</span>
          </div>

          <!-- Upgrade-in-progress bar -->
          <div
            v-if="townhallUpgradeInProgress"
            class="townhall-upgrade-bar"
          >
            <div
              class="townhall-upgrade-bar__fill"
              :style="{ width: `${townhallUpgradeProgress * 100}%` }"
            />
            <div class="townhall-upgrade-bar__label">
              {{ (ui.townHallTier || 1) === 1 ? 'Upgrading to Keep...' : 'Upgrading to Castle...' }}
            </div>
          </div>

          <!-- Tier-up button — hidden while an upgrade is in progress or tier is maxed -->
          <button
            v-else-if="(ui.townHallTier || 1) < 3"
            type="button"
            class="townhall-upgrade-btn"
            @click="$emit('action', 'upgrade-townhall')"
          >
            {{ townhallUpgradeLabel }}
          </button>
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
             rendered when exactly one unit is selected — multi-select and
             building selections hide the panel entirely. Slot count is
             fixed at INVENTORY_DISPLAY_SLOTS; the unit's inventory.size
             determines how many of those are unlocked vs. locked. Locked
             slots overlay the lock icon; held items render their own icon
             over the container; empty unlocked slots show only the
             icon-container art. -->
        <div
          v-if="ui.selectedUnits.length === 1"
          class="details-inventory"
          aria-label="Inventory"
        >
          <div
            v-for="slot in inventorySlots"
            :key="slot.index"
            class="inventory-slot"
            :class="{ 'inventory-slot--locked': slot.locked }"
            :title="slot.title"
          >
            <img
              v-if="slot.iconUrl"
              :src="slot.iconUrl"
              :alt="slot.title"
              class="inventory-slot__icon"
              draggable="false"
            />
          </div>
        </div>
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
              }"
              :disabled="ui.selection.actions[i - 1].disabled"
              type="button"
              @click="$emit('action', ui.selection.actions[i - 1].id)"
            >
              <ActionIcon :action="ui.selection.actions[i - 1]" />
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
                  v-if="parseActionLabel(ui.selection.actions[i - 1].label).hotkey"
                  class="action-tooltip__hotkey"
                >Hotkey: {{ parseActionLabel(ui.selection.actions[i - 1].label).hotkey }}</div>
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
import type { ActionItem } from '@/game/core/GameState'
import type { GameUiSnapshot } from '@/game/core/GameClient'
import { getUnitPortraitUrl } from '@/game/rendering/unitSprites'
import { getRankToneColor } from '@/game/rendering/rankColors'
import ActionIcon from '@/components/ActionIcon.vue'
import uiPanelUrl from '@/assets/ui/ui_panel_56x56_slice17.png'
import iconContainerUrl from '@/assets/ui/icon-container.png'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'
import { getActionIconImage } from '@/game/rendering/actionIconSprites'
import { getResourceIconUrl } from '@/game/rendering/resourceSprites'

const emit = defineEmits<{
  action: [actionId: string]
  'select-unit': [unitId: number]
  'deselect-unit': [unitId: number]
  'minimap-rect': [rect: DOMRect | null]
}>()

const props = defineProps<{
  ui: GameUiSnapshot
}>()

// ── Minimap panel rect tracking ────────────────────────────────────────────
// The canvas-rendered minimap reads its bounds from GameState; we push the
// panel's viewport rect up to MatchView (and through GameClient → state) any
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

// ── Townhall tier helpers ───────────────────────────────────────────────────

const TOWNHALL_TIER_NAMES = ['Town Hall', 'Keep', 'Castle']

const townhallTierLabel = computed(() => {
  const tier = props.ui.townHallTier || 1
  return TOWNHALL_TIER_NAMES[tier - 1] ?? 'Town Hall'
})

const townhallUpgradeLabel = computed(() => {
  const tier = props.ui.townHallTier || 1
  if (tier === 1) return 'Upgrade to Keep — 400g / 250w'
  if (tier === 2) return 'Upgrade to Castle — 800g / 500w'
  return ''
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
    return {
      id: u.id,
      title: `${u.name}  ${hp} / ${max}`,
      portraitUrl: getUnitPortraitUrl(u.path, u.unitType),
      initials: (u.name || u.unitType || '?').slice(0, 2).toUpperCase(),
      hpFraction,
      rank: u.rank ?? '',
      rankChevrons,
      rankColor: getRankToneColor(u.rank, 'light'),
    }
  })
})

// Inventory display: always shows INVENTORY_DISPLAY_SLOTS slots so the panel
// has a stable shape regardless of unit progression. The unit's
// inventory.size determines how many of those are unlocked; the rest render
// the lock icon. Held items render their icon over the container, looked up
// via ITEM_DEF_MAP[itemId].iconKey through the action-icon sprite loader
// (so item art lives in src/assets/actions/<iconKey>.png).
const INVENTORY_DISPLAY_SLOTS = 2

const inventorySlots = computed(() => {
  // Inventory only shows for single-unit selections.
  const unit = props.ui.selectedUnits.length === 1 ? props.ui.selectedUnits[0] : null
  const inventory = unit?.inventory
  const size = inventory?.size ?? 0
  const slots = inventory?.slots ?? []

  return Array.from({ length: INVENTORY_DISPLAY_SLOTS }, (_, index) => {
    const locked = index >= size
    if (locked) {
      return {
        index,
        locked: true,
        iconUrl: getActionIconImage('lock')?.src ?? null,
        title: 'Locked slot',
      }
    }

    const held = slots[index] ?? null
    if (!held) {
      return { index, locked: false, iconUrl: null, title: 'Empty slot' }
    }

    const def = ITEM_DEF_MAP.get(held.itemId)
    const iconKey = def?.iconKey ?? held.itemId
    return {
      index,
      locked: false,
      iconUrl: getActionIconImage(iconKey)?.src ?? null,
      title: def?.displayName ?? held.itemId,
    }
  })
})

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
</script>

<style scoped>
.selection-hud {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 18px;
  z-index: 5;
  /* Standardized fixed sizes — no clamp(), no flex distribution. The HUD
     stays a constant size regardless of viewport changes. text-align +
     inline-block on children is the non-flex equivalent of justify-content
     center; font-size: 0 here suppresses the inter-element whitespace and
     each panel resets its own font-size. */
  text-align: center;
  font-size: 0;
  --minimap-panel-width: 220px;
  --details-panel-width: 600px;
  --actions-panel-width: 260px;
  /* All three panels share the same height so the frame reads as one
     continuous footer rather than two adjacent panels at different sizes. */
  --main-panel-height: 200px;
  --hud-height: 200px;
  /* The details panel is pushed down by this amount, leaving a transparent
     gap above its frame while minimap/actions stay at full height. */
  --details-top-spacer: 32px;
  /* Horizontal breathing room between the details frame and the minimap /
     actions panels on either side of it. */
  --panel-side-gap: 12px;
  pointer-events: none;
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
  border: 17px solid transparent;
  border-radius: 0;
  border-image-source: var(--ui-panel-image);
  border-image-slice: 17 fill;
  border-image-width: 17px;
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
  border-image-slice: 17;
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

/* Right side: vertical column of inventory slots, right-aligned. Each
   slot uses the shared icon-container background. Display-only for now —
   no pointer events, no hover effect. */
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
  pointer-events: none;
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
  display: flex;
  flex-direction: column;
  gap: 8px;
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
  transition: border-color 0.12s ease-out, transform 0.08s ease-out;
}

.unit-card:hover {
  border-color: rgba(251, 205, 120, 0.9);
  transform: translateY(-1px);
}

.unit-card:active {
  transform: translateY(0);
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
  width: 56px;
  height: 56px;
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
     the panel's hard-left edge. */
  margin-left: 110px;
}

.production-queue__slot {
  flex: 0 1 44px;
  width: 44px;
  height: 44px;
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
  transition: filter 0.08s ease-out;
}

.production-queue__slot:not(:disabled):hover {
  filter: brightness(1.15);
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
  inset: 0 40px 0 0;
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

.construction-bar__builders {
  position: absolute;
  top: 50%;
  right: 8px;
  transform: translateY(-50%);
  color: rgba(255, 244, 220, 0.75);
  font-size: 11px;
  font-weight: 700;
  pointer-events: none;
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

.action-cell:not(:disabled):hover {
  filter: brightness(1.15);
}

.action-cell--active {
  filter: brightness(1.25) saturate(1.15);
}

.action-cell:disabled {
  opacity: 0.42;
  cursor: not-allowed;
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
  cursor: default;
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
/* readability cue beyond the dark overlay alone.                             */
.action-cell--perk-cooldown .action-icon {
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
}

.townhall-tier__badge {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 5px 10px;
  border-radius: 6px;
  background: linear-gradient(180deg, rgba(54, 34, 20, 0.88), rgba(36, 22, 12, 0.88));
  border: 1px solid rgba(210, 176, 113, 0.3);
  font-size: 13px;
  font-weight: 700;
  color: #f5ead2;
}

.townhall-tier__tier-num {
  font-size: 11px;
  font-weight: 600;
  color: #d4b87a;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.townhall-upgrade-btn {
  width: 100%;
  padding: 6px 10px;
  border-radius: 6px;
  border: 1px solid rgba(210, 176, 113, 0.4);
  background: linear-gradient(180deg, rgba(100, 66, 30, 0.95), rgba(60, 38, 16, 0.98));
  color: #f5ead2;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.03em;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s;
  text-align: center;
}

.townhall-upgrade-btn:hover {
  background: linear-gradient(180deg, rgba(140, 94, 44, 1), rgba(88, 54, 22, 1));
  border-color: rgba(240, 200, 120, 0.6);
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
