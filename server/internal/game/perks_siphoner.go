package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// SIPHONER PERKS
//
// This file owns the runtime helpers for every Siphoner perk (Bronze today;
// Silver / Gold land here when authored). Per the per-path file convention
// documented in perks.go, the lightweight Bronze cases live as switch arms
// in shared hook files (perks.go's tickUnitPerkStateLocked dispatch) but
// their bodies — and any helper state machines — live here.
//
// BRONZE PERKS
//
//   soul_leech       — read-only damage/heal multiplier inside the Siphon Life
//                      channel tick. Stateless; the helper just aggregates
//                      perk config for the caster.
//   withering_beam   — stamped debuff on the channel target. Caster-side
//                      accumulator (continuous siphon seconds) lives on the
//                      caster's PerkState; target-side stacks live on the
//                      target's PerkState and decay in the state.go cross-unit
//                      loop alongside WeakenedRemaining.
//   lingering_hex    — autonomous AoE slow that fires on its own cadence
//                      (no player click). Driven by tickLingeringHexPerk-
//                      Locked from tickUnitPerkStateLocked. When the perk
//                      cooldown reaches 0 AND the Siphoner has a valid
//                      anchor enemy AND enough mana, the AoE stamps every
//                      enemy within `radius` of the anchor. Pattern mirrors
//                      tickTrapPlacementLocked (trapper traps).
//   mark_of_weakness — autonomous AoE armor + healing-received debuff,
//                      same firing pattern as Lingering Hex.
//
// EXTENSION POINTS — adding more Siphoner perks later:
//   • Silver/Gold perks  → add entries under
//                          catalog/units/human/acolyte/paths/siphoner/perks/silver.json
//                          and .../gold.json. Reuse the affliction PerkState
//                          fields where possible and put helpers in this file.
// ═════════════════════════════════════════════════════════════════════════════

// siphonLifeModifiersForCasterLocked returns the (damageMultiplier,
// healingMultiplier) the channeling Siphoner currently applies to every
// Siphon Life tick. Defaults are 1.0/1.0; Soul Leech multiplies in
// additively-per-perk-instance (a single Siphoner can only own one Bronze
// perk today, so multiplication is identical to selection, but keeping the
// loop pattern means future Silver/Gold tuning that also scales the channel
// composes cleanly).
//
// Safe to call on a nil caster (returns 1.0/1.0).
//
// Caller holds s.mu (read or write).
func (s *GameState) siphonLifeModifiersForCasterLocked(caster *Unit) (damageMult, healMult float64) {
	damageMult = 1.0
	healMult = 1.0
	if caster == nil {
		return
	}
	for _, perkID := range caster.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "soul_leech":
			cfg := def.ConfigForRank(caster.Rank)
			if m := cfg["damageMultiplier"]; m > 0 {
				damageMult *= m
			}
			if m := cfg["healingMultiplier"]; m > 0 {
				healMult *= m
			}
		}
	}
	return
}

