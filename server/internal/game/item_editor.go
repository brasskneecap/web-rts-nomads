package game

import (
	"errors"
	"fmt"
)

// ─── Editor orchestration: one save request → one item def ──────────────────
//
// An item defines everything about ITSELF: its stats, its purchase price, and —
// in its crafting block — its ingredients and the two crafting prices. There is
// no second entity to keep in sync: an item IS its own recipe.
//
// WHERE an item is available — which shops stock it, which lists it belongs to,
// what drops it — is a LIST-level concern, edited in the Lists tab, not baked
// into the item.

type EditorItemSaveRequest struct {
	Item ItemDef `json:"item"`
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

// SaveEditorItem validates the item before any write, then saves it. The item's
// crafting block rides on the def itself, so there is nothing else to sync — a
// craftable item and its recipe are the same object.
//
// No availability (list / shop / loot) writes happen here: membership is a
// list-level concern, edited in the Lists tab.
func SaveEditorItem(req EditorItemSaveRequest) error {
	item := req.Item
	// ── validate-first phase (no writes) ──
	if !itemIDPattern.MatchString(item.ID) {
		return editorValidationError{fmt.Errorf("item id %q must match %s", item.ID, itemIDPattern)}
	}
	// validateItemDef covers the self-contained crafting rules (>=2 inputs, no
	// self-reference, non-negative prices).
	if err := validateItemDef(&item); err != nil {
		return editorValidationError{err}
	}
	// Inputs must already exist. Unlike the catalog loader this can use the live
	// lookup — the catalog is fully loaded by the time the editor runs.
	if err := validateItemCraftingRefs(&item, itemKnownInCatalog); err != nil {
		return editorValidationError{err}
	}

	// ── apply phase ──
	return SaveItemDef(&item)
}

// DeleteEditorItem is the editor's destructive action, and what it means depends
// on where the item came from:
//
//   - A SHIPPED item is RESET, not deleted. It goes back to the state it was in
//     before the author's last save ("undo"), or — with no undo step recorded —
//     to the shipped catalog default ("default"). The def file always survives;
//     deleting it would destroy the item on the next build (see ResetItemDef).
//   - An AUTHOR-CREATED item is genuinely deleted. Its recipe goes with it, for
//     free — the crafting block was never a separate file.
//
// The returned status is what the client shows, so it must say which happened.
// existed is false when the id names nothing.
//
// A SHIPPED item is never actually removed (see above), so its references
// stay valid no matter what points at it — the reference guard below only
// runs on the custom-item branch, where the delete is real.
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

	// Block & guide: refuse the delete rather than cascade it, naming every
	// referencing site so the author knows what to fix first. Must run
	// before DeleteItemOverride — the item must still be resolvable while
	// the scan reads the catalog.
	if refs := itemReferences(id); len(refs) > 0 {
		return "", false, editorValidationError{fmt.Errorf(
			"cannot delete item %q: still referenced by %s. Remove these references first.",
			id, formatReferences(refs))}
	}

	existed, err = DeleteItemOverride(id)
	if err != nil || !existed {
		return "", existed, err
	}
	return "deleted", true, nil
}

// ─── List editor ────────────────────────────────────────────────────────────

type EditorListSaveRequest struct {
	List ListDef `json:"list"`
}

// SaveEditorList validates and persists a list. Validation is deliberately thin:
// a list is untyped, so the only rules are "has members" and "every member is a
// real item". Whether those members make SENSE for the building the list is
// bound to is the editor's warning to give, not this function's error to raise —
// the same list may be nonsense as a recipe pool and perfect as a loot pool.
func SaveEditorList(req EditorListSaveRequest) error {
	list := req.List
	if !itemIDPattern.MatchString(list.ID) {
		return editorValidationError{fmt.Errorf("list id %q must match %s", list.ID, itemIDPattern)}
	}
	if err := validateListDef(&list); err != nil {
		return editorValidationError{err}
	}
	return SaveListDef(&list)
}

// DeleteEditorList removes an authored list. existed is false when the id names
// nothing. Note a list that ships in the embedded catalog cannot be deleted —
// only its overlay copy is removed, and the shipped version resurfaces.
//
// Block & guide: an override-only list that would actually stop resolving is
// refused when anything still references it, naming every referencing site.
// A list that also ships embedded is exempt — deleting its overlay copy only
// un-shadows the shipped def, so every existing reference stays valid.
func DeleteEditorList(id string) (existed bool, err error) {
	if !listIsEmbedded(id) {
		if refs := listReferences(id); len(refs) > 0 {
			return false, editorValidationError{fmt.Errorf(
				"cannot delete list %q: still referenced by %s. Remove these references first.",
				id, formatReferences(refs))}
		}
	}
	return DeleteListOverride(id)
}

// ─── Table editor ───────────────────────────────────────────────────────────

type EditorTableSaveRequest struct {
	Table TableDef `json:"table"`
}

// SaveEditorTable validates and persists a table. The heavy lifting is
// validateTableDef: rows tile the die, each does exactly one thing, and every
// list it names resolves.
func SaveEditorTable(req EditorTableSaveRequest) error {
	table := req.Table
	if !itemIDPattern.MatchString(table.ID) {
		return editorValidationError{fmt.Errorf("table id %q must match %s", table.ID, itemIDPattern)}
	}
	if err := validateTableDef(&table); err != nil {
		return editorValidationError{err}
	}
	return SaveTableDef(&table)
}

// DeleteEditorTable removes an authored table. A table that ships in the
// embedded catalog resurfaces once its overlay copy is gone.
func DeleteEditorTable(id string) (existed bool, err error) {
	return DeleteTableOverride(id)
}
