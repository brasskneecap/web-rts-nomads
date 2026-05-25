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

// ═════════════════════════════════════════════════════════════════════════════
// SIPHONER SILVER PERKS
//
// Four perks, all layering onto Siphon Life or the affliction pipeline:
//
//   chain_siphon     — secondary beams off the channel target. Stateless;
//                      targets resolved fresh each tick by chainSiphon-
//                      TargetsLocked and damaged via the canonical pipeline.
//                      Healing routes back through distributeSiphonHealLocked
//                      so dark_renewal can also catch chain overflow.
//
//   amplify_damage   — autonomous AoE affliction (same shape as
//                      lingering_hex / mark_of_weakness). Stamps a damage-
//                      taken multiplier on every nearby enemy. Read by the
//                      damage pipeline via amplifyDamageTakenMultiplier-
//                      Locked.
//
//   dark_renewal     — overheal-to-shield converter wired into distribute-
//                      SiphonHealLocked. Routes excess heal to a source-
//                      specific shield pool on the Siphoner (cap
//                      maxSelfShield), then to a nearby ally (cap
//                      maxAllyShield), then wastes the remainder per spec.
//                      Pools persist until depleted.
//
//   shared_suffering — echo damage from the Siphon Life primary tick to
//                      other nearby enemies that already carry a Siphoner
//                      affliction. Recursion-guarded via PerkState bool;
//                      echo damage is also tagged Kind="shared_suffering"
//                      for debug filtering.
//
// All four hook into existing systems rather than introducing new ones.
// chain_siphon and shared_suffering fire inside the channel tick (see
// tickUnitChannelLocked). amplify_damage uses the autonomous-AoE dispatch
// in tickUnitPerkStateLocked. dark_renewal injects into the Siphon Life
// heal distributor.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Chain Siphon
// ─────────────────────────────────────────────────────────────────────────────

// chainSiphonTargetsLocked picks up to `additionalTargetCount` valid chain
// targets for a Siphon Life beam centered on the primary target. Selection
// rules:
//
//   - Hostile, alive, visible.
//   - Not the primary target itself.
//   - Within chainRange of the primary target's position.
//   - Sorted by ascending distance from the primary target; ties broken by
//     ascending unit.ID for deterministic tick replay.
//
// Returns nil when the caster doesn't own chain_siphon, when the perk's
// config is malformed (additionalTargetCount <= 0 or chainRange <= 0), or
// when no chain target exists.
//
// Caller holds s.mu (read or write).
func (s *GameState) chainSiphonTargetsLocked(caster, primary *Unit) []*Unit {
	if caster == nil || primary == nil {
		return nil
	}
	def := perkDefByID("chain_siphon")
	if def == nil || !containsString(caster.PerkIDs, "chain_siphon") {
		return nil
	}
	cfg := def.ConfigForRank(caster.Rank)
	maxCount := int(cfg["additionalTargetCount"])
	chainRange := cfg["chainRange"]
	if maxCount <= 0 || chainRange <= 0 {
		return nil
	}
	rangeSq := chainRange * chainRange
	// Collect candidates with distance metadata so we can sort
	// deterministically before truncating to maxCount.
	var pool []chainSiphonCandidate
	for _, u := range s.Units {
		if u == nil || u.ID == primary.ID || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(caster.OwnerID, u.OwnerID) {
			continue
		}
		dx := u.X - primary.X
		dy := u.Y - primary.Y
		d2 := dx*dx + dy*dy
		if d2 > rangeSq {
			continue
		}
		pool = append(pool, chainSiphonCandidate{u: u, distSq: d2})
	}
	if len(pool) == 0 {
		return nil
	}
	// Stable sort: ascending distance, then ascending id.
	sortChainCandidatesByDistThenID(pool)
	if len(pool) > maxCount {
		pool = pool[:maxCount]
	}
	out := make([]*Unit, 0, len(pool))
	for _, c := range pool {
		out = append(out, c.u)
	}
	return out
}