// tickWitheringBeamChannelLocked advances the caster's continuous-siphon
// accumulator and stamps a Withering Beam stack on the current target every
// `secondsPerStack` of contact. Called once per Siphon Life channel tick
// from tickUnitChannelLocked, AFTER damage has been applied (so a tick that
// kills the target doesn't also stamp a stack on a corpse).
//
// Behaviour:
//   - If the caster doesn't own withering_beam, the helper is a no-op so the
//     check is one map lookup in the common case.
//   - If the channel target changed since last call, the accumulator resets
//     to 0 and starts counting fresh against the new target. Stacks already
//     on the previous target keep decaying naturally on its PerkState.
//   - If the target is invalid (nil / dead / same team), no stack lands —
//     the channel tick guard will stop the channel on the next iteration.
//
// Caller holds s.mu write lock.
func (s *GameState) tickWitheringBeamChannelLocked(caster, target *Unit, dt float64) {
	if caster == nil || target == nil {
		return
	}
	def := perkDefByID("withering_beam")
	if def == nil {
		return
	}
	owns := false
	for _, perkID := range caster.PerkIDs {
		if perkID == "withering_beam" {
			owns = true
			break
		}
	}
	if !owns {
		return
	}

	// Target swap detection: stacks on the previous target keep decaying
	// where they live; we just zero the caster-side accumulator so contact
	// time on the new target starts at 0.
	if caster.PerkState.WitheringBeamChannelTargetID != target.ID {
		caster.PerkState.WitheringBeamChannelTargetID = target.ID
		caster.PerkState.WitheringBeamChannelAccum = 0
	}

	caster.PerkState.WitheringBeamChannelAccum += dt
	cfg := def.ConfigForRank(caster.Rank)
	secondsPerStack := cfg["secondsPerStack"]
	if secondsPerStack <= 0 {
		return
	}
	maxStacks := int(cfg["maxStacks"])
	if maxStacks <= 0 {
		return
	}
	reductionPerStack := cfg["damageReductionPerStack"]
	lingerSeconds := cfg["lingerSeconds"]
	if lingerSeconds <= 0 {
		lingerSeconds = 1.5
	}

	// Apply as many stacks as the accumulator has accrued this tick. A
	// pathological dt could trigger multiple stacks in one call — the cap
	// at maxStacks prevents runaway. Each stack landing also refreshes the
	// shared lingerSeconds timer so the affliction stays sticky as long as
	// the beam is in contact.
	for caster.PerkState.WitheringBeamChannelAccum >= secondsPerStack {
		caster.PerkState.WitheringBeamChannelAccum -= secondsPerStack
		if target.PerkState.WitheringBeamStacks < maxStacks {
			target.PerkState.WitheringBeamStacks++
		}
		// Carry the per-stack reduction with the affliction so re-tuning
		// the perk later doesn't retroactively change live debuffs.
		target.PerkState.WitheringBeamReductionPerStack = reductionPerStack
		// Refresh the shared linger timer (no max() needed — re-stamping
		// during continuous contact simply keeps it pinned at full).
		target.PerkState.WitheringBeamRemaining = lingerSeconds
	}
}

// clearWitheringBeamCasterStateLocked zeroes the caster-side accumulator and
// tracking-target when a channel ends. Called from clearChannelStateLocked.
// No-op for non-Siphoner units (zero-valued fields are already a safe
// resting state).
//
// Caller holds s.mu write lock.
func (s *GameState) clearWitheringBeamCasterStateLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.PerkState.WitheringBeamChannelTargetID = 0
	unit.PerkState.WitheringBeamChannelAccum = 0
}

// witheringBeamDamageDebuffMultiplierLocked returns the fractional outgoing-
// damage reduction the unit currently suffers from Withering Beam stacks.
// 0 when the affliction is not active.
//
// Composed additively with WeakenedMultiplier (Punishing Guard) inside
// perkOutgoingDamageDebuffMultiplierLocked — the two sources stack but the
// final total is capped at 1.0 (cannot reduce damage below 0).
//
// Caller holds s.mu (read or write).
func witheringBeamDamageDebuffMultiplierLocked(unit *Unit) float64 {
	if unit == nil || unit.PerkState.WitheringBeamRemaining <= 0 || unit.PerkState.WitheringBeamStacks <= 0 {
		return 0
	}
	return float64(unit.PerkState.WitheringBeamStacks) * unit.PerkState.WitheringBeamReductionPerStack
}

