package game

import (
	"testing"
)

// TestNewMatchMetrics_AllMapsNonNil pins the contract that newly-constructed
// metrics serialise empty maps as `{}` rather than `null`. Any future field
// addition that breaks this should fail this test loudly.
func TestNewMatchMetrics_AllMapsNonNil(t *testing.T) {
	m := NewMatchMetrics()

	if m.TotalGoldEarned != 0 || m.TotalWoodEarned != 0 {
		t.Errorf("resource counters should start at zero, got gold=%d wood=%d", m.TotalGoldEarned, m.TotalWoodEarned)
	}
	if m.TotalEnemiesKilled != 0 {
		t.Errorf("TotalEnemiesKilled should start at zero, got %d", m.TotalEnemiesKilled)
	}
	if m.BuildingsBuiltByType == nil || m.NeutralCampsKilledByTier == nil ||
		m.UnitsTrainedByType == nil || m.UnitsByRank == nil {
		t.Error("all map fields should be non-nil after NewMatchMetrics()")
	}
	if len(m.BuildingsBuiltByType) != 0 || len(m.NeutralCampsKilledByTier) != 0 ||
		len(m.UnitsTrainedByType) != 0 || len(m.UnitsByRank) != 0 {
		t.Error("all map fields should be empty after NewMatchMetrics()")
	}
}

// TestRecordMethods_LazyInit verifies that the Record* methods are safe to
// call on a zero-value MatchMetrics (no NewMatchMetrics call) — important
// because tests that hand-construct &Player{} would otherwise nil-panic.
func TestRecordMethods_LazyInit(t *testing.T) {
	var m MatchMetrics // zero value; every map is nil

	m.RecordBuildingBuilt("barracks")
	if m.BuildingsBuilt != 1 || m.BuildingsBuiltByType["barracks"] != 1 {
		t.Errorf("RecordBuildingBuilt did not lazy-init or count: total=%d byType=%v",
			m.BuildingsBuilt, m.BuildingsBuiltByType)
	}

	m.RecordCampKilled(2)
	if m.NeutralCampsKilled != 1 || m.NeutralCampsKilledByTier[2] != 1 {
		t.Errorf("RecordCampKilled did not lazy-init or count")
	}

	m.RecordUnitTrained("soldier")
	if m.UnitsTrained != 1 || m.UnitsTrainedByType["soldier"] != 1 {
		t.Errorf("RecordUnitTrained did not lazy-init or count")
	}
}

// TestRecordGoldWoodEarned_RejectsZeroAndNegative locks in that the deposit
// hook is safe even if a malformed call site somehow passes a non-positive
// amount. We don't want metrics to silently decrement.
func TestRecordGoldWoodEarned_RejectsZeroAndNegative(t *testing.T) {
	m := NewMatchMetrics()
	m.RecordGoldEarned(0)
	m.RecordGoldEarned(-5)
	m.RecordWoodEarned(0)
	m.RecordWoodEarned(-5)
	if m.TotalGoldEarned != 0 || m.TotalWoodEarned != 0 {
		t.Errorf("zero/negative deposits leaked into counters: gold=%d wood=%d",
			m.TotalGoldEarned, m.TotalWoodEarned)
	}
	m.RecordGoldEarned(7)
	m.RecordWoodEarned(3)
	if m.TotalGoldEarned != 7 || m.TotalWoodEarned != 3 {
		t.Errorf("positive deposit not recorded: gold=%d wood=%d", m.TotalGoldEarned, m.TotalWoodEarned)
	}
}

// TestRecordResourceEarnedMetricLocked_DispatchesByType verifies the helper
// routes "gold" / "wood" to their respective counters and silently drops
// unknown resource strings (rather than e.g. panicking or inventing a field).
func TestRecordResourceEarnedMetricLocked_DispatchesByType(t *testing.T) {
	p := &Player{Metrics: NewMatchMetrics()}

	recordResourceEarnedMetricLocked(p, "gold", 25)
	recordResourceEarnedMetricLocked(p, "wood", 15)
	recordResourceEarnedMetricLocked(p, "crystal", 99) // unknown — silently ignored
	recordResourceEarnedMetricLocked(p, "gold", 0)     // no-op
	recordResourceEarnedMetricLocked(nil, "gold", 10)  // nil-safe

	if p.Metrics.TotalGoldEarned != 25 {
		t.Errorf("TotalGoldEarned: want 25, got %d", p.Metrics.TotalGoldEarned)
	}
	if p.Metrics.TotalWoodEarned != 15 {
		t.Errorf("TotalWoodEarned: want 15, got %d", p.Metrics.TotalWoodEarned)
	}
}

