package game

import (
	"embed"
	"encoding/json"
	"sort"
	"strconv"
)

// Embeds the loot-table catalog. Single file because there is exactly one
// schema and a single file keeps the diff surface tight when designers
// tune drop rates. New tables are added by appending JSON keys.
//
// Schema rules (panic at load on violation):
//   - packagedItems[id].kind ∈ {"resource_bundle","item_subtable"}
//   - resource_bundle.resources keys ∈ {"gold","wood"} (whitelist),
//     amounts > 0
//   - item_subtable.entries[*].item must resolve in the item catalog;
//     min >= 1, max >= min, ranges may not overlap
//   - tables[*][*] entries: entry must resolve in packagedItems;
//     min >= 1, max <= 100, max >= min, ranges may not overlap

//go:embed catalog/neutral_groups/loot_tables.json
var lootTablesFS embed.FS

// PackagedItemKind distinguishes the two flavors of packaged-item entries.
type PackagedItemKind string

const (
	PackagedItemResourceBundle PackagedItemKind = "resource_bundle"
	PackagedItemSubtable       PackagedItemKind = "item_subtable"
)

// LootTableEntry is one range-keyed row in a top-level loot table.
// Roll a d100; the entry whose [Min,Max] contains the result is selected.
type LootTableEntry struct {
	Entry string `json:"entry"`
	Min   int    `json:"min"`
	Max   int    `json:"max"`
}

// LootSubtableEntry is one range-keyed row inside an item_subtable packaged item.
type LootSubtableEntry struct {
	Item string `json:"item"`
	Min  int    `json:"min"`
	Max  int    `json:"max"`
}

// PackagedItem is one packagedItems entry. Only the fields matching Kind are
// populated; the other slot is its zero value.
type PackagedItem struct {
	Kind      PackagedItemKind
	Resources map[string]int      // resource_bundle only
	Entries   []LootSubtableEntry // item_subtable only
}

// LootTableDef is a fully resolved top-level loot table (slice of entries).
type LootTableDef = []LootTableEntry

// rawPackagedItem mirrors the JSON shape before validation.
type rawPackagedItem struct {
	Kind      PackagedItemKind    `json:"kind"`
	Resources map[string]int      `json:"resources"`
	Entries   []LootSubtableEntry `json:"entries"`
}

// rawLootCatalog mirrors the top-level loot_tables.json shape.
type rawLootCatalog struct {
	PackagedItems map[string]rawPackagedItem  `json:"packagedItems"`
	Tables        map[string][]LootTableEntry `json:"tables"`
}

// validLootResourceKeys is a whitelist of accepted resource keys. New keys
// require a code change so designers cannot typo-grant phantom resources.
var validLootResourceKeys = map[string]struct{}{
	"gold": {},
	"wood": {},
}

// packagedItemsByID and lootTablesByID are the runtime registries.
// Declared as var initializers (not init()) so Go's dependency-graph ordering
// ensures itemCatalogSingleton (also a var initializer in items.go) is ready
// before the cross-validation in loadPackagedItems runs. neutralGroupsByTier
// (neutral_group_defs.go) depends on lootTablesByID via getLootTable, so it
// initializes after these two.
var packagedItemsByID = loadPackagedItems()
var lootTablesByID = loadLootTables()

func mustReadRawLootCatalog() rawLootCatalog {
	const rel = "catalog/neutral_groups/loot_tables.json"
	data, err := lootTablesFS.ReadFile(rel)
	if err != nil {
		// Embedded file is always present (go:embed would have failed at compile
		// time if it were missing). Return empty only if somehow not found at
		// runtime; callers treat missing entries as "no drop."
		return rawLootCatalog{}
	}
	var raw rawLootCatalog
	if err := json.Unmarshal(data, &raw); err != nil {
		panic(rel + ": " + err.Error())
	}
	return raw
}

