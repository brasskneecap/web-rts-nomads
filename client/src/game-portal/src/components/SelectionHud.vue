<template>
  <footer class="selection-hud">
    <div class="selection-main">
      <section class="selection-panel selection-panel--primary">
        <div class="selection-title">{{ ui.selection.title }}</div>
        <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
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
          <button
            v-if="ui.selection.actions[i - 1]"
            class="action-cell"
            :class="{ 'action-cell--active': ui.selection.actions[i - 1].active }"
            :disabled="ui.selection.actions[i - 1].disabled"
            :title="ui.selection.actions[i - 1].label"
            type="button"
            @click="$emit('action', ui.selection.actions[i - 1].id)"
          >
            <ActionIcon :action="ui.selection.actions[i - 1]" />
          </button>
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
