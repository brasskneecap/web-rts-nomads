<template>
  <footer class="selection-hud">
    <div class="selection-main">
      <section class="selection-panel selection-panel--primary">
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
      </section>

      <section class="selection-panel selection-panel--details">
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
        <div class="detail-inline">
          <template v-for="(detail, index) in ui.selection.details" :key="detail.id">
            <span class="detail-entry">
              <span>{{ detail.label }}</span>
              <strong v-if="detail.value">{{ detail.value }}</strong>
            </span>
            <span v-if="index < ui.selection.details.length - 1" class="detail-separator">,</span>
          </template>
          <span v-if="ui.selection.details.length === 0" class="detail-empty">No details available</span>
        </div>
      </section>
    </div>

    <section class="selection-panel selection-panel--actions">
      <div class="action-grid">
        <template v-for="i in GRID_SIZE" :key="i">
          <template v-if="ui.selection.actions[i - 1]">
            <!-- Perk display cell (bottom row: bronze → silver → gold) -->
            <div
              v-if="ui.selection.actions[i - 1].kind === 'perk'"
              class="action-cell action-cell--perk"
              :class="`action-cell--perk-${ui.selection.actions[i - 1].perkRank}`"
            >
              <ActionIcon :action="ui.selection.actions[i - 1]" />
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
              :class="{ 'action-cell--active': ui.selection.actions[i - 1].active }"
              :disabled="ui.selection.actions[i - 1].disabled"
              :title="ui.selection.actions[i - 1].label"
              type="button"
              @click="$emit('action', ui.selection.actions[i - 1].id)"
            >
              <ActionIcon :action="ui.selection.actions[i - 1]" />
            </button>
          </template>
          <div v-else class="action-cell action-cell--empty" />
        </template>
      </div>
    </section>
  </footer>
</template>

<script setup lang="ts">
import type { GameUiSnapshot } from '@/game/core/GameClient'
import ActionIcon from '@/components/ActionIcon.vue'

defineEmits<{
  action: [actionId: string]
}>()

defineProps<{
  ui: GameUiSnapshot
}>()

const GRID_SIZE = 9
</script>

<style scoped>
.selection-hud {
  position: absolute;
  left: 18px;
  right: 18px;
  bottom: 18px;
  z-index: 5;
  display: flex;
  align-items: flex-end;
  gap: 6px;
  --selection-panel-width: clamp(180px, 20vw, 240px);
  --actions-panel-width: clamp(170px, 18vw, 210px);
  --main-panel-height: clamp(100px, 14vh, 140px);
  --hud-height: clamp(180px, 28vh, 240px);
  pointer-events: none;
}

.selection-main {
  display: flex;
  align-items: stretch;
  flex: 1 1 auto;
  min-width: 0;
  max-width: 1500px;
  height: var(--main-panel-height);
  pointer-events: auto;
}

.selection-panel {
  min-width: 0;
  padding: 12px 14px;
  border-radius: 0;
  background:
    radial-gradient(circle at top, rgba(220, 165, 80, 0.2), transparent 50%),
    linear-gradient(180deg, rgb(96, 64, 30), rgb(68, 44, 18));
  border: 1px solid rgba(180, 130, 60, 0.35);
}

.selection-panel--primary {
  display: flex;
  flex-direction: column;
  flex: 0 0 var(--selection-panel-width);
  border-radius: 14px 0 0 14px;
}

.selection-panel--details {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
  border-left: 0;
  border-radius: 0 14px 14px 0;
  overflow-y: auto;
  scrollbar-width: none;
}

.selection-panel--details::-webkit-scrollbar {
  display: none;
}

.selection-panel--actions {
  display: flex;
  flex-direction: column;
  flex: 0 0 var(--actions-panel-width);
  height: var(--hud-height);
  /* overflow: visible so perk hover tooltips can extend above the panel. */
  overflow: visible;
  border-radius: 14px;
  pointer-events: auto;
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
  grid-template-columns: repeat(3, 1fr);
  grid-template-rows: repeat(3, 1fr);
  gap: 4px;
  flex: 1 1 auto;
}

