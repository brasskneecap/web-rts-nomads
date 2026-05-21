package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestApproachCoarsePathCache_HitOnSameStartGoal verifies that two
// assignAttackApproachPathLockedWithSubBlocked calls with identical start +
// goal cells share the coarse-path result within a tick: the cache map
// holds one entry after the second call, and both units end up moving
// (the cache hit must still produce a usable path, not a broken one).
func TestApproachCoarsePathCache_HitOnSameStartGoal(t *testing.T) {
	s := newApproachBudgetState(t)
	s.EnsurePlayer("p2")
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.spawnPlayerUnitLocked("soldier", "p2", "#f00", protocol.Vec2{X: 2000, Y: 2000})
	target.Visible = true

	// Two attackers placed at the same world position → same startCell.
	a1 := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 300, Y: 300})
	a1.Visible = true
	a2 := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 300, Y: 300})
	a2.Visible = true

	// Mirror tickCombatAILocked's per-tick reset so we measure this scenario
	// in isolation.
	s.approachCoarsePathCache = map[approachPathCacheKey][]protocol.Vec2{}
	s.combatApproachBudgetRemaining = combatApproachBudgetPerTick * 2

	blocked := s.getBlockedCellsLocked()
	s.assignAttackApproachPathLockedWithSubBlocked(a1, target, blocked, nil)
	if len(s.approachCoarsePathCache) != 1 {
		t.Fatalf("cache size after first call = %d; want 1 (first call should populate)",
			len(s.approachCoarsePathCache))
	}
	s.assignAttackApproachPathLockedWithSubBlocked(a2, target, blocked, nil)
	if len(s.approachCoarsePathCache) != 1 {
		t.Errorf("cache size after second call = %d; want 1 (same start+goal must hit, not insert)",
			len(s.approachCoarsePathCache))
	}
	if !a1.Moving || !a2.Moving {
		t.Errorf("both attackers should be moving after approach; a1.Moving=%v a2.Moving=%v",
			a1.Moving, a2.Moving)
	}
}

// TestApproachCoarsePathCache_MissOnDifferentStarts verifies that two
// attackers with different startCells produce two cache entries.
func TestApproachCoarsePathCache_MissOnDifferentStarts(t *testing.T) {
	s := newApproachBudgetState(t)
	s.EnsurePlayer("p2")
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.spawnPlayerUnitLocked("soldier", "p2", "#f00", protocol.Vec2{X: 2000, Y: 2000})
	target.Visible = true

	a1 := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 300, Y: 300})
	a1.Visible = true
	// Far enough apart to land in a different gridCell (cell size = 64).
	a2 := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 500, Y: 500})
	a2.Visible = true

	s.approachCoarsePathCache = map[approachPathCacheKey][]protocol.Vec2{}
	s.combatApproachBudgetRemaining = combatApproachBudgetPerTick * 2

	blocked := s.getBlockedCellsLocked()
	s.assignAttackApproachPathLockedWithSubBlocked(a1, target, blocked, nil)
	s.assignAttackApproachPathLockedWithSubBlocked(a2, target, blocked, nil)

	if len(s.approachCoarsePathCache) != 2 {
		t.Errorf("cache size = %d; want 2 (distinct startCells must produce distinct entries)",
			len(s.approachCoarsePathCache))
	}
}
