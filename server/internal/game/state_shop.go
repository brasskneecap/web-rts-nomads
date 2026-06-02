package game

import (
	"log/slog"
	"sort"

	"webrts/server/pkg/protocol"
)

// defaultShopLootTargetCount is the base number of distinct items a shop
// rolls when its inventory comes from a loot table. The roller keeps
// rolling (with an attempt cap to prevent infinite loops on degenerate
// tables) until it has this many unique item IDs. Player-initiated
// rerolls bump this by Player.ShopItemCountBonus, so a future legend-
// point upgrade can ship by writing to that field alone.
const defaultShopLootTargetCount = 3

// shopItemTargetCountForPlayerLocked returns the effective number of
// distinct items a player's reroll should produce — the base count plus
// any ShopItemCountBonus on the player. Returns the base unmodified
// when playerID is empty or the player is missing. Must be called under
// s.mu.
func (s *GameState) shopItemTargetCountForPlayerLocked(playerID string) int {
	if playerID == "" {
		return defaultShopLootTargetCount
	}
	player, ok := s.Players[playerID]
	if !ok {
		return defaultShopLootTargetCount
	}
	target := defaultShopLootTargetCount + player.ShopItemCountBonus
	if target < 1 {
		target = 1
	}
	return target
}

// defaultMarketplaceStarterInventory is the precedence-3 fallback list for
// a player-built marketplace that the map author did not configure with a
// shopFixedInventory or shopLootTableId. Intentionally small so the
// marketplace ships as a focused "early game" shop; later progression
// (tiered marketplaces, unlocks) can grow it by either authoring per-map
// overrides or by extending this list.
var defaultMarketplaceStarterInventory = []string{
	"broad_sword",
	"potion_common_heal",
}

// defaultNeutralShopLootTableID is the loot table neutral-shop buildings
// roll from when the map author has not specified a per-instance override.
// This makes every painted neutral-shop a usable shop by default; authors
// can still set shopFixedInventory or shopLootTableId in the map editor's
// metadata fields to override.
const defaultNeutralShopLootTableID = "merchant_basic"

// Starter quantity per item slot, by building type. Stock decrements on
// purchase; at 0 the slot stays in the inventory but renders disabled on
// the client. Player marketplaces ship with a deep stock so the early
// game has reliable resupply; neutral merchants are scarce-by-design and
// give one of each rolled item.
const (
	defaultMarketplaceItemStock = 99
	defaultNeutralShopItemStock = 1
)

// defaultShopRerollsPerPlayer is the per-match merchant-reroll budget every
// player starts with. Future legend-point profile upgrades can bump this
// via applyProfileUpgradesToPlayerLocked. Used by EnsurePlayerWithUpgrades.
const defaultShopRerollsPerPlayer = 1

// starterStockForBuildingType returns the initial Quantity each item slot
// should ship with for the given building type. Defaults to 1 for any
// building not in the table; map authors who want different behavior for
// custom shop building types should add a case here.
func starterStockForBuildingType(buildingType string) int {
	switch buildingType {
	case "marketplace":
		return defaultMarketplaceItemStock
	case "neutral-shop":
		return defaultNeutralShopItemStock
	default:
		return 1
	}
}

// makeShopStockEntries wraps a list of item IDs in ShopStockEntry values,
// each carrying the building-type-appropriate starter quantity. Used by
// both the fixed-inventory and loot-rolled paths so quantity assignment
// has one source of truth.
func makeShopStockEntries(itemIDs []string, buildingType string) []protocol.ShopStockEntry {
	if len(itemIDs) == 0 {
		return nil
	}
	stock := starterStockForBuildingType(buildingType)
	entries := make([]protocol.ShopStockEntry, len(itemIDs))
	for i, id := range itemIDs {
		entries[i] = protocol.ShopStockEntry{ItemID: id, Quantity: stock}
	}
	return entries
}

// hasItemPurchaseCapability returns true when b's capability list includes
// the "item-purchase" capability — the single source of truth for "this
// building is a shop."
func hasItemPurchaseCapability(b *protocol.BuildingTile) bool {
	for _, c := range b.Capabilities {
		if c == "item-purchase" {
			return true
		}
	}
	return false
}

