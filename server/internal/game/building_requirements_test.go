package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestChapel_RequiresKeep verifies the chapel catalog gates construction
// behind a tier-2 (Keep) town hall. Regression guard against an
// accidental JSON edit that drops the requirement.
func TestChapel_RequiresKeep(t *testing.T) {
	def, ok := getBuildingDef("chapel")
	if !ok {
		t.Fatal("chapel building def not registered")
	}
	if def.RequiresTownhallTier != 2 {
		t.Errorf("chapel.RequiresTownhallTier = %d; want 2 (Keep)", def.RequiresTownhallTier)
	}
}

// TestBuildBuilding_ChapelBlockedAtTier1 verifies BuildBuilding silently
// drops a chapel placement when the player only has a tier-1 town hall.
func TestBuildBuilding_ChapelBlockedAtTier1(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 9999, "wood": 9999}
	addBuildingToState(s, "th-1", "townhall", p1, false, true) // tier defaults to 1
	preGold := s.Players[p1].Resources["gold"]
	preWood := s.Players[p1].Resources["wood"]
	preBuildings := len(s.MapConfig.Buildings)
	s.mu.Unlock()

	s.BuildBuilding(p1, "chapel", nil, 20, 20)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.MapConfig.Buildings); got != preBuildings {
		t.Errorf("buildings after blocked chapel placement = %d; want %d (no-op expected)", got, preBuildings)
	}
	if s.Players[p1].Resources["gold"] != preGold {
		t.Errorf("gold = %d; want %d (no deduction on blocked placement)", s.Players[p1].Resources["gold"], preGold)
	}
	if s.Players[p1].Resources["wood"] != preWood {
		t.Errorf("wood = %d; want %d (no deduction on blocked placement)", s.Players[p1].Resources["wood"], preWood)
	}
}

// TestBuildBuilding_ChapelAllowedAtKeep verifies BuildBuilding accepts a
// chapel placement once the player's town hall has reached tier 2 (Keep).
func TestBuildBuilding_ChapelAllowedAtKeep(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 9999, "wood": 9999}
	addBuildingToState(s, "th-1", "townhall", p1, false, true)
	// Promote the town hall to Keep (tier 2).
	for i := range s.MapConfig.Buildings {
		if s.MapConfig.Buildings[i].ID == "th-1" {
			s.MapConfig.Buildings[i].Metadata["tier"] = float64(2)
			break
		}
	}
	preBuildings := len(s.MapConfig.Buildings)
	chapelDef, _ := getBuildingDef("chapel")
	preGold := s.Players[p1].Resources["gold"]
	preWood := s.Players[p1].Resources["wood"]
	s.mu.Unlock()

	s.BuildBuilding(p1, "chapel", nil, 20, 20)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.MapConfig.Buildings); got != preBuildings+1 {
		t.Fatalf("buildings after accepted chapel placement = %d; want %d", got, preBuildings+1)
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