// ─────────────────────────────────────────────────────────────────────────────
// Lingering Hex / Mark of Weakness — autonomous AoE affliction perks
//
// Both Bronze affliction perks fire automatically, on their own cadence:
//   1. tickUnitPerkStateLocked dispatches to the perk's tick handler once
//      per unit per tick.
//   2. The handler decays its per-unit cooldown timer. When the cooldown
//      reaches 0 the perk is "armed" and waits for a fight: a valid enemy
//      anchor must exist within `castRange`. The Siphoner's current Siphon
//      Life channel target is preferred as the anchor (clean synergy with
//      the channel); otherwise the nearest hostile in range is used.
//   3. Once armed and an anchor exists, the perk pays `manaCost`, stamps
//      the AoE on every enemy within `radius` of the anchor, and resets
//      its cooldown to `cooldownSeconds`. This mirrors the trapper's
//      tickTrapPlacementLocked pattern (cooldown gate + presence gate +
//      instant fire).
//   4. The fired effect is instant — no cast time, no projectile, no
//      animation lock. The Siphoner keeps doing whatever it was doing
//      (channeling Siphon Life, walking, etc.); the perk just layers an
//      AoE debuff on top. This is the "override siphon_life" pattern in
//      practice: the perk takes precedence for the unit's automatic
//      action slot whenever it is ready, then steps aside until the
//      next cooldown.
//
// Stamp semantics: refresh-longer for duration, refresh-stronger for the
// numerical magnitudes — matches the existing prayer / aegis / weakened
// cross-unit pattern. A second pulse on the same enemy never weakens an
// existing stronger debuff.
// ─────────────────────────────────────────────────────────────────────────────

// tickLingeringHexPerkLocked is the per-tick autonomous driver for the
// Lingering Hex Bronze perk. Decays the per-unit cooldown; when ready and
// the Siphoner has a valid anchor enemy in range + enough mana, fires the
// AoE stamp via applyLingeringHexAoELocked.
//
// Called from tickUnitPerkStateLocked.
// Caller holds s.mu write lock.
func (s *GameState) tickLingeringHexPerkLocked(unit *Unit, def *PerkDef, dt float64) {
	if unit == nil || def == nil {
		return
	}
	if unit.PerkState.LingeringHexCooldownRemaining > 0 {
		unit.PerkState.LingeringHexCooldownRemaining = math.Max(0, unit.PerkState.LingeringHexCooldownRemaining-dt)
	}
	if unit.HP <= 0 {
		return
	}
	if unit.PerkState.LingeringHexCooldownRemaining > 0 {
		return
	}
	cfg := def.ConfigForRank(unit.Rank)
	manaCost := int(math.Round(cfg["manaCost"]))
	if manaCost > 0 && unit.CurrentMana < manaCost {
		return // wait for mana — do NOT consume the cooldown so the perk re-checks
	}
	anchor := s.siphonerAfflictionAnchorLocked(unit, cfg["castRange"])
	if anchor == nil {
		return
	}
	if manaCost > 0 && !s.spendUnitManaLocked(unit, manaCost) {
		return
	}
	s.applyLingeringHexAoELocked(unit, anchor)
	unit.PerkState.LingeringHexCooldownRemaining = cfg["cooldownSeconds"]
}

// tickMarkOfWeaknessPerkLocked is the per-tick autonomous driver for the
// Mark of Weakness Bronze perk. Same shape as tickLingeringHexPerkLocked.
//
// Caller holds s.mu write lock.
func (s *GameState) tickMarkOfWeaknessPerkLocked(unit *Unit, def *PerkDef, dt float64) {
	if unit == nil || def == nil {
		return
	}
	if unit.PerkState.MarkOfWeaknessCooldownRemaining > 0 {
		unit.PerkState.MarkOfWeaknessCooldownRemaining = math.Max(0, unit.PerkState.MarkOfWeaknessCooldownRemaining-dt)
	}
	if unit.HP <= 0 {
		return
	}
	if unit.PerkState.MarkOfWeaknessCooldownRemaining > 0 {
		return
	}
	cfg := def.ConfigForRank(unit.Rank)
	manaCost := int(math.Round(cfg["manaCost"]))
	if manaCost > 0 && unit.CurrentMana < manaCost {
		return
	}
	anchor := s.siphonerAfflictionAnchorLocked(unit, cfg["castRange"])
	if anchor == nil {
		return
	}
	if manaCost > 0 && !s.spendUnitManaLocked(unit, manaCost) {
		return
	}
	s.applyMarkOfWeaknessAoELocked(unit, anchor)
	unit.PerkState.MarkOfWeaknessCooldownRemaining = cfg["cooldownSeconds"]
}

