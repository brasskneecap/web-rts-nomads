import { onBeforeUnmount, ref } from 'vue'
import { GameClient, type GameUiSnapshot } from '@/game/core/GameClient'
import type { DebugSpawnConfig } from '@/game/core/GameState'
import type { ConnectionState } from '@/game/network/protocol'
import { useProfile } from '@/composables/useProfile'
import { stopBuildingSelectSound } from '@/composables/useSfx'

let client: GameClient | null = null

const emptyUiSnapshot: GameUiSnapshot = {
  player: {
    playerId: null,
    color: null,
    totalUnits: 0,
    selectedUnits: 0,
    totalHp: 0,
    resources: [],
  },
  selectedUnits: [],
  selection: {
    kind: 'none',
    title: 'No Selection',
    subtitle: 'Select a unit or building to inspect details and actions.',
    details: [],
    actions: [],
  },
  notifications: [],
  wave: {
    enabled: false,
    currentWave: 0,
    totalWaves: 0,
    state: '',
    timer: 0,
    waveDuration: 0,
  },
  battleTracker: null,
  debugBattleTracker: false,
  debugSpawn: false,
  debugSpawnTargetingActive: false,
  mapName: '',
  mapId: '',
  isDefeated: false,
  isVictory: false,
  objectives: [],
  zoneCaptureCards: [],
  zoneInspection: null,
  players: [],
  frozenEndPlayers: null,
  matchDominionPointsEarned: 0,
  upgrades: [],
  townHallTier: 0,
  selectedBuildingType: null,
  vault: [],
  vaultSelectedInstanceId: null,
  allPlayerUnits: [],
  waveUpgrade: null,
  commanderAbilities: [],
  commanderTargetingAbilityId: null,
  itemTargeting: null,
  shopCatalog: [],
  shopRerollsRemaining: 0,
  craftCatalog: [],
  hasArtificer: false,
  paused: false,
  pausedBy: '',
  pausedSinceMs: 0,
  hoveredLootDrop: null,
  cursorScreenX: 0,
  cursorScreenY: 0,
  cursorClientX: 0,
  cursorClientY: 0,
  netStats: {
    snapshotAgeMs: 0,
    snapshotAgeAvgMs: 0,
    snapshotAgeMaxMs: 0,
    receiveGapMs: 0,
    receiveGapMaxMs: 0,
    snapshotsPerSec: 0,
    bufferDepth: 0,
    lastSnapshotBytes: 0,
    totalSnapshots: 0,
    transportLabel: 'direct',
  },
}

