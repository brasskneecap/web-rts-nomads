<template>
  <div v-if="queued.length > 0" class="uqueue">
    <div class="uqueue__title">Queue</div>
    <div class="uqueue__list">
      <div v-for="q in queued" :key="q.track" class="uqitem">
        <div class="uqitem__info">
          <span class="uqitem__name">{{ q.displayName }}</span>
          <span class="uqitem__target">Lv {{ q.level }} → {{ q.level + (q.queuedCount ?? 0) }}</span>
        </div>

        <template v-if="isResearching(q)">
          <div class="uqitem__bar">
            <div class="uqitem__fill" :style="{ width: `${researchPercent(q)}%` }" />
          </div>
          <span class="uqitem__time">{{ Math.ceil(q.researchRemaining ?? 0) }}s</span>
          <button
            v-if="q.researchBuildingId"
            type="button"
            class="uqitem__cancel"
            title="Cancel current upgrade (full refund)"
            @click="onCancel(q.researchBuildingId!)"
          >Cancel</button>
        </template>
        <span v-else class="uqitem__waiting">Queued</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { PlayerUpgradeSnapshot } from '@/game/network/protocol'

const props = defineProps<{
  upgrades: PlayerUpgradeSnapshot[]
  onCancel: (buildingId: string) => void
}>()

// Any track with something in progress or waiting.
const queued = computed(() => props.upgrades.filter((u) => (u.queuedCount ?? 0) > 0))

function isResearching(u: PlayerUpgradeSnapshot): boolean {
  return (u.researchTotal ?? 0) > 0
}

function researchPercent(u: PlayerUpgradeSnapshot): number {
  const total = u.researchTotal ?? 0
  if (total <= 0) return 0
  const remaining = u.researchRemaining ?? 0
  return Math.max(0, Math.min(100, ((total - remaining) / total) * 100))
}
</script>

<style scoped>
.uqueue {
  margin-top: 4px;
  padding-top: 10px;
  border-top: 1px solid rgba(212, 168, 79, 0.18);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.uqueue__title {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(212, 168, 79, 0.7);
}

.uqueue__list {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.uqitem {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 8px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.16);
  background: rgba(20, 12, 7, 0.5);
}

.uqitem__info {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: 0 0 auto;
  min-width: 120px;
}

.uqitem__name {
  font-size: 12px;
  font-weight: 700;
  color: #f5ead2;
}

.uqitem__target {
  font-size: 11px;
  color: #d4b87a;
  font-variant-numeric: tabular-nums;
}

.uqitem__bar {
  flex: 1 1 auto;
  height: 8px;
  border-radius: 4px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: rgba(20, 12, 7, 0.7);
  overflow: hidden;
}

.uqitem__fill {
  height: 100%;
  background: linear-gradient(90deg, rgba(220, 180, 110, 0.85), rgba(240, 216, 142, 0.95));
  transition: width 0.2s linear;
}

.uqitem__time {
  flex: 0 0 auto;
  font-size: 11px;
  font-weight: 600;
  color: #f0d88e;
  font-variant-numeric: tabular-nums;
}

.uqitem__waiting {
  flex: 1 1 auto;
  text-align: right;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: rgba(240, 216, 142, 0.6);
}

.uqitem__cancel {
  flex: 0 0 auto;
  padding: 2px 8px;
  border-radius: 5px;
  border: 1px solid rgba(220, 120, 100, 0.5);
  background: rgba(80, 28, 22, 0.6);
  color: #f3cabf;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.uqitem__cancel:hover {
  background: rgba(120, 40, 32, 0.85);
  border-color: rgba(235, 150, 130, 0.7);
}
</style>
