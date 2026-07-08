<template>
  <div class="advancements" :class="{ 'advancements--single': unitType }">
    <div class="advancements__header">
      <h1 class="advancements__title">{{ titleText }}</h1>
      <button
        v-if="!unitType"
        type="button"
        class="advancements__reset"
        :disabled="isBusy || acquiredIds.size === 0"
        title="Refund all advancements and clear them (for before/after testing)"
        @click="reset"
      >
        Reset
      </button>
    </div>

    <div v-if="error" class="advancements__error" role="alert">{{ error }}</div>

    <div class="advancements__rows">
      <div
        v-for="row in visibleRows"
        :key="row.unitType"
        class="advancement-row"
      >
        <div class="advancement-row__character">
          <div
            v-if="!unitType"
            class="advancement-row__portrait"
            :style="{ backgroundImage: portraitBg(row.unitType) }"
            :aria-label="unitDisplayName(row.unitType)"
            role="img"
          ></div>
          <div v-if="!unitType" class="advancement-row__name">{{ unitDisplayName(row.unitType) }}</div>
        </div>

        <div class="advancement-row__nodes">
          <div
            v-for="(node, idx) in row.nodes"
            :key="node.id"
            class="advancement-node-cell"
          >
            <button
              type="button"
              class="advancement-node"
              :class="[
                nodeShapeClass(node),
                nodeStateClass(row, idx),
              ]"
              :style="{ backgroundImage: nodeIcon(node, isAcquired(node.id)) }"
              :disabled="isBusy || isAcquired(node.id) || !isAvailable(row, idx) || !canAcquire(node)"
              :aria-label="`${node.name} (${nodeStateLabel(row, idx)})`"
              @click="purchase(node.id)"
            >
              <UiTooltip :title="node.name" :body="tooltipBody(node)" />
            </button>
            <span v-if="unitType" class="advancement-node__cost">{{ node.cost }} DP</span>
            <span v-if="unitType && node.kind === 'major'" class="advancement-node__badge-cost">
              1<img :src="medalSlotUrl" class="advancement-node__badge-icon" alt="Conquest Badge" />
            </span>
          </div>
        </div>

      </div>
    </div>

    <div v-if="!unitType" class="advancements__pager">
      <button
        type="button"
        class="advancements__pager-btn advancements__pager-btn--prev"
        :disabled="pageIndex === 0"
        aria-label="Previous page"
        @click="prevPage"
      >
        <svg viewBox="0 0 32 24" aria-hidden="true" class="advancements__arrow">
          <path
            d="M 4 12 L 14 4 L 14 9 L 28 9 L 28 15 L 14 15 L 14 20 Z"
            fill="currentColor"
          />
          <path
            d="M 4 12 L 14 4 L 14 9 L 28 9 L 28 15 L 14 15 L 14 20 Z"
            fill="none"
            stroke="currentColor"
            stroke-width="1"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <span class="advancements__pager-label">{{ pageIndex + 1 }} / {{ totalPages }}</span>
      <button
        type="button"
        class="advancements__pager-btn advancements__pager-btn--next"
        :disabled="pageIndex >= totalPages - 1"
        aria-label="Next page"
        @click="nextPage"
      >
        <svg viewBox="0 0 32 24" aria-hidden="true" class="advancements__arrow">
          <path
            d="M 4 12 L 14 4 L 14 9 L 28 9 L 28 15 L 14 15 L 14 20 Z"
            fill="currentColor"
          />
          <path
            d="M 4 12 L 14 4 L 14 9 L 28 9 L 28 15 L 14 15 L 14 20 Z"
            fill="none"
            stroke="currentColor"
            stroke-width="1"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import UiTooltip from '@/components/ui/UiTooltip.vue'
import { useAdvancements } from '@/composables/useAdvancements'
import type { UnitAdvancementNode, UnitAdvancementTrack } from '@/types/profile'
import acolytePortraitUrl from '@/assets/units/human/acolyte/portrait.png'
import adeptPortraitUrl from '@/assets/units/human/adept/portrait.png'
import archerPortraitUrl from '@/assets/units/human/archer/portrait.png'
import soldierPortraitUrl from '@/assets/units/human/soldier/portrait.png'
import workerPortraitUrl from '@/assets/units/human/worker/portrait.png'
import unsealedUrl from '@/assets/ui/buttons/war_room/advancement/unsealed.png'
import waxSealUrl from '@/assets/ui/buttons/war_room/advancement/wax-seal.png'
import medalSlotEmptyUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot-empty.png'
import medalSlotUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot.png'

