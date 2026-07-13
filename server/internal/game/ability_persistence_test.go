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

// TestAbilityDiskRoundTrip proves the CastRange sentinel survives an actual
// disk reload (MarshalJSON -> disk -> parsePersistedAbilityFile ->
// UnmarshalJSON), not just the in-memory overlay populated by SaveAbilityDef.
func TestAbilityDiskRoundTrip(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())

	def := &AbilityDef{
		ID:           "disk_bolt",
		CastRange:    CastRange(CastRangeMatchAttackRange),
		DamageAmount: 7,
	}
	if err := SaveAbilityDef(def); err != nil {
		t.Fatalf("SaveAbilityDef: %v", err)
	}

	// Clear the overlay entry so the next lookup can only be satisfied by a
	// fresh read from disk.
	runtimeAbilitiesMu.Lock()
	delete(runtimeAbilities, "disk_bolt")
	runtimeAbilitiesMu.Unlock()

	if _, ok := getAbilityDef("disk_bolt"); ok {
		t.Fatal("disk_bolt should be unresolvable after clearing the overlay")
	}

	LoadPersistedAbilitiesIntoOverlay()

	got, ok := getAbilityDef("disk_bolt")
	if !ok {
		t.Fatal("getAbilityDef: disk_bolt not found after reload from disk")
	}
	if !got.CastRange.MatchesAttackRange() {
		t.Fatalf("CastRange sentinel lost on disk round-trip: %v", got.CastRange)
	}

	if _, err := DeleteAbilityOverride("disk_bolt"); err != nil {
		t.Fatalf("cleanup DeleteAbilityOverride: %v", err)
	}
}

// TestAbilityOverrideWinsAndRevertsToEmbed proves an authored override wins
// over an embedded def while present, and that deleting the override reverts
// resolution to the shipped embedded default rather than leaving the id
// unresolvable.
func TestAbilityOverrideWinsAndRevertsToEmbed(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())

	var embeddedID string
	var original AbilityDef
	for _, def := range ListAbilityDefs() {
		if AbilityIsEmbedded(def.ID) {
			embeddedID = def.ID
			original = abilityDefsByID[def.ID]
			break
		}
	}
	if embeddedID == "" {
		t.Skip("no embedded abilities to test revert")
	}

	overriddenName := "OVERRIDDEN_" + embeddedID
	override := original
	override.DisplayName = overriddenName
	if err := SaveAbilityDef(&override); err != nil {
		t.Fatalf("SaveAbilityDef: %v", err)
	}

	got, ok := getAbilityDef(embeddedID)
	if !ok {
		t.Fatalf("getAbilityDef(%q): not found after override save", embeddedID)
	}
	if got.DisplayName != overriddenName {
		t.Fatalf("overlay did not win: got DisplayName %q, want %q", got.DisplayName, overriddenName)
	}

	existed, err := DeleteAbilityOverride(embeddedID)
	if err != nil {
		t.Fatalf("DeleteAbilityOverride: %v", err)
	}
	if !existed {
		t.Fatalf("DeleteAbilityOverride(%q): existed=false, want true", embeddedID)
	}

	if !AbilityIsEmbedded(embeddedID) {
		t.Fatalf("%q should still be embedded after override delete", embeddedID)
	}
	reverted, ok := getAbilityDef(embeddedID)
	if !ok {
		t.Fatalf("getAbilityDef(%q): not found after revert", embeddedID)
	}
	if reverted.DisplayName != original.DisplayName {
		t.Fatalf("did not revert to embedded default: got DisplayName %q, want %q", reverted.DisplayName, original.DisplayName)
	}
}

// TestDeleteAbilityOverrideBadIDNoOp proves an id that fails abilityIDPattern
// (including a path-traversal attempt) is rejected as a no-op: no panic, no
// error, no disk touch.
func TestDeleteAbilityOverrideBadIDNoOp(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())

	existed, err := DeleteAbilityOverride("Bad/../id")
	if err != nil {
		t.Fatalf("DeleteAbilityOverride: unexpected error %v", err)
	}
	if existed {
		t.Fatal("DeleteAbilityOverride: existed=true for an invalid id, want false")
	}
}
