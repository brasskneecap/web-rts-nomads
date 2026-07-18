package game

import "testing"

// TestConvertLegacyAbility_GreaterHeal exercises ConvertLegacyAbility on a
// heal-shaped legacy ability. The real catalog "greater_heal" is
// schemaVersion:2 as of the composable-abilities migration (this test is
// literally what produced that conversion), so ConvertLegacyAbility would
// now error on it ("already composable") — this test registers the FROZEN
// pre-migration shape (ability_legacy_fixtures_test.go) under a scratch id
// instead, preserving the original test's intent: converting a clean,
// fully-runnable heal ability.
func TestConvertLegacyAbility_GreaterHeal(t *testing.T) {
	orig := legacyGreaterHealFixture()
	orig.ID = "greater_heal_legacy_convert_test"
	registerRuntimeTestAbility(t, orig)

	conv, warnings, err := ConvertLegacyAbility(orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conv.SchemaVersion != 2 || conv.Program == nil {
		t.Fatalf("not converted: v=%d prog=%v", conv.SchemaVersion, conv.Program)
	}
	// cast-setup preserved
	if conv.ManaCost != orig.ManaCost || conv.CastRange != orig.CastRange || conv.CanTargetAllies != orig.CanTargetAllies || conv.DamageType != orig.DamageType {
		t.Fatalf("cast-setup not preserved")
	}
	// mechanic fields cleared
	if conv.HealAmount != 0 || conv.Radius != 0 || conv.SummonUnitType != "" {
		t.Fatalf("mechanic fields not cleared: %+v", conv)
	}
	// converted def validates
	c := conv
	c.ID = "greater_heal_legacy_convert_test_validated" // avoid dup-id concerns; validateAbilityDef doesn't check id anyway
	if err := validateAbilityDef(&c); err != nil {
		t.Fatalf("converted def invalid: %v", err)
	}
	// greater_heal IS runnable (heal path) — no degradation warning, OR a warning only about VFX/perk-hook.
	_ = warnings
}

// TestConvertLegacyAbility_SiphonLife_NowFullyRunnable proves the flip side,
// mirroring TestConvertLegacyAbility_ArcaneMissiles_NowFullyRunnable /
// TestConvertLegacyAbility_Fireball_NowFullyRunnable: the compiled
// channel_beam action is fully executor-runnable, so converting siphon_life's
// pre-migration shape produces no degradation warning and
// AbilityProgramRunnable reports true. The real catalog "siphon_life" is
// schemaVersion:2 as of this migration (this test is literally what produced
// that conversion — it was the LAST still-legacy catalog ability), so
// ConvertLegacyAbility would now error on it ("already composable") — this
// test registers the FROZEN pre-migration shape
// (ability_legacy_fixtures_test.go) under a scratch id instead, same pattern
// as TestConvertLegacyAbility_ArcaneMissiles_NowFullyRunnable.
//
// With this flip, NO legacy-shaped AbilityDef compiles to an unregistered
// action type any more (every compileCastActions branch — channel / summon /
// heal / offensive and its sub-shapes — now produces only actions with a
// registered ActionDescriptor). ConvertLegacyAbility therefore has no
// remaining input that exercises its degradation-warning path; see
// TestDegradationWarnings_UnrunnableActionWarns below for that coverage,
// kept alive against a synthetic *AbilityProgram instead (same "no more
// catalog shape available" situation TestRunAbilityPreview_
// DeferredCustomActionSkipsHonestly already documents and solves the same
// way).
func TestConvertLegacyAbility_SiphonLife_NowFullyRunnable(t *testing.T) {
	orig := legacySiphonLifeFixture()
	orig.ID = "siphon_life_legacy_convert_test"
	registerRuntimeTestAbility(t, orig)

	conv, warnings, err := ConvertLegacyAbility(orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("siphon_life should convert with no degradation warnings now that channel_beam is executor-runnable; got %v", warnings)
	}
	if !AbilityProgramRunnable(conv.Program) {
		t.Fatal("siphon_life program should be fully runnable")
	}
}

// TestDegradationWarnings_UnrunnableActionWarns tests degradationWarnings /
// unrunnableActionTypes directly against a hand-built *AbilityProgram
// carrying a bare, unregistered ActionCustom action, decoupled from
// ConvertLegacyAbility's id-based catalog lookup. This is the "still
// deferred" fixture for the warning-generation logic itself: as of the
// siphon_life migration, no legacy AbilityDef shape compiles to an
// unrunnable program any more (see TestConvertLegacyAbility_
// SiphonLife_NowFullyRunnable's doc comment), so there is no real ability id
// left to exercise ConvertLegacyAbility's warning path — mirrors
// TestRunAbilityPreview_DeferredCustomActionSkipsHonestly's synthetic-def
// precedent for the same reason.
func TestDegradationWarnings_UnrunnableActionWarns(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}, Range: 400},
		Triggers: []AbilityTriggerDef{{
			ID:   "cast",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "deferred", Type: ActionCustom},
			},
		}},
	}
	if AbilityProgramRunnable(prog) {
		t.Fatal("a program with a bare, unregistered ActionCustom action should not be runnable")
	}
	warnings := degradationWarnings(prog)
	if len(warnings) == 0 {
		t.Fatal("expected a non-empty degradation warning for an unrunnable action type")
	}
}

