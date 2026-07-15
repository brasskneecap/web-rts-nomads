package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// makeShopTestState creates a GameState with one player ("p1") joined and
// returns it. Callers append/modify Buildings under s.mu.Lock() and then call
// reindexShopTestState() to rebuild buildingsByID and re-run the shop init.
func makeShopTestState(t *testing.T, seed int64) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
	s.EnsurePlayer("p1")
	return s
}

// reindexShopTestState rebuilds buildingsByID and runs the shop-init helpers
// against the current MapConfig.Buildings. Equivalent to the work
// setMapConfigLocked normally does, but callable mid-test after Buildings
// has been mutated. Caller must hold s.mu.
func reindexShopTestState(s *GameState) {
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	for k := range s.buildingsByID {
		delete(s.buildingsByID, k)
	}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.initShopBuildingsLocked()
	s.populateShopInventoriesLocked()
	// Neutral shops are per-player: sample each joined player's independent view
	// (mirrors what EnsurePlayerWithUpgrades does at match join).
	for _, pid := range s.sortedRealPlayerIDsLocked() {
		s.populatePlayerNeutralShopViewsLocked(pid)
	}
	s.spawnShopGuardsLocked()
}

// playerShopInventoryItemIDs flattens a player's independent view of a neutral
// shop into item IDs (slot order), for assertions that only care about which
// items are stocked.
func playerShopInventoryItemIDs(s *GameState, playerID, buildingID string) []string {
	p, ok := s.Players[playerID]
	if !ok {
		return nil
	}
	entries := p.NeutralShopInventories[buildingID]
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ItemID)
	}
	return out
}

// addShopBuilding appends a shop building of the given type with optional
// authored inventory metadata.
func addShopBuilding(s *GameState, bID, buildingType string, ownerID *string, fixedInventory []string, lootTableID string, guardGroupID string) {
	meta := map[string]interface{}{}
	if guardGroupID != "" {
		meta["guardGroupId"] = guardGroupID
	}
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:                 bID,
		BuildingType:       buildingType,
		Width:              2,
		Height:             2,
		Visible:            true,
		Occupied:           true,
		OwnerID:            ownerID,
		Capabilities:       []string{"item-purchase"},
		Metadata:           meta,
		ShopFixedInventory: fixedInventory,
		ShopLootTableID:    lootTableID,
	})
}

// shopInventoryItemIDs is a test helper that flattens a building's stock
// entries down to a slice of item IDs in slot order. Used by assertions
// that only care about which items are stocked, not their quantity.
func shopInventoryItemIDs(b *protocol.BuildingTile) []string {
	out := make([]string, 0, len(b.ShopInventory))
	for _, e := range b.ShopInventory {
		out = append(out, e.ItemID)
	}
	return out
}

// TestPopulateShopInventories_FixedList — Requirement 1, precedence 1.
func TestPopulateShopInventories_FixedList(t *testing.T) {
	s := makeShopTestState(t, 1)
	owner := "p1"
	want := []string{"broad_sword", "potion_common_heal"}

	s.mu.Lock()
	addShopBuilding(s, "ms-fixed", "marketplace", &owner, want, "", "")
	reindexShopTestState(s)
	b := s.buildingsByID["ms-fixed"]
	got := shopInventoryItemIDs(b)
	wantQty := starterStockForBuildingType("marketplace")
	for _, e := range b.ShopInventory {
		if e.Quantity != wantQty {
			t.Errorf("entry %q quantity: want %d, got %d", e.ItemID, wantQty, e.Quantity)
		}
	}
	s.mu.Unlock()

	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ShopInventory item ids: want %v, got %v", want, got)
	}
}