// initShopBuildingsLocked sets ownership/visibility on neutral-shop
// buildings at match start. Map authors brush these buildings without
// having to encode OwnerID in the map JSON; the server assigns them to
// the neutralPlayerID slot here and marks the buildings as Visible +
// Occupied so they render and block pathing immediately. The neutral
// player entry itself is NOT created here — it is lazily created by
// ensureNeutralPlayerLocked when neutral units actually spawn (e.g.
// shop guards or neutral camps). Creating it eagerly would put a
// player into s.Players before any human joins, which mis-triggers
// other code that gates on Players being non-empty. Idempotent.
// Must be called under s.mu write lock.
func (s *GameState) initShopBuildingsLocked() {
	neutralID := neutralPlayerID
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "neutral-shop" {
			continue
		}
		if b.OwnerID == nil {
			owner := neutralID
			b.OwnerID = &owner
		}
		b.Visible = true
		b.Occupied = true
	}
}

// populateShopInventoriesLocked walks every building with the
// "item-purchase" capability and fills ShopInventory exactly once, using
// the precedence:
//
//  1. ShopFixedInventory (authored list, copied verbatim)
//  2. ShopLootTableID (rolled via the seeded loot RNG)
//  3. For marketplaces only: the legacy "all items with
//     RequiredBuilding == 'marketplace'" filter
//  4. Otherwise: leave nil and log a warning
//
// Must be called under s.mu write lock. Iteration order is sorted by
// building ID so loot-table rolls are deterministic across runs.
func (s *GameState) populateShopInventoriesLocked() {
	indices := make([]int, 0, len(s.MapConfig.Buildings))
	for i := range s.MapConfig.Buildings {
		if hasItemPurchaseCapability(&s.MapConfig.Buildings[i]) {
			indices = append(indices, i)
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		return s.MapConfig.Buildings[indices[i]].ID < s.MapConfig.Buildings[indices[j]].ID
	})
	for _, idx := range indices {
		s.populateShopInventoryForBuildingLocked(&s.MapConfig.Buildings[idx])
	}
}

// populateShopInventoryForBuildingLocked fills a single building's
// ShopInventory using the precedence documented on
// populateShopInventoriesLocked. Each item ID resolved by precedence is
// wrapped in a ShopStockEntry carrying the building-type-appropriate
// starter Quantity (see starterStockForBuildingType). Safe to call again
// after a marketplace finishes construction. Must be called under s.mu
// write lock.
func (s *GameState) populateShopInventoryForBuildingLocked(b *protocol.BuildingTile) {
	if b == nil || !hasItemPurchaseCapability(b) {
		return
	}
	// Precedence 1: explicit fixed inventory.
	if len(b.ShopFixedInventory) > 0 {
		b.ShopInventory = makeShopStockEntries(b.ShopFixedInventory, b.BuildingType)
		return
	}
	// Precedence 2: loot-table roll.
	if b.ShopLootTableID != "" {
		rolled, ok := s.rollShopLootTableLocked(b.ID, b.ShopLootTableID, defaultShopLootTargetCount)
		if !ok {
			// Helper already logged; leave ShopInventory nil.
			return
		}
		b.ShopInventory = makeShopStockEntries(rolled, b.BuildingType)
		return
	}
	// Precedence 3: per-building-type defaults.
	switch b.BuildingType {
	case "marketplace":
		// Player-built marketplace with no authored shopFixedInventory /
		// shopLootTableId ships with defaultMarketplaceStarterInventory —
		// a focused early-game list. The legacy "every item with
		// RequiredBuilding == marketplace" behavior is intentionally
		// retired; authors who want a richer marketplace declare it via
		// shopFixedInventory on the map JSON.
		b.ShopInventory = makeShopStockEntries(defaultMarketplaceStarterInventory, b.BuildingType)
		return
	case "neutral-shop":
		// Authored neutral-shop with no override → roll the default
		// merchant loot table for a small randomized stock. Authors can
		// still set shopFixedInventory or shopLootTableId in the editor
		// metadata to override.
		rolled, ok := s.rollShopLootTableLocked(b.ID, defaultNeutralShopLootTableID, defaultShopLootTargetCount)
		if !ok {
			return
		}
		b.ShopInventory = makeShopStockEntries(rolled, b.BuildingType)
		return
	}
	// Precedence 4: nothing to sell.
	slog.Warn("populateShopInventoryForBuildingLocked: shop building has no inventory source",
		"buildingID", b.ID, "buildingType", b.BuildingType)
}

