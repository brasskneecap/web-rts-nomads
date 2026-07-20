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

// assignUnitPathAbilitiesLocked re-derives unit.Abilities from its current
// (UnitType, ProgressionPath, Rank). It is the canonical answer to "what
// abilities does this unit have?" and is idempotent: calling it twice with
// the same inputs produces the same result.
//
// Composition (in order):
//
//  1. Start with the unit def's base abilities (acolyte → ["heal"]).
//  2. If the unit's path declares an "abilities" override in its path JSON
//     (e.g. cleric.json's "abilities": ["greater_heal"]), REPLACE the list.
//     This is the "1-for-1 upgrade" pattern — paths declare what their units
//     have, full stop, with no swap mutation needed.
//  3. For each rank R the unit has reached (bronze → silver → gold up to its
//     current rank), append any (path, R) grants from path_ability_defs.go
//     ADDITIVELY (de-duped). This is for future "silver cleric also gets X"
//     content; the cleric/bronze cell intentionally has no grant file because
//     greater_heal lives in the path-level override above.
//
// Per-instance AutoCastEnabled / AbilityCooldowns are migrated by position
// when an entry changes (e.g. acolyte "heal" at index 0 → cleric
// "greater_heal" at index 0). A heal-autocasted acolyte promoted to
// cleric keeps autocast on greater_heal automatically.
//
// Called every promotion event (addUnitXPLocked rank loop) and from
// DebugSpawnUnit after path/rank assignment. Safe to call repeatedly.
//
// Caller holds s.mu.
func (s *GameState) assignUnitPathAbilitiesLocked(unit *Unit) {
	if unit == nil {
		return
	}

	// Step 1: base abilities from the unit def. spawnUnitFromDefLocked already
	// initialises unit.Abilities with this list — we re-derive it here so the
	// function is a pure (UnitType, Path, Rank) → []string recompute regardless
	// of what unit.Abilities currently holds.
	var newAbilities []string
	if def, ok := getUnitDef(unit.UnitType); ok {
		newAbilities = append([]string(nil), def.Abilities...)
	}

	// Step 2: path-level override. Applies whenever a path is assigned (even
	// at base rank — a debug-spawned cleric/base still gets greater_heal).
	if unit.ProgressionPath != unitPathNone {
		if pathAbilities, ok := pathAbilitiesFor(unit.ProgressionPath); ok {
			newAbilities = append([]string(nil), pathAbilities...)
		}
	}

	// Step 3: per-(path, rank) grants. Walks every rank the unit has reached
	// so the result is correct on a single call even if multiple ranks were
	// crossed at once (debug spawn, large XP gain).
	if unit.Rank != unitRankBase && unit.ProgressionPath != unitPathNone {
		for _, rank := range ranksUpToInclusive(unit.Rank) {
			for _, abilityID := range pathAbilityGrantsFor(unit.ProgressionPath, rank) {
				if !containsString(newAbilities, abilityID) {
					newAbilities = append(newAbilities, abilityID)
				}
			}
		}
	}

	// Step 3b: recorded ability-pool picks (arch-mage-spell-system §11). The
	// random roll already happened at rank-up (rollUnitPoolAbilitiesLocked) and
	// recorded its choice on unit.PoolAbilitiesByRank; here we only READ it, so
	// this recompute stays RNG-free and idempotent. Appended additively (de-
	// duped) in rank order, composing on top of the path override + rank grants.
	if unit.Rank != unitRankBase && unit.ProgressionPath != unitPathNone {
		for _, rank := range ranksUpToInclusive(unit.Rank) {
			if pick := unit.PoolAbilitiesByRank[rank]; pick != "" && !containsString(newAbilities, pick) {
				newAbilities = append(newAbilities, pick)
			}
		}
	}

	// Step 4: per-perk ability grants. A perk's PerkDef.GrantsAbilities lists
	// ability ids that should appear on the unit's action bar when the perk is
	// owned. Used by ability-granting perks (e.g. Siphoner bronze:
	// lingering_hex / mark_of_weakness — a Siphoner who rolls one of those
	// Bronze picks gains a new castable). Idempotent: containsString dedupes
	// so re-runs (DebugSpawnUnit, rank-up loop) never duplicate entries.
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		for _, abilityID := range def.GrantsAbilities {
			if abilityID == "" {
				continue
			}
			if !containsString(newAbilities, abilityID) {
				newAbilities = append(newAbilities, abilityID)
			}
		}
	}

	// Migrate AutoCastEnabled / AbilityCooldowns by position. Same-index
	// replacements (heal → greater_heal) carry player intent and runtime
	// timer state across the swap. Indices that don't change are skipped;
	// indices that grow beyond the old length are fresh slots with no
	// migration source (defaults apply).
	for i, newID := range newAbilities {
		if i >= len(unit.Abilities) {
			break
		}
		oldID := unit.Abilities[i]
		if oldID == newID {
			continue
		}
		if unit.AutoCastEnabled != nil {
			if v, had := unit.AutoCastEnabled[oldID]; had {
				unit.AutoCastEnabled[newID] = v
				delete(unit.AutoCastEnabled, oldID)
			}
		}
		if unit.AbilityCooldowns != nil {
			if v, had := unit.AbilityCooldowns[oldID]; had {
				unit.AbilityCooldowns[newID] = v
				delete(unit.AbilityCooldowns, oldID)
			}
		}
	}

	unit.Abilities = newAbilities

	// After migration, seed default auto-cast for newly-granted abilities (any
	// id present in newAbilities but with no AutoCastEnabled entry yet — e.g.
	// additive grants at higher ranks). Same helper used at spawn; honors
	// explicit player choice by skipping ids already present in the map. Enemy
	// units are also seeded — they are AI-controlled and must use their
	// abilities (player toggles never reach them, so there is no choice to
	// preserve).
	s.seedDefaultAutoCastLocked(unit)
}

// ranksUpToInclusive returns the rank slugs from bronze through `rank` in
// promotion order. base returns nil (no grants apply at base rank).
// Unknown ranks return the full bronze→gold sequence (defensive — the loader
// already panics on unknown ranks, but a runtime caller asking for an unknown
// rank gets the maximal set rather than empty).
func ranksUpToInclusive(rank string) []string {
	out := make([]string, 0, 3)
	for _, r := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		out = append(out, r)
		if r == rank {
			return out
		}
	}
	if rank == unitRankBase {
		return nil
	}
	return out
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
