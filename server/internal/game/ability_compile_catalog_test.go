package game

import (
	"encoding/json"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Full-catalog compile smoke test (Phase 4, Task 2)
//
// These tests lock the compiler's output shape for every registered ability:
// they compile the full catalog, walk the resulting program trees, and assert
// (1) no compiled program is structurally invalid or references an unknown
// action type, (2) the top-level on_cast_complete action-type sequence per
// ability matches a hand-verified table (a regression guard on compiler
// shape), and (3) the "is this ability runnable by today's executor set"
// classification matches the current phase-3 registration state.
//
// TEST-ONLY: nothing here exercises the live cast path; compileLegacyAbility
// is pure and this file makes no GameState.
// ═════════════════════════════════════════════════════════════════════════════

// collectProgramActionTypes walks the structurally-visible action tree of
// prog and returns every ActionType it finds, in traversal order (root
// triggers' actions, each action's Children triggers' actions recursively,
// then each Presentation's triggers' actions recursively). It does NOT
// descend into action Config raw JSON (e.g. create_zone's nested
// on_zone_tick triggers) — those live inside opaque per-action config and
// are out of scope for this structural walk.
func collectProgramActionTypes(prog *AbilityProgram) []ActionType {
	var out []ActionType
	var walkTrigger func(trig AbilityTriggerDef)
	var walkAction func(a AbilityActionDef)

	walkAction = func(a AbilityActionDef) {
		out = append(out, a.Type)
		for _, child := range a.Children {
			walkTrigger(child)
		}
	}
	walkTrigger = func(trig AbilityTriggerDef) {
		for _, a := range trig.Actions {
			walkAction(a)
		}
	}

	for _, trig := range prog.Triggers {
		walkTrigger(trig)
	}
	for _, pres := range prog.Presentations {
		for _, trig := range pres.Triggers {
			walkTrigger(trig)
		}
	}
	return out
}

// catalogProgram returns the program that governs def's actual runtime
// behavior: the shipped, already-compiled Program for a schemaVersion:2
// ability (heal/greater_heal/shatter/raise_skeleton/meteor as of the
// composable-abilities migration — their mechanic fields are cleared, so
// re-running compileLegacyAbility on them would silently produce an empty/
// no-behavior program instead of testing anything), or a fresh
// compileLegacyAbility(def) for every still-legacy ability. This is the
// single seam the catalog-wide structural tests below use so a full
// ListAbilityDefs() walk exercises "what this ability actually runs" for
// every entry, migrated or not.
func catalogProgram(def AbilityDef) *AbilityProgram {
	if def.SchemaVersion >= 2 && def.Program != nil {
		return def.Program
	}
	return compileLegacyAbility(def)
}

// programIsExecutorRunnable classifies prog as executor-runnable iff every
// action type reachable via collectProgramActionTypes (the
// structurally-visible tree: root actions + Children + Presentations'
// triggers' actions) has a registered ActionDescriptor with a non-nil
// Execute. It does NOT descend into action Config-embedded nested triggers
// (e.g. create_zone's on_zone_tick actions), so an ability whose only
// unexecutable behavior lives inside such a Config (none in the current
// catalog) would be misclassified as runnable — acceptable for this
// structural-shape test per the task's documented scope.
func programIsExecutorRunnable(prog *AbilityProgram) bool {
	for _, t := range collectProgramActionTypes(prog) {
		desc, ok := lookupActionDescriptor(t)
		if !ok || desc.Execute == nil {
			return false
		}
	}
	return true
}

// TestCompileAllCatalogAbilities compiles every registered ability and
// asserts the result is structurally sound: non-nil, no error-severity
// validation issues, and every action type used anywhere in the program
// (including nested Children and Presentation triggers) is a member of the
// canonical allActionTypes set.
func TestCompileAllCatalogAbilities(t *testing.T) {
	for _, def := range ListAbilityDefs() {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			prog := catalogProgram(def)
			if prog == nil {
				t.Fatalf("catalogProgram(%q) returned nil", def.ID)
			}

			if issues := validateAbilityProgram(prog); hasError(issues) {
				b, _ := json.MarshalIndent(prog, "", "  ")
				t.Fatalf("ability %q compiled to an invalid program: issues=%+v\nprogram:\n%s", def.ID, issues, b)
			}

			for _, at := range collectProgramActionTypes(prog) {
				if !isKnownActionType(at) {
					b, _ := json.MarshalIndent(prog, "", "  ")
					t.Fatalf("ability %q compiled to unknown action type %q\nprogram:\n%s", def.ID, at, b)
				}
			}
		})
	}
}