// TestPopulateShopInventories_ListMetadata verifies a neutral-shop bound to a
// list treats it as a POOL and stocks a sampled subset
// sized to the base item count — every stocked item drawn from the list, no
// duplicates. Counts/expectations derive from the catalog + tuning, not literals.
func TestPopulateShopInventories_ListMetadata(t *testing.T) {
	const listID = "marketplace"
	list, ok := getListDef(listID)
	if !ok {
		t.Fatalf("expected list %q in catalog", listID)
	}
	inList := map[string]bool{}
	for _, id := range list.Items {
		inList[id] = true
	}

	s := makeShopTestState(t, 1)
	neutral := neutralPlayerID

	s.mu.Lock()
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:           "merchant-listed",
		BuildingType: "neutral-shop",
		Width:        3,
		Height:       3,
		Visible:      true,
		Occupied:     true,
		OwnerID:      &neutral,
		Capabilities: []string{"item-purchase"},
		Metadata:     map[string]interface{}{"list": listID},
	})
	reindexShopTestState(s)
	got := playerShopInventoryItemIDs(s, "p1", "merchant-listed")
	s.mu.Unlock()

	wantCount := neutralShopBaseItemCount()
	if len(list.Items) < wantCount {
		wantCount = len(list.Items)
	}
	if len(got) != wantCount {
		t.Fatalf("sampled %d items from the pool, want %d (pool=%v)", len(got), wantCount, list.Items)
	}
	seen := map[string]bool{}
	for _, id := range got {
		if !inList[id] {
			t.Errorf("stocked item %q is not in the pool %v", id, list.Items)
		}
		if seen[id] {
			t.Errorf("stocked item %q sampled more than once", id)
		}
		seen[id] = true
	}
}

// TestShopWaveReroll_ResamplesOnCadenceRespectsPerShopOverride verifies the
// wave-based auto-reroll: a shop re-samples its list pool when the
// just-completed wave is a multiple of its interval, a shop with rerollWaves=0
// is never touched, and the edge marker fires the check once per wave.
func TestShopWaveReroll_ResamplesOnCadenceRespectsPerShopOverride(t *testing.T) {
	const listID = "wandering_merchant"
	list, ok := getListDef(listID)
	if !ok {
		t.Skipf("list %q not present", listID)
	}
	// Sentinel item NOT in the pool, so any re-sample provably replaces it.
	sentinel := "broad_sword"
	for _, id := range list.Items {
		if id == sentinel {
			t.Fatalf("test sentinel %q must not be in pool %v", sentinel, list.Items)
		}
	}

	s := makeShopTestState(t, 3)
	neutral := neutralPlayerID
	s.mu.Lock()
	defer s.mu.Unlock()
	// Shop A: default cadence (tuning rerollEveryWaves). Shop B: disabled (0).
	s.MapConfig.Buildings = append(s.MapConfig.Buildings,
		protocol.BuildingTile{
			ID: "merchant-A", BuildingType: "neutral-shop", Width: 3, Height: 3,
			Visible: true, Occupied: true, OwnerID: &neutral,
			Capabilities: []string{"item-purchase"},
			Metadata:     map[string]interface{}{"list": listID},
		},
		protocol.BuildingTile{
			ID: "merchant-B", BuildingType: "neutral-shop", Width: 3, Height: 3,
			Visible: true, Occupied: true, OwnerID: &neutral,
			Capabilities: []string{"item-purchase"},
			Metadata:     map[string]interface{}{"list": listID, "rerollWaves": float64(0)},
		},
	)
	reindexShopTestState(s)

	// Overwrite p1's views with the sentinel so a re-sample is observable.
	setSentinel := func() {
		s.Players["p1"].NeutralShopInventories["merchant-A"] = makeShopStockEntries([]string{sentinel}, "neutral-shop")
		s.Players["p1"].NeutralShopInventories["merchant-B"] = makeShopStockEntries([]string{sentinel}, "neutral-shop")
	}
	setSentinel()

	interval := neutralShopDefaultRerollWaves()
	if interval <= 0 {
		t.Skip("default reroll cadence disabled; nothing to verify")
	}

	// Odd wave (not a multiple of interval, for interval>=2): shop A is NOT
	// rerolled. Use interval-1 which is guaranteed non-divisible for interval>=2.
	nonMultiple := interval - 1
	if nonMultiple >= 1 && nonMultiple%interval != 0 {
		s.WaveManager.CurrentWave = nonMultiple
		s.WaveManager.State = "upgrade"
		s.tickShopRerollLocked()
		if got := playerShopInventoryItemIDs(s, "p1", "merchant-A"); len(got) != 1 || got[0] != sentinel {
			t.Errorf("wave %d (not a multiple of %d): shop A should not reroll, got %v", nonMultiple, interval, got)
		}
	}

	// Multiple-of-interval wave: shop A re-samples (sentinel replaced), shop B
	// (disabled) stays put.
	setSentinel()
	s.WaveManager.ShopRerollWave = 0
	s.WaveManager.CurrentWave = interval // interval % interval == 0
	s.WaveManager.State = "upgrade"
	s.tickShopRerollLocked()

	gotA := playerShopInventoryItemIDs(s, "p1", "merchant-A")
	if len(gotA) == 1 && gotA[0] == sentinel {
		t.Errorf("wave %d (multiple of %d): shop A should have re-sampled, still sentinel %v", interval, interval, gotA)
	}
	if len(gotA) != neutralShopBaseItemCount() {
		t.Errorf("shop A re-sample count: want %d, got %d (%v)", neutralShopBaseItemCount(), len(gotA), gotA)
	}
	if gotB := playerShopInventoryItemIDs(s, "p1", "merchant-B"); len(gotB) != 1 || gotB[0] != sentinel {
		t.Errorf("shop B (rerollWaves=0) must never auto-reroll, got %v", gotB)
	}

	// Edge marker: a second tick at the same wave is a no-op (re-set sentinel,
	// tick, expect it untouched because ShopRerollWave already == CurrentWave).
	s.Players["p1"].NeutralShopInventories["merchant-A"] = makeShopStockEntries([]string{sentinel}, "neutral-shop")
	s.tickShopRerollLocked()
	if got := playerShopInventoryItemIDs(s, "p1", "merchant-A"); len(got) != 1 || got[0] != sentinel {
		t.Errorf("second tick at same wave should be a no-op (edge marker), but shop A changed: %v", got)
	}
}

