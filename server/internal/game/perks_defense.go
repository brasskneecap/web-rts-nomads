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

// applyUnitDamageWithSourceLocked is the canonical damage entry point.
// It runs the full damage pipeline (redirect → mark amplification → flat
// reduction → shield → HP) AND, if the target hits HP<=0, enqueues a
// pendingDeath with full kill attribution. Drained at end of tick by
// drainPendingDeathsLocked.
//
// Pass DamageSource{} (anonymous) only from legacy call sites that do their
// own kill bookkeeping — the drain will then handle removal only, not XP
// credit, and the existing manual bookkeeping at those call sites is preserved.
//
// Returns the damage that landed on HP (after all mitigation).
//
// Damage intake order:
//  1. Caller computes post-armor damage (applyArmorMitigation).
//  2. pain_share redirect — nearby Vanguard absorbs a portion; src propagated.
//  3. Challenger's Mark amplification.
//  4. perkFlatDamageReductionLocked (reinforced_armor).
//  5. Shield pool.
//  6. HP.
//  7. enqueueDeathLocked if HP <= 0.
func (s *GameState) applyUnitDamageWithSourceLocked(target *Unit, damage int, src DamageSource) int {
	if target == nil || damage <= 0 {
		return 0
	}
	// Preserve the pre-mitigation input for Shared Pain redistribution.
	origDamage := damage
	// Step 2: pain_share redirect — propagate attribution so if the absorbing
	// Vanguard dies, the kill credits the original attacker.
	damage = s.perkRedirectIncomingDamageLocked(target, damage, src)
	if damage == 0 {
		// Even at 0 landed damage, the intended hit should still fan out via
		// Shared Pain — the attack "hit" the marked enemy, it just got fully
		// redirected. Keep the semantic consistent with the other early-exit
		// path below.
		s.perkShareDamageToMarkedLocked(target, origDamage, src)
		return 0
	}
	// Step 3: Mark amplification.
	if totalMult := target.PerkState.totalMarkMultiplier(); totalMult > 0 {
		damage = maxInt(damage, int(math.Round(float64(damage)*(1.0+totalMult))))
	}
	// Step 3b: Sanctuary aura mitigation (projectile-only). max-wins, no-stack.
	// Applied after mark amplification and before flat reduction so sanctuary
	// reduces on top of any mark bonus — consistent with the design intent that
	// sanctuary protects its zone from incoming fire.
	if sanctuaryMult := s.perkRangedDamageMultiplierFromAurasLocked(target, src); sanctuaryMult < 1.0 {
		damage = maxInt(0, int(math.Round(float64(damage)*sanctuaryMult)))
		if damage == 0 {
			s.perkShareDamageToMarkedLocked(target, origDamage, src)
			return 0
		}
	}
	// Step 4: Flat per-hit reduction.
	if reduction := s.perkFlatDamageReductionLocked(target); reduction > 0 {
		damage = maxInt(0, damage-reduction)
		if damage == 0 {
			return 0
		}
	}
	// Steps 5 & 6: Shield pool then HP.
	if target.Shield > 0 {
		if target.Shield >= damage {
			target.Shield -= damage
			// Shared Pain fires even when the shield fully absorbed the hit.
			s.perkShareDamageToMarkedLocked(target, origDamage, src)
			return 0
		}
		damage -= target.Shield
		target.Shield = 0
	}
	prevHP := target.HP
	target.HP -= damage
	// Clamp to 0 so HP is never stored as negative.
	if target.HP < 0 {
		target.HP = 0
	}
	// Overkill: damage exceeded what was on HP. The client derives its floating
	// damage numbers from HP-diffs, which clamp to prevHP for the killing blow.
	// Record the pre-clamp value so the client can show the real damage instead
	// of the capped "5 / 5" amount. Exact kills (damage == prevHP) are skipped —
	// the client's HP-delta is already correct in that case.
	if prevHP > 0 && damage > prevHP {
		s.recordLethalDamageLocked(target, damage)
	}
	// Shared Pain: redistribute a fraction of the pre-mitigation damage to
	// other marked enemies. Propagate attribution so indirect kills credit the
	// original attacker.
	s.perkShareDamageToMarkedLocked(target, origDamage, src)
	// Step 7: Enqueue death so drainPendingDeathsLocked handles cleanup and XP.
	s.enqueueDeathLocked(target, src)
	return damage
}

