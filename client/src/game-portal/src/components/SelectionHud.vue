<template>
  <footer class="selection-hud">
    <div class="selection-main">
      <section class="selection-panel selection-panel--primary">
        <div class="selection-title">{{ ui.selection.title }}</div>
        <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
      </section>

      <section class="selection-panel selection-panel--details">
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
          <button
            v-if="ui.selection.actions[i - 1]"
            class="action-cell"
            :class="{ 'action-cell--active': ui.selection.actions[i - 1].active }"
            :disabled="ui.selection.actions[i - 1].disabled"
            :title="ui.selection.actions[i - 1].label"
            type="button"
            @click="$emit('action', ui.selection.actions[i - 1].id)"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path :d="getActionIcon(ui.selection.actions[i - 1].id)" />
            </svg>
          </button>
          <div v-else class="action-cell action-cell--empty" />
        </template>
      </div>
    </section>
  </footer>
</template>

<script setup lang="ts">
import type { GameUiSnapshot } from '@/game/core/GameClient'

defineEmits<{
  action: [actionId: string]
}>()

defineProps<{
  ui: GameUiSnapshot
}>()

const GRID_SIZE = 9

const ACTION_ICONS: Record<string, string> = {
  'harvest':          'M6 18l7-7 M12 6l6 6 M10 8l6-2 3 3-2 6 M5 19l4-1-3-3-1 4',
  'train-worker':     'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2 M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
  'set-spawn-point':  'M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z M4 22v-7',
  'build':            'M10 13l-5.5 5.5a2.12 2.12 0 0 1-3-3L7 10 M16 4l4 4-4 4-4-4 4-4z M7 10l4 4',
  'attack':           'M14.5 17.5L3 6V3h3l11.5 11.5 M18 16l4-4 M9 9l4-4',
  'move':             'M12 2v20 M2 12h20 M7 7L2 12l5 5 M17 7l5 5-5 5',
  'gather':           'M6 18l7-7 M12 6l6 6 M10 8l6-2 3 3-2 6 M5 19l4-1-3-3-1 4',
  'cancel-training':  'M18 6L6 18 M6 6l12 12',
  'build-barracks':   'M3 21h18 M5 21V9h14v12 M10 21v-5h4v5 M3 9l9-6 9 6 M8 14h2 M14 14h2',
  'close-build-menu': 'M19 12H5 M12 5l-7 7 7 7',
}

function getActionIcon(id: string): string {
  return ACTION_ICONS[id] ?? 'M12 5v14 M5 12h14'
}
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
}

.selection-panel--actions {
  display: flex;
  flex-direction: column;
  flex: 0 0 var(--actions-panel-width);
  height: var(--hud-height);
  overflow: hidden;
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

.action-cell svg {
  width: 55%;
  height: 55%;
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
