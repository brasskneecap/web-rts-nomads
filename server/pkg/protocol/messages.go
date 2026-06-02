package protocol

import "encoding/json"

// Order string constants used in UnitSnapshot.Order and mirrored in the
// TypeScript client. Defined once here so both the server and the frontend
// share a single source of truth for the wire values.
const (
	OrderStringIdle         = "idle"
	OrderStringMove         = "move"
	OrderStringAttackMove   = "attack_move"
	OrderStringAttackTarget = "attack_target"
	OrderStringHold         = "hold"
	OrderStringPatrol       = "patrol"
	// OrderStringFocusFollow is the wire value for the Focus Target order — the
	// Cleric follows and prioritises healing a chosen ally. Mirrors the Go-side
	// OrderFocusFollow constant; both must match the TypeScript client's
	// OrderType enum addition (task 10.4).
	OrderStringFocusFollow  = "focus_follow"
	OrderStringPickupLoot   = "pickup_loot"
)

type Vec2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WaveConfig holds per-map tuning for wave mode. Omitted or zero values fall
// back to server defaults (60 s prep, 120 s active, totalWaves derived from
// the highest waveNumber found on spawn points).
type WaveConfig struct {
	TotalWaves   int     `json:"totalWaves,omitempty"`
	PrepDuration float64 `json:"prepDuration,omitempty"`
	WaveDuration float64 `json:"waveDuration,omitempty"`
}

// VictoryCondition defines a single win objective for a map. All conditions
// in the slice must be completed simultaneously for victory to be declared.
// Type is one of: "killUnit" | "destroyBuilding" | "surviveWaves".
// Buildings and enemy-spawnpoints link to a condition via metadata["objectiveId"].
type VictoryCondition struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
	// Count is the number of kills required for a "killUnit" objective (default 1).
	// Unused by "destroyBuilding" and "surviveWaves".
	Count int `json:"count,omitempty"`
}

// PlacedUnit is a statically authored unit in the map. PlayerSlot determines
// who controls the unit at runtime: "player1", "player2", ... spawn when that
// player joins the matching slot; "enemy" spawns at match start as a
// stationary guard. The unit type implies its faction (raider / neutral /
// human) — faction is intrinsic to the UnitDef and is not stored per instance.
type PlacedUnit struct {
	GridCoord
	ID         string  `json:"id"`
	PlayerSlot string  `json:"playerSlot"`
	UnitType   string  `json:"unitType"`
	AggroRange float64 `json:"aggroRange,omitempty"`
	LeashRange float64 `json:"leashRange,omitempty"`
}

// UnmarshalJSON accepts both the current shape (`playerSlot`) and the legacy
// shape (`owner` + `playerLabel`) so existing map JSONs continue to load
// without a one-time rewrite. The legacy `owner` field maps as:
//   - "enemy"  → playerSlot "enemy"
//   - "player" → playerSlot = legacy `playerLabel` (e.g. "player1")
//
// Maps re-saved through the editor are written in the new shape.
func (p *PlacedUnit) UnmarshalJSON(data []byte) error {
	type rawShape struct {
		GridCoord
		ID          string  `json:"id"`
		PlayerSlot  string  `json:"playerSlot"`
		Owner       string  `json:"owner"`
		PlayerLabel string  `json:"playerLabel"`
		UnitType    string  `json:"unitType"`
		AggroRange  float64 `json:"aggroRange"`
		LeashRange  float64 `json:"leashRange"`
	}
	var raw rawShape
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.GridCoord = raw.GridCoord
	p.ID = raw.ID
	p.UnitType = raw.UnitType
	p.AggroRange = raw.AggroRange
	p.LeashRange = raw.LeashRange
	switch {
	case raw.PlayerSlot != "":
		p.PlayerSlot = raw.PlayerSlot
	case raw.Owner == "enemy":
		p.PlayerSlot = "enemy"
	case raw.Owner == "player":
		p.PlayerSlot = raw.PlayerLabel
	}
	return nil
}

// NeutralSpawn is a map-authored tile that materializes a guard squad of
// "neutral" units between waves. The squad despawns instantly when a wave
// starts and respawns when the wave clears. Composition is drawn from a
// tier file in catalog/neutral_groups/. See neutral_group_defs.go for the
// runtime loader and state_neutral_camps.go for the lifecycle.
//
// GroupID is either a specific group id (e.g. "small_raider_group") or the
// sentinel "__random__" to roll a random group from the current tier each
// respawn.
//
// StartingTier defaults to 1. TierUpEveryNWaves = 0 disables auto-scaling.
// Aggro/leash and the four per-wave scaling fields mirror enemy-spawnpoint
// semantics (see state_waves.go computeWaveStatScalingLocked) so authors
// have a consistent mental model.
type NeutralSpawn struct {
	GridCoord
	ID                      string  `json:"id"`
	GroupID                 string  `json:"groupId"`
	StartingTier            int     `json:"startingTier,omitempty"`
	TierUpEveryNWaves       int     `json:"tierUpEveryNWaves,omitempty"`
	AggroRange              float64 `json:"aggroRange,omitempty"`
	LeashRange              float64 `json:"leashRange,omitempty"`
	HealthMultiplier        float64 `json:"healthMultiplier,omitempty"`
	HealthMultiplierPerWave float64 `json:"healthMultiplierPerWave,omitempty"`
	DamageMultiplier        float64 `json:"damageMultiplier,omitempty"`
	DamageMultiplierPerWave float64 `json:"damageMultiplierPerWave,omitempty"`
}

// NeutralSpawnRandomGroupID is the sentinel GroupID value meaning "pick a
// random group from the current tier each time the camp respawns."
const NeutralSpawnRandomGroupID = "__random__"

type MapConfig struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Size        string             `json:"size"`
	Width       float64            `json:"width"`
	Height      float64            `json:"height"`
	GridCols    int                `json:"gridCols"`
	GridRows    int                `json:"gridRows"`
	CellSize    float64            `json:"cellSize"`
	Terrain     []TerrainTile      `json:"terrain"`
	Tiles       []TileInstance     `json:"tiles,omitempty"`
	DefaultTile *TileCoord         `json:"defaultTile,omitempty"`
	Obstacles   []ObstacleTile     `json:"obstacles"`
	Buildings   []BuildingTile     `json:"buildings"`
	PlacedUnits   []PlacedUnit   `json:"placedUnits,omitempty"`
	NeutralSpawns []NeutralSpawn `json:"neutralSpawns,omitempty"`
	WaveConfig    *WaveConfig    `json:"waveConfig,omitempty"`
	// VictoryConditions lists the objectives that must ALL be completed for the
	// player to win. Omitted or empty means no server-managed win condition
	// (the legacy wave-complete client check still works for wave maps).
	VictoryConditions []VictoryCondition `json:"victoryConditions,omitempty"`
	Debug             *MapDebugConfig    `json:"debug,omitempty"`
}

// MapDebugConfig is the container for per-map debug/telemetry opt-ins. It
// lives on the map JSON so non-debug maps pay no cost and production maps can
// stay untouched. New debug features plug in here as additional bool fields.
type MapDebugConfig struct {
	// BattleTracker, when true, enables server-side aggregation of per-player
	// damage + kills bucketed by source kind (unit / trap / building) and
	// subtype (unit type, trap type, building type). Tracker data is included
	// in every match snapshot under BattleTracker and rendered by the client
	// in a collapsible debug panel. Use for balance tuning and regression
	// spotting when new units / traps / enemies are added.
	BattleTracker bool `json:"battleTracker,omitempty"`

	// DebugSpawn, when true, enables the in-game "spawn enemy with perks" dev
	// tool. The server will honor `debug_spawn_unit` commands from any client
	// joined to this map and place a fully-configured enemy unit at the
	// requested world coordinates. Hard-gated server-side: the command is a
	// no-op and logs a warning when this flag is false, so production maps
	// can't be exploited.
	DebugSpawn bool `json:"debugSpawn,omitempty"`
}

type GridCoord struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ActiveEffectIcon is one entry in a unit's ActiveBuffs / ActiveDebuffs list.
// ID is the perk id (for buffs) or raw icon id (for debuffs) that identifies
// which icon artwork to render. Stacks is the number of simultaneous sources
// contributing the effect — omitted from JSON when 1 so single-instance
// effects stay compact on the wire. The client renders a small count badge
// over the icon whenever Stacks >= 2.
type ActiveEffectIcon struct {
	ID     string `json:"id"`
	Stacks int    `json:"stacks,omitempty"`
}

