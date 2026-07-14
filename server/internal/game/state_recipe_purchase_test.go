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
	fireSwordDef, ok := getRecipeDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword recipe not found in catalog")
	}
	// The shop charges the LEARN price (UnlockCostGold), not the per-craft cost
	// the Artificer will charge later (CostGold).
	cost := fireSwordDef.UnlockCostGold
	if p.Resources["gold"] != 1000-cost {
		t.Fatalf("gold = %d, want %d", p.Resources["gold"], 1000-cost)
	}
	if s.buildingsByID["rs-1"].RecipeInventory[0].Quantity != 0 {
		t.Fatal("stock should decrement to 0 after purchase")
	}
}

func TestPurchaseRecipe_RejectsWhenUnaffordable(t *testing.T) {
	s, p := setupRecipePurchase(t)
	def, ok := getRecipeDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword recipe not found in catalog")
	}
	broke := def.UnlockCostGold - 1 // one gold short of the learn price, whatever it is tuned to
	s.mu.Lock()
	p.Resources["gold"] = broke
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("recipe must NOT unlock when unaffordable")
	}
	if p.Resources["gold"] != broke {
		t.Fatalf("gold should be unchanged, got %d, want %d", p.Resources["gold"], broke)
	}
	if s.buildingsByID["rs-1"].RecipeInventory[0].Quantity != 1 {
		t.Fatalf("shop stock should be unchanged, got %d", s.buildingsByID["rs-1"].RecipeInventory[0].Quantity)
	}
}

func TestPurchaseRecipe_RejectsWhenSoldOut(t *testing.T) {
	s, p := setupRecipePurchase(t)
	s.mu.Lock()
	s.buildingsByID["rs-1"].RecipeInventory[0].Quantity = 0
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playerKnowsRecipeLocked("p1", "fire_sword") {
		t.Fatal("recipe must NOT unlock when stock is sold out")
	}
	if p.Resources["gold"] != 1000 {
		t.Fatalf("gold should be unchanged, got %d", p.Resources["gold"])
	}
}

func TestPurchaseRecipe_RejectsWhenAlreadyKnown(t *testing.T) {
	s, p := setupRecipePurchase(t)
	// Pre-unlock the recipe so the buyer already knows it. Buying again must be
	// a no-op: no gold spent and the shop stock left untouched (the client greys
	// these out, but the server must not charge for a redundant purchase).
	s.mu.Lock()
	s.unlockRecipeForPlayerLocked(p, "fire_sword")
	s.mu.Unlock()
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Resources["gold"] != 1000 {
		t.Fatalf("gold should be unchanged for an already-known recipe, got %d", p.Resources["gold"])
	}
	if s.buildingsByID["rs-1"].RecipeInventory[0].Quantity != 1 {
		t.Fatalf("shop stock should be unchanged, got %d", s.buildingsByID["rs-1"].RecipeInventory[0].Quantity)
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

// overrideRecipe swaps a recipe def into the runtime overlay for one test and
// restores the catalog on cleanup, so a test can pick costs that make the two
// prices tell apart.
func overrideRecipe(t *testing.T, def *RecipeDef) {
	t.Helper()
	runtimeRecipesMu.Lock()
	prev, had := runtimeRecipes[def.ID]
	runtimeRecipes[def.ID] = def
	runtimeRecipesMu.Unlock()
	t.Cleanup(func() {
		runtimeRecipesMu.Lock()
		if had {
			runtimeRecipes[def.ID] = prev
		} else {
			delete(runtimeRecipes, def.ID)
		}
		runtimeRecipesMu.Unlock()
	})
}

// TestRecipeCostsAreIndependent is the guard for the whole point of splitting
// the two prices: learning a recipe charges UnlockCostGold and crafting with it
// charges CostGold. They are deliberately different numbers here, so a
// regression that collapses them back into one field fails this test whichever
// way it collapses.
func TestRecipeCostsAreIndependent(t *testing.T) {
	const learnCost, craftCost = 200, 30
	overrideRecipe(t, &RecipeDef{
		ID: "fire_sword", Name: "Fire Sword",
		Inputs:         []string{"broad_sword", "fire_ring"},
		CostGold:       craftCost,
		UnlockCostGold: learnCost,
		Output:         "fire_sword", Rarity: ItemTierRare,
	})

	s, p := setupRecipePurchase(t)

	// The craft half of the test needs an Artificer and the ingredients.
	s.mu.Lock()
	owner := "p1"
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "art-1", BuildingType: "artificer", Visible: true, Occupied: true,
		OwnerID: &owner, Capabilities: []string{"crafting"}, Metadata: map[string]interface{}{},
	})
	// The append may have reallocated the slice, so re-index every building.
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	for _, id := range []string{"broad_sword", "fire_ring"} {
		def, ok := getItemDef(id)
		if !ok {
			s.mu.Unlock()
			t.Fatalf("%s not in catalog", id)
		}
		s.addItemToVaultLocked(p, def)
	}
	s.mu.Unlock()

	// Learn it: the shop charges the learn price.
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	if !s.playerKnowsRecipeLocked("p1", "fire_sword") {
		s.mu.Unlock()
		t.Fatal("recipe should be unlocked after purchase")
	}
	afterLearn := p.Resources["gold"]
	if afterLearn != 1000-learnCost {
		s.mu.Unlock()
		t.Fatalf("learning charged %d gold, want the learn price %d (the craft cost is %d)",
			1000-afterLearn, learnCost, craftCost)
	}
	s.mu.Unlock()

	// Craft it: the Artificer charges the craft price, not the learn price again.
	s.CraftItem("p1", "fire_sword")
	s.mu.Lock()
	defer s.mu.Unlock()
	if got := afterLearn - p.Resources["gold"]; got != craftCost {
		t.Fatalf("crafting charged %d gold, want the craft cost %d (the learn price is %d)",
			got, craftCost, learnCost)
	}
}
