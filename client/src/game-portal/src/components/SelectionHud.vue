<template>
  <footer class="selection-hud">
    <div class="selection-main">
      <section class="selection-panel selection-panel--primary">
        <div class="selection-kicker">Selection</div>
        <div class="selection-title">{{ ui.selection.title }}</div>
        <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
      </section>

      <section class="selection-panel selection-panel--details">
        <div class="selection-kicker">Details</div>
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
      <div class="selection-kicker">Actions</div>
      <div class="action-grid">
        <button
          v-for="action in ui.selection.actions"
          :key="action.id"
          class="action-button"
          :disabled="action.disabled"
          type="button"
          @click="$emit('action', action.id)"
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

defineEmits<{
  action: [actionId: string]
}>()

defineProps<{
  ui: GameUiSnapshot
}>()
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
  gap: 14px;
  --selection-panel-width: clamp(180px, 20vw, 240px);
  --selection-main-width: clamp(620px, 68vw, 980px);
  --actions-panel-width: clamp(170px, 18vw, 210px);
  --actions-panel-height: clamp(180px, 28vh, 240px);
  --main-panel-height: clamp(82px, 11vh, 120px);
  padding: 10px;
  border: 1px solid rgba(123, 93, 48, 0.5);
  border-radius: 18px;
  background:
    radial-gradient(circle at top, rgba(176, 124, 52, 0.18), transparent 42%),
    linear-gradient(180deg, rgba(58, 35, 18, 0.98), rgba(27, 16, 10, 0.94));
  box-shadow:
    inset 0 1px 0 rgba(240, 220, 178, 0.14),
    inset 0 -1px 0 rgba(0, 0, 0, 0.35);
  pointer-events: none;
}

.selection-main {
  display: flex;
  align-items: stretch;
  gap: 14px;
  width: min(var(--selection-main-width), calc(100% - var(--actions-panel-width) - 28px));
  max-width: calc(100% - var(--actions-panel-width) - 28px);
  pointer-events: auto;
}

.selection-panel {
  min-width: 0;
  padding: 12px 14px;
  border-radius: 14px;
  background:
    radial-gradient(circle at bottom, rgba(176, 124, 52, 0.14), transparent 38%),
    linear-gradient(180deg, rgb(26, 18, 12), rgb(17, 11, 7));
}

.selection-panel--primary {
  flex: 0 0 var(--selection-panel-width);
  width: var(--selection-panel-width);
  min-height: var(--main-panel-height);
}

.selection-panel--actions {
  position: absolute;
  right: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  width: var(--actions-panel-width);
  height: var(--actions-panel-height);
  max-height: var(--actions-panel-height);
  overflow: hidden;
  pointer-events: auto;
}

.selection-panel--details {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  height: var(--main-panel-height);
  min-height: var(--main-panel-height);
  min-width: 0;
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
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  align-content: stretch;
  gap: 8px;
  width: 100%;
  flex: 1 1 auto;
  overflow-y: auto;
  padding-right: 2px;
}

.detail-inline {
  margin-top: 10px;
  color: #e7d7b6;
  font-size: 13px;
  line-height: 1.45;
  overflow-y: auto;
}

.production-card {
  margin-top: 10px;
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

.action-button,
.action-empty {
  width: 100%;
  min-width: 0;
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
  min-width: 0;
  color: #cbb893;
}

@media (max-width: 1000px) {
  .selection-hud {
    display: flex;
    align-items: flex-end;
    gap: 10px;
    left: 14px;
    right: 14px;
    bottom: 14px;
    padding: 8px;
    --selection-panel-width: 210px;
    --selection-main-width: 700px;
    --actions-panel-width: 180px;
    --actions-panel-height: 200px;
    --main-panel-height: 98px;
  }

  .selection-title {
    font-size: 15px;
  }
}

@media (max-width: 720px) {
  .selection-hud {
    display: flex;
    flex-direction: row;
    align-items: flex-end;
    gap: 8px;
    left: 10px;
    right: 10px;
    bottom: 10px;
    padding: 6px;
    --selection-panel-width: 170px;
    --selection-main-width: 430px;
    --actions-panel-width: 152px;
    --actions-panel-height: 176px;
    --main-panel-height: 88px;
  }

  .selection-panel {
    padding: 8px 10px;
  }

  .selection-main {
    gap: 8px;
  }

  .selection-title {
    font-size: 13px;
    margin-top: 2px;
  }

  .selection-subtitle {
    font-size: 10px;
    margin-top: 2px;
  }

  .action-grid {
    margin-top: 6px;
    gap: 5px;
  }

  .detail-inline {
    margin-top: 6px;
    font-size: 11px;
  }

  .production-card {
    margin-top: 6px;
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

  .action-button,
  .action-empty {
    width: 100%;
    min-width: 0;
    padding: 7px 8px;
    font-size: 11px;
  }
}
</style>
