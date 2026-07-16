package game

import "testing"

func TestSaveAndOverlayEffectDef(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	def := &EffectDef{ID: "test_glow", Duration: 0.75, Anchor: "head"}
	if err := SaveEffectDef(def); err != nil {
		t.Fatalf("SaveEffectDef: %v", err)
	}
	got, ok := getEffectDef("test_glow")
	if !ok || got.Duration != 0.75 || got.Anchor != "head" {
		t.Fatalf("overlay def not resolved: ok=%v got=%+v", ok, got)
	}
	if EffectIsEmbedded("test_glow") {
		t.Fatal("test_glow should not be embedded")
	}
	existed, err := DeleteEffectOverride("test_glow")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
	if _, ok := getEffectDef("test_glow"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestEffectDiskRoundTripAndRevert(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	// disk round-trip: save, clear overlay, reload from disk
	if err := SaveEffectDef(&EffectDef{ID: "disk_fx", Duration: 1.5, Anchor: "feet"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	runtimeEffectsMu.Lock()
	delete(runtimeEffects, "disk_fx")
	runtimeEffectsMu.Unlock()
	if _, ok := getEffectDef("disk_fx"); ok {
		t.Fatal("expected miss after clearing overlay")
	}
	LoadPersistedEffectsIntoOverlay()
	if got, ok := getEffectDef("disk_fx"); !ok || got.Duration != 1.5 || got.Anchor != "feet" {
		t.Fatalf("disk reload failed: ok=%v got=%+v", ok, got)
	}
	// embed-revert: override a real embedded effect, then delete reverts
	var embeddedID string
	for _, d := range ListEffectDefs() {
		if EffectIsEmbedded(d.ID) {
			embeddedID = d.ID
			break
		}
	}
	if embeddedID == "" {
		t.Skip("no embedded effects to test revert")
	}
	original := effectDefsByID[embeddedID]
	override := original
	override.Duration = original.Duration + 5
	if err := SaveEffectDef(&override); err != nil {
		t.Fatalf("override save: %v", err)
	}
	if got, _ := getEffectDef(embeddedID); got.Duration != original.Duration+5 {
		t.Fatal("overlay did not win over embed")
	}
	if _, err := DeleteEffectOverride(embeddedID); err != nil {
		t.Fatalf("revert delete: %v", err)
	}
	if !EffectIsEmbedded(embeddedID) {
		t.Fatal("embedded id lost embedded status")
	}
	if got, _ := getEffectDef(embeddedID); got.Duration != original.Duration {
		t.Fatalf("did not revert to embedded default: %+v", got)
	}
}

func TestSaveEffectDefRejectsBadID(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	if err := SaveEffectDef(&EffectDef{ID: "Bad/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
}
