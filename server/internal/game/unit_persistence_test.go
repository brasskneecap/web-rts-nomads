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

func TestSaveUnitDef_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{Type: "../evil", Faction: "human", HP: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1}
	if err := SaveUnitDef(&def); err == nil {
		t.Fatal("expected bad-id rejection")
	}
}
