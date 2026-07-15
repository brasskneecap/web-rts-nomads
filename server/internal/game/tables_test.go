package game

import (
	"fmt"
	"strings"
	"testing"
)

// ─── Migration fidelity ─────────────────────────────────────────────────────

// TestTableMigration_OddsAreUnchanged is the guard on the one thing this change
// could quietly break: the loot odds.
//
// The expectations below are the PRE-MIGRATION ground truth, read off the old
// loot_tables.json by hand — the d100 face → outcome mapping that raider_loot,
// wildborne_loot and merchant_basic had before they became TableDefs. Rolling
// every face of each migrated table and comparing, face by face, proves the
// migration moved the data without moving the balance.
//
// It matters most for raider_loot's 51-60, which was an implicit GAP (a
// deliberate 10% no-drop). Absorbing those ten faces into a neighbouring range
// would have silently turned a 10% chance of nothing into 10% more loot, and
// nothing else in the suite would have noticed.
func TestTableMigration_OddsAreUnchanged(t *testing.T) {
	// outcome is what a face yields, in a form comparable across the migration:
	// "nothing", "gold=N,wood=M", or "list:<id>".
	type face struct {
		from, to int
		outcome  string
	}
	cases := []struct {
		table  string
		faces  []face
		maxRoll int
	}{
		{
			table:   "raider_loot",
			maxRoll: 100,
			faces: []face{
				{1, 50, "gold=50,wood=15"}, // was packagedItem small_resource_bundle
				{51, 60, "nothing"},        // was an IMPLICIT GAP — the 10% no-drop
				{61, 90, "list:basic_weapons"},
				{91, 100, "list:basic_accessories"},
			},
		},
		{
			table:   "wildborne_loot",
			maxRoll: 100,
			faces: []face{
				{1, 25, "gold=50,wood=15"},   // small_resource_bundle
				{26, 75, "gold=100,wood=45"}, // medium_resource_bundle
				{76, 100, "list:rare_weapons"},
			},
		},
		{
			table:   "merchant_basic",
			maxRoll: 100,
			faces: []face{
				{1, 35, "list:merchant_weapons"},
				{36, 65, "list:merchant_potions"},
				{66, 85, "list:merchant_accessories"},
				{86, 100, "list:merchant_armor"},
			},
		},
	}

	for _, tc := range cases {
		table, ok := getTableDef(tc.table)
		if !ok {
			t.Fatalf("table %q not found", tc.table)
		}
		if table.MaxRoll != tc.maxRoll {
			t.Errorf("%s: maxRoll = %d, want %d", tc.table, table.MaxRoll, tc.maxRoll)
		}
		// Expand the expectation to one entry per face, so an off-by-one in ANY
		// boundary shows up as a specific face rather than a vague mismatch.
		want := make(map[int]string, tc.maxRoll)
		for _, f := range tc.faces {
			for roll := f.from; roll <= f.to; roll++ {
				want[roll] = f.outcome
			}
		}
		for roll := 1; roll <= tc.maxRoll; roll++ {
			got := outcomeForFace(table, roll)
			if got != want[roll] {
				t.Errorf("%s: roll %d yields %q, want %q (the pre-migration outcome)",
					tc.table, roll, got, want[roll])
			}
		}
	}
}

// outcomeForFace resolves what a specific die face yields, WITHOUT rolling —
// so the fidelity test compares the authored mapping rather than sampling it.
func outcomeForFace(table *TableDef, roll int) string {
	for i := range table.Rows {
		r := &table.Rows[i]
		if roll < r.Min || roll > r.Max {
			continue
		}
		switch {
		case r.Nothing:
			return "nothing"
		case len(r.Resources) > 0:
			return fmt.Sprintf("gold=%d,wood=%d", r.Resources["gold"], r.Resources["wood"])
		case r.List != "":
			return "list:" + r.List
		}
	}
	return "(uncovered)"
}

