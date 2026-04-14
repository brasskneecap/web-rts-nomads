package game

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog/building-defs.json
var buildingDefsJSON []byte

// BuildingDef holds the configuration for a buildable building type.
type BuildingDef struct {
	Type           string         `json:"type"`
	Width          int            `json:"width"`
	Height         int            `json:"height"`
	MaxHp          float64        `json:"maxHp"`
	BuildSeconds   float64        `json:"buildSeconds"`
	ResourceCost   map[string]int `json:"resourceCost"`
	Capabilities   []string       `json:"capabilities"`
	SpawnUnitTypes []string       `json:"spawnUnitTypes"`
	Metadata       map[string]any `json:"metadata"`
}

// HpPerSecond returns the build/repair rate for this building type.
func (d BuildingDef) HpPerSecond() float64 {
	if d.BuildSeconds <= 0 {
		return d.MaxHp
	}
	return d.MaxHp / d.BuildSeconds
}

var buildingDefsByType map[string]BuildingDef

func init() {
	var catalog struct {
		Buildings []BuildingDef `json:"buildings"`
	}
	if err := json.Unmarshal(buildingDefsJSON, &catalog); err != nil {
		panic("building-defs.json: " + err.Error())
	}
	buildingDefsByType = make(map[string]BuildingDef, len(catalog.Buildings))
	for _, def := range catalog.Buildings {
		buildingDefsByType[def.Type] = def
	}
}

func getBuildingDef(buildingType string) (BuildingDef, bool) {
	def, ok := buildingDefsByType[buildingType]
	return def, ok
}
