package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestPlayerFOW_NewIsAllDark verifies that a freshly allocated FOW grid is
// entirely dark (no cells seen).
func TestPlayerFOW_NewIsAllDark(t *testing.T) {
	f := newPlayerFOW(4, 3)
	for gy := 0; gy < 3; gy++ {
		for gx := 0; gx < 4; gx++ {
			if f.cellAt(gx, gy) != CellDark {
				t.Errorf("cell (%d,%d) expected Dark, got %v", gx, gy, f.cellAt(gx, gy))
			}
		}
	}
}

// TestPlayerFOW_StampCircle_CenterCell verifies that the center cell is always
// stamped Clear.
func TestPlayerFOW_StampCircle_CenterCell(t *testing.T) {
	const cellSize = 64.0
	f := newPlayerFOW(10, 10)
	// Place vision at the center of cell (5,5).
	worldX := 5.5 * cellSize
	worldY := 5.5 * cellSize
	f.stampCircle(worldX, worldY, cellSize*2, cellSize, nil)

	if f.cellAt(5, 5) != CellClear {
		t.Errorf("center cell (5,5) expected Clear after stampCircle, got %v", f.cellAt(5, 5))
	}
}

// TestPlayerFOW_ClearClearBits_PreservesEverSeen verifies that clearClearBits
// drops the Clear bit but leaves the EverSeen bit intact.
func TestPlayerFOW_ClearClearBits_PreservesEverSeen(t *testing.T) {
	f := newPlayerFOW(4, 4)
	f.stampCircle(32, 32, 48, 64, nil) // stamp at ~(0.5,0.5) in grid coords
	// At least the origin cell should now be Clear.
	if f.cellAt(0, 0) != CellClear {
		t.Skip("stamp did not reach (0,0); adjust test geometry")
	}

	f.clearClearBits()

	// EverSeen must survive, Clear must be gone.
	if f.cellAt(0, 0) != CellShroud {
		t.Errorf("after clearClearBits, cell (0,0) expected Shroud, got %v", f.cellAt(0, 0))
	}
}

// TestPlayerFOW_IsClearAtWorld_Basic checks round-trip world→grid lookup.
func TestPlayerFOW_IsClearAtWorld_Basic(t *testing.T) {
	const cellSize = 64.0
	f := newPlayerFOW(10, 10)
	f.stampCircle(100, 100, 200, cellSize, nil)

	// World (100, 100) = grid (1.5, 1.5) → cell (1,1).
	if !f.isClearAtWorld(100, 100, cellSize) {
		t.Error("isClearAtWorld(100,100) expected true")
	}
	// Far corner should still be dark.
	if f.isClearAtWorld(640-1, 640-1, cellSize) {
		t.Error("isClearAtWorld far corner expected false")
	}
}

// TestPlayerFOW_AnyFootprintClear verifies footprint intersection logic.
func TestPlayerFOW_AnyFootprintClear(t *testing.T) {
	const cellSize = 64.0
	f := newPlayerFOW(10, 10)
	// Stamp a small circle so only cell (0,0) is clear.
	f.Cells[0] = uint8(CellClear)

	b := &protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 0, Y: 0},
		Width:     2,
		Height:    2,
	}
	if !f.anyFootprintClear(b) {
		t.Error("anyFootprintClear expected true when (0,0) is clear and building starts at (0,0)")
	}

	b2 := &protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 3, Y: 3},
		Width:     2,
		Height:    2,
	}
	if f.anyFootprintClear(b2) {
		t.Error("anyFootprintClear expected false for building entirely in dark cells")
	}
}

