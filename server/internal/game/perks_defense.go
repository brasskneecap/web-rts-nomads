package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// SHIELD / HEAL / BUFF HELPERS
//
// These helpers centralize the unit-side state transitions that perks drive.
// Damage intake, heal application, and the list of "active buffs" advertised
// to the client all live here so the integration points from state.go and
// perks.go are one-liners.
//
// EXTENSION POINTS:
//   • applyUnitDamageLocked    — add new damage-intake reducers (armor-
//                                 like, reflective, etc.) before or after
//                                 the shield pool.
//   • healUnitLocked           — add new overheal routings (e.g. future
//                                 gold perks that convert overheal into
//                                 something other than shield).
//   • unitMaxShieldLocked      — aggregate max-shield from multiple perks
//                                 here if future perks also contribute.
//   • activeBuffIconsLocked    — return extra buff icon ids when new timed
//                                 or conditional states are added. Each id
//                                 must match an entry in action-icons.json.
//   • activeDebuffIconsLocked  — return extra debuff icon ids (raw icon ids,
//                                 not perk ids) for new negative status effects.
// ═════════════════════════════════════════════════════════════════════════════

// applyUnitDamageLocked applies post-armor damage to a unit, routing through
// flat perk reduction and then the shield pool. Returns the portion that
// actually reduced HP (flat-reduction and shield-absorbed amounts are NOT
// included). Callers should keep using their original `damage` value for XP
// banking, threat, on-hit reactions, etc. so internal reductions don't
// retroactively penalize attackers.
//
// Damage intake order:
//   1. Caller computes post-armor damage (applyArmorMitigation).
//   2. perkFlatDamageReductionLocked reduces it further (reinforced_armor).
//   3. Shield pool absorbs what remains.
//   4. HP takes what the shield didn't absorb.
//
// Called from every unit-damage intake site:
//   - state.go primary attack
//   - state.go building-on-unit attack
//   - perks.go savage_strikes bonus hit
//   - perks.go applyCleaveHitLocked
//   - perks.go applyWhirlwindHitLocked
//
// A damage intake that bypasses this helper will bypass flat reduction and
// shield — avoid it.
func (s *GameState) applyUnitDamageLocked(target *Unit, damage int) int {
	if target == nil || damage <= 0 {
		return 0
	}
	// Step 0: pain_share redirect — a nearby allied Vanguard with pain_share
	// absorbs redirectPercent of the incoming damage through its own mitigation
	// stack. Runs before mark amplification so the redirect acts on raw damage.
	damage = s.perkRedirectIncomingDamageLocked(target, damage)
	if damage == 0 {
		return 0
	}
	// Step 1: Challenger's Mark — amplify incoming damage before any reduction.
	// Applied first so the mark's bonus is maximised and all subsequent reduction
	// perks (flat reduction, shield) work against the amplified value.
	if target.PerkState.MarkedRemaining > 0 && target.PerkState.MarkedMultiplier > 0 {
		damage = maxInt(damage, int(math.Round(float64(damage)*(1.0+target.PerkState.MarkedMultiplier))))
	}
	// Step 2: Percentage damage reduction from Last Stand and Brace.
	// Applied after mark amplification, before flat reduction and shield.
	if mult := s.perkIncomingDamageMultiplierLocked(target); mult > 0 {
		damage = maxInt(0, int(math.Round(float64(damage)*(1.0-mult))))
		if damage == 0 {
			return 0
		}
	}
	// Step 3: Flat per-hit reduction from reinforced_armor (and future flat reducers).
	// Applied after caller's armor mitigation and percentage reduction, before the shield pool.
	// Tuning point: flatReduction in the reinforced_armor perk entry (catalog/perks/...).
	if reduction := s.perkFlatDamageReductionLocked(target); reduction > 0 {
		damage = maxInt(0, damage-reduction)
		if damage == 0 {
			return 0
		}
	}
	if target.Shield > 0 {
		if target.Shield >= damage {
			target.Shield -= damage
			return 0
		}
		damage -= target.Shield
		target.Shield = 0
	}
	target.HP -= damage
	// Clamp to 0 so HP is never stored as negative. Death detection in callers
	// uses HP <= 0, so 0 is the correct sentinel — not an arbitrary negative.
	if target.HP < 0 {
		target.HP = 0
	}
	return damage
}