.detail-inline {
  margin-top: 8px;
  color: #e7d7b6;
  font-size: 13px;
  line-height: 1.45;
  overflow-y: auto;
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

.detail-empty {
  color: #cbb893;
}

.action-cell {
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.28);
  background: linear-gradient(180deg, rgba(114, 77, 39, 0.88), rgba(60, 39, 21, 0.94));
  color: #f5ead2;
  padding: 0;
  cursor: pointer;
}

.action-cell:not(:disabled):hover {
  background: linear-gradient(180deg, rgba(148, 102, 50, 0.95), rgba(90, 58, 26, 0.98));
  border-color: rgba(220, 180, 110, 0.5);
}

.action-cell--active {
  background:
    linear-gradient(180deg, rgba(201, 145, 65, 0.98), rgba(121, 80, 34, 1));
  border-color: rgba(247, 216, 142, 0.82);
  box-shadow:
    inset 0 0 0 1px rgba(255, 241, 202, 0.24),
    0 0 0 1px rgba(247, 216, 142, 0.2);
}

.action-cell:disabled {
  opacity: 0.42;
  cursor: not-allowed;
}

.action-cell--empty {
  border: 1px solid rgba(180, 130, 60, 0.1);
  background: rgba(50, 30, 10, 0.25);
  cursor: default;
  pointer-events: none;
}

/* ── Perk display cells ───────────────────────────────────────────────────── */
/* Shared base: display-only, not clickable, slightly darker background.      */
/* pointer-events: auto so the custom hover tooltip can trigger.              */
.action-cell--perk {
  position: relative;
  cursor: default;
  pointer-events: auto;
  background: linear-gradient(180deg, rgba(30, 18, 8, 0.92), rgba(20, 12, 4, 0.96));
  color: #d4b87a;
}

/* Rank-tinted borders. Update these when adding new rank tiers. */
.action-cell--perk-bronze {
  border-color: rgba(160, 100, 30, 0.75);
  box-shadow: inset 0 0 0 1px rgba(200, 140, 60, 0.18);
}

.action-cell--perk-silver {
  border-color: rgba(140, 155, 170, 0.65);
  box-shadow: inset 0 0 0 1px rgba(180, 195, 210, 0.15);
}

.action-cell--perk-gold {
  border-color: rgba(200, 165, 40, 0.80);
  box-shadow:
    inset 0 0 0 1px rgba(240, 210, 80, 0.22),
    0 0 4px rgba(200, 165, 40, 0.18);
}

/* Locked / empty rank slot: dim the icon and border further. */
.action-cell--perk:has(.action-icon) .action-icon {
  opacity: 0.9;
}
.action-cell--perk-silver .action-icon,
.action-cell--perk-gold .action-icon {
  opacity: 0.35;
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

@media (max-width: 1000px) {
  .selection-hud {
    left: 14px;
    right: 14px;
    bottom: 14px;
    --selection-panel-width: 210px;
    --actions-panel-width: 180px;
    --main-panel-height: 110px;
    --hud-height: 200px;
  }

  .selection-title {
    font-size: 15px;
  }
}

@media (max-width: 720px) {
  .selection-hud {
    left: 10px;
    right: 10px;
    bottom: 10px;
    --selection-panel-width: 170px;
    --actions-panel-width: 152px;
    --main-panel-height: 90px;
    --hud-height: 176px;
  }

  .selection-panel {
    padding: 8px 10px;
  }

  .selection-title {
    font-size: 13px;
  }

  .selection-subtitle {
    font-size: 10px;
    margin-top: 2px;
  }

  .action-grid {
    gap: 5px;
  }

  .detail-inline {
    margin-top: 4px;
    font-size: 11px;
  }

  .production-card {
    margin-top: 2px;
  }

  .production-bar {
    height: 24px;
  }

  .detail-entry strong {
    font-size: 11px;
  }

  .production-bar__time {
    font-size: 11px;
  }

  .production-bar__cancel {
    right: 4px;
    width: 18px;
    height: 18px;
    font-size: 10px;
  }

}
</style>
