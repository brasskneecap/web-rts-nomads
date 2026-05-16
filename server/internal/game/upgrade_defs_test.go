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
