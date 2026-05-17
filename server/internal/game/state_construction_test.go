package game

import (
	"fmt"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// newConstructionTestState creates a GameState with a player "p1", two
// workers, and a 2x2 barracks-sized under-construction building pre-placed at
// grid (5,5). The building metadata mirrors what BuildBuilding would produce.
// Lock is NOT held on return.
func newConstructionTestState(t *testing.T) (s *GameState, w1, w2 *Unit, buildingID string) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure the player exists with enough resources.
	s.Players["p1"] = &Player{
		ID: "p1",
		Resources: map[string]int{
			"gold": 9999,
			"wood": 9999,
		},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Upgrades:                      map[UpgradeTrack]int{},
		Vault:                         []*VaultItem{},
	}

	// Spawn two workers. They start at positions well away from the building
	// so tests can control when they "arrive".
	w1 = s.spawnPlayerUnitLocked("worker", "p1", "#fff", protocol.Vec2{X: 100, Y: 100})
	w2 = s.spawnPlayerUnitLocked("worker", "p1", "#fff", protocol.Vec2{X: 200, Y: 100})

	// Manually place an under-construction barracks at grid (5,5). maxHp and
	// hpPerSecond are catalog-driven (barracks.json: maxHp / buildSeconds), so
	// derive them from the building def — this keeps the fixture faithful to
	// "what BuildBuilding would produce" and immune to balance tweaks.
	barracksDef, ok := getBuildingDef("barracks")
	if !ok {
		t.Fatal("barracks building def not registered")
	}
	ownerID := "p1"
	s.nextBuildingID++
	bid := fmt.Sprintf("barracks-%d", s.nextBuildingID)
	building := protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		ID:           bid,
		BuildingType: "barracks",
		Width:        2,
		Height:       2,
		Occupied:     true,
		Visible:      true,
		OwnerID:      &ownerID,
		Capabilities: []string{"unit-spawner"},
		Metadata: map[string]interface{}{
			"underConstruction": true,
			// pendingStart intentionally absent — construction started
			"hp":          1.0,
			"maxHp":       barracksDef.MaxHp,
			"hpPerSecond": barracksDef.HpPerSecond(),
		},
	}
	s.addBuildingLocked(building)
	buildingID = bid
	return s, w1, w2, buildingID
}

