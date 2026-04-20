import { GameLoop } from './GameLoop'
import { GameState } from './GameState'
import { CanvasRenderer } from '../rendering/CanvasRenderer'
import { InputManager } from '../input/InputManager'
import { Camera } from '../rendering/Camera'
import { NetworkClient } from '../network/NetworkClient'
import type { ConnectionState, MapId, WaveSnapshot } from '../network/protocol'
import type { PlayerSummary, SelectionSummary, Unit, Notification } from './GameState'
import { BUILDING_DEF_MAP, initBuildingDefs } from '../maps/buildingDefs'
import { initObstacleDefs } from '../maps/obstacleDefs'
import { UNIT_DEF_MAP, initUnitDefs } from '../maps/unitDefs'
import { initActionIcons } from '../maps/actionIconDefs'
import { initPerkDefs } from '../maps/perkDefs'
import {
  fetchBuildingDefs,
  fetchObstacleDefs,
  fetchUnitDefs,
  fetchActionIcons,
  fetchPerkDefs,
} from '../maps/catalog'

export type GameUiSnapshot = {
  player: PlayerSummary
  selectedUnits: Unit[]
  selection: SelectionSummary
  notifications: Notification[]
  wave: WaveSnapshot
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

  constructor(canvas: HTMLCanvasElement, mapId: MapId = '') {
    this.canvas = canvas
    this.state = new GameState()
    this.camera = new Camera()
    this.network = new NetworkClient(this.state)
    this.network.setPreferredMapId(mapId)

    this.network.onConnectionStateChange = (s) => {
      this.onConnectionStateChange?.(s)
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
    const [buildingDefs, obstacleDefs, unitDefs, actionIcons, perkDefs] = await Promise.all([
      fetchBuildingDefs(),
      fetchObstacleDefs(),
      fetchUnitDefs(),
      fetchActionIcons(),
      fetchPerkDefs(),
    ])
    initBuildingDefs(buildingDefs)
    initObstacleDefs(obstacleDefs)
    initUnitDefs(unitDefs)
    initActionIcons(actionIcons)
    initPerkDefs(perkDefs)
    await this.network.connect(options)
    this.loop.start()
  }

  async leaveStoredMatch() {
    await this.network.leaveStoredMatch()
  }

  retryReconnect() {
    this.network.retryReconnect()
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
    }
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

    if (selectedBuilding && actionId === 'set-spawn-point') {
      this.state.beginBuildingTargeting('set-spawn-point')
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
