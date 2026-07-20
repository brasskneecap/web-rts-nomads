package game

import (
	"strings"
	"testing"
)

// TestValidatePathFile_AbilityPoolsByRank_DedupWithBaseAbilities_ReturnsError
// covers the base ∩ pool half of the AbilityPoolsByRank dedup invariant: an
// ability id that appears in both the path's base Abilities override and in
// one rank's AbilityPoolsByRank list must be rejected, naming the offending
// id. "fireball" is a real registered AbilityDef (see
// catalog/abilities/fireball/fireball.json) so only the dedup rule can be
// triggering the rejection, not id-resolution.
func TestValidatePathFile_AbilityPoolsByRank_DedupWithBaseAbilities_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_ability_pool_dedup_base")
	abilities := []string{"fireball"}
	file.Abilities = &abilities
	file.AbilityPoolsByRank = map[string][]string{
		unitRankSilver: {"fireball"},
	}

	err := validatePathFile(file, "test_ability_pool_dedup_base")
	if err == nil {
		t.Fatal("validatePathFile(ability duplicated across base abilities and abilityPoolsByRank) = nil, want error")
	}
	if !strings.Contains(err.Error(), "fireball") {
		t.Errorf("error = %q, want it to name the duplicated ability id %q", err.Error(), "fireball")
	}
}

// TestValidatePathFile_AbilityPoolsByRank_SharedAcrossRanks_ReturnsNil covers
// the pool ∩ pool case: the same ability id authored in two different ranks'
// pools is a valid, designed configuration (e.g. bronze and silver sharing
// the same roll pool) and must validate without error. The runtime roll
// de-dupes the actual grant across ranks via unitKnownAbilitySetLocked
// (ability_pool_roll.go), so a unit never ends up with two copies of the same
// ability even though it's listed in more than one rank's pool.
func TestValidatePathFile_AbilityPoolsByRank_SharedAcrossRanks_ReturnsNil(t *testing.T) {
	file := validClericShapedPathFile("test_ability_pool_shared_ranks")
	file.AbilityPoolsByRank = map[string][]string{
		unitRankBronze: {"arcane_missiles"},
		unitRankSilver: {"arcane_missiles"},
	}

	if err := validatePathFile(file, "test_ability_pool_shared_ranks"); err != nil {
		t.Fatalf("validatePathFile(ability shared across two abilityPoolsByRank ranks) = %v, want nil", err)
	}
}

// TestValidatePathFile_AbilityPoolsByRank_DedupWithinRank_ReturnsError covers
// the within-rank half: the same ability id listed twice in ONE rank's pool
// must still be rejected, naming the id.
func TestValidatePathFile_AbilityPoolsByRank_DedupWithinRank_ReturnsError(t *testing.T) {
	file := validClericShapedPathFile("test_ability_pool_dedup_within_rank")
	file.AbilityPoolsByRank = map[string][]string{
		unitRankBronze: {"arcane_missiles", "arcane_missiles"},
	}

	err := validatePathFile(file, "test_ability_pool_dedup_within_rank")
	if err == nil {
		t.Fatal("validatePathFile(ability duplicated within a single abilityPoolsByRank rank) = nil, want error")
	}
	if !strings.Contains(err.Error(), "arcane_missiles") {
		t.Errorf("error = %q, want it to name the duplicated ability id %q", err.Error(), "arcane_missiles")
	}
}

// TestValidatePathFile_AbilityPoolsByRank_NoDuplicates_ReturnsNil is the
// clean-file counterpart: distinct ability ids across the base abilities and
// every rank's pool must validate without error.
func TestValidatePathFile_AbilityPoolsByRank_NoDuplicates_ReturnsNil(t *testing.T) {
	file := validClericShapedPathFile("test_ability_pool_no_dup")
	// validClericShapedPathFile already sets Abilities = ["greater_heal"].
	file.AbilityPoolsByRank = map[string][]string{
		unitRankBronze: {"fireball"},
		unitRankSilver: {"arcane_missiles"},
	}

	if err := validatePathFile(file, "test_ability_pool_no_dup"); err != nil {
		t.Fatalf("validatePathFile(distinct ability ids) = %v, want nil", err)
	}
}
