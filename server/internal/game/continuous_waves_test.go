package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// continuousTestPlayer returns a fully-initialised human player so the upgrade
// phase + wave-clear metrics don't trip over nil maps.
func continuousTestPlayer(id string) *Player {
	return &Player{
		ID:           id,
		Resources:    map[string]int{},
		Upgrades:     map[UpgradeTrack]int{},
		UpgradeState: newPlayerUpgradeState(1, 3),
		Metrics:      NewMatchMetrics(),
	}
}

// TestEnemiesFightNeutrals_HostilityToggle: the per-map EnemiesFightNeutrals
// flag gates ONLY the enemy↔neutral pair. Default (no WaveConfig) ⇒ they ignore
// each other; enemy/neutral stay hostile to players in both states.
func TestEnemiesFightNeutrals_HostilityToggle(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)

	// Default: off ⇒ enemy and neutral ignore each other.
	if s.playersAreHostileLocked(enemyPlayerID, neutralPlayerID) {
		t.Error("default: enemy↔neutral should NOT be hostile")
	}
	if s.playersAreHostileLocked(neutralPlayerID, enemyPlayerID) {
		t.Error("default: neutral↔enemy should NOT be hostile (symmetric)")
	}
	// Still hostile to players regardless of the toggle.
	if !s.playersAreHostileLocked(enemyPlayerID, "p1") {
		t.Error("enemy should always be hostile to a player")
	}
	if !s.playersAreHostileLocked(neutralPlayerID, "p1") {
		t.Error("neutral should always be hostile to a player")
	}

	// Toggle on ⇒ they fight.
	s.MapConfig.WaveConfig = &protocol.WaveConfig{EnemiesFightNeutrals: true}
	if !s.playersAreHostileLocked(enemyPlayerID, neutralPlayerID) {
		t.Error("toggle on: enemy↔neutral should be hostile")
	}
	if !s.playersAreHostileLocked(neutralPlayerID, enemyPlayerID) {
		t.Error("toggle on: neutral↔enemy should be hostile (symmetric)")
	}
}

// TestNeutralCamp_Continuous_PersistsAndResetsAtWaveEnd: in continuous mode camps
// stay on the field through an active wave and are reset (fresh roster, new unit
// IDs) at the end of the wave — the active→upgrade transition — same as discrete.
func TestNeutralCamp_Continuous_PersistsAndResetsAtWaveEnd(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.WaveManager.Continuous = true

	// Initial spawn during prep.
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]
	if len(camp.AliveUnitIDs) == 0 {
		t.Fatal("setup: expected initial spawn during prep")
	}

	// Wave 1 active: camp must remain Active with a roster (no mid-wave reset).
	s.WaveManager.CurrentWave = 1
	s.WaveManager.State = "active"
	s.tickNeutralCampsLocked()
	if len(camp.AliveUnitIDs) == 0 || camp.State != NeutralCampActive {
		t.Fatalf("continuous: camp should persist active during a wave; len=%d state=%v",
			len(camp.AliveUnitIDs), camp.State)
	}
	wave1IDs := append([]int(nil), camp.AliveUnitIDs...)

	// Wave 1 ends (continuous active→upgrade) → reset: the wave-1 units are gone,
	// replaced by a fresh roster.
	s.WaveManager.State = "upgrade"
	s.tickNeutralCampsLocked()
	if len(camp.AliveUnitIDs) == 0 {
		t.Fatal("continuous: camp should have a fresh roster after the wave-end reset")
	}
	for _, oldID := range wave1IDs {
		if s.getUnitByIDLocked(oldID) != nil {
			t.Errorf("continuous: wave-1 unit %d should be removed on the wave-end reset", oldID)
		}
	}
}

// TestContinuousWaves_AdvancesOnTimerWithEnemiesAlive: in continuous mode a
// non-final active wave advances to "upgrade" purely on the WaveDuration timer —
// it does NOT wait for the field to clear, so enemies persist and accumulate.
func TestContinuousWaves_AdvancesOnTimerWithEnemiesAlive(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.ensureEnemyPlayerLocked()
	s.Players["p1"] = continuousTestPlayer("p1")
	s.WaveManager = WaveManager{
		Enabled: true, Continuous: true, CurrentWave: 1, TotalWaves: 0,
		State: "active", Timer: 0, WaveDuration: 10, PrepDuration: 60,
	}
	// An enemy on the field so the wave can never "clear."
	if s.spawnPlayerUnitLocked("soldier", enemyPlayerID, enemyPlayerColor, protocol.Vec2{X: 300, Y: 300}) == nil {
		t.Fatal("setup: failed to spawn enemy unit")
	}

	for i := 0; i < 400 && s.WaveManager.State == "active"; i++ {
		s.tickWaveLocked(0.05)
	}
	if s.WaveManager.State != "upgrade" {
		t.Fatalf("continuous active should advance to upgrade on the timer; state=%q", s.WaveManager.State)
	}
	if s.countEnemyUnitsLocked() == 0 {
		t.Fatal("advance must be timer-driven, not clear-driven — enemy should still be alive")
	}
}

