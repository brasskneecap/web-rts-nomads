import type {
  AttackCommandMessage,
  AttackMoveCommandMessage,
  BuildBarracksCommandMessage,
  CancelTrainingCommandMessage,
  ClientMessage,
  GatherCommandMessage,
  LeaveMatchMessage,
  MapId,
  MatchSnapshotMessage,
  MoveCommandMessage,
  SetBuildingSpawnPointCommandMessage,
  ServerMessage,
  TrainSoldierCommandMessage,
  TrainWorkerCommandMessage,
} from './protocol'
import { GameState } from '../core/GameState'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'

function getOrCreatePlayerId(): string {
  const existing = localStorage.getItem(PLAYER_ID_STORAGE_KEY)
  if (existing) return existing

  const created = `player-${Math.random().toString(36).slice(2, 8)}`
  localStorage.setItem(PLAYER_ID_STORAGE_KEY, created)
  return created
}

function getPreferredMapId(): MapId {
  return localStorage.getItem(MAP_ID_STORAGE_KEY) ?? ''
}

function getStoredMatchId(): string | null {
  return localStorage.getItem(MATCH_ID_STORAGE_KEY)
}

export class NetworkClient {
  private socket: WebSocket | null = null
  private state: GameState
  private playerId = getOrCreatePlayerId()
  private matchId: string | null = getStoredMatchId()
  private mapId: MapId = getPreferredMapId()

  constructor(state: GameState) {
    this.state = state
    this.state.setLocalPlayerId(this.playerId)
  }

  setPreferredMapId(mapId: MapId) {
    this.mapId = mapId
    localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  }

  connect({ resume = true }: { resume?: boolean } = {}) {
    return new Promise<void>((resolve, reject) => {
      this.socket = new WebSocket('ws://localhost:8080/ws')

      this.socket.onopen = () => {
        const joinMessage: ClientMessage = {
          type: 'join_match',
          playerId: this.playerId,
          mapId: this.mapId,
          matchId: resume ? (this.matchId ?? undefined) : undefined,
        }

        this.send(joinMessage)
        resolve()
      }

      this.socket.onerror = (err) => {
        reject(err)
      }

      this.socket.onmessage = (event) => {
        const message = JSON.parse(event.data) as ServerMessage
        this.handleMessage(message)
      }

      this.socket.onclose = () => {
        console.log('socket closed')
      }
    })
  }

  disconnect() {
    this.socket?.close()
    this.socket = null
  }

  send(message: ClientMessage) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return
    this.socket.send(JSON.stringify(message))
  }

  async leaveStoredMatch() {
    const matchId = this.matchId ?? getStoredMatchId()
    if (!matchId) return

    const socket = new WebSocket('ws://localhost:8080/ws')

    await new Promise<void>((resolve, reject) => {
      socket.onopen = () => {
        const message: LeaveMatchMessage = {
          type: 'leave_match',
          playerId: this.playerId,
          matchId,
        }
        socket.send(JSON.stringify(message))
        resolve()
      }

      socket.onerror = (err) => {
        reject(err)
      }
    })

    socket.close()
    this.matchId = null
    localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  }

  sendMoveCommand(unitIds: number[], x: number, y: number) {
    const message: MoveCommandMessage = {
      type: 'move_command',
      unitIds,
      destination: { x, y },
    }

    this.send(message)
  }

  sendGatherCommand(unitIds: number[], buildingId: string) {
    const message: GatherCommandMessage = {
      type: 'gather_command',
      unitIds,
      buildingId,
    }

    this.send(message)
  }

  sendTrainWorkerCommand(buildingId: string) {
    const message: TrainWorkerCommandMessage = {
      type: 'train_worker_command',
      buildingId,
    }

    this.send(message)
  }

  sendCancelTrainingCommand(buildingId: string) {
    const message: CancelTrainingCommandMessage = {
      type: 'cancel_training_command',
      buildingId,
    }

    this.send(message)
  }

  sendBuildBarracksCommand(unitIds: number[], gridX: number, gridY: number) {
    const message: BuildBarracksCommandMessage = {
      type: 'build_barracks_command',
      unitIds,
      gridX,
      gridY,
    }
    this.send(message)
  }

  sendTrainSoldierCommand(buildingId: string) {
    const message: TrainSoldierCommandMessage = {
      type: 'train_soldier_command',
      buildingId,
    }

    this.send(message)
  }

  sendAttackCommand(unitIds: number[], targetUnitId: number) {
    const message: AttackCommandMessage = {
      type: 'attack_command',
      unitIds,
      targetUnitId,
    }

    this.send(message)
  }

  sendAttackMoveCommand(unitIds: number[], x: number, y: number) {
    const message: AttackMoveCommandMessage = {
      type: 'attack_move_command',
      unitIds,
      destination: { x, y },
    }

    this.send(message)
  }

  sendRepairCommand(unitIds: number[], buildingId: string) {
    this.send({ type: 'repair_command', unitIds, buildingId })
  }

  sendSetBuildingSpawnPointCommand(buildingId: string, x: number, y: number) {
    const message: SetBuildingSpawnPointCommandMessage = {
      type: 'set_building_spawn_point_command',
      buildingId,
      point: { x, y },
    }

    this.send(message)
  }

  private handleMessage(message: ServerMessage) {
    switch (message.type) {
      case 'welcome':
        this.matchId = message.matchId
        this.state.setLocalPlayerId(message.playerId)
        this.state.setMapConfig(message.map)
        localStorage.setItem(PLAYER_ID_STORAGE_KEY, message.playerId)
        localStorage.setItem(MAP_ID_STORAGE_KEY, message.map.id)
        localStorage.setItem(MATCH_ID_STORAGE_KEY, message.matchId)
        console.log('connected as', message.playerId, 'in', message.matchId)
        break

      case 'match_snapshot':
        this.matchId = message.matchId
        localStorage.setItem(MATCH_ID_STORAGE_KEY, message.matchId)
        this.applySnapshot(message)
        break

      case 'ping':
      this.send({ type: 'pong' })
      break
      
      case 'error':
        console.error('server error:', message.message)
        break
    }
  }

  private applySnapshot(message: MatchSnapshotMessage) {
    this.state.applySnapshot(message)
  }
}