// ShieldPoolSnapshot is one source-specific shield pool the unit currently
// carries (e.g. dark_renewal). The client uses this to render a per-source
// breakdown in the unit info tooltip ("Dark Renewal: 20 / 40"). The
// aggregate Shield / MaxShield fields on UnitSnapshot still hold the totals
// so callers that only care about the combined amount don't need to walk
// this slice.
//
// Fields:
//
//	SourceType   — namespacing tag (e.g. "dark_renewal"). The client maps
//	               this to a display label via a small lookup table.
//	SourceUnitID — granting unit id (Siphoner id for dark_renewal). Lets the
//	               HUD highlight the granting ally if desired.
//	Current      — current shield value remaining in this pool.
//	Max          — per-source cap on this pool.
//	Tags         — free-form category tags ("corruption", "siphoner", …),
//	               reserved for future per-tag interactions.
type ShieldPoolSnapshot struct {
	SourceType   string   `json:"sourceType"`
	SourceUnitID int      `json:"sourceUnitId,omitempty"`
	Current      int      `json:"current"`
	Max          int      `json:"max"`
	Tags         []string `json:"tags,omitempty"`
}

// EffectiveTrapSnapshot carries the live compounded trap stats for an
// archer/trapper unit to the client on every tick. All multiplier effects
// (extended_setup, wider_nets, amplified_effects, rapid_deployment, and the
// trap-specific silver upgrades) are already baked in — clients can render
// these numbers directly in the tooltip without any further math.
//
// Only populated for archer units on the trapper path that own a bronze trap
// perk. Nil / absent for all other units.
//
// BurstDamage is an int on the wire (rounded server-side); all other numeric
// fields are float64. Fields that are 0 for the current trap type are omitted
// from JSON (omitempty) so the payload stays compact.
type EffectiveTrapSnapshot struct {
	// PerkID is the bronze trap perk id (e.g. "caltrops", "fire_pit").
	PerkID string `json:"perkId"`
	// Global modifiers (always present for the trap's own type):
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Radius          float64 `json:"radius,omitempty"`
	TriggerRadius   float64 `json:"triggerRadius,omitempty"`  // explosive_trap only
	PlaceInterval   float64 `json:"placeInterval,omitempty"`
	DamagePerSecond float64 `json:"damagePerSecond,omitempty"` // caltrops, fire_pit
	BurstDamage     int     `json:"burstDamage,omitempty"`     // explosive_trap
	SlowMultiplier  float64 `json:"slowMultiplier,omitempty"`  // caltrops
	MarkMultiplier  float64 `json:"markMultiplier,omitempty"`  // marker_trap
	MarkDuration    float64 `json:"markDuration,omitempty"`    // marker_trap
	// Silver trap-specific upgrade stats (zero/omitted when the gating perk is absent):
	BarbedFieldRampPerSec     float64 `json:"barbedFieldRampPerSec,omitempty"`     // caltrops + barbed_field
	BarbedFieldMaxBonusDPS    float64 `json:"barbedFieldMaxBonusDPS,omitempty"`    // caltrops + barbed_field
	ExposedWeakenedMultiplier float64 `json:"exposedWeakenedMultiplier,omitempty"` // marker_trap + exposed_weakness
	LastingFlamesBurnDuration float64 `json:"lastingFlamesBurnDuration,omitempty"` // fire_pit + lasting_flames
	AftershockDelaySeconds    float64 `json:"aftershockDelaySeconds,omitempty"`    // explosive_trap + explosive_chain
}

// PerkCooldownSnapshot advertises how long until a perk's next activation.
// PerkID matches an entry in the unit's PerkIDs list. Remaining is the
// live countdown in seconds; Total is the full cooldown duration (rank- and
// modifier-adjusted) so the client can render the correct wipe fraction.
type PerkCooldownSnapshot struct {
	PerkID    string  `json:"perkId"`
	Remaining float64 `json:"remaining"`
	Total     float64 `json:"total"`
}

// AbilitySnapshot carries one of a unit's abilities to the client so the
// action bar can render its button, auto-cast glow, and cooldown overlay.
// Sent only for the owning player's units (the action bar is only shown for
// your own selection). CooldownRemaining/Total drive the same clock-wipe
// overlay the perk cooldowns use.
type AbilitySnapshot struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Icon        string `json:"icon,omitempty"`
	ManaCost    int    `json:"manaCost,omitempty"`
	// TargetCount is the number of targets this ability can affect per cast.
	// Always >= 1; single-target abilities report 1 so the client can skip the
	// multi-target indicator without special-casing "field absent".
	TargetCount       int     `json:"targetCount,omitempty"`
	SupportsAutoCast  bool    `json:"supportsAutoCast,omitempty"`
	AutoCast          bool    `json:"autoCast,omitempty"` // auto-cast currently enabled
	CooldownRemaining float64 `json:"cooldownRemaining,omitempty"`
	CooldownTotal     float64 `json:"cooldownTotal,omitempty"`
	// Channeling is true when this ability is the unit's active channel.
	// The action bar uses this to render the "channeling in progress" state
	// (e.g. a pulsing overlay instead of the cooldown wipe).
	Channeling bool `json:"channeling,omitempty"`
}

// BeamSnapshot carries one active channeled-beam visual to the client.
// The beam is rendered as a persistent line/effect between caster and target
// for the duration of the channel. FOW-filtered: only included when the
// caster or target is visible to the viewer.
type BeamSnapshot struct {
	ID           string `json:"id"`
	CasterUnitId int    `json:"casterUnitId"`
	TargetUnitId int    `json:"targetUnitId"`
	OwnerId      string `json:"ownerId"`
	AbilityId    string `json:"abilityId,omitempty"`
	Variant      string `json:"variant,omitempty"`
}

type TerrainTile struct {
	GridCoord
	Terrain string `json:"terrain"`
}

type TileCoord struct {
	Sheet string `json:"sheet"`
	SX    int    `json:"sx"`
	SY    int    `json:"sy"`
}

type TileInstance struct {
	GridCoord
	TileCoord
}

type ObstacleTile struct {
	GridCoord
	ID             string                 `json:"id,omitempty"`
	Obstacle       string                 `json:"obstacle"`
	Width          int                    `json:"width,omitempty"`
	Height         int                    `json:"height,omitempty"`
	Capabilities   []string               `json:"capabilities,omitempty"`
	ResourceType   string                 `json:"resourceType,omitempty"`
	ResourceAmount int                    `json:"resourceAmount,omitempty"`
	Hp             float64                `json:"hp,omitempty"`
	MaxHp          float64                `json:"maxHp,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ObstacleMetadataPatch is a per-tick patch for an obstacle whose live
// metadata changed since the previous broadcast. Currently only emitted for
// tree obstacles whose currentWorkers count changed (a worker entered or
// left a tree). MaxWorkers is constant per type and known to the client
// from the WelcomeMessage, so we don't re-send it. Steady-state ticks emit
// nothing; this list is empty on most snapshots.
type ObstacleMetadataPatch struct {
	ID             string `json:"id"`
	CurrentWorkers int    `json:"currentWorkers"`
}

type BuildingTile struct {
	GridCoord
	ID             string                 `json:"id"`
	BuildingType   string                 `json:"buildingType"`
	Width          int                    `json:"width"`
	Height         int                    `json:"height"`
	Occupied       bool                   `json:"occupied"`
	Visible        bool                   `json:"visible"`
	OwnerID        *string                `json:"ownerId,omitempty"`
	Capabilities   []string               `json:"capabilities"`
	ResourceType   string                 `json:"resourceType,omitempty"`
	ResourceAmount int                    `json:"resourceAmount,omitempty"`
	SpawnUnitTypes []string               `json:"spawnUnitTypes,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	// Ghost is set on buildings that are included in a FOW-filtered snapshot
	// because the viewer has seen the cell before but it is currently in shroud.
	// The client should render the last-known appearance without live state.
	Ghost        bool `json:"ghost,omitempty"`
	LastSeenTick int  `json:"lastSeenTick,omitempty"`

	// ─── Shop fields (per-building-shop-inventories) ───────────────────────
	// ShopInventory is the runtime list of items this building sells, with
	// per-item quantity remaining. Populated by populateShopInventoriesLocked
	// at match start and refilled by a successful reroll. Each entry's
	// Quantity decrements on purchase; at 0 the entry stays in the list so
	// the client can render it disabled (greyed-out).
	ShopInventory []ShopStockEntry `json:"shopInventory,omitempty"`
	// ShopLootTableID, when set, is the key into the loot-table catalog
	// rolled into ShopInventory at match start. Authored (map JSON) field;
	// mutually exclusive with ShopFixedInventory.
	ShopLootTableID string `json:"shopLootTableId,omitempty"`
	// ShopFixedInventory, when set, is copied verbatim into ShopInventory
	// at match start (each entry gets the building-type's starter quantity).
	// Authored (map JSON) field; takes precedence over ShopLootTableID.
	ShopFixedInventory []string `json:"shopFixedInventory,omitempty"`
	// ShopGuardUnitIDs is the list of unit IDs spawned to guard this
	// building. While any ID resolves to a unit with HP > 0, the shop is
	// considered locked. Runtime field; populated by spawnShopGuardsLocked.
	ShopGuardUnitIDs []int `json:"shopGuardUnitIds,omitempty"`
	// ShopLocked is a per-snapshot, computed field set by the snapshot
	// filter from shopLockedLocked(building). Always emitted for shop
	// buildings so the client doesn't need to default it.
	ShopLocked bool `json:"shopLocked,omitempty"`
	// ShopDiscovered is a per-viewer field set by the snapshot filter
	// from PlayerFOW.KnownBuildings[building.ID] != nil. True for any
	// shop the viewer has ever revealed (including their own).
	ShopDiscovered bool `json:"shopDiscovered,omitempty"`
}

