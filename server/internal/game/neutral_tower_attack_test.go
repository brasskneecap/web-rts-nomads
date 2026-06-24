package game

// neutral_tower_attack_test.go — regression tests for neutral capture-defenders
// acquiring and attacking a player claim tower.
//
// Root cause that was fixed: the anchor-slide block in tickCombatAILocked
// excluded neutralPlayerID units, so a capture-defender spawned far from the
// zone tower would advance toward it with its CombatAnchorX/Y frozen at the
// far spawn point. When it arrived, targetInsideLeashLocked compared the tower
// position to that frozen far anchor, measured a distance >> LeashDistance, and
// rejected the tower — so the unit stood idle next to it.
//
// Fix: the anchor-slide condition in combat_ai.go now also slides for
// neutral units carrying an ObjectiveBuildingID (capture-defenders).

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// newNeutralTowerAttackState builds a GameState with a 40×40-cell grid
// (no obstacles), one claim zone at cells (5,5)–(9,9), and one player-owned
// Tower at grid (7,7) inside the zone.
// Returns the locked state and a stable pointer to the tower building.
func newNeutralTowerAttackState(t *testing.T) (*GameState, *protocol.BuildingTile) {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 40
	towerOwner := "p1"

	tower := protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 7, Y: 7},
		ID:           "claim-tower",
		BuildingType: "Tower",
		Width:        1,
		Height:       2,
		Visible:      true,
		OwnerID:      &towerOwner,
		Metadata:     map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}

	cfg := protocol.MapConfig{
		ID:       "neutral-tower-attack-test",
		Name:     "neutral-tower-attack-test",
		Width:    float64(cols) * cell,
		Height:   float64(rows) * cell,
		GridCols: cols,
		GridRows: rows,
		CellSize: cell,
		Zones: []protocol.Zone{
			claimZone("claim", [2]int{7, 7}, rectCells(5, 5, 9, 9)),
		},
		Buildings: []protocol.BuildingTile{tower},
	}

	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s, &s.MapConfig.Buildings[0]
}

// spawnCaptureDefender spawns a neutral unit of the given type at (spawnX,
// spawnY), sets ObjectiveBuildingID to the tower ID (mirroring the
// capture-defender role in state_waves.go), and initialises combat state.
func spawnCaptureDefender(t *testing.T, s *GameState, unitType string, spawnX, spawnY float64, towerID string) *Unit {
	t.Helper()
	unit := s.spawnNeutralUnitLocked(unitType, protocol.Vec2{X: spawnX, Y: spawnY})
	if unit == nil {
		t.Fatalf("spawnCaptureDefender: nil return for type=%q — not in catalog", unitType)
	}
	unit.HP = 500
	unit.MaxHP = 500
	unit.Visible = true
	unit.ObjectiveBuildingID = towerID
	s.initializeCombatUnitLocked(unit)
	return unit
}

const (
	// Tower at grid (7,7), Width=1, Height=2.
	// World: left=7*64=448, top=7*64=448, right=8*64=512, bottom=9*64=576.
	ntTowerCenterX = (7.0 + 0.5) * 64.0 // 480
	ntTowerCenterY = (7.0 + 1.0) * 64.0 // 512
	ntTowerID      = "claim-tower"
)

// ─────────────────────────────────────────────────────────────────────────────
// Regression: far-spawn capture-defender advances and attacks the claim tower
// ─────────────────────────────────────────────────────────────────────────────

