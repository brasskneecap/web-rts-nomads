package game

import (
	"testing"
)

// editorEnv points every writable dir at temp dirs and cleans all overlays.
func editorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("LIST_CATALOG_DIR", t.TempDir())
	t.Setenv("TABLE_CATALOG_DIR", t.TempDir())
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		runtimeItems = map[string]*ItemDef{}
		runtimeItemsMu.Unlock()
		runtimeListsMu.Lock()
		runtimeLists = map[string]*ListDef{}
		runtimeListsMu.Unlock()
		runtimeTablesMu.Lock()
		runtimeTables = map[string]*TableDef{}
		runtimeTablesMu.Unlock()
	})
}

// TestSaveEditorItem_CraftableItemOwnsItsRecipe: a craftable item is saved as ONE
// def carrying its own crafting block. There is no second entity to sync — an
// item is its own recipe. The three prices (buy / craft / learn) are deliberately
// three different numbers here, so a save that crosses any two of them fails.
func TestSaveEditorItem_CraftableItemOwnsItsRecipe(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "editor_test_blade", DisplayName: "Editor Blade", IconKey: "editor_test_blade",
			Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon",
			CostGold: 120, Modifiers: &ItemModifiers{Damage: 9},
			Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1, Ability: "fire_bolt"}},
			Crafting: &ItemCrafting{
				Inputs:         []string{"broad_sword", "fire_ring"},
				CraftCostGold:  150,
				RecipeCostGold: 300,
				Starter:        true,
			}},
	}
	if err := SaveEditorItem(req); err != nil {
		t.Fatalf("save: %v", err)
	}
	def, ok := getItemDef("editor_test_blade")
	if !ok || !def.Overridden {
		t.Fatalf("item not registered: ok=%v %+v", ok, def)
	}
	if !def.IsCraftable() {
		t.Fatal("an item with a crafting block must be craftable")
	}
	if def.CostGold != 120 {
		t.Errorf("CostGold (buy the finished item) = %d, want 120", def.CostGold)
	}
	if def.Crafting.CraftCostGold != 150 {
		t.Errorf("CraftCostGold (make it at an Artificer) = %d, want 150", def.Crafting.CraftCostGold)
	}
	if def.Crafting.RecipeCostGold != 300 {
		t.Errorf("RecipeCostGold (learn it at a Recipe Shop) = %d, want 300", def.Crafting.RecipeCostGold)
	}
	if !def.Crafting.Starter {
		t.Error("starter flag lost")
	}
	if got := def.Crafting.Inputs; len(got) != 2 || got[0] != "broad_sword" || got[1] != "fire_ring" {
		t.Errorf("inputs = %v, want [broad_sword fire_ring]", got)
	}
	// The editor writes no availability: membership is a list-level concern.
	if mkt, ok := getListDef("marketplace"); ok && containsString(mkt.Items, "editor_test_blade") {
		t.Error("editor must not add items to the marketplace list")
	}
}

// TestSaveEditorItem_DroppingCraftingMakesItUncraftable: re-saving with no
// crafting block makes the item uncraftable. There is no separate recipe file to
// fall out of step — the block IS the recipe.
func TestSaveEditorItem_DroppingCraftingMakesItUncraftable(t *testing.T) {
	editorEnv(t)
	craftable := EditorItemSaveRequest{
		Item: ItemDef{ID: "toggle_item", DisplayName: "Toggle", IconKey: "toggle_item",
			Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon",
			Crafting: &ItemCrafting{
				Inputs: []string{"broad_sword", "fire_ring"}, CraftCostGold: 50, RecipeCostGold: 75,
			}},
	}
	if err := SaveEditorItem(craftable); err != nil {
		t.Fatalf("save craftable: %v", err)
	}
	if def, _ := getItemDef("toggle_item"); !def.IsCraftable() {
		t.Fatal("item should be craftable while it has a crafting block")
	}

	notCraftable := craftable
	notCraftable.Item.Crafting = nil
	if err := SaveEditorItem(notCraftable); err != nil {
		t.Fatalf("save non-craftable: %v", err)
	}
	if def, _ := getItemDef("toggle_item"); def.IsCraftable() {
		t.Error("item must not be craftable once its crafting block is dropped")
	}
}

// TestSaveEditorItem_ValidationRejectsBeforeAnyWrite: an invalid proc ability
// reference fails validation and never registers the item.
func TestSaveEditorItem_ValidationRejectsBeforeAnyWrite(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "bad_item", DisplayName: "Bad", IconKey: "x", Kind: ItemKindEquipment,
			Tier: ItemTierCommon,
			Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1, Ability: "no_such_ability"}}},
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

// TestSaveEditorItem_CraftingValidation: fewer than two inputs, an unknown input,
// a self-referencing input, and a negative price on EITHER crafting cost are all
// rejected before any write.
func TestSaveEditorItem_CraftingValidation(t *testing.T) {
	editorEnv(t)
	craft := func(inputs []string, craftCost, recipeCost int) EditorItemSaveRequest {
		return EditorItemSaveRequest{Item: ItemDef{
			ID: "craft_probe", DisplayName: "Probe", IconKey: "x",
			Kind: ItemKindEquipment, Tier: ItemTierCommon,
			Crafting: &ItemCrafting{Inputs: inputs, CraftCostGold: craftCost, RecipeCostGold: recipeCost},
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

// TestDeleteEditorItem_RemovesItemAndItsRecipe: deleting an editor-created
// craftable item removes its recipe for free — the crafting block was never a
// separate file.
func TestDeleteEditorItem_RemovesItemAndItsRecipe(t *testing.T) {
	editorEnv(t)
	req := EditorItemSaveRequest{
		Item: ItemDef{ID: "doomed_item", DisplayName: "Doomed", IconKey: "doomed_item",
			Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon",
			Crafting: &ItemCrafting{
				Inputs: []string{"broad_sword", "fire_ring"}, CraftCostGold: 10, RecipeCostGold: 20,
			}},
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

