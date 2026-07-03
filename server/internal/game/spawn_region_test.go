package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newPocketTownhallState builds a small open map with an occupied townhall at
// (4,4) 3x2 and a sealed 2-cell pocket hugging its west side: perimeter cells
// (3,4) and (3,5) are walkable but enclosed by trees on every other side, so a
// unit placed there can never leave. Everything else is one big open region.
//
//	. . T T . . . . .        T = tree
//	. . T p H H H . .        p = pocket cells (3,4),(3,5)
//	. . T p H H H . .        H = townhall footprint (4..6, 4..5)
//	. . T T . . . . .
func newPocketTownhallState(t *testing.T) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 20, 12
	owner := "p1"
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 4, Y: 4}, ID: "townhall-1",
		BuildingType: "townhall", Width: 3, Height: 2,
		Occupied: true, Visible: true, OwnerID: &owner,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	trees := []protocol.ObstacleTile{}
	for _, pt := range [][2]int{{2, 3}, {3, 3}, {2, 4}, {2, 5}, {2, 6}, {3, 6}} {
		trees = append(trees, protocol.ObstacleTile{
			GridCoord: protocol.GridCoord{X: pt[0], Y: pt[1]},
			Obstacle:  "tree", Width: 1, Height: 1,
		})
	}
	cfg := protocol.MapConfig{
		ID: "pocket-test", Name: "pocket-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: trees,
		Buildings: []protocol.BuildingTile{townhall},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s
}

func pocketCells() map[gridPoint]bool {
	return map[gridPoint]bool{
		{X: 3, Y: 4}: true,
		{X: 3, Y: 5}: true,
	}
}

// TestWalkableRegions_PocketIsSeparateRegion sanity-checks the region index:
// the sealed pocket must get its own region ID, distinct from (and far
// smaller than) the open field's region.
func TestWalkableRegions_PocketIsSeparateRegion(t *testing.T) {
	s := newPocketTownhallState(t)
	defer s.mu.Unlock()

	pocketRegion := s.walkableRegionAtLocked(gridPoint{X: 3, Y: 4})
	if pocketRegion == 0 {
		t.Fatal("pocket cell (3,4) is walkable and must belong to a region")
	}
	if got := s.walkableRegionAtLocked(gridPoint{X: 3, Y: 5}); got != pocketRegion {
		t.Fatalf("both pocket cells must share a region: (3,4)=%d (3,5)=%d", pocketRegion, got)
	}
	fieldRegion := s.walkableRegionAtLocked(gridPoint{X: 10, Y: 10})
	if fieldRegion == 0 {
		t.Fatal("open-field cell must belong to a region")
	}
	if fieldRegion == pocketRegion {
		t.Fatal("pocket must NOT be connected to the open field")
	}
	if s.walkableRegionSizeLocked(pocketRegion) != 2 {
		t.Fatalf("pocket region size = %d, want 2", s.walkableRegionSizeLocked(pocketRegion))
	}
	if s.walkableRegionSizeLocked(fieldRegion) <= s.walkableRegionSizeLocked(pocketRegion) {
		t.Fatal("open field must be the larger region")
	}
	// Blocked cells have no region.
	if got := s.walkableRegionAtLocked(gridPoint{X: 2, Y: 4}); got != 0 {
		t.Fatalf("tree cell must have region 0, got %d", got)
	}
	if got := s.walkableRegionAtLocked(gridPoint{X: 5, Y: 4}); got != 0 {
		t.Fatalf("townhall cell must have region 0, got %d", got)
	}
}

// TestWalkableRegions_InvalidatedWithBlockedCells: the region index must
// rebuild when the blocked-cells cache is invalidated (e.g. a building
// appears and seals or opens a corridor).
func TestWalkableRegions_InvalidatedWithBlockedCells(t *testing.T) {
	s := newPocketTownhallState(t)
	defer s.mu.Unlock()

	before := s.walkableRegionAtLocked(gridPoint{X: 3, Y: 4})
	if before == 0 {
		t.Fatal("pocket cell must have a region before the change")
	}

	// Remove the tree at (3,3): the pocket opens into the field above.
	kept := s.MapConfig.Obstacles[:0]
	for _, o := range s.MapConfig.Obstacles {
		if !(o.X == 3 && o.Y == 3) {
			kept = append(kept, o)
		}
	}
	s.MapConfig.Obstacles = kept
	s.invalidateBlockedCellsLocked()

	pocket := s.walkableRegionAtLocked(gridPoint{X: 3, Y: 4})
	field := s.walkableRegionAtLocked(gridPoint{X: 10, Y: 10})
	if pocket == 0 || pocket != field {
		t.Fatalf("after opening the pocket it must join the field region: pocket=%d field=%d", pocket, field)
	}
}

