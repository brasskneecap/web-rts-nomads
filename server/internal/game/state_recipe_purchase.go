package game

// PurchaseRecipe is the public entry point for buying a recipe from a Recipe
// Shop. Acquires s.mu and delegates to handlePurchaseRecipeLocked.
//
// itemID names the item the recipe MAKES — an item is its own recipe.
func (s *GameState) PurchaseRecipe(playerID, buildingID, itemID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePurchaseRecipeLocked(playerID, buildingID, itemID)
}

// handlePurchaseRecipeLocked validates and executes a recipe purchase from a
// neutral Recipe Shop. Validation failures are silent no-ops (mirrors
// handlePurchaseItemLocked). On success: deduct gold, unlock the recipe for
// the match, decrement shop stock. Must be called under s.mu.
func (s *GameState) handlePurchaseRecipeLocked(playerID, buildingID, itemID string) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	// Building must exist, be visible, have recipe-purchase capability, and
	// not be under construction.
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if getMetadataBool(building.Metadata, "underConstruction") {
		return
	}
	if !hasRecipePurchaseCapability(building) {
		return
	}

	// Recipe shops are always neutral-owned. Purchaser must have discovered
	// the building in FOW and the shop must not be guard-locked.
	if building.OwnerID == nil || *building.OwnerID != neutralPlayerID {
		return
	}
	fow := s.FOW[playerID]
	if fow == nil {
		return
	}
	if _, discovered := fow.KnownBuildings[building.ID]; !discovered {
		return
	}
	if s.shopLockedLocked(building) {
		return
	}

	// Recipe must be in this shop's RecipeInventory with stock remaining.
	stockIdx := -1
	for i, e := range building.RecipeInventory {
		if e.ItemID == itemID {
			stockIdx = i
			break
		}
	}
	if stockIdx < 0 || building.RecipeInventory[stockIdx].Quantity <= 0 {
		return
	}

	// The item must exist and be craftable — a recipe IS an item's crafting
	// block, so an item with no crafting block has no recipe to sell.
	def, ok := getItemDef(itemID)
	if !ok || !def.IsCraftable() {
		return
	}

	// Already-known recipes are a no-op: unlockRecipeForPlayerLocked is
	// idempotent, so without this guard the player would spend gold and burn
	// shop stock for nothing. The client greys these out; this backstops it.
	if s.playerKnowsRecipeLocked(playerID, itemID) {
		return
	}

	// Afford check. The learn price is RecipeCostGold, NOT CraftCostGold — the
	// latter is what the Artificer charges per craft, and the two are tuned
	// independently (see ItemCrafting).
	if player.Resources["gold"] < def.Crafting.RecipeCostGold {
		return
	}

	// Commit: deduct gold, unlock recipe, decrement stock.
	player.Resources["gold"] -= def.Crafting.RecipeCostGold
	s.unlockRecipeForPlayerLocked(player, itemID)
	building.RecipeInventory[stockIdx].Quantity--
}
