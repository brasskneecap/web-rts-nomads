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
  debug?: MapDebugConfig
}

// Per-map debug/telemetry opt-ins. Only set on development maps — production
// maps should omit this field so the debug UI is completely hidden.
export type MapDebugConfig = {
  // Enables the in-game Battle Tracker HUD. When true, the server streams
  // per-player damage/kill totals with every snapshot and the client renders
  // a collapsible debug panel with a save-to-localStorage button.
  battleTracker?: boolean

  // Enables the "spawn enemy with perks" dev tool. When true, the client
  // renders a debug panel for configuring unit type / path / rank / perks /
  // custom HP and placing that unit with a click on the map. The server
  // honors `debug_spawn_unit` commands from any joined client on this map.
  debugSpawn?: boolean
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

// Dev-only command issued by the DebugSpawnPanel. See MapDebugConfig.debugSpawn —
// the server hard-gates this to maps with that flag on. perkIds are applied
// verbatim (no eligibility filtering) so any combo can be tested. team="mine"
// (default) gives the unit to the caller; team="enemy" spawns it as hostile.
export type DebugSpawnUnitCommandMessage = {
  type: 'debug_spawn_unit'
  unitType: string
  team?: 'mine' | 'enemy'
  path?: string
  rank?: string
  perkIds?: string[]
  x: number
  y: number
  customHp?: number
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
  | DebugSpawnUnitCommandMessage
  | PongMessage

// One entry in a unit's activeBuffs / activeDebuffs list. `id` is the perk
// id (buffs) or raw icon id (debuffs). `stacks` is the number of concurrent
// sources contributing the effect — absent when 1 so single-instance
// effects stay compact on the wire. The renderer overlays a small count
// badge whenever stacks >= 2.
export type ActiveEffectIcon = {
  id: string
  stacks?: number
}

/** PerkCooldownSnapshot advertises a ticking cooldown on one of a unit's
 *  owned perks. The HUD renders a clock-wipe overlay covering fraction
 *  remaining / total of the icon, plus `ceil(remaining)` as a label. */
export type PerkCooldownSnapshot = {
  perkId: string
  remaining: number
  total: number
}

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
  /** Buffs currently active on this unit — each entry carries a perk id and
   *  optional stack count (omitted when 1). Stacks >= 2 render a count
   *  badge over the icon on-screen. */
  activeBuffs?: ActiveEffectIcon[]
  /** Debuffs currently active on this unit. Same shape as activeBuffs, but
   *  ids are raw icon ids (not perk ids) because debuffs can land on units
   *  that don't own the causing perk. */
  activeDebuffs?: ActiveEffectIcon[]
  /** Per-perk cooldown timers for perks owned by this unit. Only perks whose
   *  next activation is currently gated by a ticking timer appear here.
   *  Drives the clock-wipe overlay + seconds label on the perk HUD icon. */
  perkCooldowns?: PerkCooldownSnapshot[]
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
  id: string
  ownerId: string
  x: number
  y: number
  /** Damage/effect area. For explosive_trap this is the outer explosion AoE;
   *  for other types it's the single active zone. */
  radius: number
  /** Inner zone that causes detonation. Only set for trap types with a
   *  separate trigger/effect radius (currently just explosive_trap); absent
   *  for the others, where `radius` alone is the full active area. */
  triggerRadius?: number
  type: 'caltrops' | 'fire_pit' | 'explosive_trap' | 'marker_trap'
  remainingSeconds: number
  /**
   * True for exactly one snapshot tick when the trap detonates. Set for both
   * the initial blast and any follow-up blast (e.g., explosive_chain
   * aftershock). Absent on all other ticks. Client renders a one-frame burst.
   */
  triggered?: boolean
}

export type GameOverSnapshot = {
  lostPlayerIds: string[]
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
  // Present only when the active map has debug.battleTracker=true. Absent
  // otherwise — the client treats absence as "debug tracker disabled".
  battleTracker?: BattleTrackerSnapshot
  gameOver?: GameOverSnapshot
}

// ─── Battle Tracker (debug) ──────────────────────────────────────────────────

export type BattleStats = {
  damageDealt: number
  kills: number
}

export type BattleBucket = {
  // "unit" | "trap" | "building" — damage source category
  kind: 'unit' | 'trap' | 'building'
  // Unit type / trap type / building type — concrete identifier inside kind
  subtype: string
  stats: BattleStats
}

export type BattlePlayerStats = {
  // Player ID that owns the damage-dealing source. "__enemy__" is the sentinel
  // used by the server for wave / NPC enemies.
  playerId: string
  buckets: BattleBucket[]
  total: BattleStats
}

export type BattleTrackerSnapshot = {
  // Match-elapsed seconds since the tracker was armed. Shown as the "duration"
  // header in the debug panel.
  elapsedSeconds: number
  players: BattlePlayerStats[]
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
