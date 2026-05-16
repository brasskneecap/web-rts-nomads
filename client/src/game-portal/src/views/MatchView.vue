<template>
  <div class="match-view">
    <div v-if="showResumePrompt" class="menu">
      <div class="menu-title">Return to previous game?</div>
      <div class="menu-text">
        You have a saved player session on
        <strong>{{ selectedMapName }}</strong>.
      </div>

      <div class="menu-actions">
        <button @click="returnToPreviousGame">Return</button>
        <button @click="startNewGame">Start New Game</button>
      </div>
    </div>

    <div v-else-if="showSizeMenu && !showEditor" class="menu">
      <div class="menu-title">Choose Map</div>

      <label for="map-id">Map:</label>
      <div class="map-select-row">
        <select id="map-id" v-model="selectedMapId" :disabled="isLoadingMaps || mapCatalog.length === 0">
          <option v-for="map in mapCatalog" :key="map.id" :value="map.id">
            {{ map.name }}
          </option>
        </select>
        <button
          type="button"
          class="map-refresh-button"
          :disabled="isLoadingMaps"
          :title="isLoadingMaps ? 'Loading...' : 'Refresh map list'"
          @click="loadMapCatalog"
        >↺</button>
      </div>

      <div class="menu-text" v-if="selectedMapDescription">
        {{ selectedMapDescription }}
      </div>

      <div class="menu-text" v-if="isLoadingMaps">Loading maps...</div>
      <div class="menu-text" v-else-if="mapsLoadError">{{ mapsLoadError }}</div>

      <div class="menu-actions">
        <button @click="startGame(selectedMapId, { resume: false })" :disabled="!selectedMapId || isLoadingMaps">
          Start Game
        </button>
        <button @click="editorMode = true">
          Map Editor
        </button>
      </div>
    </div>

    <MatchHud v-if="hasStarted" :ui="ui" @exit="exitGame" />

    <WaveUpgradeModal
      v-if="hasStarted && ui.waveUpgrade"
      :upgrade="ui.waveUpgrade"
      :units="ui.allPlayerUnits"
      :send-choice="sendWaveUpgradeChoice"
      :send-reroll="sendWaveUpgradeReroll"
    />

    <!-- Debug panel — only rendered when the active map has debug.battleTracker.
         Floats top-right on top of the canvas and handles its own save/review. -->
    <BattleTrackerPanel v-if="hasStarted" :ui="ui" />

    <!-- Debug panel — only rendered when the active map has debug.debugSpawn.
         Lets the user spawn an enemy unit with a chosen perk loadout at a
         click location for perk/unit testing. -->
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
        <button class="victory-button" type="button" @click="exitGame">Return to Menu</button>
      </div>
    </div>

    <div v-if="hasStarted && ui.isDefeated" class="defeat-overlay">
      <div class="defeat-card">
        <div class="defeat-title">Defeated</div>
        <div class="defeat-subtitle">All townhalls have been destroyed</div>
        <button class="defeat-button" type="button" @click="exitGame">Return to Menu</button>
      </div>
    </div>

    <!-- Disconnect overlay: shown while reconnecting or after reconnect failure -->
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

    <div class="match-stage" :class="{ 'match-stage--editor': showEditor }">
      <canvas v-show="hasStarted && !showEditor" ref="canvas" class="game-canvas"></canvas>
      <div v-if="showEditor" class="editor-stage">
        <div class="editor-topbar editor-topbar--right">
          <button type="button" class="editor-topbar__button" @click="editorMode = false">
            Back To Maps
          </button>
        </div>
        <MapEditorPanel v-model="editorMap" />
      </div>
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
import { ref, computed, watch, onBeforeUnmount, onMounted } from 'vue'
import MapEditorPanel from '@/components/MapEditorPanel.vue'
import MatchHud from '@/components/MatchHud.vue'
import WaveUpgradeModal from '@/components/WaveUpgradeModal.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import BattleTrackerPanel from '@/components/BattleTrackerPanel.vue'
import DebugSpawnPanel from '@/components/DebugSpawnPanel.vue'
import VaultPanel from '@/components/VaultPanel.vue'
import { useGameClient } from '@/composables/useGameClient'
import { fetchMapCatalog } from '@/game/maps/catalog'
import type { MapCatalogEntry, MapId } from '@/game/network/protocol'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'
const MAP_EDITOR_STORAGE_KEY = 'webrts.mapEditorDraft'

const canvas = ref<HTMLCanvasElement | null>(null)
const mapCatalog = ref<MapCatalogEntry[]>([])
const isLoadingMaps = ref(true)
const mapsLoadError = ref('')
const editorMode = ref(false)

function getStoredEditorMap() {
  const stored = localStorage.getItem(MAP_EDITOR_STORAGE_KEY)

  if (!stored) {
    return createEditorMapConfig()
  }

  try {
    return createEditorMapConfig(undefined, undefined, JSON.parse(stored))
  } catch {
    return createEditorMapConfig()
  }
}