func loadPackagedItems() map[string]PackagedItem {
	const rel = "catalog/neutral_groups/loot_tables.json"
	raw := mustReadRawLootCatalog()
	out := make(map[string]PackagedItem, len(raw.PackagedItems))
	for id, pkg := range raw.PackagedItems {
		if id == "" {
			panic(rel + ": packaged item with empty id")
		}
		switch pkg.Kind {
		case PackagedItemResourceBundle:
			if len(pkg.Resources) == 0 {
				panic(rel + ": " + id + ": resource_bundle has no resources")
			}
			for k, v := range pkg.Resources {
				if _, ok := validLootResourceKeys[k]; !ok {
					panic(rel + ": " + id + ": resource key " + k + " not in whitelist")
				}
				if v <= 0 {
					panic(rel + ": " + id + "." + k + " = " + strconv.Itoa(v) + " (must be > 0)")
				}
			}
			out[id] = PackagedItem{Kind: PackagedItemResourceBundle, Resources: pkg.Resources}

		case PackagedItemSubtable:
			if len(pkg.Entries) == 0 {
				panic(rel + ": " + id + ": item_subtable has no entries")
			}
			validateSubtableRanges(rel, id, pkg.Entries)
			// Cross-validate item ids against the item catalog. getItemDef reads
			// itemCatalogSingleton, which is a var initializer in items.go. Go's
			// dependency graph guarantees itemCatalogSingleton is ready before
			// this function runs because loadPackagedItems transitively depends on
			// it.
			for i, e := range pkg.Entries {
				if _, ok := getItemDef(e.Item); !ok {
					panic(rel + ": " + id + " sub-table entry " + strconv.Itoa(i) +
						` references unknown item "` + e.Item + `"`)
				}
			}
			out[id] = PackagedItem{Kind: PackagedItemSubtable, Entries: pkg.Entries}

		case "":
			panic(rel + ": " + id + ": missing kind")

		default:
			panic(rel + ": " + id + ": unknown kind " + string(pkg.Kind))
		}
	}
	return out
}

func loadLootTables() map[string]LootTableDef {
	const rel = "catalog/neutral_groups/loot_tables.json"
	raw := mustReadRawLootCatalog()
	out := make(map[string]LootTableDef, len(raw.Tables))
	for tableID, entries := range raw.Tables {
		if tableID == "" {
			panic(rel + ": table with empty id")
		}
		if len(entries) == 0 {
			panic(rel + ": table " + tableID + " has no entries")
		}
		for _, e := range entries {
			if e.Min < 1 || e.Max < e.Min || e.Max > 100 {
				panic(rel + ": table " + tableID + " entry " + e.Entry +
					": invalid range [" + strconv.Itoa(e.Min) + "," + strconv.Itoa(e.Max) + "]")
			}
			if _, ok := packagedItemsByID[e.Entry]; !ok {
				panic(rel + ": table " + tableID + " references unknown packaged item " + e.Entry)
			}
		}
		validateTableRanges(rel, tableID, entries)
		out[tableID] = entries
	}
	return out
}

func validateSubtableRanges(rel, id string, entries []LootSubtableEntry) {
	sorted := append([]LootSubtableEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
	for i, e := range sorted {
		if e.Min < 1 || e.Max < e.Min {
			panic(rel + ": " + id + ": invalid sub-table range [" +
				strconv.Itoa(e.Min) + "," + strconv.Itoa(e.Max) + "]")
		}
		if i > 0 && sorted[i].Min <= sorted[i-1].Max {
			panic(rel + ": " + id + ": overlapping sub-table ranges around " +
				strconv.Itoa(e.Min))
		}
	}
}

func validateTableRanges(rel, tableID string, entries []LootTableEntry) {
	sorted := append([]LootTableEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Min <= sorted[i-1].Max {
			panic(rel + ": " + tableID + ": overlapping table ranges around " +
				strconv.Itoa(sorted[i].Min))
		}
	}
}

// getLootTable returns the named top-level loot table, or (nil, false) on
// miss. Overlay-aware: checks the writable loot-table overlay (see
// loot_table_persistence.go) before falling back to the embedded catalog.
//
// DELIBERATE LIVE-NESS: unlike the per-match item-catalog snapshot
// (newMatchItemCatalog), loot tables are read live under s.mu. Shop stocking
// samples them once at match start; only mid-match camp-drop rolls could
// observe an editor save landing mid-match — accepted for a single-operator
// dev tool (matches the map editor's live-registration semantics). If loot
// isolation is ever needed, snapshot the effective catalog per match like
// items do.
func getLootTable(id string) (LootTableDef, bool) {
	runtimeLootCatalogMu.RLock()
	if runtimeLootTables != nil {
		if t, ok := runtimeLootTables[id]; ok {
			runtimeLootCatalogMu.RUnlock()
			return t, true
		}
	}
	runtimeLootCatalogMu.RUnlock()
	t, ok := lootTablesByID[id]
	return t, ok
}

// getPackagedItem returns the named packaged item, or (zero, false) on miss.
// Overlay-aware: checks the writable loot-table overlay before falling back
// to the embedded catalog.
func getPackagedItem(id string) (PackagedItem, bool) {
	runtimeLootCatalogMu.RLock()
	if runtimePackagedItems != nil {
		if p, ok := runtimePackagedItems[id]; ok {
			runtimeLootCatalogMu.RUnlock()
			return p, true
		}
	}
	runtimeLootCatalogMu.RUnlock()
	p, ok := packagedItemsByID[id]
	return p, ok
}
