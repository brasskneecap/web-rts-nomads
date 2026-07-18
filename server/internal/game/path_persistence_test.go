package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// assertJSONEqual compares two json.RawMessage blobs by decoded value
// (not raw bytes) — robust against whitespace/key-order differences a
// marshal/unmarshal round trip can introduce.
func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var gotVal, wantVal any
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("unmarshal got: %v (%s)", err, got)
	}
	if err := json.Unmarshal(want, &wantVal); err != nil {
		t.Fatalf("unmarshal want: %v (%s)", err, want)
	}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Errorf("JSON mismatch: got %s, want %s", got, want)
	}
}

// clearPathOverlayForTest resets the runtime path overlay to empty and
// rebuilds the derived maps back to the pure-embedded baseline. The overlay
// maps (runtimePaths / runtimePathUnit) and the derived maps they feed are
// process-global, so every test that calls SavePathDef / DeletePathOverride
// / LoadPersistedPathsIntoOverlay MUST register this via t.Cleanup — without
// it, one test's overlay state leaks into the next test (and into the rest
// of the package's test suite, since these maps back live game logic).
func clearPathOverlayForTest(t *testing.T) {
	t.Helper()
	runtimePathsMu.Lock()
	for k := range runtimePaths {
		delete(runtimePaths, k)
	}
	for k := range runtimePathUnit {
		delete(runtimePathUnit, k)
	}
	runtimePathsMu.Unlock()
	rebuildDerivedPathMaps()
}

// withIsolatedPathCatalogDir points UNIT_CATALOG_DIR at a fresh t.TempDir()
// so Save/Delete/Load in this test never touch the real source catalog, and
// registers cleanup of the overlay so state never leaks to other tests. The
// embedded baseline (embeddedPathFiles/embeddedPathUnit) is unaffected by
// this env var — it was captured once from the real go:embed data at
// process init — so tests still see the real cleric/siphoner/etc. paths
// alongside whatever this test saves into the isolated overlay dir.
func withIsolatedPathCatalogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() { clearPathOverlayForTest(t) })
	return dir
}

func TestSavePathDef_NewPathUnderAcolyte_RoundTripsWithoutErasingEmbedded(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	file := validClericShapedPathFile("zealot")
	if err := SavePathDef("acolyte", file); err != nil {
		t.Fatalf("SavePathDef(zealot) = %v, want nil", err)
	}

	if _, ok := pathModifierLookup(pathModifierKey("zealot", unitRankBronze)); !ok {
		t.Errorf("pathModifierLookup(zealot/bronze) ok=false after SavePathDef, want true")
	}

	paths := pathsForUnitType("acolyte")
	for _, want := range []string{"zealot", unitPathCleric, unitPathSiphoner} {
		if !containsString(paths, want) {
			t.Errorf("pathsForUnitType(\"acolyte\") = %v, missing %q", paths, want)
		}
	}
}

// TestSavePathDef_MutatingCallersFileAfterSave_DoesNotCorruptOverlay pins
// Fix 2: SavePathDef must store a CLONE of the caller's *pathCatalogFile
// into runtimePaths, not the pointer itself (mirrors SavePerkPool's
// clonePerkEntries). If it stored the raw pointer, a caller mutating its
// own struct after the call returns would silently corrupt the overlay
// entry — and anything rebuilt from it — out from under any other reader.
func TestSavePathDef_MutatingCallersFileAfterSave_DoesNotCorruptOverlay(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	file := validClericShapedPathFile("zealot")
	if err := SavePathDef("acolyte", file); err != nil {
		t.Fatalf("SavePathDef(zealot) = %v, want nil", err)
	}

	// Mutate the caller's own copy after the save returned, then force
	// another rebuild (as an unrelated later Save/Delete would) so any
	// aliasing between the caller's struct and the overlay's stored entry
	// becomes observable — a rebuild always re-reads runtimePaths from
	// scratch, so a scalar field set once at save time would look "clean"
	// even with aliasing bugs present unless something re-derives it.
	file.VisionRange = 99999
	rebuildDerivedPathMaps()

	got, ok := pathVisionRangeFor("zealot")
	if !ok {
		t.Fatalf("pathVisionRangeFor(zealot) ok=false after mutating the caller's struct")
	}
	if got == 99999 {
		t.Errorf("pathVisionRangeFor(zealot) = %v — overlay was mutated by the caller's post-save edit; SavePathDef must store a clone", got)
	}
}

