package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// newObjectiveTestState builds an obstacle-free map with one player townhall
// plus any `extra` buildings, all assembled into the MapConfig BEFORE
// NewGameStateWithSeed so the building index is constructed once and correctly
// (no post-construction slice append, which would reallocate the backing array
// and invalidate the index's element pointers). Returns the locked GameState.
func newObjectiveTestState(t *testing.T, extra ...protocol.BuildingTile) *GameState {
	t.Helper()
	const cell = 64.0
	cols, rows := 40, 24
	townhall := protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: 2, Y: 10}, ID: "townhall-1",
		BuildingType: "townhall", Width: 2, Height: 2,
		Occupied: true, Visible: true, OwnerID: &townhallOwnerID,
		Metadata: map[string]interface{}{"hp": 5000.0, "maxHp": 5000.0},
	}
	buildings := append([]protocol.BuildingTile{townhall}, extra...)
	cfg := protocol.MapConfig{
		ID: "obj-test", Name: "obj-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Obstacles: []protocol.ObstacleTile{},
		Buildings: buildings,
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	return s
}

// townhallOwnerID is a package-level addressable "p1" so BuildingTile.OwnerID
// (a *string) can point at a stable address shared across the test fixtures.
var townhallOwnerID = "p1"

// objBuilding is a fixture constructor for an extra attackable player building.
func objBuilding(id, btype string, gx, gy, w, h int, hp float64) protocol.BuildingTile {
	return protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: gx, Y: gy}, ID: id,
		BuildingType: btype, Width: w, Height: h,
		Occupied: true, Visible: true, OwnerID: &townhallOwnerID,
		Metadata: map[string]interface{}{"hp": hp, "maxHp": hp},
	}
}

func TestGetNearestPlayerTownhallBuilding_ReturnsTownhall(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	b := s.getNearestPlayerTownhallBuildingLocked(2000, 768)
	if b == nil || b.ID != "townhall-1" {
		t.Fatalf("want townhall-1, got %v", b)
	}
}

// Determinism: two attackable buildings the same distance from the enemy must
// resolve to the same one every call (lower ID wins), so seeded replays agree.
// a-tower (gx=11) right edge = 12*64 = 768; z-tower (gx=20) left edge =
// 20*64 = 1280; enemy at x=1024 (= (768+1280)/2), y=736 inside both towers'
// y-span [704,768] -> distanceToBuilding == 256 for BOTH (genuinely tied), so
// only the ID tiebreak decides, and it must pick "a-tower" every call.
func TestFindNearestAttackablePlayerBuilding_DeterministicTiebreak(t *testing.T) {
	s := newObjectiveTestState(t,
		objBuilding("a-tower", "tower", 11, 11, 1, 1, 100),
		objBuilding("z-tower", "tower", 20, 11, 1, 1, 100),
	)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1024, Y: 736})
	s.initializeCombatUnitLocked(enemy)

	for i := 0; i < 20; i++ {
		got := s.findNearestAttackablePlayerBuildingLocked(enemy)
		if got == nil || got.ID != "a-tower" {
			t.Fatalf("nondeterministic/incorrect tiebreak: call %d got %v want a-tower", i, got)
		}
	}
}

// An enemy with no objective yet self-acquires the townhall and plain-moves
// toward it WITHOUT setting an attack-building target or strike escalation.
func TestEnemyAdvanceToObjective_PlainMoveNoHardTarget(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)

	blocked := s.getBlockedCellsLocked()
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if enemy.ObjectiveBuildingID != "townhall-1" {
		t.Fatalf("want sticky ObjectiveBuildingID=townhall-1, got %q", enemy.ObjectiveBuildingID)
	}
	if enemy.AttackBuildingTargetID != "" {
		t.Fatalf("must NOT hard-target the building while advancing; got %q", enemy.AttackBuildingTargetID)
	}
	if enemy.UnreachableBuildingStrikeCount != 0 {
		t.Fatalf("must NOT run strike escalation; got %d", enemy.UnreachableBuildingStrikeCount)
	}
	if !enemy.Moving {
		t.Fatal("enemy should be moving toward the townhall")
	}
}

// While already advancing, the function must early-return without recomputing
// a path — the per-tick anti-churn guard.
func TestEnemyAdvanceToObjective_NoRepathWhileMoving(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	s.enemyAdvanceToObjectiveLocked(enemy, blocked)
	pathBefore := append([]protocol.Vec2(nil), enemy.Path...)
	tx, ty := enemy.TargetX, enemy.TargetY

	s.enemyAdvanceToObjectiveLocked(enemy, blocked) // second call, still Moving

	if enemy.TargetX != tx || enemy.TargetY != ty || len(enemy.Path) != len(pathBefore) {
		t.Fatal("advancing enemy must not recompute its path while Moving")
	}
}

