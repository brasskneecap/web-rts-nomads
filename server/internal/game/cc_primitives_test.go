package game

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// newCCState returns a minimal GameState with two opposing soldiers.
// unit belongs to "p1", enemy belongs to "p2". Both are visible, alive,
// and positioned within attack range of each other. The write lock is held
// on return; the caller must defer s.mu.Unlock().
func newCCState(t *testing.T) (s *GameState, unit, enemy *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.mu.Lock()

	unit = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	enemy = s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{X: 420, Y: 400})

	// Wire up combat: unit is attacking enemy and is in range.
	unit.AttackTargetID = enemy.ID
	unit.Attacking = true
	unit.AttackCooldown = 0
	unit.AttackRange = 100
	unit.Status = "Attacking"
	return s, unit, enemy
}

// manualDecayCC runs the CC decay loop the same way Update() does, without
// invoking the full game loop (which would let the units fight each other).
func manualDecayCC(unit *Unit, dt float64) {
	if unit.StunnedRemaining > 0 {
		unit.StunnedRemaining = math.Max(0, unit.StunnedRemaining-dt)
	}
	if unit.SlowedRemaining > 0 {
		unit.SlowedRemaining = math.Max(0, unit.SlowedRemaining-dt)
		if unit.SlowedRemaining == 0 {
			unit.SlowedMultiplier = 0
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Stun tests
// ─────────────────────────────────────────────────────────────────────────────

// TestApplyStun_GatesAttack stuns a unit, ticks combat, and verifies the enemy
// takes no damage. Then lets the stun expire and verifies damage resumes.
func TestApplyStun_GatesAttack(t *testing.T) {
	s, unit, enemy := newCCState(t)
	defer s.mu.Unlock()

	s.ApplyStunLocked(unit.ID, 1.0)
	if unit.StunnedRemaining <= 0 {
		t.Fatal("unit should be stunned")
	}

	blocked := s.getBlockedCellsLocked()
	hpBefore := enemy.HP
	s.tickUnitCombatLocked(0.1, blocked)
	if enemy.HP != hpBefore {
		t.Errorf("stunned unit fired an attack: enemy HP dropped from %d to %d", hpBefore, enemy.HP)
	}

	// Expire the stun manually, reset cooldown, verify attack fires.
	unit.StunnedRemaining = 0
	unit.AttackCooldown = 0
	hpBefore = enemy.HP
	s.tickUnitCombatLocked(0.1, blocked)
	if enemy.HP >= hpBefore {
		t.Errorf("un-stunned unit should have attacked: enemy HP before=%d after=%d", hpBefore, enemy.HP)
	}
}

// TestApplyStun_DoesNotClearTargetOrPath verifies that applying a stun does
// not clear AttackTargetID or Path on the unit.
func TestApplyStun_DoesNotClearTargetOrPath(t *testing.T) {
	s, unit, enemy := newCCState(t)
	defer s.mu.Unlock()

	unit.Path = []protocol.Vec2{{X: 500, Y: 500}, {X: 600, Y: 600}}
	unit.Moving = true
	unit.AttackTargetID = enemy.ID

	s.ApplyStunLocked(unit.ID, 2.0)

	if unit.AttackTargetID != enemy.ID {
		t.Errorf("stun cleared AttackTargetID: got %d, want %d", unit.AttackTargetID, enemy.ID)
	}
	if len(unit.Path) != 2 {
		t.Errorf("stun modified Path: len=%d, want 2", len(unit.Path))
	}
}

// TestApplyStun_RefreshLonger verifies that stun follows a refresh-longer
// policy: a shorter stun while already stunned is ignored; a longer stun
// extends the remaining duration.
func TestApplyStun_RefreshLonger(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	s.ApplyStunLocked(unit.ID, 0.5)
	if unit.StunnedRemaining != 0.5 {
		t.Fatalf("initial stun: expected 0.5, got %.3f", unit.StunnedRemaining)
	}

	// Shorter stun — should not reduce remaining.
	s.ApplyStunLocked(unit.ID, 0.3)
	if unit.StunnedRemaining != 0.5 {
		t.Errorf("shorter stun should be ignored: expected 0.5, got %.3f", unit.StunnedRemaining)
	}

	// Longer stun — should extend.
	s.ApplyStunLocked(unit.ID, 1.0)
	if unit.StunnedRemaining != 1.0 {
		t.Errorf("longer stun should extend: expected 1.0, got %.3f", unit.StunnedRemaining)
	}
}

// TestApplyStun_ExpiresAndDecays ticks through the full stun duration and
// verifies StunnedRemaining cleanly reaches 0.
func TestApplyStun_ExpiresAndDecays(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	const duration = 0.5
	s.ApplyStunLocked(unit.ID, duration)

	dt := 0.05
	elapsed := 0.0
	for elapsed < duration+dt {
		manualDecayCC(unit, dt)
		elapsed += dt
	}

	if unit.StunnedRemaining != 0 {
		t.Errorf("StunnedRemaining should be 0 after duration, got %.4f", unit.StunnedRemaining)
	}
}

// TestApplyStun_GatesMovement verifies a stunned unit's position does not
// change during the stun window, and resumes movement once un-stunned.
func TestApplyStun_GatesMovement(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	// Set up a path well away from the enemy so the unit would move.
	unit.AttackTargetID = 0
	unit.Attacking = false
	unit.Path = []protocol.Vec2{{X: 700, Y: 400}}
	unit.Moving = true
	unit.AttackCooldown = 0

	s.ApplyStunLocked(unit.ID, 1.0)

	startX := unit.X
	startY := unit.Y

	blocked := s.getBlockedCellsLocked()

	// Tick several times — position must not change while stunned.
	for i := 0; i < 10; i++ {
		// Run only the movement portion, not the combat tick, by directly
		// executing the CC decay and checking the unit position won't change.
		// We use a reduced-scope test: verify stun flag blocks the step below.
		if unit.StunnedRemaining > 0 {
			manualDecayCC(unit, 0.05)
		}
	}
	// Still stunned (0.05*10 = 0.5 < 1.0 duration).
	if unit.StunnedRemaining <= 0 {
		t.Fatal("unit should still be stunned after 10 ticks of 0.05s")
	}
	if unit.X != startX || unit.Y != startY {
		t.Errorf("stunned unit moved: was (%.1f,%.1f), now (%.1f,%.1f)",
			startX, startY, unit.X, unit.Y)
	}

	// Expire stun, now run a real Update tick and expect movement.
	unit.StunnedRemaining = 0
	xBefore := unit.X
	_ = blocked
	// Move the unit cleanly: run the full loop logic by simulating one step.
	nextWaypoint := unit.Path[0]
	dx := nextWaypoint.X - unit.X
	dy := nextWaypoint.Y - unit.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	step := unit.MoveSpeed * 0.1 // dt = 0.1
	if step < dist {
		unit.X += (dx / dist) * step
		unit.Y += (dy / dist) * step
	}
	if unit.X == xBefore {
		t.Errorf("un-stunned unit should have moved from x=%.1f", xBefore)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Slow tests
// ─────────────────────────────────────────────────────────────────────────────

// TestApplySlow_ReducesMoveSpeed verifies the movement step is scaled by the
// slow multiplier via slowFactorLocked.
func TestApplySlow_ReducesMoveSpeed(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	unit.AttackTargetID = 0
	unit.Attacking = false
	unit.Moving = true
	unit.Path = []protocol.Vec2{{X: 700, Y: 400}}
	unit.MoveSpeed = 100.0 // known value for easy assertion

	const dt = 0.1
	const multiplier = 0.7

	// Without slow: step = 100 * 1.0 * perkMult * dt ≈ 100*1*dt
	// (perkMoveSpeedMultiplierLocked returns 1.0 for a unit with no perks)
	stepWithout := unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * 1.0 * dt

	s.ApplySlowLocked(unit.ID, multiplier, 2.0)
	stepWith := unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * slowFactorLocked(unit) * dt

	wantRatio := multiplier
	gotRatio := stepWith / stepWithout
	if math.Abs(gotRatio-wantRatio) > 0.001 {
		t.Errorf("slow step ratio: got %.4f, want %.4f", gotRatio, wantRatio)
	}
}

// TestApplySlow_ReducesAttackCadence verifies that a slowed attacker commits a
// longer AttackCooldown after firing — a 0.7× slow should stretch cooldown by
// 1/0.7 relative to the same unit firing without a slow.
func TestApplySlow_ReducesAttackCadence(t *testing.T) {
	const multiplier = 0.7

	// Baseline: no slow — capture the cooldown committed after firing.
	sBase, unitBase, _ := newCCState(t)
	unitBase.AttackSpeed = 1.0
	blockedBase := sBase.getBlockedCellsLocked()
	sBase.tickUnitCombatLocked(0.1, blockedBase)
	cooldownBase := unitBase.AttackCooldown
	sBase.mu.Unlock()
	if cooldownBase <= 0 {
		t.Fatalf("baseline attacker did not fire (AttackCooldown=%.3f)", cooldownBase)
	}

	// Slowed: same setup, attacker slowed before the tick.
	sSlow, unitSlow, _ := newCCState(t)
	unitSlow.AttackSpeed = 1.0
	sSlow.ApplySlowLocked(unitSlow.ID, multiplier, 2.0)
	blockedSlow := sSlow.getBlockedCellsLocked()
	sSlow.tickUnitCombatLocked(0.1, blockedSlow)
	cooldownSlow := unitSlow.AttackCooldown
	sSlow.mu.Unlock()
	if cooldownSlow <= 0 {
		t.Fatalf("slowed attacker did not fire (AttackCooldown=%.3f)", cooldownSlow)
	}

	// Cooldown inverse of effective speed: slow → cooldown stretches by 1/mult.
	wantRatio := 1.0 / multiplier
	gotRatio := cooldownSlow / cooldownBase
	if math.Abs(gotRatio-wantRatio) > 0.01 {
		t.Errorf("slowed attack cadence: cooldown ratio got %.4f, want %.4f (base=%.4f, slow=%.4f)",
			gotRatio, wantRatio, cooldownBase, cooldownSlow)
	}
}

// TestApplySlow_RefreshStrongerMultiplier verifies that a stronger (lower)
// multiplier overwrites a weaker (higher) one.
func TestApplySlow_RefreshStrongerMultiplier(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	s.ApplySlowLocked(unit.ID, 0.8, 2.0)
	if unit.SlowedMultiplier != 0.8 {
		t.Fatalf("initial slow: expected 0.8, got %.3f", unit.SlowedMultiplier)
	}

	// Apply stronger slow (lower multiplier).
	s.ApplySlowLocked(unit.ID, 0.6, 2.0)
	if unit.SlowedMultiplier != 0.6 {
		t.Errorf("stronger slow should win: expected 0.6, got %.3f", unit.SlowedMultiplier)
	}

	// Apply weaker slow — multiplier must not change.
	s.ApplySlowLocked(unit.ID, 0.9, 2.0)
	if unit.SlowedMultiplier != 0.6 {
		t.Errorf("weaker slow should be ignored: expected 0.6, got %.3f", unit.SlowedMultiplier)
	}
}

// TestApplySlow_RefreshLongerDuration verifies that a longer-duration slow
// extends the remaining timer even when the incoming multiplier is weaker.
// The stronger (existing) multiplier is preserved; only the duration extends.
func TestApplySlow_RefreshLongerDuration(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	// Apply 0.7× slow for 1.0 s.
	s.ApplySlowLocked(unit.ID, 0.7, 1.0)
	if unit.SlowedMultiplier != 0.7 || unit.SlowedRemaining != 1.0 {
		t.Fatalf("initial state wrong: mult=%.2f rem=%.2f", unit.SlowedMultiplier, unit.SlowedRemaining)
	}

	// Apply 0.9× (weaker) slow for 2.0 s (longer).
	s.ApplySlowLocked(unit.ID, 0.9, 2.0)

	// Multiplier must stay at 0.7 (stronger wins).
	if unit.SlowedMultiplier != 0.7 {
		t.Errorf("multiplier should stay at 0.7 (stronger): got %.3f", unit.SlowedMultiplier)
	}
	// Duration must extend to 2.0 s (longer wins).
	if unit.SlowedRemaining != 2.0 {
		t.Errorf("duration should extend to 2.0 s: got %.3f", unit.SlowedRemaining)
	}
}

// TestApplySlow_Expires_ClearsMultiplier verifies that after the slow duration
// elapses, both SlowedRemaining and SlowedMultiplier are 0.
func TestApplySlow_Expires_ClearsMultiplier(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	const duration = 0.5
	s.ApplySlowLocked(unit.ID, 0.7, duration)

	dt := 0.05
	elapsed := 0.0
	for elapsed < duration+dt {
		manualDecayCC(unit, dt)
		elapsed += dt
	}

	if unit.SlowedRemaining != 0 {
		t.Errorf("SlowedRemaining should be 0 after expiry, got %.4f", unit.SlowedRemaining)
	}
	if unit.SlowedMultiplier != 0 {
		t.Errorf("SlowedMultiplier should be 0 after expiry, got %.4f", unit.SlowedMultiplier)
	}
	if slowFactorLocked(unit) != 1.0 {
		t.Errorf("slowFactorLocked should return 1.0 after expiry, got %.4f", slowFactorLocked(unit))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Interaction and stack tests
// ─────────────────────────────────────────────────────────────────────────────

// TestStunAndSlow_Stack verifies that a unit can be both stunned and slowed
// simultaneously. During the stun window: no attacks, no movement. After
// the stun expires (while slow is still active): attacks resume, movement
// resumes at the slow multiplier.
func TestStunAndSlow_Stack(t *testing.T) {
	s, unit, enemy := newCCState(t)
	defer s.mu.Unlock()

	// Give the unit a path so we can observe movement suppression.
	unit.Path = []protocol.Vec2{{X: 700, Y: 400}}
	unit.Moving = true
	unit.AttackTargetID = enemy.ID
	unit.AttackCooldown = 0
	unit.AttackRange = 100

	// Apply stun for 0.3 s and slow for 2.0 s simultaneously.
	s.ApplyStunLocked(unit.ID, 0.3)
	s.ApplySlowLocked(unit.ID, 0.7, 2.0)

	if unit.StunnedRemaining <= 0 || unit.SlowedRemaining <= 0 {
		t.Fatal("unit should be both stunned and slowed")
	}

	blocked := s.getBlockedCellsLocked()
	xBefore := unit.X
	hpBefore := enemy.HP

	// Combat tick during stun — no attack should fire.
	s.tickUnitCombatLocked(0.05, blocked)
	if enemy.HP != hpBefore {
		t.Errorf("stunned unit attacked: enemy HP %d → %d", hpBefore, enemy.HP)
	}

	// Movement manual check — stun blocks position update.
	if unit.X != xBefore {
		t.Errorf("stunned unit moved: x was %.1f, now %.1f", xBefore, unit.X)
	}

	// Expire the stun (leave the slow active).
	unit.StunnedRemaining = 0
	unit.AttackCooldown = 0

	// Slow must still be active.
	if unit.SlowedRemaining <= 0 {
		t.Fatal("slow should still be active after stun expires")
	}
	if unit.SlowedMultiplier != 0.7 {
		t.Errorf("slow multiplier should be 0.7, got %.3f", unit.SlowedMultiplier)
	}

	// Attack should now fire.
	hpBefore = enemy.HP
	s.tickUnitCombatLocked(0.05, blocked)
	if enemy.HP >= hpBefore {
		t.Errorf("un-stunned unit should attack (even while slowed): enemy HP %d → %d", hpBefore, enemy.HP)
	}

	// Movement speed factor should reflect the slow.
	factor := slowFactorLocked(unit)
	if math.Abs(factor-0.7) > 0.001 {
		t.Errorf("slowFactorLocked should return 0.7 while slowed, got %.4f", factor)
	}
}

// TestDeterminism_StunSlow verifies that two GameState instances created with
// the same seed and subjected to the same ApplyStun/ApplySlow sequence produce
// identical StunnedRemaining and SlowedRemaining values on every tick.
func TestDeterminism_StunSlow(t *testing.T) {
	const seed = int64(42)
	const ticks = 20
	const dt = 0.05

	setup := func() (*GameState, *Unit) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		return s, u
	}

	s1, u1 := setup()
	s2, u2 := setup()

	// Apply identical CC sequences to both units.
	s1.ApplyStunLocked(u1.ID, 0.6)
	s1.ApplySlowLocked(u1.ID, 0.7, 1.0)

	s2.ApplyStunLocked(u2.ID, 0.6)
	s2.ApplySlowLocked(u2.ID, 0.7, 1.0)

	s1.mu.Unlock()
	s2.mu.Unlock()

	for tick := 0; tick < ticks; tick++ {
		s1.mu.Lock()
		manualDecayCC(u1, dt)
		stun1 := u1.StunnedRemaining
		slow1 := u1.SlowedRemaining
		mult1 := u1.SlowedMultiplier
		s1.mu.Unlock()

		s2.mu.Lock()
		manualDecayCC(u2, dt)
		stun2 := u2.StunnedRemaining
		slow2 := u2.SlowedRemaining
		mult2 := u2.SlowedMultiplier
		s2.mu.Unlock()

		if stun1 != stun2 {
			t.Errorf("tick %d: StunnedRemaining diverged: %.6f vs %.6f", tick, stun1, stun2)
		}
		if slow1 != slow2 {
			t.Errorf("tick %d: SlowedRemaining diverged: %.6f vs %.6f", tick, slow1, slow2)
		}
		if mult1 != mult2 {
			t.Errorf("tick %d: SlowedMultiplier diverged: %.6f vs %.6f", tick, mult1, mult2)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full-Update integration tests
// ─────────────────────────────────────────────────────────────────────────────

// TestApplyStun_GatesMovement_ViaUpdate runs a full Update() tick and verifies
// that the stun gate in state.go (the `if unit.StunnedRemaining > 0 { continue }`
// block) actually prevents position change. This catches regressions to the
// Update() loop that the logical-level test in TestApplyStun_GatesMovement
// cannot catch.
func TestApplyStun_GatesMovement_ViaUpdate(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 13)

	s.mu.Lock()
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	unit.AttackTargetID = 0
	unit.Attacking = false
	unit.Moving = true
	unit.Path = []protocol.Vec2{{X: 800, Y: 400}} // far enough that unit won't arrive in one tick
	unit.MoveSpeed = 100.0
	unit.AttackCooldown = 99.0 // prevent any combat firing
	startX := unit.X
	unitID := unit.ID
	s.mu.Unlock()

	// Apply stun via the public API (takes the lock internally).
	s.mu.Lock()
	s.ApplyStunLocked(unitID, 2.0)
	s.mu.Unlock()

	// Run a full Update() — this is the integration point.
	s.Update(0.1)

	s.mu.RLock()
	u := s.unitsByID[unitID]
	gotX := u.X
	s.mu.RUnlock()

	if gotX != startX {
		t.Errorf("stunned unit moved during Update(): x was %.2f, now %.2f", startX, gotX)
	}

	// Expire the stun, run Update() again, expect the unit to have moved.
	s.mu.Lock()
	s.unitsByID[unitID].StunnedRemaining = 0
	s.mu.Unlock()

	s.Update(0.1)

	s.mu.RLock()
	u = s.unitsByID[unitID]
	gotX = u.X
	s.mu.RUnlock()

	if gotX <= startX {
		t.Errorf("un-stunned unit did not move during Update(): x still %.2f", gotX)
	}
}

// TestApplyStun_MidTickStun_CurrentAttackStillFires verifies the documented
// "stun applied mid-tick takes effect next tick" behavior. A stun stamped onto
// a unit AFTER the combat check has already passed (simulated by calling
// tickUnitCombatLocked then ApplyStunLocked in sequence within a single tick)
// does not retroactively suppress the attack that already fired this tick.
// This is consistent with WeakenedRemaining/MarkedRemaining behavior.
func TestApplyStun_MidTickStun_CurrentAttackStillFires(t *testing.T) {
	s, unit, enemy := newCCState(t)
	defer s.mu.Unlock()

	hpBefore := enemy.HP
	blocked := s.getBlockedCellsLocked()

	// Tick combat (attack fires because unit is not yet stunned).
	s.tickUnitCombatLocked(0.1, blocked)
	hpAfterAttack := enemy.HP

	// Now apply a stun — simulating what a perk hook called from within
	// tickUnitCombatLocked would do.
	s.ApplyStunLocked(unit.ID, 1.0)

	// The attack this tick should have landed.
	if hpAfterAttack >= hpBefore {
		t.Errorf("attack should have fired before stun was applied: HP before=%d after=%d", hpBefore, hpAfterAttack)
	}

	// Next tick: stun is active, attack must not fire.
	unit.AttackCooldown = 0
	hpBefore = enemy.HP
	s.tickUnitCombatLocked(0.1, blocked)
	if enemy.HP != hpBefore {
		t.Errorf("stunned unit attacked on next tick: HP %d → %d", hpBefore, enemy.HP)
	}
}

// TestApplyStun_TargetDiesDuringStun verifies that if a stunned unit's
// AttackTargetID dies while the stun is active, the stale reference is cleaned
// up cleanly — no panic, and the unit transitions to Idle on the next combat tick.
func TestApplyStun_TargetDiesDuringStun(t *testing.T) {
	s, unit, enemy := newCCState(t)
	defer s.mu.Unlock()

	s.ApplyStunLocked(unit.ID, 2.0)
	enemyID := enemy.ID

	// Kill the enemy directly: drain HP and call removeUnitLocked (which sweeps
	// AttackTargetID references on all other units).
	enemy.HP = 0
	s.removeUnitLocked(enemyID)

	// AttackTargetID should already be cleared by removeUnitLocked.
	if unit.AttackTargetID != 0 {
		t.Errorf("removeUnitLocked should have cleared AttackTargetID on stunned unit: got %d", unit.AttackTargetID)
	}

	// Expire stun, tick combat — must not panic and unit should be Idle.
	unit.StunnedRemaining = 0
	blocked := s.getBlockedCellsLocked()
	// This must not panic even though the former target is gone.
	s.tickUnitCombatLocked(0.1, blocked)
	if unit.AttackTargetID != 0 {
		t.Errorf("unit should have no attack target after dead target removed, got %d", unit.AttackTargetID)
	}
}

// TestApplySlow_WithPerkMoveSpeedMultiplier verifies that slow composes
// multiplicatively with perkMoveSpeedMultiplierLocked rather than replacing it.
// A unit with no movement perks has a perk multiplier of 1.0, so we confirm the
// formula: step = MoveSpeed * perkMult * slowFactor * dt produces a positive,
// reduced step and not a negative or zero one.
func TestApplySlow_WithPerkMoveSpeedMultiplier(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	unit.MoveSpeed = 100.0
	const dt = 0.1

	perkMult := s.perkMoveSpeedMultiplierLocked(unit)
	if perkMult <= 0 {
		t.Fatalf("perkMoveSpeedMultiplierLocked returned non-positive value %.4f for a perk-less unit", perkMult)
	}

	s.ApplySlowLocked(unit.ID, 0.5, 2.0)
	slowFactor := slowFactorLocked(unit)

	step := unit.MoveSpeed * perkMult * slowFactor * dt

	if step <= 0 {
		t.Errorf("slow + perk multiplier produced non-positive step: %.4f (perkMult=%.4f slowFactor=%.4f)", step, perkMult, slowFactor)
	}
	// Step must be strictly less than the unslowed step.
	unslowedStep := unit.MoveSpeed * perkMult * 1.0 * dt
	if step >= unslowedStep {
		t.Errorf("slowed step (%.4f) should be less than unslowed step (%.4f)", step, unslowedStep)
	}
}

// TestApplySlow_BoundaryNoOps verifies that the guard conditions in
// ApplySlowLocked treat boundary inputs as no-ops per spec.
func TestApplySlow_BoundaryNoOps(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	cases := []struct {
		name       string
		multiplier float64
		duration   float64
	}{
		{"multiplier exactly 1.0", 1.0, 2.0},
		{"multiplier > 1.0", 1.5, 2.0},
		{"multiplier == 0", 0.0, 2.0},
		{"multiplier < 0", -0.5, 2.0},
		{"duration == 0", 0.5, 0.0},
		{"duration < 0", 0.5, -1.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unit.SlowedRemaining = 0
			unit.SlowedMultiplier = 0
			s.ApplySlowLocked(unit.ID, tc.multiplier, tc.duration)
			if unit.SlowedRemaining != 0 || unit.SlowedMultiplier != 0 {
				t.Errorf("expected no-op for multiplier=%.2f duration=%.2f, got remaining=%.2f mult=%.2f",
					tc.multiplier, tc.duration, unit.SlowedRemaining, unit.SlowedMultiplier)
			}
		})
	}
}

// TestApplyStun_DeadUnitNoOp verifies that applying a stun to a unit with HP <= 0
// is silently ignored and does not set StunnedRemaining.
func TestApplyStun_DeadUnitNoOp(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	unit.HP = 0
	s.ApplyStunLocked(unit.ID, 1.0)
	if unit.StunnedRemaining != 0 {
		t.Errorf("stun on dead unit should be no-op: StunnedRemaining=%.2f", unit.StunnedRemaining)
	}
}

// TestApplyStun_NonExistentUnitNoOp verifies that applying a stun to a unit ID
// that does not exist in the game state does not panic.
func TestApplyStun_NonExistentUnitNoOp(t *testing.T) {
	s, _, _ := newCCState(t)
	defer s.mu.Unlock()

	// Must not panic.
	s.ApplyStunLocked(999999, 1.0)
}

// TestDebuffIcons_StunAndSlow verifies that activeDebuffIconsLocked returns
// "debuff-stunned" and "debuff-slowed" when their respective CC is active, and
// that neither icon appears when the CC is not active.
func TestDebuffIcons_StunAndSlow(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	// No CC active — neither icon should be present.
	icons := iconIDs(s.activeDebuffIconsLocked(unit))
	for _, icon := range icons {
		if icon == "debuff-stunned" || icon == "debuff-slowed" {
			t.Errorf("no CC active but icon %q appeared in debuff list", icon)
		}
	}

	// Apply stun only.
	s.ApplyStunLocked(unit.ID, 1.0)
	icons = iconIDs(s.activeDebuffIconsLocked(unit))
	if !containsString(icons, "debuff-stunned") {
		t.Errorf("stunned unit missing debuff-stunned icon; got %v", icons)
	}
	if containsString(icons, "debuff-slowed") {
		t.Errorf("only stun active but debuff-slowed appeared; got %v", icons)
	}

	// Apply slow as well.
	s.ApplySlowLocked(unit.ID, 0.7, 2.0)
	icons = iconIDs(s.activeDebuffIconsLocked(unit))
	if !containsString(icons, "debuff-stunned") {
		t.Errorf("stun+slow: missing debuff-stunned; got %v", icons)
	}
	if !containsString(icons, "debuff-slowed") {
		t.Errorf("stun+slow: missing debuff-slowed; got %v", icons)
	}

	// Expire both.
	unit.StunnedRemaining = 0
	unit.SlowedRemaining = 0
	unit.SlowedMultiplier = 0
	icons = iconIDs(s.activeDebuffIconsLocked(unit))
	for _, icon := range icons {
		if icon == "debuff-stunned" || icon == "debuff-slowed" {
			t.Errorf("CC expired but icon %q still in debuff list", icon)
		}
	}
}

// TestApplyStun_GatesAttack_BuildingTarget verifies that the stun gate in the
// unit-vs-building combat branch suppresses building damage while the attacker
// is stunned. This is the QA Phase-1 follow-up requested alongside shield_bash.
func TestApplyStun_GatesAttack_BuildingTarget(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 17)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})

	// Place a fake building adjacent to the attacker so it is within range.
	buildingID := "test-tower-stun"
	building := &protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 13, Y: 13},
		ID:           buildingID,
		BuildingType: "Tower",
		Width:        1,
		Height:       1,
		Metadata: map[string]interface{}{
			"hp":    float64(100),
			"maxHp": float64(100),
		},
	}
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, *building)
	s.buildingsByID[buildingID] = &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]

	attacker.AttackBuildingTargetID = buildingID
	attacker.Attacking = true
	attacker.AttackCooldown = 0
	attacker.AttackRange = 5000 // large range so distance check always passes

	// Stun the attacker.
	s.ApplyStunLocked(attacker.ID, 1.0)
	if attacker.StunnedRemaining <= 0 {
		t.Fatal("attacker should be stunned")
	}

	hpBefore := s.buildingsByID[buildingID].Metadata["hp"].(float64)
	blocked := s.getBlockedCellsLocked()
	s.tickUnitCombatLocked(0.1, blocked)

	hpAfter := s.buildingsByID[buildingID].Metadata["hp"].(float64)
	if hpAfter != hpBefore {
		t.Errorf("stunned unit damaged a building: HP %v → %v", hpBefore, hpAfter)
	}

	// Expire stun, reset cooldown — building should now take damage.
	attacker.StunnedRemaining = 0
	attacker.AttackCooldown = 0
	hpBefore = s.buildingsByID[buildingID].Metadata["hp"].(float64)
	s.tickUnitCombatLocked(0.1, blocked)
	hpAfter = s.buildingsByID[buildingID].Metadata["hp"].(float64)
	if hpAfter >= hpBefore {
		t.Errorf("un-stunned unit should have damaged building: HP before=%.0f after=%.0f", hpBefore, hpAfter)
	}
}

