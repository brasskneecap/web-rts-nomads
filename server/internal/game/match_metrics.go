package game

// MatchMetrics tracks per-player gameplay totals during a single match. Every
// field is either a monotonically-non-decreasing counter (incremented at event
// hooks in the existing tick subsystems) or a derived map populated by the
// objective evaluator each tick.
//
// Why this exists: campaign objectives (kill_camps, build_buildings, etc.) read
// pre-aggregated totals rather than scanning units/buildings each tick. The
// shape mirrors `design.md` in the campaign-objectives-and-metrics OpenSpec
// change; do not add fields here without updating the spec.
//
// Invariants:
//   - All map fields are nil-safe to read but must NOT be written without a
//     non-nil check. Use the `Record*` methods which lazy-initialise.
//   - Counter fields only ever increase during a match.
//   - `UnitsByRank` is intentionally a derived snapshot, recomputed by the
//     objective evaluator each tick from `Unit.Rank`; event hooks elsewhere
//     do NOT mutate it directly.
type MatchMetrics struct {
	// Cumulative resource counters. Differ from Player.Resources, which is
	// current-balance (spent down by purchases). These track lifetime earned.
	TotalGoldEarned int `json:"totalGoldEarned"`
	TotalWoodEarned int `json:"totalWoodEarned"`

	// Confirmed enemy kills by this player's units / buildings / traps.
	// Excludes friendly fire; see damage_pipeline hook for the predicate.
	TotalEnemiesKilled int `json:"totalEnemiesKilled"`

	// Buildings built to completion (under-construction → done). Repairs of an
	// already-built building do NOT increment this counter.
	BuildingsBuilt       int            `json:"buildingsBuilt"`
	BuildingsBuiltByType map[string]int `json:"buildingsBuiltByType"`

	// Neutral camps fully cleared. The tier map keys on `NeutralCamp.CurrentTier`
	// at the time of clear, so a camp that tiered up before being cleared lands
	// in the higher bucket.
	NeutralCampsKilled       int         `json:"neutralCampsKilled"`
	NeutralCampsKilledByTier map[int]int `json:"neutralCampsKilledByTier"`

	// Units this player ever produced (including authored starting units and
	// upgrade-granted extras). Death does not decrement.
	UnitsTrained       int            `json:"unitsTrained"`
	UnitsTrainedByType map[string]int `json:"unitsTrainedByType"`

	// Derived map: count of currently-alive units at "this rank or higher" per
	// rank key. Recomputed by the objective evaluator each tick from s.Units.
	// Event hooks do NOT mutate this directly. Keys are unit rank strings
	// (see progression.go: "base", "bronze", "silver", "gold"); only the
	// non-base ranks are meaningful for `rank_units` objectives.
	UnitsByRank map[string]int `json:"unitsByRank"`

	// Waves this player's team has cleared (transitioned from "active" to
	// "upgrade" or "complete" in the wave state machine).
	WavesCleared int `json:"wavesCleared"`
}

// NewMatchMetrics returns a zero-value MatchMetrics with every map field
// initialised to a non-nil empty map. Use this at every player-construction
// site so the JSON snapshot serialises maps as `{}` rather than `null`.
func NewMatchMetrics() MatchMetrics {
	return MatchMetrics{
		BuildingsBuiltByType:     map[string]int{},
		NeutralCampsKilledByTier: map[int]int{},
		UnitsTrainedByType:       map[string]int{},
		UnitsByRank:              map[string]int{},
	}
}

// RecordGoldEarned banks `n` gold into the cumulative-earned counter. Negative
// or zero values are no-ops. Called from worker deposit hooks.
func (m *MatchMetrics) RecordGoldEarned(n int) {
	if n <= 0 {
		return
	}
	m.TotalGoldEarned += n
}

// RecordWoodEarned banks `n` wood into the cumulative-earned counter.
func (m *MatchMetrics) RecordWoodEarned(n int) {
	if n <= 0 {
		return
	}
	m.TotalWoodEarned += n
}

// RecordEnemyKill bumps the total-enemies-killed counter by one.
func (m *MatchMetrics) RecordEnemyKill() {
	m.TotalEnemiesKilled++
}

// RecordBuildingBuilt bumps the total + per-type counters by one. Lazy-inits
// the per-type map so callers do not need to guard against nil; existing tests
// that construct `&Player{}` directly (zero-value Metrics) remain safe.
func (m *MatchMetrics) RecordBuildingBuilt(buildingType string) {
	m.BuildingsBuilt++
	if m.BuildingsBuiltByType == nil {
		m.BuildingsBuiltByType = map[string]int{}
	}
	m.BuildingsBuiltByType[buildingType]++
}

// RecordCampKilled bumps the total + per-tier counters by one. Tier is the
// camp's `CurrentTier` at the moment of clear.
func (m *MatchMetrics) RecordCampKilled(tier int) {
	m.NeutralCampsKilled++
	if m.NeutralCampsKilledByTier == nil {
		m.NeutralCampsKilledByTier = map[int]int{}
	}
	m.NeutralCampsKilledByTier[tier]++
}

