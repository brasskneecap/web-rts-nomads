package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// A pending-start building (placed, resources committed, but no worker has
// arrived to begin construction) is a reserved ghost. Until the first worker
// arrives and clears pendingStart it must NOT grant its owner vision and must
// NOT be attackable by enemies. These tests pin both gates and verify the
// behaviour flips on the instant pendingStart is cleared.

// newPendingBuildingTestState builds an obj-test state with a p1-owned Tower
// placed far from the p1 townhall's vision, plus a p1 FOW grid. setPending
// controls whether the Tower carries the pendingStart flag. Returns with the
// state lock held.
func newPendingBuildingTestState(t *testing.T, setPending bool) (s *GameState, buildingID string) {
	t.Helper()
	// Tower at grid (30,5) — far from the townhall at (2,10) so the townhall's
	// own vision never reaches it; the Tower is the only candidate vision source.
	const gx, gy = 30, 5
	tower := objBuilding("tower-pending", "tower", gx, gy, 2, 2, 500)
	s = newObjectiveTestState(t, tower)
	s.FOW["p1"] = newPlayerFOW(s.MapConfig.GridCols, s.MapConfig.GridRows)

	b := s.getBuildingByIDLocked("tower-pending")
	if b == nil {
		s.mu.Unlock()
		t.Fatal("fixture broken: tower-pending not registered")
	}
	if setPending {
		b.Metadata["underConstruction"] = true
		b.Metadata["pendingStart"] = true
	}
	return s, "tower-pending"
}

func TestPendingBuilding_GrantsNoVision(t *testing.T) {
	s, buildingID := newPendingBuildingTestState(t, true)
	defer s.mu.Unlock()

	b := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(b)

	s.recomputeFOWLocked()
	if s.FOW["p1"].isClearAtWorld(center.X, center.Y, s.MapConfig.CellSize) {
		t.Error("a pending-start building must not grant its owner vision")
	}

	// Construction begins — pendingStart cleared. Vision switches on.
	delete(b.Metadata, "pendingStart")
	s.recomputeFOWLocked()
	if !s.FOW["p1"].isClearAtWorld(center.X, center.Y, s.MapConfig.CellSize) {
		t.Error("once construction begins the building must grant vision")
	}
}

func TestPendingBuilding_NotAttackable(t *testing.T) {
	s, buildingID := newPendingBuildingTestState(t, true)
	defer s.mu.Unlock()

	b := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(b)

	// An enemy unit adjacent to the pending tower.
	enemy := s.spawnPlayerUnitLocked("raider", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: center.X, Y: center.Y - 128})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 100, 100
	s.initializeCombatUnitLocked(enemy)

	if s.isValidHostileBuildingTarget(enemy, b) {
		t.Error("a pending-start building must not be a valid hostile target")
	}

	// Construction begins — the building becomes attackable.
	delete(b.Metadata, "pendingStart")
	if !s.isValidHostileBuildingTarget(enemy, b) {
		t.Error("once construction begins the building must be attackable")
	}
}
