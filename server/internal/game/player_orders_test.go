package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// newOrderTestState returns a minimal GameState with seed 42 and a
// player-owned soldier at (400, 400) with combat stats ready to fight.
// Lock is NOT held on return.
func newOrderTestState(t *testing.T) (s *GameState, unit *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	unit = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	unit.MaxHP = 500
	unit.HP = 500
	unit.Visible = true
	unit.AttackRange = 80
	unit.Damage = 10
	unit.AttackSpeed = 1.0
	unit.AttackCooldown = 0
	unit.MoveSpeed = 150
	s.initializeCombatUnitLocked(unit)
	return s, unit
}

// spawnOrderEnemy adds a live, visible enemy at (x,y).
func spawnOrderEnemy(t *testing.T, s *GameState, x, y float64) *Unit {
	t.Helper()
	e := s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{X: x, Y: y})
	e.MaxHP = 500
	e.HP = 500
	e.Visible = true
	e.AttackRange = 80
	e.Damage = 5
	e.AttackSpeed = 1.0
	e.MoveSpeed = 150
	s.initializeCombatUnitLocked(e)
	return e
}

// tickN drives s.Update() n times with dt=0.05 (20 Hz).
func tickN(s *GameState, n int) {
	const dt = 0.05
	for i := 0; i < n; i++ {
		s.Update(dt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. OrderHold does not chase out-of-range enemies
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderHold_DoesNotChaseOutOfRangeEnemy places a unit on Hold and spawns
// an enemy just outside AttackRange. After 200 ticks the unit must not have
// moved and must not have acquired the target.
func TestOrderHold_DoesNotChaseOutOfRangeEnemy(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	startX, startY := unit.X, unit.Y
	unitID := unit.ID
	unit.Order = OrderState{Type: OrderHold, HoldX: unit.X, HoldY: unit.Y}
	unit.CombatAnchorX = unit.X
	unit.CombatAnchorY = unit.Y

	// Enemy just outside AttackRange (AttackRange = 80, so 80+50 = 130 away).
	// MoveSpeed and Capabilities are cleared so the enemy AI doesn't advance
	// into range, which would be legitimate Hold engagement rather than chasing.
	enemy := spawnOrderEnemy(t, s, unit.X+unit.AttackRange+50, unit.Y)
	enemy.MoveSpeed = 0
	enemy.Capabilities = nil // no "attack" → excluded from unitUsesCombatAI
	enemyID := enemy.ID
	s.mu.Unlock()

	tickN(s, 200)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit was removed unexpectedly")
	}
	if u.Moving {
		t.Errorf("Hold unit should not be moving; Moving=%v", u.Moving)
	}
	if u.X != startX || u.Y != startY {
		t.Errorf("Hold unit moved: was (%.1f,%.1f), now (%.1f,%.1f)", startX, startY, u.X, u.Y)
	}
	if u.AttackTargetID == enemyID {
		t.Errorf("Hold unit acquired out-of-range enemy; AttackTargetID=%d", u.AttackTargetID)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. OrderHold fires at in-range enemies
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderHold_FiresAtInRangeEnemy places a unit on Hold and spawns an enemy
// just inside AttackRange. After a few ticks the unit should have acquired the
// target and dealt damage.
func TestOrderHold_FiresAtInRangeEnemy(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	unitID := unit.ID
	unit.Order = OrderState{Type: OrderHold, HoldX: unit.X, HoldY: unit.Y}
	unit.CombatAnchorX = unit.X
	unit.CombatAnchorY = unit.Y

	// Enemy just inside AttackRange (AttackRange = 80, so 80-10 = 70 away).
	enemy := spawnOrderEnemy(t, s, unit.X+unit.AttackRange-10, unit.Y)
	enemyID := enemy.ID
	enemyStartHP := enemy.HP
	s.mu.Unlock()

	// Tick enough for the AI to evaluate and fire at least once (1/AttackSpeed = 1 s → 20 ticks at 20 Hz).
	tickN(s, 40)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit was removed unexpectedly")
	}
	if u.AttackTargetID != enemyID {
		t.Errorf("Hold unit did not acquire in-range enemy; AttackTargetID=%d, want %d", u.AttackTargetID, enemyID)
	}
	e := s.unitsByID[enemyID]
	if e != nil && e.HP >= enemyStartHP {
		t.Errorf("Hold unit did not deal damage; enemy HP=%d, started at %d", e.HP, enemyStartHP)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. OrderHold drops target that walks out of range
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderHold_DropsTargetWhenEnemyLeavesRange verifies that a Hold unit that
// has acquired a target drops it (and does not chase) when the enemy walks out
// of AttackRange.
func TestOrderHold_DropsTargetWhenEnemyLeavesRange(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	startX, startY := unit.X, unit.Y
	unitID := unit.ID
	unit.Order = OrderState{Type: OrderHold, HoldX: unit.X, HoldY: unit.Y}
	unit.CombatAnchorX = unit.X
	unit.CombatAnchorY = unit.Y

	// Spawn enemy in range so the unit acquires it.
	enemy := spawnOrderEnemy(t, s, unit.X+unit.AttackRange-10, unit.Y)
	enemyID := enemy.ID
	s.mu.Unlock()

	// Let the unit acquire the enemy.
	tickN(s, 10)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed before acquisition check")
	}
	acquired := u.AttackTargetID == enemyID
	s.mu.RUnlock()

	if !acquired {
		t.Skip("unit did not acquire enemy in-range; skipping out-of-range drop test")
	}

	// Move the enemy well outside AttackRange.
	s.mu.Lock()
	e := s.unitsByID[enemyID]
	if e != nil {
		e.X = unit.X + unit.AttackRange + 200
		e.Y = unit.Y
	}
	s.mu.Unlock()

	// Give the AI several ticks to react.
	tickN(s, 20)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed after enemy moved away")
	}
	if u.AttackTargetID == enemyID {
		t.Errorf("Hold unit did not drop out-of-range target; AttackTargetID=%d", u.AttackTargetID)
	}
	if u.Moving {
		t.Errorf("Hold unit is Moving after dropping target; should stay put")
	}
	if u.X != startX || u.Y != startY {
		t.Errorf("Hold unit moved: was (%.1f,%.1f), now (%.1f,%.1f)", startX, startY, u.X, u.Y)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. OrderPatrol swaps waypoints on arrival
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderPatrol_SwapsWaypointsOnArrival places a unit at A, issues a patrol
// to B, ticks until it arrives near B, then asserts the waypoints have been
// swapped and the unit is moving back toward A.
func TestOrderPatrol_SwapsWaypointsOnArrival(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	// A = (400,400) (unit start), B = (600,400).
	destB := protocol.Vec2{X: 600, Y: 400}

	s.mu.Lock()
	startX := unit.X
	s.mu.Unlock()

	s.PatrolUnits("p1", []int{unitID}, destB)

	// Tick up to 400 ticks (20 s at 20 Hz) for the unit to reach B.
	const maxTicks = 400
	arrivedNearB := false
	for i := 0; i < maxTicks; i++ {
		s.Update(0.05)
		s.mu.RLock()
		u := s.unitsByID[unitID]
		if u == nil {
			s.mu.RUnlock()
			t.Fatal("unit was removed during patrol")
		}
		dist := math.Sqrt(math.Pow(u.X-destB.X, 2) + math.Pow(u.Y-destB.Y, 2))
		if dist < 30 {
			arrivedNearB = true
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}

	if !arrivedNearB {
		t.Fatal("unit did not reach patrol destination B within 400 ticks")
	}

	// Let the AI react (resume standing order, flip waypoints).
	tickN(s, 5)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed after arrival at B")
	}
	if u.Order.Type != OrderPatrol {
		t.Errorf("order type after arrival: got %v, want OrderPatrol", u.Order.Type)
	}
	// After flip, current dest should be near A (startX, 400).
	const tol = 5.0
	if math.Abs(u.Order.DestX-startX) > tol || math.Abs(u.Order.DestY-400) > tol {
		t.Errorf("waypoint not flipped to A: Order.Dest=(%.1f,%.1f), want ~(%.1f,400)", u.Order.DestX, u.Order.DestY, startX)
	}
	// Unit should be heading back toward A.
	if !u.Moving {
		t.Errorf("unit should be Moving back toward A after waypoint flip; Moving=false")
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. OrderPatrol engages an enemy and resumes after kill
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderPatrol_EngagesAndResumesAfterKill issues a patrol to a unit, places
// a weak enemy in its detection path, asserts it acquires the target, then after
// the enemy dies asserts OrderPatrol is still set and the unit resumes movement.
func TestOrderPatrol_EngagesAndResumesAfterKill(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	destB := protocol.Vec2{X: 600, Y: 400}
	s.PatrolUnits("p1", []int{unitID}, destB)

	// Drop a very low-HP enemy midway on the patrol route so it dies quickly.
	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, 500, 400)
	enemy.HP = 1
	enemy.MaxHP = 1
	enemyID := enemy.ID
	s.mu.Unlock()

	// Tick until the enemy is dead (max 200 ticks).
	const maxTicks = 200
	enemyDied := false
	for i := 0; i < maxTicks; i++ {
		s.Update(0.05)
		s.mu.RLock()
		if s.unitsByID[enemyID] == nil {
			enemyDied = true
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}

	if !enemyDied {
		t.Fatal("enemy did not die within 200 ticks")
	}

	// Give the AI a tick or two to resume patrol.
	tickN(s, 5)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("patrol unit was removed")
	}
	if u.Order.Type != OrderPatrol {
		t.Errorf("expected OrderPatrol after kill, got %v", u.Order.Type)
	}
	if u.AttackTargetID != 0 {
		t.Errorf("AttackTargetID should be 0 after enemy died, got %d", u.AttackTargetID)
	}
	if !u.Moving {
		t.Errorf("patrol unit should be Moving after kill; Moving=false")
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. OrderAttackTarget survives leash bypass
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderAttackTarget_SurvivesLeashBypass issues an attack on a distant enemy
// and verifies the unit moves toward it (does not drop due to leash) after
// several ticks.
func TestOrderAttackTarget_SurvivesLeashBypass(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	// Spawn enemy far away — well past any normal leash distance.
	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, unit.X+600, unit.Y)
	enemyID := enemy.ID
	s.mu.Unlock()

	s.AttackWithUnits("p1", []int{unitID}, enemyID)

	// A few ticks should not drop the target, and the unit should be moving.
	tickN(s, 20)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed unexpectedly")
	}
	if u.AttackTargetID != enemyID {
		t.Errorf("OrderAttackTarget was dropped due to leash; AttackTargetID=%d, want %d", u.AttackTargetID, enemyID)
	}
	if !u.Moving {
		t.Errorf("unit should be Moving toward distant enemy; Moving=false")
	}
	if u.Order.Type != OrderAttackTarget {
		t.Errorf("order type: got %v, want OrderAttackTarget", u.Order.Type)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. MoveUnits supersedes OrderAttackTarget
// ─────────────────────────────────────────────────────────────────────────────

// TestMoveUnits_SupersedesOrderAttackTarget verifies that issuing MoveUnits to
// a unit currently on OrderAttackTarget clears the attack order and target.
func TestMoveUnits_SupersedesOrderAttackTarget(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, unit.X+600, unit.Y)
	enemyID := enemy.ID
	s.mu.Unlock()

	s.AttackWithUnits("p1", []int{unitID}, enemyID)

	// Verify attack order is set.
	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u.Order.Type != OrderAttackTarget {
		t.Fatalf("expected OrderAttackTarget after AttackWithUnits, got %v", u.Order.Type)
	}
	s.mu.RUnlock()

	// Now issue a move command to a different location.
	moveDest := protocol.Vec2{X: 200, Y: 400}
	s.MoveUnits("p1", []int{unitID}, moveDest)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed unexpectedly")
	}
	if u.Order.Type != OrderMove {
		t.Errorf("expected OrderMove after MoveUnits, got %v", u.Order.Type)
	}
	if u.AttackTargetID != 0 {
		t.Errorf("AttackTargetID should be 0 after MoveUnits, got %d", u.AttackTargetID)
	}
	s.mu.RUnlock()

	// After a tick the unit should not re-acquire the enemy (OrderMove suppresses AI).
	tickN(s, 5)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	if u.AttackTargetID == enemyID {
		t.Errorf("unit re-acquired enemy during OrderMove; AttackTargetID=%d", u.AttackTargetID)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. OrderMove ignores enemies en route
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderMove_IgnoresEnemiesEnRoute issues a force-move and drops an enemy
// directly in the unit's path. The unit must not acquire the enemy during
// transit.
func TestOrderMove_IgnoresEnemiesEnRoute(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	// Move to a far destination.
	moveDest := protocol.Vec2{X: 700, Y: 400}
	s.MoveUnits("p1", []int{unitID}, moveDest)

	// Place an enemy directly in the path, in normal detection range.
	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, 550, 400)
	enemyID := enemy.ID
	s.mu.Unlock()

	// Tick up to 200 ticks; the unit should never acquire the enemy.
	const maxTicks = 200
	for i := 0; i < maxTicks; i++ {
		s.Update(0.05)
		s.mu.RLock()
		u := s.unitsByID[unitID]
		if u == nil {
			s.mu.RUnlock()
			t.Fatal("unit removed during OrderMove")
		}
		if u.AttackTargetID == enemyID {
			s.mu.RUnlock()
			t.Errorf("OrderMove unit acquired enemy at tick %d; AttackTargetID=%d", i+1, enemyID)
			return
		}
		// Stop if unit arrived.
		dist := math.Sqrt(math.Pow(u.X-moveDest.X, 2) + math.Pow(u.Y-moveDest.Y, 2))
		if dist < 20 {
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}
}
