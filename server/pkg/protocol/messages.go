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
	OrderStringFocusFollow = "focus_follow"
	OrderStringPickupLoot  = "pickup_loot"
	// OrderStringGuard is the wire value for the Guard order — hold a position,
	// engage hostiles that come within the unit's guard radius, then return.
	OrderStringGuard = "guard"
)

type Vec2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WaveConfig holds per-map tuning for wave mode. Omitted or zero values fall
// back to server defaults (60 s prep, 120 s active, totalWaves derived from
// the highest waveNumber found on spawn points).
type WaveConfig struct {
	TotalWaves int `json:"totalWaves,omitempty"`
	// InitialPrepDuration is the prep countdown before wave 1 (seconds). When
	// 0 it falls back to PrepDuration, preserving the legacy single-timer
	// behaviour. Lets a map open with a long build-up (e.g. 180s) while keeping
	// short between-wave breaks.
	InitialPrepDuration float64 `json:"initialPrepDuration,omitempty"`
	// PrepDuration is the prep countdown between subsequent waves (seconds; 0 =
	// default 60). Also used before wave 1 when InitialPrepDuration is unset.
	PrepDuration float64 `json:"prepDuration,omitempty"`
	WaveDuration float64 `json:"waveDuration,omitempty"`
	// ContinuousWaves switches the map to continuous mode: once a wave starts
	// releasing enemies it never waits for the field to clear — WaveDuration is
	// the countdown to releasing the NEXT wave, so waves overlap and accumulate.
	// An upgrade pick is still presented (sim frozen) at each new wave. Omitted
	// or false ⇒ the legacy discrete (clear-the-field) flow.
	ContinuousWaves bool `json:"continuousWaves,omitempty"`
	// EnemiesFightNeutrals toggles hostility between the __enemy__ wave faction
	// and __neutral__ camps. Default false ⇒ they ignore each other. When true
	// they attack each other, and a camp whose killing blow came from an enemy
	// unit drops no loot. Only has gameplay effect when the two coexist on the
	// field (continuous mode), but is honored generally by playersAreHostile.
	EnemiesFightNeutrals bool `json:"enemiesFightNeutrals,omitempty"`
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

// ZoneCaptureNeutralOwner is the sentinel owner value for a zone controlled by
// no team. A zone's StartingOwner is normalised to this when unset; the runtime
// initialises an uncontrolled zone's owner to this value.
const ZoneCaptureNeutralOwner = "neutral"

// ZoneCaptureTeamOwner is the sentinel owner value for a zone held by the human
// (co-op) team. Every successful capture assigns this — control is a team
// effort, not a single player's — and it is allied with every non-AI player.
// Authors can also pick it as a zone's StartingOwner to seed the frontier.
const ZoneCaptureTeamOwner = "team"

// ZoneCapture is the per-zone configuration of HOW a zone is captured. Type
// names a registered capture mechanic (control_point | presence | clear) and
// Config carries that mechanic's typed config, parsed server-side by the
// mechanic's parseConfig hook (mirrors ObjectiveDef.Config). The client treats
// Type as opaque for rendering and never simulates capture itself.
type ZoneCapture struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config,omitempty"`
}

// Zone is a map-authored territorial region. A player's team must control a
// zone to build on its cells, and capturing a zone can be a (possibly required)
// objective. Authored in the map editor's zone brush.
//
// Cells is the zone's member set, stored compactly as [x,y] pairs. A cell
// belongs to at most one zone (validated at load). The perimeter/interior
// split is DERIVED from this set, never stored: a member cell with a non-member
// 4-neighbour is perimeter, else interior.
//
// Anchor is the editor "node" — the cell the brush drops. It is the popup
// attach point and (for control_point capture) where the capturable structure
// sits.
//
// Adjacent lists this zone's capture PREREQUISITE zones (directed, not
// symmetric). The team may capture this zone only once it owns the required
// links: with RequireAllLinks, ALL of them; otherwise ANY one. An EMPTY list
// means the zone is ungated — always capturable (no prerequisite).
type Zone struct {
	ID            string      `json:"id"`
	Name          string      `json:"name,omitempty"`
	Anchor        GridCoord   `json:"anchor"`
	Cells         [][2]int    `json:"cells"`
	Capture       ZoneCapture `json:"capture"`
	StartingOwner string      `json:"startingOwner,omitempty"`
	Adjacent      []string    `json:"adjacent,omitempty"`
	// RequireAllLinks controls how the Adjacent prerequisites gate capture:
	// false (default) ⇒ owning ANY one linked zone unlocks this one; true ⇒
	// ALL linked zones must be owned. Ignored when Adjacent is empty.
	RequireAllLinks bool `json:"requireAllLinks,omitempty"`
	// CaptureCells is the optional "capture sub-zone" for presence capture: a
	// subset of Cells that a unit must stand in to progress the capture (rather
	// than anywhere in the zone). Empty ⇒ the whole zone counts. Also used by an
	// enemy-spawnpoint's `triggerCaptureZoneId` to decide occupancy.
	CaptureCells [][2]int `json:"captureCells,omitempty"`
	// ClaimPoints is the optional list of capture-point slots for the "claim"
	// capture mechanic: each entry is the top-left cell of a 2x2 tower slot. The
	// team must build and defend a tower on EVERY point to capture the zone; each
	// point is captured independently and stays captured (sticky) once defended.
	// EMPTY/absent ⇒ a single slot at Anchor (backward-compatible default). Only
	// meaningful when Capture.Type == "claim".
	ClaimPoints [][2]int `json:"claimPoints,omitempty"`
	// LockedSpawnLabel, when set, links this zone to a player's starting point
	// (the spawn-point's player label, e.g. "player1"). A linked zone is the
	// team's home territory: it starts team-owned and is NOT capturable (the
	// capture mechanic never runs on it). The Capture config is ignored. It can
	// still seed the adjacency frontier for capturing neighbouring zones.
	LockedSpawnLabel string `json:"lockedSpawnLabel,omitempty"`
	// Auras is the optional list of passive bonuses the zone grants to its
	// owner while controlled. Each entry is expressed in the shared stat-modifier
	// vocabulary (see StatModifier) rather than a zone-specific effect type, so
	// the same stat ids and operations used by perks/buffs/upgrades apply here.
	// EMPTY/absent ⇒ the zone grants no passive bonus (only vision + build rights
	// from add-map-zones). Auras travel with the static zone def (welcome
	// payload); the live owner comes from ZoneSnapshot.
	Auras []ZoneAura `json:"auras,omitempty"`
}

