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
      :paused="ui.paused"
      :paused-since-ms="ui.pausedSinceMs"
    />

    <BattleTrackerPanel v-if="hasStarted" :ui="ui" />

    <DebugSpawnPanel
      v-if="hasStarted"
      :ui="ui"
      :targeting-active="debugSpawnTargetingActive"
      :begin-debug-spawn="beginDebugSpawn"
      :cancel-debug-spawn="cancelDebugSpawn"
    />

    <div
      v-if="hasStarted && ui.paused"
      class="pause-banner"
      role="status"
      aria-live="polite"
    >
      <div class="pause-banner__title">Game Paused</div>
      <div class="pause-banner__sub">
        {{ pausedByLabel }} Open Settings to resume.
      </div>
    </div>

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
      <MatchMenuLauncher
        v-if="hasStarted"
        :active-tab="matchMenuOpen ? matchMenuTab : null"
        :abilities="ui.commanderAbilities"
        :active-ability-id="ui.commanderTargetingAbilityId"
        @open="openMenuTab"
        @cast-ability="onCommanderCast"
        @settings="matchSettingsOpen = !matchSettingsOpen"
      />
      <MatchSettingsModal
        v-if="hasStarted && matchSettingsOpen"
        :paused="ui.paused"
        @close="matchSettingsOpen = false"
        @toggle-pause="(next) => sendSetPause(next)"
      />
      <MatchMenu
        v-if="hasStarted && matchMenuOpen"
        v-model:active-tab="matchMenuTab"
        :shop-catalog="ui.shopCatalog"
        :vault="ui.vault"
        :vault-capacity="ui.vaultCapacity"
        :vault-selected-instance-id="ui.vaultSelectedInstanceId"
        :units="ui.allPlayerUnits"
        :on-select-vault-item="setVaultSelectedInstanceId"
        :on-equip-item="sendEquipItem"
        :on-unequip-item="sendUnequipItem"
        :on-use-consumable="sendUseConsumable"
        :on-transfer-item="sendTransferItem"
        @close="matchMenuOpen = false"
        @purchase="({ itemId, buildingId }) => sendPurchaseItem(buildingId, itemId)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import BattleTrackerPanel from '@/components/BattleTrackerPanel.vue'
import DebugSpawnPanel from '@/components/DebugSpawnPanel.vue'
import WaveUpgradeModal from '@/components/WaveUpgradeModal.vue'
import MatchMenu from '@/components/MatchMenu.vue'
import MatchMenuLauncher from '@/components/MatchMenuLauncher.vue'
import MatchSettingsModal from '@/components/MatchSettingsModal.vue'
import { useGameClient } from '@/composables/useGameClient'
import { useMapSelection } from '@/composables/useMapSelection'
import { setCursorGrab } from '@/services/desktopBridge'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'
// §14R: joiners arrive via ?proxy=steam — their LOCAL Go server has no
// match registered for the host's matchId, so the preflight
// /matches/<id>/status would 404 → main-menu kick. Detect proxy mode
// and skip the preflight; the WS open will reach the host's hub via
// the parked Steam transport, and the hub does its own join validation.
const STEAM_PROXY_FLAG_KEY = 'webrts.steam.proxyActive'

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
  sendPurchaseItem,
  sendEquipItem,
  sendUnequipItem,
  sendUseConsumable,
  sendTransferItem,
  setVaultSelectedInstanceId,
  sendWaveUpgradeChoice,
  sendWaveUpgradeReroll,
  sendSetPause,
  beginCommanderAbility,
  cancelCommanderAbility,
  ui,
  connectionState,
  currentMatchId,
  reconnectAttempt,
  maxReconnectAttempts,
} = useGameClient()

const debugSpawnTargetingActive = computed(() => ui.value.debugSpawnTargetingActive)

const pausedByLabel = computed(() => {
  const id = ui.value.pausedBy
  if (!id) return ''
  if (ui.value.player.playerId && id === ui.value.player.playerId) {
    return 'Paused by you.'
  }
  return `Paused by ${id}.`
})

function onCommanderCast(abilityId: string) {
  // Toggle behaviour: clicking the same slot a second time cancels the
  // pending cast instead of re-arming it. Mirrors the unit-action-bar
  // ergonomic that already cancels on the second click.
  if (ui.value.commanderTargetingAbilityId === abilityId) {
    cancelCommanderAbility()
    return
  }
  beginCommanderAbility(abilityId)
}

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

const matchMenuOpen = ref(false)
const matchMenuTab = ref<string>('shop')
const matchSettingsOpen = ref(false)

// Maps a KeyboardEvent.code to the MatchMenu tab id it opens. Each key
// jumps directly to its tab; pressing the same key again closes the menu.
const MATCH_MENU_HOTKEYS: Record<string, string> = {
  KeyS: 'shop',
  KeyU: 'upgrades',
  KeyV: 'vault',
}

function isTextInputFocused() {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  if (el.isContentEditable) return true
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
}

function openMenuTab(tabId: string) {
  if (matchMenuOpen.value && matchMenuTab.value === tabId) {
    matchMenuOpen.value = false
    return
  }
  matchMenuTab.value = tabId
  matchMenuOpen.value = true
}

