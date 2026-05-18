package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestEnemy_WalledOffFromBuilding_EngagesBlockers reproduces the spawn-freeze
// deadlock: a wave enemy whose only route to its building objective is sealed
// off by a wall of player units must NOT sit frozen at its spawn forever. It
// must drop the unreachable building and engage one of the blocking units, so
// it actually fights its way toward the base instead of bogging the sim down
// in a perpetual failed-pathfind loop.
//
// Repro construction (no reliance on default-map geometry, so it is fully
// deterministic):
//   - A bespoke obstacle-free map with a single player-owned townhall on the
//     far-left side.
//   - A continuous, sub-cell-tight vertical wall of stationary player units
//     spanning the entire map height. Units are spaced 20px apart; each blocks
//     every sub-cell within unitSeparationDistance (22px), so the union leaves
//     no 16px sub-cell gap and the wall partitions the map completely — there
//     is provably no ground path from the enemy's half to the townhall's half.
//   - The enemy spawns far to the right, well beyond its profile DetectionRange
//     (raider = 240) from the wall, so it cannot "accidentally" acquire a wall
//     unit through normal in-range target selection — the only way it engages
//     a blocker is the unreachable-building fallback this test is exercising.
func TestEnemy_WalledOffFromBuilding_EngagesBlockers(t *testing.T) {
	const (
		cellSize = 64.0
		gridCols = 40
		gridRows = 24
	)
	mapW := float64(gridCols) * cellSize // 2560
	mapH := float64(gridRows) * cellSize // 1536

	ownerID := "p1"
	townhall := protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 2, Y: 10},
		ID:           "townhall-1",
		BuildingType: "townhall",
		Width:        2,
		Height:       2,
		Occupied:     true,
		Visible:      true,
		OwnerID:      &ownerID,
		Capabilities: []string{},
		Metadata: map[string]interface{}{
			"hp":    5000.0,
			"maxHp": 5000.0,
		},
	}

	cfg := protocol.MapConfig{
		ID:        "test-walled-objective",
		Name:      "test-walled-objective",
		Width:     mapW,
		Height:    mapH,
		GridCols:  gridCols,
		GridRows:  gridRows,
		CellSize:  cellSize,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{townhall},
	}

	s := NewGameStateWithSeed(cfg, 42)

	s.mu.Lock()

	// Continuous player-unit wall at x = wallX, spanning the full map height.
	const wallX = 1200.0
	wallIDs := map[int]bool{}
	for y := 10.0; y <= mapH-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: wallX, Y: y})
		if w == nil {
			s.mu.Unlock()
			t.Fatal("failed to spawn wall unit")
		}
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0   // immovable: the wall stays a complete partition
		w.Damage = 0      // pure blocker — never kills the enemy mid-test
		w.Capabilities = nil // excluded from combat AI; still a valid hostile target
		s.initializeCombatUnitLocked(w)
		wallIDs[w.ID] = true
	}

	// Enemy spawns far right of the wall — 600px clear of it, far beyond the
	// raider DetectionRange (240), and ~1600px from the townhall.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1800, Y: 768})
	if enemy == nil {
		s.mu.Unlock()
		t.Fatal("failed to spawn enemy")
	}
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150 // mobile, so once unblocked it can close on the wall
	s.initializeCombatUnitLocked(enemy)
	enemyID := enemy.ID
	spawnX, spawnY := enemy.X, enemy.Y

	// Sanity 1: the townhall is a discoverable attackable objective for this
	// enemy (so the test exercises the building-objective path, not a no-target
	// path).
	if b := s.findNearestAttackablePlayerBuildingLocked(enemy); b == nil || b.ID != townhall.ID {
		s.mu.Unlock()
		t.Fatalf("precondition: enemy must see the townhall as an attackable building; got %v", b)
	}

	// Sanity 2: the wall is a genuine partition — a direct path request from the
	// enemy to the townhall's attack perimeter must fail. If this ever finds a
	// route the fixture is broken and the rest of the test would be meaningless.
	blocked := s.getBlockedCellsLocked()
	if pos := s.findBestBuildingAttackPositionLocked(enemy, &s.MapConfig.Buildings[0], blocked); pos == nil {
		s.mu.Unlock()
		t.Fatal("precondition: townhall must have a terrain-walkable perimeter (so commitment is unit-blocked, not terrain-blocked)")
	} else {
		probe := *enemy
		s.assignUnitPath(&probe, *pos, blocked, nil)
		if probe.Moving {
			s.mu.Unlock()
			t.Fatal("precondition: wall must fully block the path to the townhall, but a route was found")
		}
	}

	s.mu.Unlock()

	// Drive the real simulation long enough to pass the unreachable-building
	// escalation warmup AND let a freed enemy traverse the ~600px to the wall.
	tickN(s, 200)

	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.unitsByID[enemyID]
	if e == nil {
		t.Fatal("enemy unit disappeared")
	}

	// The townhall must be untouched — confirms the enemy never actually
	// reached it (the scenario stayed a genuine block for the whole run).
	hp, _, _ := getBuildingHP(&s.MapConfig.Buildings[0])
	if hp < 5000.0 {
		t.Fatalf("townhall took damage (hp=%.0f) — the wall did not stay a partition; test scenario invalid", hp)
	}

	// Core behavioral invariant: a walled-off enemy must engage one of the
	// units blocking its route, not freeze at spawn with an unreachable
	// building target.
	if e.AttackTargetID == 0 || !wallIDs[e.AttackTargetID] {
		t.Fatalf("walled-off enemy did not engage a blocking unit:\n"+
			"  AttackTargetID=%d (want one of the %d wall units)\n"+
			"  AttackBuildingTargetID=%q Moving=%v\n"+
			"  pos=(%.0f,%.0f) spawn=(%.0f,%.0f)",
			e.AttackTargetID, len(wallIDs), e.AttackBuildingTargetID, e.Moving,
			e.X, e.Y, spawnX, spawnY)
	}
}
