package game

import "testing"

func TestUpgradeCatalog_LoadsWithoutPanic(t *testing.T) {
	if len(upgradeDefsByID) == 0 {
		t.Fatal("upgrade catalog is empty")
	}
}

func TestUpgradeCatalog_GetKnownID(t *testing.T) {
	def, ok := getUpgradeDef("swift_strikes_common")
	if !ok {
		t.Fatal("expected swift_strikes_common to exist")
	}
	if def.Group != "swift_strikes" {
		t.Errorf("group: got %q, want %q", def.Group, "swift_strikes")
	}
	if def.MaxStacks != 3 {
		t.Errorf("maxStacks: got %d, want 3", def.MaxStacks)
	}
}

func TestUpgradeCatalog_RarityOrder(t *testing.T) {
	if _, ok := upgradeRarityOrder["legendary"]; !ok {
		t.Fatal("legendary missing from rarity order map")
	}
	if upgradeRarityOrder["legendary"] <= upgradeRarityOrder["epic"] {
		t.Error("legendary must rank higher than epic")
	}
}

func TestUpgradeCatalog_GetUnknownID(t *testing.T) {
	_, ok := getUpgradeDef("does_not_exist")
	if ok {
		t.Fatal("expected ok=false for unknown upgrade id")
	}
}

func TestUpgradeCatalog_ListDefsCountAndOrder(t *testing.T) {
	defs := listUpgradeDefs()
	if len(defs) != 6 {
		t.Fatalf("expected 6 upgrade defs, got %d", len(defs))
	}
	for i := 1; i < len(defs); i++ {
		if defs[i].ID <= defs[i-1].ID {
			t.Errorf("listUpgradeDefs not sorted: %q >= %q at index %d", defs[i].ID, defs[i-1].ID, i)
		}
	}
}

func TestUpgradeCatalog_FortifyCommonEffect(t *testing.T) {
	def, ok := getUpgradeDef("fortify_common")
	if !ok {
		t.Fatal("expected fortify_common to exist")
	}
	if def.Effect.Stat != "hp" {
		t.Errorf("effect.stat: got %q, want %q", def.Effect.Stat, "hp")
	}
	if def.Effect.Multiplier != 1.12 {
		t.Errorf("effect.multiplier: got %v, want 1.12", def.Effect.Multiplier)
	}
}
