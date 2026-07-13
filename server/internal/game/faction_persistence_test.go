package game

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A unit legally named "faction" must not take the faction records down with it
// when deleted — removeUnitOverrideFiles must only match <type>/<type>.json.
func TestDeleteUnitOverride_DoesNotEatFactionRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "faction")
		runtimeUnitsMu.Unlock()
	})

	// Hand-write a faction record where the registry would put one.
	factionDir := filepath.Join(dir, "test_faction")
	if err := os.MkdirAll(factionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(factionDir, "faction.json")
	if err := os.WriteFile(recordPath, []byte(`{"id":"test_faction","displayName":"Test Faction"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	unit := UnitDef{
		Type: "faction", Faction: "test_faction", Name: "Faction",
		HP: 10, MoveSpeed: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1,
	}
	if err := SaveUnitDef(&unit); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	if _, err := DeleteUnitOverride("faction"); err != nil {
		t.Fatalf("DeleteUnitOverride: %v", err)
	}
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("deleting a unit named %q destroyed the faction record: %v", "faction", err)
	}
}

// loadPersistedUnitsFromDir must skip faction.json outright — it is not a unit
// override and must not be logged as a skipped/malformed file on every boot.
func TestLoadPersistedUnitsFromDir_SkipsFactionRecord(t *testing.T) {
	dir := t.TempDir()

	factionDir := filepath.Join(dir, "test_faction")
	if err := os.MkdirAll(factionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factionDir, "faction.json"), []byte(`{"id":"test_faction","displayName":"Test Faction"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	unitDir := filepath.Join(dir, "test_faction", "footman")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unit := UnitDef{
		Type: "footman", Faction: "test_faction", Name: "Footman",
		HP: 10, MoveSpeed: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1,
	}
	raw, err := json.MarshalIndent(&unit, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "footman.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "footman")
		runtimeUnitsMu.Unlock()
	})

	n := loadPersistedUnitsFromDir(dir)
	if n != 1 {
		t.Fatalf("loadPersistedUnitsFromDir() loaded %d units, want 1 (faction.json must be skipped, not counted or warned on)", n)
	}
}

func TestSaveFactionDef_WritesRecordAndOverlays(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "night_elf")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "night_elf", DisplayName: "Night Elf", Order: 9}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "night_elf", "faction.json")); err != nil {
		t.Fatalf("expected faction record on disk: %v", err)
	}

	// An editor-created faction exists immediately, with zero units — that is
	// the whole point of the registry.
	var found bool
	for _, f := range ListFactions() {
		if f.ID == "night_elf" {
			found = true
			if f.DisplayName != "Night Elf" {
				t.Fatalf("displayName = %q, want %q", f.DisplayName, "Night Elf")
			}
		}
	}
	if !found {
		t.Fatal("saved faction is missing from ListFactions()")
	}
}

func TestSaveFactionDef_BlankDisplayNameIsTitleized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "sun_kin")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "sun_kin"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	for _, f := range ListFactions() {
		if f.ID == "sun_kin" && f.DisplayName != "Sun Kin" {
			t.Fatalf("displayName = %q, want titleized fallback %q", f.DisplayName, "Sun Kin")
		}
	}
}

func TestSaveFactionDef_RejectsBadID(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	def := FactionDef{ID: "../evil", DisplayName: "Evil"}
	if err := SaveFactionDef(&def); err == nil {
		t.Fatal("expected bad-id rejection")
	}
}

// Deleting a faction that still owns units would orphan them out of every
// filter — and in the dev tree, where the writable dir IS the source tree,
// it would be deleting real catalog content.
func TestDeleteFaction_RefusesWhileUnitsRemain(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	if len(FactionUnitTypes("human")) == 0 {
		t.Fatal("precondition: human must own units in the embedded catalog")
	}
	_, err := DeleteFactionOverride("human")
	if err == nil {
		t.Fatal("expected deletion of a populated faction to be refused")
	}
}

func TestDeleteFaction_RemovesEmptyFaction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := FactionDef{ID: "doomed", DisplayName: "Doomed"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	existed, err := DeleteFactionOverride("doomed")
	if err != nil || !existed {
		t.Fatalf("DeleteFactionOverride existed=%v err=%v", existed, err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "doomed")); !os.IsNotExist(serr) {
		t.Fatal("expected the empty faction directory to be removed")
	}
	for _, f := range ListFactions() {
		if f.ID == "doomed" {
			t.Fatal("deleted faction still listed")
		}
	}
}

