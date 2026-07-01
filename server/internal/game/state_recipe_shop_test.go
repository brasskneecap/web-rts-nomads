package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func addRecipeShop(s *GameState, bID string) {
	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: bID, BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{},
	})
}

func TestRecipeShop_PopulatesDeterministicSubset(t *testing.T) {
	roll := func(seed int64) []string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.EnsurePlayer("p1")
		s.mu.Lock()
		defer s.mu.Unlock()
		addRecipeShop(s, "rs-1")
		if s.buildingsByID == nil {
			s.buildingsByID = map[string]*protocol.BuildingTile{}
		}
		for i := range s.MapConfig.Buildings {
			b := &s.MapConfig.Buildings[i]
			s.buildingsByID[b.ID] = b
		}
		s.initShopBuildingsLocked()
		s.populateRecipeShopInventoriesLocked()
		b := s.buildingsByID["rs-1"]
		out := make([]string, 0, len(b.RecipeInventory))
		for _, e := range b.RecipeInventory {
			if e.Quantity != 1 {
				t.Errorf("recipe stock quantity = %d, want 1", e.Quantity)
			}
			out = append(out, e.RecipeID)
		}
		return out
	}
	a := roll(0xABC)
	b := roll(0xABC)
	if len(a) == 0 {
		t.Fatal("recipe shop stocked nothing")
	}
	if len(a) > 2 {
		t.Fatalf("recipe shop stocked %d > cap 2", len(a))
	}
	// Determinism: same seed → identical subset (order included).
	if len(a) != len(b) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic subset: %v vs %v", a, b)
		}
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, id := range a {
		if seen[id] {
			t.Fatalf("duplicate recipe in stock: %v", a)
		}
		seen[id] = true
	}
}

// TestRecipeShopGuards_OptInSpawnAndLock verifies that a recipe-shop with a
// guardGroupId spawns a guard squad and reports locked while they live, and
// that a recipe-shop WITHOUT guard metadata spawns no guards and is never
// locked (guards are opt-in per placed building).
func TestRecipeShopGuards_OptInSpawnAndLock(t *testing.T) {
	// Find any neutral group id present in the catalog.
	var groupID string
	for tier := 1; tier <= 10 && groupID == ""; tier++ {
		if ids := listNeutralGroupIDs(tier); len(ids) > 0 {
			groupID = ids[0]
			break
		}
	}
	if groupID == "" {
		t.Skip("no neutral group available")
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 5)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	defer s.mu.Unlock()

	neutral := neutralPlayerID
	// Guarded recipe shop.
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-guarded", BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{"guardGroupId": groupID},
	})
	// Unguarded recipe shop (no guardGroupId).
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "rs-open", BuildingType: "recipe-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"recipe-purchase"},
		Metadata:     map[string]interface{}{},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	s.spawnShopGuardsLocked()

	guarded := s.buildingsByID["rs-guarded"]
	if len(guarded.ShopGuardUnitIDs) == 0 {
		t.Fatal("guarded recipe shop should spawn at least one guard")
	}
	if !s.shopLockedLocked(guarded) {
		t.Fatal("guarded recipe shop should be locked while guards are alive")
	}

	open := s.buildingsByID["rs-open"]
	if len(open.ShopGuardUnitIDs) != 0 {
		t.Fatalf("recipe shop without guardGroupId should spawn no guards, got %d", len(open.ShopGuardUnitIDs))
	}
	if s.shopLockedLocked(open) {
		t.Fatal("recipe shop without guards should not be locked")
	}

	// Kill the guards → the guarded shop unlocks.
	for _, id := range guarded.ShopGuardUnitIDs {
		if u := s.getUnitByIDLocked(id); u != nil {
			u.HP = 0
		}
	}
	if s.shopLockedLocked(guarded) {
		t.Error("recipe shop should unlock after all guards reach HP 0")
	}
}
