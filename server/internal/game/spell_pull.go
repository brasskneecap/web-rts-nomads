package game

import "math"

// spell_pull.go is the forced-displacement (pull/push) control-effect
// subsystem (arch-mage-spell-system). It is the first knockback/displacement
// primitive in the codebase and is built to be reused by any future push/pull
// effect — apply_force (ability_exec_actions.go) is the generic entry point;
// arcane_orb's vortex pull is the first (pull-only) consumer.
//
// Model: an affected unit carries a per-unit displacement state (PullRemaining,
// PullCenter*, PullStrength, PullPush — see the Unit struct). Every tick the
// unit is moved toward (pull) or away from (push) its center by a
// deterministic delta (pure math — no wall-clock, no RNG), and its normal path
// advancement is skipped for that tick (displacement wins). On expiry the
// unit's stale path is dropped so it resumes normal AI/movement from its
// displaced position rather than snapping back along a path computed before
// the displacement.
//
// Pull vs push are NOT sign-flips of one another: pull snaps to the center the
// tick it would otherwise overshoot ("arrived"); push has no such destination,
// so it never clamps — it simply keeps moving outward at strength px/sec until
// PullRemaining runs out. Applying pull's clamp to push would teleport the
// unit onto the origin, which is the opposite of "push". See
// tickUnitPullLocked.
//
// Collision policy (design D5): displaced units CLIP THROUGH obstacles during
// the pull/push — the per-tick delta is unconditioned geometry, no
// pathfinding. Only the final rest position is reconciled, by the existing
// stuck/repath recovery once the unit resumes. This trades visual purity for
// determinism and simplicity.

// applyPullLocked places (or refreshes) a forced-displacement effect on unit,
// dragging it toward (cx,cy) at strength world-px/sec for duration seconds. A
// dead unit, non-positive strength, or non-positive duration is a no-op. A new
// pull overwrites any existing one on the same unit (refresh semantics).
//
// This does NOT filter by allegiance — the caller decides who is eligible (see
// applyPullInRadiusLocked, which pulls hostiles only). Caller holds s.mu.
func (s *GameState) applyPullLocked(unit *Unit, cx, cy, strength, duration float64) {
	s.applyForceLocked(unit, cx, cy, strength, duration, false)
}

// applyPushLocked places (or refreshes) a forced-displacement effect on unit,
// pushing it AWAY from (cx,cy) at strength world-px/sec for duration seconds.
// Same no-op guards and refresh semantics as applyPullLocked; see
// tickUnitPullLocked for why push never snaps to (or through) the center.
// Caller holds s.mu.
func (s *GameState) applyPushLocked(unit *Unit, cx, cy, strength, duration float64) {
	s.applyForceLocked(unit, cx, cy, strength, duration, true)
}

// applyForceLocked is the shared implementation behind applyPullLocked
// (push=false) and applyPushLocked (push=true). Caller holds s.mu.
func (s *GameState) applyForceLocked(unit *Unit, cx, cy, strength, duration float64, push bool) {
	if unit == nil || unit.HP <= 0 || strength <= 0 || duration <= 0 {
		return
	}
	unit.PullCenterX = cx
	unit.PullCenterY = cy
	unit.PullStrength = strength
	unit.PullRemaining = duration
	unit.PullPush = push
}

// applyPullInRadiusLocked pulls every hostile (relative to caster) within radius
// of the center point toward that center. Allies and the caster itself are
// never pulled. Iterates s.Units in slice order (deterministic). Returns the
// number of units newly affected (handy for tests / feedback). Caller holds s.mu.
func (s *GameState) applyPullInRadiusLocked(caster *Unit, cx, cy, radius, strength, duration float64) int {
	if caster == nil || radius <= 0 || strength <= 0 || duration <= 0 {
		return 0
	}
	radSq := radius * radius
	affected := 0
	for _, u := range s.Units {
		if u == nil || u.ID == caster.ID {
			continue
		}
		if u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(u.OwnerID, caster.OwnerID) {
			continue // enemies only — allies and caster are never displaced
		}
		dx := u.X - cx
		dy := u.Y - cy
		if dx*dx+dy*dy > radSq {
			continue
		}
		s.applyPullLocked(u, cx, cy, strength, duration)
		affected++
	}
	return affected
}

// tickUnitPullLocked advances one unit's active pull/push by dt. No-op when
// the unit is not being displaced.
//
// Pull (PullPush false, the pre-existing/default behavior — byte-identical to
// before push existed): the unit is dragged toward its center by
// PullStrength*dt, never overshooting (it snaps to the center if the step
// would pass it).
//
// Push (PullPush true): the unit is moved AWAY from its center by
// PullStrength*dt. There is no destination to arrive at, so push never snaps
// — it just keeps moving outward every tick, even if a single step's distance
// would exceed the remaining-to-duration total displacement. (Deliberately NOT
// a sign-flip of pull: reusing pull's overshoot clamp for push would snap the
// unit onto/through the center, which is the opposite of "push away".)
//
// Both directions: if the unit is (numerically) exactly on the center,
// dist <= 1e-6 and neither direction has a defined heading, so no movement
// happens that tick (matches the pre-existing pull guard — avoids a NaN
// normalize of a zero vector). The duration still decrements either way.
//
// When the timer runs out the effect ends and the unit's movement is reset so
// it re-plans from where it was left. Caller holds s.mu.
func (s *GameState) tickUnitPullLocked(unit *Unit, dt float64) {
	if unit == nil || unit.PullRemaining <= 0 {
		return
	}
	step := unit.PullStrength * dt
	var dx, dy float64
	if unit.PullPush {
		dx = unit.X - unit.PullCenterX
		dy = unit.Y - unit.PullCenterY
	} else {
		dx = unit.PullCenterX - unit.X
		dy = unit.PullCenterY - unit.Y
	}
	dist := math.Hypot(dx, dy)
	if step > 0 && dist > 1e-6 {
		if !unit.PullPush && step >= dist {
			unit.X = unit.PullCenterX
			unit.Y = unit.PullCenterY
		} else {
			unit.X += dx / dist * step
			unit.Y += dy / dist * step
		}
	}
	unit.PullRemaining -= dt
	if unit.PullRemaining <= 0 {
		s.endUnitPullLocked(unit)
	}
}

// endUnitPullLocked clears a unit's pull state and drops its current path so it
// resumes normal AI/movement from its displaced position — the stale pre-pull
// path is discarded so it never snaps the unit back. The unit's Order is left
// intact; the per-unit movement loop re-plans from the !Moving state next tick.
// Caller holds s.mu.
func (s *GameState) endUnitPullLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.PullRemaining = 0
	unit.PullStrength = 0
	unit.PullCenterX = 0
	unit.PullCenterY = 0
	unit.PullPush = false
	// Drop the stale path so resumption starts from the displaced position.
	unit.Path = nil
	unit.Moving = false
}
