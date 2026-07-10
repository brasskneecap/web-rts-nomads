package game

import (
	"errors"
	"fmt"
)

// ─── Editor orchestration: one save request → item + its recipe ─────────────
//
// An item defines only itself: its stats and its own costs (CostGold to
// purchase; RecipeCost + IsRecipe when craftable). WHERE an item is available
// — which shops stock it, loot tables — is a shop/loot-level concern edited
// elsewhere (future work), not baked into the item. The editor keeps a paired
// recipe def in sync with the item's IsRecipe flag so a craftable item always
// has exactly one recipe unlocking it at the Artificer.

type EditorItemSaveRequest struct {
	Item ItemDef `json:"item"`
	// Inputs are the recipe ingredients, used only when Item.IsRecipe is true.
	Inputs []string `json:"inputs"`
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

// SaveEditorItem validates the item (and its recipe inputs, when craftable)
// before any write, then saves the item def and syncs its recipe: a craftable
// item upserts a recipe (output = the item, cost = Item.RecipeCost); a
// non-craftable item drops any overlay recipe named after it. No availability
// (shop/loot) writes happen here — that is decided at the shop level.
func SaveEditorItem(req EditorItemSaveRequest) error {
	item := req.Item
	// ── validate-first phase (no writes) ──
	if !itemIDPattern.MatchString(item.ID) {
		return editorValidationError{fmt.Errorf("item id %q must match %s", item.ID, itemIDPattern)}
	}
	if err := validateItemDef(&item); err != nil {
		return editorValidationError{err}
	}
	if item.IsRecipe {
		if len(req.Inputs) < 2 {
			return editorValidationError{fmt.Errorf("a craftable item needs at least 2 recipe inputs, has %d", len(req.Inputs))}
		}
		for i, in := range req.Inputs {
			if in == item.ID {
				return editorValidationError{fmt.Errorf("recipe for %q cannot use itself as an input", item.ID)}
			}
			// Output existence is checked after the item registers (a brand-new
			// item isn't in the catalog yet); inputs must already exist.
			if _, ok := getItemDef(in); !ok {
				return editorValidationError{fmt.Errorf("recipe input[%d] %q is not a known item", i, in)}
			}
		}
		if item.RecipeCost < 0 {
			return editorValidationError{fmt.Errorf("recipeCost must not be negative")}
		}
	}

	// ── apply phase ──
	if err := SaveItemDef(&item); err != nil {
		return err
	}
	if item.IsRecipe {
		recipe := &RecipeDef{
			ID:       item.ID,
			Name:     item.DisplayName,
			Inputs:   req.Inputs,
			CostGold: item.RecipeCost,
			Output:   item.ID,
		}
		return SaveRecipeDef(recipe)
	}
	// Not craftable: drop any overlay recipe named after the item. Embedded
	// recipes can't be deleted (reverting an embedded recipe is out of scope).
	_, err := DeleteRecipeOverride(item.ID)
	return err
}

// GetItemAvailability reports where an item is currently placed across the
// shop/loot surfaces. Retained as read-only infrastructure for a future shop
// editor; the item editor itself no longer reads or writes availability. ok is
// false when the item id resolves to no def.
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

// DeleteEditorItem removes the item override and, for editor-created items
// (not in the embed), the paired recipe. Availability memberships are not
// touched — items no longer own availability, so an editor-created item was
// never added to any shop/loot list by the editor.
func DeleteEditorItem(id string) (existed bool, err error) {
	existed, err = DeleteItemOverride(id)
	if err != nil || !existed {
		return existed, err
	}
	if ItemIsEmbedded(id) {
		return true, nil // reset-to-default: embed provides the def + recipe
	}
	if _, derr := DeleteRecipeOverride(id); derr != nil {
		return true, derr
	}
	return true, nil
}

// EditorLootAvailability / EditorAvailability describe an item's placement
// across the shop/loot surfaces. Kept as the read shape for GetItemAvailability
// (future shop editor); no longer part of the item save request.
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
