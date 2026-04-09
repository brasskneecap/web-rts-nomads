export type Vec2 = {
  x: number
  y: number
}

export type MapSize = 'small' | 'medium' | 'large'

export type MapConfig = {
  size: MapSize
  width: number
  height: number
}

export type JoinMatchMessage = {
  type: 'join_match'
  playerId: string
  mapSize: MapSize
  matchId?: string
}

export type LeaveMatchMessage = {
  type: 'leave_match'
  playerId: string
  matchId: string
}

export type MoveCommandMessage = {
  type: 'move_command'
  unitIds: number[]
  destination: Vec2
}

export type ClientMessage =
  | JoinMatchMessage
  | LeaveMatchMessage
  | MoveCommandMessage
  | PongMessage

export type UnitSnapshot = {
  id: number
  ownerId: string
  color: string
  x: number
  y: number
  hp: number
  targetX?: number
  targetY?: number
  moving: boolean
}

export type WelcomeMessage = {
  type: 'welcome'
  playerId: string
  matchId: string
  map: MapConfig
}

export type MatchSnapshotMessage = {
  type: 'match_snapshot'
  tick: number
  serverNow: number
  matchId: string
  map: MapConfig
  units: UnitSnapshot[]
}

export type PingMessage = {
  type: 'ping'
}

export type PongMessage = {
  type: 'pong'
}

export type ErrorMessage = {
  type: 'error'
  message: string
}

export type ServerMessage =
  | WelcomeMessage
  | MatchSnapshotMessage
  | ErrorMessage
  | PingMessage