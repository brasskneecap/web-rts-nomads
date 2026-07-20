package game

import "webrts/server/pkg/protocol"

// Action-bar auto-cast framework (Part 10).
//
// Generic over any AbilityDef with SupportsAutoCast == true. Per unit each
// tick the loop tries, for each auto-cast-enabled ability (in slot order):
// off cooldown? enough mana? a valid target from the ability's auto-cast
// selector? — if so it initiates a normal cast (cast_time, animation, mana,
// effect all via the Part 8 lifecycle). At most one cast is initiated per
// unit per tick, and never while a cast is already in progress, so cast_time
// is respected (no queuing/stacking).
//
// State is per-unit-instance (Unit.AutoCastEnabled / AbilityCooldowns) and
// dies with the unit. The loop iterates the ordered Abilities slice, never
// the maps, so it is deterministic under a fixed seed.

// toggleAutoCastLocked flips auto-cast for abilityID on unit. Returns the new
// enabled state and whether anything changed. It is a no-op (changed=false)
// when the unit does not have the ability, the ability id is unknown, or the
// ability's def has SupportsAutoCast == false (right-clicking a
// non-auto-cast ability has no effect, per spec).
//
// Caller holds s.mu.
func (s *GameState) toggleAutoCastLocked(unit *Unit, abilityID string) (enabled bool, changed bool) {
	if unit == nil || !containsAbility(unit, abilityID) {
		return false, false
	}
	def, ok := getAbilityDef(abilityID)
	if !ok || !def.SupportsAutoCast {
		return s.autoCastEnabledLocked(unit, abilityID), false
	}
	if unit.AutoCastEnabled == nil {
		unit.AutoCastEnabled = make(map[string]bool, 1)
	}
	unit.AutoCastEnabled[abilityID] = !unit.AutoCastEnabled[abilityID]
	return unit.AutoCastEnabled[abilityID], true
}

// autoCastEnabledLocked reports whether auto-cast is currently on for
// abilityID on unit. Caller holds s.mu.
func (s *GameState) autoCastEnabledLocked(unit *Unit, abilityID string) bool {
	return unit != nil && unit.AutoCastEnabled[abilityID]
}

// seedDefaultAutoCastLocked walks the unit's current Abilities slice and seeds
// AutoCastEnabled[id] = true for every ability whose def carries both
// SupportsAutoCast = true and DefaultAutoCast = true — but ONLY when there is
// no existing entry for that ability id. Player intent (an explicit on OR off
// toggle, or a value preserved through the heal→greater_heal migration)
// always wins; this helper never overwrites it.
//
// Called from two seams to keep the "default on" behavior consistent across
// every code path that gives a unit an ability:
//   - spawnUnitFromDefLocked, right after the initial Abilities slice is set
//     (only for player-owned units — enemy clerics intentionally never auto-
//     cast).
//   - assignUnitPathAbilitiesLocked, right after the position-by-position
//     migration loop, so additive grants at higher ranks (silver/gold cleric
//     getting a new ability, future paths granting new spells) inherit their
//     authored default without each call site re-implementing it.
//
// The DefaultAutoCast / SupportsAutoCast pair is the single switch a content
// author needs to toggle — no Go change required for a new auto-cast-on-spawn
// ability.
//
// Caller holds s.mu.
func (s *GameState) seedDefaultAutoCastLocked(unit *Unit) {
	if unit == nil {
		return
	}
	for _, id := range unit.Abilities {
		def, ok := getAbilityDef(id)
		if !ok || !def.SupportsAutoCast || !def.DefaultAutoCast {
			continue
		}
		if _, set := unit.AutoCastEnabled[id]; set {
			continue // preserve explicit player choice (or migrated value)
		}
		if unit.AutoCastEnabled == nil {
			unit.AutoCastEnabled = make(map[string]bool, 1)
		}
		unit.AutoCastEnabled[id] = true
	}
}

