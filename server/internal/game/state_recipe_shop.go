package game

import (
	"sort"

	"webrts/server/pkg/protocol"
)

// defaultRecipeShopCount is how many distinct recipes a Recipe Shop stocks per
// match. Kept small so recipe discovery is a scarce, run-varied resource.
const defaultRecipeShopCount = 2

// hasRecipePurchaseCapability reports whether b is a Recipe Shop.
func hasRecipePurchaseCapability(b *protocol.BuildingTile) bool {
	for _, c := range b.Capabilities {
		if c == "recipe-purchase" {
			return true
		}
	}
	return false
}

// isShopSnapshotBuilding reports whether b should carry the ShopLocked /
// ShopDiscovered snapshot fields — any neutral building that sells items or
// recipes. Used by the per-viewer building snapshot so the client can render
// the guard-lock / discovery state for both merchants and recipe traders.
func isShopSnapshotBuilding(b *protocol.BuildingTile) bool {
	return hasItemPurchaseCapability(b) || hasRecipePurchaseCapability(b)
}

// playerKnowsRecipeLocked reports whether the player may craft recipeID this
// match (seeded from profile + purchased). Must be called under s.mu.
func (s *GameState) playerKnowsRecipeLocked(playerID, recipeID string) bool {
	p, ok := s.Players[playerID]
	if !ok {
		return false
	}
	for _, id := range p.UnlockedRecipeIDs {
		if id == recipeID {
			return true
		}
	}
	return false
}

// unlockRecipeForPlayerLocked adds recipeID to the player's in-match unlocked
// set if absent, keeping the slice sorted. Idempotent. Must be called under s.mu.
func (s *GameState) unlockRecipeForPlayerLocked(player *Player, recipeID string) {
	if player == nil || recipeID == "" {
		return
	}
	for _, id := range player.UnlockedRecipeIDs {
		if id == recipeID {
			return
		}
	}
	player.UnlockedRecipeIDs = append(player.UnlockedRecipeIDs, recipeID)
	sort.Strings(player.UnlockedRecipeIDs)
}

// populateRecipeShopInventoriesLocked fills every recipe-shop building's
// RecipeInventory with a deterministic random subset of all recipes, sampled
// via s.rngLoot. Iteration order over buildings is sorted by ID so the sample
// is reproducible across runs. Must be called under s.mu write lock, once at
// match start (reads s.MapConfig.Buildings directly; does not require buildingsByID to be populated).
func (s *GameState) populateRecipeShopInventoriesLocked() {
	all := ListRecipeDefs() // already sorted by ID
	if len(all) == 0 {
		return
	}
	indices := make([]int, 0, len(s.MapConfig.Buildings))
	for i := range s.MapConfig.Buildings {
		if hasRecipePurchaseCapability(&s.MapConfig.Buildings[i]) {
			indices = append(indices, i)
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		return s.MapConfig.Buildings[indices[i]].ID < s.MapConfig.Buildings[indices[j]].ID
	})
	for _, idx := range indices {
		b := &s.MapConfig.Buildings[idx]
		// Already populated (idempotent guard).
		if len(b.RecipeInventory) > 0 {
			continue
		}
		count := defaultRecipeShopCount
		if count > len(all) {
			count = len(all)
		}
		// Partial Fisher-Yates over a copy of the sorted recipe list using the
		// seeded loot RNG → deterministic per (seed, building order).
		pool := make([]*RecipeDef, len(all))
		copy(pool, all)
		for k := 0; k < count; k++ {
			j := k + s.rngLoot.Intn(len(pool)-k)
			pool[k], pool[j] = pool[j], pool[k]
		}
		entries := make([]protocol.RecipeStockEntry, 0, count)
		for k := 0; k < count; k++ {
			entries = append(entries, protocol.RecipeStockEntry{RecipeID: pool[k].ID, Quantity: 1})
		}
		b.RecipeInventory = entries
	}
}
