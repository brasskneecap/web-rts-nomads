package game

import (
	"encoding/json"
	"testing"
)

func TestAbilityDefCarriesProgramAndValidates(t *testing.T) {
	src := `{
		"id":"test_ability","displayName":"Test","type":"spell","schemaVersion":2,
		"program":{"entry":{"type":"unit","relations":["ally"]},
			"triggers":[{"id":"t","type":"on_cast_complete","actions":[
				{"id":"a","type":"restore_health","enabled":true,"config":{"amount":15}}]}]}
	}`
	var def AbilityDef
	if err := json.Unmarshal([]byte(src), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Program == nil {
		t.Fatal("program not decoded")
	}
	if def.SchemaVersion != 2 {
		t.Fatalf("schemaVersion = %d", def.SchemaVersion)
	}
	if err := validateAbilityDef(&def); err != nil {
		t.Fatalf("valid program rejected: %v", err)
	}
}

func TestAbilityDefRejectsInvalidProgram(t *testing.T) {
	def := AbilityDef{ID: "x", SchemaVersion: 2, Program: &AbilityProgram{
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "a", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":0}`)}}}}}}
	if err := validateAbilityDef(&def); err == nil {
		t.Fatal("expected error for amount<=0 program")
	}
}

func TestLegacyAbilityUnaffected(t *testing.T) {
	def := AbilityDef{ID: "heal", HealAmount: 10, Type: AbilitySpell}
	if err := validateAbilityDef(&def); err != nil {
		t.Fatalf("legacy def rejected: %v", err)
	}
	if def.Program != nil {
		t.Fatal("legacy def must not gain a program")
	}
}

func TestProgramWarningsDoNotBlockSave(t *testing.T) {
	// A program that only produces a WARNING (e.g. no_behavior) must NOT be
	// rejected by validateAbilityDef — only errors block.
	def := AbilityDef{ID: "warnonly", SchemaVersion: 2, Program: &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntrySelf}, Triggers: []AbilityTriggerDef{}}}
	if err := validateAbilityDef(&def); err != nil {
		t.Fatalf("warning-only program must not block: %v", err)
	}
}
