export type Vec2 = {
  x: number
  y: number
}

/** Connection lifecycle state surfaced to the Vue layer. */
export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'failed'

export type MapId = string

export type TerrainType = 'dirt' | 'grass'

export type ObstacleType = 'rock' | 'wall' | 'tree'
export type BuildingType = 'goldmine' | 'townhall' | 'barracks' | 'farm' | 'enemy-spawnpoint' | 'spawn-point' | 'recipe-shop' | 'artificer' | (string & {})
export type BuildingCapability =
  | 'resource-source'
  | 'unit-spawner'
  | 'occupiable'
  | 'deposit-point'
  | 'enemy-spawner'
  | 'selectable'
  | 'upgrade-purchase'
  | 'item-purchase'
  | 'recipe-purchase'
  | 'vault-access'
  | 'crafting'
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

/** Per-tick patch for one obstacle's live metadata. The server only emits
 *  these when a value actually changed since the previous broadcast (e.g.
 *  a worker entered or left a tree), so the wire cost is near zero in the
 *  steady state. maxWorkers is constant per obstacle type and known from
 *  welcome, so it is not part of the patch. */
export type ObstacleMetadataPatch = {
  id: string
  currentWorkers: number
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
  ghost?: boolean
  lastSeenTick?: number
  // Shop fields (per-building-shop-inventories). Runtime: shopInventory,
  // shopGuardUnitIds, shopLocked, shopDiscovered. Authored: shopLootTableId,
  // shopFixedInventory. Each shopInventory entry carries the item id and
  // the remaining purchasable quantity; quantity 0 means the slot stays
  // visible but is rendered disabled (greyed-out).
  shopInventory?: ShopStockEntry[]
  shopLootTableId?: string
  shopFixedInventory?: string[]
  shopGuardUnitIds?: number[]
  shopLocked?: boolean
  shopDiscovered?: boolean
  recipeInventory?: RecipeStockEntry[]
}

export type ShopStockEntry = {
  itemId: string
  quantity: number
}

export type RecipeStockEntry = {
  recipeId: string
  quantity: number
}

export type FogOfWarSnapshot = {
  cols: number
  rows: number
  runs: number[]
  revTick: number
}

