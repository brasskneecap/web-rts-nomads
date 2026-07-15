package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// registerTestList puts a list in the catalog for one test.
func registerTestList(t *testing.T, def *ListDef) {
	t.Helper()
	listCatalogSingleton[def.ID] = def
	t.Cleanup(func() { delete(listCatalogSingleton, def.ID) })
}

// ─── The list entity ────────────────────────────────────────────────────────

// TestListCatalog_ShippedListsResolve: the three lists that survived the
// item-list / recipe-list merge all load and name only real items.
func TestListCatalog_ShippedListsResolve(t *testing.T) {
	for _, id := range []string{"marketplace", "wandering_merchant", "druid_recipes_1"} {
		list, ok := getListDef(id)
		if !ok {
			t.Errorf("list %q not found", id)
			continue
		}
		if len(list.Items) == 0 {
			t.Errorf("list %q is empty", id)
		}
		for i, itemID := range list.Items {
			if _, ok := getItemDef(itemID); !ok {
				t.Errorf("list %q items[%d] %q is not a known item", id, i, itemID)
			}
		}
	}
}

// TestValidateListDef_Rules: a list needs at least one member, and every member
// must name a real item. It deliberately does NOT care what the members are FOR
// — a list is untyped.
func TestValidateListDef_Rules(t *testing.T) {
	if err := validateListDef(&ListDef{ID: "l", Name: "L"}); err == nil {
		t.Error("expected error for an empty list")
	}
	if err := validateListDef(&ListDef{ID: "l", Name: "L", Items: []string{"no_such_item"}}); err == nil {
		t.Error("expected error for an unknown item")
	}
	// A list of non-craftable items is perfectly VALID — it just means nothing to
	// a Recipe Shop. That is the whole point of an untyped list.
	if err := validateListDef(&ListDef{ID: "l", Name: "L", Items: []string{"broad_sword"}}); err != nil {
		t.Errorf("a list of non-craftable items must be valid: %v", err)
	}
}

// TestMarketplaceList_CoversEveryCommonItem: the marketplace list must stock at
// least every common-tier item. Higher tiers may be added by hand (the rare
// experience_potion is deliberately stocked). Derived from the live catalog, so
// adding a common item without listing it fails loudly here rather than silently
// making the item unbuyable.
func TestMarketplaceList_CoversEveryCommonItem(t *testing.T) {
	list, ok := getListDef("marketplace")
	if !ok {
		t.Fatal(`list "marketplace" not found`)
	}
	inList := make(map[string]bool, len(list.Items))
	for i, id := range list.Items {
		if inList[id] {
			t.Errorf("items[%d] %q appears more than once", i, id)
		}
		inList[id] = true
	}
	for _, def := range ListItemDefs() {
		if def.Tier == ItemTierCommon && !inList[def.ID] {
			t.Errorf("common item %q missing from the marketplace list", def.ID)
		}
	}
}

// ─── Legacy metadata keys are rejected, never ignored ───────────────────────

// TestLegacyListKeysAreRejected: a building still carrying "itemList" or
// "recipeList" must fail to load. Silently ignoring the key would erase that
// shop's stock configuration with no signal — the shop would just quietly fall
// back to its building-type default and nobody would notice until a playtest.
func TestLegacyListKeysAreRejected(t *testing.T) {
	for _, key := range []string{"itemList", "recipeList"} {
		buildings := []protocol.BuildingTile{{
			ID: "shop-1", BuildingType: "neutral-shop",
			Metadata: map[string]interface{}{key: "wandering_merchant"},
		}}
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("a map carrying the superseded %q key must fail to load, not be silently ignored", key)
					return
				}
				msg, _ := r.(string)
				if msg == "" {
					t.Errorf("%s: panic carried no message", key)
				}
			}()
			validateBuildingListKeys("test-map", buildings)
		}()
	}
}

// TestCurrentListKeyIsAccepted: the replacement key loads fine.
func TestCurrentListKeyIsAccepted(t *testing.T) {
	buildings := []protocol.BuildingTile{{
		ID: "shop-1", BuildingType: "neutral-shop",
		Metadata: map[string]interface{}{"list": "wandering_merchant"},
	}}
	validateBuildingListKeys("test-map", buildings) // must not panic
}

