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
// rerolls bump this by Player.ShopItemCountBonus, so a future dominion-
// point upgrade can ship by writing to that field alone.
const defaultShopLootTargetCount = 3

// neutralShopBaseItemCount is the number of items a neutral shop shows before
// any per-player ShopItemCountBonus. Sourced from gameplay_tuning.json
// (neutralShop.baseItemCount); a missing/invalid value defaults to 2.
func neutralShopBaseItemCount() int {
	v := gameplayTuning().NeutralShop.BaseItemCount
	if v < 1 {
		return 2
	}
	return v
}

// neutralShopDefaultRerollWaves is the default wave cadence for auto-refreshing
// a neutral shop's stock (0 = disabled). A neutral-shop instance overrides it
// via map metadata "rerollWaves". Sourced from gameplay_tuning.json.
func neutralShopDefaultRerollWaves() int {
	v := gameplayTuning().NeutralShop.RerollEveryWaves
	if v < 0 {
		return 0
	}
	return v
}

// shopItemTargetCountForPlayerLocked returns the effective number of
// distinct items a player's reroll should produce — the neutral-shop base
// count plus any ShopItemCountBonus on the player. Returns the base unmodified
// when playerID is empty or the player is missing. Must be called under
// s.mu.
func (s *GameState) shopItemTargetCountForPlayerLocked(playerID string) int {
	base := neutralShopBaseItemCount()
	player, ok := s.Players[playerID]
	if playerID == "" || !ok {
		return base
	}
	target := base + player.ShopItemCountBonus
	if target < 1 {
		target = 1
	}
	return target
}

// defaultMarketplaceItemListID names the authored list
// (catalog/lists/<id>.json) a player-built marketplace stocks when the map
// author did not configure a shopFixedInventory, a shopLootTableId, or a bound
// list. Growing the marketplace is a catalog edit, not a code change.
const defaultMarketplaceItemListID = "marketplace"

// defaultMarketplaceStarterInventory is the last-ditch fallback should the
// "marketplace" list ever be missing from the embedded catalog (it is
// validated at load, so this is belt-and-braces, not an expected path).
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
// player starts with. Future dominion-point profile upgrades can bump this
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
		if b.BuildingType != "neutral-shop" && b.BuildingType != "recipe-shop" {
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
//  3. the building's bound list ("list" metadata, a catalog/lists entry, copied verbatim)
//  4. Per-building-type defaults (marketplace stocks the "marketplace" list;
//     a neutral-shop rolls the default merchant loot table)
//  5. Otherwise: leave nil and log a warning
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
	// Precedence 1: explicit fixed inventory (all shop types, copied verbatim).
	// A fixed-inventory shop is never rerollable — the author committed to a list.
	if len(b.ShopFixedInventory) > 0 {
		b.ShopInventory = makeShopStockEntries(b.ShopFixedInventory, b.BuildingType)
		return
	}
	// Neutral-shop merchants sample their source (an assigned item-list POOL, or
	// a loot table) down to the base item count and re-sample every few waves.
	// stockNeutralShopLocked owns that logic; a failed roll leaves inventory nil.
	if b.BuildingType == "neutral-shop" {
		s.stockNeutralShopLocked(b, neutralShopBaseItemCount())
		return
	}
	// Precedence 2: loot-table roll (non-neutral shops).
	if b.ShopLootTableID != "" {
		rolled, ok := s.rollShopLootTableLocked(b.ID, b.ShopLootTableID, defaultShopLootTargetCount)
		if !ok {
			// Helper already logged; leave ShopInventory nil.
			return
		}
		b.ShopInventory = makeShopStockEntries(rolled, b.BuildingType)
		return
	}
	// Precedence 3: the building's bound list (map metadata "list"), stocked
	// verbatim for non-neutral shops (e.g. a marketplace pointed at a curated
	// list). Neutral shops handle their list above (as a sampled pool).
	if list, ok := listForBuilding(b); ok {
		b.ShopInventory = makeShopStockEntries(list.ItemIDs(), b.BuildingType)
		return
	}
	// Precedence 4: per-building-type defaults.
	switch b.BuildingType {
	case "marketplace":
		// Player-built marketplace with no authored shopFixedInventory /
		// shopLootTableId stocks the authored "marketplace" list.
		// The legacy "every item with RequiredBuilding == marketplace"
		// behavior is intentionally retired; authors who want a per-map
		// marketplace declare it via shopFixedInventory on the map JSON.
		if list, ok := getListDef(defaultMarketplaceItemListID); ok {
			b.ShopInventory = makeShopStockEntries(list.ItemIDs(), b.BuildingType)
			return
		}
		slog.Warn("populateShopInventoryForBuildingLocked: marketplace list missing; using starter fallback",
			"listID", defaultMarketplaceItemListID)
		b.ShopInventory = makeShopStockEntries(defaultMarketplaceStarterInventory, b.BuildingType)
		return
	}
	// Precedence 5: nothing to sell.
	slog.Warn("populateShopInventoryForBuildingLocked: shop building has no inventory source",
		"buildingID", b.ID, "buildingType", b.BuildingType)
}