function onMatchMenuHotkey(e: KeyboardEvent) {
  if (!hasStarted.value) return
  if (!(e.code in MATCH_MENU_HOTKEYS)) return
  if (e.repeat || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return
  if (isTextInputFocused()) return
  if (ui.value.selectedUnits.length > 0) return

  const targetTab = MATCH_MENU_HOTKEYS[e.code]
  if (matchMenuOpen.value && matchMenuTab.value === targetTab) {
    // Pressing the same tab key while already on it closes the menu.
    matchMenuOpen.value = false
  } else {
    // Open (or switch) to the requested tab.
    matchMenuTab.value = targetTab
    matchMenuOpen.value = true
  }
  e.preventDefault()
}

// ESC closes the launcher menu (Shop/Upgrades/Vault) when it's open.
// Capture-phase listener so this runs before InputManager's bubble-phase
// ESC=clearSelection handler — we stop propagation so the selection
// doesn't also get wiped underneath the menu dismissal. Defers to the
// settings modal (which has its own capture-phase ESC handler) so the
// two never race when both happen to be open.
function onMatchMenuEscape(e: KeyboardEvent) {
  if (e.code !== 'Escape') return
  if (matchSettingsOpen.value) return
  if (!matchMenuOpen.value) return
  matchMenuOpen.value = false
  e.preventDefault()
  e.stopPropagation()
}

window.addEventListener('beforeunload', markActiveSession)
window.addEventListener('keydown', onMatchMenuHotkey)
window.addEventListener('keydown', onMatchMenuEscape, { capture: true })

onMounted(async () => {
  // Confine the OS cursor to the game window for the duration of the
  // match. No-op outside the Tauri shell. Released in onBeforeUnmount so
  // returning to the menu (or any other route) restores normal cursor
  // movement across monitors.
  void setCursorGrab(true)

  const urlMatchId = route.params.matchId as string | undefined
  // §14R: detect Steam-proxy mode. Joiners arrive here via a host's
  // matchId that lives on the HOST's Go server; their own local server
  // returns 404 for the preflight. Skip the preflight and let the WS
  // connect (via ?proxy=steam) reach the host's hub directly. The hub
  // validates membership and will close the connection if the joiner
  // isn't supposed to be there.
  let isSteamProxyJoiner = false
  try {
    isSteamProxyJoiner = sessionStorage.getItem(STEAM_PROXY_FLAG_KEY) === '1'
  } catch {
    /* sessionStorage may be sandboxed */
  }
  console.log('[Match.onMounted]', { urlMatchId, isSteamProxyJoiner })

  if (urlMatchId) {
    const playerId = localStorage.getItem(PLAYER_ID_STORAGE_KEY) ?? ''
    if (!playerId) {
      console.warn('[Match.onMounted] no playerId in localStorage; kick to /')
      clearStaleSession()
      void router.push('/')
      return
    }

    if (isSteamProxyJoiner) {
      // Skip the local preflight. We don't have the match locally; the
      // host's hub does. NetworkClient.connect will WS-open with
      // ?proxy=steam and the hub's join_match handler will admit us (or
      // reject us cleanly, in which case we get a normal disconnect).
      const mapId = selectedMapId.value || localStorage.getItem(MAP_ID_STORAGE_KEY) || ''
      localStorage.setItem(MATCH_ID_STORAGE_KEY, urlMatchId)
      localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
      if (mapId) {
        localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
        await startGame(mapId, { resume: true })
      } else {
        console.warn('[Match.onMounted] steam-proxy joiner but no mapId; falling back without preferred map')
        // The hub will tell us what map the host is on via the welcome
        // message; pass empty so the server's default catalogue entry
        // applies if NetworkClient happens to need it before welcome.
        await startGame('', { resume: true })
      }
      return
    }

    try {
      const res = await fetch(`${API_BASE}/matches/${encodeURIComponent(urlMatchId)}/status?playerId=${encodeURIComponent(playerId)}`)
      console.log('[Match.onMounted] preflight', { status: res.status, ok: res.ok })
      if (res.status === 404 || !res.ok) {
        console.warn('[Match.onMounted] preflight non-OK; kick to /')
        clearStaleSession()
        void router.push('/')
        return
      }
      const data = await res.json() as { matchId: string; mapId: string; isParticipant: boolean }
      console.log('[Match.onMounted] preflight body', data)
      if (!data.isParticipant) {
        console.warn('[Match.onMounted] not a participant; kick to /')
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
    } catch (e) {
      console.warn('[Match.onMounted] preflight threw; kick to /', e)
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
  void setCursorGrab(false)
  window.removeEventListener('beforeunload', markActiveSession)
  window.removeEventListener('keydown', onMatchMenuHotkey)
  window.removeEventListener('keydown', onMatchMenuEscape, { capture: true })
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

.pause-banner {
  position: absolute;
  top: 24px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 30;
  padding: 14px 28px;
  border-radius: 10px;
  background: linear-gradient(180deg, rgba(15, 23, 42, 0.92), rgba(8, 12, 20, 0.96));
  border: 1px solid rgba(220, 180, 100, 0.45);
  color: #f5ead2;
  text-align: center;
  pointer-events: none;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.55);
}

.pause-banner__title {
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #f7d88e;
}

.pause-banner__sub {
  margin-top: 4px;
  font-size: 12px;
  letter-spacing: 0.04em;
  color: #cbb893;
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
