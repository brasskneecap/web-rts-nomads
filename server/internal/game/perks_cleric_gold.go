package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// CLERIC GOLD PERKS — Divine Intervention, Beacon of Life, Divine Judgement.
//
// All three perks key off the heal pipeline rather than a per-tick aura, and
// they interact: a primary heal may splash via Beacon, and each landing heal
// (primary or splash) may detonate Divine Judgement. To keep that interaction
// safe AND extensible, every cleric heal funnels through applyClericHealLocked
// with a HealMeta value that flags which downstream triggers are allowed.
//
// Divine Intervention is the odd one out: it fires from the death pipeline,
// not the heal pipeline. The would-die hook lives in applyUnitDamageWithSourceLocked
// just before enqueueDeathLocked so a successful save never queues a death.
//
// EXTENSION POINTS (intentional seams future code can plug into):
//   - HealMeta: add new flags for future heal-trigger perks; older sites
//     using the zero value automatically opt out, which keeps additions safe.
//   - applyDivineJudgementHitEffectLocked: per-enemy hit effect; today reads
//     the perk's `effect` JSON field. Authors a sprite for the splash by
//     setting effect.name in gold.json without touching code.
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
// applyClericHealLocked — central heal helper
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
// Beacon of Life — splash heal
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
// Divine Judgement — AoE holy damage on heal
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
// Invulnerability gate (damage pipeline)
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
// Divine Intervention — death prevention
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
