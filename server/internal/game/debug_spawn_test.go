package game

// Regression test for the debug spawner applying advancement buffs.
//
// DebugSpawnUnit must route through spawnPlayerUnitLocked so a debug-spawned
// unit picks up the owning player's advancement-effective UnitDef, exactly like
// a naturally-trained unit. A prior version called spawnUnitFromDefLocked with
// the raw catalog def, so debug-spawned archers received NO advancement buffs.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// findOwnedUnit returns the first unit of unitType owned by playerID, or nil.
func (s *GameState) findOwnedUnitLocked(playerID, unitType string) *Unit {
	for _, u := range s.Units {
		if u != nil && u.OwnerID == playerID && u.UnitType == unitType {
			return u
		}
	}
	return nil
}

func TestDebugSpawn_AppliesArcherAdvancements(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}

	advancements := []string{"archer_hp_1", masterHuntsmanID}
	wantHP := catalogDef.HP + statAddAmount(t, advancements, "maxHp")
	wantArrows := nodeEffectAmount(t, masterHuntsmanID, "unitBonusArrows")

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 31)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, advancements, nil)

	// Enable the debug-spawn gate (battletest maps set this in their JSON).
	s.mu.Lock()
	s.MapConfig.Debug = &protocol.MapDebugConfig{DebugSpawn: true}
	s.mu.Unlock()

	ok = s.DebugSpawnUnit(protocol.DebugSpawnUnitMessage{
		Type:     "debug_spawn_unit",
		UnitType: "archer",
		Team:     "mine",
		X:        400,
		Y:        400,
	}, "p1")
	if !ok {
		t.Fatal("DebugSpawnUnit returned false — spawn did not land")
	}

	s.mu.Lock()
	archer := s.findOwnedUnitLocked("p1", "archer")
	s.mu.Unlock()
	if archer == nil {
		t.Fatal("debug-spawned archer not found for p1")
	}

	// HP advancement (archer_hp_1) must be baked into the spawned unit.
	if archer.MaxHP != wantHP {
		t.Errorf("debug-spawned archer MaxHP = %d, want %d (catalog %d + advancement) — advancement buffs not applied",
			archer.MaxHP, wantHP, catalogDef.HP)
	}
	// Master Huntsman bonus arrows must also be present (item-8 capstone).
	if archer.BonusArrows != wantArrows {
		t.Errorf("debug-spawned archer BonusArrows = %d, want %d", archer.BonusArrows, wantArrows)
	}
}

// TestDebugSpawn_SpawnRank_SizesInventory guards that a unit spawned directly at
// a rank (not by earning XP) receives that rank's full inventory. Inventory size
// must follow rank STATE, not the rank-up event — otherwise a Gold debug spawn
// keeps the base-rank single slot. Regression for the "3 slots on gold" report.
func TestDebugSpawn_SpawnRank_SizesInventory(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}

	cases := []struct {
		rank     string
		wantSize int
	}{
		{unitRankBronze, 1},
		{unitRankSilver, 2},
		{unitRankGold, 3},
	}

	for _, tc := range cases {
		t.Run(tc.rank, func(t *testing.T) {
			s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 33)
			s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)

			s.mu.Lock()
			s.MapConfig.Debug = &protocol.MapDebugConfig{DebugSpawn: true}
			s.mu.Unlock()

			if !s.DebugSpawnUnit(protocol.DebugSpawnUnitMessage{
				Type: "debug_spawn_unit", UnitType: "archer", Team: "mine",
				Rank: tc.rank, X: 400, Y: 400,
			}, "p1") {
				t.Fatal("DebugSpawnUnit returned false")
			}

			s.mu.Lock()
			u := s.findOwnedUnitLocked("p1", "archer")
			s.mu.Unlock()
			if u == nil {
				t.Fatal("debug-spawned archer not found")
			}
			if u.InventorySize != tc.wantSize {
				t.Errorf("%s debug archer InventorySize = %d, want %d", tc.rank, u.InventorySize, tc.wantSize)
			}
			if len(u.Equipped) != tc.wantSize {
				t.Errorf("%s debug archer len(Equipped) = %d, want %d", tc.rank, len(u.Equipped), tc.wantSize)
			}
		})
	}
}

// TestDebugSpawn_EnemyTeam_NoAdvancements is the companion guard: an enemy-team
// debug spawn (no advancements on the NPC player) gets stock catalog stats, so
// the routing change doesn't accidentally buff enemy units.
func TestDebugSpawn_EnemyTeam_NoAdvancements(t *testing.T) {
	catalogDef, ok := getUnitDef("archer")
	if !ok {
		t.Skip("archer not in unit catalog")
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 32)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, []string{"archer_hp_1", masterHuntsmanID}, nil)

	s.mu.Lock()
	s.MapConfig.Debug = &protocol.MapDebugConfig{DebugSpawn: true}
	s.mu.Unlock()

	if !s.DebugSpawnUnit(protocol.DebugSpawnUnitMessage{
		Type: "debug_spawn_unit", UnitType: "archer", Team: "enemy", X: 800, Y: 800,
	}, "p1") {
		t.Fatal("enemy DebugSpawnUnit returned false")
	}

	s.mu.Lock()
	enemy := s.findOwnedUnitLocked(enemyPlayerID, "archer")
	s.mu.Unlock()
	if enemy == nil {
		t.Fatal("debug-spawned enemy archer not found")
	}
	if enemy.MaxHP != catalogDef.HP {
		t.Errorf("enemy archer MaxHP = %d, want %d (raw catalog — p1's advancements must not leak)", enemy.MaxHP, catalogDef.HP)
	}
	if enemy.BonusArrows != 0 {
		t.Errorf("enemy archer BonusArrows = %d, want 0", enemy.BonusArrows)
	}
}