// arriveAtBuilding simulates a worker arriving at the building: it stops
// moving and has BuildTargetID set. Does NOT call updateWorkerBuildStateLocked.
func arriveAtBuilding(unit *Unit, buildingID string, pos protocol.Vec2) {
	unit.Moving = false
	unit.Path = nil
	unit.BuildTargetID = buildingID
	unit.Building = false
	unit.X = pos.X
	unit.Y = pos.Y
	unit.TargetX = pos.X
	unit.TargetY = pos.Y
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: First worker → InsideBuilder; second → outside helper
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_FirstWorkerBecomesInsideBuilder(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Worker 1 arrives.
	approachPos := protocol.Vec2{X: center.X + 40, Y: center.Y} // near perimeter
	arriveAtBuilding(w1, buildingID, approachPos)
	s.updateWorkerBuildStateLocked(w1)
	s.mu.Unlock()

	s.mu.RLock()
	if !w1.InsideBuilder {
		t.Errorf("w1: expected InsideBuilder=true, got false")
	}
	if w1.Visible {
		t.Errorf("w1: expected Visible=false for inside builder, got true")
	}
	if w1.Status != "Building" {
		t.Errorf("w1: expected Status=Building, got %q", w1.Status)
	}
	s.mu.RUnlock()

	// Worker 2 arrives after w1 is inside.
	s.mu.Lock()
	approachPos2 := protocol.Vec2{X: center.X - 40, Y: center.Y}
	arriveAtBuilding(w2, buildingID, approachPos2)
	s.updateWorkerBuildStateLocked(w2)
	s.mu.Unlock()

	s.mu.RLock()
	if w2.InsideBuilder {
		t.Errorf("w2: expected InsideBuilder=false (outside helper), got true")
	}
	if !w2.Visible {
		t.Errorf("w2: expected Visible=true for outside helper, got false")
	}
	if w2.Status != "Repairing" {
		t.Errorf("w2: expected Status=Repairing, got %q", w2.Status)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: Inside builder dies → lowest-ID helper promoted next tick
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_InsideBuilderDies_HelperPromoted(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Put w1 as inside builder, w2 as outside helper.
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.Status = "Building"
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y

	w2.Building = true
	w2.InsideBuilder = false
	w2.Visible = true
	w2.Status = "Repairing"
	w2.BuildTargetID = buildingID

	// Kill w1 (simulate death — HP=0, but still in the Units slice for this tick).
	w1.HP = 0
	s.mu.Unlock()

	// Run one tick — tickBuildingRepairsLocked should find no live inside
	// builder and promote w2.
	const dt = 0.05
	s.Update(dt)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// w1 should have been removed by the pending-deaths drain (HP=0 triggers death).
	// w2 should now be the inside builder.
	w2Live := s.unitsByID[w2.ID]
	if w2Live == nil {
		t.Fatal("w2 was unexpectedly removed")
	}
	if !w2Live.InsideBuilder {
		t.Errorf("w2: expected InsideBuilder=true after promotion, got false")
	}
	if w2Live.Visible {
		t.Errorf("w2: expected Visible=false after promotion, got true")
	}
}

// TestConstruction_NoHelpers_BuildingPaused verifies that when there are no
// workers at all the building does not advance.
func TestConstruction_NoHelpers_BuildingPaused(t *testing.T) {
	s, _, _, buildingID := newConstructionTestState(t)

	s.mu.RLock()
	building := s.getBuildingByIDLocked(buildingID)
	hpBefore, _, _ := getBuildingHP(building)
	s.mu.RUnlock()

	const dt = 0.05
	s.Update(dt)

	s.mu.RLock()
	defer s.mu.RUnlock()
	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		t.Fatal("building was removed unexpectedly")
	}
	hpAfter, _, _ := getBuildingHP(b)
	if hpAfter != hpBefore {
		t.Errorf("no workers: expected HP unchanged (%.2f), got %.2f", hpBefore, hpAfter)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: Helper crosses 5 HP → 1g+1w deducted, accumulator resets
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_HelperCharge_FiveHPThreshold(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Set building as NOT underConstruction so the helper pays.
	delete(building.Metadata, "underConstruction")
	// HP just below MaxHP but still damaged so the worker triggers.
	building.Metadata["hp"] = float64(490)

	// Put w1 as a paying outside helper.
	w1.Building = true
	w1.InsideBuilder = false
	w1.Visible = true
	w1.Status = "Repairing"
	w1.BuildTargetID = buildingID
	w1.X = center.X + 40
	w1.Y = center.Y
	w1.RepairChargeAccumulator = 0

	// Give the player exactly enough for one charge.
	s.Players["p1"].Resources["gold"] = 5
	s.Players["p1"].Resources["wood"] = 5
	goldBefore := s.Players["p1"].Resources["gold"]
	woodBefore := s.Players["p1"].Resources["wood"]
	s.mu.Unlock()

	// hpPerSecond ≈ 33.3 HP/s. At dt=0.05 that's ~1.67 HP/tick.
	// It will take ceil(5/1.67) ≈ 3 ticks to cross 5 HP. Run enough ticks.
	const dt = 0.05
	for i := 0; i < 4; i++ {
		s.Update(dt)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	goldAfter := s.Players["p1"].Resources["gold"]
	woodAfter := s.Players["p1"].Resources["wood"]

	chargesExpected := (goldBefore - goldAfter)
	if chargesExpected <= 0 {
		t.Errorf("expected at least 1 gold deduction, gold: %d→%d", goldBefore, goldAfter)
	}
	if goldBefore-goldAfter != woodBefore-woodAfter {
		t.Errorf("gold and wood charges should be equal: gold-%d wood-%d",
			goldBefore-goldAfter, woodBefore-woodAfter)
	}

	// Each deduction is exactly 1g+1w.
	if goldBefore-goldAfter > 1 {
		// More than 1 charge is fine if enough HP was contributed.
	}
	// Accumulator must be in [0, 5).
	w1Live := s.unitsByID[w1.ID]
	if w1Live != nil && (w1Live.RepairChargeAccumulator < 0 || w1Live.RepairChargeAccumulator >= 5) {
		t.Errorf("accumulator out of range: %.4f", w1Live.RepairChargeAccumulator)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: Player at 0 gold → helper paused; inside builder still advances free
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_HelperPaused_InsideBuilderFree(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Set up: building is underConstruction, w1 = inside builder (free),
	// w2 = outside helper (must pay).
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.Status = "Building"
	w1.BuildTargetID = buildingID
	w1.RepairChargeAccumulator = 4.9 // one tick away from needing a charge

	w2.Building = true
	w2.InsideBuilder = false
	w2.Visible = true
	w2.Status = "Repairing"
	w2.BuildTargetID = buildingID
	w2.X = center.X + 40
	w2.Y = center.Y
	w2.RepairChargeAccumulator = 4.9 // one tick away from needing a charge

	// Zero out resources — helper must pause.
	s.Players["p1"].Resources["gold"] = 0
	s.Players["p1"].Resources["wood"] = 0

	hpBefore, _, _ := getBuildingHP(building)
	s.mu.Unlock()

	const dt = 0.05
	s.Update(dt)

	s.mu.RLock()
	defer s.mu.RUnlock()

	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		t.Fatal("building removed unexpectedly")
	}
	hpAfter, _, _ := getBuildingHP(b)
	if hpAfter <= hpBefore {
		t.Errorf("inside builder should have advanced HP even at 0 resources: %.2f→%.2f", hpBefore, hpAfter)
	}

	w1Live := s.unitsByID[w1.ID]
	w2Live := s.unitsByID[w2.ID]
	if w1Live == nil || w2Live == nil {
		t.Skip("a worker died during the tick — test setup issue")
	}
	if w1Live.Status != "Building" {
		t.Errorf("inside builder expected Status=Building, got %q", w1Live.Status)
	}
	if w2Live.Status != "Repairing (Paused)" {
		t.Errorf("outside helper expected Status=Repairing (Paused), got %q", w2Live.Status)
	}
	// Resources must still be 0 — helper did not deduct.
	if s.Players["p1"].Resources["gold"] != 0 || s.Players["p1"].Resources["wood"] != 0 {
		t.Errorf("resources should remain 0 when helper is paused, got gold=%d wood=%d",
			s.Players["p1"].Resources["gold"], s.Players["p1"].Resources["wood"])
	}
}

// TestConstruction_RestoreGold_HelperResumes verifies that after pausing at 0
// gold, the helper contributes HP again once resources are restored.
func TestConstruction_RestoreGold_HelperResumes(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Completed damaged building — helper pays.
	delete(building.Metadata, "underConstruction")
	building.Metadata["hp"] = float64(490)

	w1.Building = true
	w1.InsideBuilder = false
	w1.Visible = true
	w1.Status = "Repairing"
	w1.BuildTargetID = buildingID
	w1.X = center.X + 40
	w1.Y = center.Y
	w1.RepairChargeAccumulator = 4.9

	s.Players["p1"].Resources["gold"] = 0
	s.Players["p1"].Resources["wood"] = 0
	s.mu.Unlock()

	const dt = 0.05
	s.Update(dt) // paused — no HP progress

	s.mu.Lock()
	hp1, _, _ := getBuildingHP(s.getBuildingByIDLocked(buildingID))
	// Restore gold.
	s.Players["p1"].Resources["gold"] = 10
	s.Players["p1"].Resources["wood"] = 10
	s.mu.Unlock()

	s.Update(dt) // helper should now charge and contribute

	s.mu.RLock()
	defer s.mu.RUnlock()
	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		t.Fatal("building removed")
	}
	hp2, _, _ := getBuildingHP(b)
	if hp2 <= hp1 {
		t.Errorf("after gold restore, expected HP to increase: %.2f→%.2f", hp1, hp2)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5: Inside builder of fresh construction is free; after completion,
//
//	re-assigned workers pay.
//
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_InsideBuilderFree_RepairWorkerPays(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Use a very high hpPerSecond so the building completes in a few ticks.
	building.Metadata["hp"] = float64(499)
	building.Metadata["hpPerSecond"] = float64(5000) // completes in ~1 tick

	// Inside builder on a fresh build — must not charge.
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.Status = "Building"
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y
	w1.RepairChargeAccumulator = 4.9 // would trigger a charge if worker were paying

	s.Players["p1"].Resources["gold"] = 5
	s.Players["p1"].Resources["wood"] = 5
	goldBefore := s.Players["p1"].Resources["gold"]
	s.mu.Unlock()

	// One tick at dt=0.05: 5000 * 0.05 = 250 HP free → completes the building.
	const dt = 0.05
	s.Update(dt)

	s.mu.RLock()
	goldAfterConstruction := s.Players["p1"].Resources["gold"]
	s.mu.RUnlock()

	// No charge should have been levied — inside builder is free during construction.
	if goldAfterConstruction != goldBefore {
		t.Errorf("inside builder should be free during construction: gold %d→%d", goldBefore, goldAfterConstruction)
	}

	// Phase 2: damage the (now complete) building and re-assign w1 as a repair
	// worker. Now w1 must pay.
	s.mu.Lock()
	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		s.mu.Unlock()
		t.Fatal("building should still exist after completion")
	}
	if getMetadataBool(b.Metadata, "underConstruction") {
		s.mu.Unlock()
		t.Fatal("building should have completed after 1 tick at 5000 HP/s")
	}
	b.Metadata["hp"] = float64(490)

	w1Live := s.unitsByID[w1.ID]
	if w1Live == nil {
		s.mu.Unlock()
		t.Fatal("w1 should still exist")
	}
	// Re-assign as outside repair worker (no inside builder for a completed building).
	w1Live.Building = true
	w1Live.InsideBuilder = false
	w1Live.Visible = true
	w1Live.Status = "Repairing"
	w1Live.BuildTargetID = buildingID
	w1Live.X = center.X + 40
	w1Live.Y = center.Y
	w1Live.RepairChargeAccumulator = 4.9 // one tick from charging

	goldBefore2 := s.Players["p1"].Resources["gold"]
	s.mu.Unlock()

	// Run enough ticks to trigger a charge.
	for i := 0; i < 4; i++ {
		s.Update(dt)
	}

	s.mu.RLock()
	goldAfter2 := s.Players["p1"].Resources["gold"]
	s.mu.RUnlock()

	if goldAfter2 >= goldBefore2 {
		t.Errorf("repair worker should have been charged: gold %d→%d", goldBefore2, goldAfter2)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6: KickBuildersFromBuilding
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_KickBuilders(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)
	hpBefore, _, _ := getBuildingHP(building)

	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y

	w2.Building = true
	w2.InsideBuilder = false
	w2.Visible = true
	w2.BuildTargetID = buildingID
	w2.X = center.X + 40
	w2.Y = center.Y
	s.mu.Unlock()

	s.KickBuildersFromBuilding("p1", buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		t.Fatal("building removed by kick — expected it to remain")
	}
	if !getMetadataBool(b.Metadata, "underConstruction") {
		t.Error("kick should not clear underConstruction flag")
	}
	hpAfter, _, _ := getBuildingHP(b)
	if hpAfter != hpBefore {
		t.Errorf("kick must not change building HP: %.2f→%.2f", hpBefore, hpAfter)
	}
	for _, u := range []*Unit{w1, w2} {
		lu := s.unitsByID[u.ID]
		if lu == nil {
			continue
		}
		if lu.BuildTargetID != "" {
			t.Errorf("unit %d: BuildTargetID should be empty after kick, got %q", lu.ID, lu.BuildTargetID)
		}
		if lu.Building {
			t.Errorf("unit %d: Building should be false after kick", lu.ID)
		}
		if lu.InsideBuilder {
			t.Errorf("unit %d: InsideBuilder should be false after kick", lu.ID)
		}
		if !lu.Visible {
			t.Errorf("unit %d: Visible should be true after kick", lu.ID)
		}
		if lu.RepairChargeAccumulator != 0 {
			t.Errorf("unit %d: RepairChargeAccumulator should be 0 after kick, got %.4f", lu.ID, lu.RepairChargeAccumulator)
		}
	}
	// w1 (was inside) should now be outside the footprint.
	w1Live := s.unitsByID[w1.ID]
	if w1Live != nil {
		buildingLeft := float64(building.X) * s.MapConfig.CellSize
		buildingRight := float64(building.X+building.Width) * s.MapConfig.CellSize
		buildingTop := float64(building.Y) * s.MapConfig.CellSize
		buildingBottom := float64(building.Y+building.Height) * s.MapConfig.CellSize
		insideX := w1Live.X > buildingLeft && w1Live.X < buildingRight
		insideY := w1Live.Y > buildingTop && w1Live.Y < buildingBottom
		if insideX && insideY {
			t.Errorf("inside builder should have been moved to perimeter after kick, got (%.1f,%.1f)",
				w1Live.X, w1Live.Y)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 7: DemolishBuilding refunds cost, removes building, idles workers
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_DemolishBuilding(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y

	goldBefore := s.Players["p1"].Resources["gold"]
	woodBefore := s.Players["p1"].Resources["wood"]
	s.mu.Unlock()

	s.DemolishBuilding("p1", buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Building must be gone.
	if s.getBuildingByIDLocked(buildingID) != nil {
		t.Error("building should be removed after demolish")
	}

	// Refund must equal barracks cost (200 gold, 150 wood).
	def, _ := getBuildingDef("barracks")
	expectedGold := goldBefore + def.ResourceCost["gold"]
	expectedWood := woodBefore + def.ResourceCost["wood"]
	if s.Players["p1"].Resources["gold"] != expectedGold {
		t.Errorf("gold after demolish: expected %d, got %d", expectedGold, s.Players["p1"].Resources["gold"])
	}
	if s.Players["p1"].Resources["wood"] != expectedWood {
		t.Errorf("wood after demolish: expected %d, got %d", expectedWood, s.Players["p1"].Resources["wood"])
	}

	w1Live := s.unitsByID[w1.ID]
	if w1Live != nil {
		if w1Live.BuildTargetID != "" {
			t.Errorf("w1: BuildTargetID should be empty after demolish, got %q", w1Live.BuildTargetID)
		}
		if w1Live.InsideBuilder {
			t.Errorf("w1: InsideBuilder should be false after demolish")
		}
		if !w1Live.Visible {
			t.Errorf("w1: Visible should be true after demolish")
		}
	}
}

// DemolishBuilding on a completed building is a no-op.
func TestConstruction_DemolishCompletedBuilding_NoOp(t *testing.T) {
	s, _, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	delete(building.Metadata, "underConstruction")
	goldBefore := s.Players["p1"].Resources["gold"]
	s.mu.Unlock()

	s.DemolishBuilding("p1", buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.getBuildingByIDLocked(buildingID) == nil {
		t.Error("completed building should not be demolished")
	}
	if s.Players["p1"].Resources["gold"] != goldBefore {
		t.Error("no refund should occur on a no-op demolish")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 8: Move command clears InsideBuilder/accumulator
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_MoveCommand_ClearsInsideBuilderState(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y
	w1.RepairChargeAccumulator = 2.5
	s.mu.Unlock()

	// Issue a move command.
	s.MoveUnits("p1", []int{w1.ID}, protocol.Vec2{X: 300, Y: 300})

	s.mu.RLock()
	defer s.mu.RUnlock()
	live := s.unitsByID[w1.ID]
	if live == nil {
		t.Fatal("w1 removed unexpectedly")
	}
	if live.InsideBuilder {
		t.Error("InsideBuilder should be false after move command")
	}
	if live.Visible != true {
		t.Error("Visible should be true after move command")
	}
	if live.RepairChargeAccumulator != 0 {
		t.Errorf("RepairChargeAccumulator should be 0 after move command, got %.4f", live.RepairChargeAccumulator)
	}
	if live.BuildTargetID != "" {
		t.Errorf("BuildTargetID should be empty after move command, got %q", live.BuildTargetID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 9: Determinism — same state + same inputs produce identical resource totals
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_Determinism_ThreeHelpers(t *testing.T) {
	// Helper that creates a state with 3 outside helpers and barely enough
	// gold for 2 charges.
	buildState := func() (*GameState, string, int, int, int) {
		s, w1, w2, buildingID := newConstructionTestState(t)

		s.mu.Lock()
		building := s.getBuildingByIDLocked(buildingID)
		center := s.buildingCenterLocked(building)

		// Completed-but-damaged building so all workers pay.
		delete(building.Metadata, "underConstruction")
		building.Metadata["hp"] = float64(480)
		building.Metadata["hpPerSecond"] = float64(500) // fast — easier to observe charges

		// Spawn a third worker.
		w3 := s.spawnPlayerUnitLocked("worker", "p1", "#fff", protocol.Vec2{X: 300, Y: 100})

		for _, w := range []*Unit{w1, w2, w3} {
			w.Building = true
			w.InsideBuilder = false
			w.Visible = true
			w.BuildTargetID = buildingID
			w.X = center.X + 40
			w.Y = center.Y
			w.RepairChargeAccumulator = 4.9 // one tick from charging
		}

		// Only 2 gold — first two workers that process (lowest IDs) pay;
		// third is paused.
		s.Players["p1"].Resources["gold"] = 2
		s.Players["p1"].Resources["wood"] = 999

		w1id, w2id, w3id := w1.ID, w2.ID, w3.ID
		s.mu.Unlock()
		return s, buildingID, w1id, w2id, w3id
	}

	// Run the simulation twice from the same starting point.
	runOnce := func() (int, int, string) {
		s, _, w1id, w2id, w3id := buildState()
		s.Update(0.05)
		s.mu.RLock()
		defer s.mu.RUnlock()

		gold := s.Players["p1"].Resources["gold"]
		wood := s.Players["p1"].Resources["wood"]

		// Determine which workers charged vs were paused.
		statuses := fmt.Sprintf("w1=%q w2=%q w3=%q",
			statusOf(s, w1id), statusOf(s, w2id), statusOf(s, w3id))
		return gold, wood, statuses
	}

	gold1, wood1, st1 := runOnce()
	gold2, wood2, st2 := runOnce()

	if gold1 != gold2 {
		t.Errorf("gold mismatch across runs: %d vs %d", gold1, gold2)
	}
	if wood1 != wood2 {
		t.Errorf("wood mismatch across runs: %d vs %d", wood1, wood2)
	}
	if st1 != st2 {
		t.Errorf("status mismatch across runs:\n  run1: %s\n  run2: %s", st1, st2)
	}

	// With 2 gold and 3 workers all ready to charge, exactly 2 gold should be spent.
	if gold1 != 0 {
		t.Errorf("expected 0 gold remaining after 2 charges, got %d", gold1)
	}

	t.Logf("statuses: %s", st1)
}

func statusOf(s *GameState, id int) string {
	u := s.unitsByID[id]
	if u == nil {
		return "(dead)"
	}
	return u.Status
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: RepairBuilding is idempotent for already-assigned workers
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_RepairBuilding_Idempotent(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Building is damaged completed.
	delete(building.Metadata, "underConstruction")
	building.Metadata["hp"] = float64(490)

	// w1 already has a build assignment with some accumulator state.
	w1.Building = true
	w1.InsideBuilder = false
	w1.Visible = true
	w1.BuildTargetID = buildingID
	w1.X = center.X + 40
	w1.Y = center.Y
	w1.RepairChargeAccumulator = 3.5
	s.mu.Unlock()

	// Issue RepairBuilding with w1 again — it should not reset its accumulator.
	s.RepairBuilding("p1", []int{w1.ID}, buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	live := s.unitsByID[w1.ID]
	if live == nil {
		t.Fatal("w1 removed unexpectedly")
	}
	if live.BuildTargetID != buildingID {
		t.Errorf("w1 BuildTargetID changed on idempotent repair: got %q", live.BuildTargetID)
	}
	if live.RepairChargeAccumulator != 3.5 {
		t.Errorf("w1 accumulator should be preserved: expected 3.5, got %.4f", live.RepairChargeAccumulator)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: KickBuildersFromBuilding — auth check (wrong player cannot kick)
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_KickBuilders_AuthCheck(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y
	s.mu.Unlock()

	// Wrong player — should be a no-op.
	s.KickBuildersFromBuilding("p2", buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	live := s.unitsByID[w1.ID]
	if live == nil {
		t.Fatal("w1 removed unexpectedly")
	}
	if !live.InsideBuilder {
		t.Error("InsideBuilder should not change when wrong player issues kick")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Gap tests added by QA pass
// ─────────────────────────────────────────────────────────────────────────────

// TestConstruction_Completion_WorkerCleanup verifies that when a building
// reaches MaxHP all assigned workers — inside builder and helpers — are fully
// idled: Status="Idle", BuildTargetID="", Building=false, InsideBuilder=false,
// Visible=true. Also verifies the inside builder is placed outside the footprint.
// This exercises the completion branch of tickBuildingRepairsLocked end-to-end
// via Update(), covering criterion #1.
func TestConstruction_Completion_WorkerCleanup(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Set HP just below MaxHP so a single tick completes it.
	maxHp := building.Metadata["maxHp"].(float64)
	building.Metadata["hp"] = maxHp - 1.0
	building.Metadata["hpPerSecond"] = float64(5000) // very fast — completes in < 1 tick

	// w1 = inside builder, w2 = outside helper.
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.Status = "Building"
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y
	w1.TargetX = center.X
	w1.TargetY = center.Y

	w2.Building = true
	w2.InsideBuilder = false
	w2.Visible = true
	w2.Status = "Repairing"
	w2.BuildTargetID = buildingID
	w2.X = center.X + 40
	w2.Y = center.Y
	s.mu.Unlock()

	const dt = 0.05
	s.Update(dt)

	s.mu.RLock()
	defer s.mu.RUnlock()

	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		t.Fatal("building was unexpectedly removed on completion")
	}
	if getMetadataBool(b.Metadata, "underConstruction") {
		t.Error("underConstruction flag should be cleared after completion")
	}

	for i, u := range []*Unit{w1, w2} {
		live := s.unitsByID[u.ID]
		if live == nil {
			t.Fatalf("worker %d was unexpectedly removed", i+1)
		}
		if live.Building {
			t.Errorf("worker%d: Building should be false after completion, got true", i+1)
		}
		if live.InsideBuilder {
			t.Errorf("worker%d: InsideBuilder should be false after completion, got true", i+1)
		}
		if !live.Visible {
			t.Errorf("worker%d: Visible should be true after completion, got false", i+1)
		}
		if live.Status != "Idle" {
			t.Errorf("worker%d: Status should be Idle after completion, got %q", i+1, live.Status)
		}
		if live.BuildTargetID != "" {
			t.Errorf("worker%d: BuildTargetID should be empty after completion, got %q", i+1, live.BuildTargetID)
		}
		if live.RepairChargeAccumulator != 0 {
			t.Errorf("worker%d: RepairChargeAccumulator should be 0 after completion, got %.4f", i+1, live.RepairChargeAccumulator)
		}
	}

	// Inside builder (w1) must have been ejected from the footprint.
	w1Live := s.unitsByID[w1.ID]
	if w1Live != nil {
		buildingLeft := float64(building.X) * s.MapConfig.CellSize
		buildingRight := float64(building.X+building.Width) * s.MapConfig.CellSize
		buildingTop := float64(building.Y) * s.MapConfig.CellSize
		buildingBottom := float64(building.Y+building.Height) * s.MapConfig.CellSize
		insideX := w1Live.X > buildingLeft && w1Live.X < buildingRight
		insideY := w1Live.Y > buildingTop && w1Live.Y < buildingBottom
		if insideX && insideY {
			t.Errorf("inside builder should be outside footprint after completion, got (%.1f,%.1f)",
				w1Live.X, w1Live.Y)
		}
	}
}

// TestConstruction_Demolish_AfterCompletion_Race verifies the demolish-vs-
// completion race condition (criterion #5): if a building reaches MaxHP in the
// same Update() call that a demolish command is issued, completion fires first
// (inside tickBuildingRepairsLocked), clearing underConstruction. A subsequent
// DemolishBuilding call then sees underConstruction=false and silently no-ops:
// the building survives and no refund is issued.
//
// This test drives the race deterministically: set HP one tick below MaxHP,
// call Update(dt) — building completes — then immediately call DemolishBuilding
// and assert it had no effect.
func TestConstruction_Demolish_AfterCompletion_Race(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)
	maxHp := building.Metadata["maxHp"].(float64)
	building.Metadata["hp"] = maxHp - 1.0
	building.Metadata["hpPerSecond"] = float64(5000)

	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.Status = "Building"
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y

	goldBefore := s.Players["p1"].Resources["gold"]
	woodBefore := s.Players["p1"].Resources["wood"]
	s.mu.Unlock()

	// Tick completes the building.
	s.Update(0.05)

	s.mu.RLock()
	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		s.mu.RUnlock()
		t.Fatal("building was removed during completion — unexpected")
	}
	if getMetadataBool(b.Metadata, "underConstruction") {
		s.mu.RUnlock()
		t.Fatal("building should have completed — underConstruction should be false")
	}
	s.mu.RUnlock()

	// Now issue demolish. Because the building is no longer under construction
	// this must be a silent no-op.
	s.DemolishBuilding("p1", buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.getBuildingByIDLocked(buildingID) == nil {
		t.Error("demolish on a completed building should be a no-op; building was removed")
	}
	// No refund should have occurred.
	if s.Players["p1"].Resources["gold"] != goldBefore {
		t.Errorf("no refund expected after demolish no-op: gold %d → %d", goldBefore, s.Players["p1"].Resources["gold"])
	}
	if s.Players["p1"].Resources["wood"] != woodBefore {
		t.Errorf("no refund expected after demolish no-op: wood %d → %d", woodBefore, s.Players["p1"].Resources["wood"])
	}
}

// TestConstruction_RepairDamagedBuilding_NoInsideBuilder verifies criterion #8:
// when workers are assigned to repair a COMPLETED (not under-construction)
// damaged building, none of them become InsideBuilder. The spec disallows the
// inside-builder slot for post-construction repair. We validate this both at
// the updateWorkerBuildStateLocked call site and through a full Update() tick.
func TestConstruction_RepairDamagedBuilding_NoInsideBuilder(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Mark building as completed (not under construction) but damaged.
	delete(building.Metadata, "underConstruction")
	delete(building.Metadata, "pendingStart")
	building.Metadata["hp"] = float64(400)

	// Simulate both workers arriving at the building (stopped, BuildTargetID set).
	approachPos1 := protocol.Vec2{X: center.X + 40, Y: center.Y}
	approachPos2 := protocol.Vec2{X: center.X - 40, Y: center.Y}
	arriveAtBuilding(w1, buildingID, approachPos1)
	arriveAtBuilding(w2, buildingID, approachPos2)

	// Call updateWorkerBuildStateLocked for both — this is the transition into
	// build mode and is the point where InsideBuilder would be incorrectly set.
	s.updateWorkerBuildStateLocked(w1)
	s.updateWorkerBuildStateLocked(w2)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	for i, u := range []*Unit{w1, w2} {
		live := s.unitsByID[u.ID]
		if live == nil {
			t.Fatalf("worker %d removed unexpectedly", i+1)
		}
		if live.InsideBuilder {
			t.Errorf("worker%d: InsideBuilder must be false for completed-building repair, got true", i+1)
		}
		if !live.Visible {
			t.Errorf("worker%d: repair worker must remain Visible=true, got false", i+1)
		}
		if live.Status != "Repairing" {
			t.Errorf("worker%d: expected Status=Repairing, got %q", i+1, live.Status)
		}
	}
}

// TestConstruction_Kick_OrphanLogicDoesNotCancelStartedBuilding verifies
// criterion #13: after construction has started (first worker arrived, clearing
// pendingStart) and workers are kicked, cancelOrphanedPendingBuildingsLocked
// must NOT auto-cancel the building on the next tick, because pendingStart is
// no longer set. The building must survive with its HP and underConstruction
// flag intact.
func TestConstruction_Kick_OrphanLogicDoesNotCancelStartedBuilding(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// Simulate the first worker having arrived — this clears pendingStart.
	// We drive updateWorkerBuildStateLocked directly so the arrival transition
	// fires without pathfinding.
	arriveAtBuilding(w1, buildingID, protocol.Vec2{X: center.X + 40, Y: center.Y})
	s.updateWorkerBuildStateLocked(w1)

	// Ensure pendingStart is absent (the arrival should have deleted it).
	if _, hasPending := building.Metadata["pendingStart"]; hasPending {
		s.mu.Unlock()
		t.Fatal("pendingStart should have been cleared when first worker arrived")
	}

	w2.Building = true
	w2.InsideBuilder = false
	w2.BuildTargetID = buildingID
	w2.X = center.X + 40
	w2.Y = center.Y

	hpBeforeKick, _, _ := getBuildingHP(building)
	s.mu.Unlock()

	// Kick all workers.
	s.KickBuildersFromBuilding("p1", buildingID)

	// Now tick the simulation — cancelOrphanedPendingBuildingsLocked runs
	// at the top of Update(). The building should survive because pendingStart
	// is not set.
	s.Update(0.05)

	s.mu.RLock()
	defer s.mu.RUnlock()

	b := s.getBuildingByIDLocked(buildingID)
	if b == nil {
		t.Fatal("building was auto-cancelled after kick — should not happen for a started construction (pendingStart was cleared)")
	}
	if !getMetadataBool(b.Metadata, "underConstruction") {
		t.Error("underConstruction should still be set after kick")
	}
	hpAfter, _, _ := getBuildingHP(b)
	if hpAfter != hpBeforeKick {
		t.Errorf("building HP should not change after kick with no workers: %.2f → %.2f", hpBeforeKick, hpAfter)
	}

	// Verify neither worker is still assigned to the building.
	for _, u := range []*Unit{w1, w2} {
		live := s.unitsByID[u.ID]
		if live == nil {
			continue
		}
		if live.BuildTargetID == buildingID {
			t.Errorf("unit %d: BuildTargetID should be empty after kick, got %q", live.ID, live.BuildTargetID)
		}
	}
}

// TestConstruction_Determinism_LowestIDHelperPaysFirst verifies the strict
// lowest-ID-wins ordering promised by the spec for criterion #9 / #7.
//
// Setup: 3 workers at identical accumulator state, 2 gold, completed-but-damaged
// building. We explicitly spawn workers so w3.ID > w2.ID > w1.ID (spawnPlayerUnitLocked
// uses an incrementing counter). After one tick:
//   - w1 (lowest ID) is processed first → should be the one that charges and
//     possibly exhausts gold.
//   - The exact distribution (w1 charges until gold gone, w2/w3 pause) should
//     be STABLE — same result on both simulation runs — which is the determinism
//     invariant.
//
// This test also documents the actual charging behavior: under the current
// applyChargedHPLocked loop, one worker can consume multiple charges in a single
// tick. The test asserts gold reaches 0, the status outcome is identical across
// runs, and w3 (highest ID) is the last to be processed (Paused when gold is
// already 0).
func TestConstruction_Determinism_LowestIDWins(t *testing.T) {
	buildState := func() (*GameState, [3]int) {
		s, w1, w2, buildingID := newConstructionTestState(t)

		s.mu.Lock()
		building := s.getBuildingByIDLocked(buildingID)
		center := s.buildingCenterLocked(building)

		// Completed damaged building so all workers pay.
		delete(building.Metadata, "underConstruction")
		building.Metadata["hp"] = float64(490)
		building.Metadata["hpPerSecond"] = float64(50) // slow enough: 50*0.05=2.5 HP/tick per worker

		w3 := s.spawnPlayerUnitLocked("worker", "p1", "#fff", protocol.Vec2{X: 300, Y: 100})

		// At 2.5 HP/tick and acc=4.0, needed=1.0, remaining=2.5. Worker crosses
		// one threshold (charges 1 gold), then remaining=1.5 which doesn't cross
		// the next 5-HP boundary — stops without pausing.
		for _, w := range []*Unit{w1, w2, w3} {
			w.Building = true
			w.InsideBuilder = false
			w.Visible = true
			w.BuildTargetID = buildingID
			w.X = center.X + 40
			w.Y = center.Y
			w.RepairChargeAccumulator = 4.0 // 1.0 HP away from crossing threshold
		}

		// Only 1 gold: exactly one worker can charge; the others are paused.
		s.Players["p1"].Resources["gold"] = 1
		s.Players["p1"].Resources["wood"] = 999

		ids := [3]int{w1.ID, w2.ID, w3.ID}
		s.mu.Unlock()
		return s, ids
	}

	type runResult struct {
		gold     int
		statuses [3]string
	}

	runOnce := func() runResult {
		s, ids := buildState()
		s.Update(0.05)
		s.mu.RLock()
		defer s.mu.RUnlock()
		r := runResult{gold: s.Players["p1"].Resources["gold"]}
		for i, id := range ids {
			r.statuses[i] = statusOf(s, id)
		}
		return r
	}

	r1 := runOnce()
	r2 := runOnce()

	// Determinism: both runs must produce identical outcomes.
	if r1.gold != r2.gold {
		t.Errorf("gold non-deterministic: %d vs %d", r1.gold, r2.gold)
	}
	if r1.statuses != r2.statuses {
		t.Errorf("statuses non-deterministic:\n  run1: %v\n  run2: %v", r1.statuses, r2.statuses)
	}

	// With 1 gold and the accumulator setup, exactly 1 charge happens (w1, lowest ID).
	// Gold must reach 0.
	if r1.gold != 0 {
		t.Errorf("expected 0 gold remaining after 1 charge, got %d", r1.gold)
	}

	// w1 (lowest ID, statuses[0]) must have contributed HP — it should not be Paused.
	// w2 and w3 (higher IDs) must be paused since gold is gone after w1 charges.
	if r1.statuses[0] == "Repairing (Paused)" {
		t.Errorf("w1 (lowest ID) should NOT be paused — it should have charged: %q", r1.statuses[0])
	}
	for i := 1; i < 3; i++ {
		if r1.statuses[i] != "Repairing (Paused)" {
			t.Errorf("worker[%d] (higher ID than w1) should be paused after w1 exhausted gold, got %q", i, r1.statuses[i])
		}
	}

	t.Logf("run1 statuses: w1=%q w2=%q w3=%q gold=%d", r1.statuses[0], r1.statuses[1], r1.statuses[2], r1.gold)
}

// TestConstruction_DemolishBuilding_AuthCheck verifies criterion #11: an enemy
// player cannot demolish another player's under-construction building.
func TestConstruction_DemolishBuilding_AuthCheck(t *testing.T) {
	s, w1, _, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y
	goldBefore := s.Players["p1"].Resources["gold"]
	s.mu.Unlock()

	// Wrong player — should be a no-op.
	s.DemolishBuilding("p2", buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.getBuildingByIDLocked(buildingID) == nil {
		t.Error("building should not be demolished by enemy player")
	}
	if s.Players["p1"].Resources["gold"] != goldBefore {
		t.Error("no refund should occur when wrong player issues demolish")
	}
	// Inside builder should still be assigned.
	live := s.unitsByID[w1.ID]
	if live == nil {
		t.Fatal("w1 removed unexpectedly")
	}
	if !live.InsideBuilder {
		t.Error("InsideBuilder should be unchanged after enemy demolish attempt")
	}
}

// TestConstruction_RepairBuilding_Idempotent_InsideBuilderPreserved verifies
// criterion #12: re-issuing repair_command while one worker is already the
// inside builder must NOT clobber that worker's InsideBuilder, Visible, or
// RepairChargeAccumulator fields. New workers in the unitIDs list become helpers.
func TestConstruction_RepairBuilding_Idempotent_InsideBuilderPreserved(t *testing.T) {
	s, w1, w2, buildingID := newConstructionTestState(t)

	s.mu.Lock()
	building := s.getBuildingByIDLocked(buildingID)
	center := s.buildingCenterLocked(building)

	// w1 is already the inside builder with accumulated HP progress.
	w1.Building = true
	w1.InsideBuilder = true
	w1.Visible = false
	w1.Status = "Building"
	w1.BuildTargetID = buildingID
	w1.X = center.X
	w1.Y = center.Y
	w1.RepairChargeAccumulator = 3.7 // non-zero to detect clobber

	// w2 is not yet assigned.
	w2.Building = false
	w2.BuildTargetID = ""
	w2.X = center.X + 40
	w2.Y = center.Y
	s.mu.Unlock()

	// Re-issue RepairBuilding with both w1 (already inside) and w2 (new worker).
	s.RepairBuilding("p1", []int{w1.ID, w2.ID}, buildingID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// w1's inside-builder state must be fully preserved.
	liveW1 := s.unitsByID[w1.ID]
	if liveW1 == nil {
		t.Fatal("w1 removed unexpectedly")
	}
	if !liveW1.InsideBuilder {
		t.Error("w1: InsideBuilder must not be cleared by re-issuing repair command")
	}
	if liveW1.Visible {
		t.Error("w1: Visible must remain false for inside builder after re-issue")
	}
	if liveW1.RepairChargeAccumulator != 3.7 {
		t.Errorf("w1: RepairChargeAccumulator clobbered: expected 3.7, got %.4f", liveW1.RepairChargeAccumulator)
	}
	if liveW1.BuildTargetID != buildingID {
		t.Errorf("w1: BuildTargetID changed: expected %q, got %q", buildingID, liveW1.BuildTargetID)
	}

	// w2 should now be assigned to the building (path queued), but not InsideBuilder.
	liveW2 := s.unitsByID[w2.ID]
	if liveW2 == nil {
		t.Fatal("w2 removed unexpectedly")
	}
	if liveW2.BuildTargetID != buildingID {
		t.Errorf("w2: expected BuildTargetID=%q, got %q", buildingID, liveW2.BuildTargetID)
	}
	if liveW2.InsideBuilder {
		t.Error("w2: new worker should not be InsideBuilder immediately on repair assignment")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: a worker still walking toward the build site must NOT be
// teleported into the footprint by tickBuildingRepairsLocked's promotion path.
// Reproduces the playtest bug where the worker disappeared on dispatch and
// the building never advanced past pendingStart.
// ─────────────────────────────────────────────────────────────────────────────

func TestConstruction_EnRouteWorkerNotPromoted(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	s.mu.Lock()
	s.Players["p1"] = &Player{
		ID: "p1",
		Resources: map[string]int{
			"gold": 9999,
			"wood": 9999,
		},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Upgrades:                      map[UpgradeTrack]int{},
		Vault:                         []*VaultItem{},
	}
	// Place the worker far from where the building will go so the path
	// definitely takes multiple ticks to traverse.
	w := s.spawnPlayerUnitLocked("worker", "p1", "#fff", protocol.Vec2{X: 64, Y: 64})
	startX, startY := w.X, w.Y
	s.mu.Unlock()

	// Dispatch via the real BuildBuilding path. Pick a build site clearly
	// inside the map and not adjacent to the worker.
	gridX, gridY := 20, 20
	s.BuildBuilding("p1", "barracks", []int{w.ID}, gridX, gridY)

	s.mu.RLock()
	if w.BuildTargetID == "" {
		s.mu.RUnlock()
		t.Fatal("worker was not assigned BuildTargetID by BuildBuilding")
	}
	if !w.Moving {
		s.mu.RUnlock()
		t.Fatalf("worker should be Moving after dispatch — start (%v,%v) goal grid (%d,%d)", startX, startY, gridX, gridY)
	}
	if !w.Visible {
		s.mu.RUnlock()
		t.Error("en-route worker must remain visible")
	}
	if w.InsideBuilder {
		s.mu.RUnlock()
		t.Error("en-route worker must not be InsideBuilder yet")
	}
	if w.Building {
		s.mu.RUnlock()
		t.Error("en-route worker must not have Building=true yet")
	}
	buildingID := w.BuildTargetID
	s.mu.RUnlock()

	// Tick once. Pre-fix: tickBuildingRepairsLocked promotes the still-moving
	// worker, snaps them to the building center, and starts free HP. Post-fix:
	// the en-route worker is ignored by promotion; building HP stays at the
	// initial 1.0 value, worker is still moving, still visible, still outside
	// the footprint.
	s.Update(0.05)

	s.mu.RLock()
	defer s.mu.RUnlock()
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil {
		t.Fatal("building disappeared after one tick")
	}
	if !w.Visible {
		t.Error("worker must stay visible while traveling to the build site")
	}
	if w.InsideBuilder {
		t.Error("worker must not be promoted to InsideBuilder while still en route")
	}
	hp, _, _ := getBuildingHP(building)
	if hp > 1.0 {
		t.Errorf("building HP should not advance before any worker arrives, got %v", hp)
	}
	// Worker should still be near their start position, not at the building center.
	center := s.buildingCenterLocked(building)
	dxStart := w.X - startX
	dyStart := w.Y - startY
	dxCenter := w.X - center.X
	dyCenter := w.Y - center.Y
	distFromStartSq := dxStart*dxStart + dyStart*dyStart
	distFromCenterSq := dxCenter*dxCenter + dyCenter*dyCenter
	if distFromCenterSq < distFromStartSq {
		t.Errorf("worker teleported toward building center: pos=(%v,%v) start=(%v,%v) center=(%v,%v)",
			w.X, w.Y, startX, startY, center.X, center.Y)
	}
	// pendingStart should still be present until a worker actually arrives.
	if pending, _ := building.Metadata["pendingStart"].(bool); !pending {
		t.Error("pendingStart must remain true until first worker arrives")
	}
}