// tickUnitAbilityCooldownsLocked decays this unit's ability cooldowns by dt,
// clamping at 0. Iterates the unit's ordered Abilities slice (not the map)
// so it never depends on map order. No-op when nothing is on cooldown.
//
// Caller holds s.mu.
func (s *GameState) tickUnitAbilityCooldownsLocked(unit *Unit, dt float64) {
	if unit == nil {
		return
	}
	// Global cooldown decays independently of the per-ability cooldown map (a
	// unit can be on GCD with no per-ability cooldown running, e.g. after an
	// instant zero-cooldown ability).
	if unit.GlobalCooldownRemaining > 0 {
		if unit.GlobalCooldownRemaining -= dt; unit.GlobalCooldownRemaining < 0 {
			unit.GlobalCooldownRemaining = 0
		}
	}
	if len(unit.AbilityCooldowns) == 0 {
		return
	}
	for _, id := range unit.Abilities {
		if cd := unit.AbilityCooldowns[id]; cd > 0 {
			cd -= dt
			if cd <= 0 {
				delete(unit.AbilityCooldowns, id)
			} else {
				unit.AbilityCooldowns[id] = cd
			}
		}
	}
}

// tickUnitAutoCastLocked runs the auto-cast loop for one unit for this tick.
// It initiates at most one cast and never while the unit is already casting
// (so cast_time is respected). Cooldown arming happens inside
// beginAbilityCastLocked itself, so both the manual cast-ability flow and
// this auto-cast path share one source of truth — no per-path duplication.
//
// Phase 2: selection is gather → score → pick (no longer first-ready). It
// gathers every candidate that passes the UNCHANGED gates (autocast enabled,
// SupportsAutoCast, off cooldown, enough mana, a non-nil selector target),
// scores each via scoreAutoCastCandidateLocked, and casts the single highest
// (deterministic tiebreak: ascending unit.Abilities slot index, then ability
// id). A best score at/below minActivationScore ⇒ cast nothing (the basic
// attack proceeds via the unchanged combat AI).
//
// NO-REGRESSION INVARIANT: with exactly one passing candidate, scoring's
// per-category base (≥ candidateBaseScore ≫ minActivationScore for any valid
// target — see ability_priority.go) guarantees that candidate clears the
// floor, so the result is identical to the prior first-ready behaviour: the
// lone ability is cast on exactly the same ticks. Slot order is iterated
// ascending and a candidate replaces the best only on a STRICTLY greater
// score, so equal scores keep the lower slot (the spec's slot-then-id
// tiebreak; ids are unique per slot so the id tier is a documented no-op).
//
// Caller holds s.mu.
func (s *GameState) tickUnitAutoCastLocked(unit *Unit) {
	if unit == nil || unit.HP <= 0 || len(unit.AutoCastEnabled) == 0 {
		return
	}
	// A cast in progress blocks auto-cast — don't queue/stack another.
	if unit.CastAbilityID != "" {
		return
	}
	// Global cooldown from a recent cast blocks auto-cast, so a unit with
	// several ready abilities fires them spaced out rather than simultaneously.
	if unit.GlobalCooldownRemaining > 0 {
		return
	}
	// A player-issued Move command suppresses auto-cast for the duration of
	// the move. Without this, a Cleric ordered to a position would get glued
	// in place by repeatedly autocasting Heal on injured allies it passes —
	// each cast pins it (the cast lifecycle clears Moving/Path), and the
	// move command never reaches the destination. Once the unit arrives and
	// Order transitions back to Idle (or any non-Move state), autocast
	// resumes naturally.
	//
	// Scope: ONLY OrderMove. Other orders that involve travel (AttackMove,
	// Patrol, FocusFollow) intentionally still allow autocast — those orders
	// are about reaching a destination AND engaging/supporting en route, so
	// pausing for a heal is the right behaviour there. The plain Move order
	// is the only one with a "get there above all else" semantic.
	if unit.Order.Type == OrderMove {
		return
	}

	var (
		bestID    string
		bestTgt   *Unit
		bestScore float64
		haveBest  bool
	)
	for _, abilityID := range unit.Abilities { // ordered ⇒ deterministic
		if !unit.AutoCastEnabled[abilityID] {
			continue
		}
		def, ok := getAbilityDef(abilityID)
		if !ok || !def.SupportsAutoCast {
			continue
		}
		if unit.AbilityCooldowns[abilityID] > 0 {
			continue // on cooldown
		}
		if unit.CurrentMana < def.ManaCost {
			continue // not enough mana
		}
		// Channeled-ability precondition: for siphon_life (and any future
		// channeled ability with AllyHealRadius > 0), only auto-start when
		// the caster or a nearby ally needs healing. Without this guard, the
		// auto-cast would drain mana whenever an enemy is in range even when
		// the caster's team is at full health — wasteful and player-hostile.
		// def.IsChannelAbility() (not a raw ChannelType != "" check) so a
		// converted (SchemaVersion>=2) channel ability is still recognized —
		// see that method's doc comment.
		if def.IsChannelAbility() {
			spec, _ := channelSpecFor(def)
			if !s.siphonHealingNeededLocked(unit, spec.AllyHealRadius) {
				continue
			}
			// Also gate: don't start a channel while already channeling
			// another ability (the channel lifecycle itself enforces this,
			// but skipping here avoids routing a no-op cast through
			// beginAbilityCastLocked).
			if unit.ChannelAbilityID != "" {
				continue
			}
		}
		target := s.resolveAutoCastTargetLocked(unit, def)
		if target == nil {
			continue // no valid target right now
		}
		score := s.scoreAutoCastCandidateLocked(unit, def, target)
		if score <= minActivationScore {
			continue // not worth a cast (e.g. buff_ally/summon "not useful")
		}
		// Iterating slots ascending + replacing only on a strictly greater
		// score ⇒ ties resolve to the lower slot index (spec tiebreak).
		if !haveBest || score > bestScore {
			bestID, bestTgt, bestScore, haveBest = abilityID, target, score, true
		}
	}
	if !haveBest {
		return // nothing ready cleared the activation floor this tick
	}

	// beginAbilityCastLocked arms the ability cooldown itself, so both manual
	// and auto-cast paths share one source of truth — no double-arming here.
	// Point-targeted abilities (arcane_orb) aim toward the chosen enemy's
	// current position instead of casting at the unit.
	if bestDef, ok := getAbilityDef(bestID); ok && bestDef.TargetsPoint {
		s.beginAbilityCastAtPointLocked(unit, bestID, bestTgt.X, bestTgt.Y)
	} else {
		s.beginAbilityCastLocked(unit, bestID, bestTgt)
	}
	// one auto-cast initiation per unit per tick (single return path)
}

