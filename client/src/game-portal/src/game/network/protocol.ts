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
  | 'upgrade-purchase'
  | 'item-purchase'
  | 'vault-access'
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

export type TileSheet =
  | 'tileset'
  | 'grass-grass-25'
  | 'dirt-dirt-25'
  | 'grass-dirt-0'

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

/** Runtime control assignment for a statically placed unit. "playerN" spawns
 *  the unit when that player joins the matching slot; "enemy" spawns at
 *  match start as a stationary guard. Decoupled from the unit's faction,
 *  which lives on the UnitDef (raider / neutral / human). */
export type PlacedUnitSlot = string

export type PlacedUnit = {
  id: string
  x: number
  y: number
  playerSlot: PlacedUnitSlot
  unitType: string
  aggroRange?: number
  leashRange?: number
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
  /** All conditions must be completed simultaneously for victory. */
  victoryConditions?: VictoryCondition[]
  debug?: MapDebugConfig
  placedUnits?: PlacedUnit[]
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
  spawnPointCount: number
}

export type LobbyStatus = 'open' | 'started' | 'closed'

export type Lobby = {
  id: string
  mapId: string
  mapName: string
  hostPlayerId: string
  players: string[]
  maxPlayers: number
  createdAt: number
  status: LobbyStatus
  matchId?: string
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
  equippedBuffIds?: string[]
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
  /** Index of the queue entry to cancel. 0 = currently-training unit
   *  (the "X" cancel button); > 0 = a queued unit waiting behind the
   *  leader. Omitted = 0 (legacy behavior). */
  queueIndex?: number
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

export type KickBuildersCommandMessage = {
  type: 'kick_builders_command'
  buildingId: string
}

export type DemolishBuildingCommandMessage = {
  type: 'demolish_building_command'
  buildingId: string
}

// ─── Player Orders ────────────────────────────────────────────────────────────

/** Compile-time-safe union of every order string the server can put on a unit.
 *  If the server renames a value, the client breaks here at typecheck time. */
export type UnitOrder = 'idle' | 'move' | 'attack_move' | 'attack_target' | 'hold' | 'patrol'

/** Exhaustive map so a human-readable label is always available without
 *  scattered switch statements across the codebase. */
export const UNIT_ORDER_LABELS: Record<UnitOrder, string> = {
  idle: 'Idle',
  move: 'Moving',
  attack_move: 'Attack Move',
  attack_target: 'Attacking',
  hold: 'Hold',
  patrol: 'Patrol',
}

export type SetStanceCommandMessage = {
  type: 'set_stance_command'
  unitIds: number[]
  /** 'hold' | 'idle' — any other value is rejected server-side. */
  stance: 'hold' | 'idle'
}

export type PatrolCommandMessage = {
  type: 'patrol_command'
  unitIds: number[]
  destination: Vec2
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

export type PurchaseUpgradeCommand = {
  type: 'purchase_upgrade'
  track: string
}

export type UpgradeTownHallCommand = {
  type: 'upgrade_townhall'
  buildingId: string
}

export type ResourceStockSnapshot = {
  id: string
  label: string
  amount: number
  max?: number
  accent: string
}

export type PlayerUpgradeSnapshot = {
  track: string
  displayName: string
  level: number
  cap: number              // 0/3/6/9
  nextCostGold: number     // 0 if at cap
  canAfford: boolean
  hasBlacksmith: boolean
  hpPerLevel: number
  damagePerLevel: number
  armorPerLevel: number
  attackSpeedPerLevel: number
  moveSpeedPerLevel: number
}

export type PlayerSnapshot = {
  playerId: string
  color: string
  resources: ResourceStockSnapshot[]
  upgrades?: PlayerUpgradeSnapshot[]
  townHallTier?: number
  vault?: VaultItemSnapshot[]
  vaultCapacity?: number
}

export type PurchaseItemCommand = {
  type: 'purchase_item'
  buildingId: string
  itemId: string
}

export type EquipItemCommand = {
  type: 'equip_item'
  unitId: number
  slotIndex: number
  instanceId: number
}

export type UnequipItemCommand = {
  type: 'unequip_item'
  unitId: number
  slotIndex: number
}

export type UseConsumableCommand = {
  type: 'use_consumable'
  unitId: number
  slotIndex: number
}

export type TransferItemCommand = {
  type: 'transfer_item'
  fromUnitId: number
  fromSlotIdx: number
  toUnitId: number
  toSlotIdx: number
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
  | KickBuildersCommandMessage
  | DemolishBuildingCommandMessage
  | DebugSpawnUnitCommandMessage
  | SetStanceCommandMessage
  | PatrolCommandMessage
  | PurchaseUpgradeCommand
  | UpgradeTownHallCommand
  | PurchaseItemCommand
  | EquipItemCommand
  | UnequipItemCommand
  | UseConsumableCommand
  | TransferItemCommand
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

/** A single item held in an inventory slot. */
export type ItemSnapshot = {
  /** Server-assigned unique instance id — used for equip/unequip commands. */
  instanceId: number
  /** Unique item id — matches an entry in the client's ITEM_DEF_MAP catalog
   *  for display name, icon, and modifier/effect resolution. */
  itemId: string
  /** Optional stack count for stackable items (potions, charges). Omitted = 1. */
  stacks?: number
}

/** Inventory carried by a unit. `size` is the number of slots the unit has
 *  unlocked; the UI may display additional locked slots beyond that.
 *  `slots` is positionally indexed — null entries are unlocked-but-empty. */
export type InventorySnapshot = {
  size: number
  slots: (ItemSnapshot | null)[]
}

/** An item stored in the player's vault between matches / on the town hall. */
export type VaultItemSnapshot = {
  instanceId: number
  itemId: string
  stacks?: number
}

export type UnitSnapshot = {
  id: number
  ownerId: string
  color: string
  unitType: UnitType
  archetype?: string
  name: string
  capabilities?: UnitCapability[]
  /** Airborne unit. The renderer should lift the sprite and draw an elevation shadow; ground-only attackers cannot target it. Omitted (false) for ground units. */
  flyer?: boolean
  visible: boolean
  status?: string
  x: number
  y: number
  hp: number
  maxHp: number
  damage?: number
  attackSpeed?: number
  /** Effective attack range in world pixels — base catalog range × any
   *  perk range multipliers (eagle_spirit, bullseye). Omitted for melee. */
  attackRange?: number
  moveSpeed?: number
  armor?: number
  /** Effective crit probability against an unmarked target (0..1). Hunter's
   *  Mark stacks add target-dependent crit at hit time and are NOT folded
   *  into this snapshot value. Omitted when the unit has no crit sources. */
  critChance?: number
  /** Damage multiplier applied on a successful crit (default 2.0; bullseye
   *  raises to 2.5). Omitted when the unit has no crit sources. */
  critMultiplier?: number
  /** Passive HP regeneration rate in HP per second. Omitted when 0. */
  healthRegen?: number
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
  /** Non-empty when this unit is linked to a VictoryCondition by objectiveId. */
  objectiveId?: string
  carriedResourceType?: ResourceType
  carriedAmount?: number
  targetX?: number
  targetY?: number
  moving: boolean
  /** Server-authoritative attack facing — the unit→target world-space delta
   *  the server is committing to this tick. Only set while the unit is in-range
   *  and firing (status === 'Attacking'); zero/absent otherwise. The renderer
   *  uses this to orient the sprite at the exact target being shot, instead of
   *  guessing locally via a nearest-enemy search. */
  actionFacingDx?: number
  actionFacingDy?: number
  /** Building ID the worker is gathering from / constructing / repairing. Used
   * by the renderer to orient the sprite toward the exact target. */
  workTargetId?: string
  /** Current order type — one of the UnitOrder values. Omitted by old servers; treat absence as 'idle'. */
  order?: UnitOrder
  /** Inventory the unit is carrying. Optional — units without inventory
   *  capability omit it entirely. */
  inventory?: InventorySnapshot
  /**
   * Live-compounded trap stats for archer/trapper units. Only present when the
   * unit is a trapper archetype that owns at least one trap bronze perk.
   * Mirrors EffectiveTrapSnapshot on the server.
   */
  effectiveTrap?: {
    perkId: string
    durationSeconds?: number
    radius?: number
    triggerRadius?: number
    placeInterval?: number
    damagePerSecond?: number
    burstDamage?: number
    slowMultiplier?: number
    markMultiplier?: number
    markDuration?: number
    barbedFieldRampPerSec?: number
    barbedFieldMaxBonusDPS?: number
    exposedWeakenedMultiplier?: number
    lastingFlamesBurnDuration?: number
    aftershockDelaySeconds?: number
  }
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
  /** Optional visual-variant tag — renderer prefers an animation with this
   *  name over `idle` when present (e.g. "electrified" for ascendant-infused
   *  caltrops). Absent = render the trap's default animation. */
  variant?: string
  /** Extra render-scale factor applied on top of the sprite set's base scale.
   *  Populated for perks that visually inflate a trap (e.g. overload_protocol
   *  on explosive_trap → 2×). Absent = 1× (no change). */
  scaleMultiplier?: number
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

export type ObjectiveSnapshot = {
  id: string
  type: 'killUnit' | 'destroyBuilding' | 'surviveWaves'
  label?: string
  completed: boolean
  /** Current kills toward a killUnit objective. */
  progress?: number
  /** Required kills for a killUnit objective (default 1). */
  count?: number
}

export type VictorySnapshot = {
  achieved: boolean
  objectives: ObjectiveSnapshot[]
}

export type VictoryCondition = {
  id: string
  type: 'killUnit' | 'destroyBuilding' | 'surviveWaves'
  label?: string
  /** Required kills for killUnit (default 1). */
  count?: number
}

/**
 * ProjectileSnapshot carries an in-flight ranged attack to the client each tick.
 * The renderer draws a shape (or sprite, picked by `variant`) traveling along
 * the arc from (originX, originY) toward (targetX, targetY), positioned by
 * `progress` (0 = just fired, 1 = landing). `targetX`/`targetY` are the
 * server's homing target position, refreshed each tick from the target unit,
 * so moving targets don't outrun their incoming arrow.
 */
export type ProjectileSnapshot = {
  id: string
  ownerUnitId: number
  ownerId: string
  /** Target unit id — informational. The server owns homing updates; `targetX`/`targetY`
   *  already reflect the current tracked position. Absent when unknown. */
  targetUnitId?: number
  originX: number
  originY: number
  targetX: number
  targetY: number
  /** Fraction of flight completed, 0..1. */
  progress: number
  /** Sprite key — defaults to the attacker's unit type. Perks may override it
   *  at fire time for alternate shot visuals (e.g. "fire_arrow"). */
  variant?: string
  /** True on the second arrow of a Double Shot pair (Marksman gold). The
   *  client uses this to render a combined yellow damage number after both
   *  arrows have landed. */
  doubleShotSecond?: boolean
  /** True for Marksman silver pierce arrows. The renderer uses it to extend
   *  the arrow visual to TargetX/Y (which is the far endpoint of the pierce
   *  line, not a homing target position). */
  pierce?: boolean
}

/**
 * CritEventSnapshot is a per-tick record that a critical hit landed on a
 * unit. The client matches each entry to its HP-diff damage event by
 * (unitId, damage) and renders the floating number with a red circle behind
 * it. Empty when no crits land — the field is omitted from JSON entirely.
 */
export type CritEventSnapshot = {
  unitId: number
  damage: number
}

/**
 * MinorDamageEventSnapshot tags a portion of a unit's HP-delta as ancillary
 * damage (Reactive Flames splash, Electrified Caltrops bonus damage, etc.).
 * The client peels matching amounts off the floating-number popup and renders
 * that portion in a smaller font with a distinct color so the player can
 * read at a glance "this is the trap, this is the Infusion splash."
 *
 * `variant` selects the renderer color:
 *   - "fire"     → orange (default when omitted)
 *   - "electric" → purple
 */
export type MinorDamageEventSnapshot = {
  unitId: number
  damage: number
  variant?: string
}

/**
 * LethalDamageEventSnapshot carries the pre-clamp damage value for an overkill
 * killing blow. The client's killing-blow synthesis would otherwise show only
 * the victim's remaining HP (capped) because dead units are stripped from the
 * snapshot before HP-diff runs. When an entry is present for a disappearing
 * unit, the synthesized popup uses `damage` instead of `prev.hp`. Only emitted
 * for overkill — exact kills don't need an override.
 */
export type LethalDamageEventSnapshot = {
  unitId: number
  damage: number
}

/**
 * EffectSnapshot is a transient sprite-sheet VFX anchored to a unit or world
 * position. The server owns the lifecycle; the client renders sprite frames
 * driven by `progress` (0 = first frame, 1 = last frame). `anchorUnitId`
 * (when present and non-zero) tells the renderer to track the unit's
 * interpolated position rather than the static `x`/`y` fallback.
 */
export type EffectSnapshot = {
  id: number
  /** Effect name matches the directory under assets/effects/ (e.g. "whirlwind"). */
  name: string
  /** Unit the effect rides on. 0 or absent means world-anchored; use `x`/`y`. */
  anchorUnitId?: number
  /** World position resolved server-side each tick. Used as fallback when the
   *  anchor unit is missing from the current interpolated frame. */
  x: number
  y: number
  /** Lifetime fraction: 0 = just spawned, 1 = ending. */
  progress: number
  /** Draw-time scale applied to the sprite's native frame dimensions. Default 1.0. */
  sizeScale?: number
  variant?: string
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
  projectiles?: ProjectileSnapshot[]
  effects?: EffectSnapshot[]
  critEvents?: CritEventSnapshot[]
  minorDamageEvents?: MinorDamageEventSnapshot[]
  lethalDamageEvents?: LethalDamageEventSnapshot[]
  // Present only when the active map has debug.battleTracker=true. Absent
  // otherwise — the client treats absence as "debug tracker disabled".
  battleTracker?: BattleTrackerSnapshot
  gameOver?: GameOverSnapshot
  victory?: VictorySnapshot
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

// Sentinel owner ID for wave / NPC enemies. Real players are allied with each
// other; only this ID is hostile to them. Mirrors enemyPlayerID on the server.
export const ENEMY_PLAYER_ID = '__enemy__'

export type BattlePlayerStats = {
  // Player ID that owns the damage-dealing source. ENEMY_PLAYER_ID is the
  // sentinel used by the server for wave / NPC enemies.
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