// StatModifier is the canonical, system-agnostic shape for a single stat
// bonus. Zone auras — and, in future, campaign modifiers, equipment, and
// global events — all express bonuses as StatModifiers so every gameplay
// system speaks the same language (the same Stat ids and Operations the
// stat registry validates). Stacking rule, applied per stat at the read site:
//
//	effective = (base + Σ value where Operation==add) × Π value where Operation==multiply
type StatModifier struct {
	// Stat is a registered stat identifier (e.g. "healthRegen", "moveSpeed",
	// "goldGatherRate"). Validated against the server stat registry at load.
	Stat string `json:"stat"`
	// Operation is "add" (flat additive, summed) or "multiply" (multiplicative,
	// producted). Any other value is rejected at load.
	Operation string `json:"operation"`
	// Value is the additive delta (Operation=="add") or the multiplier
	// (Operation=="multiply", e.g. 1.15 for +15%).
	Value float64 `json:"value"`
}

// ZoneAura is a typed envelope around an aura effect a zone grants its owner.
// Type discriminates the kind of effect; v1 implements only "stat_modifier",
// which carries a StatModifier in Modifier. The Type and Scope fields are the
// extension seams for future kinds ("periodic", "spawn", "vision", "debuff")
// and scopes ("radius", "regional") without changing the v1 path.
type ZoneAura struct {
	// Type is the aura kind. "stat_modifier" (the only v1 type) reads Modifier.
	Type string `json:"type"`
	// Scope defaults to "global" (applies to all of the owner's units/buildings
	// regardless of position). Reserved future values: "radius", "regional".
	Scope string `json:"scope,omitempty"`
	// Modifier is the stat bonus, present when Type == "stat_modifier".
	Modifier StatModifier `json:"modifier"`
}

// Zone aura type / scope sentinels.
const (
	ZoneAuraTypeStatModifier = "stat_modifier"
	ZoneAuraScopeGlobal      = "global"
)

// PlacedUnit is a statically authored unit in the map. PlayerSlot determines
// who controls the unit at runtime: "player1", "player2", ... spawn when that
// player joins the matching slot; "enemy" spawns at match start as a
// stationary guard. The unit type implies its faction (raider / neutral /
// human) — faction is intrinsic to the UnitDef and is not stored per instance.
type PlacedUnit struct {
	GridCoord
	ID         string   `json:"id"`
	PlayerSlot string   `json:"playerSlot"`
	UnitType   string   `json:"unitType"`
	AggroRange float64  `json:"aggroRange,omitempty"`
	LeashRange float64  `json:"leashRange,omitempty"`
	Rank       string   `json:"rank,omitempty"`
	Items      []string `json:"items,omitempty"`
	Perks      []string `json:"perks,omitempty"`
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
		UnitType    string   `json:"unitType"`
		AggroRange  float64  `json:"aggroRange"`
		LeashRange  float64  `json:"leashRange"`
		Rank        string   `json:"rank"`
		Items       []string `json:"items"`
		Perks       []string `json:"perks"`
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
	p.Rank = raw.Rank
	p.Items = raw.Items
	p.Perks = raw.Perks
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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Version is an optional, human-readable map version string (e.g. "v3"),
	// authored in the map file. Display/logging only — it is NEVER used to
	// match maps between host and joiner. See ContentHash for the match key.
	Version string `json:"version,omitempty"`
	// ContentHash is a derived, deterministic hash of the map's authored
	// content (set by the server on load/save, never authored, never written
	// to the on-disk file). It is the canonical key used to detect a
	// host/joiner map mismatch at join time. Two machines running the same
	// binary over the same authored map compute the same hash. Computed by
	// computeMapContentHash; the human Version is excluded from the input so
	// bumping the version string alone does not change the hash.
	ContentHash   string         `json:"contentHash,omitempty"`
	Size          string         `json:"size"`
	Width         float64        `json:"width"`
	Height        float64        `json:"height"`
	GridCols      int            `json:"gridCols"`
	GridRows      int            `json:"gridRows"`
	CellSize      float64        `json:"cellSize"`
	Terrain       []TerrainTile  `json:"terrain"`
	Tiles         []TileInstance `json:"tiles,omitempty"`
	DefaultTile   *TileCoord     `json:"defaultTile,omitempty"`
	Obstacles     []ObstacleTile `json:"obstacles"`
	Buildings     []BuildingTile `json:"buildings"`
	PlacedUnits   []PlacedUnit   `json:"placedUnits,omitempty"`
	NeutralSpawns []NeutralSpawn `json:"neutralSpawns,omitempty"`
	// Zones are map-authored territorial regions. Empty/omitted on maps that
	// don't use the zone system — a zone-free map behaves exactly as before
	// (build-gate never fires, no zone snapshots). See the Zone type.
	Zones      []Zone      `json:"zones,omitempty"`
	WaveConfig *WaveConfig `json:"waveConfig,omitempty"`
	// Elevation lists the cells raised into a single-level plateau for cliff
	// auto-tiling. Absent/empty on maps without cliffs. See
	// server/internal/game/cliff.go (cliffTileAt / cliffCellBlocks) for the
	// derived per-cell tile + walkability; the client mirrors the same spec.
	Elevation []GridCoord `json:"elevation,omitempty"`
	// CliffTileset is the tileset id of the 4x4 cliff atlas (e.g.
	// "grass-cliff") used to render Elevation cells. Absent on maps without
	// cliffs.
	CliffTileset string `json:"cliffTileset,omitempty"`
	// Ramps lists raised (Elevation) cells that are walkable openings through
	// a cliff wall: they render as the flat plateau-top tile instead of a
	// wall/corner slot and never block movement. A ramp cell that is not
	// itself in Elevation is inert. See server/internal/game/cliff.go
	// (cliffTileAt / cliffCellBlocks) for the derivation.
	Ramps []GridCoord `json:"ramps,omitempty"`
	// Campaign, when set, tags this map as a campaign level. Its presence
	// makes the map (a) hidden from the Custom Game lobby map list, and (b)
	// contribute one level to the CampaignDef tree at startup / catalog-read
	// time. Authored in the map editor's Campaign card. Custom maps and
	// production non-campaign maps leave this nil.
	//
	// The previous design (campaign-objectives-and-metrics) placed objectives
	// on a separate CampaignLevelDef inside catalog/campaigns/*.json. That
	// indirection was removed by the map-editor-authors-campaign-maps change:
	// objectives + display name + prerequisites now live on the map file the
	// editor saves, and catalog/campaigns/*.json shrinks to a header (id,
	// displayName, description, sortOrder, locked).
	Campaign *MapCampaignBlock `json:"campaign,omitempty"`
	Debug    *MapDebugConfig   `json:"debug,omitempty"`
}

