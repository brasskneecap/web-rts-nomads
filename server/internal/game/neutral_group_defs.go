package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// Embeds the neutral-group composition catalog. Each tier_<N>.json holds
// multiple named groups; each group is a composition of (unitType, count)
// pairs. Layout:
//
//	catalog/neutral_groups/tier_1.json
//	catalog/neutral_groups/tier_2.json
//
// Composition entries reference existing unit types from the units catalog
// (e.g. raider, ranged_raider) — no new "neutral faction" unit defs are
// required. Neutrals are retagged at spawn time under the virtual
// neutralPlayerID slot (see state_neutral_camps.go, Batch C+).

//go:embed catalog/neutral_groups
var neutralGroupsFS embed.FS

// NeutralGroupCompositionEntry is one slot in a group's composition list:
// spawn Count units of UnitType around the camp center.
type NeutralGroupCompositionEntry struct {
	UnitType string `json:"unitType"`
	Count    int    `json:"count"`
}

// NeutralGroup is one named group composition (e.g. "small_raider_group").
//
// A group names AT MOST ONE loot source — either a weighted LootTable or a
// LootList, never both (setting both is a load-time panic). They are two
// deliberately different shapes:
//
//   - LootTable — the rich case. A weighted roll over lists, resource (gold/wood)
//     grants, and explicit "nothing drops" outcomes.
//   - LootList  — the simple case. Roll a list directly; always yields an item.
//     Use a table when you want a chance of nothing, or of gold.
type NeutralGroup struct {
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	Composition []NeutralGroupCompositionEntry `json:"composition"`
	LootTable   string                         `json:"lootTable,omitempty"`
	LootList    string                         `json:"lootList,omitempty"`
}

// NeutralGroupTier holds all groups available at a given tier level.
type NeutralGroupTier struct {
	Tier   int            `json:"tier"`
	Groups []NeutralGroup `json:"groups"`
}

// neutralGroupsByTier is the runtime registry. Keyed by tier number.
// Populated at startup; immutable afterwards.
var neutralGroupsByTier = loadNeutralGroupsByTier()

// neutralTiersSorted caches the sorted list of available tier numbers so
// resolveNeutralTier doesn't re-sort on every call.
var neutralTiersSorted = sortedNeutralTierKeys(neutralGroupsByTier)

var neutralGroupTierFilenameRE = regexp.MustCompile(`^tier_(\d+)\.json$`)

func loadNeutralGroupsByTier() map[int]NeutralGroupTier {
	entries, err := fs.ReadDir(neutralGroupsFS, "catalog/neutral_groups")
	if err != nil {
		// Directory missing is OK — feature is opt-in per map. Return empty.
		return map[int]NeutralGroupTier{}
	}
	result := make(map[int]NeutralGroupTier, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			panic("catalog/neutral_groups: unexpected subdirectory " + entry.Name() + " — only tier_<N>.json files allowed")
		}
		m := neutralGroupTierFilenameRE.FindStringSubmatch(entry.Name())
		if m == nil {
			// Nothing but tier files lives here any more — loot_tables.json moved
			// out to catalog/lists + catalog/tables. An unexpected file is a
			// mistake, not something to skip past silently.
			panic("catalog/neutral_groups: unexpected file " + entry.Name() + ` — must match "tier_<N>.json"`)
		}
		tierNum, err := strconv.Atoi(m[1])
		if err != nil || tierNum < 1 {
			panic("catalog/neutral_groups: invalid tier number in " + entry.Name())
		}
		rel := "catalog/neutral_groups/" + entry.Name()
		data, err := neutralGroupsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var tier NeutralGroupTier
		if err := json.Unmarshal(data, &tier); err != nil {
			panic(rel + ": " + err.Error())
		}
		if tier.Tier != tierNum {
			panic(rel + ": JSON tier field " + strconv.Itoa(tier.Tier) + " does not match filename tier " + strconv.Itoa(tierNum))
		}
		if len(tier.Groups) == 0 {
			panic(rel + ": tier has zero groups — at least one required")
		}
		seenIDs := make(map[string]struct{}, len(tier.Groups))
		for gi, g := range tier.Groups {
			if g.ID == "" {
				panic(rel + ": group " + strconv.Itoa(gi) + " missing id")
			}
			if g.Name == "" {
				panic(rel + ": group " + g.ID + " missing display name")
			}
			if _, dup := seenIDs[g.ID]; dup {
				panic(rel + ": duplicate group id " + g.ID + " within tier")
			}
			seenIDs[g.ID] = struct{}{}
			if len(g.Composition) == 0 {
				panic(rel + ": group " + g.ID + " has empty composition")
			}
			for ci, c := range g.Composition {
				if c.UnitType == "" {
					panic(rel + ": group " + g.ID + " composition entry " + strconv.Itoa(ci) + " missing unitType")
				}
				if c.Count < 1 {
					panic(rel + ": group " + g.ID + " composition entry " + c.UnitType + " has count " + strconv.Itoa(c.Count) + " (must be >= 1)")
				}
				if _, ok := getUnitDef(c.UnitType); !ok {
					panic(rel + ": group " + g.ID + " references unknown unitType " + c.UnitType)
				}
			}
			if err := validateNeutralGroupLoot(&g); err != nil {
				panic(rel + ": " + err.Error())
			}
		}
		result[tierNum] = tier
	}
	return result
}

