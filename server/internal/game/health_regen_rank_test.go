package game

import (
	"encoding/json"
	"testing"
)

// newTestStateForRegen builds a minimal state with one player, matching the
// idiom used by the other progression tests (see newTestStateForUpgrades).
func newTestStateForRegen(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	return s
}

// regenUnit builds a minimal unit with a known base regen, as spawn would.
func regenUnit(baseRegen float64) *Unit {
	return &Unit{
		ID:                       1,
		UnitType:                 "soldier",
		OwnerID:                  "p1",
		BaseMaxHP:                100,
		MaxHP:                    100,
		HP:                       100,
		BaseDamage:               10,
		BaseAttackSpeed:          1,
		BaseMoveSpeed:            50,
		Rank:                     unitRankBase,
		ProgressionPath:          unitPathNone,
		BaseHealthRegenPerSecond: baseRegen,
		HealthRegenPerSecond:     baseRegen,
	}
}

// A path/rank multiplier scales the unit's base regen. This is the capability
// being added — before this, rank never touched regen at all.
func TestApplyRankModifiers_HealthRegenScalesWithPathRank(t *testing.T) {
	s := newTestStateForRegen(t)
	unit := regenUnit(1.0)

	// Register a synthetic path whose gold rank doubles regen. Using a synthetic
	// path keeps the test independent of whatever the shipped paths author.
	key := pathModifierKey("test_regen_path", unitRankGold)
	pathModifiersByKey[key] = pathModifierDef{
		Path: "test_regen_path", Rank: unitRankGold,
		MaxHPMultiplier: 1.0, MaxMPMultiplier: 1.0, HealthRegenMultiplier: 2.0,
		DamageMultiplier: 1.0, AttackSpeedMultiplier: 1.0, MoveSpeedMultiplier: 1.0,
		AttackRangeMultiplier: 1.0,
	}
	t.Cleanup(func() { delete(pathModifiersByKey, key) })

	unit.ProgressionPath = "test_regen_path"
	unit.Rank = unitRankGold
	s.applyRankModifiersLocked(unit, true)

	if unit.HealthRegenPerSecond != 2.0 {
		t.Fatalf("regen = %v, want 2.0 (base 1.0 × gold multiplier 2.0)", unit.HealthRegenPerSecond)
	}
}

// A path that does NOT author healthRegenMultiplier must leave regen untouched.
// This is what keeps the new field from silently rebalancing every existing path.
func TestApplyRankModifiers_UnauthoredMultiplierLeavesRegenUnchanged(t *testing.T) {
	s := newTestStateForRegen(t)
	unit := regenUnit(0.2)

	unit.Rank = unitRankGold // path "none" ⇒ defaultRankCurve, which does not scale regen
	s.applyRankModifiersLocked(unit, true)

	if unit.HealthRegenPerSecond != 0.2 {
		t.Fatalf("regen = %v, want 0.2 unchanged — an unauthored multiplier must not scale regen", unit.HealthRegenPerSecond)
	}
}

// A unit authored with healthRegenRate: 0 never regenerates, at ANY rank. A
// multiplier cannot resurrect regen from zero.
func TestApplyRankModifiers_AuthoredZeroRegenStaysZeroAtEveryRank(t *testing.T) {
	s := newTestStateForRegen(t)

	key := pathModifierKey("test_construct_path", unitRankGold)
	pathModifiersByKey[key] = pathModifierDef{
		Path: "test_construct_path", Rank: unitRankGold,
		MaxHPMultiplier: 1.0, MaxMPMultiplier: 1.0, HealthRegenMultiplier: 5.0,
		DamageMultiplier: 1.0, AttackSpeedMultiplier: 1.0, MoveSpeedMultiplier: 1.0,
		AttackRangeMultiplier: 1.0,
	}
	t.Cleanup(func() { delete(pathModifiersByKey, key) })

	unit := regenUnit(0) // a construct: authored healthRegenRate 0
	unit.ProgressionPath = "test_construct_path"
	unit.Rank = unitRankGold
	s.applyRankModifiersLocked(unit, true)

	if unit.HealthRegenPerSecond != 0 {
		t.Fatalf("regen = %v, want 0 — a 5x multiplier must not resurrect regen from an authored zero", unit.HealthRegenPerSecond)
	}
}

