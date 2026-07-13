package game

import (
	"sort"
	"testing"
)

func factionByID(t *testing.T, id string) FactionDef {
	t.Helper()
	for _, f := range ListFactions() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("faction %q not found in ListFactions()", id)
	return FactionDef{}
}

// human ships a faction.json — its displayName must come from the file, and the
// loader must not panic on the file's presence inside a faction directory.
func TestListFactions_ReadsAuthoredRecord(t *testing.T) {
	human := factionByID(t, "human")
	if human.DisplayName != "Human" {
		t.Fatalf("human displayName = %q, want %q (from faction.json)", human.DisplayName, "Human")
	}
}

// The other faction dirs have no faction.json and must still be real factions
// with a readable label — that is the "no new files needed" guarantee.
func TestListFactions_SynthesizesMissingRecord(t *testing.T) {
	wither := factionByID(t, "witherborne")
	if wither.DisplayName != "Witherborne" {
		t.Fatalf("witherborne displayName = %q, want titleized %q", wither.DisplayName, "Witherborne")
	}
}

// Every faction referenced by a unit must appear — otherwise the editor's
// filter would hide units that exist.
func TestListFactions_CoversEveryFactionAUnitReferences(t *testing.T) {
	present := map[string]bool{}
	for _, f := range ListFactions() {
		present[f.ID] = true
	}
	for _, def := range ListUnitDefs() {
		if def.Faction == "" {
			continue
		}
		if !present[def.Faction] {
			t.Fatalf("unit %q has faction %q, which ListFactions() omits", def.Type, def.Faction)
		}
	}
}

func TestListFactions_OrderedBeforeUnordered(t *testing.T) {
	got := ListFactions()
	var ids []string
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	// human declares order:1; the rest are record-less (unordered) and must
	// follow it, alphabetically.
	if len(ids) < 4 || ids[0] != "human" {
		t.Fatalf("faction order = %v; want the order-declaring faction (human) first", ids)
	}
	rest := ids[1:]
	if !sort.StringsAreSorted(rest) {
		t.Fatalf("unordered factions must be alphabetical, got %v", rest)
	}
}

func TestTitleizeFactionID(t *testing.T) {
	cases := map[string]string{
		"human":       "Human",
		"witherborne": "Witherborne",
		"night_elf":   "Night Elf",
	}
	for in, want := range cases {
		if got := titleizeFactionID(in); got != want {
			t.Fatalf("titleizeFactionID(%q) = %q, want %q", in, got, want)
		}
	}
}
