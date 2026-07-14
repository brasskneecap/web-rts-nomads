package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearPerkOverlayForTest resets the runtime perk overlay to empty and
// rebuilds the registry back to the pure-embedded baseline. runtimePerkPools
// and perkDefsByID are process-global, so every test that calls
// SavePerkPool / SaveEditorPerkPool / DeletePerkPool / DeleteEditorPerkPool /
// LoadPersistedPerksIntoOverlay MUST register this via t.Cleanup.
func clearPerkOverlayForTest(t *testing.T) {
	t.Helper()
	runtimePerkPoolsMu.Lock()
	for k := range runtimePerkPools {
		delete(runtimePerkPools, k)
	}
	runtimePerkPoolsMu.Unlock()
	rebuildPerkRegistry()
}

// withIsolatedPerkCatalogDir points UNIT_CATALOG_DIR at a fresh t.TempDir()
// so Save/Delete/Load in this test never touch the real source catalog, and
// registers cleanup of the perk overlay. The embedded baseline
// (embeddedPerkPools) is unaffected by this env var — captured once from the
// real go:embed data at process init — so tests still see the real cleric
// bronze perks alongside whatever this test saves into the isolated dir.
func withIsolatedPerkCatalogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() { clearPerkOverlayForTest(t) })
	return dir
}

// countPerkDefsAt returns how many defs in the current registry resolve to
// the given (unitType,pathName,rank), read from the registry itself rather
// than any hardcoded count.
func countPerkDefsAt(unitType, pathName, rank string) int {
	n := 0
	for _, def := range snapshotPerkDefs() {
		if def.UnitType == unitType && def.Path == pathName && def.Rank == rank {
			n++
		}
	}
	return n
}

func TestSaveEditorPerkPool_NewPoolUnderClericBronze_RoundTripsAndReverts(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	embeddedCount := len(embeddedPerkPools[perkPoolKey("acolyte", unitPathCleric, unitRankBronze)])
	if embeddedCount == 0 {
		t.Fatalf("setup: acolyte/cleric/bronze has no embedded perks — catalog changed?")
	}

	entries := []perkEntryJSON{{ID: "test_perk", DisplayName: "Test Perk"}}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, entries); err != nil {
		t.Fatalf("SaveEditorPerkPool = %v, want nil", err)
	}

	def, ok := perkDefLookup("test_perk")
	if !ok {
		t.Fatal("perkDefLookup(test_perk) ok=false, want true")
	}
	if def.UnitType != "acolyte" || def.Path != unitPathCleric || def.Rank != unitRankBronze {
		t.Errorf("def = %+v, want UnitType=acolyte Path=cleric Rank=bronze", def)
	}
	// Whole-pool replace: only the overlay's one entry should be visible now.
	if got := countPerkDefsAt("acolyte", unitPathCleric, unitRankBronze); got != 1 {
		t.Errorf("countPerkDefsAt(acolyte,cleric,bronze) = %d, want 1 (overlay replaces embedded)", got)
	}

	existed, err := DeleteEditorPerkPool("acolyte", unitPathCleric, unitRankBronze)
	if err != nil {
		t.Fatalf("DeleteEditorPerkPool = %v, want nil", err)
	}
	if !existed {
		t.Errorf("existed = false, want true")
	}
	if _, ok := perkDefLookup("test_perk"); ok {
		t.Errorf("perkDefLookup(test_perk) ok=true after delete, want false")
	}
	if got := countPerkDefsAt("acolyte", unitPathCleric, unitRankBronze); got != embeddedCount {
		t.Errorf("countPerkDefsAt after delete = %d, want %d (embedded baseline restored)", got, embeddedCount)
	}
}

func TestSaveEditorPerkPool_OverlayReplacesEmbeddedPool_ThenDeleteReverts(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	embeddedCount := len(embeddedPerkPools[perkPoolKey("acolyte", unitPathCleric, unitRankBronze)])
	if embeddedCount == 0 {
		t.Fatalf("setup: acolyte/cleric/bronze has no embedded perks")
	}

	overlay := []perkEntryJSON{{ID: "overlay_only_perk"}}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, overlay); err != nil {
		t.Fatalf("SaveEditorPerkPool(overlay) = %v, want nil", err)
	}
	if _, ok := perkDefLookup("overlay_only_perk"); !ok {
		t.Fatal("perkDefLookup(overlay_only_perk) ok=false, want true")
	}
	if got := countPerkDefsAt("acolyte", unitPathCleric, unitRankBronze); got != 1 {
		t.Errorf("count after overlay = %d, want 1", got)
	}

	existed, err := DeleteEditorPerkPool("acolyte", unitPathCleric, unitRankBronze)
	if err != nil {
		t.Fatalf("DeleteEditorPerkPool = %v, want nil", err)
	}
	if !existed {
		t.Errorf("existed = false, want true")
	}
	if _, ok := perkDefLookup("overlay_only_perk"); ok {
		t.Errorf("overlay_only_perk still resolvable after delete")
	}
	if got := countPerkDefsAt("acolyte", unitPathCleric, unitRankBronze); got != embeddedCount {
		t.Errorf("count after delete = %d, want %d (embedded restored)", got, embeddedCount)
	}
}

