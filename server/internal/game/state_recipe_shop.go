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

// populateRecipeShopInventoriesLocked fills every recipe-shop building's
// RecipeInventory with a deterministic random subset of all recipes, sampled
// via s.rngLoot. Iteration order over buildings is sorted by ID so the sample
// is reproducible across runs. Must be called under s.mu write lock, once at
// match start (after buildingsByID is built).
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
