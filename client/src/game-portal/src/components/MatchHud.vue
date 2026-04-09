<template>
  <header class="hud">
    <div class="hud-crest">
      <div class="crest-mark"></div>
      <div class="crest-copy">
        <div class="hud-kicker">Warband</div>
        <div class="player-row">
          <span
            v-if="ui.player.color"
            class="player-color"
            :style="{ backgroundColor: ui.player.color }"
          ></span>
          <span class="player-name">{{ ui.player.playerId || 'Connecting...' }}</span>
        </div>
      </div>
    </div>

    <section class="hud-command">
      <div class="hud-kicker">Orders</div>
      <div class="hud-copy">
        Selected units show their health above the battlefield. Drag-select, then
        right-click to move.
      </div>
    </section>

    <section class="resource-tray" aria-label="Resources">
      <article
        v-for="resource in ui.player.resources"
        :key="resource.id"
        class="resource-card"
      >
        <span
          class="resource-gem"
          :style="{ background: `linear-gradient(180deg, ${resource.accent}, rgba(0,0,0,0.65))` }"
        ></span>
        <div class="resource-copy">
          <div class="resource-label">{{ resource.label }}</div>
          <div class="resource-amount">{{ resource.amount }}</div>
        </div>
      </article>
    </section>
  </header>
</template>

<script setup lang="ts">
import type { GameUiSnapshot } from '@/game/core/GameClient'

defineProps<{
  ui: GameUiSnapshot
}>()
</script>

<style scoped>
.hud {
  position: relative;
  z-index: 5;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 18px;
  padding: 12px 18px;
  border-bottom: 1px solid rgba(123, 93, 48, 0.5);
  background:
    radial-gradient(circle at top, rgba(176, 124, 52, 0.18), transparent 42%),
    linear-gradient(180deg, rgba(58, 35, 18, 0.98), rgba(27, 16, 10, 0.94));
  box-shadow:
    inset 0 1px 0 rgba(240, 220, 178, 0.14),
    inset 0 -1px 0 rgba(0, 0, 0, 0.35);
}

.hud-crest {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 0 1 280px;
}

.crest-mark {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background:
    radial-gradient(circle at 30% 30%, rgba(255, 225, 152, 0.8), transparent 35%),
    linear-gradient(180deg, #9a6937, #5f3c1d);
  border: 1px solid rgba(227, 194, 132, 0.4);
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 214, 0.2),
    0 3px 10px rgba(0, 0, 0, 0.25);
}

.crest-copy {
  min-width: 0;
}

.hud-kicker {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #d7bb84;
  white-space: nowrap;
}

.player-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-top: 2px;
}

.player-name {
  font-size: 16px;
  font-weight: 700;
  color: #f5ead2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.player-color {
  width: 12px;
  height: 12px;
  border-radius: 999px;
  flex: 0 0 auto;
  box-shadow: 0 0 0 2px rgba(245, 234, 210, 0.16);
}

.resource-tray {
  flex: 0 1 420px;
  min-width: 0;
  display: flex;
  justify-content: flex-end;
  flex-wrap: nowrap;
  gap: 10px;
}

.resource-card {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  min-width: 120px;
  padding: 8px 12px;
  border-radius: 999px;
  background:
    linear-gradient(180deg, rgba(111, 76, 39, 0.78), rgba(63, 41, 23, 0.92));
  border: 1px solid rgba(200, 164, 106, 0.28);
  box-shadow:
    inset 0 1px 0 rgba(246, 225, 183, 0.12),
    0 5px 12px rgba(0, 0, 0, 0.18);
}

.resource-gem {
  width: 12px;
  height: 12px;
  border-radius: 999px;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.28),
    0 0 0 2px rgba(27, 16, 10, 0.35);
}

.resource-copy {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.resource-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #d7bb84;
}

.resource-amount {
  font-size: 18px;
  font-weight: 700;
  color: #fff2d6;
}

.hud-command {
  flex: 1 1 320px;
  min-width: 0;
  text-align: center;
}

.hud-copy {
  margin-top: 2px;
  font-size: 13px;
  line-height: 1.35;
  color: #cbb893;
  text-align: center;
}

@media (max-width: 900px) {
  .hud {
    flex-direction: column;
    align-items: stretch;
    padding: 12px;
  }

  .hud-crest,
  .hud-command {
    flex: 1 1 auto;
  }

  .resource-tray {
    justify-content: flex-start;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .resource-tray::-webkit-scrollbar {
    display: none;
  }

  .hud-copy {
    text-align: left;
  }

  .hud-command {
    text-align: left;
  }
}
</style>
