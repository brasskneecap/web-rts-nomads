<template>
  <header class="hud" :style="{ '--ui-panel-image': `url(${uiPanelUrl})` }">
    <div class="hud-crest">
      <button
        class="crest-button"
        type="button"
        :aria-expanded="settingsOpen"
        aria-haspopup="menu"
        title="Open Settings"
        @click="toggleSettings"
      >
        <div class="crest-mark"></div>
      </button>
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

      <div v-if="settingsOpen" class="settings-menu" role="menu" aria-label="Settings">
        <div class="settings-title">Settings</div>
        <button class="settings-item" type="button" role="menuitem" @click="exitGame">
          Exit Game
        </button>
      </div>
    </div>

    <!-- Wave indicator — only rendered when the server has wave mode enabled -->
    <section v-if="ui.wave.enabled" class="wave-panel" aria-label="Wave status">
      <div class="wave-label">
        {{ waveLabel }}
      </div>
      <div class="wave-timer">{{ waveTimerText }}</div>
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
          <div class="resource-amount">{{ resource.max != null ? `${resource.amount}/${resource.max}` : resource.amount }}</div>
        </div>
      </article>
    </section>
  </header>

  <transition-group name="toast" tag="div" class="toast-stack">
    <div
      v-for="n in ui.notifications"
      :key="n.id"
      class="toast"
    >{{ n.message }}</div>
  </transition-group>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { GameUiSnapshot } from '@/game/core/GameClient'
import uiPanelUrl from '@/assets/ui/ui_panel_56x56_slice17.png'

const emit = defineEmits<{
  exit: []
}>()

const props = defineProps<{
  ui: GameUiSnapshot
}>()

const settingsOpen = ref(false)

function formatSeconds(s: number): string {
  const total = Math.max(0, Math.ceil(s))
  const m = Math.floor(total / 60)
  const sec = total % 60
  return `${m}:${sec.toString().padStart(2, '0')}`
}

const waveLabel = computed(() => {
  const w = props.ui.wave
  if (w.state === 'complete') return 'Victory'
  if (w.state === 'prep') {
    const next = w.currentWave + 1
    return `Wave ${next}`
  }
  return `Wave ${w.currentWave}`
})

const waveTimerText = computed(() => {
  const w = props.ui.wave
  if (w.state === 'prep') return `Next Wave In: ${formatSeconds(w.timer)}`
  if (w.state === 'active') {
    const timerExpired = w.waveDuration > 0 && w.timer >= w.waveDuration
    if (timerExpired) return 'Finish them!'
    return `Wave Time: ${formatSeconds(w.waveDuration - w.timer)}`
  }
  return ''
})

function toggleSettings() {
  settingsOpen.value = !settingsOpen.value
}

function exitGame() {
  settingsOpen.value = false
  emit('exit')
}
</script>

<style scoped>
.hud {
  position: relative;
  /* Above DebugSpawnPanel (z-index: 10) so the settings dropdown — which is
     clipped to this stacking context — pops in front of the debug panel when
     both are open. The HUD itself is along the top edge and doesn't overlap
     other UI, so elevating the whole bar is harmless. */
  z-index: 20;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 18px;
  padding: 4px 18px;

  /* 9-slice panel: shared 56×56 source, 16px corners. */
  background: none;
  border: 17px solid transparent;
  border-image-source: var(--ui-panel-image);
  border-image-slice: 17 fill;
  border-image-width: 17px;
  border-image-repeat: round;
  image-rendering: pixelated;
}

.hud-crest {
  position: relative;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 0 1 280px;
}

.crest-button {
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
}

.crest-button:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 3px;
  border-radius: 12px;
}

.crest-mark {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background:
    radial-gradient(circle at 30% 30%, rgba(255, 225, 152, 0.8), transparent 35%),
    linear-gradient(180deg, #9a6937, #5f3c1d);
  border: 1px solid rgba(227, 194, 132, 0.4);
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 214, 0.2),
    0 3px 10px rgba(0, 0, 0, 0.25);
}

.crest-button:hover .crest-mark {
  filter: brightness(1.08);
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

.settings-menu {
  position: absolute;
  top: calc(100% + 10px);
  left: 0;
  min-width: 180px;
  padding: 10px;

  /* 9-slice panel frame */
  background: none;
  border: 17px solid transparent;
  border-image-source: var(--ui-panel-image);
  border-image-slice: 17 fill;
  border-image-width: 17px;
  border-image-repeat: round;
  image-rendering: pixelated;
  box-shadow: 0 12px 26px rgba(0, 0, 0, 0.34);
}

.settings-title {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #d7bb84;
}

.settings-item {
  width: 100%;
  margin-top: 8px;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.24);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.85), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font-size: 13px;
  font-weight: 700;
  text-align: left;
  cursor: pointer;
}

.settings-item:hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.95), rgba(83, 53, 28, 0.98));
  border-color: rgba(220, 180, 110, 0.5);
}

.resource-card {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  min-width: 120px;
  padding: 4px 12px;
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

.wave-panel {
  flex: 1 1 220px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
}

.wave-label {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  white-space: nowrap;
}

.wave-timer {
  font-size: 11px;
  font-weight: 600;
  color: #cbb893;
  letter-spacing: 0.06em;
  white-space: nowrap;
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
    gap: 10px;
    padding: 8px 12px;
  }

  .hud-command {
    display: none;
  }

  .hud-crest {
    flex: 0 0 auto;
  }

  .resource-tray {
    flex: 1 1 0;
    min-width: 0;
    justify-content: flex-end;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .resource-tray::-webkit-scrollbar {
    display: none;
  }
}

@media (max-width: 600px) {
  .hud {
    gap: 8px;
    padding: 6px 10px;
  }

  .crest-mark {
    width: 26px;
    height: 26px;
  }

  .player-name {
    font-size: 13px;
  }

  .resource-card {
    min-width: 0;
    padding: 5px 9px;
    gap: 6px;
  }

  .resource-label {
    font-size: 10px;
  }

  .resource-amount {
    font-size: 14px;
  }
}

.toast-stack {
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  z-index: 100;
  pointer-events: none;
}

.toast {
  background: rgba(20, 10, 10, 0.88);
  color: #f0c070;
  border: 1px solid rgba(200, 80, 60, 0.5);
  border-radius: 6px;
  padding: 7px 16px;
  font-size: 13px;
  font-weight: 600;
  backdrop-filter: blur(6px);
  white-space: nowrap;
}

.toast-enter-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.toast-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}
.toast-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
