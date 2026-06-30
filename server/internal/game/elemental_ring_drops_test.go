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

	// reachableItems walks a top-level loot table and returns the set of item
	// IDs that can be reached through any item_subtable packaged-item entry.
	reachableItems := func(tableID string) map[string]struct{} {
		out := make(map[string]struct{})
		table, ok := getLootTable(tableID)
		if !ok {
			t.Fatalf("loot table %q not found", tableID)
		}
		for _, entry := range table {
			pkg, ok := getPackagedItem(entry.Entry)
			if !ok {
				continue
			}
			if pkg.Kind != PackagedItemSubtable {
				continue
			}
			for _, sub := range pkg.Entries {
				out[sub.Item] = struct{}{}
			}
		}
		return out
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
