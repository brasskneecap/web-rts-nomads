package game

import (
	"strings"
	"testing"
)

// baseUnitDefForPathChancesTest returns a copy of the real acolyte UnitDef
// (which already has a valid pathChances: {"cleric":1,"siphoner":1}) so
// each test only has to touch PathChances and every OTHER validateUnitDef
// invariant (hp>0, moveSpeed>0, attackRange/attackSpeed when damage>0, ...)
// stays satisfied for free.
func baseUnitDefForPathChancesTest(t *testing.T) UnitDef {
	t.Helper()
	def, ok := getUnitDef("acolyte")
	if !ok {
		t.Fatalf("setup: getUnitDef(acolyte) not found")
	}
	return def
}

// clearUnitOverlayForTest removes overlay entries for the given unit types.
// SaveEditorUnit in these tests always re-saves "acolyte" with a modified
// PathChances; without this cleanup the modified acolyte would leak into
// every other test in the package that depends on the real acolyte
// pathChances (cleric/siphoner) or its other fields.
func clearUnitOverlayForTest(t *testing.T, unitTypes ...string) {
	t.Helper()
	runtimeUnitsMu.Lock()
	for _, ut := range unitTypes {
		delete(runtimeUnits, ut)
	}
	runtimeUnitsMu.Unlock()
}

// ─── validateUnitPathChances: direct unit tests (pin the exact messages) ────

func TestValidateUnitPathChances_DanglingReference_NamesPathAndUnit(t *testing.T) {
	def := baseUnitDefForPathChancesTest(t)
	def.PathChances = map[string]float64{"nope_xyz": 1}

	err := validateUnitPathChances(&def)
	if err == nil {
		t.Fatal("validateUnitPathChances(dangling reference) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "nope_xyz") || !strings.Contains(err.Error(), def.Type) {
		t.Errorf("err = %q, want it to name both %q and %q", err.Error(), "nope_xyz", def.Type)
	}
}

func TestValidateUnitPathChances_PathWithNoRankCurve_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	barren := validClericShapedPathFile("barren_path_direct")
	barren.Ranks = nil // validatePathFile allows this; using it is what's rejected
	if err := SaveEditorPath("acolyte", barren); err != nil {
		t.Fatalf("setup: SaveEditorPath(barren_path_direct) = %v, want nil", err)
	}

	def := baseUnitDefForPathChancesTest(t)
	def.PathChances = map[string]float64{"barren_path_direct": 1}

	err := validateUnitPathChances(&def)
	if err == nil {
		t.Fatal("validateUnitPathChances(no rank curve) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "no rank curve") {
		t.Errorf("err = %q, want it to mention \"no rank curve\"", err.Error())
	}
}

func TestValidateUnitPathChances_NegativeWeight_Rejected(t *testing.T) {
	def := baseUnitDefForPathChancesTest(t)
	def.PathChances = map[string]float64{unitPathCleric: -0.5}

	err := validateUnitPathChances(&def)
	if err == nil {
		t.Fatal("validateUnitPathChances(negative weight) = nil, want error")
	}
	if !strings.Contains(err.Error(), "negative weight") {
		t.Errorf("err = %q, want it to mention negative weight", err.Error())
	}
}

func TestValidateUnitPathChances_WeightsSumToZero_Rejected(t *testing.T) {
	def := baseUnitDefForPathChancesTest(t)
	def.PathChances = map[string]float64{unitPathCleric: 0, unitPathSiphoner: 0}

	err := validateUnitPathChances(&def)
	if err == nil {
		t.Fatal("validateUnitPathChances(sum to zero) = nil, want error")
	}
	if !strings.Contains(err.Error(), "sum to more than 0") {
		t.Errorf("err = %q, want it to mention summing to more than 0", err.Error())
	}
}

func TestValidateUnitPathChances_ValidPathChances_ReturnsNil(t *testing.T) {
	def := baseUnitDefForPathChancesTest(t)
	def.PathChances = map[string]float64{unitPathCleric: 1, unitPathSiphoner: 1}

	if err := validateUnitPathChances(&def); err != nil {
		t.Errorf("validateUnitPathChances(valid) = %v, want nil", err)
	}
}

// ─── SaveEditorUnit wiring ───────────────────────────────────────────────

func TestSaveEditorUnit_PathChancesReferencesNonexistentPath_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)
	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })

	unit.PathChances = map[string]float64{"totally_made_up_path_xyz": 1}
	err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit})
	if err == nil {
		t.Fatal("SaveEditorUnit(dangling pathChances) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "totally_made_up_path_xyz") || !strings.Contains(err.Error(), unit.Type) {
		t.Errorf("err = %q, want it to name both the path and the unit", err.Error())
	}
}