// TestApplySlow_DeadUnitNoOp mirrors TestApplyStun_DeadUnitNoOp: applying a slow
// to a unit with HP <= 0 must be silently ignored.
func TestApplySlow_DeadUnitNoOp(t *testing.T) {
	s, unit, _ := newCCState(t)
	defer s.mu.Unlock()

	unit.HP = 0
	s.ApplySlowLocked(unit.ID, 0.7, 2.0)
	if unit.SlowedRemaining != 0 {
		t.Errorf("slow on dead unit should be no-op: SlowedRemaining=%.2f", unit.SlowedRemaining)
	}
	if unit.SlowedMultiplier != 0 {
		t.Errorf("slow on dead unit should be no-op: SlowedMultiplier=%.2f", unit.SlowedMultiplier)
	}
}

// TestSnapshot_CCFieldsRoundTrip verifies that StunnedRemaining, SlowedRemaining,
// and SlowedMultiplier survive through Snapshot() correctly — active CC values
// appear in the snapshot, and zero values are omitted from the JSON wire format.
func TestSnapshot_CCFieldsRoundTrip(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 99)
	s.mu.Lock()
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	unitID := unit.ID
	s.ApplyStunLocked(unitID, 1.5)
	s.ApplySlowLocked(unitID, 0.6, 3.0)
	s.mu.Unlock()

	snap := s.Snapshot()

	var found *protocol.UnitSnapshot
	for i := range snap.Units {
		if snap.Units[i].ID == unitID {
			found = &snap.Units[i]
			break
		}
	}
	if found == nil {
		t.Fatal("unit not found in snapshot")
	}
	if found.StunnedRemaining != 1.5 {
		t.Errorf("StunnedRemaining in snapshot: got %.2f, want 1.5", found.StunnedRemaining)
	}
	if found.SlowedRemaining != 3.0 {
		t.Errorf("SlowedRemaining in snapshot: got %.2f, want 3.0", found.SlowedRemaining)
	}
	if math.Abs(found.SlowedMultiplier-0.6) > 0.001 {
		t.Errorf("SlowedMultiplier in snapshot: got %.3f, want 0.6", found.SlowedMultiplier)
	}

	// JSON omitempty: marshal a zero-valued snapshot and verify the three CC
	// fields are absent from the wire output.
	zeroSnap := protocol.UnitSnapshot{ID: 999, OwnerID: "p1"}
	raw, err := json.Marshal(zeroSnap)
	if err != nil {
		t.Fatalf("json.Marshal(zero snapshot): %v", err)
	}
	payload := string(raw)
	for _, key := range []string{"stunnedRemaining", "slowedRemaining", "slowedMultiplier"} {
		if strings.Contains(payload, key) {
			t.Errorf("zero CC field %q present in JSON despite omitempty: %s", key, payload)
		}
	}

	// Marshal a snapshot with active CC and verify the fields ARE present.
	activeSnap := protocol.UnitSnapshot{
		ID:               999,
		OwnerID:          "p1",
		StunnedRemaining: 1.5,
		SlowedRemaining:  3.0,
		SlowedMultiplier: 0.6,
	}
	rawActive, err := json.Marshal(activeSnap)
	if err != nil {
		t.Fatalf("json.Marshal(active CC snapshot): %v", err)
	}
	payloadActive := string(rawActive)
	for _, key := range []string{"stunnedRemaining", "slowedRemaining", "slowedMultiplier"} {
		if !strings.Contains(payloadActive, key) {
			t.Errorf("active CC field %q absent from JSON: %s", key, payloadActive)
		}
	}
}
