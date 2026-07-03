<template>
  <!-- The whole card is the click target: clicking anywhere selects the unit
       and recenters the camera. Item assignment is drag-and-drop, so a plain
       click never equips. -->
  <div
    class="ucard"
    :class="{
      'ucard--ineligible': hasSelectedItem && !card.eligible,
      'ucard--consumable-target': acceptsConsumableDrop,
    }"
    role="button"
    tabindex="0"
    @click="emit('focus', card.id)"
    @keydown.enter.prevent="emit('focus', card.id)"
    @keydown.space.prevent="emit('focus', card.id)"
    @dragover="onCardDragOver"
    @drop="onCardDrop"
  >
    <!-- Top row: portrait on the left, name + XP + perks stacked to its right. -->
    <div class="ucard__main">
      <div class="ucard__portrait">
        <img
          v-if="card.portraitUrl"
          :src="card.portraitUrl"
          :alt="card.specializationName"
          draggable="false"
        />
        <span v-else class="ucard__portrait-fallback">{{ card.initials }}</span>
        <div
          v-if="card.rankChevrons > 0"
          class="ucard__rank"
          :style="{ color: card.rankColor }"
          :aria-label="`Rank ${card.rank}`"
        >
          <svg
            v-for="n in card.rankChevrons"
            :key="n"
            viewBox="0 0 10 6"
            class="ucard__rank-chevron"
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

      <div class="ucard__info">
        <div class="ucard__name">{{ card.specializationName }}</div>
        <div v-if="card.maxHp != null" class="ucard__hp">
          {{ Math.round(card.hp ?? 0) }} / {{ Math.round(card.maxHp) }} HP
        </div>
        <XpProgressBar
          :xp-into="card.xpInto"
          :xp-to-next="card.xpToNext"
          :is-max-rank="card.isMaxRank"
          :rank-color="card.rankColor"
        />
      </div>
    </div>

    <!-- Inventory first, on its own left-aligned row below the portrait/name. -->
    <div class="ucard__inv">
      <div class="ucard__section-label">Inventory</div>
      <UnitInventorySlots
        :slots="card.inventory"
        :accepts-drop="acceptsDrop"
        :icon-container-url="iconContainerUrl"
        @slot-dragstart="(slotIndex) => emit('slot-dragstart', { unitId: card.id, slotIndex })"
        @slot-dragend="emit('slot-dragend')"
        @slot-drop="(slotIndex) => emit('slot-drop', { unitId: card.id, slotIndex })"
      />
    </div>

    <!-- Perks below the inventory row. -->
    <div class="ucard__perks">
      <div class="ucard__section-label">Perks</div>
      <PerkIconRow :perks="card.perks" :icon-container-url="iconContainerUrl" />
    </div>
  </div>
</template>

<script setup lang="ts">
import XpProgressBar from './XpProgressBar.vue'
import PerkIconRow from './PerkIconRow.vue'
import UnitInventorySlots from './UnitInventorySlots.vue'
import type { VaultUnitCardData } from './types'

const props = defineProps<{
  card: VaultUnitCardData
  /** A vault item is selected (drives the ineligible-dimming). */
  hasSelectedItem: boolean
  /** A compatible equipment item is being dragged and could land on this unit. */
  acceptsDrop: boolean
  /** A bag consumable is being dragged — the whole card is a valid drop target
   *  (applies the consumable to this unit). */
  acceptsConsumableDrop: boolean
  iconContainerUrl: string
}>()

const emit = defineEmits<{
  focus: [unitId: number]
  'slot-dragstart': [payload: { unitId: number; slotIndex: number }]
  'slot-dragend': []
  'slot-drop': [payload: { unitId: number; slotIndex: number }]
  /** A bag consumable was dropped anywhere on this card. */
  'card-drop': [unitId: number]
}>()

// Allow drops anywhere on the card so a bag consumable can be released over any
// part of it. Equipment continues to drop onto the specific inventory slots;
// those slot drops bubble here too, but the parent only treats a drop as a
// consumable application when the active drag is a bag item.
function onCardDragOver(e: DragEvent) {
  e.preventDefault()
}

function onCardDrop(e: DragEvent) {
  e.preventDefault()
  emit('card-drop', props.card.id)
}
</script>

<style scoped>
.ucard {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
  padding: 10px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(212, 168, 79, 0.22);
  border-radius: 8px;
  transition: border-color 0.12s ease, background 0.12s ease, filter 0.12s ease;
}

.ucard__main {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.ucard:hover,
.ucard:focus-visible {
  border-color: rgba(214, 178, 110, 0.7);
  background: rgba(20, 14, 6, 0.45);
  filter: brightness(1.08);
  outline: none;
}

.ucard--ineligible {
  opacity: 0.4;
}

/* A bag consumable is being dragged: the whole card lights up as a drop target
   (matches the blue drop-target language used by the inventory slots). */
.ucard--consumable-target {
  border-color: rgba(96, 165, 250, 0.9);
  box-shadow: 0 0 12px rgba(96, 165, 250, 0.55);
}

.ucard__portrait {
  position: relative;
  flex: 0 0 64px;
  width: 64px;
  height: 64px;
  border-radius: 6px;
  overflow: hidden;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}

.ucard__portrait img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  image-rendering: pixelated;
}

.ucard__portrait-fallback {
  font-size: 18px;
  font-weight: 700;
  color: rgba(232, 217, 184, 0.7);
}

/* Rank chevrons stacked in the top-left of the portrait. */
.ucard__rank {
  position: absolute;
  top: 2px;
  left: 2px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0;
  filter: drop-shadow(0 1px 1px rgba(0, 0, 0, 0.9));
}

.ucard__rank-chevron {
  width: 12px;
  height: 7px;
  margin-bottom: -3px;
}

.ucard__info {
  display: flex;
  flex-direction: column;
  gap: 5px;
  flex: 1 1 auto;
  min-width: 0;
}

.ucard__name {
  font-size: 16px;
  font-weight: 700;
  color: #f0e0c0;
  letter-spacing: 0.03em;
  line-height: 1.2;
  /* Full name, no truncation. */
  white-space: normal;
  overflow-wrap: anywhere;
}

.ucard__hp {
  font-size: 12px;
  font-weight: 600;
  color: rgba(232, 217, 184, 0.75);
  letter-spacing: 0.02em;
  line-height: 1.1;
}

.ucard__section-label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(212, 168, 79, 0.7);
}

/* Perks and inventory each get their own full-width, left-aligned row below
   the portrait/name. */
.ucard__perks,
.ucard__inv {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 5px;
}
</style>
