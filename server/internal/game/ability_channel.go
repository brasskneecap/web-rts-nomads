package game

import (
	"math"

	"webrts/server/pkg/protocol"
)

// Channeled-ability lifecycle.
//
// A channeled ability (channelSpecFor(def) ok) differs from a one-shot cast
// in that it persists across multiple ticks, applying its effect on a
// cadence (TickIntervalSeconds) until one of the stop conditions is met. The
// Siphon Life ability is the first in-game channeled ability.
//
// Flow:
//   beginAbilityChannelLocked  — validate, lock caster, spawn Beam, start
//                                ChannelNextTickIn = 0 so first tick fires
//                                immediately.
//   tickUnitChannelLocked      — per-tick: re-validate target, decrement
//                                ChannelNextTickIn, fire effect when <= 0.
//   stopUnitChannelLocked      — records a failure reason, despawns beam,
//                                clears channel state.
//   clearUnitChannelLocked     — same but no reason (caster died).
//
// Channels are mutually exclusive with one-shot casts: a unit may have
// either CastAbilityID or ChannelAbilityID set, never both. The entry point
// (beginAbilityCastLocked in ability_cast.go) branches on
// def.IsChannelAbility() so existing callers need no change.
//
// Cancel triggers (any of these → stopUnitChannelLocked):
//   - Caster dies (HP <= 0) → clearUnitChannelLocked (no UI reason needed).
//   - Target nil / HP≤0 / Visible=false / wrong team → castFailTargetLost.
//   - Target out of range → castFailTargetLost.
//   - Caster cannot afford next tick's mana → castFailNotEnoughMana.
//   - New order issued (move, attack, stop) → "Order issued"
//     (via resetUnitMovementLocked in state_movement.go).
//   - Caster stunned → "Channel interrupted."
//
// ── composable migration ────────────────────────────────────────────────────
// siphon_life is migrated to schemaVersion 2 like the ten abilities before
// it, following the SAME "keep the legacy runtime, delegate to it" shape as
// launch_projectile/launch_vortex/charge_fire_volley: every function below
// (beginAbilityChannelLocked, tickUnitChannelLocked, the stop/clear helpers,
// the four Siphoner perk hooks, distributeSiphonHealLocked's self/ally/
// dark_renewal rule) is UNCHANGED runtime. What changes is WHERE the
// mechanic config comes from:
//
//   - channelSpec is the resolved, effective config every function below
//     reads instead of a raw AbilityDef's channel fields directly.
//     channelSpecFor resolves it from either a legacy def's flat fields
//     (unchanged) or, for a converted (SchemaVersion>=2) def, from the
//     compiled Program's channel_beam action config — never from the
//     converted def's own (cleared, per ConvertLegacyAbility) flat fields.
//     Mirrors chargeFireSpec/chargeFireSpecFor (spell_charge.go) exactly.
//   - startChannelLocked is the extracted MECHANICAL start step (state
//     fields, cast lock, beam spawn) with NO validation of its own — shared
//     by the legacy direct call in beginAbilityChannelLocked below and the
//     channel_beam action's Execute (ability_exec_channel.go) for a
//     converted ability.
//
// ── THE ORDERING DECISION (why the dispatch lives INSIDE
// beginAbilityChannelLocked, not in cast RESOLUTION) ────────────────────────
// Every other migration's action fires from resolveAbilityProgramCastLocked
// (cast RESOLUTION, well after mana/GCD/cooldown have already been
// committed by beginAbilityCastLocked). Channel-start cannot use that same
// seam: beginAbilityChannelLocked is not a "resolve a completed cast" step,
// it IS the begin-time gate (ownership → busy → GCD → target → range → mana-
// for-first-tick), and it also ARMS the per-ability cooldown and the global
// cooldown itself. If beginAbilityCastLocked instead let a channel-shaped
// ability fall through into the normal one-shot path (mana check, GCD arm,
// cooldown arm, then — since siphon_life's castTime is 0 — an immediate
// resolveAbilityCastLocked call) and the channel actually STARTED from
// inside that resolution step, beginAbilityChannelLocked's own GCD guard
// (`caster.GlobalCooldownRemaining > 0`) would immediately reject the start
// it was just asked to perform, because the outer path already armed the
// GCD before resolving. Rather than special-case that self-inflicted
// conflict, beginAbilityCastLocked keeps its ORIGINAL structural position:
// a begin-time branch, evaluated BEFORE any one-shot-cast plumbing runs (see
// that function), just re-keyed on def.IsChannelAbility() instead of the
// now-cleared def.ChannelType field. beginAbilityChannelLocked owns 100% of
// the gating exactly as it did pre-migration; only the COMMIT step at the
// end dispatches through the executor for a converted ability, mirroring
// fireChargeFullLocked's gate-then-dispatch split (spell_charge.go) — the
// gate (everything above) runs first and unconditionally; the trigger firing
// after it is unconditional too.

