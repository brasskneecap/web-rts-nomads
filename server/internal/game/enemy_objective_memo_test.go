package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newObstacleWalledState builds the standard objective test map but with a
// full-height terrain obstacle column partitioning it — so the townhall is
// unreachable WITHOUT any blocking units (acquireNearestBlockingHostileLocked
// finds nothing, isolating the objective pathfind for the cache test).
func newObstacleWalledState(t *testing.T) *GameState {
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
	obstacles := make([]protocol.ObstacleTile, 0, rows)
	for y := 0; y < rows; y++ {
		obstacles = append(obstacles, protocol.ObstacleTile{
			GridCoord: protocol.GridCoord{X: cols / 2, Y: y},
		})
	}
	cfg := protocol.MapConfig{
		ID: "obj-obstacle", Name: "obj-obstacle",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: obstacles,
		Buildings: []protocol.BuildingTile{townhall},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s
}

// TestObjAdvance_FailedPath_CachesUnreachableObjective: a failed objective
// pathfind must populate the ARMY-WIDE cache (s.objectiveUnreachableUntil) so
// no enemy re-pays the budget-bounded failed A* until the TTL lapses.
func TestObjAdvance_FailedPath_CachesUnreachableObjective(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		s.initializeCombatUnitLocked(w)
	}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1300, Y: 704})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	enemy.ObjectiveBuildingID = "townhall-1"
	blocked := s.getBlockedCellsLocked()

	if _, cached := s.objectiveUnreachableUntil["townhall-1"]; cached {
		t.Fatal("precondition: cache must start empty")
	}

	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	until, cached := s.objectiveUnreachableUntil["townhall-1"]
	if !cached || until <= s.Tick {
		t.Fatalf("a failed objective pathfind must arm the army-wide cache with a "+
			"future TTL; entry=%d cached=%v Tick=%d", until, cached, s.Tick)
	}
}

// TestObjAdvance_CacheActive_SkipsArmyWide is the core army-wide property: a
// FRESH enemy (no per-unit state) must skip the objective pathfind purely
// because ANOTHER enemy already cached it unreachable. With no blocking
// hostiles, the only thing that could call findUnitPath is the objective path
// itself, so a zero delta proves the whole army benefits from one failure.
func TestObjAdvance_CacheActive_SkipsArmyWide(t *testing.T) {
	s := newObstacleWalledState(t)
	defer s.mu.Unlock()
	tr := injectPathTracker(s)

	// Simulate "some other enemy already failed and armed the cache".
	s.objectiveUnreachableUntil["townhall-1"] = s.Tick + 100

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1952, Y: 704})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	enemy.ObjectiveBuildingID = "townhall-1"
	blocked := s.getBlockedCellsLocked()

	before := tr.totalFinePathCalls
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if tr.totalFinePathCalls != before {
		t.Fatalf("a fresh enemy must skip the objective pathfind when the army-wide "+
			"cache is armed; totalFinePathCalls %d -> %d", before, tr.totalFinePathCalls)
	}
}

// TestObjAdvance_SuccessClearsCache: when the TTL has lapsed and the gated
// enemy's retry now succeeds (route reopened by killing through the wall), the
// first success must clear the cache entry so the whole army resumes
// advancing immediately rather than waiting out stale suppression.
func TestObjAdvance_SuccessClearsCache(t *testing.T) {
	s := newObjectiveTestState(t) // obstacle-free: townhall reachable
	defer s.mu.Unlock()

	// Stale entry whose TTL has already lapsed (s.Tick is not < entry).
	s.objectiveUnreachableUntil["townhall-1"] = s.Tick

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 800, Y: 704})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	enemy.ObjectiveBuildingID = "townhall-1"
	blocked := s.getBlockedCellsLocked()

	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if !enemy.Moving {
		t.Fatal("fixture invalid: townhall must be reachable so the retry succeeds")
	}
	if _, cached := s.objectiveUnreachableUntil["townhall-1"]; cached {
		t.Fatal("a successful objective pathfind must clear the army-wide cache entry (self-heal)")
	}
}
