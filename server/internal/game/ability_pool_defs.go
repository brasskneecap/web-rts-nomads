package game

// ability_pool_defs.go owns abilityPoolFor, the read path for the data-driven
// ability pools (arch-mage-spell-system, §11). A pool is the set of abilities an
// archetype MAY be granted at a given rank; exactly one is rolled per unit at
// promotion (ability_pool_roll.go).
//
// Pools are authored directly on the promotion path's JSON file
// (PathDef.AbilityPoolsByRank, see path_defs.go) — there is no longer a
// separate standalone catalog file for ability pools. The archetype key passed
// in here is the promotion-path id (e.g. "arch_mage"), so the roll can look a
// pool up straight from unit.ProgressionPath.

// abilityPoolFor returns the candidate ability ids an archetype may roll at
// `rank`. Each rank's pool is self-contained — no cumulative union across
// ranks. A path that wants Silver to offer the same candidates as Bronze
// authors the same list under both rank keys (see arch_mage.json); the
// no-duplicate logic in ability_pool_roll.go still ensures a unit is never
// granted the same ability twice even when two ranks share a pool.
//
// Returns a freshly built copy (nil-safe, see pathAbilityPoolsForRank); nil
// when the path has no authored pool for that rank.
func abilityPoolFor(archetype, rank string) []string {
	return pathAbilityPoolsForRank(archetype, rank)
}