// When `unitType` is provided, the component renders only that unit's track
// (and hides pagination) — used by the Barracks unit-detail popup. With no
// prop it shows every track with pagination, as in the War Room.
const props = withDefaults(defineProps<{
  unitType?: string
}>(), {
  unitType: undefined,
})

const { catalog, acquiredIds, isBusy, error, isAcquired, canAcquire, conquestBadges, purchase, reset } =
  useAdvancements()

// Portrait lookup: unitType -> static import URL. Extended as new unit types
// get advancement tracks. Unknown types fall back to a transparent pixel.
const PORTRAIT_MAP: Record<string, string> = {
  soldier: soldierPortraitUrl,
  archer: archerPortraitUrl,
  acolyte: acolytePortraitUrl,
  adept: adeptPortraitUrl,
  worker: workerPortraitUrl,
}

// Human-readable label for a unit type. Falls back to capitalised unitType.
function unitDisplayName(unitType: string): string {
  const overrides: Record<string, string> = {
    soldier: 'Soldier',
    archer: 'Archer',
    acolyte: 'Acolyte',
    adept: 'Adept',
    worker: 'Worker',
  }
  return overrides[unitType] ?? (unitType.charAt(0).toUpperCase() + unitType.slice(1))
}

// In single-unit (Barracks popup) mode the header names the unit, e.g.
// "Advancements - Soldier". The CSS uppercases it for display.
const titleText = computed(() =>
  props.unitType ? `Advancements - ${unitDisplayName(props.unitType)}` : 'Advancements',
)

function portraitBg(unitType: string): string {
  const url = PORTRAIT_MAP[unitType]
  return url ? `url(${url})` : 'none'
}

// Preferred display order for unit tracks. The server serves tracks sorted
// alphabetically by unitType; we override that for presentation so the roster
// reads in roster order rather than A–Z. Unit types not listed here fall back
// to alphabetical, after the listed ones.
const UNIT_DISPLAY_ORDER = ['soldier', 'archer', 'acolyte', 'adept', 'worker']

function unitOrderRank(unitType: string): number {
  const idx = UNIT_DISPLAY_ORDER.indexOf(unitType)
  return idx === -1 ? UNIT_DISPLAY_ORDER.length : idx
}

// Only show tracks that have at least one node — empty tracks (acolyte/adept
// before their advancements.json ships) are hidden rather than shown as ghost
// rows — then order them by UNIT_DISPLAY_ORDER for display.
const rowsWithNodes = computed<UnitAdvancementTrack[]>(() =>
  catalog.value
    .filter((t) => t.nodes.length > 0)
    .filter((t) => !props.unitType || t.unitType === props.unitType)
    .slice()
    .sort((a, b) => {
      const ra = unitOrderRank(a.unitType)
      const rb = unitOrderRank(b.unitType)
      return ra !== rb ? ra - rb : a.unitType.localeCompare(b.unitType)
    }),
)

const PAGE_SIZE = 3
const pageIndex = ref(0)

const totalPages = computed(() => Math.max(1, Math.ceil(rowsWithNodes.value.length / PAGE_SIZE)))

const visibleRows = computed<UnitAdvancementTrack[]>(() => {
  const start = pageIndex.value * PAGE_SIZE
  return rowsWithNodes.value.slice(start, start + PAGE_SIZE)
})

function prevPage() {
  if (pageIndex.value > 0) pageIndex.value -= 1
}

function nextPage() {
  if (pageIndex.value < totalPages.value - 1) pageIndex.value += 1
}

function isAvailable(track: UnitAdvancementTrack, idx: number): boolean {
  if (idx === 0) return true
  return isAcquired(track.nodes[idx - 1].id)
}

function nodeShapeClass(node: UnitAdvancementNode): string {
  return node.kind === 'major' ? 'advancement-node--square' : 'advancement-node--circle'
}

function nodeIcon(node: UnitAdvancementNode, acquired: boolean): string {
  if (node.kind === 'major') {
    return `url(${acquired ? medalSlotUrl : medalSlotEmptyUrl})`
  }
  return `url(${acquired ? waxSealUrl : unsealedUrl})`
}

function nodeStateClass(track: UnitAdvancementTrack, idx: number): string {
  const node = track.nodes[idx]
  if (isAcquired(node.id)) return 'advancement-node--acquired'
  if (isAvailable(track, idx)) {
    return canAcquire(node)
      ? 'advancement-node--available'
      : 'advancement-node--unaffordable'
  }
  return 'advancement-node--locked'
}

