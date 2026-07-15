package game

// CraftItem is the public entry point for crafting a recipe at an Artificer.
// Acquires s.mu, delegates, and returns whether the craft succeeded. The
// recipe-crafted profile-record handler fires internally on success; the caller
// does not need to take any action on true.
func (s *GameState) CraftItem(playerID, itemID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleCraftItemLocked(playerID, itemID)
}

// handleCraftItemLocked validates and executes a craft. Returns true on success.
// Validation failures are silent no-ops (return false). On success: consume one
// of each input item from the vault, deduct gold, add the crafted item to the
// vault. Must be called under s.mu.
//
// itemID names the item being MADE — an item is its own recipe (ItemDef.Crafting).
func (s *GameState) handleCraftItemLocked(playerID, itemID string) bool {
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	// Must own a built crafting building that will make THIS item — a building
	// bound to a list only makes what is on it (a Dwarven Forge that only makes
	// weapons). An unbound crafting building makes anything.
	if !s.playerOwnsCraftingBuildingForLocked(playerID, itemID) {
		return false
	}
	// The recipe must have been learned. A building's list SCOPES what it can
	// make; it never grants a recipe you have not learned.
	if !s.playerKnowsRecipeLocked(playerID, itemID) {
		return false
	}
	outDef, ok := getItemDef(itemID)
	if !ok || !outDef.IsCraftable() {
		return false
	}
	craft := outDef.Crafting
	// Afford gold. The CRAFT cost — the recipe cost was paid once, when learning.
	if player.Resources["gold"] < craft.CraftCostGold {
		return false
	}
	// Vault must contain every input, accounting for duplicates (e.g. a recipe
	// that needs 2 of the same item).
	needed := make(map[string]int, len(craft.Inputs))
	for _, in := range craft.Inputs {
		needed[in]++
	}
	for itemID, count := range needed {
		if vaultItemCountLocked(player, itemID) < count {
			return false
		}
	}
	// Inputs are removed first, freeing slots before the output is added.
	// We still guard addItemToVaultLocked's return below.

	// Consume inputs.
	for inID, count := range needed {
		for k := 0; k < count; k++ {
			if !s.removeOneItemFromVaultByItemIDLocked(player, inID) {
				// Should never happen (counts were verified above); abort safely.
				return false
			}
		}
	}
	// Deduct gold.
	player.Resources["gold"] -= craft.CraftCostGold
	// Produce output.
	if !s.addItemToVaultLocked(player, outDef) {
		// Extremely unlikely (we just freed >=2 slots). Refund to avoid losing
		// the player's gold + items on a capacity edge case.
		player.Resources["gold"] += craft.CraftCostGold
		for inID, count := range needed {
			for k := 0; k < count; k++ {
				inDef, _ := getItemDef(inID)
				s.addItemToVaultLocked(player, inDef)
			}
		}
		return false
	}
	if s.recipeCraftedHandler != nil {
		handler := s.recipeCraftedHandler
		go handler(playerID, itemID)
	}
	return true
}

// SetRecipeCraftedHandler installs the post-craft callback. Safe to call once
// at match construction. Passing nil disables the hook (the default — tests
// that do not exercise profile persistence do not need to set it).
func (s *GameState) SetRecipeCraftedHandler(fn func(playerID, recipeID string)) {
	s.recipeCraftedHandler = fn
}