// ShopStockEntry is one item slot in a shop building's inventory.
// Quantity decrements on each successful purchase; when it reaches 0 the
// entry stays in the list (so the client can render it greyed) but
// further purchases of that item from that shop are rejected.
type ShopStockEntry struct {
	ItemID   string `json:"itemId"`
	Quantity int    `json:"quantity"`
}

type JoinMatchMessage struct {
	Type              string         `json:"type"`
	PlayerID          string         `json:"playerId"`
	MapID             string         `json:"mapId"`
	MatchID           string         `json:"matchId,omitempty"`
	OwnedUpgradeRanks map[string]int `json:"ownedUpgradeRanks,omitempty"`
	ActiveUpgradeIDs  []string       `json:"activeUpgradeIds,omitempty"`
	// AcquiredAdvancementIDs is the sorted list of advancement node IDs the
	// player currently owns (extracted from PlayerProfile.AcquiredAdvancements
	// by the client at join time). Nil / absent means no advancements.
	AcquiredAdvancementIDs []string `json:"acquiredAdvancementIds,omitempty"`
}

type LeaveMatchMessage struct {
	Type     string `json:"type"`
	PlayerID string `json:"playerId"`
	MatchID  string `json:"matchId"`
}

type MoveCommandMessage struct {
	Type        string `json:"type"`
	UnitIDs     []int  `json:"unitIds"`
	Destination Vec2   `json:"destination"`
}

type GatherCommandMessage struct {
	Type     string `json:"type"`
	UnitIDs  []int  `json:"unitIds"`
	TargetID string `json:"targetId"`
}

// PickupLootCommandMessage is the right-click "go collect that chest"
// order. Mirrors GatherCommandMessage exactly so transport/replay
// layers handle it uniformly. Server validates ownership + chest existence.
type PickupLootCommandMessage struct {
	Type     string `json:"type"`
	UnitIDs  []int  `json:"unitIds"`
	TargetID string `json:"targetId"` // LootDrop.ID
}

// DepositCommandMessage is the player-directed "drop off carried resources at
// THIS specific deposit-point building" command. Workers without carried
// resources in UnitIDs are silently ignored server-side; the client routes
// them through a separate move command (see InputManager.onRightClick).
type DepositCommandMessage struct {
	Type       string `json:"type"`
	UnitIDs    []int  `json:"unitIds"`
	BuildingID string `json:"buildingId"`
}

type TrainUnitCommandMessage struct {
	Type       string `json:"type"`
	UnitType   string `json:"unitType"`
	BuildingID string `json:"buildingId"`
}

type AttackCommandMessage struct {
	Type         string `json:"type"`
	UnitIDs      []int  `json:"unitIds"`
	TargetUnitID int    `json:"targetUnitId"`
}