function nodeStateLabel(track: UnitAdvancementTrack, idx: number): string {
  const node = track.nodes[idx]
  if (isAcquired(node.id)) return 'acquired'
  if (isAvailable(track, idx)) {
    return canAcquire(node)
      ? 'available'
      : (node.kind === 'major' && conquestBadges.value < 1
          ? 'requires a Conquest Badge'
          : 'not enough Dominion Points')
  }
  return 'locked'
}

function tooltipBody(node: UnitAdvancementNode): string {
  const lines: string[] = []
  if (node.description) lines.push(node.description)
  // In single-unit mode the cost is shown as a label under each node, so it's
  // omitted from the tooltip to avoid duplication.
  if (!props.unitType) lines.push(`Cost: ${node.cost} DP`)
  if (node.kind === 'major') lines.push('Requires: 1 Conquest Badge')
  return lines.join('\n')
}


</script>

<style scoped>
.advancements {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  padding: 1% 2%;
  box-sizing: border-box;
  /*
   * Single scale unit driving every size below. `--s` is ~1px at the
   * reference parchment width (~1076px, i.e. a 1080p viewport) and scales
   * linearly with the container via cqw, so the whole panel grows and
   * shrinks with the parchment. Every value below is `calc(var(--s) * <px>)`
   * — a 1:1 map from the original fixed pixels. Retune the whole panel by
   * changing this one number.
   */
  --s: 0.0929cqw;
  /* No overflow clipping — tooltips on top-row nodes need to extend above
     the panel bounds. Pagination already keeps content from overflowing. */
}

.advancements__header {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: calc(var(--s) * 8);
  gap: calc(var(--s) * 16);
}

/* Single-unit (Barracks popup) mode: no reset/portrait. Header inherits the
   default `justify-content: space-between`, which left-aligns the title
   when the reset button is hidden, matching the Campaign panel header. */

/*
 * Single-unit mode has the whole panel for one row, so the nodes get larger
 * and are centered both axes. The (now-empty) character column is removed so
 * its gap doesn't bias the horizontal centering.
 */
.advancements--single .advancements__rows {
  justify-content: center;
}

.advancements--single .advancement-row {
  padding-left: 0;
  justify-content: flex-start;
}

.advancements--single .advancement-row__character {
  display: none;
}

.advancements--single .advancement-row__nodes {
  justify-content: flex-start;
  align-items: flex-end;
  gap: calc(var(--s) * 18);
}

/* Node + its cost label stack vertically; bottom-aligned in the row so the
   cost labels line up regardless of node size. */
.advancement-node-cell {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: calc(var(--s) * 6);
}

.advancement-node__cost {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: #3a1f0a;
  white-space: nowrap;
}

/* Conquest Badge cost for major (medal) nodes. Absolutely positioned so it
   hangs below the DP cost without adding to the cell's laid-out height — that
   keeps every node's "DP" label on the same line across the bottom-aligned
   row, with the badge dangling underneath only on major nodes. */
.advancement-node__badge-cost {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  margin-top: calc(var(--s) * 4);
  display: flex;
  justify-content: center;
  align-items: center;
  gap: calc(var(--s) * 5);
  font-family: var(--font-title);
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: #3a1f0a;
  white-space: nowrap;
}

.advancement-node__badge-icon {
  height: calc(var(--s) * 24);
  width: calc(var(--s) * 24);
  object-fit: contain;
}

.advancements--single .advancement-node {
  width: calc(var(--s) * 76);
  height: calc(var(--s) * 76);
}

.advancements--single .advancement-node--square {
  width: calc(var(--s) * 96);
  height: calc(var(--s) * 96);
}

.advancements__title {
  margin: 0;
  text-align: left;
  font-family: var(--font-title);
  font-size: calc(var(--s) * 18);
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #3a1f0a;
}

.advancements__reset {
  flex: 0 0 auto;
  padding: calc(var(--s) * 4) calc(var(--s) * 12);
  border: 1px solid #9a4a2a;
  border-radius: 4px;
  background-color: rgba(150, 50, 30, 0.16);
  font-family: var(--font-title);
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #7a1a0a;
}

.advancements__reset:hover:not(:disabled) {
  background-color: rgba(150, 50, 30, 0.3);
  border-color: #b85a36;
}

.advancements__reset:disabled {
  opacity: 0.4;
}

.advancements__error {
  flex: 0 0 auto;
  padding: calc(var(--s) * 4) calc(var(--s) * 8);
  margin-bottom: calc(var(--s) * 6);
  border-radius: 4px;
  background-color: rgba(180, 40, 20, 0.15);
  border: 1px solid rgba(180, 40, 20, 0.4);
  font-family: var(--font-title);
  font-size: calc(var(--s) * 11);
  color: #7a1a0a;
}

