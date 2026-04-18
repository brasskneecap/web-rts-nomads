package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// AURA AND REDIRECT PERK INFRASTRUCTURE
//
// This file implements the per-tick aura cache for guardian_aura, the
// damage-redirect hook for pain_share, and the banner effect helpers for
// rallying_banner.
//
// All functions must be called under s.mu (read or write) lock.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// guardian_aura — per-tick cache rebuild
//
// rebuildGuardianAuraCacheLocked rebuilds s.guardianAuraCache from scratch
// each tick using a three-phase algorithm designed for determinism:
//
// Phase 1 — Snapshot:
//
//	Iterate s.Units in slice order; collect each alive, visible unit that owns
//	guardian_aura as an aura source. Record base radius, base DR, and bonus
//	config values. No writes to guardianAuraCache in this phase.
//
// Phase 2 — Companion counting:
//
//	For each source, count OTHER sources with the same OwnerID whose position
//	is within THIS source's BASE radius (dist² ≤ baseR²). This is the critical
//	rule: companion detection always uses baseR, never the effective radius,
//	to prevent recursive radius inflation. Compute effR and effDR from the
//	count and store on the snapshot entry. Phase 2 reads only from Phase 1
//	snapshot data — never from other sources' in-progress effR/effDR.
//
// Phase 3 — Fan-out:
//
//	Iterate sources again (slice order). For each source, iterate s.Units and
//	set cache[allyID] = max(cache[allyID], effDR) for every allied, alive,
//	visible unit (excluding the owner) within effR². Using max ensures a unit
//	covered by multiple allied auras benefits from the strongest one, not the
//	sum (summation happens via the additive total in perkIncomingDamageMultiplierLocked).
//
// Determinism guarantee: slice-order iteration + max() are both stable and
// commutative. The cache produces bitwise-identical output for identical
// s.Units state.
//
// Must be called under s.mu write lock. Called from Update(dt) before combat.
// ─────────────────────────────────────────────────────────────────────────────
type auraSource struct {
	unitID  int
	ownerID string
	x, y    float64
	baseR   float64
	baseDR  float64
	rBonus  float64
	drBonus float64
	effR    float64
	effDR   float64
}

func (s *GameState) rebuildGuardianAuraCacheLocked() {
	// Clear the cache; reuse the map allocation if the server is still running.
	for k := range s.guardianAuraCache {
		delete(s.guardianAuraCache, k)
	}

	def := perkDefByID("guardian_aura")
	if def == nil {
		return
	}

	// Phase 1 — Snapshot all alive, visible units with guardian_aura.
	var sources []auraSource
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !containsString(u.PerkIDs, "guardian_aura") {
			continue
		}
		sources = append(sources, auraSource{
			unitID:  u.ID,
			ownerID: u.OwnerID,
			x:       u.X,
			y:       u.Y,
			baseR:   def.Config["radius"],
			baseDR:  def.Config["damageReduction"],
			rBonus:  def.Config["synergyRadiusBonus"],
			drBonus: def.Config["synergyDRBonus"],
		})
	}

	if len(sources) == 0 {
		return
	}

	// Phase 2 — Count companions within BASE radius; compute effR and effDR.
	// A companion is another source with the SAME ownerID within baseR² of
	// THIS source's position. We read only from the Phase 1 snapshot (baseR,
	// not any effR computed earlier in this loop) to prevent recursive feedback.
	for i := range sources {
		companions := 0
		baseRSq := sources[i].baseR * sources[i].baseR
		for j := range sources {
			if i == j {
				continue
			}
			if sources[j].ownerID != sources[i].ownerID {
				continue
			}
			dx := sources[j].x - sources[i].x
			dy := sources[j].y - sources[i].y
			if dx*dx+dy*dy <= baseRSq {
				companions++
			}
		}
		sources[i].effR = sources[i].baseR + float64(companions)*sources[i].rBonus
		sources[i].effDR = sources[i].baseDR + float64(companions)*sources[i].drBonus
	}

	// Phase 3 — Fan-out: for each source, mark allied units within effR².
	// Owner is excluded (owner does NOT benefit from their own aura).
	for i := range sources {
		effRSq := sources[i].effR * sources[i].effR
		for _, u := range s.Units {
			if u == nil || u.HP <= 0 || !u.Visible {
				continue
			}
			if u.ID == sources[i].unitID {
				continue // owner excluded from own aura
			}
			if u.OwnerID != sources[i].ownerID {
				continue // enemies excluded
			}
			dx := u.X - sources[i].x
			dy := u.Y - sources[i].y
			if dx*dx+dy*dy > effRSq {
				continue
			}
			// max: ally covered by multiple auras gets the strongest single DR.
			if existing, ok := s.guardianAuraCache[u.ID]; !ok || sources[i].effDR > existing {
				s.guardianAuraCache[u.ID] = sources[i].effDR
			}
		}
	}
}

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
func (s *GameState) perkRedirectIncomingDamageLocked(target *Unit, damage int) int {
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
	s.applyUnitDamageLocked(best, redirected)
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
