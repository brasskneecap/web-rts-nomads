package game

import "testing"

// TestPlayerSlotColorsAreFixedAndDeterministic verifies that player colors are
// assigned by slot (not randomized): player1->blue, player2->orange, and that
// the assignment is identical regardless of the match seed. The health-bar
// color is how a player tells their units apart from allies/enemies, so it must
// be stable and must never collide with the enemy or neutral colors.
func TestPlayerSlotColorsAreFixedAndDeterministic(t *testing.T) {
	joinTwo := func(seed int64) (aliceColor, bobColor string) {
		s := NewGameStateWithSeed(GetMapConfigByID("forest-1"), seed)
		s.EnsurePlayer("alice")
		s.EnsurePlayer("bob")
		return s.Players["alice"].Color, s.Players["bob"].Color
	}

	a1, b1 := joinTwo(1)
	a2, b2 := joinTwo(9999)

	// Seed independence: no randomness left.
	if a1 != a2 || b1 != b2 {
		t.Fatalf("colors depend on seed: seed1=(%s,%s) seed2=(%s,%s)", a1, b1, a2, b2)
	}

	// First joiner claims the player1 slot -> blue; second claims player2 -> orange.
	if a1 != playerSlotColors[0] {
		t.Errorf("first joiner color = %s; want player1 %s (blue)", a1, playerSlotColors[0])
	}
	if b1 != playerSlotColors[1] {
		t.Errorf("second joiner color = %s; want player2 %s (orange)", b1, playerSlotColors[1])
	}

	// Never collide with the reserved enemy/neutral health-bar colors.
	for name, c := range map[string]string{"alice": a1, "bob": b1} {
		if c == enemyPlayerColor || c == neutralPlayerColor {
			t.Errorf("%s color %s collides with a reserved (enemy/neutral) color", name, c)
		}
	}
}

// TestSlotColorSpec pins the requested slot->color mapping for players 1-4.
func TestSlotColorSpec(t *testing.T) {
	want := []struct {
		label string
		color string
	}{
		{"player1", "#3498db"}, // blue
		{"player2", "#e67e22"}, // orange
		{"player3", "#8e44ad"}, // purple
		{"player4", "#f1c40f"}, // yellow
	}
	for _, tc := range want {
		idx, ok := playerLabelIndex(tc.label)
		if !ok {
			t.Fatalf("playerLabelIndex(%q) failed to parse", tc.label)
		}
		if got := playerSlotColors[idx]; got != tc.color {
			t.Errorf("%s -> %s; want %s", tc.label, got, tc.color)
		}
	}

	// Player-3 purple must differ from the neutral purple so units stay
	// distinguishable — this is the whole reason the palette was fixed.
	if playerSlotColors[2] == neutralPlayerColor {
		t.Errorf("player3 color %s equals neutral color %s", playerSlotColors[2], neutralPlayerColor)
	}
}