// TestPlayerFOW_EncodeRLE_RoundTrip verifies the RLE encoder produces the
// expected [state, count, ...] pairs for a known input.
func TestPlayerFOW_EncodeRLE_RoundTrip(t *testing.T) {
	f := newPlayerFOW(6, 1)
	// Manually set: [Dark, Dark, Clear, Clear, Shroud, Dark]
	f.Cells[0] = uint8(CellDark)
	f.Cells[1] = uint8(CellDark)
	f.Cells[2] = uint8(CellClear)
	f.Cells[3] = uint8(CellClear)
	f.Cells[4] = uint8(CellShroud)
	f.Cells[5] = uint8(CellDark)

	runs := f.encodeRLE()
	// Expected: [0,2, 3,2, 1,1, 0,1]
	expected := []int{0, 2, 3, 2, 1, 1, 0, 1}
	if len(runs) != len(expected) {
		t.Fatalf("RLE length mismatch: got %v, want %v", runs, expected)
	}
	for i, v := range expected {
		if runs[i] != v {
			t.Errorf("runs[%d] = %d, want %d", i, runs[i], v)
		}
	}
}

// TestRecomputeFOW_UnitRevealsCells verifies that a player's unit stamps Clear
// cells in the FOW grid during recomputeFOWLocked.
func TestRecomputeFOW_UnitRevealsCells(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))
	playerID := "test-player"
	state.EnsurePlayer(playerID)

	fow := state.FOW[playerID]
	if fow == nil {
		t.Fatal("FOW not initialized for player")
	}

	// Spawn a unit directly so the test is independent of map-specific placed units.
	state.mu.Lock()
	u := state.spawnPlayerUnitLocked("worker", playerID, "#ffffff", protocol.Vec2{X: 200, Y: 200})
	state.mu.Unlock()
	if u == nil {
		t.Fatal("failed to spawn test unit")
	}

	// Run one Update to trigger recomputeFOWLocked.
	state.Update(0.05)

	state.mu.RLock()
	defer state.mu.RUnlock()

	cellSize := state.MapConfig.CellSize
	if !fow.isClearAtWorld(u.X, u.Y, cellSize) {
		t.Errorf("cell at unit position (%.1f, %.1f) expected Clear after Update", u.X, u.Y)
	}
}

// TestSnapshotForPlayer_EnemyUnitFiltered verifies that an enemy unit far from
// the player's vision is excluded from SnapshotForPlayer.
func TestSnapshotForPlayer_EnemyUnitFiltered(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))
	playerID := "p1"
	state.EnsurePlayer(playerID)

	// Spawn an enemy unit at a position far from the player's starting area.
	state.mu.Lock()
	enemyUnit := state.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 9999, Y: 9999})
	state.mu.Unlock()

	if enemyUnit == nil {
		t.Fatal("failed to spawn enemy unit")
	}

	// Run one Update to build FOW state.
	state.Update(0.05)

	snap := state.SnapshotForPlayer(playerID)

	for _, u := range snap.Units {
		if u.ID == enemyUnit.ID {
			t.Errorf("enemy unit at (9999,9999) should be filtered from FOW snapshot but was included")
		}
	}
}

// TestHasLOS_BlockedByObstacle verifies that an obstacle between source and
// target prevents line-of-sight, while the obstacle cell itself remains visible.
func TestHasLOS_BlockedByObstacle(t *testing.T) {
	// Obstacle at (3,0) blocks the ray from (0,0) to (5,0).
	blocking := map[gridPoint]bool{{X: 3, Y: 0}: true}

	if !hasLOS(0, 0, 2, 0, blocking) {
		t.Error("(0,0) → (2,0): no blocker between them, expected LOS clear")
	}
	// Obstacle at (3,0) should block (4,0) and (5,0).
	if hasLOS(0, 0, 4, 0, blocking) {
		t.Error("(0,0) → (4,0): blocker at (3,0) should obstruct LOS")
	}
	// The obstacle cell itself is always visible (target == obstacle).
	if !hasLOS(0, 0, 3, 0, blocking) {
		t.Error("(0,0) → (3,0): target IS the blocker; unit should still see the obstacle surface")
	}
}

