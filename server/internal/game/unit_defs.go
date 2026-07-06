package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

// Embeds the entire per-unit catalog tree. Layout:
//
//	catalog/units/<faction>/<unit>/<unit>.json   — UnitDef for that unit (loaded here)
//	catalog/units/<faction>/<unit>/paths/*.json  — per-path stat modifiers
//	                                                (loaded by path_defs.go)
//
// Adding a new unit: create catalog/units/<faction>/<newunit>/<newunit>.json.
// The faction directory name is taken as-is (no allowlist) and must match the
// JSON's `faction` field; mismatch panics at startup. Adding a brand-new
// faction is the same operation: just place the new <faction> directory in
// catalog/units and drop a unit folder inside.
//
//go:embed catalog/units
var unitDefsFS embed.FS

// UnitDef holds the configuration for a trainable unit type.
// Client-only fields (TrainLabel, Bounds) are passed through to the API
// as-is; the server game logic never reads them.
type UnitDef struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// Faction categorises the unit's default origin for map editor brushing:
	// "raider" | "neutral" | "human". Decoupled from runtime ownership — a
	// "raider" unit can still be assigned to a player slot in the editor for
	// scenarios where the player takes over a raider squad. Required.
	Faction   string `json:"faction"`
	Archetype string `json:"archetype,omitempty"`
	// NonCombat marks the unit as passive: it will not auto-acquire targets in
	// the combat AI, and only engages when the player issues an explicit
	// OrderAttackTarget (via AttackWithUnits). The unit still carries the
	// `"attack"` capability so the player's attack command is accepted.
	NonCombat bool `json:"nonCombat,omitempty"`
	HP        int  `json:"hp"`
	// Armor is the catalog base armor for this unit type. All unit catalog JSON
	// files carry an explicit "armor" field (0 when the unit has no base armor,
	// 33 for soldier). The value is used directly by applyRankModifiersLocked to
	// set unit.Armor for unpathed units, and contributes the advancement-bonus
	// delta for promoted units. Do NOT seed unit.BaseArmor from this field at
	// spawn — BaseArmor is reserved for player-upgrade-track armor only
	// (applyPlayerUpgradesAtSpawnLocked).
	Armor int `json:"armor,omitempty"`
	// SpawnExp is pre-loaded XP granted to a unit at spawn (before any rank
	// modifiers run). Zero-value default is safe; units without this field
	// spawn at 0 XP as before. Used by the "Veteran Initiates" advancement.
	SpawnExp    int     `json:"spawnExp,omitempty"`
	Damage      int     `json:"damage"`
	AttackRange float64 `json:"attackRange"`
	AttackSpeed float64 `json:"attackSpeed"`
	// SplashRadius: when > 0, every attack landing on a primary target also
	// deals the same damage to every other hostile within this radius of the
	// target's position. Direct damage only — does NOT trigger on-attack
	// perks on the splashed targets (so it doesn't chain hunters_mark,
	// savage_strikes, etc.). Friendly fire excluded.
	SplashRadius float64 `json:"splashRadius,omitempty"`
	// MoveSpeed: base pixels-per-second pathing speed. Path multipliers
	// (pathModifierTable) and perk multipliers (momentum) stack on top of it.
	MoveSpeed        float64        `json:"moveSpeed"`
	GoldGatherAmount int            `json:"goldGatherAmount,omitempty"`
	WoodGatherAmount int            `json:"woodGatherAmount,omitempty"`
	ResourceCost     map[string]int `json:"resourceCost"`
	MeatCost         int            `json:"meatCost"`
	SpawnSeconds     float64        `json:"spawnSeconds"`
	Capabilities     []string       `json:"capabilities"`
	TrainLabel       string         `json:"trainLabel,omitempty"`
	// CombatProfile picks the AI behavior profile (target scoring, detection
	// range, ranged-vs-melee, etc.) from combatProfiles in combat_ai_profiles.go.
	// When empty, the server falls back to inferCombatArchetype's hardcoded
	// mapping. Validated against combatProfiles at init; unknown names panic.
	CombatProfile string          `json:"combatProfile,omitempty"`
	AttackVisual  json.RawMessage `json:"attackVisual,omitempty"`
	// AttackType names the melee attack sound the client plays when this unit's
	// swing resolves — "swing", "stab", etc. (keyed to a file in the client's
	// audio/sfx/combat folder). Melee-only: ranged units leave this empty and
	// get their sound from the projectile they fire. A promotion path may
	// override it (see pathCatalogFile.AttackType), e.g. a soldier ("swing")
	// promoting to the vanguard path ("stab"). Purely presentational — the
	// simulation never reads it.
	AttackType string `json:"attackType,omitempty"`
	// Bounds describes the unit's visual footprint (halfWidth, top, bottom
	// offsets from unit.x/unit.y). Client uses it to anchor the sprite's
	// feet, size the selection ring, and compute hit-test rects. Passed
	// through as-is; the server game logic never reads it.
	Bounds json.RawMessage `json:"bounds,omitempty"`
	// Shadow is optional per-unit ground-shadow tuning (enabled, radiusX,
	// radiusY, opacity, offsetX, offsetY). Client-only render config; the
	// server never reads it and only passes it through. Absent ⇒ the client
	// derives a default blob shadow from Bounds.
	Shadow json.RawMessage `json:"shadow,omitempty"`

	// DominionPointDropChance is the per-kill probability that this unit type
	// drops dominion points when killed by a player. Must be in [0,1].
	// Zero means no drop. Overrides the base tuning value from gameplay_tuning.json.
	DominionPointDropChance float64 `json:"dominionPointDropChance,omitempty"`
	// DominionPointAmount is how many dominion points drop when the drop chance
	// triggers. Must be >= 0. Overrides the base tuning value.
	DominionPointAmount int `json:"dominionPointAmount,omitempty"`

	// Experience is the raw XP this unit yields when killed in "split" mode
	// (catalog/tuning/gameplay_tuning.json experience.mode). Pointer so the
	// catalog can distinguish absent (→ splitDefaultXP) from an explicit 0
	// (unit grants no XP). Ignored entirely in "classic" mode.
	Experience *int `json:"experience,omitempty"`

	// VisionRange is the base vision radius in world pixels. When 0 or absent,
	// the spawn path falls back to defaultVisionRange.
	VisionRange float64 `json:"visionRange,omitempty"`

	// Flyer marks the unit as airborne. Flyers ignore terrain and ground-unit
	// obstacles when pathing — only map bounds and other flyers constrain
	// them. They are also a distinct target class: a unit can only attack a
	// flyer if "flyer" appears in its TargetableTypes.
	Flyer bool `json:"flyer,omitempty"`

	// ── Spellcaster kit (optional; zero values = a non-caster unit) ─────────
	// MaxMana / ManaRegenRate seed the unit's mana pool at spawn (Part 3
	// mechanics). CurrentMana starts at MaxMana. Absent ⇒ 0 ⇒ no mana.
	MaxMana       int     `json:"maxMana,omitempty"`
	ManaRegenRate float64 `json:"manaRegenRate,omitempty"`
	// Projectile is the id of a ProjectileDef (Part 1) this unit's basic
	// ranged attack fires (e.g. "fire_bolt"). Empty ⇒ the default procedural
	// shot (Variant = unit type), unchanged from before. Validated at load.
	Projectile string `json:"projectile,omitempty"`
	// DamageType tags this unit's basic attack damage (Part 2). Optional
	// flavor/metadata; empty ⇒ physical. Validated at load when non-empty.
	DamageType DamageType `json:"damageType,omitempty"`
	// ProjectileScale is a render-size multiplier for this unit's projectile
	// sprite, applied client-side on top of the base projectile-sprite scale
	// (e.g. 2 ⇒ twice as large, 0.5 ⇒ half). It is per-unit, not per
	// projectile def, so two units sharing one projectile (e.g. "fire_bolt")
	// can size it differently; a promotion path may override it (see
	// pathCatalogFile.ProjectileScale). Purely visual — never read by the
	// simulation. Omitted / 0 ⇒ the client default (1×). Must be >= 0;
	// validated at load.
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	// Abilities is the unit's ability id list (AbilityDef ids, Part 6), slot
	// order. Not validated at load — an ability may be authored in a later
	// part; resolution is fail-safe at use (getAbilityDef).
	Abilities []string `json:"abilities,omitempty"`

	// TargetableTypes is the set of target classes this unit's attacks are
	// valid against. Recognised entries: "ground", "flyer". When empty, the
	// default is derived at spawn time from AttackVisual.kind: a projectile
	// attack defaults to ["ground","flyer"], any other attack defaults to
	// ["ground"]. Authoring an explicit value overrides the default — e.g.
	// "anti-air only" units would author ["flyer"].
	TargetableTypes []string `json:"targetableTypes,omitempty"`

	// RequiresBuildings is the list of building types the player must own
	// fully built (Visible, not underConstruction) before this unit can
	// be trained. Empty/omitted = no requirement. Multiple entries are
	// ANDed. Validated at load time against the building catalog.
	RequiresBuildings []string `json:"requiresBuildings,omitempty"`

	// ChannelLoop, when set, defines the inclusive frame range the client
	// one-way loops through on this unit's casting sprite sheet while it is
	// channeling a beam ability (Siphon Life, etc.). Pointer-to-struct so
	// the loader can distinguish "field absent" (no channel pose authored —
	// degenerates to frame 0 hold) from "field present" (use these frames).
	// A promotion path may override this via pathCatalogFile.ChannelLoop —
	// resolution order is path > unit. Validated at load (start >= 0,
	// end >= start). Purely visual; server simulation never reads it.
	ChannelLoop *ChannelLoopRange `json:"channelLoop,omitempty"`

	// PathChances is the weighted distribution over promotion paths this unit
	// type rolls the first time it reaches Bronze rank. Keyed by path id (a
	// directory under catalog/units/<faction>/<unit>/paths/), value is a
	// RELATIVE weight: the roll normalizes by the sum, so {"trapper":1,
	// "marksman":1} is a 50/50 split and {"trapper":7,"marksman":3} is 70/30.
	// A single entry (e.g. {"arch_mage":1}) is a guaranteed promotion. Empty /
	// absent ⇒ the unit type has no promotion path (workers, raiders) and
	// stays on unitPathNone. Replaces the old hardcoded per-type switch in
	// assignUnitPathOnRankUpLocked. Validated at load: every key must be a
	// real path dir under this unit (path_defs.go init), every weight >= 0,
	// and the sum must be > 0 when the map is non-empty (loadUnitDefsByType).
	PathChances map[string]float64 `json:"pathChances,omitempty"`

	// ── Advancement-granted bonuses (not authored in unit JSON) ──────────────
	// These three fields are zero in the catalog and are only set by the Archer
	// "Master Huntsman" advancement (advancement_defs.go effect kinds
	// unitBonusArrows / unitTrapEffectMul / unitTrapRadiusMul) on a player's
	// EffectiveUnitDefs copy. They flow def → unit at spawn, so only units whose
	// effective def was modified (i.e. the owning player's archers) carry them.

	// BonusArrows is the number of extra arrows this unit fires per attack on
	// top of any split_shot perk, routed through the split-shot fan-out
	// (fireSplitShotsLocked). Zero ⇒ no bonus arrow.
	BonusArrows int `json:"-"`
	// TrapEffectBonus is an additive fraction applied to the unit's trap
	// EffectMultiplier: the trap pipeline multiplies by (1 + TrapEffectBonus),
	// so 1.0 ⇒ ×2 trap effect strength. Zero ⇒ no change.
	TrapEffectBonus float64 `json:"-"`
	// TrapRadiusBonus is the same additive fraction for the trap
	// RadiusMultiplier: (1 + TrapRadiusBonus), so 1.0 ⇒ ×2 trap radius. Zero ⇒
	// no change.
	TrapRadiusBonus float64 `json:"-"`
}

