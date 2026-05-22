package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// CLERIC SILVER PERKS — Divine Aegis, Restoration Aura, Zealous March,
// Divine Healer.
//
// All four perks are support-oriented and follow the existing cleric idiom of
// reading config live each tick. Owner-side timers (DivineAegisPulseRemaining,
// RestorationPulseRemaining) live on the cleric's PerkState and decay via
// tickUnitPerkStateLocked. Cross-unit recipient state (DivineAegisRemaining)
// lives on the recipient's PerkState and decays alongside the other cross-
// unit buff timers in state.go Update().
//
// AURA RADIUS CONVENTION
//
// All three cleric auras (sanctuary, divine_aegis, restoration_aura,
// zealous_march) read their radius from the config key "radiusPixels" — the
// same key the bronze cleric perks use. This is intentional: a perk's
// in-world reach is a *Cleric* concept, not a per-perk concept, so keeping
// the key uniform makes future tuning passes (and future cleric perks)
// trivial. The Vanguard auras use the shorter "radius" key for the same
// reason on their side.
//
// STACKING POLICY
//
// All cleric silver auras are max-wins, no-stack. Two Clerics with
// zealous_march overlapping on an ally grant the strongest aura's speed,
// not the sum. Two Clerics with divine_aegis overlapping refresh the same
// single charge with the strongest remaining duration. Two Clerics with
// restoration_aura overlapping fire independent pulses (each on their own
// pulse cadence) which intentionally produces extra heal events — the
// "stacking" comes from cadence overlap, not aura multiplication.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Divine Aegis — owner pulse
//
// tickDivineAegisPulseLocked drives the periodic aura pulse for a Cleric that
// owns divine_aegis. Decrements the pulse timer each tick; when it reaches 0
// it scans s.Units for allies within radiusPixels, stamps a single-charge
// protection on each (refresh-longer when the recipient already carries a
// charge), and re-arms the timer.
//
// Caller holds s.mu.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) tickDivineAegisPulseLocked(owner *Unit, def *PerkDef, dt float64) {
	if owner == nil || def == nil {
		return
	}
	cfg := def.ConfigForRank(owner.Rank)
	interval := cfg["intervalSeconds"]
	if interval <= 0 {
		return // misconfigured — never pulse
	}

	// Decay first so a pulse that fired at t=0 lands an interval at t=interval.
	owner.PerkState.DivineAegisPulseRemaining = math.Max(0, owner.PerkState.DivineAegisPulseRemaining-dt)
	if owner.PerkState.DivineAegisPulseRemaining > 0 {
		return
	}

	radius := cfg["radiusPixels"]
	radiusSq := radius * radius
	duration := cfg["protectionDurationSeconds"]
	if duration <= 0 || radius <= 0 {
		// Misconfigured — re-arm and skip so we don't busy-loop the pulse.
		owner.PerkState.DivineAegisPulseRemaining = interval
		return
	}

	for _, ally := range s.Units {
		if ally == nil || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if !s.unitsFriendlyLocked(owner, ally) {
			continue
		}
		dx := ally.X - owner.X
		dy := ally.Y - owner.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		if duration > ally.PerkState.DivineAegisRemaining {
			ally.PerkState.DivineAegisRemaining = duration
		}
	}

	owner.PerkState.DivineAegisPulseRemaining = interval
}

