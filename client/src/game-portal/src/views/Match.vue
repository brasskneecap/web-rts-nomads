<template>
  <div class="match-view">
    <div v-if="showResumePrompt" class="menu">
      <div class="menu-title">Return to previous game?</div>
      <div class="menu-text">
        You have a saved player session on
        <strong>{{ resumeMapName }}</strong>.
      </div>

      <div class="menu-actions">
        <button @click="returnToPreviousGame">Return</button>
        <button @click="startNewGame">Start New Game</button>
      </div>
    </div>

    <MatchHud v-if="hasStarted" :ui="ui" @exit="exitGame" />

    <WaveUpgradeModal
      v-if="hasStarted && ui.waveUpgrade"
      :upgrade="ui.waveUpgrade!"
      :units="ui.allPlayerUnits.filter(u => u.unitType !== 'worker')"
      :send-choice="sendWaveUpgradeChoice"
      :send-reroll="sendWaveUpgradeReroll"
    />

    <BattleTrackerPanel v-if="hasStarted" :ui="ui" />

    <DebugSpawnPanel
      v-if="hasStarted"
      :ui="ui"
      :targeting-active="debugSpawnTargetingActive"
      :begin-debug-spawn="beginDebugSpawn"
      :cancel-debug-spawn="cancelDebugSpawn"
    />

    <div v-if="hasStarted && ((ui.wave.enabled && ui.wave.state === 'complete' && !ui.objectives.length) || ui.isVictory)" class="victory-overlay">
      <div class="victory-card">
        <div class="victory-title">Victory</div>
        <div class="victory-subtitle">{{ ui.objectives.length ? 'All objectives completed' : 'All waves defeated' }}</div>
        <!-- TODO: wire legendPointsEarned from MatchSummary — display as "You earned N legend points" -->
        <button class="victory-button" type="button" @click="exitGame">Return to Menu</button>
      </div>
    </div>

    <div v-if="hasStarted && ui.isDefeated" class="defeat-overlay">
      <div class="defeat-card">
        <div class="defeat-title">Defeated</div>
        <div class="defeat-subtitle">All townhalls have been destroyed</div>
        <!-- TODO: wire legendPointsEarned from MatchSummary — display as "You earned N legend points" -->
        <button class="defeat-button" type="button" @click="exitGame">Return to Menu</button>
      </div>
    </div>

    <div
      v-if="hasStarted && (connectionState === 'reconnecting' || connectionState === 'failed')"
      class="disconnect-overlay"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="connectionState === 'reconnecting' ? 'disconnect-title-reconnecting' : 'disconnect-title-failed'"
      :aria-describedby="connectionState === 'reconnecting' ? 'disconnect-desc-reconnecting' : 'disconnect-desc-failed'"
    >
      <div class="disconnect-card">
        <template v-if="connectionState === 'reconnecting'">
          <div id="disconnect-title-reconnecting" class="disconnect-title">Connection Lost</div>
          <div id="disconnect-desc-reconnecting" class="disconnect-desc">
            Reconnecting...
            <span v-if="reconnectAttempt > 0">(attempt {{ reconnectAttempt }} of {{ maxReconnectAttempts }})</span>
          </div>
          <div class="disconnect-spinner" aria-hidden="true"></div>
        </template>

        <template v-else>
          <div id="disconnect-title-failed" class="disconnect-title disconnect-title--failed">
            Unable to Reconnect
          </div>
          <div id="disconnect-desc-failed" class="disconnect-desc">
            Could not reach the server after {{ maxReconnectAttempts }} attempts.
          </div>
          <div class="disconnect-actions">
            <button type="button" class="disconnect-button disconnect-button--retry" @click="retryReconnect">
              Retry
            </button>
            <button type="button" class="disconnect-button disconnect-button--exit" @click="exitGame">
              Return to Menu
            </button>
          </div>
        </template>
      </div>
    </div>

    <div class="match-stage">
      <canvas ref="canvas" class="game-canvas"></canvas>
      <SelectionHud
        v-if="hasStarted"
        :ui="ui"
        @action="performSelectionAction"
        @select-unit="selectUnitOnly"
        @deselect-unit="deselectUnit"
        @minimap-rect="setMinimapPanelRect"
        @use-consumable="({ unitId, slotIndex }) => sendUseConsumable(unitId, slotIndex)"
        @unequip-item="({ unitId, slotIndex }) => sendUnequipItem(unitId, slotIndex)"
        @equip-item="({ unitId, slotIndex, instanceId }) => sendEquipItem(unitId, slotIndex, instanceId)"
      />
      <VaultPanel
        v-if="hasStarted && ui.vaultPanelOpen"
        :vault="ui.vault"
        :vault-capacity="ui.vaultCapacity"
        :vault-selected-instance-id="ui.vaultSelectedInstanceId"
        :units="ui.allPlayerUnits"
        :on-select-vault-item="setVaultSelectedInstanceId"
        :on-equip-item="sendEquipItem"
        :on-unequip-item="sendUnequipItem"
        :on-use-consumable="sendUseConsumable"
        :on-transfer-item="sendTransferItem"
        :on-close="() => performSelectionAction('open-vault')"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import VaultPanel from '@/components/VaultPanel.vue'