// channelInterruptedStun is the failure reason recorded when a stun stops a
// channel. Kept here (not in ability_cast.go) since it is channel-specific.
const channelInterruptedStun = "Channel interrupted."

// channelInterruptedOrder is the failure reason recorded when a new player
// order (move / attack / stop / etc.) cancels an active channel.
const channelInterruptedOrder = "Order issued."

// channelMaxTicksPerUpdate caps how many channel effect ticks fire in one
// Update() call when dt is unusually large. Prevents pathological dt
// explosions from applying dozens of ticks in a single frame.
const channelMaxTicksPerUpdate = 4

// channelSpec is the resolved, effective channel configuration every
// function in this file reads instead of a raw AbilityDef's channel fields
// directly — see the file doc comment's "composable migration" section and
// chargeFireSpec (spell_charge.go) for the identical rationale/shape.
type channelSpec struct {
	ChannelType         string
	TickIntervalSeconds float64
	ManaCostPerTick     int
	DamagePerTick       int
	HealingMultiplier   float64
	AllyHealRadius      float64
}

// channelSpecFor resolves def's channel configuration, if any. For a legacy
// (SchemaVersion<2) def this reads the flat mechanic fields directly
// (ChannelType != "" gates it, same condition as always). For a converted
// (SchemaVersion>=2, Program!=nil) def it recovers the same magnitudes from
// the compiled on_cast_complete trigger's channel_beam action config
// instead — see compileChannelBeamAction (ability_compile.go). Pure; no lock
// needed. Mirrors chargeFireSpecFor (spell_charge.go) exactly.
func channelSpecFor(def AbilityDef) (channelSpec, bool) {
	if def.ChannelType != "" {
		return channelSpec{
			ChannelType:         def.ChannelType,
			TickIntervalSeconds: def.TickIntervalSeconds,
			ManaCostPerTick:     def.ManaCostPerTick,
			DamagePerTick:       def.DamagePerTick,
			HealingMultiplier:   def.HealingMultiplier,
			AllyHealRadius:      def.AllyHealRadius,
		}, true
	}
	if def.SchemaVersion >= 2 && def.Program != nil {
		if cfg, ok := findChannelBeamConfig(def.Program); ok {
			return channelSpec{
				ChannelType:         cfg.ChannelType,
				TickIntervalSeconds: cfg.TickIntervalSeconds,
				ManaCostPerTick:     cfg.ManaCostPerTick,
				DamagePerTick:       cfg.DamagePerTick,
				HealingMultiplier:   cfg.HealingMultiplier,
				AllyHealRadius:      cfg.AllyHealRadius,
			}, true
		}
	}
	return channelSpec{}, false
}

// findChannelBeamConfig walks prog's top-level triggers for an
// on_cast_complete trigger with a CHANNELED beam action (a beam whose config
// Channeled == true) and decodes its config. Also the seam
// AbilityDef.IsChannelAbility() uses to recognize a converted channel ability
// by its Program's SHAPE (never by a cleared flat field) — mirrors
// findChargeFireVolleyConfig (spell_charge.go). The Channeled flag is what
// distinguishes siphon_life's channel-start beam from chain_lightning's
// momentary bounce beams, which are the same ActionBeam type.
func findChannelBeamConfig(prog *AbilityProgram) (beamConfig, bool) {
	if prog == nil {
		return beamConfig{}, false
	}
	for _, trig := range prog.Triggers {
		if trig.Type != TriggerOnCastComplete {
			continue
		}
		for _, act := range trig.Actions {
			if act.Type != ActionBeam {
				continue
			}
			var cfg beamConfig
			decodeActionConfig(act.Config, &cfg)
			if !cfg.Channeled {
				continue
			}
			return cfg, true
		}
	}
	return beamConfig{}, false
}

