package game

import (
	"testing"
)

func TestSaveAndOverlayAbilityDef(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ABILITY_CATALOG_DIR", dir)

	def := &AbilityDef{
		ID:           "test_bolt",
		DisplayName:  "Test Bolt",
		Type:         AbilitySpell,
		CastRange:    CastRange(CastRangeMatchAttackRange),
		DamageAmount: 40,
	}
	if err := SaveAbilityDef(def); err != nil {
		t.Fatalf("SaveAbilityDef: %v", err)
	}

	got, ok := getAbilityDef("test_bolt")
	if !ok {
		t.Fatal("getAbilityDef: overlay def not found")
	}
	if !got.CastRange.MatchesAttackRange() {
		t.Fatalf("CastRange sentinel lost on round-trip: %v", got.CastRange)
	}
	if got.TargetCount != 1 {
		t.Fatalf("TargetCount not normalized: %d", got.TargetCount)
	}

	// Not embedded → delete removes it entirely.
	if AbilityIsEmbedded("test_bolt") {
		t.Fatal("test_bolt should not be embedded")
	}
	existed, err := DeleteAbilityOverride("test_bolt")
	if err != nil || !existed {
		t.Fatalf("DeleteAbilityOverride existed=%v err=%v", existed, err)
	}
	if _, ok := getAbilityDef("test_bolt"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestSaveAbilityDefRejectsBadID(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	if err := SaveAbilityDef(&AbilityDef{ID: "Bad ID/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
}
