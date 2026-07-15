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

// playerKnowsRecipeLocked reports whether the player has learned itemID's recipe
// this match (seeded from profile + purchased). Must be called under s.mu.
func (s *GameState) playerKnowsRecipeLocked(playerID, itemID string) bool {
	p, ok := s.Players[playerID]
	if !ok {
		return false
	}
	for _, id := range p.UnlockedCraftableIDs {
		if id == itemID {
			return true
		}
	}
	return false
}

// unlockRecipeForPlayerLocked adds itemID to the player's in-match learned set
// if absent, keeping the slice sorted. Idempotent. Must be called under s.mu.
func (s *GameState) unlockRecipeForPlayerLocked(player *Player, itemID string) {
	if player == nil || itemID == "" {
		return
	}
	for _, id := range player.UnlockedCraftableIDs {
		if id == itemID {
			return
		}
	}
	player.UnlockedCraftableIDs = append(player.UnlockedCraftableIDs, itemID)
	sort.Strings(player.UnlockedCraftableIDs)
}

// craftableItemDefs returns every craftable item in the catalog, sorted by ID.
// This is the global recipe pool — "a recipe" is just an item with a crafting
// block, so there is no separate recipe catalog to consult.
func craftableItemDefs() []*ItemDef {
	all := ListItemDefs() // sorted by ID
	out := make([]*ItemDef, 0, len(all))
	for _, def := range all {
		if def.IsCraftable() {
			out = append(out, def)
		}
	}
	return out
}

// starterCraftableItemIDs returns the IDs of every item whose recipe every
// player starts the match already knowing (crafting.starter), sorted so seeding
// never depends on map iteration order.
func starterCraftableItemIDs() []string {
	ids := make([]string, 0)
	for _, def := range craftableItemDefs() {
		if def.Crafting.Starter {
			ids = append(ids, def.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// populateRecipeShopInventoriesLocked fills every recipe-shop building's
// RecipeInventory with a deterministic random subset of the recipes it can sell,
// sampled via s.rngLoot. Iteration order over buildings is sorted by ID so the
// sample is reproducible across runs. Must be called under s.mu write lock, once
// at match start (reads s.MapConfig.Buildings directly; does not require
// buildingsByID to be populated).
func (s *GameState) populateRecipeShopInventoriesLocked() {
	all := craftableItemDefs() // sorted by ID
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
		src := recipeShopPool(b, all)
		count := defaultRecipeShopCount
		if count > len(src) {
			count = len(src)
		}
		// Partial Fisher-Yates over a copy of the sorted pool using the seeded
		// loot RNG → deterministic per (seed, building order).
		pool := make([]*ItemDef, len(src))
		copy(pool, src)
		for k := 0; k < count; k++ {
			j := k + s.rngLoot.Intn(len(pool)-k)
			pool[k], pool[j] = pool[j], pool[k]
		}
		entries := make([]protocol.RecipeStockEntry, 0, count)
		for k := 0; k < count; k++ {
			entries = append(entries, protocol.RecipeStockEntry{ItemID: pool[k].ID, Quantity: 1})
		}
		b.RecipeInventory = entries
	}
}

// recipeShopPool returns the pool a Recipe Shop samples its stock from: the
// CRAFTABLE members of its bound list, else the global craftable pool `all`.
//
// Non-craftable members are silently skipped rather than rejected — a list is
// untyped, so the same list may legitimately be shop stock or a loot pool where
// those members do belong. A list with no craftable members at all falls back to
// the global pool rather than leaving the shop empty.
func recipeShopPool(b *protocol.BuildingTile, all []*ItemDef) []*ItemDef {
	list, ok := listForBuilding(b)
	if !ok {
		return all
	}
	pool := make([]*ItemDef, 0, len(list.Items))
	seen := make(map[string]bool, len(list.Items))
	for _, id := range list.Items {
		if seen[id] {
			continue
		}
		if def, ok := getItemDef(id); ok && def.IsCraftable() {
			pool = append(pool, def)
			seen[id] = true
		}
	}
	if len(pool) == 0 {
		return all
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].ID < pool[j].ID })
	return pool
}
