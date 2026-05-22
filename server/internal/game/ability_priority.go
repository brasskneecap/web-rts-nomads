package game

// ability_priority.go — Phase-2 category-driven autocast priority scoring.
//
// scoreAutoCastCandidateLocked scores one (ability, resolved-target) candidate
// that has ALREADY passed every autocast gate (enabled, SupportsAutoCast, off
// cooldown, mana-affordable, a non-nil selector target). tickUnitAutoCastLocked
// gathers candidates, scores each here, and casts the highest (deterministic
// tiebreak: slot index, then id). A score below minActivationScore means
// "don't bother" → the unit casts nothing and its basic attack proceeds.
//
// Determinism / AI_RULES: reads only live unit/world state and the ordered
// s.Units slice; no map iteration, no wall-clock, no RNG; never persists a
// *Unit (target is a within-tick value). Caller holds s.mu.
//
// NO-REGRESSION INVARIANT (load-bearing): every category — and the
// empty/unknown fallback — returns a per-category BASE that is strictly
// greater than minActivationScore for ANY candidate that has a valid target,
// before any situational bonus. So with a single autocast ability (e.g. an
// un-promoted heal-only Acolyte) "highest-scored-ready" collapses to
// exactly the prior "first-ready" behaviour: the lone candidate always clears
// the floor and is cast on exactly the ticks it would have been before. Only
// buff_ally / summon may dip to 0 (return "not worth it"), which is the
// intended design ("~0 otherwise") and never applies to heal/offensive.

// minActivationScore is the floor a candidate's score must strictly exceed to
// be cast. Kept just above zero so a "not useful" buff_ally/summon (which
// return 0) is correctly skipped, while every valid-target heal/offensive/
// fallback candidate (base ≥ candidateBaseScore = 1.0) clears it.
//
// Placeholder pending the balance pass (proposal Non-Goal): the EXACT value is
// un-tuned; only the invariant "0 ≤ minActivationScore < candidateBaseScore"
// is load-bearing and asserted by tests.
const minActivationScore = 0.05

// candidateBaseScore is the per-category floor added for simply having a valid
// target. Being ≥ 1.0 (≫ minActivationScore) is what makes the no-regression
// invariant hold by construction for heal/offensive and the fallback.
const candidateBaseScore = 1.0

// healMissingHPFullScale is the HP-fraction missing at which the heal bonus
// saturates. 1.0 ⇒ the bonus scales linearly with any missing HP (the heal
// selector already gates "below 100%"). Placeholder tunable.
const healMissingHPFullScale = 1.0

// abilityCategoryWeights are the situational-bonus multipliers, keyed by
// AbilityCategory. They scale ONLY the variable term on top of
// candidateBaseScore, so tuning them cannot break the no-regression invariant.
// Deliberately a small Go table (not on CombatProfile); JSON-tunable later.
// All values are placeholders pending the balance pass.
var abilityCategoryWeights = map[AbilityCategory]float64{
	AbilityCategoryHeal:      4.0,
	AbilityCategoryOffensive: 3.0,
	AbilityCategoryBuffAlly:  3.0,
	AbilityCategorySummon:    3.0,
}

// scoreAutoCastCandidateLocked returns the priority score for casting def at
// target. Caller holds s.mu; target is the non-nil within-tick unit the
// ability's selector resolved.
func (s *GameState) scoreAutoCastCandidateLocked(unit *Unit, def AbilityDef, target *Unit) float64 {
	if unit == nil || target == nil {
		return 0
	}
	switch def.Category {
	case AbilityCategoryHeal:
		return s.scoreHealCandidateLocked(unit, def, target)
	case AbilityCategoryOffensive:
		return s.scoreOffensiveCandidateLocked(unit, def, target)
	case AbilityCategoryBuffAlly:
		return s.scoreBuffAllyCandidateLocked(unit, def, target)
	case AbilityCategorySummon:
		return s.scoreSummonCandidateLocked(unit, def, target)
	default:
		// Empty / unregistered category: a defined conservative fallback that
		// still clears minActivationScore for a lone valid-target candidate, so
		// an uncategorised autocast ability behaves exactly as first-ready did.
		return candidateBaseScore
	}
}

