package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newTwoPlayerTargetedSpawnState builds a minimal two-base map that mirrors
// forest-1's shape: two labeled townhalls (player1/player2), each linked to a
// spawn-point via an explicit townhallId, plus one enemy-spawnpoint that
// targets player2. The WaveManager is initialised and dropped into wave 1
// active. Lock is held on return; defer s.mu.Unlock().
func newTwoPlayerTargetedSpawnState(t *testing.T) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 60, 24
	p1, p2 := "p1", "p2"
	townhall1 := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 10}, ID: "townhall-p1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &p1,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	townhall2 := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 54, Y: 10}, ID: "townhall-p2",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &p2,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	spawnPoint1 := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 4, Y: 12}, ID: "spawn-point-p1",
		BuildingType: "spawn-point", Width: 1, Height: 1,
		Metadata: map[string]interface{}{"playerLabel": "player1", "townhallId": "townhall-p1"},
	}
	spawnPoint2 := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 52, Y: 12}, ID: "spawn-point-p2",
		BuildingType: "spawn-point", Width: 1, Height: 1,
		Metadata: map[string]interface{}{"playerLabel": "player2", "townhallId": "townhall-p2"},
	}
	// Enemy spawnpoint dedicated to player2's base (startingWave 1, no delay).
	enemySpawn := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 30, Y: 11}, ID: "enemy-spawn-p2",
		BuildingType: "enemy-spawnpoint", Width: 1, Height: 1, Visible: true,
		Metadata: map[string]interface{}{
			"startingWave":      1.0,
			"spawnDelaySeconds": 0.0,
			"spawnCount":        1.0,
			"unitType":          "raider",
			"targetPlayerLabel": "player2",
		},
	}
	cfg := protocol.MapConfig{
		ID: "two-player-target-test", Name: "two-player-target-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{townhall1, townhall2, spawnPoint1, spawnPoint2, enemySpawn},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	s.initWaveManagerLocked()
	s.WaveManager.State = "active"
	s.WaveManager.CurrentWave = 1
	s.WaveManager.Timer = 0
	return s
}

// A player2-targeted enemy spawnpoint must KEEP spawning after player2's
// townhall is destroyed, as long as player2 was in the match. The units
// re-route to the surviving player1 base. Regression: previously the
// spawnpoint went dormant because findPlayerIDByLabelLocked could no longer
// resolve the label once the townhall was removed from the map.
func TestWaveSpawn_TargetedSpawnerKeepsFiringAfterTargetLosesBase(t *testing.T) {
	s := newTwoPlayerTargetedSpawnState(t)
	defer s.mu.Unlock()
	blocked := s.getBlockedCellsLocked()

	// player2 joined the match (records the label as an ever-active target).
	s.joinedTargetLabels["player2"] = true

	// player2's townhall is destroyed and removed from the map (combat path).
	s.removeBuildingLocked("townhall-p2")
	if s.findPlayerIDByLabelLocked("player2") != "" {
		t.Fatal("setup: player2 label should be unresolvable after its townhall is destroyed")
	}

	s.tickEnemySpawnpointsLocked(0.05, blocked)

	if countEnemyUnits(s) == 0 {
		t.Fatal("player2-targeted spawner went dormant after player2 lost its base; " +
			"it must keep firing while any player base survives")
	}
}

// A player2-targeted enemy spawnpoint must STAY dormant when player2 never
// joined the match (2-player map played by one human). This guards the
// existing behaviour that the fix must not regress.
func TestWaveSpawn_TargetedSpawnerDormantWhenTargetNeverJoined(t *testing.T) {
	s := newTwoPlayerTargetedSpawnState(t)
	defer s.mu.Unlock()
	blocked := s.getBlockedCellsLocked()

	// player2 never joined: release their townhall (unowned/unclaimed) and do
	// NOT record the label as ever-active.
	s.releaseTownhallForPlayerLocked("p2")
	if s.findPlayerIDByLabelLocked("player2") != "" {
		t.Fatal("setup: unclaimed player2 label should resolve to empty")
	}

	// Tick well past any spawn delay.
	for i := 0; i < 40; i++ {
		s.tickEnemySpawnpointsLocked(0.05, blocked)
	}

	if countEnemyUnits(s) != 0 {
		t.Fatalf("player2-targeted spawner fired for a player that never joined; got %d enemies", countEnemyUnits(s))
	}
}
