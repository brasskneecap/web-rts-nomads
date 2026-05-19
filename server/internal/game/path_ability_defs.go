package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// path_ability_defs.go is the structural twin of path_defs.go for Phase-2
// per-path ability kits. It loads, per promotion (path, rank), the ordered
// list of ability ids that are GRANTED to a unit when it reaches that rank
// on that path.
//
// Layout (mirrors the perks layout, one directory deeper):
//
//	catalog/units/<faction>/<unit>/paths/<path>/<path>.json        — stat curve (path_defs.go)
//	catalog/units/<faction>/<unit>/paths/<path>/perks/<rank>.json  — perk pool (perk_defs.go)
//	catalog/units/<faction>/<unit>/paths/<path>/abilities/<rank>.json — ability grants (THIS FILE)
//
// File shape: { "grant": ["greater_heal", ...] }. Order is significant — it is
// the order ids are appended to unit.Abilities at promotion, which is also the
// autocast slot order. A missing abilities/ dir or a missing <rank>.json is a
// legitimate empty grant (most (path,rank) cells grant nothing), NOT an error.
//
//go:embed catalog/units
var pathAbilityDefsFS embed.FS

// pathAbilityCatalogFile is the on-disk shape of one
// paths/<path>/abilities/<rank>.json. Only "grant" is meaningful; the path and
// rank come from the directory/file names (same convention as the perk files).
type pathAbilityCatalogFile struct {
	Grant []string `json:"grant"`
}

// pathAbilityGrantsByKey maps pathModifierKey(path, rank) → ordered granted
// ability ids. Populated at init; read-only during simulation (no determinism
// or concurrency concern). Missing key ⇒ no grant (see pathAbilityGrantsFor).
var pathAbilityGrantsByKey map[string][]string

// pathAbilityGrantsFor returns the ordered ability ids granted at (path, rank),
// or nil when that cell grants nothing. The returned slice is the loader's own
// backing array — callers MUST NOT mutate it (assignUnitPathAbilitiesLocked
// only reads it).
func pathAbilityGrantsFor(path, rank string) []string {
	return pathAbilityGrantsByKey[pathModifierKey(path, rank)]
}

func init() {
	// NOTE: this init validates every granted id against the ability-def
	// registry (getAbilityDef). Go initializes package files in lexical
	// filename order, so ability_defs.go ("a") runs before
	// path_ability_defs.go ("p") and abilityDefsByID is already populated
	// here — same implicit ordering path_defs.go already relies on.
	pathAbilityGrantsByKey = make(map[string][]string, 16)

	factionEntries, err := fs.ReadDir(pathAbilityDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	for _, factionEntry := range factionEntries {
		if !factionEntry.IsDir() {
			continue // unit_defs.go already panics on stray files
		}
		factionKey := factionEntry.Name()
		unitEntries, err := fs.ReadDir(pathAbilityDefsFS, "catalog/units/"+factionKey)
		if err != nil {
			continue
		}
		for _, unitEntry := range unitEntries {
			if !unitEntry.IsDir() {
				continue
			}
			unitKey := unitEntry.Name()
			pathsDir := "catalog/units/" + factionKey + "/" + unitKey + "/paths"
			pathEntries, err := fs.ReadDir(pathAbilityDefsFS, pathsDir)
			if err != nil {
				continue // unit has no promotion paths
			}
			for _, pathEntry := range pathEntries {
				if !pathEntry.IsDir() {
					continue // path_defs.go already panics on loose files here
				}
				pathKey := pathEntry.Name()
				abilitiesDir := pathsDir + "/" + pathKey + "/abilities"
				abilityEntries, err := fs.ReadDir(pathAbilityDefsFS, abilitiesDir)
				if err != nil {
					continue // no abilities/ — this path grants no abilities (legit)
				}
				for _, abilityEntry := range abilityEntries {
					if abilityEntry.IsDir() {
						panic(fmt.Sprintf("%s/%s: unexpected directory — abilities/ must contain <rank>.json files only",
							abilitiesDir, abilityEntry.Name()))
					}
					name := abilityEntry.Name()
					if !strings.HasSuffix(name, ".json") {
						panic(fmt.Sprintf("%s/%s: unexpected non-JSON file in abilities/", abilitiesDir, name))
					}
					rankName := strings.TrimSuffix(name, ".json")
					if _, ok := validRankName[rankName]; !ok {
						panic(fmt.Sprintf("%s/%s: unknown rank %q (want bronze/silver/gold)", abilitiesDir, name, rankName))
					}
					rel := abilitiesDir + "/" + name
					data, err := pathAbilityDefsFS.ReadFile(rel)
					if err != nil {
						panic(rel + ": " + err.Error())
					}
					var file pathAbilityCatalogFile
					if err := json.Unmarshal(data, &file); err != nil {
						panic(rel + ": " + err.Error())
					}
					for _, abilityID := range file.Grant {
						if abilityID == "" {
							panic(rel + `: empty ability id in "grant"`)
						}
						if _, ok := getAbilityDef(abilityID); !ok {
							panic(fmt.Sprintf("%s: granted ability %q has no registered AbilityDef", rel, abilityID))
						}
					}
					key := pathModifierKey(pathKey, rankName)
					if _, exists := pathAbilityGrantsByKey[key]; exists {
						panic(fmt.Sprintf("%s: duplicate ability grant for %s", rel, key))
					}
					// Copy so the stored slice is independent of the unmarshal buffer.
					grants := make([]string, len(file.Grant))
					copy(grants, file.Grant)
					pathAbilityGrantsByKey[key] = grants
				}
			}
		}
	}
}

// assignUnitPathAbilitiesLocked grants unit the path-specific abilities for
// its current (ProgressionPath, Rank). It is the ability twin of
// assignUnitPerkLocked (perks.go) and is called from addUnitXPLocked's
// per-crossed-rank loop, immediately after assignUnitPerkLocked.
//
// Invariants (see openspec/changes/.../per-path-ability-kits/spec.md):
//   - Idempotent: an id already on unit.Abilities is never appended twice, so
//     it is safe across the multi-rank catch-up loop and on re-invocation.
//   - Ordered: ids are appended in catalog "grant" order (== autocast slot
//     order), deterministically.
//   - RNG-free: the only progression RNG remains the path *choice* in
//     assignUnitPathOnRankUpLocked; granting introduces none.
//
// Base-rank or path-less units get nothing (mirrors assignUnitPerkLocked's
// unitRankBase short-circuit). Granted spell abilities need no spawn-path
// change: autocast/cooldown maps initialise lazily on first use exactly as
// for base abilities.
//
// Caller holds s.mu.
func (s *GameState) assignUnitPathAbilitiesLocked(unit *Unit) {
	if unit == nil || unit.Rank == unitRankBase || unit.ProgressionPath == unitPathNone {
		return
	}
	for _, abilityID := range pathAbilityGrantsFor(unit.ProgressionPath, unit.Rank) {
		if !containsAbility(unit, abilityID) {
			unit.Abilities = append(unit.Abilities, abilityID)
		}
	}
}

// ListPathAbilityGrants returns every (path,rank) grant, sorted by key, for
// stable test/diagnostic output. Mirrors ListPathBounds.
func ListPathAbilityGrants() map[string][]string {
	out := make(map[string][]string, len(pathAbilityGrantsByKey))
	for k, v := range pathAbilityGrantsByKey {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// sortedPathAbilityKeys is a deterministic-iteration helper for tests.
func sortedPathAbilityKeys() []string {
	keys := make([]string, 0, len(pathAbilityGrantsByKey))
	for k := range pathAbilityGrantsByKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