// MapCampaignBlock is the wire shape of the "this map is a campaign level"
// tag. When non-nil on a MapConfig, the engine treats the map as a level in
// the campaign with `CampaignID` and exposes it through /api/catalog/campaigns
// under that campaign. Same JSON shape both ways — server reads it from
// catalog/maps/*.json and the editor writes it back via /maps POST.
//
// Objectives are kept as a slice of `MapCampaignObjective` rather than a
// stronger typed union because the per-type `config` shapes live in the
// server's game package (objective_handlers.go) and the protocol layer
// cannot import that without a dependency cycle. Each entry is validated
// once at catalog load by `parseAndValidateObjectiveDef`; bad authored data
// panics at startup with the offending file + level + objective id named.
type MapCampaignBlock struct {
	// CampaignID is the parent campaign's id (e.g. "forest"). Must match a
	// header file in catalog/campaigns/<CampaignID>.json or catalog load
	// panics.
	CampaignID string `json:"campaignId"`
	// LevelID is the stable, globally-unique level id (e.g. "forest_01").
	// Persisted to the player profile's `completedCampaignLevels` and
	// `completedCampaignObjectives` keys.
	LevelID string `json:"levelId"`
	// DisplayName is the human-readable label shown in the campaign panel's
	// level list and in the end-of-match recap.
	DisplayName string `json:"displayName"`
	// PrerequisiteLevelID gates which other level must be completed before
	// this one unlocks. Nil for the first level of a chain. Must reference
	// another level in the SAME campaign (cross-campaign prereqs unsupported).
	PrerequisiteLevelID *string `json:"prerequisiteLevelId"`
	// Description is a short blurb shown on the level row in the campaign
	// panel. Optional.
	Description string `json:"description,omitempty"`
	// SortOrder controls the level row order within the campaign. Ties broken
	// by LevelID.
	SortOrder int `json:"sortOrder,omitempty"`
	// Objectives is the per-level objective list. Each entry mirrors the
	// `game.ObjectiveDef` JSON shape. The game-package catalog loader runs
	// every entry through `parseAndValidateObjectiveDef` at startup, so a
	// reachable map with an invalid objective panics fast.
	Objectives []MapCampaignObjective `json:"objectives,omitempty"`
}

// MapCampaignObjective is the wire shape of one authored objective on a
// campaign-tagged map. Mirrors the JSON tags of `game.ObjectiveDef` so the
// game package can json.Unmarshal directly between the two without a
// per-field hand conversion. The `parsedConfig` cache field on ObjectiveDef
// is unexported and does not appear here.
type MapCampaignObjective struct {
	ID                   string          `json:"id"`
	Type                 string          `json:"type"`
	Description          string          `json:"description,omitempty"`
	Scope                string          `json:"scope,omitempty"`
	Required             bool            `json:"required,omitempty"`
	RewardDominionPoints int             `json:"rewardDominionPoints,omitempty"`
	RewardConquestBadges int             `json:"rewardConquestBadges,omitempty"`
	Config               json.RawMessage `json:"config"`
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
	TriggerRadius   float64 `json:"triggerRadius,omitempty"` // explosive_trap only
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
	// Description is the player-facing tooltip prose for this ability: the
	// author's override when set, otherwise text generated from the ability's
	// configured fields (server-side, the single source of truth — see
	// AbilityDef.EffectiveDescription). The action bar renders it directly so
	// the tooltip text is never hardcoded client-side.
	Description string `json:"description,omitempty"`
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
	// Passive marks an ability that is never manually/auto cast (arcane_missiles).
	// The client hides it from the castable action row (it may surface it as a
	// passive/charge indicator instead).
	Passive bool `json:"passive,omitempty"`
	// SpellSlotRank, when set (bronze/silver/gold), marks this ability as a
	// learnable spell-slot spell the unit gained at that rank
	// (arch-mage-spell-system). The client renders it in the matching perk cell
	// as a CASTABLE slot instead of in the normal ability row.
	SpellSlotRank string `json:"spellSlotRank,omitempty"`
	// ChargeCurrent / ChargeRequired surface a charge-fire passive's progress
	// (arcane_missiles) so the client can draw a charge meter. Both 0 for
	// non-charge abilities.
	ChargeCurrent  float64 `json:"chargeCurrent,omitempty"`
	ChargeRequired float64 `json:"chargeRequired,omitempty"`
	// Projectile is the ability's projectile-def id, if any. The client uses it
	// as the action-icon fallback: an ability with no bundled ability art
	// (assets/abilities/<id>) renders its projectile image instead. Empty for
	// instant/non-projectile abilities.
	Projectile string `json:"projectile,omitempty"`
	// TargetsPoint marks a ground/point-targeted ability (arcane_orb): the
	// client arms a ground-target cursor and sends the clicked world point.
	TargetsPoint bool `json:"targetsPoint,omitempty"`
}

// BeamSnapshot carries one active beam visual to the client. Two flavors:
//
//   - Channeled (Momentary omitted/false): a persistent line between caster and
//     target for the duration of a channel. The client derives endpoints from
//     the LIVE unit positions each frame; Origin/Target coords are not sent.
//
//   - Momentary (Momentary == true): a one-shot proc "zap" that decays quickly.
//     Its endpoints are FROZEN server-side (the participants may have died) and
//     sent as OriginX/Y → TargetX/Y so the client renders it from coords.
//
// FOW-filtered: only included when either endpoint is visible to the viewer.
type BeamSnapshot struct {
	ID           string `json:"id"`
	CasterUnitId int    `json:"casterUnitId"`
	TargetUnitId int    `json:"targetUnitId"`
	OwnerId      string `json:"ownerId"`
	AbilityId    string `json:"abilityId,omitempty"`
	Variant      string `json:"variant,omitempty"`
	// Momentary marks a self-contained one-shot beam flash rendered from the
	// frozen coords below rather than live unit positions.
	Momentary bool `json:"momentary,omitempty"`
	// OriginX/Y and TargetX/Y are the frozen world endpoints of a momentary
	// beam. Only meaningful (and only sent) when Momentary is true.
	OriginX float64 `json:"originX,omitempty"`
	OriginY float64 `json:"originY,omitempty"`
	TargetX float64 `json:"targetX,omitempty"`
	TargetY float64 `json:"targetY,omitempty"`
}

type TerrainTile struct {
	GridCoord
	Terrain string `json:"terrain"`
}

type TileCoord struct {
	Tileset string `json:"tileset"`
	Col     int    `json:"col"`
	Row     int    `json:"row"`
}