// TestCompileCatalogActionTypeShape locks the top-level on_cast_complete
// action-type sequence per ability. A future compiler change that alters an
// ability's structure will fail this test — that's the point. It asserts
// SHAPE only (action types in order), never tunable numbers (damage/heal
// amounts etc. belong to fixture/golden tests, and per project convention we
// don't pin balance numbers in tests).
func TestCompileCatalogActionTypeShape(t *testing.T) {
	expected := map[string][]ActionType{
		"arcane_bolt":     {ActionLaunchProjectile},
		"arcane_missiles": {ActionChargeFireVolley},
		"arcane_orb":      {ActionLaunchVortex},
		"chain_lightning": {ActionLaunchProjectile},
		"fireball":        {ActionLaunchProjectile},
		"greater_heal":    {ActionSelectTargets, ActionRestoreHealth, ActionPlayPresentation},
		"heal":            {ActionSelectTargets, ActionRestoreHealth, ActionPlayPresentation},
		"meteor":          {ActionPlayPresentation},
		"raise_skeleton":  {ActionSummonUnit},
		"shatter":         {ActionSelectTargets, ActionDealDamage, ActionApplyStatus, ActionPlayPresentation},
		"siphon_life":     {ActionChannelBeam},
	}

	defs := ListAbilityDefs()
	if len(defs) != len(expected) {
		ids := make([]string, len(defs))
		for i, d := range defs {
			ids[i] = d.ID
		}
		t.Fatalf("catalog has %d abilities but expectation table has %d entries; catalog ids: %v", len(defs), len(expected), ids)
	}

	for _, def := range defs {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			want, ok := expected[def.ID]
			if !ok {
				t.Fatalf("ability %q has no entry in the expected action-type table", def.ID)
			}
			prog := catalogProgram(def)
			got := actionTypes(prog.Triggers[0].Actions)
			if !actionTypeSlicesEqual(got, want) {
				t.Fatalf("ability %q top-level on_cast_complete action types = %v, want %v", def.ID, got, want)
			}
		})
	}
}

