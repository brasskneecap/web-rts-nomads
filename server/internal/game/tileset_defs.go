package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/tilesets/*.json
var tilesetDefsFS embed.FS

// TilesetDef describes one terrain tileset image sheet available to the
// (future) tileset editor: which embedded PNG it points at, and how that
// sheet is sliced into a grid of tiles.
type TilesetDef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
	OffsetX    int    `json:"offsetX"`
	OffsetY    int    `json:"offsetY"`
	TileWidth  int    `json:"tileWidth"`
	TileHeight int    `json:"tileHeight"`
	SpacingX   int    `json:"spacingX"`
	SpacingY   int    `json:"spacingY"`
}

// tilesetDefsByID is the package-level map of tileset definitions loaded
// once at startup from catalog/tilesets/*.json. Never mutated after
// initialisation.
var tilesetDefsByID = loadTilesetDefs()

func loadTilesetDefs() map[string]TilesetDef {
	entries, err := fs.ReadDir(tilesetDefsFS, "catalog/tilesets")
	if err != nil {
		panic("catalog/tilesets: " + err.Error())
	}
	result := make(map[string]TilesetDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		data, readErr := tilesetDefsFS.ReadFile("catalog/tilesets/" + filename)
		if readErr != nil {
			panic("catalog/tilesets/" + filename + ": " + readErr.Error())
		}
		var def TilesetDef
		if jsonErr := json.Unmarshal(data, &def); jsonErr != nil {
			panic("catalog/tilesets/" + filename + ": " + jsonErr.Error())
		}
		if def.ID == "" {
			panic("catalog/tilesets/" + filename + `: missing "id"`)
		}
		if def.Image == "" {
			panic("catalog/tilesets/" + filename + `: missing "image"`)
		}
		if def.Cols <= 0 {
			panic("catalog/tilesets/" + filename + `: "cols" must be > 0`)
		}
		if def.Rows <= 0 {
			panic("catalog/tilesets/" + filename + `: "rows" must be > 0`)
		}
		if def.TileWidth <= 0 {
			panic("catalog/tilesets/" + filename + `: "tileWidth" must be > 0`)
		}
		if def.TileHeight <= 0 {
			panic("catalog/tilesets/" + filename + `: "tileHeight" must be > 0`)
		}
		if _, dup := result[def.ID]; dup {
			panic(`catalog/tilesets/` + filename + `: duplicate id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// ListTilesetDefs returns all registered tileset definitions (embedded
// baseline plus any runtime editor overlay) sorted by ID ascending.
func ListTilesetDefs() []TilesetDef {
	all := currentTilesetDefs()
	defs := make([]TilesetDef, 0, len(all))
	for _, d := range all {
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].ID < defs[j].ID
	})
	return defs
}

// GetTilesetDef returns the TilesetDef for id (embedded baseline overlaid
// with any runtime editor save) and whether it was found.
func GetTilesetDef(id string) (TilesetDef, bool) {
	def, ok := currentTilesetDefs()[id]
	return def, ok
}
