package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// Thorns — the defender-side base-authorable stat (the second genuinely-new
// one, sibling of lifesteal). The stat form of the retaliation perk: a fraction
// of an ATTACK's damage is reflected back at the attacker.
//
// Like lifesteal it exercises the whole base-stat path: a unit type authors a
// base thorns fraction on UnitDef.BaseStats, perks/statuses/zone-auras add to
// it via the shared PerkStatModifier vocabulary, and the effective value is
// read through effectiveStatLocked — no bespoke per-unit field.
// ═════════════════════════════════════════════════════════════════════════════

// applyThornsLocked reflects the defender's effective thorns fraction of an
// incoming attack's damage back at the attacker. Called from the attack-hit
// reaction hook (onPerkDamageTakenLocked, perks_defense.go) — so it fires on
// the same hits the retaliation perk does (basic attacks + cleave/whirlwind
// secondaries), NOT on ability/DoT/splash damage, matching retaliation's scope.
//
// The reflected damage goes through the shared, attributed damage pipeline
// (applyUnitDamageWithSourceLocked) crediting the defender, so armor mitigates
// it and a reflected kill is booked correctly. ThornsActive guards the (today
// impossible — reflected damage never re-enters this hook) recursive case,
// mirroring retaliation's RetaliationActive. Only hostile attackers are
// punished. The common case (no thorns) bails after the stage fold returns 0.
//
// Must be called under s.mu write lock.
func (s *GameState) applyThornsLocked(target, attacker *Unit, damage int) {
	if target == nil || attacker == nil || damage <= 0 {
		return
	}
	if target.PerkState.ThornsActive {
		return // already inside a thorns reflection; do not chain
	}
	if attacker.HP <= 0 || !s.playersAreHostileLocked(attacker.OwnerID, target.OwnerID) {
		return
	}
	frac := s.effectiveStatLocked(target, unitBaseStat(target, statThorns), statThorns)
	if frac <= 0 {
		return
	}
	reflected := int(math.Round(frac * float64(damage)))
	if reflected <= 0 {
		return
	}
	target.PerkState.ThornsActive = true
	s.applyUnitDamageWithSourceLocked(attacker, reflected, DamageSource{
		AttackerUnitID: target.ID,
		Kind:           "thorns",
		Category:       DamageCategoryPerk, // a defender-side reaction, like retaliation
	})
	target.PerkState.ThornsActive = false
	// Debug: reflected damage counts under the defender's unit bucket.
	s.trackBattleDamageLocked(battleSourceFromUnit(target), attacker, reflected)
}
