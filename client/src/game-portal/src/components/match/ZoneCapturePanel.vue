<template>
  <div v-if="cards.length" class="zone-capture" role="status" aria-live="polite">
    <div class="zone-capture__header">Capturing</div>
    <ul class="zone-capture__list">
      <li
        v-for="card in cards"
        :key="card.id"
        class="zone-card"
        :class="`zone-card--${card.state}`"
        :style="card.ownerColor ? { borderLeftColor: card.ownerColor } : undefined"
      >
        <div class="zone-card__name">{{ card.name }}</div>
        <div class="zone-card__req">{{ card.requirement }}</div>
        <div class="zone-card__status">{{ card.status }}</div>
        <div v-if="card.progress > 0" class="zone-card__bar">
          <div class="zone-card__bar-fill" :style="{ width: `${Math.round(card.progress * 100)}%` }" />
        </div>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import type { ZoneCaptureCard } from '@/game/zones/zoneCaptureCards'

const props = defineProps<{ cards: ZoneCaptureCard[] }>()
void props
</script>

<style scoped>
.zone-capture {
  width: 280px;
  max-width: 90vw;
  pointer-events: auto;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  color: #f4d27a;
  background: rgba(28, 18, 8, 0.78);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 4px;
  padding: 10px 12px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.55), 0 0 0 1px rgba(0, 0, 0, 0.25) inset;
}

.zone-capture__header {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-bottom: 6px;
}

.zone-capture__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.zone-card {
  border-left: 3px solid rgba(212, 168, 71, 0.55);
  padding-left: 8px;
  font-family: 'Trebuchet MS', 'Lucida Sans Unicode', system-ui, sans-serif;
  line-height: 1.3;
}

.zone-card__name {
  font-size: 13px;
  font-weight: 700;
  color: #f4d27a;
}

.zone-card__req {
  font-size: 12px;
  color: rgba(244, 210, 122, 0.85);
}

.zone-card__status {
  font-size: 12px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: rgba(244, 210, 122, 0.9);
}

.zone-card--contested .zone-card__status {
  color: #f0b070;
}

.zone-card--locked .zone-card__status {
  color: rgba(244, 210, 122, 0.55);
}

.zone-card__bar {
  margin-top: 3px;
  height: 5px;
  background: rgba(0, 0, 0, 0.45);
  border-radius: 3px;
  overflow: hidden;
}

.zone-card__bar-fill {
  height: 100%;
  background: rgba(96, 165, 250, 0.9);
}

.zone-card--contested .zone-card__bar-fill {
  background: rgba(251, 191, 36, 0.95);
}
</style>