import BattleTrackerPanel from '@/components/BattleTrackerPanel.vue'
import DebugSpawnPanel from '@/components/DebugSpawnPanel.vue'
import WaveUpgradeModal from '@/components/WaveUpgradeModal.vue'
import { useGameClient } from '@/composables/useGameClient'
import { useMapSelection } from '@/composables/useMapSelection'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'

const router = useRouter()
const route = useRoute()
const canvas = ref<HTMLCanvasElement | null>(null)
const hasStarted = ref(false)

const { selectedMapId, selectedMapName, setSelectedMapId } = useMapSelection()

const hasPreviousSession = ref(
  localStorage.getItem(HAS_ACTIVE_SESSION_KEY) === 'true' &&
    !!localStorage.getItem(PLAYER_ID_STORAGE_KEY) &&
    !!localStorage.getItem(MATCH_ID_STORAGE_KEY),
)

const resumeMapName = computed(() => {
  if (selectedMapName.value) return selectedMapName.value
  if (selectedMapId.value) return selectedMapId.value
  return 'Unknown Map'
})

const showResumePrompt = computed(() => !hasStarted.value && hasPreviousSession.value)

const {
  init,
  destroy,
  leaveStoredMatch,
  performSelectionAction,
  retryReconnect,
  beginDebugSpawn,
  cancelDebugSpawn,
  selectUnitOnly,
  deselectUnit,
  setMinimapPanelRect,
  sendEquipItem,
  sendUnequipItem,
  sendUseConsumable,
  sendTransferItem,
  setVaultSelectedInstanceId,
  sendWaveUpgradeChoice,
  sendWaveUpgradeReroll,
  ui,
  connectionState,
  currentMatchId,
  reconnectAttempt,
  maxReconnectAttempts,
} = useGameClient()

const debugSpawnTargetingActive = computed(() => ui.value.debugSpawnTargetingActive)

function clearStaleSession() {
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
}

async function startGame(mapId: string, options: { resume?: boolean } = {}) {
  if (!canvas.value || !mapId) return
  await init(canvas.value, mapId, options)
  hasStarted.value = true
  localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
}

async function returnToPreviousGame() {
  const mapId = selectedMapId.value || localStorage.getItem(MAP_ID_STORAGE_KEY) || ''
  await startGame(mapId, { resume: true })
}

async function startNewGame() {
  await leaveStoredMatch()
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  hasPreviousSession.value = false
  const mapId = selectedMapId.value
  if (!mapId) {
    void router.push('/')
    return
  }
  await startGame(mapId, { resume: false })
}

