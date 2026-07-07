package game

import "math"

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
	// Channeled abilities use a separate lifecycle. Route them here so that
	// the WS handler and auto-cast code paths need no change — they all call
	// beginAbilityCastLocked and this branch handles the dispatch transparently.
	if def.ChannelType != "" {
		return s.beginAbilityChannelLocked(caster, abilityID, target)
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

	// Arm the cooldown at cast START so both manual and auto-cast paths share
	// the same gate. This is the single source of truth for cooldown timing;
	// auto-cast formerly armed cooldown post-cast in tickUnitAutoCastLocked,
	// which meant manual casts left the action button looking "ready" while
	// the unit was still mid-resolve. Arming on start also matches the
	// player's mental model: clicking the ability "spends" it immediately,
	// the wipe overlay starts ticking right away. An interrupted cast keeps
	// the cooldown (consistent with how every other RTS handles it).
	//
	// EffectiveCooldown() clamps the duration to max(Cooldown, CastTime), so a
	// spell like base Heal (1s cast / 0s authored cooldown) still produces a
	// visible 1s wipe while the unit is locked in place mid-cast — without
	// the clamp, the action button would silently flash with no countdown
	// even though the player can see the cast animation playing.
	cdDuration := def.EffectiveCooldown()
	if cdDuration > 0 {
		if caster.AbilityCooldowns == nil {
			caster.AbilityCooldowns = make(map[string]float64, 1)
		}
		caster.AbilityCooldowns[abilityID] = cdDuration
	}

	// Zero / negative cast time ⇒ instant ability (no lock, resolve now).
	if def.CastTime <= 0 {
		targets := s.buildCastTargetSetLocked(caster, def, target)
		s.resolveAbilityCastLocked(caster, def, targets)
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
	// Re-resolve the primary/anchor target by ID every tick (targeting
	// invariant). If it is gone the cast cancels regardless of TargetCount.
	primary := s.getUnitByIDLocked(unit.CastTargetID)
	if !s.canAbilityTargetUnitLocked(def, unit, primary) || !def.WithinCastRange(unit, primary) {
		s.cancelUnitCastLocked(unit, castFailTargetLost)
		return
	}

	unit.CastTimeRemaining -= dt
	if unit.CastTimeRemaining > 0 {
		return // still casting; Part 5 guard keeps Status pinned to "Casting"
	}
	targets := s.buildCastTargetSetLocked(unit, def, primary)
	s.resolveAbilityCastLocked(unit, def, targets)
	s.clearUnitCastLocked(unit)
}

// resolveAbilityCastLocked applies a completed cast: deduct mana once, then
// apply the ability's effect to each target in the slice. For single-target
// abilities the slice has exactly one element and the behaviour is unchanged.
// For multi-target abilities (def.TargetCount > 1) the effect (heal, damage,
// VFX, perk hook) fires once per target; mana is deducted once regardless of
// how many targets were hit.
//
// Heal restores HealAmount HP clamped to the target's MaxHP (no overheal —
// does NOT route through healUnitLocked, which converts overheal to a perk
// shield). The on-target effect (e.g. "healing_glow") plays via the shared
// transient-effect pipeline.
//
// An empty or nil targets slice is a no-op: mana is never spent for a cast
// with no valid targets (edge case: all targets died between cast-start and
// resolution).
//
// Caller holds s.mu. Caller is responsible for clearing the cast state.
func (s *GameState) resolveAbilityCastLocked(caster *Unit, def AbilityDef, targets []*Unit) {
	if caster == nil || len(targets) == 0 {
		return
	}
	// Mana is paid here (on completion). spendUnitManaLocked is the single
	// authoritative spend; a false return (shouldn't happen post-init-check)
	// fails the cast gracefully with no effect.
	if !s.spendUnitManaLocked(caster, def.ManaCost) {
		caster.LastCastFailure = castFailNotEnoughMana
		return
	}

	for _, target := range targets {
		if target == nil {
			continue
		}
		s.resolveAbilityCastOnTargetLocked(caster, def, target)
	}
}

// resolveAbilityCastOnTargetLocked applies the ability's per-target effects
// (heal, damage, VFX, perk hooks) to a single target. Mana is NOT deducted
// here — it is deducted once by resolveAbilityCastLocked before this is called.
//
// Caller holds s.mu.
func (s *GameState) resolveAbilityCastOnTargetLocked(caster *Unit, def AbilityDef, target *Unit) {
	if target == nil {
		return
	}
	if def.HealAmount > 0 && target.HP > 0 {
		// Divine Healer (silver cleric) scales every heal amount produced by
		// the caster. Default multiplier is 1.0; perks_cleric_silver.go owns
		// the lookup so future heal-sources just call the same helper.
		amount := int(math.Round(float64(def.HealAmount) * s.perkClericHealOutputMultiplierLocked(caster)))
		if amount < 0 {
			amount = 0
		}
		// Route the heal through the central cleric helper so gold-tier
		// triggers (beacon_of_life splash, divine_judgement AoE) fire under
		// the canonical metadata flags. The helper handles overheal routing,
		// records the heal event with the EFFECTIVE gain, and dispatches
		// triggers using the INTENDED amount (per spec: Judgement uses the
		// full heal value even on full-HP / overheal targets).
		s.applyClericHealLocked(caster, target, amount, healMetaPrimaryAbility())
	}

	// Offensive resolve step (symmetric to HealAmount). Two delivery modes:
	//   - Projectile abilities (def.Projectile set) launch a homing bolt that
	//     carries the damage and applies it ON IMPACT (arcane_bolt). The bolt
	//     rides the same pipeline basic-attack shots use, so mitigation, the
	//     death pipeline, threat, and determinism all apply at landing.
	//   - Otherwise the damage is applied INSTANTLY (hitscan) through the shared
	//     authoritative pipeline — the prior behaviour.
	// 0 / absent DamageAmount ⇒ no damage (inert for non-offensive abilities).
	if def.DamageAmount > 0 && target.HP > 0 {
		if def.Projectile != "" {
			s.fireAbilityProjectileLocked(caster, target, def)
		} else {
			s.applyUnitDamageWithSourceLocked(target, def.DamageAmount, DamageSource{
				AttackerUnitID: caster.ID,
				Kind:           "ability",
				DamageType:     def.DamageType.OrPhysical(),
			})
		}
	}

	if def.EffectOnTarget != "" {
		s.playEffectOnUnitLocked(target, def.EffectOnTarget)
	}

	if def.SummonUnitType != "" {
		s.spawnSummonedUnitLocked(caster, def)
	}

	// Perk hook: fire once per resolved target so perks like battle_prayer
	// can stamp cross-unit buffs on every ally the ability touches.
	s.onPerkAbilityResolvedLocked(caster, def, target)
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
