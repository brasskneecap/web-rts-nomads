package game

import "testing"

// Regression tests for camp-clear metric attribution: the kill_camps
// objective credited every human player whenever a camp's roster hit zero
// while active — including camps wiped by the __enemy__ wave faction (or by
// anonymous damage). In the field this silently completed "kill 3 rank 2
// camps" and ended a match the player never earned.
//
// The rule now: the clear only counts when the killing blow on the camp's
// final unit came from a real player's team. Tier semantics are unchanged —
// the camp's CurrentTier at wipe time is what's credited.

// wipeCampAttributedTo removes every alive camp unit, marking each kill as
// landed by killerOwnerID — the same (mark, remove) order the damage
// pipeline's drainPendingDeathsLocked uses.
func wipeCampAttributedTo(t *testing.T, s *GameState, camp *NeutralCamp, killerOwnerID string) {
	t.Helper()
	ids := append([]int(nil), camp.AliveUnitIDs...)
	if len(ids) == 0 {
		t.Fatal("setup: camp has no alive units to wipe")
	}
	for _, id := range ids {
		s.markCampKillerLocked(camp.PlacementID, killerOwnerID)
		s.removeUnitLocked(id)
	}
	if len(camp.AliveUnitIDs) != 0 {
		t.Fatalf("setup: camp roster should be empty after wipe, has %d", len(camp.AliveUnitIDs))
	}
}

func TestCampClearMetric_PlayerKillCreditsAtCurrentTier(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.Players["p1"] = continuousTestPlayer("p1")
	s.tickNeutralCampsLocked() // initial roster spawn
	camp := &s.NeutralCamps[0]
	tier := camp.CurrentTier

	wipeCampAttributedTo(t, s, camp, "p1")

	m := s.Players["p1"].Metrics
	if m.NeutralCampsKilled != 1 {
		t.Errorf("player-killed camp should credit NeutralCampsKilled=1, got %d", m.NeutralCampsKilled)
	}
	if m.NeutralCampsKilledByTier[tier] != 1 {
		t.Errorf("credit should land at the camp's current tier %d, got map %v", tier, m.NeutralCampsKilledByTier)
	}
}

func TestCampClearMetric_EnemyFactionWipeDoesNotCredit(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.Players["p1"] = continuousTestPlayer("p1")
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]

	wipeCampAttributedTo(t, s, camp, enemyPlayerID)

	if got := s.Players["p1"].Metrics.NeutralCampsKilled; got != 0 {
		t.Errorf("enemy-wiped camp must NOT credit the player, got %d", got)
	}
}

func TestCampClearMetric_AnonymousWipeDoesNotCredit(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.Players["p1"] = continuousTestPlayer("p1")
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]

	wipeCampAttributedTo(t, s, camp, "")

	if got := s.Players["p1"].Metrics.NeutralCampsKilled; got != 0 {
		t.Errorf("anonymous camp wipe must NOT credit the player, got %d", got)
	}
}
