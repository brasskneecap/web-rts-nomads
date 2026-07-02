package game

import "testing"

// TestItemLists_MarketplaceList asserts the "marketplace" item list —
// catalog/items/lists/marketplace.json — exists and contains exactly the set
// of common-tier items in the catalog. The expected set is derived from the
// live item catalog rather than pinned, so adding a new common item without
// updating the marketplace list fails here, loudly, at the right place.
func TestItemLists_MarketplaceList(t *testing.T) {
	list, ok := getItemListDef("marketplace")
	if !ok {
		t.Fatal(`item list "marketplace" not found`)
	}

	inList := make(map[string]bool, len(list.Items))
	for i, id := range list.Items {
		if inList[id] {
			t.Errorf("items[%d] %q appears more than once", i, id)
		}
		inList[id] = true
		def, ok := getItemDef(id)
		if !ok {
			t.Errorf("items[%d] %q is not a known item", i, id)
			continue
		}
		if def.Tier != ItemTierCommon {
			t.Errorf("items[%d] %q is tier %q — the marketplace list is common items only", i, id, def.Tier)
		}
	}

	for _, def := range ListItemDefs() {
		if def.Tier == ItemTierCommon && !inList[def.ID] {
			t.Errorf("common item %q missing from the marketplace list", def.ID)
		}
	}
}