.advancements__rows {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 18);
}

.advancement-row {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 18);
}

/*
 * Stagger left padding so the rows lean inward following the slanted
 * left edge of the parchment in the war_room_bg artwork.
 */
.advancement-row:nth-child(1) { padding-left: calc(var(--s) * 60); }
.advancement-row:nth-child(2) { padding-left: calc(var(--s) * 30); }
.advancement-row:nth-child(3) { padding-left: 0; }

.advancement-row__character {
  flex: 0 0 auto;
  display: flex;
  flex-direction: row-reverse;
  align-items: center;
  gap: calc(var(--s) * 8);
}

.advancement-row__name {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 13);
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #3a1f0a;
  white-space: nowrap;
}

.advancement-row__portrait {
  flex: 0 0 auto;
  width: calc(var(--s) * 52);
  height: calc(var(--s) * 52);
  border-radius: 50%;
  border: 2px solid #c68c44;
  background-color: #1a1208;
  background-size: cover;
  background-position: center top;
  background-repeat: no-repeat;
  image-rendering: pixelated;
  box-shadow:
    inset 0 0 0 2px rgba(0, 0, 0, 0.4),
    0 2px 4px rgba(0, 0, 0, 0.6);
}

.advancement-row__nodes {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 10);
  flex-wrap: nowrap;
}

.advancement-node {
  position: relative;
  flex: 0 0 auto;
  width: calc(var(--s) * 48);
  height: calc(var(--s) * 48);
  padding: 0;
  border: 0;
  background-color: transparent;
  background-repeat: no-repeat;
  background-position: center;
  background-size: contain;
  image-rendering: pixelated;
  transition:
    transform 120ms ease,
    filter 120ms ease,
    opacity 120ms ease;
}

.advancement-node:hover .ui-tooltip,
.advancement-node:focus-visible .ui-tooltip {
  opacity: 1;
  visibility: visible;
}

.advancement-node--circle {
  /* Hit area matches the rendered disc. */
  border-radius: 50%;
}

.advancement-node--square {
  width: calc(var(--s) * 60);
  height: calc(var(--s) * 60);
  border-radius: 4px;
}

.advancement-node--acquired {
  filter: drop-shadow(0 0 6px rgba(230, 179, 90, 0.55));
}

.advancement-node--available {
  animation: advancement-pulse 1.6s ease-in-out infinite;
}

.advancement-node--unaffordable {
  /* Unlocked but the player can't pay yet — keep the art readable but
     drop the pulse and dim slightly to signal "wait". */
  filter: grayscale(0.4) brightness(0.85);
  opacity: 0.85;
  cursor: not-allowed;
}

.advancement-node--locked {
  filter: grayscale(0.85) brightness(0.7);
  opacity: 0.75;
  cursor: not-allowed;
}

.advancement-node:hover:not(:disabled) {
  transform: translateY(-2px);
  filter: drop-shadow(0 0 8px rgba(255, 220, 140, 0.8));
}

.advancement-node:active:not(:disabled) {
  transform: translateY(0);
  filter: brightness(0.9);
}

@keyframes advancement-pulse {
  0%, 100% {
    filter: drop-shadow(0 0 3px rgba(230, 179, 90, 0.35));
  }
  50% {
    filter: drop-shadow(0 0 10px rgba(230, 179, 90, 0.85));
  }
}

.advancements__pager {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: calc(var(--s) * 14);
  padding-top: calc(var(--s) * 8);
  transform: translate(calc(var(--s) * -65), calc(var(--s) * -25));
}

.advancements__pager-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: calc(var(--s) * 4) calc(var(--s) * 6);
  border: 0;
  background: transparent;
  color: #3a1f0a;
  transition:
    transform 120ms ease,
    filter 120ms ease,
    opacity 120ms ease,
    color 120ms ease;
}

.advancements__arrow {
  width: calc(var(--s) * 32);
  height: calc(var(--s) * 24);
  display: block;
  /* The double-stroked path catches a touch of ink-bleed underneath for a
     stamped-on-parchment feel. */
  filter: drop-shadow(0 1px 0 rgba(58, 31, 10, 0.25));
}

.advancements__pager-btn--next .advancements__arrow {
  transform: scaleX(-1);
}

.advancements__pager-btn:hover:not(:disabled) {
  color: #1f0f02;
  transform: translateY(-1px);
}

.advancements__pager-btn:active:not(:disabled) {
  transform: translateY(0);
  filter: brightness(0.85);
}

.advancements__pager-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.advancements__pager-label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #3a1f0a;
  min-width: calc(var(--s) * 32);
  text-align: center;
}
</style>
