import type {
  AttackCommandMessage,
  AttackMoveCommandMessage,
  BuildBuildingCommandMessage,
  CancelTrainingCommandMessage,
  ClientMessage,
  ConnectionState,
  GatherCommandMessage,
  LeaveMatchMessage,
  MapId,
  MatchSnapshotMessage,
  MoveCommandMessage,
  PatrolCommandMessage,
  SetBuildingSpawnPointCommandMessage,
  SetStanceCommandMessage,
  ServerMessage,
  TrainUnitCommandMessage,
} from './protocol'
import { GameState } from '../core/GameState'

/** Derive the WebSocket base URL from the HTTP base URL env var.
 *  http -> ws, https -> wss so both schemes work in prod and dev.
 *  When the env var is empty, fall back to the page origin so the same
 *  build works behind a tunnel/proxy without a hardcoded host. */
function getWsBaseUrl(): string {
  const http = import.meta.env.VITE_API_BASE_URL || window.location.origin
  return http.replace(/^http/, 'ws')
}

const WS_URL = `${getWsBaseUrl()}/ws`

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'

// Backoff schedule: 1s, 2s, 4s, 8s — total ~15s across 4 retries, well within the 30s server grace.
// After 4 attempts without success the state moves to 'failed'.
const BACKOFF_DELAYS_MS = [1000, 2000, 4000, 8000]
const MAX_RECONNECT_ATTEMPTS = BACKOFF_DELAYS_MS.length

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

  /** Set to false before calling close() for an intentional disconnect so the
   *  reconnect loop does not fire. */
  private shouldReconnect = true

  private reconnectAttempt = 0
  private reconnectTimerId: ReturnType<typeof setTimeout> | null = null

  /** Called whenever the connection state changes. GameClient wires this up. */
  onConnectionStateChange: ((state: ConnectionState) => void) | null = null

  /** Callback that lets GameClient clear the interpolation buffer before the
   *  first fresh snapshot arrives after a successful reconnect. */
  onReconnectSuccess: (() => void) | null = null

  constructor(state: GameState) {
    this.state = state
    this.state.setLocalPlayerId(this.playerId)
  }

  setPreferredMapId(mapId: MapId) {
    this.mapId = mapId
    localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  }

  // -------------------------------------------------------------------------
  // Public connect / disconnect
  // -------------------------------------------------------------------------

  connect({ resume = true }: { resume?: boolean } = {}) {
    this.shouldReconnect = true
    this.reconnectAttempt = 0
    this.notifyState('connecting')
    return this.openSocket({ resume, isReconnect: false })
  }

  disconnect() {
    this.shouldReconnect = false
    this.clearReconnectTimer()
    this.closeSocket()
  }

  // -------------------------------------------------------------------------
  // Internal socket helpers
  // -------------------------------------------------------------------------

  private openSocket({
    resume,
    isReconnect,
  }: {
    resume: boolean
    isReconnect: boolean
  }): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const ws = new WebSocket(WS_URL)
      this.socket = ws

      ws.onopen = () => {
        const joinMessage: ClientMessage = {
          type: 'join_match',
          playerId: this.playerId,
          mapId: this.mapId,
          matchId: resume ? (this.matchId ?? undefined) : undefined,
        }
        this.send(joinMessage)

        if (!isReconnect) {
          resolve()
        }
      }

      ws.onerror = (err) => {
        if (!isReconnect) {
          reject(err)
        }
        // For reconnect attempts, onerror is followed by onclose — handle there.
      }

      ws.onmessage = (event) => {
        const message = JSON.parse(event.data) as ServerMessage
        this.handleMessage(message, isReconnect)
      }

      ws.onclose = () => {
        // If the socket we just closed is no longer the active one (e.g. we
        // already opened a replacement), ignore the stale close event.
        if (this.socket !== ws) return

        if (this.shouldReconnect) {
          this.scheduleReconnect()
        }
      }
    })
  }

  private closeSocket() {
    if (this.socket) {
      this.socket.onclose = null // suppress reconnect handler
      this.socket.close()
      this.socket = null
    }
  }

  // -------------------------------------------------------------------------
  // Reconnect logic
  // -------------------------------------------------------------------------

  private scheduleReconnect() {
    if (this.reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      this.notifyState('failed')
      return
    }

    this.notifyState('reconnecting')

    const delay = BACKOFF_DELAYS_MS[this.reconnectAttempt]
    this.reconnectAttempt++

    this.reconnectTimerId = setTimeout(() => {
      this.reconnectTimerId = null
      // Guard: user may have intentionally disconnected while timer was ticking.
      if (!this.shouldReconnect) return

      void this.openSocket({ resume: true, isReconnect: true })
    }, delay)
  }

  /** Called by the UI's "Retry" button after 'failed'. */
  retryReconnect() {
    this.shouldReconnect = true
    this.reconnectAttempt = 0
    this.clearReconnectTimer()
    this.closeSocket()
    this.scheduleReconnect()
  }

  private clearReconnectTimer() {
    if (this.reconnectTimerId !== null) {
      clearTimeout(this.reconnectTimerId)
      this.reconnectTimerId = null
    }
  }

  private notifyState(state: ConnectionState) {
    this.onConnectionStateChange?.(state)
  }

  get currentReconnectAttempt(): number {
    return this.reconnectAttempt
  }

  get maxReconnectAttempts(): number {
    return MAX_RECONNECT_ATTEMPTS
  }

  // -------------------------------------------------------------------------
  // Send helpers
  // -------------------------------------------------------------------------

  send(message: ClientMessage) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return
    this.socket.send(JSON.stringify(message))
  }

  // -------------------------------------------------------------------------
  // Leave stored match (intentional)
  // -------------------------------------------------------------------------

  async leaveStoredMatch() {
    const matchId = this.matchId ?? getStoredMatchId()
    if (!matchId) return

    this.shouldReconnect = false
    this.clearReconnectTimer()

    const message: LeaveMatchMessage = {
      type: 'leave_match',
      playerId: this.playerId,
      matchId,
    }

    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      // Send on the live socket so the server removes THIS client from the
      // match — allowing it to see ClientCount == 0 and delete the match.
      this.socket.send(JSON.stringify(message))
      // Give the server one event-loop tick to process before we close.
      await new Promise<void>((resolve) => setTimeout(resolve, 50))
      this.closeSocket()
    } else {
      // No live socket (page reload, tab restore) — open a temp one.
      const socket = new WebSocket(WS_URL)
      await new Promise<void>((resolve, reject) => {
        socket.onopen = () => {
          socket.send(JSON.stringify(message))
          resolve()
        }
        socket.onerror = (err) => { reject(err) }
      })
      socket.close()
    }

    this.matchId = null
    // localStorage cleared by the caller (MatchView / startNewGame / exitGame).
  }

  // -------------------------------------------------------------------------
  // Command senders
  // -------------------------------------------------------------------------

  sendMoveCommand(unitIds: number[], x: number, y: number) {
    const message: MoveCommandMessage = {
      type: 'move_command',
      unitIds,
      destination: { x, y },
    }

    this.send(message)
  }

  sendGatherCommand(unitIds: number[], targetId: string) {
    const message: GatherCommandMessage = {
      type: 'gather_command',
      unitIds,
      targetId,
    }

    this.send(message)
  }

  sendTrainUnitCommand(buildingId: string, unitType: string) {
    const message: TrainUnitCommandMessage = {
      type: 'train_unit_command',
      unitType,
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

  sendBuildBuildingCommand(unitIds: number[], buildingType: string, gridX: number, gridY: number) {
    const message: BuildBuildingCommandMessage = {
      type: 'build_building_command',
      buildingType,
      unitIds,
      gridX,
      gridY,
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

  sendStanceCommand(unitIds: number[], stance: 'hold' | 'idle') {
    const message: SetStanceCommandMessage = {
      type: 'set_stance_command',
      unitIds,
      stance,
    }
    this.send(message)
  }

  sendPatrolCommand(unitIds: number[], x: number, y: number) {
    const message: PatrolCommandMessage = {
      type: 'patrol_command',
      unitIds,
      destination: { x, y },
    }
    this.send(message)
  }

  // Dev-only: spawn a unit with a custom perk loadout at (x, y). Only
  // succeeds when the active map has debug.debugSpawn enabled; the server
  // silently ignores the command on production maps. team defaults to "mine"
  // (caller-owned) server-side if omitted.
  sendDebugSpawnUnitCommand(payload: {
    unitType: string
    team?: 'mine' | 'enemy'
    path?: string
    rank?: string
    perkIds?: string[]
    x: number
    y: number
    customHp?: number
  }) {
    this.send({
      type: 'debug_spawn_unit',
      unitType: payload.unitType,
      team: payload.team,
      path: payload.path,
      rank: payload.rank,
      perkIds: payload.perkIds,
      x: payload.x,
      y: payload.y,
      customHp: payload.customHp,
    })
  }

  sendSetBuildingSpawnPointCommand(buildingId: string, x: number, y: number) {
    const message: SetBuildingSpawnPointCommandMessage = {
      type: 'set_building_spawn_point_command',
      buildingId,
      point: { x, y },
    }

    this.send(message)
  }

  // -------------------------------------------------------------------------
  // Message handling
  // -------------------------------------------------------------------------

  private handleMessage(message: ServerMessage, isReconnect: boolean) {
    switch (message.type) {
      case 'welcome':
        this.matchId = message.matchId
        this.state.setLocalPlayerId(message.playerId)
        this.state.setMapConfig(message.map)
        localStorage.setItem(PLAYER_ID_STORAGE_KEY, message.playerId)
        localStorage.setItem(MAP_ID_STORAGE_KEY, message.map.id)
        localStorage.setItem(MATCH_ID_STORAGE_KEY, message.matchId)
        console.log('connected as', message.playerId, 'in', message.matchId)

        if (isReconnect) {
          // Clear stale interpolation frames before the fresh snapshot arrives
          // to avoid a visual glitch from interpolating across the gap.
          this.state.clearSnapshotBuffer()
          this.onReconnectSuccess?.()
        }

        this.reconnectAttempt = 0
        this.notifyState('connected')
        break

      case 'match_snapshot':
        this.matchId = message.matchId
        localStorage.setItem(MATCH_ID_STORAGE_KEY, message.matchId)
        this.applySnapshot(message)
        break

      case 'ping':
        this.send({ type: 'pong' })
        break

      case 'notification':
        this.state.addNotification(message.message)
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