// TestListMigration_WeightedRangesAreUnchanged: the 7 subtables became weighted
// lists with their roll ranges intact, including the two that roll their own
// small die rather than a d100 (basic_weapons 1-15, rare_weapons 1-20 — those
// maxima used to be IMPLIED by the highest entry and are now explicit).
func TestListMigration_WeightedRangesAreUnchanged(t *testing.T) {
	type entry struct {
		item     string
		min, max int
	}
	cases := []struct {
		list    string
		maxRoll int
		entries []entry
	}{
		{"basic_weapons", 15, []entry{{"broad_sword", 1, 10}, {"scimitar", 11, 15}}},
		{"rare_weapons", 20, []entry{
			{"scimitar", 1, 14}, {"fire_sword", 15, 16}, {"frost_sword", 17, 18}, {"lightning_sword", 19, 20}}},
		{"merchant_weapons", 100, []entry{
			{"broad_sword", 1, 40}, {"scimitar", 41, 65}, {"flame_sword", 66, 85},
			{"frost_scimitar", 86, 95}, {"shadow_blade", 96, 100}}},
		{"merchant_potions", 100, []entry{
			{"potion_common_heal", 1, 40}, {"potion_uncommon_heal", 41, 65}, {"potion_rare_heal", 66, 75},
			{"experience_potion", 76, 85}, {"potion_epic_heal", 86, 95}, {"potion_legendary_heal", 96, 100}}},
		{"merchant_accessories", 100, []entry{
			{"fire_ring", 1, 33}, {"ice_ring", 34, 67}, {"lightning_ring", 68, 100}}},
		{"basic_accessories", 100, []entry{
			{"fire_ring", 1, 33}, {"ice_ring", 34, 67}, {"lightning_ring", 68, 100}}},
		{"merchant_armor", 100, []entry{
			{"leather_armor", 1, 50}, {"half_plate", 51, 85}, {"plate_armor", 86, 100}}},
	}

	for _, tc := range cases {
		list, ok := getListDef(tc.list)
		if !ok {
			t.Errorf("list %q not found", tc.list)
			continue
		}
		if !list.IsWeighted() {
			t.Errorf("%s: should be a WEIGHTED list (it was a loot subtable)", tc.list)
			continue
		}
		if list.MaxRoll != tc.maxRoll {
			t.Errorf("%s: maxRoll = %d, want %d", tc.list, list.MaxRoll, tc.maxRoll)
		}
		if len(list.Entries) != len(tc.entries) {
			t.Errorf("%s: %d entries, want %d", tc.list, len(list.Entries), len(tc.entries))
			continue
		}
		for i, want := range tc.entries {
			got := list.Entries[i]
			if got.Item != want.item || got.Min != want.min || got.Max != want.max {
				t.Errorf("%s: entries[%d] = %s %d-%d, want %s %d-%d",
					tc.list, i, got.Item, got.Min, got.Max, want.item, want.min, want.max)
			}
		}
	}
}

// ─── Coverage rules ─────────────────────────────────────────────────────────

// TestValidateTableDef_CoverageIsTotal: a table's rows must tile 1..maxRoll.
// A gap is an error — that is what makes the `nothing` row necessary, and it is
// what stops a hole in the ranges from reading like a typo (which is exactly how
// raider_loot's deliberate 51-60 no-drop used to read).
func TestValidateTableDef_CoverageIsTotal(t *testing.T) {
	row := func(a, b int) TableRow { return TableRow{Min: a, Max: b, Nothing: true} }

	err := validateTableDef(&TableDef{ID: "t", MaxRoll: 100, Rows: []TableRow{row(1, 50), row(61, 100)}})
	if err == nil {
		t.Error("a gap (51-60) must be rejected")
	} else if !strings.Contains(err.Error(), "51-60") {
		t.Errorf("the error should name the uncovered rolls, got: %v", err)
	}

	err = validateTableDef(&TableDef{ID: "t", MaxRoll: 100, Rows: []TableRow{row(1, 50), row(40, 100)}})
	if err == nil {
		t.Error("an overlap must be rejected")
	}

	err = validateTableDef(&TableDef{ID: "t", MaxRoll: 100, Rows: []TableRow{row(1, 50)}})
	if err == nil {
		t.Error("a trailing gap (51-100) must be rejected")
	}

	// Total coverage in any authored order.
	if err := validateTableDef(&TableDef{ID: "t", MaxRoll: 100, Rows: []TableRow{row(51, 100), row(1, 50)}}); err != nil {
		t.Errorf("rows that tile the die must validate regardless of order: %v", err)
	}
}

