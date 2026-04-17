package game

import (
	_ "embed"
	"encoding/json"
	"sort"
)

//go:embed catalog/unit-defs.json
var unitDefsJSON []byte

// UnitDef holds the configuration for a trainable unit type.
// Client-only fields (TrainLabel, Render) are passed through to the API
// as-is; the server game logic never reads them.
type UnitDef struct {
	Type             string          `json:"type"`
	Name             string          `json:"name"`
	Archetype        string          `json:"archetype,omitempty"`
	HP               int             `json:"hp"`
	Damage           int             `json:"damage"`
	AttackRange      float64         `json:"attackRange"`
	AttackSpeed      float64         `json:"attackSpeed"`
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
	var catalog struct {
		Units []UnitDef `json:"units"`
	}
	if err := json.Unmarshal(unitDefsJSON, &catalog); err != nil {
		panic("unit-defs.json: " + err.Error())
	}
	unitDefsByType = make(map[string]UnitDef, len(catalog.Units))
	for _, def := range catalog.Units {
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
