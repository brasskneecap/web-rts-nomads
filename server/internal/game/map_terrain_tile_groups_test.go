package game

import (
	"encoding/json"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

func terrainTileEntry() MapCatalogEntry {
	return MapCatalogEntry{
		ID:   "tt-test",
		Name: "Terrain/Tile Test",
		Map: protocol.MapConfig{
			ID:       "tt-test",
			Width:    640,
			Height:   640,
			GridCols: 10,
			GridRows: 10,
			CellSize: 64,
			Terrain: []protocol.TerrainTile{
				{GridCoord: protocol.GridCoord{X: 1, Y: 1}, Terrain: "grass"},
				{GridCoord: protocol.GridCoord{X: 2, Y: 1}, Terrain: "grass"},
				{GridCoord: protocol.GridCoord{X: 3, Y: 3}, Terrain: "dirt"},
			},
			Tiles: []protocol.TileInstance{
				{GridCoord: protocol.GridCoord{X: 1, Y: 1}, TileCoord: protocol.TileCoord{Tileset: "tileset", Col: 2, Row: 1}},
				{GridCoord: protocol.GridCoord{X: 2, Y: 1}, TileCoord: protocol.TileCoord{Tileset: "tileset", Col: 2, Row: 1}},
				{GridCoord: protocol.GridCoord{X: 5, Y: 5}, TileCoord: protocol.TileCoord{Tileset: "tileset", Col: 2, Row: 2}},
			},
			Obstacles: []protocol.ObstacleTile{},
			Buildings: []protocol.BuildingTile{},
		},
	}
}

// TestTerrainTile_MarshalGrouped verifies the grouped on-disk shape: terrain is
// an object keyed by type, tiles an array of {sheet,sx,sy,locations} groups.
func TestTerrainTile_MarshalGrouped(t *testing.T) {
	raw, err := json.Marshal(terrainTileEntry())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"terrain":{"dirt":[[3,3]],"grass":[[1,1],[2,1]]}`) {
		t.Errorf("terrain not grouped as expected: %s", s)
	}
	// Tiles: array of groups, each distinct (tileset,col,row) once, with locations.
	if !strings.Contains(s, `"tileset":"tileset","col":2,"row":1,"locations":[[1,1],[2,1]]`) {
		t.Errorf("tile group (col2,row1) missing/incorrect: %s", s)
	}
	if strings.Count(s, `"col":2,"row":1`) != 1 {
		t.Errorf("tile (col2,row1) metadata should appear once: %s", s)
	}
	// No per-tile x/y objects remain in the tiles section.
	if strings.Contains(s, `{"x":1,"y":1,"tileset"`) {
		t.Errorf("tiles must not contain flat per-cell entries: %s", s)
	}
}

// TestTerrainTile_RoundTrip verifies marshal→unmarshal preserves terrain and
// tile cells.
func TestTerrainTile_RoundTrip(t *testing.T) {
	original := terrainTileEntry()
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MapCatalogEntry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Map.Terrain) != len(original.Map.Terrain) {
		t.Fatalf("terrain count: want %d got %d", len(original.Map.Terrain), len(decoded.Map.Terrain))
	}
	terr := map[[2]int]string{}
	for _, tt := range decoded.Map.Terrain {
		terr[[2]int{tt.X, tt.Y}] = tt.Terrain
	}
	for _, want := range original.Map.Terrain {
		if terr[[2]int{want.X, want.Y}] != want.Terrain {
			t.Errorf("terrain (%d,%d): want %q", want.X, want.Y, want.Terrain)
		}
	}

	if len(decoded.Map.Tiles) != len(original.Map.Tiles) {
		t.Fatalf("tile count: want %d got %d", len(original.Map.Tiles), len(decoded.Map.Tiles))
	}
	tiles := map[[2]int]protocol.TileCoord{}
	for _, ti := range decoded.Map.Tiles {
		tiles[[2]int{ti.X, ti.Y}] = ti.TileCoord
	}
	for _, want := range original.Map.Tiles {
		got := tiles[[2]int{want.X, want.Y}]
		if got != want.TileCoord {
			t.Errorf("tile (%d,%d): %+v != %+v", want.X, want.Y, got, want.TileCoord)
		}
	}
}

// TestTerrainTile_LegacyArraysLoad verifies the old flat terrain + tile arrays
// still unmarshal.
func TestTerrainTile_LegacyArraysLoad(t *testing.T) {
	legacy := `{
		"id":"legacy","name":"L","description":"","sortOrder":0,
		"map":{"id":"legacy","width":640,"height":640,"gridCols":10,"gridRows":10,"cellSize":64,
			"terrain":[{"x":1,"y":1,"terrain":"grass"},{"x":2,"y":2,"terrain":"dirt"}],
			"tiles":[{"x":1,"y":1,"sheet":"tileset","sx":64,"sy":32}],
			"buildings":[],"obstacles":[]
		}
	}`
	var entry MapCatalogEntry
	if err := json.Unmarshal([]byte(legacy), &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.Map.Terrain) != 2 {
		t.Errorf("legacy terrain: want 2, got %d", len(entry.Map.Terrain))
	}
	if len(entry.Map.Tiles) != 1 || entry.Map.Tiles[0].Tileset != "tileset" || entry.Map.Tiles[0].Col != 2 || entry.Map.Tiles[0].Row != 1 {
		t.Errorf("legacy tiles wrong: %+v", entry.Map.Tiles)
	}
}

// TestTerrainTile_RealMapRoundTrips verifies a real tile-heavy map survives the
// grouped round-trip with identical terrain + tile cells.
func TestTerrainTile_RealMapRoundTrips(t *testing.T) {
	entry, ok := GetMapCatalogEntryByID("exploration")
	if !ok {
		t.Skip("exploration not in catalog")
	}
	wantTerrain := len(entry.Map.Terrain)
	wantTiles := len(entry.Map.Tiles)
	if wantTiles == 0 {
		t.Skip("exploration has no tiles")
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MapCatalogEntry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Map.Terrain) != wantTerrain {
		t.Errorf("terrain count drift: %d -> %d", wantTerrain, len(decoded.Map.Terrain))
	}
	if len(decoded.Map.Tiles) != wantTiles {
		t.Errorf("tile count drift: %d -> %d", wantTiles, len(decoded.Map.Tiles))
	}
}

// TestCompactCoordArrays_LeavesStringsAlone guards the compaction's string
// skipping — a '[' inside a string value must not trigger array compaction.
func TestCompactCoordArrays_LeavesStringsAlone(t *testing.T) {
	in := []byte("{\n  \"desc\": \"weird [[1,2]] text\",\n  \"locations\": [\n    [\n      3,\n      0\n    ]\n  ]\n}")
	out := string(compactCoordArrays(in))
	if !strings.Contains(out, `"desc": "weird [[1,2]] text"`) {
		t.Errorf("string literal was altered: %s", out)
	}
	if !strings.Contains(out, `"locations": [[3,0]]`) {
		t.Errorf("locations not compacted: %s", out)
	}
}
