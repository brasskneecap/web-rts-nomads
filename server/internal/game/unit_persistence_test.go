package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveUnitDef_OverlayWinsAndReverts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)

	// Override an existing embedded unit (archer) with a changed damage value.
	base, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer must exist in embed")
	}
	edited := base
	edited.Damage = base.Damage + 777
	if err := SaveUnitDef(&edited); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	got, _ := getUnitDef("archer")
	if got.Damage != base.Damage+777 {
		t.Fatalf("overlay did not win: got damage %d", got.Damage)
	}
	// File written under <faction>/<type>/<type>.json
	if _, err := os.Stat(filepath.Join(dir, "human", "archer", "archer.json")); err != nil {
		t.Fatalf("expected override file: %v", err)
	}
	// Delete reverts to embed.
	existed, err := DeleteUnitOverride("archer")
	if err != nil || !existed {
		t.Fatalf("DeleteUnitOverride existed=%v err=%v", existed, err)
	}
	reverted, _ := getUnitDef("archer")
	if reverted.Damage != base.Damage {
		t.Fatalf("did not revert: got damage %d want %d", reverted.Damage, base.Damage)
	}
}

func TestSaveUnitDef_LosslessArtBlobs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{
		Type: "art_unit", Faction: "human", Name: "Art", HP: 1, Damage: 1,
		AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1,
		Bounds: json.RawMessage(`{"w":42,"h":7}`),
		Shadow: json.RawMessage(`{"scale":0.5}`),
	}
	if err := SaveUnitDef(&def); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "human", "art_unit", "art_unit.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var round UnitDef
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(round.Bounds) == "" || string(round.Shadow) == "" {
		t.Fatalf("art blobs lost on round-trip: bounds=%q shadow=%q", round.Bounds, round.Shadow)
	}
}

// A plain re-save of a unit whose override directory contains ONLY its own
// <type>.json (a freshly-created unit — no paths/, sprites, advancements, etc.)
// must NOT delete it. Regression: SaveUnitDef MkdirAll'd the dir, then
// removeUnitOverrideFiles deleted the old file AND its now-empty dir, and the
// WriteFile that followed failed ENOENT — leaving the unit file-less, so it
// vanished on the next catalog reload. (Acolyte survived only because its dir
// had other files and so never went empty.)
func TestSaveUnitDef_ReSaveDoesNotDeleteFreshUnit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "spear_maiden")
		runtimeUnitsMu.Unlock()
	})
	def := UnitDef{
		Type: "spear_maiden", Faction: "raider", Name: "Spear Maiden",
		HP: 100, Damage: 10, AttackRange: 32, AttackSpeed: 1, MoveSpeed: 60,
	}
	if err := SaveUnitDef(&def); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// The editor re-saves the full def after any edit; this second save is the
	// one that regressed.
	if err := SaveUnitDef(&def); err != nil {
		t.Fatalf("re-save (regression): %v", err)
	}
	path := filepath.Join(dir, "raider", "spear_maiden", "spear_maiden.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("override file missing after re-save — unit was deleted: %v", err)
	}
}

// Deleting the last override for a type must not leave an orphaned, empty
// <type>/ directory behind — that skeleton then blocks the parent faction
// directory from ever being recognized as empty.
func TestDeleteUnitOverride_RemovesEmptyTypeDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "moon_dancer")
		runtimeUnitsMu.Unlock()
	})

	unit := UnitDef{
		Type: "moon_dancer", Faction: "night_elf", Name: "Moon Dancer",
		HP: 10, MoveSpeed: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1,
	}
	if err := SaveUnitDef(&unit); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	if _, err := DeleteUnitOverride("moon_dancer"); err != nil {
		t.Fatalf("DeleteUnitOverride: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "night_elf", "moon_dancer")); !os.IsNotExist(err) {
		t.Fatalf("expected orphaned unit type directory to be removed, stat err=%v", err)
	}
}

// The full lifecycle the E2E run actually caught: save faction, save a unit
// into it, delete the unit, then delete the faction. If the unit delete
// leaves an orphaned <type>/ directory behind, the faction directory is never
// empty and DeleteFactionOverride silently leaves it on disk despite
// reporting success.
func TestUnitAndFactionLifecycle_DeletingBothLeavesNoOrphanDirectories(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "night_elf")
		runtimeFactionsMu.Unlock()
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "moon_dancer")
		runtimeUnitsMu.Unlock()
	})

	faction := FactionDef{ID: "night_elf", DisplayName: "Night Elf"}
	if err := SaveFactionDef(&faction); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	unit := UnitDef{
		Type: "moon_dancer", Faction: "night_elf", Name: "Moon Dancer",
		HP: 10, MoveSpeed: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1,
	}
	if err := SaveUnitDef(&unit); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	if _, err := DeleteUnitOverride("moon_dancer"); err != nil {
		t.Fatalf("DeleteUnitOverride: %v", err)
	}
	if _, err := DeleteFactionOverride("night_elf"); err != nil {
		t.Fatalf("DeleteFactionOverride: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "night_elf")); !os.IsNotExist(err) {
		t.Fatalf("expected the faction directory to be fully removed, stat err=%v", err)
	}
}

// A unit directory that still owns other content (e.g. a paths/ subdir) must
// keep its directory when the unit json is deleted — proving the cleanup
// never destroys content it doesn't own.
func TestDeleteUnitOverride_KeepsTypeDirectoryWithOtherContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "ranger")
		runtimeUnitsMu.Unlock()
	})

	unit := UnitDef{
		Type: "ranger", Faction: "human", Name: "Ranger",
		HP: 10, MoveSpeed: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1,
	}
	if err := SaveUnitDef(&unit); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	pathsDir := filepath.Join(dir, "human", "ranger", "paths")
	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pathsDir, "p1.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := DeleteUnitOverride("ranger"); err != nil {
		t.Fatalf("DeleteUnitOverride: %v", err)
	}
	if _, err := os.Stat(pathsDir); err != nil {
		t.Fatalf("DeleteUnitOverride destroyed content it does not own — the directory must survive: %v", err)
	}
}

func TestSaveUnitDef_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{Type: "../evil", Faction: "human", HP: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1}
	if err := SaveUnitDef(&def); err == nil {
		t.Fatal("expected bad-id rejection")
	}
}

func TestSaveUnitDef_LosslessAttackOrigin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{
		Type: "origin_unit", Faction: "human", Name: "Origin", HP: 1, Damage: 1,
		AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1,
		AttackOrigin: json.RawMessage(`{"default":{"x":3,"y":-40},"byFacing":{"east":{"x":14,"y":-30}}}`),
	}
	if err := SaveUnitDef(&def); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "human", "origin_unit", "origin_unit.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var round UnitDef
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(round.AttackOrigin) == 0 {
		t.Fatal("attackOrigin lost on round-trip")
	}
	var got struct {
		Default struct{ X, Y float64 } `json:"default"`
	}
	if err := json.Unmarshal(round.AttackOrigin, &got); err != nil || got.Default.X != 3 || got.Default.Y != -40 {
		t.Fatalf("attackOrigin not preserved verbatim: %s", round.AttackOrigin)
	}
}
