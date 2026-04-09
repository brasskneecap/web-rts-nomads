import { GameLoop } from './GameLoop'
import { GameState } from './GameState'
import { CanvasRenderer } from '../rendering/CanvasRenderer'
import { InputManager } from '../input/InputManager'
import { Camera } from '../rendering/Camera'
import { NetworkClient } from '../network/NetworkClient'
import type { MapSize } from '../network/protocol'

export class GameClient {
  private state: GameState
  private renderer: CanvasRenderer
  private input: InputManager
  private loop: GameLoop
  private camera: Camera
  private network: NetworkClient

  constructor(canvas: HTMLCanvasElement, mapSize: MapSize = 'large') {
    this.state = new GameState()
    this.camera = new Camera()
    this.network = new NetworkClient(this.state)
    this.network.setPreferredMapSize(mapSize)
    this.renderer = new CanvasRenderer(canvas, this.state, this.camera)
    this.input = new InputManager(canvas, this.state, this.camera, this.network)

    this.loop = new GameLoop({
      update: (dt) => this.state.update(dt),
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
    this.network.disconnect()
  }
}