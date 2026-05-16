package game

// Ability cast lifecycle.
//
// Flow:
//   beginAbilityCastLocked   — validate (target / range / mana / not busy),
//                              lock the caster, start the cast timer. Returns
//                              (false, reason) on a synchronous failure; the
//                              WS/action-bar layer turns `reason` into the
//                              existing protocol.NotificationMessage (same
//                              pattern as the "Not enough resources" path).
//   tickUnitCastLocked       — per-tick: re-resolve & re-validate the target
//                              by ID, count the timer down, resolve on 0, or
//                              cancel if the target became invalid.
//   resolveAbilityCastLocked — spend mana, apply the effect (Heal: clamped,
//                              no overheal), play the on-target effect.
//
// Cast is UNINTERRUPTIBLE by design decision: damage / CC do NOT cancel it.
// It only ends early if the caster dies or the target becomes invalid
// (dies / leaves range / wrong allegiance). A clear seam exists to add
// damage/CC interrupts later (see cancelUnitCastLocked call sites).
//
// Mana is checked at initiation and DEDUCTED ON COMPLETION (per spec), so an
// interrupted/cancelled cast costs nothing.

// Cast failure reasons. Stable, human-readable strings — the WS layer passes
// them straight into protocol.NotificationMessage.Message.
const (
	castFailUnknownAbility = "That ability is unavailable."
	castFailNotOwned       = "This unit can't cast that ability."
	castFailAlreadyCasting = "Already casting."
	castFailInvalidTarget  = "Invalid target."
	castFailOutOfRange     = "Target is out of range."
	castFailNotEnoughMana  = "Not enough mana."
	castFailTargetLost     = "Target lost." // async: target died / left range mid-cast
)

// containsAbility reports whether the unit has abilityID in its ability list.
func containsAbility(u *Unit, abilityID string) bool {
	for _, id := range u.Abilities {
		if id == abilityID {
			return true
		}
	}
	return false
}

// beginAbilityCastLocked attempts to start caster casting abilityID at target.
// Returns (true, "") when the cast has started (or, for a 0 cast-time
// ability, resolved immediately); otherwise (false, reason) where reason is
// one of the castFail* strings, suitable for direct player feedback.
//
// Validation order is deliberate so the feedback is the most relevant:
// ownership → not-busy → target legality → range → mana.
//
// Caller holds s.mu.
func (s *GameState) beginAbilityCastLocked(caster *Unit, abilityID string, target *Unit) (bool, string) {
	if caster == nil || caster.HP <= 0 {
		return false, castFailInvalidTarget
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return s.failCastLocked(caster, castFailUnknownAbility)
	}
	if !containsAbility(caster, abilityID) {
		return s.failCastLocked(caster, castFailNotOwned)
	}
	if caster.CastAbilityID != "" {
		return s.failCastLocked(caster, castFailAlreadyCasting)
	}
	if !s.canAbilityTargetUnitLocked(def, caster, target) {
		return s.failCastLocked(caster, castFailInvalidTarget)
	}
	if !def.WithinCastRange(caster, target) {
		return s.failCastLocked(caster, castFailOutOfRange)
	}
	if caster.CurrentMana < def.ManaCost {
		return s.failCastLocked(caster, castFailNotEnoughMana)
	}

	caster.LastCastFailure = ""

	// Zero / negative cast time ⇒ instant ability (no lock, resolve now).
	if def.CastTime <= 0 {
		s.resolveAbilityCastLocked(caster, def, target)
		return true, ""
	}

	caster.CastAbilityID = abilityID
	caster.CastTargetID = target.ID
	caster.CastTimeRemaining = def.CastTime
	s.beginUnitCastingLocked(caster) // Part 5: lock + "Casting" animation slot
	return true, ""
}

// failCastLocked records the failure reason on the unit (for async/UI
// surfacing) and returns the (false, reason) tuple.
func (s *GameState) failCastLocked(caster *Unit, reason string) (bool, string) {
	if caster != nil {
		caster.LastCastFailure = reason
	}
	return false, reason
}

// tickUnitCastLocked advances an in-progress cast by dt seconds. No-op for a
// unit that is not casting. The target is held by ID and re-resolved every
// tick (per the targeting invariant): if it dies, leaves range, or otherwise
// becomes an illegal target, the cast is cancelled cleanly (no heal, no mana
// spent). Otherwise the timer counts down and the ability resolves at 0.
//
// Caller holds s.mu.
func (s *GameState) tickUnitCastLocked(unit *Unit, dt float64) {
	if unit == nil || unit.CastAbilityID == "" {
		return
	}
	def, ok := getAbilityDef(unit.CastAbilityID)
	if !ok {
		s.cancelUnitCastLocked(unit, castFailUnknownAbility)
		return
	}
	if unit.HP <= 0 {
		// Caster died mid-cast — just clear the cast bookkeeping (the unit is
		// being removed anyway).
		s.clearUnitCastLocked(unit)
		return
	}
	target := s.getUnitByIDLocked(unit.CastTargetID)
	if !s.canAbilityTargetUnitLocked(def, unit, target) || !def.WithinCastRange(unit, target) {
		s.cancelUnitCastLocked(unit, castFailTargetLost)
		return
	}

	unit.CastTimeRemaining -= dt
	if unit.CastTimeRemaining > 0 {
		return // still casting; Part 5 guard keeps Status pinned to "Casting"
	}
	s.resolveAbilityCastLocked(unit, def, target)
	s.clearUnitCastLocked(unit)
}

// resolveAbilityCastLocked applies a completed cast: deduct mana, then the
// ability's effect. Heal restores HealAmount HP clamped to the target's MaxHP
// (no overheal — does NOT route through healUnitLocked, which converts
// overheal to a perk shield). The on-target effect (e.g. "healing_glow")
// plays via the shared transient-effect pipeline.
//
// Caller holds s.mu. Caller is responsible for clearing the cast state.
func (s *GameState) resolveAbilityCastLocked(caster *Unit, def AbilityDef, target *Unit) {
	if caster == nil || target == nil {
		return
	}
	// Mana is paid here (on completion). spendUnitManaLocked is the single
	// authoritative spend; a false return (shouldn't happen post-init-check)
	// fails the cast gracefully with no effect.
	if !s.spendUnitManaLocked(caster, def.ManaCost) {
		caster.LastCastFailure = castFailNotEnoughMana
		return
	}

	if def.HealAmount > 0 && target.HP > 0 {
		target.HP += def.HealAmount
		if target.HP > target.MaxHP {
			target.HP = target.MaxHP // no overheal
		}
	}

	if def.EffectOnTarget != "" {
		s.playEffectOnUnitLocked(target, def.EffectOnTarget)
	}
}

// cancelUnitCastLocked aborts an in-progress cast (target lost, etc.): no
// mana spent, no effect, records the reason for feedback, then clears state.
func (s *GameState) cancelUnitCastLocked(unit *Unit, reason string) {
	if unit == nil {
		return
	}
	unit.LastCastFailure = reason
	s.clearUnitCastLocked(unit)
}

// clearUnitCastLocked clears cast bookkeeping and releases the Part 5 cast
// lock (Casting flag / "Casting" status). Safe to call when not casting.
func (s *GameState) clearUnitCastLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.CastAbilityID = ""
	unit.CastTargetID = 0
	unit.CastTimeRemaining = 0
	s.endUnitCastingLocked(unit)
}