// TestNeutralShop_PerPlayerIndependentViews verifies neutral shops are
// per-player: each player's view is sized to their own ShopItemCountBonus, and
// one player's purchase never affects another's stock.
func TestNeutralShop_PerPlayerIndependentViews(t *testing.T) {
	const listID = "wandering_merchant"
	list, ok := getListDef(listID)
	if !ok {
		t.Skipf("list %q not present", listID)
	}
	base := neutralShopBaseItemCount()
	if len(list.Items) < base+1 {
		t.Skipf("pool too small (%d) to distinguish per-player counts", len(list.Items))
	}

	s := makeShopTestState(t, 5)
	s.mu.Lock()
	// A second player with a +1 "expanded selection" upgrade (constructed
	// directly to avoid the spawn/claim machinery — we only need its shop view).
	s.Players["p2"] = &Player{ID: "p2", ShopItemCountBonus: 1}
	neutral := neutralPlayerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID: "merchant", BuildingType: "neutral-shop", Width: 3, Height: 3,
		Visible: true, Occupied: true, OwnerID: &neutral,
		Capabilities: []string{"item-purchase"},
		Metadata:     map[string]interface{}{"list": listID},
	})
	reindexShopTestState(s)
	p1View := playerShopInventoryItemIDs(s, "p1", "merchant")
	p2View := playerShopInventoryItemIDs(s, "p2", "merchant")
	s.mu.Unlock()

	// Each player's view is sized to their own count.
	if len(p1View) != base {
		t.Errorf("p1 view count: want %d, got %d (%v)", base, len(p1View), p1View)
	}
	if len(p2View) != base+1 {
		t.Errorf("p2 view count (with +1 bonus): want %d, got %d (%v)", base+1, len(p2View), p2View)
	}

	// Independence: p1 buying from their own view leaves p2's view untouched.
	s.mu.Lock()
	b := s.buildingsByID["merchant"]
	clone := *b
	s.FOW["p1"].KnownBuildings[b.ID] = &clone
	s.Players["p1"].Resources["gold"] = 1000000
	buyItem := s.Players["p1"].NeutralShopInventories["merchant"][0].ItemID
	p2Before := append([]protocol.ShopStockEntry(nil), s.Players["p2"].NeutralShopInventories["merchant"]...)
	s.mu.Unlock()

	s.PurchaseItem("p1", "merchant", buyItem)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if q := s.Players["p1"].NeutralShopInventories["merchant"][0].Quantity; q != 0 {
		t.Errorf("after p1 buys %q, p1's own view qty: want 0, got %d", buyItem, q)
	}
	p2After := s.Players["p2"].NeutralShopInventories["merchant"]
	if len(p2After) != len(p2Before) {
		t.Fatalf("p2 view length changed by p1's purchase: %d → %d", len(p2Before), len(p2After))
	}
	for i := range p2Before {
		if p2Before[i] != p2After[i] {
			t.Errorf("p1's purchase mutated p2's view at slot %d: %+v → %+v", i, p2Before[i], p2After[i])
		}
	}
}