const selectedMapId = ref<MapId>(localStorage.getItem(MAP_ID_STORAGE_KEY) ?? '')
const editorMap = ref(getStoredEditorMap())

watch(selectedMapId, (val) => {
  if (val) {
    localStorage.setItem(MAP_ID_STORAGE_KEY, val)
  }
})

watch(
  editorMap,
  (val) => {
    localStorage.setItem(MAP_EDITOR_STORAGE_KEY, JSON.stringify(val))
  },
  { deep: true },
)

const hasPreviousSession = ref(
  localStorage.getItem(HAS_ACTIVE_SESSION_KEY) === 'true' &&
    !!localStorage.getItem(PLAYER_ID_STORAGE_KEY) &&
    !!localStorage.getItem(MATCH_ID_STORAGE_KEY),
)

const hasStarted = ref(false)

const showResumePrompt = computed(
  () => !hasStarted.value && hasPreviousSession.value,
)

const showSizeMenu = computed(
  () => !hasStarted.value && !hasPreviousSession.value,
)

const showEditor = computed(
  () => showSizeMenu.value && editorMode.value,
)

const selectedMap = computed(
  () => mapCatalog.value.find((map) => map.id === selectedMapId.value) ?? null,
)

const selectedMapName = computed(() => {
  if (selectedMap.value?.name) return selectedMap.value.name
  if (selectedMapId.value) return selectedMapId.value
  return 'Unknown Map'
})

const selectedMapDescription = computed(
  () => selectedMap.value?.description ?? '',
)

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
  reconnectAttempt,
  maxReconnectAttempts,
} = useGameClient()

const debugSpawnTargetingActive = computed(() => ui.value.debugSpawnTargetingActive)

async function startGame(mapId: MapId, options: { resume?: boolean } = {}) {
  if (!canvas.value) return
  if (!mapId) return

  await init(canvas.value, mapId, options)
  hasStarted.value = true

  localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
}

async function returnToPreviousGame() {
  await startGame(selectedMapId.value, { resume: true })
}

async function startNewGame() {
  await leaveStoredMatch()

  localStorage.removeItem(PLAYER_ID_STORAGE_KEY)
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)

  selectedMapId.value = mapCatalog.value[0]?.id ?? ''
  hasPreviousSession.value = false
}

async function exitGame() {
  await leaveStoredMatch()
  destroy()
  hasStarted.value = false
  hasPreviousSession.value = false
  localStorage.removeItem(PLAYER_ID_STORAGE_KEY)
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
}

async function loadMapCatalog() {
  isLoadingMaps.value = true
  mapsLoadError.value = ''

  try {
    const maps = await fetchMapCatalog()
    mapCatalog.value = maps

    if (!maps.some((map) => map.id === selectedMapId.value)) {
      selectedMapId.value = maps[0]?.id ?? ''
    }
  } catch (error) {
    mapsLoadError.value =
      error instanceof Error ? error.message : 'Failed to load maps.'
  } finally {
    isLoadingMaps.value = false
  }
}

function markActiveSession() {
  if (hasStarted.value) {
    localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
  }
}

window.addEventListener('beforeunload', markActiveSession)

onMounted(() => {
  void loadMapCatalog()
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

.map-select-row {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-bottom: 8px;
}

.map-select-row select {
  flex: 1 1 auto;
  min-width: 0;
}

.map-refresh-button {
  flex: 0 0 auto;
  padding: 5px 9px;
  border-radius: 6px;
  border: 1px solid rgba(255, 255, 255, 0.15);
  background: rgba(255, 255, 255, 0.08);
  color: white;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
}

.map-refresh-button:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.16);
}

.map-refresh-button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.match-stage {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
}

.match-stage--editor {
  padding: 0 12px 12px;
  box-sizing: border-box;
}

.game-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #111;
}

.editor-stage {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  overflow: hidden;
}

.editor-topbar {
  position: absolute;
  top: 16px;
  z-index: 20;
}

.editor-topbar--right {
  right: 16px;
}

.editor-topbar__button {
  padding: 10px 14px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: rgba(0, 0, 0, 0.75);
  color: white;
  backdrop-filter: blur(10px);
  cursor: pointer;
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

/* ------------------------------------------------------------------ */
/* Disconnect overlay                                                   */
/* ------------------------------------------------------------------ */

.disconnect-overlay {
  position: absolute;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(5, 8, 13, 0.78);
  backdrop-filter: blur(4px);
  /* Pointer events deliberately kept ON — blocks game input while disconnected */
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

/* Simple CSS spinner */
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

/* ------------------------------------------------------------------ */
/* Defeat overlay                                                       */
/* ------------------------------------------------------------------ */

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