// UnmarshalJSON accepts both the current shape (`tileset`/`col`/`row`) and the
// legacy pixel-based shape (`sheet`/`sx`/`sy`) so existing map JSONs continue
// to load without a one-time rewrite. Legacy pixel coordinates are converted
// to grid indices by integer-dividing by the old logical tile size (32px):
// col = sx/32, row = sy/32. Maps re-saved through the editor are written in
// the new shape (default marshaling already emits tileset/col/row).
func (c *TileCoord) UnmarshalJSON(b []byte) error {
	var raw struct {
		Tileset string `json:"tileset"`
		Col     *int   `json:"col"`
		Row     *int   `json:"row"`
		Sheet   string `json:"sheet"`
		SX      *int   `json:"sx"`
		SY      *int   `json:"sy"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw.Tileset != "" || raw.Col != nil || raw.Row != nil {
		c.Tileset = raw.Tileset
		if raw.Col != nil {
			c.Col = *raw.Col
		}
		if raw.Row != nil {
			c.Row = *raw.Row
		}
		return nil
	}
	c.Tileset = raw.Sheet
	if raw.SX != nil {
		c.Col = *raw.SX / 32
	}
	if raw.SY != nil {
		c.Row = *raw.SY / 32
	}
	return nil
}

type TileInstance struct {
	GridCoord
	TileCoord
}

// UnmarshalJSON decodes GridCoord and TileCoord separately rather than
// relying on default struct-embedding decode. This is required, not
// stylistic: TileCoord's custom UnmarshalJSON (pointer receiver) promotes to
// *TileInstance's method set, which would make encoding/json delegate the
// ENTIRE tile payload to TileCoord.UnmarshalJSON and silently drop the
// sibling GridCoord x/y fields if this method were absent.
func (t *TileInstance) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &t.GridCoord); err != nil {
		return err
	}
	return json.Unmarshal(b, &t.TileCoord)
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
	// ShopDisplayName is a computed, snapshot-only label for a shop. When the
	// shop is stocked from a named item list (metadata "itemList"), it is that
	// list's Name (e.g. "Wandering Merchant"); empty otherwise, so the client
	// falls back to the building's type label. Set by the snapshot filter.
	ShopDisplayName string `json:"shopDisplayName,omitempty"`

	// ─── Recipe Shop fields ───────────────────────────────────────────────
	// RecipeInventory is the runtime list of recipes this building sells,
	// with per-slot quantity remaining. Populated by
	// populateRecipeShopInventoriesLocked at match start. Quantity 0 means
	// sold out (entry kept in list so the client renders it disabled),
	// mirroring ShopInventory behaviour.
	RecipeInventory []RecipeStockEntry `json:"recipeInventory,omitempty"`
}

// ShopStockEntry is one item slot in a shop building's inventory.
// Quantity decrements on each successful purchase; when it reaches 0 the
// entry stays in the list (so the client can render it greyed) but
// further purchases of that item from that shop are rejected.
type ShopStockEntry struct {
	ItemID   string `json:"itemId"`
	Quantity int    `json:"quantity"`
}

// RecipeStockEntry is one purchasable recipe slot in a Recipe Shop's inventory.
// Quantity decrements on purchase; 0 means sold out (kept in the list so the
// client can render it disabled), mirroring ShopStockEntry.
//
// ItemID names the item the recipe MAKES. An item is its own recipe (see
// ItemDef.Crafting), so a recipe has no identity of its own to carry here.
type RecipeStockEntry struct {
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
	// KnownCraftableIDs is the list of ITEM IDs whose recipes the player has
	// learned and may craft this match, sourced from the profile's
	// KnownCraftableIDs at join time. Nil / absent means nothing is pre-learned.
	KnownCraftableIDs []string `json:"knownCraftableIds,omitempty"`
	// CachedMapHashes is the set of map contentHashes the client already holds
	// locally for MapID (content-addressed map distribution). The server omits
	// the (gzipped) map from the welcome when the match map's contentHash is in
	// this list — the client renders from its own cache. Empty/absent = "I have
	// nothing", so the welcome carries the map. Transport-agnostic: this is the
	// only signal used for the hit/miss decision, made server-side here.
	CachedMapHashes []string `json:"cachedMapHashes,omitempty"`
	// Ephemeral requests a throwaway editor-playtest match: the server creates
	// a fresh match via MatchManager.NewEphemeralMatch instead of joining/
	// creating a shared FindOrCreateMatch match, and suppresses reward
	// persistence for the duration (see GameState.Ephemeral).
	Ephemeral bool `json:"ephemeral,omitempty"`
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
	// TargetX / TargetY are the clicked world point for GROUND/POINT-targeted
	// abilities (arcane_orb). Ignored for unit-targeted abilities.
	TargetX float64 `json:"targetX,omitempty"`
	TargetY float64 `json:"targetY,omitempty"`
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

// GuardCommandMessage issues an in-place guard stance (like Hold): each unit
// guards the position it is currently standing on, engaging hostiles that enter
// its guard radius and returning to that spot afterward. There is no
// destination — the units do not move to take up the stance.
type GuardCommandMessage struct {
	Type    string `json:"type"`
	UnitIDs []int  `json:"unitIds"`
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
// BuildingID names the blacksmith to research at; empty means "auto-assign to
// any idle blacksmith" (used by the global Blacksmith panel).
type PurchaseUpgradeCommandMessage struct {
	Type       string `json:"type"`
	Track      string `json:"track"`
	BuildingID string `json:"buildingId,omitempty"`
}

// CancelUpgradeCommandMessage cancels a queued upgrade at BuildingID (full
// refund of gold + wood paid). QueueIndex selects the entry: 0 (the default,
// omitted) is the in-progress upgrade; higher indices are queued behind it.
type CancelUpgradeCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
	QueueIndex int    `json:"queueIndex,omitempty"`
}

// UpgradeBuildingCommandMessage requests a tier-up on the specified building
// (townhall → keep → castle, chapel → temple, …). BuildingID must be the ID of
// a tier-upgradeable building the player owns.
type UpgradeBuildingCommandMessage struct {
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

// PurchaseRecipeCommandMessage buys one recipe from a Recipe Shop, unlocking it
// for crafting this match. ItemID names the item the recipe makes — an item is
// its own recipe (see game.ItemDef.Crafting).
type PurchaseRecipeCommandMessage struct {
	Type       string `json:"type"`
	BuildingID string `json:"buildingId"`
	ItemID     string `json:"itemId"`
}

// CraftItemCommandMessage crafts one item at the player's Artificer, consuming
// that item's crafting inputs from the vault plus its craft cost in gold.
type CraftItemCommandMessage struct {
	Type   string `json:"type"`
	ItemID string `json:"itemId"`
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

// UseItemAtCommandMessage uses a consumable from the player's vault as a
// ground-targeted AoE at world point (X, Y): allied units within the item's
// range are affected, with the effect amount split across them unless the
// item def disables splitting.
type UseItemAtCommandMessage struct {
	Type       string  `json:"type"`
	InstanceID int64   `json:"instanceId"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
}

// UseItemOnUnitCommandMessage uses a consumable from the player's vault directly
// on a single unit (the Vault "Items" drag-onto-a-unit-card path). The unit
// receives the item's full effect (no AoE split) and one stack is consumed.
type UseItemOnUnitCommandMessage struct {
	Type       string `json:"type"`
	InstanceID int64  `json:"instanceId"`
	UnitID     int    `json:"unitId"`
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

// UpgradeStatDelta is one stat bonus granted by an upgrade tier (e.g.
// {stat:"maxHp", amount:25}). Emitted in PlayerUpgradeSnapshot.NextStats so the
// client can render the *next* tier's bonuses (which vary per tier).
type UpgradeStatDelta struct {
	Stat   string  `json:"stat"`
	Amount float64 `json:"amount"`
}

// PlayerUpgradeSnapshot describes the current state of one upgrade track for a
// player. Emitted per-player in every MatchSnapshotMessage.Players entry.
type PlayerUpgradeSnapshot struct {
	Track       string `json:"track"`
	DisplayName string `json:"displayName"`
	// Capability is the building capability that offers this track (e.g.
	// "blacksmith-upgrade"). The client shows the track in a selected building's
	// panel when the building holds this capability.
	Capability string `json:"capability"`
	Level      int    `json:"level"`
	// Cap is the absolute max level (total tiers defined). PurchasableCap is the
	// highest level currently unlocked given the player's buildings (per-tier
	// requiresBuilding gates). PurchasableCap <= Cap; when it is strictly less,
	// NextRequirement names what unlocks the next tier.
	Cap            int `json:"cap"`
	PurchasableCap int `json:"purchasableCap"`
	// QueuedCount is how many of this track are queued at the player's building
	// (in progress + waiting). 0 when idle. Level + QueuedCount is the level the
	// player will reach once the queue drains; the next purchase stacks above it.
	QueuedCount  int `json:"queuedCount,omitempty"`
	NextCostGold int `json:"nextCostGold"`
	// NextCostWood is the wood cost of the next level. 0 at cap.
	NextCostWood int `json:"nextCostWood"`
	// NextStats is the stat bonuses the next purchasable tier grants (for the UI
	// tooltip). Empty when the track is fully maxed.
	NextStats []UpgradeStatDelta `json:"nextStats,omitempty"`
	// NextRequirement is a display label for the building that unlocks the next
	// tier when it is gated out (e.g. "Keep", "Castle"). Empty when the next tier
	// is already available or the track is fully maxed.
	NextRequirement string `json:"nextRequirement,omitempty"`
	CanAfford       bool   `json:"canAfford"`
	// CanStart is true when the player can start this upgrade via the global
	// panel's auto-assign path: affordable, below the purchasable cap, and at
	// least one qualifying building is available.
	CanStart bool `json:"canStart"`
	// HasBlacksmith is true when the player owns a fully-built building offering
	// this track's capability. (Name retained for wire compatibility; applies to
	// any upgrade building, not only the blacksmith.)
	HasBlacksmith bool `json:"hasBlacksmith"`
	// ResearchTotal / ResearchRemaining describe an in-progress upgrade for
	// this track (this player, at any building). ResearchTotal is the full
	// duration in seconds (0 when idle); ResearchRemaining counts down to 0.
	// ResearchBuildingID is the source building performing the research (used to
	// target a cancel and to tell a selected building whether it is the one
	// doing the work). While ResearchTotal > 0 the track is locked everywhere.
	ResearchTotal      float64 `json:"researchTotal,omitempty"`
	ResearchRemaining  float64 `json:"researchRemaining,omitempty"`
	ResearchBuildingID string  `json:"researchBuildingId,omitempty"`
	// QueueBuildingID is the building holding this track's queue (in progress
	// or merely queued). Equals ResearchBuildingID when the track is at the head;
	// set even when the track waits behind another. Empty when the track is idle.
	// The cancel/queue target for this track.
	QueueBuildingID string `json:"queueBuildingId,omitempty"`
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
	PlayerID string `json:"playerId"`
	Color    string `json:"color"`
	// TeamID is the player's alliance group. 0 = the default shared team
	// (all players allied — current behavior). Same TeamID ⇒ allies. The
	// client mirrors the server hostility predicate from this value.
	TeamID       int                     `json:"teamId"`
	Resources    []ResourceStock         `json:"resources"`
	Upgrades     []PlayerUpgradeSnapshot `json:"upgrades,omitempty"`
	TownHallTier int                     `json:"townHallTier,omitempty"`
	Vault        []VaultItemSnapshot     `json:"vault"`
	// LockedUnitTypes lists the unit types this player currently cannot
	// train because their RequiresBuildings list is unsatisfied. Empty
	// or omitted = no locks. The client uses this to grey out train
	// actions in the building action panel.
	LockedUnitTypes []string `json:"lockedUnitTypes,omitempty"`
	// UnitCostOverrides carries this player's effective training costs for
	// unit types whose cost differs from the static catalog because of
	// advancements (e.g. the worker goldCost reduction). Only unit types
	// that actually differ are sent; the client overlays these on the
	// catalog cost so the build-menu price matches what the server charges.
	// Owner-relevant only, but harmless on other players' snapshots.
	UnitCostOverrides []UnitCostOverride `json:"unitCostOverrides,omitempty"`
	ActiveBuffs       []ActiveEffectIcon `json:"activeBuffs,omitempty"`
	// CommanderAbilities are the player-level abilities surfaced on the
	// action bar (Smite, Blessing). Always populated for the snapshot's
	// owner so the HUD can render slots even when every ability is ready.
	CommanderAbilities []CommanderAbilitySnapshot `json:"commanderAbilities,omitempty"`
	// ShopRerollsRemaining is the player's remaining merchant-reroll
	// budget for this match. Drives the reroll button on neutral-shop
	// buildings (enabled when > 0).
	ShopRerollsRemaining int `json:"shopRerollsRemaining,omitempty"`
	// UnlockedCraftableIDs is the player's in-match set of ITEM IDs whose
	// recipes they have learned and may craft at an Artificer. Seeded from
	// profile KnownCraftableIDs at join and grown by purchase_recipe commands.
	// Omitted when empty.
	UnlockedCraftableIDs []string `json:"unlockedCraftableIds,omitempty"`
	// Metrics carries this player's cumulative match metrics (gold earned,
	// kills, buildings built, etc). Always present so every snapshot
	// recipient can render the end-of-round per-player comparison columns
	// (§15). For team-scope objectives, the evaluator aggregates these per
	// tick — but the wire still carries the per-player breakdown.
	Metrics MatchMetricsSnapshot `json:"metrics"`
}

// UnitCostOverride is a single unit type's effective training cost when it
// differs from the static catalog def (advancement deltas baked in). Sent on
// PlayerSnapshot.UnitCostOverrides. ResourceCost and MeatCost are the full
// effective values (not deltas), so the client replaces the catalog cost
// outright for that unit type.
type UnitCostOverride struct {
	UnitType     string         `json:"unitType"`
	ResourceCost map[string]int `json:"resourceCost"`
	MeatCost     int            `json:"meatCost"`
}

type UnitSnapshot struct {
	ID           int      `json:"id"`
	OwnerID      string   `json:"ownerId"`
	Color        string   `json:"color"`
	UnitType     string   `json:"unitType"`
	Archetype    string   `json:"archetype,omitempty"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities,omitempty"`
	// Flyer marks the unit as airborne so the client can render an elevation
	// shadow / altitude offset. omitempty so ground units drop the field.
	Flyer   bool   `json:"flyer,omitempty"`
	Visible bool   `json:"visible"`
	Status  string `json:"status,omitempty"`
	// Order is the unit's current standing order (see OrderString* constants).
	// omitempty so old clients receiving snapshots from new servers still parse
	// cleanly — an absent field is treated as "idle" by clients.
	Order       string  `json:"order,omitempty"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	HP          int     `json:"hp"`
	MaxHP       int     `json:"maxHp"`
	Damage      int     `json:"damage,omitempty"`
	AttackSpeed float64 `json:"attackSpeed,omitempty"`
	// AttackRange is the unit's effective attack range in world pixels — base
	// catalog range × any perk range multipliers (eagle_spirit / bullseye).
	// Surfaced so the HUD can display it and the renderer can show range rings
	// for selected units. omitempty so melee units (range 0) drop the field.
	AttackRange float64 `json:"attackRange,omitempty"`
	MoveSpeed   float64 `json:"moveSpeed,omitempty"`
	Armor       int     `json:"armor,omitempty"`
	// CritChance is the unit's effective crit probability against an unmarked
	// target (0..1). Excludes Hunter's Mark since that is target-dependent.
	// omitempty so non-Marksman units (no crit sources) drop the field.
	CritChance float64 `json:"critChance,omitempty"`
	// CritMultiplier is the damage multiplier applied on a successful crit
	// (e.g. 2.0 = double damage). Reported as 0 when the unit has no crit
	// sources so the HUD can hide the row entirely.
	CritMultiplier float64 `json:"critMultiplier,omitempty"`
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
	ExtraPerkSlots map[string]int `json:"extraPerkSlots,omitempty"`
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
	// ObjectiveID was the link to a legacy VictoryCondition in MapConfig.
	// The field carries through the wire for snapshot stability but is no
	// longer consulted by the engine; §6 of the campaign-objectives-and-metrics
	// change removed `MapConfig.VictoryConditions`, and §9 will drop the
	// related state hooks. Map JSONs no longer author this field.
	ObjectiveID string `json:"objectiveId,omitempty"`
	// StunnedRemaining / SlowedRemaining / SlowedMultiplier carry the current CC
	// state to the client each tick so it can render stun/slow indicator icons.
	// All three use omitempty so they are absent from the JSON when not active.
	StunnedRemaining float64 `json:"stunnedRemaining,omitempty"`
	SlowedRemaining  float64 `json:"slowedRemaining,omitempty"`
	SlowedMultiplier float64 `json:"slowedMultiplier,omitempty"`
	// ColdSlowedRemaining / ColdSlowedMultiplier are the cold (chill) slow track,
	// separate from the physical slow above. The client paints an icy overlay
	// while ColdSlowedRemaining > 0. Absent (omitempty) when no chill is active.
	ColdSlowedRemaining  float64 `json:"coldSlowedRemaining,omitempty"`
	ColdSlowedMultiplier float64 `json:"coldSlowedMultiplier,omitempty"`
	// BurningRemaining is the greatest remaining duration across the unit's
	// active burn (fire DoT) stacks — from a fire_sword proc or a Trapper
	// fire_pit perk. The client paints an animated burning overlay while it is
	// > 0. Absent (omitempty) when the unit is not on fire.
	BurningRemaining float64 `json:"burningRemaining,omitempty"`
	// ArcaneCharge is the accumulated Arcane Charge on a unit with the
	// arcane_missiles passive (Arch Mage). The client renders one rotating
	// purple orb above the unit per 10 charge. 0/absent for every other unit.
	ArcaneCharge float64 `json:"arcaneCharge,omitempty"`
	// BurningAnchor is where the burning overlay sits on the unit
	// ("feet" | "center" | "head"), authored server-side in
	// catalog/effects/burning/burning.json. Sent only while burning; absent
	// (omitempty) otherwise, in which case the client falls back to "feet".
	BurningAnchor string `json:"burningAnchor,omitempty"`
	// ChannelLoopStart / ChannelLoopEnd are the inclusive frame range the
	// client loops through (one-way, forward) on the unit's casting sprite
	// sheet while it is channeling a beam ability (Siphon Life). Set only
	// when Status == "Channeling"; absent otherwise. start == end produces
	// a single held pose; start < end produces a small loop at the unit's
	// natural frame cadence. Out-of-range values modulo against the
	// sheet's frame count on the client.
	ChannelLoopStart    int     `json:"channelLoopStart,omitempty"`
	ChannelLoopEnd      int     `json:"channelLoopEnd,omitempty"`
	CarriedResourceType string  `json:"carriedResourceType,omitempty"`
	CarriedAmount       int     `json:"carriedAmount,omitempty"`
	TargetX             float64 `json:"targetX,omitempty"`
	TargetY             float64 `json:"targetY,omitempty"`
	Moving              bool    `json:"moving"`
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
	WorkTargetID string `json:"workTargetId,omitempty"`
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

// WelcomeMessage is the join-time envelope. The map is content-addressed:
// MapID + ContentHash always identify the match map; MapGz carries the full map
// (base64 of gzip'd flat MapConfig JSON) ONLY when the client did not already
// have ContentHash (see JoinMatchMessage.CachedMapHashes). On a cache hit MapGz
// is empty and the client renders the map from its own content-addressed cache.
type WelcomeMessage struct {
	Type        string `json:"type"`
	PlayerID    string `json:"playerId"`
	MatchID     string `json:"matchId"`
	MapID       string `json:"mapId"`
	ContentHash string `json:"contentHash"`
	MapGz       string `json:"mapGz,omitempty"`
}

// RequestMapMessage is the client→server fallback when the client claimed a
// cache hit (MapGz omitted) but could not load ContentHash locally — e.g. the
// cache entry was evicted between the join and the render. The server replies
// with MapContentMessage. This pair is also the seam where chunking would live
// if a single compressed map ever approached the transport size cap.
type RequestMapMessage struct {
	Type        string `json:"type"`
	MapID       string `json:"mapId"`
	ContentHash string `json:"contentHash"`
}

// MapContentMessage carries a full map out-of-band (same gzip+base64 form as
// WelcomeMessage.MapGz), in reply to a RequestMapMessage.
type MapContentMessage struct {
	Type        string `json:"type"`
	MapID       string `json:"mapId"`
	ContentHash string `json:"contentHash"`
	MapGz       string `json:"mapGz"`
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
	// YourDominionPointsEarned is the snapshot viewer's own full per-match
	// earned dominion-point total: per-kill drops plus the win/loss bonus from
	// tuning. Per-viewer: each client sees its own number. The host persists
	// this server-side; a remote joiner reads this field to persist into its
	// own local profile (the host can't write the joiner's profile, which lives
	// on a different machine).
	YourDominionPointsEarned int `json:"yourDominionPointsEarned,omitempty"`
}

// MatchSummary carries per-player match-end data alongside the game-over
// snapshot. Populated once per match when the game ends. The match manager's
// OnGameOver hook calls a DominionPointCommitter (implemented by
// profile.Manager) to persist DominionPointsEarned into the profile —
// simulation code does not touch the profile store directly.
type MatchSummary struct {
	PlayerID             string `json:"playerId"`
	Won                  bool   `json:"won"`
	DominionPointsEarned int    `json:"dominionPointsEarned,omitempty"`
}

// ObjectiveSnapshot carries the current state of one victory condition to the
// client every tick (once the map has VictoryConditions defined). Clients use
// this to render an objective tracker HUD and detect when all are complete.
// ObjectiveSnapshot is the per-tick wire shape for one campaign objective's
// state from the perspective of the snapshot's viewer. Reshaped in §10 of
// campaign-objectives-and-metrics: the old `label`/`progress`/`count` fields
// (designed for the legacy killUnit/destroyBuilding/surviveWaves system)
// are gone, replaced by the richer fields below.
//
// Scope semantics:
//   - team-scope objectives carry the team-aggregated state — every viewer
//     sees identical values.
//   - player-scope objectives carry the VIEWER's own per-player state. Two
//     players in the same lobby see different values on the same objective.
type ObjectiveSnapshot struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	// Scope is the literal "team" or "player". Drives client rendering hints
	// (e.g. team-scope objectives may render a single shared progress bar
	// vs player-scope showing each player's column at end of round).
	Scope string `json:"scope"`
	// Required: when true, this objective must complete for victory to fire
	// (per the §9 AND-gate). Optional objectives never gate victory.
	Required bool `json:"required,omitempty"`
	// Current is the live counter value (e.g. camps killed, gold earned).
	Current int `json:"current"`
	// RequiredCount is the threshold value (e.g. count, amount, wavesToSurvive)
	// the handler uses to declare completion.
	RequiredCount int `json:"requiredCount"`
	// Completed becomes true when Current >= RequiredCount AND the handler
	// has declared the objective complete. Sticky: once true, stays true.
	Completed bool `json:"completed"`
	// Failed is only set by time-boxed objectives (e.g. kill_camps_before_wave).
	// Sticky once set. omitempty so the wire stays compact for the common case.
	Failed bool `json:"failed,omitempty"`
	// RewardDominionPoints is the DP reward this objective grants the first
	// time (ever, per player) it is completed. The match-end client echoes it
	// back with the completion POST so the server can credit it. 0 = no reward.
	RewardDominionPoints int `json:"rewardDominionPoints,omitempty"`
	// RewardConquestBadges is the Conquest Badge reward granted the first time
	// (ever, per player) this objective is completed. 0 = no reward.
	RewardConquestBadges int `json:"rewardConquestBadges,omitempty"`
}

// VictorySnapshot is included on every per-tick MatchSnapshot when the
// launching campaign level declared any objectives. Custom Game matches
// (zero objectives) have nil Victory.
//
// Achieved is the server's authoritative win flag — when true the client
// should show the victory recap. The AND-gate that drives it lives in
// `(*GameState).checkVictoryLocked`: wave/townhall condition met AND every
// required objective complete.
type VictorySnapshot struct {
	Achieved   bool                `json:"achieved"`
	Objectives []ObjectiveSnapshot `json:"objectives"`
}

// MatchMetricsSnapshot mirrors the server-side game.MatchMetrics struct on
// the wire. Identical JSON tags so the marshaled bytes are equivalent; lives
// in the protocol package so PlayerSnapshot can embed it without a circular
// import. See server/internal/game/match_metrics.go for field semantics.
type MatchMetricsSnapshot struct {
	TotalGoldEarned          int            `json:"totalGoldEarned"`
	TotalWoodEarned          int            `json:"totalWoodEarned"`
	TotalEnemiesKilled       int            `json:"totalEnemiesKilled"`
	BuildingsBuilt           int            `json:"buildingsBuilt"`
	BuildingsBuiltByType     map[string]int `json:"buildingsBuiltByType"`
	NeutralCampsKilled       int            `json:"neutralCampsKilled"`
	NeutralCampsKilledByTier map[int]int    `json:"neutralCampsKilledByTier"`
	UnitsTrained             int            `json:"unitsTrained"`
	UnitsTrainedByType       map[string]int `json:"unitsTrainedByType"`
	UnitsByRank              map[string]int `json:"unitsByRank"`
	WavesCleared             int            `json:"wavesCleared"`
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
	// OriginUnitID is the unit the client anchors the SPAWN sprite to (its
	// chest) — the hit enemy for a split bolt spawned at
	// spawnOrigin=current_event_position, etc. 0 (omitted) for an ordinary shot
	// or a pure-position origin, where the client falls back to OwnerUnitID.
	OriginUnitID int     `json:"originUnitId,omitempty"`
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

// MeleeAttackSnapshot is a per-tick record that a melee unit's swing resolved.
// AttackType is the sound key authored on the unit def (or its promotion path)
// — "swing", "stab", etc. — resolved server-side so the client just plays the
// matching effect. X/Y is the attacker's world position at swing time so the
// client can suppress the sound when the swing is off-screen. Drained per tick
// like CritEventSnapshot. Ranged attacks don't push here; their sound is driven
// by the projectile they spawn.
type MeleeAttackSnapshot struct {
	AttackType string  `json:"attackType"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
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

// EvadeEventSnapshot is a one-tick "the defender avoided a basic attack"
// event: the client floats "Dodged!" / "Blocked!" over the unit. Kind is
// "dodge" or "block".
type EvadeEventSnapshot struct {
	UnitID int    `json:"unitId"`
	Kind   string `json:"kind"`
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

// DamageHitSnapshot records one individual landed hit's HP loss so the client
// can split its HP-diff popup into per-hit numbers. Without this the client
// only sees the net HP delta per snapshot and renders two simultaneous
// soldier strikes (12 + 12) as a single "24". Auto-emitted at the HP-loss
// point of applyUnitDamageWithSourceLocked for every hit — the client sums
// the entries for a unit and, when they reconcile with the (post-minor-peel)
// HP delta and there are 2+ of them, draws one staggered popup per hit
// instead of one combined number. Purely visual; gameplay reads nothing
// here, and unmatched entries safely fall back to the single-number popup.
type DamageHitSnapshot struct {
	UnitID int `json:"unitId"`
	Damage int `json:"damage"`
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

// ZoneClaimPointSnapshot is the per-tick wire view of one claim capture point.
// Progress is a normalised 0..1 fraction of the shared defendSeconds; Captured
// is set once the point has been defended to completion (sticky). Entries are
// in the same authored order as Zone.ClaimPoints.
type ZoneClaimPointSnapshot struct {
	Progress float64 `json:"progress"`
	Captured bool    `json:"captured,omitempty"`
}

// ZoneSnapshot is the per-tick wire view of one zone's mutable control state.
// The static geometry (cells, anchor, adjacency, capture type) travels once in
// the WelcomeMessage's MapConfig.Zones; only the fields that change during play
// need to be here. Sent unfiltered — zones are mapper-authored regions that
// should render regardless of fog of war.
//
// Owner is the controlling team/player id, or ZoneCaptureNeutralOwner.
// Contested is true when more than one team occupies a presence zone.
// Progress is the capture/defend timer as a normalised 0..1 fraction of the
// mechanic's configured duration (NOT raw seconds — the client renders it as a
// fill over the zone's cells, so a value > 1 would index past the array).
type ZoneSnapshot struct {
	ID        string  `json:"id"`
	Owner     string  `json:"owner"`
	Contested bool    `json:"contested,omitempty"`
	Progress  float64 `json:"progress,omitempty"`
	// OwnerColor is the controlling player's display color, resolved server-side
	// (the client lacks slot/label data): a specific player owner → their color;
	// the team sentinel → the LOWEST-slot joined player's color; neutral/unowned
	// → empty (the client renders grey). Drives the zone perimeter tint.
	OwnerColor string `json:"ownerColor,omitempty"`
	// ClaimPoints carries per-capture-point progress for a multi-point claim
	// zone, in authored order. Empty for non-claim zones. Drives the "N/M points"
	// HUD and per-point timer bars.
	ClaimPoints []ZoneClaimPointSnapshot `json:"claimPoints,omitempty"`
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
	Type      string         `json:"type"`
	Tick      int            `json:"tick"`
	ServerNow int64          `json:"serverNow"`
	MatchID   string         `json:"matchId"`
	Buildings []BuildingTile `json:"buildings"`
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
	ObstacleMetadata   []ObstacleMetadataPatch     `json:"obstacleMetadata,omitempty"`
	Players            []PlayerSnapshot            `json:"players"`
	Units              []UnitSnapshot              `json:"units"`
	Wave               WaveSnapshot                `json:"wave"`
	Banners            []BannerSnapshot            `json:"banners,omitempty"`
	Traps              []TrapSnapshot              `json:"traps,omitempty"`
	Projectiles        []ProjectileSnapshot        `json:"projectiles,omitempty"`
	Beams              []BeamSnapshot              `json:"beams,omitempty"`
	Effects            []EffectSnapshot            `json:"effects,omitempty"`
	CritEvents         []CritEventSnapshot         `json:"critEvents,omitempty"`
	MeleeAttackEvents  []MeleeAttackSnapshot       `json:"meleeAttackEvents,omitempty"`
	MinorDamageEvents  []MinorDamageEventSnapshot  `json:"minorDamageEvents,omitempty"`
	EvadeEvents        []EvadeEventSnapshot        `json:"evadeEvents,omitempty"`
	HitDamageEvents    []DamageHitSnapshot         `json:"hitDamageEvents,omitempty"`
	DamageTypeHints    []DamageTypeHintSnapshot    `json:"damageTypeHints,omitempty"`
	LethalDamageEvents []LethalDamageEventSnapshot `json:"lethalDamageEvents,omitempty"`
	HealEvents         []HealEventSnapshot         `json:"healEvents,omitempty"`
	ManaRestoreEvents  []ManaRestoreEventSnapshot  `json:"manaRestoreEvents,omitempty"`
	BattleTracker      *BattleTrackerSnapshot      `json:"battleTracker,omitempty"`
	GameOver           *GameOverSnapshot           `json:"gameOver,omitempty"`
	Victory            *VictorySnapshot            `json:"victory,omitempty"`
	Fow                *FogOfWarSnapshot           `json:"fow,omitempty"`
	WaveUpgrade        *WaveUpgradeOfferSnapshot   `json:"waveUpgrade,omitempty"`
	NeutralCamps       []NeutralCampSnapshot       `json:"neutralCamps,omitempty"`
	LootDrops          []LootDropSnapshot          `json:"lootDrops,omitempty"`
	// Zones carries per-tick zone control state (owner / contested / progress).
	// Empty on maps without zones. Static zone geometry is in the welcome
	// MapConfig; this is only the mutable control layer.
	Zones []ZoneSnapshot `json:"zones,omitempty"`

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
	// CombatEvents is a bounded, append-only forensic log of individual landed
	// hits — attacker + target positions, distance, attacker range, and
	// lethality at the instant damage applied. Debug-only; present only when
	// the map arms the battle tracker and capped to the most recent entries
	// server-side. Omitted when empty. Lets a saved battle log answer "did this
	// hit land outside the attacker's range, and what killed the victim?".
	CombatEvents []BattleCombatEvent `json:"combatEvents,omitempty"`
}

// BattleCombatEvent is one landed damage instance captured for forensic debug.
// It records WHERE both units were at the moment damage applied, so an
// investigator can see whether a swing connected beyond the attacker's
// AttackRange, who killed whom, and with what kind of attack. All positions are
// world pixels. Kind mirrors DamageSource.Kind ("melee", "projectile",
// "trap_dot", …). Lethal is true when the victim's HP hit 0 on this hit.
type BattleCombatEvent struct {
	Tick           int     `json:"tick"`
	ElapsedSeconds float64 `json:"t"`
	AttackerID     int     `json:"atkId"`
	AttackerType   string  `json:"atkType"`
	AttackerOwner  string  `json:"atkOwner"`
	AttackerX      float64 `json:"atkX"`
	AttackerY      float64 `json:"atkY"`
	AttackRange    float64 `json:"atkRange"`
	TargetID       int     `json:"tgtId"`
	TargetType     string  `json:"tgtType"`
	TargetOwner    string  `json:"tgtOwner"`
	TargetX        float64 `json:"tgtX"`
	TargetY        float64 `json:"tgtY"`
	Distance       float64 `json:"dist"`
	Damage         int     `json:"dmg"`
	Kind           string  `json:"kind"`
	Lethal         bool    `json:"lethal"`
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
	Type       string `json:"type"` // "loot_collected"
	PlayerID   string `json:"playerId"`
	LootDropID string `json:"lootDropId"`
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
