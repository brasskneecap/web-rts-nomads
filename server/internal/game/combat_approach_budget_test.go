package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newApproachBudgetState builds a minimal map with one player townhall and
// helpers for spawning attacker/target pairs. Lock NOT held on return.
func newApproachBudgetState(t *testing.T) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 60, 40
	owner := "p1"
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 2}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &owner,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	cfg := protocol.MapConfig{
		ID: "approach-budget-test", Name: "approach-budget-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{townhall},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.EnsurePlayer("p1")
	return s
}

// TestApproachBudget_ExhaustionDriftsRemainingUnits verifies that once
// combatApproachBudgetPerTick refresh calls have fired in a single tick,
// the next refresh drops the unit into drift mode instead of running A*.
// Drift = AttackDrifting true, Path nil, TargetX/Y set to target's position.
func TestApproachBudget_ExhaustionDriftsRemainingUnits(t *testing.T) {
	s := newApproachBudgetState(t)
	// Ensure p2 outside the lock so EnsurePlayer can acquire it cleanly.
	s.EnsurePlayer("p2")
	s.mu.Lock()
	defer s.mu.Unlock()

	// Spawn one target way out of any attacker's range, and N+2 attackers all
	// out of range of that target. With N = combatApproachBudgetPerTick, the
	// first N consume budget via A*, the rest must drift.
	target := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 1500, Y: 1500})
	target.OwnerID = "p2" // make hostile

	n := combatApproachBudgetPerTick
	attackers := make([]*Unit, 0, n+2)
	for i := 0; i < n+2; i++ {
		// Spread attackers so they don't collide / share paths.
		a := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{
			X: 300 + float64(i)*80,
			Y: 300,
		})
		a.Visible = true
		attackers = append(attackers, a)
	}
	target.Visible = true

	// Refill budget (mirrors what tickCombatAILocked does at tick start).
	s.combatApproachBudgetRemaining = combatApproachBudgetPerTick

	blocked := s.getBlockedCellsLocked()
	driftedAt := -1
	for i, a := range attackers {
		profile := resolveCombatProfile(a)
		// force=true to bypass the in-range short-circuit (target really is
		// far out of range, but exercise the budget code path explicitly).
		s.refreshUnitAttackApproachLocked(a, target, profile, blocked, true)
		if a.AttackDrifting && a.Path == nil && driftedAt < 0 {
			driftedAt = i
		}
	}

	if driftedAt < 0 {
		t.Fatalf("expected at least one attacker to drift; none did (budget=%d, attackers=%d)",
			combatApproachBudgetPerTick, len(attackers))
	}
	if driftedAt != combatApproachBudgetPerTick {
		t.Errorf("first drifted attacker index = %d; want %d (budget should let exactly N units A* before drifting)",
			driftedAt, combatApproachBudgetPerTick)
	}
	if s.combatApproachBudgetRemaining != 0 {
		t.Errorf("budget remaining after %d A* calls = %d; want 0",
			combatApproachBudgetPerTick, s.combatApproachBudgetRemaining)
	}
}

// TestApproachBudget_ResetEveryTick verifies that tickCombatAILocked
// refills the budget at the start of every combat tick.
func TestApproachBudget_ResetEveryTick(t *testing.T) {
	s := newApproachBudgetState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.combatApproachBudgetRemaining = 0
	blocked := s.getBlockedCellsLocked()
	s.tickCombatAILocked(0.05, blocked)
	if s.combatApproachBudgetRemaining != combatApproachBudgetPerTick {
		t.Errorf("budget after tickCombatAILocked = %d; want %d (tick start must refill)",
			s.combatApproachBudgetRemaining, combatApproachBudgetPerTick)
	}
}
