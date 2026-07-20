package game

import "math"

// siphonLifeAbilityID is the catalog id of the Siphoner's life-drain channel.
// The Siphoner perks that augment that channel (repurposed_life's mana-on-kill,
// beam_mastery's range multiplier) are gated to this specific ability by id —
// deliberately per-ability, so an unrelated future channel a Siphoner might
// carry doesn't silently inherit these perks (see the doc comments at each gate
// site). This named constant replaces the literal "siphon_life" that used to be
// duplicated across the three gate sites (channelRangeMultiplierForCasterLocked,
// clearChannelStateLocked, onSiphonVictimDeathLocked): a rename is now one edit
// and every gate is greppable. If a second siphon-style channel is ever added,
// generalize the gates here rather than adding a second literal.
const siphonLifeAbilityID = "siphon_life"

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
//   mark_of_weakness — NOT driven from here anymore. It used to be an
//                      autonomous AoE armor + healing-received debuff with
//                      the same firing pattern as Lingering Hex above; it is
//                      now a perk-granted composable ability
//                      (catalog/abilities/mark_of_weakness, auto-cast via
//                      the generic action-bar loop) instead. See this
//                      file's stat-hook-helpers section doc comment for
//                      what was deleted.
//
// EXTENSION POINTS — adding more Siphoner perks later:
//   • Silver/Gold perks  → add entries under
//                          catalog/units/human/acolyte/paths/siphoner/perks/silver.json
//                          and .../gold.json. Reuse the affliction PerkState
//                          fields where possible and put helpers in this file.
// ═════════════════════════════════════════════════════════════════════════════

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
// Lingering Hex — autonomous AoE affliction perk
//
// (Mark of Weakness used to be documented alongside Lingering Hex here as a
// second bespoke autonomous-fire perk with the identical shape. It was
// migrated to a perk-granted composable ability
// (catalog/abilities/mark_of_weakness) — see perks_siphoner.go's
// stat-hook-helpers section for what remains. Lingering Hex has not been
// migrated and still works exactly as described below.)
//
// The Bronze affliction perk fires automatically, on its own cadence:
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

// ─────────────────────────────────────────────────────────────────────────────
// Stat-hook helpers used by the shared stat functions
// (effectiveArmorLocked, healUnitLocked, etc.)
//
// Mark of Weakness's own bespoke pulse driver / AoE stamp / armor-reduction
// and healing-received-multiplier readers used to live here
// (applyMarkOfWeaknessAoELocked / markOfWeaknessArmorReductionLocked /
// markOfWeaknessHealingReceivedMultiplierLocked). The perk now GRANTS a
// composable ability (catalog/abilities/mark_of_weakness) whose program
// applies the SAME debuff via an authored apply_status(StatModifiers) —
// armor/healingReceived are read generically by effectiveArmorLocked /
// healUnitLocked via unitStatusStatModifiersLocked (perk_stat_modifiers.go).
// The PerkState.MarkOfWeakness* fields and their cross-unit decay
// (state.go) were removed in the same change — see the mark_of_weakness
// perk-to-ability migration's report for the full deletion list.
// ─────────────────────────────────────────────────────────────────────────────

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