// TestConvertLegacyAbility_ArcaneMissiles_NowFullyRunnable proves the flip
// side, mirroring TestConvertLegacyAbility_Fireball_NowFullyRunnable: the
// compiled charge_fire_volley action is fully executor-runnable, so
// converting arcane_missiles' pre-migration shape produces no degradation
// warning and AbilityProgramRunnable reports true. The real catalog
// "arcane_missiles" is schemaVersion:2 as of this migration (this test is
// literally what produced that conversion), so ConvertLegacyAbility would now
// error on it ("already composable") — this test registers the FROZEN
// pre-migration shape (ability_legacy_fixtures_test.go) under a scratch id
// instead, same pattern as TestConvertLegacyAbility_GreaterHeal/Fireball.
func TestConvertLegacyAbility_ArcaneMissiles_NowFullyRunnable(t *testing.T) {
	orig := legacyArcaneMissilesFixture()
	orig.ID = "arcane_missiles_legacy_convert_test"
	registerRuntimeTestAbility(t, orig)

	conv, warnings, err := ConvertLegacyAbility(orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("arcane_missiles should convert with no degradation warnings now that charge_fire_volley is executor-runnable; got %v", warnings)
	}
	if !AbilityProgramRunnable(conv.Program) {
		t.Fatal("arcane_missiles program should be fully runnable")
	}
}

// TestConvertLegacyAbility_Fireball_NowFullyRunnable proves the flip side:
// fireball's compiled launch_projectile action is fully executor-runnable, so
// converting it produces no degradation warning and AbilityProgramRunnable
// reports true. The real catalog "fireball" is schemaVersion:2 as of the
// composable-abilities migration (this test is literally what produced that
// conversion), so ConvertLegacyAbility would now error on it ("already
// composable") — this test registers the FROZEN pre-migration shape
// (ability_legacy_fixtures_test.go) under a scratch id instead, same pattern
// as TestConvertLegacyAbility_GreaterHeal.
func TestConvertLegacyAbility_Fireball_NowFullyRunnable(t *testing.T) {
	orig := legacyFireballFixture()
	orig.ID = "fireball_legacy_convert_test"
	registerRuntimeTestAbility(t, orig)

	conv, warnings, err := ConvertLegacyAbility(orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("fireball should convert with no degradation warnings now that launch_projectile is executor-runnable; got %v", warnings)
	}
	if !AbilityProgramRunnable(conv.Program) {
		t.Fatal("fireball program should be fully runnable")
	}
}

// TestConvertLegacyAbility_AlreadyV2 verifies ConvertLegacyAbility errors on
// an ability that is already schemaVersion:2. The real catalog "greater_heal"
// IS already composable as of the composable-abilities migration, so it
// serves directly as the fixture here — no need to hand-convert-then-register
// a synthetic one anymore.
func TestConvertLegacyAbility_AlreadyV2(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	if def.SchemaVersion < 2 || def.Program == nil {
		t.Fatalf("precondition: catalog greater_heal must be schemaVersion>=2 with a Program; got schemaVersion=%d program=%v", def.SchemaVersion, def.Program)
	}

	if _, _, err := ConvertLegacyAbility("greater_heal"); err == nil {
		t.Fatal("want error converting an already-composable (schemaVersion 2) ability")
	}
}

func TestConvertLegacyAbility_Unknown(t *testing.T) {
	if _, _, err := ConvertLegacyAbility("does_not_exist"); err == nil {
		t.Fatal("want error for unknown id")
	}
}