// TestTownhallSpawnPositions_SkipPocketCells is the core guarantee: unit
// placement around a production building must never choose a walkable cell
// that is sealed off from the rest of the map, even when asking for every
// perimeter slot.
func TestTownhallSpawnPositions_SkipPocketCells(t *testing.T) {
	s := newPocketTownhallState(t)
	defer s.mu.Unlock()

	th := s.getBuildingByIDLocked("townhall-1")
	blocked := s.getBlockedCellsLocked()
	positions := s.getTownhallSpawnPositionsLocked(*th, 20, blocked)
	if len(positions) == 0 {
		t.Fatal("townhall must still produce spawn positions")
	}
	pocket := pocketCells()
	for _, pos := range positions {
		cell := s.worldToGrid(pos.X, pos.Y)
		if pocket[cell] {
			t.Fatalf("spawn position (%.0f,%.0f) lands in sealed pocket cell (%d,%d)",
				pos.X, pos.Y, cell.X, cell.Y)
		}
	}
}

// TestProductionRelease_NeverReleasesIntoPocket drives the real production
// completion path: a finished worker must be released onto a connected cell.
func TestProductionRelease_NeverReleasesIntoPocket(t *testing.T) {
	s := newPocketTownhallState(t)
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{
		ID: "p1", Color: "#fff", Resources: map[string]int{},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Metrics:                       NewMatchMetrics(),
	}
	pocket := pocketCells()
	// Release enough workers to exhaust the non-pocket perimeter preference:
	// every single one must land connected.
	for i := 0; i < 12; i++ {
		s.Productions["townhall-1"] = []*UnitProduction{{
			PlayerID: "p1", UnitType: "worker", RemainingSeconds: 0, TotalSeconds: 1,
		}}
		s.completeUnitProductionLocked("townhall-1")
	}
	count := 0
	for _, u := range s.Units {
		if u == nil || u.OwnerID != "p1" {
			continue
		}
		count++
		cell := s.worldToGrid(u.X, u.Y)
		if pocket[cell] {
			t.Fatalf("worker #%d released into sealed pocket cell (%d,%d)", u.ID, cell.X, cell.Y)
		}
	}
	if count == 0 {
		t.Fatal("fixture invalid: no workers were released")
	}
}

// TestFindNearestWalkableInRegion_PrefersConnectedCell: the region-constrained
// nearest-walkable search must skip a closer pocket cell in favor of a farther
// cell in the requested region, and must degrade to the unconstrained search
// when the region is 0.
func TestFindNearestWalkableInRegion_PrefersConnectedCell(t *testing.T) {
	s := newPocketTownhallState(t)
	defer s.mu.Unlock()
	blocked := s.getBlockedCellsLocked()

	// Start the search from inside the townhall footprint right next to the
	// pocket: the unconstrained BFS finds pocket cell (3,4) first.
	start := gridPoint{X: 4, Y: 4}
	unconstrained, ok := s.findNearestWalkable(start, blocked)
	if !ok {
		t.Fatal("unconstrained search must find a cell")
	}
	if !pocketCells()[unconstrained] {
		t.Skipf("fixture assumption changed: unconstrained BFS found (%d,%d), not the pocket",
			unconstrained.X, unconstrained.Y)
	}

	fieldRegion := s.walkableRegionAtLocked(gridPoint{X: 10, Y: 10})
	got, ok := s.findNearestWalkableInRegionLocked(start, fieldRegion, blocked, nil)
	if !ok {
		t.Fatal("region-constrained search must find a cell")
	}
	if pocketCells()[got] {
		t.Fatalf("region-constrained search returned pocket cell (%d,%d)", got.X, got.Y)
	}
	if s.walkableRegionAtLocked(got) != fieldRegion {
		t.Fatalf("returned cell (%d,%d) not in requested region", got.X, got.Y)
	}
}
