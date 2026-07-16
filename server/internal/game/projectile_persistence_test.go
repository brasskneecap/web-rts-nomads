package game

import "testing"

func TestSaveAndOverlayProjectileDef(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	// empty Kind + zero Speed should normalize on save
	if err := SaveProjectileDef(&ProjectileDef{ID: "test_bolt"}); err != nil {
		t.Fatalf("SaveProjectileDef: %v", err)
	}
	got, ok := getProjectileDef("test_bolt")
	if !ok || got.Kind != EmitterKindProjectile || got.Speed != defaultProjectileSpeed {
		t.Fatalf("normalize-on-save failed: ok=%v got=%+v", ok, got)
	}
	if ProjectileIsEmbedded("test_bolt") {
		t.Fatal("test_bolt should not be embedded")
	}
	existed, err := DeleteProjectileOverride("test_bolt")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
	if _, ok := getProjectileDef("test_bolt"); ok {
		t.Fatal("def still resolvable after delete")
	}
}

func TestProjectileDiskRoundTripAndRevert(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	if err := SaveProjectileDef(&ProjectileDef{ID: "disk_bolt", Speed: 300, FollowEffect: "fizzle"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	runtimeProjectilesMu.Lock()
	delete(runtimeProjectiles, "disk_bolt")
	runtimeProjectilesMu.Unlock()
	if _, ok := getProjectileDef("disk_bolt"); ok {
		t.Fatal("expected miss after clearing overlay")
	}
	LoadPersistedProjectilesIntoOverlay()
	if got, ok := getProjectileDef("disk_bolt"); !ok || got.Speed != 300 || got.FollowEffect != "fizzle" {
		t.Fatalf("disk reload failed: ok=%v got=%+v", ok, got)
	}
	var embeddedID string
	for _, d := range ListProjectileDefs() {
		if ProjectileIsEmbedded(d.ID) {
			embeddedID = d.ID
			break
		}
	}
	if embeddedID == "" {
		t.Skip("no embedded projectiles to test revert")
	}
	original := projectileDefsByID[embeddedID]
	override := original
	override.Speed = original.Speed + 111
	if err := SaveProjectileDef(&override); err != nil {
		t.Fatalf("override save: %v", err)
	}
	if got, _ := getProjectileDef(embeddedID); got.Speed != original.Speed+111 {
		t.Fatal("overlay did not win over embed")
	}
	if _, err := DeleteProjectileOverride(embeddedID); err != nil {
		t.Fatalf("revert delete: %v", err)
	}
	if !ProjectileIsEmbedded(embeddedID) {
		t.Fatal("embedded id lost embedded status")
	}
	if got, _ := getProjectileDef(embeddedID); got.Speed != original.Speed {
		t.Fatalf("did not revert to embedded default: %+v", got)
	}
}

func TestSaveProjectileDefRejectsBadID(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	if err := SaveProjectileDef(&ProjectileDef{ID: "Bad/../x"}); err == nil {
		t.Fatal("expected id-pattern rejection")
	}
}