// fireChannelBeamTickLocked fires a converted (SchemaVersion>=2) channel
// ability's compiled on_beam_tick trigger for exactly one tick — the
// authored, per-tick DAMAGE step (a single deal_damage action against the
// channel's one target, compileChannelBeamTickTrigger in ability_compile.go)
// — and returns the total damage it actually applied
// (ctx.lastAppliedDamage), clamped to >= 0. tickUnitChannelLocked reads this
// return value as tickDamage, driving heal distribution and every Siphoner
// perk hook off it exactly as it already does for a legacy-computed
// tickDamage — the channel LIFECYCLE stays entirely in Go; only this one
// number is authored.
//
// ctx.damageEffectivenessMultiplier is set to damageMult (the caller's
// mods.DamageMult) so deal_damage folds the caster's Siphoner perk scaler
// exactly ONCE, through the same ctx.effectiveDamageMultiplier() seam every
// other deal_damage call uses — see RuntimeAbilityContext.lastAppliedDamage's
// doc comment (ability_exec.go) and dealDamageConfig's Execute
// (ability_program_registry.go) for the fold-once arithmetic
// (round(DamagePerTick * mods.DamageMult), byte-identical to the legacy
// inline computation this replaces since effectiveAbilityDamageLocked is
// identity for siphon_life and FlatOffset is 0).
//
// Returns 0 (no damage) if def's Program has no compiled on_beam_tick
// trigger at all — an authoring gap degrades to "no damage this tick",
// mirroring what an absent legacy DamagePerTick would do.
//
// Caller holds s.mu write lock.
func (s *GameState) fireChannelBeamTickLocked(caster, target *Unit, def AbilityDef, damageMult float64) int {
	cfg, ok := findChannelBeamConfig(def.Program)
	if !ok {
		return 0
	}
	var actions []AbilityActionDef
	for _, trig := range cfg.Triggers {
		if trig.Type == TriggerOnTick {
			actions = trig.Actions
			break
		}
	}
	if len(actions) == 0 {
		return 0
	}

	ctx := &RuntimeAbilityContext{
		CasterID:                      caster.ID,
		AbilityID:                     def.ID,
		InitialTarget:                 target.ID,
		CurrentEventUnitID:            target.ID,
		program:                       def.Program,
		abilityDef:                    &def,
		Named:                         map[string]ContextValue{},
		Trace:                         s.previewTrace,
		now:                           s.previewClock,
		damageEffectivenessMultiplier: damageMult,
	}
	path := "on_tick"
	for i := range actions {
		if ctx.opsExhausted() {
			break
		}
		s.executeActionLocked(ctx, &actions[i], path)
	}
	if ctx.lastAppliedDamage < 0 {
		return 0
	}
	return ctx.lastAppliedDamage
}

// startChannelLocked performs the MECHANICAL channel-start step only: state
// fields, the cast lock, and the beam spawn. Every gating decision
// (ownership, busy, GCD, mana-for-first-tick, range) has ALREADY run in
// beginAbilityChannelLocked by the time this is called — it validates
// nothing itself. Shared by the legacy direct call below and the
// channel_beam action's Execute (ability_exec_channel.go) for a converted
// ability, mirroring how spawnArcaneOrbLocked is the shared mechanical seam
// both legacy and launch_vortex's Execute call.
//
// Caller holds s.mu write lock.
func (s *GameState) startChannelLocked(caster, target *Unit, abilityID string, spec channelSpec) {
	// Set channel state. ChannelNextTickIn starts at TickIntervalSeconds so
	// the first effect tick fires after one full interval has elapsed. This
	// prevents a double-fire on the very first Update call when dt exactly
	// equals the interval (decrement would land exactly on 0, triggering the
	// loop a second time).
	caster.ChannelAbilityID = abilityID
	caster.ChannelTargetID = target.ID
	caster.ChannelTickInterval = spec.TickIntervalSeconds
	caster.ChannelNextTickIn = spec.TickIntervalSeconds

	// Lock movement and set "Casting" animation — reuses the one-shot cast
	// primitive so existing movement / combat guards treat this unit as busy.
	// Then upgrade the status string to "Channeling" so the client can pin
	// the sprite to ChannelHoldFrame instead of cycling the casting cycle.
	// The combat-tick guard in state_combat.go re-derives the right status
	// each tick (Channeling when ChannelAbilityID is set, else Casting).
	s.beginUnitCastingLocked(caster)
	caster.Status = unitStatusChanneling

	// Spawn the beam visual entity.
	s.spawnBeamLocked(caster, target, abilityID, abilityID) // variant = abilityID
}

