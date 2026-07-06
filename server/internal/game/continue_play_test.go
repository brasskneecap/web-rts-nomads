package game

import (
	"testing"
	"time"

	"webrts/server/pkg/protocol"
)

// TestIsSimulationHalted_ContinuePlayMatrix pins the core decoupling: victory
// in a match WITH a required objective keeps the sim live, while victory in a
// match without one (and any defeat) halts it.
func TestIsSimulationHalted_ContinuePlayMatrix(t *testing.T) {
	t.Run("victory with required objective keeps simulating", func(t *testing.T) {
		s := &GameState{Players: map[string]*Player{}}
		installObjective(t, s, "req", "survive_waves", "team", true,
			surviveWavesConfig{WavesToSurvive: 1})
		s.victoryAchieved = true
		if s.IsSimulationHalted() {
			t.Fatal("continue-play match must keep simulating after victory")
		}
		if !s.IsGameOver() {
			t.Fatal("victory should still register as game-over (win banked)")
		}
	})

	t.Run("victory without required objective halts", func(t *testing.T) {
		s := &GameState{Players: map[string]*Player{}}
		installObjective(t, s, "opt", "kill_camps", "team", false,
			killCampsConfig{Count: 1})
		s.victoryAchieved = true
		if !s.IsSimulationHalted() {
			t.Fatal("non-continue match must freeze on victory")
		}
	})

	t.Run("defeat halts even a continue-play match", func(t *testing.T) {
		s := &GameState{Players: map[string]*Player{}}
		installObjective(t, s, "req", "survive_waves", "team", true,
			surviveWavesConfig{WavesToSurvive: 1})
		s.lostPlayerIDs = map[string]bool{"p1": true}
		if !s.IsSimulationHalted() {
			t.Fatal("a defeated match must halt regardless of required objectives")
		}
	})
}

// TestCheckPlayerLoss_UnlosableAfterVictory verifies the "match unlosable"
// choice: once a continue-play match is won, losing every townhall does NOT
// mark the player as lost.
func TestCheckPlayerLoss_UnlosableAfterVictory(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}
	installObjective(t, s, "req", "survive_waves", "team", true,
		surviveWavesConfig{WavesToSurvive: 1})

	// p1 owned a townhall, then lost it — MapConfig has no townhall buildings,
	// so the normal path would flag p1 as lost this tick.
	s.playersWithTownhall = map[string]bool{"p1": true}
	s.lostPlayerIDs = map[string]bool{}
	s.victoryAchieved = true

	s.checkPlayerLossLocked()

	if s.lostPlayerIDs["p1"] {
		t.Fatal("continue-play match must be unlosable after victory: p1 was marked lost")
	}
}

// TestLoop_ContinuePlayKeepsSimulatingAfterVictory is the regression for the
// reported bug: after clicking "Continue Playing" the units stayed frozen. The
// loop must keep advancing the tick counter (i.e. keep calling Update) once a
// continue-play match has been won.
func TestLoop_ContinuePlayKeepsSimulatingAfterVictory(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	installObjective(t, s, "req", "survive_waves", "team", true,
		surviveWavesConfig{WavesToSurvive: 1})
	s.mu.Lock()
	s.Objectives[0].TeamState.Completed = true
	s.victoryAchieved = true
	s.WaveManager.State = "complete" // avoid the upgrade-phase early return
	startTick := s.Tick
	s.mu.Unlock()

	l := NewLoop(s, &countingBroadcaster{})
	l.Start()
	defer l.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.RLock()
		cur := s.Tick
		s.mu.RUnlock()
		if cur >= startTick+3 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.RLock()
	endTick := s.Tick
	s.mu.RUnlock()
	if endTick < startTick+3 {
		t.Fatalf("sim froze after victory in continue-play match: tick advanced %d -> %d", startTick, endTick)
	}
}

// TestLoop_NonContinueVictoryFreezesSim guards the unchanged path: a match with
// no required objectives still freezes the sim on victory (Update stops) while
// continuing to broadcast the frozen state.
func TestLoop_NonContinueVictoryFreezesSim(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	s.WaveManager.State = "complete"
	s.victoryAchieved = true // no required objectives installed -> not continue-eligible
	s.mu.Unlock()

	b := &countingBroadcaster{}
	l := NewLoop(s, b)
	l.Start()
	defer l.Stop()

	// Give it several ticks worth of wall-clock.
	deadline := time.Now().Add(2 * time.Second)
	for b.n.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.RLock()
	tick := s.Tick
	s.mu.RUnlock()
	// Exactly one Update runs (the game-over tick) before the sim halts.
	if tick > 1 {
		t.Fatalf("non-continue victory must freeze the sim, but tick advanced to %d", tick)
	}
	if b.n.Load() < 5 {
		t.Fatalf("loop must keep broadcasting the frozen state, got %d broadcasts", b.n.Load())
	}
}

// TestContinuePlay_PostVictoryKillsStillEarnDP locks in that kill-drop DP is NOT
// gated by victory: once a continue-play match is won and the player keeps
// playing, killing enemies still rolls dominion-point drops. The sim running is
// what makes this reachable in production (see the loop test above); this guards
// the earning logic itself against a future victory gate.
func TestContinuePlay_PostVictoryKillsStillEarnDP(t *testing.T) {
	s, playerID := newLPTestState(t)

	s.mu.Lock()
	defer s.mu.Unlock()
	installObjective(t, s, "req", "survive_waves", "team", true,
		surviveWavesConfig{WavesToSurvive: 1})
	s.victoryAchieved = true // match already won → continue-play eligible

	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 100, Y: 100})
	before := s.Players[playerID].MatchDominionPointsEarned
	// MatchDominionPointsEarned is the always-on earned total (mode-independent).
	for i := 0; i < 1000; i++ {
		s.rollDominionPointDropLocked(playerID, enemy)
	}
	after := s.Players[playerID].MatchDominionPointsEarned

	if after <= before {
		t.Fatalf("post-victory kills must still earn DP; MatchDominionPointsEarned %d -> %d", before, after)
	}
}

// TestContinuePlay_PostVictoryObjectiveCompletionEvaluated locks in that
// objective evaluation is NOT gated by victory: an OPTIONAL objective completed
// during the victory lap still flips to Completed, so its first-time-ever DP
// reward is credited at exit (the client POSTs the live completed-objective set).
func TestContinuePlay_PostVictoryObjectiveCompletionEvaluated(t *testing.T) {
	s := &GameState{Players: map[string]*Player{}}
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics()}

	installObjective(t, s, "req", "survive_waves", "team", true,
		surviveWavesConfig{WavesToSurvive: 1})
	installObjective(t, s, "opt_camps", "kill_camps", "team", false,
		killCampsConfig{Count: 1})
	s.victoryAchieved = true // required objective already satisfied → match won

	// Player clears a neutral camp AFTER victory during the continue-play lap.
	s.Players["p1"].Metrics.NeutralCampsKilled = 1

	s.evaluateObjectivesLocked()

	if !s.Objectives[1].TeamState.Completed {
		t.Fatal("optional objective must be completable after victory so continue-play earns objective DP")
	}
}
