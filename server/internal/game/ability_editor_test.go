package game

import "testing"

// abilityEditorEnv points the writable abilities dir at a temp dir and
// cleans the overlay + undo store afterward. Mirrors editorEnv
// (item_editor_test.go) for the ability side of the editor.
func abilityEditorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	t.Cleanup(func() {
		runtimeAbilitiesMu.Lock()
		runtimeAbilities = map[string]AbilityDef{}
		runtimeAbilitiesMu.Unlock()
		abilityUndoMu.Lock()
		abilityUndo = map[string]*AbilityDef{}
		abilityUndoMu.Unlock()
	})
}

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

func TestEditorAbilityIssues_ProgramError(t *testing.T) {
	def := AbilityDef{ID: "x", DisplayName: "X", SchemaVersion: 2, Program: &AbilityProgram{
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "a", Type: ActionDealDamage, Config: []byte(`{"amount":0}`)}}}}}}
	issues := EditorAbilityIssues(def)
	found := false
	for _, i := range issues {
		if i.Code == "empty_required_property" && i.Severity == "error" && i.Path != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected deal_damage amount issue with path, got %+v", issues)
	}
}

func TestEditorAbilityIssues_CleanLegacy(t *testing.T) {
	def, _ := getAbilityDef("heal") // legacy, no program
	if issues := EditorAbilityIssues(def); len(issues) != 0 {
		t.Fatalf("legacy heal should be clean, got %+v", issues)
	}
}

func TestEditorAbilityIssues_BadIdentity(t *testing.T) {
	def := AbilityDef{ID: "Bad Id!", DamageType: "not_a_school"}
	issues := EditorAbilityIssues(def)
	hasID, hasSchool := false, false
	for _, i := range issues {
		if i.Code == "invalid_id" {
			hasID = true
		}
		if i.Code == "invalid_damage_type" {
			hasSchool = true
		}
	}
	if !hasID || !hasSchool {
		t.Fatalf("want id + damageType issues, got %+v", issues)
	}
}

// TestEditorAbilityIssues_ParityWithSave guards against the hand-duplication
// between validateAbilityDef (the save-path gate) and EditorAbilityIssues
// (the dry-run inspection): for every def below, both must agree on whether
// the def is valid. If this test ever fails, either a check was added to
// validateAbilityDef without a matching EditorAbilityIssues check (the round
// -trip bug this endpoint exists to prevent — the editor would show "clean"
// for a def the server then 400s), or vice versa. The longer-term fix, if
// this drifts again, is to refactor both to share one accumulate-issues
// helper instead of two hand-maintained copies.
func TestEditorAbilityIssues_ParityWithSave(t *testing.T) {
	invalid := []struct {
		name string
		def  AbilityDef
	}{
		{"bad_id", AbilityDef{ID: "Bad Id!", DamageAmount: 5}},
		{"invalid_damage_type", AbilityDef{ID: "parity_dt", DamageType: "not_a_school"}},
		{"invalid_category", AbilityDef{ID: "parity_cat", Category: "not_a_category"}},
		{"burn_without_impact_delay", AbilityDef{ID: "parity_burn1", BurnDurationSeconds: 1, BurnTickIntervalSeconds: 0.5}},
		{"burn_without_tick_interval", AbilityDef{ID: "parity_burn2", BurnDurationSeconds: 1, ImpactDelaySeconds: 0.5}},
		{"program_deal_damage_zero_amount", AbilityDef{
			ID: "parity_prog", DisplayName: "Parity Prog", SchemaVersion: 2,
			Program: &AbilityProgram{Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "a", Type: ActionDealDamage, Config: []byte(`{"amount":0}`)}}}}},
		}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
			if err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: tc.def}); err == nil {
				t.Fatalf("SaveEditorAbility accepted an invalid def")
			}
			issues := EditorAbilityIssues(tc.def)
			hasError := false
			for _, i := range issues {
				if i.Severity == "error" {
					hasError = true
				}
			}
			if !hasError {
				t.Fatalf("EditorAbilityIssues found no error-severity issue for an invalid def, got %+v", issues)
			}
		})
	}

	// Valid cases: assert EditorAbilityIssues is clean. Deliberately NOT
	// calling SaveEditorAbility here (it writes to disk/overlay) — the
	// invalid cases above are safe to call because validation runs before
	// any write, but a valid def would actually persist, which this test
	// does not want as a side effect.
	valid := []struct {
		name string
		def  func() AbilityDef
	}{
		{"clean_legacy_heal", func() AbilityDef {
			def, ok := getAbilityDef("heal")
			if !ok {
				t.Fatalf("catalog ability %q not found", "heal")
			}
			return def
		}},
		{"valid_v2_program", func() AbilityDef {
			return AbilityDef{
				ID: "parity_valid_prog", DisplayName: "Parity Valid Prog", SchemaVersion: 2,
				Program: &AbilityProgram{Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
					{ID: "a", Type: ActionDealDamage, Config: []byte(`{"amount":10}`)}}}}},
			}
		}},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			def := tc.def()
			issues := EditorAbilityIssues(def)
			for _, i := range issues {
				if i.Severity == "error" {
					t.Fatalf("EditorAbilityIssues found an error-severity issue for a valid def, got %+v", issues)
				}
			}
		})
	}
}

