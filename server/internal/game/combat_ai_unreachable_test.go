package game

// combat_ai_unreachable_test.go — QA-authored tests for the unreachable-target
// cooldown introduced to prevent per-tick A* storms when many units crowd an
// inaccessible enemy.
//
// Coverage:
//   A. assignAttackApproachPathLocked stamps UnreachableTargetID + UnreachableUntilTick
//      and clears AttackTargetID when findPath returns empty (goal reachable by
//      BFS but no A* route from start to goal).
//   B. selectBestTargetLocked skips the memoised target; the unit picks a second
//      reachable enemy instead.
//   C. After unreachableTargetCooldownTicks have elapsed, the memoised target is
//      eligible again — the unit can reacquire it.
//   D. Sticky-order path (OrderAttackTarget): memo IS stamped, target is NOT cleared.
//
// Implementation note on the "no-walkable-neighbor" branch
// (assignAttackApproachPathLocked line ~84):
//   findNearestWalkable performs an unbounded BFS and returns (_, false) only
//   when EVERY cell in the entire grid is blocked. That state is impossible on
//   any real map. All tests here exercise the findPath-returns-empty branch
//   (lines ~96-104), which is the path actually triggered by the reported bug
//   (target surrounded by units / terrain, goal cell walkable, but no A* route).

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// newUnreachableTestState returns a GameState with:
//   - Player "p1" with a soldier at (400, 224) — grid row 3, above a wall we build.
//   - An enemy soldier at (400, 928) — grid row 14, below the wall.
//
// A 3-row horizontal wall at grid rows 7-9 separates them; both the attacker's
// cell and the target's goal cell are walkable, but A* cannot cross the wall.
// Lock is NOT held on return.
func newUnreachableTestState(t *testing.T) (s *GameState, attacker, target *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Attacker at grid row 3 (y=3*64+32=224) — above the wall.
	attacker = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 224})
	attacker.MaxHP = 500
	attacker.HP = 500
	attacker.Visible = true
	attacker.AttackRange = 80
	attacker.Damage = 10
	attacker.AttackSpeed = 1.0
	attacker.MoveSpeed = 150
	s.initializeCombatUnitLocked(attacker)

	// Target at grid row 14 (y=14*64+32=928) — below the wall.
	target = s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 928})
	target.MaxHP = 500
	target.HP = 500
	target.Visible = true
	target.AttackRange = 80
	target.Damage = 5
	target.AttackSpeed = 1.0
	target.MoveSpeed = 0 // stationary
	s.initializeCombatUnitLocked(target)

	return s, attacker, target
}

