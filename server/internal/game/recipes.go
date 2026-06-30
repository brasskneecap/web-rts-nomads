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
}

var recipeCatalogSingleton = loadRecipeCatalog()

func loadRecipeCatalog() map[string]*RecipeDef {
	catalog := make(map[string]*RecipeDef)
	err := fs.WalkDir(recipeDefsFS, "catalog/recipes", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
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

// validateRecipeDef enforces: at least two inputs, and every input + the output
// resolves to a real item def. Called at catalog load (fail-fast).
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
	if def.CostGold <= 0 {
		return fmt.Errorf("recipe %q: costGold must be positive, got %d", def.ID, def.CostGold)
	}
	return nil
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