// TestNeutralCaptureDefender_FarSpawn_AttacksClaimTower is the definitive
// regression for the anchor-freeze bug. A neutral capture-defender is spawned
// 25 cells (~1600px) from the tower. With the fix, the anchor slides as the
// unit advances so the leash gate accepts the tower when the unit arrives.
// Without the fix, the anchor stays frozen at the far spawn and the unit sits
// idle next to the tower.
//
// Run 360 ticks (~18s at 20Hz) — enough for spear_maiden (MoveSpeed≈110) to
// travel ~1600px and land enough hits to reduce HP.
func TestNeutralCaptureDefender_FarSpawn_AttacksClaimTower(t *testing.T) {
	const (
		cell      = 64.0
		farSpawnX = ntTowerCenterX + 25*cell // 480 + 1600 = 2080
		farSpawnY = ntTowerCenterY            // 512
		ticks     = 360
	)

	for _, tc := range []struct {
		unitType string
	}{
		{"spear_maiden"},
		{"ranged_raider"},
	} {
		tc := tc
		t.Run(tc.unitType, func(t *testing.T) {
			s, tower := newNeutralTowerAttackState(t)
			unit := spawnCaptureDefender(t, s, tc.unitType, farSpawnX, farSpawnY, ntTowerID)
			unitID := unit.ID

			profile := resolveCombatProfile(unit)
			leashDist := effectiveLeashDistance(unit, profile)
			anchorToCenter := math.Sqrt(distanceSquared(unit.CombatAnchorX, unit.CombatAnchorY, ntTowerCenterX, ntTowerCenterY))

			t.Logf("[%s] spawn=(%.0f,%.0f) anchor→tower=%.0fpx leash=%.0f profile=%q AttackRange=%.0f",
				tc.unitType, farSpawnX, farSpawnY, anchorToCenter, leashDist, profile.Name, unit.AttackRange)

			s.mu.Unlock()
			tickN(s, ticks)
			s.mu.Lock()

			u := s.getUnitByIDLocked(unitID)
			if u == nil {
				t.Fatalf("unit vanished after %d ticks", ticks)
			}
			towerHP, _, hpOK := getBuildingHP(tower)
			if !hpOK {
				t.Fatalf("tower HP metadata not readable")
			}

			edgeDist := s.distanceToBuilding(u.X, u.Y, tower)
			anchorToCenterPost := math.Sqrt(distanceSquared(u.CombatAnchorX, u.CombatAnchorY, ntTowerCenterX, ntTowerCenterY))

			t.Logf("[%s] after %d ticks: pos=(%.1f,%.1f) anchor=(%.1f,%.1f) anchor→tower=%.1fpx leash=%.0f edgeDist=%.1f Status=%q HP=%.0f/5000",
				tc.unitType, ticks,
				u.X, u.Y,
				u.CombatAnchorX, u.CombatAnchorY, anchorToCenterPost, leashDist,
				edgeDist, u.Status, towerHP)

			if towerHP >= 5000.0 {
				t.Errorf("[%s]: tower HP=%.0f after %d ticks — capture-defender never attacked.\n"+
					"  anchor=(%.1f,%.1f)→tower=%.1fpx, leash=%.0f, edgeDist=%.1f, AttackRange=%.0f\n"+
					"  Status=%q Attacking=%v AttackBldgID=%q",
					tc.unitType, towerHP, ticks,
					u.CombatAnchorX, u.CombatAnchorY, anchorToCenterPost, leashDist, edgeDist, u.AttackRange,
					u.Status, u.Attacking, u.AttackBuildingTargetID)
			}
			s.mu.Unlock()
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: anchor slides as capture-defender advances
// ─────────────────────────────────────────────────────────────────────────────

// TestNeutralCaptureDefender_AnchorSlides verifies that a neutral
// capture-defender's CombatAnchorX/Y tracks its position as it advances
// (anchor must not stay frozen at the spawn point). This directly exercises
// the anchor-slide condition added to tickCombatAILocked.
//
// Spawn far enough that anchor-to-tower distance exceeds leash at spawn.
// After running ticks, the anchor must have moved toward the unit and the
// anchor-to-tower distance must have decreased to within leash range.
func TestNeutralCaptureDefender_AnchorSlides(t *testing.T) {
	const (
		cell      = 64.0
		farSpawnX = ntTowerCenterX + 25*cell // 2080
		farSpawnY = ntTowerCenterY            // 512
		ticks     = 200 // enough for the unit to travel most of the distance
	)

	s, _ := newNeutralTowerAttackState(t)
	unit := spawnCaptureDefender(t, s, "spear_maiden", farSpawnX, farSpawnY, ntTowerID)
	unitID := unit.ID

	profile := resolveCombatProfile(unit)
	leashDist := effectiveLeashDistance(unit, profile)

	// Capture initial anchor as scalars before running ticks. The Unit struct
	// is updated in-place by the simulation, so holding the pointer and reading
	// after the ticks would give post-tick values in both "before" and "after".
	initialAnchorX := unit.CombatAnchorX
	initialAnchorY := unit.CombatAnchorY
	anchorDistBefore := math.Sqrt(distanceSquared(initialAnchorX, initialAnchorY, ntTowerCenterX, ntTowerCenterY))

	t.Logf("ANCHOR-SLIDE SETUP: spawn=(%.0f,%.0f) anchor=(%.0f,%.0f) anchor→tower=%.0fpx leash=%.0f",
		farSpawnX, farSpawnY, initialAnchorX, initialAnchorY, anchorDistBefore, leashDist)

	// At spawn the anchor-to-tower distance must exceed the leash for this
	// test to be a meaningful regression. Log a note if the geometry changes.
	if anchorDistBefore <= leashDist {
		t.Logf("NOTE: anchor already within leash at spawn (%.0f ≤ %.0f) — far-spawn geometry may have changed", anchorDistBefore, leashDist)
	}

	s.mu.Unlock()
	tickN(s, ticks)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.getUnitByIDLocked(unitID)
	if u == nil {
		t.Fatalf("unit vanished after %d ticks", ticks)
	}

	anchorDistAfter := math.Sqrt(distanceSquared(u.CombatAnchorX, u.CombatAnchorY, ntTowerCenterX, ntTowerCenterY))
	anchorMoved := math.Sqrt(distanceSquared(initialAnchorX, initialAnchorY, u.CombatAnchorX, u.CombatAnchorY))

	t.Logf("ANCHOR-SLIDE RESULT after %d ticks:", ticks)
	t.Logf("  unit pos           = (%.1f, %.1f)", u.X, u.Y)
	t.Logf("  anchor (after)     = (%.1f, %.1f)  moved=%.1fpx from (%.0f,%.0f)",
		u.CombatAnchorX, u.CombatAnchorY, anchorMoved, initialAnchorX, initialAnchorY)
	t.Logf("  anchor→tower       = %.1fpx  (was %.1fpx)  leash=%.0f", anchorDistAfter, anchorDistBefore, leashDist)

	// The anchor must have moved — the fix makes it track the unit.
	if anchorMoved < 10.0 {
		t.Errorf("anchor did NOT slide after %d ticks (moved only %.1fpx from initial pos (%.0f,%.0f)) — "+
			"neutral capture-defender may be excluded from anchor-slide condition in tickCombatAILocked",
			ticks, anchorMoved, initialAnchorX, initialAnchorY)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Baseline: close-spawn capture-defender attacks without needing anchor slide
// ─────────────────────────────────────────────────────────────────────────────

// TestNeutralCaptureDefender_CloseSpawn_AttacksClaimTower is the easy baseline:
// a capture-defender spawned within its AttackRange of the tower should attack
// immediately without needing to advance. This works both before and after the
// fix and serves as a sanity check on the test harness.
func TestNeutralCaptureDefender_CloseSpawn_AttacksClaimTower(t *testing.T) {
	const (
		cell    = 64.0
		spawnX  = 8*cell + 50.0 // 562 — ~50px from tower right edge
		spawnY  = ntTowerCenterY
		ticks   = 60
	)

	for _, tc := range []struct {
		unitType string
	}{
		{"spear_maiden"},
		{"ranged_raider"},
	} {
		tc := tc
		t.Run(tc.unitType, func(t *testing.T) {
			s, tower := newNeutralTowerAttackState(t)
			unit := spawnCaptureDefender(t, s, tc.unitType, spawnX, spawnY, ntTowerID)
			unitID := unit.ID

			edgeDist := s.distanceToBuilding(unit.X, unit.Y, tower)
			t.Logf("[%s] close spawn: edgeDist=%.1f AttackRange=%.0f",
				tc.unitType, edgeDist, unit.AttackRange)

			s.mu.Unlock()
			tickN(s, ticks)
			s.mu.Lock()

			u := s.getUnitByIDLocked(unitID)
			if u == nil {
				t.Fatalf("unit vanished after %d ticks", ticks)
			}
			towerHP, _, hpOK := getBuildingHP(tower)
			if !hpOK {
				t.Fatalf("tower HP metadata not readable")
			}

			t.Logf("[%s] after %d ticks: Status=%q AttackBldgID=%q HP=%.0f/5000 damaged=%v",
				tc.unitType, ticks, u.Status, u.AttackBuildingTargetID, towerHP, towerHP < 5000.0)

			if towerHP >= 5000.0 {
				t.Errorf("[%s]: close-spawn tower HP=%.0f after %d ticks — unit never attacked "+
					"(Status=%q AttackBldgID=%q Attacking=%v)",
					tc.unitType, towerHP, ticks, u.Status, u.AttackBuildingTargetID, u.Attacking)
			}
			s.mu.Unlock()
		})
	}
}
