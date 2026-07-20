package game

// TestAbilityModifiers covers abilityScalarModifiersForCasterLocked, the
// generic aggregator that composes every owned perk's AbilityModifiers
// entries (targeting a given ability id) multiplicatively. This replaced the
// bespoke siphon-only aggregator that used to live in perks_siphoner.go; the
// siphon_life channel read-points (ability_channel.go) now call this helper
// directly, with soul_leech / beam_mastery's scalars authored as data on the
// perks themselves.
//
// Synthetic perks are injected via the same overlay mechanism the perk
// persistence tests use (withIsolatedPerkCatalogDir + SavePerkDef), so
// perkDefByID resolves real *PerkDef values built by the normal save/rebuild
// path rather than a mock.

import (
	"math"
	"testing"
)

func TestAbilityModifiers(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	perkA := &PerkDef{
		ID:          "test_ability_mod_perk_a",
		DisplayName: "Test Ability Mod Perk A",
		AbilityModifiers: []AbilityModifier{
			{Target: "siphon_life", DamageMult: 2.0, HealMult: 2.0},
		},
	}
	if err := SavePerkDef(perkA); err != nil {
		t.Fatalf("SavePerkDef(perkA): %v", err)
	}

	perkB := &PerkDef{
		ID:          "test_ability_mod_perk_b",
		DisplayName: "Test Ability Mod Perk B",
		AbilityModifiers: []AbilityModifier{
			{Target: "siphon_life", DamageMult: 1.5, ManaCostMult: 0.8},
		},
	}
	if err := SavePerkDef(perkB); err != nil {
		t.Fatalf("SavePerkDef(perkB): %v", err)
	}

	// perkC targets a different ability entirely; it must be ignored when
	// querying "siphon_life".
	perkC := &PerkDef{
		ID:          "test_ability_mod_perk_c",
		DisplayName: "Test Ability Mod Perk C",
		AbilityModifiers: []AbilityModifier{
			{Target: "other_ability", DamageMult: 5.0},
		},
	}
	if err := SavePerkDef(perkC); err != nil {
		t.Fatalf("SavePerkDef(perkC): %v", err)
	}

	// perkD carries an "unset" (zero) damageMult entry — must not zero the
	// composed result, matching the ">0 treated as unset" convention.
	perkD := &PerkDef{
		ID:          "test_ability_mod_perk_d",
		DisplayName: "Test Ability Mod Perk D",
		AbilityModifiers: []AbilityModifier{
			{Target: "siphon_life", DamageMult: 0},
		},
	}
	if err := SavePerkDef(perkD); err != nil {
		t.Fatalf("SavePerkDef(perkD): %v", err)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x1)
	s.mu.Lock()
	defer s.mu.Unlock()

	const eps = 1e-9

	// Nil caster → identity.
	if got := s.abilityScalarModifiersForCasterLocked(nil, "siphon_life"); got != identityAbilityModifierSet() {
		t.Fatalf("nil caster: got %+v, want identity", got)
	}

	// Empty-perk caster → identity.
	emptyCaster := &Unit{}
	if got := s.abilityScalarModifiersForCasterLocked(emptyCaster, "siphon_life"); got != identityAbilityModifierSet() {
		t.Fatalf("empty caster: got %+v, want identity", got)
	}

	// Caster owning perkA + perkB → composed damage/heal/mana, range untouched.
	caster := &Unit{PerkIDs: []string{"test_ability_mod_perk_a", "test_ability_mod_perk_b"}}
	got := s.abilityScalarModifiersForCasterLocked(caster, "siphon_life")
	if math.Abs(got.DamageMult-3.0) > eps {
		t.Errorf("DamageMult = %v, want 3.0", got.DamageMult)
	}
	if math.Abs(got.HealMult-2.0) > eps {
		t.Errorf("HealMult = %v, want 2.0", got.HealMult)
	}
	if math.Abs(got.ManaCostMult-0.8) > eps {
		t.Errorf("ManaCostMult = %v, want 0.8", got.ManaCostMult)
	}
	if math.Abs(got.RangeMult-1.0) > eps {
		t.Errorf("RangeMult = %v, want 1.0", got.RangeMult)
	}

	// perkC targets a different ability → ignored for siphon_life.
	casterWithC := &Unit{PerkIDs: []string{"test_ability_mod_perk_a", "test_ability_mod_perk_c"}}
	got = s.abilityScalarModifiersForCasterLocked(casterWithC, "siphon_life")
	if math.Abs(got.DamageMult-2.0) > eps {
		t.Errorf("with unrelated perkC: DamageMult = %v, want 2.0 (perkC's 5x on other_ability must not apply)", got.DamageMult)
	}
	// And querying "other_ability" directly picks it up.
	got = s.abilityScalarModifiersForCasterLocked(casterWithC, "other_ability")
	if math.Abs(got.DamageMult-5.0) > eps {
		t.Errorf("other_ability query: DamageMult = %v, want 5.0", got.DamageMult)
	}

	// perkD's zero damageMult is "unset" — must not zero the result.
	casterWithD := &Unit{PerkIDs: []string{"test_ability_mod_perk_a", "test_ability_mod_perk_d"}}
	got = s.abilityScalarModifiersForCasterLocked(casterWithD, "siphon_life")
	if math.Abs(got.DamageMult-2.0) > eps {
		t.Errorf("with perkD's zero damageMult: DamageMult = %v, want 2.0 (unset entry must not zero it)", got.DamageMult)
	}
}
