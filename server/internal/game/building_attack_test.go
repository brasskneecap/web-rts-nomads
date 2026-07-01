package game

// building_attack_test.go — regression tests for enemy ranged units acquiring
// and damaging player buildings via AI (no preset target).
//
// Root cause that was fixed: the "enemy_archer" CombatProfile was missing
// TargetBuildings:true, so every unit resolving to that profile (e.g.
// ranged_raider) could never auto-acquire a building target.  The fix adds
// TargetBuildings:true to the enemy_archer profile entry in
// combat_ai_profiles.go.

import (
	"fmt"
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// newBuildingAttackState creates an obstacle-free 40×24-cell map with a single
// player-owned townhall at grid (5,5), Width=2, Height=2, HP=5000.
// Returns the locked GameState and a stable pointer to Buildings[0].
func newBuildingAttackState(t *testing.T) (*GameState, *protocol.BuildingTile) {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 24
	owner := "p1"
	th := protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		ID:           "th-attack",
		BuildingType: "townhall",
		Width:        2,
		Height:       2,
		Occupied:     true,
		Visible:      true,
		OwnerID:      &owner,
		Metadata:     map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	cfg := protocol.MapConfig{
		ID: "building-attack-test", Name: "building-attack-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: []protocol.BuildingTile{th},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s, &s.MapConfig.Buildings[0]
}

// spawnBuildingAttackEnemy spawns an enemy unit of the given type at (x,y),
// marks it visible, sets HP to a large value, and initialises combat state.
func spawnBuildingAttackEnemy(t *testing.T, s *GameState, unitType string, x, y float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked(unitType, enemyPlayerID, "#e74c3c", protocol.Vec2{X: x, Y: y})
	if u == nil {
		t.Fatalf("spawnBuildingAttackEnemy: nil return for type=%q", unitType)
	}
	u.Visible = true
	u.HP = 999
	u.MaxHP = 999
	s.initializeCombatUnitLocked(u)
	return u
}

// distBuildingCenter returns the distance from (ux,uy) to the building center —
// used only for diagnostic logging, not for assertions.
func distBuildingCenter(s *GameState, ux, uy float64, b *protocol.BuildingTile) float64 {
	ctr := s.buildingCenterLocked(b)
	dx := ux - ctr.X
	dy := uy - ctr.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: enemy_archer profile has TargetBuildings enabled
// ─────────────────────────────────────────────────────────────────────────────

// TestEnemyArcherProfile_TargetBuildingsEnabled asserts that the enemy_archer
// CombatProfile has TargetBuildings==true. This is a direct guard on the
// profile table — if this flag is ever accidentally cleared, the downstream
// acquisition tests below will also fail, but this test names the root cause.
func TestEnemyArcherProfile_TargetBuildingsEnabled(t *testing.T) {
	s, _ := newBuildingAttackState(t)
	defer s.mu.Unlock()

	for _, tc := range []struct {
		unitType string
		want     bool
	}{
		// raider is melee and known-working control.
		{"raider", true},
		// ranged_raider resolves to enemy_archer profile — was broken.
		{"ranged_raider", true},
		// raider_roc resolves to flyer_skirmisher profile — was broken (same
		// omission as enemy_archer): the flyer could never auto-acquire a
		// building and just circled it.
		{"raider_roc", true},
	} {
		u := spawnBuildingAttackEnemy(t, s, tc.unitType, 800, 400)
		profile := resolveCombatProfile(u)
		if profile.TargetBuildings != tc.want {
			t.Errorf("[%s]: profile %q has TargetBuildings=%v, want %v — "+
				"unit cannot auto-acquire buildings via the AI",
				tc.unitType, profile.Name, profile.TargetBuildings, tc.want)
		}
	}
}

// TestFlyerRaiderRoc_AcquiresAndDamagesBuilding spawns a raider_roc (flyer,
// flyer_skirmisher profile) within its attack range of a player townhall with
// no preset target. The AI must acquire the building and deal damage — the
// reported bug was the roc circling the building without ever attacking,
// because flyer_skirmisher was missing TargetBuildings:true.
func TestFlyerRaiderRoc_AcquiresAndDamagesBuilding(t *testing.T) {
	const (
		cell        = 64.0
		thRightEdge = 7 * cell        // 448 — right edge (grid cols 5+2=7)
		spawnX      = thRightEdge + 100.0 // 548 — inside roc AttackRange (150)
		unitY       = (5 + 1) * cell      // 384 — vertically centred on building
		ticks       = 60
	)

	s, b := newBuildingAttackState(t)
	unit := spawnBuildingAttackEnemy(t, s, "raider_roc", spawnX, unitY)
	unitID := unit.ID

	if !unit.Flyer {
		t.Fatalf("raider_roc expected to be a flyer, Flyer=%v", unit.Flyer)
	}
	profile := resolveCombatProfile(unit)
	t.Logf("[raider_roc] profile=%q TargetBuildings=%v AttackRange=%.0f DistToEdge=%.1f",
		profile.Name, profile.TargetBuildings, unit.AttackRange,
		s.distanceToBuilding(unit.X, unit.Y, b))

	s.mu.Unlock()
	tickN(s, ticks)
	s.mu.Lock()

	u := s.getUnitByIDLocked(unitID)
	if u == nil {
		t.Fatalf("unit disappeared after %d ticks", ticks)
	}
	hp, _, hpOK := getBuildingHP(b)
	if !hpOK {
		t.Fatalf("building HP metadata not readable")
	}

	t.Logf("[raider_roc] after %d ticks: Status=%q AttackBldgID=%q HP=%.0f/5000 damaged=%v",
		ticks, u.Status, u.AttackBuildingTargetID, hp, hp < 5000.0)

	if hp >= 5000.0 {
		t.Errorf("raider_roc: building HP=%.0f after %d ticks — flyer never fired "+
			"(profile=%q TargetBuildings=%v AttackBldgID=%q Status=%q)",
			hp, ticks, profile.Name, profile.TargetBuildings,
			u.AttackBuildingTargetID, u.Status)
	}
	s.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: ranged enemy unit within range acquires and damages a building
// ─────────────────────────────────────────────────────────────────────────────

// TestEnemyRangedUnit_AcquiresAndDamagesBuilding spawns a ranged_raider within
// its attack range of a player townhall with no preset target. The AI must
// acquire the building and deal damage within 60 ticks (~3s at 20Hz).
//
// Also runs the melee raider as a control to confirm the test harness itself
// works for both unit kinds.
//
// Townhall: grid (5,5), Width=2, Height=2 → right edge x=7*64=448.
// Spawn 200px to the right (x=648). ranged_raider AttackRange ≥ 200 → in range
// at spawn. raider AttackRange ~60 → must path first.
func TestEnemyRangedUnit_AcquiresAndDamagesBuilding(t *testing.T) {
	const (
		cell        = 64.0
		thRightEdge = 7 * cell  // 448 — right edge (grid cols 5+2=7)
		spawnX      = thRightEdge + 200.0
		unitY       = (5 + 1) * cell // 384 — vertically centred on building
		ticks       = 60
		buildingID  = "th-attack"
	)

	for _, tc := range []struct {
		unitType string
	}{
		{"raider"},
		{"ranged_raider"},
	} {
		tc := tc
		t.Run(tc.unitType, func(t *testing.T) {
			s, b := newBuildingAttackState(t)
			unit := spawnBuildingAttackEnemy(t, s, tc.unitType, spawnX, unitY)
			unitID := unit.ID

			profile := resolveCombatProfile(unit)
			edgeDist := s.distanceToBuilding(unit.X, unit.Y, b)
			t.Logf("[%s] profile=%q TargetBuildings=%v AttackRange=%.0f DistToEdge=%.1f",
				tc.unitType, profile.Name, profile.TargetBuildings, unit.AttackRange, edgeDist)

			s.mu.Unlock()
			tickN(s, ticks)
			s.mu.Lock()

			u := s.getUnitByIDLocked(unitID)
			if u == nil {
				t.Fatalf("unit disappeared after %d ticks", ticks)
			}
			hp, _, hpOK := getBuildingHP(b)
			if !hpOK {
				t.Fatalf("building HP metadata not readable")
			}

			t.Logf("[%s] after %d ticks: Status=%q AttackBldgID=%q HP=%.0f/5000 damaged=%v",
				tc.unitType, ticks, u.Status, u.AttackBuildingTargetID, hp, hp < 5000.0)

			if hp >= 5000.0 {
				t.Errorf("[%s]: building HP=%.0f after %d ticks — unit never fired "+
					"(profile=%q TargetBuildings=%v AttackBldgID=%q Status=%q)",
					tc.unitType, hp, ticks,
					profile.Name, profile.TargetBuildings,
					u.AttackBuildingTargetID, u.Status)
			}
			s.mu.Unlock()
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: ranged enemy must path to and damage a building beyond its
// initial spawn distance (not already in range at spawn)
// ─────────────────────────────────────────────────────────────────────────────

// TestEnemyRangedUnit_AdvancesAndDamagesBuilding spawns a ranged_raider well
// outside its detection range so it must advance before acquiring. The enemy
// objective system walks it toward the townhall; once within detection range
// the combat AI must acquire and fire.
//
// Spawn at x=848 (400px past right edge). ranged_raider DetectionRange ~320
// means the building center is at ~448+64=512; distance from spawn to center
// ≈848-512=336 — just outside detection at spawn. The unit advances via
// objective logic and closes in. Run 120 ticks (~6s) for travel + attack.
func TestEnemyRangedUnit_AdvancesAndDamagesBuilding(t *testing.T) {
	const (
		cell        = 64.0
		thRightEdge = 7 * cell   // 448
		spawnX      = thRightEdge + 400.0 // 848
		unitY       = (5 + 1) * cell      // 384
		ticks       = 120
	)

	for _, tc := range []struct {
		unitType string
	}{
		{"raider"},
		{"ranged_raider"},
	} {
		tc := tc
		t.Run(tc.unitType, func(t *testing.T) {
			s, b := newBuildingAttackState(t)
			unit := spawnBuildingAttackEnemy(t, s, tc.unitType, spawnX, unitY)
			unitID := unit.ID

			profile := resolveCombatProfile(unit)
			ctrDist := distBuildingCenter(s, unit.X, unit.Y, b)
			detRange := effectiveDetectionRange(unit, profile)
			t.Logf("[%s] profile=%q TargetBuildings=%v AttackRange=%.0f DetRange=%.0f DistToCenter=%.1f",
				tc.unitType, profile.Name, profile.TargetBuildings, unit.AttackRange, detRange, ctrDist)

			s.mu.Unlock()
			tickN(s, ticks)
			s.mu.Lock()

			u := s.getUnitByIDLocked(unitID)
			if u == nil {
				t.Fatalf("unit disappeared after %d ticks", ticks)
			}
			hp, _, hpOK := getBuildingHP(b)
			if !hpOK {
				t.Fatalf("building HP metadata not readable")
			}

			edgeDist := s.distanceToBuilding(u.X, u.Y, b)
			t.Logf("[%s] after %d ticks: pos=(%.1f,%.1f) DistToEdge=%.1f Status=%q HP=%.0f/5000",
				tc.unitType, ticks, u.X, u.Y, edgeDist, u.Status, hp)

			if hp >= 5000.0 {
				t.Errorf("[%s]: building HP=%.0f after %d ticks — unit never fired "+
					"(DistToEdge=%.1f AttackRange=%.0f AttackBldgID=%q Status=%q)",
					tc.unitType, hp, ticks,
					edgeDist, u.AttackRange, u.AttackBuildingTargetID, u.Status)
			}
			s.mu.Unlock()
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Control: preset-target building fire path (not the AI acquisition path)
// ─────────────────────────────────────────────────────────────────────────────

// TestEnemyUnit_PresetBuildingTarget_Fires verifies the fire path in
// tickUnitCombatLocked works for both melee and ranged when the building
// target is preset (bypasses AI acquisition). This is the baseline that
// confirms the combat loop itself can fire at buildings — if this fails the
// bug is in the fire path, not acquisition.
func TestEnemyUnit_PresetBuildingTarget_Fires(t *testing.T) {
	const (
		cell        = 64.0
		thRightEdge = 7 * cell // 448
		spawnX      = thRightEdge + cell // 512 — one cell right of edge, within any attack range
		unitY       = (5 + 1) * cell    // 384
		ticks       = 40
		buildingID  = "th-attack"
	)

	for _, tc := range []struct {
		unitType string
	}{
		{"raider"},
		{"ranged_raider"},
	} {
		tc := tc
		t.Run(tc.unitType, func(t *testing.T) {
			s, b := newBuildingAttackState(t)
			unit := spawnBuildingAttackEnemy(t, s, tc.unitType, spawnX, unitY)
			unitID := unit.ID

			// Pre-set the building target — bypass AI acquisition entirely.
			unit.AttackBuildingTargetID = buildingID
			unit.Status = "Moving To Attack"

			profile := resolveCombatProfile(unit)
			t.Logf("[%s] preset target: profile=%q Melee=%v AttackRange=%.0f DistToEdge=%.1f",
				tc.unitType, profile.Name, profile.Melee, unit.AttackRange,
				s.distanceToBuilding(unit.X, unit.Y, b))

			s.mu.Unlock()
			tickN(s, ticks)
			s.mu.Lock()

			u := s.getUnitByIDLocked(unitID)
			if u == nil {
				t.Fatalf("unit disappeared after %d ticks", ticks)
			}
			hp, _, hpOK := getBuildingHP(b)
			if !hpOK {
				t.Fatalf("building HP metadata not readable")
			}

			if hp >= 5000.0 {
				t.Errorf("[%s]: preset-target fire path: building HP=%.0f after %d ticks — unit never fired "+
					"(profile=%q Melee=%v AttackRange=%.0f DistToEdge=%.1f Status=%q)",
					tc.unitType, hp, ticks,
					profile.Name, profile.Melee, u.AttackRange,
					s.distanceToBuilding(u.X, u.Y, b), u.Status)
			}
			s.mu.Unlock()
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hypothesis-B control: findBestBuildingAttackPositionLocked result is in range
// ─────────────────────────────────────────────────────────────────────────────

// TestEnemyUnit_BestBuildingAttackPosition_IsInRange verifies that the
// position returned by findBestBuildingAttackPositionLocked is within the
// unit's AttackRange for both melee and ranged unit types. If this fails the
// pathfinder is sending units to a position they can't fire from.
func TestEnemyUnit_BestBuildingAttackPosition_IsInRange(t *testing.T) {
	const (
		cell        = 64.0
		thRightEdge = 7 * cell
		spawnX      = thRightEdge + 400.0
		unitY       = (5 + 1) * cell
	)

	s, b := newBuildingAttackState(t)
	defer s.mu.Unlock()

	blocked := s.getBlockedCellsLocked()

	for _, tc := range []struct {
		unitType string
	}{
		{"raider"},
		{"ranged_raider"},
	} {
		u := spawnBuildingAttackEnemy(t, s, tc.unitType, spawnX, unitY)
		profile := resolveCombatProfile(u)
		pos := s.findBestBuildingAttackPositionLocked(u, b, blocked)
		if pos == nil {
			t.Logf("[%s]: findBestBuildingAttackPositionLocked returned nil (no walkable perimeter cell)", tc.unitType)
			continue
		}
		posEdgeDist := s.distanceToBuilding(pos.X, pos.Y, b)
		inRange := posEdgeDist <= u.AttackRange
		t.Logf("[%s]: profile=%q TargetBuildings=%v AttackRange=%.0f pos=(%.1f,%.1f) edgeDist=%.1f inRange=%v",
			tc.unitType, profile.Name, profile.TargetBuildings, u.AttackRange, pos.X, pos.Y, posEdgeDist, inRange)

		if !inRange {
			t.Errorf(fmt.Sprintf("[%s]: attack position %.1f px from edge > AttackRange=%.0f — unit can't fire from there",
				tc.unitType, posEdgeDist, u.AttackRange))
		}
	}
}
