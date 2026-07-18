package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestAssignUnitPerkLocked_EmptyPerkPool_NoPanicNoGrant pins the Intn(0)
// guard at perks.go (assignUnitPerkLocked: `if len(pool)==0 { return }`) and
// the equivalent guard in maybeAssignExtraPerkLocked. A unit whose
// (UnitType, ProgressionPath, Rank) matches no perk in the catalog yields an
// empty pool; rand.Intn(0) panics, so if either guard were ever removed by a
// future refactor, this test must catch it.
//
// Post standalone-perks flip there is no editor operation that empties an
// embedded unit/path/rank pool (perks are individual, id-addressed defs). We
// instead drive the guard with a synthetic ProgressionPath that no shipped
// perk targets, which is exactly the "no eligible perks" state the guard
// exists for.
func TestAssignUnitPerkLocked_EmptyPerkPool_NoPanicNoGrant(t *testing.T) {
	const emptyPath = "no_perks_test_path"

	// Sanity: no catalog perk targets this synthetic path, so the pool is
	// genuinely empty for a unit on it.
	if got := countPerkDefsAt("acolyte", emptyPath, unitRankBronze); got != 0 {
		t.Fatalf("setup: %d perks target acolyte/%s/bronze, want 0", got, emptyPath)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	s.mu.Lock()
	defer s.mu.Unlock()

	unit := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	if unit == nil {
		t.Fatal("spawnPlayerUnitLocked returned nil")
	}
	unit.ProgressionPath = emptyPath
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
	// the owner has an ExtraPerkSlots entry for this unit type/rank — set that
	// up directly (bypassing the advancement-purchase flow, which is exercised
	// elsewhere) so this test actually reaches the pool re-query rather than
	// short-circuiting on the "no extra slot" branch first.
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

// countPerkDefsAt returns how many defs in the current registry resolve to the
// given (unitType, pathName, rank), read from the registry itself.
func countPerkDefsAt(unitType, pathName, rank string) int {
	n := 0
	for _, def := range snapshotPerkDefs() {
		if def.UnitType == unitType && def.Path == pathName && def.Rank == rank {
			n++
		}
	}
	return n
}