// beginAbilityChannelLocked starts a channel on caster for the named ability
// targeting target. Returns (true, "") when the channel has started;
// (false, reason) on a synchronous failure.
//
// Validation order mirrors beginAbilityCastLocked:
//   ownership → not-busy → target legality → range → mana-for-first-tick.
//
// Caller holds s.mu write lock.
func (s *GameState) beginAbilityChannelLocked(caster *Unit, abilityID string, target *Unit) (bool, string) {
	if caster == nil || caster.HP <= 0 {
		return false, castFailInvalidTarget
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return s.failCastLocked(caster, castFailUnknownAbility)
	}
	spec, ok := channelSpecFor(def)
	if !ok {
		// Reached only via beginAbilityCastLocked's own IsChannelAbility()
		// gate, which uses this same resolver — should never fail here.
		// Defensive fallback (e.g. a content edit racing a live cast).
		return s.failCastLocked(caster, castFailUnknownAbility)
	}
	if !containsAbility(caster, abilityID) {
		return s.failCastLocked(caster, castFailNotOwned)
	}
	// Cannot start a channel while a one-shot cast OR another channel is in
	// progress.
	if caster.CastAbilityID != "" || caster.ChannelAbilityID != "" {
		return s.failCastLocked(caster, castFailAlreadyCasting)
	}
	if caster.GlobalCooldownRemaining > 0 {
		return s.failCastLocked(caster, castFailGlobalCooldown)
	}
	if !s.canAbilityTargetUnitLocked(def, caster, target) {
		return s.failCastLocked(caster, castFailInvalidTarget)
	}
	if !def.WithinCastRange(caster, target) {
		return s.failCastLocked(caster, castFailOutOfRange)
	}
	// Require mana for at least the first tick so the channel doesn't start
	// on a caster that is already empty. Reads spec.ManaCostPerTick, never
	// def.ManaCostPerTick directly — that field is cleared on a converted
	// (SchemaVersion>=2) ability, see channelSpecFor.
	if caster.CurrentMana < spec.ManaCostPerTick {
		return s.failCastLocked(caster, castFailNotEnoughMana)
	}

	caster.LastCastFailure = ""

	// Defensively clear any stale one-shot cast state (should be empty due to
	// the guard above, but be safe against future refactor paths).
	s.clearUnitCastLocked(caster)

	// Arm cooldown at channel START, matching the one-shot cast convention so
	// both manual and auto-cast paths share the same gate. Cooldown/CastTime
	// are cast-setup fields that survive conversion untouched (see
	// ConvertLegacyAbility), so def.EffectiveCooldown() needs no spec
	// resolution.
	cdDuration := def.EffectiveCooldown()
	if cdDuration > 0 {
		if caster.AbilityCooldowns == nil {
			caster.AbilityCooldowns = make(map[string]float64, 1)
		}
		caster.AbilityCooldowns[abilityID] = cdDuration
	}
	armAbilityGlobalCooldownLocked(caster)

	// Fire on_cast_start (composable-only) now that every gate has passed
	// and cooldown/GCD are armed — mirrors the unit-target/point cast entry
	// points; see fireCastStartTriggerLocked (ability_cast.go) for the full
	// ordering rationale and the unpaired-on-interrupt hazard it accepts.
	s.fireCastStartTriggerLocked(caster, def, target, protocol.Vec2{})

	// Composable (schemaVersion>=2) abilities dispatch the mechanical start
	// step (startChannelLocked) through the executor's channel_beam action
	// instead of calling it directly, so the migrated Program is the real
	// runtime path — not a shape kept alive only for describe/shape parity.
	// Every gating decision above has ALREADY run; the trigger fires
	// unconditionally, mirroring fireChargeFullLocked's gate-then-dispatch
	// split (spell_charge.go). See the file doc comment's ORDERING section
	// for why this dispatch lives HERE and not in cast resolution.
	if def.SchemaVersion >= 2 && def.Program != nil {
		ctx := &RuntimeAbilityContext{
			CasterID:      caster.ID,
			AbilityID:     def.ID,
			InitialTarget: target.ID,
			program:       def.Program,
			abilityDef:    &def,
			Named:         map[string]ContextValue{},
			Trace:         s.previewTrace,
			now:           s.previewClock,
		}
		s.runProgramTriggersLocked(ctx, def.Program.Triggers, TriggerOnCastComplete)
	} else {
		s.startChannelLocked(caster, target, abilityID, spec)
	}

	return true, ""
}