// TestShippedMapsCarryNoLegacyKeys: every map in the catalog has been migrated.
// The catalog loader would panic on one that had not, so reaching this point at
// all proves it — but assert explicitly so the intent is recorded.
func TestShippedMapsCarryNoLegacyKeys(t *testing.T) {
	for _, sum := range ListMapCatalogSummaries() {
		entry, ok := GetMapCatalogEntryByID(sum.ID)
		if !ok {
			t.Fatalf("map %q in the summary list but not resolvable", sum.ID)
		}
		validateBuildingListKeys(entry.ID, entry.Map.Buildings) // must not panic
	}
}

// ─── Camp loot from a list ──────────────────────────────────────────────────

// TestNeutralGroupLootSources: a group names at most ONE loot source. Setting
// both a weighted table and a list is a load error rather than a precedence
// rule, because a silent winner between two sources is exactly the thing that
// gets mis-authored and never noticed.
func TestNeutralGroupLootSources(t *testing.T) {
	// Both sources → rejected.
	if err := validateNeutralGroupLoot(&NeutralGroup{
		ID: "g", LootTable: "raider_loot", LootList: "marketplace",
	}); err == nil {
		t.Error("a group naming BOTH a loot table and a loot list must be rejected, not silently resolved")
	}
	// Either one alone → fine.
	if err := validateNeutralGroupLoot(&NeutralGroup{ID: "g", LootTable: "raider_loot"}); err != nil {
		t.Errorf("a weighted loot table alone must be valid: %v", err)
	}
	if err := validateNeutralGroupLoot(&NeutralGroup{ID: "g", LootList: "marketplace"}); err != nil {
		t.Errorf("a loot list alone must be valid: %v", err)
	}
	// Neither → fine (the group just drops nothing).
	if err := validateNeutralGroupLoot(&NeutralGroup{ID: "g"}); err != nil {
		t.Errorf("a group with no loot source must be valid: %v", err)
	}
	// A source that does not resolve → rejected.
	if err := validateNeutralGroupLoot(&NeutralGroup{ID: "g", LootList: "no_such_list"}); err == nil {
		t.Error("an unknown lootList must be rejected")
	}
	if err := validateNeutralGroupLoot(&NeutralGroup{ID: "g", LootTable: "no_such_table"}); err == nil {
		t.Error("an unknown lootTable must be rejected")
	}
}

// TestListLootDrop_UniformAndAlwaysDrops: a camp whose loot source is a list
// always drops exactly one member, chosen uniformly, and reproducibly under a
// fixed seed. A list cannot express "no drop" or a resource bundle — that is
// what weighted loot tables are for.
func TestListLootDrop_UniformAndAlwaysDrops(t *testing.T) {
	registerTestList(t, &ListDef{
		ID: "test_loot_pool", Name: "Loot Pool",
		Items: []string{"broad_sword", "fire_ring", "steel_shield"},
	})

	drawn := map[string]int{}
	const runs = 60
	for seed := int64(0); seed < runs; seed++ {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		camp := &NeutralCamp{PlacementID: "camp-1", X: 5, Y: 5}
		before := len(s.LootDrops)
		s.dropListChestForCampLocked(camp, "test_loot_pool")
		if len(s.LootDrops) != before+1 {
			s.mu.Unlock()
			t.Fatalf("seed %d: a list loot source must ALWAYS drop a chest", seed)
		}
		// The chest holds exactly one item and no resources.
		var newest *LootDrop
		for _, d := range s.LootDrops {
			newest = d
		}
		if len(newest.ItemGrants) != 1 {
			s.mu.Unlock()
			t.Fatalf("seed %d: chest holds %d items, want exactly 1", seed, len(newest.ItemGrants))
		}
		if len(newest.ResourceGrants) != 0 {
			s.mu.Unlock()
			t.Fatalf("seed %d: a list drop must not grant resources (that is a weighted loot table's job)", seed)
		}
		drawn[newest.ItemGrants[0]]++
		s.mu.Unlock()
	}

	// Every member should turn up across 60 seeds — uniform, not fixed on one.
	for _, id := range []string{"broad_sword", "fire_ring", "steel_shield"} {
		if drawn[id] == 0 {
			t.Errorf("item %q never dropped in %d rolls — the pick is not uniform", id, runs)
		}
	}
}

