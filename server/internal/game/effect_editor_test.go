package game

import "testing"

func TestSaveEditorEffectValidation(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	err := SaveEditorEffect(EditorEffectSaveRequest{Effect: EffectDef{ID: "bad", Duration: -1}})
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("expected editor validation error, got %v", err)
	}
}

func TestSaveEditorEffectOK(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	if err := SaveEditorEffect(EditorEffectSaveRequest{Effect: EffectDef{ID: "ok_fx", Duration: 1}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getEffectDef("ok_fx"); !ok {
		t.Fatal("saved effect not resolvable")
	}
}