// Equipment regen is a FLAT add on top of the scaled base, and is not
// double-counted now that the old delta hack is gone. Re-running the rank
// recompute repeatedly must be idempotent.
func TestApplyRankModifiers_EquipmentRegenAddsOnceAndIsIdempotent(t *testing.T) {
	s := newTestStateForRegen(t)
	unit := regenUnit(1.0)
	unit.EquipmentBonus.HealthRegen = 3.0

	s.applyRankModifiersLocked(unit, true)
	first := unit.HealthRegenPerSecond
	if first != 4.0 {
		t.Fatalf("regen = %v, want 4.0 (base 1.0 + equipment 3.0)", first)
	}

	// The old delta implementation would drift on every recompute. This must not.
	s.applyRankModifiersLocked(unit, true)
	s.applyRankModifiersLocked(unit, true)
	if unit.HealthRegenPerSecond != first {
		t.Fatalf("regen drifted to %v across recomputes, want a stable %v — equipment regen is being double-counted", unit.HealthRegenPerSecond, first)
	}
}

// Equipment can grant regen to a unit whose base is 0 (the flat-add escape
// hatch), even though a multiplier cannot.
func TestApplyRankModifiers_EquipmentCanGrantRegenToAZeroBaseUnit(t *testing.T) {
	s := newTestStateForRegen(t)
	unit := regenUnit(0)
	unit.EquipmentBonus.HealthRegen = 2.0

	s.applyRankModifiersLocked(unit, true)

	if unit.HealthRegenPerSecond != 2.0 {
		t.Fatalf("regen = %v, want 2.0 — a flat equipment add must still reach a zero-base unit", unit.HealthRegenPerSecond)
	}
}

// Negative equipment regen must clamp at 0, never go negative (which would
// drain HP through the regen tick).
func TestApplyRankModifiers_RegenClampsAtZero(t *testing.T) {
	s := newTestStateForRegen(t)
	unit := regenUnit(1.0)
	unit.EquipmentBonus.HealthRegen = -5.0

	s.applyRankModifiersLocked(unit, true)

	if unit.HealthRegenPerSecond != 0 {
		t.Fatalf("regen = %v, want 0 — a large negative equipment regen must clamp, not drain HP", unit.HealthRegenPerSecond)
	}
}

// The tests above inject pathModifiersByKey directly, which bypasses JSON
// parsing entirely — so a typo'd json tag would still let them all pass while
// the field silently never loaded from a real path file. This test closes that
// gap by going through the actual unmarshal.
func TestPathRankStatsJSON_HealthRegenMultiplierDeserializes(t *testing.T) {
	var row pathRankStatsJSON
	raw := []byte(`{"maxHPMultiplier":1.2,"healthRegenMultiplier":2.5,"damageMultiplier":1.0}`)
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if row.HealthRegenMultiplier != 2.5 {
		t.Fatalf("healthRegenMultiplier = %v, want 2.5 — the json tag does not match what a path file would author", row.HealthRegenMultiplier)
	}
}

// An omitted healthRegenMultiplier must land as 0 in the JSON struct, which the
// loader then normalizes to 1.0. Pins the "omitted ⇒ no-op" contract.
func TestPathRankStatsJSON_OmittedHealthRegenMultiplierIsZero(t *testing.T) {
	var row pathRankStatsJSON
	if err := json.Unmarshal([]byte(`{"maxHPMultiplier":1.2}`), &row); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if row.HealthRegenMultiplier != 0 {
		t.Fatalf("omitted healthRegenMultiplier = %v, want 0 (the loader turns 0 into 1.0)", row.HealthRegenMultiplier)
	}
}
