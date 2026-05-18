package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// passthroughMapConfig builds a deterministic, obstacle-free map so the only
// thing that can block a path is a wall of units (never terrain). Mirrors the
// fixture geometry proven to be a complete unit-partition in
// TestEnemy_WalledOffFromBuilding_EngagesBlockers.
func passthroughMapConfig() protocol.MapConfig {
	const (
		cellSize = 64.0
		gridCols = 40
		gridRows = 24
	)
	return protocol.MapConfig{
		ID:        "test-friendly-passthrough",
		Name:      "test-friendly-passthrough",
		Width:     float64(gridCols) * cellSize,
		Height:    float64(gridRows) * cellSize,
		GridCols:  gridCols,
		GridRows:  gridRows,
		CellSize:  cellSize,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{},
	}
}

// spawnUnitWall lays a continuous, sub-cell-tight vertical wall of stationary
// units at x=wallX spanning the full map height. Units are spaced 20px apart;
// each blocks every sub-cell within unitSeparationDistance (22px), so the union
// leaves no 16px sub-cell gap and the wall fully partitions the map for any
// unit that treats these as obstacles.
func spawnUnitWall(t *testing.T, s *GameState, ownerID string, wallX float64) {
	t.Helper()
	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: wallX, Y: y})
		if w == nil {
			t.Fatal("failed to spawn wall unit")
		}
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0 // immovable: the wall stays a complete partition
		s.initializeCombatUnitLocked(w)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pathing: a stationary wall of allied units must NOT block an ally's path.
// ─────────────────────────────────────────────────────────────────────────────

// TestFriendlyWall_DoesNotBlockAllyPathing is the core new behavior: a unit
// asked to move past a wall of stationary friendly (same-owner) units must
// find a route — friendly units are excluded from the pathing blocked map, so
// the path plans straight through them instead of detouring or failing.
func TestFriendlyWall_DoesNotBlockAllyPathing(t *testing.T) {
	s := NewGameStateWithSeed(passthroughMapConfig(), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	const wallX = 1200.0
	spawnUnitWall(t, s, "p1", wallX)

	mover := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1800, Y: 768})
	if mover == nil {
		t.Fatal("failed to spawn mover")
	}
	mover.Visible = true
	mover.MaxHP, mover.HP = 500, 500
	mover.MoveSpeed = 150
	s.initializeCombatUnitLocked(mover)

	// Destination is on the far side of the wall from the mover.
	blocked := s.getBlockedCellsLocked()
	s.assignUnitPath(mover, protocol.Vec2{X: 400, Y: 768}, blocked, nil)

	if !mover.Moving || len(mover.Path) == 0 {
		t.Fatalf("ally mover could not path past a stationary friendly wall: "+
			"Moving=%v PathLen=%d (friendly units must not block ally pathing)",
			mover.Moving, len(mover.Path))
	}
}

// TestFriendlyWall_StillBlocksHostileMover guards against over-broadening: the
// SAME player-owned wall must still be a hard partition for a hostile (enemy
// faction) mover. Also proves the fixture is a genuine unit-partition rather
// than a terrain-walkable shortcut.
func TestFriendlyWall_StillBlocksHostileMover(t *testing.T) {
	s := NewGameStateWithSeed(passthroughMapConfig(), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	const wallX = 1200.0
	spawnUnitWall(t, s, "p1", wallX)

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1800, Y: 768})
	if enemy == nil {
		t.Fatal("failed to spawn enemy mover")
	}
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 500, 500
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)

	blocked := s.getBlockedCellsLocked()
	s.assignUnitPath(enemy, protocol.Vec2{X: 400, Y: 768}, blocked, nil)

	if enemy.Moving {
		t.Fatal("hostile mover routed through a player-owned wall — friendly " +
			"pass-through must not weaken hostile blocking")
	}
}