async function exitGame() {
  await leaveStoredMatch()
  destroy()
  hasStarted.value = false
  hasPreviousSession.value = false
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  setSelectedMapId('', '')
  void router.push('/')
}

function markActiveSession() {
  if (hasStarted.value) {
    localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
  }
}

watch(currentMatchId, (id) => {
  if (id && route.params.matchId !== id) {
    void router.replace({ path: `/match/${id}`, query: route.query })
  }
})

window.addEventListener('beforeunload', markActiveSession)

onMounted(async () => {
  const urlMatchId = route.params.matchId as string | undefined
  if (urlMatchId) {
    const playerId = localStorage.getItem(PLAYER_ID_STORAGE_KEY) ?? ''
    if (!playerId) {
      clearStaleSession()
      void router.push('/')
      return
    }
    try {
      const res = await fetch(`${API_BASE}/matches/${encodeURIComponent(urlMatchId)}/status?playerId=${encodeURIComponent(playerId)}`)
      if (res.status === 404 || !res.ok) {
        clearStaleSession()
        void router.push('/')
        return
      }
      const data = await res.json() as { matchId: string; mapId: string; isParticipant: boolean }
      if (!data.isParticipant) {
        clearStaleSession()
        void router.push('/')
        return
      }
      localStorage.setItem(MATCH_ID_STORAGE_KEY, data.matchId)
      localStorage.setItem(MAP_ID_STORAGE_KEY, data.mapId)
      localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
      setSelectedMapId(data.mapId, '')
      await startGame(data.mapId, { resume: true })
      return
    } catch {
      clearStaleSession()
      void router.push('/')
      return
    }
  }

  if (route.query.resume === '1' && hasPreviousSession.value) {
    await returnToPreviousGame()
    return
  }

  if (hasPreviousSession.value) {
    return
  }

  const mapId = selectedMapId.value || localStorage.getItem(MAP_ID_STORAGE_KEY) || ''
  if (!mapId) {
    void router.push('/')
    return
  }

  await startGame(mapId, { resume: false })
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', markActiveSession)
  destroy()
})
</script>

<style scoped>
.match-view {
  width: 100%;
  height: 100dvh;
  position: relative;
  overflow: hidden;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  background:
    radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%),
    #05080d;
}

.menu {
  position: absolute;
  top: 16px;
  left: 16px;
  z-index: 20;
  min-width: 260px;
  background: rgba(0, 0, 0, 0.75);
  color: white;
  padding: 12px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(10px);
}

.menu-title {
  font-weight: 700;
  margin-bottom: 8px;
}

.menu-text {
  margin-bottom: 10px;
}

.menu-actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.match-stage {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
}

.game-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #111;
}

.victory-overlay {
  position: absolute;
  inset: 0;
  z-index: 50;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(5, 8, 13, 0.72);
  backdrop-filter: blur(4px);
}

.victory-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 48px 56px;
  border-radius: 20px;
  background:
    radial-gradient(circle at top, rgba(196, 158, 62, 0.22), transparent 52%),
    linear-gradient(180deg, rgba(62, 39, 20, 0.98), rgba(22, 13, 7, 0.98));
  border: 1px solid rgba(220, 180, 100, 0.35);
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 200, 0.14),
    0 24px 60px rgba(0, 0, 0, 0.6);
}

.victory-title {
  font-size: 48px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #f7d88e;
  text-transform: uppercase;
}

.victory-subtitle {
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #cbb893;
}

.victory-button {
  margin-top: 10px;
  padding: 12px 32px;
  border-radius: 12px;
  border: 1px solid rgba(220, 180, 100, 0.35);
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.95), rgba(83, 53, 28, 0.98));
  color: #f5ead2;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.06em;
  cursor: pointer;
}

.victory-button:hover {
  background: linear-gradient(180deg, rgba(175, 118, 58, 1), rgba(105, 67, 35, 1));
  border-color: rgba(240, 200, 120, 0.55);
}

