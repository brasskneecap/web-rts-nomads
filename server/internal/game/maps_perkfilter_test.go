package game

import "testing"

func TestFilterKnownPerkIDsForUnit(t *testing.T) {
	// A real acolyte-associated perk (siphoner path) kept for an acolyte,
	// dropped for a soldier.
	got := filterKnownPerkIDsForUnit("acolyte", []string{"lingering_hex"})
	if len(got) != 1 || got[0] != "lingering_hex" {
		t.Errorf("acolyte should keep lingering_hex, got %v", got)
	}
	got = filterKnownPerkIDsForUnit("soldier", []string{"lingering_hex"})
	if len(got) != 0 {
		t.Errorf("soldier should drop acolyte perk lingering_hex, got %v", got)
	}
	// Unknown id is dropped.
	got = filterKnownPerkIDsForUnit("acolyte", []string{"no_such_perk"})
	if len(got) != 0 {
		t.Errorf("unknown perk should be dropped, got %v", got)
	}
}
