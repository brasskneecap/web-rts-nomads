import { GameLoop } from './GameLoop'
import { GameState } from './GameState'
import { CanvasRenderer } from '../rendering/CanvasRenderer'
import { InputManager } from '../input/InputManager'
import { Camera } from '../rendering/Camera'
import { NetworkClient } from '../network/NetworkClient'
import type {
  BattleTrackerSnapshot,
  ConnectionState,
  MapId,
  PlayerUpgradeSnapshot,
  VaultItemSnapshot,
  WaveSnapshot,
} from '../network/protocol'
import type { DebugSpawnConfig, PlayerSummary, SelectionSummary, Unit, Notification } from './GameState'
import { BUILDING_DEF_MAP, initBuildingDefs } from '../maps/buildingDefs'
import { initObstacleDefs } from '../maps/obstacleDefs'
import { UNIT_DEF_MAP, initUnitDefs } from '../maps/unitDefs'
import { initActionIcons } from '../maps/actionIconDefs'
import { initPerkDefs } from '../maps/perkDefs'
import { initItemDefs } from '../maps/itemDefs'
import {
  fetchBuildingDefs,
  fetchObstacleDefs,
  fetchUnitDefs,
  fetchActionIcons,
  fetchPerkDefs,
  fetchItemDefs,
} from '../maps/catalog'

export type GameUiSnapshot = {
  player: PlayerSummary
  selectedUnits: Unit[]
  selection: SelectionSummary
  notifications: Notification[]
  wave: WaveSnapshot
  // Battle tracker (debug). Null when the active map does not opt in via
  // debug.battleTracker. HUD consumers render the panel only when non-null.
  battleTracker: BattleTrackerSnapshot | null
  // Individual debug opt-ins surfaced to the HUD so each debug panel can
  // show/hide itself from config without touching this file. False on any
  // non-debug map.
  debugBattleTracker: boolean
  debugSpawn: boolean
  // True iff the client is currently armed to spawn a unit on the next
  // world click (via DebugSpawnPanel's "Place on Map").
  debugSpawnTargetingActive: boolean
  mapName: string
  mapId: string
  // True when the local player has lost all their townhalls.
  isDefeated: boolean
  // True when all victory objectives have been completed.
  isVictory: boolean
  objectives: import('../network/protocol').ObjectiveSnapshot[]
  // Permanent per-player upgrades. Empty array until the server sends upgrade data.
  upgrades: PlayerUpgradeSnapshot[]
  // Current town hall tier for the local player (1/2/3). 0 until first snapshot.
  townHallTier: number
  // buildingType of the currently selected building, or null when nothing (or
  // a non-building entity) is selected. Used to gate overlay panels such as
  // UpgradeCenterPanel.
  selectedBuildingType: string | null
  // Vault contents for the local player.
  vault: VaultItemSnapshot[]
  vaultCapacity: number
  vaultPanelOpen: boolean
  vaultSelectedInstanceId: number | null
  // All local-player units (not just selected ones). Needed by VaultPanel to
  // show all units that can receive equipped items.
  allPlayerUnits: Unit[]
}

export class GameClient {
  private state: GameState
  private renderer: CanvasRenderer
  private input: InputManager
  private loop: GameLoop
  private camera: Camera
  private network: NetworkClient
  private canvas: HTMLCanvasElement
  private hasCenteredCameraOnSpawn = false

  /** Wired by useGameClient to propagate connection state into Vue refs. */
  onConnectionStateChange: ((state: ConnectionState) => void) | null = null

  /** Wired by useGameClient to propagate the current matchId into a Vue ref. */
  onMatchIdChange: ((id: string) => void) | null = null

  constructor(canvas: HTMLCanvasElement, mapId: MapId = '') {
    this.canvas = canvas
    this.state = new GameState()
    this.camera = new Camera()
    this.network = new NetworkClient(this.state)
    this.network.setPreferredMapId(mapId)

    this.network.onConnectionStateChange = (s) => {
      this.onConnectionStateChange?.(s)
    }

    this.network.onMatchIdChange = (id) => {
      this.onMatchIdChange?.(id)
    }

    this.network.onReconnectSuccess = () => {
      // Buffer already cleared inside NetworkClient.handleMessage for welcome.
      // Nothing extra needed here right now, but the hook is available.
    }

    this.renderer = new CanvasRenderer(canvas, this.state, this.camera)
    this.input = new InputManager(canvas, this.state, this, this.camera, this.network)

    this.loop = new GameLoop({
      update: (dt) => {
        this.state.update(dt)
        this.centerCameraOnSpawnIfNeeded()
      },
      render: () => this.renderer.render(),
    })
  }

