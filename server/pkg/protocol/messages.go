package protocol

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
}

type JoinMatchMessage struct {
	Type     string `json:"type"`
	PlayerID string `json:"playerId"`
	MapID    string `json:"mapId"`
	MatchID  string `json:"matchId,omitempty"`
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

type AttackMoveCommandMessage struct {
	Type        string `json:"type"`
	UnitIDs     []int  `json:"unitIds"`
	Destination Vec2   `json:"destination"`
}

type CancelTrainingCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
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

type PlayerSnapshot struct {
	PlayerID  string          `json:"playerId"`
	Color     string          `json:"color"`
	Resources []ResourceStock `json:"resources"`
}

type UnitSnapshot struct {
	ID                  int      `json:"id"`
	OwnerID             string   `json:"ownerId"`
	Color               string   `json:"color"`
	UnitType            string   `json:"unitType"`
	Archetype           string   `json:"archetype,omitempty"`
	Name                string   `json:"name"`
	Capabilities        []string `json:"capabilities,omitempty"`
	Visible             bool     `json:"visible"`
	Status              string   `json:"status,omitempty"`
	X                   float64  `json:"x"`
	Y                   float64  `json:"y"`
	HP                  int      `json:"hp"`
	MaxHP               int      `json:"maxHp"`
	Damage              int      `json:"damage,omitempty"`
	AttackSpeed         float64  `json:"attackSpeed,omitempty"`
	MoveSpeed           float64  `json:"moveSpeed,omitempty"`
	Armor               int      `json:"armor,omitempty"`
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
	// EffectiveTrap carries the live compounded trap stats for the unit's current
	// bronze trap perk. Only present for archer units on the trapper path that own
	// a bronze trap perk; nil/omitted for all other units.
	EffectiveTrap *EffectiveTrapSnapshot `json:"effectiveTrap,omitempty"`
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
	BattleTracker *BattleTrackerSnapshot  `json:"battleTracker,omitempty"`
	GameOver      *GameOverSnapshot       `json:"gameOver,omitempty"`
	Victory       *VictorySnapshot        `json:"victory,omitempty"`
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
//   - Path (trapper / vanguard / berserker / none) is set directly, bypassing
//     assignUnitPathOnRankUpLocked. Empty string means "none".
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
