package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed catalog/recipes
var recipeDefsFS embed.FS

// RecipeDef is the catalog definition for one craftable recipe: a set of input
// item IDs (consumed) plus a gold cost that produce one output item ID.
type RecipeDef struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Inputs   []string `json:"inputs"`
	CostGold int      `json:"costGold"`
	Output   string   `json:"output"`
	// Rarity is the recipe's quality tier, derived from the subdirectory it
	// lives in under catalog/recipes (e.g. catalog/recipes/rare/*.json → "rare").
	// A file directly under catalog/recipes/ defaults to "common". This mirrors
	// how items organize by tier and drives the Recipe Shop's icon selection
	// (see the client's ${rarity}_recipe asset lookup). It is populated at
	// catalog load, not read from JSON.
	Rarity ItemTier `json:"rarity"`
	// Starter, when true, marks a recipe every player has unlocked at their
	// Artificer from match start — no Recipe Shop purchase required. Seeded into
	// Player.UnlockedRecipeIDs at join (see EnsurePlayerWithUpgrades).
	Starter bool `json:"starter,omitempty"`
}

var recipeCatalogSingleton = loadRecipeCatalog()

// recipeListsSubdir is the catalog/recipes subdirectory that holds named recipe
// lists (a different schema — see RecipeListDef). It is skipped by the recipe
// def walk so list files are never parsed as recipe defs.
const recipeListsSubdir = "catalog/recipes/lists"

func loadRecipeCatalog() map[string]*RecipeDef {
	catalog := make(map[string]*RecipeDef)
	err := fs.WalkDir(recipeDefsFS, "catalog/recipes", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == recipeListsSubdir {
				return fs.SkipDir // recipe lists are loaded separately
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := recipeDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def RecipeDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		def.Rarity = recipeRarityFromPath(path)
		if !validRecipeRarities[def.Rarity] {
			panic(fmt.Sprintf("%s: rarity %q is not a known tier — put the recipe under a catalog/recipes/<tier>/ subdirectory (common/uncommon/rare/epic/legendary)", path, def.Rarity))
		}
		if err := validateRecipeDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/recipes: " + err.Error())
	}
	return catalog
}

// recipeRarityFromPath derives a recipe's rarity tier from its catalog path:
// the immediate parent directory under catalog/recipes (e.g.
// "catalog/recipes/rare/fire_sword.json" → "rare"). A file directly under
// catalog/recipes/ (no tier subdir) defaults to common. Embed paths always use
// forward slashes.
func recipeRarityFromPath(path string) ItemTier {
	rel := strings.TrimPrefix(path, "catalog/recipes/")
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return ItemTierCommon
	}
	return ItemTier(parts[len(parts)-2])
}

// validRecipeRarities is the set of tiers a recipe subdirectory may name. It is
// the same tier vocabulary items use (see ItemTier), so a recipe organized by
// the tier of its output item always has a valid folder.
var validRecipeRarities = map[ItemTier]bool{
	ItemTierCommon:    true,
	ItemTierUncommon:  true,
	ItemTierRare:      true,
	ItemTierEpic:      true,
	ItemTierLegendary: true,
}

// validateRecipeDef enforces: at least two inputs, and every input + the output
// resolves to a real item def. Called at catalog load (fail-fast). Rarity is
// validated separately in the loader since it is derived from the file path,
// not the def's own content.
func validateRecipeDef(def *RecipeDef) error {
	if len(def.Inputs) < 2 {
		return fmt.Errorf("recipe %q: needs at least 2 inputs, has %d", def.ID, len(def.Inputs))
	}
	for i, in := range def.Inputs {
		if _, ok := getItemDef(in); !ok {
			return fmt.Errorf("recipe %q: input[%d] %q is not a known item", def.ID, i, in)
		}
	}
	if _, ok := getItemDef(def.Output); !ok {
		return fmt.Errorf("recipe %q: output %q is not a known item", def.ID, def.Output)
	}
	// Negative gold would grant the player gold on craft (an exploit); zero is
	// allowed for recipes whose only cost is their ingredient items.
	if def.CostGold < 0 {
		return fmt.Errorf("recipe %q: costGold must not be negative, got %d", def.ID, def.CostGold)
	}
	return nil
}

// starterRecipeIDs returns the IDs of all recipes flagged Starter, sorted, so
// they can be seeded into every player's unlocked set at match start. Order is
// deterministic (sorted) so seeding never depends on map iteration order.
func starterRecipeIDs() []string {
	ids := make([]string, 0)
	for id, def := range recipeCatalogSingleton {
		if def.Starter {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func getRecipeDef(id string) (*RecipeDef, bool) {
	def, ok := recipeCatalogSingleton[id]
	return def, ok
}

// ListRecipeDefs returns all recipe defs sorted by ID (for the HTTP route and
// deterministic iteration).
func ListRecipeDefs() []*RecipeDef {
	defs := make([]*RecipeDef, 0, len(recipeCatalogSingleton))
	for _, def := range recipeCatalogSingleton {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// RecipeListDef is a named, curated set of recipe IDs. A Recipe Shop assigned a
// list (via map metadata "recipeList") stocks from that list's recipes instead
// of the global pool. Authored under catalog/recipes/lists/<id>.json.
type RecipeListDef struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Recipes []string `json:"recipes"`
}

var recipeListCatalogSingleton = loadRecipeListCatalog()

func loadRecipeListCatalog() map[string]*RecipeListDef {
	catalog := make(map[string]*RecipeListDef)
	entries, err := fs.ReadDir(recipeDefsFS, recipeListsSubdir)
	if err != nil {
		// No lists/ directory is valid — recipe lists are optional.
		return catalog
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := recipeListsSubdir + "/" + e.Name()
		data, err := recipeDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def RecipeListDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		if err := validateRecipeListDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
	}
	return catalog
}

// validateRecipeListDef enforces: at least one recipe, and every referenced
// recipe ID resolves to a real recipe def. Called at catalog load (fail-fast).
func validateRecipeListDef(def *RecipeListDef) error {
	if len(def.Recipes) == 0 {
		return fmt.Errorf("recipe list %q: needs at least 1 recipe", def.ID)
	}
	for i, id := range def.Recipes {
		if _, ok := getRecipeDef(id); !ok {
			return fmt.Errorf("recipe list %q: recipes[%d] %q is not a known recipe", def.ID, i, id)
		}
	}
	return nil
}

func getRecipeListDef(id string) (*RecipeListDef, bool) {
	def, ok := recipeListCatalogSingleton[id]
	return def, ok
}

// ListRecipeListDefs returns all recipe-list defs sorted by ID (for the HTTP
// route and deterministic iteration).
func ListRecipeListDefs() []*RecipeListDef {
	defs := make([]*RecipeListDef, 0, len(recipeListCatalogSingleton))
	for _, def := range recipeListCatalogSingleton {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
