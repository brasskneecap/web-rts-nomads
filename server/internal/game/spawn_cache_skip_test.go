package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newSpawnpointState builds an obstacle-free map with a player townhall and a
// gameStart enemy-spawnpoint (fires once immediately, bypassing wave gating),
// so a single tickEnemySpawnpointsLocked call spawns + routes a batch of
// enemies toward the townhall.
func newSpawnpointState(t *testing.T) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 24
	owner := "p1"
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 10}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &owner,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	spawnpoint := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 34, Y: 11}, ID: "spawn-1",
		BuildingType: "enemy-spawnpoint", Width: 1, Height: 1, Visible: true,
		Metadata: map[string]interface{}{
			"gameStart": true, "spawnCount": 5.0, "unitType": "raider",
		},
	}
	cfg := protocol.MapConfig{
		ID: "spawn-test", Name: "spawn-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{townhall, spawnpoint},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s
}

func countEnemyUnits(s *GameState) int {
	n := 0
	for _, u := range s.Units {
		if u != nil && u.OwnerID == enemyPlayerID {
			n++
		}
	}
	return n
}

// TestSpawn_ObjectiveCached_SkipsSpawnPathfind: when the objective is already
// cached unreachable army-wide, the spawn loop must NOT pay the per-unit
// budgeted A* (the ~18ms-per-wave residual). The units still spawn — they get
// routed by enemyAdvanceToObjectiveLocked on their first eval, which (same
// cache) goes straight to engaging blockers.
func TestSpawn_ObjectiveCached_SkipsSpawnPathfind(t *testing.T) {
	s := newSpawnpointState(t)
	defer s.mu.Unlock()
	tr := injectPathTracker(s)

	// Simulate "the base is walled off and an enemy already cached it".
	s.objectiveUnreachableUntil["townhall-1"] = s.Tick + 100

	blocked := s.getBlockedCellsLocked()
	before := tr.totalFinePathCalls
	s.tickEnemySpawnpointsLocked(0.05, blocked)

	if countEnemyUnits(s) == 0 {
		t.Fatal("fixture invalid: no enemies spawned")
	}
	if tr.totalFinePathCalls != before {
		t.Fatalf("spawn must skip pathing to a cached-unreachable objective; "+
			"totalFinePathCalls %d -> %d (the per-wave spawn spike)", before, tr.totalFinePathCalls)
	}
}

// TestSpawn_ObjectiveReachable_PathsNormally is the control: with no cache
// entry, spawn must still path the units (behavior preserved for the normal
// reachable case).
func TestSpawn_ObjectiveReachable_PathsNormally(t *testing.T) {
	s := newSpawnpointState(t)
	defer s.mu.Unlock()
	tr := injectPathTracker(s)

	blocked := s.getBlockedCellsLocked()
	before := tr.totalFinePathCalls
	s.tickEnemySpawnpointsLocked(0.05, blocked)

	if countEnemyUnits(s) == 0 {
		t.Fatal("fixture invalid: no enemies spawned")
	}
	if tr.totalFinePathCalls <= before {
		t.Fatalf("with no cache entry spawn must path the units; "+
			"totalFinePathCalls %d -> %d", before, tr.totalFinePathCalls)
	}
}