// TestValidateTableDef_OneOutcomePerRow: a row does exactly one thing.
func TestValidateTableDef_OneOutcomePerRow(t *testing.T) {
	full := func(r TableRow) *TableDef {
		r.Min, r.Max = 1, 100
		return &TableDef{ID: "t", MaxRoll: 100, Rows: []TableRow{r}}
	}
	if err := validateTableDef(full(TableRow{})); err == nil {
		t.Error("a row with no outcome must be rejected")
	}
	if err := validateTableDef(full(TableRow{List: "marketplace", Nothing: true})); err == nil {
		t.Error("a row with two outcomes must be rejected")
	}
	if err := validateTableDef(full(TableRow{List: "no_such_list"})); err == nil {
		t.Error("a row naming an unknown list must be rejected")
	}
	if err := validateTableDef(full(TableRow{Resources: map[string]int{"gems": 5}})); err == nil {
		t.Error("a row granting an unknown resource must be rejected")
	}
	if err := validateTableDef(full(TableRow{Resources: map[string]int{"gold": 0}})); err == nil {
		t.Error("a non-positive grant must be rejected")
	}
	if err := validateTableDef(full(TableRow{Nothing: true})); err != nil {
		t.Errorf("a single valid row covering the die must validate: %v", err)
	}
}

// ─── Rolling ────────────────────────────────────────────────────────────────

// TestRollTable_EachOutcomeKind exercises the three row kinds.
func TestRollTable_EachOutcomeKind(t *testing.T) {
	registerTestList(t, &ListDef{ID: "tt_pool", Name: "Pool", Items: []string{"broad_sword"}})

	only := func(r TableRow) *TableDef {
		r.Min, r.Max = 1, 100
		return &TableDef{ID: "tt", MaxRoll: 100, Rows: []TableRow{r}}
	}
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 5)
	s.mu.Lock()
	defer s.mu.Unlock()

	got := s.rollTableLocked(only(TableRow{Nothing: true}))
	if !got.Empty() {
		t.Errorf("a `nothing` row must yield nothing, got %+v", got)
	}

	got = s.rollTableLocked(only(TableRow{Resources: map[string]int{"gold": 50, "wood": 15}}))
	if got.Resources["gold"] != 50 || got.Resources["wood"] != 15 || len(got.Items) != 0 {
		t.Errorf("a resource row must grant exactly its resources and no item, got %+v", got)
	}

	got = s.rollTableLocked(only(TableRow{List: "tt_pool"}))
	if len(got.Items) != 1 || got.Items[0] != "broad_sword" || len(got.Resources) != 0 {
		t.Errorf("a list row must yield one item and no resources, got %+v", got)
	}
}

// TestRollTable_MutatingTheResultCannotCorruptTheCatalog: the grant map handed
// back is a copy. Without it, a caller topping up a chest's gold would edit the
// catalog's table for every future roll of the match.
func TestRollTable_MutatingTheResultCannotCorruptTheCatalog(t *testing.T) {
	table := &TableDef{ID: "tt", MaxRoll: 100, Rows: []TableRow{
		{Min: 1, Max: 100, Resources: map[string]int{"gold": 50}},
	}}
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 5)
	s.mu.Lock()
	defer s.mu.Unlock()

	first := s.rollTableLocked(table)
	first.Resources["gold"] = 99999

	second := s.rollTableLocked(table)
	if second.Resources["gold"] != 50 {
		t.Fatalf("mutating a roll's grant leaked into the catalog: next roll gave %d gold, want 50",
			second.Resources["gold"])
	}
}

// TestRollTable_Deterministic: same seed, same outcome.
func TestRollTable_Deterministic(t *testing.T) {
	roll := func() string {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 909)
		s.mu.Lock()
		defer s.mu.Unlock()
		table, _ := getTableDef("raider_loot")
		r := s.rollTableLocked(table)
		return fmt.Sprintf("%v|%v", r.Items, r.Resources)
	}
	if a, b := roll(), roll(); a != b {
		t.Fatalf("same seed produced %q then %q — loot must stay deterministic", a, b)
	}
}
