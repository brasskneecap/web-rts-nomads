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
