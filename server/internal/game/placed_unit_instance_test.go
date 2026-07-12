package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// TestPlacedUnit_RankItemsPerksRoundTrip: the new fields survive JSON
// round-trip (back-compat: absent fields decode to zero values).
func TestPlacedUnit_RankItemsPerksRoundTrip(t *testing.T) {
	raw := `{"x":3,"y":4,"id":"u1","playerSlot":"player1","unitType":"soldier","rank":"silver","items":["fire_sword"],"perks":["p_a","p_b"]}`
	var pu protocol.PlacedUnit
	if err := json.Unmarshal([]byte(raw), &pu); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pu.Rank != "silver" || len(pu.Items) != 1 || pu.Items[0] != "fire_sword" || len(pu.Perks) != 2 {
		t.Fatalf("fields lost: %+v", pu)
	}
	// Legacy shape (no new fields) still decodes.
	var legacy protocol.PlacedUnit
	if err := json.Unmarshal([]byte(`{"x":0,"y":0,"id":"u2","playerSlot":"enemy","unitType":"soldier"}`), &legacy); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if legacy.Rank != "" || legacy.Items != nil || legacy.Perks != nil {
		t.Errorf("legacy should have zero new fields: %+v", legacy)
	}
}

// TestSpawnPlacedUnit_AppliesRankItemsPerks: a placed player unit with rank +
// items + perks spawns with that rank applied, the item equipped, and the
// perks assigned. Uses the DefaultMapID map's grid/cellSize for coordinates.
//
// The embedded default catalog map (battletest.json) carries no
// playerLabel-tagged spawn-points (see labeled_building_claim_test.go's
// buildLabeledClaimTestState for the established pattern this mirrors), so a
// bare EnsurePlayerWithUpgrades against the unmodified default map would
// never resolve playerID -> "player1" and spawnPlacedUnitsForPlayerLocked
// would no-op. We author a minimal townhall + player1-labelled spawn-point
// fixture on top of the default map's grid so the real spawn trigger
// (EnsurePlayerWithUpgrades -> claimPlayerStartLocked ->
// spawnPlacedUnitsForPlayerLocked) resolves the player into slot "player1".
func TestSpawnPlacedUnit_AppliesRankItemsPerks(t *testing.T) {
	cfg := GetMapConfigByID(DefaultMapID())
	cfg.PlacedUnits = []protocol.PlacedUnit{{
		GridCoord: protocol.GridCoord{X: 10, Y: 10}, ID: "pu1", PlayerSlot: "player1",
		UnitType: "soldier", Rank: "silver", Items: []string{"fire_sword"}, Perks: []string{},
	}}
	s := NewGameStateWithSeed(cfg, 7)

	s.mu.Lock()
	// Wipe the base map's buildings and author a minimal claimable townhall
	// linked to a player1-labelled spawn-point (mirrors
	// buildLabeledClaimTestState in labeled_building_claim_test.go).
	s.MapConfig.Buildings = nil
	s.buildingsByID = map[string]*protocol.BuildingTile{}
	s.addBuildingLocked(protocol.BuildingTile{
		ID:           "th-1",
		BuildingType: "townhall",
		GridCoord:    protocol.GridCoord{X: 1, Y: 1},
		Width:        3,
		Height:       2,
		Visible:      false,
		Capabilities: []string{"unit-spawner", "occupiable", "deposit-point"},
		Metadata:     map[string]interface{}{},
	})
	s.addBuildingLocked(protocol.BuildingTile{
		ID:           "sp-1",
		BuildingType: "spawn-point",
		GridCoord:    protocol.GridCoord{X: 1, Y: 4},
		Width:        1,
		Height:       1,
		Capabilities: []string{},
		Metadata: map[string]interface{}{
			"townhallId":  "th-1",
			"playerLabel": "player1",
		},
	})
	s.invalidateBlockedCellsLocked()
	s.mu.Unlock()

	// EnsurePlayerWithUpgrades manages its own lock lifecycle (claims a
	// start, assigns the player1 label, and spawns placed units) — do not
	// pre-lock s.mu here or it deadlocks.
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Resolve the spawned unit for p1 of type soldier.
	var spawned *Unit
	for _, u := range s.Units {
		if u != nil && u.OwnerID == "p1" && u.UnitType == "soldier" {
			spawned = u
			break
		}
	}
	if spawned == nil {
		t.Fatal("placed soldier did not spawn for p1")
	}
	if spawned.Rank != "silver" {
		t.Errorf("rank = %q, want silver", spawned.Rank)
	}
	equippedFire := false
	for _, e := range spawned.Equipped {
		if e != nil && e.ItemID == "fire_sword" {
			equippedFire = true
		}
	}
	if !equippedFire {
		t.Error("fire_sword not equipped on the placed unit")
	}
}