func TestSaveEditorUnit_PathWithNoRankCurve_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	barren := validClericShapedPathFile("barren_path")
	barren.Ranks = nil
	if err := SaveEditorPath("acolyte", barren); err != nil {
		t.Fatalf("setup: SaveEditorPath(barren_path) = %v, want nil", err)
	}

	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })
	unit.PathChances = map[string]float64{"barren_path": 1}

	err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit})
	if err == nil {
		t.Fatal("SaveEditorUnit(no-rank-curve path) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "no rank curve") {
		t.Errorf("err = %q, want it to mention \"no rank curve\"", err.Error())
	}
}

func TestSaveEditorUnit_NegativeWeight_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)
	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })

	unit.PathChances = map[string]float64{unitPathCleric: -1}
	err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit})
	if err == nil {
		t.Fatal("SaveEditorUnit(negative weight) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
}

func TestSaveEditorUnit_PathChancesWeightsSumToZero_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)
	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })

	unit.PathChances = map[string]float64{unitPathCleric: 0, unitPathSiphoner: 0}
	err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit})
	if err == nil {
		t.Fatal("SaveEditorUnit(weights sum to 0) = nil, want error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
}

func TestSaveEditorUnit_ValidPathChances_Succeeds(t *testing.T) {
	withIsolatedPathCatalogDir(t)
	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })

	unit.PathChances = map[string]float64{unitPathCleric: 1, unitPathSiphoner: 1}
	if err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit}); err != nil {
		t.Fatalf("SaveEditorUnit(valid pathChances) = %v, want nil", err)
	}
}

// ─── SaveEditorPath: single-owner guard ──────────────────────────────────
//
// A path id must belong to exactly one unit. Re-parenting it silently (by
// saving the same path id under a different unit type) would orphan any
// existing pathChances reference on the ORIGINAL owner at runtime — the
// overlay-wins rebuild would move the path to the new unit's
// pathsByUnitType and drop it from the old one out from under a live
// reference, without ever tripping validateUnitPathChances (that check only
// runs when the unit itself is re-saved).

func TestSaveEditorPath_ReparentToDifferentUnit_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	zealot := validClericShapedPathFile("zealot")
	if err := SaveEditorPath("acolyte", zealot); err != nil {
		t.Fatalf("setup: SaveEditorPath(acolyte, zealot) = %v, want nil", err)
	}

	reparented := validClericShapedPathFile("zealot")
	err := SaveEditorPath("adept", reparented)
	if err == nil {
		t.Fatal("SaveEditorPath(adept, zealot) = nil, want rejection — zealot already belongs to acolyte")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "acolyte") || !strings.Contains(err.Error(), "adept") || !strings.Contains(err.Error(), "zealot") {
		t.Errorf("err = %q, want it to name the path and both units", err.Error())
	}

	// Rejected save must be a no-op: zealot still belongs to acolyte.
	owner, ok := resolvePathOwningUnit("zealot")
	if !ok || owner != "acolyte" {
		t.Errorf("resolvePathOwningUnit(zealot) = (%q,%v), want (\"acolyte\", true) — rejection must not have re-parented it", owner, ok)
	}
}

func TestSaveEditorPath_ResaveUnderSameOwner_Succeeds(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	zealot := validClericShapedPathFile("zealot")
	if err := SaveEditorPath("acolyte", zealot); err != nil {
		t.Fatalf("SaveEditorPath(acolyte, zealot) first save = %v, want nil", err)
	}

	edited := validClericShapedPathFile("zealot")
	edited.VisionRange = 999
	if err := SaveEditorPath("acolyte", edited); err != nil {
		t.Fatalf("SaveEditorPath(acolyte, zealot) re-save under same owner = %v, want nil", err)
	}

	if got, ok := pathVisionRangeFor("zealot"); !ok || got != 999 {
		t.Errorf("pathVisionRangeFor(zealot) = (%v,%v), want (999,true) after re-save took effect", got, ok)
	}
}

