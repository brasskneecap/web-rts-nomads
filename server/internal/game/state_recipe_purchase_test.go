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
	s.buildingsByID["rs-1"].RecipeInventory = []protocol.RecipeStockEntry{{ItemID: "fire_sword", Quantity: 1}}
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
	fireSword, ok := getItemDef("fire_sword")
	if !ok || !fireSword.IsCraftable() {
		t.Fatal("fire_sword is not a craftable item in the catalog")
	}
	// The shop charges the LEARN price (RecipeCostGold), not the per-craft cost
	// the Artificer will charge later (CraftCostGold).
	cost := fireSword.Crafting.RecipeCostGold
	if p.Resources["gold"] != 1000-cost {
		t.Fatalf("gold = %d, want %d", p.Resources["gold"], 1000-cost)
	}
	if s.buildingsByID["rs-1"].RecipeInventory[0].Quantity != 0 {
		t.Fatal("stock should decrement to 0 after purchase")
	}
}

func TestPurchaseRecipe_RejectsWhenUnaffordable(t *testing.T) {
	s, p := setupRecipePurchase(t)
	def, ok := getItemDef("fire_sword")
	if !ok || !def.IsCraftable() {
		t.Fatal("fire_sword is not a craftable item in the catalog")
	}
	broke := def.Crafting.RecipeCostGold - 1 // one gold short of the learn price, whatever it is tuned to
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


// overrideItem swaps an item def into the runtime overlay for one test and
// restores the catalog on cleanup, so a test can pick prices that tell the two
// crafting costs apart.
func overrideItem(t *testing.T, def *ItemDef) {
	t.Helper()
	runtimeItemsMu.Lock()
	prev, had := runtimeItems[def.ID]
	runtimeItems[def.ID] = def
	runtimeItemsMu.Unlock()
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		if had {
			runtimeItems[def.ID] = prev
		} else {
			delete(runtimeItems, def.ID)
		}
		runtimeItemsMu.Unlock()
	})
}

// addArtificer gives p1 a built crafting building, optionally bound to a list.
// Pass listID "" for an unbound Artificer (makes anything).
func addArtificer(t *testing.T, s *GameState, listID string) {
	t.Helper()
	owner := "p1"
	meta := map[string]interface{}{}
	if listID != "" {
		meta["list"] = listID
	}
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "art-1", BuildingType: "artificer", Visible: true, Occupied: true,
		OwnerID: &owner, Capabilities: []string{"crafting"}, Metadata: meta,
	})
	// The append may have reallocated the slice, so re-index every building.
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
}

// TestCraftAndRecipeCostsAreIndependent is the guard for the whole point of
// keeping two crafting prices: LEARNING an item's recipe charges RecipeCostGold,
// and CRAFTING it charges CraftCostGold. They are deliberately different numbers
// here, so a regression that collapses them into one field fails this test
// whichever way it collapses.
func TestCraftAndRecipeCostsAreIndependent(t *testing.T) {
	const learnCost, craftCost = 200, 30
	base, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword not in catalog")
	}
	swapped := *base
	swapped.Crafting = &ItemCrafting{
		Inputs:         []string{"broad_sword", "fire_ring"},
		CraftCostGold:  craftCost,
		RecipeCostGold: learnCost,
	}
	overrideItem(t, &swapped)

	s, p := setupRecipePurchase(t)

	s.mu.Lock()
	addArtificer(t, s, "") // unbound: makes anything the player has learned
	for _, id := range []string{"broad_sword", "fire_ring"} {
		def, ok := getItemDef(id)
		if !ok {
			s.mu.Unlock()
			t.Fatalf("%s not in catalog", id)
		}
		s.addItemToVaultLocked(p, def)
	}
	s.mu.Unlock()

	// Learn it: the shop charges the LEARN price.
	s.PurchaseRecipe("p1", "rs-1", "fire_sword")
	s.mu.Lock()
	if !s.playerKnowsRecipeLocked("p1", "fire_sword") {
		s.mu.Unlock()
		t.Fatal("recipe should be learned after purchase")
	}
	afterLearn := p.Resources["gold"]
	if afterLearn != 1000-learnCost {
		s.mu.Unlock()
		t.Fatalf("learning charged %d gold, want the learn price %d (the craft cost is %d)",
			1000-afterLearn, learnCost, craftCost)
	}
	s.mu.Unlock()

	// Craft it: the Artificer charges the CRAFT price, not the learn price again.
	if !s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should succeed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if got := afterLearn - p.Resources["gold"]; got != craftCost {
		t.Fatalf("crafting charged %d gold, want the craft cost %d (the learn price is %d)",
			got, craftCost, learnCost)
	}
}

// TestCraftingBuildingListScopesWhatItMakes: a crafting building bound to a list
// makes only what is ON that list, even for recipes the player has learned. This
// is what lets a Dwarven Forge be weapons-only. The list NARROWS; it never grants.
func TestCraftingBuildingListScopesWhatItMakes(t *testing.T) {
	s, p := setupRecipePurchase(t)

	s.mu.Lock()
	// druid_recipes_1 contains fire_sword but NOT scimitar.
	addArtificer(t, s, "druid_recipes_1")
	// Learn both, and stock the ingredients for each.
	s.unlockRecipeForPlayerLocked(p, "fire_sword")
	s.unlockRecipeForPlayerLocked(p, "scimitar")
	for _, id := range []string{"broad_sword", "broad_sword", "broad_sword", "fire_ring"} {
		def, _ := getItemDef(id)
		s.addItemToVaultLocked(p, def)
	}
	p.Resources["gold"] = 10000
	s.mu.Unlock()

	// scimitar is learned and affordable, but it is NOT on this forge's list.
	if s.CraftItem("p1", "scimitar") {
		t.Error("a crafting building bound to a list must refuse an item not on it, even when learned")
	}
	// fire_sword IS on the list.
	if !s.CraftItem("p1", "fire_sword") {
		t.Error("a crafting building must make an item that is both learned and on its list")
	}
}

// TestCraftingBuildingListDoesNotGrantUnlearnedRecipes: being on the building's
// list is not a substitute for learning the recipe.
func TestCraftingBuildingListDoesNotGrantUnlearnedRecipes(t *testing.T) {
	s, p := setupRecipePurchase(t)
	s.mu.Lock()
	addArtificer(t, s, "druid_recipes_1") // contains fire_sword
	for _, id := range []string{"broad_sword", "fire_ring"} {
		def, _ := getItemDef(id)
		s.addItemToVaultLocked(p, def)
	}
	p.Resources["gold"] = 10000
	// Deliberately do NOT learn fire_sword.
	s.mu.Unlock()

	if s.CraftItem("p1", "fire_sword") {
		t.Error("a list must not grant a recipe the player has never learned")
	}
}