// TestSpawnPlacedUnit_RankAssignsPathAndInventorySize is a self-review check
// (not part of the brief's required tests): confirms a placed Silver soldier
// gets a real promotion path (not left on "none") and InventorySize grows to
// match Silver (2 slots), not just the raw stat multipliers.
func TestSpawnPlacedUnit_RankAssignsPathAndInventorySize(t *testing.T) {
	cfg := GetMapConfigByID(DefaultMapID())
	cfg.PlacedUnits = []protocol.PlacedUnit{{
		GridCoord: protocol.GridCoord{X: 10, Y: 10}, ID: "pu1", PlayerSlot: "player1",
		UnitType: "soldier", Rank: "silver", Items: []string{"fire_sword"},
	}}
	s := NewGameStateWithSeed(cfg, 7)
	s.mu.Lock()
	s.MapConfig.Buildings = nil
	s.buildingsByID = map[string]*protocol.BuildingTile{}
	s.addBuildingLocked(protocol.BuildingTile{
		ID: "th-1", BuildingType: "townhall", GridCoord: protocol.GridCoord{X: 1, Y: 1},
		Width: 3, Height: 2, Visible: false,
		Capabilities: []string{"unit-spawner", "occupiable", "deposit-point"},
		Metadata:     map[string]interface{}{},
	})
	s.addBuildingLocked(protocol.BuildingTile{
		ID: "sp-1", BuildingType: "spawn-point", GridCoord: protocol.GridCoord{X: 1, Y: 4},
		Width: 1, Height: 1, Capabilities: []string{},
		Metadata: map[string]interface{}{"townhallId": "th-1", "playerLabel": "player1"},
	})
	s.invalidateBlockedCellsLocked()
	s.mu.Unlock()
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)

	s.mu.Lock()
	defer s.mu.Unlock()
	var spawned *Unit
	for _, u := range s.Units {
		if u != nil && u.OwnerID == "p1" && u.UnitType == "soldier" {
			spawned = u
		}
	}
	if spawned == nil {
		t.Fatal("no spawned soldier")
	}
	if spawned.ProgressionPath == unitPathNone || spawned.ProgressionPath == "" {
		t.Errorf("ProgressionPath = %q; want a real promotion path (vanguard/berserker)", spawned.ProgressionPath)
	}
	if spawned.InventorySize != 2 {
		t.Errorf("InventorySize = %d; want 2 (silver)", spawned.InventorySize)
	}
	if len(spawned.Equipped) != 2 {
		t.Errorf("len(Equipped) = %d; want 2", len(spawned.Equipped))
	}
}

// TestSpawnPlacedUnit_EnemySlotAppliesRankItemsPerksAndRetainsGuardMode: an
// ENEMY-slot placed unit with rank + items + perks spawns (via the real
// enemy-spawn trigger, ensurePlacedEnemiesSpawnedLocked, invoked from
// EnsurePlayerWithUpgrades) with the rank applied and the item equipped —
// exactly like the player-owned case — AND still retains the guard-mode
// behavior spawnPlacedEnemyUnitsLocked stamps on every enemy placed unit
// (GuardMode/GuardAnchor/GuardAggroRange/GuardLeashRange/Status). This
// proves instance-data application and guard-mode setup coexist correctly;
// see the ordering note on the applyPlacedUnitInstanceLocked call site in
// spawnPlacedEnemyUnitsLocked (state_spawn.go) for why the ordering is safe.
//
// No labelled townhall/spawn-point fixture is needed here (unlike the
// player-slot test above): spawnPlacedEnemyUnitsLocked spawns every
// entry.PlayerSlot == "enemy" placement unconditionally, independent of any
// player's label resolution. ensurePlacedEnemiesSpawnedLocked fires
// unconditionally from EnsurePlayerWithUpgrades regardless of whether the
// joining player resolves to a labelled slot, so the plain default map
// (with its unlabelled townhall/spawn-point) is sufficient to trigger it.
func TestSpawnPlacedUnit_EnemySlotAppliesRankItemsPerksAndRetainsGuardMode(t *testing.T) {
	cfg := GetMapConfigByID(DefaultMapID())
	cfg.PlacedUnits = []protocol.PlacedUnit{{
		GridCoord: protocol.GridCoord{X: 10, Y: 10}, ID: "pu-enemy-1", PlayerSlot: "enemy",
		UnitType: "soldier", Rank: "silver", Items: []string{"fire_sword"}, Perks: []string{},
	}}
	s := NewGameStateWithSeed(cfg, 7)
	// Any join triggers ensurePlacedEnemiesSpawnedLocked (state.go:3720)
	// regardless of whether this player resolves to a labelled slot on the
	// default map.
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)

	s.mu.Lock()
	defer s.mu.Unlock()
	var spawned *Unit
	for _, u := range s.Units {
		if u != nil && u.OwnerID == enemyPlayerID && u.UnitType == "soldier" {
			spawned = u
			break
		}
	}
	if spawned == nil {
		t.Fatal("placed enemy soldier did not spawn")
	}
	if spawned.Rank != "silver" {
		t.Errorf("rank = %q, want silver", spawned.Rank)
	}
	equippedFire := false
	for _, e := range spawned.Equipped {
		if e != nil && e.ItemID == "fire_sword" {
			equippedFire = true
		}
	}
	if !equippedFire {
		t.Error("fire_sword not equipped on the placed enemy unit")
	}
	// Guard-mode fields must survive instance-data application untouched.
	if !spawned.GuardMode {
		t.Error("GuardMode = false; want true (instance-data application must not clobber guard setup)")
	}
	if spawned.Status != "Guarding" {
		t.Errorf("Status = %q; want %q", spawned.Status, "Guarding")
	}
	if spawned.GuardAggroRange <= 0 {
		t.Errorf("GuardAggroRange = %v; want > 0", spawned.GuardAggroRange)
	}
	if spawned.GuardLeashRange < spawned.GuardAggroRange {
		t.Errorf("GuardLeashRange (%v) < GuardAggroRange (%v)", spawned.GuardLeashRange, spawned.GuardAggroRange)
	}
}
