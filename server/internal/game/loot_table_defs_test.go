package game

import "testing"

// TestLootTableLoader_Loads pins structural invariants. Deliberately does NOT
// pin specific drop chances or quantities — those are balance.
func TestLootTableLoader_Loads(t *testing.T) {
	table, ok := getLootTable("raider_loot")
	if !ok {
		t.Fatalf("expected raider_loot table to be loaded")
	}
	if len(table) == 0 {
		t.Fatalf("raider_loot table has zero entries")
	}
	for _, e := range table {
		if e.Min < 1 || e.Max < e.Min || e.Max > 100 {
			t.Errorf("raider_loot entry %q: invalid range [%d,%d]", e.Entry, e.Min, e.Max)
		}
		if _, ok := getPackagedItem(e.Entry); !ok {
			t.Errorf("raider_loot entry references unknown packaged item %q", e.Entry)
		}
	}
}

// TestLootTableLoader_PackagedItems pins kind + structural validity.
func TestLootTableLoader_PackagedItems(t *testing.T) {
	bundle, ok := getPackagedItem("small_resource_bundle")
	if !ok {
		t.Fatalf("small_resource_bundle missing")
	}
	if bundle.Kind != PackagedItemResourceBundle {
		t.Errorf("small_resource_bundle kind = %v, want resource_bundle", bundle.Kind)
	}
	if len(bundle.Resources) == 0 {
		t.Errorf("small_resource_bundle has no resources")
	}
	for k, v := range bundle.Resources {
		if k != "gold" && k != "wood" {
			t.Errorf("small_resource_bundle has unexpected resource key %q", k)
		}
		if v <= 0 {
			t.Errorf("small_resource_bundle.%s = %d, want > 0", k, v)
		}
	}

	weapons, ok := getPackagedItem("basic_weapons")
	if !ok {
		t.Fatalf("basic_weapons missing")
	}
	if weapons.Kind != PackagedItemSubtable {
		t.Errorf("basic_weapons kind = %v, want item_subtable", weapons.Kind)
	}
	if len(weapons.Entries) == 0 {
		t.Errorf("basic_weapons has zero sub-table entries")
	}
	for i, e := range weapons.Entries {
		if e.Min < 1 || e.Max < e.Min {
			t.Errorf("basic_weapons[%d] invalid range [%d,%d]", i, e.Min, e.Max)
		}
		if _, ok := getItemDef(e.Item); !ok {
			t.Errorf("basic_weapons[%d] references unknown item %q", i, e.Item)
		}
	}
}

// TestLootTableLoader_UnknownLookups verifies miss paths.
func TestLootTableLoader_UnknownLookups(t *testing.T) {
	if _, ok := getLootTable("nope_not_a_table"); ok {
		t.Errorf("expected miss on unknown table id")
	}
	if _, ok := getPackagedItem("nope_not_an_item"); ok {
		t.Errorf("expected miss on unknown packaged-item id")
	}
}
