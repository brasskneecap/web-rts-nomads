package game

import (
	"fmt"
	"sort"
	"strings"
)

// ─── Referential-integrity scan: "what still points at this id?" ────────────
//
// A catalog delete must never leave a dangling reference — that is exactly
// the class of state that panics init()'s boot-time cross-validation (see
// path_editor.go's DeleteEditorPath, faction_editor.go's DeleteEditorFaction,
// which guard the same way for paths and factions). This file is the item
// and list equivalent: a read-only scan over every storage surface that can
// name an item or list id, used to BLOCK a delete rather than cascade it.
//
// The scan never mutates anything and takes no game-state lock — it runs
// from the editor HTTP path, outside the tick loop, and reads exclusively
// through the same accessors the rest of the package already uses
// (ListListDefs, ListItemDefs, ListTableDefs, currentMapCatalogSnapshot,
// etc.), each of which does its own locking where the underlying store is
// mutable.

// Reference names one place in the catalog that points at an item or list
// id. Kind is a short, sortable category label ("list", "recipe",
// "upgrade", "map", "table", "neutral group"). ID is the referencing
// entity's own id, used only to make the sort deterministic. Detail is the
// ready-to-print human description of the site, e.g. `list "Fire Loot"
// (fire_loot)` or `map "forest-1" building "merchant_1"`.
type Reference struct {
	Kind   string
	ID     string
	Detail string
}

// sortReferences orders references by (Kind, ID, Detail) so a rejection
// message is reproducible across calls, independent of Go's randomized map
// iteration order — required for the message to be test-assertable and for
// two runs against the same catalog state to always report referrers in the
// same order.
func sortReferences(refs []Reference) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		if refs[i].ID != refs[j].ID {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Detail < refs[j].Detail
	})
}

// formatReferences renders an already-sorted reference slice into one
// comma-joined clause naming every site, for embedding in an
// editorValidationError message.
func formatReferences(refs []Reference) string {
	details := make([]string, len(refs))
	for i, r := range refs {
		details[i] = r.Detail
	}
	return strings.Join(details, ", ")
}

// itemReferences scans every catalog surface that can name an item id and
// returns every site still referencing it, sorted for a deterministic
// message. Scanned surfaces:
//
//  1. Lists — both the uniform (Items) and weighted (Entries[].Item) forms,
//     via ListDef.ItemIDs().
//  2. Other items' crafting inputs. The item's OWN recipe is never a hit:
//     an item can never legally list itself as an input in the first place
//     (validateItemDef forbids self-reference), so no explicit skip beyond
//     "id == item.ID" is needed for that case, but the id==item.ID guard
//     is kept anyway as the cheap, obviously-correct exclusion.
//  3. Upgrade defs whose equipment effect grants this item.
//  4. Maps — every building's shopFixedInventory and every placed unit's
//     starting items.
func itemReferences(id string) []Reference {
	var refs []Reference

	for _, list := range ListListDefs() {
		if containsString(list.ItemIDs(), id) {
			refs = append(refs, Reference{
				Kind:   "list",
				ID:     list.ID,
				Detail: fmt.Sprintf("list %q (%s)", list.Name, list.ID),
			})
		}
	}

	for _, item := range ListItemDefs() {
		if item.ID == id || item.Crafting == nil {
			continue
		}
		if containsString(item.Crafting.Inputs, id) {
			refs = append(refs, Reference{
				Kind:   "recipe",
				ID:     item.ID,
				Detail: fmt.Sprintf("recipe for %q (%s)", item.DisplayName, item.ID),
			})
		}
	}

	for _, up := range listUpgradeDefs() {
		if up.Effect.ItemID == id {
			refs = append(refs, Reference{
				Kind:   "upgrade",
				ID:     up.ID,
				Detail: fmt.Sprintf("upgrade %q (%s)", up.Name, up.ID),
			})
		}
	}

	for _, m := range currentMapCatalogSnapshot() {
		for _, b := range m.Map.Buildings {
			if containsString(b.ShopFixedInventory, id) {
				refs = append(refs, Reference{
					Kind:   "map",
					ID:     m.ID,
					Detail: fmt.Sprintf("map %q building %q shopFixedInventory", m.ID, b.ID),
				})
			}
		}
		for _, u := range m.Map.PlacedUnits {
			if containsString(u.Items, id) {
				refs = append(refs, Reference{
					Kind:   "map",
					ID:     m.ID,
					Detail: fmt.Sprintf("map %q placed unit %q", m.ID, u.ID),
				})
			}
		}
	}

	sortReferences(refs)
	return refs
}

// listReferences scans every catalog surface that can name a list id and
// returns every site still referencing it, sorted for a deterministic
// message. Scanned surfaces:
//
//  1. Tables — TableRow.List.
//  2. Maps — the building-metadata "list" key (listMetadataKey), which
//     binds a shop/camp building to a list. The superseded "itemList" /
//     "recipeList" spellings are rejected at map load time (see
//     validateBuildingListKeys) and can never appear in a loaded map, so
//     only the canonical key needs checking here.
//  3. Neutral groups — NeutralGroup.LootList.
func listReferences(id string) []Reference {
	var refs []Reference

	for _, table := range ListTableDefs() {
		for _, row := range table.Rows {
			if row.List == id {
				refs = append(refs, Reference{
					Kind:   "table",
					ID:     table.ID,
					Detail: fmt.Sprintf("table %q (%s)", table.Name, table.ID),
				})
				break
			}
		}
	}

	for _, m := range currentMapCatalogSnapshot() {
		for _, b := range m.Map.Buildings {
			if v, ok := getMetadataString(b.Metadata, listMetadataKey); ok && v == id {
				refs = append(refs, Reference{
					Kind:   "map",
					ID:     m.ID,
					Detail: fmt.Sprintf("map %q building %q", m.ID, b.ID),
				})
			}
		}
	}

	for _, tier := range neutralTiersSorted {
		for _, g := range neutralGroupsByTier[tier].Groups {
			if g.LootList == id {
				refs = append(refs, Reference{
					Kind:   "neutral group",
					ID:     g.ID,
					Detail: fmt.Sprintf("neutral group %q tier %d (%s)", g.Name, tier, g.ID),
				})
			}
		}
	}

	sortReferences(refs)
	return refs
}
