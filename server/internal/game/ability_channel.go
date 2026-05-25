package game

import "math"

// Channeled-ability lifecycle.
//
// A channeled ability (ChannelType != "") differs from a one-shot cast in
// that it persists across multiple ticks, applying its effect on a cadence
// (TickIntervalSeconds) until one of the stop conditions is met. The Siphon
// Life ability is the first in-game channeled ability.
//
// Flow:
//   beginAbilityChannelLocked  — validate, lock caster, spawn Beam, start
//                                ChannelNextTickIn = 0 so first tick fires
//                                immediately.
//   tickUnitChannelLocked      — per-tick: re-validate target, decrement
//                                ChannelNextTickIn, fire effect when <= 0.
//   stopUnitChannelLocked      — records a failure reason, despawns beam,
//                                clears channel state.
//   clearUnitChannelLocked     — same but no reason (caster died).
//
// Channels are mutually exclusive with one-shot casts: a unit may have
// either CastAbilityID or ChannelAbilityID set, never both. The entry point
// (beginAbilityCastLocked in ability_cast.go) branches on ChannelType so
// existing callers need no change.
//
// Cancel triggers (any of these → stopUnitChannelLocked):
//   - Caster dies (HP <= 0) → clearUnitChannelLocked (no UI reason needed).
//   - Target nil / HP≤0 / Visible=false / wrong team → castFailTargetLost.
//   - Target out of range → castFailTargetLost.
//   - Caster cannot afford next tick's mana → castFailNotEnoughMana.
//   - New order issued (move, attack, stop) → "Order issued"
//     (via resetUnitMovementLocked in state_movement.go).
//   - Caster stunned → "Channel interrupted."

// channelInterruptedStun is the failure reason recorded when a stun stops a
// channel. Kept here (not in ability_cast.go) since it is channel-specific.
const channelInterruptedStun = "Channel interrupted."

// channelInterruptedOrder is the failure reason recorded when a new player
// order (move / attack / stop / etc.) cancels an active channel.
const channelInterruptedOrder = "Order issued."

// channelMaxTicksPerUpdate caps how many channel effect ticks fire in one
// Update() call when dt is unusually large. Prevents pathological dt
// explosions from applying dozens of ticks in a single frame.
const channelMaxTicksPerUpdate = 4

// beginAbilityChannelLocked starts a channel on caster for the named ability
// targeting target. Returns (true, "") when the channel has started;
// (false, reason) on a synchronous failure.
//
// Validation order mirrors beginAbilityCastLocked:
//   ownership → not-busy → target legality → range → mana-for-first-tick.
//
// Caller holds s.mu write lock.
func (s *GameState) beginAbilityChannelLocked(caster *Unit, abilityID string, target *Unit) (bool, string) {
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
	// Cannot start a channel while a one-shot cast OR another channel is in
	// progress.
	if caster.CastAbilityID != "" || caster.ChannelAbilityID != "" {
		return s.failCastLocked(caster, castFailAlreadyCasting)
	}
	if !s.canAbilityTargetUnitLocked(def, caster, target) {
		return s.failCastLocked(caster, castFailInvalidTarget)
	}
	if !def.WithinCastRange(caster, target) {
		return s.failCastLocked(caster, castFailOutOfRange)
	}
	// Require mana for at least the first tick so the channel doesn't start
	// on a caster that is already empty.
	if caster.CurrentMana < def.ManaCostPerTick {
		return s.failCastLocked(caster, castFailNotEnoughMana)
	}

	caster.LastCastFailure = ""

	// Defensively clear any stale one-shot cast state (should be empty due to
	// the guard above, but be safe against future refactor paths).
	s.clearUnitCastLocked(caster)

	// Arm cooldown at channel START, matching the one-shot cast convention so
	// both manual and auto-cast paths share the same gate.
	cdDuration := def.EffectiveCooldown()
	if cdDuration > 0 {
		if caster.AbilityCooldowns == nil {
			caster.AbilityCooldowns = make(map[string]float64, 1)
		}
		caster.AbilityCooldowns[abilityID] = cdDuration
	}

	// Set channel state. ChannelNextTickIn starts at TickIntervalSeconds so
	// the first effect tick fires after one full interval has elapsed. This
	// prevents a double-fire on the very first Update call when dt exactly
	// equals the interval (decrement would land exactly on 0, triggering the
	// loop a second time).
	caster.ChannelAbilityID = abilityID
	caster.ChannelTargetID = target.ID
	caster.ChannelTickInterval = def.TickIntervalSeconds
	caster.ChannelNextTickIn = def.TickIntervalSeconds

	// Lock movement and set "Casting" animation — reuses the one-shot cast
	// primitive so existing movement / combat guards treat this unit as busy.
	// Then upgrade the status string to "Channeling" so the client can pin
	// the sprite to ChannelHoldFrame instead of cycling the casting cycle.
	// The combat-tick guard in state_combat.go re-derives the right status
	// each tick (Channeling when ChannelAbilityID is set, else Casting).
	s.beginUnitCastingLocked(caster)
	caster.Status = unitStatusChanneling

	// Spawn the beam visual entity.
	s.spawnBeamLocked(caster, target, abilityID, abilityID) // variant = abilityID

	return true, ""
}

