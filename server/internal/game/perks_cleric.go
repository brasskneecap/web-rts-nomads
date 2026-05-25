package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// CLERIC PERKS — SILVER & GOLD
//
// This file owns the runtime helpers for every Cleric Silver and Gold perk.
// Cleric BRONZE perks (sanctuary, battle_prayer, bolstering_prayer,
// mana_conduit) are light enough to live as case arms in the shared hook
// files (perks.go, perks_attack.go, perks_defense.go, perks_icons.go) —
// see the per-path file convention in perks.go.
//
// Silver perks (support-oriented):
//   - divine_aegis      — owner-pulsed one-charge damage block on nearby allies.
//   - restoration_aura  — owner-pulsed periodic heal on nearby allies.
//   - zealous_march     — aura granting move-speed to nearby allies.
//   - divine_healer     — owner-side multiplier on every heal amount AND on
//                         every heal-triggered buff (battle_prayer, etc.).
//
// Gold perks (heal-triggered AoE + emergency save):
//   - beacon_of_life    — splash heal around a heal target.
//   - divine_judgement  — holy AoE damage around a heal target.
//   - divine_intervention — restore HP + brief invulnerability when an ally
//                           would die within range.
//
// AURA RADIUS CONVENTION
//
// All cleric auras (sanctuary bronze, divine_aegis silver, restoration_aura
// silver, zealous_march silver) read their radius from the config key
// "radiusPixels". A perk's in-world reach is a Cleric concept, not a per-perk
// concept, so keeping the key uniform makes future tuning passes trivial.
//
// STACKING POLICY
//
// All cleric auras are max-wins, no-stack at the magnitude level. Two Clerics
// with zealous_march overlapping on an ally grant the strongest aura's speed,
// not the sum (with a small per-additional-source stack bonus). Two Clerics
// with divine_aegis overlapping refresh the same single charge with the
// strongest remaining duration. Two Clerics with restoration_aura overlapping
// fire independent pulses on their own cadence — the "stacking" comes from
// cadence overlap, not aura multiplication.
//
// EXTENSION POINTS
//   - HealMeta: add new flags for future heal-trigger perks; older sites
//     using the zero value automatically opt out, which keeps additions safe.
//   - applyDivineJudgementHitEffectLocked: per-enemy visual hook; today reads
//     the perk's `effect` JSON field. Authors a sprite for the splash by
//     setting effect.name in gold.json without touching code.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// SILVER — Divine Aegis (owner pulse)
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
// SILVER — Divine Aegis (recipient consumption)
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
// SILVER — Restoration Aura (owner pulse)
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
// SILVER — Zealous March (move-speed aura, recipient query)
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
// BRONZE — Mana Conduit (aura: bonus mana regen for nearby allies)
//
// Recipient-query pattern (same shape as zealous_march): each unit's mana
// regen tick asks "is any allied Cleric with mana_conduit covering me?" and
// if so adds the strongest covering source's bonusManaRegen to the per-tick
// rate. Max-wins across multiple Clerics — they don't stack (consistent
// with the "all cleric auras are max-wins, no-stack at the magnitude
// level" convention).
//
// The Cleric is inside their own aura (distance 0) so they benefit too,
// matching the old self-only behavior the perk replaced.
// ─────────────────────────────────────────────────────────────────────────────

// manaConduitAuraBonusLocked returns the bonus mana-regen rate (per second)
// the unit receives from every allied Cleric with mana_conduit whose aura
// covers it. Max-wins across covering sources — a unit in two Clerics'
// auras still receives only the strongest bonus, not the sum.
//
// Returns 0 when no covering source exists. Called from
// tickUnitManaRegenLocked on every mana-bearing unit per tick, so it stays
// cheap: skips units with no mana pool earlier (caller's gate), and the
// scan walks s.Units once with simple distance + perk checks per candidate.
//
// Caller holds s.mu (read or write).
func (s *GameState) manaConduitAuraBonusLocked(unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	def := perkDefByID("mana_conduit")
	if def == nil {
		return 0
	}
	best := 0.0
	for _, src := range s.Units {
		if src == nil || src.HP <= 0 || !src.Visible {
			continue
		}
		if !containsString(src.PerkIDs, "mana_conduit") {
			continue
		}
		if !s.unitsFriendlyLocked(src, unit) {
			continue
		}
		cfg := def.ConfigForRank(src.Rank)
		radius := cfg["radiusPixels"]
		bonus := cfg["bonusManaRegen"]
		if radius <= 0 || bonus <= 0 {
			continue
		}
		dx := src.X - unit.X
		dy := src.Y - unit.Y
		if dx*dx+dy*dy > radius*radius {
			continue
		}
		if bonus > best {
			best = bonus
		}
	}
	return best
}