// TestEnemyWall_StillBlocksEnemyMover protects the enemy-blocked-objective
// invariant: the wave AI (__enemy__) is never friendly even to itself, so a
// wall of enemy units must still block another enemy unit's path. If this
// regressed, walled-off waves would clip through their own blockers instead of
// engaging the player's wall.
func TestEnemyWall_StillBlocksEnemyMover(t *testing.T) {
	s := NewGameStateWithSeed(passthroughMapConfig(), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	const wallX = 1200.0
	spawnUnitWall(t, s, enemyPlayerID, wallX)

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1800, Y: 768})
	if enemy == nil {
		t.Fatal("failed to spawn enemy mover")
	}
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 500, 500
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)

	blocked := s.getBlockedCellsLocked()
	s.assignUnitPath(enemy, protocol.Vec2{X: 400, Y: 768}, blocked, nil)

	if enemy.Moving {
		t.Fatal("enemy mover routed through a wall of its own faction — the " +
			"enemy-blocked-objective invariant requires __enemy__ units to " +
			"block each other")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Separation: a moving ally ghosts through an idle ally; idle allies still
// spread out among themselves.
// ─────────────────────────────────────────────────────────────────────────────

// TestMovingAlly_GhostsThroughIdleAlly is the second new behavior: when at
// least one of a friendly pair is moving, the per-tick separation push is
// skipped entirely, so a moving unit slides straight through a stationary ally
// (works even in a one-unit-wide corridor where the idle ally has nowhere to
// be shoved).
func TestMovingAlly_GhostsThroughIdleAlly(t *testing.T) {
	s := NewGameStateWithSeed(passthroughMapConfig(), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	idle := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 500, Y: 500})
	mover := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 500, Y: 500})
	for _, u := range []*Unit{idle, mover} {
		u.Visible = true
		u.MaxHP, u.HP = 500, 500
		s.initializeCombatUnitLocked(u)
	}
	idle.Moving = false
	mover.Moving = true

	idleX, idleY := idle.X, idle.Y
	moverX, moverY := mover.X, mover.Y

	s.applyUnitSeparationLocked(s.getBlockedCellsLocked())

	if idle.X != idleX || idle.Y != idleY {
		t.Fatalf("idle ally was shoved by a moving ally: (%.3f,%.3f) → (%.3f,%.3f); "+
			"a mover must ghost through allies, not push them",
			idleX, idleY, idle.X, idle.Y)
	}
	if mover.X != moverX || mover.Y != moverY {
		t.Fatalf("moving ally was deflected by an idle ally: (%.3f,%.3f) → (%.3f,%.3f); "+
			"separation must not apply between allies while one is moving",
			moverX, moverY, mover.X, mover.Y)
	}
}

// TestIdleAllies_StillSeparate guards the "no permanent stacking" half of the
// design: two overlapping allies that are BOTH idle must still be pushed apart,
// so a selected blob spreads out instead of fusing onto a single point.
func TestIdleAllies_StillSeparate(t *testing.T) {
	s := NewGameStateWithSeed(passthroughMapConfig(), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 500, Y: 500})
	b := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 504, Y: 500})
	for _, u := range []*Unit{a, b} {
		u.Visible = true
		u.MaxHP, u.HP = 500, 500
		u.Moving = false
		s.initializeCombatUnitLocked(u)
	}

	beforeSq := distanceSquared(a.X, a.Y, b.X, b.Y)
	s.applyUnitSeparationLocked(s.getBlockedCellsLocked())
	afterSq := distanceSquared(a.X, a.Y, b.X, b.Y)

	if afterSq <= beforeSq {
		t.Fatalf("two idle allies did not separate: distSq %.3f → %.3f; idle "+
			"allies must still spread out (no permanent stacking)",
			beforeSq, afterSq)
	}
}

// TestMovingUnit_StillShovesHostile guards against over-broadening the skip:
// the friendly-pass-through must NOT relax separation between hostile units.
// A moving unit overlapping an enemy must still push it apart.
func TestMovingUnit_StillShovesHostile(t *testing.T) {
	s := NewGameStateWithSeed(passthroughMapConfig(), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	mover := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 500, Y: 500})
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 503, Y: 500})
	mover.Visible, enemy.Visible = true, true
	mover.MaxHP, mover.HP = 500, 500
	enemy.MaxHP, enemy.HP = 500, 500
	s.initializeCombatUnitLocked(mover)
	s.initializeCombatUnitLocked(enemy)
	mover.Moving = true
	enemy.Moving = false

	beforeSq := distanceSquared(mover.X, mover.Y, enemy.X, enemy.Y)
	s.applyUnitSeparationLocked(s.getBlockedCellsLocked())
	afterSq := distanceSquared(mover.X, mover.Y, enemy.X, enemy.Y)

	if afterSq <= beforeSq {
		t.Fatalf("a moving unit did not shove an overlapping hostile: distSq "+
			"%.3f → %.3f; friendly pass-through must not relax hostile "+
			"separation", beforeSq, afterSq)
	}
}