// siphonerAfflictionAnchorLocked picks the enemy unit a Siphoner Bronze
// affliction perk should center its AoE on. Selection order:
//
//  1. The Siphoner's current Siphon Life channel target, if it is still a
//     legal hostile and within castRange. This gives the clean "the unit
//     I'm draining is also the one I curse" synergy with no extra player
//     input.
//  2. Otherwise the closest visible hostile inside castRange. Iterates
//     s.Units (slice → deterministic order); ties broken by ascending
//     unit.ID.
//
// Returns nil when no eligible enemy exists, so the perk holds fire.
//
// Caller holds s.mu (read or write).
func (s *GameState) siphonerAfflictionAnchorLocked(unit *Unit, castRange float64) *Unit {
	if unit == nil || castRange <= 0 {
		return nil
	}
	rangeSq := castRange * castRange
	// Prefer current channel target.
	if unit.ChannelTargetID != 0 {
		if t := s.getUnitByIDLocked(unit.ChannelTargetID); t != nil && t.HP > 0 && t.Visible &&
			s.playersAreHostileLocked(unit.OwnerID, t.OwnerID) {
			dx := t.X - unit.X
			dy := t.Y - unit.Y
			if dx*dx+dy*dy <= rangeSq {
				return t
			}
		}
	}
	// Fall back to closest hostile in range.
	var best *Unit
	var bestSq float64
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(unit.OwnerID, u.OwnerID) {
			continue
		}
		dx := u.X - unit.X
		dy := u.Y - unit.Y
		d2 := dx*dx + dy*dy
		if d2 > rangeSq {
			continue
		}
		if best == nil || d2 < bestSq || (d2 == bestSq && u.ID < best.ID) {
			best = u
			bestSq = d2
		}
	}
	return best
}

// applyLingeringHexAoELocked finds every visible hostile within the perk's
// configured radius of the anchor's position and stamps Lingering Hex onto
// each. The anchor itself is included.
func (s *GameState) applyLingeringHexAoELocked(caster, anchor *Unit) {
	perkDef := perkDefByID("lingering_hex")
	if perkDef == nil || caster == nil || anchor == nil {
		return
	}
	cfg := perkDef.ConfigForRank(caster.Rank)
	radius := cfg["radius"]
	duration := cfg["durationSeconds"]
	moveMult := cfg["moveSpeedMultiplier"]
	atkMult := cfg["attackSpeedMultiplier"]
	if radius <= 0 || duration <= 0 {
		return
	}
	radiusSq := radius * radius
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(caster.OwnerID, u.OwnerID) {
			continue
		}
		dx := u.X - anchor.X
		dy := u.Y - anchor.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Placeholder visual: queue a shadowburst per-victim (mirrors the
		// trap-detonation cadence in trap.go). TODO(siphoner-fx): replace
		// "shadowburst" with a dedicated hex effect when one is authored.
		s.queueEffectLocked("shadowburst", u.ID, u.X, u.Y, 1.0, 0.6, "")
		// Refresh-longer for duration; refresh-stronger (= lower multiplier)
		// for both slows so an existing harsher hex isn't softened by a
		// re-cast. A multiplier of 0 means "field not set" so we only
		// compare when both sides are populated.
		if duration > u.PerkState.LingeringHexRemaining {
			u.PerkState.LingeringHexRemaining = duration
		}
		if moveMult > 0 && (u.PerkState.LingeringHexMoveMult == 0 || moveMult < u.PerkState.LingeringHexMoveMult) {
			u.PerkState.LingeringHexMoveMult = moveMult
		}
		if atkMult > 0 && (u.PerkState.LingeringHexAttackSpeedMult == 0 || atkMult < u.PerkState.LingeringHexAttackSpeedMult) {
			u.PerkState.LingeringHexAttackSpeedMult = atkMult
		}
	}
}

