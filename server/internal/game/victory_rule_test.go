package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// installObjective is a small helper for the victory-rule tests: builds an
// objectiveRuntime from a typed config and appends it to s.Objectives.
// Mirrors how SetCampaignLevelLocked would have installed it from JSON.
func installObjective(t *testing.T, s *GameState, id, typeKey, scope string, required bool, cfg any) {
	t.Helper()
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:       id,
		Type:     typeKey,
		Scope:    ObjectiveScope(scope),
		Required: required,
		Config:   rawCfg,
	})
	s.Objectives = append(s.Objectives, newObjectiveRuntime(def))
}

// TestVictoryRule_RequiredObjectiveBlocksUntilWavesComplete is the integration
// test required by §9.4. A level with one required `survive_waves` objective
// and one optional `kill_camps` objective verifies the AND-gate:
//
//   - Waves not complete → no victory (regardless of objective state).
//   - Waves complete + required survive_waves complete → victory.
//   - Optional kill_camps in any state (complete or not) NEVER affects victory.
//
// This is the load-bearing scenario for Phase 1: campaign players survive 10
// waves to win, and any optional bonuses they pick up along the way are
// tracked but never block the win.
func TestVictoryRule_RequiredObjectiveBlocksUntilWavesComplete(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}

	installObjective(t, s, "survive_to_wave_3", "survive_waves", "team", true,
		surviveWavesConfig{WavesToSurvive: 3})
	installObjective(t, s, "clear_some_camps", "kill_camps", "team", false,
		killCampsConfig{Count: 5})

	// Tick 0 — wave still in prep, no waves cleared, no camps cleared.
	s.WaveManager.State = "prep"
	s.evaluateObjectivesLocked()
	s.checkVictoryLocked()
	if s.victoryAchieved {
		t.Fatal("victory should NOT fire before waves complete")
	}

	// Mid-match — 3 waves cleared (so the required objective could complete
	// on its own), but wave manager is still ticking. Optional camps left
	// at zero.
	for i := 0; i < 3; i++ {
		s.Players["p1"].Metrics.RecordWaveCleared()
	}
	s.WaveManager.State = "active"
	s.evaluateObjectivesLocked()
	s.checkVictoryLocked()
	if s.victoryAchieved {
		t.Fatal("victory should NOT fire while waves are still active, even with required objective met")
	}

	// Wave manager flips to "complete" — both gates now satisfied.
	s.WaveManager.State = "complete"
	s.evaluateObjectivesLocked()
	s.checkVictoryLocked()
	if !s.victoryAchieved {
		t.Fatal("victory should fire when waves complete AND required objective is met")
	}
	if !s.Objectives[0].TeamState.Completed {
		t.Errorf("required objective should be marked completed")
	}
	if s.Objectives[1].TeamState.Completed {
		t.Errorf("optional kill_camps should NOT be completed (we never cleared a camp)")
	}
}

// TestVictoryRule_OptionalIncompleteDoesNotBlock locks in that an optional
// objective that's never completed has zero effect on victory. Paranoid
// guard: a regression that treated all objectives as required would still
// pass the test above because both are complete at wave-complete time. This
// test forces the difference by leaving the optional incomplete.
func TestVictoryRule_OptionalIncompleteDoesNotBlock(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}

	installObjective(t, s, "survive_to_wave_3", "survive_waves", "team", true,
		surviveWavesConfig{WavesToSurvive: 3})
	installObjective(t, s, "clear_some_camps", "kill_camps", "team", false,
		killCampsConfig{Count: 100}) // unreachable count → never completes

	for i := 0; i < 3; i++ {
		s.Players["p1"].Metrics.RecordWaveCleared()
	}
	s.WaveManager.State = "complete"
	s.evaluateObjectivesLocked()
	s.checkVictoryLocked()
	if !s.victoryAchieved {
		t.Fatal("victory should fire when required objective is met, even if optional is incomplete")
	}
}

// TestVictoryRule_RequiredIncompleteBlocksAfterWaves verifies the AND-gate
// in the opposite direction: if waves complete but a required objective is
// still incomplete (which can happen if the required objective is something
// other than survive_waves), victory is blocked.
func TestVictoryRule_RequiredIncompleteBlocksAfterWaves(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}

	// Required objective that has nothing to do with waves — needs 5 camps.
	installObjective(t, s, "must_clear_camps", "kill_camps", "team", true,
		killCampsConfig{Count: 5})

	// Waves complete but no camps killed.
	s.WaveManager.State = "complete"
	s.evaluateObjectivesLocked()
	s.checkVictoryLocked()
	if s.victoryAchieved {
		t.Fatal("victory should be blocked while required objective is incomplete")
	}

	// Clear 5 camps; required now satisfied.
	for i := 0; i < 5; i++ {
		s.Players["p1"].Metrics.RecordCampKilled(1)
	}
	s.evaluateObjectivesLocked()
	s.checkVictoryLocked()
	if !s.victoryAchieved {
		t.Fatal("victory should fire once both gates are met")
	}
}

// TestVictoryRule_CustomGameNoObjectivesUnaffected verifies that a match with
// zero objectives (Custom Game) wins on the legacy wave rule alone — the
// AND-gate degenerates to "AND true" when there are no required objectives.
func TestVictoryRule_CustomGameNoObjectivesUnaffected(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}
	// s.Objectives is left empty/nil.

	s.WaveManager.State = "complete"
	s.checkVictoryLocked()
	if !s.victoryAchieved {
		t.Fatal("Custom Game (no objectives) should win on waves complete alone")
	}
}

// TestVictoryRule_VictoryIsSticky guards the absorbing invariant on
// victoryAchieved. Once set, no later state change can clear it.
func TestVictoryRule_VictoryIsSticky(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.WaveManager.State = "complete"
	s.checkVictoryLocked()
	if !s.victoryAchieved {
		t.Fatal("precondition: should have won on the first check")
	}

	// Pretend the wave manager somehow flipped backward (it cannot in
	// production, but the invariant matters).
	s.WaveManager.State = "active"
	s.checkVictoryLocked()
	if !s.victoryAchieved {
		t.Fatal("victoryAchieved must stay true once set")
	}
}

// Ensure the protocol import is still referenced (kept for future test
// additions; remove this anchor if the package import disappears).
var _ = protocol.MapConfig{}
