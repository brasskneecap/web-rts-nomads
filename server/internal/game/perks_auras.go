package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// AURA AND REDIRECT PERK INFRASTRUCTURE
//
// This file implements the damage-redirect hook for pain_share and the
// banner effect helpers for rallying_banner. guardian_aura's own per-tick
// cache used to live here (rebuildGuardianAuraCacheLocked / guardianAuraValue
// / perkBonusArmorFromAurasLocked / perkArmorPercentBonusFromAurasLocked) —
// it has been migrated to the generic, data-driven PerkDef.Auras vocabulary
// resolved by perk_aura_stat_cache.go's rebuildAuraStatCacheLocked (statArmor
// / statArmorPercent), the same engine zealous_march, mana_conduit, and
// sanctuary already use. See perks_defense.go's effectiveArmorLocked for the
// fold site and perk_aura_migration_test.go for the characterization proof
// against the frozen pre-migration algorithm (kept there as a test-only
// oracle).
//
// All functions must be called under s.mu (read or write) lock.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// pain_share — damage redirect hook
//
// perkRedirectIncomingDamageLocked scans for the nearest allied Vanguard with
// pain_share that is alive, has HP > 0, is within the configured radius of
// target, and is not currently absorbing a redirect (PainShareActive == false).
//
// If found, redirectPercent of incoming damage is redirected to that Vanguard,
// which absorbs it through its own full mitigation stack via a recursive call to
// applyUnitDamageLocked. The guard flag PainShareActive prevents Vanguard-to-
// Vanguard redirect loops — a Vanguard absorbing a redirect cannot itself be
// selected as the absorber for another Vanguard's redirect check during this
// same call stack.
//
// Returns the damage remaining for the original target (damage - redirected).
//
// Call site: step 0 of applyUnitDamageLocked, before mark amplification.
// Must be called under s.mu write lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkRedirectIncomingDamageLocked(target *Unit, damage int, src DamageSource) int {
	if damage <= 0 {
		return damage
	}

	def := perkDefByID("pain_share")
	if def == nil {
		return damage
	}

	radius := def.Config["radius"]
	radiusSq := radius * radius
	redirectPct := def.Config["redirectPercent"]

	// Find the nearest allied Vanguard with pain_share that is eligible.
	var best *Unit
	var bestDistSq float64

	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if u.ID == target.ID {
			continue
		}
		if u.OwnerID != target.OwnerID {
			continue
		}
		if !containsString(u.PerkIDs, "pain_share") {
			continue
		}
		if u.PerkState.PainShareActive {
			continue // currently absorbing a redirect; skip
		}
		dx := u.X - target.X
		dy := u.Y - target.Y
		dSq := dx*dx + dy*dy
		if dSq > radiusSq {
			continue
		}
		if best == nil || dSq < bestDistSq {
			best = u
			bestDistSq = dSq
		}
	}

	if best == nil {
		return damage
	}

	redirected := maxInt(1, int(math.Round(float64(damage)*redirectPct)))
	// Guard prevents this Vanguard from being re-selected as a redirect target
	// for any nested damage call triggered during the redirect absorption.
	best.PerkState.PainShareActive = true
	// Propagate the original attacker IDs so if the absorbing Vanguard dies,
	// the kill credits the unit/building/trap that triggered the hit — not the
	// unit being protected. Kind is overridden to "pain_share_redirect" for
	// telemetry clarity.
	//
	// SourceAbilityID is propagated too, deliberately: this is the SAME
	// damage instance as src, just redirected to a different victim — it is
	// still, transitively, that ability's damage. If the absorbing Vanguard
	// dies from it, an authored execute/cleave-style ability's on_unit_death
	// should fire for the Vanguard exactly as it would for the originally
	// intended victim. See DamageSource.SourceAbilityID's doc comment
	// (damage_pipeline.go) for the full argument and its contrast with
	// retaliation's reflected counter-hit, which is NOT propagated (a brand
	// new damage instance, not this one redirected).
	//
	// Category is propagated for the exact same reason: this redirect is not
	// a new perk-created damage instance (contrast savage_strikes/whirlwind/
	// cleave/retaliation, which ARE DamageCategoryPerk) — it is src's own
	// damage, fanned out to a different victim, so it keeps src's Category
	// (basic attack stays basic attack, an ability's hit stays that ability's
	// hit, etc.) rather than being hard-coded to Perk.
	redirectSrc := DamageSource{
		AttackerUnitID:     src.AttackerUnitID,
		AttackerBuildingID: src.AttackerBuildingID,
		AttackerTrapID:     src.AttackerTrapID,
		Kind:               "pain_share_redirect",
		Category:           src.Category,
		SourceAbilityID:    src.SourceAbilityID,
	}
	s.applyUnitDamageWithSourceLocked(best, redirected, redirectSrc)
	best.PerkState.PainShareActive = false

	return damage - redirected
}

// ─────────────────────────────────────────────────────────────────────────────
// rallying_banner — banner effect helpers
//
// perkBonusArmorFromBannersLocked returns the total flat armor bonus this unit
// receives from all active rallying banners planted by the same player.
// Contributions from multiple banners are summed — no cap per the spec.
//
// Called from effectiveArmorLocked alongside perkBonusArmorLocked.
// Must be called under s.mu (read or write) lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkBonusArmorFromBannersLocked(unit *Unit) int {
	if unit == nil || len(s.Banners) == 0 {
		return 0
	}
	total := 0
	for _, b := range s.Banners {
		if b.OwnerPlayerID != unit.OwnerID {
			continue
		}
		dx := unit.X - b.X
		dy := unit.Y - b.Y
		if dx*dx+dy*dy <= b.Radius*b.Radius {
			total += b.ArmorBonus
		}
	}
	return total
}

// sanctuary — ranged damage reduction aura — migrated to the generic,
// data-driven PerkDef.Auras vocabulary (perk_aura_stat_cache.go /
// statProjectileDamageReduction). See perks_defense.go's Step 3b for the
// fold site (the src.Kind == "projectile" gate lives there now, since the
// generic aura cache has no notion of damage kind) and
// perk_aura_migration_test.go for the characterization proof against the
// frozen pre-migration algorithm.

// perkAttackSpeedBonusFromBannersLocked returns the total attack-speed bonus
// this unit receives from all active rallying banners planted by the same player.
// Contributions from multiple banners are summed.
//
// Called from perkAttackSpeedBonusLocked's total.
// Must be called under s.mu (read or write) lock.
func (s *GameState) perkAttackSpeedBonusFromBannersLocked(unit *Unit) float64 {
	if unit == nil || len(s.Banners) == 0 {
		return 0
	}
	total := 0.0
	for _, b := range s.Banners {
		if b.OwnerPlayerID != unit.OwnerID {
			continue
		}
		dx := unit.X - b.X
		dy := unit.Y - b.Y
		if dx*dx+dy*dy <= b.Radius*b.Radius {
			total += b.AttackSpeedBonus
		}
	}
	return total
}
