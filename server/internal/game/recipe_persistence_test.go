package game

import (
	"os"
	"path/filepath"
	"testing"
)

func recipeOverlayCleanup(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		runtimeRecipesMu.Lock()
		delete(runtimeRecipes, id)
		runtimeRecipesMu.Unlock()
	})
}

// TestSaveRecipeDef_RarityFromOutputTierAndLiveRegistration.
func TestSaveRecipeDef_RarityFromOutputTierAndLiveRegistration(t *testing.T) {
	const id = "test_recipe_save"
	recipeOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("RECIPE_CATALOG_DIR", dir)
	// Inputs/output must be known items — use shipped ones.
	// The two prices are deliberately different, so a round-trip that drops or
	// crosses either one is visible here.
	def := &RecipeDef{ID: id, Name: "Test Recipe", Inputs: []string{"steel_shield", "fire_ring"},
		CostGold: 100, UnlockCostGold: 250, Output: "fire_shield"}
	if err := SaveRecipeDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	// fire_shield is rare → file lands in rare/.
	if _, err := os.Stat(filepath.Join(dir, "rare", id+".json")); err != nil {
		t.Fatalf("expected rare/%s.json: %v", id, err)
	}
	got, ok := getRecipeDef(id)
	if !ok || got.Rarity != ItemTierRare || got.CostGold != 100 || got.UnlockCostGold != 250 {
		t.Fatalf("live registration wrong: ok=%v %+v", ok, got)
	}
	// Validation still gates: unknown input rejected before write.
	bad := &RecipeDef{ID: "bad_r", Name: "x", Inputs: []string{"no_such_item", "fire_ring"}, CostGold: 1, Output: "fire_shield"}
	if err := SaveRecipeDef(bad); err == nil {
		t.Error("expected unknown-input validation error")
	}
}

// TestRecipeOverlay_LoadFromDirDerivesRarityFromPath.
func TestRecipeOverlay_LoadFromDirDerivesRarityFromPath(t *testing.T) {
	const id = "test_recipe_load"
	recipeOverlayCleanup(t, id)
	dir := t.TempDir()
	sub := filepath.Join(dir, "uncommon")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{ "id": "` + id + `", "name": "Loaded", "inputs": ["steel_shield", "fire_ring"], "costGold": 50, "output": "fire_shield" }`)
	if err := os.WriteFile(filepath.Join(sub, id+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if n := loadPersistedRecipesFromDir(dir); n < 1 {
		t.Fatalf("expected >=1 recipe loaded, got %d", n)
	}
	got, ok := getRecipeDef(id)
	if !ok || got.Rarity != ItemTierUncommon {
		t.Fatalf("rarity from path failed: ok=%v %+v", ok, got)
	}
}

// TestEnsureItemListMembership_AddRemoveIdempotent operates on the shipped
// marketplace list via the overlay (never mutating the embed).
func TestEnsureItemListMembership_AddRemoveIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeItemListsMu.Lock()
		delete(runtimeItemLists, "marketplace")
		runtimeItemListsMu.Unlock()
	})
	before, _ := getItemListDef("marketplace")
	baseLen := len(before.Items)

	// fire_shield (not fire_ring — fire_ring already ships in marketplace.json,
	// which would make the add a no-op) is a known item absent from the list.
	if err := ensureItemListMembership("marketplace", "fire_shield", true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := ensureItemListMembership("marketplace", "fire_shield", true); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	after, _ := getItemListDef("marketplace")
	if len(after.Items) != baseLen+1 {
		t.Fatalf("add not idempotent: %d → %d, want +1", baseLen, len(after.Items))
	}
	// File written whole to <items dir>/lists/.
	if _, err := os.Stat(filepath.Join(dir, "lists", "marketplace.json")); err != nil {
		t.Fatalf("list file not written: %v", err)
	}
	if err := ensureItemListMembership("marketplace", "fire_shield", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := ensureItemListMembership("marketplace", "fire_shield", false); err != nil {
		t.Fatalf("re-remove: %v", err)
	}
	final, _ := getItemListDef("marketplace")
	if len(final.Items) != baseLen {
		t.Fatalf("remove not idempotent: want %d, got %d", baseLen, len(final.Items))
	}
}

// TestEnsureRecipeListMembership_Idempotent — same shape for recipe lists.
func TestEnsureRecipeListMembership_Idempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RECIPE_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeRecipeListsMu.Lock()
		delete(runtimeRecipeLists, "druid_recipes_1")
		runtimeRecipeListsMu.Unlock()
	})
	before, _ := getRecipeListDef("druid_recipes_1")
	baseLen := len(before.Recipes)
	if err := ensureRecipeListMembership("druid_recipes_1", "scimitar", true); err != nil {
		t.Fatalf("add: %v", err)
	}
	after, _ := getRecipeListDef("druid_recipes_1")
	if len(after.Recipes) != baseLen+1 {
		t.Fatalf("want +1 member, got %d → %d", baseLen, len(after.Recipes))
	}
	if err := ensureRecipeListMembership("druid_recipes_1", "scimitar", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	final, _ := getRecipeListDef("druid_recipes_1")
	if len(final.Recipes) != baseLen {
		t.Fatalf("remove failed: %d", len(final.Recipes))
	}
}