// TestPopulateShopInventories_LootTableDeterministic — Requirement 1,
// precedence 2 + determinism scenario.
func TestPopulateShopInventories_LootTableDeterministic(t *testing.T) {
	rollWithSeed := func(seed int64) []string {
		s := makeShopTestState(t, seed)
		s.mu.Lock()
		defer s.mu.Unlock()
		// Any shipped table will do — the point is determinism, not the odds.
		var tableID string
		for _, candidate := range []string{"raider_loot", "wildborne_loot"} {
			if _, ok := getTableDef(candidate); ok {
				tableID = candidate
				break
			}
		}
		if tableID == "" {
			t.Skip("no loot table available in test catalog")
		}
		addShopBuilding(s, "ms-roll", "neutral-shop", nil, nil, tableID, "")
		reindexShopTestState(s)
		return shopInventoryItemIDs(s.buildingsByID["ms-roll"])
	}

	first := rollWithSeed(2026)
	second := rollWithSeed(2026)
	if len(first) != len(second) {
		t.Fatalf("loot-roll length differs across same-seed runs: first=%v second=%v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("loot-roll order differs at index %d: first=%v second=%v", i, first, second)
		}
	}
}

// TestPopulateShopInventories_MarketplaceFallback — Requirement 1, precedence 3.
// A player-built marketplace with no authored shopFixedInventory or
// shopLootTableId stocks the authored "marketplace" item list
// (catalog/items/lists/marketplace.json), not the entire catalog.
func TestPopulateShopInventories_MarketplaceFallback(t *testing.T) {
	s := makeShopTestState(t, 1)
	owner := "p1"

	s.mu.Lock()
	addShopBuilding(s, "ms-default", "marketplace", &owner, nil, "", "")
	reindexShopTestState(s)
	got := shopInventoryItemIDs(s.buildingsByID["ms-default"])
	s.mu.Unlock()

	marketplaceList, ok := getListDef("marketplace")
	if !ok {
		t.Fatal(`item list "marketplace" not found`)
	}
	want := marketplaceList.Items
	if len(got) != len(want) {
		t.Fatalf("marketplace fallback length: want %d, got %d (want=%v got=%v)", len(want), len(got), want, got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("at %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

// TestPopulateShopInventories_UnknownLootTable_LogsAndSkips — Requirement 1
// error path: bogus loot-table id leaves ShopInventory nil and does not panic.
func TestPopulateShopInventories_UnknownLootTable_LogsAndSkips(t *testing.T) {
	s := makeShopTestState(t, 1)

	s.mu.Lock()
	addShopBuilding(s, "ms-bogus", "neutral-shop", nil, nil, "nonexistent_table_xyz", "")
	defer s.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("expected no panic; got %v", r)
		}
	}()
	reindexShopTestState(s)
	if got := s.buildingsByID["ms-bogus"].ShopInventory; got != nil {
		t.Errorf("expected ShopInventory nil after unknown loot table; got %v", got)
	}
}

// TestShopGuards_LockedWhileAlive — Requirement: guard-locking.
func TestShopGuards_LockedWhileAlive(t *testing.T) {
	// Discover a guard group id present in the catalog.
	var anyGroupID string
	for tier := 1; tier <= 10 && anyGroupID == ""; tier++ {
		if ids := listNeutralGroupIDs(tier); len(ids) > 0 {
			anyGroupID = ids[0]
			break
		}
	}
	if anyGroupID == "" {
		t.Skip("no neutral group available")
	}

	s := makeShopTestState(t, 1)
	s.mu.Lock()
	addShopBuilding(s, "ms-guarded", "neutral-shop", nil, []string{"broad_sword"}, "", anyGroupID)
	reindexShopTestState(s)
	b := s.buildingsByID["ms-guarded"]
	if len(b.ShopGuardUnitIDs) == 0 {
		s.mu.Unlock()
		t.Fatal("expected at least one guard unit spawned")
	}
	if !s.shopLockedLocked(b) {
		s.mu.Unlock()
		t.Fatal("expected shop to be locked while guards are alive")
	}
	// Kill the guards.
	for _, id := range b.ShopGuardUnitIDs {
		if u := s.getUnitByIDLocked(id); u != nil {
			u.HP = 0
		}
	}
	if s.shopLockedLocked(b) {
		t.Error("expected shop unlocked after all guards reach HP 0")
	}
	s.mu.Unlock()
}

// TestPurchase_RejectsIfItemNotInInventory — Requirement: purchase validation.
func TestPurchase_RejectsIfItemNotInInventory(t *testing.T) {
	s := makeShopTestState(t, 1)
	owner := "p1"

	s.mu.Lock()
	// Fixed inventory deliberately excludes broad_sword.
	addShopBuilding(s, "ms-narrow", "marketplace", &owner, []string{"potion_common_heal"}, "", "")
	reindexShopTestState(s)
	s.mu.Unlock()

	goldBefore := s.Players["p1"].Resources["gold"]
	s.PurchaseItem("p1", "ms-narrow", "broad_sword")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Players["p1"].Vault); got != 0 {
		t.Errorf("expected no vault entry, got %d", got)
	}
	if s.Players["p1"].Resources["gold"] != goldBefore {
		t.Errorf("expected no gold deducted: before=%d, after=%d", goldBefore, s.Players["p1"].Resources["gold"])
	}
}

// TestPurchase_RejectsFromUndiscoveredNeutralShop — Requirement: discovery gate.
func TestPurchase_RejectsFromUndiscoveredNeutralShop(t *testing.T) {
	s := makeShopTestState(t, 1)
	s.mu.Lock()
	// Neutral shop, no guards (unlocked from start), fixed inventory.
	addShopBuilding(s, "ms-undiscovered", "neutral-shop", nil, []string{"broad_sword"}, "", "")
	reindexShopTestState(s)
	// Sanity: building belongs to neutral player.
	b := s.buildingsByID["ms-undiscovered"]
	if b.OwnerID == nil || *b.OwnerID != neutralPlayerID {
		s.mu.Unlock()
		t.Fatalf("expected neutral ownership, got %v", b.OwnerID)
	}
	// Sanity: p1 has not discovered it.
	if fow := s.FOW["p1"]; fow != nil {
		if _, has := fow.KnownBuildings[b.ID]; has {
			s.mu.Unlock()
			t.Fatal("test setup: p1 already discovered the shop")
		}
	}
	s.mu.Unlock()

	s.PurchaseItem("p1", "ms-undiscovered", "broad_sword")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Players["p1"].Vault); got != 0 {
		t.Errorf("expected purchase rejected (undiscovered), got %d vault items", got)
	}
}