// TestListLootDrop_Deterministic: the same seed reproduces the same drop.
func TestListLootDrop_Deterministic(t *testing.T) {
	registerTestList(t, &ListDef{
		ID: "test_loot_pool2", Name: "Loot Pool",
		Items: []string{"broad_sword", "fire_ring", "steel_shield"},
	})
	roll := func() string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 4242)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.dropListChestForCampLocked(&NeutralCamp{PlacementID: "camp-1", X: 5, Y: 5}, "test_loot_pool2")
		for _, d := range s.LootDrops {
			return d.ItemGrants[0]
		}
		return ""
	}
	a, b := roll(), roll()
	if a == "" || a != b {
		t.Fatalf("same seed produced %q then %q — loot must stay deterministic", a, b)
	}
}

// ─── Weighted lists ─────────────────────────────────────────────────────────

// TestValidateListDef_FormRules: a list has exactly ONE form. Declaring both a
// uniform `items` set and weighted `entries` means two different answers to "how
// likely is this member", so it is rejected rather than silently preferring one.
func TestValidateListDef_FormRules(t *testing.T) {
	both := &ListDef{ID: "l", Name: "L",
		Items:   []string{"broad_sword"},
		MaxRoll: 10,
		Entries: []ListEntry{{Item: "broad_sword", Min: 1, Max: 10}},
	}
	if err := validateListDef(both); err == nil {
		t.Error("a list declaring BOTH forms must be rejected")
	}

	// A uniform list has no die.
	if err := validateListDef(&ListDef{ID: "l", Name: "L", Items: []string{"broad_sword"}, MaxRoll: 10}); err == nil {
		t.Error("a uniform list carrying a maxRoll must be rejected")
	}

	// Both forms, alone, are fine.
	if err := validateListDef(&ListDef{ID: "l", Name: "L", Items: []string{"broad_sword"}}); err != nil {
		t.Errorf("a uniform list must validate: %v", err)
	}
	if err := validateListDef(&ListDef{ID: "l", Name: "L", MaxRoll: 10,
		Entries: []ListEntry{{Item: "broad_sword", Min: 1, Max: 10}}}); err != nil {
		t.Errorf("a weighted list must validate: %v", err)
	}
}

// TestValidateListDef_WeightedCoverageIsTotal: a weighted list's entries tile
// its die. A weighted list has no "nothing" outcome — it is a POOL, and whether
// anything drops at all is a table's decision — so a gap here is always a bug.
func TestValidateListDef_WeightedCoverageIsTotal(t *testing.T) {
	l := func(entries ...ListEntry) *ListDef {
		return &ListDef{ID: "l", Name: "L", MaxRoll: 100, Entries: entries}
	}
	if err := validateListDef(l(ListEntry{"broad_sword", 1, 50})); err == nil {
		t.Error("a trailing gap must be rejected")
	}
	if err := validateListDef(l(
		ListEntry{"broad_sword", 1, 50}, ListEntry{"scimitar", 61, 100})); err == nil {
		t.Error("a gap must be rejected")
	}
	if err := validateListDef(l(
		ListEntry{"broad_sword", 1, 60}, ListEntry{"scimitar", 50, 100})); err == nil {
		t.Error("an overlap must be rejected")
	}
	if err := validateListDef(l(
		ListEntry{"broad_sword", 1, 50}, ListEntry{"scimitar", 51, 100})); err != nil {
		t.Errorf("entries that tile the die must validate: %v", err)
	}
}

