package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
)

// ErrLastMerchantItem is returned by SetMerchantItemAvailability when a
// removal would leave its subtable empty. Callers that perform best-effort
// cleanup sweeps (DeleteEditorItem) match on this with errors.Is to treat it
// as non-fatal.
var ErrLastMerchantItem = errors.New("cannot remove: last item in merchant subtable")

// ─── Writable loot-table overlay (single-file catalog) ──────────────────────
//
// The neutral-groups loot catalog is one JSON file (packagedItems + tables).
// The editor edits merchant subtable membership by WIDTH (d100 range size),
// renormalizing each edited subtable to sum exactly 100. The whole file is
// rewritten on every change and overlaid at startup. Mirrors the item/recipe
// overlay conventions (item_persistence.go, recipe_persistence.go) but is
// whole-file rather than one-def-per-file because the catalog is one file.

var (
	runtimeLootCatalogMu sync.RWMutex
	runtimeLootCatalog   *rawLootCatalog
	runtimePackagedItems map[string]PackagedItem
	runtimeLootTables    map[string]LootTableDef
)

// resolveNeutralGroupsDir mirrors resolveItemsDir/resolveRecipesDir: env
// override, else the dev source catalog dir so editor saves land as ordinary
// git-visible changes.
func resolveNeutralGroupsDir() (string, error) {
	if dir := os.Getenv("NEUTRAL_GROUPS_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "neutral_groups")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("neutral_groups directory not found at %s; set NEUTRAL_GROUPS_DIR env var to override", dir)
}

// merchantSubtableForCategory picks the merchant subtable an item's category
// stocks into. Shields sell beside armor; unknown categories default to
// accessories (the most generic bucket).
func merchantSubtableForCategory(category string) string {
	switch category {
	case "Weapon":
		return "merchant_weapons"
	case "Armor", "Shield":
		return "merchant_armor"
	case "Consumable":
		return "merchant_potions"
	case "Accessory":
		return "merchant_accessories"
	default:
		return "merchant_accessories"
	}
}

// currentRawLootCatalogCopy returns a deep copy of the effective raw catalog
// (overlay if present, else the parsed embed). The copy is private to the
// caller, so mutating it never touches the embed or the live overlay.
func currentRawLootCatalogCopy() (*rawLootCatalog, error) {
	runtimeLootCatalogMu.RLock()
	src := runtimeLootCatalog
	runtimeLootCatalogMu.RUnlock()
	if src == nil {
		raw := mustReadRawLootCatalog()
		return deepCopyRawLootCatalog(&raw)
	}
	return deepCopyRawLootCatalog(src)
}

// deepCopyRawLootCatalog clones via JSON round-trip (small data,
// editor-frequency calls; simplicity wins over a hand-rolled deep copy).
func deepCopyRawLootCatalog(src *rawLootCatalog) (*rawLootCatalog, error) {
	blob, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var cp rawLootCatalog
	if err := json.Unmarshal(blob, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// renormalizeSubtable reassigns contiguous 1..100 ranges from the given
// widths, scaling proportionally with largest-remainder rounding. Entries
// keep their slice order (deterministic — no map iteration involved).
func renormalizeSubtable(entries []LootSubtableEntry, widths []int) []LootSubtableEntry {
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= 0 || len(entries) == 0 || len(entries) >= 100 {
		// >=100 members cannot each hold a >=1-wide d100 slot; refuse to
		// renormalize rather than emit zero-width rows (unreachable for the
		// small merchant subtables, guarded for safety).
		return entries
	}
	scaled := make([]int, len(widths))
	remainders := make([]float64, len(widths))
	sum := 0
	for i, w := range widths {
		exact := float64(w) * 100.0 / float64(total)
		scaled[i] = int(exact)
		if scaled[i] < 1 {
			scaled[i] = 1 // every member keeps at least 1% — never silently vanishes
		}
		remainders[i] = exact - float64(int(exact))
		sum += scaled[i]
	}
	// Distribute the remainder (may be negative if the min-1 clamps overshot).
	for sum != 100 {
		bestIdx, best := 0, -1.0
		for i, r := range remainders {
			if sum < 100 && r > best {
				best, bestIdx = r, i
			}
			if sum > 100 && scaled[i] > 1 && (best < 0 || r < best) {
				best, bestIdx = r, i
			}
		}
		if sum < 100 {
			scaled[bestIdx]++
			remainders[bestIdx] = 0
			sum++
		} else {
			scaled[bestIdx]--
			remainders[bestIdx] = 1
			sum--
		}
	}
	cursor := 1
	out := make([]LootSubtableEntry, len(entries))
	for i, e := range entries {
		out[i] = LootSubtableEntry{Item: e.Item, Min: cursor, Max: cursor + scaled[i] - 1}
		cursor += scaled[i]
	}
	return out
}

// SetMerchantItemAvailability adds/removes itemID (with the given d100 width
// as its weight; ≤0 defaults to 10) in the merchant subtable matching
// category, renormalizes that subtable, persists the whole loot catalog, and
// swaps the runtime overlay. Idempotent in both directions.
func SetMerchantItemAvailability(itemID, category string, enabled bool, weight int) error {
	if weight <= 0 {
		weight = 10
	}
	subtableID := merchantSubtableForCategory(category)
	cat, err := currentRawLootCatalogCopy()
	if err != nil {
		return err
	}
	sub, ok := cat.PackagedItems[subtableID]
	if !ok || sub.Kind != PackagedItemSubtable {
		return fmt.Errorf("merchant subtable %q not found", subtableID)
	}
	idx := -1
	for i, e := range sub.Entries {
		if e.Item == itemID {
			idx = i
			break
		}
	}
	if !enabled && idx >= 0 && len(sub.Entries) == 1 {
		return fmt.Errorf("cannot remove %q: it is the last item in %s (merchant subtables cannot be empty): %w", itemID, subtableID, ErrLastMerchantItem)
	}
	if enabled == (idx >= 0) {
		// Already in desired membership state. On enable, still allow weight
		// updates: fall through only when the width differs.
		if !enabled {
			return nil
		}
		if cur := sub.Entries[idx].Max - sub.Entries[idx].Min + 1; cur == weight {
			return nil
		}
	}
	widths := make([]int, 0, len(sub.Entries)+1)
	entries := make([]LootSubtableEntry, 0, len(sub.Entries)+1)
	for i, e := range sub.Entries {
		if i == idx && !enabled {
			continue // dropping this row
		}
		w := e.Max - e.Min + 1
		if i == idx && enabled {
			w = weight // weight update in place
		}
		entries = append(entries, e)
		widths = append(widths, w)
	}
	if enabled && idx < 0 {
		entries = append(entries, LootSubtableEntry{Item: itemID})
		widths = append(widths, weight)
	}
	sub.Entries = renormalizeSubtable(entries, widths)
	cat.PackagedItems[subtableID] = sub
	return persistAndSwapLootCatalog(cat)
}

// persistAndSwapLootCatalog validates, writes the whole catalog file, and
// rebuilds the derived overlay maps. Validation runs BEFORE the write so a
// bug in the mutation path never persists a broken catalog to disk.
func persistAndSwapLootCatalog(cat *rawLootCatalog) error {
	if err := validateRawLootCatalog(cat); err != nil {
		return fmt.Errorf("refusing to persist invalid loot catalog: %w", err)
	}
	dir, err := resolveNeutralGroupsDir()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "loot_tables.json"), raw, 0o644); err != nil {
		return err
	}
	return swapLootOverlayFromRaw(cat)
}

// swapLootOverlayFromRaw validates cat with the same rules the embed loader
// enforces (loadPackagedItems/loadLootTables), derives the lookup maps, and
// installs everything under one lock. Rejects invalid catalogs instead of
// installing a partial/broken overlay.
func swapLootOverlayFromRaw(cat *rawLootCatalog) error {
	if err := validateRawLootCatalog(cat); err != nil {
		return err
	}
	packaged := make(map[string]PackagedItem, len(cat.PackagedItems))
	for id, rawPI := range cat.PackagedItems {
		packaged[id] = PackagedItem{Kind: rawPI.Kind, Resources: rawPI.Resources, Entries: rawPI.Entries}
	}
	tables := make(map[string]LootTableDef, len(cat.Tables))
	for id, entries := range cat.Tables {
		tables[id] = entries
	}
	runtimeLootCatalogMu.Lock()
	runtimeLootCatalog = cat
	runtimePackagedItems = packaged
	runtimeLootTables = tables
	runtimeLootCatalogMu.Unlock()
	return nil
}

// validateRawLootCatalog mirrors the panic-on-violation schema rules the
// embed loader (loadPackagedItems/loadLootTables in loot_table_defs.go)
// enforces, but returns an error instead of panicking — this catalog may
// come from a hand-edited or corrupted file on disk, not the trusted embed.
func validateRawLootCatalog(cat *rawLootCatalog) error {
	for id, pkg := range cat.PackagedItems {
		if id == "" {
			return fmt.Errorf("packaged item with empty id")
		}
		switch pkg.Kind {
		case PackagedItemResourceBundle:
			if len(pkg.Resources) == 0 {
				return fmt.Errorf("%s: resource_bundle has no resources", id)
			}
			for k, v := range pkg.Resources {
				if _, ok := validLootResourceKeys[k]; !ok {
					return fmt.Errorf("%s: resource key %s not in whitelist", id, k)
				}
				if v <= 0 {
					return fmt.Errorf("%s.%s = %s (must be > 0)", id, k, strconv.Itoa(v))
				}
			}
		case PackagedItemSubtable:
			if len(pkg.Entries) == 0 {
				return fmt.Errorf("%s: item_subtable has no entries", id)
			}
			if err := validateSubtableRangesErr(id, pkg.Entries); err != nil {
				return err
			}
			for i, e := range pkg.Entries {
				if _, ok := getItemDef(e.Item); !ok {
					return fmt.Errorf("%s sub-table entry %d references unknown item %q", id, i, e.Item)
				}
			}
		case "":
			return fmt.Errorf("%s: missing kind", id)
		default:
			return fmt.Errorf("%s: unknown kind %s", id, pkg.Kind)
		}
	}
	for tableID, entries := range cat.Tables {
		if tableID == "" {
			return fmt.Errorf("table with empty id")
		}
		if len(entries) == 0 {
			return fmt.Errorf("table %s has no entries", tableID)
		}
		for _, e := range entries {
			if e.Min < 1 || e.Max < e.Min || e.Max > 100 {
				return fmt.Errorf("table %s entry %s: invalid range [%d,%d]", tableID, e.Entry, e.Min, e.Max)
			}
			if _, ok := cat.PackagedItems[e.Entry]; !ok {
				return fmt.Errorf("table %s references unknown packaged item %s", tableID, e.Entry)
			}
		}
		if err := validateTableRangesErr(tableID, entries); err != nil {
			return err
		}
	}
	return nil
}

// validateSubtableRangesErr mirrors validateSubtableRanges (loot_table_defs.go)
// as a non-panicking check for persisted/on-disk data.
func validateSubtableRangesErr(id string, entries []LootSubtableEntry) error {
	sorted := append([]LootSubtableEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
	for i, e := range sorted {
		if e.Min < 1 || e.Max < e.Min {
			return fmt.Errorf("%s: invalid sub-table range [%d,%d]", id, e.Min, e.Max)
		}
		if i > 0 && sorted[i].Min <= sorted[i-1].Max {
			return fmt.Errorf("%s: overlapping sub-table ranges around %d", id, e.Min)
		}
	}
	return nil
}

// validateTableRangesErr mirrors validateTableRanges (loot_table_defs.go) as
// a non-panicking check for persisted/on-disk data.
func validateTableRangesErr(tableID string, entries []LootTableEntry) error {
	sorted := append([]LootTableEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Min <= sorted[i-1].Max {
			return fmt.Errorf("%s: overlapping table ranges around %d", tableID, sorted[i].Min)
		}
	}
	return nil
}

// LoadPersistedLootTablesIntoOverlay — startup hook, best-effort. Mirrors
// LoadPersistedItemsIntoOverlay/LoadPersistedRecipesIntoOverlay.
func LoadPersistedLootTablesIntoOverlay() {
	dir, err := resolveNeutralGroupsDir()
	if err != nil {
		slog.Info("persisted loot tables: no writable dir; using embedded catalog only", "err", err)
		return
	}
	path := filepath.Join(dir, "loot_tables.json")
	if loadPersistedLootTablesFromFile(path) {
		slog.Info("persisted loot tables: overlaid on embedded catalog", "file", path)
	}
}

// loadPersistedLootTablesFromFile reads and validates path, installing it as
// the runtime overlay on success. Returns false (and leaves the current
// overlay untouched) on any read/parse/validation failure.
func loadPersistedLootTablesFromFile(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cat rawLootCatalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		slog.Warn("persisted loot tables: skipped invalid file", "file", path, "err", err)
		return false
	}
	if err := swapLootOverlayFromRaw(&cat); err != nil {
		slog.Warn("persisted loot tables: skipped invalid catalog", "file", path, "err", err)
		return false
	}
	return true
}
