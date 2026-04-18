package game

// ─────────────────────────────────────────────────────────────────────────────
// CC (crowd-control) primitives — Stun and Slow
//
// These are generic status effects that any perk, ability, or enemy mechanic
// can stamp onto any unit. They are NOT perk-specific: the fields live on
// Unit (not UnitPerkState) and their decay runs in the main Update() loop
// alongside the other cross-unit debuffs (WeakenedRemaining, MarkedRemaining).
//
// Phase 1: primitives only. No perk that calls these functions exists yet —
// that lands in Phase 2 (shield_bash) and later phases. The helpers are
// exported so callers in future files can use them without import gymnastics.
//
// Locking convention: all three functions must be called under s.mu write lock
// (suffix "Locked"). The slow helper slowFactorLocked is a pure function that
// reads only from the unit pointer; it has no GameState receiver and requires
// no lock to call, but callers inside locked methods should treat it as part
// of the locked surface.
// ─────────────────────────────────────────────────────────────────────────────

// ApplyStunLocked stamps a stun onto the target unit for duration seconds.
// Refresh-longer policy: if a longer stun is already active it is kept; a
// shorter incoming stun is silently ignored. A new stun longer than the
// current remaining time will extend to the new duration.
//
// No-op if target is not found in the game state or is dead (HP <= 0).
// AttackTargetID and Path are NOT cleared — the unit resumes combat and
// movement exactly where it left off once the stun expires.
//
// Must be called under s.mu write lock.
func (s *GameState) ApplyStunLocked(targetID int, duration float64) {
	if duration <= 0 {
		return
	}
	target := s.getUnitByIDLocked(targetID)
	if target == nil || target.HP <= 0 {
		return
	}
	if duration > target.StunnedRemaining {
		target.StunnedRemaining = duration
	}
}

// ApplySlowLocked stamps a slow onto the target unit with the given multiplier
// and duration.
//
// Refresh-stronger policy for multiplier: keep the lower value (stronger slow).
//   e.g. existing 0.8× + incoming 0.6× → result is 0.6×.
//
// Refresh-longer policy for duration: keep the higher value.
//   e.g. existing 1.0 s + incoming 2.0 s → result is 2.0 s.
//
// The two policies are independent — a weaker slow that lasts longer extends
// duration without losing the stronger existing multiplier.
//
// No-op if target is not found or is dead.
//
// Must be called under s.mu write lock.
func (s *GameState) ApplySlowLocked(targetID int, multiplier, duration float64) {
	if duration <= 0 || multiplier <= 0 || multiplier >= 1.0 {
		return
	}
	target := s.getUnitByIDLocked(targetID)
	if target == nil || target.HP <= 0 {
		return
	}
	// Refresh-stronger: keep the lower multiplier (= stronger slow).
	if target.SlowedRemaining <= 0 || multiplier < target.SlowedMultiplier {
		target.SlowedMultiplier = multiplier
	}
	// Refresh-longer: keep the greater remaining duration.
	if duration > target.SlowedRemaining {
		target.SlowedRemaining = duration
	}
}

// slowFactorLocked returns the effective movement-speed fraction for unit due
// to an active slow. Returns the SlowedMultiplier while SlowedRemaining > 0,
// otherwise returns 1.0 (no reduction). Composing this factor into the step
// calculation in Update() is the only place it needs to appear — see state.go.
//
// This is a pure function of the unit pointer; no GameState receiver is needed.
func slowFactorLocked(unit *Unit) float64 {
	if unit.SlowedRemaining > 0 && unit.SlowedMultiplier > 0 {
		return unit.SlowedMultiplier
	}
	return 1.0
}
