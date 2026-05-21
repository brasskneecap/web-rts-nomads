package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// newGroupMoveTestState builds an obstacle-free map with one player and a
// townhall safely off in a corner so it doesn't interfere with unit placement.
// Lock NOT held on return.
func newGroupMoveTestState(t *testing.T) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 60, 40
	owner := "p1"
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 35}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &owner,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	cfg := protocol.MapConfig{
		ID: "group-move-test", Name: "group-move-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{townhall},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.EnsurePlayer("p1")
	return s
}

// TestGroupMove_FrontUnitDoesNotWalkBackward reproduces the user-reported
// stutter where, in a 2D formation, units geometrically ahead of the leader
// were forced to walk back to the leader's start position before tracking
// the spine forward. The fix is in assignGroupPathsLocked: each follower
// enters the leader's spine at the first waypoint that's actually closer
// to the destination than the follower's current position.
//
// Scenario: 9 soldiers in a 3x3 grid, all with identical displacement to
// their formation slots. With ties, the leader heuristic picks the first
// iterated unit — the back-left of the grid. The front-right follower
// must NOT receive a path whose first waypoint sits at the back-left.
func TestGroupMove_FrontUnitDoesNotWalkBackward(t *testing.T) {
	s := newGroupMoveTestState(t)
	s.mu.Lock()
	// 3x3 grid centred near (800, 800). 40-unit spacing — same as
	// unitFormationSpacing so the formation slots line up cleanly with the
	// units' relative positions, producing tied distances to each slot.
	const sp = 40.0
	const cx, cy = 800.0, 800.0
	units := make([]*Unit, 0, 9)
	for row := -1; row <= 1; row++ {
		for col := -1; col <= 1; col++ {
			u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{
				X: cx + float64(col)*sp,
				Y: cy + float64(row)*sp,
			})
			u.Visible = true
			units = append(units, u)
		}
	}
	// Front-right unit (max X, min Y given forward = +X and right = -Y).
	frontUnit := units[2] // row=-1, col=+1 → (cx+sp, cy-sp) = (840, 760)
	frontUnitID := frontUnit.ID
	unitIDs := make([]int, len(units))
	for i, u := range units {
		unitIDs[i] = u.ID
	}
	s.mu.Unlock()

	dest := protocol.Vec2{X: 2400, Y: 800}
	s.MoveUnits("p1", unitIDs, dest)

	s.mu.RLock()
	defer s.mu.RUnlock()
	front := s.unitsByID[frontUnitID]
	if front == nil {
		t.Fatal("front unit went missing after MoveUnits")
	}
	if len(front.Path) == 0 {
		t.Fatal("front unit got empty path after MoveUnits")
	}
	// Reproducer assertion: the FIRST waypoint must not be behind the unit
	// along the X axis (destination is +X). Pre-fix the first waypoint sat
	// at the leader's start (~x=760 if back-left becomes leader), behind
	// the front unit (x=840). Allow a small tolerance — the formation slot
	// can land a half-cell behind the unit's current X without anyone
	// perceiving "walking backward."
	first := front.Path[0]
	if first.X < front.X-sp/2 {
		t.Errorf("front unit at (%.1f, %.1f) got path[0]=(%.1f, %.1f) — walking backward (bug regression)",
			front.X, front.Y, first.X, first.Y)
	}
	last := front.Path[len(front.Path)-1]
	if math.Hypot(last.X-dest.X, last.Y-dest.Y) > 200 {
		t.Errorf("front unit final waypoint (%.1f,%.1f) is far from destination (%.1f,%.1f); formation slot should be nearby",
			last.X, last.Y, dest.X, dest.Y)
	}
}

// TestGroupMove_BackUnitStillFollowsSpine verifies the fix didn't break the
// happy path: a unit behind the leader (closer to start than the leader is)
// should still pick up the leader's spine and walk forward via it.
func TestGroupMove_BackUnitStillFollowsSpine(t *testing.T) {
	s := newGroupMoveTestState(t)
	s.mu.Lock()
	units := []*Unit{
		s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 1200, Y: 800}),
		s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 1500, Y: 800}),
		s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 1800, Y: 800}),
		s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 2100, Y: 800}),
	}
	for _, u := range units {
		u.Visible = true
	}
	backUnit := units[0] // the one at x=1200
	backUnitID := backUnit.ID
	unitIDs := []int{units[0].ID, units[1].ID, units[2].ID, units[3].ID}
	s.mu.Unlock()

	dest := protocol.Vec2{X: 2700, Y: 800}
	s.MoveUnits("p1", unitIDs, dest)

	s.mu.RLock()
	defer s.mu.RUnlock()
	back := s.unitsByID[backUnitID]
	if back == nil {
		t.Fatal("back unit went missing after MoveUnits")
	}
	if len(back.Path) == 0 {
		t.Fatal("back unit got empty path after MoveUnits")
	}
	// Back unit should be moving forward (its waypoint X should be > its X).
	first := back.Path[0]
	if first.X < back.X {
		t.Errorf("back unit path[0].X = %.1f but unit.X = %.1f — should be moving forward",
			first.X, back.X)
	}
}