// handleRerollShopLocked re-rolls the named building's inventory and
// decrements the player's ShopRerollsRemaining. The building must be a
// neutral-shop the player has discovered AND unlocked AND that uses a
// loot table (fixed-inventory shops aren't rerollable). The player must
// have at least one reroll remaining. All validation failures are silent
// no-ops; on success the building's ShopInventory is replaced with a
// fresh roll using the same loot table (or its default for
// unconfigured neutral-shops). Must be called under s.mu write lock.
func (s *GameState) handleRerollShopLocked(playerID, buildingID string) {
	player, ok := s.Players[playerID]
	if !ok || player.ShopRerollsRemaining <= 0 {
		return
	}
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if !hasItemPurchaseCapability(building) {
		return
	}
	if building.BuildingType != "neutral-shop" {
		// Reroll only applies to neutral merchants. Player marketplaces and
		// any other shop building type ignore the message.
		return
	}
	if building.OwnerID == nil || *building.OwnerID != neutralPlayerID {
		return
	}
	// Discovery + lock gates: same rules as purchasing.
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
	// Determine which loot table to roll. Fixed-inventory shops are not
	// rerollable (the author committed to a specific list).
	if len(building.ShopFixedInventory) > 0 {
		return
	}
	tableID := building.ShopLootTableID
	if tableID == "" {
		tableID = defaultNeutralShopLootTableID
	}
	targetCount := s.shopItemTargetCountForPlayerLocked(playerID)
	rolled, ok := s.rollShopLootTableLocked(building.ID, tableID, targetCount)
	if !ok {
		// Helper already logged. Don't charge the player for a failed roll.
		return
	}
	building.ShopInventory = makeShopStockEntries(rolled, building.BuildingType)
	player.ShopRerollsRemaining--
}

// RerollShop is the public entry point for rerolling a neutral shop's
// inventory. Acquires s.mu and delegates to handleRerollShopLocked.
func (s *GameState) RerollShop(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleRerollShopLocked(playerID, buildingID)
}

// rollShopLootTableLocked rolls the named top-level loot table repeatedly
// until it has accumulated `targetCount` distinct item IDs, or until the
// attempt budget (targetCount * 8) is exhausted. Returns (nil, false) and
// logs slog.Error when the table is missing. Roll order is RNG-
// deterministic. Resource bundles are skipped (shops don't sell resources);
// sub-table gaps and duplicates are skipped silently. If the table cannot
// produce targetCount distinct items within the attempt budget (degenerate
// table, all resources, etc.), returns whatever it has collected — never
// loops forever.
func (s *GameState) rollShopLootTableLocked(buildingID, tableID string, targetCount int) ([]string, bool) {
	table, ok := getLootTable(tableID)
	if !ok {
		slog.Error("rollShopLootTableLocked: unknown loot table",
			"buildingID", buildingID, "lootTableId", tableID)
		return nil, false
	}
	if targetCount <= 0 {
		return nil, true
	}
	maxAttempts := targetCount * 8
	seen := make(map[string]struct{}, targetCount)
	items := make([]string, 0, targetCount)
	for attempt := 0; attempt < maxAttempts && len(items) < targetCount; attempt++ {
		r := s.rngLoot.Intn(100) + 1
		var hit *LootTableEntry
		for i := range table {
			if r >= table[i].Min && r <= table[i].Max {
				hit = &table[i]
				break
			}
		}
		if hit == nil {
			continue
		}
		pkg, ok := getPackagedItem(hit.Entry)
		if !ok {
			continue
		}
		if pkg.Kind != PackagedItemSubtable {
			continue
		}
		maxSub := 0
		for _, e := range pkg.Entries {
			if e.Max > maxSub {
				maxSub = e.Max
			}
		}
		if maxSub == 0 {
			continue
		}
		subRoll := s.rngLoot.Intn(maxSub) + 1
		for _, e := range pkg.Entries {
			if subRoll >= e.Min && subRoll <= e.Max {
				if _, dup := seen[e.Item]; !dup {
					seen[e.Item] = struct{}{}
					items = append(items, e.Item)
				}
				break
			}
		}
	}
	return items, true
}


