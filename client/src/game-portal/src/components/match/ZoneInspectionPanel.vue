<template>
  <div class="zone-inspection" role="region" :aria-label="`Zone: ${info.name}`">
    <div class="zone-inspection__name">{{ info.name }}</div>

    <div class="zone-inspection__owner-row">
      <span class="zone-inspection__owner-label">Owner</span>
      <span class="zone-inspection__owner-value">
        <span
          v-if="info.ownerColor"
          class="zone-inspection__color-swatch"
          :style="{ background: info.ownerColor }"
          aria-hidden="true"
        />
        <span>{{ info.ownerLabel }}</span>
      </span>
    </div>

    <template v-if="info.auras.length">
      <div class="zone-inspection__bonuses-header">Bonuses</div>
      <ul class="zone-inspection__bonuses-list">
        <li
          v-for="(aura, i) in info.auras"
          :key="i"
          class="zone-inspection__bonus"
        >
          {{ formatModifier(aura.modifier) }}
        </li>
      </ul>
    </template>
    <div v-else class="zone-inspection__no-bonuses">No bonuses</div>
  </div>
</template>

<script setup lang="ts">
import type { ZoneInspectionInfo } from '@/game/core/GameState'
import { formatModifier } from '@/game/stats/statRegistry'

const props = defineProps<{ info: ZoneInspectionInfo }>()
void props
</script>

<style scoped>
.zone-inspection {
  width: 240px;
  max-width: 90vw;
  pointer-events: auto;
  font-family: var(--font-body);
  color: #f4d27a;
  background: rgba(28, 18, 8, 0.78);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 4px;
  padding: 10px 12px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.55), 0 0 0 1px rgba(0, 0, 0, 0.25) inset;
}

.zone-inspection__name {
  font-family: var(--font-title);
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #f4d27a;
  margin-bottom: 8px;
  border-bottom: 1px solid rgba(212, 168, 71, 0.3);
  padding-bottom: 6px;
}

.zone-inspection__owner-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  margin-bottom: 8px;
}

.zone-inspection__owner-label {
  color: rgba(244, 210, 122, 0.7);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}

.zone-inspection__owner-value {
  display: flex;
  align-items: center;
  gap: 5px;
  font-weight: 600;
  color: #f4d27a;
}

.zone-inspection__color-swatch {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 1px solid rgba(0, 0, 0, 0.4);
  flex-shrink: 0;
}

.zone-inspection__bonuses-header {
  font-family: var(--font-title);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-bottom: 4px;
}

.zone-inspection__bonuses-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.zone-inspection__bonus {
  font-size: 12px;
  color: rgba(244, 210, 122, 0.9);
  line-height: 1.4;
}

.zone-inspection__no-bonuses {
  font-size: 11px;
  color: rgba(244, 210, 122, 0.45);
  font-style: italic;
}
</style>