func TestSavePathDef_ExistingEmbeddedID_OverlayWinsThenDeleteReverts(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	// Baseline: the shipped cleric.json defines all three ranks and no
	// visionRange override. Read this from the map itself rather than
	// hardcoding, per project convention (no pinned balance numbers).
	embeddedVision, embeddedVisionOk := pathVisionRangeFor(unitPathCleric)
	if _, ok := pathModifierLookup(pathModifierKey(unitPathCleric, unitRankSilver)); !ok {
		t.Fatalf("setup: cleric/silver missing from embedded baseline — catalog changed?")
	}

	overrideVision := embeddedVision + 999 // guaranteed distinct from the baseline either way
	override := validClericShapedPathFile(unitPathCleric)
	override.VisionRange = overrideVision
	// Overlay is authored whole — only a bronze row, deliberately, to prove
	// the overlay REPLACES the embedded file rather than merging fields.

	if err := SavePathDef("acolyte", override); err != nil {
		t.Fatalf("SavePathDef(cleric override) = %v, want nil", err)
	}

	gotVision, gotOk := pathVisionRangeFor(unitPathCleric)
	if !gotOk || gotVision != overrideVision {
		t.Fatalf("pathVisionRangeFor(cleric) after overlay save = (%v,%v), want (%v,true)", gotVision, gotOk, overrideVision)
	}
	if _, ok := pathModifierLookup(pathModifierKey(unitPathCleric, unitRankSilver)); ok {
		t.Errorf("cleric/silver still resolvable after a bronze-only overlay save — overlay must replace the whole file, not merge")
	}

	existed, err := DeletePathOverride(unitPathCleric)
	if err != nil {
		t.Fatalf("DeletePathOverride(cleric) = %v, want nil", err)
	}
	if !existed {
		t.Errorf("DeletePathOverride(cleric) existed = false, want true (an overlay was present)")
	}

	revertedVision, revertedOk := pathVisionRangeFor(unitPathCleric)
	if revertedOk != embeddedVisionOk || revertedVision != embeddedVision {
		t.Errorf("pathVisionRangeFor(cleric) after delete = (%v,%v), want back to embedded (%v,%v)",
			revertedVision, revertedOk, embeddedVision, embeddedVisionOk)
	}
	if _, ok := pathModifierLookup(pathModifierKey(unitPathCleric, unitRankSilver)); !ok {
		t.Errorf("cleric/silver missing after DeletePathOverride — embedded baseline should be fully restored")
	}
}

func TestDeletePathOverride_OverlayOnlyPath_RemovedWithoutTouchingEmbedded(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	file := validClericShapedPathFile("zealot")
	if err := SavePathDef("acolyte", file); err != nil {
		t.Fatalf("SavePathDef(zealot) = %v, want nil", err)
	}
	if !containsString(pathsForUnitType("acolyte"), "zealot") {
		t.Fatalf("setup: pathsForUnitType(acolyte) missing zealot right after save")
	}

	existed, err := DeletePathOverride("zealot")
	if err != nil {
		t.Fatalf("DeletePathOverride(zealot) = %v, want nil", err)
	}
	if !existed {
		t.Errorf("DeletePathOverride(zealot) existed = false, want true")
	}

	paths := pathsForUnitType("acolyte")
	if containsString(paths, "zealot") {
		t.Errorf("pathsForUnitType(acolyte) = %v, still includes zealot after delete", paths)
	}
	for _, want := range []string{unitPathCleric, unitPathSiphoner} {
		if !containsString(paths, want) {
			t.Errorf("pathsForUnitType(acolyte) = %v, missing embedded path %q after deleting an unrelated overlay path", paths, want)
		}
	}
}

func TestSavePathDef_WritesFileToDisk_AndDeleteRemovesDirectory(t *testing.T) {
	dir := withIsolatedPathCatalogDir(t)

	unitDef, ok := getUnitDef("acolyte")
	if !ok {
		t.Fatalf("setup: getUnitDef(acolyte) not found")
	}

	file := validClericShapedPathFile("zealot")
	if err := SavePathDef("acolyte", file); err != nil {
		t.Fatalf("SavePathDef(zealot) = %v, want nil", err)
	}

	wantFile := filepath.Join(dir, unitDef.Faction, "acolyte", "paths", "zealot", "zealot.json")
	if _, err := os.Stat(wantFile); err != nil {
		t.Fatalf("expected saved file at %s, stat error: %v", wantFile, err)
	}

	wantDir := filepath.Join(dir, unitDef.Faction, "acolyte", "paths", "zealot")
	if _, err := DeletePathOverride("zealot"); err != nil {
		t.Fatalf("DeletePathOverride(zealot) = %v, want nil", err)
	}
	if _, err := os.Stat(wantDir); !os.IsNotExist(err) {
		t.Errorf("expected %s removed after delete, stat err = %v", wantDir, err)
	}
}