// spawnShopGuardsLocked walks every neutral-shop building, reads its
// optional guard metadata (`guardGroupId`, `guardStartingTier`,
// `guardAggroRange`, `guardLeashRange`), and spawns the declared squad
// around the building's perimeter. Spawned unit IDs are stored on
// `BuildingTile.ShopGuardUnitIDs` so shopLockedLocked can read them.
// Buildings with no `guardGroupId` are skipped (unlocked from spawn).
//
// Iteration order is sorted by building ID so guard composition rolls
// are deterministic across runs. Must be called under s.mu write lock.
// Idempotent — safe to call again only if existing guards are dead;
// no logic prevents re-spawning, so callers should run once at init.
func (s *GameState) spawnShopGuardsLocked() {
	indices := make([]int, 0)
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "neutral-shop" {
			continue
		}
		gid, ok := getMetadataString(b.Metadata, "guardGroupId")
		if !ok || gid == "" {
			continue
		}
		indices = append(indices, i)
	}
	if len(indices) == 0 {
		return
	}
	sort.Slice(indices, func(i, j int) bool {
		return s.MapConfig.Buildings[indices[i]].ID < s.MapConfig.Buildings[indices[j]].ID
	})
	s.ensureNeutralPlayerLocked()
	blocked := s.getBlockedCellsLocked()
	cellSize := s.MapConfig.CellSize
	for _, idx := range indices {
		b := &s.MapConfig.Buildings[idx]
		groupID, _ := getMetadataString(b.Metadata, "guardGroupId")
		startingTier := 1
		if t, ok := getMetadataFloat(b.Metadata, "guardStartingTier"); ok && t >= 1 {
			startingTier = int(t)
		}
		aggro, _ := getMetadataFloat(b.Metadata, "guardAggroRange")
		leash, _ := getMetadataFloat(b.Metadata, "guardLeashRange")
		if aggro < guardMinAggroRange {
			aggro = guardMinAggroRange
		}
		if leash < aggro {
			leash = aggro
		}

		tier := resolveNeutralTier(startingTier)
		if tier == 0 {
			slog.Warn("spawnShopGuardsLocked: no tier available; skipping",
				"buildingID", b.ID, "guardGroupId", groupID)
			continue
		}
		group, ok := getNeutralGroup(tier, groupID)
		if !ok {
			slog.Warn("spawnShopGuardsLocked: group not found at tier; skipping",
				"buildingID", b.ID, "guardGroupId", groupID, "tier", tier)
			continue
		}

		centerWX := (float64(b.X) + float64(b.Width)/2) * cellSize
		centerWY := (float64(b.Y) + float64(b.Height)/2) * cellSize
		centerCell := s.worldToGrid(centerWX, centerWY)
		placedOrderID := s.nextMovementOrderIDLocked()

		spawnIdx := 0
		for _, entry := range group.Composition {
			for i := 0; i < entry.Count; i++ {
				offsetCell := neutralCampRingOffset(centerCell, spawnIdx)
				spawnCell, found := s.findNearestWalkable(offsetCell, blocked)
				if !found {
					spawnCell, found = s.findNearestWalkable(centerCell, blocked)
					if !found {
						slog.Warn("spawnShopGuardsLocked: no walkable cell; skipping unit",
							"buildingID", b.ID, "unitType", entry.UnitType, "spawnIdx", spawnIdx)
						spawnIdx++
						continue
					}
				}
				spawnPos := s.gridToWorldCenter(spawnCell)
				unit := s.spawnNeutralUnitLocked(entry.UnitType, spawnPos)
				if unit == nil {
					spawnIdx++
					continue
				}
				unit.OrderID = placedOrderID
				unit.GuardMode = true
				unit.GuardAnchorX = centerWX
				unit.GuardAnchorY = centerWY
				unit.GuardAggroRange = aggro
				unit.GuardLeashRange = leash
				unit.IgnoreWaveClear = true
				unit.Order = OrderState{Type: OrderHold, HoldX: centerWX, HoldY: centerWY}
				unit.CombatAnchorX = centerWX
				unit.CombatAnchorY = centerWY
				unit.Status = "Guarding"

				b.ShopGuardUnitIDs = append(b.ShopGuardUnitIDs, unit.ID)
				spawnIdx++
			}
		}
	}
}

// shopLockedLocked reports whether the building is currently locked by
// at least one alive guard unit. Empty guard list → unlocked.
// Must be called under s.mu lock.
func (s *GameState) shopLockedLocked(b *protocol.BuildingTile) bool {
	if b == nil || len(b.ShopGuardUnitIDs) == 0 {
		return false
	}
	for _, id := range b.ShopGuardUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u != nil && u.HP > 0 {
			return true
		}
	}
	return false
}
