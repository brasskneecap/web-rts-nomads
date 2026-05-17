package game

import (
	"math"
	"testing"
)

func TestApplyUpgrade_StatMultiplierAffectsMatchingUnits(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	// Spawn a ranged unit owned by p1.
	unit := s.spawnPlayerUnitLocked("archer", "p1", "#fff", s.gridToWorldCenter(s.worldToGrid(200, 200)))
	if unit == nil {
		t.Skip("archer unit type not available in test map")
	}
	baseDmg := unit.BaseDamage

	// iron_warlord_common is archetype=melee; archer is not melee, so BaseDamage should not change.
	s.applyUpgradeLocked("p1", "iron_warlord_common", 0)
	if unit.BaseDamage != baseDmg {
		t.Errorf("archer BaseDamage should be unchanged by melee upgrade; was %d, got %d", baseDmg, unit.BaseDamage)
	}

	// Applying a fortify (army-wide HP) must affect the archer.
	fortifyDef, ok := getUpgradeDef("fortify_common")
	if !ok {
		t.Fatal("expected fortify_common to exist in catalog")
	}
	baseHP := unit.BaseMaxHP
	s.applyUpgradeLocked("p1", "fortify_common", 0)
	expectedHP := int(math.Round(float64(baseHP) * fortifyDef.Effect.Multiplier))
	if unit.BaseMaxHP != expectedHP {
		t.Errorf("fortify_common: BaseMaxHP want %d, got %d", expectedHP, unit.BaseMaxHP)
	}
}

func TestApplyUpgrade_IncrementsStackCounter(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	s.applyUpgradeLocked("p1", "fortify_common", 0)
	if got := s.Players["p1"].UpgradeState.UpgradeStacks["fortify"]; got != 1 {
		t.Errorf("stack counter: want 1, got %d", got)
	}
	s.applyUpgradeLocked("p1", "fortify_common", 0)
	if got := s.Players["p1"].UpgradeState.UpgradeStacks["fortify"]; got != 2 {
		t.Errorf("stack counter after 2nd apply: want 2, got %d", got)
	}
}

func TestApplyUpgrade_XPGrantReachesTargetUnit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", s.gridToWorldCenter(s.worldToGrid(200, 200)))
	if unit == nil {
		t.Skip("soldier unit type not available in test map")
	}
	startXP := unit.XP
	s.applyUpgradeLocked("p1", "battlefield_wisdom_common", unit.ID)
	if unit.XP <= startXP {
		t.Errorf("XP did not increase: before %d, after %d", startXP, unit.XP)
	}
}

// TestWaveStatBuffs_SpawnedUnitReceivesStackedMultipliers verifies that a unit
// spawned after multiple wave damage upgrades receives the full stacked
// multiplier, not just the most recent one.
func TestWaveStatBuffs_SpawnedUnitReceivesStackedMultipliers(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		Upgrades:     map[UpgradeTrack]int{},
		UpgradeState: newPlayerUpgradeState(1, 99),
	}

	// Spawn a soldier so we know its catalog BaseDamage.
	ref := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", s.gridToWorldCenter(s.worldToGrid(200, 200)))
	if ref == nil {
		t.Skip("soldier unit type not available in test map")
	}
	catalogBase := ref.BaseDamage

	// Simulate three wave damage upgrades (matching the user scenario: 18%, 18%, 10%).
	// iron_warlord_rare targets archetype=soldier (+18%); iron_warlord_common is +10%.
	if ref.Archetype != "soldier" {
		t.Skipf("soldier archetype=%q, not soldier; adjust upgrade ID in test", ref.Archetype)
	}
	s.applyUpgradeLocked("p1", "iron_warlord_rare", 0)   // +18%
	s.applyUpgradeLocked("p1", "iron_warlord_rare", 0)   // +18%
	s.applyUpgradeLocked("p1", "iron_warlord_common", 0) // +10%

	// Spawn a second soldier AFTER the upgrades.
	pos := s.gridToWorldCenter(s.worldToGrid(220, 200))
	newUnit := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", pos)
	if newUnit == nil {
		t.Fatal("could not spawn second soldier")
	}

	// The accumulated multiplier on the first unit and the new spawn must match.
	if ref.BaseDamage != newUnit.BaseDamage {
		t.Errorf("stacked upgrade mismatch: existing soldier BaseDamage=%d, newly spawned BaseDamage=%d (catalog=%d)",
			ref.BaseDamage, newUnit.BaseDamage, catalogBase)
	}

	// Both must be greater than catalog base (upgrades actually applied).
	if newUnit.BaseDamage <= catalogBase {
		t.Errorf("new unit BaseDamage %d not above catalog %d — wave buffs not applied at spawn",
			newUnit.BaseDamage, catalogBase)
	}
}

func TestApplyUpgrade_UnknownIDIsNoOp(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{
		ID:           "p1",
		Resources:    map[string]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
	}
	// Should not panic.
	s.applyUpgradeLocked("p1", "nonexistent_upgrade", 0)
}
