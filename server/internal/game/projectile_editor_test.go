package game

import "testing"

func TestSaveEditorProjectileValidation(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	err := SaveEditorProjectile(EditorProjectileSaveRequest{Projectile: ProjectileDef{ID: "bad", Kind: "laser"}})
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("expected editor validation error, got %v", err)
	}
}

func TestSaveEditorProjectileOK(t *testing.T) {
	t.Setenv("PROJECTILE_CATALOG_DIR", t.TempDir())
	if err := SaveEditorProjectile(EditorProjectileSaveRequest{Projectile: ProjectileDef{ID: "ok_bolt", Speed: 200}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getProjectileDef("ok_bolt"); !ok {
		t.Fatal("saved projectile not resolvable")
	}
}