// RecordUnitTrained bumps the total + per-type counters by one. Called from
// `spawnPlayerUnitLocked` for every successfully-spawned unit owned by a
// real player (AI / neutral owners are excluded at the call site).
func (m *MatchMetrics) RecordUnitTrained(unitType string) {
	m.UnitsTrained++
	if m.UnitsTrainedByType == nil {
		m.UnitsTrainedByType = map[string]int{}
	}
	m.UnitsTrainedByType[unitType]++
}

// RecordWaveCleared bumps the waves-cleared counter by one. Called from the
// wave state-machine transitions in `tickWaveLocked`.
func (m *MatchMetrics) RecordWaveCleared() {
	m.WavesCleared++
}

// recordResourceEarnedMetricLocked dispatches a deposit to the right
// MatchMetrics counter based on the resource type string used by Unit.
// Unknown resources are silently ignored — gold and wood are the only
// counters today; adding a third would require both a new struct field
// and an objective-handler config option, so we don't silently invent one.
//
// Lives here (not on *MatchMetrics) because the resource-string-to-counter
// mapping is the call site's concern, not the metric's.
func recordResourceEarnedMetricLocked(player *Player, resourceType string, amount int) {
	if player == nil || amount <= 0 {
		return
	}
	switch resourceType {
	case "gold":
		player.Metrics.RecordGoldEarned(amount)
	case "wood":
		player.Metrics.RecordWoodEarned(amount)
	}
}

// recordEnemyKillMetricLocked credits attackerOwnerID's TotalEnemiesKilled when
// the kill was against a hostile target. Skips:
//   - AI-owned attackers (enemyPlayerID, neutralPlayerID): no objectives read
//     their metrics, so the bookkeeping is pure waste.
//   - Friendly fire: attacker and target on the same team should not count
//     as an enemy kill regardless of the gameplay outcome.
//
// Uses the existing `playersAreHostileLocked` predicate so PvP teams credit
// correctly when they ship.
func (s *GameState) recordEnemyKillMetricLocked(attackerOwnerID, targetOwnerID string) {
	if attackerOwnerID == enemyPlayerID || attackerOwnerID == neutralPlayerID {
		return
	}
	if !s.playersAreHostileLocked(attackerOwnerID, targetOwnerID) {
		return
	}
	if player, ok := s.Players[attackerOwnerID]; ok {
		player.Metrics.RecordEnemyKill()
	}
}

// recordCampClearedMetricLocked credits every non-AI player with one cleared
// camp at the given tier. Per the design note (see state_neutral_camps.go),
// Phase 1 has a single shared team for campaign play, so crediting all human
// players is equivalent to "the team that landed the killing blow." PvP
// campaigns will need to thread the killer's TeamID through.
func (s *GameState) recordCampClearedMetricLocked(tier int) {
	for id, player := range s.Players {
		if id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		player.Metrics.RecordCampKilled(tier)
	}
}

// recordWaveClearedMetricLocked credits every non-AI player with one wave
// clear. Mirrors recordCampClearedMetricLocked: in Phase 1 every human
// player is on the same team and "the team that survived" is "all human
// players."
func (s *GameState) recordWaveClearedMetricLocked() {
	for id, player := range s.Players {
		if id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		player.Metrics.RecordWaveCleared()
	}
}

// recomputeUnitsByRankForOwnerLocked walks the owner's currently-alive units
// and rebuilds the UnitsByRank map under the "at-or-above" semantic. A silver
// unit increments bronze and silver buckets; a gold unit increments all three.
// Called on rank-up and on death (so a previously-counted unit drops out when
// killed before its objective marks complete).
//
// Cost: O(len(s.Units)) per call. Tolerable because rank-ups and ranked-unit
// deaths are discrete events, not per-tick scans.
func (s *GameState) recomputeUnitsByRankForOwnerLocked(ownerID string) {
	if ownerID == enemyPlayerID || ownerID == neutralPlayerID {
		return
	}
	player, ok := s.Players[ownerID]
	if !ok {
		return
	}
	bronze, silver, gold := 0, 0, 0
	for _, u := range s.Units {
		if u == nil || u.OwnerID != ownerID || u.HP <= 0 {
			continue
		}
		switch u.Rank {
		case unitRankBronze:
			bronze++
		case unitRankSilver:
			bronze++
			silver++
		case unitRankGold:
			bronze++
			silver++
			gold++
		}
	}
	if player.Metrics.UnitsByRank == nil {
		player.Metrics.UnitsByRank = map[string]int{}
	}
	player.Metrics.UnitsByRank[unitRankBronze] = bronze
	player.Metrics.UnitsByRank[unitRankSilver] = silver
	player.Metrics.UnitsByRank[unitRankGold] = gold
}