// TestRecordEnemyKillMetricLocked_FiltersFriendlyAndAI verifies that the kill
// counter only credits real-player attackers against hostile targets. Three
// rejection paths matter for the scoreboard:
//   - AI-owned attackers (enemy / neutral) — their stats are never read for
//     objectives, so this is bookkeeping waste at best, noise at worst.
//   - Friendly fire — should never count, regardless of game-mechanic outcome.
//   - Self-fire — same player, same team; trivial subset of friendly fire.
func TestRecordEnemyKillMetricLocked_FiltersFriendlyAndAI(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", TeamID: 0, Metrics: NewMatchMetrics()}
	s.Players["p2"] = &Player{ID: "p2", TeamID: 1, Metrics: NewMatchMetrics()}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Metrics: NewMatchMetrics()}
	s.Players[neutralPlayerID] = &Player{ID: neutralPlayerID, Metrics: NewMatchMetrics()}

	// p1 (team 0) kills p2 (team 1) — hostile, credits p1.
	s.recordEnemyKillMetricLocked("p1", "p2")
	if s.Players["p1"].Metrics.TotalEnemiesKilled != 1 {
		t.Errorf("hostile kill should credit attacker, got %d", s.Players["p1"].Metrics.TotalEnemiesKilled)
	}

	// p1 kills p1 — self-fire, no credit.
	s.recordEnemyKillMetricLocked("p1", "p1")
	if s.Players["p1"].Metrics.TotalEnemiesKilled != 1 {
		t.Errorf("self-fire should not credit, got %d", s.Players["p1"].Metrics.TotalEnemiesKilled)
	}

	// enemy AI kills p1 — AI attacker, no credit (and no panic if enemy player has no metrics consumer).
	s.recordEnemyKillMetricLocked(enemyPlayerID, "p1")
	if s.Players[enemyPlayerID].Metrics.TotalEnemiesKilled != 0 {
		t.Errorf("enemy AI attacker should not get credit, got %d", s.Players[enemyPlayerID].Metrics.TotalEnemiesKilled)
	}

	// neutral camp kills p1 — neutral attacker, no credit.
	s.recordEnemyKillMetricLocked(neutralPlayerID, "p1")
	if s.Players[neutralPlayerID].Metrics.TotalEnemiesKilled != 0 {
		t.Errorf("neutral attacker should not get credit, got %d", s.Players[neutralPlayerID].Metrics.TotalEnemiesKilled)
	}

	// p1 kills enemy AI — always hostile, credits p1.
	s.recordEnemyKillMetricLocked("p1", enemyPlayerID)
	if s.Players["p1"].Metrics.TotalEnemiesKilled != 2 {
		t.Errorf("kill on enemy AI should credit, got %d", s.Players["p1"].Metrics.TotalEnemiesKilled)
	}
}

// TestRecordCampClearedMetricLocked_SkipsAIPlayers verifies that camp clears
// credit every real-player Player entry and skip the enemy/neutral entries.
// This is the Phase 1 simplification: in single-team campaign play this is
// equivalent to "credit the team that landed the killing blow."
func TestRecordCampClearedMetricLocked_SkipsAIPlayers(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}
	s.Players["p2"] = &Player{ID: "p2", Metrics: NewMatchMetrics()}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Metrics: NewMatchMetrics()}
	s.Players[neutralPlayerID] = &Player{ID: neutralPlayerID, Metrics: NewMatchMetrics()}

	s.recordCampClearedMetricLocked(2)

	for _, id := range []string{"p1", "p2"} {
		if got := s.Players[id].Metrics.NeutralCampsKilled; got != 1 {
			t.Errorf("%s NeutralCampsKilled: want 1, got %d", id, got)
		}
		if got := s.Players[id].Metrics.NeutralCampsKilledByTier[2]; got != 1 {
			t.Errorf("%s NeutralCampsKilledByTier[2]: want 1, got %d", id, got)
		}
	}
	for _, id := range []string{enemyPlayerID, neutralPlayerID} {
		if got := s.Players[id].Metrics.NeutralCampsKilled; got != 0 {
			t.Errorf("%s (AI) NeutralCampsKilled: want 0, got %d", id, got)
		}
	}
}