// validateNeutralGroupLoot enforces that a group names AT MOST ONE loot source,
// and that whichever it names exists.
//
// Two sources with a silent winner is precisely the kind of thing that gets
// mis-authored and never noticed, so it is an error rather than a precedence
// rule. A group with no loot source at all is legal — it simply drops nothing.
func validateNeutralGroupLoot(g *NeutralGroup) error {
	if g.LootTable != "" && g.LootList != "" {
		return fmt.Errorf("group %s sets both %q and %q — a group drops from one source or the other, never both",
			g.ID, "lootTable", "lootList")
	}
	if g.LootTable != "" {
		if _, ok := getTableDef(g.LootTable); !ok {
			return fmt.Errorf("group %s references unknown lootTable %q", g.ID, g.LootTable)
		}
	}
	if g.LootList != "" {
		if _, ok := getListDef(g.LootList); !ok {
			return fmt.Errorf("group %s references unknown lootList %q", g.ID, g.LootList)
		}
	}
	return nil
}

func sortedNeutralTierKeys(m map[int]NeutralGroupTier) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// resolveNeutralTier returns the largest available tier number <= requested.
// Returns 0 (sentinel "no tier available") when:
//   - requested <= 0
//   - no tier files have been loaded
//   - no shipped tier is <= requested
func resolveNeutralTier(requested int) int {
	if requested <= 0 || len(neutralTiersSorted) == 0 {
		return 0
	}
	for i := len(neutralTiersSorted) - 1; i >= 0; i-- {
		if neutralTiersSorted[i] <= requested {
			return neutralTiersSorted[i]
		}
	}
	return 0
}

// getNeutralGroup looks up a specific group by id within a tier.
// tier must be a key in neutralGroupsByTier (use resolveNeutralTier first
// if you want fallback). Returns (group, true) on hit, (zero, false) when
// the tier is unloaded or the id is unknown.
func getNeutralGroup(tier int, id string) (NeutralGroup, bool) {
	t, ok := neutralGroupsByTier[tier]
	if !ok {
		return NeutralGroup{}, false
	}
	for _, g := range t.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return NeutralGroup{}, false
}

// listNeutralGroupIDs returns all group ids in a tier in JSON order.
// Used by the random selector (Batch D) and the HTTP catalog endpoint (Batch G).
func listNeutralGroupIDs(tier int) []string {
	t, ok := neutralGroupsByTier[tier]
	if !ok {
		return nil
	}
	out := make([]string, len(t.Groups))
	for i, g := range t.Groups {
		out[i] = g.ID
	}
	return out
}

// ListNeutralGroupsForCatalog returns a serializable view of every
// shipped tier and its groups (id + name only). Used by the
// /api/catalog/neutral-groups HTTP endpoint so the map editor can
// populate its tier/group dropdowns. Tier order is ascending and
// stable across calls.
func ListNeutralGroupsForCatalog() []NeutralGroupTierSummary {
	out := make([]NeutralGroupTierSummary, 0, len(neutralTiersSorted))
	for _, tier := range neutralTiersSorted {
		t := neutralGroupsByTier[tier]
		groups := make([]NeutralGroupSummary, len(t.Groups))
		for i, g := range t.Groups {
			groups[i] = NeutralGroupSummary{ID: g.ID, Name: g.Name}
		}
		out = append(out, NeutralGroupTierSummary{Tier: tier, Groups: groups})
	}
	return out
}

// NeutralGroupTierSummary is the wire-level view of one tier for the
// map editor catalog endpoint.
type NeutralGroupTierSummary struct {
	Tier   int                   `json:"tier"`
	Groups []NeutralGroupSummary `json:"groups"`
}

// NeutralGroupSummary is the wire-level view of one group: just enough
// for the editor dropdown.
type NeutralGroupSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