  async start(options: { resume?: boolean } = {}) {
    const [buildingDefs, obstacleDefs, unitDefs, actionIcons, perkDefs, itemDefs] = await Promise.all([
      fetchBuildingDefs(),
      fetchObstacleDefs(),
      fetchUnitDefs(),
      fetchActionIcons(),
      fetchPerkDefs(),
      fetchItemDefs().catch(() => []),
    ])
    initBuildingDefs(buildingDefs)
    initObstacleDefs(obstacleDefs)
    initUnitDefs(unitDefs)
    initActionIcons(actionIcons)
    initPerkDefs(perkDefs)
    initItemDefs(itemDefs)
    await this.network.connect(options)
    this.loop.start()
  }

  async leaveStoredMatch() {
    await this.network.leaveStoredMatch()
  }

  retryReconnect() {
    this.network.retryReconnect()
  }

  /** Anchors the canvas-rendered minimap (and minimap input handlers) to the
   *  given viewport-space DOMRect. Pass null to fall back to the default
   *  top-right corner placement. The rect is converted into canvas-pixel
   *  space here so callers (HUD components) can pass raw DOMRects.
   *
   *  The rect is inset by MINIMAP_FRAME_INSET on each side so the minimap
   *  draws inside the visible interior of the panel rather than being clipped
   *  by the 9-slice frame border that overlays it. */
  setMinimapPanelRect(rect: DOMRect | null) {
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      this.state.minimapPanelRect = null
      return
    }
    const inset = 17
    const canvasRect = this.canvas.getBoundingClientRect()
    this.state.minimapPanelRect = {
      x: rect.left - canvasRect.left + inset,
      y: rect.top - canvasRect.top + inset,
      width: Math.max(0, rect.width - inset * 2),
      height: Math.max(0, rect.height - inset * 2),
    }
  }

  get reconnectAttempt(): number {
    return this.network.currentReconnectAttempt
  }

  get maxReconnectAttempts(): number {
    return this.network.maxReconnectAttempts
  }

  stop() {
    this.loop.stop()
    this.input.destroy()
    this.renderer.destroy()
    this.network.disconnect()
  }

  getUiSnapshot(): GameUiSnapshot {
    return {
      player: this.state.getPlayerSummary(),
      selectedUnits: this.state.getSelectedUnits(),
      selection: this.state.getSelectionSummary(),
      notifications: [...this.state.notifications],
      wave: this.state.getWaveSnapshot(),
      battleTracker: this.state.battleTracker,
      debugBattleTracker: this.state.mapConfig.debug?.battleTracker === true,
      debugSpawn: this.state.mapConfig.debug?.debugSpawn === true,
      debugSpawnTargetingActive: this.state.isBuildingTargetingActive('debug-spawn-unit'),
      mapName: this.state.mapConfig.name,
      mapId: this.state.mapConfig.id,
      isDefeated: this.state.isLocalPlayerDefeated(),
      isVictory: this.state.isVictoryAchieved(),
      objectives: this.state.getObjectives(),
      upgrades: this.state.playerUpgrades,
      townHallTier: this.state.townHallTier,
      selectedBuildingType: this.state.getSelectedBuildingType(),
      vault: this.state.localPlayerVault,
      vaultCapacity: this.state.localPlayerVaultCapacity,
      vaultPanelOpen: this.state.vaultPanelOpen,
      vaultSelectedInstanceId: this.state.vaultSelectedInstanceId,
      allPlayerUnits: this.state.getLocalPlayerUnits(),
    }
  }

  purchaseUpgrade(track: string): void {
    this.network.sendPurchaseUpgrade(track)
  }

  upgradeTownHall(buildingId: string): void {
    this.network.sendUpgradeTownHall(buildingId)
  }

  sendPurchaseItem(buildingId: string, itemId: string): void {
    this.network.send({ type: 'purchase_item', buildingId, itemId })
  }

  sendEquipItem(unitId: number, slotIndex: number, instanceId: number): void {
    this.network.send({ type: 'equip_item', unitId, slotIndex, instanceId })
  }

  sendUnequipItem(unitId: number, slotIndex: number): void {
    this.network.send({ type: 'unequip_item', unitId, slotIndex })
  }

  sendUseConsumable(unitId: number, slotIndex: number): void {
    this.network.send({ type: 'use_consumable', unitId, slotIndex })
  }

  sendTransferItem(fromUnitId: number, fromSlotIdx: number, toUnitId: number, toSlotIdx: number): void {
    this.network.send({ type: 'transfer_item', fromUnitId, fromSlotIdx, toUnitId, toSlotIdx })
  }

  setVaultSelectedInstanceId(instanceId: number | null): void {
    this.state.vaultSelectedInstanceId = instanceId
  }

  // Arms the 'debug-spawn-unit' targeting mode. Exposed for the Debug Spawn
  // panel so it can just call this rather than reaching into GameState.
  beginDebugSpawn(config: DebugSpawnConfig) {
    this.state.beginDebugSpawnTargeting(config)
    this.input.refreshCursor()
  }

  cancelDebugSpawn() {
    this.state.cancelBuildingTargeting()
    this.input.refreshCursor()
  }

  selectUnitOnly(unitId: number) {
    this.state.selectUnit(unitId)
  }

  deselectUnit(unitId: number) {
    this.state.removeUnitFromSelection(unitId)
  }

  /** Bind the current selection to control group N (1..10) — Ctrl+N. */
  assignControlGroup(groupKey: number) {
    this.state.assignControlGroup(groupKey)
  }

  /** Recall control group N (1..10), replacing the current selection — N. */
  selectControlGroup(groupKey: number) {
    this.state.selectControlGroup(groupKey)
  }

  performSelectionAction(actionId: string) {
    const selectedBuilding = this.state.getSelectedBuilding()

    if (actionId === 'move') {
      this.state.beginUnitTargeting('move')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'gather') {
      this.state.beginUnitTargeting('gather')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'repair') {
      this.state.beginUnitTargeting('repair')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'attack') {
      this.state.beginUnitTargeting('attack')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'hold') {
      const unitIds = this.state.getOrderedSelectedUnitIds()
      if (unitIds.length > 0) {
        this.network.sendStanceCommand(unitIds, 'hold')
      }
      return
    }

    if (actionId === 'patrol') {
      this.state.beginUnitTargeting('patrol')
      this.input.refreshCursor()
      return
    }

    if (actionId === 'build') {
      this.state.openWorkerBuildMenu()
      return
    }

    if (actionId === 'close-build-menu') {
      this.state.closeWorkerBuildMenu()
      return
    }

    if (actionId.startsWith('build-') && BUILDING_DEF_MAP.has(actionId.slice(6))) {
      const unitIds = this.state.getOrderedSelectedUnitIds()
      this.state.closeWorkerBuildMenu()
      this.state.beginBuildPlacement(actionId.slice(6), unitIds)
      return
    }

    if (selectedBuilding && actionId.startsWith('train-') && UNIT_DEF_MAP.has(actionId.slice(6))) {
      this.network.sendTrainUnitCommand(selectedBuilding.id, actionId.slice(6))
      return
    }

    if (selectedBuilding && actionId === 'cancel-training') {
      this.network.sendCancelTrainingCommand(selectedBuilding.id)
      return
    }

    // Queue-slot cancel — emitted by SelectionHud when a queued unit is
    // left-clicked. Action id format: "cancel-queue-<index>" where index
    // is the queue position (1..7, since 0 is the leading unit handled by
    // the "X" cancel button above).
    if (selectedBuilding && actionId.startsWith('cancel-queue-')) {
      const index = Number(actionId.slice('cancel-queue-'.length))
      if (Number.isInteger(index) && index > 0) {
        this.network.sendCancelTrainingCommand(selectedBuilding.id, index)
      }
      return
    }

    if (selectedBuilding && actionId === 'set-spawn-point') {
      this.state.beginBuildingTargeting('set-spawn-point')
      return
    }

    if (selectedBuilding && actionId === 'upgrade-townhall') {
      this.network.sendUpgradeTownHall(selectedBuilding.id)
      return
    }

    if (selectedBuilding && actionId.startsWith('upgrade-')) {
      const track = actionId.slice('upgrade-'.length)
      if (track && track !== 'townhall') {
        this.network.sendPurchaseUpgrade(track)
        return
      }
    }

    if (actionId.startsWith('buy-item-')) {
      const itemId = actionId.slice('buy-item-'.length)
      if (selectedBuilding) {
        this.sendPurchaseItem(selectedBuilding.id, itemId)
      }
      return
    }

    if (actionId === 'open-vault') {
      this.state.vaultPanelOpen = !this.state.vaultPanelOpen
      return
    }
  }

  tryHandleWorldClick(x: number, y: number) {
    if (this.state.isBuildPlacementActive()) {
      this.state.updateBuildPlacement(x, y)
      const placement = this.state.buildPlacement
      if (placement?.valid) {
        this.network.sendBuildBuildingCommand(placement.builderUnitIds, placement.buildingType, placement.cursorGridX, placement.cursorGridY)
        this.state.cancelBuildPlacement()
      } else {
        this.state.addNotification('Cannot place building here')
      }
      return true
    }

    // Debug spawn: fire a debug_spawn_unit command with the pending loadout.
    // Mode stays active so the user can drop multiple copies in a row; right-
    // click cancels via the existing cancelTargeting() path.
    if (this.state.isBuildingTargetingActive('debug-spawn-unit') && this.state.debugSpawnConfig) {
      const cfg = this.state.debugSpawnConfig
      this.network.sendDebugSpawnUnitCommand({
        unitType: cfg.unitType,
        team: cfg.team,
        path: cfg.path,
        rank: cfg.rank,
        perkIds: cfg.perkIds,
        customHp: cfg.customHp,
        x,
        y,
      })
      return true
    }

    const selectedBuilding = this.state.getSelectedBuilding()
    if (!selectedBuilding || !this.state.isBuildingTargetingActive('set-spawn-point')) {
      const unitIds = this.state.getOrderedSelectedUnitIds()

      if (this.state.isUnitTargetingActive('move') && unitIds.length > 0) {
        this.state.addFormationMoveMarkers(x, y)
        this.network.sendMoveCommand(unitIds, x, y)
        this.state.cancelUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('gather') && unitIds.length > 0) {
        const clickedBuilding = this.state.getBuildingAtPosition(x, y, 16)

        if (
          clickedBuilding &&
          clickedBuilding.capabilities.includes('resource-source') &&
          this.state.selectedUnitsCanGather()
        ) {
          const cellSize = this.state.mapConfig.cellSize
          const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
          const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
          this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
          this.network.sendGatherCommand(unitIds, clickedBuilding.id)
        } else {
          const clickedObstacle = this.state.getGatherableObstacleAtPosition(x, y, 16)
          if (clickedObstacle && clickedObstacle.id && this.state.selectedUnitsCanGather()) {
            const cellSize = this.state.mapConfig.cellSize
            const obstacleCenterX = (clickedObstacle.x + (clickedObstacle.width ?? 1) / 2) * cellSize
            const obstacleCenterY = (clickedObstacle.y + (clickedObstacle.height ?? 1) / 2) * cellSize
            this.state.addMoveMarker(obstacleCenterX, obstacleCenterY, 700)
            this.network.sendGatherCommand(unitIds, clickedObstacle.id)
          }
        }

        this.state.cancelUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('attack') && unitIds.length > 0) {
        const clickedUnit = this.state.getEnemyUnitAtPosition(x, y)

        if (clickedUnit) {
          this.network.sendAttackCommand(unitIds, clickedUnit.id)
        } else {
          this.state.addFormationMoveMarkers(x, y)
          this.network.sendAttackMoveCommand(unitIds, x, y)
        }

        this.state.cancelUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('patrol') && unitIds.length > 0) {
        this.state.addFormationMoveMarkers(x, y)
        this.network.sendPatrolCommand(unitIds, x, y)
        this.state.cancelUnitTargeting()
        return true
      }

      if (this.state.isUnitTargetingActive('repair') && unitIds.length > 0) {
        const clickedBuilding = this.state.getBuildingAtPosition(x, y, 16)

        if (
          clickedBuilding &&
          clickedBuilding.ownerId === this.state.localPlayerId &&
          clickedBuilding.metadata?.['underConstruction'] === true
        ) {
          const cellSize = this.state.mapConfig.cellSize
          const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
          const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
          this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
          this.network.sendRepairCommand(unitIds, clickedBuilding.id)
        }

        this.state.cancelUnitTargeting()
        return true
      }

      return false
    }

    const spawnPoint = this.state.getTargetedBuildingSpawnPoint(x, y)
    if (!spawnPoint) return false

    this.network.sendSetBuildingSpawnPointCommand(selectedBuilding.id, spawnPoint.x, spawnPoint.y)
    this.state.addMoveMarker(spawnPoint.x, spawnPoint.y, 800)
    this.state.cancelBuildingTargeting()
    return true
  }

  cancelTargeting() {
    this.state.cancelBuildingTargeting()
    this.state.cancelUnitTargeting()
    this.state.cancelBuildPlacement()
  }

  private centerCameraOnSpawnIfNeeded() {
    if (this.hasCenteredCameraOnSpawn) return

    const spawnCenter = this.state.getLocalPlayerSpawnCenter()
    if (!spawnCenter) return

    this.camera.centerOn(
      spawnCenter.x,
      spawnCenter.y,
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )

    this.hasCenteredCameraOnSpawn = true
  }
}