// healUnitLocked adds `amount` HP to a unit, clamped to MaxHP. If the unit has
// blood_engine (gold berserker), any excess beyond MaxHP becomes shield up to
// the perk's configured cap. Safe to call with non-positive amounts.
//
// ADD NEW OVERHEAL ROUTINGS HERE (e.g. future perks that convert overheal
// into something other than shield).
func (s *GameState) healUnitLocked(unit *Unit, amount int) {
	if unit == nil || amount <= 0 || unit.HP <= 0 {
		return
	}
	missing := unit.MaxHP - unit.HP
	if amount <= missing {
		unit.HP += amount
		return
	}
	unit.HP = unit.MaxHP
	overheal := amount - missing
	maxShield := s.unitMaxShieldLocked(unit)
	if maxShield <= 0 || overheal <= 0 {
		return
	}
	unit.Shield = minInt(maxShield, unit.Shield+overheal)
}

// unitMaxShieldLocked returns the unit's current shield capacity, aggregated
// from all perks that contribute a shield pool. 0 for units with no such perk.
// ADD NEW SHIELD-GRANTING PERKS HERE.
func (s *GameState) unitMaxShieldLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "blood_engine":
			total += int(def.Config["maxShield"])
		case "bulwark":
			total += int(def.Config["maxShield"])
		// ── add cases for new shield-granting perks below this line ─────────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 11 — incoming damage multiplier (percentage reduction)
//
// perkIncomingDamageMultiplierLocked returns the total fractional damage
// reduction (0 = no reduction, 0.5 = take 50% less) granted by the target's
// perks. Applied in applyUnitDamageLocked BEFORE flat reduction and before
// the shield pool so all reduction stacks in a predictable order.
//
// Multiple reducers stack additively, clamped to 0.75 max to prevent
// invulnerability. Capping at 0.75 (not 1.0) is intentional: even a fully
// stacked defensive Vanguard should still be killable without requiring
// coordinated CC. Last Stand's defensive contribution is not here — it
// grants flat armor via perkBonusArmorLocked, which pre-mitigates damage
// before this hook runs.
//
// Handles: brace.
// ADD NEW PERCENTAGE-REDUCTION PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkIncomingDamageMultiplierLocked(target *Unit) float64 {
	if target == nil {
		return 0
	}
	total := 0.0
	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {

		case "brace":
			// Count visible enemies within the configured radius. Early-exit once
			// threshold is reached via countEnemiesInRangeLocked (shared with the
			// buff-icon scan in activeBuffIconsLocked).
			enemyThreshold := int(def.Config["enemyThreshold"])
			if s.countEnemiesInRangeLocked(target, def.Config["radius"], enemyThreshold) >= enemyThreshold {
				total += def.Config["damageReduction"]
			}

		case "steady_advance":
			// Active while the unit is Moving AND has a visible enemy whose
			// direction is within the configured alignment cone in front of the unit.
			// isAdvancingTowardEnemyLocked evaluates the dot-product of the unit's
			// velocity vector (toward next waypoint) with the vector to the nearest
			// visible enemy. Shared with activeBuffIconsLocked to avoid duplicating math.
			if target.Moving && s.isAdvancingTowardEnemyLocked(target) {
				total += def.Config["damageReduction"]
			}

		// ── add cases for new percentage-reduction perks below this line ─────
		}
	}

	// guardian_aura contribution — not in the perk loop above because the aura
	// is applied by OTHER units (the Vanguard owners), not by the target's own
	// perk list. The pre-built cache from rebuildGuardianAuraCacheLocked holds
	// the strongest aura DR for this unit as of the start of this tick.
	if aura, ok := s.guardianAuraCache[target.ID]; ok {
		total += aura
	}

	// Clamp to 0.75 — prevents any combination of stacked perks from making
	// the unit functionally invulnerable. Both Last Stand (0.30) and Brace
	// (0.20) together reach 0.50, well below the cap with room for a Gold perk.
	return clampFloat(total, 0, 0.75)
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 12 — outgoing damage debuff multiplier (attacker-side penalty)
//
// perkOutgoingDamageDebuffMultiplierLocked returns the fractional outgoing
// damage penalty currently on the unit (e.g. 0.30 = deal 30% less damage).
// Applied in tickUnitCombatLocked to the raw damage before armor mitigation.
//
// The debuff (WeakenedRemaining / WeakenedMultiplier) is stamped onto the
// attacker by Punishing Guard when the Vanguard takes a hit. It decays in the
// main Update loop (cross-unit, same pattern as TauntRemaining) regardless of
// whether the weakened unit itself owns any perks.
//
// Returns 0 when no debuff is active.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkOutgoingDamageDebuffMultiplierLocked(unit *Unit) float64 {
	if unit == nil || unit.PerkState.WeakenedRemaining <= 0 {
		return 0
	}
	return unit.PerkState.WeakenedMultiplier
}

