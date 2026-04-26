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
        <!-- Header: title/subtitle/rank shown only for single-unit / building.
              Multi-unit selection shows the cards instead. -->
        <header v-if="unitCards.length <= 1" class="selection-header">
          <div class="selection-header__copy">
            <div class="selection-title">
              {{ ui.selection.title }}<span
                v-if="ui.selection.kind === 'unit' && ui.selection.pathLabel"
                class="selection-title__path"
              > ({{ ui.selection.pathLabel }})</span>
            </div>
            <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
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
          <div v-if="iconDetails.length > 0" class="detail-stats">
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

        <div v-if="ui.selection.production" class="production-card">
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
        <div v-if="inlineDetails.length > 0" class="detail-inline">
          <template v-for="(detail, index) in inlineDetails" :key="detail.id">
            <span class="detail-entry" :title="detail.tooltip">
              <span>{{ detail.label }}</span>
              <strong v-if="detail.value">{{ detail.value }}</strong>
            </span>
            <span v-if="index < inlineDetails.length - 1" class="detail-separator">,</span>
          </template>
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
                'action-cell--has-cost': !!ui.selection.actions[i - 1].cost?.length,
              }"
              :disabled="ui.selection.actions[i - 1].disabled"
              :title="ui.selection.actions[i - 1].cost?.length ? undefined : ui.selection.actions[i - 1].label"
              type="button"
              @click="$emit('action', ui.selection.actions[i - 1].id)"
            >
              <ActionIcon :action="ui.selection.actions[i - 1]" />
              <div
                v-if="ui.selection.actions[i - 1].cost?.length"
                class="cost-tooltip"
              >
                <div class="cost-tooltip__title">{{ ui.selection.actions[i - 1].label }}</div>
                <div class="cost-tooltip__body">
                  <div
                    v-for="c in ui.selection.actions[i - 1].cost"
                    :key="c.resourceId"
                    class="cost-tooltip__row"
                  >
                    <span
                      class="cost-tooltip__gem"
                      :style="{ background: `linear-gradient(180deg, ${c.accent}, rgba(0,0,0,0.55))` }"
                    />
                    <span class="cost-tooltip__name">{{ resourceDisplayName(c.resourceId) }}</span>
                    <span class="cost-tooltip__amount">{{ c.amount }}</span>
                  </div>
                </div>
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
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
  scrollbar-width: none;
  pointer-events: auto;
}

.selection-panel--details::-webkit-scrollbar {
  display: none;
}

.selection-header {
  display: flex;
  flex-direction: row;
  align-items: stretch;
  gap: 14px;
  flex: 0 0 auto;
}

.selection-header__copy {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
}

.selection-panel--actions {
  position: absolute;
  top: 0;
  /* Pushed right so its left-edge frame doesn't crowd the details panel. */
  right: calc(0px - var(--panel-side-gap));
  width: var(--actions-panel-width);
  height: var(--hud-height);
  font-size: 13px;
  /* overflow: visible so perk hover tooltips can extend above the panel. */
  overflow: visible;
  pointer-events: auto;
  padding: 8px;
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

.selection-progression {
  margin-top: auto;
  padding-top: 8px;
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
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
  grid-auto-rows: auto;
  gap: 4px;
  flex: 0 0 auto;
}

.action-grid > * {
  aspect-ratio: 1 / 1;
}

.detail-stats {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  flex: 0 0 auto;
  min-width: 90px;
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

.production-card {
  margin-top: 2px;
}

.production-bar {
  position: relative;
  overflow: hidden;
  height: 30px;
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
  inset: 0 32px 0 0;
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

.production-bar__cancel {
  position: absolute;
  top: 50%;
  right: 5px;
  width: 20px;
  height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transform: translateY(-50%);
  border: 0;
  border-radius: 999px;
  background: rgba(46, 20, 10, 0.72);
  color: #fff4dc;
  font-size: 12px;
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

/* ── Cost hover tooltip (build/train actions) ────────────────────────────── */
/* Mirrors the perk-tooltip visual language: appears above the cell on hover, */
/* lists each resource cost with a gem, name, and amount. pointer-events:     */
/* none so the tooltip doesn't swallow clicks on the button itself.           */
.cost-tooltip {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 50%;
  transform: translateX(-50%);
  min-width: 140px;
  max-width: 220px;
  padding: 8px 10px;
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

.action-cell--has-cost:hover .cost-tooltip {
  opacity: 1;
  visibility: visible;
}

.cost-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
}

.cost-tooltip__body {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.cost-tooltip__row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.2;
  color: #d4b87a;
}

.cost-tooltip__gem {
  display: inline-block;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex: 0 0 9px;
  box-shadow: 0 0 2px rgba(0, 0, 0, 0.6);
}

.cost-tooltip__name {
  flex: 1 1 auto;
}

.cost-tooltip__amount {
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

.action-cell--empty {
  background: none;
  cursor: default;
  pointer-events: none;
  opacity: 0.25;
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
  bottom: calc(100% + 8px);
  left: 50%;
  transform: translateX(-50%);
  min-width: 180px;
  max-width: 260px;
  padding: 8px 10px;
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
.action-grid > *:nth-child(4n) .cost-tooltip {
  left: auto;
  right: 0;
  transform: none;
}

.action-grid > *:nth-child(4n + 1) .perk-tooltip,
.action-grid > *:nth-child(4n + 1) .cost-tooltip {
  left: 0;
  right: auto;
  transform: none;
}

.perk-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 3px;
}

.perk-tooltip__body {
  font-size: 12px;
  line-height: 1.4;
  color: #d4b87a;
}

</style>
