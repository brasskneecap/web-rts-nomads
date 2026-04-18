package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/units/*.json
var unitDefsFS embed.FS

// UnitDef holds the configuration for a trainable unit type.
// Client-only fields (TrainLabel, Render) are passed through to the API
// as-is; the server game logic never reads them.
type UnitDef struct {
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Archetype   string  `json:"archetype,omitempty"`
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
	AttackVisual     json.RawMessage `json:"attackVisual,omitempty"`
	Render           json.RawMessage `json:"render,omitempty"`
	// RenderVariants holds optional alternate render definitions keyed by
	// unit path (e.g. "vanguard", "berserker"). Passed through to the client
	// as-is; the server game logic never reads it.
	RenderVariants json.RawMessage `json:"renderVariants,omitempty"`
}

var unitDefsByType map[string]UnitDef

func init() {
	// Each file under catalog/units/ is a single UnitDef object. The filename
	// (minus ".json") is expected to match def.Type; the file content is the
	// authoritative source.
	entries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	unitDefsByType = make(map[string]UnitDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := unitDefsFS.ReadFile("catalog/units/" + entry.Name())
		if err != nil {
			panic("catalog/units/" + entry.Name() + ": " + err.Error())
		}
		var def UnitDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/units/" + entry.Name() + ": " + err.Error())
		}
		unitDefsByType[def.Type] = def
	}
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
