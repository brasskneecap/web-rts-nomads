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
// def (with IsRecipe + RecipeCost) AND a paired recipe whose output is the
// item, cost is RecipeCost, and inputs are the request inputs. No availability
// (shop/loot) writes happen — that is a shop-level concern.
func TestSaveEditorItem_CraftableSyncsRecipe(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "editor_test_blade", DisplayName: "Editor Blade", IconKey: "editor_test_blade",
			Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon",
			CostGold: 120, IsRecipe: true, RecipeCost: 150, RecipeStarter: true, Modifiers: &ItemModifiers{Damage: 9},
			Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1, Effect: "fire_bolt_ignite"}}},
		Inputs: []string{"broad_sword", "fire_ring"},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	def, ok := getItemDef("editor_test_blade")
	if !ok || !def.Overridden || def.CostGold != 120 || !def.IsRecipe || def.RecipeCost != 150 {
		t.Fatalf("item not registered with craft fields: ok=%v %+v", ok, def)
	}
	rec, ok := getRecipeDef("editor_test_blade")
	if !ok || rec.Output != "editor_test_blade" || rec.CostGold != 150 || !rec.Starter {
		t.Fatalf("recipe not synced (incl. starter): ok=%v %+v", ok, rec)
	}
	if len(rec.Inputs) != 2 || rec.Inputs[0] != "broad_sword" || rec.Inputs[1] != "fire_ring" {
		t.Errorf("recipe inputs = %v, want [broad_sword fire_ring]", rec.Inputs)
	}
	// The editor writes no availability: the item joins no marketplace list.
	if mkt, ok := getItemListDef("marketplace"); ok && containsString(mkt.Items, "editor_test_blade") {
		t.Error("editor must not add items to the marketplace list")
	}
}

// TestSaveEditorItem_NonCraftableDropsRecipe: saving an item with IsRecipe
// false removes any overlay recipe named after it (toggling craftable off).
func TestSaveEditorItem_NonCraftableDropsRecipe(t *testing.T) {
	editorEnv(t)
	craftable := EditorItemSaveRequest{
		Item:   ItemDef{ID: "toggle_item", DisplayName: "Toggle", IconKey: "toggle_item", Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", IsRecipe: true, RecipeCost: 50},
		Inputs: []string{"broad_sword", "fire_ring"},
	}
	if err := SaveEditorItem(craftable); err != nil {
		t.Fatalf("save craftable: %v", err)
	}
	if _, ok := getRecipeDef("toggle_item"); !ok {
		t.Fatal("recipe should exist while craftable")
	}
	// Re-save with crafting off.
	notCraftable := craftable
	notCraftable.Item.IsRecipe = false
	notCraftable.Item.RecipeCost = 0
	if err := SaveEditorItem(notCraftable); err != nil {
		t.Fatalf("save non-craftable: %v", err)
	}
	if _, ok := getRecipeDef("toggle_item"); ok {
		t.Error("recipe should be dropped when crafting is toggled off")
	}
	if def, _ := getItemDef("toggle_item"); def.IsRecipe {
		t.Error("item IsRecipe should be false after toggle-off")
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
// unknown input, and a self-referencing input are all rejected before any
// write.
func TestSaveEditorItem_CraftableInputValidation(t *testing.T) {
	editorEnv(t)
	base := ItemDef{ID: "craft_probe", DisplayName: "Probe", IconKey: "x", Kind: ItemKindEquipment, Tier: ItemTierCommon, IsRecipe: true, RecipeCost: 10}

	tooFew := EditorItemSaveRequest{Item: base, Inputs: []string{"broad_sword"}}
	if err := SaveEditorItem(tooFew); err == nil {
		t.Error("expected error for <2 inputs")
	}
	unknown := EditorItemSaveRequest{Item: base, Inputs: []string{"broad_sword", "no_such_item"}}
	if err := SaveEditorItem(unknown); err == nil {
		t.Error("expected error for unknown input")
	}
	selfRef := EditorItemSaveRequest{Item: base, Inputs: []string{"craft_probe", "fire_ring"}}
	if err := SaveEditorItem(selfRef); err == nil {
		t.Error("expected error for self-referencing input")
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
		Item:   ItemDef{ID: "doomed_item", DisplayName: "Doomed", IconKey: "doomed_item", Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon", IsRecipe: true, RecipeCost: 10},
		Inputs: []string{"broad_sword", "fire_ring"},
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
