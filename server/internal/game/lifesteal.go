package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// Lifesteal — the first genuinely NEW base-authorable stat (stat_modifiers.go).
//
// It exercises the full "unit carries a base value for any registered stat"
// path end to end: a unit type authors a base lifesteal fraction on
// UnitDef.BaseStats, perks/statuses/zone-auras add to it via the shared
// PerkStatModifier vocabulary, and the effective value is read at a combat
// chokepoint through effectiveStatLocked — no bespoke per-unit field, no
// hand-rolled aggregation. Adding the NEXT such stat (thorns, …) is the same
// three lines: a registry entry, a statBaseAuthorable entry, and a read site.
// ═════════════════════════════════════════════════════════════════════════════

// applyLifestealLocked heals the attacking unit for its effective lifesteal
// fraction of the damage that just landed. Called from the single canonical
// HP-loss point (applyUnitDamageWithSourceLocked, perks_defense.go) as a peer
// of fireOnDamageDealtLocked, so every unit-sourced hit — melee, projectile,
// pierce, ability, DoT tick, splash — grants lifesteal uniformly on the exact
// post-mitigation amount that reached HP.
//
// The lifesteal fraction is unitBaseStat(attacker, "lifesteal") (the unit's
// authored per-type base or 0) folded through effectiveStatLocked so owned
// perks, active statuses, and the owner's zone auras can add to it. The heal
// runs through healUnitLocked, so it respects the attacker's MaxHP cap and any
// healingReceived modifier on the attacker (a heal-reduction debuff cuts
// lifesteal too — consistent with every other heal).
//
// The overwhelming common case (no lifesteal anywhere) bails after the stage
// fold returns the identity: heal 0 → return. Self-damage never lifesteals
// (attacker == target is filtered by the caller only tracking a real attacker;
// a unit damaging itself has src.AttackerUnitID == its own id, so we guard it).
//
// Must be called under s.mu write lock.
func (s *GameState) applyLifestealLocked(target *Unit, damage int, src DamageSource) {
	if damage <= 0 || src.AttackerUnitID == 0 {
		return
	}
	if target != nil && src.AttackerUnitID == target.ID {
		return // a unit damaging itself does not lifesteal off its own HP
	}
	attacker := s.getUnitByIDLocked(src.AttackerUnitID)
	if attacker == nil || attacker.HP <= 0 {
		return
	}
	frac := s.effectiveStatLocked(attacker, unitBaseStat(attacker, statLifesteal), statLifesteal)
	if frac <= 0 {
		return
	}
	heal := int(math.Round(frac * float64(damage)))
	if heal <= 0 {
		return
	}
	s.healUnitLocked(attacker, heal)
}
