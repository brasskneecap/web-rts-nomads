package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/obstacles/*.json
var obstacleDefsFS embed.FS

// ObstacleDef holds the configuration for an obstacle type (tree, rock, wall).
// Obstacles block pathing and can optionally be selectable, harvestable, or
// destructible. Client-only fields (Color, Label, Render, SelectionRing)
// pass through to the catalog API untouched; the server game logic never
// reads them.
type ObstacleDef struct {
	Type           string                    `json:"type"`
	Width          int                       `json:"width"`
	Height         int                       `json:"height"`
	MaxHp          float64                   `json:"maxHp"`
	Selectable     bool                      `json:"selectable"`
	BlocksPathing  bool                      `json:"blocksPathing"`
	ResourceType   string                    `json:"resourceType,omitempty"`
	ResourceAmount int                       `json:"resourceAmount,omitempty"`
	Capabilities   []string                  `json:"capabilities"`
	Color          string                    `json:"color,omitempty"`
	Label          string                    `json:"label,omitempty"`
	Render         *ObstacleRenderDef        `json:"render,omitempty"`
	SelectionRing  *ObstacleSelectionRingDef `json:"selectionRing,omitempty"`
}

// ObstacleRenderDef lets an obstacle's sprite extend beyond its grid
// footprint without affecting pathing or hit-testing. All fields are in
// *cell units*. Zero values mean "use default" (offset=0, width/height =
// grid footprint) so authors only specify the deltas they care about.
type ObstacleRenderDef struct {
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	Width   float64 `json:"width,omitempty"`
	Height  float64 `json:"height,omitempty"`
}

// ObstacleSelectionRingDef nudges where the yellow selection/hover ring
// sits relative to an obstacle's grid footprint. All fields are in *cell
// units*. Defaults (when a field is zero / omitted):
//   offsetX = footprintWidth / 2          (ring centered horizontally)
//   offsetY = footprintHeight * 0.95      (ring near the footprint bottom)
//   radiusX = footprintWidth * 0.55
//   radiusY = radiusX * 0.34
// offsetX/offsetY are measured from the footprint's top-left corner, so
// offsetX = 0.5 in a 1×1 footprint centers the ring on the tile's middle.
// Leaving the block off entirely preserves the default ground-ring look.
type ObstacleSelectionRingDef struct {
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	RadiusX float64 `json:"radiusX,omitempty"`
	RadiusY float64 `json:"radiusY,omitempty"`
}

// obstacleDefsByType is initialized as a package-level var so it is ready
// before any other package-level var initializer runs (Go runs variable
// initializers before init() functions, but package-level var initializers
// are ordered by dependency, and `mapCatalog` in maps.go calls
// hydrateObstacles which depends on this map being populated).
var obstacleDefsByType = loadObstacleDefs()

func loadObstacleDefs() map[string]ObstacleDef {
	entries, err := fs.ReadDir(obstacleDefsFS, "catalog/obstacles")
	if err != nil {
		panic("catalog/obstacles: " + err.Error())
	}
	defs := make(map[string]ObstacleDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := obstacleDefsFS.ReadFile("catalog/obstacles/" + entry.Name())
		if err != nil {
			panic("catalog/obstacles/" + entry.Name() + ": " + err.Error())
		}
		var def ObstacleDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/obstacles/" + entry.Name() + ": " + err.Error())
		}
		defs[def.Type] = def
	}
	return defs
}

func getObstacleDef(obstacleType string) (ObstacleDef, bool) {
	def, ok := obstacleDefsByType[obstacleType]
	return def, ok
}

func ListObstacleDefs() []ObstacleDef {
	defs := make([]ObstacleDef, 0, len(obstacleDefsByType))
	for _, def := range obstacleDefsByType {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Type < defs[j].Type })
	return defs
}
