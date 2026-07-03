package game

import "testing"

// TestItemLists_MarketplaceList asserts the "marketplace" item list —
// catalog/items/lists/marketplace.json — exists, references only real items
// with no duplicates, and contains AT LEAST every common-tier item in the
// catalog. Higher-tier items may be added by hand (e.g. the rare
// experience_potion is deliberately marketplace-stocked); the baseline set is
// derived from the live item catalog rather than pinned, so adding a new
// common item without updating the marketplace list fails here, loudly, at
// the right place.
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
		if _, ok := getItemDef(id); !ok {
			t.Errorf("items[%d] %q is not a known item", i, id)
		}
	}

	for _, def := range ListItemDefs() {
		if def.Tier == ItemTierCommon && !inList[def.ID] {
			t.Errorf("common item %q missing from the marketplace list", def.ID)
		}
	}
}