func TestSaveEditorPerkPool_ResaveSameLocation_Succeeds(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	entries := []perkEntryJSON{{ID: "resave_perk", DisplayName: "Original"}}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, entries); err != nil {
		t.Fatalf("first save = %v, want nil", err)
	}
	edited := []perkEntryJSON{{ID: "resave_perk", DisplayName: "Edited"}}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, edited); err != nil {
		t.Fatalf("re-save at same location = %v, want nil", err)
	}
	def, ok := perkDefLookup("resave_perk")
	if !ok || def.DisplayName != "Edited" {
		t.Errorf("def = %+v (ok=%v), want DisplayName=Edited", def, ok)
	}
}

func TestSaveEditorPerkPool_DuplicateIDAcrossLocations_Rejected(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	entries := []perkEntryJSON{{ID: "shared_perk_id"}}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, entries); err != nil {
		t.Fatalf("setup SaveEditorPerkPool = %v, want nil", err)
	}

	elsewhere := []perkEntryJSON{{ID: "shared_perk_id"}}
	err := SaveEditorPerkPool("acolyte", unitPathSiphoner, unitRankBronze, elsewhere)
	if err == nil {
		t.Fatal("SaveEditorPerkPool(dup id at different location) = nil, want rejection")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "shared_perk_id") || !strings.Contains(err.Error(), "acolyte/cleric/bronze") {
		t.Errorf("err = %q, want it to name the id and the other owner location", err.Error())
	}
}

func TestSaveEditorPerkPool_DuplicateIDWithinArray_Rejected(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	entries := []perkEntryJSON{{ID: "dup_in_array"}, {ID: "dup_in_array"}}
	err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, entries)
	if err == nil {
		t.Fatal("SaveEditorPerkPool(dup within array) = nil, want rejection")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "dup_in_array") {
		t.Errorf("err = %q, want it to name the duplicated id", err.Error())
	}
}

func TestSaveEditorPerkPool_EmptyPool_ValidNoPerksGranted(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankSilver, []perkEntryJSON{}); err != nil {
		t.Fatalf("SaveEditorPerkPool(empty pool) = %v, want nil", err)
	}
	if got := countPerkDefsAt("acolyte", unitPathCleric, unitRankSilver); got != 0 {
		t.Errorf("count = %d, want 0 for an explicitly-empty overlay pool", got)
	}

	// Exercising the actual rank-up filter must not panic and must return
	// nothing eligible from this now-empty pool.
	unit := &Unit{UnitType: "acolyte", ProgressionPath: unitPathCleric}
	eligible := eligiblePerksForUnitAtRank(unit, unitRankSilver)
	for _, def := range eligible {
		if def.Path == unitPathCleric && def.Rank == unitRankSilver {
			t.Errorf("eligiblePerksForUnitAtRank still returned %q from an emptied pool", def.ID)
		}
	}
}

func TestSaveEditorPerkPool_RejectsBadIdentifiers(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	entries := []perkEntryJSON{{ID: "ok_id"}}
	if err := SaveEditorPerkPool("../escape", unitPathCleric, unitRankBronze, entries); err == nil {
		t.Error("SaveEditorPerkPool(bad unit type) = nil, want error")
	} else if !IsEditorValidationError(err) {
		t.Errorf("bad unit type err = %v, want editorValidationError", err)
	}
	if err := SaveEditorPerkPool("acolyte", "../escape", unitRankBronze, entries); err == nil {
		t.Error("SaveEditorPerkPool(bad path id) = nil, want error")
	}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, "platinum", entries); err == nil {
		t.Error("SaveEditorPerkPool(bad rank) = nil, want error")
	}
	badEntries := []perkEntryJSON{{ID: "../escape"}}
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, badEntries); err == nil {
		t.Error("SaveEditorPerkPool(bad entry id) = nil, want error")
	}
}

