package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/buildings/*.json
var buildingDefsFS embed.FS

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
	AttackVisual   json.RawMessage `json:"attackVisual,omitempty"`
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
	// Each file under catalog/buildings/ is a single BuildingDef object.
	entries, err := fs.ReadDir(buildingDefsFS, "catalog/buildings")
	if err != nil {
		panic("catalog/buildings: " + err.Error())
	}
	buildingDefsByType = make(map[string]BuildingDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := buildingDefsFS.ReadFile("catalog/buildings/" + entry.Name())
		if err != nil {
			panic("catalog/buildings/" + entry.Name() + ": " + err.Error())
		}
		var def BuildingDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/buildings/" + entry.Name() + ": " + err.Error())
		}
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