// CastCommanderAbilityCommandMessage is a player-level "commander" ability
// cast — the player is the implicit caster (no unit selection required) and
// the ability resolves at the supplied world position. Server validates the
// player's cooldown on the chosen ability; rejection comes back via a
// NotificationMessage (same pattern as CastAbilityCommandMessage).
type CastCommanderAbilityCommandMessage struct {
	Type      string  `json:"type"`
	AbilityID string  `json:"abilityId"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

// CastAbilityCommandMessage is the action-bar standard-cast (left-click):
// caster casts AbilityID at TargetUnitID. The server validates ownership,
// targeting, range, and mana; on failure it replies with a
// NotificationMessage (same pattern as train_unit_command).
type CastAbilityCommandMessage struct {
	Type         string `json:"type"`
	CasterUnitID int    `json:"casterUnitId"`
	AbilityID    string `json:"abilityId"`
	TargetUnitID int    `json:"targetUnitId"`
}

// SetFocusTargetCommandMessage sets or clears a Cleric's sticky focus target.
// Type tag: "set_focus_target_command". TargetUnitID == 0 means "clear focus".
// The server validates match membership and caster ownership before applying.
// Validation failures are reported via NotificationMessage.
type SetFocusTargetCommandMessage struct {
	Type         string `json:"type"`
	CasterUnitID int    `json:"casterUnitId"`
	TargetUnitID int    `json:"targetUnitId"` // 0 = clear focus
}

// ToggleAutoCastCommandMessage is the action-bar auto-cast toggle
// (right-click) for UnitID's AbilityID. Silent no-op when the ability does
// not support auto-cast or the unit is not owned by the sender.
type ToggleAutoCastCommandMessage struct {
	Type      string `json:"type"`
	UnitID    int    `json:"unitId"`
	AbilityID string `json:"abilityId"`
}

type AttackMoveCommandMessage struct {
	Type        string `json:"type"`
	UnitIDs     []int  `json:"unitIds"`
	Destination Vec2   `json:"destination"`
}

// SetStanceCommandMessage instructs the server to change the standing order for
// the given units to a non-movement stance. Stance must be "hold" or "idle".
type SetStanceCommandMessage struct {
	UnitIDs []int  `json:"unitIds"`
	Stance  string `json:"stance"` // "hold" | "idle"
}

// PatrolCommandMessage issues a patrol order to the given units. The unit's
// current position becomes one waypoint; Destination becomes the other.
// Units with the "attack" capability only (mirrors AttackMoveCommandMessage).
type PatrolCommandMessage struct {
	UnitIDs     []int `json:"unitIds"`
	Destination Vec2  `json:"destination"`
}

type CancelTrainingCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
	// Index of the queue entry to cancel. 0 = currently-training unit (the
	// existing "X" cancel button); > 0 = a queued unit waiting behind the
	// leader (player left-clicked a queue slot). Omitted on old clients;
	// absence is treated as 0 to preserve the prior "cancel current" behavior.
	QueueIndex int `json:"queueIndex,omitempty"`
}

type SetBuildingSpawnPointCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
	Point      Vec2   `json:"point"`
}

type BuildBuildingCommandMessage struct {
	Type         string `json:"type"`
	BuildingType string `json:"buildingType"`
	UnitIDs      []int  `json:"unitIds"`
	GridX        int    `json:"gridX"`
	GridY        int    `json:"gridY"`
}

type RepairCommandMessage struct {
	Type       string `json:"type"`
	UnitIDs    []int  `json:"unitIds"`
	BuildingID string `json:"buildingId"`
}

type KickBuildersCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
}

type DemolishBuildingCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
}

type ClientMessage struct {
	Type string `json:"type"`
}

// HelloMessage is the first message the SPA sends after the WebSocket upgrade
// completes. The server compares Version against its own compiled version and
// closes the connection with a defined close code (4000) if they differ; the
// SPA renders that as the "Build mismatch — please restart" modal. Sending
// HelloMessage is optional for non-browser peers (transportbridge sends its
// own version handshake — §13 task 13.7).
type HelloMessage struct {
	Type    string `json:"type"`    // "hello"
	Version string `json:"version"` // SPA build version (from __APP_VERSION__)
}

type ResourceStock struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Amount int    `json:"amount"`
	Max    *int   `json:"max,omitempty"`
	Accent string `json:"accent"`
}

// PurchaseUpgradeCommandMessage requests a permanent upgrade purchase for a
// unit track. Track must match an UpgradeTrack constant ("soldier" or "archer").
type PurchaseUpgradeCommandMessage struct {
	Type  string `json:"type"`
	Track string `json:"track"`
}

// UpgradeTownHallCommandMessage requests a tier-up on the specified town hall.
// BuildingID must be the ID of a townhall the player owns.
type UpgradeTownHallCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
}

// PurchaseItemCommandMessage requests buying an item from a marketplace building.
// BuildingID must be the ID of a building with the "item-purchase" capability.
// ItemID must match an entry in the item catalog.
type PurchaseItemCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
	ItemID     string `json:"itemId"`
}

// RerollShopCommandMessage requests rerolling the inventory of a neutral
// shop building. BuildingID must reference a building of type "neutral-shop".
// The server validates that the requesting player has discovered and
// unlocked the shop, and that the player has at least one reroll remaining
// in their personal pool (Player.ShopRerollsRemaining); on success the
// shop's inventory is regenerated from its loot table and the player's
// pool decrements by one.
type RerollShopCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
}

// EquipItemCommandMessage moves an item from the player's vault into a unit
// slot. InstanceID identifies the specific vault entry. SlotIndex is 0-based;
// must be within the unit's InventorySize.
type EquipItemCommandMessage struct {
	Type       string `json:"type"`
	UnitID     int    `json:"unitId"`
	SlotIndex  int    `json:"slotIndex"`
	InstanceID int64  `json:"instanceId"`
}

// UnequipItemCommandMessage returns an equipped item from a unit slot back to
// the player's vault. SlotIndex is 0-based.
type UnequipItemCommandMessage struct {
	Type      string `json:"type"`
	UnitID    int    `json:"unitId"`
	SlotIndex int    `json:"slotIndex"`
}

// UseConsumableCommandMessage applies the effect of a consumable item in the
// given unit slot and decrements its stack count. SlotIndex is 0-based.
type UseConsumableCommandMessage struct {
	Type      string `json:"type"`
	UnitID    int    `json:"unitId"`
	SlotIndex int    `json:"slotIndex"`
}

// TransferItemCommandMessage moves an equipped item from one unit's slot to
// another unit's slot (or a different slot on the same unit). Both units must
// be owned by the player. The destination slot must be empty — no implicit
// swap. FromSlotIdx and ToSlotIdx are 0-based.
type TransferItemCommandMessage struct {
	Type        string `json:"type"`
	FromUnitID  int    `json:"fromUnitId"`
	FromSlotIdx int    `json:"fromSlotIdx"`
	ToUnitID    int    `json:"toUnitId"`
	ToSlotIdx   int    `json:"toSlotIdx"`
}

// PlayerUpgradeSnapshot describes the current state of one upgrade track for a
// player. Emitted per-player in every MatchSnapshotMessage.Players entry.
type PlayerUpgradeSnapshot struct {
	Track               string  `json:"track"`
	DisplayName         string  `json:"displayName"`
	Level               int     `json:"level"`
	Cap                 int     `json:"cap"`
	NextCostGold        int     `json:"nextCostGold"`
	CanAfford           bool    `json:"canAfford"`
	HasBlacksmith       bool    `json:"hasBlacksmith"`
	HPPerLevel          int     `json:"hpPerLevel"`
	DamagePerLevel      int     `json:"damagePerLevel"`
	ArmorPerLevel       int     `json:"armorPerLevel"`
	AttackSpeedPerLevel float64 `json:"attackSpeedPerLevel"`
	MoveSpeedPerLevel   float64 `json:"moveSpeedPerLevel"`
}

// VaultItemSnapshot carries one vault entry to the client each tick.
// InstanceID is the unique handle used for equip/unequip commands.
type VaultItemSnapshot struct {
	InstanceID int64  `json:"instanceId"`
	ItemID     string `json:"itemId"`
	Stacks     int    `json:"stacks,omitempty"`
}

// ItemSnapshot describes one item in a unit's equipment slot. Nil slots are
// represented as nil pointers in InventorySnapshot.Slots.
type ItemSnapshot struct {
	InstanceID int64  `json:"instanceId"`
	ItemID     string `json:"itemId"`
	Stacks     int    `json:"stacks,omitempty"`
}

// InventorySnapshot carries the full slot layout for a unit's item inventory.
// Size is the number of slots the unit has (0 = no inventory; rank-gated).
// Slots is positional and always len == Size; nil entries are empty slots.
type InventorySnapshot struct {
	Size  int             `json:"size"`
	Slots []*ItemSnapshot `json:"slots"` // positional; nil = empty slot
}

// CommanderAbilitySnapshot is one player-level ability slot in the
// action bar. Cooldowns drive the same clock-wipe overlay the unit-ability
// cooldowns use. Sent per-player (only relevant for the snapshot's owner;
// other players ignore it).
type CommanderAbilitySnapshot struct {
	ID                string  `json:"id"`
	DisplayName       string  `json:"displayName,omitempty"`
	Icon              string  `json:"icon,omitempty"`
	Radius            float64 `json:"radius,omitempty"`
	CooldownTotal     float64 `json:"cooldownTotal,omitempty"`
	CooldownRemaining float64 `json:"cooldownRemaining,omitempty"`
	// Damage / Heal each carry the per-cast magnitude when the ability is a
	// damaging / healing AoE respectively. Exactly one is non-zero per
	// ability (mirroring the server's apply path which switches on
	// Damage>0 vs Heal>0). Surfaced so the HUD tooltip can show the
	// number without duplicating the catalog client-side.
	Damage int `json:"damage,omitempty"`
	Heal   int `json:"heal,omitempty"`
}

type PlayerSnapshot struct {
	PlayerID      string                  `json:"playerId"`
	Color         string                  `json:"color"`
	// TeamID is the player's alliance group. 0 = the default shared team
	// (all players allied — current behavior). Same TeamID ⇒ allies. The
	// client mirrors the server hostility predicate from this value.
	TeamID        int                     `json:"teamId"`
	Resources     []ResourceStock         `json:"resources"`
	Upgrades      []PlayerUpgradeSnapshot `json:"upgrades,omitempty"`
	TownHallTier  int                     `json:"townHallTier,omitempty"`
	Vault         []VaultItemSnapshot     `json:"vault"`
	VaultCapacity int                     `json:"vaultCapacity,omitempty"`
	// LockedUnitTypes lists the unit types this player currently cannot
	// train because their RequiresBuildings list is unsatisfied. Empty
	// or omitted = no locks. The client uses this to grey out train
	// actions in the building action panel.
	LockedUnitTypes []string               `json:"lockedUnitTypes,omitempty"`
	ActiveBuffs     []ActiveEffectIcon     `json:"activeBuffs,omitempty"`
	// CommanderAbilities are the player-level abilities surfaced on the
	// action bar (Smite, Blessing). Always populated for the snapshot's
	// owner so the HUD can render slots even when every ability is ready.
	CommanderAbilities []CommanderAbilitySnapshot `json:"commanderAbilities,omitempty"`
	// ShopRerollsRemaining is the player's remaining merchant-reroll
	// budget for this match. Drives the reroll button on neutral-shop
	// buildings (enabled when > 0).
	ShopRerollsRemaining int `json:"shopRerollsRemaining,omitempty"`
}

type UnitSnapshot struct {
	ID                  int      `json:"id"`
	OwnerID             string   `json:"ownerId"`
	Color               string   `json:"color"`
	UnitType            string   `json:"unitType"`
	Archetype           string   `json:"archetype,omitempty"`
	Name                string   `json:"name"`
	Capabilities        []string `json:"capabilities,omitempty"`
	// Flyer marks the unit as airborne so the client can render an elevation
	// shadow / altitude offset. omitempty so ground units drop the field.
	Flyer               bool     `json:"flyer,omitempty"`
	Visible             bool     `json:"visible"`
	Status              string   `json:"status,omitempty"`
	// Order is the unit's current standing order (see OrderString* constants).
	// omitempty so old clients receiving snapshots from new servers still parse
	// cleanly — an absent field is treated as "idle" by clients.
	Order               string   `json:"order,omitempty"`
	X                   float64  `json:"x"`
	Y                   float64  `json:"y"`
	HP                  int      `json:"hp"`
	MaxHP               int      `json:"maxHp"`
	Damage              int      `json:"damage,omitempty"`
	AttackSpeed         float64  `json:"attackSpeed,omitempty"`
	// AttackRange is the unit's effective attack range in world pixels — base
	// catalog range × any perk range multipliers (eagle_spirit / bullseye).
	// Surfaced so the HUD can display it and the renderer can show range rings
	// for selected units. omitempty so melee units (range 0) drop the field.
	AttackRange         float64  `json:"attackRange,omitempty"`
	MoveSpeed           float64  `json:"moveSpeed,omitempty"`
	Armor               int      `json:"armor,omitempty"`
	// CritChance is the unit's effective crit probability against an unmarked
	// target (0..1). Excludes Hunter's Mark since that is target-dependent.
	// omitempty so non-Marksman units (no crit sources) drop the field.
	CritChance          float64  `json:"critChance,omitempty"`
	// CritMultiplier is the damage multiplier applied on a successful crit
	// (e.g. 2.0 = double damage). Reported as 0 when the unit has no crit
	// sources so the HUD can hide the row entirely.
	CritMultiplier      float64  `json:"critMultiplier,omitempty"`
	// HealthRegen is the current HP-per-second passive regeneration rate.
	// omitempty so units with no regen (0) are absent from the payload.
	HealthRegen         float64  `json:"healthRegen,omitempty"`
	XP                  int      `json:"xp,omitempty"`
	Rank                string   `json:"rank,omitempty"`
	XPToNextRank        int      `json:"xpToNextRank,omitempty"`
	XPIntoCurrentRank   int      `json:"xpIntoCurrentRank,omitempty"`
	RecentRankUpSeconds float64  `json:"recentRankUpSeconds,omitempty"`
	ProgressionPath     string   `json:"progressionPath,omitempty"`
	PerkIDs             []string `json:"perkIds,omitempty"`
	// ExtraPerkSlots reports advancement-granted extra perk slot counts per tier
	// for this unit's owner. Empty / nil when the owner has no
	// unitExtraPerkSlot advancements for this unit type. Keyed by tier
	// ("bronze" | "silver" | "gold"); value is the count of EXTRA slots at that
	// tier (1 for Twin Bronze, 2 for hypothetical Triple Bronze, etc.). The
	// client uses this to render extra locked-or-filled perk slots beyond the
	// standard 3.
	ExtraPerkSlots      map[string]int `json:"extraPerkSlots,omitempty"`
	// Shield / MaxShield: aggregate "displayed shield" — sum of every active
	// shield source on this unit (legacy single Unit.Shield pool from
	// blood_engine + every source-specific pool from perks like dark_renewal).
	// 0 when the unit has no active shield at all (omitempty drops from wire).
	Shield    int `json:"shield,omitempty"`
	MaxShield int `json:"maxShield,omitempty"`
	// ShieldPools: per-source breakdown of the source-specific shield pools
	// the unit currently carries. Lets the client surface "Dark Renewal: 20/40"
	// in the unit info tooltip independently of the aggregate above. Omitted
	// when the unit has no source-specific pools (legacy blood_engine shields
	// don't appear here — they're surfaced only via the aggregate above).
	ShieldPools []ShieldPoolSnapshot `json:"shieldPools,omitempty"`
	// Mana / MaxMana: optional spellcaster resource pool. MaxMana == 0 means
	// the unit has no mana (non-caster), which omitempty drops from the wire.
	// Drives the blue mana bar under the HP bar for casters (e.g. acolyte).
	Mana    int `json:"mana,omitempty"`
	MaxMana int `json:"maxMana,omitempty"`
	// ManaRegen is the unit's effective passive mana regen in mana/second
	// for the current tick — base ManaRegenPerSecond plus any covering
	// Mana Conduit aura bonus (max-wins across allied Clerics in range).
	// Surfaced in the selection HUD's mana stat row when > 0; omitempty
	// drops it from the wire when the unit has no regen this tick.
	ManaRegen float64 `json:"manaRegen,omitempty"`
	// ActiveBuffs: entries for buffs currently active on this unit. `id` is
	// a perk id (resolved to an icon via the PerkDef catalog). See
	// ActiveEffectIcon for the stacks semantics.
	ActiveBuffs []ActiveEffectIcon `json:"activeBuffs,omitempty"`
	// ActiveDebuffs: entries for negative status effects currently active on
	// the unit. `id` is a raw icon id (not a perk id), because debuffs can
	// land on units that don't own the causing perk. Same stacks semantics
	// as ActiveBuffs.
	ActiveDebuffs []ActiveEffectIcon `json:"activeDebuffs,omitempty"`
	// PerkCooldowns: entries for perks currently on cooldown. The HUD renders
	// a clock-wipe overlay + countdown number on the perk icon when an entry
	// is present. Only perks with a ticking cooldown (whirlwind_core,
	// rallying_banner, trap-placement perks) ever appear here, and only while
	// Remaining > 0 — ready-to-fire perks are omitted entirely.
	PerkCooldowns []PerkCooldownSnapshot `json:"perkCooldowns,omitempty"`
	// Abilities: the unit's activatable abilities (Part 6/8) with live
	// auto-cast + cooldown state, for the action bar. Owned units only;
	// absent for units with no abilities.
	Abilities []AbilitySnapshot `json:"abilities,omitempty"`
	// FocusTargetID is the unit ID of the ally this Cleric is focused on.
	// Zero when no focus is active. Drives the Focus Target button highlight
	// and selection-HUD focus indicator on the client. omitempty so non-Cleric
	// units and Clerics without a focus target drop the field from the wire.
	FocusTargetID int `json:"focusTargetId,omitempty"`
	// PickupLootID is the LootDrop.ID this unit is walking to collect.
	// Empty when the unit has no active pickup order. omitempty so it drops
	// from the wire for the overwhelming majority of units that are not
	// pickup-bound.
	PickupLootID string `json:"pickupLootId,omitempty"`
	// ObjectiveID is non-empty when this unit is linked to a victory condition.
	// Matches a VictoryCondition.ID in MapConfig.VictoryConditions.
	ObjectiveID string `json:"objectiveId,omitempty"`
	// StunnedRemaining / SlowedRemaining / SlowedMultiplier carry the current CC
	// state to the client each tick so it can render stun/slow indicator icons.
	// All three use omitempty so they are absent from the JSON when not active.
	StunnedRemaining float64 `json:"stunnedRemaining,omitempty"`
	SlowedRemaining  float64 `json:"slowedRemaining,omitempty"`
	SlowedMultiplier float64 `json:"slowedMultiplier,omitempty"`
	// ChannelLoopStart / ChannelLoopEnd are the inclusive frame range the
	// client loops through (one-way, forward) on the unit's casting sprite
	// sheet while it is channeling a beam ability (Siphon Life). Set only
	// when Status == "Channeling"; absent otherwise. start == end produces
	// a single held pose; start < end produces a small loop at the unit's
	// natural frame cadence. Out-of-range values modulo against the
	// sheet's frame count on the client.
	ChannelLoopStart int `json:"channelLoopStart,omitempty"`
	ChannelLoopEnd   int `json:"channelLoopEnd,omitempty"`
	CarriedResourceType string   `json:"carriedResourceType,omitempty"`
	CarriedAmount       int      `json:"carriedAmount,omitempty"`
	TargetX             float64  `json:"targetX,omitempty"`
	TargetY             float64  `json:"targetY,omitempty"`
	Moving              bool     `json:"moving"`
	// ActionFacingDX/DY is the unit→target world-space delta the server is
	// committing to for the current tick's attack. Non-zero while the unit is
	// actively firing; both zero when the unit is not in-swing. Always sent
	// (no omitempty) so the client can distinguish "server sent zero" (not
	// firing) from "field absent" (old server). Without this, a purely
	// vertical or horizontal attack direction (one component = 0) would be
	// omitted and the client would fall back to the expensive findAttackFacing
	// scan for every affected unit every frame.
	ActionFacingDX float64 `json:"actionFacingDx"`
	ActionFacingDY float64 `json:"actionFacingDy"`
	// WorkTargetID is the building this unit is currently gathering from,
	// constructing, or repairing. The client uses it to orient the worker
	// sprite toward the exact building it is interacting with (there may be
	// more than one valid candidate within range, so "nearest" is not
	// sufficient). Empty when the unit is not in a work state.
	WorkTargetID        string   `json:"workTargetId,omitempty"`
	// EffectiveTrap carries the live compounded trap stats for the unit's current
	// bronze trap perk. Only present for archer units on the trapper path that own
	// a bronze trap perk; nil/omitted for all other units.
	EffectiveTrap *EffectiveTrapSnapshot `json:"effectiveTrap,omitempty"`
	// Inventory carries the unit's item slots. Nil/omitted for units with no
	// inventory (rank below bronze). Size is the slot count; Slots is positional.
	Inventory *InventorySnapshot `json:"inventory,omitempty"`

	// Pathing diagnostics: opt-in telemetry for debugging stuck-unit / repath
	// behaviour. All fields use omitempty so they drop from the wire when zero —
	// production clients pay no cost.
	RepathCount       int `json:"repathCount,omitempty"`
	StuckTriggerCount int `json:"stuckTriggerCount,omitempty"`
	LastStuckTick     int `json:"lastStuckTick,omitempty"`
}

type WelcomeMessage struct {
	Type     string    `json:"type"`
	PlayerID string    `json:"playerId"`
	MatchID  string    `json:"matchId"`
	Map      MapConfig `json:"map"`
}

// WaveSnapshot carries the current wave phase and timer to the client each tick.
// State is "prep" (counting down to wave start) or "active" (wave in progress)
// or "complete" (all waves finished). Timer is seconds-remaining in prep and
// seconds-elapsed in active, so the client can display both styles cheaply.
type WaveSnapshot struct {
	Enabled      bool    `json:"enabled"`
	CurrentWave  int     `json:"currentWave"`
	TotalWaves   int     `json:"totalWaves"`
	State        string  `json:"state"`
	Timer        float64 `json:"timer"`
	WaveDuration float64 `json:"waveDuration"`
}

// WaveUpgradeOfferSnapshot is the per-player upgrade offer sent during the
// "upgrade" wave phase. Nil/absent means the player has no pending offer
// (not in upgrade phase, or already resolved).
type WaveUpgradeOfferSnapshot struct {
	Wave        int            `json:"wave"`
	Offers      []UpgradeOffer `json:"offers"`
	RerollsLeft int            `json:"rerollsLeft"`
	DeadlineMs  int64          `json:"deadlineMs"` // unix ms when auto-pick fires
}

// UpgradeOffer is one card in the wave upgrade offer set.
type UpgradeOffer struct {
	ID                 string `json:"id"`
	Group              string `json:"group"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Rarity             string `json:"rarity"`
	Scope              string `json:"scope"`
	StackCurrent       int    `json:"stackCurrent"`
	StackMax           int    `json:"stackMax"`
	RequiresTargetUnit bool   `json:"requiresTargetUnit,omitempty"`
}

