package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// markRecipeShopDiscovered records buildingID in the player's FOW KnownBuildings
// so the neutral-shop discovery gate passes. KnownBuildings is
// map[string]*protocol.BuildingTile, matching the real PlayerFOW definition.
func markRecipeShopDiscovered(s *GameState, playerID, buildingID string) {
	fow := s.FOW[playerID]
	if fow == nil {
		return
	}
	b, ok := s.buildingsByID[buildingID]
	if !ok {
		return
	}
	if fow.KnownBuildings == nil {
		fow.KnownBuildings = map[string]*protocol.BuildingTile{}
	}
	clone := *b
	fow.KnownBuildings[buildingID] = &clone
}

func setupRecipePurchase(t *testing.T) (*GameState, *Player) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.EnsurePlayer("p1")
	s.mu.Lock()
	addRecipeShop(s, "rs-1") // helper from Task 2's test file (same package)
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	// Force a known stock so the test is independent of the sampler.
	s.buildingsByID["rs-1"].RecipeInventory = []protocol.RecipeStockEntry{{RecipeID: "fire_sword", Quantity: 1}}
	p := s.Players["p1"]
	p.Resources["gold"] = 1000
	markRecipeShopDiscovered(s, "p1", "rs-1")
	s.mu.Unlock()
	return s, p
}

func TestPurchaseRecipe_Success(t *testing.T) {
	s, p := setupRecipePurchase(t)
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("recipe should be unlocked after purchase")
	}
	cost := 150
	if p.Resources["gold"] != 1000-cost {
		t.Fatalf("gold = %d, want %d", p.Resources["gold"], 1000-cost)
	}
	if s.buildingsByID["rs-1"].RecipeInventory[0].Quantity != 0 {
		t.Fatal("stock should decrement to 0 after purchase")
	}
}

func TestPurchaseRecipe_RejectsWhenUnaffordable(t *testing.T) {
	s, p := setupRecipePurchase(t)
	s.mu.Lock()
	p.Resources["gold"] = 10 // < 150
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("recipe must NOT unlock when unaffordable")
	}
	if p.Resources["gold"] != 10 {
		t.Fatalf("gold should be unchanged, got %d", p.Resources["gold"])
	}
}

func TestPurchaseRecipe_RejectsUndiscovered(t *testing.T) {
	s, _ := setupRecipePurchase(t)
	s.mu.Lock()
	delete(s.FOW["p1"].KnownBuildings, "rs-1")
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("undiscovered recipe shop purchase must be rejected")
	}
}
