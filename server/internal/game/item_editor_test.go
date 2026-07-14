package game

import (
	"os"
	"testing"
)

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

// TestSaveEditorItem_CraftableSyncsRecipe: a craftable item registers the item
// def (which owns ONLY its purchase price) AND a paired recipe that owns
// everything about crafting — inputs, the per-craft cost, and the price of
// learning the recipe, which are three separate numbers here so a save that
// crosses any two of them fails. No availability (shop/loot) writes happen —
// that is a shop-level concern.
func TestSaveEditorItem_CraftableSyncsRecipe(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "editor_test_blade", DisplayName: "Editor Blade", IconKey: "editor_test_blade",
			Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon",
			CostGold: 120, Modifiers: &ItemModifiers{Damage: 9},
			Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1, Effect: "fire_bolt_ignite"}}},
		Crafting: &EditorItemCrafting{
			Inputs:         []string{"broad_sword", "fire_ring"},
			CraftCostGold:  150,
			RecipeCostGold: 300,
			Starter:        true,
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	def, ok := getItemDef("editor_test_blade")
	if !ok || !def.Overridden || def.CostGold != 120 {
		t.Fatalf("item not registered with its purchase price: ok=%v %+v", ok, def)
	}
	rec, ok := getRecipeDef("editor_test_blade")
	if !ok {
		t.Fatal("craftable item must have a paired recipe")
	}
	if rec.Output != "editor_test_blade" || !rec.Starter {
		t.Errorf("recipe output/starter wrong: %+v", rec)
	}
	if rec.CostGold != 150 {
		t.Errorf("recipe CostGold (the craft cost) = %d, want 150", rec.CostGold)
	}
	if rec.UnlockCostGold != 300 {
		t.Errorf("recipe UnlockCostGold (the price to learn it) = %d, want 300", rec.UnlockCostGold)
	}
	if len(rec.Inputs) != 2 || rec.Inputs[0] != "broad_sword" || rec.Inputs[1] != "fire_ring" {
		t.Errorf("recipe inputs = %v, want [broad_sword fire_ring]", rec.Inputs)
	}
	// The editor writes no availability: the item joins no marketplace list.
	if mkt, ok := getItemListDef("marketplace"); ok && containsString(mkt.Items, "editor_test_blade") {
		t.Error("editor must not add items to the marketplace list")
	}
}

// TestSaveEditorItem_NonCraftableDropsRecipe: saving an item with no Crafting
// block removes any overlay recipe named after it (toggling craftable off).
// The recipe's existence IS the item's craftability — there is no flag on the
// item to fall out of step with it.
func TestSaveEditorItem_NonCraftableDropsRecipe(t *testing.T) {
	editorEnv(t)
	craftable := EditorItemSaveRequest{
		Item: ItemDef{ID: "toggle_item", DisplayName: "Toggle", IconKey: "toggle_item", Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon"},
		Crafting: &EditorItemCrafting{
			Inputs: []string{"broad_sword", "fire_ring"}, CraftCostGold: 50, RecipeCostGold: 75,
		},
	}
	if err := SaveEditorItem(craftable); err != nil {
		t.Fatalf("save craftable: %v", err)
	}
	if _, ok := getRecipeDef("toggle_item"); !ok {
		t.Fatal("recipe should exist while craftable")
	}
	// Re-save with crafting off.
	notCraftable := craftable
	notCraftable.Crafting = nil
	if err := SaveEditorItem(notCraftable); err != nil {
		t.Fatalf("save non-craftable: %v", err)
	}
	if _, ok := getRecipeDef("toggle_item"); ok {
		t.Error("recipe should be dropped when crafting is toggled off")
	}
}

// TestSaveEditorItem_ValidationRejectsBeforeAnyWrite: an invalid proc effect
// reference fails validation and never registers the item.
func TestSaveEditorItem_ValidationRejectsBeforeAnyWrite(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "bad_item", DisplayName: "Bad", IconKey: "x", Kind: ItemKindEquipment,
			Tier: ItemTierCommon,
			Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1, Effect: "no_such_effect"}}},
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
}