func TestLoadPersistedPathsIntoOverlay_PicksUpFileWrittenDirectlyToDisk(t *testing.T) {
	dir := withIsolatedPathCatalogDir(t)

	unitDef, ok := getUnitDef("acolyte")
	if !ok {
		t.Fatalf("setup: getUnitDef(acolyte) not found")
	}

	// Write straight to disk (bypassing SavePathDef) to isolate "does Load
	// pick up a pre-existing file" from "does Save also register it".
	file := validClericShapedPathFile("zealot")
	outDir := filepath.Join(dir, unitDef.Faction, "acolyte", "paths", "zealot")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "zealot.json"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, ok := pathModifierLookup(pathModifierKey("zealot", unitRankBronze)); ok {
		t.Fatalf("setup: zealot already resolvable before LoadPersistedPathsIntoOverlay ran")
	}

	LoadPersistedPathsIntoOverlay()

	if _, ok := pathModifierLookup(pathModifierKey("zealot", unitRankBronze)); !ok {
		t.Errorf("pathModifierLookup(zealot/bronze) ok=false after LoadPersistedPathsIntoOverlay, want true")
	}
	if !containsString(pathsForUnitType("acolyte"), "zealot") {
		t.Errorf("pathsForUnitType(acolyte) missing zealot after LoadPersistedPathsIntoOverlay")
	}
}

func TestPathIsEmbedded_KnownEmbeddedAndOverlayOnlyIDs(t *testing.T) {
	if !PathIsEmbedded(unitPathCleric) {
		t.Errorf("PathIsEmbedded(%q) = false, want true", unitPathCleric)
	}
	if PathIsEmbedded("not_a_real_path_xyz") {
		t.Errorf("PathIsEmbedded(unknown) = true, want false")
	}
}

func TestSavePathDef_RejectsBadUnitTypeAndPathID(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	file := validClericShapedPathFile("zealot")
	if err := SavePathDef("../escape", file); err == nil {
		t.Error("SavePathDef(traversal unit type) = nil, want error")
	}

	badFile := validClericShapedPathFile("../escape")
	if err := SavePathDef("acolyte", badFile); err == nil {
		t.Error("SavePathDef(traversal path id) = nil, want error")
	}

	invalidFile := validClericShapedPathFile("zealot")
	invalidFile.ProjectileScale = -1
	if err := SavePathDef("acolyte", invalidFile); err == nil {
		t.Error("SavePathDef(fails validatePathFile) = nil, want error")
	}
}

func TestDeletePathOverride_UnknownPathID_ReturnsNotExisted(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	existed, err := DeletePathOverride("never_saved_this_path_xyz")
	if err != nil {
		t.Fatalf("DeletePathOverride(unknown) = %v, want nil", err)
	}
	if existed {
		t.Errorf("DeletePathOverride(unknown) existed = true, want false")
	}
}

// TestSaveEditorPath_AttackOriginRoundTrips_ThenDeleteReverts covers the
// path-level attackOrigin passthrough end to end: pathAttackOriginFor,
// ListPathDefsFull's full def blob, and ListPathBounds' client-facing entry
// (the single field the game client actually fetches via GET
// /catalog/units) all carry the saved blob, and a delete removes it from
// every one of them.
func TestSaveEditorPath_AttackOriginRoundTrips_ThenDeleteReverts(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	originJSON := json.RawMessage(`{"default":{"x":0,"y":-30},"byFacing":{"east":{"x":14,"y":-28}}}`)
	file := validClericShapedPathFile("attack_origin_test_path")
	file.AttackOrigin = originJSON

	if err := SaveEditorPath("acolyte", file); err != nil {
		t.Fatalf("SaveEditorPath = %v, want nil", err)
	}

	got, ok := pathAttackOriginFor("attack_origin_test_path")
	if !ok {
		t.Fatal("pathAttackOriginFor ok=false after save, want true")
	}
	assertJSONEqual(t, got, originJSON)

	var foundInFull bool
	for _, e := range ListPathDefsFull() {
		if e.Path != "attack_origin_test_path" {
			continue
		}
		foundInFull = true
		var def struct {
			AttackOrigin json.RawMessage `json:"attackOrigin"`
		}
		if err := json.Unmarshal(e.Def, &def); err != nil {
			t.Fatalf("decode ListPathDefsFull entry.Def: %v", err)
		}
		assertJSONEqual(t, def.AttackOrigin, originJSON)
	}
	if !foundInFull {
		t.Fatalf("attack_origin_test_path not found in ListPathDefsFull()")
	}

	var foundInBounds bool
	for _, e := range ListPathBounds() {
		if e.Path != "attack_origin_test_path" {
			continue
		}
		foundInBounds = true
		assertJSONEqual(t, e.AttackOrigin, originJSON)
	}
	if !foundInBounds {
		t.Fatalf("attack_origin_test_path not found in ListPathBounds()")
	}

	existed, err := DeleteEditorPath("attack_origin_test_path")
	if err != nil {
		t.Fatalf("DeleteEditorPath = %v, want nil", err)
	}
	if !existed {
		t.Errorf("existed = false, want true")
	}
	if _, ok := pathAttackOriginFor("attack_origin_test_path"); ok {
		t.Errorf("pathAttackOriginFor still ok=true after delete")
	}
	for _, e := range ListPathBounds() {
		if e.Path == "attack_origin_test_path" {
			t.Errorf("attack_origin_test_path still present in ListPathBounds() after delete")
		}
	}
}

