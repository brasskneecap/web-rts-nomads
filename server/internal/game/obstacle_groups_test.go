package game

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

func tree(x, y int) protocol.ObstacleTile {
	return protocol.ObstacleTile{
		GridCoord:      protocol.GridCoord{X: x, Y: y},
		ID:             "tree-" + strconv.Itoa(x) + "-" + strconv.Itoa(y),
		Obstacle:       "tree",
		Width:          1,
		Height:         1,
		Capabilities:   []string{"resource-source", "selectable"},
		ResourceType:   "wood",
		ResourceAmount: 250,
	}
}

func sampleEntry() MapCatalogEntry {
	return MapCatalogEntry{
		ID:        "grp-test",
		Name:      "Group Test",
		SortOrder: 5,
		Map: protocol.MapConfig{
			ID:        "grp-test",
			Width:     640,
			Height:    640,
			GridCols:  10,
			GridRows:  10,
			CellSize:  64,
			Terrain:   []protocol.TerrainTile{},
			Buildings: []protocol.BuildingTile{},
			Obstacles: []protocol.ObstacleTile{
				tree(3, 0), tree(4, 0), tree(5, 0),
				{
					GridCoord:    protocol.GridCoord{X: 7, Y: 7},
					ID:           "rock-7-7",
					Obstacle:     "rock",
					Width:        1,
					Height:       1,
					Capabilities: []string{"selectable"},
					MaxHp:        500,
					Hp:           500,
				},
			},
		},
	}
}

// TestObstacleGroups_MarshalIsGroupedWithoutIDs verifies the on-disk/editor JSON
// stores metadata once per type with a locations array and drops per-obstacle
// ids and runtime hp.
func TestObstacleGroups_MarshalIsGroupedWithoutIDs(t *testing.T) {
	raw, err := json.Marshal(sampleEntry())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	// Grouped shape: obstacles is an object keyed by type with locations.
	if !strings.Contains(s, `"obstacles":{"rock":`) && !strings.Contains(s, `"tree":`) {
		t.Fatalf("expected grouped obstacles object, got: %s", s)
	}
	if !strings.Contains(s, `"locations":[[3,0],[4,0],[5,0]]`) {
		t.Errorf("expected tree locations array, got: %s", s)
	}
	// No per-instance ids on disk.
	if strings.Contains(s, "tree-3-0") || strings.Contains(s, "rock-7-7") {
		t.Errorf("grouped form must not contain per-obstacle ids: %s", s)
	}
	// Runtime hp must not be persisted.
	if strings.Contains(s, `"hp":`) {
		t.Errorf("grouped form must not contain runtime hp: %s", s)
	}
	// Shared metadata stored once.
	if strings.Count(s, `"resourceType":"wood"`) != 1 {
		t.Errorf("tree resourceType should appear exactly once, got: %s", s)
	}
}

// TestObstacleGroups_RoundTrip verifies marshal→unmarshal preserves the obstacle
// set (positions, types, metadata), regenerating ids via hydration.
func TestObstacleGroups_RoundTrip(t *testing.T) {
	original := sampleEntry()
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded MapCatalogEntry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	hydrateObstacles(decoded.Map.Obstacles)

	if len(decoded.Map.Obstacles) != len(original.Map.Obstacles) {
		t.Fatalf("obstacle count: want %d, got %d", len(original.Map.Obstacles), len(decoded.Map.Obstacles))
	}

	// Build a position->tile index for comparison (order may differ: grouped
	// expansion sorts by type).
	byPos := map[[2]int]protocol.ObstacleTile{}
	for _, o := range decoded.Map.Obstacles {
		byPos[[2]int{o.X, o.Y}] = o
	}
	for _, want := range original.Map.Obstacles {
		got, ok := byPos[[2]int{want.X, want.Y}]
		if !ok {
			t.Fatalf("missing obstacle at (%d,%d) after round-trip", want.X, want.Y)
		}
		if got.Obstacle != want.Obstacle {
			t.Errorf("(%d,%d): type %q != %q", want.X, want.Y, got.Obstacle, want.Obstacle)
		}
		if got.ResourceType != want.ResourceType || got.ResourceAmount != want.ResourceAmount {
			t.Errorf("(%d,%d): resource %q/%d != %q/%d", want.X, want.Y,
				got.ResourceType, got.ResourceAmount, want.ResourceType, want.ResourceAmount)
		}
		// Id regenerated as "<type>-<x>-<y>".
		wantID := want.Obstacle + "-" + strconv.Itoa(want.X) + "-" + strconv.Itoa(want.Y)
		if got.ID != wantID {
			t.Errorf("(%d,%d): id %q, want %q", want.X, want.Y, got.ID, wantID)
		}
	}
}

// TestObstacleGroups_LegacyArrayLoads verifies a map file still using the old
// flat obstacle array unmarshals correctly (back-compat).
func TestObstacleGroups_LegacyArrayLoads(t *testing.T) {
	legacy := `{
		"id":"legacy","name":"Legacy","description":"","sortOrder":0,
		"map":{"id":"legacy","width":640,"height":640,"gridCols":10,"gridRows":10,"cellSize":64,
			"terrain":[],"buildings":[],
			"obstacles":[
				{"x":1,"y":1,"id":"tree-1-1","obstacle":"tree","width":1,"height":1,
				 "capabilities":["resource-source","selectable"],"resourceType":"wood","resourceAmount":250},
				{"x":2,"y":1,"obstacle":"tree"}
			]
		}
	}`
	var entry MapCatalogEntry
	if err := json.Unmarshal([]byte(legacy), &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.Map.Obstacles) != 2 {
		t.Fatalf("legacy array: want 2 obstacles, got %d", len(entry.Map.Obstacles))
	}
	if entry.Map.Obstacles[0].Obstacle != "tree" || entry.Map.Obstacles[0].X != 1 {
		t.Errorf("legacy array obstacle[0] wrong: %+v", entry.Map.Obstacles[0])
	}
}

// TestObstacleGroups_RealMapRoundTrips verifies a real shipped map survives a
// grouped marshal→unmarshal with the same obstacle positions/types.
func TestObstacleGroups_RealMapRoundTrips(t *testing.T) {
	entry, ok := GetMapCatalogEntryByID("forest-1")
	if !ok {
		t.Skip("forest-1 not in catalog")
	}
	if len(entry.Map.Obstacles) == 0 {
		t.Skip("forest-1 has no obstacles")
	}
	wantPositions := map[[2]int]string{}
	for _, o := range entry.Map.Obstacles {
		wantPositions[[2]int{o.X, o.Y}] = o.Obstacle
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MapCatalogEntry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	hydrateObstacles(decoded.Map.Obstacles)

	if len(decoded.Map.Obstacles) != len(entry.Map.Obstacles) {
		t.Fatalf("obstacle count drift: %d -> %d", len(entry.Map.Obstacles), len(decoded.Map.Obstacles))
	}
	for _, o := range decoded.Map.Obstacles {
		want, ok := wantPositions[[2]int{o.X, o.Y}]
		if !ok {
			t.Fatalf("decoded obstacle at (%d,%d) not in original", o.X, o.Y)
		}
		if want != o.Obstacle {
			t.Errorf("(%d,%d): type %q != %q", o.X, o.Y, o.Obstacle, want)
		}
		if o.ID == "" {
			t.Errorf("(%d,%d): missing regenerated id", o.X, o.Y)
		}
	}
}
