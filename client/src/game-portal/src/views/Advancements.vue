<template>
  <div class="advancements">
    <div class="advancements__header">
      <h1 class="advancements__title">Advancements</h1>
      <button
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
            class="advancement-row__portrait"
            :style="{ backgroundImage: portraitBg(row.unitType) }"
            :aria-label="unitDisplayName(row.unitType)"
            role="img"
          ></div>
          <div class="advancement-row__name">{{ unitDisplayName(row.unitType) }}</div>
        </div>

        <div class="advancement-row__nodes">
          <button
            v-for="(node, idx) in row.nodes"
            :key="node.id"
            type="button"
            class="advancement-node"
            :class="[
              nodeShapeClass(node),
              nodeStateClass(row, idx),
            ]"
            :style="{ backgroundImage: nodeIcon(node, isAcquired(node.id)) }"
            :disabled="isBusy || isAcquired(node.id) || !isAvailable(row, idx) || !canAfford(node.cost)"
            :aria-label="`${node.name} (${nodeStateLabel(row, idx)})`"
            @click="purchase(node.id)"
          >
            <UiTooltip :title="node.name" :body="tooltipBody(node)" />
          </button>
        </div>

      </div>
    </div>

    <div class="advancements__pager">
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
import unsealedUrl from '@/assets/ui/buttons/war_room/advancement/unsealed.png'
import waxSealUrl from '@/assets/ui/buttons/war_room/advancement/wax-seal.png'
import medalSlotEmptyUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot-empty.png'
import medalSlotUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot.png'

const { catalog, acquiredIds, isBusy, error, isAcquired, canAfford, purchase, reset } =
  useAdvancements()

// Portrait lookup: unitType -> static import URL. Extended as new unit types
// get advancement tracks. Unknown types fall back to a transparent pixel.
const PORTRAIT_MAP: Record<string, string> = {
  soldier: soldierPortraitUrl,
  archer: archerPortraitUrl,
  acolyte: acolytePortraitUrl,
  adept: adeptPortraitUrl,
}

// Human-readable label for a unit type. Falls back to capitalised unitType.
function unitDisplayName(unitType: string): string {
  const overrides: Record<string, string> = {
    soldier: 'Soldier',
    archer: 'Archer',
    acolyte: 'Acolyte',
    adept: 'Adept',
  }
  return overrides[unitType] ?? (unitType.charAt(0).toUpperCase() + unitType.slice(1))
}

function portraitBg(unitType: string): string {
  const url = PORTRAIT_MAP[unitType]
  return url ? `url(${url})` : 'none'
}

// Preferred display order for unit tracks. The server serves tracks sorted
// alphabetically by unitType; we override that for presentation so the roster
// reads in roster order rather than A–Z. Unit types not listed here fall back
// to alphabetical, after the listed ones.
const UNIT_DISPLAY_ORDER = ['soldier', 'archer', 'acolyte', 'adept']

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
    return canAfford(node.cost)
      ? 'advancement-node--available'
      : 'advancement-node--unaffordable'
  }
  return 'advancement-node--locked'
}

function nodeStateLabel(track: UnitAdvancementTrack, idx: number): string {
  const node = track.nodes[idx]
  if (isAcquired(node.id)) return 'acquired'
  if (isAvailable(track, idx)) return canAfford(node.cost) ? 'available' : 'not enough Legend Points'
  return 'locked'
}

function tooltipBody(node: UnitAdvancementNode): string {
  const lines: string[] = []
  if (node.description) lines.push(node.description)
  lines.push(`Cost: ${node.cost} LP`)
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

.advancements__title {
  margin: 0;
  text-align: left;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 18);
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #3a1f0a;
  transform: translateX(calc(var(--s) * 80));
}

.advancements__reset {
  flex: 0 0 auto;
  padding: calc(var(--s) * 4) calc(var(--s) * 12);
  border: 1px solid #9a4a2a;
  border-radius: 4px;
  background-color: rgba(150, 50, 30, 0.16);
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
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
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
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
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
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
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #3a1f0a;
  min-width: calc(var(--s) * 32);
  text-align: center;
}
</style>
