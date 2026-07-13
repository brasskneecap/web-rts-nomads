package game

import "testing"

func TestSaveEditorUnit_ValidationError(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	req := EditorUnitSaveRequest{Unit: UnitDef{Type: "bad", Faction: "human", HP: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1, Projectile: "nope"}}
	err := SaveEditorUnit(req)
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("want editor validation error, got %v", err)
	}
}

func TestDeleteEditorUnit_EmbedResets(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	base, _ := getUnitDef("archer")
	edited := base
	edited.Damage += 5
	if err := SaveEditorUnit(EditorUnitSaveRequest{Unit: edited}); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := DeleteEditorUnit("archer")
	if err != nil || !existed {
		t.Fatalf("delete existed=%v err=%v", existed, err)
	}
}