// The user-facing message must not end with the sentinel's own text — that
// happens when the sentinel is wrapped with %w, and fmt.Errorf appends its
// Error() string to the message. The client renders this string verbatim in
// the editor panel, so a redundant, code-smelling tail is a real defect. The
// errors.Is classification (used by DeleteEditorFaction to pick 400 vs 500)
// must survive the fix.
func TestDeleteFactionOverride_ErrorMessageHasNoRedundantSentinelTail(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	_, err := DeleteFactionOverride("human")
	if err == nil {
		t.Fatal("expected deletion of a populated faction to be refused")
	}
	if strings.Contains(err.Error(), ": faction still has units") {
		t.Fatalf("error message still has the redundant sentinel tail: %q", err.Error())
	}
	if !errors.Is(err, errFactionHasUnits) {
		t.Fatalf("errors.Is(err, errFactionHasUnits) must still hold: %v", err)
	}
	if _, editorErr := DeleteEditorFaction("human"); !IsEditorValidationError(editorErr) {
		t.Fatalf("classification must survive the refactor: got %v", editorErr)
	}
}

// "Still has units" must reach the handler as a 400 validation error, not a 500.
func TestDeleteEditorFaction_PopulatedIsValidationError(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	_, err := DeleteEditorFaction("human")
	if err == nil || !IsEditorValidationError(err) {
		t.Fatalf("want editor validation error, got %v", err)
	}
}

// The on-disk emptiness check in DeleteFactionOverride is the ONLY thing
// standing between it and deleting content it doesn't own — no other test in
// this file fails if the plain, non-recursive os.Remove(factionDir) call were
// swapped for a recursive directory delete, because every other delete
// test's faction directory really is empty. This test plants content the
// in-memory unit registry cannot see, so the disk-emptiness check is
// exercised for real.
func TestDeleteFaction_NeverRemovesNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "haunted")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "haunted", DisplayName: "Haunted"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	// Content ListUnitDefs() cannot see. The on-disk emptiness check is the
	// ONLY guard here — if the delete call ever became recursive, this dies.
	stray := filepath.Join(dir, "haunted", "keepme")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "keepme.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := DeleteFactionOverride("haunted"); err != nil {
		t.Fatalf("DeleteFactionOverride: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stray, "keepme.json")); err != nil {
		t.Fatalf("DeleteFactionOverride destroyed content it does not own — the delete call must stay non-recursive: %v", err)
	}
}

// A failed record delete must NOT silently report success — otherwise the API
// answers 200 "deleted" while the file is still on disk, and the faction
// resurrects on the next boot via LoadPersistedFactionsIntoOverlay. The
// failure is forced by replacing faction.json with a non-empty directory of
// the same name: plain os.Remove refuses to remove a directory with children,
// which is a reliable cross-platform way to make the exact os.Remove call in
// DeleteFactionOverride fail without touching real permissions.
func TestDeleteFaction_RecordDeleteFailure_ReturnsErrorAndKeepsOverlay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "cursed")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "cursed", DisplayName: "Cursed"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}

	recordPath := filepath.Join(dir, "cursed", "faction.json")
	if err := os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recordPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordPath, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	existed, err := DeleteFactionOverride("cursed")
	if err == nil {
		t.Fatal("expected DeleteFactionOverride to surface the record-delete failure")
	}
	if existed {
		t.Fatal("existed must be false when the delete failed")
	}
	// The overlay entry must survive the failed delete — otherwise the faction
	// vanishes from ListFactions() in this process even though its record is
	// still on disk (and would resurrect on next boot regardless).
	runtimeFactionsMu.RLock()
	_, stillOverlaid := runtimeFactions["cursed"]
	runtimeFactionsMu.RUnlock()
	if !stillOverlaid {
		t.Fatal("overlay entry must not be dropped when the on-disk delete failed")
	}
}

// DeleteEditorFaction must NOT turn an infrastructure failure (the disk error
// exercised above) into a 400 validation error — only "still has units" is the
// caller's fault; everything else must pass through so the HTTP layer reports
// a 500 instead of telling the user they typed something wrong.
func TestDeleteEditorFaction_InfrastructureFailureIsNotValidationError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "veiled")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "veiled", DisplayName: "Veiled"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	recordPath := filepath.Join(dir, "veiled", "faction.json")
	if err := os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recordPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordPath, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := DeleteEditorFaction("veiled")
	if err == nil {
		t.Fatal("expected an error")
	}
	if IsEditorValidationError(err) {
		t.Fatal("an infrastructure failure must not be reported as a validation error")
	}
}
