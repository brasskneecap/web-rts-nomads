package game

import (
	"os"
	"path/filepath"
	"testing"
)

func lootOverlayCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		runtimeLootCatalogMu.Lock()
		runtimeLootCatalog = nil
		runtimePackagedItems = nil
		runtimeLootTables = nil
		runtimeLootCatalogMu.Unlock()
	})
}

// subtableWidths returns item→width for a packaged subtable (test helper).
func subtableWidths(t *testing.T, subtable string) map[string]int {
	t.Helper()
	pi, ok := getPackagedItem(subtable)
	if !ok {
		t.Fatalf("subtable %q missing", subtable)
	}
	out := map[string]int{}
	total := 0
	for _, e := range pi.Entries {
		w := e.Max - e.Min + 1
		out[e.Item] = w
		total += w
	}
	if total != 100 {
		t.Fatalf("subtable %q widths sum to %d, want 100", subtable, total)
	}
	return out
}

// TestSetMerchantItemAvailability_AddRenormalizesTo100.
func TestSetMerchantItemAvailability_AddRenormalizesTo100(t *testing.T) {
	lootOverlayCleanup(t)
	dir := t.TempDir()
	t.Setenv("NEUTRAL_GROUPS_DIR", dir)

	before := subtableWidths(t, "merchant_armor")
	if _, present := before["elven_cloak"]; present {
		t.Skip("elven_cloak already in merchant_armor")
	}
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", true, 20); err != nil {
		t.Fatalf("add: %v", err)
	}
	after := subtableWidths(t, "merchant_armor") // re-validates sum==100
	if w := after["elven_cloak"]; w < 15 || w > 25 {
		t.Errorf("added weight ~20 expected (rounding tolerance), got %d", w)
	}
	// Relative order of pre-existing entries preserved (widths scaled, not zeroed).
	for item, w := range before {
		if after[item] == 0 {
			t.Errorf("pre-existing %q lost its slot", item)
		}
		_ = w
	}
	// Whole file written.
	if _, err := os.Stat(filepath.Join(dir, "loot_tables.json")); err != nil {
		t.Fatalf("loot_tables.json not written: %v", err)
	}
}

// TestSetMerchantItemAvailability_RemoveAndIdempotence.
func TestSetMerchantItemAvailability_RemoveAndIdempotence(t *testing.T) {
	lootOverlayCleanup(t)
	dir := t.TempDir()
	t.Setenv("NEUTRAL_GROUPS_DIR", dir)

	if err := SetMerchantItemAvailability("elven_cloak", "Armor", true, 10); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", false, 0); err != nil {
		t.Fatalf("remove: %v", err)
	}
	after := subtableWidths(t, "merchant_armor")
	if _, present := after["elven_cloak"]; present {
		t.Error("item must be gone after remove")
	}
	// Removing again is a no-op, not an error.
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", false, 0); err != nil {
		t.Fatalf("re-remove: %v", err)
	}
}

// TestLootOverlay_LoadFromFileOverridesEmbed: a persisted loot_tables.json is
// picked up at startup and wins over the embed.
func TestLootOverlay_LoadFromFileOverridesEmbed(t *testing.T) {
	lootOverlayCleanup(t)
	dir := t.TempDir()
	t.Setenv("NEUTRAL_GROUPS_DIR", dir)
	// Produce a persisted file via the editing API, then clear the in-memory
	// overlay and reload it from disk — simulating a restart.
	if err := SetMerchantItemAvailability("elven_cloak", "Armor", true, 10); err != nil {
		t.Fatalf("seed: %v", err)
	}
	runtimeLootCatalogMu.Lock()
	runtimeLootCatalog, runtimePackagedItems, runtimeLootTables = nil, nil, nil
	runtimeLootCatalogMu.Unlock()
	if ok := loadPersistedLootTablesFromFile(filepath.Join(dir, "loot_tables.json")); !ok {
		t.Fatal("reload from disk failed")
	}
	widths := subtableWidths(t, "merchant_armor")
	if _, present := widths["elven_cloak"]; !present {
		t.Error("persisted membership lost across simulated restart")
	}
}

// TestMerchantSubtableForCategory mapping.
func TestMerchantSubtableForCategory(t *testing.T) {
	cases := map[string]string{
		"Weapon": "merchant_weapons", "Armor": "merchant_armor", "Shield": "merchant_armor",
		"Accessory": "merchant_accessories", "Consumable": "merchant_potions", "Anything": "merchant_accessories",
	}
	for cat, want := range cases {
		if got := merchantSubtableForCategory(cat); got != want {
			t.Errorf("%s → %s, want %s", cat, got, want)
		}
	}
}
