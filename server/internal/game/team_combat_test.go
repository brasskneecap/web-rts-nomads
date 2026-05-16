package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// setTeam forces ownerID's TeamID, creating the Player entry if needed.
// Caller holds s.mu.
func setTeam(s *GameState, ownerID string, team int) {
	if s.Players[ownerID] == nil {
		s.Players[ownerID] = &Player{ID: ownerID, TeamID: team}
		return
	}
	s.Players[ownerID].TeamID = team
}

// teamCombatUnit spawns a combat-ready soldier for ownerID at (x,y).
// Caller holds s.mu.
func teamCombatUnit(t *testing.T, s *GameState, ownerID string, x, y float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", ownerID, "#888", protocol.Vec2{X: x, Y: y})
	u.MaxHP, u.HP = 500, 500
	u.Visible = true
	u.AttackRange = 90
	u.Damage = 25
	u.AttackSpeed = 2.0
	u.AttackCooldown = 0
	u.MoveSpeed = 0 // stay put so range/engagement is deterministic
	s.initializeCombatUnitLocked(u)
	return u
}

// Two owners, units in range of each other. Same team ⇒ they never fight;
// different team ⇒ they do. Exercises the P0 chokepoint end-to-end through
// real combat ticks (acquisition gated by playersAreHostileLocked).
func TestTeam_CombatRespectsAlliance(t *testing.T) {
	run := func(t *testing.T, sameTeam bool) (aHP, bHP int) {
		s := newProjectileTestState(t)
		s.mu.Lock()
		a := teamCombatUnit(t, s, "p1", 400, 400)
		b := teamCombatUnit(t, s, "p2", 460, 400) // 60px apart, within range 90
		setTeam(s, "p1", 0)
		if sameTeam {
			setTeam(s, "p2", 0)
		} else {
			setTeam(s, "p2", 1)
		}
		aID, bID := a.ID, b.ID
		s.mu.Unlock()

		tickN(s, 60) // ~3s of combat at dt=0.05

		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.unitsByID[aID].HP, s.unitsByID[bID].HP
	}

	t.Run("same team → no friendly combat", func(t *testing.T) {
		aHP, bHP := run(t, true)
		if aHP != 500 || bHP != 500 {
			t.Errorf("same-team units must not fight; HP a=%d b=%d (want 500/500)", aHP, bHP)
		}
	})

	t.Run("different team → they engage", func(t *testing.T) {
		aHP, bHP := run(t, false)
		if aHP == 500 && bHP == 500 {
			t.Error("different-team units should have fought (expected HP loss on at least one)")
		}
	})
}

// Flipping a previously-allied player to a different team makes their units
// mutually hostile — the "PvP is just data" guarantee, at the combat layer.
func TestTeam_FlipToHostileEnablesCombat(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	a := teamCombatUnit(t, s, "p1", 400, 400)
	b := teamCombatUnit(t, s, "p2", 460, 400)
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 0) // start allied
	aID, bID := a.ID, b.ID
	s.mu.Unlock()

	tickN(s, 30)
	s.mu.RLock()
	if s.unitsByID[aID].HP != 500 || s.unitsByID[bID].HP != 500 {
		s.mu.RUnlock()
		t.Fatal("precondition: allied units should not have fought")
	}
	s.mu.RUnlock()

	// Flip p2 to a different team — pure data change, no other wiring.
	s.mu.Lock()
	setTeam(s, "p2", 7)
	s.mu.Unlock()

	tickN(s, 60)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[aID].HP == 500 && s.unitsByID[bID].HP == 500 {
		t.Error("after flipping teams, the now-hostile units should have engaged")
	}
}
