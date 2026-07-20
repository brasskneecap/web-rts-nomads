package game

import (
	"math"
	"testing"
)

// TestUnitPerkStatModifiersLocked covers unitPerkStatModifiersLocked, the
// generic aggregator that composes every owned perk's StatModifiers entries
// (for a given stat) into per-stage (add, mul) pools. Synthetic perks are
// injected via the same overlay mechanism as TestAbilityModifiers
// (withIsolatedPerkCatalogDir + SavePerkDef), so perkDefByID resolves real
// *PerkDef values built by the normal save/rebuild path.
func TestUnitPerkStatModifiersLocked(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	// perkA: +5 base moveSpeed add, ×2 base moveSpeed multiply.
	perkA := &PerkDef{
		ID:          "test_stat_mod_perk_a",
		DisplayName: "Test Stat Mod Perk A",
		StatModifiers: []PerkStatModifier{
			{Stat: statMoveSpeed, Op: statOpAdd, Value: 5},
			{Stat: statMoveSpeed, Op: statOpMultiply, Value: 2},
		},
	}
	if err := SavePerkDef(perkA); err != nil {
		t.Fatalf("SavePerkDef(perkA): %v", err)
	}

	// perkB: +3 base moveSpeed add (composes with perkA's base pool), and a
	// final-stage ×2 that must land in a SEPARATE stage bucket.
	perkB := &PerkDef{
		ID:          "test_stat_mod_perk_b",
		DisplayName: "Test Stat Mod Perk B",
		StatModifiers: []PerkStatModifier{
			{Stat: statMoveSpeed, Op: statOpAdd, Value: 3},
			{Stat: statMoveSpeed, Op: statOpMultiply, Value: 2, Stage: statStageFinal},
		},
	}
	if err := SavePerkDef(perkB); err != nil {
		t.Fatalf("SavePerkDef(perkB): %v", err)
	}

	// perkC modifies a DIFFERENT stat entirely — must be ignored when
	// querying moveSpeed.
	perkC := &PerkDef{
		ID:          "test_stat_mod_perk_c",
		DisplayName: "Test Stat Mod Perk C",
		StatModifiers: []PerkStatModifier{
			{Stat: statArmor, Op: statOpAdd, Value: 100},
		},
	}
	if err := SavePerkDef(perkC); err != nil {
		t.Fatalf("SavePerkDef(perkC): %v", err)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x1)
	s.mu.Lock()
	defer s.mu.Unlock()

	const eps = 1e-9

	// Nil unit -> nil (identity).
	if got := s.unitPerkStatModifiersLocked(nil, statMoveSpeed); got != nil {
		t.Fatalf("nil unit: got %+v, want nil", got)
	}

	// Unit with no perks -> nil (identity).
	emptyUnit := &Unit{}
	if got := s.unitPerkStatModifiersLocked(emptyUnit, statMoveSpeed); got != nil {
		t.Fatalf("empty unit: got %+v, want nil", got)
	}

	// Unknown stat -> nil (identity), even for a unit that owns matching perks.
	unit := &Unit{PerkIDs: []string{"test_stat_mod_perk_a"}}
	if got := s.unitPerkStatModifiersLocked(unit, "not_a_real_stat"); got != nil {
		t.Fatalf("unknown stat: got %+v, want nil", got)
	}

	// perkA alone: base stage {Add:5, Mul:2}.
	got := s.unitPerkStatModifiersLocked(unit, statMoveSpeed)
	base, ok := got[statStageBase]
	if !ok {
		t.Fatalf("perkA alone: want base stage present, got %+v", got)
	}
	if math.Abs(base.Add-5) > eps || math.Abs(base.Mul-2) > eps {
		t.Fatalf("perkA alone: base = %+v, want {Add:5 Mul:2}", base)
	}
	if _, ok := got[statStageFinal]; ok {
		t.Fatalf("perkA alone: want no final stage, got %+v", got[statStageFinal])
	}

	// perkA + perkB: base adds sum (5+3=8), base muls compose (perkA's ×2,
	// perkB contributes no base multiply so stays ×2); final stage carries
	// perkB's ×2 in its own bucket, independent of base.
	casterAB := &Unit{PerkIDs: []string{"test_stat_mod_perk_a", "test_stat_mod_perk_b"}}
	got = s.unitPerkStatModifiersLocked(casterAB, statMoveSpeed)
	base = got[statStageBase]
	if math.Abs(base.Add-8) > eps || math.Abs(base.Mul-2) > eps {
		t.Fatalf("perkA+perkB: base = %+v, want {Add:8 Mul:2}", base)
	}
	final, ok := got[statStageFinal]
	if !ok {
		t.Fatalf("perkA+perkB: want final stage present, got %+v", got)
	}
	if math.Abs(final.Add-0) > eps || math.Abs(final.Mul-2) > eps {
		t.Fatalf("perkA+perkB: final = %+v, want {Add:0 Mul:2}", final)
	}

	// Order of unit.PerkIDs must not affect the result — aggregation sorts
	// by perk id internally (determinism rule).
	casterBA := &Unit{PerkIDs: []string{"test_stat_mod_perk_b", "test_stat_mod_perk_a"}}
	gotReversed := s.unitPerkStatModifiersLocked(casterBA, statMoveSpeed)
	if gotReversed[statStageBase] != got[statStageBase] || gotReversed[statStageFinal] != got[statStageFinal] {
		t.Fatalf("PerkIDs order changed result: got %+v, want %+v", gotReversed, got)
	}

	// perkC targets a different stat -> ignored when querying moveSpeed.
	casterWithC := &Unit{PerkIDs: []string{"test_stat_mod_perk_a", "test_stat_mod_perk_c"}}
	gotMove := s.unitPerkStatModifiersLocked(casterWithC, statMoveSpeed)
	if gotMove[statStageBase] != (statStageAccum{Add: 5, Mul: 2}) {
		t.Fatalf("with unrelated perkC, moveSpeed base = %+v, want {Add:5 Mul:2} (perkC's armor mod must not leak in)", gotMove[statStageBase])
	}
	// And querying armor directly picks perkC up, unaffected by perkA.
	gotArmor := s.unitPerkStatModifiersLocked(casterWithC, statArmor)
	if gotArmor[statStageBase] != (statStageAccum{Add: 100, Mul: 1}) {
		t.Fatalf("armor query: got %+v, want {Add:100 Mul:1}", gotArmor[statStageBase])
	}
}
