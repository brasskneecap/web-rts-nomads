package game

import (
	"errors"
	"fmt"
)

// ─── Editor orchestration: one save request → item + its recipe ─────────────
//
// An item defines only itself: its stats and its own purchase price
// (ItemDef.CostGold). Everything about CRAFTING it — the ingredients, the craft
// cost, the cost to learn the recipe, whether every player starts knowing it —
// belongs to the paired RecipeDef, which is the single source of truth for
// craftability. The item carries no mirror of any of it.
//
// WHERE an item is available — which shops stock it, loot tables — is a
// shop/loot-level concern edited elsewhere (future work), not baked into the
// item.

type EditorItemSaveRequest struct {
	Item ItemDef `json:"item"`
	// Crafting is the item's paired recipe, or nil when the item is not
	// craftable (in which case any overlay recipe named after it is dropped).
	Crafting *EditorItemCrafting `json:"crafting,omitempty"`
}

// EditorItemCrafting is the recipe half of an item save. The two gold fields
// are independent prices and the editor surfaces them as separate inputs — see
// RecipeDef for what each one buys.
type EditorItemCrafting struct {
	// Inputs are the recipe ingredients (2+), consumed on each craft.
	Inputs []string `json:"inputs"`
	// CraftCostGold is charged per craft at the Artificer (→ RecipeDef.CostGold).
	CraftCostGold int `json:"craftCostGold"`
	// RecipeCostGold is charged once at a Recipe Shop to learn the recipe
	// (→ RecipeDef.UnlockCostGold).
	RecipeCostGold int `json:"recipeCostGold"`
	// Starter marks the recipe as pre-learned by every player at match start,
	// which makes RecipeCostGold moot (it is never purchased).
	Starter bool `json:"starter,omitempty"`
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

// SaveEditorItem validates the item (and its recipe, when craftable) before any
// write, then saves the item def and syncs its recipe: a craftable item upserts
// a recipe (output = the item, craft cost + recipe cost from req.Crafting); a
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
	if c := req.Crafting; c != nil {
		if len(c.Inputs) < 2 {
			return editorValidationError{fmt.Errorf("a craftable item needs at least 2 recipe inputs, has %d", len(c.Inputs))}
		}
		for i, in := range c.Inputs {
			if in == item.ID {
				return editorValidationError{fmt.Errorf("recipe for %q cannot use itself as an input", item.ID)}
			}
			// Output existence is checked after the item registers (a brand-new
			// item isn't in the catalog yet); inputs must already exist.
			if _, ok := getItemDef(in); !ok {
				return editorValidationError{fmt.Errorf("recipe input[%d] %q is not a known item", i, in)}
			}
		}
		if c.CraftCostGold < 0 {
			return editorValidationError{fmt.Errorf("craft cost must not be negative")}
		}
		if c.RecipeCostGold < 0 {
			return editorValidationError{fmt.Errorf("recipe cost must not be negative")}
		}
	}

	// ── apply phase ──
	if err := SaveItemDef(&item); err != nil {
		return err
	}
	if c := req.Crafting; c != nil {
		recipe := &RecipeDef{
			ID:             item.ID,
			Name:           item.DisplayName,
			Inputs:         c.Inputs,
			CostGold:       c.CraftCostGold,
			UnlockCostGold: c.RecipeCostGold,
			Output:         item.ID,
			Starter:        c.Starter,
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

// DeleteEditorItem is the editor's destructive action, and what it means depends
// on where the item came from:
//
//   - A SHIPPED item is RESET, not deleted. It goes back to the state it was in
//     before the author's last save ("undo"), or — with no undo step recorded —
//     to the shipped catalog default ("default"). The def file always survives;
//     deleting it would destroy the item on the next build (see ResetItemDef).
//   - An AUTHOR-CREATED item is genuinely deleted, along with its paired recipe.
//
// The returned status is what the client shows, so it must say which happened.
// existed is false when the id names nothing.
func DeleteEditorItem(id string) (status string, existed bool, err error) {
	if ItemIsEmbedded(id) {
		mode, ok, rerr := ResetItemDef(id)
		if rerr != nil || !ok {
			return "", ok, rerr
		}
		if mode == "undo" {
			return "reverted", true, nil
		}
		return "reset", true, nil
	}

	existed, err = DeleteItemOverride(id)
	if err != nil || !existed {
		return "", existed, err
	}
	if _, derr := DeleteRecipeOverride(id); derr != nil {
		return "deleted", true, derr
	}
	return "deleted", true, nil
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
