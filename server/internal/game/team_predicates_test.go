package game

import "testing"

// Truth table for the team/alliance chokepoint predicates. Constructs
// players directly (no map structure needed) so it is robust regardless of
// which map DefaultMapID() resolves to.
func TestTeamPredicates_TruthTable(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	defer s.mu.Unlock()

	// p1,p2 on team 0 (default); p3 on team 1; pNoEntry has no Player record.
	s.Players["p1"] = &Player{ID: "p1", TeamID: 0}
	s.Players["p2"] = &Player{ID: "p2", TeamID: 0}
	s.Players["p3"] = &Player{ID: "p3", TeamID: 1}
	const e = enemyPlayerID

	// playerTeamLocked: known players resolve; absent ⇒ default team 0.
	if s.playerTeamLocked("p3") != 1 || s.playerTeamLocked("p1") != 0 {
		t.Fatal("playerTeamLocked wrong for known players")
	}
	if s.playerTeamLocked("pNoEntry") != 0 {
		t.Error("absent player must resolve to default team 0")
	}

	hostile := []struct{ a, b string }{
		{"p1", "p3"}, // different team ⇒ hostile
		{"p3", "p1"},
		{"p1", e}, // player vs PvE AI ⇒ hostile
		{e, "p1"},
	}
	for _, c := range hostile {
		if !s.playersAreHostileLocked(c.a, c.b) {
			t.Errorf("playersAreHostileLocked(%q,%q) = false; want true", c.a, c.b)
		}
	}

	notHostile := []struct{ a, b string }{
		{"p1", "p1"}, // same owner ⇒ never hostile
		{"p1", "p2"}, // same team (0) ⇒ not hostile  (== current default behavior)
		{e, e},       // enemy vs enemy ⇒ same owner ⇒ not hostile
	}
	for _, c := range notHostile {
		if s.playersAreHostileLocked(c.a, c.b) {
			t.Errorf("playersAreHostileLocked(%q,%q) = true; want false", c.a, c.b)
		}
	}

	friendly := []struct{ a, b string }{
		{"p1", "p1"}, // self
		{"p1", "p2"}, // same team
		{"p2", "p1"},
	}
	for _, c := range friendly {
		if !s.playersAreFriendlyLocked(c.a, c.b) {
			t.Errorf("playersAreFriendlyLocked(%q,%q) = false; want true", c.a, c.b)
		}
	}

	notFriendly := []struct{ a, b string }{
		{"p1", "p3"}, // different team
		{"p1", e},    // PvE AI is never an ally...
		{e, e},       // ...not even to itself (friendly != !hostile here)
	}
	for _, c := range notFriendly {
		if s.playersAreFriendlyLocked(c.a, c.b) {
			t.Errorf("playersAreFriendlyLocked(%q,%q) = true; want false", c.a, c.b)
		}
	}

	// friendly is NOT the negation of hostile for the PvE AI: enemy-vs-enemy
	// is neither hostile nor friendly.
	if s.playersAreHostileLocked(e, e) || s.playersAreFriendlyLocked(e, e) {
		t.Error("enemy vs enemy must be neither hostile nor friendly")
	}

	// Unit-level forms.
	mk := func(id int, owner string) *Unit { return &Unit{ID: id, OwnerID: owner} }
	u1 := mk(1, "p1")
	u1b := mk(2, "p1") // same team, different unit
	u3 := mk(3, "p3")  // other team
	uE := mk(4, e)

	if !s.unitsHostileLocked(u1, u3) || s.unitsHostileLocked(u1, u1b) || s.unitsHostileLocked(u1, u1) {
		t.Error("unitsHostileLocked: want hostile cross-team, not same-team, not self")
	}
	if !s.unitsHostileLocked(u1, uE) {
		t.Error("unitsHostileLocked: player vs PvE AI must be hostile")
	}
	if !s.unitsFriendlyLocked(u1, u1b) || !s.unitsFriendlyLocked(u1, u1) {
		t.Error("unitsFriendlyLocked: same-team and self must be friendly")
	}
	if s.unitsFriendlyLocked(u1, u3) || s.unitsFriendlyLocked(u1, uE) {
		t.Error("unitsFriendlyLocked: cross-team and PvE AI must not be friendly")
	}
	// nil-safety
	if s.unitsHostileLocked(nil, u1) || s.unitsFriendlyLocked(u1, nil) {
		t.Error("unit predicates must be nil-safe (false)")
	}
}

// Default-equivalence: with everyone on team 0, hostility is exactly the
// pre-team rule (only the __enemy__ AI is hostile; all real players allied).
func TestTeamPredicates_DefaultEquivalence(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["a"] = &Player{ID: "a", TeamID: 0}
	s.Players["b"] = &Player{ID: "b", TeamID: 0}

	// Mirror the old free-function logic and assert identical results.
	old := func(a, b string) bool {
		if a == b {
			return false
		}
		return a == enemyPlayerID || b == enemyPlayerID
	}
	for _, pair := range [][2]string{
		{"a", "b"}, {"a", "a"}, {"a", enemyPlayerID}, {enemyPlayerID, "b"},
		{enemyPlayerID, enemyPlayerID}, {"a", "unknownPlayer"},
	} {
		if got, want := s.playersAreHostileLocked(pair[0], pair[1]), old(pair[0], pair[1]); got != want {
			t.Errorf("default-equivalence broken for (%q,%q): got %v want %v", pair[0], pair[1], got, want)
		}
	}
}
