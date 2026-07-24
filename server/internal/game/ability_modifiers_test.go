package game

// TestAbilityModifiers covers abilityScalarModifiersForCasterLocked, the
// generic aggregator that composes every owned perk's AbilityModifiers
// entries (targeting a given ability id) multiplicatively. This replaced the
// bespoke siphon-only aggregator that used to live in perks_siphoner.go.
//
// AbilityModifier now carries only the ABILITY-LEVEL scalars that cannot be
// reached by an abilityFields modifier: mana cost, cast range, and cooldown.
// (Damage and healing scalers moved to abilityFields on the owning action —
// see soul_leech / beam_mastery — so they are no longer aggregated here.)
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
			{Target: "siphon_life", ManaCostMult: 2.0, CooldownMult: 2.0},
		},
	}
	if err := SavePerkDef(perkA); err != nil {
		t.Fatalf("SavePerkDef(perkA): %v", err)
	}

	perkB := &PerkDef{
		ID:          "test_ability_mod_perk_b",
		DisplayName: "Test Ability Mod Perk B",
		AbilityModifiers: []AbilityModifier{
			{Target: "siphon_life", ManaCostMult: 1.5, RangeMult: 0.8},
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
			{Target: "other_ability", ManaCostMult: 5.0},
		},
	}
	if err := SavePerkDef(perkC); err != nil {
		t.Fatalf("SavePerkDef(perkC): %v", err)
	}

	// perkD carries an "unset" (zero) manaCostMult entry — must not zero the
	// composed result, matching the ">0 treated as unset" convention.
	perkD := &PerkDef{
		ID:          "test_ability_mod_perk_d",
		DisplayName: "Test Ability Mod Perk D",
		AbilityModifiers: []AbilityModifier{
			{Target: "siphon_life", ManaCostMult: 0},
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

	// Caster owning perkA + perkB → composed mana/cooldown/range.
	caster := &Unit{PerkIDs: []string{"test_ability_mod_perk_a", "test_ability_mod_perk_b"}}
	got := s.abilityScalarModifiersForCasterLocked(caster, "siphon_life")
	if math.Abs(got.ManaCostMult-3.0) > eps {
		t.Errorf("ManaCostMult = %v, want 3.0", got.ManaCostMult)
	}
	if math.Abs(got.CooldownMult-2.0) > eps {
		t.Errorf("CooldownMult = %v, want 2.0", got.CooldownMult)
	}
	if math.Abs(got.RangeMult-0.8) > eps {
		t.Errorf("RangeMult = %v, want 0.8", got.RangeMult)
	}

	// perkC targets a different ability → ignored for siphon_life.
	casterWithC := &Unit{PerkIDs: []string{"test_ability_mod_perk_a", "test_ability_mod_perk_c"}}
	got = s.abilityScalarModifiersForCasterLocked(casterWithC, "siphon_life")
	if math.Abs(got.ManaCostMult-2.0) > eps {
		t.Errorf("with unrelated perkC: ManaCostMult = %v, want 2.0 (perkC's 5x on other_ability must not apply)", got.ManaCostMult)
	}
	// And querying "other_ability" directly picks it up.
	got = s.abilityScalarModifiersForCasterLocked(casterWithC, "other_ability")
	if math.Abs(got.ManaCostMult-5.0) > eps {
		t.Errorf("other_ability query: ManaCostMult = %v, want 5.0", got.ManaCostMult)
	}

	// perkD's zero manaCostMult is "unset" — must not zero the result.
	casterWithD := &Unit{PerkIDs: []string{"test_ability_mod_perk_a", "test_ability_mod_perk_d"}}
	got = s.abilityScalarModifiersForCasterLocked(casterWithD, "siphon_life")
	if math.Abs(got.ManaCostMult-2.0) > eps {
		t.Errorf("with perkD's zero manaCostMult: ManaCostMult = %v, want 2.0 (unset entry must not zero it)", got.ManaCostMult)
	}
}