// BannerSnapshot carries a rallying_banner entity to the client each tick.
// The client renders the banner at the given world position for its remaining
// duration. OwnerID is the player who planted it (used for team-colour tinting).
type BannerSnapshot struct {
	ID               int     `json:"id"`
	OwnerID          string  `json:"ownerId"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	Radius           float64 `json:"radius"`
	RemainingSeconds float64 `json:"remainingSeconds"`
}

// TrapSnapshot carries a Trapper trap entity to the client each tick.
// The client renders the trap zone at the given world position for its remaining
// duration. OwnerID is the player who placed it (team-colour tinting).
// Triggered is omitted from JSON when false (omitempty).
type TrapSnapshot struct {
	ID      string  `json:"id"`
	OwnerID string  `json:"ownerId"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	// Radius is the damage/effect area. For explosive_trap this is the outer
	// explosion (AoE) radius; for all other types it's the single active zone.
	Radius float64 `json:"radius"`
	// TriggerRadius is the inner zone that causes detonation. Only populated
	// for trap types with a separate trigger/effect radius (currently just
	// explosive_trap); 0/omitted for the others, where Radius alone suffices.
	TriggerRadius float64 `json:"triggerRadius,omitempty"`
	// Variant is an optional visual tag the client uses to pick a
	// non-default animation for this trap (e.g. "electrified" caltrops under
	// ascendant_infusion). Empty/omitted means "use the trap's default
	// animation". Values are coordinated between server and client assets.
	Variant string `json:"variant,omitempty"`
	// ScaleMultiplier is an extra render-scale factor applied on top of the
	// sprite set's base scale. Populated for perks that visually inflate the
	// trap (e.g. overload_protocol → explosive_trap → 2×). 0/omitted on the
	// wire means "no multiplier"; the client treats that as 1×.
	ScaleMultiplier  float64 `json:"scaleMultiplier,omitempty"`
	Type             string  `json:"type"`
	RemainingSeconds float64 `json:"remainingSeconds"`
	Triggered        bool    `json:"triggered,omitempty"`
}