// chainSiphonTargetsLocked picks the bouncing chain of up to
// additionalTargetCount Siphon Life secondary victims. The chain BOUNCES:
// each link is the nearest valid enemy within chainRange of the PREVIOUS
// link (or the primary target for link 0). The result is an ordered slice
// where targets[i] is `chainRange` from targets[i-1] (and targets[0] is
// chainRange from `primary`).
//
// Selection rules per hop:
//
//   - Hostile, alive, visible.
//   - Not the primary target; not any unit already chosen earlier in this
//     chain (no oscillation between two close enemies).
//   - Within chainRange of the cursor (the previous link's unit).
//   - Ties on distance broken by ascending unit.ID for deterministic replay.
//
// Returns nil when the caster doesn't own chain_siphon, when the perk's
// config is malformed (additionalTargetCount <= 0 or chainRange <= 0), or
// when no chain target is reachable from the primary on the first hop.
//
// Caller holds s.mu (read or write).
func (s *GameState) chainSiphonTargetsLocked(caster, primary *Unit) []*Unit {
	if caster == nil || primary == nil {
		return nil
	}
	if !containsString(caster.PerkIDs, "chain_siphon") {
		return nil
	}
	cfg := s.chainSiphonEffectiveConfigLocked(caster)
	if cfg == nil {
		return nil
	}
	maxCount := int(cfg["additionalTargetCount"])
	chainRange := cfg["chainRange"]
	if maxCount <= 0 || chainRange <= 0 {
		return nil
	}
	rangeSq := chainRange * chainRange

	// Bookkeeping: every unit that's already a link in the chain (plus the
	// primary and the caster) is excluded from subsequent hops. Caster
	// exclusion is defensive — the bouncing chain only ever picks hostiles
	// so the caster (friendly) can't match the hostility filter anyway, but
	// keeping the id in the set makes the intent explicit and is free.
	excluded := make(map[int]struct{}, maxCount+2)
	excluded[primary.ID] = struct{}{}
	excluded[caster.ID] = struct{}{}

	cursor := primary
	out := make([]*Unit, 0, maxCount)
	for i := 0; i < maxCount; i++ {
		next := s.nearestChainBounceTargetLocked(caster.OwnerID, cursor, rangeSq, excluded)
		if next == nil {
			break // chain breaks: no eligible enemy within chainRange of the last link
		}
		out = append(out, next)
		excluded[next.ID] = struct{}{}
		cursor = next
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// nearestChainBounceTargetLocked returns the nearest hostile (to
// casterOwnerID) that is alive, visible, within `rangeSq` of `from`, and not
// in the `excluded` set. Ties on squared distance break by ascending unit.ID
// for deterministic tick replay.
//
// Returns nil when no candidate exists — that's how the bounce loop knows
// the chain has hit a dead end.
//
// Caller holds s.mu (read or write).
func (s *GameState) nearestChainBounceTargetLocked(casterOwnerID string, from *Unit, rangeSq float64, excluded map[int]struct{}) *Unit {
	var best *Unit
	var bestSq float64
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if _, skip := excluded[u.ID]; skip {
			continue
		}
		if !s.playersAreHostileLocked(casterOwnerID, u.OwnerID) {
			continue
		}
		dx := u.X - from.X
		dy := u.Y - from.Y
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
	if !containsString(caster.PerkIDs, "chain_siphon") {
		return
	}
	// Read through the effective-config helper so Gold modifiers
	// (beam_mastery, ascended_corruption) automatically scale every
	// chain_siphon mechanic without per-perk patches here.
	cfg := s.chainSiphonEffectiveConfigLocked(caster)
	if cfg == nil {
		return
	}
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
			// apply on chain victims for free. The chain-beam major popup
			// auto-colors dark purple via the damage-type hint emitted inside
			// applyUnitDamageWithSourceLocked off DamageType=DamageShadow.
			// Category is Perk, not Ability: chain_siphon is a perk-granted
			// fan-out that creates NEW secondary damage instances off of
			// siphon_life's primary tick (it does carry SourceAbilityID,
			// since it's siphon_life's own damage in spirit — but per the
			// perk-vs-ability rule this task settled on, a perk hook that
			// CREATES a new instance is DamageCategoryPerk; only a REDIRECT/
			// PROPAGATION of an existing instance forwards the origin's own
			// Category — see perkRedirectIncomingDamageLocked, perks_auras.go).
			s.applyUnitDamageWithSourceLocked(t, secondaryDamage, DamageSource{
				AttackerUnitID:  caster.ID,
				Kind:            "chain_siphon",
				Category:        DamageCategoryPerk,
				DamageType:      DamageShadow,
				SourceAbilityID: abilityID,
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
// caster against the freshly-bounced chain reconciles the chain prefix-wise:
//
//   - If the primary target ID has changed since the last tick, ALL tracked
//     chain links are despawned (they emanate from a now-stale chain) and
//     the slice is rebuilt below from scratch.
//   - We walk the new chain and the recorded chain in lockstep. Every
//     leading position whose TargetID still matches is REUSED (no respawn).
//   - On the first divergence (different TargetID or recorded chain ran
//     out), we despawn the recorded beam at that position and EVERY
//     recorded beam after it — because the source units for those later
//     beams have just shifted, so the beams would visually anchor in the
//     wrong place.
//   - For positions past the kept prefix, we spawn a fresh beam with
//     caster = previous link's unit (or `primary` for position 0) and
//     target = new link's unit, recording the new (TargetID, BeamID) pair.
//
// Reusing beams across stable ticks keeps the visual flicker-free at the
// 0.25s channel cadence; only true entry / exit / re-route transitions
// touch the beam list. Pass chainTargets = nil to clear every link without
// touching the primary id (used by the per-tick "no chain reachable" path).
//
// Caller holds s.mu write lock.
func (s *GameState) syncChainSiphonBeamsLocked(caster, primary *Unit, chainTargets []*Unit, abilityID string) {
	if caster == nil {
		return
	}
	// Primary-target swap (or primary lost): every tracked link anchors off
	// a stale chain. Drop them all so the rebuild below re-anchors against
	// the new primary.
	if primary == nil || caster.PerkState.ChainSiphonPrimaryTargetID != primary.ID {
		for _, link := range caster.PerkState.ChainSiphonLinks {
			s.removeBeamByIDLocked(link.BeamID)
		}
		caster.PerkState.ChainSiphonLinks = caster.PerkState.ChainSiphonLinks[:0]
		if primary == nil {
			caster.PerkState.ChainSiphonPrimaryTargetID = 0
			return
		}
		caster.PerkState.ChainSiphonPrimaryTargetID = primary.ID
	}

	recorded := caster.PerkState.ChainSiphonLinks

	// Walk both chains in lockstep to find the longest matching prefix.
	keep := 0
	for keep < len(recorded) && keep < len(chainTargets) {
		if chainTargets[keep] == nil {
			break
		}
		if recorded[keep].TargetID != chainTargets[keep].ID {
			break
		}
		keep++
	}

	// Despawn the diverged tail (recorded[keep:]). Their source units
	// shifted (or they're being replaced by different targets) so the
	// beams would draw from the wrong place.
	for i := keep; i < len(recorded); i++ {
		s.removeBeamByIDLocked(recorded[i].BeamID)
	}
	recorded = recorded[:keep]

	// Spawn fresh beams for every new tail position. The beam's "caster"
	// (visual source) is the unit at chainTargets[i-1] for i > 0, or the
	// primary for i == 0 — that's the bouncing chain shape (x — x — x).
	for i := keep; i < len(chainTargets); i++ {
		next := chainTargets[i]
		if next == nil {
			break // defensive: don't spawn a half-formed link
		}
		var fromUnit *Unit
		if i == 0 {
			fromUnit = primary
		} else {
			fromUnit = chainTargets[i-1]
		}
		beam := s.spawnBeamLocked(fromUnit, next, abilityID, chainSiphonBeamVariant)
		recorded = append(recorded, ChainSiphonLink{TargetID: next.ID, BeamID: beam.ID})
	}

	caster.PerkState.ChainSiphonLinks = recorded
}

// clearChainSiphonBeamsLocked despawns every chain beam the unit currently
// owns and resets the bookkeeping fields. Called from clearChannelState-
// Locked so chain beams die alongside the primary channel — whether the
// channel ends naturally (target lost, mana depleted, order issued) or
// because the caster died.
//
// No-op for units that never owned chain_siphon (the slice stays at its
// nil zero value); the primary-target id is still defensively reset so a
// unit that owned chain_siphon, channeled once, then lost the perk doesn't
// carry a stale ID forever.
//
// Caller holds s.mu write lock.
func (s *GameState) clearChainSiphonBeamsLocked(unit *Unit) {
	if unit == nil {
		return
	}
	for _, link := range unit.PerkState.ChainSiphonLinks {
		s.removeBeamByIDLocked(link.BeamID)
	}
	unit.PerkState.ChainSiphonLinks = nil
	unit.PerkState.ChainSiphonPrimaryTargetID = 0
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
// Amplify Damage Silver perk. Same shape as tickLingeringHexPerkLocked (the
// bespoke Mark of Weakness driver this used to also be compared to,
// tickMarkOfWeaknessPerkLocked, was deleted by that perk's migration to a
// granted ability — see perks_siphoner.go's stat-hook-helpers section).
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
	if caster == nil || anchor == nil {
		return
	}
	// Effective config layers ascended_corruption modifiers (radius, duration,
	// potency) on top of the base when the caster owns the Gold perk. Base
	// values returned unchanged otherwise.
	cfg := s.amplifyDamageEffectiveConfigLocked(caster)
	if cfg == nil {
		return
	}
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
	if !containsString(siphoner.PerkIDs, "dark_renewal") {
		return 0
	}
	// Effective config: base dark_renewal config with ascended_corruption
	// modifiers layered in (bigger self/ally caps, more ally targets). The
	// helper also injects allyTargetCount = 1 (default) so the multi-ally
	// loop has a single key to read.
	cfg := s.darkRenewalEffectiveConfigLocked(siphoner)
	if cfg == nil {
		return 0
	}
	conversionPercent := cfg["shieldConversionPercent"]
	if conversionPercent <= 0 {
		return 0
	}
	maxSelfShield := int(cfg["maxSelfShield"])
	maxAllyShield := int(cfg["maxAllyShield"])
	allyTargetCount := int(cfg["allyTargetCount"])
	if allyTargetCount < 1 {
		allyTargetCount = 1
	}
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

	// Step 3: up to allyTargetCount nearest in-range allies, each with room.
	// Walk one ally at a time, exclude already-served recipients via a small
	// id-set so we never pick the same ally twice in one call. Without
	// ascended_corruption allyTargetCount == 1 and this collapses to the
	// legacy single-ally behaviour.
	if available > 0 && maxAllyShield > 0 {
		served := make(map[int]struct{}, allyTargetCount)
		for i := 0; i < allyTargetCount && available > 0; i++ {
			ally := s.darkRenewalAllyRecipientLockedExcluding(siphoner, allyR, maxAllyShield, served)
			if ally == nil {
				break
			}
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
			served[ally.ID] = struct{}{}
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
	return s.darkRenewalAllyRecipientLockedExcluding(siphoner, radius, maxAllyShield, nil)
}

// darkRenewalAllyRecipientLockedExcluding is the multi-target variant used
// by applyDarkRenewalExcessLocked when ascended_corruption raises
// allyTargetCount above 1. `exclude` carries the set of ally ids already
// served in the current cascade so we never pick the same ally twice in one
// call. Pass nil (or empty) for the legacy single-pick behaviour — the
// shadowed wrapper above does exactly that.
//
// Caller holds s.mu (read or write).
func (s *GameState) darkRenewalAllyRecipientLockedExcluding(siphoner *Unit, radius float64, maxAllyShield int, exclude map[int]struct{}) *Unit {
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
		if _, skipped := exclude[u.ID]; skipped {
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
// Shared Suffering — damage echo to nearby enemies
//
// MIGRATED to a data-driven ability rider (see
// docs/superpowers/plans/2026-07-19-perk-ability-riders-tier-b.md Task 4):
// catalog/perks/siphoner/shared_suffering/shared_suffering.json's
// abilityRiders entry (target "siphon_life", trigger "on_tick") now
// carries the select_targets + deal_damage actions that used to live in
// applySharedSufferingLocked below, run generically by
// runAbilityRidersForCasterLocked (ability_riders.go) from the same call
// site in ability_channel.go. Base-value behavior (radius 120, share 0.4)
// is byte-identical to the old Go helper — proven by
// perks_siphoner_shared_suffering_migration_test.go. Two accepted deltas:
//   - The Gold ascended_corruption overlay (radius×1.5, share+0.2) is
//     temporarily INERT (perk-modifies-perk, deferred to Tier B.5) — see
//     sharedSufferingEffectiveConfigLocked below, still exercised on its
//     own by TestAscendedCorruption_SharedSufferingLayering but no longer
//     consulted by the live echo path.
//   - The echo's DamageSource.Kind changed from "shared_suffering" to
//     "ability" (the generic deal_damage action's tag), and the old
//     shadowburst VFX + minor-popup (side-falling) split are gone — the
//     echo now renders as an ordinary shadow-tinted floating-up hit like
//     the primary tick. Both are debug-label/cosmetic-only; grepped clean
//     of any downstream reader keyed on Kind=="shared_suffering".
// ─────────────────────────────────────────────────────────────────────────────

// ═════════════════════════════════════════════════════════════════════════════
// SIPHONER GOLD PERKS
//
//   ascended_corruption — adaptive. Layers per-Silver-perk modifiers via
//                         the effective-config helpers below. Mirrors the
//                         Trapper ascendant_infusion pattern: one Gold
//                         perk produces different effects depending on
//                         which Silver perk the unit already owns.
//
//   beam_mastery       — global Siphon Life buff. Scales damage / healing /
//                         range / mana cost via its data-driven
//                         abilityModifiers entry (consumed by the generic
//                         abilityScalarModifiersForCasterLocked aggregator
//                         in ability_modifiers.go), and bumps Chain Siphon's
//                         target count via chainSiphonEffectiveConfigLocked
//                         when the unit also owns Chain Siphon.
//
//   repurposed_life    — on-death mana support. Fires when an enemy
//                         actively being drained by a Siphoner with the
//                         perk dies — restores mana to nearby allies
//                         (including the Siphoner). Wired through the
//                         death-pipeline hook onSiphonVictimDeathLocked.
//
// ── EFFECTIVE-CONFIG PATTERN ─────────────────────────────────────────────────
//
// Each Silver perk has a thin "effective config" helper that returns the
// JSON config with every modifier applied. The Silver helpers (chain
// targets / amplify AoE / dark renewal cascade / shared suffering echo) all
// read from these helpers instead of perkDefByID(...).Config directly, so
// adding a new modifier source (Gold, future Legendary, item, etc.) means
// touching ONE helper per Silver perk rather than every consumer.
//
// Modifier composition rules (followed by every helper below):
//   - "Bonus" fields ADD to the base (e.g. +0.25 damage share).
//   - "Multiplier" fields MULTIPLY the base (e.g. 1.5× radius).
//   - "Count" bonuses add to integer counts (additionalTargetCount + 1).
//   - Missing Gold perk = base config returned unchanged.
//
// All effective-config helpers return a fresh map (never alias the catalog
// map) so mutation later in the call stack is safe.
// ═════════════════════════════════════════════════════════════════════════════

// chainSiphonEffectiveConfigLocked returns the chain_siphon config with
// every modifier from the caster's other perks layered in. Layers:
//   - beam_mastery: +chainAdditionalTargetBonus to additionalTargetCount.
//   - ascended_corruption: +chainAdditionalTargetCountBonus, ×chainRange-
//     Multiplier, +chainSecondaryDamageMultiplierBonus, +chainSecondary-
//     HealingMultiplierBonus.
//
// Returns nil when chain_siphon is not in the catalog (defensive). Returns
// the base config unchanged when the caster owns no modifiers.
//
// Caller holds s.mu (read or write).
func (s *GameState) chainSiphonEffectiveConfigLocked(caster *Unit) map[string]float64 {
	def := perkDefByID("chain_siphon")
	if def == nil {
		return nil
	}
	base := def.ConfigForRank(rankOrEmpty(caster))
	cfg := copyConfigMap(base)
	if caster == nil {
		return cfg
	}
	if beam := perkDefByID("beam_mastery"); beam != nil && containsString(caster.PerkIDs, "beam_mastery") {
		bcfg := beam.ConfigForRank(caster.Rank)
		if v := bcfg["chainAdditionalTargetBonus"]; v > 0 {
			cfg["additionalTargetCount"] = cfg["additionalTargetCount"] + v
		}
	}
	if asc := perkDefByID("ascended_corruption"); asc != nil && containsString(caster.PerkIDs, "ascended_corruption") {
		acfg := asc.ConfigForRank(caster.Rank)
		if v := acfg["chainAdditionalTargetCountBonus"]; v > 0 {
			cfg["additionalTargetCount"] = cfg["additionalTargetCount"] + v
		}
		if v := acfg["chainRangeMultiplier"]; v > 0 {
			cfg["chainRange"] = cfg["chainRange"] * v
		}
		if v := acfg["chainSecondaryDamageMultiplierBonus"]; v > 0 {
			cfg["secondaryDamageMultiplier"] = cfg["secondaryDamageMultiplier"] + v
		}
		if v := acfg["chainSecondaryHealingMultiplierBonus"]; v > 0 {
			cfg["secondaryHealingMultiplier"] = cfg["secondaryHealingMultiplier"] + v
		}
	}
	return cfg
}

// amplifyDamageEffectiveConfigLocked returns the amplify_damage config with
// ascended_corruption modifiers layered in. Layers:
//   - amplifyRadiusMultiplier  → ×radius
//   - amplifyDurationMultiplier → ×durationSeconds
//   - amplifyDamageTakenMultiplierBonus → +damageTakenMultiplier
//
// Caller holds s.mu (read or write).
func (s *GameState) amplifyDamageEffectiveConfigLocked(caster *Unit) map[string]float64 {
	def := perkDefByID("amplify_damage")
	if def == nil {
		return nil
	}
	cfg := copyConfigMap(def.ConfigForRank(rankOrEmpty(caster)))
	if caster == nil {
		return cfg
	}
	if asc := perkDefByID("ascended_corruption"); asc != nil && containsString(caster.PerkIDs, "ascended_corruption") {
		acfg := asc.ConfigForRank(caster.Rank)
		if v := acfg["amplifyRadiusMultiplier"]; v > 0 {
			cfg["radius"] = cfg["radius"] * v
		}
		if v := acfg["amplifyDurationMultiplier"]; v > 0 {
			cfg["durationSeconds"] = cfg["durationSeconds"] * v
		}
		if v := acfg["amplifyDamageTakenMultiplierBonus"]; v > 0 {
			cfg["damageTakenMultiplier"] = cfg["damageTakenMultiplier"] + v
		}
	}
	return cfg
}

// darkRenewalEffectiveConfigLocked returns the dark_renewal config with
// ascended_corruption modifiers layered in. Layers:
//   - darkMaxSelfShieldBonus  → +maxSelfShield
//   - darkMaxAllyShieldBonus  → +maxAllyShield
//   - darkAdditionalAllyShieldTargets → injected as "allyTargetCount"
//     (default 1; total = 1 + bonus). Read by applyDarkRenewalExcessLocked
//     to drive the multi-ally loop.
//
// Caller holds s.mu (read or write).
func (s *GameState) darkRenewalEffectiveConfigLocked(caster *Unit) map[string]float64 {
	def := perkDefByID("dark_renewal")
	if def == nil {
		return nil
	}
	cfg := copyConfigMap(def.ConfigForRank(rankOrEmpty(caster)))
	// Always inject the ally target count so the consumer has a single key
	// to read (default 1 = single nearest ally, matching legacy behaviour).
	cfg["allyTargetCount"] = 1
	if caster == nil {
		return cfg
	}
	if asc := perkDefByID("ascended_corruption"); asc != nil && containsString(caster.PerkIDs, "ascended_corruption") {
		acfg := asc.ConfigForRank(caster.Rank)
		if v := acfg["darkMaxSelfShieldBonus"]; v > 0 {
			cfg["maxSelfShield"] = cfg["maxSelfShield"] + v
		}
		if v := acfg["darkMaxAllyShieldBonus"]; v > 0 {
			cfg["maxAllyShield"] = cfg["maxAllyShield"] + v
		}
		if v := acfg["darkAdditionalAllyShieldTargets"]; v > 0 {
			cfg["allyTargetCount"] = cfg["allyTargetCount"] + v
		}
	}
	return cfg
}

// sharedSufferingEffectiveConfigLocked returns the shared_suffering config
// with ascended_corruption modifiers layered in. Layers:
//   - sharedRadiusMultiplier  → ×radius
//   - sharedDamageSharePercentBonus → +damageSharePercent
//
// Caller holds s.mu (read or write).
func (s *GameState) sharedSufferingEffectiveConfigLocked(caster *Unit) map[string]float64 {
	def := perkDefByID("shared_suffering")
	if def == nil {
		return nil
	}
	cfg := copyConfigMap(def.ConfigForRank(rankOrEmpty(caster)))
	if caster == nil {
		return cfg
	}
	if asc := perkDefByID("ascended_corruption"); asc != nil && containsString(caster.PerkIDs, "ascended_corruption") {
		acfg := asc.ConfigForRank(caster.Rank)
		if v := acfg["sharedRadiusMultiplier"]; v > 0 {
			cfg["radius"] = cfg["radius"] * v
		}
		if v := acfg["sharedDamageSharePercentBonus"]; v > 0 {
			cfg["damageSharePercent"] = cfg["damageSharePercent"] + v
		}
	}
	return cfg
}

// copyConfigMap returns a shallow copy of a perk config map so callers can
// mutate it without disturbing the catalog's shared instance. Cheap — perk
// configs hold a handful of keys at most.
func copyConfigMap(src map[string]float64) map[string]float64 {
	if src == nil {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// rankOrEmpty returns caster.Rank, or "" when caster is nil. Used by the
// effective-config helpers so they accept a nil caster (during catalog
// inspection) without panicking. ConfigForRank("") falls back to base.
func rankOrEmpty(caster *Unit) string {
	if caster == nil {
		return ""
	}
	return caster.Rank
}

// ─────────────────────────────────────────────────────────────────────────────
// Repurposed Life — on-death mana support
//
// Fires when an enemy that is CURRENTLY being drained by a Siphoner with the
// perk dies. The trigger is per-Siphoner: every alive Siphoner whose
// ChannelAbilityID == "siphon_life" and whose ChannelTargetID == victim.ID
// at the moment of death (and who owns repurposed_life) fires its own mana
// pulse to its nearby allies. Multiple Siphoners simultaneously draining
// the same enemy each fire their own pulse — that's intentional, the
// "siphoned at moment of death" condition is a per-channel fact.
//
// Recursion safety: mana restore is a pure CurrentMana mutation via
// addUnitManaLocked — it does not trigger any death event or perk hook,
// so no recursion guard is needed.
// ─────────────────────────────────────────────────────────────────────────────

// onSiphonVictimDeathLocked is called from drainPendingDeathsLocked once
// per dying unit (after kill attribution / XP bookkeeping). It scans for
// every active Siphoner whose Siphon Life channel was targeting the dying
// unit at the moment of death — for each that owns repurposed_life, fires
// the mana pulse to nearby allies.
//
// Cheap when no Siphoner is channeling the victim (early-out on the
// channel-target check); pays the per-Siphoner scan only when there's
// actually a Siphoner draining this unit.
//
// Caller holds s.mu write lock.
func (s *GameState) onSiphonVictimDeathLocked(victim *Unit) {
	if victim == nil {
		return
	}
	def := perkDefByID("repurposed_life")
	if def == nil {
		return
	}
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 {
			continue
		}
		if u.ChannelAbilityID != siphonLifeAbilityID || u.ChannelTargetID != victim.ID {
			continue
		}
		if !containsString(u.PerkIDs, "repurposed_life") {
			continue
		}
		s.fireRepurposedLifeManaRestoreLocked(u, def)
	}
}

// fireRepurposedLifeManaRestoreLocked pulses manaRestoreAmount to every
// alive friendly unit within radius of the Siphoner, the Siphoner included.
// Routed through addUnitManaLocked so the per-recipient cap is respected
// uniformly (no over-restore beyond MaxMana). Allies without a mana pool
// (MaxMana == 0) are skipped automatically by the helper.
//
// Caller holds s.mu write lock.
func (s *GameState) fireRepurposedLifeManaRestoreLocked(siphoner *Unit, def *PerkDef) {
	if siphoner == nil || def == nil {
		return
	}
	cfg := def.ConfigForRank(siphoner.Rank)
	amount := int(cfg["manaRestoreAmount"])
	radius := cfg["radius"]
	if amount <= 0 || radius <= 0 {
		return
	}
	radiusSq := radius * radius
	for _, ally := range s.Units {
		if ally == nil || ally.HP <= 0 {
			continue
		}
		if !s.unitsFriendlyLocked(siphoner, ally) {
			continue
		}
		dx := ally.X - siphoner.X
		dy := ally.Y - siphoner.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		s.addUnitManaLocked(ally, amount)
	}
}