// TestPurchase_RejectsFromLockedNeutralShop — Requirement: discovery + lock gate.
func TestPurchase_RejectsFromLockedNeutralShop(t *testing.T) {
	var anyGroupID string
	for tier := 1; tier <= 10 && anyGroupID == ""; tier++ {
		if ids := listNeutralGroupIDs(tier); len(ids) > 0 {
			anyGroupID = ids[0]
			break
		}
	}
	if anyGroupID == "" {
		t.Skip("no neutral group available")
	}

	s := makeShopTestState(t, 1)
	s.mu.Lock()
	addShopBuilding(s, "ms-locked", "neutral-shop", nil, []string{"broad_sword"}, "", anyGroupID)
	reindexShopTestState(s)
	b := s.buildingsByID["ms-locked"]
	// Force-discover the shop for p1 so the lock is the only remaining gate.
	if s.FOW["p1"] == nil {
		s.mu.Unlock()
		t.Fatal("p1 FOW missing; EnsurePlayer should have created it")
	}
	clone := *b
	s.FOW["p1"].KnownBuildings[b.ID] = &clone
	if !s.shopLockedLocked(b) {
		s.mu.Unlock()
		t.Fatal("expected locked precondition")
	}
	s.mu.Unlock()

	s.PurchaseItem("p1", "ms-locked", "broad_sword")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Players["p1"].Vault); got != 0 {
		t.Errorf("expected purchase rejected (locked), got %d vault items", got)
	}
}

