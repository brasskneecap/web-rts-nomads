package game

import (
	"encoding/json"
	"testing"
)

// hasError reports whether any issue in issues is error-severity.
func hasError(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}

// actionTypes returns the ActionType of every action in acts, in order, for
// readable test-failure output.
func actionTypes(acts []AbilityActionDef) []ActionType {
	out := make([]ActionType, len(acts))
	for i, a := range acts {
		out[i] = a.Type
	}
	return out
}

// These four tests exercise compileLegacyAbility's SHAPE for each mechanic
// family it handles. greater_heal / heal / shatter / raise_skeleton are now
// schemaVersion:2 in the live catalog (composable-abilities migration), so
// getAbilityDef would return an already-compiled Program with cleared
// mechanic fields — reading it here would test nothing (compileLegacyAbility
// on a def with HealAmount==0 etc. just produces an empty action list). These
// tests are specifically about the COMPILER, not the catalog's live state, so
// they compile the frozen pre-migration fixture (ability_legacy_fixtures_test.go)
// instead — exactly what ConvertLegacyAbility itself did to produce the
// shipped Program in the first place.

func TestCompileGreaterHealStructure(t *testing.T) {
	def := legacyGreaterHealFixture()
	prog := compileLegacyAbility(def)
	if prog == nil || len(prog.Triggers) != 1 || prog.Triggers[0].Type != TriggerOnCastComplete {
		t.Fatalf("bad trigger shape")
	}
	acts := prog.Triggers[0].Actions
	if acts[0].Type != ActionSelectTargets || acts[1].Type != ActionRestoreHealth {
		t.Fatalf("bad action sequence: %v", actionTypes(acts))
	}
	if acts[0].Target == nil || acts[0].Target.MaxCount != def.TargetCount {
		t.Fatalf("select maxCount %d != targetCount %d", acts[0].Target.MaxCount, def.TargetCount)
	}
	var rh restoreHealthConfig
	if err := json.Unmarshal(acts[1].Config, &rh); err != nil {
		t.Fatal(err)
	}
	if rh.Amount != def.HealAmount || rh.School != def.DamageType {
		t.Fatalf("heal cfg mismatch: %+v vs amount=%d school=%s", rh, def.HealAmount, def.DamageType)
	}
	if hasError(validateAbilityProgram(prog)) {
		t.Fatalf("compiled program invalid: %+v", validateAbilityProgram(prog))
	}
}

func TestCompileBasicHealStructure(t *testing.T) {
	def := legacyHealFixture()
	if def.TargetCount != 1 {
		t.Fatalf("expected heal.targetCount == 1 (test assumption), got %d", def.TargetCount)
	}
	prog := compileLegacyAbility(def)
	if prog == nil || len(prog.Triggers) != 1 || prog.Triggers[0].Type != TriggerOnCastComplete {
		t.Fatalf("bad trigger shape")
	}
	acts := prog.Triggers[0].Actions
	if len(acts) < 2 {
		t.Fatalf("expected at least select_targets + restore_health, got %v", actionTypes(acts))
	}
	if acts[0].Type != ActionSelectTargets || acts[1].Type != ActionRestoreHealth {
		t.Fatalf("bad action sequence: %v", actionTypes(acts))
	}
	if acts[0].Target == nil || acts[0].Target.Source != SrcInitialTarget {
		t.Fatalf("single-target heal should select from initial_target, got %+v", acts[0].Target)
	}
	var rh restoreHealthConfig
	if err := json.Unmarshal(acts[1].Config, &rh); err != nil {
		t.Fatal(err)
	}
	if rh.Amount != def.HealAmount || rh.School != def.DamageType {
		t.Fatalf("heal cfg mismatch: %+v vs amount=%d school=%s", rh, def.HealAmount, def.DamageType)
	}
	if hasError(validateAbilityProgram(prog)) {
		t.Fatalf("compiled program invalid: %+v", validateAbilityProgram(prog))
	}
}

