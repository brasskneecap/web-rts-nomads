package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// activeDebuffIconsLocked tests
// ─────────────────────────────────────────────────────────────────────────────

// newDebuffIconState returns a minimal GameState with a single unit owned by
// "p1". The lock is held on return; the caller must defer s.mu.Unlock().
func newDebuffIconState(t *testing.T) (s *GameState, unit *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	unit = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	return s, unit
}

// TestActiveDebuffIcons_None verifies that a clean unit returns nil (not an
// empty slice) so omitempty suppresses the field from the JSON snapshot.
func TestActiveDebuffIcons_None(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	got := s.activeDebuffIconsLocked(unit)
	if got != nil {
		t.Errorf("expected nil for clean unit, got %v", got)
	}
}

// TestActiveDebuffIcons_Taunted verifies that a unit with an active taunt
// produces the "debuff-taunted" icon.
func TestActiveDebuffIcons_Taunted(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	unit.TauntedByUnitID = 99
	unit.TauntRemaining = 2.0

	got := s.activeDebuffIconsLocked(unit)
	if len(got) != 1 || got[0] != "debuff-taunted" {
		t.Errorf("expected [debuff-taunted], got %v", got)
	}
}

// TestActiveDebuffIcons_Taunted_ZeroRemaining verifies that a taunt with
// TauntRemaining == 0 does NOT produce the icon (condition: both fields must
// be non-zero).
func TestActiveDebuffIcons_Taunted_ZeroRemaining(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	unit.TauntedByUnitID = 99
	unit.TauntRemaining = 0

	got := s.activeDebuffIconsLocked(unit)
	if got != nil {
		t.Errorf("expected nil when TauntRemaining==0, got %v", got)
	}
}

// TestActiveDebuffIcons_Weakened verifies that WeakenedRemaining > 0 produces
// the "debuff-weakened" icon.
func TestActiveDebuffIcons_Weakened(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	unit.PerkState.WeakenedRemaining = 3.0

	got := s.activeDebuffIconsLocked(unit)
	if len(got) != 1 || got[0] != "debuff-weakened" {
		t.Errorf("expected [debuff-weakened], got %v", got)
	}
}

// TestActiveDebuffIcons_Marked verifies that MarkedRemaining > 0 produces
// the "debuff-marked" icon.
func TestActiveDebuffIcons_Marked(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	unit.PerkState.MarkedRemaining = 4.0

	got := s.activeDebuffIconsLocked(unit)
	if len(got) != 1 || got[0] != "debuff-marked" {
		t.Errorf("expected [debuff-marked], got %v", got)
	}
}

// TestActiveDebuffIcons_Multiple verifies that all three active debuffs produce
// all three icons in stable (taunted, weakened, marked) order, regardless of
// which fields were set first.
func TestActiveDebuffIcons_Multiple(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	// Set in reverse declaration order to confirm output order is fixed.
	unit.PerkState.MarkedRemaining = 1.0
	unit.PerkState.WeakenedRemaining = 1.0
	unit.TauntedByUnitID = 99
	unit.TauntRemaining = 1.0

	got := s.activeDebuffIconsLocked(unit)

	want := []string{"debuff-taunted", "debuff-weakened", "debuff-marked"}
	if len(got) != len(want) {
		t.Fatalf("expected %d icons, got %d: %v", len(want), len(got), got)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("position %d: expected %q, got %q", i, id, got[i])
		}
	}
}

// TestActiveDebuffIcons_NilUnit verifies the nil guard — calling with nil does
// not panic and returns nil.
func TestActiveDebuffIcons_NilUnit(t *testing.T) {
	s, _ := newDebuffIconState(t)
	defer s.mu.Unlock()

	got := s.activeDebuffIconsLocked(nil)
	if got != nil {
		t.Errorf("expected nil for nil unit, got %v", got)
	}
}
