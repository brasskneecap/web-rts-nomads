package game

import (
	"os"
	"testing"
)

// TestDeleteEditorItem_LastMerchantItemIsNonFatal: if the deleted item is the
// sole surviving entry in a merchant subtable, the cleanup sweep's removal
// call fails with ErrLastMerchantItem — DeleteEditorItem must treat that as
// non-fatal (log + continue) rather than aborting the whole delete, per the
// SetMerchantItemAvailability guard added to keep subtables non-empty.
func TestDeleteEditorItem_LastMerchantItemIsNonFatal(t *testing.T) {
	editorEnv(t)
	const id = "solo_accessory_item"
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: id, DisplayName: "Solo", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Accessory", SlotKind: "any"},
		Availability: EditorAvailability{
			LootTable: EditorLootAvailability{Enabled: true, Weight: 10},
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Drain every pre-existing merchant_accessories entry so id ends up the
	// sole survivor in the subtable.
	pi, ok := getPackagedItem("merchant_accessories")
	if !ok {
		t.Fatal("merchant_accessories subtable missing")
	}
	for _, e := range pi.Entries {
		if e.Item == id {
			continue
		}
		if err := SetMerchantItemAvailability(e.Item, "Accessory", false, 0); err != nil {
			t.Fatalf("drain %q: %v", e.Item, err)
		}
	}
	if pi, _ := getPackagedItem("merchant_accessories"); len(pi.Entries) != 1 || pi.Entries[0].Item != id {
		t.Fatalf("setup: expected subtable drained to just %q, got %+v", id, pi.Entries)
	}

	existed, err := DeleteEditorItem(id)
	if err != nil || !existed {
		t.Fatalf("delete must succeed despite the last-item guard: existed=%v err=%v", existed, err)
	}
	if _, ok := getItemDef(id); ok {
		t.Error("item override must still be removed")
	}
	// The dangling loot row is the accepted tradeoff — it survives deletion.
	if pi, _ := getPackagedItem("merchant_accessories"); len(pi.Entries) != 1 || pi.Entries[0].Item != id {
		t.Errorf("expected dangling loot row for %q to survive, got %+v", id, pi.Entries)
	}
}

// editorEnv points every writable dir at temp dirs and cleans all overlays.
func editorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		runtimeItems = map[string]*ItemDef{}
		runtimeItemsMu.Unlock()
		runtimeRecipesMu.Lock()
		runtimeRecipes = map[string]*RecipeDef{}
		runtimeRecipesMu.Unlock()
		runtimeItemListsMu.Lock()
		runtimeItemLists = map[string]*ItemListDef{}
		runtimeItemListsMu.Unlock()
		runtimeRecipeListsMu.Lock()
		runtimeRecipeLists = map[string]*RecipeListDef{}
		runtimeRecipeListsMu.Unlock()
		runtimeLootCatalogMu.Lock()
		runtimeLootCatalog, runtimePackagedItems, runtimeLootTables = nil, nil, nil
		runtimeLootCatalogMu.Unlock()
	})
	_ = os.Getenv // silence unused import if not otherwise used
}

// TestSaveEditorItem_FullSurface: item + recipe + all four availability
// surfaces round-trip through every reader.
func TestSaveEditorItem_FullSurface(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "editor_test_blade", DisplayName: "Editor Blade", IconKey: "editor_test_blade",
			Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon", SlotKind: "any",
			CostGold: 120, Modifiers: &ItemModifiers{Damage: 9},
			OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "fire_bolt_ignite"}},
		Recipe: &EditorRecipeSpec{Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 150},
		Availability: EditorAvailability{
			Marketplace: true, WanderingMerchant: true,
			LootTable:  EditorLootAvailability{Enabled: true, Weight: 15},
			RecipeList: true,
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	if def, ok := getItemDef("editor_test_blade"); !ok || !def.Overridden || def.CostGold != 120 {
		t.Fatalf("item not registered: ok=%v %+v", ok, def)
	}
	if rec, ok := getRecipeDef("editor_test_blade"); !ok || rec.Output != "editor_test_blade" || rec.CostGold != 150 {
		t.Fatalf("recipe not registered: ok=%v %+v", ok, rec)
	}
	mkt, _ := getItemListDef("marketplace")
	if !containsString(mkt.Items, "editor_test_blade") {
		t.Error("missing from marketplace list")
	}
	wm, _ := getItemListDef("wandering_merchant")
	if !containsString(wm.Items, "editor_test_blade") {
		t.Error("missing from wandering_merchant list")
	}
	if pi, ok := getPackagedItem("merchant_weapons"); ok {
		found := false
		for _, e := range pi.Entries {
			if e.Item == "editor_test_blade" {
				found = true
			}
		}
		if !found {
			t.Error("missing from merchant_weapons subtable")
		}
	} else {
		t.Error("merchant_weapons subtable missing")
	}
	dr, _ := getRecipeListDef("druid_recipes_1")
	if !containsString(dr.Recipes, "editor_test_blade") {
		t.Error("recipe missing from druid_recipes_1")
	}
}