// TestContinuousWaves_UpgradeResolutionAdvancesWave: resolving the upgrade phase
// in continuous mode releases the next wave (active, CurrentWave++), instead of
// returning to a prep countdown.
func TestContinuousWaves_UpgradeResolutionAdvancesWave(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.Players["p1"] = continuousTestPlayer("p1")
	s.WaveManager = WaveManager{Enabled: true, Continuous: true, CurrentWave: 1, TotalWaves: 0, State: "upgrade"}
	s.Players["p1"].UpgradeState.Resolved = true

	s.tickUpgradePhaseLocked()

	if s.WaveManager.State != "active" {
		t.Fatalf("continuous upgrade resolution should go to active; state=%q", s.WaveManager.State)
	}
	if s.WaveManager.CurrentWave != 2 {
		t.Errorf("continuous upgrade resolution should bump CurrentWave to 2; got %d", s.WaveManager.CurrentWave)
	}
}

// TestContinuousWaves_BoundedFinalWaveClearsToComplete: a bounded continuous map
// (TotalWaves>0) reverts to clear-to-win on its final wave.
func TestContinuousWaves_BoundedFinalWaveClearsToComplete(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.ensureEnemyPlayerLocked()
	s.Players["p1"] = continuousTestPlayer("p1")
	s.WaveManager = WaveManager{
		Enabled: true, Continuous: true, CurrentWave: 2, TotalWaves: 2,
		State: "active", Timer: 0, WaveDuration: 10,
	}
	// No enemies on the field → the final wave clears once minActive elapses.
	for i := 0; i < 400 && s.WaveManager.State == "active"; i++ {
		s.tickWaveLocked(0.05)
	}
	if s.WaveManager.State != "complete" {
		t.Fatalf("bounded continuous final wave should clear to complete; state=%q", s.WaveManager.State)
	}
}

// TestContinuousWaves_EndlessNeverCompletesViaWaves: an endless continuous map
// (TotalWaves==0) timer-advances forever and never reaches "complete."
func TestContinuousWaves_EndlessNeverCompletesViaWaves(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	s.Players["p1"] = continuousTestPlayer("p1")
	s.WaveManager = WaveManager{
		Enabled: true, Continuous: true, CurrentWave: 1, TotalWaves: 0,
		State: "active", Timer: 0, WaveDuration: 5,
	}
	for i := 0; i < 200 && s.WaveManager.State == "active"; i++ {
		s.tickWaveLocked(0.05)
	}
	if s.WaveManager.State == "complete" {
		t.Fatal("endless continuous map must never reach complete via waves")
	}
	if s.WaveManager.State != "upgrade" {
		t.Fatalf("endless continuous active should advance to upgrade; state=%q", s.WaveManager.State)
	}
}

// TestContinuousWaves_EndlessVictoryGate: the wave victory gate is satisfied for
// endless continuous maps (objectives drive victory) but still requires
// "complete" for bounded continuous maps.
func TestContinuousWaves_EndlessVictoryGate(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)

	s.WaveManager = WaveManager{Enabled: true, Continuous: true, TotalWaves: 0, State: "active"}
	if !s.waveOrTownhallConditionMetLocked() {
		t.Error("endless continuous map should satisfy the wave gate without 'complete'")
	}

	s.WaveManager = WaveManager{Enabled: true, Continuous: true, TotalWaves: 3, State: "active"}
	if s.waveOrTownhallConditionMetLocked() {
		t.Error("bounded continuous map should NOT satisfy the gate until 'complete'")
	}
	s.WaveManager.State = "complete"
	if !s.waveOrTownhallConditionMetLocked() {
		t.Error("bounded continuous map should satisfy the gate at 'complete'")
	}
}

// TestNeutralCamp_EnemyWipeDropsNoLoot: markCampKillerLocked records the killing
// faction, and a camp whose final unit was killed by an enemy drops no loot.
func TestNeutralCamp_EnemyWipeDropsNoLoot(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	camp := &s.NeutralCamps[0]
	camp.GroupID = "small_raider_group"
	camp.CurrentTier = 1
	s.spawnGroupForCampLocked(camp)
	if len(camp.AliveUnitIDs) == 0 {
		t.Fatal("setup: expected camp spawn")
	}

	// Flag logic: a player killer leaves it false, an enemy killer sets it true.
	s.markCampKillerLocked(camp.PlacementID, "p1")
	if camp.LastKillerWasEnemy {
		t.Error("player killer must leave LastKillerWasEnemy false")
	}
	s.markCampKillerLocked(camp.PlacementID, enemyPlayerID)
	if !camp.LastKillerWasEnemy {
		t.Error("enemy killer must set LastKillerWasEnemy true")
	}

	// Drive to the final unit (intermediate removals: camp still has units → no loot).
	for len(camp.AliveUnitIDs) > 1 {
		s.removeUnitLocked(camp.AliveUnitIDs[0])
	}
	// Mirror the pipeline: stamp the enemy killer, then remove the final unit.
	s.markCampKillerLocked(camp.PlacementID, enemyPlayerID)
	before := len(s.LootDrops)
	s.removeUnitLocked(camp.AliveUnitIDs[0])

	if len(camp.AliveUnitIDs) != 0 {
		t.Fatalf("camp should be empty after the final removal; got %d", len(camp.AliveUnitIDs))
	}
	if len(s.LootDrops) != before {
		t.Errorf("enemy-wiped camp must drop no loot; LootDrops %d → %d", before, len(s.LootDrops))
	}
}