// TestPurchase_SucceedsFromClearedDiscoveredNeutralShop — happy path.
func TestPurchase_SucceedsFromClearedDiscoveredNeutralShop(t *testing.T) {
	s := makeShopTestState(t, 1)
	s.mu.Lock()
	addShopBuilding(s, "ms-open", "neutral-shop", nil, []string{"broad_sword"}, "", "")
	reindexShopTestState(s)
	b := s.buildingsByID["ms-open"]
	// Force-discover for p1.
	clone := *b
	s.FOW["p1"].KnownBuildings[b.ID] = &clone
	if s.shopLockedLocked(b) {
		s.mu.Unlock()
		t.Fatal("expected unlocked precondition (no guards)")
	}
	// Ensure p1 has gold.
	s.Players["p1"].Resources["gold"] = 1000
	s.mu.Unlock()

	s.PurchaseItem("p1", "ms-open", "broad_sword")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Players["p1"].Vault); got != 1 {
		t.Errorf("expected 1 vault item after purchase, got %d", got)
	}
	if s.Players["p1"].Resources["gold"] >= 1000 {
		t.Errorf("expected gold deducted, still have %d", s.Players["p1"].Resources["gold"])
	}
}

// TestNeutralShopDefault_RollsMerchantBasic verifies that an authored
// neutral-shop with no shopFixedInventory or shopLootTableId override
// defaults to rolling the merchant_basic loot table and ends up with
// defaultShopLootTargetCount distinct items (currently 3).
func TestNeutralShopDefault_RollsMerchantBasic(t *testing.T) {
	s := makeShopTestState(t, 1)
	s.mu.Lock()
	addShopBuilding(s, "ms-default-neutral", "neutral-shop", nil, nil, "", "")
	reindexShopTestState(s)
	got := shopInventoryItemIDs(s.buildingsByID["ms-default-neutral"])
	s.mu.Unlock()

	if len(got) != neutralShopBaseItemCount() {
		t.Errorf("neutral-shop default: want %d items, got %d (%v)",
			neutralShopBaseItemCount(), len(got), got)
	}
	// Every rolled item ID must resolve in the catalog.
	for _, id := range got {
		if _, ok := getItemDef(id); !ok {
			t.Errorf("rolled item %q is not in the catalog", id)
		}
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Errorf("rolled item %q appears more than once", id)
		}
		seen[id] = true
	}
}

// TestMarketplaceStarterInventory_ItemsExistInCatalog guards against typos
// in defaultMarketplaceStarterInventory by asserting every ID resolves in
// the item catalog. A typo would silently produce a shop with no items.
func TestMarketplaceStarterInventory_ItemsExistInCatalog(t *testing.T) {
	if len(defaultMarketplaceStarterInventory) == 0 {
		t.Fatal("defaultMarketplaceStarterInventory is empty; the marketplace fallback path would be dead")
	}
	for _, id := range defaultMarketplaceStarterInventory {
		if _, ok := getItemDef(id); !ok {
			t.Errorf("starter inventory references unknown item %q", id)
		}
	}
}

// TestPurchase_DecrementsStockAndRejectsAtZero verifies that buying an item
// from a neutral merchant (single-stock) decrements the entry's quantity
// to 0 and that subsequent attempts to buy the same item are silent no-ops
// while the entry remains in the inventory (greyed-out on the client).
func TestPurchase_DecrementsStockAndRejectsAtZero(t *testing.T) {
	s := makeShopTestState(t, 1)
	s.mu.Lock()
	addShopBuilding(s, "ms-stock", "neutral-shop", nil, []string{"broad_sword"}, "", "")
	reindexShopTestState(s)
	b := s.buildingsByID["ms-stock"]
	clone := *b
	s.FOW["p1"].KnownBuildings[b.ID] = &clone
	s.Players["p1"].Resources["gold"] = 10000
	s.mu.Unlock()

	// First purchase: succeeds, the buyer's own view quantity drops from 1 to 0.
	s.PurchaseItem("p1", "ms-stock", "broad_sword")
	s.mu.RLock()
	if got := len(s.Players["p1"].Vault); got != 1 {
		s.mu.RUnlock()
		t.Fatalf("first purchase: want 1 vault item, got %d", got)
	}
	if q := s.Players["p1"].NeutralShopInventories["ms-stock"][0].Quantity; q != 0 {
		s.mu.RUnlock()
		t.Errorf("after purchase: want Quantity 0 in buyer's view, got %d", q)
	}
	s.mu.RUnlock()

	// Second purchase attempt: rejected, entry stays in the buyer's view.
	s.PurchaseItem("p1", "ms-stock", "broad_sword")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Players["p1"].Vault); got != 1 {
		t.Errorf("second purchase should be rejected; vault: want 1, got %d", got)
	}
	if got := len(s.Players["p1"].NeutralShopInventories["ms-stock"]); got != 1 {
		t.Errorf("entry should remain in buyer's view greyed-out; len: want 1, got %d", got)
	}
}

