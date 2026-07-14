package game

import "testing"

// clearAbilityOverlayForTest empties the in-memory ability overlay. Used
// alongside clearPathOverlayForTest (path_persistence_test.go) to simulate
// "a fresh process that hasn't called any LoadPersisted*IntoOverlay yet"
// while leaving the on-disk overlay files (already written by an earlier
// Save*) untouched — exactly the boot-time state main.go's Load* calls see.
func clearAbilityOverlayForTest(t *testing.T) {
	t.Helper()
	runtimeAbilitiesMu.Lock()
	for k := range runtimeAbilities {
		delete(runtimeAbilities, k)
	}
	runtimeAbilitiesMu.Unlock()
}

// TestBootLoadOrder_PathsMustLoadAfterAbilities pins Fix 1 (cmd/api/main.go's
// boot load ordering): rebuildDerivedPathMaps re-validates every overlay
// path via validatePathFile, which rejects a path whose "abilities"
// reference isn't a registered ability id yet. If
// LoadPersistedPathsIntoOverlay ran before LoadPersistedAbilitiesIntoOverlay,
// an overlay path referencing an overlay-authored ability would be silently
// skipped (+ slog.Warn) on every restart and never rebuilt.
//
// This test drives the exact Load* sequence a real boot goes through —
// wrong order first (demonstrating the bug), then the corrected order
// main.go now uses (demonstrating the fix) — against the SAME on-disk
// overlay files, by clearing only the IN-MEMORY overlay maps between
// attempts. That's a faithful simulation of a process restart: the on-disk
// files persist across a restart, the in-memory maps do not — re-running
// go:embed's init() isn't possible (and isn't the point; init() only ever
// sees the embedded catalog, never the overlay, which is exactly the gap
// ValidateAllUnitPathChances exists to cover separately).
func TestBootLoadOrder_PathsMustLoadAfterAbilities(t *testing.T) {
	unitDir := t.TempDir()
	abilityDir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", unitDir)
	t.Setenv("ABILITY_CATALOG_DIR", abilityDir)
	t.Cleanup(func() {
		clearPathOverlayForTest(t)
		clearAbilityOverlayForTest(t)
	})

	// Author an ability, then a path referencing it — the same order the
	// editor UX enforces for pathChances (referenced thing first).
	if err := SaveEditorAbility(EditorAbilitySaveRequest{Ability: AbilityDef{ID: "route_test_reload_ability"}}); err != nil {
		t.Fatalf("setup SaveEditorAbility = %v, want nil", err)
	}
	abilities := []string{"route_test_reload_ability"}
	pathFile := &pathCatalogFile{
		Path:      "route_test_reload_path",
		Abilities: &abilities,
		Ranks: map[string]pathRankStatsJSON{
			unitRankBronze: {MaxHPMultiplier: 1.1, DamageMultiplier: 1.1, AttackSpeedMultiplier: 1.0, MoveSpeedMultiplier: 1.0, AttackRangeMultiplier: 1.0},
		},
	}
	if err := SaveEditorPath("acolyte", pathFile); err != nil {
		t.Fatalf("setup SaveEditorPath = %v, want nil", err)
	}

	// Both are now on disk AND in memory (the Save* calls above populated
	// the overlay directly). Reset only the in-memory state to simulate a
	// freshly started process that hasn't loaded anything from disk yet.
	clearPathOverlayForTest(t)
	clearAbilityOverlayForTest(t)

	// WRONG order (paths before abilities) — reproduces the bug: the path
	// fails validatePathFile (ability not registered yet) and is skipped
	// with a warning, never rebuilt even after abilities load afterward.
	LoadPersistedPathsIntoOverlay()
	LoadPersistedAbilitiesIntoOverlay()
	if _, ok := pathModifierLookup(pathModifierKey("route_test_reload_path", unitRankBronze)); ok {
		t.Fatalf("setup invariant broken: path resolved even with the WRONG load order — this test can no longer demonstrate the bug/fix")
	}

	// Reset again and load in the CORRECTED order (abilities before paths)
	// — main.go's fixed sequence.
	clearPathOverlayForTest(t)
	clearAbilityOverlayForTest(t)

	LoadPersistedAbilitiesIntoOverlay()
	LoadPersistedPathsIntoOverlay()

	if _, ok := pathModifierLookup(pathModifierKey("route_test_reload_path", unitRankBronze)); !ok {
		t.Errorf("path did not survive reload even with the CORRECT load order (abilities before paths) — Fix 1 regressed")
	}
	got, ok := pathAbilitiesFor("route_test_reload_path")
	if !ok || len(got) != 1 || got[0] != "route_test_reload_ability" {
		t.Errorf("pathAbilitiesFor(route_test_reload_path) = (%v,%v), want ([route_test_reload_ability], true)", got, ok)
	}
}
