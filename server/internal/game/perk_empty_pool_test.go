package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestAssignUnitPerkLocked_EmptyPerkPool_NoPanicNoGrant pins the Intn(0)
// guard at perks.go:942 (assignUnitPerkLocked: `if len(pool)==0 { return }`)
// and the equivalent guard in maybeAssignExtraPerkLocked (perks.go:975-977).
// An editor-created path can save a rank's perk pool as an empty array
// (SaveEditorPerkPool allows this — see perk_persistence_test.go's
// TestSaveEditorPerkPool_EmptyPool_ValidNoPerksGranted). A unit promoted
// into that (unit,path,rank) combination must not crash the server on its
// first rank-up: rand.Intn(0) panics, so if either guard above were ever
// removed by a future refactor, this test must catch it.
func TestAssignUnitPerkLocked_EmptyPerkPool_NoPanicNoGrant(t *testing.T) {
	withIsolatedPerkCatalogDir(t)

	// Overlay acolyte/cleric/bronze with an explicitly empty pool — the
	// embedded cleric bronze pool has 4 perks (sanctuary, battle_prayer,
	// bolstering_prayer, mana_conduit); the overlay wholly replaces it.
	if err := SaveEditorPerkPool("acolyte", unitPathCleric, unitRankBronze, []perkEntryJSON{}); err != nil {
		t.Fatalf("setup: SaveEditorPerkPool(empty pool) = %v, want nil", err)
	}
	if got := countPerkDefsAt("acolyte", unitPathCleric, unitRankBronze); got != 0 {
		t.Fatalf("setup: acolyte/cleric/bronze pool has %d perks, want 0 (overlay should have replaced it)", got)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	s.mu.Lock()
	defer s.mu.Unlock()

	unit := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	if unit == nil {
		t.Fatal("spawnPlayerUnitLocked returned nil")
	}
	unit.ProgressionPath = unitPathCleric
	unit.Rank = unitRankBronze

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("assignUnitPerkLocked panicked on an empty perk pool: %v", r)
			}
		}()
		s.assignUnitPerkLocked(unit)
	}()

	if len(unit.PerkIDs) != 0 {
		t.Errorf("PerkIDs = %v after assignUnitPerkLocked on an empty pool, want empty", unit.PerkIDs)
	}

	// maybeAssignExtraPerkLocked only reaches its own len(pool)==0 guard when
	// the owner has an ExtraPerkSlots entry for this unit type/rank — set
	// that up directly (bypassing the advancement-purchase flow, which is
	// exercised elsewhere) so this test actually reaches the pool re-query
	// rather than short-circuiting on the "no extra slot" branch first.
	s.Players["p1"].ExtraPerkSlots = map[string]map[string]bool{
		"acolyte": {unitRankBronze: true},
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("maybeAssignExtraPerkLocked panicked on an empty perk pool: %v", r)
			}
		}()
		s.maybeAssignExtraPerkLocked(unit)
	}()

	if len(unit.PerkIDs) != 0 {
		t.Errorf("PerkIDs = %v after maybeAssignExtraPerkLocked on an empty pool, want still empty", unit.PerkIDs)
	}
}