// TestRerollShop_AppliesPlayerItemCountBonus verifies that a player with
// ShopItemCountBonus > 0 gets that many extra distinct items when they
// reroll a merchant. This is the seam future dominion-point profile
// upgrades plug into — bumping the field is the only change needed for
// the upgrade to take effect.
func TestRerollShop_AppliesPlayerItemCountBonus(t *testing.T) {
	s := makeShopTestState(t, 99)
	s.mu.Lock()
	addShopBuilding(s, "ms-bonus", "neutral-shop", nil, nil, "", "")
	reindexShopTestState(s)
	b := s.buildingsByID["ms-bonus"]
	clone := *b
	s.FOW["p1"].KnownBuildings[b.ID] = &clone
	// Grant the player a bonus and refill the reroll budget so we can spend it.
	s.Players["p1"].ShopItemCountBonus = 2
	s.Players["p1"].ShopRerollsRemaining = 1
	s.mu.Unlock()

	s.RerollShop("p1", "ms-bonus")

	s.mu.RLock()
	defer s.mu.RUnlock()
	got := playerShopInventoryItemIDs(s, "p1", "ms-bonus")
	want := neutralShopBaseItemCount() + 2
	if len(got) != want {
		t.Errorf("rerolled view length with +2 bonus: want %d, got %d (%v)", want, len(got), got)
	}
}

// TestRerollShop_RegeneratesInventoryAndDecrementsBudget verifies the
// reroll handler replaces a neutral-shop's inventory with a fresh roll
// and decrements the player's ShopRerollsRemaining. A player with zero
// rerolls remaining gets no inventory change.
func TestRerollShop_RegeneratesInventoryAndDecrementsBudget(t *testing.T) {
	s := makeShopTestState(t, 7)
	s.mu.Lock()
	addShopBuilding(s, "ms-reroll", "neutral-shop", nil, nil, "", "")
	reindexShopTestState(s)
	b := s.buildingsByID["ms-reroll"]
	clone := *b
	s.FOW["p1"].KnownBuildings[b.ID] = &clone
	// EnsurePlayer set ShopRerollsRemaining to defaultShopRerollsPerPlayer.
	startingBudget := s.Players["p1"].ShopRerollsRemaining
	beforeIDs := playerShopInventoryItemIDs(s, "p1", "ms-reroll")
	s.mu.Unlock()

	if startingBudget <= 0 {
		t.Fatalf("test setup: expected starting reroll budget > 0, got %d", startingBudget)
	}

	s.RerollShop("p1", "ms-reroll")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := s.Players["p1"].ShopRerollsRemaining; got != startingBudget-1 {
		t.Errorf("reroll budget: want %d, got %d", startingBudget-1, got)
	}
	// The buyer's view should still have base-count items (may or may not be
	// identical to beforeIDs depending on RNG — we just assert the length and
	// that every ID is in the catalog).
	got := playerShopInventoryItemIDs(s, "p1", "ms-reroll")
	if len(got) != neutralShopBaseItemCount() {
		t.Errorf("rerolled view length: want %d, got %d (before=%v after=%v)",
			neutralShopBaseItemCount(), len(got), beforeIDs, got)
	}
	for _, id := range got {
		if _, ok := getItemDef(id); !ok {
			t.Errorf("rerolled item %q is not in the catalog", id)
		}
	}

	// Attempt a second reroll with budget at 0: no-op, view unchanged.
	beforeSecond := playerShopInventoryItemIDs(s, "p1", "ms-reroll")
	s.mu.RUnlock()
	s.RerollShop("p1", "ms-reroll")
	s.mu.RLock()
	afterSecond := playerShopInventoryItemIDs(s, "p1", "ms-reroll")
	if len(beforeSecond) != len(afterSecond) {
		t.Errorf("second reroll on empty budget should be no-op; len differs %d→%d", len(beforeSecond), len(afterSecond))
	} else {
		for i := range beforeSecond {
			if beforeSecond[i] != afterSecond[i] {
				t.Errorf("second reroll mutated inventory at index %d: %q → %q", i, beforeSecond[i], afterSecond[i])
			}
		}
	}
	if got := s.Players["p1"].ShopRerollsRemaining; got != 0 {
		t.Errorf("second reroll: budget should still be 0, got %d", got)
	}
}
