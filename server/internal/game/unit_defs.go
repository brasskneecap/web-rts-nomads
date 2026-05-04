package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

// Embeds the entire per-unit catalog tree. Layout:
//
//	catalog/units/<unit>/<unit>.json   — UnitDef for that unit (loaded here)
//	catalog/units/<unit>/paths/*.json  — per-path stat modifiers for that unit
//	                                     (loaded by path_defs.go from the same tree)
//
// Adding a new unit: create catalog/units/<newunit>/<newunit>.json. Adding a
// promotion path to an existing unit: drop a file under that unit's paths/
// subfolder.
//
//go:embed catalog/units
var unitDefsFS embed.FS

// UnitDef holds the configuration for a trainable unit type.
// Client-only fields (TrainLabel, Bounds) are passed through to the API
// as-is; the server game logic never reads them.
type UnitDef struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
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
	// MoveSpeed: base pixels-per-second pathing speed. Path multipliers
	// (pathModifierTable) and perk multipliers (momentum) stack on top of it.
	MoveSpeed        float64         `json:"moveSpeed"`
	GoldGatherAmount int             `json:"goldGatherAmount,omitempty"`
	WoodGatherAmount int             `json:"woodGatherAmount,omitempty"`
	ResourceCost     map[string]int  `json:"resourceCost"`
	MeatCost         int             `json:"meatCost"`
	SpawnSeconds     float64         `json:"spawnSeconds"`
	Capabilities     []string        `json:"capabilities"`
	TrainLabel       string          `json:"trainLabel,omitempty"`
	// CombatProfile picks the AI behavior profile (target scoring, detection
	// range, ranged-vs-melee, etc.) from combatProfiles in combat_ai_profiles.go.
	// When empty, the server falls back to inferCombatArchetype's hardcoded
	// mapping. Validated against combatProfiles at init; unknown names panic.
	CombatProfile    string          `json:"combatProfile,omitempty"`
	AttackVisual     json.RawMessage `json:"attackVisual,omitempty"`
	// Bounds describes the unit's visual footprint (halfWidth, top, bottom
	// offsets from unit.x/unit.y). Client uses it to anchor the sprite's
	// feet, size the selection ring, and compute hit-test rects. Passed
	// through as-is; the server game logic never reads it.
	Bounds json.RawMessage `json:"bounds,omitempty"`
}

var unitDefsByType = loadUnitDefsByType()

func loadUnitDefsByType() map[string]UnitDef {
	// Per-unit directory layout: each catalog/units/<dir>/ holds that unit's
	// JSON at <dir>/<dir>.json. Walk top-level directories only; path JSONs
	// live under <dir>/paths/ and are loaded by path_defs.go.
	entries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	result := make(map[string]UnitDef, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			// Loose files at the catalog root are a structural mistake — every
			// unit now lives inside its own subdirectory. Panic so the mistake
			// is caught at startup rather than producing a silent mis-load.
			panic("catalog/units: unexpected file at root " + entry.Name() + " — unit JSONs must live at catalog/units/<unit>/<unit>.json")
		}
		unitKey := entry.Name()
		rel := "catalog/units/" + unitKey + "/" + unitKey + ".json"
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
			// Directory name is the canonical id. Mismatch means someone edited
			// one without the other; fail loud so the catalog stays coherent.
			panic(rel + ": def.Type " + def.Type + " does not match directory name " + unitKey)
		}
		if def.CombatProfile != "" {
			if _, ok := combatProfiles[def.CombatProfile]; !ok {
				panic(rel + `: combatProfile "` + def.CombatProfile + `" is not a known profile (see combat_ai_profiles.go)`)
			}
		}
		result[def.Type] = def
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