// TestSaveEditorPerkPool_NonexistentPath_Rejected pins the editor-only
// ordering guard (a perk pool must be authored against a path that already
// exists on the unit): saving perks for a path id that was never created
// via SaveEditorPath/SavePathDef must be rejected, not silently write an
// orphaned .../paths/<pathName>/perks/<rank>.json that nothing references.
// This check is deliberately NOT in validatePerkPoolEntries (which
// LoadPersistedPerksIntoOverlay also calls, and which must stay tolerant of
// a perks/ dir surviving on disk without its sibling path file).
func TestSaveEditorPerkPool_NonexistentPath_Rejected(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	entries := []perkEntryJSON{{ID: "orphan_perk"}}
	err := SaveEditorPerkPool("acolyte", "not_a_real_path_xyz", unitRankBronze, entries)
	if err == nil {
		t.Fatal("SaveEditorPerkPool(nonexistent path) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "not_a_real_path_xyz") || !strings.Contains(err.Error(), "acolyte") {
		t.Errorf("err = %q, want it to name both the path and the unit", err.Error())
	}
	if _, ok := perkDefLookup("orphan_perk"); ok {
		t.Errorf("orphan_perk resolvable after a rejected save — must be a no-op")
	}
}

func TestSaveEditorPerkPool_WritesFileToDisk_AndDeleteRemovesOnlyThatRankFile(t *testing.T) {
	dir := withIsolatedPerkCatalogDir(t)
	unitDef, ok := getUnitDef("acolyte")
	if !ok {
		t.Fatalf("setup: getUnitDef(acolyte) not found")
	}

	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankGold, []perkEntryJSON{{ID: "disk_check_perk"}}); err != nil {
		t.Fatalf("SaveEditorPerkPool = %v, want nil", err)
	}
	wantFile := filepath.Join(dir, unitDef.Faction, "acolyte", "paths", unitPathCleric, "perks", "gold.json")
	if _, err := os.Stat(wantFile); err != nil {
		t.Fatalf("expected file at %s, stat error: %v", wantFile, err)
	}

	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankSilver, []perkEntryJSON{{ID: "sibling_perk"}}); err != nil {
		t.Fatalf("SaveEditorPerkPool(sibling) = %v, want nil", err)
	}
	siblingFile := filepath.Join(dir, unitDef.Faction, "acolyte", "paths", unitPathCleric, "perks", "silver.json")

	if _, err := DeleteEditorPerkPool("acolyte", unitPathCleric, unitRankGold); err != nil {
		t.Fatalf("DeleteEditorPerkPool = %v, want nil", err)
	}
	if _, err := os.Stat(wantFile); !os.IsNotExist(err) {
		t.Errorf("expected %s removed, stat err = %v", wantFile, err)
	}
	if _, err := os.Stat(siblingFile); err != nil {
		t.Errorf("sibling rank file %s should still exist, stat err = %v", siblingFile, err)
	}
}

func TestLoadPersistedPerksIntoOverlay_PicksUpFileWrittenDirectlyToDisk(t *testing.T) {
	dir := withIsolatedPerkCatalogDir(t)
	unitDef, ok := getUnitDef("acolyte")
	if !ok {
		t.Fatalf("setup: getUnitDef(acolyte) not found")
	}

	outDir := filepath.Join(dir, unitDef.Faction, "acolyte", "paths", unitPathCleric, "perks")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw, err := json.MarshalIndent([]perkEntryJSON{{ID: "loaded_perk"}}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "gold.json"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, ok := perkDefLookup("loaded_perk"); ok {
		t.Fatalf("setup: loaded_perk already resolvable before Load")
	}

	LoadPersistedPerksIntoOverlay()

	def, ok := perkDefLookup("loaded_perk")
	if !ok {
		t.Fatal("perkDefLookup(loaded_perk) ok=false after Load, want true")
	}
	if def.UnitType != "acolyte" || def.Path != unitPathCleric || def.Rank != unitRankGold {
		t.Errorf("def = %+v, want UnitType=acolyte Path=cleric Rank=gold", def)
	}
}

func TestPerkPoolIsEmbedded_KnownEmbeddedAndUnknown(t *testing.T) {
	if !PerkPoolIsEmbedded("acolyte", unitPathCleric, unitRankBronze) {
		t.Errorf("PerkPoolIsEmbedded(acolyte,cleric,bronze) = false, want true")
	}
	if PerkPoolIsEmbedded("acolyte", unitPathCleric, "not_a_real_rank") {
		t.Errorf("PerkPoolIsEmbedded(bad rank) = true, want false")
	}
}

func TestPerkDefAccessors_ConcurrentReadsDoNotRace(t *testing.T) {
	const goroutines = 8
	const iterations = 200

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < iterations; j++ {
				_, _ = perkDefLookup("battle_prayer")
				_ = snapshotPerkDefs()
				_ = perkDefByID("battle_prayer")
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}
