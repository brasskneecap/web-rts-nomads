package game

import (
	"errors"
	"fmt"
	"log/slog"
)

// ─── Editor orchestration: one save request → item + recipe + availability ──

type EditorRecipeSpec struct {
	Inputs   []string `json:"inputs"`
	CostGold int      `json:"costGold"`
}

type EditorLootAvailability struct {
	Enabled bool `json:"enabled"`
	Weight  int  `json:"weight"`
}

type EditorAvailability struct {
	Marketplace       bool                   `json:"marketplace"`
	WanderingMerchant bool                   `json:"wanderingMerchant"`
	LootTable         EditorLootAvailability `json:"lootTable"`
	RecipeList        bool                   `json:"recipeList"`
}

type EditorItemSaveRequest struct {
	Item         ItemDef            `json:"item"`
	Recipe       *EditorRecipeSpec  `json:"recipe"`
	Availability EditorAvailability `json:"availability"`
}

// editorValidationError wraps content errors so the HTTP layer maps them to
// 400 (everything else is a 500). Mirrors IsMapSaveValidationError.
type editorValidationError struct{ err error }

func (e editorValidationError) Error() string { return e.err.Error() }
func (e editorValidationError) Unwrap() error { return e.err }

func IsEditorValidationError(err error) bool {
	var v editorValidationError
	return errors.As(err, &v)
}

// SaveEditorItem validates EVERYTHING first (so a failure never leaves a
// partial save), then applies: item def → recipe → list memberships → loot
// membership. Directory-resolution or IO failures can still land mid-way
// (documented last-write-wins editor semantics, same as maps).
func SaveEditorItem(req EditorItemSaveRequest) error {
	item := req.Item
	// ── validate-first phase (no writes) ──
	if !itemIDPattern.MatchString(item.ID) {
		return editorValidationError{fmt.Errorf("item id %q must match %s", item.ID, itemIDPattern)}
	}
	if err := validateItemDef(&item); err != nil {
		return editorValidationError{err}
	}
	var recipe *RecipeDef
	if req.Recipe != nil {
		for _, in := range req.Recipe.Inputs {
			if in == item.ID {
				return editorValidationError{fmt.Errorf("recipe for %q cannot use itself as an input", item.ID)}
			}
		}
		recipe = &RecipeDef{
			ID:       item.ID,
			Name:     item.DisplayName,
			Inputs:   req.Recipe.Inputs,
			CostGold: req.Recipe.CostGold,
			Output:   item.ID,
		}
		// validateRecipeDef resolves inputs/output via getItemDef; the output
		// isn't registered yet on a brand-new item, so validate inputs here
		// and the full recipe after the item registers.
		for i, in := range recipe.Inputs {
			if _, ok := getItemDef(in); !ok {
				return editorValidationError{fmt.Errorf("recipe input[%d] %q is not a known item", i, in)}
			}
		}
		if len(recipe.Inputs) < 2 {
			return editorValidationError{fmt.Errorf("recipe needs at least 2 inputs, has %d", len(recipe.Inputs))}
		}
		if recipe.CostGold < 0 {
			return editorValidationError{fmt.Errorf("recipe costGold must not be negative")}
		}
	}

	// ── apply phase ──
	if err := SaveItemDef(&item); err != nil {
		return err
	}
	if recipe != nil {
		if err := SaveRecipeDef(recipe); err != nil {
			return err
		}
	} else {
		// Crafting toggled off: drop any overlay recipe named after the item.
		// Embedded recipes can't be deleted — reverting an embedded recipe is
		// out of scope (spec).
		if _, err := DeleteRecipeOverride(item.ID); err != nil {
			return err
		}
	}
	if err := ensureItemListMembership("marketplace", item.ID, req.Availability.Marketplace); err != nil {
		return err
	}
	if err := ensureItemListMembership("wandering_merchant", item.ID, req.Availability.WanderingMerchant); err != nil {
		return err
	}
	if err := SetMerchantItemAvailability(item.ID, item.Category, req.Availability.LootTable.Enabled, req.Availability.LootTable.Weight); err != nil {
		return err
	}
	inRecipeList := req.Availability.RecipeList && recipe != nil
	if err := ensureRecipeListMembership("druid_recipes_1", item.ID, inRecipeList); err != nil {
		return err
	}
	return nil
}

// GetItemAvailability reports where an item is currently placed across the
// four editor-managed surfaces. ok is false when the item id resolves to no
// def. Loot weight is the row's current d100 width in the item's
// category-mapped merchant subtable.
func GetItemAvailability(id string) (EditorAvailability, bool) {
	def, ok := getItemDef(id)
	if !ok {
		return EditorAvailability{}, false
	}
	var av EditorAvailability
	if list, ok := getItemListDef("marketplace"); ok {
		av.Marketplace = containsString(list.Items, id)
	}
	if list, ok := getItemListDef("wandering_merchant"); ok {
		av.WanderingMerchant = containsString(list.Items, id)
	}
	if sub, ok := getPackagedItem(merchantSubtableForCategory(def.Category)); ok {
		for _, e := range sub.Entries {
			if e.Item == id {
				av.LootTable.Enabled = true
				av.LootTable.Weight = e.Max - e.Min + 1
				break
			}
		}
	}
	if list, ok := getRecipeListDef("druid_recipes_1"); ok {
		av.RecipeList = containsString(list.Recipes, id)
	}
	return av, true
}

// DeleteEditorItem removes the item override. For editor-created items (not
// in the embed) it also strips the recipe, list memberships, and loot rows so
// no dangling references survive (list/recipe validators would reject them at
// next startup otherwise).
func DeleteEditorItem(id string) (existed bool, err error) {
	existed, err = DeleteItemOverride(id)
	if err != nil || !existed {
		return existed, err
	}
	if ItemIsEmbedded(id) {
		return true, nil // reset-to-default: embed provides the def; memberships stay
	}
	if _, derr := DeleteRecipeOverride(id); derr != nil {
		return true, derr
	}
	if lerr := ensureItemListMembership("marketplace", id, false); lerr != nil {
		return true, lerr
	}
	if lerr := ensureItemListMembership("wandering_merchant", id, false); lerr != nil {
		return true, lerr
	}
	if lerr := ensureRecipeListMembership("druid_recipes_1", id, false); lerr != nil {
		return true, lerr
	}
	// Sweep every merchant subtable (category may have changed since save).
	// ErrLastMerchantItem is non-fatal here: it means id is the sole entry in
	// that subtable, so removing it would leave an empty subtable, which
	// SetMerchantItemAvailability refuses to persist. We accept the dangling
	// loot row rather than abort the whole delete over it: the stale
	// reference to the now-deleted item is validated away at the next
	// startup load (loadPersistedLootTablesFromFile rejects a catalog that
	// references an unknown item and falls back to the trusted embedded
	// catalog), so it cannot ship as broken state — a single-operator dev
	// tool, same tradeoff as the loot-table live-ness decision above.
	for _, sub := range []string{"Weapon", "Armor", "Accessory", "Consumable"} {
		if lerr := SetMerchantItemAvailability(id, sub, false, 0); lerr != nil {
			if errors.Is(lerr, ErrLastMerchantItem) {
				slog.Warn("delete editor item: left dangling loot row (last item in subtable)", "id", id, "subtable", sub, "err", lerr)
				continue
			}
			return true, lerr
		}
	}
	return true, nil
}
