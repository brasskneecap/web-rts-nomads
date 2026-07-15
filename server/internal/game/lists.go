package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"webrts/server/pkg/protocol"
)

//go:embed catalog/lists
var listDefsFS embed.FS

// ListDef is a named set of item IDs — the catalog's single grouping primitive.
//
// A list is UNTYPED: it does not declare what it is for. Meaning is assigned by
// whatever consumes it, according to that consumer's capability:
//
//	Shop (item-purchase)     → items on the shelf, at ItemDef.CostGold
//	Recipe Shop (recipe-purchase) → recipes for sale, at Crafting.RecipeCostGold
//	Artificer (crafting)     → the craftable scope, ∩ what the player has learned,
//	                           charged at Crafting.CraftCostGold
//	Camp                     → a uniform drop pool
//
// So the same list can legitimately serve several roles at once. Consumers that
// only care about craftable items skip the members that are not craftable rather
// than erroring — see recipeShopPool / craftableSetForBuilding.
// A list takes exactly ONE of two forms:
//
//   - UNIFORM  — Items only. Every member is equally likely.
//   - WEIGHTED — MaxRoll + Entries. Each member owns a slice of a 1..MaxRoll
//     die, and its share of the die IS its likelihood.
//
// The weighted form is what loot "item subtables" used to be. Weights govern
// every sampling of the list, not just loot: a rare member is rare on a shop
// shelf for the same reason it is rare in a chest.
//
// Consumers that care only about MEMBERSHIP (a crafting building's scope, a
// Recipe Shop's pool, a marketplace's verbatim shelf) call ItemIDs() and never
// have to know which form they got.
type ListDef struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Items is the UNIFORM form.
	Items []string `json:"items,omitempty"`

	// MaxRoll + Entries are the WEIGHTED form. Entries tile 1..MaxRoll exactly:
	// no gaps, no overlaps. A weighted list, once rolled, ALWAYS yields an item —
	// whether anything drops AT ALL is a table's decision, not a pool's.
	MaxRoll int         `json:"maxRoll,omitempty"`
	Entries []ListEntry `json:"entries,omitempty"`
}

// ListEntry is one member of a weighted list: an item and the rolls it owns.
type ListEntry struct {
	Item string `json:"item"`
	Min  int    `json:"min"`
	Max  int    `json:"max"`
}

// IsWeighted reports whether this list rolls a die rather than picking evenly.
func (l *ListDef) IsWeighted() bool { return l != nil && len(l.Entries) > 0 }

// ItemIDs returns the list's members regardless of form, in authored order.
// This is the form-agnostic membership view: a uniform list and a weighted list
// holding the same items are indistinguishable through it, which is exactly what
// membership-only consumers want.
func (l *ListDef) ItemIDs() []string {
	if l == nil {
		return nil
	}
	if !l.IsWeighted() {
		return l.Items
	}
	ids := make([]string, 0, len(l.Entries))
	for _, e := range l.Entries {
		ids = append(ids, e.Item)
	}
	return ids
}

var listCatalogSingleton = loadListCatalog()

func loadListCatalog() map[string]*ListDef {
	catalog := make(map[string]*ListDef)
	err := fs.WalkDir(listDefsFS, "catalog/lists", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := listDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def ListDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		if err := validateListDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/lists: " + err.Error())
	}
	return catalog
}

// validateListDef enforces: exactly one form, at least one member, every member
// resolves to a real item, and — for a weighted list — that the entries tile the
// die.
//
// It deliberately does NOT check what the members are FOR: a list is untyped, and
// a member that is useless to one consumer is the point of another.
func validateListDef(def *ListDef) error {
	weighted := len(def.Entries) > 0
	uniform := len(def.Items) > 0

	// One notion of "how likely is this member", not two.
	if weighted && uniform {
		return fmt.Errorf(`list %q: declares both "items" (uniform) and "entries" (weighted) — a list has one form, not both`, def.ID)
	}
	if !weighted && !uniform {
		return fmt.Errorf("list %q: needs at least 1 item", def.ID)
	}

	if uniform {
		if def.MaxRoll != 0 {
			return fmt.Errorf(`list %q: a uniform list has no die — drop "maxRoll", or give it weighted "entries"`, def.ID)
		}
		for i, id := range def.Items {
			if _, ok := getItemDef(id); !ok {
				return fmt.Errorf("list %q: items[%d] %q is not a known item", def.ID, i, id)
			}
		}
		return nil
	}

	ranges := make([]rollRange, 0, len(def.Entries))
	for i, e := range def.Entries {
		if _, ok := getItemDef(e.Item); !ok {
			return fmt.Errorf("list %q: entries[%d] %q is not a known item", def.ID, i, e.Item)
		}
		ranges = append(ranges, rollRange{Min: e.Min, Max: e.Max, Label: e.Item})
	}
	return validateRollCoverage("list", def.ID, def.MaxRoll, ranges)
}

