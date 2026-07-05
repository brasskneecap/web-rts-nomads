package game

import "testing"

// armorItemIDs is the common armor line, in ascending power order.
var armorItemIDs = []string{"leather_armor", "half_plate", "plate_armor"}

// itemTierRank orders the ItemTier vocabulary from weakest to strongest so
// tests can assert tier *relationships* without pinning any item to a specific
// tier (tiers are catalog-owned and get rebalanced). Unknown tiers rank -1 so
// they fail the "known tier" check below.
var itemTierRank = map[ItemTier]int{
	ItemTierCommon:    0,
	ItemTierUncommon:  1,
	ItemTierRare:      2,
	ItemTierEpic:      3,
	ItemTierLegendary: 4,
}

// TestArmorItems_Load asserts the armor line exists in the catalog and is
// shaped correctly: equipment, armor slot, Armor category, and a positive armor
// modifier that strictly ascends leather → half plate → plate. Tier and armor
// values are balance tunables owned by the catalog JSON, so the test checks
// ordering invariants (armor strictly ascends, tier never descends) rather than
// pinning each item to a specific tier or number.
func TestArmorItems_Load(t *testing.T) {
	prevArmor := 0
	prevTierRank := -1
	for _, id := range armorItemIDs {
		def, ok := getItemDef(id)
		if !ok {
			t.Fatalf("item def %q not found", id)
		}
		if def.Kind != ItemKindEquipment {
			t.Errorf("%s: kind = %q, want equipment", id, def.Kind)
		}
		tierRank, known := itemTierRank[def.Tier]
		if !known {
			t.Errorf("%s: tier = %q is not a known tier", id, def.Tier)
		} else if tierRank < prevTierRank {
			t.Errorf("%s: tier %q ranks below the previous item in the line — a stronger armor item must not be a lower tier", id, def.Tier)
		}
		if def.SlotKind != ItemSlotKindArmor {
			t.Errorf("%s: slotKind = %q, want armor", id, def.SlotKind)
		}
		if def.Category != "Armor" {
			t.Errorf("%s: category = %q, want Armor", id, def.Category)
		}
		if def.Modifiers == nil || def.Modifiers.Armor <= 0 {
			t.Fatalf("%s: expected a positive armor modifier, got %+v", id, def.Modifiers)
		}
		if def.Modifiers.Armor <= prevArmor {
			t.Errorf("%s: armor %d not greater than previous tier's %d — the line must strictly ascend", id, def.Modifiers.Armor, prevArmor)
		}
		prevArmor = def.Modifiers.Armor
		prevTierRank = tierRank
	}
}

// TestArmorItems_InLootTables asserts every armor item is reachable from the
// neutral-merchant table so a painted neutral-shop can actually stock it.
// Same two-tier walk as TestElementalRings_InLootTables.
func TestArmorItems_InLootTables(t *testing.T) {
	table, ok := getLootTable("merchant_basic")
	if !ok {
		t.Fatal(`loot table "merchant_basic" not found`)
	}
	reachable := make(map[string]struct{})
	for _, entry := range table {
		pkg, ok := getPackagedItem(entry.Entry)
		if !ok || pkg.Kind != PackagedItemSubtable {
			continue
		}
		for _, sub := range pkg.Entries {
			reachable[sub.Item] = struct{}{}
		}
	}
	for _, id := range armorItemIDs {
		if _, ok := reachable[id]; !ok {
			t.Errorf("armor item %q not reachable from merchant_basic", id)
		}
	}
}