// ─────────────────────────────────────────────────────────────────────────────
// Divine Aegis — recipient consumption
//
// consumeDivineAegisLocked reports whether the target currently holds an
// unconsumed protection charge. When true, the field is cleared so the
// charge cannot block a second hit in the same call stack. Called at the
// top of the damage pipeline (after the pain_share redirect, before mark
// amplification / sanctuary / flat reduction / shield / HP).
//
// Caller holds s.mu write lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) consumeDivineAegisLocked(target *Unit) bool {
	if target == nil || target.PerkState.DivineAegisRemaining <= 0 {
		return false
	}
	target.PerkState.DivineAegisRemaining = 0
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// Restoration Aura — owner pulse
//
// tickRestorationAuraPulseLocked heals every nearby ally for healAmount on
// each tick once the interval timer hits zero. Heals are recorded via
// recordHealEventLocked so the client shows the floating +N. By design the
// pulse fires regardless of whether allies are at full HP — future
// heal-trigger systems are intended to react to the pulse event itself,
// not the resulting HP delta. healUnitLocked routes through any overheal
// system (today only blood_engine), so a full-HP ally with blood_engine
// will still see the pulse converted to shield.
//
// Divine Healer (when owned by the cleric) scales the pulse amount via
// perkClericHealOutputMultiplierLocked.
//
// Caller holds s.mu.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) tickRestorationAuraPulseLocked(owner *Unit, def *PerkDef, dt float64) {
	if owner == nil || def == nil {
		return
	}
	cfg := def.ConfigForRank(owner.Rank)
	interval := cfg["intervalSeconds"]
	if interval <= 0 {
		return
	}

	owner.PerkState.RestorationPulseRemaining = math.Max(0, owner.PerkState.RestorationPulseRemaining-dt)
	if owner.PerkState.RestorationPulseRemaining > 0 {
		return
	}

	radius := cfg["radiusPixels"]
	radiusSq := radius * radius
	baseHeal := cfg["healAmount"]
	scaledHeal := baseHeal * s.perkClericHealOutputMultiplierLocked(owner)
	healInt := int(math.Round(scaledHeal))
	if radius <= 0 || healInt <= 0 {
		owner.PerkState.RestorationPulseRemaining = interval
		return
	}

	for _, ally := range s.Units {
		if ally == nil || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if !s.unitsFriendlyLocked(owner, ally) {
			continue
		}
		dx := ally.X - owner.X
		dy := ally.Y - owner.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Visual: every pulse recipient gets the same healing_glow that the
		// Heal ability uses, so all cleric heal sources look consistent.
		s.playEffectOnUnitLocked(ally, "healing_glow")
		// Route through the central cleric heal helper so gold-tier triggers
		// (divine_judgement) fire under the restoration-aura meta. Beacon is
		// intentionally NOT triggered by aura pulses (see HealMeta docs).
		s.applyClericHealLocked(owner, ally, healInt, healMetaRestorationAura())
	}

	owner.PerkState.RestorationPulseRemaining = interval
}

// ─────────────────────────────────────────────────────────────────────────────
// Zealous March — move-speed aura (recipient query)
//
// perkMoveSpeedBonusFromClericAurasLocked returns the move-speed bonus the
// unit receives from every allied Cleric with zealous_march whose aura covers
// it. The first covering source contributes the full moveSpeedMultiplier;
// every additional source contributes the smaller stackBonus on top. So with
// the default tuning (base 30%, stack 5%) one cleric gives +30%, two clerics
// give +35%, three clerics give +40%, and so on. Designed to mirror the
// vanguard guardian_aura "companion synergy" feel but on the recipient
// scan rather than the source scan, since zealous_march has no per-source
// synergy state to cache.
//
// Both the base and the stack bonus are taken max-wins across covering
// sources (matters only when rank-tuning differs between covering clerics).
// Empty / missing stackBonus collapses cleanly to base-only behaviour, so
// re-tuning the JSON cannot break this helper.
//
// Caller holds s.mu (read or write).
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkMoveSpeedBonusFromClericAurasLocked(unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	def := perkDefByID("zealous_march")
	if def == nil {
		return 0
	}
	bestBase := 0.0
	bestStack := 0.0
	count := 0
	for _, src := range s.Units {
		if src == nil || src.HP <= 0 || !src.Visible {
			continue
		}
		if !containsString(src.PerkIDs, "zealous_march") {
			continue
		}
		if !s.unitsFriendlyLocked(src, unit) {
			continue
		}
		cfg := def.ConfigForRank(src.Rank)
		radius := cfg["radiusPixels"]
		if radius <= 0 {
			continue
		}
		dx := src.X - unit.X
		dy := src.Y - unit.Y
		if dx*dx+dy*dy > radius*radius {
			continue
		}
		count++
		if base := cfg["moveSpeedMultiplier"]; base > bestBase {
			bestBase = base
		}
		if stack := cfg["stackBonus"]; stack > bestStack {
			bestStack = stack
		}
	}
	if count == 0 {
		return 0
	}
	return bestBase + float64(count-1)*bestStack
}

