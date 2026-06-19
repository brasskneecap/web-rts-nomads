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
// shipped tier <= K. Derives expectations from the tiers actually shipped
// (neutralTiersSorted) rather than hardcoding a tier count, so adding a
// tier_<N>.json file doesn't break this test.
func TestNeutralGroupLoader_TierFallback(t *testing.T) {
	if len(neutralTiersSorted) == 0 {
		t.Skip("no neutral tier files shipped")
	}
	minTier := neutralTiersSorted[0]
	maxTier := neutralTiersSorted[len(neutralTiersSorted)-1]

	// Each shipped tier resolves to itself.
	for _, tier := range neutralTiersSorted {
		if got := resolveNeutralTier(tier); got != tier {
			t.Errorf("resolveNeutralTier(%d): got %d, want %d (exact shipped tier)", tier, got, tier)
		}
	}
	// Requesting well above the max resolves down to the largest shipped tier.
	if got := resolveNeutralTier(maxTier + 100); got != maxTier {
		t.Errorf("resolveNeutralTier(%d): got %d, want %d (largest shipped)", maxTier+100, got, maxTier)
	}
	// Requesting the minimum shipped tier resolves to itself, not below.
	if got := resolveNeutralTier(minTier); got != minTier {
		t.Errorf("resolveNeutralTier(%d): got %d, want %d", minTier, got, minTier)
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
