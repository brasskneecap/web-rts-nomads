package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// CRIT SYSTEM
//
// Single, centralized crit primitive consumed by the combat pipeline. Two
// inputs feed it:
//
//   - perkCritChanceBonusLocked(attacker)   — chance bonus from the attacker's
//                                              perks (eagle_spirit, vulture_spirit,
//                                              bullseye)
//   - huntersMarkCritBonusLocked(target)    — chance bonus from the target's
//                                              active Hunter's Mark stacks
//   - perkCritMultiplierBonusLocked(attacker) — flat multiplier override from
//                                              the attacker (bullseye → 2.5×)
//
// Total crit chance is clamped to [0, 1]. Total crit multiplier is the
// global default plus the attacker's largest perk override (NOT additive —
// taking max keeps multiple crit-mult perks from compounding into absurd
// 3×+ values).
//
// ADD NEW CRIT-CONTRIBUTING PERKS TO THE FUNCTIONS BELOW.
//
// Crit ROLLING happens at fire time for projectiles (so the snapshotted
// damage already includes the crit) and at hit time for instant-resolve
// melee. Both paths call rollCritDamage which returns the multiplier (1.0
// on miss, > 1.0 on hit). For perk-driven secondary projectiles (split shot,
// pierce, double shot, explosive_tips) each shot rolls independently.
// Explosive_tips' AoE explosion does NOT roll crit — it's a splash effect,
// not an arrow hit.
// ═════════════════════════════════════════════════════════════════════════════

const (
	// defaultCritChance is the baseline crit probability that every unit has
	// before any perk modifiers. 5% — Marksman perks add on top of this so
	// e.g. eagle_spirit (+10%) on a marksman archer ends up at 15% effective
	// chance against an unmarked target.
	defaultCritChance = 0.05

	// defaultCritMultiplier is the damage multiplier on a normal crit. 2.0
	// (double damage). bullseye overrides this to 2.5 — see perkCritMultiplierBonusLocked.
	defaultCritMultiplier = 2.0
)

// perkCritChanceBonusLocked returns the attacker's total flat crit-chance
// bonus from their own perks. Stacks additively. Hunter's Mark contribution
// is target-dependent and lives in huntersMarkCritBonusLocked.
//
// ADD NEW SELF-PERK CRIT-CHANCE SOURCES HERE.
func (s *GameState) perkCritChanceBonusLocked(attacker *Unit) float64 {
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
		case "eagle_spirit":
			total += def.Config["critChanceBonus"]
		case "bullseye":
			total += def.Config["critChanceBonus"]

		// vulture_spirit's crit-chance bonus is now data-driven — see
		// PerkDef.StatModifiers (catalog/perks/marksman/vulture_spirit) —
		// and folds in below via unitPerkStatModifiersLocked instead of a
		// case arm here.
		}
	}

	// Zone-aura crit chance AND data-driven perk stat modifiers (e.g.
	// vulture_spirit's StatModifier{Stat:"critChance", Op:"add"}). Folded the
	// same way as attackSpeed's total (perkAttackSpeedBonusLocked): the
	// caller adds this to defaultCritChance, so we compose against that same
	// constant baseline (there is no per-unit base field for crit chance) and
	// return the bonus that, added to defaultCritChance, reproduces the
	// canonical applyStatStages result. No active aura and no perk
	// StatModifiers on this stat ⇒ returns `total` unchanged.
	add, mul := s.playerStatModifierLocked(attacker.OwnerID, statCritChance)
	perkStages := s.unitPerkStatModifiersLocked(attacker, statCritChance)
	if add != 0 || mul != 1 || len(perkStages) > 0 {
		effective := applyStatStages(defaultCritChance+total, mergeZoneIntoBaseStage(perkStages, add, mul))
		return effective - defaultCritChance
	}

	return total
}