// tickUnitChannelLocked advances an active channel by dt seconds. No-op for a
// unit that is not channeling. Re-validates the target by ID every tick per
// the ID-not-pointer invariant. Fires the channel effect (damage + siphon
// heal) for each elapsed tick interval.
//
// Caller holds s.mu write lock.
func (s *GameState) tickUnitChannelLocked(unit *Unit, dt float64) {
	if unit == nil || unit.ChannelAbilityID == "" {
		return
	}
	def, ok := getAbilityDef(unit.ChannelAbilityID)
	if !ok {
		// Unknown ability — clear silently, no reason to surface.
		s.clearUnitChannelLocked(unit)
		return
	}

	// Caster died — just clear bookkeeping (unit is being removed anyway).
	if unit.HP <= 0 {
		s.clearUnitChannelLocked(unit)
		return
	}

	// Stun interrupts the channel.
	if unit.StunnedRemaining > 0 {
		s.stopUnitChannelLocked(unit, channelInterruptedStun)
		return
	}

	// Re-resolve and validate the target every tick (ID-not-pointer rule).
	target := s.getUnitByIDLocked(unit.ChannelTargetID)
	if !s.isValidChannelTargetLocked(unit, def, target) {
		s.stopUnitChannelLocked(unit, castFailTargetLost)
		return
	}

	// Decrement the timer and fire as many ticks as have elapsed.
	unit.ChannelNextTickIn -= dt

	safetyCounter := 0
	for unit.ChannelNextTickIn <= 0 {
		if safetyCounter >= channelMaxTicksPerUpdate {
			// Pathological dt — skip remaining ticks this Update and let
			// the next call catch up normally.
			break
		}
		safetyCounter++

		// Check mana for this tick.
		if unit.CurrentMana < def.ManaCostPerTick {
			s.stopUnitChannelLocked(unit, castFailNotEnoughMana)
			return
		}
		if !s.spendUnitManaLocked(unit, def.ManaCostPerTick) {
			// Should not happen after the check above, but be safe.
			s.stopUnitChannelLocked(unit, castFailNotEnoughMana)
			return
		}

		// Aggregate Siphoner perk modifiers (Soul Leech) so a single Bronze
		// pick scales BOTH the damage applied and the heal generated by
		// this tick. Defaults to 1.0/1.0 for Siphoners without the perk.
		dmgMult, healMult := s.siphonLifeModifiersForCasterLocked(unit)
		tickDamage := int(math.Round(float64(def.DamagePerTick) * dmgMult))
		if tickDamage < 0 {
			tickDamage = 0
		}

		// Apply damage to the target through the central pipeline so
		// mitigation, the death pipeline, threat, and determinism all apply.
		if tickDamage > 0 {
			s.applyUnitDamageWithSourceLocked(target, tickDamage, DamageSource{
				AttackerUnitID: unit.ID,
				Kind:           "ability",
				DamageType:     def.DamageType.OrPhysical(),
			})
		}

		// Compute heal amount from the SAME tickDamage (so Soul Leech's
		// damage multiplier feeds through proportionally) and distribute
		// via siphon heal logic. HealingMultiplier (ability-level) stacks
		// multiplicatively with healMult (perk-level).
		healAmount := int(math.Round(float64(tickDamage) * def.HealingMultiplier * healMult))
		if healAmount > 0 {
			s.distributeSiphonHealLocked(unit, healAmount, def.AllyHealRadius)
		}

		// Withering Beam — accrue continuous-siphon time and stamp stacks
		// on the target every secondsPerStack. Runs after damage so a
		// killing tick doesn't also stamp on a corpse (the helper checks
		// target.HP, but applyUnitDamageWithSourceLocked may have already
		// removed the unit via the pending-death drain on the next tick).
		s.tickWitheringBeamChannelLocked(unit, target, unit.ChannelTickInterval)

		// Advance the timer for the next tick.
		unit.ChannelNextTickIn += unit.ChannelTickInterval
	}

	// Re-validate the target after the loop in case tick damage killed it.
	// Re-resolve so we get the fresh HP.
	target = s.getUnitByIDLocked(unit.ChannelTargetID)
	if !s.isValidChannelTargetLocked(unit, def, target) {
		s.stopUnitChannelLocked(unit, castFailTargetLost)
	}
}

