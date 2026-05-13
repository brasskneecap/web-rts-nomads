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
// Adding a new unit: create catalog/units/<faction>/<newunit>/<newunit>.json
// where <faction> is one of "raider" | "neutral" | "human" | "wildborne". The
// directory name must match the JSON's `faction` field; mismatch panics at
// startup.
//
//go:embed catalog/units
var unitDefsFS embed.FS

// validFactions lists the faction directory names accepted under catalog/units.
// Mirrors the runtime-validated values on UnitDef.Faction.
var validFactions = map[string]struct{}{
	"raider":    {},
	"neutral":   {},
	"human":     {},
	"wildborne": {},
}

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
	NonCombat   bool    `json:"nonCombat,omitempty"`
	HP          int     `json:"hp"`
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
	// Bounds describes the unit's visual footprint (halfWidth, top, bottom
	// offsets from unit.x/unit.y). Client uses it to anchor the sprite's
	// feet, size the selection ring, and compute hit-test rects. Passed
	// through as-is; the server game logic never reads it.
	Bounds json.RawMessage `json:"bounds,omitempty"`

	// LegendPointDropChance is the per-kill probability that this unit type
	// drops legend points when killed by a player. Must be in [0,1].
	// Zero means no drop. Overrides the base tuning value from gameplay_tuning.json.
	LegendPointDropChance float64 `json:"legendPointDropChance,omitempty"`
	// LegendPointAmount is how many legend points drop when the drop chance
	// triggers. Must be >= 0. Overrides the base tuning value.
	LegendPointAmount int `json:"legendPointAmount,omitempty"`

	// Flyer marks the unit as airborne. Flyers ignore terrain and ground-unit
	// obstacles when pathing — only map bounds and other flyers constrain
	// them. They are also a distinct target class: a unit can only attack a
	// flyer if "flyer" appears in its TargetableTypes.
	Flyer bool `json:"flyer,omitempty"`

	// TargetableTypes is the set of target classes this unit's attacks are
	// valid against. Recognised entries: "ground", "flyer". When empty, the
	// default is derived at spawn time from AttackVisual.kind: a projectile
	// attack defaults to ["ground","flyer"], any other attack defaults to
	// ["ground"]. Authoring an explicit value overrides the default — e.g.
	// "anti-air only" units would author ["flyer"].
	TargetableTypes []string `json:"targetableTypes,omitempty"`
}

// Target class strings recognised by TargetableTypes. Kept as a small closed
// set so misspellings in JSON are caught at catalog load.
const (
	TargetClassGround = "ground"
	TargetClassFlyer  = "flyer"
)

var unitDefsByType = loadUnitDefsByType()

func loadUnitDefsByType() map[string]UnitDef {
	// Two-level directory layout: catalog/units/<faction>/<unit>/<unit>.json.
	// The faction directory name must match one of validFactions; the unit
	// directory name must match the JSON's "type" field; the JSON's "faction"
	// field must match its parent directory. Any drift panics at startup so
	// the catalog stays coherent.
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
		if _, ok := validFactions[factionKey]; !ok {
			panic("catalog/units: unknown faction directory " + factionKey + ` — must be one of "raider" | "neutral" | "human" | "wildborne"`)
		}
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
			if def.LegendPointDropChance < 0 || def.LegendPointDropChance > 1 {
				panic(rel + `: unit "` + def.Type + `": legendPointDropChance must be in [0,1]`)
			}
			if def.LegendPointAmount < 0 {
				panic(rel + `: unit "` + def.Type + `": legendPointAmount must be >= 0`)
			}
			for _, t := range def.TargetableTypes {
				if t != TargetClassGround && t != TargetClassFlyer {
					panic(rel + `: unit "` + def.Type + `": targetableTypes entry "` + t + `" must be one of "ground" | "flyer"`)
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
