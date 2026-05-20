package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// buildLabeledClaimTestState constructs a fixture that mimics what an editor
// would author: a townhall linked to a "player1"-labelled spawn-point, plus
// two extra player-class buildings — a chapel tagged as "player1" and a
// barracks tagged as "player2". After EnsurePlayer("p1") the chapel must be
// owned by p1 and the barracks must remain unowned. Lock is NOT held on
// return.
func buildLabeledClaimTestState(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)

	s.mu.Lock()
	// Wipe the base map's buildings so we can author a minimal fixture.
	s.MapConfig.Buildings = nil
	s.buildingsByID = map[string]*protocol.BuildingTile{}

	// Townhall at (5,5) — claimable, no preset owner.
	addLabeledBuilding(s, protocol.BuildingTile{
		ID:           "th-1",
		BuildingType: "townhall",
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		Width:        3,
		Height:       2,
		Visible:      false,
		Capabilities: []string{"unit-spawner", "occupiable", "deposit-point"},
		Metadata:     map[string]interface{}{},
	})
	// Spawn-point at (5,8) linked to that townhall, labelled player1.
	addLabeledBuilding(s, protocol.BuildingTile{
		ID:           "sp-1",
		BuildingType: "spawn-point",
		GridCoord:    protocol.GridCoord{X: 5, Y: 8},
		Width:        1,
		Height:       1,
		Capabilities: []string{},
		Metadata: map[string]interface{}{
			"townhallId":  "th-1",
			"playerLabel": "player1",
		},
	})
	// Chapel labelled player1 — should be claimed.
	addLabeledBuilding(s, protocol.BuildingTile{
		ID:           "chapel-1",
		BuildingType: "chapel",
		GridCoord:    protocol.GridCoord{X: 12, Y: 5},
		Width:        2,
		Height:       2,
		Capabilities: []string{},
		Metadata: map[string]interface{}{
			"playerLabel": "player1",
		},
	})
	// Barracks labelled player2 — should NOT be claimed by p1.
	addLabeledBuilding(s, protocol.BuildingTile{
		ID:           "barracks-1",
		BuildingType: "barracks",
		GridCoord:    protocol.GridCoord{X: 15, Y: 5},
		Width:        3,
		Height:       3,
		Capabilities: []string{},
		Metadata: map[string]interface{}{
			"playerLabel": "player2",
		},
	})
	// Blacksmith with no playerLabel — should stay unowned.
	addLabeledBuilding(s, protocol.BuildingTile{
		ID:           "bs-1",
		BuildingType: "blacksmith",
		GridCoord:    protocol.GridCoord{X: 18, Y: 5},
		Width:        2,
		Height:       2,
		Capabilities: []string{},
		Metadata:     map[string]interface{}{},
	})
	s.invalidateBlockedCellsLocked()
	s.mu.Unlock()
	return s
}

func addLabeledBuilding(s *GameState, b protocol.BuildingTile) {
	// Use addBuildingLocked so the buildingsByID index is re-walked after each
	// append. Direct slice-append + pointer-store would leave the index
	// dangling once the backing array reallocates, causing
	// resolveSpawnPointTownhallLocked → getBuildingByIDLocked to return nil.
	s.addBuildingLocked(b)
}

func TestClaimLabeledBuildings_PlayerOnlyClaimsMatchingLabel(t *testing.T) {
	s := buildLabeledClaimTestState(t)
	s.EnsurePlayer("p1")

	s.mu.RLock()
	defer s.mu.RUnlock()

	chapel := s.getBuildingByIDLocked("chapel-1")
	if chapel.OwnerID == nil || *chapel.OwnerID != "p1" {
		t.Errorf("chapel.OwnerID = %v; want p1", chapel.OwnerID)
	}
	if !chapel.Visible || !chapel.Occupied {
		t.Errorf("chapel.Visible=%v Occupied=%v; want both true after claim", chapel.Visible, chapel.Occupied)
	}
	chapelDef, _ := getBuildingDef("chapel")
	if hp, _, ok := getBuildingHP(chapel); !ok || hp != chapelDef.MaxHp {
		t.Errorf("chapel hp = %v (ok=%v); want %v", hp, ok, chapelDef.MaxHp)
	}
	// SpawnUnitTypes must be hydrated from the catalog so the chapel can train
	// apprentices immediately on a claimed pre-built structure.
	if len(chapel.SpawnUnitTypes) == 0 || chapel.SpawnUnitTypes[0] != "apprentice" {
		t.Errorf("chapel.SpawnUnitTypes = %v; want [apprentice]", chapel.SpawnUnitTypes)
	}

	barracks := s.getBuildingByIDLocked("barracks-1")
	if barracks.OwnerID != nil {
		t.Errorf("barracks.OwnerID = %v; want nil (label belongs to player2, not p1)", *barracks.OwnerID)
	}

	bs := s.getBuildingByIDLocked("bs-1")
	if bs.OwnerID != nil {
		t.Errorf("blacksmith.OwnerID = %v; want nil (no playerLabel set)", *bs.OwnerID)
	}
}

// TestClaimLabeledBuildings_Idempotent verifies that re-running the claim
// helper after the building is already owned does not flip ownership or
// reset metadata.
func TestClaimLabeledBuildings_Idempotent(t *testing.T) {
	s := buildLabeledClaimTestState(t)
	s.EnsurePlayer("p1")

	s.mu.Lock()
	chapel := s.getBuildingByIDLocked("chapel-1")
	chapel.Metadata["hp"] = 1.0 // simulate damage taken
	s.claimLabeledBuildingsForPlayerLocked("p1")
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	chapel = s.getBuildingByIDLocked("chapel-1")
	if hp, _, _ := getBuildingHP(chapel); hp != 1.0 {
		t.Errorf("chapel hp after second claim = %v; want 1.0 (already-owned buildings must not be reset)", hp)
	}
}
