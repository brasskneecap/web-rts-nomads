<template>
  <div class="advancements">
    <h1 class="advancements__title">Advancements</h1>
    <div class="advancements__rows">
      <div
        v-for="row in visibleRows"
        :key="row.id"
        class="advancement-row"
      >
        <div class="advancement-row__character">
          <div
            class="advancement-row__portrait"
            :style="{ backgroundImage: `url(${row.portraitUrl})` }"
            :aria-label="row.name"
            role="img"
          ></div>
          <div class="advancement-row__name">{{ row.name }}</div>
        </div>

        <div class="advancement-row__nodes">
          <button
            v-for="(node, idx) in row.nodes"
            :key="node.id"
            type="button"
            class="advancement-node"
            :class="[
              `advancement-node--${node.shape}`,
              nodeStateClass(row, idx),
            ]"
            :style="{ backgroundImage: `url(${node.acquired ? node.acquiredIcon : node.icon})` }"
            :disabled="!isAvailable(row, idx) && !node.acquired"
            :aria-label="`${node.label} (${nodeStateLabel(row, idx)})`"
            @click="acquire(row, idx)"
          ></button>
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
import { computed, reactive, ref } from 'vue'
import acolytePortraitUrl from '@/assets/units/human/acolyte/portrait.png'
import adeptPortraitUrl from '@/assets/units/human/adept/portrait.png'
import archerPortraitUrl from '@/assets/units/human/archer/portrait.png'
import soldierPortraitUrl from '@/assets/units/human/soldier/portrait.png'
import unsealedUrl from '@/assets/ui/buttons/war_room/advancement/unsealed.png'
import waxSealUrl from '@/assets/ui/buttons/war_room/advancement/wax-seal.png'
import medalSlotEmptyUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot-empty.png'
import medalSlotUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot.png'

type NodeShape = 'circle' | 'square'

interface AdvancementNode {
  id: string
  label: string
  icon: string
  acquiredIcon: string
  shape: NodeShape
  acquired: boolean
}

interface AdvancementRow {
  id: string
  name: string
  portraitUrl: string
  nodes: AdvancementNode[]
}

const PAGE_SIZE = 3

const SEAL_SLOT = { icon: unsealedUrl, acquiredIcon: waxSealUrl, shape: 'circle' as const }
const MEDAL_SLOT = { icon: medalSlotEmptyUrl, acquiredIcon: medalSlotUrl, shape: 'square' as const }

const TRACK_LAYOUT = [
  SEAL_SLOT,
  SEAL_SLOT,
  SEAL_SLOT,
  MEDAL_SLOT,
  SEAL_SLOT,
  SEAL_SLOT,
  SEAL_SLOT,
  MEDAL_SLOT,
]

function placeholderNodes(rowId: string): AdvancementNode[] {
  return TRACK_LAYOUT.map((slot, i) => ({
    id: `${rowId}-${i + 1}`,
    label: `Tier ${i + 1}`,
    icon: slot.icon,
    acquiredIcon: slot.acquiredIcon,
    shape: slot.shape,
    acquired: false,
  }))
}

const rows = reactive<AdvancementRow[]>([
  {
    id: 'soldier',
    name: 'Soldier',
    portraitUrl: soldierPortraitUrl,
    nodes: placeholderNodes('soldier'),
  },
  {
    id: 'archer',
    name: 'Archer',
    portraitUrl: archerPortraitUrl,
    nodes: placeholderNodes('archer'),
  },
  {
    id: 'acolyte',
    name: 'Acolyte',
    portraitUrl: acolytePortraitUrl,
    nodes: placeholderNodes('acolyte'),
  },
  {
    id: 'adept',
    name: 'Adept',
    portraitUrl: adeptPortraitUrl,
    nodes: placeholderNodes('adept'),
  },
])

const pageIndex = ref(0)

const totalPages = computed(() => Math.max(1, Math.ceil(rows.length / PAGE_SIZE)))

const visibleRows = computed(() => {
  const start = pageIndex.value * PAGE_SIZE
  return rows.slice(start, start + PAGE_SIZE)
})

function prevPage() {
  if (pageIndex.value > 0) pageIndex.value -= 1
}

function nextPage() {
  if (pageIndex.value < totalPages.value - 1) pageIndex.value += 1
}

function isAvailable(row: AdvancementRow, idx: number): boolean {
  if (idx === 0) return true
  return row.nodes[idx - 1].acquired
}

function nodeStateClass(row: AdvancementRow, idx: number) {
  const node = row.nodes[idx]
  if (node.acquired) return 'advancement-node--acquired'
  if (isAvailable(row, idx)) return 'advancement-node--available'
  return 'advancement-node--locked'
}

function nodeStateLabel(row: AdvancementRow, idx: number): string {
  const node = row.nodes[idx]
  if (node.acquired) return 'acquired'
  if (isAvailable(row, idx)) return 'available'
  return 'locked'
}

function acquire(row: AdvancementRow, idx: number) {
  if (!isAvailable(row, idx)) return
  row.nodes[idx].acquired = true
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
  overflow: hidden;
}

.advancements__title {
  flex: 0 0 auto;
  margin: 0 0 8px;
  text-align: left;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #3a1f0a;
  transform: translateX(80px);
}

.advancements__rows {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: 18px;
  overflow-y: auto;
  min-height: 0;
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

.advancement-node {
  flex: 0 0 auto;
  width: 48px;
  height: 48px;
  padding: 0;
  border: 0;
  background-color: transparent;
  background-repeat: no-repeat;
  background-position: center;
  background-size: contain;
  cursor: pointer;
  image-rendering: pixelated;
  transition:
    transform 120ms ease,
    filter 120ms ease,
    opacity 120ms ease;
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
  cursor: pointer;
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