// actionTypeSlicesEqual reports whether a and b contain the same ActionType
// values in the same order.
func actionTypeSlicesEqual(a, b []ActionType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCompileMeteorActions_ImpactTriggerUsesAuthoredDelay locks in Phase 6b
// Task 4: the compiled meteor program's "impact" on_animation_marker trigger
// must carry the def's authored ImpactDelaySeconds as Timing.DelaySeconds so
// the on_animation_marker scheduler (Task 2) fires it that long after cast,
// matching the legacy live-match GroundHazard path instead of firing on the
// tick right after cast. This is specifically about compileLegacyAbility's
// behavior, so it compiles the frozen pre-migration fixture
// (ability_legacy_fixtures_test.go) rather than the live catalog def, which
// is schemaVersion:2 with ImpactDelaySeconds cleared to 0 as of the
// composable-abilities migration. The expected value is read from the
// fixture, never hardcoded.
func TestCompileMeteorActions_ImpactTriggerUsesAuthoredDelay(t *testing.T) {
	def := legacyMeteorFixture()
	if def.ImpactDelaySeconds <= 0 {
		t.Fatalf("meteor fixture ImpactDelaySeconds = %v; want > 0 (test is meaningless at 0)", def.ImpactDelaySeconds)
	}

	prog := compileLegacyAbility(def)
	if len(prog.Presentations) == 0 {
		t.Fatal("compiled meteor program has no presentations")
	}

	var impactTrig *AbilityTriggerDef
	for i := range prog.Presentations[0].Triggers {
		trg := &prog.Presentations[0].Triggers[i]
		if trg.Type == TriggerOnAnimationMarker && trg.Timing != nil && trg.Timing.Marker == "impact" {
			impactTrig = trg
		}
	}
	if impactTrig == nil {
		t.Fatal(`compiled meteor program: no on_animation_marker trigger with Timing.Marker == "impact"`)
	}
	if impactTrig.Timing.DelaySeconds != def.ImpactDelaySeconds {
		t.Errorf("impact trigger Timing.DelaySeconds = %v; want %v (catalog ImpactDelaySeconds)", impactTrig.Timing.DelaySeconds, def.ImpactDelaySeconds)
	}
}

// TestCompileExecutorRunnableClassification classifies every catalog ability
// as executor-runnable or deferred, per programIsExecutorRunnable, and
// asserts the runnable set matches exactly the abilities whose compiled
// programs use ONLY actions with a registered, executable ActionDescriptor
// as of this phase.
//
// NOTE on the runnable set (updated Phase 6b, Task 1): play_presentation now
// has a registered ActionDescriptor (ability_exec_presentation.go), so heal,
// greater_heal, and shatter — each of which compiles a trailing
// play_presentation action from a non-empty catalog "effectOnTarget"/
// "effectAtPoint" (see compileHealActions / compileShatterActions in
// ability_compile.go) — are now fully runnable: every action type in their
// compiled trees (select_targets, restore_health/deal_damage, apply_status,
// play_presentation) has a registered, executable descriptor. meteor is
// runnable too: its compiled program uses play_presentation, select_targets,
// deal_damage, and create_zone (the latter already registered in
// ability_zone.go from an earlier phase) — all four now have Execute.
//
// UPDATED (Phase 6c): launch_projectile now has a registered ActionDescriptor
// too (ability_exec_projectile.go), so arcane_bolt, chain_lightning, and
// fireball — each of which compiles to a single launch_projectile action
// (compileProjectileActions) — are now fully runnable as well. The catalog
// JSONs themselves are NOT migrated by this phase (they stay schemaVersion 0
// / legacy at runtime; catalogProgram compiles them fresh via
// compileLegacyAbility purely for this structural classification, same as
// every other still-legacy ability).
//
// UPDATED (arcane_orb composable migration): launch_vortex now has a
// registered ActionDescriptor too (ability_exec_vortex.go), and arcane_orb
// IS migrated to schemaVersion:2 in the live catalog (catalogProgram returns
// its shipped Program directly rather than recompiling).
//
// UPDATED (arcane_missiles composable migration): charge_fire_volley now has
// a registered ActionDescriptor too (spell_charge.go), and arcane_missiles IS
// migrated to schemaVersion:2 in the live catalog.
//
// UPDATED (siphon_life composable migration — finishes the catalog):
// channel_beam now has a registered ActionDescriptor too
// (ability_exec_channel.go), and siphon_life IS migrated to schemaVersion:2
// in the live catalog. Every catalog ability is now executor-runnable; there
// is no remaining deferred ability.
func TestCompileExecutorRunnableClassification(t *testing.T) {
	wantRunnable := map[string]bool{
		"raise_skeleton":  true,
		"heal":            true,
		"greater_heal":    true,
		"shatter":         true,
		"meteor":          true,
		"arcane_bolt":     true,
		"chain_lightning": true,
		"fireball":        true,
		"arcane_orb":      true,
		"arcane_missiles": true,
		"siphon_life":     true,
	}

	for _, def := range ListAbilityDefs() {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			prog := catalogProgram(def)
			got := programIsExecutorRunnable(prog)
			want := wantRunnable[def.ID]
			if got != want {
				t.Fatalf("ability %q executor-runnable = %v, want %v (action types: %v)", def.ID, got, want, collectProgramActionTypes(prog))
			}
		})
	}
}