// tickUnitChannelLocked advances an active channel by dt seconds. No-op for a
// unit that is not channeling. Re-validates the target by ID every tick per
// the ID-not-pointer invariant. Fires the channel effect (damage + siphon
// heal) for each elapsed tick interval.
//
// Caller holds s.mu write lock.
func (s *GameState) tickUnitChannelLocked(unit *Unit, dt float64) {
	if unit == nil || unit.ChannelAbilityID == "" {
		return
	}
	def, ok := getAbilityDef(unit.ChannelAbilityID)
	if !ok {
		// Unknown ability — clear silently, no reason to surface.
		s.clearUnitChannelLocked(unit)
		return
	}
	spec, ok := channelSpecFor(def)
	if !ok {
		// Ability is no longer channel-shaped (e.g. a content edit racing a
		// live channel) — clear silently, same as the unknown-ability case.
		s.clearUnitChannelLocked(unit)
		return
	}

	// Caster died — just clear bookkeeping (unit is being removed anyway).
	if unit.HP <= 0 {
		s.clearUnitChannelLocked(unit)
		return
	}

	// Stun interrupts the channel.
	if unit.StunnedRemaining > 0 {
		s.stopUnitChannelLocked(unit, channelInterruptedStun)
		return
	}

	// Re-resolve and validate the target every tick (ID-not-pointer rule).
	target := s.getUnitByIDLocked(unit.ChannelTargetID)
	if !s.isValidChannelTargetLocked(unit, def, target) {
		s.stopUnitChannelLocked(unit, castFailTargetLost)
		return
	}

	// Decrement the timer and fire as many ticks as have elapsed.
	unit.ChannelNextTickIn -= dt

	safetyCounter := 0
	for unit.ChannelNextTickIn <= 0 {
		if safetyCounter >= channelMaxTicksPerUpdate {
			// Pathological dt — skip remaining ticks this Update and let
			// the next call catch up normally.
			break
		}
		safetyCounter++

		// Aggregate every perk-authored abilityModifiers entry targeting this
		// channel ability (e.g. Soul Leech / Beam Mastery on siphon_life):
		// damage, healing, mana cost, and range scalers all in one struct.
		// Defaults to identity when the caster owns no matching perk. Mana
		// cost is computed from the scaled value with a floor of 0 — a heavy
		// discount could in theory floor mana cost.
		mods := s.abilityScalarModifiersForCasterLocked(unit, def.ID)
		tickManaCost := int(math.Round(float64(spec.ManaCostPerTick) * mods.ManaCostMult))
		if tickManaCost < 0 {
			tickManaCost = 0
		}

		// Check mana for this tick (scaled cost).
		if unit.CurrentMana < tickManaCost {
			s.stopUnitChannelLocked(unit, castFailNotEnoughMana)
			return
		}
		if !s.spendUnitManaLocked(unit, tickManaCost) {
			// Should not happen after the check above, but be safe.
			s.stopUnitChannelLocked(unit, castFailNotEnoughMana)
			return
		}

		// Per-tick DAMAGE: for a converted (SchemaVersion>=2) def, the amount
		// is authored (on_beam_tick/deal_damage, see fireChannelBeamTickLocked
		// and this file's "composable migration" doc comment); for a legacy
		// def it stays the original inline computation, byte-for-byte
		// unchanged. Either way tickDamage lands as the SAME single number
		// the heal + perk hooks below read.
		var tickDamage int
		if def.SchemaVersion >= 2 && def.Program != nil {
			tickDamage = s.fireChannelBeamTickLocked(unit, target, def, mods.DamageMult)
		} else {
			tickDamage = int(math.Round(float64(spec.DamagePerTick) * mods.DamageMult))
			if tickDamage < 0 {
				tickDamage = 0
			}

			// Apply damage to the target through the central pipeline so
			// mitigation, the death pipeline, threat, and determinism all apply.
			// Color of the resulting floating-up popup is driven automatically
			// by applyUnitDamageWithSourceLocked → recordDamageTypeHintLocked
			// off DamageSource.DamageType, so siphon_life ticks naturally render
			// dark-purple without any per-callsite plumbing here.
			if tickDamage > 0 {
				s.applyUnitDamageWithSourceLocked(target, tickDamage, DamageSource{
					AttackerUnitID:  unit.ID,
					Kind:            "ability",
					Category:        DamageCategoryAbility,
					DamageType:      def.DamageType.OrPhysical(),
					SourceAbilityID: def.ID,
				})
			}
		}

		// Compute heal amount from the SAME tickDamage (so Soul Leech's
		// damage multiplier feeds through proportionally) and distribute
		// via siphon heal logic. HealingMultiplier (ability-level) stacks
		// multiplicatively with mods.HealMult (perk-level).
		healAmount := int(math.Round(float64(tickDamage) * spec.HealingMultiplier * mods.HealMult))
		if healAmount > 0 {
			s.distributeSiphonHealLocked(unit, healAmount, spec.AllyHealRadius)
		}

		// ── Silver Siphoner perks ──────────────────────────────────────────
		// Chain Siphon: fire secondary beams to up to N nearby enemies. Damage
		// scales by secondaryDamageMultiplier of the primary tick; heal scales
		// by secondaryHealingMultiplier of healAmount and routes back through
		// distributeSiphonHealLocked so dark_renewal can also catch chain
		// overflow. No-op when the perk isn't owned. Fires even on a killing
		// primary tick — chain victims still get hit before target.HP=0 is
		// drained, which feels right ("the beam fans out even as the primary
		// dies"). The channel ability id is threaded through so the spawned
		// chain beams carry the same AbilityID on the wire as the primary,
		// keeping the client's per-ability beam dispatch consistent.
		s.applyChainSiphonBeamsLocked(unit, target, tickDamage, healAmount, spec.AllyHealRadius, unit.ChannelAbilityID)

		// Shared Suffering (and any FUTURE rider on siphon_life's on_beam_tick
		// trigger) — data-driven via PerkDef.AbilityRiders, migrated off the
		// bespoke applySharedSufferingLocked helper (see
		// docs/superpowers/plans/2026-07-19-perk-ability-riders-tier-b.md
		// Task 4). No-op when the caster owns no matching rider. The rider's
		// own select_targets query reproduces the old helper's target set
		// (visible hostiles within radius of the primary target, primary
		// excluded) and deal_damage echoes a fraction of tickDamage (bound as
		// ctx.Named["trigger_damage"]) tagged DamageSource.Kind="ability"
		// (was "shared_suffering" — a debug-label-only change, see the perk
		// JSON and the migration characterization test for the accepted
		// deltas: no VFX, no minor-popup split, ascended_corruption's overlay
		// temporarily inert).
		s.runAbilityRidersForCasterLocked(unit, target, unit.ChannelAbilityID, TriggerOnTick, tickDamage)

		// Withering Beam — accrue continuous-siphon time and stamp stacks
		// on the target every secondsPerStack. Runs after damage so a
		// killing tick doesn't also stamp on a corpse (the helper checks
		// target.HP, but applyUnitDamageWithSourceLocked may have already
		// removed the unit via the pending-death drain on the next tick).
		s.tickWitheringBeamChannelLocked(unit, target, unit.ChannelTickInterval)

		// Advance the timer for the next tick.
		unit.ChannelNextTickIn += unit.ChannelTickInterval
	}

	// Re-validate the target after the loop in case tick damage killed it.
	// Re-resolve so we get the fresh HP.
	target = s.getUnitByIDLocked(unit.ChannelTargetID)
	if !s.isValidChannelTargetLocked(unit, def, target) {
		s.stopUnitChannelLocked(unit, castFailTargetLost)
	}
}