// firstEmbeddedAbility returns the id and current def of some ability that
// ships in the embedded catalog, skipping the test entirely if none exist
// (mirrors TestAbilityOverrideWinsAndRevertsToEmbed's own lookup, so this
// suite never hardcodes a catalog id).
func firstEmbeddedAbility(t *testing.T) (string, AbilityDef) {
	t.Helper()
	for _, def := range ListAbilityDefs() {
		if AbilityIsEmbedded(def.ID) {
			return def.ID, abilityDefsByID[def.ID]
		}
	}
	t.Skip("no embedded abilities to test against")
	return "", AbilityDef{}
}

// TestDeleteEditorAbility_AuthoredAbilityDeleted: an author-created ability
// (one with no embedded counterpart) is genuinely removed — DeleteEditorAbility
// reports "deleted" and the id stops resolving. Mirrors
// TestDeleteEditorItem_RemovesItemAndItsRecipe.
func TestDeleteEditorAbility_AuthoredAbilityDeleted(t *testing.T) {
	abilityEditorEnv(t)
	const id = "editor_test_doomed_bolt"
	if AbilityIsEmbedded(id) {
		t.Fatalf("%q must not already be an embedded ability", id)
	}
	if err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: AbilityDef{ID: id, DamageAmount: 5}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	status, existed, err := DeleteEditorAbility(id)
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if status != "deleted" {
		t.Errorf("status = %q, want deleted (an author-created ability is really removed)", status)
	}
	if _, ok := getAbilityDef(id); ok {
		t.Error("ability still resolvable after delete")
	}
}

// TestDeleteEditorAbility_ShippedRevertsToPreSaveState: DELETE on a SHIPPED
// ability undoes the author's last save — it restores the state the ability
// was in before that save ("reverted"), not the catalog default. A second
// delete (no undo step left) falls back to the default ("reset"). Mirrors
// TestDeleteEditorItem_ShippedItemRevertsToPreSaveState; also exercises the
// save-then-revert round trip.
func TestDeleteEditorAbility_ShippedRevertsToPreSaveState(t *testing.T) {
	abilityEditorEnv(t)
	id, original := firstEmbeddedAbility(t)
	shippedName := original.DisplayName

	// First save: a deliberate edit the author wants to keep.
	first := original
	first.DisplayName = "Keeper"
	if err := SaveAbilityDef(&first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Second save: the mistake.
	second := first
	second.DisplayName = "Oops"
	if err := SaveAbilityDef(&second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got, _ := getAbilityDef(id); got.DisplayName != "Oops" {
		t.Fatalf("setup failed: %q", got.DisplayName)
	}

	// First delete undoes the LAST save -> back to "Keeper", not the shipped
	// default.
	status, existed, err := DeleteEditorAbility(id)
	if err != nil || !existed {
		t.Fatalf("revert: existed=%v err=%v", existed, err)
	}
	if status != "reverted" {
		t.Errorf("status = %q, want reverted", status)
	}
	if got, ok := getAbilityDef(id); !ok || got.DisplayName != "Keeper" {
		t.Errorf("after revert = (ok=%v) %q, want %q (the state before the last save)", ok, got.DisplayName, "Keeper")
	}

	// A second delete has no undo step left, so it falls back to the shipped
	// default.
	status, existed, err = DeleteEditorAbility(id)
	if err != nil || !existed {
		t.Fatalf("second delete: existed=%v err=%v", existed, err)
	}
	if status != "reset" {
		t.Errorf("second status = %q, want reset", status)
	}
	if got, ok := getAbilityDef(id); !ok || got.DisplayName != shippedName {
		t.Errorf("after second delete = (ok=%v) %q, want the shipped default %q", ok, got.DisplayName, shippedName)
	}
}

// TestDeleteEditorAbility_NotFound: an id naming nothing at all reports
// existed=false regardless of embed status.
func TestDeleteEditorAbility_NotFound(t *testing.T) {
	abilityEditorEnv(t)
	status, existed, err := DeleteEditorAbility("no_such_ability_id_at_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if existed {
		t.Fatal("existed = true for an id that names nothing")
	}
	if status != "" {
		t.Errorf("status = %q, want empty when existed=false", status)
	}
}
