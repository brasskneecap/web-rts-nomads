package game

// ═════════════════════════════════════════════════════════════════════════════
// COMPOSABLE PROGRAM RUNNABLE CLASSIFICATION (Phase 5a, Task 3)
//
// AbilityProgramRunnable answers "will the composable executor actually run
// this program end-to-end, or will part of it silently do nothing?" It is
// the package-level counterpart of the TEST-only helper
// programIsExecutorRunnable (ability_compile_catalog_test.go) introduced in
// Phase 4 — same algorithm, promoted out of _test.go so production code (the
// /abilities/{id}/convert endpoint) can call it too.
// ═════════════════════════════════════════════════════════════════════════════

// AbilityProgramRunnable classifies prog as executor-runnable iff every
// action type reachable via the STRUCTURALLY-VISIBLE tree — root
// prog.Triggers[].Actions[], each action's Children[].Actions[]
// (recursively), each prog.Presentations[].Triggers[].Actions[]
// (recursively), and prog.NamedTriggers values' actions — has a registered
// ActionDescriptor with a non-nil Execute. It does NOT descend into action
// Config-embedded nested triggers (e.g. create_zone's on_zone_tick actions),
// matching the Phase-4 classification's documented scope: an ability whose
// only unexecutable behavior lives inside such a Config would be
// misclassified as runnable. Acceptable per that same documented scope.
func AbilityProgramRunnable(prog *AbilityProgram) bool {
	if prog == nil {
		// No program ⇒ nothing to run. Treated as "not runnable" rather than
		// vacuously true so callers don't mistake "no program" for "fully
		// composable and working".
		return false
	}

	runnable := true
	var walkAction func(a AbilityActionDef)
	var walkTrigger func(trig AbilityTriggerDef)

	walkAction = func(a AbilityActionDef) {
		if d, ok := lookupActionDescriptor(a.Type); !ok || d.Execute == nil {
			runnable = false
		}
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
	for _, trig := range prog.NamedTriggers {
		walkTrigger(trig)
	}

	return runnable
}