// chainSiphonCandidate is the tuple held by chainSiphonTargetsLocked while it
// gathers and sorts chain targets. Named (rather than inlined as a struct
// literal at both the slice and the sort helper) so the sort helper can
// reference the same type without Go's strict structural-type rules
// rejecting the call.
type chainSiphonCandidate struct {
	u      *Unit
	distSq float64
}

// chainSiphonBeamVariant is the client renderer key for a Siphon Life
// secondary beam (primary target → chain target). Distinct from the
// primary "siphon_life" variant so the client can diverge the visual
// later (thinner, dimmer, different tint) without changing this code.
const chainSiphonBeamVariant = "chain_siphon"

// applyChainSiphonBeamsLocked fires the secondary chain beams for a single
// Siphon Life tick. Called once per channel tick from tickUnitChannelLocked,
// AFTER the primary damage has been applied so a tick that kills the primary
// still does NOT short-circuit the chain (per the design "chain beams
// continue to fire on this tick"). Each secondary beam:
//
//   - Spawns / updates a visual beam from the primary target's position to
//     the chain target's position via syncChainSiphonBeamsLocked. Beams
//     persist across ticks while their chain target stays in the selected
//     set, so the visual is stable (no 4-fps flicker at the channel
//     cadence). When a chain target falls out of range or dies, its beam is
//     removed on the next tick (or immediately via removeBeamForTarget-
//     Locked if it dies).
//   - Damages its target via applyUnitDamageWithSourceLocked using a
//     scaled-down per-tick damage (primaryDamage * secondaryDamageMultiplier).
//     Damage routes through the canonical pipeline so amplify_damage, mark
//     amplification, shields, etc. all flow naturally on chain victims.
//   - Generates secondary healing scaled by secondaryHealingMultiplier of
//     the original Siphon Life healing for this tick, then routes the
//     healing through distributeSiphonHealLocked. That keeps the
//     "self-first then ally / dark_renewal" semantic identical to the
//     primary beam.
//
// Chain beams do NOT recursively spawn more chain beams (no recursion
// guard is needed — chain_siphon is only invoked from this helper, which is
// only invoked from the channel tick, never from the damage pipeline).
//
// Caller holds s.mu write lock.
func (s *GameState) applyChainSiphonBeamsLocked(caster, primary *Unit, primaryDamage int, perTickHealing int, allyHealRadius float64, abilityID string) {
	if caster == nil || primary == nil {
		return
	}
	def := perkDefByID("chain_siphon")
	if def == nil || !containsString(caster.PerkIDs, "chain_siphon") {
		return
	}
	cfg := def.ConfigForRank(caster.Rank)
	dmgMult := cfg["secondaryDamageMultiplier"]
	healMult := cfg["secondaryHealingMultiplier"]
	chainTargets := s.chainSiphonTargetsLocked(caster, primary)

	// Sync beam visuals BEFORE damage so the visual lands even on a tick that
	// kills a chain target — removeBeamForTargetLocked then drops the
	// freshly-spawned beam at end of tick, but the visual was present for
	// the moment the killing tick happened. Always called (even when
	// chainTargets is empty) so beams from a previous tick whose targets
	// dropped out are cleaned up.
	s.syncChainSiphonBeamsLocked(caster, primary, chainTargets, abilityID)

	if primaryDamage <= 0 || len(chainTargets) == 0 {
		return
	}
	if dmgMult <= 0 && healMult <= 0 {
		return
	}
	secondaryDamage := int(math.Round(float64(primaryDamage) * dmgMult))
	secondaryHeal := int(math.Round(float64(perTickHealing) * healMult))
	for _, t := range chainTargets {
		if secondaryDamage > 0 {
			// Tag with Kind="chain_siphon" so telemetry / debug filters can
			// distinguish chain beams from the primary tick. Routes through the
			// canonical pipeline so amplify_damage, sanctuary, shields etc. all
			// apply on chain victims for free.
			s.applyUnitDamageWithSourceLocked(t, secondaryDamage, DamageSource{
				AttackerUnitID: caster.ID,
				Kind:           "chain_siphon",
				DamageType:     DamageShadow,
			})
		}
		// Heal generation runs per chain victim so two chain targets produce
		// twice the heal output — the secondary multiplier is per-beam, not
		// shared. Matches the "additionalTargetCount scales heal output"
		// design intent of a fan-out beam perk.
		if secondaryHeal > 0 {
			s.distributeSiphonHealLocked(caster, secondaryHeal, allyHealRadius)
		}
	}
}

