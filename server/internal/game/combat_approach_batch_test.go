package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestApproachBatch_MultipleAttackersOneTarget verifies the batched approach
// pass: K attackers that newly acquire the same target in one evaluate pass
// must end up with non-empty paths after processApproachBatchLocked, and the
// budget should be debited exactly once (one leader A* per target group,
// not K).
func TestApproachBatch_MultipleAttackersOneTarget(t *testing.T) {
	s := newApproachBudgetState(t)
	s.EnsurePlayer("p2")
	s.mu.Lock()
	defer s.mu.Unlock()

	// One hostile target far from the attackers.
	target := s.spawnPlayerUnitLocked("soldier", "p2", "#f00", protocol.Vec2{X: 2000, Y: 2000})
	target.Visible = true

	const k = 4
	attackers := make([]*Unit, 0, k)
	for i := 0; i < k; i++ {
		// Spread attackers so LoS to leader's first waypoint succeeds for all
		// (lineWalkableLocked is straight-line; nearby attackers in open terrain
		// will share the leader's path via truncation).
		a := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{
			X: 300 + float64(i)*20,
			Y: 300,
		})
		a.Visible = true
		attackers = append(attackers, a)
	}

	// Build the batch: all attackers acquire the same target this tick.
	batch := map[int][]*Unit{target.ID: attackers}
	s.combatApproachBudgetRemaining = combatApproachBudgetPerTick

	blocked := s.getBlockedCellsLocked()
	s.processApproachBatchLocked(batch, blocked)

	// Budget should have decremented exactly once — one leader A* covered all K.
	if s.combatApproachBudgetRemaining != combatApproachBudgetPerTick-1 {
		t.Errorf("budget after batched group = %d; want %d (one leader A* should cover all K attackers)",
			s.combatApproachBudgetRemaining, combatApproachBudgetPerTick-1)
	}

	// Every attacker should have received a path (leader from A*, followers
	// via leader-path truncation).
	for i, a := range attackers {
		if len(a.Path) == 0 {
			t.Errorf("attacker[%d] got no path after batch; expected leader-follower truncation to assign one", i)
		}
		if !a.Moving {
			t.Errorf("attacker[%d] Moving=false after batch; expected true", i)
		}
	}
}

// TestApproachBatch_BudgetExhaustionDriftsExcessGroups verifies that when more
// distinct target groups exist than budget allows, the over-budget groups
// drift instead of paying an A* — i.e. the budget gates groups, not units.
func TestApproachBatch_BudgetExhaustionDriftsExcessGroups(t *testing.T) {
	s := newApproachBudgetState(t)
	s.EnsurePlayer("p2")
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create combatApproachBudgetPerTick + 2 distinct targets. The first N
	// groups consume budget; the rest drift.
	n := combatApproachBudgetPerTick + 2
	batch := map[int][]*Unit{}
	attackerByTarget := map[int]*Unit{}
	for i := 0; i < n; i++ {
		target := s.spawnPlayerUnitLocked("soldier", "p2", "#f00", protocol.Vec2{
			X: 2000 + float64(i)*30,
			Y: 2000,
		})
		target.Visible = true
		attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{
			X: 300 + float64(i)*40,
			Y: 300,
		})
		attacker.Visible = true
		batch[target.ID] = []*Unit{attacker}
		attackerByTarget[target.ID] = attacker
	}

	s.combatApproachBudgetRemaining = combatApproachBudgetPerTick
	blocked := s.getBlockedCellsLocked()
	s.processApproachBatchLocked(batch, blocked)

	if s.combatApproachBudgetRemaining != 0 {
		t.Errorf("budget after %d groups (cap=%d) = %d; want 0",
			n, combatApproachBudgetPerTick, s.combatApproachBudgetRemaining)
	}

	// Count drifted vs path-assigned attackers.
	drifted := 0
	pathed := 0
	for _, attacker := range attackerByTarget {
		if attacker.AttackDrifting && len(attacker.Path) == 0 {
			drifted++
		} else if len(attacker.Path) > 0 {
			pathed++
		}
	}
	if pathed != combatApproachBudgetPerTick {
		t.Errorf("pathed groups = %d; want %d (budget cap)", pathed, combatApproachBudgetPerTick)
	}
	if drifted != n-combatApproachBudgetPerTick {
		t.Errorf("drifted groups = %d; want %d (excess past budget)", drifted, n-combatApproachBudgetPerTick)
	}
}
