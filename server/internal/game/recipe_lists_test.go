package game

import "testing"

// TestRecipeListCatalog_LoadsAndValidates confirms the lists/ subdirectory is
// loaded as recipe-list defs (not as recipe defs) and that the seeded
// druid_recipes_1 list resolves to real recipes.
func TestRecipeListCatalog_LoadsAndValidates(t *testing.T) {
	def, ok := getRecipeListDef("druid_recipes_1")
	if !ok {
		t.Fatal("recipe list druid_recipes_1 not found")
	}
	if def.Name == "" {
		t.Error("druid_recipes_1: name should be populated")
	}
	if len(def.Recipes) == 0 {
		t.Fatal("druid_recipes_1: expected at least one recipe")
	}
	for _, id := range def.Recipes {
		if _, ok := getRecipeDef(id); !ok {
			t.Errorf("druid_recipes_1 references unknown recipe %q", id)
		}
	}
	if len(ListRecipeListDefs()) < 1 {
		t.Fatalf("expected >=1 recipe list, got %d", len(ListRecipeListDefs()))
	}
}

// TestRecipeCatalog_ExcludesListsDir guards that files under catalog/recipes/lists
// are NOT parsed as recipe defs (they have a different schema and would fail
// RecipeDef validation).
func TestRecipeCatalog_ExcludesListsDir(t *testing.T) {
	if _, ok := getRecipeDef("druid_recipes_1"); ok {
		t.Error("a recipe list id must not be registered as a recipe def")
	}
}

func TestValidateRecipeListDef_Rules(t *testing.T) {
	if err := validateRecipeListDef(&RecipeListDef{ID: "l", Recipes: []string{}}); err == nil {
		t.Error("expected error for empty recipe list")
	}
	if err := validateRecipeListDef(&RecipeListDef{ID: "l", Recipes: []string{"no_such_recipe"}}); err == nil {
		t.Error("expected error for unknown recipe id")
	}
	if err := validateRecipeListDef(&RecipeListDef{ID: "l", Recipes: []string{"fire_sword"}}); err != nil {
		t.Errorf("valid recipe list rejected: %v", err)
	}
}
