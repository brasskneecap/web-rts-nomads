package game

import "math"

// spell_pull.go is the forced-displacement (pull) control-effect subsystem
// (arch-mage-spell-system). It is the first knockback/displacement primitive in
// the codebase and is built to be reused by any future push/pull effect.
//
// Model: an affected unit carries a per-unit pull state (PullRemaining,
// PullCenter*, PullStrength — see the Unit struct). Every tick the unit is
// dragged toward its pull center by a deterministic delta (pure math — no
// wall-clock, no RNG), and its normal path advancement is skipped for that tick
// (displacement wins). On expiry the unit's stale path is dropped so it resumes
// normal AI/movement from its displaced position rather than snapping back
// along a path computed before the pull.
//
// Collision policy (design D5): pulled units CLIP THROUGH obstacles during the
// pull — the per-tick delta is unconditioned geometry, no pathfinding. Only the
// final rest position is reconciled, by the existing stuck/repath recovery once
// the unit resumes. This trades visual purity for determinism and simplicity.

// applyPullLocked places (or refreshes) a forced-displacement effect on unit,
// dragging it toward (cx,cy) at strength world-px/sec for duration seconds. A
// dead unit, non-positive strength, or non-positive duration is a no-op. A new
// pull overwrites any existing one on the same unit (refresh semantics).
//
// This does NOT filter by allegiance — the caller decides who is eligible (see
// applyPullInRadiusLocked, which pulls hostiles only). Caller holds s.mu.
func (s *GameState) applyPullLocked(unit *Unit, cx, cy, strength, duration float64) {
	if unit == nil || unit.HP <= 0 || strength <= 0 || duration <= 0 {
		return
	}
	unit.PullCenterX = cx
	unit.PullCenterY = cy
	unit.PullStrength = strength
	unit.PullRemaining = duration
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

// tickUnitPullLocked advances one unit's active pull by dt. No-op when the unit
// is not being pulled. The unit is dragged toward its pull center by
// PullStrength*dt, never overshooting (it snaps to the center if the step would
// pass it). When the timer runs out the pull ends and the unit's movement is
// reset so it re-plans from where it was left. Caller holds s.mu.
func (s *GameState) tickUnitPullLocked(unit *Unit, dt float64) {
	if unit == nil || unit.PullRemaining <= 0 {
		return
	}
	step := unit.PullStrength * dt
	dx := unit.PullCenterX - unit.X
	dy := unit.PullCenterY - unit.Y
	dist := math.Hypot(dx, dy)
	if step > 0 && dist > 1e-6 {
		if step >= dist {
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
	// Drop the stale path so resumption starts from the displaced position.
	unit.Path = nil
	unit.Moving = false
}