// TestRecordWaveClearedMetricLocked_SkipsAIPlayers mirrors the camp-clear test
// for the wave counter. Important so the AI player's stats don't drift when
// e.g. a dev tool later reads them by mistake.
func TestRecordWaveClearedMetricLocked_SkipsAIPlayers(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Metrics: NewMatchMetrics()}

	s.recordWaveClearedMetricLocked()
	s.recordWaveClearedMetricLocked()

	if s.Players["p1"].Metrics.WavesCleared != 2 {
		t.Errorf("p1 WavesCleared: want 2, got %d", s.Players["p1"].Metrics.WavesCleared)
	}
	if s.Players[enemyPlayerID].Metrics.WavesCleared != 0 {
		t.Errorf("enemy AI WavesCleared: want 0, got %d", s.Players[enemyPlayerID].Metrics.WavesCleared)
	}
}

// TestRecomputeUnitsByRank_AtOrAboveSemantic locks in the design call: a unit
// at silver counts in BOTH the bronze and silver buckets. A unit at gold
// counts in all three. The objective handler reads "UnitsByRank[bronze]" and
// gets the count of bronze-or-better, which is what "have N bronze units"
// objectives mean.
func TestRecomputeUnitsByRank_AtOrAboveSemantic(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}

	// 3 bronze, 1 silver, 1 gold.
	s.Units = []*Unit{
		{OwnerID: "p1", Rank: unitRankBase, HP: 10},
		{OwnerID: "p1", Rank: unitRankBronze, HP: 10},
		{OwnerID: "p1", Rank: unitRankBronze, HP: 10},
		{OwnerID: "p1", Rank: unitRankBronze, HP: 10},
		{OwnerID: "p1", Rank: unitRankSilver, HP: 10},
		{OwnerID: "p1", Rank: unitRankGold, HP: 10},
		// Excluded: dead unit, other-owner unit.
		{OwnerID: "p1", Rank: unitRankGold, HP: 0},
		{OwnerID: "p2", Rank: unitRankGold, HP: 10},
	}

	s.recomputeUnitsByRankForOwnerLocked("p1")
	m := s.Players["p1"].Metrics.UnitsByRank

	if m[unitRankBronze] != 5 {
		t.Errorf("bronze-or-above: want 5 (3 bronze + 1 silver + 1 gold), got %d", m[unitRankBronze])
	}
	if m[unitRankSilver] != 2 {
		t.Errorf("silver-or-above: want 2 (1 silver + 1 gold), got %d", m[unitRankSilver])
	}
	if m[unitRankGold] != 1 {
		t.Errorf("gold-or-above: want 1, got %d", m[unitRankGold])
	}
	// Base is not tracked; rank_units objective does not accept "base" as a config.
	if _, ok := m[unitRankBase]; ok {
		t.Errorf("base rank should not appear in UnitsByRank map")
	}
}

// TestRecomputeUnitsByRank_SkipsAIOwners verifies the recompute is a no-op
// for enemy/neutral owners. Their Players entries exist but no objective
// reads their metrics; calling the recompute on them would be wasted work
// AND would obscure failures elsewhere if it silently worked.
func TestRecomputeUnitsByRank_SkipsAIOwners(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Metrics: NewMatchMetrics()}
	s.Units = []*Unit{
		{OwnerID: enemyPlayerID, Rank: unitRankGold, HP: 10},
	}

	s.recomputeUnitsByRankForOwnerLocked(enemyPlayerID)

	if got := s.Players[enemyPlayerID].Metrics.UnitsByRank[unitRankGold]; got != 0 {
		t.Errorf("enemy AI UnitsByRank should not be populated, got %d gold-or-above", got)
	}
}
