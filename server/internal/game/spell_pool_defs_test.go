package game

import "testing"

// A well-formed pool referencing registered abilities loads and is queryable.
func TestSpellPools_ValidLoads(t *testing.T) {
	// "heal" and "arcane_bolt" are registered catalog abilities; use them as
	// stand-in pool members so this test is independent of the arch_mage
	// content (which is populated later).
	data := []byte(`{"arch_mage":{"bronze":["arcane_bolt","heal"],"silver":[]}}`)
	pools, err := loadSpellPools(data)
	if err != nil {
		t.Fatalf("loadSpellPools: %v", err)
	}
	bronze := pools["arch_mage"]["bronze"]
	if len(bronze) != 2 || bronze[0] != "arcane_bolt" || bronze[1] != "heal" {
		t.Errorf("bronze = %v; want [arcane_bolt heal] (order preserved)", bronze)
	}
	if got := pools["arch_mage"]["silver"]; len(got) != 0 {
		t.Errorf("silver = %v; want empty", got)
	}
}

// Unknown ability id is rejected with the offender named.
func TestSpellPools_UnknownIDRejected(t *testing.T) {
	_, err := loadSpellPools([]byte(`{"arch_mage":{"bronze":["not_a_real_spell"]}}`))
	if err == nil {
		t.Fatal("expected error for unknown ability id")
	}
}

// Unknown rank key is rejected.
func TestSpellPools_UnknownRankRejected(t *testing.T) {
	_, err := loadSpellPools([]byte(`{"arch_mage":{"platinum":["heal"]}}`))
	if err == nil {
		t.Fatal("expected error for unknown rank key")
	}
}

// A missing archetype/rank cell resolves to an empty pool via spellPoolFor
// (exercised against the real embedded catalog).
func TestSpellPools_MissingCellEmpty(t *testing.T) {
	if got := spellPoolFor("no_such_archetype", "bronze"); got != nil {
		t.Errorf("missing archetype = %v; want nil", got)
	}
	if got := spellPoolFor("arch_mage", "gold"); got != nil {
		t.Errorf("missing rank cell = %v; want nil", got)
	}
}

// Empty spell id inside a pool is rejected.
func TestSpellPools_EmptyIDRejected(t *testing.T) {
	if _, err := loadSpellPools([]byte(`{"arch_mage":{"bronze":[""]}}`)); err == nil {
		t.Fatal("expected error for empty spell id")
	}
}