// Objective destroyed mid-advance -> re-acquire the nearest player building.
func TestEnemyAdvanceToObjective_ReacquireOnObjectiveLoss(t *testing.T) {
	s := newObjectiveTestState(t, objBuilding("tower-1", "tower", 30, 11, 1, 1, 100))
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	enemy.ObjectiveBuildingID = "townhall-1"
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	// Destroy the townhall.
	s.MapConfig.Buildings[0].Metadata["hp"] = 0.0
	enemy.Moving = false
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if enemy.ObjectiveBuildingID != "tower-1" {
		t.Fatalf("want re-acquired ObjectiveBuildingID=tower-1, got %q", enemy.ObjectiveBuildingID)
	}
}

// Objective exists but is fully walled off -> fall back to engaging the
// nearest hostile (anti-spawn-freeze), still without hard-targeting.
func TestEnemyAdvanceToObjective_PartitionFallsBackToBlocker(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	// Full vertical unit-wall partition at x=1200 (same construction as the
	// walled-off regression test).
	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		w.Damage = 0
		w.Capabilities = nil
		s.initializeCombatUnitLocked(w)
	}
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1800, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	enemy.ObjectiveBuildingID = "townhall-1"
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	enemy.Moving = false
	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	if !enemy.Moving {
		t.Fatal("walled-off enemy must move toward a blocking hostile, not freeze")
	}
	if enemy.AttackBuildingTargetID != "" || enemy.UnreachableBuildingStrikeCount != 0 {
		t.Fatalf("fallback must not hard-target/escalate; bld=%q strikes=%d",
			enemy.AttackBuildingTargetID, enemy.UnreachableBuildingStrikeCount)
	}
}

// The spawn-time objective resolver seeds ObjectiveBuildingID for routed
// enemies (targetPlayerLabel and default), and leaves it empty for stay-at-
// spawn / static-objective units. Tests the extracted helper directly to
// avoid driving the wave-timer machinery.
func TestSpawnObjectiveSeeding(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 2200, Y: 768})
	if enemy == nil {
		t.Fatal("spawnEnemyUnitLocked returned nil")
	}
	s.seedEnemyObjectiveAtSpawnLocked(enemy, "", protocol.Vec2{X: 2200, Y: 768})
	if enemy.ObjectiveBuildingID != "townhall-1" {
		t.Fatalf("default route should seed townhall-1; got %q", enemy.ObjectiveBuildingID)
	}

	stay := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 2200, Y: 700})
	s.seedEnemyObjectiveAtSpawnLocked(stay, "__none__", protocol.Vec2{X: 2200, Y: 700})
	if stay.ObjectiveBuildingID != "" {
		t.Fatalf("stay-at-spawn (__none__) must NOT seed an objective; got %q", stay.ObjectiveBuildingID)
	}
}

// The spawn-time path destination resolver: capture defenders head for the
// passed claim tower; enemies with a target player head for that player's
// NEAREST building (a forward tower, not the townhall); everyone else heads for
// the nearest townhall.
func TestEnemySpawnPathDestination(t *testing.T) {
	// Townhall at (2,10); a forward tower much closer to the spawn at x≈2000.
	s := newObjectiveTestState(t, objBuilding("forward-tower", "tower", 30, 11, 1, 1, 100))
	defer s.mu.Unlock()

	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 2000, Y: 736})
	spawnPos := protocol.Vec2{X: 2000, Y: 736}

	// Capture defender → heads for the resolved capture destination, ignoring
	// player/townhall.
	capDest := protocol.Vec2{X: 1632, Y: 736}
	dest := s.enemySpawnPathDestinationLocked(enemy, "p1", spawnPos, &capDest)
	if dest == nil || *dest != capDest {
		t.Fatalf("capture defender should head for the capture destination; got %v want %v", dest, capDest)
	}

	// Target player, no capture → the player's NEAREST building (forward tower).
	nb := s.findNearestAttackableBuildingForPlayerLocked(enemy, "p1")
	if nb == nil || nb.ID != "forward-tower" {
		t.Fatalf("sanity: nearest p1 building to the spawn should be forward-tower, got %v", nb)
	}
	dest = s.enemySpawnPathDestinationLocked(enemy, "p1", spawnPos, nil)
	if want := s.buildingCenterLocked(nb); dest == nil || *dest != want {
		t.Fatalf("target-player enemy should head for the nearest player building; got %v want %v", dest, want)
	}

	// No target player, no capture → nearest townhall.
	dest = s.enemySpawnPathDestinationLocked(enemy, "", spawnPos, nil)
	wantTH := s.getNearestPlayerTownhallCenterLocked(spawnPos.X, spawnPos.Y)
	if dest == nil || wantTH == nil || *dest != *wantTH {
		t.Fatalf("default enemy should head for the nearest townhall; got %v want %v", dest, wantTH)
	}
}

