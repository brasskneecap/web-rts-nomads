import { GameLoop } from './GameLoop'
import { GameState } from './GameState'
import { CanvasRenderer } from '../rendering/CanvasRenderer'
import { InputManager } from '../input/InputManager'
import { Camera } from '../rendering/Camera'
import { NetworkClient } from '../network/NetworkClient'
import type { MapId } from '../network/protocol'
import type { PlayerSummary, SelectionSummary, Unit } from './GameState'

export type GameUiSnapshot = {
  player: PlayerSummary
  selectedUnits: Unit[]
  selection: SelectionSummary
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

  constructor(canvas: HTMLCanvasElement, mapId: MapId = '') {
    this.canvas = canvas
    this.state = new GameState()
    this.camera = new Camera()
    this.network = new NetworkClient(this.state)
    this.network.setPreferredMapId(mapId)
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
    await this.network.connect(options)
    this.loop.start()
  }

  async leaveStoredMatch() {
    await this.network.leaveStoredMatch()
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

    if (actionId === 'build') {
      this.state.openWorkerBuildMenu()
      return
    }

    if (actionId === 'close-build-menu') {
      this.state.closeWorkerBuildMenu()
      return
    }

    if (actionId === 'build-barracks') {
      const unitIds = this.state.getOrderedSelectedUnitIds()
      this.state.closeWorkerBuildMenu()
      this.state.beginBuildPlacement('barracks', unitIds)
      return
    }

    if (selectedBuilding && actionId === 'train-worker') {
      this.network.sendTrainWorkerCommand(selectedBuilding.id)
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
        this.network.sendBuildBarracksCommand(placement.builderUnitIds, placement.cursorGridX, placement.cursorGridY)
        this.state.cancelBuildPlacement()
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