// ═════════════════════════════════════════════════════════════════════════════
// VANGUARD PERK HOOKS
//
// These three functions implement the defender-side perk effects introduced
// for the Vanguard path. They are called from the damage pipeline and the
// rank-modifier application path.
//
// EXTENSION POINTS — adding more perks later:
//   • More Bronze Vanguard perks  → add entries to perk-defs.json under
//                                   units.soldier.paths.vanguard.bronze
//                                   and add cases to the relevant hook(s) below.
//   • Silver/Gold Vanguard perks  → add entries under vanguard.silver / .gold
//                                   in perk-defs.json, then add cases here as
//                                   needed. Same hooks apply.
//   • Perks for other unit types  → add the unit type under units.<type>.paths
//                                   in perk-defs.json and add cases here.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Hook 8 — on damage received (defender-side reactions)
//
// onPerkDamageTakenLocked is called after a unit takes damage from an attacker.
// `damage` is the post-armor value that was passed into the damage pipeline
// (i.e. what the attacker intended after armor, before flat reduction or shield).
//
// Called from:
//   - state.go tickUnitCombatLocked     — primary attack
//   - perks.go savage_strikes bonus hit — secondary hit
//   - perks.go applyCleaveHitLocked     — cleave secondary
//   - perks.go applyWhirlwindHitLocked  — whirlwind AoE
//
// ADD NEW DEFENDER-SIDE PERK REACTIONS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) onPerkDamageTakenLocked(target, attacker *Unit, damage int) {
	if target == nil || attacker == nil || damage <= 0 || len(target.PerkIDs) == 0 {
		return
	}
	// Skip reactions if the unit is already dead this tick.
	if target.HP <= 0 {
		return
	}

	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "retaliation":
			// Reflect damage equal to (armorPercent × this unit's armor) back to the
			// attacker on each hit. Higher-armor Vanguards punish attackers more.
			//
			// Guard: RetaliationActive prevents recursive reflection if the attacker
			// also has retaliation. The reflected hit goes through applyUnitDamageLocked
			// only — no XP, threat, or further perk hooks — keeping the chain flat.
			//
			// Tuning point: armorPercent in perk-defs.json → retaliation.config.
			if target.PerkState.RetaliationActive {
				continue // already inside a reflection; do not chain
			}
			if attacker.HP <= 0 || attacker.OwnerID == target.OwnerID {
				continue
			}
			// Use effective armor so conditional armor perks (last_stand) boost
			// reflected damage — a low-HP Vanguard with Retaliation punishes
			// attackers harder, which is the intended synergy.
			reflected := maxInt(0, int(math.Round(float64(s.effectiveArmorLocked(target))*def.Config["armorPercent"])))
			if reflected <= 0 {
				continue
			}
			// Set guard before the call so any path that re-enters this function
			// for this unit is a no-op.
			target.PerkState.RetaliationActive = true
			// Bypass the full damage pipeline — no XP, no threat, no further hooks.
			// This keeps reflected damage simple and prevents infinite chains.
			s.applyUnitDamageLocked(attacker, reflected)
			target.PerkState.RetaliationActive = false

		case "punishing_guard":
			// Stamp a weakened debuff on the attacker: they deal reduced outgoing
			// damage for durationSeconds. Refreshes on every hit so persistent
			// attackers remain debuffed.
			// The debuff lives on the attacker's PerkState and decays in Update().
			if attacker.HP > 0 {
				attacker.PerkState.WeakenedMultiplier = def.Config["weakenedMultiplier"]
				attacker.PerkState.WeakenedRemaining = def.Config["durationSeconds"]
			}

		// ── add cases for new defender-side reactions below this line ────────
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 9 — flat per-hit damage reduction query (defender-side)
//
// perkFlatDamageReductionLocked returns the total flat damage reduction the
// target gets from its perks, applied per hit after armor mitigation and before
// the shield pool. Returns 0 for units with no relevant perk.
//
// Called from applyUnitDamageLocked — covers all damage sources automatically.
//
// ADD NEW FLAT-DAMAGE-REDUCTION PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkFlatDamageReductionLocked(target *Unit) int {
	if target == nil || len(target.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "reinforced_armor":
			// Tuning point: flatReduction in perk-defs.json → reinforced_armor.config.
			total += int(def.Config["flatReduction"])
		// ── add cases for new flat-reduction perks below this line ───────────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 10b — bonus armor (defender-side stat modifier, conditional or passive)
//
// perkBonusArmorLocked returns the total flat armor bonus the unit currently
// has from its perks. Stacked additively on top of unit.Armor via
// effectiveArmorLocked.
//
// Unlike perkFlatMaxHPBonusLocked this is NOT baked into unit.Armor via
// applyRankModifiersLocked — the bonus can be conditional (last_stand fires
// only below an HP threshold) and needs to react live. Reading effective armor
// through the helper means the bonus automatically flows into:
//   - every applyArmorMitigation call (primary combat, savage_strikes,
//     cleave, whirlwind)
//   - retaliation reflection (synergy: more armor → more reflected damage)
//   - UnitSnapshot.Armor for HUD display
//
// Handles: last_stand (active only below hpThresholdPercent).
// ADD NEW FLAT-ARMOR PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkBonusArmorLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "last_stand":
			// Bonus active only while HP is below the configured threshold.
			// Recomputed live so the bonus appears/disappears cleanly as HP
			// changes without requiring state updates.
			if unit.MaxHP > 0 {
				hpFrac := float64(unit.HP) / float64(unit.MaxHP)
				if hpFrac <= def.Config["hpThresholdPercent"] {
					total += int(def.Config["bonusArmor"])
				}
			}

		case "interlock":
			// Flat armor bonus while any allied (same OwnerID), visible, alive unit
			// is within the configured radius. O(N) per call — same cost as brace.
			// hasAllyInRangeLocked is shared with activeBuffIconsLocked to avoid
			// duplicating the scan loop.
			if s.hasAllyInRangeLocked(unit, def.Config["radius"]) {
				total += int(def.Config["bonusArmor"])
			}

		// ── add cases for new flat-armor perks below this line ───────────────
		}
	}
	return total
}

// effectiveArmorLocked returns unit.Armor plus any live perk bonus. Use this
// everywhere armor is read for damage mitigation, damage reflection, and HUD
// display so conditional armor perks (last_stand) propagate consistently.
func (s *GameState) effectiveArmorLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	return unit.Armor + s.perkBonusArmorLocked(unit) + s.perkBonusArmorFromBannersLocked(unit)
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 10 — flat max HP bonus query (passive stat modifier)
//
// perkFlatMaxHPBonusLocked returns the total flat max HP bonus granted by the
// unit's perks. Applied additively on top of rank × path multipliers inside
// applyRankModifiersLocked (progression.go) so it is always included when stats
// are recalculated. Returns 0 for units with no relevant perk.
//
// Called from progression.go applyRankModifiersLocked.
//
// ADD NEW FLAT-MAX-HP PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkFlatMaxHPBonusLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "hold_the_line":
			// Tuning point: bonusMaxHP in perk-defs.json → hold_the_line.config.
			total += int(def.Config["bonusMaxHP"])
		// ── add cases for new flat max HP perks below this line ──────────────
		}
	}
	return total
}
