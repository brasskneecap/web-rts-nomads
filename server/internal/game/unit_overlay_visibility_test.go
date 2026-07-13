package game

import "testing"

// TestUnitOverlayVisibleToGetUnitDef guards against the same class of bug
// that once shipped for the item catalog: GameState.itemCatalog snapshotted
// the embed-only singleton at match creation, so editor-saved items never
// became visible to gameplay. This test proves an overlay edit saved via
// SaveUnitDef is immediately visible through getUnitDef — the exact accessor
// the unit spawn path (spawnPlayerUnitLocked in state_spawn.go) reads live on
// every spawn call, and the accessor applyAdvancementsToEffectiveDefsLocked
// (advancement_defs.go) reads when building player.EffectiveUnitDefs at match
// start. Neither path snapshots from an embed-only map, so a single
// getUnitDef-level assertion covers both.
func TestUnitOverlayVisibleToGetUnitDef(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	base, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer must exist")
	}
	edited := base
	edited.HP = base.HP + 12345
	if err := SaveUnitDef(&edited); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := getUnitDef("archer")
	if got.HP != base.HP+12345 {
		t.Fatalf("overlay edit not visible via getUnitDef: HP=%d", got.HP)
	}
	_, _ = DeleteUnitOverride("archer")
}
