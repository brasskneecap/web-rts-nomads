package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

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

// wipeCampViaMeleeCombat clears the camp the way real gameplay does: a
// player-owned attacker lands killing blows through resolveAttackHitLocked,
// which appends each dead unit to deadUnitIDs and the caller removes them
// synchronously (the legacy direct-remove path) — NOT the deferred
// drainPendingDeathsLocked path the manual wipeCampAttributedTo helper mimics.
// This is the path that actually runs when a player clears a camp on forest-1.
func wipeCampViaMeleeCombat(t *testing.T, s *GameState, camp *NeutralCamp, attacker *Unit) {
	t.Helper()
	ids := append([]int(nil), camp.AliveUnitIDs...)
	if len(ids) == 0 {
		t.Fatal("setup: camp has no alive units to wipe")
	}
	var deadUnitIDs []int
	for _, id := range ids {
		target := s.getUnitByIDLocked(id)
		if target == nil {
			continue
		}
		// Overkill damage so each hit is lethal in one blow.
		s.resolveAttackHitLocked(attacker, target, target.MaxHP+1_000_000, &deadUnitIDs)
	}
	// Caller-side synchronous removal, mirroring the real combat loops in
	// state_combat.go / projectile.go.
	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
	if len(camp.AliveUnitIDs) != 0 {
		t.Fatalf("setup: camp roster should be empty after melee wipe, has %d", len(camp.AliveUnitIDs))
	}
}

// Regression: a player who clears a camp with normal melee combat must get
// objective credit. The kill flows through resolveAttackHitLocked's legacy
// direct-remove path, which (before the fix) never called markCampKillerLocked,
// so LastKillerWasPlayer stayed false and the kill_camps objective was never
// credited even though the player did all the killing.
func TestCampClearMetric_MeleeCombatKillCredits(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = continuousTestPlayer("p1")
	s.tickNeutralCampsLocked() // initial roster spawn
	camp := &s.NeutralCamps[0]
	tier := camp.CurrentTier

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})

	wipeCampViaMeleeCombat(t, s, camp, attacker)

	m := s.Players["p1"].Metrics
	if m.NeutralCampsKilled != 1 {
		t.Errorf("player melee-cleared camp should credit NeutralCampsKilled=1, got %d", m.NeutralCampsKilled)
	}
	if m.NeutralCampsKilledByTier[tier] != 1 {
		t.Errorf("credit should land at the camp's current tier %d, got map %v", tier, m.NeutralCampsKilledByTier)
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
