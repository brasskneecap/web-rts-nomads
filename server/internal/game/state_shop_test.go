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
	s.spawnShopGuardsLocked()
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

// TestPopulateShopInventories_LootTableDeterministic — Requirement 1,
// precedence 2 + determinism scenario.
func TestPopulateShopInventories_LootTableDeterministic(t *testing.T) {
	rollWithSeed := func(seed int64) []string {
		s := makeShopTestState(t, seed)
		s.mu.Lock()
		defer s.mu.Unlock()
		// Use any existing loot table id. neutral_groups/loot_tables.json
		// ships one named "tier1_loot" (or similar). Look up the first
		// available table id by exploring the loaded catalog via a dummy
		// roll: we'll just pick one by reflection through getLootTable on
		// the conventional name. To keep the test robust to catalog drift
		// we walk the embedded catalog if needed.
		var tableID string
		for _, candidate := range []string{"raider_loot", "wildborne_loot"} {
			if _, ok := getLootTable(candidate); ok {
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
// shopLootTableId ships with the focused defaultMarketplaceStarterInventory
// (currently broad_sword + minor healing potion), not the entire catalog.
func TestPopulateShopInventories_MarketplaceFallback(t *testing.T) {
	s := makeShopTestState(t, 1)
	owner := "p1"

	s.mu.Lock()
	addShopBuilding(s, "ms-default", "marketplace", &owner, nil, "", "")
	reindexShopTestState(s)
	got := shopInventoryItemIDs(s.buildingsByID["ms-default"])
	s.mu.Unlock()

	want := defaultMarketplaceStarterInventory
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

	if len(got) != defaultShopLootTargetCount {
		t.Errorf("neutral-shop default: want %d items, got %d (%v)",
			defaultShopLootTargetCount, len(got), got)
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

	// First purchase: succeeds, quantity drops from 1 to 0.
	s.PurchaseItem("p1", "ms-stock", "broad_sword")
	s.mu.RLock()
	if got := len(s.Players["p1"].Vault); got != 1 {
		s.mu.RUnlock()
		t.Fatalf("first purchase: want 1 vault item, got %d", got)
	}
	if q := s.buildingsByID["ms-stock"].ShopInventory[0].Quantity; q != 0 {
		s.mu.RUnlock()
		t.Errorf("after purchase: want Quantity 0, got %d", q)
	}
	s.mu.RUnlock()

	// Second purchase attempt: rejected, entry stays in inventory.
	s.PurchaseItem("p1", "ms-stock", "broad_sword")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Players["p1"].Vault); got != 1 {
		t.Errorf("second purchase should be rejected; vault: want 1, got %d", got)
	}
	if got := len(s.buildingsByID["ms-stock"].ShopInventory); got != 1 {
		t.Errorf("entry should remain in inventory greyed-out; len: want 1, got %d", got)
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
	beforeIDs := shopInventoryItemIDs(s.buildingsByID["ms-reroll"])
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
	// Inventory should still have defaultShopLootTargetCount items (may
	// or may not be identical to beforeIDs depending on the RNG state —
	// we just assert the length and that every ID is in the catalog).
	got := shopInventoryItemIDs(s.buildingsByID["ms-reroll"])
	if len(got) != defaultShopLootTargetCount {
		t.Errorf("rerolled inventory length: want %d, got %d (before=%v after=%v)",
			defaultShopLootTargetCount, len(got), beforeIDs, got)
	}
	for _, id := range got {
		if _, ok := getItemDef(id); !ok {
			t.Errorf("rerolled item %q is not in the catalog", id)
		}
	}

	// Attempt a second reroll with budget at 0: no-op, inventory unchanged.
	beforeSecond := shopInventoryItemIDs(s.buildingsByID["ms-reroll"])
	s.mu.RUnlock()
	s.RerollShop("p1", "ms-reroll")
	s.mu.RLock()
	afterSecond := shopInventoryItemIDs(s.buildingsByID["ms-reroll"])
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
