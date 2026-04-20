export type Vec2 = {
  x: number
  y: number
}

/** Connection lifecycle state surfaced to the Vue layer. */
export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'failed'

export type MapId = string

export type TerrainType = 'dirt' | 'grass'

export type ObstacleType = 'rock' | 'wall' | 'tree'
export type BuildingType = 'goldmine' | 'townhall' | 'barracks' | 'farm' | 'enemy-spawnpoint' | 'spawn-point' | (string & {})
export type BuildingCapability =
  | 'resource-source'
  | 'unit-spawner'
  | 'occupiable'
  | 'deposit-point'
  | 'enemy-spawner'
  | 'selectable'
export type ObstacleCapability = 'resource-source' | 'selectable'
export type ResourceType = 'gold' | 'wood'
export type UnitType = 'worker' | 'soldier' | (string & {})
export type UnitCapability = 'move' | 'gather' | 'build' | 'attack'
export type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
export type JsonObject = { [key: string]: JsonValue }

export type GridCoord = {
  x: number
  y: number
}

export type TerrainTile = GridCoord & {
  terrain: TerrainType
}

export type TileSheet = 'tileset'

export type TileCoord = {
  sheet: TileSheet
  sx: number
  sy: number
}

export type TileInstance = GridCoord & TileCoord

export type ObstacleTile = GridCoord & {
  obstacle: ObstacleType
  id?: string
  width?: number
  height?: number
  capabilities?: ObstacleCapability[]
  resourceType?: ResourceType
  resourceAmount?: number
  hp?: number
  maxHp?: number
  metadata?: JsonObject
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
  metadata?: JsonObject
}

export type WaveConfig = {
  totalWaves?: number
  prepDuration?: number
  waveDuration?: number
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
  tiles?: TileInstance[]
  defaultTile?: TileCoord
  obstacles: ObstacleTile[]
  buildings: BuildingTile[]
  waveConfig?: WaveConfig
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
  targetId: string
}

export type TrainUnitCommandMessage = {
  type: 'train_unit_command'
  unitType: string
  buildingId: string
}

export type AttackCommandMessage = {
  type: 'attack_command'
  unitIds: number[]
  targetUnitId: number
}

export type AttackMoveCommandMessage = {
  type: 'attack_move_command'
  unitIds: number[]
  destination: Vec2
}

export type CancelTrainingCommandMessage = {
  type: 'cancel_training_command'
  buildingId: string
}

export type SetBuildingSpawnPointCommandMessage = {
  type: 'set_building_spawn_point_command'
  buildingId: string
  point: Vec2
}

export type BuildBuildingCommandMessage = {
  type: 'build_building_command'
  buildingType: string
  unitIds: number[]
  gridX: number
  gridY: number
}

export type RepairCommandMessage = {
  type: 'repair_command'
  unitIds: number[]
  buildingId: string
}

export type ResourceStockSnapshot = {
  id: string
  label: string
  amount: number
  max?: number
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
  | TrainUnitCommandMessage
  | AttackCommandMessage
  | AttackMoveCommandMessage
  | CancelTrainingCommandMessage
  | SetBuildingSpawnPointCommandMessage
  | BuildBuildingCommandMessage
  | RepairCommandMessage
  | PongMessage

export type UnitSnapshot = {
  id: number
  ownerId: string
  color: string
  unitType: UnitType
  archetype?: string
  name: string
  capabilities?: UnitCapability[]
  visible: boolean
  status?: string
  x: number
  y: number
  hp: number
  maxHp: number
  damage?: number
  attackSpeed?: number
  moveSpeed?: number
  armor?: number
  xp?: number
  rank?: string
  xpToNextRank?: number
  xpIntoCurrentRank?: number
  recentRankUpSeconds?: number
  progressionPath?: string
  perkIds?: string[]
  /** Temporary HP pool (from blood_engine). 0/undefined when absent. */
  shield?: number
  /** Max shield pool advertised by the unit's perks. */
  maxShield?: number
  /** Perk-id list for buffs currently active on this unit. */
  activeBuffs?: string[]
  /** Icon-id list for negative status effects currently active on this unit.
   *  Unlike activeBuffs, these are raw icon ids (not perk ids) because debuffs
   *  can land on units that don't own the causing perk. */
  activeDebuffs?: string[]
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

/**
 * Wave state snapshot sent with every MatchSnapshotMessage.
 * - enabled: false means the map uses legacy always-on spawn behaviour.
 * - state "prep"     → timer = seconds remaining until wave starts
 * - state "active"   → timer = seconds elapsed since wave started
 * - state "complete" → all waves finished; timer is irrelevant
 */
export type WaveSnapshot = {
  enabled: boolean
  currentWave: number
  totalWaves: number    // 0 = infinite waves
  state: 'prep' | 'active' | 'complete' | ''
  timer: number
  waveDuration: number
}

export type BannerSnapshot = {
  id: number
  ownerId: string
  x: number
  y: number
  radius: number
  remainingSeconds: number
}

export type TrapSnapshot = {
  id: number
  ownerId: string
  x: number
  y: number
  radius: number
  type: 'caltrops' | 'fire_pit' | 'explosive_trap' | 'marker_trap'
  remainingSeconds: number
  /**
   * True for exactly one snapshot tick when the trap detonates. Set for both
   * the initial blast and any follow-up blast (e.g., explosive_chain
   * aftershock). Absent on all other ticks. Client renders a one-frame burst.
   */
  triggered?: boolean
}

export type MatchSnapshotMessage = {
  type: 'match_snapshot'
  tick: number
  serverNow: number
  matchId: string
  buildings: BuildingTile[]
  obstacles: ObstacleTile[]
  players: PlayerSnapshot[]
  units: UnitSnapshot[]
  wave: WaveSnapshot
  banners?: BannerSnapshot[]
  traps?: TrapSnapshot[]
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

export type NotificationMessage = {
  type: 'notification'
  message: string
}

export type ServerMessage =
  | WelcomeMessage
  | MatchSnapshotMessage
  | ErrorMessage
  | NotificationMessage
  | PingMessage