// GameOverSnapshot is included in every snapshot once one or more players have
// lost all their townhalls. LostPlayerIDs lists the player IDs that have lost;
// each client checks whether its own ID is present.
type GameOverSnapshot struct {
	LostPlayerIDs []string `json:"lostPlayerIds"`
}

// MatchSummary carries per-player match-end data alongside the game-over
// snapshot. Populated once per match when the game ends. The match manager's
// OnGameOver hook calls a LegendPointCommitter (implemented by
// profile.Manager) to persist LegendPointsEarned into the profile —
// simulation code does not touch the profile store directly.
type MatchSummary struct {
	PlayerID           string `json:"playerId"`
	Won                bool   `json:"won"`
	LegendPointsEarned int    `json:"legendPointsEarned,omitempty"`
}

// ObjectiveSnapshot carries the current state of one victory condition to the
// client every tick (once the map has VictoryConditions defined). Clients use
// this to render an objective tracker HUD and detect when all are complete.
type ObjectiveSnapshot struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Label     string `json:"label,omitempty"`
	Completed bool   `json:"completed"`
	// Progress / Count are only meaningful for "killUnit" objectives.
	Progress int `json:"progress,omitempty"`
	Count    int `json:"count,omitempty"`
}

// VictorySnapshot is included in every snapshot when the map has
// VictoryConditions defined. Achieved becomes true once ALL objectives are
// complete — that is the client's cue to show the victory screen.
type VictorySnapshot struct {
	Achieved   bool               `json:"achieved"`
	Objectives []ObjectiveSnapshot `json:"objectives"`
}

// ProjectileSnapshot carries an in-flight ranged attack to the client each tick.
// The client renders a shape (or sprite, by Variant) traveling along the arc
// from (OriginX, OriginY) toward the homing target position (TargetX, TargetY),
// positioned by Progress (0 = just fired, 1 = landing). OwnerID is used for
// team-color tinting. TargetUnitID is informational — the server owns the
// homing update, so Target fields already reflect the current tracked position.
type ProjectileSnapshot struct {
	ID           string  `json:"id"`
	OwnerUnitID  int     `json:"ownerUnitId"`
	OwnerID      string  `json:"ownerId"`
	TargetUnitID int     `json:"targetUnitId,omitempty"`
	OriginX      float64 `json:"originX"`
	OriginY      float64 `json:"originY"`
	TargetX      float64 `json:"targetX"`
	TargetY      float64 `json:"targetY"`
	// Progress is the fraction of the flight completed, 0..1.
	Progress float64 `json:"progress"`
	// Variant is the sprite key used by the client to pick a visual. Defaults
	// to the attacker's unit type; perks may override it at fire time for
	// alternate shot visuals (e.g. "fire_arrow").
	Variant string `json:"variant,omitempty"`
	// DoubleShotSecond is set on the second arrow of a Double Shot pair
	// (Marksman gold). The client tracks these projectiles so it can render
	// a yellow combined damage number after the second arrow lands, summing
	// both shots' damage on the same target.
	DoubleShotSecond bool `json:"doubleShotSecond,omitempty"`
	// Pierce is set on Marksman silver pierce arrows so the client renderer
	// can extend the arrow visual past the primary target — pierce arrows
	// fly all the way to TargetX/Y (the far endpoint of the line) rather
	// than stopping at the target unit.
	Pierce bool `json:"pierce,omitempty"`
	// Scale is a per-shot render-size multiplier applied on top of the
	// client's base projectile-sprite scale (same role as
	// TrapSnapshot.ScaleMultiplier / EffectSnapshot.SizeScale). Resolved
	// server-side from the firing unit's projectileScale (unit def, or its
	// promotion-path override), so two different units firing the same
	// projectile can render it at different sizes. 0/omitted ⇒ the client's
	// default (1×) — every existing projectile is unchanged.
	Scale float64 `json:"scale,omitempty"`
}

