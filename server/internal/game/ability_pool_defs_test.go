package game

import "testing"

// A missing archetype resolves to a nil pool via abilityPoolFor. Gold resolves
// to an empty pool for arch_mage: it is the perk tier and grants no pool
// ability (only bronze/silver author a pool on arch_mage.json).
func TestAbilityPools_MissingCellEmpty(t *testing.T) {
	if got := abilityPoolFor("no_such_archetype", "bronze"); got != nil {
		t.Errorf("missing archetype = %v; want nil", got)
	}
	if got := abilityPoolFor("arch_mage", "gold"); len(got) != 0 {
		t.Errorf("gold pool = %v; want empty (Gold is the perk tier, grants no ability)", got)
	}
}
