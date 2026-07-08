package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

// spell_pool_defs.go loads the data-driven spell pools (arch-mage-spell-system,
// §11). A pool is the set of spells an archetype MAY be granted at a given rank;
// exactly one is rolled per unit at promotion (spell_pool_roll.go).
//
// Pools are deliberately SEPARATE from spell definitions (the AbilityDef
// catalog): adding a spell to a pool is a one-line edit here, and adding a
// brand-new spell is a new AbilityDef — the two concerns never touch. The
// archetype key is the promotion-path id (e.g. "arch_mage"), so the roll can
// look a pool up straight from unit.ProgressionPath.
//
// File shape (single catalog file):
//
//	{ "<archetype>": { "bronze": ["id", ...], "silver": [...], "gold": [...] } }
//
// Every listed id MUST resolve to a registered AbilityDef and every rank key
// MUST be bronze/silver/gold — both are validated at load with a panic naming
// the offender (catalog-strictness convention). A missing archetype or rank
// resolves to an empty pool, never an error.
//
//go:embed catalog/spell-pools.json
var spellPoolsRaw []byte

// spellPoolsByArchetype maps archetype → rank → ordered candidate ability ids.
// Populated at init; read-only during simulation.
var spellPoolsByArchetype = mustLoadSpellPools(spellPoolsRaw)

// loadSpellPools parses and validates the spell-pool catalog bytes. Returns an
// error (not a panic) so the parse/validation logic is unit-testable with
// synthetic inputs; the package-level loader wraps it in a panic. Validation:
// every rank key is bronze/silver/gold, and every ability id is a registered
// AbilityDef.
func loadSpellPools(data []byte) (map[string]map[string][]string, error) {
	var raw map[string]map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("spell-pools: %w", err)
	}
	out := make(map[string]map[string][]string, len(raw))
	for archetype, byRank := range raw {
		if archetype == "" {
			return nil, fmt.Errorf("spell-pools: empty archetype key")
		}
		ranks := make(map[string][]string, len(byRank))
		for rank, ids := range byRank {
			if _, ok := validRankName[rank]; !ok {
				return nil, fmt.Errorf("spell-pools: archetype %q has unknown rank %q (want bronze/silver/gold)", archetype, rank)
			}
			pool := make([]string, 0, len(ids))
			for _, id := range ids {
				if id == "" {
					return nil, fmt.Errorf("spell-pools: archetype %q rank %q has an empty spell id", archetype, rank)
				}
				if _, ok := getAbilityDef(id); !ok {
					return nil, fmt.Errorf("spell-pools: archetype %q rank %q spell %q has no registered AbilityDef", archetype, rank, id)
				}
				pool = append(pool, id)
			}
			ranks[rank] = pool
		}
		out[archetype] = ranks
	}
	return out, nil
}

// mustLoadSpellPools is the package-init wrapper: a malformed spell-pool catalog
// is a build/authoring error and crashes at startup, same discipline as the
// other catalog loaders.
func mustLoadSpellPools(data []byte) map[string]map[string][]string {
	pools, err := loadSpellPools(data)
	if err != nil {
		panic(err.Error())
	}
	return pools
}

// spellPoolFor returns the candidate ability ids an archetype may roll at `rank`.
// The shared spell pool spans BRONZE and SILVER only, cumulatively: Bronze rolls
// from the Bronze list; Silver rolls from Bronze ∪ Silver (so the two ranks
// share one pool). A single authored list under "bronze" is therefore rolled at
// both promotions, and the no-duplicate logic in spell_pool_roll.go ensures each
// rank grants a distinct spell.
//
// GOLD intentionally grants NO pool spell (resolves to an empty pool ⇒ rolls
// nothing): Gold is the Arch Mage's PERK tier — a set of perks that buff spells
// and the caster — not a third learnable spell. Returns a freshly built,
// deduped, order-stable slice (bronze order, then any new silver ids); callers
// may sort it (the roll does). nil when the archetype has no pool, or for Gold /
// any unknown rank.
func spellPoolFor(archetype, rank string) []string {
	byRank, ok := spellPoolsByArchetype[archetype]
	if !ok {
		return nil
	}
	var order []string
	switch rank {
	case unitRankBronze:
		order = []string{unitRankBronze}
	case unitRankSilver:
		order = []string{unitRankBronze, unitRankSilver}
	default:
		return nil // Gold (perk tier) and any unknown rank grant no pool spell.
	}
	seen := make(map[string]bool)
	var out []string
	for _, r := range order {
		for _, id := range byRank[r] {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// ListSpellPools returns a deep copy of every pool, sorted-key stable, for
// diagnostics and tests.
func ListSpellPools() map[string]map[string][]string {
	out := make(map[string]map[string][]string, len(spellPoolsByArchetype))
	for archetype, byRank := range spellPoolsByArchetype {
		ranks := make(map[string][]string, len(byRank))
		for rank, ids := range byRank {
			cp := make([]string, len(ids))
			copy(cp, ids)
			ranks[rank] = cp
		}
		out[archetype] = ranks
	}
	return out
}

// sortedSpellPoolArchetypes is a deterministic-iteration helper for tests.
func sortedSpellPoolArchetypes() []string {
	keys := make([]string, 0, len(spellPoolsByArchetype))
	for k := range spellPoolsByArchetype {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
