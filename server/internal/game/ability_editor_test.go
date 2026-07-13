package game

import "testing"

func TestSaveEditorAbilityValidation(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: AbilityDef{ID: "bad", Category: "nope"}})
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("expected editor validation error, got %v", err)
	}
}

func TestSaveEditorAbilityOK(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	if err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: AbilityDef{ID: "ok_bolt", DamageAmount: 10}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getAbilityDef("ok_bolt"); !ok {
		t.Fatal("saved ability not resolvable")
	}
}