// Neutral capture-defender: once its claim tower is invalid (gone/missing),
// it must NOT fall back to hunting any off-zone player building. It clears its
// ObjectiveBuildingID and idles so that selectBestTargetLocked picks up in-
// zone targets normally — the off-zone base fallback must never apply.
func TestEnemyAdvanceToObjective_NeutralClearsClearsAndIdlesOnInvalidObjective(t *testing.T) {
	// newObjectiveTestState places a townhall at grid (2,10) owned by "p1";
	// that acts as the off-zone base building the neutral must NOT route toward.
	// The neutral's ObjectiveBuildingID points at a nonexistent claim tower so
	// getBuildingByIDLocked returns nil → isValidHostileBuildingTarget is false.
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	neutral := s.spawnPlayerUnitLocked("soldier", neutralPlayerID, "#aaaaaa",
		protocol.Vec2{X: 1500, Y: 768})
	neutral.Visible = true
	neutral.MoveSpeed = 150
	neutral.ObjectiveBuildingID = "claim-tower-gone" // no such building in state
	s.initializeCombatUnitLocked(neutral)
	blocked := s.getBlockedCellsLocked()

	s.enemyAdvanceToObjectiveLocked(neutral, blocked)

	if neutral.ObjectiveBuildingID != "" {
		t.Errorf("neutral: ObjectiveBuildingID must be cleared; got %q", neutral.ObjectiveBuildingID)
	}
	if neutral.Moving {
		t.Errorf("neutral: must NOT start moving toward off-zone base after objective is gone")
	}
	if neutral.AttackBuildingTargetID != "" {
		t.Errorf("neutral: must NOT acquire an off-zone building target; got %q", neutral.AttackBuildingTargetID)
	}
}

// Contrast: an enemy unit with the same invalid objective DOES fall back to the
// nearest player building (the townhall), confirming the enemy re-acquisition
// path is unchanged by the neutral guard.
func TestEnemyAdvanceToObjective_EnemyStillReacquiresOnInvalidObjective(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1500, Y: 768})
	enemy.Visible = true
	enemy.MoveSpeed = 150
	enemy.ObjectiveBuildingID = "claim-tower-gone" // no such building → invalid
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	s.enemyAdvanceToObjectiveLocked(enemy, blocked)

	// Enemy must re-acquire a real building (the townhall) and start moving.
	if enemy.ObjectiveBuildingID == "" {
		t.Errorf("enemy: should have re-acquired a player building objective")
	}
	if !enemy.Moving {
		t.Errorf("enemy: should be moving toward the re-acquired objective")
	}
}

// End-to-end through the real sim: a routed enemy with a clear lane reaches
// and destroys the townhall via normal scoring (no hard-target while
// advancing), and engages an in-range player unit on the way then resumes.
func TestEnemy_AttackMovesToTownhall_EngagesEnRoute(t *testing.T) {
	s := newObjectiveTestState(t)
	ownerID := "p1"

	// One defender between the enemy and the townhall.
	def := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db",
		protocol.Vec2{X: 900, Y: 768})
	def.Visible = true
	def.MaxHP, def.HP = 60, 60
	def.MoveSpeed = 0
	s.initializeCombatUnitLocked(def)
	defID := def.ID

	// Use "raider" so the enemy profile has TargetBuildings=true; the soldier
	// profile omits it, so a soldier enemy would plain-move to the townhall but
	// never commit an attack via scoreBuildingTargetLocked.
	enemy := s.spawnPlayerUnitLocked("raider", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 2200, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 2000, 2000
	enemy.Damage = 25
	enemy.MoveSpeed = 180
	s.initializeCombatUnitLocked(enemy)
	enemyID := enemy.ID
	s.mu.Unlock()

	tickN(s, 600)

	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.unitsByID[enemyID]
	if e == nil {
		t.Fatal("enemy disappeared")
	}
	if d := s.unitsByID[defID]; d != nil && d.HP > 0 {
		t.Fatalf("enemy should have engaged & killed the en-route defender (hp=%d)", d.HP)
	}
	hp, _, _ := getBuildingHP(&s.MapConfig.Buildings[0])
	if hp >= 5000.0 {
		t.Fatalf("enemy should have reached and damaged the townhall; hp still %.0f", hp)
	}
}