// TestSaveEditorItem_ValidationRejectsBeforeAnyWrite: an invalid proc effect
// reference fails without touching any availability file.
func TestSaveEditorItem_ValidationRejectsBeforeAnyWrite(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "bad_item", DisplayName: "Bad", IconKey: "x", Kind: ItemKindEquipment,
			Tier: ItemTierCommon, SlotKind: "any",
			OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}},
		Availability: EditorAvailability{Marketplace: true},
	}
	err := SaveEditorItem(req)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("expected validation-class error, got %v", err)
	}
	if _, ok := getItemDef("bad_item"); ok {
		t.Error("invalid item must not register")
	}
	mkt, _ := getItemListDef("marketplace")
	if containsString(mkt.Items, "bad_item") {
		t.Error("availability must not change on failed save")
	}
}

// TestSaveEditorItem_SelfRecipeRejected.
func TestSaveEditorItem_SelfRecipeRejected(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item:   ItemDef{ID: "selfy", DisplayName: "Selfy", IconKey: "x", Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any"},
		Recipe: &EditorRecipeSpec{Inputs: []string{"selfy", "fire_ring"}, CostGold: 10},
	}
	if err := SaveEditorItem(req); err == nil {
		t.Fatal("self-referencing recipe must be rejected")
	}
}

// TestDeleteEditorItem_CleansEverythingForEditorCreated.
func TestDeleteEditorItem_CleansEverythingForEditorCreated(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item:   ItemDef{ID: "doomed_item", DisplayName: "Doomed", IconKey: "doomed_item", Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", SlotKind: "any"},
		Recipe: &EditorRecipeSpec{Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 10},
		Availability: EditorAvailability{Marketplace: true, LootTable: EditorLootAvailability{Enabled: true, Weight: 10}, RecipeList: true},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := DeleteEditorItem("doomed_item")
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if _, ok := getItemDef("doomed_item"); ok {
		t.Error("item still visible")
	}
	if _, ok := getRecipeDef("doomed_item"); ok {
		t.Error("recipe still visible")
	}
	mkt, _ := getItemListDef("marketplace")
	if containsString(mkt.Items, "doomed_item") {
		t.Error("still in marketplace list")
	}
	if pi, _ := getPackagedItem("merchant_weapons"); pi.Entries != nil {
		for _, e := range pi.Entries {
			if e.Item == "doomed_item" {
				t.Error("still in merchant subtable")
			}
		}
	}
}

// TestGetItemAvailability_ReflectsAllFourSurfaces: after a full-surface save,
// availability reads back exactly what was requested (weight ≈ requested,
// rounding tolerance from renormalization).
func TestGetItemAvailability_ReflectsAllFourSurfaces(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "avail_probe", DisplayName: "Probe", IconKey: "avail_probe",
			Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", SlotKind: "any", CostGold: 5},
		Recipe: &EditorRecipeSpec{Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 10},
		Availability: EditorAvailability{
			Marketplace: true, WanderingMerchant: false,
			LootTable:  EditorLootAvailability{Enabled: true, Weight: 20},
			RecipeList: true,
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := GetItemAvailability("avail_probe")
	if !ok {
		t.Fatal("item exists, availability must resolve")
	}
	if !got.Marketplace || got.WanderingMerchant {
		t.Errorf("list flags wrong: %+v", got)
	}
	if !got.LootTable.Enabled || got.LootTable.Weight < 15 || got.LootTable.Weight > 25 {
		t.Errorf("loot flag/weight wrong (want enabled, ~20): %+v", got.LootTable)
	}
	if !got.RecipeList {
		t.Errorf("recipeList flag wrong: %+v", got)
	}
	// Unknown item → ok=false.
	if _, ok := GetItemAvailability("no_such_item_at_all"); ok {
		t.Error("unknown item must report ok=false")
	}
	// A shipped item with no placements reports all-false without error.
	if av, ok := GetItemAvailability("frost_sword"); !ok || av.Marketplace {
		t.Errorf("frost_sword: ok=%v av=%+v (crafted-only item, not in marketplace)", ok, av)
	}
}