// isValidChannelTargetLocked reports whether target is a legal, in-range
// enemy for the channel. Consolidates the nil / HP / Visible / ownership /
// range guards so they are expressed once and applied at both the loop entry
// and exit.
//
// Caller holds s.mu (read or write).
func (s *GameState) isValidChannelTargetLocked(caster *Unit, def AbilityDef, target *Unit) bool {
	if target == nil || target.HP <= 0 || !target.Visible {
		return false
	}
	if !s.canAbilityTargetUnitLocked(def, caster, target) {
		return false
	}
	if !def.WithinCastRange(caster, target) {
		return false
	}
	return true
}

// stopUnitChannelLocked records reason on the unit (for async/UI surfacing),
// despawns the caster's beam, clears channel state, and releases the cast
// lock. Use this when the channel ends with a player-visible reason (target
// lost, not enough mana, new order, stun). Use clearUnitChannelLocked when
// the caster died (no reason to surface).
//
// Caller holds s.mu write lock.
func (s *GameState) stopUnitChannelLocked(unit *Unit, reason string) {
	if unit == nil {
		return
	}
	unit.LastCastFailure = reason
	s.removeBeamForUnitLocked(unit.ID)
	s.clearChannelStateLocked(unit)
}

// clearUnitChannelLocked clears channel bookkeeping and releases the cast
// lock without recording a failure reason. Use when the caster died — no
// UI feedback is needed.
//
// Caller holds s.mu write lock.
func (s *GameState) clearUnitChannelLocked(unit *Unit) {
	if unit == nil {
		return
	}
	s.removeBeamForUnitLocked(unit.ID)
	s.clearChannelStateLocked(unit)
}

// clearChannelStateLocked zeroes the channel fields and calls endUnitCastingLocked.
// Internal helper shared by stop and clear.
//
// Caller holds s.mu write lock.
func (s *GameState) clearChannelStateLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.ChannelAbilityID = ""
	unit.ChannelTargetID = 0
	unit.ChannelTickInterval = 0
	unit.ChannelNextTickIn = 0
	// Withering Beam: zero the caster-side accumulator + tracking target so
	// a fresh channel starts cleanly. Stacks already on previously-siphoned
	// enemies keep decaying via the cross-unit loop in state.go.
	s.clearWitheringBeamCasterStateLocked(unit)
	s.endUnitCastingLocked(unit)
}

// channelLoopRangeForUnitLocked returns the (start, end) frame range the
// snapshot should ship for a channeling unit, or (0, 0) when the unit is
// not channeling or has no channel-pose configured. The client loops one-
// way through [start, end] inclusive on the casting sprite sheet while
// status == "Channeling". start == end produces a single held frame;
// start < end produces a small loop. Snapshot builders gate Status to
// "Channeling" so the client only reads this when it matters; (0, 0) is
// also a safe default (pin frame 0 if for any reason the lookup races a
// clear or the unit has no authored channel pose).
//
// Resolution order (visual data lives on the caster, not the ability — two
// units sharing one channel ability can pin different frames on their own
// sheets):
//   1. Path override:   pathChannelLoopByPath[unit.ProgressionPath]
//   2. Unit def:        unitDefsByType[unit.UnitType].ChannelLoop
//   3. Fallback:        (0, 0)
//
// Caller holds s.mu (read or write).
func (s *GameState) channelLoopRangeForUnitLocked(unit *Unit) (start, end int) {
	if unit == nil || unit.ChannelAbilityID == "" {
		return 0, 0
	}
	// Path override wins when present.
	if unit.ProgressionPath != "" {
		if r, ok := pathChannelLoopByPath[unit.ProgressionPath]; ok {
			return r.Start, r.End
		}
	}
	// Fall back to the base unit def.
	if def, ok := getUnitDef(unit.UnitType); ok && def.ChannelLoop != nil {
		return def.ChannelLoop.Start, def.ChannelLoop.End
	}
	return 0, 0
}

