package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestDiagnostic_AIUnreachableRetarget_IsObserved proves the unreachable-
// retarget instrumentation actually fires on the exact hypothesized stutter
// condition: an enemy with an AI-acquired unit target it cannot path to
// because a wall of (hostile, from the enemy's POV) units closes the fine
// sub-cell corridor. Coarse terrain A* succeeds (the wall is units, not
// terrain), the fine sub-cell A* exhausts and fails, and the enemy gives up
// → dropUnreachableAITargetLocked.
//
// This is an evidence test, not a behavior test: it asserts the DIAGNOSTIC
// observes the condition (fineFail counter + enemy unreachable-retarget
// counter both increment), so a real-game capture can't read zero here due to
// a wiring gap. The tracker is injected directly because the production one is
// gated behind WEBRTS_DEBUG_PATHING at package-init time.
func TestDiagnostic_AIUnreachableRetarget_IsObserved(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	tracker := &debugPathTracker{
		unitStats:    make(map[int]*unitPathDebugStats),
		callerCounts: make(map[string]int),
	}
	s.debugPathTracker = tracker

	// Full player-unit wall partition (units, not terrain): coarse A* still
	// finds a terrain route, the fine sub-cell A* cannot get through.
	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		s.initializeCombatUnitLocked(w)
	}

	victim := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 768})
	victim.Visible = true
	victim.MaxHP, victim.HP = 50, 50
	s.initializeCombatUnitLocked(victim)
	victimID := victim.ID

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1300, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)

	blocked := s.getBlockedCellsLocked()

	// AI-acquired (not OrderAttackTarget) unit target behind the wall.
	enemy.AttackTargetID = victimID
	s.assignAttackApproachPathLocked(enemy, s.unitsByID[victimID], blocked)

	if tracker.totalFinePathFails == 0 {
		t.Fatalf("diagnostic did not observe a fine sub-cell A* failure; "+
			"fineFail=%d (the chased-into-a-wall path must register)",
			tracker.totalFinePathFails)
	}
	if tracker.unreachableRetargets == 0 || tracker.unreachableRetargetsEnemy == 0 {
		t.Fatalf("diagnostic did not observe an enemy unreachable-retarget; "+
			"unreachRetarget=%d enemy=%d",
			tracker.unreachableRetargets, tracker.unreachableRetargetsEnemy)
	}
	// Coarse A* should have SUCCEEDED here (wall is units, not terrain) — this
	// is what makes fineFail the dominant cost rather than coarseFail.
	if tracker.totalCoarsePathFails != 0 {
		t.Fatalf("expected coarse terrain A* to succeed (wall is units); "+
			"coarseFail=%d", tracker.totalCoarsePathFails)
	}
}