export function useGameClient() {
  const isRunning = ref(false)
  const ui = ref<GameUiSnapshot>(emptyUiSnapshot)
  const connectionState = ref<ConnectionState>('idle')
  const currentMatchId = ref('')
  // Attempt counters are polled from the client each time the state changes so
  // the overlay can display "attempt N of M" without needing a separate channel.
  const reconnectAttempt = ref(0)
  const maxReconnectAttempts = ref(0)

  let rafId = 0

  function syncUi() {
    const prevBuildingType = ui.value.selectedBuildingType
    ui.value = client?.getUiSnapshot() ?? emptyUiSnapshot

    // Stop the building-select sound the moment a building is deselected —
    // clicked away, destroyed, or replaced by a unit/obstacle selection. Every
    // deselect path clears selectedBuildingType, so this single check covers
    // them all. Selecting a different building re-fires the sound from the
    // input layer (which also stops the prior one).
    if (prevBuildingType && !ui.value.selectedBuildingType) {
      stopBuildingSelectSound()
    }

    if (client) {
      rafId = requestAnimationFrame(syncUi)
    }
  }

  function stopUiSync() {
    if (rafId) {
      cancelAnimationFrame(rafId)
      rafId = 0
    }
  }

  async function init(
    canvas: HTMLCanvasElement,
    mapId = '',
    options: { resume?: boolean; ephemeral?: boolean } = {},
  ) {
    client?.stop()
    stopUiSync()

    const { refresh: refreshProfile, profile } = useProfile()
    // One fetch, always fresh. refresh() re-fetches /profile (and populates the
    // singleton profile ref) regardless of prior init, so on a first-ever start
    // this replaces the old initProfile()+refreshProfile() pair that fired two
    // identical /profile requests back-to-back. Fresh state is required because
    // mid-session toggles/purchases land in the profile ref before a match.
    await refreshProfile()

    client = new GameClient(canvas, mapId)
    // null signals "use server-side default" (all owned upgrades active per schema v3).
    client.setActiveUpgradeIds(profile.value?.activeUpgradeIds ?? null)
    client.setOwnedUpgradeRanks(profile.value?.ownedUpgradeRanks ?? {})
    // Send acquired advancement IDs so the server applies them at match start.
    // Falls back to [] when the profile hasn't loaded — server treats empty as "none".
    client.setAcquiredAdvancementIds(
      (profile.value?.acquiredAdvancements ?? []).map((a) => a.id),
    )
    client.setKnownCraftableIds(profile.value?.knownCraftableIds ?? [])

    // Wire the connection state callback. This runs outside the RAF loop so
    // connection state changes are never masked by the snapshot polling rhythm.
    client.onConnectionStateChange = (state) => {
      connectionState.value = state
      reconnectAttempt.value = client?.reconnectAttempt ?? 0
      maxReconnectAttempts.value = client?.maxReconnectAttempts ?? 0
    }

    client.onMatchIdChange = (id) => {
      currentMatchId.value = id
    }

    await client.start(options)
    syncUi()
    isRunning.value = true
  }

  async function leaveStoredMatch() {
    if (!client) {
      const tempCanvas = document.createElement('canvas')
      client = new GameClient(tempCanvas)
    }
    await client.leaveStoredMatch()
    currentMatchId.value = ''
  }

  function destroy() {
    stopUiSync()
    stopBuildingSelectSound()
    client?.stop()
    client = null
    ui.value = emptyUiSnapshot
    isRunning.value = false
    connectionState.value = 'idle'
    currentMatchId.value = ''
  }

  function performSelectionAction(actionId: string) {
    client?.performSelectionAction(actionId)
  }

  function retryReconnect() {
    client?.retryReconnect()
  }

  // Forwarders for the Debug Spawn panel. Wrapped so the panel doesn't need
  // a handle to `client` — composable keeps the encapsulation clean.
  function beginDebugSpawn(config: DebugSpawnConfig) {
    client?.beginDebugSpawn(config)
  }

  function cancelDebugSpawn() {
    client?.cancelDebugSpawn()
  }

  function selectUnitOnly(unitId: number) {
    client?.selectUnitOnly(unitId)
  }

  function focusUnit(unitId: number, menuRightPx?: number) {
    client?.focusUnit(unitId, menuRightPx)
  }

  function deselectUnit(unitId: number) {
    client?.deselectUnit(unitId)
  }

  function setMinimapPanelRect(rect: DOMRect | null) {
    client?.setMinimapPanelRect(rect)
  }

  function purchaseUpgrade(track: string) {
    // No building id from the global panel — server auto-assigns to an idle
    // blacksmith.
    client?.purchaseUpgrade(track)
  }

  function cancelUpgrade(buildingId: string) {
    client?.cancelUpgrade(buildingId)
  }

  function upgradeBuilding(buildingId: string) {
    client?.upgradeBuilding(buildingId)
  }

  function sendPurchaseItem(buildingId: string, itemId: string) {
    client?.sendPurchaseItem(buildingId, itemId)
  }

  function sendPurchaseRecipe(buildingId: string, recipeId: string) {
    client?.sendPurchaseRecipe(buildingId, recipeId)
  }

  function rerollShop(buildingId: string) {
    client?.sendRerollShop(buildingId)
  }

  function craftItem(recipeId: string) {
    client?.sendCraftItem(recipeId)
  }

  function sendEquipItem(unitId: number, slotIndex: number, instanceId: number) {
    client?.sendEquipItem(unitId, slotIndex, instanceId)
  }

  function sendUnequipItem(unitId: number, slotIndex: number) {
    client?.sendUnequipItem(unitId, slotIndex)
  }

  function sendUseConsumable(unitId: number, slotIndex: number) {
    client?.sendUseConsumable(unitId, slotIndex)
  }

  function sendTransferItem(fromUnitId: number, fromSlotIdx: number, toUnitId: number, toSlotIdx: number) {
    client?.sendTransferItem(fromUnitId, fromSlotIdx, toUnitId, toSlotIdx)
  }

  function sendUseItemOnUnit(unitId: number, instanceId: number) {
    client?.sendUseItemOnUnit(unitId, instanceId)
  }

  function setVaultSelectedInstanceId(instanceId: number | null) {
    client?.setVaultSelectedInstanceId(instanceId)
  }

  function sendWaveUpgradeChoice(upgradeID: string, targetUnitID?: number) {
    client?.sendWaveUpgradeChoice(upgradeID, targetUnitID)
  }

  function sendWaveUpgradeReroll() {
    client?.sendWaveUpgradeReroll()
  }

  function sendSetPause(paused: boolean) {
    client?.sendSetPause(paused)
  }

  function beginCommanderAbility(abilityId: string) {
    client?.beginCommanderAbility(abilityId)
  }

  function cancelCommanderAbility() {
    client?.cancelCommanderAbility()
  }

  function beginItemUse(instanceId: number, itemId: string) {
    client?.beginItemUse(instanceId, itemId)
  }

  function cancelItemUse() {
    client?.cancelItemUse()
  }

  onBeforeUnmount(() => {
    destroy()
  })

  return {
    init,
    destroy,
    isRunning,
    leaveStoredMatch,
    performSelectionAction,
    retryReconnect,
    beginDebugSpawn,
    cancelDebugSpawn,
    selectUnitOnly,
    focusUnit,
    deselectUnit,
    setMinimapPanelRect,
    purchaseUpgrade,
    cancelUpgrade,
    upgradeBuilding,
    sendPurchaseItem,
    sendPurchaseRecipe,
    rerollShop,
    craftItem,
    sendEquipItem,
    sendUnequipItem,
    sendUseConsumable,
    sendTransferItem,
    sendUseItemOnUnit,
    setVaultSelectedInstanceId,
    sendWaveUpgradeChoice,
    sendWaveUpgradeReroll,
    sendSetPause,
    beginCommanderAbility,
    cancelCommanderAbility,
    beginItemUse,
    cancelItemUse,
    ui,
    connectionState,
    currentMatchId,
    reconnectAttempt,
    maxReconnectAttempts,
  }
}
