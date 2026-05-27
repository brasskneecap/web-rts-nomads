package game

import "testing"

// TestNeutralGroupLoader_LoadsTier1 pins the structural invariant: the
// shipped tier_1.json loads, has at least one group, and the group has
// non-empty composition. Deliberately does NOT pin specific group ids or
// counts — those are balance content and live in JSON.
func TestNeutralGroupLoader_LoadsTier1(t *testing.T) {
	tier, ok := neutralGroupsByTier[1]
	if !ok {
		t.Fatalf("tier 1 catalog missing — expected tier_1.json to load at startup")
	}
	if len(tier.Groups) == 0 {
		t.Fatalf("tier 1 has zero groups — at least one required")
	}
	for _, g := range tier.Groups {
		if g.ID == "" {
			t.Errorf("tier 1 group has empty id: %+v", g)
		}
		if g.Name == "" {
			t.Errorf("tier 1 group %q has empty display name", g.ID)
		}
		if len(g.Composition) == 0 {
			t.Errorf("tier 1 group %q has empty composition", g.ID)
		}
		for _, c := range g.Composition {
			if c.Count < 1 {
				t.Errorf("tier 1 group %q composition entry %q has count %d (must be >= 1)", g.ID, c.UnitType, c.Count)
			}
			if _, ok := getUnitDef(c.UnitType); !ok {
				t.Errorf("tier 1 group %q references unknown unitType %q", g.ID, c.UnitType)
			}
		}
	}
}

// TestNeutralGroupLoader_TierFallback covers the spec's tier-fallback rule:
// requesting tier K when tier_K.json is missing resolves to the largest
// tier file <= K. With only tier 1 shipped today, every K >= 1 resolves to 1.
func TestNeutralGroupLoader_TierFallback(t *testing.T) {
	cases := []struct {
		requested int
		want      int
	}{
		{1, 1},
		{2, 1},
		{5, 1},
		{100, 1},
	}
	for _, tc := range cases {
		got := resolveNeutralTier(tc.requested)
		if got != tc.want {
			t.Errorf("resolveNeutralTier(%d): got %d, want %d", tc.requested, got, tc.want)
		}
	}
}

// TestNeutralGroupLoader_TierZeroSentinel: requesting tier <= 0 returns the
// sentinel 0, which spawnGroupForCampLocked treats as "no tier available,
// skip respawn." (Distinct from "found a fallback.")
func TestNeutralGroupLoader_TierZeroSentinel(t *testing.T) {
	if got := resolveNeutralTier(0); got != 0 {
		t.Errorf("resolveNeutralTier(0): got %d, want 0", got)
	}
	if got := resolveNeutralTier(-3); got != 0 {
		t.Errorf("resolveNeutralTier(-3): got %d, want 0", got)
	}
}

// TestNeutralGroupLoader_GetSpecific verifies getNeutralGroup returns the
// shipped group by id, and returns (zero, false) for unknown ids.
func TestNeutralGroupLoader_GetSpecific(t *testing.T) {
	g, ok := getNeutralGroup(1, "small_raider_group")
	if !ok {
		t.Fatalf("getNeutralGroup(1, small_raider_group): not found")
	}
	if g.ID != "small_raider_group" {
		t.Errorf("got id %q, want small_raider_group", g.ID)
	}
	if _, ok := getNeutralGroup(1, "nonexistent_group"); ok {
		t.Errorf("getNeutralGroup with unknown id should return false")
	}
	if _, ok := getNeutralGroup(99, "small_raider_group"); ok {
		t.Errorf("getNeutralGroup at unloaded tier should return false")
	}
}
