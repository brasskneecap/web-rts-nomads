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
	if len(unit.PerkIDs) == 0 {
		return 0
	}

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

		// ── add cases for new attack-speed perks below this line ────────────
		}

		// savage_strikes and cleaving_rage do not modify attack speed.
	}

	// Banner contribution — rallying_banner auras are placed by other units
	// (not necessarily by this unit), so they are not in the perk loop above.
	total += s.perkAttackSpeedBonusFromBannersLocked(unit)

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
// primaryDamage is the raw damage dealt by the triggering attack. Not consumed
// by current Bronze Berserker perks, but rename from _ when a future perk
// needs to scale off or react to the hit value.
func (s *GameState) onPerkAttackFiredLocked(attacker, primaryTarget *Unit, _ int, deadUnitIDs *[]int) {
	if attacker == nil || len(attacker.PerkIDs) == 0 {
		return
	}

	// Reset idle timer once per attack — shared across all the attacker's perks.
	attacker.PerkState.TimeSinceLastAttack = 0

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
						s.applyUnitDamageLocked(primaryTarget, actualDmg)
						s.onUnitDamagedLocked(attacker, primaryTarget, actualDmg)
						s.onPerkDamageTakenLocked(primaryTarget, attacker, actualDmg)
						s.recordDamageDealtLocked(attacker, primaryTarget, actualDmg)
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
			// While the whirlwind window is active, every attack also hits all
			// other enemies within the configured radius of the attacker.
			if attacker.PerkState.WhirlwindActiveRemaining > 0 {
				s.applyWhirlwindHitLocked(attacker, primaryTarget, def.Config["radius"], deadUnitIDs)
			}

		case "taunting_strike":
			// On proc, apply a taunt to the primary target for a short duration.
			// The taunted enemy strongly prefers targeting this Vanguard while the
			// taunt is active. Falls off naturally via decayThreatLocked in combat_ai.go.
			// Proc chance and duration are tunable in perk-defs.json (tauntChance, tauntDurationSeconds).
			if primaryTarget != nil && s.rngPerks.Float64() < def.Config["tauntChance"] {
				s.ApplyTauntLocked(primaryTarget.ID, attacker.ID, def.Config["tauntDurationSeconds"])
			}

		case "challengers_mark":
			// Stamp a damage-amplification mark on the target. The mark increases
			// ALL incoming damage (from any source) by bonusMultiplier for
			// durationSeconds. Refreshed on every Vanguard attack.
			// The mark lives on the target's PerkState and decays in Update().
			if primaryTarget != nil && primaryTarget.HP > 0 {
				primaryTarget.PerkState.MarkedMultiplier = def.Config["bonusMultiplier"]
				primaryTarget.PerkState.MarkedRemaining = def.Config["durationSeconds"]
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
		if candidate.OwnerID == attacker.OwnerID {
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
		s.applyUnitDamageLocked(candidate, damage)
		s.onUnitDamagedLocked(attacker, candidate, damage)
		s.onPerkDamageTakenLocked(candidate, attacker, damage)
		s.recordDamageDealtLocked(attacker, candidate, damage)
		s.onPerkAttackDamageAppliedLocked(attacker, candidate, damage)
		if candidate.HP <= 0 {
			candidate.HP = 0
			s.awardKillXPLocked(attacker)
			s.payoutDamageDealtXPLocked(candidate)
			s.awardSoldierTankKillXPLocked(candidate.ID)
			s.onPerkKillLocked(attacker)
			*deadUnitIDs = append(*deadUnitIDs, candidate.ID)
		}
	}
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
		if candidate.OwnerID == attacker.OwnerID {
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
	s.applyUnitDamageLocked(secondary, damage)
	s.onUnitDamagedLocked(attacker, secondary, damage)
	s.onPerkDamageTakenLocked(secondary, attacker, damage)
	s.recordDamageDealtLocked(attacker, secondary, damage)
	// Let on-damage perks (blood_sustain) react to cleave hits.
	s.onPerkAttackDamageAppliedLocked(attacker, secondary, damage)
	if secondary.HP <= 0 {
		secondary.HP = 0
		s.awardKillXPLocked(attacker)
		s.payoutDamageDealtXPLocked(secondary)
		s.awardSoldierTankKillXPLocked(secondary.ID)
		*deadUnitIDs = append(*deadUnitIDs, secondary.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 4 of 4 — on kill
// ─────────────────────────────────────────────────────────────────────────────

// onPerkKillLocked is called immediately after a unit lands a killing blow on
// a target. Called alongside awardKillXPLocked in state.go.
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
	if attacker == nil || len(attacker.PerkIDs) == 0 {
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

		case "shield_bash":
			// RNG-proc stun + slow on the target. onPerkAttackDamageAppliedLocked
			// is only reached from the unit-vs-unit combat path (not building
			// targets) so no type-guard is needed here.
			// Slow starts immediately — duration = stunSeconds + slowSeconds —
			// so the slow is active from the moment of the proc even while the
			// stun is also running. This ensures the slow lands even if the
			// target later becomes stun-immune.
			if target.HP > 0 && s.rngPerks.Float64() < def.Config["procChance"] {
				stunSec := def.Config["stunSeconds"]
				slowSec := def.Config["slowSeconds"]
				s.ApplyStunLocked(target.ID, stunSec)
				s.ApplySlowLocked(target.ID, def.Config["slowMultiplier"], stunSec+slowSec)
			}

		// ── add cases for new on-hit reaction perks below this line ─────────
		}
	}
}
