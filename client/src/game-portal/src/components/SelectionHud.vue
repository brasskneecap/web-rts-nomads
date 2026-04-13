<template>
  <footer class="selection-hud">
    <section class="selection-panel">
      <div class="selection-kicker">Selection</div>
      <div class="selection-title">{{ ui.selection.title }}</div>
      <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
    </section>

    <section v-if="'hp' in ui.selection" class="selection-panel">
      <div class="selection-kicker">Durability</div>
      <div class="stat-value">{{ ui.selection.hp }} / {{ ui.selection.maxHp }}</div>
      <div class="health-track">
        <div
          class="health-fill"
          :style="{ width: `${getHealthPercent(ui.selection.hp ?? 0, ui.selection.maxHp ?? 0)}%` }"
        ></div>
      </div>
    </section>

    <section
      v-else-if="'resourceStockLabel' in ui.selection && ui.selection.resourceStockLabel"
      class="selection-panel"
    >
      <div class="selection-kicker">{{ ui.selection.resourceStockLabel }}</div>
      <div class="stat-value">{{ ui.selection.resourceStockAmount ?? 0 }}</div>
      <div class="health-track">
        <div class="health-fill resource-fill" :style="{ width: '100%' }"></div>
      </div>
    </section>

    <section
      v-if="'resourceLabel' in ui.selection && ui.selection.resourceLabel"
      class="selection-panel"
    >
      <div class="selection-kicker">{{ ui.selection.resourceLabel }}</div>
      <div class="stat-value">{{ ui.selection.resourceAmount ?? 0 }}</div>
    </section>

    <section class="selection-panel selection-panel--actions">
      <div class="selection-kicker">Actions</div>
      <div class="action-grid">
        <button
          v-for="action in ui.selection.actions"
          :key="action.id"
          class="action-button"
          :disabled="action.disabled"
          type="button"
        >
          {{ action.label }}
        </button>
        <div v-if="ui.selection.actions.length === 0" class="action-empty">
          No actions available
        </div>
      </div>
    </section>
  </footer>
</template>

<script setup lang="ts">
import type { GameUiSnapshot } from '@/game/core/GameClient'

defineProps<{
  ui: GameUiSnapshot
}>()

function getHealthPercent(hp: number, maxHp: number) {
  if (!maxHp) return 0
  return Math.max(0, Math.min((hp / maxHp) * 100, 100))
}
</script>

<style scoped>
.selection-hud {
  position: relative;
  z-index: 5;
  display: grid;
  grid-template-columns: minmax(220px, 0.9fr) minmax(180px, 0.8fr) minmax(160px, 0.6fr) minmax(280px, 1.2fr);
  gap: 14px;
  padding: 14px 18px 18px;
  border-top: 1px solid rgba(123, 93, 48, 0.5);
  background:
    radial-gradient(circle at bottom, rgba(176, 124, 52, 0.14), transparent 38%),
    linear-gradient(180deg, rgba(26, 18, 12, 0.96), rgba(17, 11, 7, 0.98));
  box-shadow: inset 0 1px 0 rgba(240, 220, 178, 0.1);
}

.selection-panel {
  min-width: 0;
  padding: 12px 14px;
  border-radius: 14px;
  background: linear-gradient(180deg, rgba(92, 62, 31, 0.46), rgba(46, 28, 16, 0.8));
  border: 1px solid rgba(198, 160, 104, 0.2);
}

.selection-panel--actions {
  display: flex;
  flex-direction: column;
}

.selection-kicker {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: #d7bb84;
}

.selection-title {
  margin-top: 4px;
  font-size: 20px;
  font-weight: 700;
  color: #f5ead2;
}

.selection-subtitle {
  margin-top: 4px;
  font-size: 13px;
  color: #cbb893;
}

.stat-value {
  margin-top: 6px;
  font-size: 24px;
  font-weight: 700;
  color: #fff2d6;
}

.health-track {
  margin-top: 10px;
  height: 10px;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.8);
  overflow: hidden;
}

.health-fill {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, #c96e43, #d6b45c);
}

.health-fill.resource-fill {
  background: linear-gradient(90deg, #4a7c3f, #a3c96e);
}

.action-grid {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.action-button,
.action-empty {
  min-width: 112px;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.28);
  background: linear-gradient(180deg, rgba(114, 77, 39, 0.88), rgba(60, 39, 21, 0.94));
  color: #f5ead2;
  font-size: 13px;
  font-weight: 700;
  text-align: center;
}

.action-button:disabled {
  opacity: 0.58;
}

.action-empty {
  color: #cbb893;
}

@media (max-width: 1000px) {
  .selection-hud {
    grid-template-columns: 1fr 1fr;
  }
}

@media (max-width: 720px) {
  .selection-hud {
    grid-template-columns: 1fr;
    padding: 12px;
  }
}
</style>