func TestCompileShatterStructure(t *testing.T) {
	def := legacyShatterFixture()
	prog := compileLegacyAbility(def)
	if prog == nil || len(prog.Triggers) != 1 || prog.Triggers[0].Type != TriggerOnCastComplete {
		t.Fatalf("bad trigger shape")
	}
	acts := prog.Triggers[0].Actions
	if len(acts) < 3 {
		t.Fatalf("expected select+damage+slow(+vfx), got %v", actionTypes(acts))
	}
	if acts[0].Type != ActionSelectTargets {
		t.Fatalf("action[0] = %s, want select_targets", acts[0].Type)
	}
	if acts[0].Target == nil || acts[0].Target.Origin != OriginCastPoint || acts[0].Target.Radius != def.Radius {
		t.Fatalf("shatter select target mismatch: %+v", acts[0].Target)
	}
	foundRelEnemy := false
	for _, r := range acts[0].Target.Relations {
		if r == RelEnemy {
			foundRelEnemy = true
		}
	}
	if !foundRelEnemy {
		t.Fatalf("shatter select relations missing enemy: %v", acts[0].Target.Relations)
	}

	if acts[1].Type != ActionDealDamage {
		t.Fatalf("action[1] = %s, want deal_damage", acts[1].Type)
	}
	var dd dealDamageConfig
	if err := json.Unmarshal(acts[1].Config, &dd); err != nil {
		t.Fatal(err)
	}
	if dd.Amount != def.DamageAmount || dd.Type != def.DamageType {
		t.Fatalf("deal_damage cfg mismatch: %+v vs amount=%d type=%s", dd, def.DamageAmount, def.DamageType)
	}

	var sawSlow, sawVFX bool
	for _, a := range acts[2:] {
		switch a.Type {
		case ActionApplyStatus:
			sawSlow = true
			var as applyStatusConfig
			if err := json.Unmarshal(a.Config, &as); err != nil {
				t.Fatal(err)
			}
			if as.Status != "slow" || as.Multiplier != def.SlowMultiplier || as.Duration != def.SlowDurationSeconds || as.School != def.DamageType {
				t.Fatalf("apply_status(slow) cfg mismatch: %+v", as)
			}
		case ActionPlayPresentation:
			sawVFX = true
		}
	}
	if !sawSlow {
		t.Fatalf("expected an apply_status(slow) action, got %v", actionTypes(acts))
	}
	if def.EffectAtPoint != "" && !sawVFX {
		t.Fatalf("expected a play_presentation action for effectAtPoint %q, got %v", def.EffectAtPoint, actionTypes(acts))
	}

	if hasError(validateAbilityProgram(prog)) {
		t.Fatalf("compiled program invalid: %+v", validateAbilityProgram(prog))
	}
}

func TestCompileRaiseSkeletonStructure(t *testing.T) {
	def := legacyRaiseSkeletonFixture()
	prog := compileLegacyAbility(def)
	if prog == nil || len(prog.Triggers) != 1 || prog.Triggers[0].Type != TriggerOnCastComplete {
		t.Fatalf("bad trigger shape")
	}
	acts := prog.Triggers[0].Actions
	if len(acts) != 1 || acts[0].Type != ActionSummonUnit {
		t.Fatalf("expected a single summon_unit action, got %v", actionTypes(acts))
	}
	var su summonUnitConfig
	if err := json.Unmarshal(acts[0].Config, &su); err != nil {
		t.Fatal(err)
	}
	if su.UnitType != def.SummonUnitType || su.Count != def.SummonCount {
		t.Fatalf("summon_unit cfg mismatch: %+v vs unitType=%s count=%d", su, def.SummonUnitType, def.SummonCount)
	}
	if hasError(validateAbilityProgram(prog)) {
		t.Fatalf("compiled program invalid: %+v", validateAbilityProgram(prog))
	}
}