// ChannelLoopRange is an inclusive [Start, End] frame range on a unit's
// casting sprite sheet. The client one-way loops through these frames at
// the unit's normal frame cadence while the unit is channeling. start == end
// produces a single held pose; start < end produces a small loop. Defined
// at package scope so UnitDef and pathCatalogFile can share the type.
type ChannelLoopRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Target class strings recognised by TargetableTypes. Kept as a small closed
// set so misspellings in JSON are caught at catalog load.
const (
	TargetClassGround = "ground"
	TargetClassFlyer  = "flyer"
)

// unitDefsByType MUST remain a var initializer (not init()) because
// maps.go's mapCatalog var initializer references it via
// `_ = unitDefsByType` to force dependency-ordered loading
// (placedUnits hydration calls getUnitDef during catalog load).
// All var initializers run before any init() function, so converting
// this to init() would cause every map's placedUnits to be silently
// dropped at startup with "unknown unitType" warnings.
//
// loadUnitDefsByType validates each unit's RequiresBuildings against
// the building catalog, which means buildingDefsByType must also be
// a var initializer; Go's dependency analysis then orders building
// defs before unit defs automatically.
var unitDefsByType = loadUnitDefsByType()

func loadUnitDefsByType() map[string]UnitDef {
	// Two-level directory layout: catalog/units/<faction>/<unit>/<unit>.json.
	// Faction directory names are accepted as-is; the unit directory name
	// must match the JSON's "type" field; the JSON's "faction" field must
	// match its parent directory. Any drift panics at startup so the catalog
	// stays coherent.
	factionEntries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	result := make(map[string]UnitDef, 16)
	for _, factionEntry := range factionEntries {
		if !factionEntry.IsDir() {
			panic("catalog/units: unexpected file at root " + factionEntry.Name() + " — top-level entries must be faction directories")
		}
		factionKey := factionEntry.Name()
		unitEntries, err := fs.ReadDir(unitDefsFS, "catalog/units/"+factionKey)
		if err != nil {
			panic("catalog/units/" + factionKey + ": " + err.Error())
		}
		for _, entry := range unitEntries {
			if !entry.IsDir() {
				panic("catalog/units/" + factionKey + ": unexpected file " + entry.Name() + " — units must live at catalog/units/<faction>/<unit>/<unit>.json")
			}
			unitKey := entry.Name()
			rel := "catalog/units/" + factionKey + "/" + unitKey + "/" + unitKey + ".json"
			data, err := unitDefsFS.ReadFile(rel)
			if err != nil {
				panic(rel + ": " + err.Error())
			}
			var def UnitDef
			if err := json.Unmarshal(data, &def); err != nil {
				panic(rel + ": " + err.Error())
			}
			if def.Type == "" {
				panic(rel + `: missing "type" field`)
			}
			if def.Type != unitKey {
				panic(rel + ": def.Type " + def.Type + " does not match directory name " + unitKey)
			}
			if def.Faction != factionKey {
				panic(rel + `: def.Faction "` + def.Faction + `" does not match parent directory "` + factionKey + `"`)
			}
			if def.CombatProfile != "" {
				if _, ok := combatProfiles[def.CombatProfile]; !ok {
					panic(rel + `: combatProfile "` + def.CombatProfile + `" is not a known profile (see combat_ai_profiles.go)`)
				}
			}
			if def.DominionPointDropChance < 0 || def.DominionPointDropChance > 1 {
				panic(rel + `: unit "` + def.Type + `": dominionPointDropChance must be in [0,1]`)
			}
			if def.DominionPointAmount < 0 {
				panic(rel + `: unit "` + def.Type + `": dominionPointAmount must be >= 0`)
			}
			for _, t := range def.TargetableTypes {
				if t != TargetClassGround && t != TargetClassFlyer {
					panic(rel + `: unit "` + def.Type + `": targetableTypes entry "` + t + `" must be one of "ground" | "flyer"`)
				}
			}
			if def.DamageType != "" && !IsValidDamageType(def.DamageType) {
				panic(rel + `: damageType "` + string(def.DamageType) + `" is not a registered damage type`)
			}
			if def.Projectile != "" {
				if _, ok := getProjectileDef(def.Projectile); !ok {
					panic(rel + `: projectile "` + def.Projectile + `" is not a registered projectile def`)
				}
			}
			if def.ProjectileScale < 0 {
				panic(rel + `: unit "` + def.Type + `": projectileScale must be >= 0 (0/omitted ⇒ client default 1×)`)
			}
			if def.ChannelLoop != nil {
				if def.ChannelLoop.Start < 0 {
					panic(rel + `: unit "` + def.Type + `": channelLoop.start must be >= 0`)
				}
				if def.ChannelLoop.End < def.ChannelLoop.Start {
					panic(rel + `: unit "` + def.Type + `": channelLoop.end must be >= channelLoop.start`)
				}
			}
			if def.MaxMana < 0 || def.ManaRegenRate < 0 {
				panic(rel + `: maxMana and manaRegenRate must be >= 0`)
			}
			if len(def.PathChances) > 0 {
				// Weights are relative and normalized at roll time, so any
				// non-negative set with a positive sum is valid. Per-key
				// path-existence is cross-checked in path_defs.go init (the
				// paths catalog is not yet loaded at this var-init stage).
				var pathWeightSum float64
				for path, weight := range def.PathChances {
					if weight < 0 {
						panic(rel + `: unit "` + def.Type + `": pathChances["` + path + `"] must be >= 0`)
					}
					pathWeightSum += weight
				}
				if pathWeightSum <= 0 {
					panic(rel + `: unit "` + def.Type + `": pathChances weights must sum to > 0`)
				}
			}
			for _, b := range def.RequiresBuildings {
				if _, ok := getBuildingDef(b); !ok {
					panic(rel + `: requiresBuildings entry "` + b +
						`" is not a registered building type`)
				}
			}
			if _, dup := result[def.Type]; dup {
				panic(rel + `: duplicate unit type "` + def.Type + `" — type ids must be globally unique across factions`)
			}
			result[def.Type] = def
		}
	}
	return result
}

func getUnitDef(unitType string) (UnitDef, bool) {
	def, ok := unitDefsByType[unitType]
	return def, ok
}

func ListUnitDefs() []UnitDef {
	defs := make([]UnitDef, 0, len(unitDefsByType))
	for _, def := range unitDefsByType {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Type < defs[j].Type })
	return defs
}
