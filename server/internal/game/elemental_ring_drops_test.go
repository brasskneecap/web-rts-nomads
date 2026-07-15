package game

import "testing"

// TestElementalRings_InLootTables asserts that fire_ring, ice_ring, and
// lightning_ring are each reachable from the neutral-merchant table
// ("merchant_basic") and from an enemy/camp drop table ("raider_loot").
//
// Loot tables are two-tiered: the top-level table rows reference packaged
// items by ID; packaged items of kind "item_subtable" hold the actual item
// entries. Both tiers must be walked to collect reachable item IDs.
func TestElementalRings_InLootTables(t *testing.T) {
	wantInMerchant := map[string]bool{
		"fire_ring":      false,
		"ice_ring":       false,
		"lightning_ring": false,
	}
	wantInEnemy := map[string]bool{
		"fire_ring":      false,
		"ice_ring":       false,
		"lightning_ring": false,
	}

	// reachableItems returns every item a table can ever produce — the union of
	// the members of every list it rolls.
	reachableItems := func(tableID string) map[string]struct{} {
		table, ok := getTableDef(tableID)
		if !ok {
			t.Fatalf("loot table %q not found", tableID)
		}
		return table.ReachableItemIDs()
	}

	merchantItems := reachableItems("merchant_basic")
	for ring := range wantInMerchant {
		if _, ok := merchantItems[ring]; ok {
			wantInMerchant[ring] = true
		}
	}

	enemyItems := reachableItems("raider_loot")
	for ring := range wantInEnemy {
		if _, ok := enemyItems[ring]; ok {
			wantInEnemy[ring] = true
		}
	}

	for id, found := range wantInMerchant {
		if !found {
			t.Errorf("ring %q not reachable from merchant_basic", id)
		}
	}
	for id, found := range wantInEnemy {
		if !found {
			t.Errorf("ring %q not reachable from raider_loot", id)
		}
	}
}
