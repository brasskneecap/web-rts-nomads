package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newObjectiveTestState builds an obstacle-free map with one player townhall
// plus any `extra` buildings, all assembled into the MapConfig BEFORE
// NewGameStateWithSeed so the building index is constructed once and correctly
// (no post-construction slice append, which would reallocate the backing array
// and invalidate the index's element pointers). Returns the locked GameState.
func newObjectiveTestState(t *testing.T, extra ...protocol.BuildingTile) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 24
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 10}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &townhallOwnerID,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	buildings := append([]protocol.BuildingTile{townhall}, extra...)
	cfg := protocol.MapConfig{
		ID: "obj-test", Name: "obj-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: buildings,
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s
}

// townhallOwnerID is a package-level addressable "p1" so BuildingTile.OwnerID
// (a *string) can point at a stable address shared across the test fixtures.
var townhallOwnerID = "p1"

// objBuilding is a fixture constructor for an extra attackable player building.
func objBuilding(id, btype string, gx, gy, w, h int, hp float64) protocol.BuildingTile {
	return protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: gx, Y: gy}, ID: id,
		BuildingType: btype, Width: w, Height: h,
		Occupied: true, Visible: true, OwnerID: &townhallOwnerID,
		Metadata: map[string]interface{}{"hp": hp, "maxHp": hp},
	}
}

func TestGetNearestPlayerTownhallBuilding_ReturnsTownhall(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	b := s.getNearestPlayerTownhallBuildingLocked(2000, 768)
	if b == nil || b.ID != "townhall-1" {
		t.Fatalf("want townhall-1, got %v", b)
	}
}

// Determinism: two attackable buildings the same distance from the enemy must
// resolve to the same one every call (lower ID wins), so seeded replays agree.
// a-tower (gx=11) right edge = 12*64 = 768; z-tower (gx=20) left edge =
// 20*64 = 1280; enemy at x=1024 (= (768+1280)/2), y=736 inside both towers'
// y-span [704,768] -> distanceToBuilding == 256 for BOTH (genuinely tied), so
// only the ID tiebreak decides, and it must pick "a-tower" every call.
func TestFindNearestAttackablePlayerBuilding_DeterministicTiebreak(t *testing.T) {
	s := newObjectiveTestState(t,
		objBuilding("a-tower", "tower", 11, 11, 1, 1, 100),
		objBuilding("z-tower", "tower", 20, 11, 1, 1, 100),
	)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1024, Y: 736})
	s.initializeCombatUnitLocked(enemy)

	for i := 0; i < 20; i++ {
		got := s.findNearestAttackablePlayerBuildingLocked(enemy)
		if got == nil || got.ID != "a-tower" {
			t.Fatalf("nondeterministic/incorrect tiebreak: call %d got %v want a-tower", i, got)
		}
	}
}