// scoreHealCandidateLocked: base + weight·(missing-HP fraction of the picked
// target) + a bounded bonus for additional same-team damaged allies in cast
// range. The selector already guarantees target is the lowest-HP% ally in
// range and below 100%, so the variable term is > 0; the base guarantees the
// no-regression floor regardless.
func (s *GameState) scoreHealCandidateLocked(unit *Unit, def AbilityDef, target *Unit) float64 {
	score := candidateBaseScore
	if target.MaxHP > 0 {
		hpFrac := float64(target.HP) / float64(target.MaxHP)
		missing := clamp01((1.0 - hpFrac) / healMissingHPFullScale)
		score += abilityCategoryWeights[AbilityCategoryHeal] * missing
	}
	// Bounded "many wounded allies" bonus: count other same-team units below
	// full HP within this ability's cast range (deterministic s.Units scan).
	wounded := 0
	for _, u := range s.Units {
		if u == nil || u == target || u.MaxHP <= 0 || u.HP <= 0 {
			continue
		}
		if u.HP >= u.MaxHP {
			continue
		}
		if !s.unitsFriendlyLocked(unit, u) || !def.WithinCastRange(unit, u) {
			continue
		}
		wounded++
		if wounded >= 3 {
			break // bound the bonus (and the scan work)
		}
	}
	score += 0.25 * float64(wounded)
	return score
}

// scoreOffensiveCandidateLocked: base + weight·(normalised target strategic
// value) + a finishing-potential term (lower target HP% ⇒ higher). Reuses the
// existing unitStrategicValue scorer for "how valuable is this kill".
func (s *GameState) scoreOffensiveCandidateLocked(unit *Unit, def AbilityDef, target *Unit) float64 {
	score := candidateBaseScore
	// unitStrategicValue is ~1..~6 in practice; normalise by 10 and clamp so a
	// high-value target meaningfully — but boundedly — raises the score.
	value := clamp01(s.unitStrategicValue(target) / 10.0)
	finishing := 0.0
	if target.MaxHP > 0 {
		finishing = clamp01(1.0 - float64(target.HP)/float64(target.MaxHP))
	}
	w := abilityCategoryWeights[AbilityCategoryOffensive]
	score += w * (0.7*value + 0.3*finishing)
	return score
}

// scoreBuffAllyCandidateLocked: high when the (friendly) target is engaged in
// combat, ~0 otherwise — per the design. Phase 2 authors no buff_ally ability,
// so this branch is exercised only by direct unit tests; precise per-buff
// "already has this buff" dedup is deferred to when such an ability exists
// (proposal Non-Goal). Returning 0 when not useful makes the loop skip it.
func (s *GameState) scoreBuffAllyCandidateLocked(unit *Unit, def AbilityDef, target *Unit) float64 {
	if target.HP <= 0 || !s.unitsFriendlyLocked(unit, target) {
		return 0
	}
	inCombat := target.AttackTargetID != 0 || target.AttackBuildingTargetID != ""
	if !inCombat {
		return 0 // ~0 ⇒ below minActivationScore ⇒ not cast
	}
	return candidateBaseScore + abilityCategoryWeights[AbilityCategoryBuffAlly]
}

// scoreSummonCandidateLocked: derived from the local force deficit around the
// caster (more nearby hostiles than same-team allies ⇒ summon). Target is self
// (the "self" selector). 0 when there is no deficit. Phase 2 authors no summon
// ability; exercised only by direct unit tests.
func (s *GameState) scoreSummonCandidateLocked(unit *Unit, def AbilityDef, target *Unit) float64 {
	const radius = 320.0
	radiusSq := radius * radius
	hostiles, allies := 0, 0
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 {
			continue
		}
		if distanceSquared(unit.X, unit.Y, u.X, u.Y) > radiusSq {
			continue
		}
		switch {
		case s.unitsHostileLocked(unit, u):
			hostiles++
		case u != unit && s.unitsFriendlyLocked(unit, u):
			allies++
		}
	}
	deficit := hostiles - allies
	if deficit <= 0 {
		return 0
	}
	// Bound the deficit's contribution so one chaotic fight can't dominate.
	d := float64(deficit)
	if d > 5 {
		d = 5
	}
	return candidateBaseScore + abilityCategoryWeights[AbilityCategorySummon]*(d/5.0)
}
