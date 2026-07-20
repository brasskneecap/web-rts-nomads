package game

import (
	"sort"
	"testing"
)

// wantArchMagePool is the effective candidate set abilityPoolFor returns per
// rank, pinned across the ability-pool-migration (ability pools moved from a
// standalone catalog file onto arch_mage.json's abilityPoolsByRank — this
// test guards that the resulting candidate sets stayed identical).
var wantArchMagePool = map[string][]string{
	"bronze": {"arcane_orb", "chain_lightning", "fireball", "meteor", "shatter"},
	"silver": {"arcane_orb", "chain_lightning", "fireball", "meteor", "shatter"},
	"gold":   {}, // gold grants no pool ability
}

func TestArchMageAbilityPoolCandidates(t *testing.T) {
	for _, rank := range []string{"bronze", "silver", "gold"} {
		got := append([]string(nil), abilityPoolFor("arch_mage", rank)...)
		sort.Strings(got)
		want := wantArchMagePool[rank]
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", rank, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", rank, got, want)
				break
			}
		}
	}
}
