package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ─── Writable recipe / list overlays (see item_persistence.go) ──────────────

var (
	runtimeRecipesMu sync.RWMutex
	runtimeRecipes   = map[string]*RecipeDef{}

	runtimeRecipeListsMu sync.RWMutex
	runtimeRecipeLists   = map[string]*RecipeListDef{}

	runtimeItemListsMu sync.RWMutex
	runtimeItemLists   = map[string]*ItemListDef{}
)

func resolveRecipesDir() (string, error) {
	if dir := os.Getenv("RECIPE_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "recipes")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("recipes directory not found at %s; set RECIPE_CATALOG_DIR env var to override", dir)
}

// SaveRecipeDef validates, writes <dir>/<rarity>/<id>.json (rarity = the
// OUTPUT item's tier, matching how the embed loader derives Rarity from the
// subdirectory), and registers live.
func SaveRecipeDef(def *RecipeDef) error {
	if err := validateRecipeDef(def); err != nil {
		return err
	}
	out, ok := getItemDef(def.Output)
	if !ok {
		return fmt.Errorf("recipe %q: output %q is not a known item", def.ID, def.Output)
	}
	rarity := out.Tier
	if rarity == "" {
		rarity = ItemTierUncommon
	}
	dir, err := resolveRecipesDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, string(rarity))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// Rarity is path-derived on load; never author it into the file.
	fileDef := *def
	fileDef.Rarity = ""
	raw, err := json.MarshalIndent(&fileDef, "", "  ")
	if err != nil {
		return err
	}
	removeRecipeOverrideFiles(dir, def.ID)
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	reg.Rarity = rarity
	runtimeRecipesMu.Lock()
	runtimeRecipes[def.ID] = &reg
	runtimeRecipesMu.Unlock()
	return nil
}

// DeleteRecipeOverride removes the writable recipe file + overlay entry.
func DeleteRecipeOverride(id string) (existed bool, err error) {
	dir, derr := resolveRecipesDir()
	if derr != nil {
		return false, derr
	}
	removed := removeRecipeOverrideFiles(dir, id)
	runtimeRecipesMu.Lock()
	_, inOverlay := runtimeRecipes[id]
	delete(runtimeRecipes, id)
	runtimeRecipesMu.Unlock()
	return removed || inOverlay, nil
}

func removeRecipeOverrideFiles(dir, id string) bool {
	removed := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == id+".json" {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
		return nil
	})
	return removed
}

// LoadPersistedRecipesIntoOverlay — startup hook, best-effort.
func LoadPersistedRecipesIntoOverlay() {
	dir, err := resolveRecipesDir()
	if err != nil {
		slog.Info("persisted recipes: no writable recipes dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedRecipesFromDir(dir); n > 0 {
		slog.Info("persisted recipes: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedRecipesFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedRecipeFile(dir, path)
		if perr != nil {
			slog.Warn("persisted recipes: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeRecipesMu.Lock()
		runtimeRecipes[def.ID] = def
		runtimeRecipesMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedRecipeFile(root, path string) (def *RecipeDef, err error) {
	defer func() {
		if r := recover(); r != nil {
			def = nil
			err = fmt.Errorf("invalid recipe: %v", r)
		}
	}()
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d RecipeDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("recipe has empty id")
	}
	// Rarity from the subdirectory relative to root, like the embed loader.
	rel, rerr2 := filepath.Rel(root, path)
	if rerr2 == nil {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 {
			d.Rarity = ItemTier(parts[0])
		}
	}
	if d.Rarity == "" {
		d.Rarity = ItemTierUncommon
	}
	if verr := validateRecipeDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}

// ─── List saves + membership helpers ─────────────────────────────────────────

// SaveItemListDef writes the whole list to <items dir>/lists/<id>.json and
// registers it in the overlay.
func SaveItemListDef(def *ItemListDef) error {
	dir, err := resolveItemsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, "lists")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	runtimeItemListsMu.Lock()
	runtimeItemLists[def.ID] = &reg
	runtimeItemListsMu.Unlock()
	return nil
}

// SaveRecipeListDef — same for recipe lists under <recipes dir>/lists/.
func SaveRecipeListDef(def *RecipeListDef) error {
	dir, err := resolveRecipesDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, "lists")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	runtimeRecipeListsMu.Lock()
	runtimeRecipeLists[def.ID] = &reg
	runtimeRecipeListsMu.Unlock()
	return nil
}

// ensureItemListMembership adds/removes itemID in the named list and saves the
// whole list. Idempotent: no write when already in the desired state. An
// unknown list is created on add (empty name = title-cased id).
func ensureItemListMembership(listID, itemID string, member bool) error {
	cur, ok := getItemListDef(listID)
	var list ItemListDef
	if ok {
		list = *cur
		list.Items = append([]string(nil), cur.Items...)
	} else {
		if !member {
			return nil
		}
		list = ItemListDef{ID: listID, Name: listID}
	}
	idx := -1
	for i, id := range list.Items {
		if id == itemID {
			idx = i
			break
		}
	}
	switch {
	case member && idx >= 0, !member && idx < 0:
		return nil // already in desired state
	case member:
		list.Items = append(list.Items, itemID)
		sort.Strings(list.Items)
	default:
		list.Items = append(list.Items[:idx], list.Items[idx+1:]...)
	}
	return SaveItemListDef(&list)
}

// ensureRecipeListMembership — mirror for recipe lists.
func ensureRecipeListMembership(listID, recipeID string, member bool) error {
	cur, ok := getRecipeListDef(listID)
	var list RecipeListDef
	if ok {
		list = *cur
		list.Recipes = append([]string(nil), cur.Recipes...)
	} else {
		if !member {
			return nil
		}
		list = RecipeListDef{ID: listID, Name: listID}
	}
	idx := -1
	for i, id := range list.Recipes {
		if id == recipeID {
			idx = i
			break
		}
	}
	switch {
	case member && idx >= 0, !member && idx < 0:
		return nil
	case member:
		list.Recipes = append(list.Recipes, recipeID)
		sort.Strings(list.Recipes)
	default:
		list.Recipes = append(list.Recipes[:idx], list.Recipes[idx+1:]...)
	}
	return SaveRecipeListDef(&list)
}