// hasZealousMarchAuraLocked is a cheap one-shot probe used by the HUD code to
// decide whether to display the zealous_march recipient buff icon. Shares the
// same scan shape as perkMoveSpeedBonusFromClericAurasLocked but bails on the
// first matching source instead of finding the max.
//
// Caller holds s.mu (read or write).
func (s *GameState) hasZealousMarchAuraLocked(unit *Unit) bool {
	if unit == nil {
		return false
	}
	def := perkDefByID("zealous_march")
	if def == nil {
		return false
	}
	for _, src := range s.Units {
		if src == nil || src.HP <= 0 || !src.Visible {
			continue
		}
		if !containsString(src.PerkIDs, "zealous_march") {
			continue
		}
		if !s.unitsFriendlyLocked(src, unit) {
			continue
		}
		cfg := def.ConfigForRank(src.Rank)
		radius := cfg["radiusPixels"]
		if radius <= 0 {
			continue
		}
		dx := src.X - unit.X
		dy := src.Y - unit.Y
		if dx*dx+dy*dy <= radius*radius {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Divine Healer — heal/trigger scaling
//
// perkClericHealOutputMultiplierLocked returns the multiplier applied to
// every heal AMOUNT produced by `caster`. Default is 1.0 (no scaling); when
// the caster owns divine_healer it scales by healMultiplier. Used by:
//   - resolveAbilityCastOnTargetLocked        — scales AbilityDef.HealAmount
//   - tickRestorationAuraPulseLocked          — scales the aura pulse heal
//
// Designed to be called from any future heal source so divine_healer
// extends to new mechanics for free.
//
// Caller holds s.mu (read or write).
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkClericHealOutputMultiplierLocked(caster *Unit) float64 {
	if caster == nil {
		return 1.0
	}
	def := perkDefByID("divine_healer")
	if def == nil {
		return 1.0
	}
	if !containsString(caster.PerkIDs, "divine_healer") {
		return 1.0
	}
	cfg := def.ConfigForRank(caster.Rank)
	m := cfg["healMultiplier"]
	if m <= 0 {
		return 1.0
	}
	return m
}

// perkClericHealTriggeredMultiplierLocked returns the multiplier applied to
// the BUFF VALUES stamped by heal-triggered perks (battle_prayer,
// bolstering_prayer, and any future heal-trigger perks). Default is 1.0;
// divine_healer scales by triggeredEffectMultiplier. Used by:
//   - onPerkAbilityResolvedLocked  — scales battle_prayer / bolstering_prayer
//                                    duration and bonus values
//
// Deliberately disjoint from perkClericHealOutputMultiplierLocked so the
// two effects can be tuned independently per the silver-perk spec
// ("primarily affect: heal values, shield values, buff strength/duration,
// heal-triggered effects").
//
// Caller holds s.mu (read or write).
func (s *GameState) perkClericHealTriggeredMultiplierLocked(caster *Unit) float64 {
	if caster == nil {
		return 1.0
	}
	def := perkDefByID("divine_healer")
	if def == nil {
		return 1.0
	}
	if !containsString(caster.PerkIDs, "divine_healer") {
		return 1.0
	}
	cfg := def.ConfigForRank(caster.Rank)
	m := cfg["triggeredEffectMultiplier"]
	if m <= 0 {
		return 1.0
	}
	return m
}