// perkCritMultiplierBonusLocked returns the attacker's largest crit-multiplier
// override, or 0 if none of the owned perks override the default. The largest
// (rather than sum) is used so multiple crit-mult perks don't compound into
// runaway numbers — design intent is "your strongest crit perk wins".
//
// ADD NEW CRIT-MULTIPLIER OVERRIDES HERE.
func (s *GameState) perkCritMultiplierBonusLocked(attacker *Unit) float64 {
	if attacker == nil {
		return 0
	}
	best := 0.0
	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "bullseye":
			// bullseye sets the floor at this value (e.g. 2.5). Stored as the
			// full multiplier, NOT a delta on top of default.
			if v := def.Config["critMultiplier"]; v > best {
				best = v
			}
		}
	}

	// Zone-aura crit multiplier. Composed against defaultCritMultiplier (the
	// same baseline unitCritMultiplierLocked starts from) via the canonical
	// (base + add) × mul rule, then folded into the existing "largest wins"
	// rule so it can only raise the multiplier — never stack with a perk
	// override. No active aura ⇒ (0, 1) ⇒ effective == defaultCritMultiplier,
	// which never beats a real perk override and leaves `best` unchanged.
	add, mul := s.playerStatModifierLocked(attacker.OwnerID, statCritMult)
	if add != 0 || mul != 1 {
		if effective := (defaultCritMultiplier + add) * mul; effective > best {
			best = effective
		}
	}

	return best
}

// huntersMarkCritBonusLocked computes the crit-chance bonus the attacker
// gets against a target carrying Hunter's Mark stacks. Diminishing returns
// per stack (first = full, every additional = additionalStackBonus). Reads
// the perk def directly so tuning is centralized in JSON.
//
// Bonus applies to ANY attacker — the user spec calls out "team crit support
// through Hunter's Mark", so allies of the marker also benefit.
func (s *GameState) huntersMarkCritBonusLocked(target *Unit) float64 {
	if target == nil {
		return 0
	}
	stacks := target.PerkState.huntersMarkCount()
	if stacks == 0 {
		return 0
	}
	def := perkDefByID("hunters_mark")
	if def == nil {
		return 0
	}
	base := def.Config["critChanceBonus"]
	additional := def.Config["additionalStackBonus"]
	if base <= 0 {
		return 0
	}
	// First stack = base; every additional stack = additional.
	// Example tuning: base=0.15, additional=0.10 → 1=0.15, 2=0.25, 3=0.35.
	return base + float64(stacks-1)*additional
}

// unitCritChanceLocked returns the total probability of a crit on this
// attacker→target pair, clamped to [0, 1]. Combines defaultCritChance,
// attacker perk bonus, and target's Hunter's Mark stacks.
func (s *GameState) unitCritChanceLocked(attacker, target *Unit) float64 {
	chance := defaultCritChance + s.perkCritChanceBonusLocked(attacker)
	if target != nil {
		chance += s.huntersMarkCritBonusLocked(target)
	}
	if chance < 0 {
		return 0
	}
	if chance > 1 {
		return 1
	}
	return chance
}

// unitCritMultiplierLocked returns the multiplier applied to damage on a
// successful crit. defaultCritMultiplier (2.0) unless an attacker perk
// overrides it. Always >= 1.
func (s *GameState) unitCritMultiplierLocked(attacker *Unit) float64 {
	mult := defaultCritMultiplier
	if override := s.perkCritMultiplierBonusLocked(attacker); override > mult {
		mult = override
	}
	return math.Max(1.0, mult)
}

// rollCritDamage rolls the attacker's crit chance against target and
// returns the damage multiplier to apply: 1.0 on a non-crit, >1.0 on a
// crit. Single seam for the combat pipeline so call sites stay terse.
//
// Uses the perks-RNG stream so determinism / replay parity is preserved.
func (s *GameState) rollCritDamage(attacker, target *Unit) float64 {
	chance := s.unitCritChanceLocked(attacker, target)
	if chance <= 0 {
		return 1.0
	}
	if s.rngPerks.Float64() < chance {
		return s.unitCritMultiplierLocked(attacker)
	}
	return 1.0
}