// channelLoopStartForUnitLocked and channelLoopEndForUnitLocked are thin
// wrappers around channelLoopRangeForUnitLocked that return a single field
// each, so the snapshot struct literal can assign them inline without
// threading a local pair through three near-identical builders.
func (s *GameState) channelLoopStartForUnitLocked(unit *Unit) int {
	start, _ := s.channelLoopRangeForUnitLocked(unit)
	return start
}

func (s *GameState) channelLoopEndForUnitLocked(unit *Unit) int {
	_, end := s.channelLoopRangeForUnitLocked(unit)
	return end
}

// ── Auto-cast precondition ────────────────────────────────────────────────────

// siphonHealingNeededLocked reports whether the Siphon Life auto-cast
// precondition is met: the caster OR any ally within def.AllyHealRadius has
// HP < MaxHP. Only when this returns true does the auto-cast loop consider
// starting the channel. This prevents wasteful mana drain when the whole team
// is at full health.
//
// Generic over any channeled ability with AllyHealRadius: if AllyHealRadius
// is 0, only the caster's own HP is tested (no ally scan).
//
// Caller holds s.mu (read or write).
func (s *GameState) siphonHealingNeededLocked(caster *Unit, def AbilityDef) bool {
	if caster == nil {
		return false
	}
	// Self is injured — healing is needed regardless of allies.
	if caster.HP < caster.MaxHP {
		return true
	}
	// Scan allies within allyHealRadius.
	if def.AllyHealRadius <= 0 {
		return false
	}
	radiusSq := def.AllyHealRadius * def.AllyHealRadius
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if u.ID == caster.ID {
			continue
		}
		if !s.unitsFriendlyLocked(caster, u) {
			continue
		}
		if u.HP >= u.MaxHP {
			continue
		}
		if distanceSquared(caster.X, caster.Y, u.X, u.Y) <= radiusSq {
			return true
		}
	}
	return false
}

// ── Siphon heal distribution ──────────────────────────────────────────────────

// distributeSiphonHealLocked applies amount HP of healing according to the
// Siphon Life binary rule:
//
//   - If the Siphoner's HP < MaxHP: route the entire amount to the Siphoner.
//   - Else: find the lowest-HP-percent ally within allyHealRadius (alive,
//     visible, same team, HP < MaxHP), heal that ally. Ties broken by
//     ascending unit.ID for determinism.
//
// In both cases the heal routes through applyClericHealLocked so the existing
// cleric perk pipeline (beacon_of_life, divine_judgement) fires when the
// Siphoner is also a Cleric (future cross-path extension point). Overheal is
// wasted; healing is always clamped to MaxHP. Returns the unit that received
// the heal, or nil if healing was wasted (no valid ally, or caster at full HP
// with no ally in range).
//
// Caller holds s.mu write lock.
func (s *GameState) distributeSiphonHealLocked(siphoner *Unit, amount int, allyHealRadius float64) *Unit {
	if siphoner == nil || amount <= 0 {
		return nil
	}

	// Self-heal path: Siphoner is below max HP.
	if siphoner.HP < siphoner.MaxHP {
		s.applyClericHealLocked(siphoner, siphoner, amount, healMetaPrimaryAbility())
		return siphoner
	}

	// Ally path: find lowest-HP-percent ally within allyHealRadius.
	// Iterate s.Units (slice → deterministic order). Filter: same team, alive,
	// visible, not self, HP < MaxHP, within allyHealRadius.
	var best *Unit
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if u.ID == siphoner.ID {
			continue // self excluded from ally path
		}
		if !s.unitsFriendlyLocked(siphoner, u) {
			continue
		}
		if u.HP >= u.MaxHP {
			continue // at full HP — no benefit
		}
		// Range check using squared distance to avoid sqrt.
		if allyHealRadius > 0 {
			distSq := distanceSquared(siphoner.X, siphoner.Y, u.X, u.Y)
			if distSq > allyHealRadius*allyHealRadius {
				continue
			}
		}

		if best == nil {
			best = u
			continue
		}
		// Lower HP% wins. Integer cross-multiply avoids floats; int64 prevents
		// overflow for large HP values.
		lhs := int64(u.HP) * int64(best.MaxHP)
		rhs := int64(best.HP) * int64(u.MaxHP)
		switch {
		case lhs < rhs:
			best = u
		case lhs == rhs:
			// Tie-break: ascending unit.ID for determinism.
			if u.ID < best.ID {
				best = u
			}
		}
	}

	if best == nil {
		// No injured ally in radius — healing is wasted.
		// extension point: future perk may bank overflow or log telemetry here.
		return nil
	}

	s.applyClericHealLocked(siphoner, best, amount, healMetaPrimaryAbility())
	return best
}