// syncChainSiphonBeamsLocked diffs the currently-tracked chain beams on the
// caster against the freshly-selected chainTargets and reconciles:
//
//   - If the primary target ID has changed since the last tick, ALL tracked
//     chain beams are despawned (they emanated from a now-stale primary).
//     The map is then rebuilt below from scratch.
//   - For each previously-tracked chain target that is NOT in the new set:
//     remove its beam via removeBeamByIDLocked and delete the map entry.
//   - For each new chain target that is not yet tracked: spawn a beam with
//     caster = primary target, target = chain target, and record its ID.
//
// Reusing beams whose target is unchanged keeps the visual stable across
// ticks — only entry/exit transitions cause a respawn, not the 0.25s-cadence
// channel tick. Pass chainTargets = nil (or empty slice) to clear ALL
// tracked chain beams without resetting the primary-target id;
// clearChainSiphonBeamsLocked is the dedicated cleanup entry point used by
// clearChannelStateLocked when the channel ends.
//
// Caller holds s.mu write lock.
func (s *GameState) syncChainSiphonBeamsLocked(caster, primary *Unit, chainTargets []*Unit, abilityID string) {
	if caster == nil {
		return
	}
	// Primary-target swap: every tracked beam roots from a stale unit. Drop
	// them all so the rebuild below re-anchors against the new primary.
	if primary == nil || caster.PerkState.ChainSiphonPrimaryTargetID != primary.ID {
		for _, id := range caster.PerkState.ChainSiphonBeamIDs {
			s.removeBeamByIDLocked(id)
		}
		caster.PerkState.ChainSiphonBeamIDs = nil
		if primary == nil {
			caster.PerkState.ChainSiphonPrimaryTargetID = 0
			return
		}
		caster.PerkState.ChainSiphonPrimaryTargetID = primary.ID
	}

	// Build the new target-id set for diffing.
	newSet := make(map[int]struct{}, len(chainTargets))
	for _, t := range chainTargets {
		if t != nil {
			newSet[t.ID] = struct{}{}
		}
	}

	// Drop beams whose target is no longer in the chain set.
	for targetID, beamID := range caster.PerkState.ChainSiphonBeamIDs {
		if _, keep := newSet[targetID]; !keep {
			s.removeBeamByIDLocked(beamID)
			delete(caster.PerkState.ChainSiphonBeamIDs, targetID)
		}
	}

	// Spawn beams for newly-selected chain targets. Lazy-init the map only
	// when we actually need to add an entry so units without the perk pay
	// nothing for the field.
	for _, t := range chainTargets {
		if t == nil {
			continue
		}
		if _, exists := caster.PerkState.ChainSiphonBeamIDs[t.ID]; exists {
			continue
		}
		beam := s.spawnBeamLocked(primary, t, abilityID, chainSiphonBeamVariant)
		if caster.PerkState.ChainSiphonBeamIDs == nil {
			caster.PerkState.ChainSiphonBeamIDs = make(map[int]string, len(chainTargets))
		}
		caster.PerkState.ChainSiphonBeamIDs[t.ID] = beam.ID
	}
}