// rollListLocked yields one item from a list: by weight when the list is
// weighted, evenly when it is uniform. A list ALWAYS yields an item — whether
// anything drops at all is a table's decision.
//
// Draws from s.rngLoot, so a fixed seed reproduces the roll. Returns "" only for
// an empty list, which validation forbids. Must be called under s.mu write lock.
func (s *GameState) rollListLocked(list *ListDef) string {
	if list == nil {
		return ""
	}
	if !list.IsWeighted() {
		if len(list.Items) == 0 {
			return ""
		}
		return list.Items[s.rngLoot.Intn(len(list.Items))]
	}
	roll := s.rngLoot.Intn(list.MaxRoll) + 1
	for _, e := range list.Entries {
		if roll >= e.Min && roll <= e.Max {
			return e.Item
		}
	}
	return "" // unreachable: coverage is total (validateRollCoverage)
}

func getListDef(id string) (*ListDef, bool) {
	runtimeListsMu.RLock()
	if def, ok := runtimeLists[id]; ok {
		runtimeListsMu.RUnlock()
		return def, true
	}
	runtimeListsMu.RUnlock()
	def, ok := listCatalogSingleton[id]
	return def, ok
}

// ListListDefs returns all lists sorted by ID (for the HTTP route and
// deterministic iteration).
func ListListDefs() []*ListDef {
	merged := make(map[string]*ListDef, len(listCatalogSingleton))
	for id, def := range listCatalogSingleton {
		merged[id] = def
	}
	runtimeListsMu.RLock()
	for id, def := range runtimeLists {
		merged[id] = def
	}
	runtimeListsMu.RUnlock()
	defs := make([]*ListDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// listMetadataKey is the ONE building-metadata key that binds a list. There are
// no aliases: the superseded keys "itemList" and "recipeList" are rejected at
// map load (see validateBuildingListMetadata) rather than silently ignored,
// because quietly dropping one would erase that shop's stock configuration with
// no signal.
const listMetadataKey = "list"

// legacyListMetadataKeys are the pre-unification spellings of listMetadataKey.
// They are a load-time ERROR, never a fallback.
var legacyListMetadataKeys = []string{"itemList", "recipeList"}

// listForBuilding resolves the list bound to a building, if any. ok is false
// when the building binds no list, or names one that does not resolve.
func listForBuilding(b *protocol.BuildingTile) (*ListDef, bool) {
	id, ok := getMetadataString(b.Metadata, listMetadataKey)
	if !ok || id == "" {
		return nil, false
	}
	return getListDef(id)
}

// validateBuildingListKeys rejects a map whose building still carries a
// superseded list key. It PANICS, matching validateZones and the rest of the map
// loader's fail-fast contract (the overlay loader recovers panics into a logged
// skip, so a stale user map is a loud warning, not a crash).
//
// Erroring rather than ignoring the key is the whole point: a silently-dropped
// binding would leave that shop quietly falling back to its building-type
// default, and nobody would notice until a playtest.
func validateBuildingListKeys(mapName string, buildings []protocol.BuildingTile) {
	for i := range buildings {
		b := &buildings[i]
		for _, key := range legacyListMetadataKeys {
			if v, ok := getMetadataString(b.Metadata, key); ok && v != "" {
				panic(fmt.Sprintf(
					"%s: building %q uses metadata key %q, which was replaced by %q — rename the key (its value %q is still a valid list id)",
					mapName, b.ID, key, listMetadataKey, v))
			}
		}
	}
}