func TestSaveEditorPath_EmbeddedPath_ReauthoredUnderOwnUnitSucceeds_ButOtherUnitRejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	// cleric is embedded and owned by acolyte — re-authoring it under its
	// own unit is a legitimate edit.
	ownReauthor := validClericShapedPathFile(unitPathCleric)
	ownReauthor.VisionRange = 777
	if err := SaveEditorPath("acolyte", ownReauthor); err != nil {
		t.Fatalf("SaveEditorPath(acolyte, cleric) re-author under own unit = %v, want nil", err)
	}
	if got, ok := pathVisionRangeFor(unitPathCleric); !ok || got != 777 {
		t.Errorf("pathVisionRangeFor(cleric) = (%v,%v), want (777,true)", got, ok)
	}

	// Saving the SAME embedded path id under a different unit must be
	// rejected — cleric belongs to acolyte, not adept.
	elsewhere := validClericShapedPathFile(unitPathCleric)
	err := SaveEditorPath("adept", elsewhere)
	if err == nil {
		t.Fatal("SaveEditorPath(adept, cleric) = nil, want rejection — cleric already belongs to acolyte")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "acolyte") || !strings.Contains(err.Error(), "adept") {
		t.Errorf("err = %q, want it to name both units", err.Error())
	}
}

// ─── DeleteEditorPath: reference guard ──────────────────────────────────

func TestDeleteEditorPath_PathReferencedByUnit_Rejected(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	zealot := validClericShapedPathFile("zealot")
	if err := SaveEditorPath("acolyte", zealot); err != nil {
		t.Fatalf("setup: SaveEditorPath(zealot) = %v, want nil", err)
	}

	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })
	unit.PathChances = map[string]float64{"zealot": 1}
	if err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit}); err != nil {
		t.Fatalf("setup: SaveEditorUnit(acolyte referencing zealot) = %v, want nil", err)
	}

	existed, err := DeleteEditorPath("zealot")
	if err == nil {
		t.Fatal("DeleteEditorPath(referenced path) = nil error, want rejection")
	}
	if existed {
		t.Errorf("DeleteEditorPath(referenced path) existed = true, want false (nothing deleted)")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if !strings.Contains(err.Error(), "zealot") || !strings.Contains(err.Error(), "acolyte") {
		t.Errorf("err = %q, want it to name both the path and the referencing unit", err.Error())
	}

	// The rejected delete must have had no effect at all.
	if _, ok := pathModifierLookup(pathModifierKey("zealot", unitRankBronze)); !ok {
		t.Errorf("zealot/bronze no longer resolvable after a REJECTED delete — rejection must be a no-op")
	}
}

// ─── THE boot-panic proof ────────────────────────────────────────────────

// TestBootPanicProof_EditorDrivenCatalogNeverFailsPathChancesCrossValidation
// drives the exact editor-shaped sequence a real author would: add a path,
// point a unit's pathChances at it, then try (and fail) to delete the path
// while it's still referenced. It then re-runs the boot-time pathChances
// cross-validation (the same check that PANICS in path_defs.go's init())
// as a non-panicking function and asserts it finds nothing to reject. This
// is the actual proof of spec §9.1 — the per-rule rejection tests above are
// what keep this one green.
func TestBootPanicProof_EditorDrivenCatalogNeverFailsPathChancesCrossValidation(t *testing.T) {
	withIsolatedPathCatalogDir(t)

	zealot := validClericShapedPathFile("zealot")
	if err := SaveEditorPath("acolyte", zealot); err != nil {
		t.Fatalf("SaveEditorPath(zealot) = %v, want nil", err)
	}

	unit := baseUnitDefForPathChancesTest(t)
	t.Cleanup(func() { clearUnitOverlayForTest(t, unit.Type) })
	unit.PathChances = map[string]float64{"zealot": 1}
	if err := SaveEditorUnit(EditorUnitSaveRequest{Unit: unit}); err != nil {
		t.Fatalf("SaveEditorUnit(acolyte referencing zealot) = %v, want nil", err)
	}

	// The dangerous operation: delete the still-referenced path. MUST be
	// rejected — if it weren't, the very next boot would panic.
	if _, err := DeleteEditorPath("zealot"); err == nil {
		t.Fatal("DeleteEditorPath(zealot, still referenced) = nil, want rejection")
	}

	// Simulate a fresh boot's pathChances cross-validation over the
	// resulting merged catalog. Must find nothing to reject.
	if err := validateAllUnitPathChances(); err != nil {
		t.Fatalf("validateAllUnitPathChances() = %v, want nil — this catalog would panic init() at boot", err)
	}
}