// rollNeutralShopStockIDsLocked samples the item IDs for a neutral shop's stock
// from its configured source, sampling `count` items: its bound list (map
// metadata "list", treated as a POOL) when set and valid, else its loot table
// (b.ShopLootTableID or the default merchant table). Returns (ids, true) or
// (nil, false) on a failed roll (unknown loot table); a list that does not
// resolve falls through to the loot table. When both a list and a loot table are
// set the list wins. Must be called under s.mu write lock (advances s.rngLoot).
//
// Note a neutral shop treats its list as a POOL to sample from, while a
// marketplace stocks its list VERBATIM. That difference belongs to the building,
// not to the list — the same list can serve both.
func (s *GameState) rollNeutralShopStockIDsLocked(b *protocol.BuildingTile, count int) ([]string, bool) {
	if list, ok := listForBuilding(b); ok {
		return s.sampleListLocked(list, count), true
	}
	tableID := b.ShopLootTableID
	if tableID == "" {
		tableID = defaultNeutralShopLootTableID
	}
	return s.rollShopLootTableLocked(b.ID, tableID, count)
}

// stockNeutralShopLocked (re)stocks the SHARED BuildingTile.ShopInventory for a
// neutral shop, sampling `count` items. This shared list is only a display
// fallback (e.g. the one-time join snapshot); the authoritative per-player views
// live in Player.NeutralShopInventories. Returns false without mutating on a
// failed roll. Must be called under s.mu write lock.
func (s *GameState) stockNeutralShopLocked(b *protocol.BuildingTile, count int) bool {
	ids, ok := s.rollNeutralShopStockIDsLocked(b, count)
	if !ok {
		return false
	}
	b.ShopInventory = makeShopStockEntries(ids, b.BuildingType)
	return true
}

// stockNeutralShopForPlayerLocked (re)samples a single player's INDEPENDENT view
// of a neutral shop into player.NeutralShopInventories[b.ID]. A fixed-inventory
// shop copies the authored list verbatim (per-player quantities); others sample
// their pool / loot table at `count`. No-op on a failed roll or unknown player.
// Returns true when the view was (re)stocked. Must be called under s.mu write
// lock.
func (s *GameState) stockNeutralShopForPlayerLocked(playerID string, b *protocol.BuildingTile, count int) bool {
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	var ids []string
	if len(b.ShopFixedInventory) > 0 {
		ids = b.ShopFixedInventory
	} else {
		rolled, rolledOK := s.rollNeutralShopStockIDsLocked(b, count)
		if !rolledOK {
			return false
		}
		ids = rolled
	}
	if player.NeutralShopInventories == nil {
		player.NeutralShopInventories = map[string][]protocol.ShopStockEntry{}
	}
	player.NeutralShopInventories[b.ID] = makeShopStockEntries(ids, b.BuildingType)
	return true
}

// populatePlayerNeutralShopViewsLocked samples this player's independent view of
// every neutral shop on the map, at the player's effective item count (base +
// their ShopItemCountBonus). Called once when the player joins. Deterministic
// (sorted building order). Must be called under s.mu write lock.
func (s *GameState) populatePlayerNeutralShopViewsLocked(playerID string) {
	count := s.shopItemTargetCountForPlayerLocked(playerID)
	for _, idx := range s.sortedNeutralShopIndicesLocked() {
		s.stockNeutralShopForPlayerLocked(playerID, &s.MapConfig.Buildings[idx], count)
	}
}

// sortedNeutralShopIndicesLocked returns the indices of every neutral-shop
// item-purchase building in MapConfig.Buildings, sorted by building ID so
// re-sample rolls are deterministic across runs. Must be called under s.mu.
func (s *GameState) sortedNeutralShopIndicesLocked() []int {
	indices := make([]int, 0)
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType == "neutral-shop" && hasItemPurchaseCapability(b) {
			indices = append(indices, i)
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		return s.MapConfig.Buildings[indices[i]].ID < s.MapConfig.Buildings[indices[j]].ID
	})
	return indices
}

