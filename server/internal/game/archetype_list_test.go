package game

import (
	"sort"
	"testing"
)

func TestListArchetypes_ReturnsEverySortedProfileKey(t *testing.T) {
	got := ListArchetypes()
	// Anchor: every assertion below is relative to combatProfiles, so an
	// emptied map would make them all pass vacuously. "soldier" is
	// resolveCombatProfile's hard-coded final fallback, so requiring its
	// presence is a genuine invariant, not a pinned content value.
	if len(got) == 0 {
		t.Fatal("ListArchetypes returned no entries")
	}
	hasSoldier := false
	for _, key := range got {
		if key == "soldier" {
			hasSoldier = true
			break
		}
	}
	if !hasSoldier {
		t.Fatalf("ListArchetypes must include %q (resolveCombatProfile's fallback), got %v", "soldier", got)
	}
	if len(got) != len(combatProfiles) {
		t.Fatalf("ListArchetypes returned %d entries, want %d (one per combat profile)", len(got), len(combatProfiles))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("ListArchetypes must be sorted for stable catalog output, got %v", got)
	}
	for _, key := range got {
		if _, ok := combatProfiles[key]; !ok {
			t.Fatalf("ListArchetypes returned %q, which is not a combat profile", key)
		}
	}
}