// CritEventSnapshot is a per-tick record of a critical hit that landed.
// Drained on the wire each tick alongside the rest of the snapshot; the
// client matches each entry to its HP-diff damage event by (UnitID, Damage)
// and renders the floating number with a red circle behind it. Empty when
// no crits land — the field is omitted from JSON entirely in that case.
type CritEventSnapshot struct {
	UnitID int `json:"unitId"`
	Damage int `json:"damage"`
}

// MinorDamageEventSnapshot tags a portion of a unit's HP-delta as ancillary
// damage (e.g. ascendant_infusion's Reactive Flames AoE, Electrified Caltrops
// bonus damage). The client renders the matching portion of its floating-
// number popup in a smaller font with a distinct color so the player can
// tell "the trap did X, the infusion added Y" at a glance. Drained per tick
// like CritEventSnapshot. Variant maps to a renderer color (e.g. "fire" =
// orange, "electric" = purple); empty defaults to fire/orange.
type MinorDamageEventSnapshot struct {
	UnitID  int    `json:"unitId"`
	Damage  int    `json:"damage"`
	Variant string `json:"variant,omitempty"`
}

// DamageTypeHintSnapshot carries a server-side hint that one chunk of HP
// loss this tick has a specific damage type. Used by the client to COLOR
// the existing major (floating-up) damage popup it derives from HP-diff —
// it does NOT spawn an additional popup. Auto-emitted by the damage
// pipeline whenever a typed DamageSource produces HP loss; safe to silently
// drop unmatched entries (the popup falls back to the default color).
type DamageTypeHintSnapshot struct {
	UnitID  int    `json:"unitId"`
	Damage  int    `json:"damage"`
	Variant string `json:"variant,omitempty"`
}

// LethalDamageEventSnapshot carries the pre-clamp damage value for an overkill
// killing blow. HP-deltas on the wire clamp to remaining HP, so the client's
// HP-diff popup would otherwise show only the leftover HP. Each entry tells
// the client "the synthesized killing-blow popup for this UnitID should use
// Damage instead." Only emitted for overkill — exact kills don't need it.
type LethalDamageEventSnapshot struct {
	UnitID int `json:"unitId"`
	Damage int `json:"damage"`
}

// HealEventSnapshot is a per-tick record of intentional healing landing on a
// unit (the heal ability / any AbilityDef.HealAmount). The client resolves the
// unit's live position by UnitID and spawns a light-green "+Amount" floating
// number over it. Passive HP regen is intentionally not reported here so the
// screen isn't spammed with +1s. Drained per tick like CritEventSnapshot;
// omitted from JSON when no heals land.
type HealEventSnapshot struct {
	UnitID int `json:"unitId"`
	Amount int `json:"amount"`
}

// ManaRestoreEventSnapshot mirrors HealEventSnapshot for intentional mana
// grants (perks like Repurposed Life, future cleric mana abilities). The
// client spawns a blue "+Amount" floating popup over the recipient. Passive
// regen is intentionally not reported — it would spam +1s at the natural
// 0.2/s rate. Drained per-tick alongside the other transient event queues.
type ManaRestoreEventSnapshot struct {
	UnitID int `json:"unitId"`
	Amount int `json:"amount"`
}