// abilityStatesLocked builds the per-ability snapshot slice for unit's
// owner-facing UnitSnapshot: each ability the unit has, with its live
// auto-cast toggle and cooldown. Skips ids with no registered AbilityDef.
// Returns nil for units with no abilities (omitempty drops the field).
//
// Caller holds s.mu.
func (s *GameState) abilityStatesLocked(unit *Unit) []protocol.AbilitySnapshot {
	if unit == nil || len(unit.Abilities) == 0 {
		return nil
	}
	out := make([]protocol.AbilitySnapshot, 0, len(unit.Abilities))
	for _, id := range unit.Abilities {
		def, ok := getAbilityDef(id)
		if !ok {
			continue
		}
		// Cooldown shown to the client = the ability's own cooldown, but with the
		// global cooldown folded in so EVERY castable ability shows a brief
		// clock-wipe during the ~1s GCD after any cast (otherwise a GCD-blocked
		// click looks like it does nothing). The own cooldown wins when it is
		// longer; otherwise the GCD drives a short shared wipe of length
		// abilityGlobalCooldownSeconds. Passives are never castable (and are
		// hidden from the castable row), so they don't take the GCD wipe.
		cdRemaining := unit.AbilityCooldowns[id]
		cdTotal := def.EffectiveCooldown()
		if !def.IsPassive() && unit.GlobalCooldownRemaining > cdRemaining {
			cdRemaining = unit.GlobalCooldownRemaining
			cdTotal = abilityGlobalCooldownSeconds
		}
		snap := protocol.AbilitySnapshot{
			ID:               def.ID,
			DisplayName:      def.DisplayName,
			Description:      def.EffectiveDescription(),
			Icon:             def.Icon,
			ManaCost:         def.ManaCost,
			TargetCount:      def.TargetCount,
			SupportsAutoCast: def.SupportsAutoCast,
			AutoCast:         unit.AutoCastEnabled[id],
			// EffectiveCooldown() matches what beginAbilityCastLocked arms, so
			// the client's wipe fraction (remaining / total) is consistent with
			// the actual decay. For Heal (cast=1, cd=0) this surfaces 1s of
			// visible cooldown that doubles as a "you're casting" indicator.
			// GCD is folded in above so it shows on every ability.
			CooldownRemaining: cdRemaining,
			CooldownTotal:     cdTotal,
			// Channeling is true when this ability is the unit's active channel.
			// The action bar uses this to render the "channeling in progress" state.
			Channeling: unit.ChannelAbilityID == id,
			// Passive / ability-slot metadata (arch-mage-spell-system): the client
			// hides passives from the castable row and renders ability-slot
			// abilities in their rank's perk cell.
			Passive:         def.IsPassive(),
			AbilitySlotRank: abilitySlotRankLocked(unit, id),
			Projectile:      def.Projectile,
			TargetsPoint:    def.TargetsPoint,
		}
		// chargeFireSpecFor (not a raw def.ChargeRequired read) so a converted
		// (SchemaVersion>=2) charge-fire ability still reports its real
		// threshold to the client — see spell_charge.go.
		if spec, ok := chargeFireSpecFor(def); ok {
			snap.ChargeCurrent = unit.ArcaneCharge
			snap.ChargeRequired = spec.ChargeRequired
		}
		out = append(out, snap)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ── Player-facing entry points (lock-acquiring; mirror AttackWithUnits) ───────

// RequestAbilityCast is the player-issued standard-cast entry point (action
// bar left-click). Validates the caster is owned by playerID, then runs the
// Part 8 cast lifecycle. Returns (false, reason) on failure so the WS layer
// can surface `reason` via protocol.NotificationMessage (same pattern as
// "Not enough resources" on train_unit_command).
func (s *GameState) RequestAbilityCast(playerID string, casterUnitID int, abilityID string, targetUnitID int, targetX, targetY float64) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := s.getUnitByIDLocked(casterUnitID)
	if caster == nil || caster.OwnerID != playerID {
		return false, castFailNotOwned
	}
	// Point-targeted abilities (arcane_orb) fire toward the clicked world
	// location; unit-targeted abilities resolve against the target unit.
	if def, ok := getAbilityDef(abilityID); ok && def.TargetsPoint {
		return s.beginAbilityCastAtPointLocked(caster, abilityID, targetX, targetY)
	}
	target := s.getUnitByIDLocked(targetUnitID)
	return s.beginAbilityCastLocked(caster, abilityID, target)
}

// ToggleAutoCast is the player-issued auto-cast toggle (action bar
// right-click). Validates ownership, then toggles. Returns the new enabled
// state and whether it changed (changed=false ⇒ silently no-op, e.g.
// right-clicking an ability that does not support auto-cast — the spec's
// "no effect"; the WS layer sends no notification in that case).
func (s *GameState) ToggleAutoCast(playerID string, unitID int, abilityID string) (enabled bool, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unit := s.getUnitByIDLocked(unitID)
	if unit == nil || unit.OwnerID != playerID {
		return false, false
	}
	return s.toggleAutoCastLocked(unit, abilityID)
}
