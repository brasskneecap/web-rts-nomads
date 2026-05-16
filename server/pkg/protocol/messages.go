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
	PlacedUnits []PlacedUnit       `json:"placedUnits,omitempty"`
	WaveConfig  *WaveConfig        `json:"waveConfig,omitempty"`
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
	ID                string  `json:"id"`
	DisplayName       string  `json:"displayName,omitempty"`
	Icon              string  `json:"icon,omitempty"`
	ManaCost          int     `json:"manaCost,omitempty"`
	SupportsAutoCast  bool    `json:"supportsAutoCast,omitempty"`
	AutoCast          bool    `json:"autoCast,omitempty"` // auto-cast currently enabled
	CooldownRemaining float64 `json:"cooldownRemaining,omitempty"`
	CooldownTotal     float64 `json:"cooldownTotal,omitempty"`
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
}

type JoinMatchMessage struct {
	Type            string   `json:"type"`
	PlayerID        string   `json:"playerId"`
	MapID           string   `json:"mapId"`
	MatchID         string   `json:"matchId,omitempty"`
	EquippedBuffIDs []string `json:"equippedBuffIds,omitempty"`
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
	ActiveBuffs   []ActiveEffectIcon      `json:"activeBuffs,omitempty"`
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
	// Shield / MaxShield: temporary HP pool (from blood_engine). 0 when absent.
	Shield    int `json:"shield,omitempty"`
	MaxShield int `json:"maxShield,omitempty"`
	// Mana / MaxMana: optional spellcaster resource pool. MaxMana == 0 means
	// the unit has no mana (non-caster), which omitempty drops from the wire.
	// Drives the blue mana bar under the HP bar for casters (e.g. apprentice).
	Mana    int `json:"mana,omitempty"`
	MaxMana int `json:"maxMana,omitempty"`
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
	// ObjectiveID is non-empty when this unit is linked to a victory condition.
	// Matches a VictoryCondition.ID in MapConfig.VictoryConditions.
	ObjectiveID string `json:"objectiveId,omitempty"`
	// StunnedRemaining / SlowedRemaining / SlowedMultiplier carry the current CC
	// state to the client each tick so it can render stun/slow indicator icons.
	// All three use omitempty so they are absent from the JSON when not active.
	StunnedRemaining float64 `json:"stunnedRemaining,omitempty"`
	SlowedRemaining  float64 `json:"slowedRemaining,omitempty"`
	SlowedMultiplier float64 `json:"slowedMultiplier,omitempty"`
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
// snapshot. Populated once per match when the game ends. The HTTP layer
// (profile handler) is responsible for persisting LegendPointsEarned to the
// player profile via profileManager.WithLocked — the simulation only computes
// the totals; it does not touch the profile store directly.
//
// TODO: the profile REST handler should call profileManager.WithLocked to add
// LegendPointsEarned to profile.LegendPoints and profile.LifetimeLegendPoints.
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

type MatchSnapshotMessage struct {
	Type          string                  `json:"type"`
	Tick          int                     `json:"tick"`
	ServerNow     int64                   `json:"serverNow"`
	MatchID       string                  `json:"matchId"`
	Buildings     []BuildingTile          `json:"buildings"`
	Obstacles     []ObstacleTile          `json:"obstacles"`
	Players       []PlayerSnapshot        `json:"players"`
	Units         []UnitSnapshot          `json:"units"`
	Wave          WaveSnapshot            `json:"wave"`
	Banners       []BannerSnapshot        `json:"banners,omitempty"`
	Traps         []TrapSnapshot          `json:"traps,omitempty"`
	Projectiles   []ProjectileSnapshot    `json:"projectiles,omitempty"`
	Effects       []EffectSnapshot        `json:"effects,omitempty"`
	CritEvents         []CritEventSnapshot         `json:"critEvents,omitempty"`
	MinorDamageEvents  []MinorDamageEventSnapshot  `json:"minorDamageEvents,omitempty"`
	LethalDamageEvents []LethalDamageEventSnapshot `json:"lethalDamageEvents,omitempty"`
	HealEvents         []HealEventSnapshot         `json:"healEvents,omitempty"`
	BattleTracker *BattleTrackerSnapshot  `json:"battleTracker,omitempty"`
	GameOver      *GameOverSnapshot       `json:"gameOver,omitempty"`
	Victory       *VictorySnapshot        `json:"victory,omitempty"`
	Fow           *FogOfWarSnapshot       `json:"fow,omitempty"`
	WaveUpgrade   *WaveUpgradeOfferSnapshot `json:"waveUpgrade,omitempty"`
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
