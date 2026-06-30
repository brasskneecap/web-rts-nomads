package game

// CraftItem is the public entry point for crafting a recipe at an Artificer.
// Acquires s.mu, delegates, and returns whether a craft succeeded. The boolean
// lets the caller (WS handler) fire the account-wide recipe-record seam only on
// success.
func (s *GameState) CraftItem(playerID, recipeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleCraftItemLocked(playerID, recipeID)
}

// handleCraftItemLocked validates and executes a craft. Returns true on success.
// Validation failures are silent no-ops (return false). On success: consume one
// of each input item from the vault, deduct gold, add the output item to the
// vault. Must be called under s.mu.
func (s *GameState) handleCraftItemLocked(playerID, recipeID string) bool {
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	// Must own a built Artificer.
	if !s.playerOwnsBuiltCapabilityLocked(playerID, "crafting") {
		return false
	}
	// Recipe must be unlocked this match.
	if !s.playerKnowsRecipeLocked(playerID, recipeID) {
		return false
	}
	def, ok := getRecipeDef(recipeID)
	if !ok {
		return false
	}
	outDef, ok := getItemDef(def.Output)
	if !ok {
		return false
	}
	// Afford gold.
	if player.Resources["gold"] < def.CostGold {
		return false
	}
	// Vault must contain every input, accounting for duplicates (e.g. a recipe
	// that needs 2 of the same item).
	needed := make(map[string]int, len(def.Inputs))
	for _, in := range def.Inputs {
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
	for itemID, count := range needed {
		for k := 0; k < count; k++ {
			if !s.removeOneItemFromVaultByItemIDLocked(player, itemID) {
				// Should never happen (counts were verified above); abort safely.
				return false
			}
		}
	}
	// Deduct gold.
	player.Resources["gold"] -= def.CostGold
	// Produce output.
	if !s.addItemToVaultLocked(player, outDef) {
		// Extremely unlikely (we just freed >=2 slots). Refund to avoid losing
		// the player's gold + items on a capacity edge case.
		player.Resources["gold"] += def.CostGold
		for itemID, count := range needed {
			for k := 0; k < count; k++ {
				inDef, _ := getItemDef(itemID)
				s.addItemToVaultLocked(player, inDef)
			}
		}
		return false
	}
	return true
}