// isValidChannelTargetLocked reports whether target is a legal, in-range
// enemy for the channel. Consolidates the nil / HP / Visible / ownership /
// range guards so they are expressed once and applied at both the loop entry
// and exit.
//
// Range check uses the channel's range scaler (per-caster perk modifiers
// like beam_mastery on Siphon Life). For abilities with no scaler in play
// the multiplier collapses to 1.0 and the check is identical to
// def.WithinCastRange.
//
// Caller holds s.mu (read or write).
func (s *GameState) isValidChannelTargetLocked(caster *Unit, def AbilityDef, target *Unit) bool {
	if target == nil || target.HP <= 0 || !target.Visible {
		return false
	}
	if !s.canAbilityTargetUnitLocked(def, caster, target) {
		return false
	}
	rangeMult := s.channelRangeMultiplierForCasterLocked(caster, def)
	if !def.WithinCastRangeScaled(caster, target, rangeMult) {
		return false
	}
	return true
}

// channelRangeMultiplierForCasterLocked returns the cast-range scaler the
// caster's perks apply to this channel ability, via the generic
// abilityModifiers aggregator (e.g. beam_mastery's rangeMult on
// siphon_life). An ability whose caster has no matching modifiers returns
// 1.0, so this is safe and generic across any channel ability — no
// per-ability gating needed here.
//
// Caller holds s.mu (read or write).
func (s *GameState) channelRangeMultiplierForCasterLocked(caster *Unit, def AbilityDef) float64 {
	if caster == nil {
		return 1.0
	}
	return s.abilityScalarModifiersForCasterLocked(caster, def.ID).RangeMult
}

// stopUnitChannelLocked records reason on the unit (for async/UI surfacing),
// despawns the caster's beam, clears channel state, and releases the cast
// lock. Use this when the channel ends with a player-visible reason (target
// lost, not enough mana, new order, stun). Use clearUnitChannelLocked when
// the caster died (no reason to surface).
//
// Caller holds s.mu write lock.
func (s *GameState) stopUnitChannelLocked(unit *Unit, reason string) {
	if unit == nil {
		return
	}
	unit.LastCastFailure = reason
	s.removeBeamForUnitLocked(unit.ID)
	s.clearChannelStateLocked(unit)
}

// clearUnitChannelLocked clears channel bookkeeping and releases the cast
// lock without recording a failure reason. Use when the caster died — no
// UI feedback is needed.
//
// Caller holds s.mu write lock.
func (s *GameState) clearUnitChannelLocked(unit *Unit) {
	if unit == nil {
		return
	}
	s.removeBeamForUnitLocked(unit.ID)
	s.clearChannelStateLocked(unit)
}

