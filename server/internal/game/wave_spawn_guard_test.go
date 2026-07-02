package game

import "testing"

// Regression tests for the premature final-wave completion bug: a clear-the-
// field wave (discrete waves, and the FINAL wave of a bounded continuous map)
// was declared "complete" after only minActiveSeconds of empty field — before
// its spawnpoints (which commonly carry spawn delays) had placed a single
// enemy. In the field this ended a forest-1 match six seconds into wave 10.
//
// The fix: the clear condition additionally requires that at least one wave-
// gated enemy has spawned this wave (WaveManager.SpawnedThisWave > 0), or
// that the wave's spawn window has closed (Timer >= WaveDuration) so a wave
// with no spawners at all still terminates.

func waveGuardTestState() *GameState {
	return &GameState{Players: map[string]*Player{"p1": continuousTestPlayer("p1")}}
}

// The final wave must NOT complete on an empty field before any of its
// spawns have fired, no matter how long past minActiveSeconds it has been.
func TestWave_FinalWaveNotClearedBeforeSpawnsFire(t *testing.T) {
	s := waveGuardTestState()
	s.WaveManager = WaveManager{
		Enabled:      true,
		State:        "active",
		CurrentWave:  3,
		TotalWaves:   3,
		WaveDuration: 120,
	}

	// 10 simulated seconds — well past the 5s minimum — with zero enemies on
	// the field and zero spawns fired.
	for i := 0; i < 200; i++ {
		s.tickWaveLocked(0.05)
	}

	if s.WaveManager.State == "complete" {
		t.Fatal("final wave completed before any wave spawn fired")
	}
}

// Once a wave-gated enemy HAS spawned (and subsequently died — field empty),
// the clear proceeds as before.
func TestWave_FinalWaveClearsAfterSpawnsFiredAndFieldEmpty(t *testing.T) {
	s := waveGuardTestState()
	s.WaveManager = WaveManager{
		Enabled:      true,
		State:        "active",
		CurrentWave:  3,
		TotalWaves:   3,
		WaveDuration: 120,
		Timer:        6, // past minActiveSeconds
	}
	s.WaveManager.SpawnedThisWave = 1

	s.tickWaveLocked(0.05)

	if s.WaveManager.State != "complete" {
		t.Fatalf("final wave should complete once spawns fired and field is empty; state=%q", s.WaveManager.State)
	}
	if s.Players["p1"].Metrics.WavesCleared != 1 {
		t.Errorf("wave clear should credit the metric, got %d", s.Players["p1"].Metrics.WavesCleared)
	}
}

// A final wave whose spawnpoints never fire (misconfigured map) must still
// terminate once the spawn window closes — otherwise the match soft-locks.
func TestWave_FinalWaveClearsOnClosedSpawnWindowWithoutSpawns(t *testing.T) {
	s := waveGuardTestState()
	s.WaveManager = WaveManager{
		Enabled:      true,
		State:        "active",
		CurrentWave:  3,
		TotalWaves:   3,
		WaveDuration: 120,
		Timer:        120, // spawn window closed
	}

	s.tickWaveLocked(0.05)

	if s.WaveManager.State != "complete" {
		t.Fatalf("spawnless final wave should complete once the spawn window closes; state=%q", s.WaveManager.State)
	}
}

// The per-wave spawn counter resets when a new wave activates (prep → active)
// so a prior wave's spawns can't satisfy the next wave's guard.
func TestWave_SpawnedThisWaveResetsOnActivation(t *testing.T) {
	s := waveGuardTestState()
	s.EnemySpawnTimers = map[string]*EnemySpawnTimer{}
	s.WaveManager = WaveManager{
		Enabled:      true,
		State:        "prep",
		CurrentWave:  1,
		TotalWaves:   3,
		WaveDuration: 120,
		Timer:        0.01,
	}
	s.WaveManager.SpawnedThisWave = 7

	s.tickWaveLocked(0.05) // prep timer expires → wave 2 activates

	if s.WaveManager.State != "active" || s.WaveManager.CurrentWave != 2 {
		t.Fatalf("setup: expected wave 2 active, got wave %d %q", s.WaveManager.CurrentWave, s.WaveManager.State)
	}
	if s.WaveManager.SpawnedThisWave != 0 {
		t.Errorf("SpawnedThisWave should reset on wave activation, got %d", s.WaveManager.SpawnedThisWave)
	}
}
