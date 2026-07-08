package game

import "sort"

// spell_pool_roll.go owns the ONE-TIME random spell assignment from an
// archetype's spell pool (arch-mage-spell-system §11).
//
// The design deliberately splits "roll" from "recompute":
//
//   - rollUnitPoolSpellsLocked (here) draws from the seeded rngPerks stream and
//     RECORDS the pick on unit.PoolSpellsByRank. It runs once per rank at
//     rank-up (progression.go) and from DebugSpawnUnit.
//   - assignUnitPathAbilitiesLocked (path_ability_defs.go) READS the recorded
//     pick and appends it to unit.Abilities. That recompute stays idempotent
//     and RNG-free.
//
// This mirrors how the promotion PATH is a one-time roll recorded on
// unit.ProgressionPath and then read wherever it is needed.

// rollUnitPoolSpellsLocked ensures every rank the unit has reached has a
// recorded spell-pool pick (where a non-empty pool exists). Idempotent: a rank
// already recorded is never re-rolled, so it is safe to call on every rank-up
// and from DebugSpawnUnit regardless of how many ranks were crossed. Ranks are
// processed bronze→silver→gold so a later rank's roll excludes an earlier
// rank's pick (no duplicate known spells).
//
// Caller holds s.mu.
func (s *GameState) rollUnitPoolSpellsLocked(unit *Unit) {
	if unit == nil || unit.ProgressionPath == unitPathNone || unit.Rank == unitRankBase {
		return
	}
	for _, rank := range ranksUpToInclusive(unit.Rank) {
		s.rollUnitPoolSpellForRankLocked(unit, rank)
	}
}

// rollUnitPoolSpellForRankLocked rolls a single spell for one rank and records
// it. No-op when the rank is already recorded, the pool is empty, or every pool
// candidate is already known by the unit. The roll draws exactly once from
// rngPerks when (and only when) it records a pick — so an empty/exhausted pool
// consumes no RNG and does not perturb the deterministic stream.
//
// Caller holds s.mu.
func (s *GameState) rollUnitPoolSpellForRankLocked(unit *Unit, rank string) {
	if _, done := unit.PoolSpellsByRank[rank]; done {
		return
	}
	pool := spellPoolFor(unit.ProgressionPath, rank)
	if len(pool) == 0 {
		return
	}
	// Candidates = pool minus already-known spells. Sorted so neither map nor
	// pool iteration order can drive the outcome (determinism invariant).
	known := s.unitKnownSpellSetLocked(unit)
	candidates := make([]string, 0, len(pool))
	for _, id := range pool {
		if !known[id] {
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		return // whole pool already known — nothing to grant, no RNG drawn
	}
	sort.Strings(candidates)
	// rngPerks.Float64() ∈ [0,1) ⇒ index ∈ [0,len-1]; no clamp needed.
	pick := candidates[int(s.rngPerks.Float64()*float64(len(candidates)))]
	if unit.PoolSpellsByRank == nil {
		unit.PoolSpellsByRank = make(map[string]string, 1)
	}
	unit.PoolSpellsByRank[rank] = pick
}

// spellSlotRankForAbilityLocked returns the rank at which `abilityID` was
// learned as a spell-slot spell (the unit's PoolSpellsByRank entry equal to it),
// or "" when the ability is not a learned slot spell. This is what marks an
// ability as a "spell slot" for the client's perk-cell rendering — the generic
// spell-slot system is realized entirely by the pool + PoolSpellsByRank, no
// separate slot registry. Caller holds s.mu.
func spellSlotRankForAbilityLocked(unit *Unit, abilityID string) string {
	if unit == nil || abilityID == "" {
		return ""
	}
	for _, rank := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		if unit.PoolSpellsByRank[rank] == abilityID {
			return rank
		}
	}
	return ""
}

// unitKnownSpellSetLocked is the set of ability ids the unit already knows for
// the purpose of no-duplicate pool rolls: its current abilities plus every
// previously-recorded pool pick.
//
// Caller holds s.mu.
func (s *GameState) unitKnownSpellSetLocked(unit *Unit) map[string]bool {
	known := make(map[string]bool, len(unit.Abilities)+len(unit.PoolSpellsByRank))
	for _, id := range unit.Abilities {
		known[id] = true
	}
	for _, id := range unit.PoolSpellsByRank {
		known[id] = true
	}
	return known
}
