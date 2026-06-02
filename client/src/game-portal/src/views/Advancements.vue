<template>
  <div class="advancements">
    <div class="advancements__header">
      <h1 class="advancements__title">Advancements</h1>
      <div class="advancements__balance" aria-label="Legend points balance">
        <span class="advancements__balance-label">Legend Points</span>
        <span class="advancements__balance-value">{{ legendPoints }}</span>
      </div>
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

        <div class="advancement-row__cost">
          <template v-if="nextNodeFor(row)">
            <span class="advancement-row__cost-label">Next</span>
            <span class="advancement-row__cost-value">{{ nextNodeFor(row)!.cost }} LP</span>
          </template>
          <span v-else class="advancement-row__cost-complete">Complete</span>
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

const { catalog, legendPoints, isBusy, error, isAcquired, canAfford, nextNodeFor, purchase } =
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

// Only show tracks that have at least one node — empty tracks (archer/acolyte/
// adept before their advancements.json ships) are hidden rather than shown as
// ghost rows.
const rowsWithNodes = computed<UnitAdvancementTrack[]>(() =>
  catalog.value.filter((t) => t.nodes.length > 0),
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
  /* No overflow clipping — tooltips on top-row nodes need to extend above
     the panel bounds. Pagination already keeps content from overflowing. */
}

.advancements__header {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  gap: 16px;
}

.advancements__title {
  margin: 0;
  text-align: left;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #3a1f0a;
  transform: translateX(80px);
}

.advancements__balance {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  padding: 4px 10px;
  border: 1px solid #c68c44;
  border-radius: 4px;
  background-color: rgba(198, 140, 68, 0.12);
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  color: #3a1f0a;
}

.advancements__balance-label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.advancements__balance-value {
  font-size: 14px;
  font-weight: 700;
}

.advancements__error {
  flex: 0 0 auto;
  padding: 4px 8px;
  margin-bottom: 6px;
  border-radius: 4px;
  background-color: rgba(180, 40, 20, 0.15);
  border: 1px solid rgba(180, 40, 20, 0.4);
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 11px;
  color: #7a1a0a;
}

.advancements__rows {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.advancement-row {
  display: flex;
  align-items: center;
  gap: 18px;
}

/*
 * Stagger left padding so the rows lean inward following the slanted
 * left edge of the parchment in the war_room_bg artwork.
 */
.advancement-row:nth-child(1) { padding-left: 60px; }
.advancement-row:nth-child(2) { padding-left: 30px; }
.advancement-row:nth-child(3) { padding-left: 0; }

.advancement-row__character {
  flex: 0 0 auto;
  display: flex;
  flex-direction: row-reverse;
  align-items: center;
  gap: 8px;
}

.advancement-row__name {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #3a1f0a;
  white-space: nowrap;
}

.advancement-row__portrait {
  flex: 0 0 auto;
  width: 52px;
  height: 52px;
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
  gap: 10px;
  flex-wrap: nowrap;
}

.advancement-row__cost {
  margin-left: auto;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  color: #3a1f0a;
  white-space: nowrap;
}

.advancement-row__cost-label {
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  opacity: 0.75;
}

.advancement-row__cost-value {
  font-size: 13px;
  font-weight: 700;
}

.advancement-row__cost-complete {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  opacity: 0.65;
}

.advancement-node {
  position: relative;
  flex: 0 0 auto;
  width: 48px;
  height: 48px;
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
  width: 60px;
  height: 60px;
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
  gap: 14px;
  padding-top: 8px;
  transform: translate(-65px, -25px);
}

.advancements__pager-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px 6px;
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
  width: 32px;
  height: 24px;
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
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #3a1f0a;
  min-width: 32px;
  text-align: center;
}
</style>