// clearChannelStateLocked zeroes the channel fields and calls endUnitCastingLocked.
// Internal helper shared by stop and clear.
//
// Caller holds s.mu write lock.
func (s *GameState) clearChannelStateLocked(unit *Unit) {
	if unit == nil {
		return
	}
	// Repurposed Life — if this Siphoner's channel is ending because the
	// channel target just died (Siphoner's own killing tick is the most
	// common case), fire the mana restore BEFORE clearing the channel
	// fields. The drainPendingDeathsLocked path catches the parallel case
	// where an ally landed the killing blow while the channel was still
	// running, but THAT path can't see this case because the channel auto-
	// stops at the post-validate inside tickUnitChannelLocked before the
	// drain runs. Without this hook here, repurposed_life would silently
	// miss every kill the Siphoner delivers themselves.
	//
	// Gated on (siphon_life channel + target died this tick + perk owned)
	// so unrelated channel stops (mana out, interrupt, target invisible,
	// caster died but target lived) don't fire spuriously. target.HP <= 0
	// is the precise "the channel ended because they died" signal — every
	// other stop reason leaves the target alive (or removes the target via
	// FoW, in which case getUnitByIDLocked returns nil and we skip).
	if unit.ChannelAbilityID == siphonLifeAbilityID && unit.ChannelTargetID != 0 &&
		containsString(unit.PerkIDs, "repurposed_life") {
		if target := s.getUnitByIDLocked(unit.ChannelTargetID); target != nil && target.HP <= 0 {
			if def := perkDefByID("repurposed_life"); def != nil {
				s.fireRepurposedLifeManaRestoreLocked(unit, def)
			}
		}
	}
	unit.ChannelAbilityID = ""
	unit.ChannelTargetID = 0
	unit.ChannelTickInterval = 0
	unit.ChannelNextTickIn = 0
	// Withering Beam: zero the caster-side accumulator + tracking target so
	// a fresh channel starts cleanly. Stacks already on previously-siphoned
	// enemies keep decaying via the cross-unit loop in state.go.
	s.clearWitheringBeamCasterStateLocked(unit)
	// Chain Siphon: despawn every secondary beam the caster was rendering
	// against the now-departed primary. No-op for units without the perk.
	s.clearChainSiphonBeamsLocked(unit)
	s.endUnitCastingLocked(unit)
}

// channelLoopRangeForUnitLocked returns the (start, end) frame range the
// snapshot should ship for a channeling unit, or (0, 0) when the unit is
// not channeling or has no channel-pose configured. The client loops one-
// way through [start, end] inclusive on the casting sprite sheet while
// status == "Channeling". start == end produces a single held frame;
// start < end produces a small loop. Snapshot builders gate Status to
// "Channeling" so the client only reads this when it matters; (0, 0) is
// also a safe default (pin frame 0 if for any reason the lookup races a
// clear or the unit has no authored channel pose).
//
// Resolution order (visual data lives on the caster, not the ability — two
// units sharing one channel ability can pin different frames on their own
// sheets):
//   1. Path override:   pathChannelLoopFor(unit.ProgressionPath)
//   2. Unit def:        unitDefsByType[unit.UnitType].ChannelLoop
//   3. Fallback:        (0, 0)
//
// Caller holds s.mu (read or write).
func (s *GameState) channelLoopRangeForUnitLocked(unit *Unit) (start, end int) {
	if unit == nil || unit.ChannelAbilityID == "" {
		return 0, 0
	}
	// Path override wins when present.
	if unit.ProgressionPath != "" {
		if r, ok := pathChannelLoopFor(unit.ProgressionPath); ok {
			return r.Start, r.End
		}
	}
	// Fall back to the base unit def.
	if def, ok := getUnitDef(unit.UnitType); ok && def.ChannelLoop != nil {
		return def.ChannelLoop.Start, def.ChannelLoop.End
	}
	return 0, 0
}

// channelLoopStartForUnitLocked and channelLoopEndForUnitLocked are thin
// wrappers around channelLoopRangeForUnitLocked that return a single field
// each, so the snapshot struct literal can assign them inline without
// threading a local pair through three near-identical builders.
func (s *GameState) channelLoopStartForUnitLocked(unit *Unit) int {
	start, _ := s.channelLoopRangeForUnitLocked(unit)
	return start
}

func (s *GameState) channelLoopEndForUnitLocked(unit *Unit) int {
	_, end := s.channelLoopRangeForUnitLocked(unit)
	return end
}

// ── Auto-cast precondition ────────────────────────────────────────────────────

// siphonHealingNeededLocked reports whether the Siphon Life auto-cast
// precondition is met: the caster OR any ally within allyHealRadius has
// HP < MaxHP. Only when this returns true does the auto-cast loop consider
// starting the channel. This prevents wasteful mana drain when the whole team
// is at full health.
//
// Generic over any channeled ability with AllyHealRadius: if allyHealRadius
// is 0, only the caster's own HP is tested (no ally scan). Takes the resolved
// radius directly (not an AbilityDef) — callers resolve it via
// channelSpecFor first, since a converted (SchemaVersion>=2) ability's
// AllyHealRadius field is cleared (see channelSpecFor's doc comment).
//
// Caller holds s.mu (read or write).
func (s *GameState) siphonHealingNeededLocked(caster *Unit, allyHealRadius float64) bool {
	if caster == nil {
		return false
	}
	// Self is injured — healing is needed regardless of allies.
	if caster.HP < caster.MaxHP {
		return true
	}
	// Scan allies within allyHealRadius.
	if allyHealRadius <= 0 {
		return false
	}
	radiusSq := allyHealRadius * allyHealRadius
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if u.ID == caster.ID {
			continue
		}
		if !s.unitsFriendlyLocked(caster, u) {
			continue
		}
		if u.HP >= u.MaxHP {
			continue
		}
		if distanceSquared(caster.X, caster.Y, u.X, u.Y) <= radiusSq {
			return true
		}
	}
	return false
}

