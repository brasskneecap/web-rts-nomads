package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// distanceBetween returns the Euclidean distance between two world-space points.
func distanceBetween(x1, y1, x2, y2 float64) float64 {
	dx := x1 - x2
	dy := y1 - y2
	return math.Sqrt(dx*dx + dy*dy)
}

// spawnPointCenter returns the world-space center of a spawn-point building.
func spawnPointCenter(b *protocol.BuildingTile, cellSize float64) (float64, float64) {
	cx := (float64(b.X) + float64(b.Width)/2) * cellSize
	cy := (float64(b.Y) + float64(b.Height)/2) * cellSize
	return cx, cy
}

// TestExtraStartingUnits_SpawnsNearSpawnPoint verifies that the
// additional_worker profile upgrade at rank 2 spawns two workers anchored on
// the player's spawn-point — i.e. each new worker is closer (or equal) to
// the spawn-point center than to the townhall center.
func TestExtraStartingUnits_SpawnsNearSpawnPoint(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)

	// Baseline: count workers spawned without the upgrade so we isolate the
	// two extras granted by additional_worker rank 2.
	s.EnsurePlayer("baseline")
	s.mu.RLock()
	var baselineCount int
	for _, u := range s.Units {
		if u.OwnerID == "baseline" && u.UnitType == "worker" {
			baselineCount++
		}
	}
	s.mu.RUnlock()

	s.EnsurePlayerWithUpgrades("p1", map[string]int{"additional_worker": 2}, nil, nil)

	s.mu.RLock()
	defer s.mu.RUnlock()

	sp := s.findPlayerSpawnPointLocked("p1")
	if sp == nil {
		t.Fatal("expected p1 to have an associated spawn-point on the default map")
	}
	spX, spY := spawnPointCenter(sp, s.MapConfig.CellSize)

	// Locate p1's townhall to compare distances.
	var townhall *protocol.BuildingTile
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || b.OwnerID == nil || *b.OwnerID != "p1" {
			continue
		}
		townhall = b
		break
	}
	if townhall == nil {
		t.Fatal("expected p1 to have a claimed townhall")
	}
	thX := (float64(townhall.X) + float64(townhall.Width)/2) * s.MapConfig.CellSize
	thY := (float64(townhall.Y) + float64(townhall.Height)/2) * s.MapConfig.CellSize

	var p1Workers []*Unit
	for _, u := range s.Units {
		if u.OwnerID == "p1" && u.UnitType == "worker" {
			p1Workers = append(p1Workers, u)
		}
	}
	if got, want := len(p1Workers), baselineCount+2; got != want {
		t.Fatalf("p1 worker count: want %d (baseline %d + 2 extra), got %d", want, baselineCount, got)
	}

	// At least the two newest workers (highest IDs) should be the upgrade-
	// granted ones, since spawnUnitsForPlayerAtSpawnPointLocked runs after
	// authored spawns. Confirm they are closer (or equal) to the spawn-point
	// than to the townhall.
	for i := 0; i < len(p1Workers); i++ {
		for j := i + 1; j < len(p1Workers); j++ {
			if p1Workers[j].ID > p1Workers[i].ID {
				p1Workers[i], p1Workers[j] = p1Workers[j], p1Workers[i]
			}
		}
	}
	for i := 0; i < 2 && i < len(p1Workers); i++ {
		w := p1Workers[i]
		dSP := distanceBetween(w.X, w.Y, spX, spY)
		dTH := distanceBetween(w.X, w.Y, thX, thY)
		if dSP > dTH {
			t.Errorf("extra worker id=%d at (%.1f, %.1f) is closer to townhall (d=%.1f) than to spawn-point (d=%.1f); upgrade should anchor on spawn-point",
				w.ID, w.X, w.Y, dTH, dSP)
		}
	}
}

