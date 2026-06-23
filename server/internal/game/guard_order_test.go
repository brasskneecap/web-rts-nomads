package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// The Guard command (GuardUnits) is the player-facing face of the GuardMode
// machinery. These tests pin the command wiring — field setup, derived ranges,
// engage-then-return behavior, and that any subsequent order exits guard mode.
// The underlying combat behavior (aggro / leash / return-to-anchor) is covered
// by the neutral-camp and placed-enemy guard tests; here we only verify that a
// player-issued guard plugs into it correctly.

// expectedGuardRanges mirrors applyGuardOrderLocked so assertions derive from
// the same constants the implementation uses rather than hardcoding tunables.
func expectedGuardRanges(unit *Unit) (aggro, leash float64) {
	aggro = unit.VisionRange
	if aggro < guardMinAggroRange {
		aggro = guardMinAggroRange
	}
	return aggro, aggro * guardLeashAggroMultiplier
}

func TestGuard_SetsGuardModeAndDerivedRanges(t *testing.T) {
	s, unit := newOrderTestState(t)
	post := protocol.Vec2{X: unit.X, Y: unit.Y} // guard in place: anchor is known, no travel

	s.GuardUnits("p1", []int{unit.ID})

	s.mu.RLock()
	defer s.mu.RUnlock()

	if unit.Order.Type != OrderGuard {
		t.Errorf("Order.Type = %v, want OrderGuard", unit.Order.Type)
	}
	if !unit.GuardMode {
		t.Error("GuardMode should be true after GuardUnits")
	}
	if unit.GuardAnchorX != post.X || unit.GuardAnchorY != post.Y {
		t.Errorf("GuardAnchor = (%.1f,%.1f), want (%.1f,%.1f)",
			unit.GuardAnchorX, unit.GuardAnchorY, post.X, post.Y)
	}
	if unit.CombatAnchorX != post.X || unit.CombatAnchorY != post.Y {
		t.Errorf("CombatAnchor = (%.1f,%.1f), want (%.1f,%.1f)",
			unit.CombatAnchorX, unit.CombatAnchorY, post.X, post.Y)
	}
	wantAggro, wantLeash := expectedGuardRanges(unit)
	if unit.GuardAggroRange != wantAggro {
		t.Errorf("GuardAggroRange = %.1f, want %.1f", unit.GuardAggroRange, wantAggro)
	}
	if unit.GuardLeashRange != wantLeash {
		t.Errorf("GuardLeashRange = %.1f, want %.1f", unit.GuardLeashRange, wantLeash)
	}
	if unit.GuardLeashRange <= unit.GuardAggroRange {
		t.Error("leash must exceed aggro so an edge-of-aggro target is not dropped on acquisition")
	}
}

// Workers carry the "attack" capability for last-ditch self-defense but are
// NonCombat (they never auto-acquire), so guarding them would be inert.
// GuardUnits excludes them.
func TestGuard_WorkerExcluded(t *testing.T) {
	s, _ := newOrderTestState(t)

	s.mu.Lock()
	worker := s.spawnPlayerUnitLocked("worker", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	worker.Visible = true
	if !worker.NonCombat {
		s.mu.Unlock()
		t.Fatal("fixture assumption broken: worker should be NonCombat")
	}
	workerID := worker.ID
	s.mu.Unlock()

	s.GuardUnits("p1", []int{workerID})

	s.mu.RLock()
	defer s.mu.RUnlock()
	if worker.GuardMode {
		t.Error("a NonCombat worker must not be put into guard mode")
	}
	if worker.Order.Type == OrderGuard {
		t.Error("a NonCombat worker must not receive OrderGuard")
	}
}

// Eligibility also enforces ownership: a player cannot guard-order units they
// don't own.
func TestGuard_RejectsUnitsNotOwnedByCommander(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.GuardUnits("p2", []int{unit.ID})

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unit.GuardMode {
		t.Error("a unit must not enter guard mode from another player's command")
	}
	if unit.Order.Type == OrderGuard {
		t.Error("a unit must not receive OrderGuard from another player's command")
	}
}

func TestGuard_EngagesEnemyInAggroThenReturns(t *testing.T) {
	s, unit := newOrderTestState(t)
	anchor := protocol.Vec2{X: unit.X, Y: unit.Y}

	s.GuardUnits("p1", []int{unit.ID})

	s.mu.RLock()
	aggro := unit.GuardAggroRange
	s.mu.RUnlock()

	// Enemy well inside the aggro radius but offset enough that the guard must
	// leave its anchor to reach attack range.
	enemy := spawnOrderEnemy(t, s, anchor.X, anchor.Y+aggro*0.6)

	// Engage: within a short window the guard acquires the intruder.
	acquired := false
	for i := 0; i < 60 && !acquired; i++ {
		tickN(s, 1)
		s.mu.RLock()
		acquired = unit.AttackTargetID == enemy.ID
		s.mu.RUnlock()
	}
	if !acquired {
		t.Fatal("guard did not acquire an enemy that entered its aggro radius")
	}

	// Remove the intruder; the guard should walk home and resume guarding.
	s.mu.Lock()
	enemy.HP = 0
	s.mu.Unlock()

	tickN(s, 120)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if !unit.GuardMode {
		t.Error("guard should still be in guard mode after the fight")
	}
	if unit.AttackTargetID != 0 {
		t.Errorf("guard should have no target after the enemy died, got %d", unit.AttackTargetID)
	}
	distToAnchor := math.Hypot(unit.X-anchor.X, unit.Y-anchor.Y)
	if distToAnchor > 40 {
		t.Errorf("guard should have returned to its anchor, distance = %.1f", distToAnchor)
	}
	if unit.Status != "Guarding" {
		t.Errorf("guard back at post should report Status=Guarding, got %q", unit.Status)
	}
}

func TestGuard_IgnoresEnemyBeyondAggro(t *testing.T) {
	s, unit := newOrderTestState(t)
	anchor := protocol.Vec2{X: unit.X, Y: unit.Y}

	s.GuardUnits("p1", []int{unit.ID})

	s.mu.RLock()
	aggro := unit.GuardAggroRange
	s.mu.RUnlock()

	// Enemy parked comfortably outside the aggro radius.
	spawnOrderEnemy(t, s, anchor.X, anchor.Y+aggro+150)

	tickN(s, 60)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unit.AttackTargetID != 0 {
		t.Errorf("guard acquired an enemy beyond its aggro radius (target=%d)", unit.AttackTargetID)
	}
	if math.Hypot(unit.X-anchor.X, unit.Y-anchor.Y) > 40 {
		t.Error("guard left its post to chase an out-of-aggro enemy")
	}
}

func TestGuard_NewOrderClearsGuardMode(t *testing.T) {
	s, unit := newOrderTestState(t)
	s.GuardUnits("p1", []int{unit.ID})

	s.mu.RLock()
	if !unit.GuardMode {
		s.mu.RUnlock()
		t.Fatal("precondition: unit should be guarding")
	}
	s.mu.RUnlock()

	// Any subsequent order must exit guard mode.
	s.MoveUnits("p1", []int{unit.ID}, protocol.Vec2{X: unit.X + 200, Y: unit.Y})

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unit.GuardMode {
		t.Error("a move order must clear GuardMode")
	}
	if unit.GuardAggroRange != 0 || unit.GuardLeashRange != 0 {
		t.Errorf("guard ranges should be cleared on a new order, got aggro=%.1f leash=%.1f",
			unit.GuardAggroRange, unit.GuardLeashRange)
	}
	if unit.Order.Type != OrderMove {
		t.Errorf("Order.Type = %v, want OrderMove", unit.Order.Type)
	}
}
