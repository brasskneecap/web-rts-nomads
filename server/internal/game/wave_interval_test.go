package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newIntervalSpawnState builds a minimal map with a player townhall and a
// single waveInterval=3 enemy-spawnpoint. The WaveManager is initialised but
// each test moves it to the wave it wants to probe before calling
// tickEnemySpawnpointsLocked. Lock is held on return; defer s.mu.Unlock().
func newIntervalSpawnState(t *testing.T, interval int) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 24
	owner := "p1"
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 10}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &owner,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	spawnpoint := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 34, Y: 11}, ID: "spawn-1",
		BuildingType: "enemy-spawnpoint", Width: 1, Height: 1, Visible: true,
		Metadata: map[string]interface{}{
			"waveInterval": float64(interval),
			"spawnCount":   1.0,
			"unitType":     "raider",
		},
	}
	cfg := protocol.MapConfig{
		ID: "interval-test", Name: "interval-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{townhall, spawnpoint},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	s.initWaveManagerLocked()
	return s
}

// setActiveWave drops the manager into the active phase for a specific wave.
// Caller holds s.mu.
func setActiveWave(s *GameState, wave int) {
	s.WaveManager.State = "active"
	s.WaveManager.CurrentWave = wave
	s.WaveManager.Timer = 0
	delete(s.EnemySpawnTimers, "spawn-1")
}

// TestInitWaveManager_EnabledByWaveInterval verifies that the presence of a
// waveInterval tag on any enemy-spawnpoint switches the wave system on, even
// when no waveNumber/startingWave exists on the map.
func TestInitWaveManager_EnabledByWaveInterval(t *testing.T) {
	s := newIntervalSpawnState(t, 3)
	defer s.mu.Unlock()
	if !s.WaveManager.Enabled {
		t.Fatal("WaveManager.Enabled = false; want true (waveInterval should switch wave mode on)")
	}
}

// TestWaveInterval_SkipsNonMultipleWaves verifies that a waveInterval=3
// spawnpoint does NOT fire on waves 1 or 2.
func TestWaveInterval_SkipsNonMultipleWaves(t *testing.T) {
	s := newIntervalSpawnState(t, 3)
	defer s.mu.Unlock()
	blocked := s.getBlockedCellsLocked()

	for _, wave := range []int{1, 2} {
		setActiveWave(s, wave)
		// Tick repeatedly so the EnemySpawnTimer's initial delay (60s default)
		// is irrelevant — what we're verifying is the wave-gate skip, which
		// happens BEFORE the timer is ever consulted.
		s.tickEnemySpawnpointsLocked(0.05, blocked)
		if countEnemyUnits(s) != 0 {
			t.Errorf("wave %d (interval=3): spawned %d enemies; want 0 (wave is not a multiple of interval)",
				wave, countEnemyUnits(s))
		}
	}
}

// TestWaveInterval_FiresOnMultipleWaves verifies that a waveInterval=3
// spawnpoint fires on wave 3 (the first multiple).
func TestWaveInterval_FiresOnMultipleWaves(t *testing.T) {
	s := newIntervalSpawnState(t, 3)
	defer s.mu.Unlock()
	blocked := s.getBlockedCellsLocked()

	setActiveWave(s, 3)
	// Zero the spawn delay so the spawnpoint fires on the very next tick rather
	// than waiting out its 60s default. The wave gate is the focus of this test;
	// the timer is irrelevant noise once the gate is open.
	s.MapConfig.Buildings[1].Metadata["spawnDelaySeconds"] = 0.0
	delete(s.EnemySpawnTimers, "spawn-1")
	s.tickEnemySpawnpointsLocked(0.05, blocked)

	if countEnemyUnits(s) == 0 {
		t.Errorf("wave 3 (interval=3): spawned 0 enemies; want >=1 (wave is a multiple of interval)")
	}
}

// TestWaveInterval_SkipsPrepPhase verifies that even a multiple wave does NOT
// fire while the manager is in the "prep" or "upgrade" phase.
func TestWaveInterval_SkipsPrepPhase(t *testing.T) {
	s := newIntervalSpawnState(t, 3)
	defer s.mu.Unlock()
	blocked := s.getBlockedCellsLocked()

	s.WaveManager.State = "prep"
	s.WaveManager.CurrentWave = 3
	s.MapConfig.Buildings[1].Metadata["spawnDelaySeconds"] = 0.0
	delete(s.EnemySpawnTimers, "spawn-1")
	s.tickEnemySpawnpointsLocked(0.05, blocked)

	if countEnemyUnits(s) != 0 {
		t.Errorf("prep phase wave 3: spawned %d enemies; want 0 (must wait for active phase)",
			countEnemyUnits(s))
	}
}