// buildImpassableWall blocks all cells in rows 7, 8, 9 across the full map
// width (GridCols=24) so there is no walkable route between row 3 and row 14.
// Both the attacker's cell and the target's goal cell remain walkable; only
// the passage between them is sealed. This forces findPath to return empty.
func buildImpassableWall(blocked map[gridPoint]bool) {
	for x := 0; x < 24; x++ {
		blocked[gridPoint{X: x, Y: 7}] = true
		blocked[gridPoint{X: x, Y: 8}] = true
		blocked[gridPoint{X: x, Y: 9}] = true
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A. findPath-returns-empty branch: memo stamped, target cleared
// ─────────────────────────────────────────────────────────────────────────────

// TestUnreachable_EmptyPath_MemoStamped verifies that when findPath returns an
// empty slice (start and goal cells individually walkable but no A* route
// exists), assignAttackApproachPathLocked stamps UnreachableTargetID and
// UnreachableUntilTick and clears AttackTargetID (for a non-sticky order).
func TestUnreachable_EmptyPath_MemoStamped(t *testing.T) {
	s, attacker, target := newUnreachableTestState(t)

	s.mu.Lock()

	attacker.AttackTargetID = target.ID
	// Default order is OrderIdle — no sticky order.

	blocked := s.getBlockedCellsLocked()
	buildImpassableWall(blocked)

	tickBefore := s.Tick
	s.assignAttackApproachPathLocked(attacker, target, blocked)

	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if attacker.UnreachableTargetID != target.ID {
		t.Errorf("UnreachableTargetID: want %d, got %d", target.ID, attacker.UnreachableTargetID)
	}
	if attacker.UnreachableUntilTick <= tickBefore {
		t.Errorf("UnreachableUntilTick (%d) should be > tick at call (%d)",
			attacker.UnreachableUntilTick, tickBefore)
	}
	expectedCooldown := tickBefore + unreachableTargetCooldownTicks
	if attacker.UnreachableUntilTick != expectedCooldown {
		t.Errorf("UnreachableUntilTick: want tick+%d=%d, got %d",
			unreachableTargetCooldownTicks, expectedCooldown, attacker.UnreachableUntilTick)
	}
	// Target must be cleared for a non-sticky order.
	if attacker.AttackTargetID != 0 {
		t.Errorf("AttackTargetID: want 0 after empty path, got %d", attacker.AttackTargetID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B. Scoring skips memoised target; picks the reachable alternative
// ─────────────────────────────────────────────────────────────────────────────

// TestUnreachable_ScoringSkipsMemoised verifies that when UnreachableTargetID
// is set and s.Tick < UnreachableUntilTick, selectBestTargetLocked ignores
// that candidate and selects a different in-range reachable enemy.
func TestUnreachable_ScoringSkipsMemoised(t *testing.T) {
	s, attacker, unreachableEnemy := newUnreachableTestState(t)

	s.mu.Lock()

	// Reposition the attacker and both enemies onto the same open area so
	// detection range and leash are not factors.
	attacker.X = 400
	attacker.Y = 400
	attacker.CombatAnchorX = attacker.X
	attacker.CombatAnchorY = attacker.Y

	unreachableEnemy.X = attacker.X + 60
	unreachableEnemy.Y = attacker.Y

	// Stamp the memo as if path-finding already failed this tick.
	attacker.UnreachableTargetID = unreachableEnemy.ID
	attacker.UnreachableUntilTick = s.Tick + unreachableTargetCooldownTicks

	// Spawn a second, fully reachable enemy right next to the attacker.
	reachableEnemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: attacker.X + 50, Y: attacker.Y + 10})
	reachableEnemy.MaxHP = 500
	reachableEnemy.HP = 500
	reachableEnemy.Visible = true
	reachableEnemy.AttackRange = 80
	reachableEnemy.Damage = 5
	reachableEnemy.AttackSpeed = 1.0
	reachableEnemy.MoveSpeed = 0
	s.initializeCombatUnitLocked(reachableEnemy)

	index := newCombatSpatialIndex(combatSpatialBucketSize)
	for _, u := range s.Units {
		if u == nil || !u.Visible || u.HP <= 0 {
			continue
		}
		s.initializeCombatUnitLocked(u)
		index.add(u)
	}
	blocked := s.getBlockedCellsLocked()
	ctx := combatEvalContext{index: index, blocked: blocked}
	profile := resolveCombatProfile(attacker)

	best := s.selectBestTargetLocked(attacker, profile, ctx)

	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if best.Kind != combatTargetUnit {
		t.Fatalf("expected a unit target, got kind=%d (no target)", best.Kind)
	}
	if best.Unit == nil {
		t.Fatal("best.Unit is nil")
	}
	if best.Unit.ID == unreachableEnemy.ID {
		t.Errorf("scoring selected the memoised unreachable enemy (ID %d); should have picked the reachable one (ID %d)",
			unreachableEnemy.ID, reachableEnemy.ID)
	}
	if best.Unit.ID != reachableEnemy.ID {
		t.Errorf("expected best target = reachable enemy (ID %d), got ID %d",
			reachableEnemy.ID, best.Unit.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// C. Cooldown expiry: target becomes eligible again
// ─────────────────────────────────────────────────────────────────────────────

// TestUnreachable_CooldownExpiry_TargetReeligible verifies that once s.Tick
// has advanced to unreachableUntilTick, the condition (s.Tick < UnreachableUntilTick)
// is false and the previously memoised target is scored and selected again.
func TestUnreachable_CooldownExpiry_TargetReeligible(t *testing.T) {
	s, attacker, enemy := newUnreachableTestState(t)

	s.mu.Lock()

	// Place everyone in the same open area, within detection range.
	attacker.X = 400
	attacker.Y = 400
	attacker.CombatAnchorX = attacker.X
	attacker.CombatAnchorY = attacker.Y
	enemy.X = attacker.X + 60
	enemy.Y = attacker.Y

	// Stamp the memo expiring at tick 40.
	attacker.UnreachableTargetID = enemy.ID
	attacker.UnreachableUntilTick = unreachableTargetCooldownTicks // 40

	buildIndex := func() combatEvalContext {
		index := newCombatSpatialIndex(combatSpatialBucketSize)
		for _, u := range s.Units {
			if u == nil || !u.Visible || u.HP <= 0 {
				continue
			}
			s.initializeCombatUnitLocked(u)
			index.add(u)
		}
		blocked := s.getBlockedCellsLocked()
		return combatEvalContext{index: index, blocked: blocked}
	}

	profile := resolveCombatProfile(attacker)

	// During cooldown: tick 39 < 40 → enemy should be skipped (no other candidates).
	s.Tick = unreachableTargetCooldownTicks - 1
	bestDuringCooldown := s.selectBestTargetLocked(attacker, profile, buildIndex())

	// After cooldown: tick 40 < 40 is false → enemy is eligible.
	s.Tick = unreachableTargetCooldownTicks
	bestAfterCooldown := s.selectBestTargetLocked(attacker, profile, buildIndex())

	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	// During cooldown: enemy must NOT appear in selection (only candidate, so result is None).
	if bestDuringCooldown.Kind == combatTargetUnit && bestDuringCooldown.Unit != nil &&
		bestDuringCooldown.Unit.ID == enemy.ID {
		t.Errorf("during cooldown (tick %d < %d): memoised enemy was selected; want no target",
			unreachableTargetCooldownTicks-1, unreachableTargetCooldownTicks)
	}

	// After cooldown: enemy must be selected.
	if bestAfterCooldown.Kind != combatTargetUnit {
		t.Fatalf("after cooldown (tick %d >= %d): expected combatTargetUnit, got kind=%d (no target)",
			unreachableTargetCooldownTicks, unreachableTargetCooldownTicks, bestAfterCooldown.Kind)
	}
	if bestAfterCooldown.Unit == nil || bestAfterCooldown.Unit.ID != enemy.ID {
		gotID := 0
		if bestAfterCooldown.Unit != nil {
			gotID = bestAfterCooldown.Unit.ID
		}
		t.Errorf("after cooldown expiry: want enemy ID %d, got %d", enemy.ID, gotID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// D. Sticky OrderAttackTarget: memo stamped but target NOT cleared
// ─────────────────────────────────────────────────────────────────────────────

// TestUnreachable_StickyOrder_MemoStampedTargetPreserved verifies that when
// unit.Order.Type == OrderAttackTarget, a path failure still stamps the
// UnreachableTargetID memo (future scoring skips the candidate) but does NOT
// call clearCombatTargetLocked — AttackTargetID must remain set so the
// player's explicit directive is honoured.
func TestUnreachable_StickyOrder_MemoStampedTargetPreserved(t *testing.T) {
	s, attacker, target := newUnreachableTestState(t)

	s.mu.Lock()

	// Issue a sticky player order on this attacker.
	attacker.Order = OrderState{Type: OrderAttackTarget}
	attacker.AttackTargetID = target.ID

	blocked := s.getBlockedCellsLocked()
	// Separate attacker (row 3) and target (row 14) with the impassable wall.
	buildImpassableWall(blocked)

	tickBefore := s.Tick
	s.assignAttackApproachPathLocked(attacker, target, blocked)

	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Memo must be stamped.
	if attacker.UnreachableTargetID != target.ID {
		t.Errorf("UnreachableTargetID: want %d, got %d", target.ID, attacker.UnreachableTargetID)
	}
	if attacker.UnreachableUntilTick != tickBefore+unreachableTargetCooldownTicks {
		t.Errorf("UnreachableUntilTick: want %d, got %d",
			tickBefore+unreachableTargetCooldownTicks, attacker.UnreachableUntilTick)
	}

	// Target must NOT be cleared — sticky order preserved.
	if attacker.AttackTargetID != target.ID {
		t.Errorf("AttackTargetID cleared for sticky order: want %d, got %d",
			target.ID, attacker.AttackTargetID)
	}
	// Order must still be OrderAttackTarget.
	if attacker.Order.Type != OrderAttackTarget {
		t.Errorf("Order.Type changed for sticky order: want OrderAttackTarget (%d), got %d",
			OrderAttackTarget, attacker.Order.Type)
	}
}
