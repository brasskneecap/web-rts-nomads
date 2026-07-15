package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed catalog/tables
var tableDefsFS embed.FS

// TableDef is a weighted roll over lists, resource grants, and no-drop outcomes.
// It is what a camp rolls when it is cleared, and what a shop rolls to stock its
// shelves.
//
// A table is a die (MaxRoll) and rows that TILE it — every roll from 1 to MaxRoll
// lands on exactly one row. That totality is the point: "nothing happens" is a
// row you can see and read a percentage off, not a hole in the ranges.
type TableDef struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	MaxRoll int        `json:"maxRoll"`
	Rows    []TableRow `json:"rows"`
}

// TableRow owns a slice of the die and exactly ONE outcome.
//
// Resource grants are inline rather than a named entity: the only thing a named
// "resource bundle" ever bought was letting two tables share a {gold, wood} pair,
// which is not worth a catalog of its own.
type TableRow struct {
	Min int `json:"min"`
	Max int `json:"max"`

	// Exactly one of the three:
	List      string         `json:"list,omitempty"`      // roll this list, grant what it yields
	Resources map[string]int `json:"resources,omitempty"` // grant these resources
	Nothing   bool           `json:"nothing,omitempty"`   // grant nothing
}

// outcomeCount reports how many outcomes this row declares. Exactly 1 is legal.
func (r *TableRow) outcomeCount() int {
	n := 0
	if r.List != "" {
		n++
	}
	if len(r.Resources) > 0 {
		n++
	}
	if r.Nothing {
		n++
	}
	return n
}

// label names the row's outcome, for coverage error messages.
func (r *TableRow) label() string {
	switch {
	case r.List != "":
		return "list " + r.List
	case len(r.Resources) > 0:
		return "resources"
	case r.Nothing:
		return "nothing"
	}
	return "(no outcome)"
}

var tableCatalogSingleton = loadTableCatalog()

func loadTableCatalog() map[string]*TableDef {
	catalog := make(map[string]*TableDef)
	err := fs.WalkDir(tableDefsFS, "catalog/tables", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := tableDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def TableDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		if err := validateTableDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/tables: " + err.Error())
	}
	return catalog
}

// validLootResourceKeys is what a table row may grant. A typo'd resource would
// otherwise be a silently-ignored grant.
var validLootResourceKeys = map[string]bool{"gold": true, "wood": true}

// validateTableDef enforces: every row has exactly one outcome, named lists
// resolve, resources are real and positive, and the rows tile 1..MaxRoll.
func validateTableDef(def *TableDef) error {
	if len(def.Rows) == 0 {
		return fmt.Errorf("table %q: needs at least 1 row", def.ID)
	}
	ranges := make([]rollRange, 0, len(def.Rows))
	for i := range def.Rows {
		r := &def.Rows[i]
		switch n := r.outcomeCount(); {
		case n == 0:
			return fmt.Errorf(`table %q: rows[%d] has no outcome — it must name a "list", grant "resources", or set "nothing"`, def.ID, i)
		case n > 1:
			return fmt.Errorf(`table %q: rows[%d] declares %d outcomes — a row does exactly one thing`, def.ID, i, n)
		}
		if r.List != "" {
			if _, ok := getListDef(r.List); !ok {
				return fmt.Errorf("table %q: rows[%d] references unknown list %q", def.ID, i, r.List)
			}
		}
		for k, v := range r.Resources {
			if !validLootResourceKeys[k] {
				return fmt.Errorf("table %q: rows[%d] grants unknown resource %q", def.ID, i, k)
			}
			if v <= 0 {
				return fmt.Errorf("table %q: rows[%d] grants %d %s — a grant must be positive", def.ID, i, v, k)
			}
		}
		ranges = append(ranges, rollRange{Min: r.Min, Max: r.Max, Label: r.label()})
	}
	return validateRollCoverage("table", def.ID, def.MaxRoll, ranges)
}

func getTableDef(id string) (*TableDef, bool) {
	runtimeTablesMu.RLock()
	if def, ok := runtimeTables[id]; ok {
		runtimeTablesMu.RUnlock()
		return def, true
	}
	runtimeTablesMu.RUnlock()
	def, ok := tableCatalogSingleton[id]
	return def, ok
}

// ListTableDefs returns all tables sorted by ID (HTTP route + deterministic
// iteration).
func ListTableDefs() []*TableDef {
	merged := make(map[string]*TableDef, len(tableCatalogSingleton))
	for id, def := range tableCatalogSingleton {
		merged[id] = def
	}
	runtimeTablesMu.RLock()
	for id, def := range runtimeTables {
		merged[id] = def
	}
	runtimeTablesMu.RUnlock()
	defs := make([]*TableDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// ReachableItemIDs returns every item this table can ever produce — the union of
// the members of every list it rolls. Resource and `nothing` rows contribute
// nothing.
//
// This is the "can a painted shop actually stock this item" question, and it is
// asked by several tests and by nothing at runtime.
func (t *TableDef) ReachableItemIDs() map[string]struct{} {
	out := make(map[string]struct{})
	if t == nil {
		return out
	}
	for i := range t.Rows {
		r := &t.Rows[i]
		if r.List == "" {
			continue
		}
		list, ok := getListDef(r.List)
		if !ok {
			continue
		}
		for _, id := range list.ItemIDs() {
			out[id] = struct{}{}
		}
	}
	return out
}

// TableRollResult is what one roll of a table produced. Both halves may be
// empty — that is the `nothing` outcome, and it is a legitimate result, not a
// failure.
type TableRollResult struct {
	Items     []string
	Resources map[string]int
}

// Empty reports whether the roll produced nothing at all.
func (r TableRollResult) Empty() bool { return len(r.Items) == 0 && len(r.Resources) == 0 }

// rollTableLocked rolls a table once and resolves the row it lands on: a list is
// rolled for an item, resources are granted verbatim, and `nothing` yields an
// empty result.
//
// Draws from s.rngLoot (both the table roll and any list roll it triggers), so a
// fixed seed reproduces the outcome exactly. Must be called under s.mu write lock.
func (s *GameState) rollTableLocked(table *TableDef) TableRollResult {
	if table == nil || len(table.Rows) == 0 {
		return TableRollResult{}
	}
	roll := s.rngLoot.Intn(table.MaxRoll) + 1
	for i := range table.Rows {
		r := &table.Rows[i]
		if roll < r.Min || roll > r.Max {
			continue
		}
		switch {
		case r.Nothing:
			return TableRollResult{}
		case len(r.Resources) > 0:
			// Defensive copy: the caller mutates the grant map, and the catalog's
			// copy is shared across every roll of this table.
			res := make(map[string]int, len(r.Resources))
			for k, v := range r.Resources {
				res[k] = v
			}
			return TableRollResult{Resources: res}
		case r.List != "":
			list, ok := getListDef(r.List)
			if !ok {
				return TableRollResult{} // validated at load; defensive
			}
			if item := s.rollListLocked(list); item != "" {
				return TableRollResult{Items: []string{item}}
			}
			return TableRollResult{}
		}
	}
	return TableRollResult{} // unreachable: coverage is total
}
