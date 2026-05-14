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
	Class          string          `json:"class,omitempty"`
	Buildable      *bool           `json:"buildable,omitempty"`
	Width          int             `json:"width"`
	Height         int             `json:"height"`
	MaxHp          float64         `json:"maxHp"`
	BuildSeconds   float64         `json:"buildSeconds"`
	Damage         int             `json:"damage,omitempty"`
	AttackRange    float64         `json:"attackRange,omitempty"`
	AttackSpeed    float64         `json:"attackSpeed,omitempty"`
	VisionRange    float64         `json:"visionRange,omitempty"`
	AttackVisual   json.RawMessage `json:"attackVisual,omitempty"`
	ResourceType   string          `json:"resourceType,omitempty"`
	ResourceAmount int             `json:"resourceAmount,omitempty"`
	ResourceCost   map[string]int  `json:"resourceCost"`
	Capabilities   []string        `json:"capabilities"`
	SpawnUnitTypes []string        `json:"spawnUnitTypes"`
	Metadata       map[string]any  `json:"metadata"`
	Color          string          `json:"color,omitempty"`
	Label          string          `json:"label,omitempty"`
	Hotkey         string          `json:"hotkey,omitempty"`
	Render         json.RawMessage `json:"render,omitempty"`
	// SpriteRender lets a building's sprite extend beyond its grid footprint
	// without affecting pathing, selection hit-testing, or the grid cells
	// the building occupies. Mirrors ObstacleRenderDef semantics (cell units,
	// omitted fields use footprint defaults). Used for e.g. a barracks with
	// a flag pole taller than its footprint, or a wider sprite that spills
	// half a cell over each side.
	SpriteRender *BuildingSpriteRenderDef `json:"spriteRender,omitempty"`
}

// BuildingSpriteRenderDef is the sprite-overflow config for a building.
// Shape parallels ObstacleRenderDef. All fields are in cell units; zero
// means "use default" (offset=0, width/height = footprint).
type BuildingSpriteRenderDef struct {
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	Width   float64 `json:"width,omitempty"`
	Height  float64 `json:"height,omitempty"`
}

// Class returns the building's class, defaulting to "player" when unspecified.
func (d BuildingDef) ClassOrDefault() string {
	if d.Class == "" {
		return "player"
	}
	return d.Class
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
