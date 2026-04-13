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
    this.input = new InputManager(canvas, this.state, this.camera, this.network)

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
