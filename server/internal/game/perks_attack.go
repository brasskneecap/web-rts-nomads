package game

import "math"

// ─────────────────────────────────────────────────────────────────────────────
// Hook 2 of 4 — attack speed bonus query
// ─────────────────────────────────────────────────────────────────────────────

// perkAttackSpeedBonusLocked returns the current attack-speed bonus from the
// unit's perk. Recomputed fresh on every call so dynamic perks (frenzy_core)
// always reflect live game state. Returns 0 for units with no relevant perk.
//
// Used in state.go tickUnitCombatLocked():
//
//	effectiveSpeed := unit.AttackSpeed + s.perkAttackSpeedBonusLocked(unit)
//	unit.AttackCooldown = 1.0 / math.Max(0.1, effectiveSpeed)
//
// ADD NEW ATTACK-SPEED-MODIFYING PERKS HERE.
func (s *GameState) perkAttackSpeedBonusLocked(unit *Unit) float64 {
	total := 0.0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "bloodlust":
			total += unit.PerkState.BloodlustBonus

		case "frenzy_core":
			// Bonus scales linearly from 0 at full HP to maxBonus at 0 HP.
			if unit.MaxHP > 0 {
				hpFraction := clampFloat(float64(unit.HP)/float64(unit.MaxHP), 0, 1)
				total += (1.0 - hpFraction) * def.Config["maxBonus"]
			}

		case "relentless":
			total += unit.PerkState.RelentlessBonus

		case "berserk_state":
			// Passive: bonus active only while the unit's own HP is below the
			// threshold. Mirrors the damage multiplier above.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					total += def.Config["attackSpeedBonus"]
				}
			}

		case "momentum":
			// Post-attack burst. Shares MomentumRemaining with the move-speed
			// buff in perks_movement.go — both apply while the timer is live
			// and drop together when it decays to 0 in tickUnitPerkStateLocked.
			if unit.PerkState.MomentumRemaining > 0 {
				total += def.Config["attackSpeedBonus"]
			}

		case "hawk_spirit":
			// Marksman bronze passive — flat attack-speed bonus added to the
			// unit's effective attack speed. Stacks additively with any other
			// AS sources (banner auras, momentum, etc.).
			total += def.Config["attackSpeedBonus"]

		// ── add cases for new attack-speed perks below this line ────────────
		}

		// savage_strikes and cleaving_rage do not modify attack speed.
	}

	// Banner contribution — rallying_banner auras are placed by other units
	// (not necessarily by this unit), so they are not in the perk loop above.
	total += s.perkAttackSpeedBonusFromBannersLocked(unit)

	// Battle Prayer buff — cross-unit buff applied to the healed target by a
	// Cleric with battle_prayer. The buffed unit does not need to own the perk;
	// the multiplier lives on PerkState (same as WeakenedRemaining pattern).
	if unit.PerkState.BattlePrayerRemaining > 0 {
		total += unit.PerkState.BattlePrayerMultiplier
	}

	// Zone-aura attack speed. Every call site reads effective speed as
	// unit.AttackSpeed + perkAttackSpeedBonusLocked(unit), so to apply the
	// canonical (base + add) × mul rule we return the bonus that makes that sum
	// equal (unit.AttackSpeed + perkTotal + add) × mul. No active aura ⇒ (0, 1),
	// which returns `total` unchanged.
	add, mul := s.playerStatModifierLocked(unit.OwnerID, statAttackSpeed)
	if add != 0 || mul != 1 {
		effective := (unit.AttackSpeed + total + add) * mul
		return effective - unit.AttackSpeed
	}

	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 3 of 4 — on attack fired
// ─────────────────────────────────────────────────────────────────────────────

