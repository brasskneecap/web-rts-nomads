package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newStuckRecoveryTestState builds a wide, fully-walkable map (no DefaultTile ->
// addTerrainBlocks is a no-op, so every cell is walkable) with no buildings or
// obstacles. Lock is NOT held on return, so callers can drive full s.Update
// ticks. Callers add/remove obstacles at runtime to simulate a transient block.
func newStuckRecoveryTestState(t *testing.T) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 32, 24
	cfg := protocol.MapConfig{
		ID: "stuck-test", Name: "stuck-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{},
	}
	return NewGameStateWithSeed(cfg, 42)
}

// TestStuckUnit_RecoversAfterTransientBlockClears reproduces the reported
// "unit snags on a building/tree and never redirects" bug.
//
// A unit is given a plain Move order across the map. Mid-travel a full-height
// wall is dropped between it and its destination, making the destination
// momentarily unreachable. The unit presses against the wall and its forced
// repath fails (no route). The bug: on repath failure the movement loop sets
// Moving=false / Path=nil and discards the Move order, and the stuck watchdog
// only runs for Moving units — so once the transient block clears, nothing ever
// retries and the unit sits abandoned forever.
//
// Expected (post-fix): a failed repath keeps the order alive and retries on a
// bounded cadence, so when the obstruction clears the unit resumes and reaches
// its destination.
func TestStuckUnit_RecoversAfterTransientBlockClears(t *testing.T) {
	s := newStuckRecoveryTestState(t)

	s.mu.Lock()
	u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 768})
	u.Visible = true
	u.HP = u.MaxHP
	s.initializeCombatUnitLocked(u)
	id := u.ID
	s.mu.Unlock()

	dest := protocol.Vec2{X: 1400, Y: 768}
	s.MoveUnits("p1", []int{id}, dest)

	// Let the unit start moving toward the (future) wall column.
	for i := 0; i < 10; i++ {
		s.Update(0.05)
	}

	// Drop a full-height wall at grid column 8, fully partitioning the unit's
	// side from the destination. Invalidate the blocked-cell cache so the sim
	// picks it up.
	const wallCol = 8
	s.mu.Lock()
	for y := 0; y < s.MapConfig.GridRows; y++ {
		s.MapConfig.Obstacles = append(s.MapConfig.Obstacles, protocol.ObstacleTile{
			GridCoord: protocol.GridCoord{X: wallCol, Y: y}, Obstacle: "tree",
		})
	}
	s.blockedCellsValid = false
	s.mu.Unlock()

	// Unit advances into the wall; the forced repath to the now-unreachable
	// destination fails. Under the current code this abandons the order here.
	for i := 0; i < 40; i++ {
		s.Update(0.05)
	}

	// Sanity: the wall did its job — the unit could not cross it. (True under
	// both current and fixed code; confirms the reproduction is exercising the
	// repath-failure path rather than passing trivially.)
	s.mu.Lock()
	beforeX := s.unitsByID[id].X
	s.mu.Unlock()
	if beforeX >= float64(wallCol)*64.0 {
		t.Fatalf("setup invalid: unit crossed the wall (x=%.0f); expected it stopped west of col %d", beforeX, wallCol)
	}

	// The transient obstruction clears.
	s.mu.Lock()
	s.MapConfig.Obstacles = nil
	s.blockedCellsValid = false
	s.mu.Unlock()

	// The unit must recover and reach the destination vicinity.
	reached := false
	for i := 0; i < 600; i++ {
		s.Update(0.05)
		s.mu.Lock()
		x := s.unitsByID[id].X
		s.mu.Unlock()
		if x >= dest.X-40 {
			reached = true
			break
		}
	}

	if !reached {
		s.mu.Lock()
		fu := s.unitsByID[id]
		x, y, order, moving, pathLen := fu.X, fu.Y, fu.Order.Type, fu.Moving, len(fu.Path)
		s.mu.Unlock()
		t.Fatalf("unit never recovered after the obstruction cleared: x=%.0f y=%.0f order=%d moving=%v pathLen=%d (wanted x>=%.0f)",
			x, y, order, moving, pathLen, dest.X-40)
	}
}
