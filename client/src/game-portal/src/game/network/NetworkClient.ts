import type {
  ClientMessage,
  LeaveMatchMessage,
  MapSize,
  MatchSnapshotMessage,
  MoveCommandMessage,
  ServerMessage,
} from './protocol'
import { GameState } from '../core/GameState'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_SIZE_STORAGE_KEY = 'webrts.mapSize'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'

function getOrCreatePlayerId(): string {
  const existing = localStorage.getItem(PLAYER_ID_STORAGE_KEY)
  if (existing) return existing

  const created = `player-${Math.random().toString(36).slice(2, 8)}`
  localStorage.setItem(PLAYER_ID_STORAGE_KEY, created)
  return created
}

function getPreferredMapSize(): MapSize {
  const stored = localStorage.getItem(MAP_SIZE_STORAGE_KEY)
  if (stored === 'small' || stored === 'medium' || stored === 'large') {
    return stored
  }
  return 'large'
}

function getStoredMatchId(): string | null {
  return localStorage.getItem(MATCH_ID_STORAGE_KEY)
}

export class NetworkClient {
  private socket: WebSocket | null = null
  private state: GameState
  private playerId = getOrCreatePlayerId()
  private matchId: string | null = getStoredMatchId()
  private mapSize: MapSize = getPreferredMapSize()

  constructor(state: GameState) {
    this.state = state
    this.state.setLocalPlayerId(this.playerId)
  }

  setPreferredMapSize(size: MapSize) {
    this.mapSize = size
    localStorage.setItem(MAP_SIZE_STORAGE_KEY, size)
  }

  connect({ resume = true }: { resume?: boolean } = {}) {
    return new Promise<void>((resolve, reject) => {
      this.socket = new WebSocket('ws://localhost:8080/ws')

      this.socket.onopen = () => {
        const joinMessage: ClientMessage = {
          type: 'join_match',
          playerId: this.playerId,
          mapSize: this.mapSize,
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

  private handleMessage(message: ServerMessage) {
    switch (message.type) {
      case 'welcome':
        this.matchId = message.matchId
        this.state.setLocalPlayerId(message.playerId)
        this.state.setMapConfig(message.map)
        localStorage.setItem(PLAYER_ID_STORAGE_KEY, message.playerId)
        localStorage.setItem(MAP_SIZE_STORAGE_KEY, message.map.size)
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