// applyUnitDamageLocked is the legacy wrapper around applyUnitDamageWithSourceLocked
// with an anonymous DamageSource. Call sites that have not been migrated to pass
// attribution should use this; the drain still catches HP=0 units they miss
// (defensive safety net) but does not award XP for them — those call sites
// continue to do their own kill bookkeeping as before.
//
// Damage intake order:
//   1. Caller computes post-armor damage (applyArmorMitigation). Armor
//      mitigation accounts for all flat and percent armor bonuses from perks
//      (last_stand, interlock, brace, guardian_aura, banners)
//      via effectiveArmorLocked. This means armor already reduces damage before
//      we enter this function.
//   2. pain_share redirect — nearby allied Vanguard absorbs a portion of raw damage.
//   3. Challenger's Mark amplification — amplifies after armor reduction and
//      after the redirect, so the mark bonus applies to whatever survives both
//      of those stages. NOTE: mark is therefore relatively stronger against
//      already-armored targets than it was under the old percentage-DR system.
//      This is intentional — see design approval in commit history.
//   4. perkFlatDamageReductionLocked (reinforced_armor) — per-hit flat reduction.
//   5. Shield pool absorbs what remains.
//   6. HP takes what the shield didn't absorb.
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
	return s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{})
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
		// ── add cases for new shield-granting perks below this line ─────────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 11 — percent armor bonus (self-perk, fractional)
//
// perkArmorPercentBonusLocked returns the total fractional armor bonus from
// this unit's own perks (e.g. 0.20 = +20% of base armor). Used in
// effectiveArmorLocked. Percents stack additively.
//
// Currently empty — guardian_aura's percent bonus flows through the aura cache
// via perkArmorPercentBonusFromAurasLocked. This hook exists for symmetry and
// as the future home for any self-perk percent-armor sources.
//
// ADD NEW SELF-PERK PERCENT-ARMOR SOURCES HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkArmorPercentBonusLocked(unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	total := 0.0
	// No self-perk percent-armor sources yet.
	// ── add cases for new self-perk percent-armor perks below this line ──────
	_ = total
	return 0
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
			if attacker.HP <= 0 || !s.playersAreHostileLocked(attacker.OwnerID, target.OwnerID) {
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
			// Route through the attributed helper so if the attacker dies from
			// reflected damage, the drain handles kill bookkeeping (XP to target,
			// trackBattleKillLocked) and removeUnitLocked. The manual
			// trackBattleKillLocked below is replaced by the drain.
			s.applyUnitDamageWithSourceLocked(attacker, reflected, DamageSource{
				AttackerUnitID: target.ID,
				Kind:           "retaliation",
			})
			target.PerkState.RetaliationActive = false
			// Debug: reflected damage counts under the defender's unit bucket.
			s.trackBattleDamageLocked(battleSourceFromUnit(target), attacker, reflected)

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
			// Bonus active only during the timed window opened by an HP dip
			// below threshold (see last_stand tick in perks.go). Reads the
			// decaying LastStandRemaining timer directly so the bonus
			// disappears cleanly when the window expires — independent of
			// current HP, so heals during the window keep the armor up.
			if unit.PerkState.LastStandRemaining > 0 {
				total += int(def.Config["bonusArmor"])
			}

		case "interlock":
			// Flat armor bonus while any allied (same OwnerID), visible, alive unit
			// is within the configured radius. O(N) per call — same cost as brace.
			// hasAllyInRangeLocked is shared with activeBuffIconsLocked to avoid
			// duplicating the scan loop.
			if s.hasAllyInRangeLocked(unit, def.Config["radius"]) {
				total += int(def.Config["bonusArmor"])
			}

		case "brace":
			// Flat armor bonus when surrounded by at least enemyThreshold visible
			// enemies within the configured radius. Moved from the old percentage-
			// DR hook — now contributes pre-mitigation armor instead.
			enemyThreshold := int(def.Config["enemyThreshold"])
			if s.countEnemiesInRangeLocked(unit, def.Config["radius"], enemyThreshold) >= enemyThreshold {
				total += int(def.Config["bonusArmor"])
			}

		// ── add cases for new flat-armor perks below this line ───────────────
		}
	}
	return total
}

// effectiveArmorLocked returns the unit's total effective armor including all
// flat and percent bonuses from perks and banners. Use this everywhere armor is
// read for damage mitigation, damage reflection, and HUD display so conditional
// armor perks (last_stand, brace, guardian_aura) propagate consistently.
//
// Formula:
//
//	effectiveArmor = floor(unit.Armor × (1 + percentBonus)) + flatBonus
//
// Where:
//   - flatBonus    = perkBonusArmorLocked + perkBonusArmorFromBannersLocked + perkBonusArmorFromAurasLocked
//   - percentBonus = perkArmorPercentBonusLocked + perkArmorPercentBonusFromAurasLocked
//
// Percent bonuses stack additively: two sources of +20% = +40% of base armor.
// This means high-armor units benefit more from percent bonuses, which is the
// intended design (guardian_aura scales with the unit's invested armor stat).
func (s *GameState) effectiveArmorLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	flatBonus := s.perkBonusArmorLocked(unit) +
		s.perkBonusArmorFromBannersLocked(unit) +
		s.perkBonusArmorFromAurasLocked(unit)
	percentBonus := s.perkArmorPercentBonusLocked(unit) +
		s.perkArmorPercentBonusFromAurasLocked(unit)
	return int(math.Floor(float64(unit.Armor)*(1.0+percentBonus))) + flatBonus
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