export type WaveConfig = {
  totalWaves?: number
  prepDuration?: number
  waveDuration?: number
  // Continuous mode: waves keep releasing on the waveDuration timer instead of
  // waiting for the field to clear; an upgrade pick is shown at each new wave.
  continuousWaves?: boolean
  // Whether the enemy wave faction and neutral camps attack each other. When on,
  // a camp wiped by enemies drops no loot. Default off (they ignore each other).
  enemiesFightNeutrals?: boolean
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

export interface NeutralSpawn {
  id: string
  x: number
  y: number
  groupId: string
  startingTier?: number
  tierUpEveryNWaves?: number
  aggroRange?: number
  leashRange?: number
  healthMultiplier?: number
  healthMultiplierPerWave?: number
  damageMultiplier?: number
  damageMultiplierPerWave?: number
}

export const NEUTRAL_SPAWN_RANDOM_GROUP_ID = '__random__'

// Mirrors server-side neutralPlayerColor (state_waves.go). Centralized here
// so the map-editor marker and any future minimap/HUD treatment stay in sync.
export const NEUTRAL_PLAYER_COLOR = '#9b59b6'

// Per-tick wire view of a neutral camp. The static placement (position,
// group, scaling) lives in MapConfig.neutralSpawns; this carries only the
// fields that change per wave (currentTier, aliveUnitCount). Sent
// unfiltered — neutrals are mapper-authored points of interest and always
// reach the client regardless of fog of war.
//
// aliveUnitCount lets the client hide the minimap POI dot for cleared or
// wave-hidden camps so the minimap reflects which camps currently have
// enemies on the field.
export interface NeutralCampSnapshot {
  id: string
  x: number
  y: number
  currentTier: number
  aliveUnitCount: number
}

// Per-tick wire view of one ground-loot chest. Sent unfiltered (no FOW
// gating) so chests behave like POI dots on the minimap. The pre-rolled
// `resources` and `itemIds` are shown in the hover tooltip — these are
// the same values granted on pickup (less vault-overflow items).
export interface LootDropSnapshot {
  id: string
  x: number
  y: number
  iconKey: string
  resources?: Record<string, number>
  itemIds?: string[]
}

// Right-click "go collect that chest" order. Mirrors GatherCommandMessage
// exactly so transport / replay layers handle it uniformly.
export interface PickupLootCommandMessage {
  type: 'pickup_loot_command'
  unitIds: number[]
  targetId: string
}

// Pushed to the collecting player when a chest pickup completes. The HUD
// renders a toast listing resources + items received; overflowItemIds
// lists items that couldn't fit in the vault and were lost.
export interface LootCollectedNotification {
  type: 'loot_collected'
  playerId: string
  lootDropId: string
  collectingUnitId: number
  resources?: Record<string, number>
  itemIds?: string[]
  overflowItemIds?: string[]
}

// Wire string for the new OrderPickupLoot type. The server emits this
// in UnitSnapshot.order when the unit is en route to collect a chest.
export const OrderStringPickupLoot = 'pickup_loot'

// Catalog DTOs from GET /api/catalog/neutral-groups
export interface NeutralGroupSummary {
  id: string
  name: string
}
export interface NeutralGroupTierSummary {
  tier: number
  groups: NeutralGroupSummary[]
}

/** Mirror of server protocol.ZoneCapture. `config` is opaque JSON whose
 *  shape is determined by the `type` key. Three registered types:
 *  `control_point` (no extra config), `presence` ({captureSeconds:number}),
 *  `clear` (no extra config). Source of truth is ListZoneCaptureTypes() on
 *  the server; hardcoded here for the editor selector. */
export type ZoneCapture = {
  type: string
  config?: Record<string, unknown>
}

/** Mirror of server protocol.Zone. `cells` is a compact list of [x,y] pairs;
 *  perimeter/interior are derived client-side from the cell set and never
 *  stored. `anchor` is GridCoord {x,y}. `captureCells` is the optional
 *  capture sub-zone: the subset of cells a unit must stand in to progress a
 *  PRESENCE capture. Empty/absent means the whole zone counts. */
export type Zone = {
  id: string
  name?: string
  anchor: GridCoord
  cells: [number, number][]
  capture: ZoneCapture
  startingOwner?: string
  /** Directed capture-prerequisite zone ids. Empty ⇒ ungated (always
   *  capturable). See requireAllLinks for any-vs-all semantics. Mirrors
   *  protocol.Zone.Adjacent. */
  adjacent?: string[]
  /** false (default) ⇒ owning ANY linked zone unlocks this one; true ⇒ ALL
   *  linked zones must be owned. Ignored when adjacent is empty. Mirrors
   *  protocol.Zone.RequireAllLinks. */
  requireAllLinks?: boolean
  captureCells?: [number, number][]
  /** Capture-point slots for the CLAIM mechanic: each is the top-left cell of a
   *  2x2 tower slot. The team must build + defend a tower on every point to
   *  capture the zone. Empty/absent ⇒ a single slot at `anchor`. */
  claimPoints?: [number, number][]
  /** When set to a player label (e.g. "player1"), this zone is that team's
   *  HOME zone: it starts team-owned and is NOT capturable (the capture
   *  mechanic is skipped). Mirrors protocol.Zone.LockedSpawnLabel on the
   *  server (json:"lockedSpawnLabel,omitempty"). */
  lockedSpawnLabel?: string
  /** Optional passive bonuses the zone grants its owner while controlled.
   *  Expressed in the shared stat-modifier vocabulary (same stat ids/operations
   *  as perks/buffs). Static (travels in the welcome payload). Mirrors
   *  protocol.Zone.Auras. */
  auras?: ZoneAura[]
}

/** Canonical, system-agnostic stat bonus. Mirrors protocol.StatModifier.
 *  operation is "add" (flat, summed) or "multiply" (multiplicative, e.g. 1.15
 *  for +15%). */
export type StatModifier = {
  stat: string
  operation: 'add' | 'multiply'
  value: number
}

/** A single zone aura. `type` discriminates the kind; v1 only "stat_modifier"
 *  (reads `modifier`). `scope` defaults to "global". Mirrors protocol.ZoneAura. */
export type ZoneAura = {
  type: string
  scope?: string
  modifier: StatModifier
}

export const ZONE_AURA_TYPE_STAT_MODIFIER = 'stat_modifier'
export const ZONE_AURA_SCOPE_GLOBAL = 'global'

/** Per-tick runtime state of one zone. Mirrors server protocol.ZoneSnapshot. */
export type ZoneSnapshot = {
  id: string
  owner: string
  contested?: boolean
  progress?: number
  /** Controlling player's display color, resolved server-side (team owner →
   *  lowest-slot player's color). Empty/absent ⇒ unowned → render grey.
   *  Mirrors protocol.ZoneSnapshot.OwnerColor. */
  ownerColor?: string
  /** Per-capture-point progress for a multi-point CLAIM zone, in the same
   *  order as the zone's `claimPoints`. `progress` is a 0..1 fraction of the
   *  shared defend duration; `captured` flips true once the point is held. */
  claimPoints?: { progress: number; captured?: boolean }[]
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
  /** SHA-256 over the map's canonical authored JSON. Delivered in the
   *  WelcomeMessage so the joiner can cache the received map keyed by id+hash.
   *  Empty/absent on older servers. */
  contentHash?: string
  /** Optional human-readable version label. Display only. */
  version?: string
  terrain: TerrainTile[]
  tiles?: TileInstance[]
  defaultTile?: TileCoord
  obstacles: ObstacleTile[]
  buildings: BuildingTile[]
  waveConfig?: WaveConfig
  /** Optional campaign tag. Presence makes the map a campaign level — the
   *  Custom Game lobby hides it from the map dropdown, and the server
   *  contributes its level info to /api/catalog/campaigns under the named
   *  campaign. Edited via the map editor's Campaign card. */
  campaign?: MapCampaignBlock
  debug?: MapDebugConfig
  placedUnits?: PlacedUnit[]
  neutralSpawns?: NeutralSpawn[]
  /** Authored map zones (capture points, presence zones, etc.). Optional —
   *  maps without zones behave identically to pre-zone maps. */
  zones?: Zone[]
}

/** Wire shape of the "this map is a campaign level" tag.
 *  Mirrors `protocol.MapCampaignBlock` on the server. Authored in the map
 *  editor; the server validates the contained objectives at save time and
 *  again at catalog-read time so bad data is rejected before it reaches
 *  match runtime. */
export type MapCampaignBlock = {
  /** Parent campaign id (e.g. `forest`). Must match a header file at
   *  `server/internal/game/catalog/campaigns/<campaignId>.json`. */
  campaignId: string
  /** Stable, globally-unique level id (e.g. `forest_01`). */
  levelId: string
  /** Human-readable label shown in the campaign panel + recap. */
  displayName: string
  /** Level that must be completed before this one unlocks. Use `null` for
   *  the first level of a chain. Cross-campaign prereqs unsupported. */
  prerequisiteLevelId: string | null
  /** Short blurb shown on the level row. */
  description?: string
  /** Row ordering within the campaign. Ties broken by levelId. */
  sortOrder?: number
  /** Per-level objectives. Each `config` is opaque to the client; the
   *  server registry's per-type schema lives in `objective_handlers.go`. */
  objectives?: MapCampaignObjective[]
}

/** Wire shape of one authored objective on a campaign-tagged map.
 *  Mirrors `protocol.MapCampaignObjective` on the server. */
export type MapCampaignObjective = {
  id: string
  type: string
  description?: string
  scope?: 'team' | 'player'
  required?: boolean
  /** DP reward granted the first time (ever, per player) this objective is
   *  completed. Absent/0 = no reward. Mirrors `RewardDominionPoints` on the
   *  server's `protocol.MapCampaignObjective`. */
  rewardDominionPoints?: number
  /** Conquest Badge reward granted on first-ever completion. Absent/0 = no reward. */
  rewardConquestBadges?: number
  config?: Record<string, unknown>
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
  /** When non-empty, the map is tagged as a campaign level. The Custom
   *  Game lobby filters these out of its map dropdown; the map editor
   *  still shows them in its load-map list. */
  campaignId?: string
  /** SHA-256 over the map's canonical authored JSON (hash field excluded from
   *  its own input). Empty/absent on older servers that have not been updated. */
  contentHash?: string
  /** Optional human-readable version label (e.g. "v3"). Display only — never
   *  used for matching. Empty/absent when not set. */
  version?: string
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
  /** Set when the lobby was created for a campaign level. Drives server
   *  objective injection at match start (see §7 of campaign-objectives-and-metrics). */
  campaignLevelId?: string
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
  activeUpgradeIds?: string[]
  ownedUpgradeRanks?: Record<string, number>
  acquiredAdvancementIds?: string[]
  knownRecipeIds?: string[]
  /** Content-addressed map distribution: the map contentHashes this client
   *  already holds in its local cache for `mapId`. The server omits the map
   *  from the welcome when the match map's hash is in this list. */
  cachedMapHashes?: string[]
}

/** Client→server fallback when the client claimed a cache hit but couldn't load
 *  the map locally (eviction race). The server replies with MapContentMessage. */
export type RequestMapMessage = {
  type: 'request_map'
  mapId: MapId
  contentHash: string
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

export type DepositCommandMessage = {
  type: 'deposit_command'
  unitIds: number[]
  buildingId: string
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

/** Player-level "commander" ability cast — fired from the bottom action bar
 *  at a world position; no unit selection required. The server validates the
 *  player's cooldown; rejections come back as NotificationMessage. */
export type CastCommanderAbilityCommandMessage = {
  type: 'cast_commander_ability'
  abilityId: string
  x: number
  y: number
}

/** Action-bar standard cast (left-click → click target). */
export type CastAbilityCommandMessage = {
  type: 'cast_ability_command'
  casterUnitId: number
  abilityId: string
  targetUnitId: number
}

/** Action-bar "focus target" assignment (player-issued sticky support
 *  target for Clerics). When `targetUnitId === 0` the focus is cleared.
 *  Server validates ownership of `casterUnitId` and team-allegiance of
 *  `targetUnitId`; rejection comes back via NotificationMessage. */
export type SetFocusTargetCommandMessage = {
  type: 'set_focus_target_command'
  casterUnitId: number
  /** Allied target unit ID; 0 clears the current focus. */
  targetUnitId: number
}

/** Action-bar auto-cast toggle (right-click an ability). */
export type ToggleAutoCastCommandMessage = {
  type: 'toggle_autocast_command'
  unitId: number
  abilityId: string
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
export type UnitOrder =
  | 'idle'
  | 'move'
  | 'attack_move'
  | 'attack_target'
  | 'hold'
  | 'patrol'
  | 'guard'
  | 'focus_follow'
  | 'pickup_loot'

/** Exhaustive map so a human-readable label is always available without
 *  scattered switch statements across the codebase. */
export const UNIT_ORDER_LABELS: Record<UnitOrder, string> = {
  idle: 'Idle',
  move: 'Moving',
  attack_move: 'Attack Move',
  attack_target: 'Attacking',
  hold: 'Hold',
  patrol: 'Patrol',
  guard: 'Guard',
  focus_follow: 'Following',
  pickup_loot: 'Picking Up',
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

export type GuardCommandMessage = {
  type: 'guard_command'
  unitIds: number[]
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
  // Blacksmith to research at. Omitted = auto-assign to any idle blacksmith
  // (used by the global Blacksmith panel).
  buildingId?: string
}

export type CancelUpgradeCommand = {
  type: 'cancel_upgrade'
  buildingId: string
  // Queue entry to cancel: 0 (default, omitted) is the in-progress upgrade;
  // higher indices are queued behind it.
  queueIndex?: number
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
  /** How many of this track are queued (in progress + waiting). 0 when idle.
   *  level + queuedCount is the level the queue will reach; the next purchase
   *  stacks above it. */
  queuedCount?: number
  nextCostGold: number     // cost of the next stackable level; 0 if at cap
  nextCostWood: number     // mirrors nextCostGold; 0 if at cap
  canAfford: boolean
  /** True when this upgrade can be started OR queued: below the projected cap,
   *  affordable, and a blacksmith exists to host it. */
  canStart: boolean
  hasBlacksmith: boolean
  /** In-progress research for this track — only populated while the track is at
   *  the HEAD of its blacksmith's queue. researchTotal is the full duration in
   *  seconds (0/absent when not actively researching); researchRemaining counts
   *  down to 0; researchBuildingId is the blacksmith performing the work. */
  researchTotal?: number
  researchRemaining?: number
  researchBuildingId?: string
  /** Blacksmith holding this track's queue (in progress or merely queued); the
   *  cancel/queue target. Equals researchBuildingId when at the head. Empty when
   *  the track is idle. */
  queueBuildingId?: string
  hpPerLevel: number
  damagePerLevel: number
  armorPerLevel: number
  attackSpeedPerLevel: number
  moveSpeedPerLevel: number
}

/** Commander ability slot — player-level ability with a live cooldown. */
export type CommanderAbilitySnapshot = {
  id: string
  displayName?: string
  icon?: string
  radius?: number
  cooldownTotal?: number
  cooldownRemaining?: number
  /** Per-cast magnitude — exactly one is non-zero per ability, mirroring the
   *  server's Damage>0 vs Heal>0 apply switch. Surfaced for HUD tooltips. */
  damage?: number
  heal?: number
}

export type PlayerSnapshot = {
  playerId: string
  color: string
  /** Alliance group. 0 = the default shared team (all players allied —
   *  current behavior). Same teamId ⇒ allies. The client mirrors the server
   *  hostility predicate from this; absent (older servers) ⇒ treat as 0. */
  teamId: number
  resources: ResourceStockSnapshot[]
  upgrades?: PlayerUpgradeSnapshot[]
  townHallTier?: number
  vault?: VaultItemSnapshot[]
  vaultCapacity?: number
  unlockedRecipeIds?: string[]
  /** Unit types this player cannot train because their server-side
   *  RequiresBuildings list is unsatisfied. Absent/empty = no locks. */
  lockedUnitTypes?: string[]
  /** Commander abilities slotted in the bottom action bar. Always present
   *  for the local player; absent / empty for older servers. */
  commanderAbilities?: CommanderAbilitySnapshot[]
  /** Remaining merchant-reroll budget for this match. Drives the reroll
   *  button on neutral-shop buildings (enabled when > 0). Absent = 0. */
  shopRerollsRemaining?: number
  /** Cumulative per-player match metrics. Always present so the
   *  end-of-round recap (§15) can render per-player comparison columns
   *  regardless of who is viewing. Older servers omit this; consumers
   *  should default-coalesce. */
  metrics?: MatchMetricsSnapshot
}

export type PurchaseItemCommand = {
  type: 'purchase_item'
  buildingId: string
  itemId: string
}

export type RerollShopCommand = {
  type: 'reroll_shop'
  buildingId: string
}

export type PurchaseRecipeCommand = {
  type: 'purchase_recipe'
  buildingId: string
  recipeId: string
}

export type CraftItemCommand = {
  type: 'craft_item'
  recipeId: string
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

// Uses a consumable from the player's vault as a ground-targeted AoE at the
// given world point. Allied units within the item's range are affected; the
// effect amount is split across them unless the item def disables splitting.
export type UseItemAtCommand = {
  type: 'use_item_at'
  instanceId: number
  x: number
  y: number
}

// Uses a consumable from the player's vault directly on a single unit (the
// Vault "Items" section: drag a bag item onto a unit card). The unit receives
// the item's full effect (no AoE split) and one stack is consumed.
export type UseItemOnUnitCommand = {
  type: 'use_item_on_unit'
  instanceId: number
  unitId: number
}

export type TransferItemCommand = {
  type: 'transfer_item'
  fromUnitId: number
  fromSlotIdx: number
  toUnitId: number
  toSlotIdx: number
}

export type WaveUpgradeChoiceCommand = {
  type: 'wave_upgrade_choice'
  upgradeId: string
  targetUnitId: number
}

export type WaveUpgradeRerollCommand = {
  type: 'wave_upgrade_reroll'
}

// Sent when the player toggles the pause state from the in-match settings
// menu. Mirrors server protocol.SetPauseMessage.
export interface SetPauseMessage {
  type: 'set_pause'
  paused: boolean
}

export type ClientMessage =
  | JoinMatchMessage
  | RequestMapMessage
  | LeaveMatchMessage
  | MoveCommandMessage
  | GatherCommandMessage
  | PickupLootCommandMessage
  | DepositCommandMessage
  | TrainUnitCommandMessage
  | AttackCommandMessage
  | CastAbilityCommandMessage
  | CastCommanderAbilityCommandMessage
  | ToggleAutoCastCommandMessage
  | SetFocusTargetCommandMessage
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
  | GuardCommandMessage
  | PurchaseUpgradeCommand
  | CancelUpgradeCommand
  | UpgradeTownHallCommand
  | PurchaseItemCommand
  | RerollShopCommand
  | PurchaseRecipeCommand
  | CraftItemCommand
  | EquipItemCommand
  | UnequipItemCommand
  | UseConsumableCommand
  | UseItemAtCommand
  | UseItemOnUnitCommand
  | TransferItemCommand
  | WaveUpgradeChoiceCommand
  | WaveUpgradeRerollCommand
  | SetPauseMessage
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

/** ShieldPoolSnapshot is one source-specific shield pool the unit currently
 *  carries (e.g. dark_renewal). The aggregate shield/maxShield on Unit hold
 *  the totals; this slice gives the per-source breakdown for the unit-info
 *  tooltip ("Dark Renewal: 20 / 40"). sourceType is a stable wire id; the
 *  client maps it to a display label via a small lookup table. */
export type ShieldPoolSnapshot = {
  sourceType: string
  sourceUnitId?: number
  current: number
  max: number
  tags?: string[]
}

/** PerkCooldownSnapshot advertises a ticking cooldown on one of a unit's
 *  owned perks. The HUD renders a clock-wipe overlay covering fraction
 *  remaining / total of the icon, plus `ceil(remaining)` as a label. */
export type PerkCooldownSnapshot = {
  perkId: string
  remaining: number
  total: number
}

/** AbilitySnapshot is one of a unit's activatable abilities, with live
 *  auto-cast + cooldown state. Sent only for the owning player's units; the
 *  action bar renders a button per entry (left-click cast, right-click
 *  toggles auto-cast when supportsAutoCast). */
export type AbilitySnapshot = {
  id: string
  displayName?: string
  icon?: string
  manaCost?: number
  supportsAutoCast?: boolean
  /** Auto-cast currently enabled for this ability on this unit instance. */
  autoCast?: boolean
  cooldownRemaining?: number
  cooldownTotal?: number
  /** Number of targets this ability hits per cast. 1 for single-target (the
   *  default when the server omits the field); >1 for multi-target abilities
   *  like Greater Heal. The action-bar uses this to render a multi-target
   *  hint and the targeting cursor adapts accordingly. */
  targetCount?: number
  /** True while this ability is the unit's active channel. The action bar
   *  renders a distinct "channeling in progress" state (pulsing green border)
   *  when this is set. Omitted/false when not channeling. */
  channeling?: boolean
}

/**
 * BeamSnapshot carries one active channeled-beam visual to the client.
 * The beam is rendered as a persistent line/effect from the caster to the
 * target for the duration of the channel. Position data is intentionally
 * absent — the beam stretches dynamically between the current positions of
 * casterUnitId and targetUnitId, both of which are in the unit list each frame.
 * FOW-filtered: only included when the caster or target is visible to the viewer.
 */
export type BeamSnapshot = {
  id: string
  casterUnitId: number
  targetUnitId: number
  ownerId: string
  abilityId?: string
  /** Visual variant tag. "siphon_life" = necrotic green drain beam. */
  variant?: string
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
  /** Inclusive frame range the client one-way loops through on the unit's
   *  casting sprite sheet while it is channeling a beam ability. start ==
   *  end produces a single held frame; start < end produces a small loop
   *  (e.g. 3 → 4 → 3 → 4 …) at the unit's normal frame cadence. Both fields
   *  only present when status === 'Channeling'; absent otherwise. Out-of-
   *  range values modulo against the sheet's frame count at render time. */
  channelLoopStart?: number
  channelLoopEnd?: number
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
  /** Advancement-granted extra perk slot counts, keyed by tier.
   *  Mirrors server `UnitSnapshot.ExtraPerkSlots`. Populated only when the
   *  unit's owner has a `unitExtraPerkSlot` advancement (e.g. Twin Bronze)
   *  for this unit type. Value is the count of EXTRA slots at that tier
   *  (1 for Twin Bronze; future Triple Bronze would set 2). Absent /
   *  undefined for units whose owner has no such advancement. The HUD uses
   *  this to render extra perk cells beyond the standard 3 — locked icon
   *  before the perk is granted, perk icon after. */
  extraPerkSlots?: Record<string, number>
  /** Aggregate temporary HP pool — sum of every active shield source on the
   *  unit (legacy single Unit.Shield from blood_engine + every source-specific
   *  pool). 0/undefined when the unit has no active shield at all. */
  shield?: number
  /** Aggregate max shield — sum of every active source's cap. The HUD's
   *  "Shield X / Y" line uses these two values for the totals; the per-source
   *  breakdown lives in shieldPools below. */
  maxShield?: number
  /** Per-source shield pool breakdown. Each entry is one independently-capped
   *  pool (e.g. dark_renewal) the unit currently carries. Omitted when the
   *  unit has no source-specific pools. The legacy blood_engine pool is NOT
   *  reflected here — it's represented only in the aggregate shield/maxShield
   *  above. The unit-info tooltip iterates this slice to surface "Dark
   *  Renewal: 20/40" etc. */
  shieldPools?: ShieldPoolSnapshot[]
  /** Current mana for spellcaster units (e.g. acolyte). Omitted (0) for
   *  non-casters. */
  mana?: number
  /** Max mana pool. 0/undefined for units that have no mana. Drives the
   *  blue mana bar under the HP bar. */
  maxMana?: number
  /** Base passive mana regen in mana/second. Omitted when 0. Excludes
   *  conditional aura bonuses (e.g. Mana Conduit) — those live in their
   *  perk tooltip, not the unit's intrinsic stat row. */
  manaRegen?: number
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
  /** Activatable abilities (owned units only) for the action bar. */
  abilities?: AbilitySnapshot[]
  /** Legacy: was the link to a VictoryCondition by objectiveId. The
   *  campaign-objectives-and-metrics §6 migration removed MapConfig.VictoryConditions,
   *  so this field is no longer authored by maps. Section 9 drops the
   *  server-side hooks; this client field is retained on the wire until §10. */
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
  /** Focus Target — the ally ID this unit (typically a Cleric) is following
   *  and prioritising for support casts. Paired with `order === 'focus_follow'`
   *  while focus is active; 0/absent means no focus. The client uses this to
   *  drive the Focus Target button's highlight and the selection-HUD focus
   *  indicator. */
  focusTargetId?: number
  /** Non-empty when this unit has a pending pickup-loot order and is en route
   *  to a chest. The value is the LootDrop.ID the unit is walking toward.
   *  Cleared when the order finishes or is replaced by another order. */
  pickupLootId?: string
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
  /** Content-addressed map identity, always present. */
  mapId: MapId
  contentHash: string
  /** base64(gzip(flat MapConfig JSON)). Present ONLY on a cache miss; on a hit
   *  the client renders the map from its own content-addressed cache. */
  mapGz?: string
}

/** Server→client reply to RequestMapMessage: the full map, gzip+base64 (same
 *  form as WelcomeMessage.mapGz). */
export type MapContentMessage = {
  type: 'map_content'
  mapId: MapId
  contentHash: string
  mapGz: string
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

export interface UpgradeOffer {
  id: string
  group: string
  name: string
  description: string
  rarity: 'common' | 'rare' | 'epic' | 'legendary'
  scope: string
  stackCurrent: number
  stackMax: number
  requiresTargetUnit?: boolean
}

export interface WaveUpgradeOfferSnapshot {
  wave: number
  offers: UpgradeOffer[]
  rerollsLeft: number
  deadlineMs: number
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
  /** This viewer's own per-match earned dominion points. Present only at
   *  game-over. A remote joiner persists this into its own local profile. */
  yourDominionPointsEarned?: number
}

/** Per-tick wire shape of one player's cumulative match metrics. Mirror of
 *  the server's `protocol.MatchMetricsSnapshot`; carried inside
 *  `PlayerSnapshot.metrics` on every snapshot so the end-of-round recap
 *  (§15) can render per-player comparison columns regardless of who is
 *  viewing. Maps may be empty / undefined on the wire — consumers should
 *  default-coalesce with `?? {}`. */
export type MatchMetricsSnapshot = {
  totalGoldEarned: number
  totalWoodEarned: number
  totalEnemiesKilled: number
  buildingsBuilt: number
  buildingsBuiltByType: Record<string, number>
  neutralCampsKilled: number
  /** Keys are numeric strings (server-side `map[int]int`); use bracket
   *  notation with the tier number converted to string. */
  neutralCampsKilledByTier: Record<string, number>
  unitsTrained: number
  unitsTrainedByType: Record<string, number>
  unitsByRank: Record<string, number>
  wavesCleared: number
}

/** Per-tick wire shape of one campaign objective's state from the viewer's
 *  perspective. Reshaped in §10 of campaign-objectives-and-metrics to
 *  carry the richer state needed by the in-match HUD and end-of-round
 *  recap (description, scope, required, current vs required count,
 *  completed/failed). The legacy `label`/`progress`/`count` fields were
 *  dropped alongside the legacy victoryConditions system in §6. */
export type ObjectiveSnapshot = {
  id: string
  /** Handler dispatch key: `kill_camps` | `build_buildings` |
   *  `collect_resource` | `kill_camps_before_wave` | `rank_units` |
   *  `survive_waves`. String not enum so adding handlers server-side
   *  doesn't require client lockstep updates. */
  type: string
  description?: string
  scope: 'team' | 'player'
  required?: boolean
  current: number
  requiredCount: number
  completed: boolean
  /** Only set by time-boxed objectives that missed their deadline. Sticky. */
  failed?: boolean
  /** DP reward this objective grants on first-ever completion. Echoed back to
   *  the server with the match-end completion POST. Absent/0 = no reward. */
  rewardDominionPoints?: number
  /** Conquest Badge reward for first-ever completion. Echoed back to the server
   *  with the match-end completion POST. Absent/0 = no reward. */
  rewardConquestBadges?: number
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
  /** Per-shot render-size multiplier applied on top of the base
   *  projectile-sprite scale (same role as TrapSnapshot.scaleMultiplier).
   *  Resolved server-side from the firing unit's `projectileScale`, so two
   *  units firing the same projectile can draw it at different sizes.
   *  Absent / 0 ⇒ 1× (no change). */
  scale?: number
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
 * DamageTypeHintSnapshot tags a chunk of HP loss with its damage type so the
 * client can COLOR the existing major (floating-up) damage popup it derives
 * from HP-diff. Unlike MinorDamageEventSnapshot, this does NOT spawn an
 * extra popup — it only changes the color of the popup that would already
 * appear.
 *
 * Auto-emitted by the server damage pipeline whenever a typed DamageSource
 * (shadow, fire, holy, lightning, …) lands HP loss. Unmatched hints are
 * safe to silently drop; the popup then keeps its default white/red.
 *
 * `variant` shares the same palette as MinorDamageEventSnapshot.variant so
 * the same damage type reads the same color in both the major popup
 * (floating up) and the minor splash popup (drifting sideways).
 */
export type DamageTypeHintSnapshot = {
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
 * HealEventSnapshot is a per-tick record of intentional healing landing on a
 * unit (the heal ability). The client resolves the unit's live position by
 * `unitId` and spawns a light-green "+amount" floating number over it. Passive
 * HP regen is intentionally not reported. Drained per tick like
 * CritEventSnapshot; absent when no heals land.
 */
export type HealEventSnapshot = {
  unitId: number
  amount: number
}

/**
 * ManaRestoreEventSnapshot mirrors HealEventSnapshot for intentional mana
 * grants (Repurposed Life, future cleric mana abilities). The client spawns
 * a blue "+amount" floating popup over the recipient. Passive mana regen is
 * intentionally not reported here — it would spam +1s at the natural 0.2/s
 * rate. Only grants that route through the server's addUnitManaLocked
 * helper emit a popup.
 */
export type ManaRestoreEventSnapshot = {
  unitId: number
  amount: number
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
  /** Where the effect renders relative to its anchor unit: "center"
   *  (default / absent), "feet", or "head". Absent/"center" preserves the
   *  historical origin placement (so existing perk effects are unchanged);
   *  "feet"/"head" shift vertically using the unit's bounds. */
  anchor?: 'center' | 'feet' | 'head' | ''
}

export type MatchSnapshotMessage = {
  type: 'match_snapshot'
  tick: number
  serverNow: number
  matchId: string
  buildings: BuildingTile[]
  /** Obstacle IDs that have been removed from the world since the previous
   *  broadcast (trees chopped, rocks mined to depletion). Only present on
   *  broadcasts that follow a server-side removal; the steady state ships
   *  nothing in this field. Clients maintain their obstacle mirror from the
   *  WelcomeMessage's MapConfig and apply removals listed here. Replaces
   *  the per-tick full-list resend that previously cost ~870KB/snapshot on
   *  the exploration map. */
  obstaclesRemoved?: string[]
  /** Live obstacle metadata patches whose value changed since the previous
   *  broadcast — currently only tree currentWorkers (worker count) updates.
   *  maxWorkers is constant per obstacle type and known from welcome, so
   *  it is not resent. Empty/absent on the steady state. */
  obstacleMetadata?: ObstacleMetadataPatch[]
  players: PlayerSnapshot[]
  units: UnitSnapshot[]
  wave: WaveSnapshot
  banners?: BannerSnapshot[]
  traps?: TrapSnapshot[]
  projectiles?: ProjectileSnapshot[]
  beams?: BeamSnapshot[]
  effects?: EffectSnapshot[]
  critEvents?: CritEventSnapshot[]
  minorDamageEvents?: MinorDamageEventSnapshot[]
  damageTypeHints?: DamageTypeHintSnapshot[]
  lethalDamageEvents?: LethalDamageEventSnapshot[]
  healEvents?: HealEventSnapshot[]
  manaRestoreEvents?: ManaRestoreEventSnapshot[]
  // Present only when the active map has debug.battleTracker=true. Absent
  // otherwise — the client treats absence as "debug tracker disabled".
  battleTracker?: BattleTrackerSnapshot
  gameOver?: GameOverSnapshot
  victory?: VictorySnapshot
  fow?: FogOfWarSnapshot
  waveUpgrade?: WaveUpgradeOfferSnapshot
  neutralCamps?: NeutralCampSnapshot[]
  // Ground-loot chests currently on the map. Sent unfiltered — always
  // visible regardless of FOW so the minimap POI dot is always shown.
  lootDrops?: LootDropSnapshot[]
  // Server-side pause flag. When true the simulation is frozen; the client
  // shows a paused overlay and freezes the visible wave-upgrade timer.
  paused?: boolean
  // Player ID that initiated the pause (empty/absent when not paused).
  pausedBy?: string
  /** Per-tick zone ownership/capture state. Parallel to MapConfig.zones
   *  (same ids, same order). Absent when the map has no zones. */
  zones?: ZoneSnapshot[]
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
// other; this ID is hostile to them. Mirrors enemyPlayerID on the server.
export const ENEMY_PLAYER_ID = '__enemy__'

// Sentinel owner ID for neutral camp mobs. Hostile to every real player team
// and to the wave-enemy AI; not allied with anyone (not even other neutrals
// for ally-scoring purposes). Mirrors neutralPlayerID on the server.
export const NEUTRAL_PLAYER_ID = '__neutral__'

// Sentinel zone owner for the human (co-op) team. Every zone capture and every
// locked home zone resolves to this; allied with every non-AI player. Mirrors
// protocol.ZoneCaptureTeamOwner on the server.
export const ZONE_TEAM_OWNER = 'team'

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
  | MapContentMessage
  | MatchSnapshotMessage
  | ErrorMessage
  | NotificationMessage
  | LootCollectedNotification
  | PingMessage