.disconnect-overlay {
  position: absolute;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(5, 8, 13, 0.78);
  backdrop-filter: blur(4px);
}

.disconnect-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 44px 52px;
  border-radius: 20px;
  background:
    radial-gradient(circle at top, rgba(80, 120, 200, 0.16), transparent 52%),
    linear-gradient(180deg, rgba(16, 22, 38, 0.98), rgba(8, 11, 20, 0.98));
  border: 1px solid rgba(100, 140, 220, 0.25);
  box-shadow:
    inset 0 1px 0 rgba(160, 190, 255, 0.1),
    0 24px 60px rgba(0, 0, 0, 0.65);
  min-width: 320px;
  text-align: center;
}

.disconnect-title {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #a8c4f0;
  text-transform: uppercase;
}

.disconnect-title--failed {
  color: #f0a0a0;
}

.disconnect-desc {
  font-size: 14px;
  color: #8899bb;
  line-height: 1.5;
}

.disconnect-spinner {
  width: 28px;
  height: 28px;
  border: 3px solid rgba(100, 140, 220, 0.25);
  border-top-color: #7aabee;
  border-radius: 50%;
  animation: disconnect-spin 0.8s linear infinite;
}

@keyframes disconnect-spin {
  to { transform: rotate(360deg); }
}

.disconnect-actions {
  display: flex;
  gap: 10px;
  margin-top: 4px;
}

.disconnect-button {
  padding: 10px 24px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.05em;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}

.disconnect-button--retry {
  background: linear-gradient(180deg, rgba(60, 100, 180, 0.9), rgba(35, 65, 130, 0.95));
  border: 1px solid rgba(100, 150, 240, 0.4);
  color: #ccdeff;
}

.disconnect-button--retry:hover {
  background: linear-gradient(180deg, rgba(80, 120, 200, 1), rgba(50, 85, 155, 1));
  border-color: rgba(130, 175, 255, 0.6);
}

.disconnect-button--exit {
  background: linear-gradient(180deg, rgba(50, 30, 30, 0.9), rgba(30, 18, 18, 0.95));
  border: 1px solid rgba(160, 80, 80, 0.35);
  color: #e0b8b8;
}

.disconnect-button--exit:hover {
  background: linear-gradient(180deg, rgba(80, 40, 40, 1), rgba(50, 28, 28, 1));
  border-color: rgba(200, 100, 100, 0.55);
}

.defeat-overlay {
  position: absolute;
  inset: 0;
  z-index: 50;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(5, 8, 13, 0.72);
  backdrop-filter: blur(4px);
  pointer-events: all;
}

.defeat-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 48px 56px;
  border-radius: 20px;
  background:
    radial-gradient(circle at top, rgba(160, 30, 30, 0.22), transparent 52%),
    linear-gradient(180deg, rgba(40, 12, 12, 0.98), rgba(16, 6, 6, 0.98));
  border: 1px solid rgba(200, 60, 60, 0.35);
  box-shadow:
    inset 0 1px 0 rgba(255, 180, 180, 0.1),
    0 24px 60px rgba(0, 0, 0, 0.65);
}

.defeat-title {
  font-size: 48px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #f07070;
  text-transform: uppercase;
}

.defeat-subtitle {
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #b88888;
}

.defeat-button {
  margin-top: 10px;
  padding: 12px 32px;
  border-radius: 12px;
  border: 1px solid rgba(200, 60, 60, 0.35);
  background: linear-gradient(180deg, rgba(120, 30, 30, 0.95), rgba(70, 16, 16, 0.98));
  color: #f5d8d8;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.06em;
  cursor: pointer;
}

.defeat-button:hover {
  background: linear-gradient(180deg, rgba(150, 40, 40, 1), rgba(90, 22, 22, 1));
  border-color: rgba(230, 80, 80, 0.55);
}
</style>