// TestExtraStartingUnits_NoSpawnPointWarnsAndSkips verifies that a player who
// joins a map with no spawn-points at all gets no extra unit and the existing
// units count is unchanged. The strict requirement is: skip + warn, no
// townhall fallback.
func TestExtraStartingUnits_NoSpawnPointWarnsAndSkips(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)

	// Strip all spawn-points from the map config so no player has one.
	filtered := make([]protocol.BuildingTile, 0, len(s.MapConfig.Buildings))
	for _, b := range s.MapConfig.Buildings {
		if b.BuildingType == "spawn-point" {
			continue
		}
		filtered = append(filtered, b)
	}
	s.mu.Lock()
	s.MapConfig.Buildings = filtered
	s.invalidateBlockedCellsLocked()
	s.mu.Unlock()

	// Baseline count for a player WITHOUT the upgrade.
	s.EnsurePlayer("baseline")
	s.mu.RLock()
	var baselineCount int
	for _, u := range s.Units {
		if u.OwnerID == "baseline" {
			baselineCount++
		}
	}
	s.mu.RUnlock()

	s.EnsurePlayerWithUpgrades("p1", map[string]int{"additional_worker": 2}, nil, nil)

	s.mu.RLock()
	var p1Count int
	for _, u := range s.Units {
		if u.OwnerID == "p1" {
			p1Count++
		}
	}
	if sp := s.findPlayerSpawnPointLocked("p1"); sp != nil {
		t.Fatalf("test setup: expected p1 to have no spawn-point after stripping; got %s", sp.ID)
	}
	s.mu.RUnlock()

	if p1Count != baselineCount {
		t.Errorf("with no spawn-point, additional_worker must spawn nothing: want %d units (matching baseline), got %d",
			baselineCount, p1Count)
	}
}

// TestWaveUpgrade_SpawnUnit_PicksSpawnUnitAtSpawnPoint verifies the
// spawn_soldier_rare and spawn_archer_rare wave upgrades load correctly,
// dispatch to the shared helper, and produce one new owned unit per pick.
// Re-picking does not increment UpgradeStacks (because the upgrades are
// `unlimited: true`).
func TestWaveUpgrade_SpawnUnit_PicksSpawnUnitAtSpawnPoint(t *testing.T) {
	for _, tc := range []struct {
		upgradeID string
		unitType  string
	}{
		{"spawn_soldier_rare", "soldier"},
		{"spawn_archer_rare", "archer"},
	} {
		t.Run(tc.upgradeID, func(t *testing.T) {
			def, ok := getUpgradeDef(tc.upgradeID)
			if !ok {
				t.Fatalf("upgrade %q missing from catalog", tc.upgradeID)
			}
			if def.Effect.Type != upgradeEffectTypeSpawnUnit {
				t.Errorf("upgrade %q effect type: want %q, got %q", tc.upgradeID, upgradeEffectTypeSpawnUnit, def.Effect.Type)
			}
			if def.Effect.UnitType != tc.unitType {
				t.Errorf("upgrade %q unitType: want %q, got %q", tc.upgradeID, tc.unitType, def.Effect.UnitType)
			}
			if !def.Unlimited {
				t.Errorf("upgrade %q must be unlimited:true", tc.upgradeID)
			}

			s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
			s.EnsurePlayer("p1")

			s.mu.Lock()
			defer s.mu.Unlock()

			before := 0
			for _, u := range s.Units {
				if u.OwnerID == "p1" && u.UnitType == tc.unitType {
					before++
				}
			}

			s.applyUpgradeLocked("p1", tc.upgradeID, 0)
			after := 0
			for _, u := range s.Units {
				if u.OwnerID == "p1" && u.UnitType == tc.unitType {
					after++
				}
			}
			if got := after - before; got != 1 {
				t.Errorf("first pick: expected +1 %s, got %+d", tc.unitType, got)
			}

			// Re-pick: unlimited upgrades do not increment the stack counter.
			stacksBefore := s.Players["p1"].UpgradeState.UpgradeStacks[def.Group]
			s.applyUpgradeLocked("p1", tc.upgradeID, 0)
			stacksAfter := s.Players["p1"].UpgradeState.UpgradeStacks[def.Group]
			if stacksAfter != stacksBefore {
				t.Errorf("unlimited upgrade should not increment UpgradeStacks: before=%d after=%d", stacksBefore, stacksAfter)
			}

			after2 := 0
			for _, u := range s.Units {
				if u.OwnerID == "p1" && u.UnitType == tc.unitType {
					after2++
				}
			}
			if got := after2 - after; got != 1 {
				t.Errorf("second pick: expected +1 more %s, got %+d", tc.unitType, got)
			}
		})
	}
}
