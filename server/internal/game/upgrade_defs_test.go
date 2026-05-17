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
	// maxStacks is a tunable catalog value; a capped (non-unlimited)
	// upgrade must allow at least one stack.
	if !def.Unlimited && def.MaxStacks < 1 {
		t.Errorf("maxStacks: got %d, want >= 1 for a capped upgrade", def.MaxStacks)
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
	// The number of upgrades is catalog-driven (the user adds/removes
	// JSON files freely); assert non-empty and sorted, not an exact count.
	if len(defs) == 0 {
		t.Fatal("expected at least one upgrade def, got 0")
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
	if def.Effect.Stat != upgradeEffectStatHP {
		t.Errorf("effect.stat: got %q, want %q", def.Effect.Stat, upgradeEffectStatHP)
	}
	// The exact multiplier is a tunable balance value; fortify is a
	// positive HP buff, so assert the invariant (strictly increases HP).
	if def.Effect.Multiplier <= 1.0 {
		t.Errorf("effect.multiplier: got %v, want > 1.0 (fortify must increase HP)", def.Effect.Multiplier)
	}
}
