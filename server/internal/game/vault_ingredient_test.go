package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestVaultIngredientHelpers(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Players["p1"]

	broad, _ := getItemDef("broad_sword")
	ring, _ := getItemDef("fire_ring")
	s.addItemToVaultLocked(p, broad)
	s.addItemToVaultLocked(p, broad)
	s.addItemToVaultLocked(p, ring)

	if got := vaultItemCountLocked(p, "broad_sword"); got != 2 {
		t.Fatalf("broad_sword count = %d, want 2", got)
	}
	if got := vaultItemCountLocked(p, "fire_ring"); got != 1 {
		t.Fatalf("fire_ring count = %d, want 1", got)
	}
	if !s.removeOneItemFromVaultByItemIDLocked(p, "broad_sword") {
		t.Fatal("remove should succeed")
	}
	if got := vaultItemCountLocked(p, "broad_sword"); got != 1 {
		t.Fatalf("after remove: broad_sword count = %d, want 1", got)
	}
	if s.removeOneItemFromVaultByItemIDLocked(p, "no_such_item") {
		t.Fatal("remove of missing item should fail")
	}
}

func TestPlayerOwnsBuiltCapability(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := "p1"
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:           "art-1",
		BuildingType: "artificer",
		Visible:      true,
		Occupied:     true,
		OwnerID:      &owner,
		Capabilities: []string{"crafting"},
		Metadata:     map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	if !s.playerOwnsBuiltCapabilityLocked("p1", "crafting") {
		t.Fatal("player should own a built crafting building")
	}
	if s.playerOwnsBuiltCapabilityLocked("p2", "crafting") {
		t.Fatal("p2 owns nothing")
	}
}
