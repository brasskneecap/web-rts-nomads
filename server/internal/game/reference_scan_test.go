package game

import (
	"strings"
	"testing"
)

// ─── DeleteEditorItem: reference guard ───────────────────────────────────

// TestDeleteEditorItem_ReferencedByList_Rejected: a custom item still listed
// as a member of a list must be refused, and the message must name the
// list.
func TestDeleteEditorItem_ReferencedByList_Rejected(t *testing.T) {
	editorEnv(t)

	item := EditorItemSaveRequest{Item: ItemDef{
		ID: "ref_scan_item_a", DisplayName: "Ref Scan Item A", IconKey: "ref_scan_item_a",
		Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon",
	}}
	if err := SaveEditorItem(item); err != nil {
		t.Fatalf("setup: save item: %v", err)
	}
	list := EditorListSaveRequest{List: ListDef{
		ID: "ref_scan_list_a", Name: "Ref Scan List A", Items: []string{"ref_scan_item_a"},
	}}
	if err := SaveEditorList(list); err != nil {
		t.Fatalf("setup: save list: %v", err)
	}

	status, existed, err := DeleteEditorItem("ref_scan_item_a")
	if err == nil {
		t.Fatal("DeleteEditorItem(referenced item) = nil error, want rejection")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if existed {
		t.Errorf("existed = true, want false (nothing deleted on a rejected delete)")
	}
	if status != "" {
		t.Errorf("status = %q, want empty on a rejected delete", status)
	}
	if !strings.Contains(err.Error(), "ref_scan_list_a") {
		t.Errorf("err = %q, want it to name the referencing list", err.Error())
	}

	// The rejected delete must have had no effect at all.
	if _, ok := getItemDef("ref_scan_item_a"); !ok {
		t.Error("item no longer resolvable after a REJECTED delete — rejection must be a no-op")
	}
}

// TestDeleteEditorItem_ReferencedByRecipe_Rejected: a custom item still
// named as a crafting input on another item must be refused, and the
// message must name the recipe.
func TestDeleteEditorItem_ReferencedByRecipe_Rejected(t *testing.T) {
	editorEnv(t)

	ingredient := EditorItemSaveRequest{Item: ItemDef{
		ID: "ref_scan_item_b", DisplayName: "Ref Scan Item B", IconKey: "ref_scan_item_b",
		Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon",
	}}
	if err := SaveEditorItem(ingredient); err != nil {
		t.Fatalf("setup: save ingredient: %v", err)
	}
	recipe := EditorItemSaveRequest{Item: ItemDef{
		ID: "ref_scan_item_c", DisplayName: "Ref Scan Item C", IconKey: "ref_scan_item_c",
		Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon",
		Crafting: &ItemCrafting{Inputs: []string{"ref_scan_item_b", "broad_sword"}, CraftCostGold: 10, RecipeCostGold: 20},
	}}
	if err := SaveEditorItem(recipe); err != nil {
		t.Fatalf("setup: save recipe: %v", err)
	}

	status, existed, err := DeleteEditorItem("ref_scan_item_b")
	if err == nil {
		t.Fatal("DeleteEditorItem(referenced-by-recipe item) = nil error, want rejection")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if existed {
		t.Errorf("existed = true, want false (nothing deleted on a rejected delete)")
	}
	if status != "" {
		t.Errorf("status = %q, want empty on a rejected delete", status)
	}
	if !strings.Contains(err.Error(), "ref_scan_item_c") {
		t.Errorf("err = %q, want it to name the referencing recipe", err.Error())
	}

	if _, ok := getItemDef("ref_scan_item_b"); !ok {
		t.Error("item no longer resolvable after a REJECTED delete — rejection must be a no-op")
	}
}

// TestDeleteEditorItem_NoReferences_Deleted: a custom item nothing points at
// deletes cleanly.
func TestDeleteEditorItem_NoReferences_Deleted(t *testing.T) {
	editorEnv(t)

	item := EditorItemSaveRequest{Item: ItemDef{
		ID: "ref_scan_item_d", DisplayName: "Ref Scan Item D", IconKey: "ref_scan_item_d",
		Kind: ItemKindEquipment, Tier: ItemTierCommon, Category: "Weapon",
	}}
	if err := SaveEditorItem(item); err != nil {
		t.Fatalf("setup: save item: %v", err)
	}

	status, existed, err := DeleteEditorItem("ref_scan_item_d")
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if status != "deleted" {
		t.Errorf("status = %q, want deleted", status)
	}
	if _, ok := getItemDef("ref_scan_item_d"); ok {
		t.Error("item still visible after a clean delete")
	}
}

// TestDeleteEditorItem_ShippedItemReferenced_NotBlocked: a SHIPPED item is
// reset, not removed — its def file survives, so any existing reference to
// it stays valid no matter what points at it. The reference guard must never
// run on this branch.
func TestDeleteEditorItem_ShippedItemReferenced_NotBlocked(t *testing.T) {
	editorEnv(t)
	const id = "broad_sword"
	if !ItemIsEmbedded(id) {
		t.Skipf("%s is not a shipped item in this build", id)
	}

	list := EditorListSaveRequest{List: ListDef{
		ID: "ref_scan_list_shipped", Name: "Ref Scan List Shipped", Items: []string{id},
	}}
	if err := SaveEditorList(list); err != nil {
		t.Fatalf("setup: save list referencing shipped item: %v", err)
	}

	status, existed, err := DeleteEditorItem(id)
	if err != nil {
		t.Fatalf("DeleteEditorItem(shipped, referenced) = %v, want nil (shipped items reset, never blocked)", err)
	}
	if !existed {
		t.Fatal("existed = false, want true")
	}
	if status != "reverted" && status != "reset" {
		t.Errorf("status = %q, want reverted or reset", status)
	}
	if _, ok := getItemDef(id); !ok {
		t.Error("shipped item must still resolve after reset")
	}
}

// ─── DeleteEditorList: reference guard ───────────────────────────────────

// TestDeleteEditorList_ReferencedByTable_Rejected: a custom list still
// rolled by a table's row must be refused, and the message must name the
// table.
func TestDeleteEditorList_ReferencedByTable_Rejected(t *testing.T) {
	editorEnv(t)

	list := EditorListSaveRequest{List: ListDef{
		ID: "ref_scan_list_e", Name: "Ref Scan List E", Items: []string{"broad_sword"},
	}}
	if err := SaveEditorList(list); err != nil {
		t.Fatalf("setup: save list: %v", err)
	}
	table := EditorTableSaveRequest{Table: TableDef{
		ID: "ref_scan_table_e", Name: "Ref Scan Table E", MaxRoll: 1,
		Rows: []TableRow{{Min: 1, Max: 1, List: "ref_scan_list_e"}},
	}}
	if err := SaveEditorTable(table); err != nil {
		t.Fatalf("setup: save table: %v", err)
	}

	existed, err := DeleteEditorList("ref_scan_list_e")
	if err == nil {
		t.Fatal("DeleteEditorList(referenced list) = nil error, want rejection")
	}
	if !IsEditorValidationError(err) {
		t.Errorf("err = %v, want editorValidationError", err)
	}
	if existed {
		t.Errorf("existed = true, want false (nothing deleted on a rejected delete)")
	}
	if !strings.Contains(err.Error(), "ref_scan_table_e") {
		t.Errorf("err = %q, want it to name the referencing table", err.Error())
	}

	if _, ok := getListDef("ref_scan_list_e"); !ok {
		t.Error("list no longer resolvable after a REJECTED delete — rejection must be a no-op")
	}
}

// TestDeleteEditorList_NoReferences_Deleted: a custom list nothing points at
// deletes cleanly.
func TestDeleteEditorList_NoReferences_Deleted(t *testing.T) {
	editorEnv(t)

	list := EditorListSaveRequest{List: ListDef{
		ID: "ref_scan_list_f", Name: "Ref Scan List F", Items: []string{"broad_sword"},
	}}
	if err := SaveEditorList(list); err != nil {
		t.Fatalf("setup: save list: %v", err)
	}

	existed, err := DeleteEditorList("ref_scan_list_f")
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if _, ok := getListDef("ref_scan_list_f"); ok {
		t.Error("list still visible after a clean delete")
	}
}
