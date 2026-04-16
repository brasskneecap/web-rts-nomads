package game

import (
	_ "embed"
	"encoding/json"
	"sort"
)

//go:embed catalog/building-defs.json
var buildingDefsJSON []byte

// BuildingDef holds the configuration for a buildable building type.
// Client-only fields (Color, Label, Hotkey, Render) are passed through to the
// API as-is; the server game logic never reads them.
type BuildingDef struct {
	Type           string          `json:"type"`
	Buildable      *bool           `json:"buildable,omitempty"`
	Width          int             `json:"width"`
	Height         int             `json:"height"`
	MaxHp          float64         `json:"maxHp"`
	BuildSeconds   float64         `json:"buildSeconds"`
	Damage         int             `json:"damage,omitempty"`
	AttackRange    float64         `json:"attackRange,omitempty"`
	AttackSpeed    float64         `json:"attackSpeed,omitempty"`
	ResourceCost   map[string]int  `json:"resourceCost"`
	Capabilities   []string        `json:"capabilities"`
	SpawnUnitTypes []string        `json:"spawnUnitTypes"`
	Metadata       map[string]any  `json:"metadata"`
	Color          string          `json:"color,omitempty"`
	Label          string          `json:"label,omitempty"`
	Hotkey         string          `json:"hotkey,omitempty"`
	Render         json.RawMessage `json:"render,omitempty"`
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

func (d BuildingDef) IsBuildable() bool {
	return d.Buildable == nil || *d.Buildable
}

func ListBuildingDefs() []BuildingDef {
	defs := make([]BuildingDef, 0, len(buildingDefsByType))
	for _, def := range buildingDefsByType {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Type < defs[j].Type })
	return defs
}