// sortedRealPlayerIDsLocked returns the IDs of all human/AI players (excluding
// the enemy and neutral pseudo-players), sorted for deterministic iteration.
// Must be called under s.mu.
func (s *GameState) sortedRealPlayerIDsLocked() []string {
	ids := make([]string, 0, len(s.Players))
	for id := range s.Players {
		if id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// sampleListLocked returns up to `count` DISTINCT items from a list, using the
// seeded loot RNG (deterministic).
//
// A WEIGHTED list is sampled BY WEIGHT: it is rolled repeatedly and duplicates
// are discarded, so a member with a bigger slice of the die shows up on the
// shelf more often — a rare sword is rare in a shop for the same reason it is
// rare in a chest. A UNIFORM list is sampled evenly (a partial Fisher-Yates
// shuffle), which is exactly what every list did before weights existed.
//
// Fewer than `count` results only when the list has fewer distinct members (or,
// for a weighted list, when the attempt budget runs out before the rare tail
// turns up — which is itself the weighting working). Must be called under s.mu
// write lock (advances s.rngLoot).
func (s *GameState) sampleListLocked(list *ListDef, count int) []string {
	if list == nil || count <= 0 {
		return nil
	}
	if list.IsWeighted() {
		return s.sampleWeightedListLocked(list, count)
	}
	return s.sampleItemsFromListLocked(list.Items, count)
}

// sampleWeightedListLocked rolls the list until it has `count` distinct items or
// the attempt budget is spent. The budget is what keeps a list whose tail is
// vanishingly rare from spinning: it is a shop shelf, not a guarantee.
func (s *GameState) sampleWeightedListLocked(list *ListDef, count int) []string {
	distinct := map[string]struct{}{}
	for _, e := range list.Entries {
		distinct[e.Item] = struct{}{}
	}
	n := count
	if n > len(distinct) {
		n = len(distinct)
	}
	maxAttempts := n * 8
	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)
	for attempt := 0; attempt < maxAttempts && len(out) < n; attempt++ {
		id := s.rollListLocked(list)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// sampleItemsFromListLocked returns up to `count` distinct items chosen at
// random from the pool using the seeded loot RNG (deterministic). Duplicates in
// the pool are collapsed first; fewer than `count` results only when the pool is
// smaller. Order is randomized (a partial Fisher-Yates shuffle). Must be called
// under s.mu write lock (advances s.rngLoot).
func (s *GameState) sampleItemsFromListLocked(pool []string, count int) []string {
	if count <= 0 || len(pool) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(pool))
	uniq := make([]string, 0, len(pool))
	for _, id := range pool {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	n := count
	if n > len(uniq) {
		n = len(uniq)
	}
	for i := 0; i < n; i++ {
		j := i + s.rngLoot.Intn(len(uniq)-i)
		uniq[i], uniq[j] = uniq[j], uniq[i]
	}
	out := make([]string, n)
	copy(out, uniq[:n])
	return out
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
	// Fixed-inventory shops are not rerollable (the author committed to a list).
	if len(building.ShopFixedInventory) > 0 {
		return
	}
	// Re-sample only THIS player's independent view of the shop, at their
	// effective count (base + their ShopItemCountBonus upgrade). Other players'
	// views are untouched.
	targetCount := s.shopItemTargetCountForPlayerLocked(playerID)
	if !s.stockNeutralShopForPlayerLocked(playerID, building, targetCount) {
		// Helper already logged. Don't charge the player for a failed roll.
		return
	}
	player.ShopRerollsRemaining--
}

// shopDisplayNameFor returns a shop's display label from its bound list — e.g.
// "Wandering Merchant" — or "" when it has no list or the list is unknown (the
// client then falls back to the building type label). Used to populate the
// snapshot-only BuildingTile.ShopDisplayName.
func shopDisplayNameFor(b *protocol.BuildingTile) string {
	if list, ok := listForBuilding(b); ok {
		return list.Name
	}
	return ""
}

// neutralShopRerollWavesFor returns a neutral shop's auto-reroll wave cadence:
// its per-instance map metadata "rerollWaves" override when present, else the
// gameplay-tuning default (neutralShopDefaultRerollWaves). 0 disables auto-
// reroll for the shop. Must be called under s.mu.
func neutralShopRerollWavesFor(b *protocol.BuildingTile) int {
	if v, ok := getMetadataFloat(b.Metadata, "rerollWaves"); ok {
		if v < 0 {
			return 0
		}
		return int(v)
	}
	return neutralShopDefaultRerollWaves()
}

// tickShopRerollLocked auto-refreshes neutral-shop stock on a wave cadence. Once
// per wave-end (edge-detected via WaveManager.ShopRerollWave, exactly like the
// neutral-camp reset), every neutral shop whose reroll interval divides the
// just-completed wave number is re-sampled for EVERY player independently, each
// at their own effective item count. Fixed-inventory shops are skipped
// (author-committed, not rerollable). On non-wave maps CurrentWave stays 0 so
// this never fires. Iteration is sorted (shops by ID, players by ID) so
// re-sample rolls are deterministic. Must be called under s.mu write lock.
func (s *GameState) tickShopRerollLocked() {
	wm := &s.WaveManager
	// A wave has ended when it left "active" (but has begun and is not terminal).
	// Mirrors tickNeutralCampsLocked's edge condition.
	waveEnded := wm.CurrentWave >= 1 && wm.State != "active" && wm.State != "complete"
	if !waveEnded || wm.ShopRerollWave >= wm.CurrentWave {
		return
	}
	wm.ShopRerollWave = wm.CurrentWave

	playerIDs := s.sortedRealPlayerIDsLocked()
	for _, idx := range s.sortedNeutralShopIndicesLocked() {
		b := &s.MapConfig.Buildings[idx]
		if len(b.ShopFixedInventory) > 0 {
			continue
		}
		interval := neutralShopRerollWavesFor(b)
		if interval <= 0 || wm.CurrentWave%interval != 0 {
			continue
		}
		for _, pid := range playerIDs {
			s.stockNeutralShopForPlayerLocked(pid, b, s.shopItemTargetCountForPlayerLocked(pid))
		}
	}
}

// RerollShop is the public entry point for rerolling a neutral shop's
// inventory. Acquires s.mu and delegates to handleRerollShopLocked.
func (s *GameState) RerollShop(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleRerollShopLocked(playerID, buildingID)
}

// rollShopLootTableLocked rolls the named table repeatedly until it has
// accumulated `targetCount` distinct item IDs, or until the attempt budget
// (targetCount * 8) is exhausted. Returns (nil, false) and logs when the table
// is missing.
//
// Rows that grant resources or `nothing` are skipped — a shop sells items, and
// those outcomes simply produce no shelf entry — as are duplicates. A degenerate
// table (all-resources, say) cannot produce targetCount items, so the attempt
// budget is what stops this looping forever; it returns whatever it collected.
//
// Roll order is RNG-deterministic. Must be called under s.mu write lock.
func (s *GameState) rollShopLootTableLocked(buildingID, tableID string, targetCount int) ([]string, bool) {
	table, ok := getTableDef(tableID)
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
		result := s.rollTableLocked(table)
		for _, id := range result.Items {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			items = append(items, id)
		}
	}
	return items, true
}

// spawnShopGuardsLocked walks every neutral shop building (neutral-shop
// merchants and recipe-shop recipe traders), reads its optional guard
// metadata (`guardGroupId`, `guardStartingTier`, `guardAggroRange`,
// `guardLeashRange`), and spawns the declared squad around the building's
// perimeter. Spawned unit IDs are stored on `BuildingTile.ShopGuardUnitIDs`
// so shopLockedLocked can read them. Buildings with no `guardGroupId` are
// skipped (unlocked from spawn) — guards are opt-in per placed building.
//
// Iteration order is sorted by building ID so guard composition rolls
// are deterministic across runs. Must be called under s.mu write lock.
// Idempotent — safe to call again only if existing guards are dead;
// no logic prevents re-spawning, so callers should run once at init.
func (s *GameState) spawnShopGuardsLocked() {
	indices := make([]int, 0)
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "neutral-shop" && b.BuildingType != "recipe-shop" {
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

		// Guards ring around (and anchor to) the shop footprint center by
		// default. A map-editor-chosen spawn cell (guardSpawnX/Y metadata)
		// overrides that center, letting the author place the squad off to one
		// side of the shop. Both coordinates must be present for the override.
		centerWX := (float64(b.X) + float64(b.Width)/2) * cellSize
		centerWY := (float64(b.Y) + float64(b.Height)/2) * cellSize
		if sx, okX := getMetadataFloat(b.Metadata, "guardSpawnX"); okX {
			if sy, okY := getMetadataFloat(b.Metadata, "guardSpawnY"); okY {
				spawnCenter := s.gridToWorldCenter(gridPoint{X: int(sx), Y: int(sy)})
				centerWX, centerWY = spawnCenter.X, spawnCenter.Y
			}
		}
		centerCell := s.worldToGrid(centerWX, centerWY)
		placedOrderID := s.nextMovementOrderIDLocked()
		// Guards anchor to the spawn center's region so ring displacement
		// can't strand one in a sealed pocket beside the shop. When the
		// center sits inside the shop footprint (blocked, region 0) this
		// degrades to the unconstrained search.
		centerRegion := s.walkableRegionAtLocked(centerCell)

		spawnIdx := 0
		for _, entry := range group.Composition {
			for i := 0; i < entry.Count; i++ {
				offsetCell := neutralCampRingOffset(centerCell, spawnIdx)
				spawnCell, found := s.findNearestWalkableInRegionLocked(offsetCell, centerRegion, blocked, nil)
				if !found {
					spawnCell, found = s.findNearestWalkableInRegionLocked(centerCell, centerRegion, blocked, nil)
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