// ── Siphon heal distribution ──────────────────────────────────────────────────

// distributeSiphonHealLocked applies amount HP of healing according to the
// Siphon Life binary rule:
//
//   - If the Siphoner's HP < MaxHP: heal the Siphoner first (up to missing
//     HP). Any leftover with dark_renewal routes through the shield cascade
//     (self pool → ally pool → waste); without dark_renewal leftover is
//     simply wasted (heals never overheal-then-spill to ally HP).
//   - Else (Siphoner at full HP): with dark_renewal, the whole amount routes
//     through the shield cascade. Without dark_renewal, fall through to the
//     legacy ally-heal path (lowest-HP-percent ally within allyHealRadius).
//
// In all cases the HP heal routes through applyClericHealLocked so the
// existing cleric perk pipeline (beacon_of_life, divine_judgement) fires
// when the Siphoner is also a Cleric (future cross-path extension point).
// Returns the unit that received the heal, or nil if healing was wasted
// (no valid ally, no shielding banked, caster at full HP without
// dark_renewal and no ally in range).
//
// Caller holds s.mu write lock.
func (s *GameState) distributeSiphonHealLocked(siphoner *Unit, amount int, allyHealRadius float64) *Unit {
	if siphoner == nil || amount <= 0 {
		return nil
	}

	hasDarkRenewal := containsString(siphoner.PerkIDs, "dark_renewal")

	// Self-heal path: Siphoner is below max HP. Heal only what fits; with
	// dark_renewal route any overflow through the shield cascade rather
	// than re-attempting an ally heal.
	if siphoner.HP < siphoner.MaxHP {
		selfNeed := siphoner.MaxHP - siphoner.HP
		selfHeal := amount
		if selfHeal > selfNeed {
			selfHeal = selfNeed
		}
		s.applyClericHealLocked(siphoner, siphoner, selfHeal, healMetaPrimaryAbility())
		remaining := amount - selfHeal
		if remaining > 0 && hasDarkRenewal {
			s.applyDarkRenewalExcessLocked(siphoner, remaining, allyHealRadius)
		}
		return siphoner
	}

	// Siphoner is at full HP. dark_renewal overrides the legacy ally-heal
	// path — overflow becomes shielding on self (then on a nearby ally) per
	// the perk spec, never an ally HP heal. Without dark_renewal, fall
	// through to the existing lowest-HP-percent ally selector below.
	if hasDarkRenewal {
		if s.applyDarkRenewalExcessLocked(siphoner, amount, allyHealRadius) > 0 {
			return siphoner
		}
		return nil
	}

	// Ally path: find lowest-HP-percent ally within allyHealRadius.
	// Iterate s.Units (slice → deterministic order). Filter: same team, alive,
	// visible, not self, HP < MaxHP, within allyHealRadius.
	var best *Unit
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if u.ID == siphoner.ID {
			continue // self excluded from ally path
		}
		if !s.unitsFriendlyLocked(siphoner, u) {
			continue
		}
		if u.HP >= u.MaxHP {
			continue // at full HP — no benefit
		}
		// Range check using squared distance to avoid sqrt.
		if allyHealRadius > 0 {
			distSq := distanceSquared(siphoner.X, siphoner.Y, u.X, u.Y)
			if distSq > allyHealRadius*allyHealRadius {
				continue
			}
		}

		if best == nil {
			best = u
			continue
		}
		// Lower HP% wins. Integer cross-multiply avoids floats; int64 prevents
		// overflow for large HP values.
		lhs := int64(u.HP) * int64(best.MaxHP)
		rhs := int64(best.HP) * int64(u.MaxHP)
		switch {
		case lhs < rhs:
			best = u
		case lhs == rhs:
			// Tie-break: ascending unit.ID for determinism.
			if u.ID < best.ID {
				best = u
			}
		}
	}

	if best == nil {
		// No injured ally in radius — healing is wasted.
		// extension point: future perk may bank overflow or log telemetry here.
		return nil
	}

	s.applyClericHealLocked(siphoner, best, amount, healMetaPrimaryAbility())
	return best
}