// applyMarkOfWeaknessAoELocked finds every visible hostile within the
// perk's configured radius of the anchor's position and stamps Mark of
// Weakness onto each. ArmorReduction is integer; HealingReceivedMult is a
// fraction < 1 (e.g. 0.7 = 30% less incoming healing).
func (s *GameState) applyMarkOfWeaknessAoELocked(caster, anchor *Unit) {
	perkDef := perkDefByID("mark_of_weakness")
	if perkDef == nil || caster == nil || anchor == nil {
		return
	}
	cfg := perkDef.ConfigForRank(caster.Rank)
	radius := cfg["radius"]
	duration := cfg["durationSeconds"]
	armorReduction := int(math.Round(cfg["armorReduction"]))
	healMult := cfg["healingReceivedMultiplier"]
	if radius <= 0 || duration <= 0 {
		return
	}
	radiusSq := radius * radius
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(caster.OwnerID, u.OwnerID) {
			continue
		}
		dx := u.X - anchor.X
		dy := u.Y - anchor.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Placeholder visual: shadowburst on each marked victim. TODO
		// (siphoner-fx): swap to a dedicated mark effect when authored.
		s.queueEffectLocked("shadowburst", u.ID, u.X, u.Y, 1.0, 0.6, "")
		if duration > u.PerkState.MarkOfWeaknessRemaining {
			u.PerkState.MarkOfWeaknessRemaining = duration
		}
		if armorReduction > u.PerkState.MarkOfWeaknessArmorReduction {
			u.PerkState.MarkOfWeaknessArmorReduction = armorReduction
		}
		// Refresh-stronger for healing-received = lower multiplier wins.
		if healMult > 0 && (u.PerkState.MarkOfWeaknessHealingReceivedMult == 0 ||
			healMult < u.PerkState.MarkOfWeaknessHealingReceivedMult) {
			u.PerkState.MarkOfWeaknessHealingReceivedMult = healMult
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Stat-hook helpers used by the shared stat functions
// (effectiveArmorLocked, healUnitLocked, etc.)
// ─────────────────────────────────────────────────────────────────────────────

// markOfWeaknessArmorReductionLocked returns the flat armor amount the
// affliction is currently stripping from the unit. Read by effective-
// ArmorLocked. 0 when inactive.
func markOfWeaknessArmorReductionLocked(unit *Unit) int {
	if unit == nil || unit.PerkState.MarkOfWeaknessRemaining <= 0 {
		return 0
	}
	return unit.PerkState.MarkOfWeaknessArmorReduction
}

// markOfWeaknessHealingReceivedMultiplierLocked returns the multiplier
// applied to every incoming heal on this unit (e.g. 0.7 = take 70% of the
// authored heal amount). Returns 1.0 when inactive so callers can multiply
// unconditionally.
func markOfWeaknessHealingReceivedMultiplierLocked(unit *Unit) float64 {
	if unit == nil || unit.PerkState.MarkOfWeaknessRemaining <= 0 || unit.PerkState.MarkOfWeaknessHealingReceivedMult <= 0 {
		return 1.0
	}
	return unit.PerkState.MarkOfWeaknessHealingReceivedMult
}

// lingeringHexMoveSpeedFactorLocked returns the multiplicative move-speed
// factor from Lingering Hex (e.g. 0.75 = 75% of base move speed). Returns
// 1.0 when inactive. Applied alongside slowFactorLocked in the movement
// step so the two debuffs stack multiplicatively.
func lingeringHexMoveSpeedFactorLocked(unit *Unit) float64 {
	if unit == nil || unit.PerkState.LingeringHexRemaining <= 0 || unit.PerkState.LingeringHexMoveMult <= 0 {
		return 1.0
	}
	return unit.PerkState.LingeringHexMoveMult
}

// lingeringHexAttackSpeedFactorLocked returns the multiplicative attack-
// speed factor from Lingering Hex. Returns 1.0 when inactive. Applied to
// the unit's effective attack speed in the combat tick (see
// tickUnitCombatLocked attack-cooldown computation).
func lingeringHexAttackSpeedFactorLocked(unit *Unit) float64 {
	if unit == nil || unit.PerkState.LingeringHexRemaining <= 0 || unit.PerkState.LingeringHexAttackSpeedMult <= 0 {
		return 1.0
	}
	return unit.PerkState.LingeringHexAttackSpeedMult
}
