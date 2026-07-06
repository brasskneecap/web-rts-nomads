package game

import "strconv"

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

// Slows come in independent CATEGORIES that are tracked and timed separately
// and STACK multiplicatively (see slowFactorLocked):
//
//   - Physical/generic slow (SlowedRemaining/SlowedMultiplier): traps, melee
//     concussive perks. Applied via ApplySlowLocked.
//   - Cold slow (ColdSlowedRemaining/ColdSlowedMultiplier): chill from cold
//     equipment procs (frost_sword). Applied via ApplyColdSlowLocked. Drives
//     the client's icy overlay.
//
// Within a category, refreshing follows refresh-stronger (keep the lower
// multiplier) + refresh-longer (keep the greater duration), independently.
// Across categories the two never overwrite each other.

// slowTargetLocked validates a slow request and returns the target unit, or nil
// when the request is a no-op (out-of-range values, missing/dead target).
func (s *GameState) slowTargetLocked(targetID int, multiplier, duration float64) *Unit {
	if duration <= 0 || multiplier <= 0 || multiplier >= 1.0 {
		return nil
	}
	target := s.getUnitByIDLocked(targetID)
	if target == nil || target.HP <= 0 {
		return nil
	}
	return target
}

// applySlowToTrack applies the refresh-stronger / refresh-longer policy to one
// slow track (a remaining/multiplier field pair).
func applySlowToTrack(remaining, multiplier *float64, mult, dur float64) {
	if *remaining <= 0 || mult < *multiplier {
		*multiplier = mult // refresh-stronger: keep the lower (stronger) multiplier
	}
	if dur > *remaining {
		*remaining = dur // refresh-longer: keep the greater duration
	}
}

// ApplySlowLocked stamps a PHYSICAL/generic slow onto the target (traps, melee
// concussive perks). No-op if target is not found or is dead.
//
// Must be called under s.mu write lock.
func (s *GameState) ApplySlowLocked(targetID int, multiplier, duration float64) {
	if target := s.slowTargetLocked(targetID, multiplier, duration); target != nil {
		applySlowToTrack(&target.SlowedRemaining, &target.SlowedMultiplier, multiplier, duration)
	}
}

// ApplyColdSlowLocked stamps a COLD (chill) slow onto the target — a separate
// track from the physical slow, so a chilled unit can also carry a trap slow
// and both apply. Drives the client's icy overlay. No-op if target is not found
// or is dead.
//
// Must be called under s.mu write lock.
func (s *GameState) ApplyColdSlowLocked(targetID int, multiplier, duration float64) {
	if target := s.slowTargetLocked(targetID, multiplier, duration); target != nil {
		applySlowToTrack(&target.ColdSlowedRemaining, &target.ColdSlowedMultiplier, multiplier, duration)
	}
}

// applyProcSlowLocked routes an equipment proc's on-hit slow to the correct
// track by the proc's damage type: cold damage lands a chill (cold slow → icy
// overlay), anything else lands a physical/generic slow. Both no-op on zero /
// out-of-range values. The shared seam both the projectile and beam proc-land
// paths call so a cold proc always chills and a physical proc never does.
//
// Must be called under s.mu write lock.
func (s *GameState) applyProcSlowLocked(targetID int, multiplier, duration float64, dmgType DamageType) {
	if dmgType == DamageCold {
		s.ApplyColdSlowLocked(targetID, multiplier, duration)
	} else {
		s.ApplySlowLocked(targetID, multiplier, duration)
	}
}

// applyProcBurnLocked ignites an equipment proc's target with a fire
// damage-over-time stack (fire_sword). It reuses the shared burn system that
// backs the Trapper fire_pit perks (UnitPerkState.BurnStacks) so a weapon burn
// ticks through the same tickTrapperSilverDebuffsLocked loop and lights up the
// same client burning overlay. The stack is keyed per attacker
// ("weaponburn:<attackerID>") so the same wielder refreshes its stack
// (refresh-stronger DPS / refresh-longer duration) rather than piling up
// infinite stacks, while different wielders ignite independent stacks. Tagged
// burnSourceWeapon so the burn tick credits the wielding unit, not a trap.
// No-op on zero / non-positive values, or when the target is gone. The shared
// seam both the projectile and beam proc-land paths call.
//
// Must be called under s.mu write lock.
func (s *GameState) applyProcBurnLocked(targetID int, dps, duration float64, attackerUnitID int) {
	if dps <= 0 || duration <= 0 {
		return
	}
	target := s.getUnitByIDLocked(targetID)
	if target == nil || target.HP <= 0 || !target.Visible {
		return
	}
	target.PerkState.applyBurnStack(
		"weaponburn:"+strconv.Itoa(attackerUnitID),
		"", // ungrouped: shares the global per-victim cap with other non-trap burns
		attackerUnitID,
		dps,
		duration,
		0, 0, // no Reactive Flames — that's a Trapper-gold trap effect only
		burnSourceWeapon,
	)
}

// slowFactorLocked returns the effective speed fraction for unit from ALL active
// slow categories, composed multiplicatively (physical × cold). Returns 1.0
// when nothing is active. Applied to both movement step (state.go Update()) and
// attack cadence (state_combat.go) so every slow scales both.
//
// This is a pure function of the unit pointer; no GameState receiver is needed.
func slowFactorLocked(unit *Unit) float64 {
	factor := 1.0
	if unit.SlowedRemaining > 0 && unit.SlowedMultiplier > 0 {
		factor *= unit.SlowedMultiplier
	}
	if unit.ColdSlowedRemaining > 0 && unit.ColdSlowedMultiplier > 0 {
		factor *= unit.ColdSlowedMultiplier
	}
	return factor
}