// TestStampCircle_LOSBlocksHiddenCells verifies that a cell directly behind a
// blocking obstacle is not stamped Clear, while the obstacle cell itself is.
func TestStampCircle_LOSBlocks(t *testing.T) {
	const cellSize = 64.0
	f := newPlayerFOW(10, 10)
	// Unit at center of cell (2,2). Obstacle at cell (4,2).
	blocking := map[gridPoint]bool{{X: 4, Y: 2}: true}
	// Vision radius covers cell (5,2) if LOS is ignored.
	f.stampCircle(2.5*cellSize, 2.5*cellSize, 4*cellSize, cellSize, blocking)

	if f.cellAt(2, 2) != CellClear {
		t.Error("unit's own cell (2,2) expected Clear")
	}
	if f.cellAt(4, 2) != CellClear {
		t.Error("obstacle cell (4,2) should be visible (you see the tree surface)")
	}
	if f.cellAt(5, 2) == CellClear {
		t.Error("cell (5,2) behind obstacle should NOT be Clear — LOS blocked by (4,2)")
	}
}

// TestSnapshotForPlayer_OwnUnitAlwaysIncluded verifies that the viewer's own
// units are always in the snapshot regardless of FOW.
func TestSnapshotForPlayer_OwnUnitAlwaysIncluded(t *testing.T) {
	state := NewGameState(GetMapConfigByID(DefaultMapID()))
	playerID := "p1"
	state.EnsurePlayer(playerID)

	// Spawn a unit so the test is independent of map-specific placed units.
	state.mu.Lock()
	u := state.spawnPlayerUnitLocked("worker", playerID, "#ffffff", protocol.Vec2{X: 9999, Y: 9999})
	state.mu.Unlock()
	if u == nil {
		t.Fatal("failed to spawn test unit")
	}
	ownUnitID := u.ID

	// Run one Update to build FOW state.
	state.Update(0.05)

	snap := state.SnapshotForPlayer(playerID)

	found := false
	for _, us := range snap.Units {
		if us.ID == ownUnitID {
			found = true
			break
		}
	}
	if !found {
		t.Error("own unit not found in FOW-filtered snapshot even though it is far from vision")
	}
}

// A building flagged UnobstructedVision (e.g. Tower) sees past obstacles/trees
// within its range; a normal building's vision is still occluded by the same
// obstacle. Geometry: a 2x2 building centred on cell (3,3); a tree at (4,3); the
// cell behind it at (5,3) is ~163px away — within both the Tower's 512px and a
// normal building's 320px default range, so range is not the variable, blocking
// is.
func TestRecomputeFOW_UnobstructedBuildingVision(t *testing.T) {
	fowFor := func(buildingType string) *PlayerFOW {
		owner := "p1"
		cfg := protocol.MapConfig{
			ID: "fow-vis", CellSize: 64, GridCols: 20, GridRows: 20,
			Width: 20 * 64, Height: 20 * 64,
			Obstacles: []protocol.ObstacleTile{
				{GridCoord: protocol.GridCoord{X: 4, Y: 3}, Obstacle: "tree"},
			},
			Buildings: []protocol.BuildingTile{
				{GridCoord: protocol.GridCoord{X: 2, Y: 2}, ID: "b", BuildingType: buildingType,
					Width: 2, Height: 2, Visible: true, Occupied: true, OwnerID: &owner,
					Metadata: map[string]interface{}{"hp": 500.0, "maxHp": 500.0}},
			},
		}
		state := NewGameState(cfg)
		state.EnsurePlayer("p1")
		state.mu.Lock()
		defer state.mu.Unlock()
		state.recomputeFOWLocked()
		return state.FOW["p1"]
	}

	tower := fowFor("Tower")
	if tower.cellAt(4, 3) != CellClear {
		t.Fatal("the tree cell itself should always be visible")
	}
	if tower.cellAt(5, 3) != CellClear {
		t.Fatal("Tower has unobstructed vision: the cell behind the tree should be Clear")
	}

	normal := fowFor("barracks")
	if normal.cellAt(4, 3) != CellClear {
		t.Fatal("the tree cell should still be visible to a normal building")
	}
	if normal.cellAt(5, 3) == CellClear {
		t.Fatal("a normal building's vision must be blocked by the tree")
	}
}
