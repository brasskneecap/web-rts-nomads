package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// addTownhallAtTier injects a fully-built town hall at the given tier owned by
// playerID. Caller holds s.mu.
func addTownhallAtTier(s *GameState, id, playerID string, tier int) {
	owner := playerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:           id,
		BuildingType: "townhall",
		Width:        2,
		Height:       2,
		Visible:      true,
		OwnerID:      &owner,
		Capabilities: []string{},
		Metadata:     map[string]interface{}{"tier": float64(tier)},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	last := &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
	s.buildingsByID[last.ID] = last
}

// TestChapel_BuildGateMatchesCatalog verifies BuildBuilding honours WHATEVER
// requiresTownhallTier the chapel catalog declares — the expected town-hall tier
// is read from the def, never pinned to a literal. Below the required tier the
// placement is a silent no-op (no building, no resource spend); at the required
// tier it succeeds and charges the catalog cost. When the catalog sets no
// requirement (tier ≤ 1) only the success case applies. This lets the chapel's
// tier gate be retuned in JSON without ever touching this test.
func TestChapel_BuildGateMatchesCatalog(t *testing.T) {
	chapelDef, ok := getBuildingDef("chapel")
	if !ok {
		t.Fatal("chapel building def not registered")
	}
	required := chapelDef.RequiresTownhallTier

	// Below the requirement → blocked. Only meaningful when required ≥ 2.
	if required >= 2 {
		s, p1 := newRequirementsTestState(t)
		s.mu.Lock()
		s.Players[p1].Resources = map[string]int{"gold": 9999, "wood": 9999}
		addTownhallAtTier(s, "th-1", p1, required-1)
		preBuildings := len(s.MapConfig.Buildings)
		preGold := s.Players[p1].Resources["gold"]
		preWood := s.Players[p1].Resources["wood"]
		s.mu.Unlock()

		s.BuildBuilding(p1, "chapel", nil, 20, 20)

		s.mu.RLock()
		if got := len(s.MapConfig.Buildings); got != preBuildings {
			t.Errorf("chapel placed at townhall tier %d (requires %d); want no-op", required-1, required)
		}
		if s.Players[p1].Resources["gold"] != preGold || s.Players[p1].Resources["wood"] != preWood {
			t.Errorf("resources spent on a blocked chapel placement")
		}
		s.mu.RUnlock()
	}

	// At (or above) the requirement → allowed. Unrequired chapels build at tier 1.
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 9999, "wood": 9999}
	tier := required
	if tier < 1 {
		tier = 1
	}
	addTownhallAtTier(s, "th-1", p1, tier)
	preBuildings := len(s.MapConfig.Buildings)
	preGold := s.Players[p1].Resources["gold"]
	preWood := s.Players[p1].Resources["wood"]
	s.mu.Unlock()

	s.BuildBuilding(p1, "chapel", nil, 20, 20)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.MapConfig.Buildings); got != preBuildings+1 {
		t.Fatalf("chapel not placed at townhall tier %d (requires %d)", tier, required)
	}
	wantGold := preGold - chapelDef.ResourceCost["gold"]
	wantWood := preWood - chapelDef.ResourceCost["wood"]
	if s.Players[p1].Resources["gold"] != wantGold {
		t.Errorf("gold = %d; want %d", s.Players[p1].Resources["gold"], wantGold)
	}
	if s.Players[p1].Resources["wood"] != wantWood {
		t.Errorf("wood = %d; want %d", s.Players[p1].Resources["wood"], wantWood)
	}
}

// TestSetBuildingSpawnPoint_ChapelAccepted verifies that a chapel (a
// unit-spawner that is neither "townhall" nor "barracks") accepts a
// rally-point command. Regression guard against re-introducing the
// hardcoded type allowlist in SetBuildingSpawnPoint.
func TestSetBuildingSpawnPoint_ChapelAccepted(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	owner := p1
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:             "chapel-1",
		BuildingType:   "chapel",
		Width:          2,
		Height:         2,
		Visible:        true,
		OwnerID:        &owner,
		Capabilities:   []string{"unit-spawner"},
		SpawnUnitTypes: []string{"acolyte"},
		Metadata:       map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	last := &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
	s.buildingsByID[last.ID] = last
	s.mu.Unlock()

	want := protocol.Vec2{X: 512, Y: 384}
	s.SetBuildingSpawnPoint(p1, "chapel-1", want)

	s.mu.RLock()
	defer s.mu.RUnlock()
	b := s.getBuildingByIDLocked("chapel-1")
	gotX, xOk := getMetadataFloat(b.Metadata, "spawnPointX")
	gotY, yOk := getMetadataFloat(b.Metadata, "spawnPointY")
	if !xOk || !yOk {
		t.Fatalf("chapel rally point not stored: spawnPointX/Y missing from metadata %v", b.Metadata)
	}
	if gotX != want.X || gotY != want.Y {
		t.Errorf("chapel rally point = (%v, %v); want (%v, %v)", gotX, gotY, want.X, want.Y)
	}
}

// TestBuildBuilding_BlacksmithUnaffected verifies buildings with no
// requiresTownhallTier are still constructible at tier 1. Regression
// guard against accidentally gating every player building behind Keep.
func TestBuildBuilding_BlacksmithUnaffected(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 9999, "wood": 9999}
	addBuildingToState(s, "th-1", "townhall", p1, false, true)
	preBuildings := len(s.MapConfig.Buildings)
	s.mu.Unlock()

	s.BuildBuilding(p1, "blacksmith", nil, 20, 20)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.MapConfig.Buildings); got != preBuildings+1 {
		t.Fatalf("blacksmith placement at tier 1 = %d buildings; want %d", got, preBuildings+1)
	}
}