// TestListPathBounds_IncludesPathsWithOnlyAttackOrigin locks in the union
// semantics ListPathBounds needed to gain: before path-level attackOrigin
// existed, this function only ever iterated pathBoundsByPath, so a path
// declaring ONLY an attackOrigin (no bounds) would have been silently
// absent from the client's /catalog/units "paths" field.
func TestListPathBounds_IncludesPathsWithOnlyAttackOrigin(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	file := validClericShapedPathFile("origin_only_test_path")
	file.Bounds = nil
	file.AttackOrigin = json.RawMessage(`{"default":{"x":1,"y":2}}`)

	if err := SaveEditorPath("acolyte", file); err != nil {
		t.Fatalf("SaveEditorPath = %v, want nil", err)
	}

	var found bool
	for _, e := range ListPathBounds() {
		if e.Path == "origin_only_test_path" {
			found = true
			if len(e.AttackOrigin) == 0 {
				t.Errorf("entry.AttackOrigin empty, want the saved blob")
			}
		}
	}
	if !found {
		t.Fatalf("origin_only_test_path (bounds-less, attackOrigin-only) missing from ListPathBounds()")
	}
}

// TestPathPerksByRankRoundTripAndValidate covers PathDef.PerksByRank end to
// end: validatePathFile accepts a well-formed reference and rejects both an
// unknown rank key and an unknown perk id, and a saved path's refs are
// readable back via pathPerkRefsForRank (the reader Task 2's
// eligiblePerksForUnitAtRank will call during rank-up).
func TestPathPerksByRankRoundTripAndValidate(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	perkID := ListPerkDefs()[0].ID

	file := validClericShapedPathFile("zealot")
	file.PerksByRank = map[string][]string{unitRankBronze: {perkID}}
	if err := validatePathFile(file, "zealot"); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	if err := SavePathDef("acolyte", file); err != nil {
		t.Fatalf("SavePathDef: %v", err)
	}

	got := pathPerkRefsForRank("zealot", unitRankBronze)
	if len(got) != 1 || got[0] != perkID {
		t.Fatalf("pathPerkRefsForRank(zealot, bronze) = %v, want [%s]", got, perkID)
	}
	// Ranks not authored in PerksByRank stay nil (auto-match only).
	if got := pathPerkRefsForRank("zealot", unitRankSilver); got != nil {
		t.Fatalf("pathPerkRefsForRank(zealot, silver) = %v, want nil", got)
	}

	// bad rank key
	badRank := validClericShapedPathFile("zealot")
	badRank.PerksByRank = map[string][]string{"platinum": {perkID}}
	if err := validatePathFile(badRank, "zealot"); err == nil {
		t.Fatal("expected unknown-rank rejection")
	}

	// unknown perk id
	badPerk := validClericShapedPathFile("zealot")
	badPerk.PerksByRank = map[string][]string{unitRankBronze: {"no_such_perk_xyz"}}
	if err := validatePathFile(badPerk, "zealot"); err == nil {
		t.Fatal("expected unknown-perk rejection")
	}
}

// TestPathPerksByRank_SaveClonesMap pins the same "SavePathDef stores a
// clone, not the caller's map" contract the existing
// TestSavePathDef_MutatingCallersFileAfterSave_DoesNotCorruptOverlay test
// pins for the scalar fields — PerksByRank is a map, so a shallow struct
// copy alone would leave the overlay entry aliased to the caller's map.
func TestPathPerksByRank_SaveClonesMap(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	perkID := ListPerkDefs()[0].ID
	file := validClericShapedPathFile("zealot")
	file.PerksByRank = map[string][]string{unitRankBronze: {perkID}}
	if err := SavePathDef("acolyte", file); err != nil {
		t.Fatalf("SavePathDef: %v", err)
	}

	// Mutate the caller's own map after the save returned, then force
	// another rebuild so any aliasing becomes observable.
	file.PerksByRank[unitRankBronze][0] = "corrupted"
	rebuildDerivedPathMaps()

	got := pathPerkRefsForRank("zealot", unitRankBronze)
	if len(got) != 1 || got[0] != perkID {
		t.Errorf("pathPerkRefsForRank(zealot, bronze) = %v — overlay was mutated by the caller's post-save edit; SavePathDef must deep-clone PerksByRank", got)
	}
}
