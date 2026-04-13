<template>
  <footer class="selection-hud">
    <section class="selection-panel selection-panel--primary">
      <div class="selection-kicker">Selection</div>
      <div class="selection-title">{{ ui.selection.title }}</div>
      <div class="selection-subtitle">{{ ui.selection.subtitle }}</div>
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
          @click="$emit('action', action.id)"
        >
          {{ action.label }}
        </button>
        <div v-if="ui.selection.actions.length === 0" class="action-empty">
          No actions available
        </div>
      </div>
    </section>

    <section class="selection-panel selection-panel--details">
      <div class="selection-kicker">Details</div>
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
  position: relative;
  z-index: 5;
  display: flex;
  align-items: stretch;
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

.selection-panel--primary {
  flex: 0 1 300px;
}

.selection-panel--actions {
  display: flex;
  flex-direction: column;
  flex: 1 1 420px;
}

.selection-panel--details {
  display: flex;
  flex-direction: column;
  flex: 0 1 240px;
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

.action-grid {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 8px;
  width: 100%;
}

.detail-inline {
  margin-top: 10px;
  color: #e7d7b6;
  font-size: 13px;
  line-height: 1.45;
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
  width: auto;
  min-width: 108px;
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
    align-items: stretch;
    overflow-x: auto;
    scrollbar-width: none;
    gap: 10px;
    padding: 10px 14px 14px;
  }

  .selection-hud::-webkit-scrollbar {
    display: none;
  }

  .selection-panel--primary {
    flex: 0 0 240px;
  }

  .selection-panel--actions {
    flex: 1 0 320px;
  }

  .selection-panel--details {
    flex: 0 0 220px;
  }

  .selection-title {
    font-size: 16px;
  }
}

@media (max-width: 720px) {
  .selection-hud {
    display: flex;
    flex-direction: row;
    align-items: stretch;
    overflow-x: auto;
    scrollbar-width: none;
    gap: 8px;
    padding: 8px 10px 10px;
  }

  .selection-hud::-webkit-scrollbar {
    display: none;
  }

  .selection-panel {
    flex: 0 0 auto;
    min-width: 130px;
    padding: 8px 10px;
  }

  .selection-panel--actions {
    min-width: 180px;
  }

  .selection-title {
    font-size: 14px;
    margin-top: 2px;
  }

  .selection-subtitle {
    font-size: 11px;
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

  .detail-entry strong {
    font-size: 11px;
  }

  .action-button,
  .action-empty {
    width: auto;
    min-width: 84px;
    padding: 7px 8px;
    font-size: 11px;
  }
}
</style>