// onPerkAttackFiredLocked is called immediately after a unit fires a normal
// attack at a target unit, before the caller checks whether the target died.
//
// Rules for this hook:
//   - Reset TimeSinceLastAttack to 0 here (already done below for all perks).
//   - If a perk kills a SECONDARY target (e.g. cleaving_rage), append its ID
//     to *deadUnitIDs — the caller handles removal.
//   - If a perk deals extra damage to the PRIMARY target (e.g. savage_strikes),
//     do NOT append the primary to *deadUnitIDs — the caller checks target.HP <= 0
//     after this function returns and handles it there.
//
// ADD NEW ON-ATTACK PERKS HERE.
// primaryDamage is the raw damage dealt by the triggering attack. Used by
// Marksman split-shot to scale extra-shot damage proportionally; future
// perks that scale off the hit value can read it the same way.
func (s *GameState) onPerkAttackFiredLocked(attacker, primaryTarget *Unit, primaryDamage int, deadUnitIDs *[]int) {
	if attacker == nil || len(attacker.PerkIDs) == 0 {
		return
	}

	// Reset idle timer once per attack — shared across all the attacker's perks.
	attacker.PerkState.TimeSinceLastAttack = 0
	_ = primaryDamage // available for future hooks; Marksman fire-time effects
	// have moved to fireProjectileLocked for visible-projectile correctness.
	_ = deadUnitIDs

	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "bloodlust":
			// Accumulate attack-speed bonus, capped at maxBonus.
			attacker.PerkState.BloodlustBonus = math.Min(
				attacker.PerkState.BloodlustBonus+def.Config["bonusPerAttack"],
				def.Config["maxBonus"],
			)

		case "savage_strikes":
			attacker.PerkState.AttackCounter++
			n := int(def.Config["everyNthAttack"])
			if n > 0 && attacker.PerkState.AttackCounter >= n {
				attacker.PerkState.AttackCounter = 0
				// Fire the bonus hit only if the primary target survived the normal hit.
				if primaryTarget != nil && primaryTarget.HP > 0 {
					bonusDmg := maxInt(0, int(math.Round(float64(attacker.Damage)*def.Config["bonusMultiplier"])))
					actualDmg := applyArmorMitigation(bonusDmg, s.effectiveArmorLocked(primaryTarget))
					if actualDmg > 0 {
						s.applyUnitDamageWithSourceLocked(primaryTarget, actualDmg, DamageSource{AttackerUnitID: attacker.ID, Kind: "savage_strikes"})
						s.onUnitDamagedLocked(attacker, primaryTarget, actualDmg)
						s.onPerkDamageTakenLocked(primaryTarget, attacker, actualDmg)
						s.recordDamageDealtLocked(attacker, primaryTarget, actualDmg)
						s.trackBattleDamageLocked(battleSourceFromUnit(attacker), primaryTarget, actualDmg)
						// Let on-damage perks (blood_sustain) react to the extra hit.
						s.onPerkAttackDamageAppliedLocked(attacker, primaryTarget, actualDmg)
						// Primary target death is handled by the caller — do NOT append here.
					}
				}
			}

		case "cleaving_rage":
			s.applyCleaveHitLocked(attacker, primaryTarget, def.Config["splashRadius"], deadUnitIDs)

		case "momentum":
			// Refresh the post-attack move-speed buff. Overwrites any remaining
			// duration so consecutive attacks keep the buff at full value.
			attacker.PerkState.MomentumBonus = def.Config["moveSpeedBonus"]
			attacker.PerkState.MomentumRemaining = def.Config["durationSeconds"]

		case "whirlwind_core":
			// Each normal attack rolls against procChance. On proc, fire a bonus
			// AoE hit (full damage to every visible enemy within radius) and
			// queue the effect so the client overlays the spin animation.
			// The regular attack flow is NOT interrupted — this is strictly
			// additive; the primary target already took its hit before this
			// hook ran, and the unit's attack cooldown continues on its normal
			// cadence.
			if s.rngPerks.Float64() < def.Config["procChance"] {
				s.applyWhirlwindHitLocked(attacker, primaryTarget, def.Config["radius"], deadUnitIDs)
				s.applyPerkEffectLocked(def.Effect, attacker, primaryTarget)
			}

		case "challengers_mark":
			// Stamp a damage-amplification mark on the target. Keyed by the
			// attacker's unit id (prefixed "unit-" to keep trap ids and unit
			// ids in separate namespaces) so multiple Vanguards each land
			// their own stack; same-Vanguard re-attacks refresh in place.
			// Decays in state.go per-tick stack decay.
			if primaryTarget != nil && primaryTarget.HP > 0 {
				primaryTarget.PerkState.applyMarkStack(
					unitMarkSourceID(attacker.ID),
					attacker.ID,
					def.Config["bonusMultiplier"],
					def.Config["durationSeconds"],
				)
			}

		// ── add cases for new on-attack perks below this line ───────────────
		}
	}
}

// applyWhirlwindHitLocked deals full attacker damage to every visible enemy
// (other than primaryTarget) within radius of the attacker. Routes through the
// same shield/on-hit/XP pipeline as a normal attack so lifesteal, damage XP,
// and kill XP all work transparently.
func (s *GameState) applyWhirlwindHitLocked(attacker, primaryTarget *Unit, radius float64, deadUnitIDs *[]int) {
	if attacker == nil || radius <= 0 {
		return
	}
	radiusSq := radius * radius
	primaryID := 0
	if primaryTarget != nil {
		primaryID = primaryTarget.ID
	}
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == primaryID {
			continue
		}
		if !s.playersAreHostileLocked(candidate.OwnerID, attacker.OwnerID) {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - attacker.X
		dy := candidate.Y - attacker.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		damage := applyArmorMitigation(attacker.Damage, s.effectiveArmorLocked(candidate))
		if damage == 0 {
			continue
		}
		s.applyUnitDamageWithSourceLocked(candidate, damage, DamageSource{AttackerUnitID: attacker.ID, Kind: "whirlwind"})
		s.onUnitDamagedLocked(attacker, candidate, damage)
		s.onPerkDamageTakenLocked(candidate, attacker, damage)
		s.recordDamageDealtLocked(attacker, candidate, damage)
		s.trackBattleDamageLocked(battleSourceFromUnit(attacker), candidate, damage)
		s.onPerkAttackDamageAppliedLocked(attacker, candidate, damage)
		if candidate.HP <= 0 {
			candidate.HP = 0
			s.awardUnitDeathXPLocked(candidate, attacker)
			s.awardSoldierTankKillXPLocked(candidate.ID)
			s.onPerkKillLocked(attacker)
			s.trackBattleKillLocked(battleSourceFromUnit(attacker), candidate)
			*deadUnitIDs = append(*deadUnitIDs, candidate.ID)
		}
	}
}

