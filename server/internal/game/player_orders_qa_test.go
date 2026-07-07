package game

// player_orders_qa_test.go — QA-authored tests covering acceptance criteria
// not fully exercised by player_orders_test.go.
//
// AC2  – Retarget: projectile still lands on A; AttackTargetID becomes B; ThreatTable keeps A's entries.
// AC3  – Command-supersedes-attack: Path populated after MoveUnits (extends existing test).
// AC5  – Patrol repeats indefinitely: two full A→B→A cycles observed.
// AC6  – AttackMove: engages mid-path, kills, resumes with OrderAttackMove intact.
// AC8  – Determinism: same seed + same intent stream → identical unit state every tick.
// AC11 – Hold under retreat conditions: Path stays nil, unit does not move.
// AC13 – AttackTarget on invisible target: order demotes to OrderIdle.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// NonCombat flag — workers never auto-aggro
// ─────────────────────────────────────────────────────────────────────────────

// TestNonCombatWorker_DoesNotAutoAcquire verifies that a worker (NonCombat=true
// from worker.json) standing idle next to an enemy will not acquire it. The
// worker has `"attack"` capability and non-zero Damage so it passes
// unitUsesCombatAI, but the NonCombat gate in tickCombatAILocked must skip
// evaluateCombatLocked for any unit whose Order.Type is not OrderAttackTarget.
func TestNonCombatWorker_DoesNotAutoAcquire(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	s.mu.Lock()
	worker := s.spawnPlayerUnitLocked("worker", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	worker.Visible = true
	if !worker.NonCombat {
		s.mu.Unlock()
		t.Fatalf("worker.NonCombat should be true from catalog; got false")
	}
	workerID := worker.ID

	// Spawn a stationary enemy well within the worker's AttackRange.
	enemy := spawnOrderEnemy(t, s, worker.X+30, worker.Y)
	enemy.MoveSpeed = 0
	enemy.Capabilities = nil
	enemyID := enemy.ID
	s.mu.Unlock()

	tickN(s, 60)

	s.mu.RLock()
	w := s.unitsByID[workerID]
	e := s.unitsByID[enemyID]
	if w == nil {
		s.mu.RUnlock()
		t.Fatal("worker was removed unexpectedly")
	}
	if w.AttackTargetID != 0 {
		t.Errorf("NonCombat worker auto-acquired target %d; AttackTargetID must be 0", w.AttackTargetID)
	}
	if w.Attacking {
		t.Errorf("NonCombat worker is Attacking; expected no auto-engagement")
	}
	if e == nil {
		t.Error("enemy was destroyed by worker; worker should not have engaged")
	}
	s.mu.RUnlock()
}

// TestNonCombatWorker_AttacksWhenExplicitlyOrdered verifies that AttackWithUnits
// still works on a NonCombat worker — the player's explicit order must be the
// one path that bypasses the auto-aggro gate.
func TestNonCombatWorker_AttacksWhenExplicitlyOrdered(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	s.mu.Lock()
	worker := s.spawnPlayerUnitLocked("worker", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	worker.Visible = true
	workerID := worker.ID

	enemy := spawnOrderEnemy(t, s, worker.X+30, worker.Y)
	enemy.HP = 10
	enemy.MaxHP = 10
	enemy.MoveSpeed = 0
	enemy.Capabilities = nil
	enemyID := enemy.ID
	s.mu.Unlock()

	s.AttackWithUnits("p1", []int{workerID}, enemyID)

	s.mu.RLock()
	w := s.unitsByID[workerID]
	if w == nil {
		s.mu.RUnlock()
		t.Fatal("worker was removed after AttackWithUnits")
	}
	if w.Order.Type != OrderAttackTarget {
		t.Errorf("Order.Type = %v after AttackWithUnits; want OrderAttackTarget", w.Order.Type)
	}
	if w.AttackTargetID != enemyID {
		t.Errorf("AttackTargetID = %d; want %d", w.AttackTargetID, enemyID)
	}
	s.mu.RUnlock()

	// Tick until the enemy dies.
	const maxTicks = 200
	died := false
	for i := 0; i < maxTicks; i++ {
		s.Update(0.05)
		s.mu.RLock()
		if s.unitsByID[enemyID] == nil {
			died = true
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}
	if !died {
		t.Fatalf("worker did not kill 10-HP enemy within %d ticks despite explicit attack order", maxTicks)
	}

	// After the kill, the worker must demote to OrderIdle and stop engaging.
	tickN(s, 3)
	s.mu.RLock()
	w = s.unitsByID[workerID]
	if w.Order.Type != OrderIdle {
		t.Errorf("Order.Type after kill = %v; want OrderIdle (worker should go passive again)", w.Order.Type)
	}
	if w.AttackTargetID != 0 {
		t.Errorf("AttackTargetID after kill = %d; want 0", w.AttackTargetID)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// OrderPickupLoot — pickup-bound units ignore enemies en route (like OrderMove)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderPickupLoot_DoesNotAutoAcquireEnemy pins the contract documented on
// the OrderPickupLoot enum (state.go): "Combat AI does NOT engage on the way
// (matches OrderMove semantics — the player wants the chest, not a fight)."
//
// Reproduces the reported bug: a unit commanded to pick up a chest, with an
// enemy standing next to its path, auto-acquires the enemy and abandons the
// pickup order. The pickup-bound unit must keep OrderPickupLoot / PickupLootID
// and never set AttackTargetID.
func TestOrderPickupLoot_DoesNotAutoAcquireEnemy(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	unitID := unit.ID

	// newOrderTestState spawns the unit but registers no Player; without a
	// Players["p1"] entry tickLootDropsLocked cancels the pickup order via its
	// player-nil guard, masking the combat-acquisition behavior under test.
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	// A chest far to the east so the unit is still walking toward it (order
	// stays active) for the whole test window.
	drop := &LootDrop{
		ID:             "loot-test-pickup",
		X:              unit.X + 1200,
		Y:              unit.Y,
		ResourceGrants: map[string]int{"gold": 10},
		IconKey:        chestIconKeyDefault,
	}
	s.LootDrops = map[string]*LootDrop{drop.ID: drop}

	// Stationary, inert enemy right next to the unit's start — squarely inside
	// acquisition range. Capabilities nil keeps it from moving/attacking so the
	// test isolates our unit's auto-acquisition behavior.
	enemy := spawnOrderEnemy(t, s, unit.X+30, unit.Y)
	enemy.MoveSpeed = 0
	enemy.Capabilities = nil
	enemyID := enemy.ID
	s.mu.Unlock()

	// Issue the real player pickup command (acquires its own lock).
	s.PickupLootWithUnits("p1", []int{unitID}, drop.ID)

	tickN(s, 40)

	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed unexpectedly")
	}
	if u.AttackTargetID != 0 {
		t.Errorf("pickup-bound unit auto-acquired enemy %d; AttackTargetID must stay 0", u.AttackTargetID)
	}
	if u.Order.Type != OrderPickupLoot {
		t.Errorf("Order.Type = %v, want OrderPickupLoot (pickup order was overwritten by combat)", u.Order.Type)
	}
	if u.PickupLootID != drop.ID {
		t.Errorf("PickupLootID = %q, want %q (pickup intent lost)", u.PickupLootID, drop.ID)
	}
	if e := s.unitsByID[enemyID]; e != nil && e.HP < e.MaxHP {
		t.Errorf("pickup-bound unit attacked the enemy (HP=%d/%d); it should have walked past", e.HP, e.MaxHP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Retaliation — idle unit fights back when shot from beyond detection range
// ─────────────────────────────────────────────────────────────────────────────

// TestIdleUnit_RetaliatesAgainstOutOfSightAttacker reproduces the bug where a
// soldier (profile DetectionRange=240, AttackRange=60) standing idle was sat
// still while a ranged attacker pelted it from 280px away — outside the
// soldier's detection radius but well within the attacker's reach.
//
// With the threat-table acquisition path, any hostile that has dealt damage
// is eligible regardless of detection / leash, so the soldier engages.
func TestIdleUnit_RetaliatesAgainstOutOfSightAttacker(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	unitID := unit.ID
	// Use the real soldier profile so DetectionRange (240) and LeashDistance
	// (230) come from combatProfiles. AttackRange stays at the test default
	// of 80 for melee engagement once we close the gap.
	unit.UnitType = "soldier"
	unit.Archetype = "soldier"
	unit.AttackRange = 80

	// Stationary archer 280px away — outside the soldier's profile
	// DetectionRange (240) so the spatial query never finds them. The archer
	// has its attack range pushed to 320 so it can fire from there.
	enemy := spawnOrderEnemy(t, s, unit.X+280, unit.Y)
	enemy.UnitType = "archer"
	enemy.Archetype = "archer"
	enemy.AttackRange = 320
	enemy.MoveSpeed = 0 // hold position so distance stays > soldier detection
	enemy.AttackCooldown = 0
	enemyID := enemy.ID
	s.mu.Unlock()

	// Tick long enough for the archer to land at least one shot and the
	// soldier to react. Projectile travel + cooldown means we want at least
	// ~30 ticks before asserting acquisition.
	const ticksToFire = 60
	acquired := false
	for i := 0; i < ticksToFire; i++ {
		s.Update(0.05)
		s.mu.RLock()
		u := s.unitsByID[unitID]
		if u == nil {
			s.mu.RUnlock()
			t.Fatal("soldier removed before retaliating")
		}
		if u.AttackTargetID == enemyID {
			acquired = true
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}
	if !acquired {
		s.mu.RLock()
		u := s.unitsByID[unitID]
		t.Errorf("soldier did not acquire out-of-sight attacker within %d ticks; AttackTargetID=%d Status=%q",
			ticksToFire, u.AttackTargetID, u.Status)
		s.mu.RUnlock()
	}
}

// TestHoldUnit_DoesNotChaseOutOfSightAttacker is the opposite-direction guard:
// a Hold unit must not break formation to chase an attacker, even one that has
// added itself to the threat table by dealing damage. Hold's contract is
// "engage in-range only" and the threat-table path explicitly skips Hold.
func TestHoldUnit_DoesNotChaseOutOfSightAttacker(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	unitID := unit.ID
	startX, startY := unit.X, unit.Y
	unit.Order = OrderState{Type: OrderHold, HoldX: unit.X, HoldY: unit.Y}
	unit.CombatAnchorX = unit.X
	unit.CombatAnchorY = unit.Y

	enemy := spawnOrderEnemy(t, s, unit.X+280, unit.Y)
	enemy.UnitType = "archer"
	enemy.Archetype = "archer"
	enemy.AttackRange = 320
	enemy.MoveSpeed = 0
	enemy.AttackCooldown = 0
	s.mu.Unlock()

	for i := 0; i < 80; i++ {
		s.Update(0.05)
		s.mu.RLock()
		u := s.unitsByID[unitID]
		if u == nil {
			s.mu.RUnlock()
			break
		}
		dist := math.Sqrt(math.Pow(u.X-startX, 2) + math.Pow(u.Y-startY, 2))
		if dist > 5.0 {
			s.mu.RUnlock()
			t.Errorf("tick %d: Hold unit moved %.1fpx from start; threat-table path must not override Hold", i+1, dist)
			return
		}
		s.mu.RUnlock()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC2 – Retarget
// ─────────────────────────────────────────────────────────────────────────────

// TestRetarget_ProjectileLandsOnOriginalTarget fires a projectile at enemy A,
// then immediately issues AttackWithUnits to enemy B. The in-flight projectile
// must still target A (TargetUnitID == A) in the tick after it fires, the
// unit's AttackTargetID must be B, and enemy A's ThreatTable must contain the
// attacker ID after the projectile lands.
//
// Projectile travel time = distance / defaultProjectileSpeed (500 px/s).
// At 300px travel time = 0.6s = 12 ticks — well above the 1-tick minimum so
// the projectile stays in-flight between Update() calls.
func TestRetarget_ProjectileLandsOnOriginalTarget(t *testing.T) {
	s, unit := newOrderTestState(t)

	// Make the unit an archer so it fires projectiles instead of instant-hit melee.
	// Set Archetype explicitly so resolveCombatProfile returns the archer profile
	// without relying on inferCombatArchetype heuristics.
	s.mu.Lock()
	unit.UnitType = "archer"
	unit.Archetype = "archer"
	unitID := unit.ID
	unit.AttackRange = 400 // enough to reach both enemies
	unit.AttackCooldown = 0

	// Enemy A at 300px — travel time 0.6s (12 ticks) → well above dt=0.05.
	enemyA := spawnOrderEnemy(t, s, unit.X+300, unit.Y)
	enemyAID := enemyA.ID
	// Enemy B slightly offset so the unit's approach path differs.
	enemyB := spawnOrderEnemy(t, s, unit.X+310, unit.Y+20)
	enemyBID := enemyB.ID
	s.mu.Unlock()

	// Issue attack on A.
	s.AttackWithUnits("p1", []int{unitID}, enemyAID)

	// Tick through the swing's windup. The unit was spawned via the order
	// test harness with the soldier default AttackSpeed=1.0 (the test only
	// reassigns UnitType, not AttackSpeed), so windup = min(1, 1/1.0) ×
	// attackDamageDeliveryFraction = 0.7s = 14 decay ticks at 0.05s. Add
	// a few ticks for the begin-windup tick and post-fire projectile
	// inspection — travel time 0.6s keeps it in-flight here.
	for i := 0; i < 17; i++ {
		s.Update(0.05)
	}

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		s.mu.RUnlock()
		t.Fatal("unit removed unexpectedly after windup completed")
	}
	projectileCount := 0
	for _, proj := range s.Projectiles {
		if proj.OwnerUnitID == unitID {
			projectileCount++
			if proj.TargetUnitID != enemyAID {
				t.Errorf("in-flight projectile targets unit %d, want %d (enemy A)", proj.TargetUnitID, enemyAID)
			}
		}
	}
	s.mu.RUnlock()

	if projectileCount == 0 {
		t.Fatalf("no in-flight projectile found after windup completed; archer profile or attack path is not working as expected")
	}

	// Retarget to B while projectile is still in-flight.
	s.AttackWithUnits("p1", []int{unitID}, enemyBID)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	if u == nil {
		t.Fatal("unit removed after retarget")
	}

	// AttackTargetID must now be B.
	if u.AttackTargetID != enemyBID {
		t.Errorf("AttackTargetID = %d after retarget, want %d (enemy B)", u.AttackTargetID, enemyBID)
	}

	// In-flight projectile TargetUnitID must still be A — retarget must not
	// alter already-fired projectiles.
	for _, proj := range s.Projectiles {
		if proj.OwnerUnitID == unitID {
			if proj.TargetUnitID != enemyAID {
				t.Errorf("in-flight projectile was retargeted to unit %d; should still be %d (A)", proj.TargetUnitID, enemyAID)
			}
		}
	}
	s.mu.RUnlock()

	// Tick until the projectile lands (12 ticks total; give 20 to be safe).
	tickN(s, 20)

	// Threat is populated on the _target_ (enemy A) when damage lands via
	// addThreatLocked inside resolveAttackHitLocked → onUnitDamagedLocked.
	s.mu.RLock()
	eA := s.unitsByID[enemyAID]
	if eA != nil {
		if _, hasThreat := eA.ThreatTable[unitID]; !hasThreat {
			t.Errorf("ThreatTable on enemy A does not contain attacker ID %d after projectile landed; threat should survive retarget", unitID)
		}
	}
	// If eA is nil, enemy A died from the hit — the threat evidence is gone but
	// that just means it was a one-shot. The projectile behavior itself was
	// verified by TargetUnitID check above.

	// AttackTargetID should still be B (or 0 if B walked out of range, but
	// B is stationary so it should still be targeted).
	u = s.unitsByID[unitID]
	eB := s.unitsByID[enemyBID]
	if u != nil && eB != nil && u.AttackTargetID != enemyBID {
		t.Errorf("AttackTargetID = %d after projectile landing, want %d (enemy B)", u.AttackTargetID, enemyBID)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// AC3 – Command supersedes attack: assert Path is populated
// ─────────────────────────────────────────────────────────────────────────────

// TestMoveUnits_SupersedesOrderAttackTarget_PathPopulated is a targeted
// extension of the existing AC3 test. It asserts that after issuing MoveUnits
// to a unit on OrderAttackTarget, Path is non-nil and points generally toward
// the move destination (not the old attack target).
func TestMoveUnits_SupersedesOrderAttackTarget_PathPopulated(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, unit.X+600, unit.Y) // far enemy
	enemyID := enemy.ID
	s.mu.Unlock()

	s.AttackWithUnits("p1", []int{unitID}, enemyID)

	moveDest := protocol.Vec2{X: 200, Y: 200} // different direction from enemy
	s.MoveUnits("p1", []int{unitID}, moveDest)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		s.mu.RUnlock()
		t.Fatal("unit removed unexpectedly")
	}
	if u.Order.Type != OrderMove {
		t.Errorf("Order.Type = %v, want OrderMove", u.Order.Type)
	}
	if u.AttackTargetID != 0 {
		t.Errorf("AttackTargetID = %d after MoveUnits, want 0", u.AttackTargetID)
	}
	if len(u.Path) == 0 {
		t.Errorf("Path is empty after MoveUnits; unit should be moving toward (%.0f,%.0f)", moveDest.X, moveDest.Y)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// AC5 – Patrol repeats indefinitely (two full A→B→A cycles)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderPatrol_TwoFullCycles runs a patrol unit through at least two complete
// A→B→A loops and asserts OrderPatrol is maintained throughout.
func TestOrderPatrol_TwoFullCycles(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	// A = (400,400), B = (600,400). Short enough to complete quickly.
	destB := protocol.Vec2{X: 600, Y: 400}
	startX := unit.X // 400

	s.PatrolUnits("p1", []int{unitID}, destB)

	// Helper: wait until the unit is within radius of a target point.
	waitNearPoint := func(targetX, targetY, radius float64, maxTicks int, label string) bool {
		for i := 0; i < maxTicks; i++ {
			s.Update(0.05)
			s.mu.RLock()
			u := s.unitsByID[unitID]
			if u == nil {
				s.mu.RUnlock()
				t.Fatalf("unit removed during %s", label)
			}
			dist := math.Sqrt(math.Pow(u.X-targetX, 2) + math.Pow(u.Y-targetY, 2))
			order := u.Order.Type
			s.mu.RUnlock()
			if order != OrderPatrol {
				t.Errorf("%s: Order.Type = %v, want OrderPatrol", label, order)
				return false
			}
			if dist < radius {
				return true
			}
		}
		return false
	}

	const arrival = 30.0
	const maxTicks = 500 // 25s at 20 Hz per leg — generous for 200px at 150px/s

	// Cycle 1: A → B
	if !waitNearPoint(destB.X, destB.Y, arrival, maxTicks, "cycle1 A→B") {
		t.Fatal("unit did not reach B in cycle 1")
	}
	// Cycle 1: B → A
	if !waitNearPoint(startX, 400, arrival, maxTicks, "cycle1 B→A") {
		t.Fatal("unit did not return to A in cycle 1")
	}
	// Cycle 2: A → B
	if !waitNearPoint(destB.X, destB.Y, arrival, maxTicks, "cycle2 A→B") {
		t.Fatal("unit did not reach B in cycle 2")
	}

	// Final state check: still OrderPatrol, still moving.
	tickN(s, 5)
	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		s.mu.RUnlock()
		t.Fatal("unit removed at end of cycle 2")
	}
	if u.Order.Type != OrderPatrol {
		t.Errorf("Order.Type after 2 cycles = %v, want OrderPatrol", u.Order.Type)
	}
	s.mu.RUnlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// AC6 – AttackMove: engages mid-path, kills, resumes with OrderAttackMove
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderAttackMove_EngagesAndResumesAfterKill issues an AMove to a distant
// destination, places a very low-HP enemy midway, waits for it to die, then
// asserts OrderAttackMove is still set and the unit is moving toward the dest.
func TestOrderAttackMove_EngagesAndResumesAfterKill(t *testing.T) {
	s, unit := newOrderTestState(t)
	unitID := unit.ID

	// AMove toward a far destination.
	amoveDest := protocol.Vec2{X: 700, Y: 400}
	s.AttackMoveUnits("p1", []int{unitID}, amoveDest)

	// Spawn a very low-HP enemy midway so it dies quickly when the unit spots it.
	s.mu.Lock()
	midEnemy := spawnOrderEnemy(t, s, 550, 400)
	midEnemy.HP = 1
	midEnemy.MaxHP = 1
	midEnemy.MoveSpeed = 0
	enemyID := midEnemy.ID
	s.mu.Unlock()

	// Verify AMove order was set immediately.
	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		s.mu.RUnlock()
		t.Fatal("unit removed after AttackMoveUnits")
	}
	if u.Order.Type != OrderAttackMove {
		t.Fatalf("Order.Type after AttackMoveUnits = %v, want OrderAttackMove", u.Order.Type)
	}
	s.mu.RUnlock()

	// Tick until the enemy dies (max 300 ticks).
	const maxTicks = 300
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
		t.Fatal("mid-path enemy did not die within 300 ticks; unit may not have engaged")
	}

	// Give the AI a few ticks to call resumeStandingOrderLocked.
	tickN(s, 5)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	if u == nil {
		t.Fatal("AMove unit was removed unexpectedly")
	}
	if u.Order.Type != OrderAttackMove {
		t.Errorf("Order.Type after kill = %v, want OrderAttackMove (order must survive kill)", u.Order.Type)
	}
	if u.AttackTargetID != 0 {
		t.Errorf("AttackTargetID = %d after enemy died, want 0", u.AttackTargetID)
	}
	if !u.Moving {
		t.Errorf("unit should be Moving toward AMove destination after kill; Moving=false")
	}
	s.mu.RUnlock()
}

// TestOrderAttackMove_EngagesEnemyFarFromDestination is a regression test for
// the "AMove walks past enemies" bug. Repro: a unit given AMove to a distant
// point will not engage an enemy adjacent to it along the way, because the
// combat anchor was pinned at the destination and the leash check rejects any
// hostile whose position is more than LeashDistance from the destination.
//
// The fix slides the combat anchor to the unit's current position each tick
// while no target is held (mirroring the pattern already used for enemy
// advance). This keeps the leash centred on where the unit actually IS, so
// enemies in detection range can be acquired regardless of where the AMove
// destination sits.
//
// Setup:
//   - soldier (DetectionRange=240, LeashDistance=230) at (300, 400).
//   - AMove destination: (1200, 400) — 900px away, well beyond leash.
//   - Enemy at (500, 400) — 200px from unit (inside detection), 700px from
//     destination (well outside LeashDistance=230 anchored at dest).
//
// Without the fix: the unit walks past the enemy; AttackTargetID stays 0.
// With the fix: the unit acquires and engages the enemy within a few ticks.
func TestOrderAttackMove_EngagesEnemyFarFromDestination(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	unit.X, unit.Y = 300, 400
	unitID := unit.ID
	s.mu.Unlock()

	amoveDest := protocol.Vec2{X: 1200, Y: 400}
	s.AttackMoveUnits("p1", []int{unitID}, amoveDest)

	s.mu.Lock()
	enemy := spawnOrderEnemy(t, s, 500, 400)
	enemy.MoveSpeed = 0
	enemy.Capabilities = nil
	enemyID := enemy.ID
	s.mu.Unlock()

	const maxTicks = 60
	acquired := false
	for i := 0; i < maxTicks; i++ {
		s.Update(0.05)
		s.mu.RLock()
		u := s.unitsByID[unitID]
		if u != nil && u.AttackTargetID == enemyID {
			acquired = true
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}

	if !acquired {
		s.mu.RLock()
		u := s.unitsByID[unitID]
		t.Fatalf("AMove unit did not engage enemy at (500,400) within %d ticks; "+
			"AttackTargetID=%d, unit at (%.1f,%.1f), anchor at (%.1f,%.1f)",
			maxTicks, u.AttackTargetID, u.X, u.Y, u.CombatAnchorX, u.CombatAnchorY)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC8 – Determinism: same seed + same intent stream → identical state every tick
// ─────────────────────────────────────────────────────────────────────────────

// TestDeterminism_OrdersIntentStream creates two independent GameState instances
// with the same seed, replays the same sequence of player commands on both, and
// asserts that every unit's position, HP, and Order.Type match tick-for-tick for
// 100 ticks. Any divergence is a determinism bug.
func TestDeterminism_OrdersIntentStream(t *testing.T) {
	const seed = int64(77)
	const ticks = 100
	const dt = 0.05

	// setup returns a fresh state with one friendly soldier and one enemy soldier.
	// Returns (state, friendlyID, enemyID).
	setup := func() (*GameState, int, int) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		defer s.mu.Unlock()

		friendly := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		friendly.MaxHP = 500
		friendly.HP = 500
		friendly.Visible = true
		friendly.AttackRange = 80
		friendly.Damage = 10
		friendly.AttackSpeed = 1.0
		friendly.AttackCooldown = 0
		friendly.MoveSpeed = 150
		s.initializeCombatUnitLocked(friendly)

		enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 700, Y: 400})
		enemy.MaxHP = 500
		enemy.HP = 500
		enemy.Visible = true
		enemy.AttackRange = 80
		enemy.Damage = 5
		enemy.AttackSpeed = 1.0
		enemy.MoveSpeed = 0
		enemy.Capabilities = nil // keep enemy stationary; no AI
		s.initializeCombatUnitLocked(enemy)

		return s, friendly.ID, enemy.ID
	}

	s1, f1, e1 := setup()
	s2, f2, e2 := setup()

	// Replay the same intent stream on both states.
	// Tick 0: issue AttackMove toward destination past the enemy.
	amoveDest := protocol.Vec2{X: 900, Y: 400}
	s1.AttackMoveUnits("p1", []int{f1}, amoveDest)
	s2.AttackMoveUnits("p1", []int{f2}, amoveDest)

	// Tick 10: re-issue a patrol between two waypoints.
	// (Simulates mid-match re-order to test order-field determinism.)
	applyPatrolAt := 10
	patrolDest := protocol.Vec2{X: 600, Y: 400}

	for tick := 0; tick < ticks; tick++ {
		if tick == applyPatrolAt {
			s1.PatrolUnits("p1", []int{f1}, patrolDest)
			s2.PatrolUnits("p1", []int{f2}, patrolDest)
		}

		s1.Update(dt)
		s2.Update(dt)

		s1.mu.RLock()
		s2.mu.RLock()

		u1 := s1.unitsByID[f1]
		u2 := s2.unitsByID[f2]
		e1unit := s1.unitsByID[e1]
		e2unit := s2.unitsByID[e2]

		if (u1 == nil) != (u2 == nil) {
			t.Fatalf("tick %d: friendly unit existence diverged: s1=%v s2=%v", tick, u1 != nil, u2 != nil)
		}
		if u1 != nil {
			if math.Abs(u1.X-u2.X) > 1e-9 || math.Abs(u1.Y-u2.Y) > 1e-9 {
				t.Errorf("tick %d: friendly position diverged: s1=(%.6f,%.6f) s2=(%.6f,%.6f)", tick, u1.X, u1.Y, u2.X, u2.Y)
			}
			if u1.HP != u2.HP {
				t.Errorf("tick %d: friendly HP diverged: s1=%d s2=%d", tick, u1.HP, u2.HP)
			}
			if u1.Order.Type != u2.Order.Type {
				t.Errorf("tick %d: friendly Order.Type diverged: s1=%v s2=%v", tick, u1.Order.Type, u2.Order.Type)
			}
		}
		if (e1unit == nil) != (e2unit == nil) {
			t.Fatalf("tick %d: enemy unit existence diverged: s1=%v s2=%v", tick, e1unit != nil, e2unit != nil)
		}
		if e1unit != nil && e1unit.HP != e2unit.HP {
			t.Errorf("tick %d: enemy HP diverged: s1=%d s2=%d", tick, e1unit.HP, e2unit.HP)
		}

		s1.mu.RUnlock()
		s2.mu.RUnlock()

		if t.Failed() {
			// Stop at first divergence to keep output readable.
			return
		}
	}
	// Suppress unused variable warning from setup return; e1/e2 used above.
	_ = e1
	_ = e2
}

// ─────────────────────────────────────────────────────────────────────────────
// AC11 – Hold under retreat conditions: Path stays nil
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderHold_SuppressesRetreat places a Hold unit in a scenario that would
// normally trigger shouldRetreatLocked (multiple melee threats within
// RetreatTriggerMeleeRange). The unit must not move (Path stays nil) regardless
// of how many threats surround it.
func TestOrderHold_SuppressesRetreat(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	startX, startY := unit.X, unit.Y
	unitID := unit.ID

	// Give the unit a combat profile that has explicit retreat parameters.
	// We achieve this by giving it low HP so retreat would naturally trigger.
	// The retreat trigger is melee threats within RetreatTriggerMeleeRange.
	unit.Order = OrderState{Type: OrderHold, HoldX: unit.X, HoldY: unit.Y}
	unit.CombatAnchorX = unit.X
	unit.CombatAnchorY = unit.Y
	unit.HP = 1 // critically low — would cause retreat if not on Hold

	// Spawn three stationary melee threats very close, all within typical
	// RetreatTriggerMeleeRange (~72px per combatMeleeProximityRadius).
	for i := 0; i < 3; i++ {
		angle := float64(i) * 2.0 * math.Pi / 3.0
		ex := unit.X + 60.0*math.Cos(angle)
		ey := unit.Y + 60.0*math.Sin(angle)
		threat := spawnOrderEnemy(t, s, ex, ey)
		threat.MoveSpeed = 0 // stationary
	}
	s.mu.Unlock()

	// Tick 200 times. If retreat suppression is missing the unit will move.
	for i := 0; i < 200; i++ {
		s.Update(0.05)
		s.mu.RLock()
		u := s.unitsByID[unitID]
		if u == nil {
			s.mu.RUnlock()
			// Unit may have died from melee hits — that is acceptable; the
			// important assertion is that it never moved before dying.
			break
		}
		if u.Path != nil && len(u.Path) > 0 {
			s.mu.RUnlock()
			t.Errorf("tick %d: Hold unit has a non-nil Path under retreat conditions; retreat suppression failed", i+1)
			return
		}
		dist := math.Sqrt(math.Pow(u.X-startX, 2) + math.Pow(u.Y-startY, 2))
		if dist > 3.0 { // 3px tolerance for separation pushback
			s.mu.RUnlock()
			t.Errorf("tick %d: Hold unit moved %.1fpx from start position; should stay put", i+1, dist)
			return
		}
		s.mu.RUnlock()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC13 – AttackTarget drops when target becomes invisible
// ─────────────────────────────────────────────────────────────────────────────

// TestOrderAttackTarget_DropsOnInvisibleTarget issues an AttackWithUnits order,
// then sets the target's Visible flag to false mid-pursuit. The simulation must
// call shouldDropCurrentTargetLocked, which returns true, and
// clearCombatTargetLocked must demote Order.Type to OrderIdle.
func TestOrderAttackTarget_DropsOnInvisibleTarget(t *testing.T) {
	s, unit := newOrderTestState(t)

	s.mu.Lock()
	unitID := unit.ID

	// Spawn enemy just outside attack range so the unit is moving toward it.
	enemy := spawnOrderEnemy(t, s, unit.X+200, unit.Y)
	enemyID := enemy.ID
	s.mu.Unlock()

	s.AttackWithUnits("p1", []int{unitID}, enemyID)

	// Tick a few times to let the AI confirm the attack order.
	tickN(s, 5)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	if u == nil {
		s.mu.RUnlock()
		t.Fatal("unit removed unexpectedly")
	}
	if u.Order.Type != OrderAttackTarget {
		s.mu.RUnlock()
		t.Fatalf("expected OrderAttackTarget before invisibility test, got %v", u.Order.Type)
	}
	s.mu.RUnlock()

	// Make the target invisible (simulating fog-of-war or stealth).
	s.mu.Lock()
	e := s.unitsByID[enemyID]
	if e != nil {
		e.Visible = false
	}
	s.mu.Unlock()

	// Tick once for shouldDropCurrentTargetLocked to fire.
	tickN(s, 2)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	if u == nil {
		s.mu.RUnlock()
		t.Fatal("unit removed after target became invisible")
	}
	if u.AttackTargetID != 0 {
		t.Errorf("AttackTargetID = %d after target went invisible, want 0", u.AttackTargetID)
	}
	if u.Order.Type != OrderIdle {
		t.Errorf("Order.Type = %v after target went invisible, want OrderIdle", u.Order.Type)
	}
	s.mu.RUnlock()
}