// EffectSnapshot is a generalized transient visual effect anchored to a unit
// or a world position. It is emitted by the server (typically via perk hooks)
// and drained per-tick to the client alongside ProjectileSnapshot and
// ExplosionSnapshot. The client identifies the renderer by Name.
//
// AnchorUnitID: when non-zero the client should track the unit's current
// position; X/Y hold the last known position as a fallback for when the unit
// is not in the client's current view or has died mid-effect.
//
// Progress: 0 = just spawned, 1 = fully elapsed. The client drives its
// animation timeline from this value.
type EffectSnapshot struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	AnchorUnitID int     `json:"anchorUnitId,omitempty"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	Progress     float64 `json:"progress"`
	SizeScale    float64 `json:"sizeScale,omitempty"`
	Variant      string  `json:"variant,omitempty"`
	// Anchor is where the effect renders relative to its anchor unit:
	// "center" (default / empty), "feet", or "head". Empty is treated as
	// center by the client (preserves prior perk-effect behavior).
	Anchor string `json:"anchor,omitempty"`
}

// FogOfWarSnapshot carries the per-player FOW grid to the client each tick.
// Runs is an RLE-encoded sequence of [state, count, state, count, ...] pairs
// where state is 0 (dark), 1 (shroud/ever-seen), or 3 (clear).
// RevTick is the server tick at which this FOW was last recomputed.
type FogOfWarSnapshot struct {
	Cols    int   `json:"cols"`
	Rows    int   `json:"rows"`
	Runs    []int `json:"runs"`
	RevTick int   `json:"revTick"`
}

// NeutralCampSnapshot is the per-tick wire view of one neutral spawn camp.
// Lightweight by design: the static placement (position, group, scaling)
// lives in MapConfig.NeutralSpawns and is sent once at match join; only the
// fields that change per wave (CurrentTier, AliveUnitCount) need to be in
// the per-tick snapshot. Sent unfiltered — neutrals are mapper-authored
// points of interest and should appear on the minimap regardless of fog
// of war.
//
// AliveUnitCount = 0 lets the client hide the minimap POI dot for camps
// the player has cleared (or that are wave-hidden), so the minimap
// reflects which camps currently have enemies on the field.
type NeutralCampSnapshot struct {
	ID             string `json:"id"`
	X              int    `json:"x"`
	Y              int    `json:"y"`
	CurrentTier    int    `json:"currentTier"`
	AliveUnitCount int    `json:"aliveUnitCount"`
}

// LootDropSnapshot is the per-tick wire view of one ground-loot chest.
// Sent unfiltered (no FOW gating) so chests behave like POI dots on the
// minimap — the player can always navigate back to an uncollected chest.
//
// Resources and ItemIDs mirror the chest's pre-rolled contents so the
// client can render a hover tooltip showing what the chest contains
// before the player collects it. These are the same values granted on
// pickup (less vault-overflow items).
type LootDropSnapshot struct {
	ID        string         `json:"id"`
	X         float64        `json:"x"`
	Y         float64        `json:"y"`
	IconKey   string         `json:"iconKey"`
	Resources map[string]int `json:"resources,omitempty"`
	ItemIDs   []string       `json:"itemIds,omitempty"`
}

type MatchSnapshotMessage struct {
	Type          string                  `json:"type"`
	Tick          int                     `json:"tick"`
	ServerNow     int64                   `json:"serverNow"`
	MatchID       string                  `json:"matchId"`
	Buildings     []BuildingTile          `json:"buildings"`
	// ObstaclesRemoved: obstacle IDs that have been removed from the world
	// since the previous broadcast (trees chopped, rocks mined to depletion).
	// Only populated on broadcasts that follow a removeObstacleByIDLocked;
	// empty on the steady state. The full obstacle geometry is sent ONCE in
	// the WelcomeMessage's MapConfig — clients maintain their local mirror
	// and apply removals from this list. Eliminates the per-tick retransmit
	// of the entire obstacle array (~870KB on the exploration map at 20Hz).
	ObstaclesRemoved []string `json:"obstaclesRemoved,omitempty"`
	// ObstacleMetadata: per-obstacle live metadata patches that have changed
	// since the previous broadcast. Currently only `currentWorkers` on tree
	// obstacles (used by the HUD tooltip). Only obstacles whose count changed
	// since the last send are included — steady-state ticks send nothing.
	// `maxWorkers` is constant per obstacle type and known to the client from
	// the WelcomeMessage, so we don't re-send it.
	ObstacleMetadata []ObstacleMetadataPatch `json:"obstacleMetadata,omitempty"`
	Players       []PlayerSnapshot        `json:"players"`
	Units         []UnitSnapshot          `json:"units"`
	Wave          WaveSnapshot            `json:"wave"`
	Banners       []BannerSnapshot        `json:"banners,omitempty"`
	Traps         []TrapSnapshot          `json:"traps,omitempty"`
	Projectiles   []ProjectileSnapshot    `json:"projectiles,omitempty"`
	Beams         []BeamSnapshot          `json:"beams,omitempty"`
	Effects       []EffectSnapshot        `json:"effects,omitempty"`
	CritEvents         []CritEventSnapshot         `json:"critEvents,omitempty"`
	MinorDamageEvents  []MinorDamageEventSnapshot  `json:"minorDamageEvents,omitempty"`
	DamageTypeHints    []DamageTypeHintSnapshot    `json:"damageTypeHints,omitempty"`
	LethalDamageEvents []LethalDamageEventSnapshot `json:"lethalDamageEvents,omitempty"`
	HealEvents         []HealEventSnapshot         `json:"healEvents,omitempty"`
	ManaRestoreEvents  []ManaRestoreEventSnapshot  `json:"manaRestoreEvents,omitempty"`
	BattleTracker *BattleTrackerSnapshot  `json:"battleTracker,omitempty"`
	GameOver      *GameOverSnapshot       `json:"gameOver,omitempty"`
	Victory       *VictorySnapshot        `json:"victory,omitempty"`
	Fow           *FogOfWarSnapshot       `json:"fow,omitempty"`
	WaveUpgrade   *WaveUpgradeOfferSnapshot `json:"waveUpgrade,omitempty"`
	NeutralCamps  []NeutralCampSnapshot   `json:"neutralCamps,omitempty"`
	LootDrops     []LootDropSnapshot      `json:"lootDrops,omitempty"`

	// Paused is true when the simulation is frozen via the in-match settings
	// "Pause Game" action. The client renders a paused overlay and freezes the
	// wave-upgrade selection timer while this is true. PausedBy is the player
	// ID that initiated the pause; the client maps it to a display name.
	Paused   bool   `json:"paused,omitempty"`
	PausedBy string `json:"pausedBy,omitempty"`

	// PersistentlyStuckUnits lists IDs of units whose pathing watchdog has fired
	// 4+ times in the current wave. Computed at snapshot time, not stored on the
	// state. omitempty so the field drops from the wire when the slice is empty.
	PersistentlyStuckUnits []int `json:"persistentlyStuckUnits,omitempty"`
}

// ─── Battle Tracker (debug) ──────────────────────────────────────────────────
// Only populated when MapConfig.Debug.BattleTracker is true. Rendered by the
// client's debug panel for balance tuning. Represents running totals across
// the whole match; saved/reviewed snapshots on the client capture this struct
// verbatim.

// BattleStats is the per-bucket accumulator. Always paired with a source kind
// + subtype identifying the attacker lane (see BattleBucket).
type BattleStats struct {
	DamageDealt int `json:"damageDealt"`
	Kills       int `json:"kills"`
}

// BattleBucket is one (kind, subtype) lane — e.g. ("unit","archer") or
// ("trap","caltrops") — with its accumulated stats. Buckets are grouped under
// a player in BattlePlayerStats.
type BattleBucket struct {
	Kind    string      `json:"kind"`    // "unit" | "trap" | "building"
	Subtype string      `json:"subtype"` // unit type / trap type / building type
	Stats   BattleStats `json:"stats"`
}

// BattlePlayerStats collects all buckets under a single player ID.
// PlayerID == "__enemy__" represents wave / NPC enemies.
type BattlePlayerStats struct {
	PlayerID string         `json:"playerId"`
	Buckets  []BattleBucket `json:"buckets"`
	Total    BattleStats    `json:"total"`
}

// BattleTrackerSnapshot is the wire format for live debug data. ElapsedSeconds
// is the simulation time since the tracker was armed (match start when the
// map flag is set).
type BattleTrackerSnapshot struct {
	ElapsedSeconds float64             `json:"elapsedSeconds"`
	Players        []BattlePlayerStats `json:"players"`
}

type PingMessage struct {
	Type string `json:"type"`
}

type PongMessage struct {
	Type string `json:"type"`
}

type ErrorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type NotificationMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// LootCollectedNotification is pushed to the collecting player when a
// chest pickup completes. The HUD renders a toast listing the resources
// and items received. Items that couldn't fit in the vault are listed in
// OverflowItemIDs so the toast can show "+50 gold, Broad Sword (lost —
// vault full)".
type LootCollectedNotification struct {
	Type            string         `json:"type"` // "loot_collected"
	PlayerID        string         `json:"playerId"`
	LootDropID      string         `json:"lootDropId"`
	// CollectingUnitID is the ID of the unit that walked to and collected
	// the chest. Used by the client to position the floating "+X gold"
	// pickup text in world space above that unit.
	CollectingUnitID int            `json:"collectingUnitId"`
	Resources        map[string]int `json:"resources,omitempty"`
	ItemIDs          []string       `json:"itemIds,omitempty"`
	OverflowItemIDs  []string       `json:"overflowItemIds,omitempty"`
}

// DebugSpawnUnitMessage is a dev-only command that spawns a fully configured
// unit at the requested world position. Only honored on maps with
// Debug.DebugSpawn == true; silently ignored otherwise (the server logs a
// warning but does not send an error so a client accidentally sending this
// on a production map doesn't surface noise in the HUD).
//
// Semantics:
//   - Team chooses ownership. "mine" (the default when empty) assigns the
//     unit to the caller — convenient for testing your own perk loadouts
//     in live matches. "enemy" assigns to the NPC/wave owner so the unit
//     behaves as a test dummy hostile to everyone.
//   - PerkIDs are appended verbatim to the spawned unit's PerkIDs slice in
//     the order given (typically Bronze, Silver, Gold). They are NOT
//     validated against the eligibility filter — the debug tool must be
//     able to produce any perk combo, including ones the rank-up pool
//     would normally exclude.
//   - Rank (base / bronze / silver / gold) determines stat scaling via
//     applyRankModifiersLocked. Empty string is treated as "base".
//   - Path (trapper / marksman / vanguard / berserker / none) is set directly,
//     bypassing assignUnitPathOnRankUpLocked. Empty string means "none".
//   - CustomHP > 0 overrides both MaxHP and HP after rank scaling. Use 0
//     (or omit) to keep the default max HP.
type DebugSpawnUnitMessage struct {
	Type     string   `json:"type"`
	UnitType string   `json:"unitType"`
	Team     string   `json:"team,omitempty"` // "mine" | "enemy"; empty = "mine"
	Path     string   `json:"path,omitempty"`
	Rank     string   `json:"rank,omitempty"`
	PerkIDs  []string `json:"perkIds,omitempty"`
	X        float64  `json:"x"`
	Y        float64  `json:"y"`
	CustomHP int      `json:"customHp,omitempty"`
}

// WaveUpgradeChoiceMessage is sent by the client when the player picks an upgrade.
type WaveUpgradeChoiceMessage struct {
	Type         string `json:"type"`
	UpgradeID    string `json:"upgradeId"`
	TargetUnitID int    `json:"targetUnitId,omitempty"` // set only when RequiresTargetUnit = true
}

// WaveUpgradeRerollMessage is sent by the client when the player uses a reroll.
type WaveUpgradeRerollMessage struct {
	Type string `json:"type"`
}

// SetPauseMessage is sent by the client when the player toggles the pause
// state from the in-match settings menu. Paused=true pauses the simulation;
// false resumes. Either action is allowed from any player in the match.
type SetPauseMessage struct {
	Type   string `json:"type"`
	Paused bool   `json:"paused"`
}