// applyPerkEffectLocked queues a visual effect driven by a perk definition.
// Anchors to the attacker for target="self" (or ""), to primaryTarget for
// target="enemies". No-op when effect is nil, has no name, or the resolved
// anchor is missing. Must be called under s.mu write lock.
func (s *GameState) applyPerkEffectLocked(effect *PerkEffect, attacker, primaryTarget *Unit) {
	if effect == nil || effect.Name == "" {
		return
	}
	var anchor *Unit
	switch effect.Target {
	case "self", "":
		anchor = attacker
	case "enemies":
		anchor = primaryTarget
	default:
		return
	}
	if anchor == nil {
		return
	}
	s.queueEffectLocked(effect.Name, anchor.ID, anchor.X, anchor.Y, effect.SizeScale, effect.DurationSeconds, effect.Variant)
}

// applyCleaveHitLocked finds the nearest enemy within splashRadius of
// primaryTarget (excluding the primary itself) and applies full damage to it.
// Awards XP and appends to deadUnitIDs if the secondary target dies.
func (s *GameState) applyCleaveHitLocked(attacker, primaryTarget *Unit, splashRadius float64, deadUnitIDs *[]int) {
	if primaryTarget == nil {
		return
	}
	var secondary *Unit
	var secondaryDist float64
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == primaryTarget.ID {
			continue
		}
		if !s.playersAreHostileLocked(candidate.OwnerID, attacker.OwnerID) {
			continue // do not cleave friendlies
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - primaryTarget.X
		dy := candidate.Y - primaryTarget.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > splashRadius {
			continue
		}
		if secondary == nil || dist < secondaryDist {
			secondary = candidate
			secondaryDist = dist
		}
	}
	if secondary == nil {
		return
	}
	damage := applyArmorMitigation(attacker.Damage, s.effectiveArmorLocked(secondary))
	if damage == 0 {
		return
	}
	s.applyUnitDamageWithSourceLocked(secondary, damage, DamageSource{AttackerUnitID: attacker.ID, Kind: "cleave"})
	s.onUnitDamagedLocked(attacker, secondary, damage)
	s.onPerkDamageTakenLocked(secondary, attacker, damage)
	s.recordDamageDealtLocked(attacker, secondary, damage)
	s.trackBattleDamageLocked(battleSourceFromUnit(attacker), secondary, damage)
	// Let on-damage perks (blood_sustain) react to cleave hits.
	s.onPerkAttackDamageAppliedLocked(attacker, secondary, damage)
	if secondary.HP <= 0 {
		secondary.HP = 0
		s.awardUnitDeathXPLocked(secondary, attacker)
		s.awardSoldierTankKillXPLocked(secondary.ID)
		s.trackBattleKillLocked(battleSourceFromUnit(attacker), secondary)
		*deadUnitIDs = append(*deadUnitIDs, secondary.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 4 of 4 — on kill
// ─────────────────────────────────────────────────────────────────────────────

// onPerkKillLocked is called immediately after a unit lands a killing blow on
// a target. Called at each kill site alongside awardUnitDeathXPLocked (progression.go).
//
// ADD NEW ON-KILL PERKS HERE.
func (s *GameState) onPerkKillLocked(attacker *Unit) {
	if attacker == nil || len(attacker.PerkIDs) == 0 {
		return
	}

	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "relentless":
			// Grant the post-kill attack-speed burst; overwrites any remaining duration.
			attacker.PerkState.RelentlessBonus = def.Config["bonus"]
			attacker.PerkState.RelentlessRemaining = def.Config["durationSeconds"]

		// ── add cases for new on-kill perks below this line ─────────────────
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 5 — outgoing damage multiplier (pre-armor)
//
// perkBonusDamageMultiplierLocked returns an additive multiplier applied to
// the attacker's raw damage BEFORE armor mitigation, for attacks against the
// given target. 0 means "no bonus" (final damage = base damage).
//
// Used in state.go tickUnitCombatLocked() primary-attack damage calc:
//
//	raw := float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, target))
//	damage := applyArmorMitigation(int(math.Round(raw)), target.Armor)
//
// Scoped to the PRIMARY attack only — secondary perk hits (savage_strikes
// bonus, cleave) deliberately do not stack this bonus.
//
// ADD NEW OUTGOING-DAMAGE-MODIFYING PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
//
// Safe to call with a nil target (e.g. from Snapshot() when computing the
// effective damage to show in the HUD): target-dependent cases like
// executioner no-op, self-based cases like berserk_state still apply.
func (s *GameState) perkBonusDamageMultiplierLocked(attacker, target *Unit) float64 {
	if attacker == nil {
		return 0
	}

	total := 0.0
	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "executioner":
			// Bonus applies only when the target is below the HP threshold
			// at the time damage is dealt. No-op when called without a target.
			if target != nil && target.MaxHP > 0 {
				hpFraction := float64(target.HP) / float64(target.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					total += def.Config["bonusMultiplier"]
				}
			}

		case "berserk_state":
			// Passive: bonus active only while the attacker's own HP is below
			// the threshold. Recomputed live, so the bonus appears/disappears
			// cleanly as HP changes without requiring state updates.
			if attacker.MaxHP > 0 {
				hpFraction := float64(attacker.HP) / float64(attacker.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					total += def.Config["damageMultiplier"]
				}
			}

		case "hawk_spirit", "vulture_spirit":
			// Marksman bronze passives — flat outgoing damage bonus, target-
			// agnostic so it shows in the HUD via Snapshot()'s nil-target call.
			total += def.Config["damageMultiplier"]

		// ── add cases for new damage-multiplier perks below this line ───────
		}
	}

	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 6 — after damage applied (on-hit reactions)
//
// onPerkAttackDamageAppliedLocked is called whenever a perk-capable attack
// actually deals damage to a target, for every damage source that comes from
// the attacker's attack resolution:
//   - primary attack (state.go)
//   - savage_strikes bonus hit (perks.go onPerkAttackFiredLocked)
//   - cleaving_rage secondary (perks.go applyCleaveHitLocked)
//
// `damage` is the post-armor damage actually applied. Safe to call with 0 or
// negative damage — the hook early-outs in that case.
//
// ADD NEW ON-HIT REACTION PERKS (lifesteal, on-hit procs) HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) onPerkAttackDamageAppliedLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 || len(attacker.PerkIDs) == 0 {
		return
	}
	// Dead attackers don't heal (blood_sustain) — guards against weird edges
	// where a perk hits after the attacker has already died this tick.
	if attacker.HP <= 0 {
		return
	}

	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "blood_sustain":
			// Heal for a percentage of damage dealt. Routed through
			// healUnitLocked so blood_engine (gold) can convert overheal into
			// shield. No recursion risk — healing never triggers damage events.
			heal := int(math.Round(float64(damage) * def.Config["lifestealPercent"]))
			if heal > 0 {
				s.healUnitLocked(attacker, heal)
			}

		case "blood_engine":
			// Gold blood_engine now carries its own lifesteal so it's useful
			// without blood_sustain. Stacks additively when both are taken.
			heal := int(math.Round(float64(damage) * def.Config["lifestealPercent"]))
			if heal > 0 {
				s.healUnitLocked(attacker, heal)
			}

		case "shield_bash":
			// RNG-proc stun + slow + taunt on the target. onPerkAttackDamageAppliedLocked
			// is only reached from the unit-vs-unit combat path (not building
			// targets) so no type-guard is needed here.
			// Slow starts immediately — duration = stunSeconds + slowSeconds —
			// so the slow is active from the moment of the proc even while the
			// stun is also running. This ensures the slow lands even if the
			// target later becomes stun-immune.
			// Taunt is applied on the same single proc roll so all three
			// effects land together (merged from the former taunting_strike
			// bronze perk). Taunt falls off naturally via decayThreatLocked.
			if target.HP > 0 && s.rngPerks.Float64() < def.Config["procChance"] {
				stunSec := def.Config["stunSeconds"]
				slowSec := def.Config["slowSeconds"]
				s.ApplyStunLocked(target.ID, stunSec)
				s.ApplySlowLocked(target.ID, def.Config["slowMultiplier"], stunSec+slowSec)
				s.ApplyTauntLocked(target.ID, attacker.ID, def.Config["tauntDurationSeconds"])
			}

		// ── add cases for new on-hit reaction perks below this line ─────────
		}
	}

	// Marksman post-hit dispatch — explosive_tips fires its AoE here so it
	// runs after the primary hit's damage has already landed (and after any
	// other on-hit perk reactions like blood_sustain heal), keeping order
	// predictable and preventing the explosion from short-circuiting them.
	s.onMarksmanDamageAppliedLocked(attacker, target, damage)
}