// clearChainSiphonBeamsLocked despawns every chain beam the unit currently
// owns and resets the bookkeeping fields. Called from clearChannelState-
// Locked so chain beams die alongside the primary channel — whether the
// channel ends naturally (target lost, mana depleted, order issued) or
// because the caster died.
//
// No-op for units that never owned chain_siphon (the map stays at its zero
// nil value); the primary-target id is still defensively reset so a unit
// that owned chain_siphon, channeled once, then lost the perk doesn't carry
// a stale ID forever.
//
// Caller holds s.mu write lock.
func (s *GameState) clearChainSiphonBeamsLocked(unit *Unit) {
	if unit == nil {
		return
	}
	for _, id := range unit.PerkState.ChainSiphonBeamIDs {
		s.removeBeamByIDLocked(id)
	}
	unit.PerkState.ChainSiphonBeamIDs = nil
	unit.PerkState.ChainSiphonPrimaryTargetID = 0
}

// sortChainCandidatesByDistThenID is a small deterministic sort helper for
// chain target selection. Operates on the named chainSiphonCandidate type
// declared next to chainSiphonTargetsLocked.
func sortChainCandidatesByDistThenID(c []chainSiphonCandidate) {
	// Simple insertion sort — chain pools are tiny (typically 0–3 enemies
	// inside a 140-pixel radius), so the constant-factor cost of
	// sort.Slice's reflection beats the asymptotic win. Stable too.
	for i := 1; i < len(c); i++ {
		x := c[i]
		j := i - 1
		for j >= 0 && (c[j].distSq > x.distSq || (c[j].distSq == x.distSq && c[j].u.ID > x.u.ID)) {
			c[j+1] = c[j]
			j--
		}
		c[j+1] = x
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Amplify Damage — autonomous AoE damage-taken multiplier
//
// Mirrors the lingering_hex / mark_of_weakness pattern: per-unit cooldown
// gate, anchor enemy (current channel target preferred, else nearest hostile
// in castRange), AoE stamps every enemy within `radius` of the anchor with a
// damage-taken multiplier that lasts `durationSeconds`. No mana cost.
//
// The multiplier is read by the damage pipeline (amplifyDamageTakenMultiplier-
// Locked) and amplifies every incoming damage instance multiplicatively with
// (1 + mult). Refresh-stronger semantics (max-wins) so two Siphoners stamping
// the same enemy keep the stronger multiplier; refresh-longer for duration.
// Damage amplifications from different Siphoner perks (amplify_damage vs the
// existing mark stacks) compose ADDITIVELY at the damage-pipeline level —
// each system is independent.
// ─────────────────────────────────────────────────────────────────────────────

// tickAmplifyDamagePerkLocked is the per-tick autonomous driver for the
// Amplify Damage Silver perk. Same shape as tickLingeringHexPerkLocked /
// tickMarkOfWeaknessPerkLocked.
//
// Caller holds s.mu write lock.
func (s *GameState) tickAmplifyDamagePerkLocked(unit *Unit, def *PerkDef, dt float64) {
	if unit == nil || def == nil {
		return
	}
	if unit.PerkState.AmplifyDamageCooldownRemaining > 0 {
		unit.PerkState.AmplifyDamageCooldownRemaining = math.Max(0, unit.PerkState.AmplifyDamageCooldownRemaining-dt)
	}
	if unit.HP <= 0 {
		return
	}
	if unit.PerkState.AmplifyDamageCooldownRemaining > 0 {
		return
	}
	cfg := def.ConfigForRank(unit.Rank)
	anchor := s.siphonerAfflictionAnchorLocked(unit, cfg["castRange"])
	if anchor == nil {
		return
	}
	s.applyAmplifyDamageAoELocked(unit, anchor)
	unit.PerkState.AmplifyDamageCooldownRemaining = cfg["cooldownSeconds"]
}

// applyAmplifyDamageAoELocked stamps the affliction on every hostile within
// `radius` of the anchor. Refresh-longer for duration, refresh-stronger
// (max-wins) for the multiplier — a re-cast that would weaken an existing
// stronger affliction is rejected per the design rule "multiple Amplify
// Damage effects should not stack multiplicatively unless explicitly
// configured later".
//
// Caller holds s.mu write lock.
func (s *GameState) applyAmplifyDamageAoELocked(caster, anchor *Unit) {
	def := perkDefByID("amplify_damage")
	if def == nil || caster == nil || anchor == nil {
		return
	}
	cfg := def.ConfigForRank(caster.Rank)
	radius := cfg["radius"]
	duration := cfg["durationSeconds"]
	mult := cfg["damageTakenMultiplier"]
	if radius <= 0 || duration <= 0 || mult <= 0 {
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
		// Visual placeholder: shadowburst per victim (matches the other
		// Siphoner affliction perks; TODO(siphoner-fx) author dedicated FX).
		s.queueEffectLocked("shadowburst", u.ID, u.X, u.Y, 1.0, 0.6, "")
		if duration > u.PerkState.AmplifyDamageRemaining {
			u.PerkState.AmplifyDamageRemaining = duration
		}
		if mult > u.PerkState.AmplifyDamageMultiplier {
			u.PerkState.AmplifyDamageMultiplier = mult
		}
	}
}

// amplifyDamageTakenMultiplierLocked returns the active damage-taken
// fraction from Amplify Damage on a unit (0 when inactive). Read by
// applyUnitDamageWithSourceLocked as (1 + mult) — so 0.25 means the unit
// takes 125% incoming damage. Composed additively with the existing mark
// amplification at the call site, not here, so both perks compose with the
// same multiplicative ceiling.
func amplifyDamageTakenMultiplierLocked(unit *Unit) float64 {
	if unit == nil || unit.PerkState.AmplifyDamageRemaining <= 0 {
		return 0
	}
	return unit.PerkState.AmplifyDamageMultiplier
}

// ─────────────────────────────────────────────────────────────────────────────
// Dark Renewal — excess-heal-to-shield converter
//
// Wired into distributeSiphonHealLocked: when the Siphoner owns dark_renewal
// and the heal amount exceeds what the Siphoner's HP can absorb, the
// remainder is converted to shielding rather than routed to an ally HP heal.
// Cascade order per spec:
//
//   1. Self HP (already done by distributeSiphonHealLocked before calling).
//   2. Self dark_renewal shield pool (cap maxSelfShield).
//   3. Ally dark_renewal shield pool on the nearest in-range ally that has
//      room in its dark_renewal pool (cap maxAllyShield per ally).
//   4. Wasted (per spec — no further fallback).
//
// Both pools persist until depleted; the source-specific shield system
// owns the per-pool cap and damage-pipeline drain order.
// ─────────────────────────────────────────────────────────────────────────────

const darkRenewalShieldSource = "dark_renewal"

// applyDarkRenewalExcessLocked consumes `remaining` HP worth of excess heal
// and routes it through the dark_renewal shield cascade (self → ally →
// waste). `remaining` is decremented in-place to reflect what was actually
// banked. Caller is responsible for the upstream self-heal portion; this
// helper only handles the overflow.
//
// Returns the amount actually banked across both pools so callers can log
// or telemetry the waste.
//
// Caller holds s.mu write lock.
func (s *GameState) applyDarkRenewalExcessLocked(siphoner *Unit, remaining int, allyRadius float64) int {
	if siphoner == nil || remaining <= 0 {
		return 0
	}
	def := perkDefByID("dark_renewal")
	if def == nil || !containsString(siphoner.PerkIDs, "dark_renewal") {
		return 0
	}
	cfg := def.ConfigForRank(siphoner.Rank)
	conversionPercent := cfg["shieldConversionPercent"]
	if conversionPercent <= 0 {
		return 0
	}
	maxSelfShield := int(cfg["maxSelfShield"])
	maxAllyShield := int(cfg["maxAllyShield"])
	allyR := cfg["allyRadius"]
	if allyR <= 0 {
		allyR = allyRadius // fall back to the channel's allyHealRadius
	}

	// Convert excess heal to shielding magnitude (caller currently passes the
	// raw HP overflow, so this just rescales). Round to nearest int — losing
	// 0.5 in either direction is fine and the alternative (banking fractional
	// shield) bloats the type for a corner case.
	available := int(math.Round(float64(remaining) * conversionPercent))
	if available <= 0 {
		return 0
	}

	banked := 0

	// Step 2: Siphoner self pool.
	if maxSelfShield > 0 {
		applied := s.applyShieldFromSourceLocked(
			siphoner,
			darkRenewalShieldSource,
			siphoner.ID,
			available,
			maxSelfShield,
			[]string{"corruption", "siphoner"},
		)
		banked += applied
		available -= applied
	}

	// Step 3: nearest in-range ally pool (with room).
	if available > 0 && maxAllyShield > 0 {
		ally := s.darkRenewalAllyRecipientLocked(siphoner, allyR, maxAllyShield)
		if ally != nil {
			applied := s.applyShieldFromSourceLocked(
				ally,
				darkRenewalShieldSource,
				siphoner.ID,
				available,
				maxAllyShield,
				[]string{"corruption", "siphoner"},
			)
			banked += applied
			available -= applied
		}
	}

	// Step 4: leftover is wasted (per spec). No log noise — wasted
	// shielding is expected late-game when allies are already capped.

	return banked
}

// darkRenewalAllyRecipientLocked picks the nearest visible, friendly,
// non-self ally within `radius` whose dark_renewal shield pool is not yet
// capped. Because dark_renewal is a Shared-stacking source, the pool is one
// shared bucket per recipient regardless of how many Siphoners are feeding
// it — the cap check looks at any pool of SourceType=dark_renewal on the
// candidate. Allies whose shared pool is already at MaxValue are skipped so
// overflow doesn't waste against a saturated recipient — the Siphoner keeps
// scanning until either a recipient is found or no eligible ally exists.
//
// Tie-break: ascending distance; allies tied on distance fall back to
// ascending unit.ID for deterministic replay.
//
// Returns nil when no eligible ally exists.
//
// Caller holds s.mu (read or write).
func (s *GameState) darkRenewalAllyRecipientLocked(siphoner *Unit, radius float64, maxAllyShield int) *Unit {
	if siphoner == nil || radius <= 0 {
		return nil
	}
	// Match the apply-side keying so we look at the same pool the apply
	// helper would top up. dark_renewal is registered as Shared today, so
	// SourceUnitID is ignored when probing for an existing pool — any
	// dark_renewal pool on the candidate counts toward the shared cap.
	shared := shieldStackingFor(darkRenewalShieldSource) == ShieldStackingShared
	radiusSq := radius * radius
	var best *Unit
	var bestSq float64
	for _, u := range s.Units {
		if u == nil || u.ID == siphoner.ID || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.unitsFriendlyLocked(siphoner, u) {
			continue
		}
		dx := u.X - siphoner.X
		dy := u.Y - siphoner.Y
		d2 := dx*dx + dy*dy
		if d2 > radiusSq {
			continue
		}
		// Cap check: walk pools and tally the current value of every pool
		// whose key matches the apply-side keying. For Shared sources this
		// is "any pool of this SourceType" (the shared bucket); for
		// PerSource it's "the pool granted by THIS Siphoner specifically".
		// O(pools) per candidate — pools per unit are tiny in practice.
		current := 0
		for i := range u.PerkState.ShieldPools {
			p := &u.PerkState.ShieldPools[i]
			if p.SourceType != darkRenewalShieldSource {
				continue
			}
			if !shared && p.SourceUnitID != siphoner.ID {
				continue
			}
			current += p.CurrentValue
			if shared {
				break // shared pool is unique per SourceType — first hit is the bucket
			}
		}
		if current >= maxAllyShield {
			continue
		}
		if best == nil || d2 < bestSq || (d2 == bestSq && u.ID < best.ID) {
			best = u
			bestSq = d2
		}
	}
	return best
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared Suffering — damage echo to nearby afflicted enemies
//
// When the Siphoner's primary Siphon Life tick damages an enemy, a
// percentage of that damage is also dealt to every other enemy within
// `radius` of the primary that already carries any Siphoner affliction
// (withering_beam, lingering_hex, mark_of_weakness, amplify_damage, and any
// future Siphoner-tagged affliction added via hasAnySiphonerAfflictionLocked).
//
// Recursion safety:
//
//   - The echo damage carries DamageSource.Kind = "shared_suffering" so any
//     future on-damage perk that reads Kind can filter it out.
//   - PerkState.SharedSufferingActive on the CASTER acts as a recursion
//     guard so this helper cannot re-enter itself within a single tick
//     (defensive — there's no current code path that would trigger this,
//     but the guard makes the invariant explicit and self-documenting).
// ─────────────────────────────────────────────────────────────────────────────

// applySharedSufferingLocked echoes a fraction of `primaryDamage` to every
// enemy within `radius` of `primary` that already carries any Siphoner
// affliction. Fires no-op when the caster doesn't own the perk.
//
// Caller holds s.mu write lock.
func (s *GameState) applySharedSufferingLocked(caster, primary *Unit, primaryDamage int) {
	if caster == nil || primary == nil || primaryDamage <= 0 {
		return
	}
	def := perkDefByID("shared_suffering")
	if def == nil || !containsString(caster.PerkIDs, "shared_suffering") {
		return
	}
	if caster.PerkState.SharedSufferingActive {
		return // recursion guard
	}
	cfg := def.ConfigForRank(caster.Rank)
	radius := cfg["radius"]
	sharePct := cfg["damageSharePercent"]
	if radius <= 0 || sharePct <= 0 {
		return
	}
	echoDamage := int(math.Round(float64(primaryDamage) * sharePct))
	if echoDamage <= 0 {
		return
	}
	radiusSq := radius * radius
	caster.PerkState.SharedSufferingActive = true
	defer func() { caster.PerkState.SharedSufferingActive = false }()

	for _, u := range s.Units {
		if u == nil || u.ID == primary.ID || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(caster.OwnerID, u.OwnerID) {
			continue
		}
		if !hasAnySiphonerAfflictionLocked(u) {
			continue
		}
		dx := u.X - primary.X
		dy := u.Y - primary.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Visual placeholder: shadowburst on each echo victim. A dedicated
		// "shared suffering" arc VFX can replace this later.
		s.queueEffectLocked("shadowburst", u.ID, u.X, u.Y, 0.6, 0.4, "")
		s.applyUnitDamageWithSourceLocked(u, echoDamage, DamageSource{
			AttackerUnitID: caster.ID,
			Kind:           "shared_suffering",
			DamageType:     DamageShadow,
		})
	}
}

// hasAnySiphonerAfflictionLocked reports whether `unit` currently carries
// any active Siphoner-source affliction. Generic by design so new Siphoner
// affliction perks added later only need to expose a Remaining > 0 check
// here — the consumer (shared_suffering) auto-includes them.
//
// Today's set: withering_beam, lingering_hex, mark_of_weakness,
// amplify_damage.
func hasAnySiphonerAfflictionLocked(unit *Unit) bool {
	if unit == nil {
		return false
	}
	ps := &unit.PerkState
	return ps.WitheringBeamRemaining > 0 ||
		ps.LingeringHexRemaining > 0 ||
		ps.MarkOfWeaknessRemaining > 0 ||
		ps.AmplifyDamageRemaining > 0
}
