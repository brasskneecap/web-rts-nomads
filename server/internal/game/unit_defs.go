package game

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog/unit-defs.json
var unitDefsJSON []byte

// UnitDef holds the configuration for a trainable unit type.
type UnitDef struct {
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	HP           int            `json:"hp"`
	Damage       int            `json:"damage"`
	AttackRange  float64        `json:"attackRange"`
	AttackSpeed  float64        `json:"attackSpeed"`
	ResourceCost map[string]int `json:"resourceCost"`
	MeatCost     int            `json:"meatCost"`
	SpawnSeconds float64        `json:"spawnSeconds"`
	Capabilities []string       `json:"capabilities"`
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