// TestItemIDs_FormAgnostic: membership reads the same whichever form the list
// took. Every consumer that only cares WHO is on the list (an Artificer's scope,
// a Recipe Shop's pool, a marketplace's shelf) goes through ItemIDs and must not
// be able to tell.
func TestItemIDs_FormAgnostic(t *testing.T) {
	uniform := &ListDef{ID: "u", Items: []string{"broad_sword", "scimitar"}}
	weighted := &ListDef{ID: "w", MaxRoll: 100, Entries: []ListEntry{
		{"broad_sword", 1, 90}, {"scimitar", 91, 100},
	}}
	if got, want := uniform.ItemIDs(), weighted.ItemIDs(); len(got) != len(want) {
		t.Fatalf("membership differs by form: %v vs %v", got, want)
	}
	for i := range uniform.ItemIDs() {
		if uniform.ItemIDs()[i] != weighted.ItemIDs()[i] {
			t.Errorf("membership differs by form at %d: %v vs %v", i, uniform.ItemIDs(), weighted.ItemIDs())
		}
	}
	if uniform.IsWeighted() || !weighted.IsWeighted() {
		t.Error("IsWeighted misreports the form")
	}
}

// TestRollList_RespectsWeights: a member holding 90% of the die turns up about
// 90% of the time. Asserted as a broad band, not an exact count — this is a
// check that weights are APPLIED, not a test of the RNG's distribution.
func TestRollList_RespectsWeights(t *testing.T) {
	registerTestList(t, &ListDef{ID: "wt_pool", Name: "Weighted", MaxRoll: 100, Entries: []ListEntry{
		{Item: "broad_sword", Min: 1, Max: 90},
		{Item: "scimitar", Min: 91, Max: 100},
	}})
	list, _ := getListDef("wt_pool")

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 11)
	s.mu.Lock()
	defer s.mu.Unlock()

	counts := map[string]int{}
	const rolls = 2000
	for i := 0; i < rolls; i++ {
		counts[s.rollListLocked(list)]++
	}
	if counts["broad_sword"]+counts["scimitar"] != rolls {
		t.Fatalf("a weighted list must ALWAYS yield an item: %v", counts)
	}
	// 90/10 — allow a wide band; the point is that it is nothing like 50/50.
	if counts["broad_sword"] < rolls*8/10 || counts["broad_sword"] > rolls*97/100 {
		t.Errorf("broad_sword (90%% of the die) came up %d/%d times — weights are not being applied",
			counts["broad_sword"], rolls)
	}
}

// TestRollList_UniformIsEven: a uniform list still picks evenly.
func TestRollList_UniformIsEven(t *testing.T) {
	registerTestList(t, &ListDef{ID: "uni_pool", Name: "Uniform",
		Items: []string{"broad_sword", "scimitar", "fire_ring"}})
	list, _ := getListDef("uni_pool")

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 12)
	s.mu.Lock()
	defer s.mu.Unlock()

	counts := map[string]int{}
	const rolls = 3000
	for i := 0; i < rolls; i++ {
		counts[s.rollListLocked(list)]++
	}
	for _, id := range []string{"broad_sword", "scimitar", "fire_ring"} {
		if counts[id] < rolls/5 {
			t.Errorf("uniform list: %q came up only %d/%d times — not even", id, counts[id], rolls)
		}
	}
}

// TestSampleList_WeightedShopStockFavoursCommonItems: a shop bound to a weighted
// list samples it BY WEIGHT. This is the deliberate behaviour change — a rare
// sword is rare on a shelf for the same reason it is rare in a chest.
func TestSampleList_WeightedShopStockFavoursCommonItems(t *testing.T) {
	registerTestList(t, &ListDef{ID: "shelf_pool", Name: "Shelf", MaxRoll: 100, Entries: []ListEntry{
		{Item: "broad_sword", Min: 1, Max: 98},
		{Item: "shadow_blade", Min: 99, Max: 100},
	}})
	list, _ := getListDef("shelf_pool")

	common, rare := 0, 0
	for seed := int64(0); seed < 60; seed++ {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		for _, id := range s.sampleListLocked(list, 1) {
			if id == "broad_sword" {
				common++
			}
			if id == "shadow_blade" {
				rare++
			}
		}
		s.mu.Unlock()
	}
	if common <= rare {
		t.Errorf("weighted shop sampling: the 98%% item appeared %d times and the 2%% item %d — weights are not applied to shop stock",
			common, rare)
	}
}