// hasManaConduitAuraLocked is a cheap one-shot probe used by the HUD code
// to decide whether to display the mana_conduit recipient buff icon.
// Mirrors hasZealousMarchAuraLocked: same scan shape as the bonus helper
// above but bails on the first matching source.
//
// Caller holds s.mu (read or write).
func (s *GameState) hasManaConduitAuraLocked(unit *Unit) bool {
	if unit == nil {
		return false
	}
	def := perkDefByID("mana_conduit")
	if def == nil {
		return false
	}
	for _, src := range s.Units {
		if src == nil || src.HP <= 0 || !src.Visible {
			continue
		}
		if !containsString(src.PerkIDs, "mana_conduit") {
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
// SILVER — Divine Healer (heal / trigger scaling)
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

// ═════════════════════════════════════════════════════════════════════════════
// GOLD — Divine Intervention, Beacon of Life, Divine Judgement
//
// All three perks key off the heal pipeline rather than a per-tick aura, and
// they interact: a primary heal may splash via Beacon, and each landing heal
// (primary or splash) may detonate Divine Judgement. To keep that interaction
// safe AND extensible, every cleric heal funnels through applyClericHealLocked
// with a HealMeta value that flags which downstream triggers are allowed.
//
// Divine Intervention is the odd one out: it fires from the death pipeline,
// not the heal pipeline. The would-die hook lives in
// applyUnitDamageWithSourceLocked just before enqueueDeathLocked so a
// successful save never queues a death.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// HealMeta — flag bundle threaded through every cleric heal
//
// Each cleric heal source produces a HealMeta value describing what the heal
// "is" and what downstream perks are allowed to react to it. Carrying the
// flags in the meta rather than per-unit bools means a chain like
// `primary → beacon splash → divine judgement on each splash victim` cannot
// accidentally re-enter beacon (the splash sets CanTriggerBeacon = false on
// the metas it produces).
//
// The zero value is the safe default: nothing triggers. New heal sources can
// adopt the helper without accidentally proccing cleric perks — they only
// opt in by populating the flags.
// ─────────────────────────────────────────────────────────────────────────────

// HealSource is a tiny enum used as a telemetry/debug label and as the key
// future heal-trigger perks can branch on if they want source-specific
// behaviour ("only react to ability heals", etc.). Adding a new source value
// here does not change any existing behaviour.
type HealSource string

const (
	HealSourceAbility         HealSource = "ability"          // Greater Heal cast
	HealSourceRestorationAura HealSource = "restoration_aura" // silver cleric pulse
	HealSourceBeaconSplash    HealSource = "beacon_splash"    // gold cleric splash
	HealSourceIntervention    HealSource = "intervention"     // gold cleric revive (does NOT go through applyClericHealLocked today)
)

// HealMeta classifies a single heal event for downstream trigger perks.
type HealMeta struct {
	// Source labels the heal for telemetry/debug. Has no mechanical effect
	// in the current code; future per-source trigger gating can switch on it.
	Source HealSource

	// CanTriggerBeacon controls whether beacon_of_life splashes from this
	// heal. False on splash heals so they cannot chain into infinite
	// healing — this is the single most important invariant in the gold
	// cleric system.
	CanTriggerBeacon bool

	// CanTriggerJudgement controls whether divine_judgement detonates around
	// the healed unit. True on every cleric-originated heal by default;
	// intervention deliberately opts out (emergency saves shouldn't AoE
	// enemies into a fresh death window).
	CanTriggerJudgement bool
}

// healMetaPrimaryAbility is the canonical meta for a heal that originates
// from a cast Heal/Greater Heal ability. Triggers everything.
func healMetaPrimaryAbility() HealMeta {
	return HealMeta{
		Source:              HealSourceAbility,
		CanTriggerBeacon:    true,
		CanTriggerJudgement: true,
	}
}

// healMetaRestorationAura is the canonical meta for a Restoration Aura pulse.
// Triggers Divine Judgement but NOT Beacon (per the spec). Flipping the
// beacon flag here is a one-line change if the design ever shifts.
func healMetaRestorationAura() HealMeta {
	return HealMeta{
		Source:              HealSourceRestorationAura,
		CanTriggerBeacon:    false,
		CanTriggerJudgement: true,
	}
}

// healMetaBeaconSplash returns the meta for the splash heals beacon_of_life
// emits. Always sets CanTriggerBeacon = false (hard invariant). The judgement
// flag is forwarded from the parent meta so a primary ability heal with both
// perks owned still detonates judgement around each splash victim — the
// "Beacon splash heal may still trigger Divine Judgement if allowed" rule.
func healMetaBeaconSplash(parent HealMeta) HealMeta {
	return HealMeta{
		Source:              HealSourceBeaconSplash,
		CanTriggerBeacon:    false, // hard rule
		CanTriggerJudgement: parent.CanTriggerJudgement,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GOLD — applyClericHealLocked (central heal helper)
//
// Every cleric heal funnels through here. The function:
//   1. Routes HP through healUnitLocked (preserves overheal→shield routing).
//   2. Records the EFFECTIVE gain as a heal event so the floating +N reflects
//      the HP the unit actually gained (not the intended amount).
//   3. Plays healing_glow on the target so every cleric heal — ability,
//      aura pulse, beacon splash — looks consistent.
//   4. Fires the gold trigger perks (beacon_of_life, divine_judgement) using
//      the metadata flags. Triggers always receive the INTENDED amount, not
//      the post-clamp gain, so a heal landing on a full-HP target still
//      proccs judgement at full strength per the design spec.
//
// Safe to call with caster == nil (no perks fire) or target == nil/dead (no-op).
//
// Caller holds s.mu write lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) applyClericHealLocked(caster, target *Unit, amount int, meta HealMeta) {
	if target == nil || amount <= 0 || target.HP <= 0 {
		return
	}
	before := target.HP
	s.healUnitLocked(target, amount)
	if target.HP > before {
		s.recordHealEventLocked(target, target.HP-before)
	}
	// VFX is intentionally caller-side. Heal abilities already declare
	// effectOnTarget in their JSON (Greater Heal → "healing_glow") and
	// resolveAbilityCastOnTargetLocked plays it; restoration_aura and beacon
	// splash play healing_glow explicitly per recipient. Centralising the
	// visual here would double-play it for ability heals.

	// Triggers receive the INTENDED amount, not the post-clamp gain. Beacon's
	// splash percent and Judgement's holy damage both use the full pre-clamp
	// value per the design spec — heals on full-HP targets still proc both.
	if meta.CanTriggerBeacon {
		s.fireBeaconOfLifeSplashLocked(caster, target, amount, meta)
	}
	if meta.CanTriggerJudgement {
		s.fireDivineJudgementLocked(caster, target, amount)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GOLD — Beacon of Life (splash heal)
//
// fireBeaconOfLifeSplashLocked emits a partial heal to every allied unit
// within splashRadius of the primary heal's target (excluding the primary
// itself). The splash meta hard-disables CanTriggerBeacon to prevent
// chain healing and forwards parent's CanTriggerJudgement so a splash victim
// still gets a judgement detonation if the originating heal was judgement-
// enabled.
//
// The splash amount is splashHealPercent × INTENDED primary amount so a
// divine_healer-boosted primary heal splashes proportionally. Per-ally splash
// amounts are rounded to nearest int and skipped if they round to zero.
//
// Caller holds s.mu write lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) fireBeaconOfLifeSplashLocked(caster, primary *Unit, primaryAmount int, parentMeta HealMeta) {
	if caster == nil || primary == nil {
		return
	}
	if !containsString(caster.PerkIDs, "beacon_of_life") {
		return
	}
	def := perkDefByID("beacon_of_life")
	if def == nil {
		return
	}
	cfg := def.ConfigForRank(caster.Rank)
	pct := cfg["splashHealPercent"]
	radius := cfg["splashRadius"]
	if pct <= 0 || radius <= 0 || primaryAmount <= 0 {
		return
	}
	splashAmount := int(math.Round(float64(primaryAmount) * pct))
	if splashAmount <= 0 {
		return
	}
	radiusSq := radius * radius
	splashMeta := healMetaBeaconSplash(parentMeta)
	for _, ally := range s.Units {
		if ally == nil || ally.ID == primary.ID {
			continue
		}
		if ally.HP <= 0 || !ally.Visible {
			continue
		}
		if !s.unitsFriendlyLocked(caster, ally) {
			continue
		}
		dx := ally.X - primary.X
		dy := ally.Y - primary.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Visual: every splash recipient gets a healing_glow so the cascade
		// reads at a glance. applyClericHealLocked deliberately does not play
		// VFX itself — see its comment.
		s.playEffectOnUnitLocked(ally, "healing_glow")
		// Recurse intentionally — splashMeta has CanTriggerBeacon=false so
		// this cannot re-enter the splash loop. CanTriggerJudgement IS
		// forwarded, so a splash victim still gets a judgement detonation.
		s.applyClericHealLocked(caster, ally, splashAmount, splashMeta)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GOLD — Divine Judgement (AoE holy damage on heal)
//
// fireDivineJudgementLocked detonates a holy-damage AoE around the healed
// unit. Per spec the damage equals the INTENDED heal amount regardless of
// whether the target was at full HP, overhealed, or healed by an aura — so
// using the pre-clamp `intendedAmount` is correct.
//
// Damage routes through the canonical pipeline (applyUnitDamageWithSourceLocked)
// so armor mitigation, mark amplification, the death pipeline, and battle
// telemetry all work without special-casing. The DamageHoly tag carries the
// element forward (sanctuary mitigates projectiles only, so a holy-typed
// ability damage instance is unaffected by sanctuary — design-intentional).
//
// The user has asked to attach a per-enemy hit effect later. The seam is
// applyDivineJudgementHitEffectLocked below — it reads def.Effect and
// queues a visual on each hit enemy. Author the effect name in gold.json
// to enable.
//
// Recursion safety: divine_judgement damage cannot itself trigger another
// judgement because judgement only fires from heal events, never from damage
// events. If a future perk introduces heal-on-damage-taken, that perk's
// integration should set CanTriggerJudgement = false on its meta to keep
// the no-recursion guarantee.
//
// Caller holds s.mu write lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) fireDivineJudgementLocked(caster, healed *Unit, intendedAmount int) {
	if caster == nil || healed == nil {
		return
	}
	if !containsString(caster.PerkIDs, "divine_judgement") {
		return
	}
	def := perkDefByID("divine_judgement")
	if def == nil {
		return
	}
	cfg := def.ConfigForRank(caster.Rank)
	radius := cfg["radius"]
	if radius <= 0 || intendedAmount <= 0 {
		return
	}
	radiusSq := radius * radius

	src := DamageSource{
		AttackerUnitID: caster.ID,
		Kind:           "divine_judgement",
		DamageType:     DamageHoly,
	}

	for _, enemy := range s.Units {
		if enemy == nil || enemy.HP <= 0 || !enemy.Visible {
			continue
		}
		if !s.playersAreHostileLocked(caster.OwnerID, enemy.OwnerID) {
			continue
		}
		dx := enemy.X - healed.X
		dy := enemy.Y - healed.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Play per-enemy effect FIRST (anchors before damage in case the
		// hit kills the enemy — same pattern as fireFinalExposureLocked).
		s.applyDivineJudgementHitEffectLocked(def, enemy)
		// Tag this hit as a minor "holy" damage event so the client renders
		// it with the gold sideways-falling popup that other elemental
		// damage sources (fire, electric) use. Queued BEFORE the damage call
		// because the client's HP-delta loop pairs queued minor amounts with
		// this tick's HP loss — the value passed must match the amount the
		// damage call will actually subtract from the enemy. Routes through
		// recordMinorDamageHitLocked so a single helper owns the visual
		// classification for every elemental ancillary source.
		s.recordMinorDamageHitLocked(enemy, intendedAmount, "holy")
		// Damage routes through the canonical pipeline so mitigation,
		// shared pain, death attribution, etc. all work transparently.
		s.applyUnitDamageWithSourceLocked(enemy, intendedAmount, src)
	}
}

// applyDivineJudgementHitEffectLocked is the per-enemy visual hook called for
// every enemy caught in a Divine Judgement detonation. Reads def.Effect from
// the perk JSON; if effect.name is non-empty, queues that effect anchored to
// the enemy with the configured size/duration/variant.
//
// EXTENSION POINT — author a new effect name in catalog/.../perks/gold.json's
// divine_judgement.effect.name to make a sprite appear on every splash hit.
// No code changes are needed to wire it up; this helper looks it up by name
// every tick. If a more complex per-hit reaction is wanted later (debuff
// stamp, AI behavior, …) extend this function rather than scattering hooks
// in fireDivineJudgementLocked above.
//
// Caller holds s.mu write lock.
func (s *GameState) applyDivineJudgementHitEffectLocked(def *PerkDef, enemy *Unit) {
	if def == nil || def.Effect == nil || def.Effect.Name == "" || enemy == nil {
		return
	}
	size := def.Effect.SizeScale
	if size <= 0 {
		size = 1.0
	}
	duration := def.Effect.DurationSeconds
	if duration <= 0 {
		duration = 0.6
	}
	s.queueEffectLocked(def.Effect.Name, enemy.ID, enemy.X, enemy.Y, size, duration, def.Effect.Variant)
}

// ─────────────────────────────────────────────────────────────────────────────
// GOLD — Invulnerability gate (damage pipeline)
//
// consumeInvulnerabilityLocked reports whether the target is currently inside
// an invulnerability window. The name says "consume" for symmetry with
// consumeDivineAegisLocked, but invulnerability is TIME-based, not charge-
// based: the field is NOT cleared here. Multiple hits during the window all
// short-circuit to "blocked" until the timer decays to 0 in state.go.
//
// Centralised in this file because divine_intervention is currently the sole
// producer of the field; future temp-invuln perks just stamp the same field
// and inherit this gate for free.
//
// Caller holds s.mu (read or write).
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) consumeInvulnerabilityLocked(target *Unit) bool {
	if target == nil {
		return false
	}
	return target.PerkState.InvulnerabilityRemaining > 0
}

// ─────────────────────────────────────────────────────────────────────────────
// GOLD — Divine Intervention (death prevention)
//
// tryDivineInterventionLocked is called by the damage pipeline at the moment
// a unit's HP would drop to (or below) 0. It scans nearby allied clerics
// (slice order — deterministic) for one with divine_intervention off
// cooldown, and if found:
//
//   1. Restores the target's HP to healAmount (clamped to MaxHP).
//   2. Stamps InvulnerabilityRemaining on the target so the brief follow-up
//      window cannot kill them via a second hit this tick.
//   3. Arms the saving cleric's cooldown.
//   4. Plays the divine_intervention VFX anchored on the saved unit.
//
// Returns true when a save fired (the damage pipeline then skips enqueueDeath).
//
// Recursion safety: the HP restore does NOT go through applyClericHealLocked.
// Intervention is an emergency save and intentionally does not trigger Beacon
// (no "death prevented → all allies healed" cascade) or Judgement (no AoE
// around a freshly-revived unit, which would risk dragging more allies into
// danger). The protection field is invulnerability rather than a hit-absorb
// so a same-tick second hit cannot land before the save can register.
//
// Caller holds s.mu write lock.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) tryDivineInterventionLocked(target *Unit) bool {
	if target == nil {
		return false
	}
	def := perkDefByID("divine_intervention")
	if def == nil {
		return false
	}
	for _, saver := range s.Units {
		if saver == nil || saver.ID == target.ID {
			continue
		}
		if saver.HP <= 0 || !saver.Visible {
			continue
		}
		if !s.unitsFriendlyLocked(saver, target) {
			continue
		}
		if !containsString(saver.PerkIDs, "divine_intervention") {
			continue
		}
		if saver.PerkState.DivineInterventionCooldownRemaining > 0 {
			continue
		}
		cfg := def.ConfigForRank(saver.Rank)
		radius := cfg["triggerRadius"]
		if radius <= 0 {
			continue
		}
		dx := saver.X - target.X
		dy := saver.Y - target.Y
		if dx*dx+dy*dy > radius*radius {
			continue
		}

		// Eligible saver found — fire intervention.
		healAmount := int(math.Round(cfg["healAmount"]))
		if healAmount < 1 {
			healAmount = 1
		}
		target.HP = healAmount
		if target.HP > target.MaxHP {
			target.HP = target.MaxHP
		}
		// Brief invulnerability window so a same-tick follow-up cannot kill
		// the just-saved unit. The damage pipeline early-outs on this field
		// at the very top of applyUnitDamageWithSourceLocked.
		if dur := cfg["protectionDurationSeconds"]; dur > target.PerkState.InvulnerabilityRemaining {
			target.PerkState.InvulnerabilityRemaining = dur
		}
		// Arm the saving cleric's cooldown. Multiple clerics with the perk
		// each have their own gate — only the first eligible one this tick
		// pays the cooldown.
		saver.PerkState.DivineInterventionCooldownRemaining = cfg["cooldownSeconds"]
		// VFX anchored on the saved unit.
		s.playEffectOnUnitLocked(target, "divine_intervention")
		return true
	}
	return false
}
