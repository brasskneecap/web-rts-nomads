export type Vec2 = {
  x: number
  y: number
}

export type MapId = string

export type TerrainType = 'dirt' | 'water' | 'forest'

export type ObstacleType = 'rock' | 'wall' | 'tree'
export type BuildingType = 'goldmine' | 'townhall' | 'tree'
export type BuildingCapability =
  | 'resource-source'
  | 'unit-spawner'
  | 'occupiable'
  | 'deposit-point'
export type ResourceType = 'gold' | 'wood'
export type UnitType = 'worker'
export type UnitCapability = 'move' | 'gather' | 'build'

export type GridCoord = {
  x: number
  y: number
}

export type TerrainTile = GridCoord & {
  terrain: TerrainType
}

export type ObstacleTile = GridCoord & {
  obstacle: ObstacleType
}

export type BuildingTile = GridCoord & {
  id: string
  buildingType: BuildingType
  width: number
  height: number
  occupied: boolean
  visible: boolean
  ownerId?: string | null
  capabilities: BuildingCapability[]
  resourceType?: ResourceType
  resourceAmount?: number
  spawnUnitTypes?: string[]
  metadata?: Record<string, string | number | boolean | null>
}

export type MapConfig = {
  id: MapId
  name: string
  description: string
  width: number
  height: number
  gridCols: number
  gridRows: number
  cellSize: number
  terrain: TerrainTile[]
  obstacles: ObstacleTile[]
  buildings: BuildingTile[]
}

export type MapCatalogEntry = {
  id: MapId
  name: string
  description: string
  gridCols: number
  gridRows: number
}

export type MapCatalogMapPayload = Omit<MapConfig, 'id' | 'name' | 'description'>

export type MapCatalogFile = {
  id: MapId
  name: string
  description: string
  sortOrder: number
  map: MapCatalogMapPayload
}

export type JoinMatchMessage = {
  type: 'join_match'
  playerId: string
  mapId: MapId
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

export type GatherCommandMessage = {
  type: 'gather_command'
  unitIds: number[]
  buildingId: string
}

export type TrainWorkerCommandMessage = {
  type: 'train_worker_command'
  buildingId: string
}

export type SetBuildingSpawnPointCommandMessage = {
  type: 'set_building_spawn_point_command'
  buildingId: string
  point: Vec2
}

export type ResourceStockSnapshot = {
  id: string
  label: string
  amount: number
  accent: string
}

export type PlayerSnapshot = {
  playerId: string
  color: string
  resources: ResourceStockSnapshot[]
}

export type ClientMessage =
  | JoinMatchMessage
  | LeaveMatchMessage
  | MoveCommandMessage
  | GatherCommandMessage
  | TrainWorkerCommandMessage
  | SetBuildingSpawnPointCommandMessage
  | PongMessage

export type UnitSnapshot = {
  id: number
  ownerId: string
  color: string
  unitType: UnitType
  name: string
  capabilities?: UnitCapability[]
  visible: boolean
  status?: string
  x: number
  y: number
  hp: number
  maxHp: number
  carriedResourceType?: ResourceType
  carriedAmount?: number
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
  players: PlayerSnapshot[]
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