// TestSaveEditorItem_CraftableInputValidation: fewer than two inputs, an
// unknown input, a self-referencing input, and a negative price on EITHER of
// the two crafting costs are all rejected before any write.
func TestSaveEditorItem_CraftableInputValidation(t *testing.T) {
	editorEnv(t)
	base := ItemDef{ID: "craft_probe", DisplayName: "Probe", IconKey: "x", Kind: ItemKindEquipment, Tier: ItemTierCommon}
	craft := func(inputs []string, craftCost, recipeCost int) EditorItemSaveRequest {
		return EditorItemSaveRequest{Item: base, Crafting: &EditorItemCrafting{
			Inputs: inputs, CraftCostGold: craftCost, RecipeCostGold: recipeCost,
		}}
	}
	good := []string{"broad_sword", "fire_ring"}

	if err := SaveEditorItem(craft([]string{"broad_sword"}, 10, 10)); err == nil {
		t.Error("expected error for <2 inputs")
	}
	if err := SaveEditorItem(craft([]string{"broad_sword", "no_such_item"}, 10, 10)); err == nil {
		t.Error("expected error for unknown input")
	}
	if err := SaveEditorItem(craft([]string{"craft_probe", "fire_ring"}, 10, 10)); err == nil {
		t.Error("expected error for self-referencing input")
	}
	if err := SaveEditorItem(craft(good, -1, 10)); err == nil {
		t.Error("expected error for a negative craft cost")
	}
	if err := SaveEditorItem(craft(good, 10, -1)); err == nil {
		t.Error("expected error for a negative recipe cost")
	}
	if _, ok := getItemDef("craft_probe"); ok {
		t.Error("no invalid variant should have registered the item")
	}
}

// TestDeleteEditorItem_RemovesItemAndRecipe: deleting an editor-created
// craftable item removes both the item override and its recipe.
func TestDeleteEditorItem_RemovesItemAndRecipe(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "doomed_item", DisplayName: "Doomed", IconKey: "doomed_item", Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon"},
		Crafting: &EditorItemCrafting{
			Inputs: []string{"broad_sword", "fire_ring"}, CraftCostGold: 10, RecipeCostGold: 20,
		},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	status, existed, err := DeleteEditorItem("doomed_item")
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if status != "deleted" {
		t.Errorf("status = %q, want deleted (an author-created item is really removed)", status)
	}
	if _, ok := getItemDef("doomed_item"); ok {
		t.Error("item still visible")
	}
	if _, ok := getRecipeDef("doomed_item"); ok {
		t.Error("recipe still visible")
	}
}

// TestDeleteEditorItem_ShippedItemRevertsToPreSaveState: Reset on a SHIPPED item
// undoes the author's last save — it restores the state the item was in before
// that save, not the catalog default. A second Reset (no undo step left) falls
// back to the default.
func TestDeleteEditorItem_ShippedItemRevertsToPreSaveState(t *testing.T) {
	editorEnv(t)
	const id = "broad_sword"
	shipped, ok := getItemDef(id)
	if !ok {
		t.Skipf("%s not in catalog", id)
	}
	shippedName := shipped.DisplayName

	// First save: a deliberate edit the author wants to keep.
	first := *shipped
	first.DisplayName = "Keeper"
	if err := SaveItemDef(&first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Second save: the mistake.
	second := first
	second.DisplayName = "Oops"
	if err := SaveItemDef(&second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got, _ := getItemDef(id); got.DisplayName != "Oops" {
		t.Fatalf("setup failed: %q", got.DisplayName)
	}

	// Reset undoes the LAST save → back to "Keeper", not to the shipped default.
	status, existed, err := DeleteEditorItem(id)
	if err != nil || !existed {
		t.Fatalf("reset: existed=%v err=%v", existed, err)
	}
	if status != "reverted" {
		t.Errorf("status = %q, want reverted", status)
	}
	if got, _ := getItemDef(id); got.DisplayName != "Keeper" {
		t.Errorf("after reset = %q, want %q (the state before the last save)", got.DisplayName, "Keeper")
	}

	// A second reset has no undo step left, so it falls back to the default.
	status, existed, err = DeleteEditorItem(id)
	if err != nil || !existed {
		t.Fatalf("second reset: existed=%v err=%v", existed, err)
	}
	if status != "reset" {
		t.Errorf("second status = %q, want reset", status)
	}
	if got, _ := getItemDef(id); got.DisplayName != shippedName {
		t.Errorf("after second reset = %q, want the shipped default %q", got.DisplayName, shippedName)
	}
}

// TestGetItemAvailability_ReadsShopSurfaces: GetItemAvailability (read-only
// infra for a future shop editor) reflects placements written directly through
// the membership helpers — the item editor no longer writes these itself.
func TestGetItemAvailability_ReadsShopSurfaces(t *testing.T) {
	editorEnv(t)
	item := ItemDef{ID: "avail_probe", DisplayName: "Probe", IconKey: "avail_probe",
		Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", CostGold: 5}
	if err := SaveItemDef(&item); err != nil {
		t.Fatalf("save item: %v", err)
	}
	// Place it via the shop/loot helpers directly (the future shop-editor path).
	if err := ensureItemListMembership("marketplace", "avail_probe", true); err != nil {
		t.Fatalf("marketplace: %v", err)
	}
	if err := SetMerchantItemAvailability("avail_probe", "Weapon", true, 20); err != nil {
		t.Fatalf("loot: %v", err)
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
	if _, ok := GetItemAvailability("no_such_item_at_all"); ok {
		t.Error("unknown item must report ok=false")
	}
}
