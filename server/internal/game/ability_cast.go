package game

import (
	"math"

	"webrts/server/pkg/protocol"
)

// Ability cast lifecycle.
//
// Flow:
//   beginAbilityCastLocked   — validate (target / range / mana / not busy),
//                              lock the caster, start the cast timer. Returns
//                              (false, reason) on a synchronous failure; the
//                              WS/action-bar layer turns `reason` into the
//                              existing protocol.NotificationMessage (same
//                              pattern as the "Not enough resources" path).
//   tickUnitCastLocked       — per-tick: re-resolve & re-validate the target
//                              by ID, count the timer down, resolve on 0, or
//                              cancel if the target became invalid.
//   resolveAbilityCastLocked — spend mana, apply the effect (Heal: clamped,
//                              no overheal), play the on-target effect.
//
// Cast is UNINTERRUPTIBLE by design decision: damage / CC do NOT cancel it.
// It only ends early if the caster dies or the target becomes invalid
// (dies / leaves range / wrong allegiance). A clear seam exists to add
// damage/CC interrupts later (see cancelUnitCastLocked call sites).
//
// Mana is checked at initiation and DEDUCTED ON COMPLETION (per spec), so an
// interrupted/cancelled cast costs nothing.

// Cast failure reasons. Stable, human-readable strings — the WS layer passes
// them straight into protocol.NotificationMessage.Message.
const (
	castFailUnknownAbility = "That ability is unavailable."
	castFailNotOwned       = "This unit can't cast that ability."
	castFailAlreadyCasting = "Already casting."
	castFailInvalidTarget  = "Invalid target."
	castFailOutOfRange     = "Target is out of range."
	castFailNotEnoughMana  = "Not enough mana."
	castFailTargetLost     = "Target lost." // async: target died / left range mid-cast
	castFailGlobalCooldown = "Not ready yet." // global cooldown from a recent cast still active
)

// abilityGlobalCooldownSeconds is the global cooldown (GCD) applied to a unit
// the instant it initiates ANY ability cast: no other ability — nor the same
// one — can be initiated until it elapses. This spaces a unit's abilities out so
// one with several ready spells casts them one after another instead of
// simultaneously. Applies to both manual and auto-cast. It is a floor, not a
// replacement for per-ability cooldowns (which are usually longer).
const abilityGlobalCooldownSeconds = 1.0

// armAbilityGlobalCooldownLocked starts the caster's global cooldown. Called at
// the commit point of every cast entry (one-shot, point, channel), parallel to
// where the per-ability cooldown is armed. Caller holds s.mu.
func armAbilityGlobalCooldownLocked(caster *Unit) {
	caster.GlobalCooldownRemaining = abilityGlobalCooldownSeconds
}

// containsAbility reports whether the unit has abilityID in its ability list.
func containsAbility(u *Unit, abilityID string) bool {
	for _, id := range u.Abilities {
		if id == abilityID {
			return true
		}
	}
	return false
}

// beginAbilityCastLocked attempts to start caster casting abilityID at target.
// Returns (true, "") when the cast has started (or, for a 0 cast-time
// ability, resolved immediately); otherwise (false, reason) where reason is
// one of the castFail* strings, suitable for direct player feedback.
//
// Validation order is deliberate so the feedback is the most relevant:
// ownership → not-busy → target legality → range → mana.
//
// Caller holds s.mu.
func (s *GameState) beginAbilityCastLocked(caster *Unit, abilityID string, target *Unit) (bool, string) {
	if caster == nil || caster.HP <= 0 {
		return false, castFailInvalidTarget
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return s.failCastLocked(caster, castFailUnknownAbility)
	}
	// Channeled abilities use a separate lifecycle. Route them here so that
	// the WS handler and auto-cast code paths need no change — they all call
	// beginAbilityCastLocked and this branch handles the dispatch
	// transparently. Keyed on def.IsChannelAbility() (not a raw
	// ChannelType != "" check) so a converted (SchemaVersion>=2) channel
	// ability is still recognized — see that method's doc comment. This
	// branch MUST stay here, before any one-shot-cast plumbing below (mana
	// check, GCD arm, cooldown arm) — see ability_channel.go's file doc
	// comment ("THE ORDERING DECISION") for why routing a channel-start
	// through cast RESOLUTION instead would double-run
	// beginAbilityChannelLocked's own gating.
	if def.IsChannelAbility() {
		return s.beginAbilityChannelLocked(caster, abilityID, target)
	}
	// Passive abilities (arcane_missiles) are never manually or auto-cast —
	// their effect is driven by their own system. Reject any cast attempt.
	if def.IsPassive() {
		return s.failCastLocked(caster, castFailNotOwned)
	}
	// Point-targeted abilities (arcane_orb) must go through the point-cast path
	// (beginAbilityCastAtPointLocked) — never the unit-target path, whose damage
	// step would mis-fire a homing bolt. RequestAbilityCast / auto-cast route
	// them correctly; this guards any stray unit-target call.
	if def.TargetsPoint {
		return s.failCastLocked(caster, castFailInvalidTarget)
	}
	if !containsAbility(caster, abilityID) {
		return s.failCastLocked(caster, castFailNotOwned)
	}
	if caster.CastAbilityID != "" {
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
	// Resolve the effective (modifier-folded) values for this cast. Mana,
	// cooldown, and cast time are all read from the EffectiveSpell, never the
	// raw def — so a perk/buff/item that reduces mana cost or cast time takes
	// effect here without any change to this gate. The base def is untouched.
	eff := s.effectiveSpellLocked(caster, def)
	if caster.CurrentMana < eff.ManaCost {
		return s.failCastLocked(caster, castFailNotEnoughMana)
	}

	caster.LastCastFailure = ""

	// Arm the cooldown at cast START so both manual and auto-cast paths share
	// the same gate. This is the single source of truth for cooldown timing;
	// auto-cast formerly armed cooldown post-cast in tickUnitAutoCastLocked,
	// which meant manual casts left the action button looking "ready" while
	// the unit was still mid-resolve. Arming on start also matches the
	// player's mental model: clicking the ability "spends" it immediately,
	// the wipe overlay starts ticking right away. An interrupted cast keeps
	// the cooldown (consistent with how every other RTS handles it).
	//
	// EffectiveCooldown() clamps the duration to max(Cooldown, CastTime), so a
	// spell like base Heal (1s cast / 0s authored cooldown) still produces a
	// visible 1s wipe while the unit is locked in place mid-cast — without
	// the clamp, the action button would silently flash with no countdown
	// even though the player can see the cast animation playing.
	cdDuration := eff.EffectiveCooldown()
	if cdDuration > 0 {
		if caster.AbilityCooldowns == nil {
			caster.AbilityCooldowns = make(map[string]float64, 1)
		}
		caster.AbilityCooldowns[abilityID] = cdDuration
	}
	armAbilityGlobalCooldownLocked(caster)

	// Zero / negative cast time ⇒ instant ability (no lock, resolve now).
	if eff.CastTime <= 0 {
		targets := s.buildCastTargetSetLocked(caster, def, target)
		s.resolveAbilityCastLocked(caster, def, targets)
		return true, ""
	}

	caster.CastAbilityID = abilityID
	caster.CastTargetID = target.ID
	caster.CastTimeRemaining = eff.CastTime
	s.beginUnitCastingLocked(caster) // Part 5: lock + "Casting" animation slot
	return true, ""
}

// beginAbilityCastAtPointLocked casts a GROUND/POINT-targeted ability
// (TargetsPoint) toward world point (x,y). Unlike the unit-target path there is
// no target unit to re-resolve — the direction is fixed at cast time. A
// zero cast-time ability (arcane_orb, shatter) resolves immediately, same as
// before. A non-zero cast-time ability (meteor) locks the caster and stores
// the point (CastIsPoint/CastPointX/Y); tickUnitCastLocked resolves it when
// the timer elapses, mirroring the unit-target lifecycle in
// beginAbilityCastLocked.
//
// Caller holds s.mu.
func (s *GameState) beginAbilityCastAtPointLocked(caster *Unit, abilityID string, x, y float64) (bool, string) {
	if caster == nil || caster.HP <= 0 {
		return false, castFailInvalidTarget
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return s.failCastLocked(caster, castFailUnknownAbility)
	}
	if def.IsPassive() || !def.TargetsPoint {
		return s.failCastLocked(caster, castFailNotOwned)
	}
	if !containsAbility(caster, abilityID) {
		return s.failCastLocked(caster, castFailNotOwned)
	}
	if caster.CastAbilityID != "" {
		return s.failCastLocked(caster, castFailAlreadyCasting)
	}
	if caster.GlobalCooldownRemaining > 0 {
		return s.failCastLocked(caster, castFailGlobalCooldown)
	}
	eff := s.effectiveSpellLocked(caster, def)
	if caster.CurrentMana < eff.ManaCost {
		return s.failCastLocked(caster, castFailNotEnoughMana)
	}
	caster.LastCastFailure = ""
	if cd := eff.EffectiveCooldown(); cd > 0 {
		if caster.AbilityCooldowns == nil {
			caster.AbilityCooldowns = make(map[string]float64, 1)
		}
		caster.AbilityCooldowns[abilityID] = cd
	}
	armAbilityGlobalCooldownLocked(caster)
	// Timed point cast: lock the caster and store the target point; resolution
	// happens in tickUnitCastLocked when the timer elapses. Zero cast time keeps
	// the prior behavior (resolve now). Mana is spent on completion, so an
	// interrupted wind-up costs nothing (matches the unit-target path).
	if eff.CastTime > 0 {
		caster.CastAbilityID = abilityID
		caster.CastIsPoint = true
		caster.CastPointX = x
		caster.CastPointY = y
		caster.CastTimeRemaining = eff.CastTime
		s.beginUnitCastingLocked(caster) // lock + "Casting" animation slot
		return true, ""
	}
	s.resolveAbilityCastAtPointLocked(caster, def, eff, x, y)
	return true, ""
}

// resolveAbilityCastAtPointLocked applies a completed point cast: spends mana
// once, then fires the ability's point effect toward (x,y). Three point
// abilities exist today: arcane_orb (a projectile + pull ⇒ traveling vortex),
// meteor (a delayed-impact GroundHazard ⇒ falling AoE + lingering burn), and
// shatter (an instant hitscan AoE burst). The branches are mutually exclusive
// and ordered so meteor's delayed-impact check runs before shatter's instant
// AoE, since both match `Projectile=="" && Radius>0`.
//
// Caller holds s.mu.
func (s *GameState) resolveAbilityCastAtPointLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, x, y float64) {
	if caster == nil {
		return
	}
	// Composable (schemaVersion>=2) abilities route through the executor
	// instead of the legacy branches below. No shipped catalog ability sets
	// SchemaVersion>=2 (Phase 5), so this is a no-op for every ability live
	// today — only authored composable abilities reach it.
	if def.SchemaVersion >= 2 && def.Program != nil {
		s.resolveAbilityProgramCastLocked(caster, def, eff, nil, protocol.Vec2{X: x, Y: y})
		return
	}
	if !s.spendUnitManaLocked(caster, eff.ManaCost) {
		caster.LastCastFailure = castFailNotEnoughMana
		return
	}
	// Traveling orb: launch a slow straight-line vortex from the caster toward
	// the clicked point, up to the ability's full cast-range distance.
	if eff.PullStrength > 0 && def.Projectile != "" {
		s.spawnArcaneOrbLocked(caster, x, y, def, eff, def.CastRange.Resolve(caster))
	}

	// Delayed-impact ground hazard (Meteor and future sky-drop spells). A point
	// cast that declares an impact delay does NOT resolve its AoE instantly:
	// instead it spawns a GroundHazard that falls, impacts once after
	// ImpactDelaySeconds, then leaves a lingering burn. Data-driven off the def
	// (no per-spell branch) — any ability with impactDelaySeconds > 0 and no
	// projectile reuses this. The visual is a world-anchored effect whose sprite
	// metadata drives the fall animation + per-frame render layering (client).
	// Checked BEFORE the instant-AoE (Shatter) branch below since Meteor also
	// matches `Projectile=="" && Radius>0` — the return keeps the two mutually
	// exclusive.
	//
	// EXTENSION POINT: to add another delayed-AoE spell, author its ability JSON
	// with impactDelay/burn fields + an effectAtPoint effect — nothing here changes.
	if def.ImpactDelaySeconds > 0 && def.Projectile == "" {
		cx, cy := clampPointToRange(caster.X, caster.Y, x, y, def.CastRange.Resolve(caster))
		s.spawnGroundHazardLocked(caster, def, eff, cx, cy)
		// World-anchored VFX at the impact point. Duration comes from the meteor
		// EffectDef; impactDelay is authored to line up with the sprite's impact
		// frame. Plays regardless of hits so a whiffed ground cast still reads.
		s.playEffectAtPointLocked(def.EffectAtPoint, cx, cy, def.EffectScale)
		return
	}

	// Instant point AoE (Shatter): a hitscan area burst at the clicked
	// location — no traveling projectile. Fires for any point ability that has
	// an area radius and no projectile, so it stays generic (data-driven off
	// the def, no spell-specific branch). The effect centre is clamped to the
	// caster's cast range so a far click lands at max reach, mirroring how the
	// orb caps its travel at CastRange.
	if def.Projectile == "" && eff.Radius > 0 {
		cx, cy := clampPointToRange(caster.X, caster.Y, x, y, def.CastRange.Resolve(caster))
		s.resolveAbilityAoeAtPointLocked(caster, def, eff, cx, cy)
		// Ground burst VFX at the (clamped) cast point. Plays regardless of
		// whether any enemy was caught, so a whiffed ground cast still reads.
		s.playEffectAtPointLocked(def.EffectAtPoint, cx, cy, def.EffectScale)
	}
}

// resolveAbilityProgramCastLocked resolves a completed cast for a composable
// (SchemaVersion>=2, Program != nil) ability by running its on_cast_complete
// triggers through the executor (runProgramTriggersLocked), instead of the
// legacy heal/damage/pull/etc. branches. Mana is spent exactly once here,
// mirroring both legacy resolvers (resolveAbilityCastLocked /
// resolveAbilityCastAtPointLocked) — a false return from spendUnitManaLocked
// (shouldn't happen post-init-check) fails the cast gracefully with no effect,
// same as the legacy path.
//
// eff is the effective (modifier-folded) spell to resolve with — callers
// pass the SAME eff they already resolved for their own mana/gating checks
// (beginAbilityCastAtPointLocked / tickUnitCastLocked / resolveAbilityCastLocked
// via s.effectiveSpellLocked) or a deliberately customized one (unstable_magic's
// free, reduced-effectiveness proc — perks_arch_mage.go). This function MUST
// NOT re-derive its own eff: doing so would silently discard a customized
// ManaCost (a "free proc" would charge full mana) and
// DamageEffectivenessMultiplier (a reduced-effectiveness proc would deal full
// damage) — see the composable-abilities-executor-parity investigation.
//
// primary is the unit-target cast's primary/anchor target (nil for a
// point cast); point is the point-cast's world target (zero Vec2 for a
// unit-target cast). ImpactPosition mirrors CastPoint so a program that
// reads impact_position for an instant point-AoE resolves at the cast
// location — marker-delayed impact is deferred, so an instant point cast's
// cast_point and impact_position are always the same point.
//
// abilityDef is set on the context so deal_damage scales the caster's
// spell-modifiers for this ability's school/tags, at parity with the legacy
// path's effectiveSpellLocked-derived damage. eff.DamageEffectivenessMultiplier
// is copied onto the context so deal_damage also honours any caller-applied
// reduced/boosted effectiveness on top of that modifier fold.
//
// Caller holds s.mu.
func (s *GameState) resolveAbilityProgramCastLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, primary *Unit, point protocol.Vec2) {
	if !s.spendUnitManaLocked(caster, eff.ManaCost) {
		caster.LastCastFailure = castFailNotEnoughMana
		return
	}
	ctx := &RuntimeAbilityContext{
		CasterID:                      caster.ID,
		AbilityID:                     def.ID,
		program:                       def.Program,
		abilityDef:                    &def,
		Named:                         map[string]ContextValue{},
		CastPoint:                     point,
		ImpactPosition:                point,
		Trace:                         s.previewTrace,
		now:                           s.previewClock,
		damageEffectivenessMultiplier: eff.effectivenessMultiplier(),
	}
	if primary != nil {
		ctx.InitialTarget = primary.ID
	}
	s.runProgramTriggersLocked(ctx, def.Program.Triggers, TriggerOnCastComplete)
}

// playEffectAtPointLocked queues a registered effect at a WORLD position
// (anchorUnitID 0) through the shared transient-effect pipeline, so it renders
// via the same path unit/perk effects use. No-op for an empty id or an
// unregistered effect (fail-safe, matching playEffectOnUnitLocked). The
// effect's duration comes from its EffectDef; scale <= 0 defaults to 1×.
//
// Caller holds s.mu.
func (s *GameState) playEffectAtPointLocked(effectID string, x, y, scale float64) {
	if effectID == "" {
		return
	}
	def, ok := getEffectDef(effectID)
	if !ok {
		return
	}
	if scale <= 0 {
		scale = 1.0
	}
	s.queueEffectLocked(effectID, 0, x, y, scale, def.Duration, "")
}

// playEffectAtPointForDurationLocked is playEffectAtPointLocked with an explicit
// duration override instead of the EffectDef's authored Duration. Use it when an
// effect's lifetime must match a gameplay window rather than a fixed animation
// length — e.g. Meteor's burning crater, which must persist exactly as long as
// the GroundHazard's burn phase (authored on the ability, not the effect). The
// EffectDef must still exist (fail-safe no-op otherwise, matching
// playEffectAtPointLocked); its authored Duration is ignored here.
//
// Caller holds s.mu.
func (s *GameState) playEffectAtPointForDurationLocked(effectID string, x, y, scale, duration float64) {
	if effectID == "" {
		return
	}
	if _, ok := getEffectDef(effectID); !ok {
		return
	}
	if scale <= 0 {
		scale = 1.0
	}
	if duration <= 0 {
		duration = 1.0
	}
	s.queueEffectLocked(effectID, 0, x, y, scale, duration, "")
}

// clampPointToRange returns (px,py) moved to lie within `maxRange` of the
// origin (ox,oy): if it is already within range, or maxRange <= 0 (no limit),
// it is returned unchanged; otherwise it is pulled back along the origin→point
// ray to exactly maxRange. Pure geometry — no lock, no state.
func clampPointToRange(ox, oy, px, py, maxRange float64) (float64, float64) {
	if maxRange <= 0 {
		return px, py
	}
	dx, dy := px-ox, py-oy
	d2 := dx*dx + dy*dy
	if d2 <= maxRange*maxRange {
		return px, py
	}
	d := math.Sqrt(d2)
	return ox + dx/d*maxRange, oy + dy/d*maxRange
}

// resolveAbilityAoeAtPointLocked applies an instant (hitscan) area effect
// centred on (cx,cy): every hostile, living, visible unit within eff.Radius
// takes eff.Damage (when > 0) and, when the ability declares a slow, is
// slowed via the shared proc-slow seam — a "cold" ability chills (cold-slow
// track / icy overlay), any other school lands a physical slow. Fully
// data-driven off the def; there is no per-spell branch, so any future
// instant point-AoE spell reuses it unchanged. Damage rides the authoritative
// pipeline (mitigation, death, threat, determinism), matching
// applyAbilitySplashDamageLocked. A unit killed by the damage is skipped by
// applyProcSlowLocked's dead-target guard, so no chill is wasted on a corpse.
//
// Caller holds s.mu.
func (s *GameState) resolveAbilityAoeAtPointLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, cx, cy float64) {
	if caster == nil || eff.Radius <= 0 {
		return
	}
	radSq := eff.Radius * eff.Radius
	dmgType := def.DamageType.OrPhysical()
	for _, u := range s.Units {
		if u == nil || u.ID == caster.ID || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(u.OwnerID, caster.OwnerID) {
			continue
		}
		dx := u.X - cx
		dy := u.Y - cy
		if dx*dx+dy*dy > radSq {
			continue
		}
		if eff.Damage > 0 {
			s.applyUnitDamageWithSourceLocked(u, eff.Damage, DamageSource{
				AttackerUnitID: caster.ID,
				Kind:           "ability",
				DamageType:     dmgType,
			})
		}
		// Chill / slow every unit in the burst. No-op when the ability declares
		// no slow, or the value is out of range, or the unit just died.
		s.applyProcSlowLocked(u.ID, def.SlowMultiplier, def.SlowDurationSeconds, dmgType)
	}
}

// failCastLocked records the failure reason on the unit (for async/UI
// surfacing) and returns the (false, reason) tuple.
func (s *GameState) failCastLocked(caster *Unit, reason string) (bool, string) {
	if caster != nil {
		caster.LastCastFailure = reason
	}
	return false, reason
}

// tickUnitCastLocked advances an in-progress cast by dt seconds. No-op for a
// unit that is not casting. The target is held by ID and re-resolved every
// tick (per the targeting invariant): if it dies, leaves range, or otherwise
// becomes an illegal target, the cast is cancelled cleanly (no heal, no mana
// spent). Otherwise the timer counts down and the ability resolves at 0.
//
// Caller holds s.mu.
func (s *GameState) tickUnitCastLocked(unit *Unit, dt float64) {
	if unit == nil || unit.CastAbilityID == "" {
		return
	}
	def, ok := getAbilityDef(unit.CastAbilityID)
	if !ok {
		s.cancelUnitCastLocked(unit, castFailUnknownAbility)
		return
	}
	if unit.HP <= 0 {
		// Caster died mid-cast — just clear the cast bookkeeping (the unit is
		// being removed anyway).
		s.clearUnitCastLocked(unit)
		return
	}
	// Point (ground) casts have no target unit to re-resolve — the aim point was
	// fixed at cast start. Count down and resolve at the stored world point.
	if unit.CastIsPoint {
		unit.CastTimeRemaining -= dt
		if unit.CastTimeRemaining > 0 {
			return // still casting; Part 5 guard keeps Status pinned to "Casting"
		}
		eff := s.effectiveSpellLocked(unit, def)
		s.resolveAbilityCastAtPointLocked(unit, def, eff, unit.CastPointX, unit.CastPointY)
		s.clearUnitCastLocked(unit)
		return
	}
	// Re-resolve the primary/anchor target by ID every tick (targeting
	// invariant). If it is gone the cast cancels regardless of TargetCount.
	primary := s.getUnitByIDLocked(unit.CastTargetID)
	if !s.canAbilityTargetUnitLocked(def, unit, primary) || !def.WithinCastRange(unit, primary) {
		s.cancelUnitCastLocked(unit, castFailTargetLost)
		return
	}

	unit.CastTimeRemaining -= dt
	if unit.CastTimeRemaining > 0 {
		return // still casting; Part 5 guard keeps Status pinned to "Casting"
	}
	targets := s.buildCastTargetSetLocked(unit, def, primary)
	s.resolveAbilityCastLocked(unit, def, targets)
	s.clearUnitCastLocked(unit)
}

// resolveAbilityCastLocked applies a completed cast: deduct mana once, then
// apply the ability's effect to each target in the slice. For single-target
// abilities the slice has exactly one element and the behaviour is unchanged.
// For multi-target abilities (def.TargetCount > 1) the effect (heal, damage,
// VFX, perk hook) fires once per target; mana is deducted once regardless of
// how many targets were hit.
//
// Heal restores HealAmount HP clamped to the target's MaxHP (no overheal —
// does NOT route through healUnitLocked, which converts overheal to a perk
// shield). The on-target effect (e.g. "healing_glow") plays via the shared
// transient-effect pipeline.
//
// An empty or nil targets slice is a no-op: mana is never spent for a cast
// with no valid targets (edge case: all targets died between cast-start and
// resolution).
//
// This is the "derive-my-own-eff" entry point used by the normal cast timeline
// (tickUnitCastLocked), where no caller customizes the EffectiveSpell. A
// caller that DOES need to hand in a customized eff (unstable_magic's free,
// reduced-effectiveness proc — perks_arch_mage.go) must call
// resolveAbilityCastWithEffLocked directly instead of duplicating the
// schema-version branch below; see that function's doc comment.
//
// Caller holds s.mu. Caller is responsible for clearing the cast state.
func (s *GameState) resolveAbilityCastLocked(caster *Unit, def AbilityDef, targets []*Unit) {
	if caster == nil || len(targets) == 0 {
		return
	}
	// Resolve effective values once at resolution time (a modifier that
	// changed since cast-start is honoured here — resolution reflects the
	// spell as it fires). Mana spend and every per-target magnitude read from
	// this EffectiveSpell, never the raw def.
	eff := s.effectiveSpellLocked(caster, def)
	s.resolveAbilityCastWithEffLocked(caster, def, eff, targets)
}

// resolveAbilityCastWithEffLocked is the shared unit-targeted cast-resolution
// seam: it branches on SchemaVersion exactly like resolveAbilityCastAtPointLocked
// does for point casts, and — critically — resolves EVERY target using the
// caller-supplied eff rather than re-deriving one from scratch. That makes it
// the correct entry point for any caller that needs a customized
// EffectiveSpell (unstable_magic's free/reduced-effectiveness proc —
// perks_arch_mage.go's fireUnstableMagicLocked) as well as for
// resolveAbilityCastLocked's own normal-cast path (which derives its own eff
// and forwards it here unchanged).
//
// Before this function existed, fireUnstableMagicLocked's unit-targeted
// branch called resolveAbilityCastOnTargetLocked directly — the inner
// LEGACY-only per-target applier, below the schema-version check. That
// silently ran a v2 (schemaVersion>=2, Program != nil) proc target through the
// legacy branch, which reads def.DamageAmount — a field ConvertLegacyAbility
// clears to 0 on migration, so the proc dealt no damage. Routing through this
// seam instead means the proc gets the same SchemaVersion>=2 branch every
// other unit-targeted cast gets.
//
// targets[0] is treated as the primary/anchor target for the composable
// branch (matching resolveAbilityCastLocked's existing contract) — safe for
// both callers: buildCastTargetSetLocked always force-includes the primary
// target first, and the proc always hands in a single-element slice.
//
// An empty or nil targets slice is a no-op: mana is never spent for a cast
// with no valid targets.
//
// Caller holds s.mu.
func (s *GameState) resolveAbilityCastWithEffLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, targets []*Unit) {
	if caster == nil || len(targets) == 0 {
		return
	}
	// Composable (schemaVersion>=2) abilities route through the executor
	// instead of the legacy per-target loop below. No shipped catalog
	// ability sets SchemaVersion>=2 (Phase 5), so this is a no-op for every
	// ability live today — only authored composable abilities reach it.
	if def.SchemaVersion >= 2 && def.Program != nil {
		s.resolveAbilityProgramCastLocked(caster, def, eff, targets[0], protocol.Vec2{})
		return
	}
	// Mana is paid here (on completion). spendUnitManaLocked is the single
	// authoritative spend; a false return (shouldn't happen post-init-check)
	// fails the cast gracefully with no effect. A zero eff.ManaCost (the
	// unstable_magic free-proc case) is a guaranteed-true no-op — see
	// spendUnitManaLocked's cost<=0 early return.
	if !s.spendUnitManaLocked(caster, eff.ManaCost) {
		caster.LastCastFailure = castFailNotEnoughMana
		return
	}

	for _, target := range targets {
		if target == nil {
			continue
		}
		s.resolveAbilityCastOnTargetLocked(caster, def, eff, target)
	}
}

// resolveAbilityCastOnTargetLocked applies the ability's per-target effects
// (heal, damage, VFX, perk hooks) to a single target. Mana is NOT deducted
// here — it is deducted once by resolveAbilityCastLocked before this is called.
//
// Caller holds s.mu.
func (s *GameState) resolveAbilityCastOnTargetLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, target *Unit) {
	if target == nil {
		return
	}
	if def.HealAmount > 0 && target.HP > 0 {
		// Divine Healer (silver cleric) scales every heal amount produced by
		// the caster. Default multiplier is 1.0; perks_cleric_silver.go owns
		// the lookup so future heal-sources just call the same helper.
		amount := int(math.Round(float64(def.HealAmount) * s.perkClericHealOutputMultiplierLocked(caster)))
		if amount < 0 {
			amount = 0
		}
		// Route the heal through the central cleric helper so gold-tier
		// triggers (beacon_of_life splash, divine_judgement AoE) fire under
		// the canonical metadata flags. The helper handles overheal routing,
		// records the heal event with the EFFECTIVE gain, and dispatches
		// triggers using the INTENDED amount (per spec: Judgement uses the
		// full heal value even on full-HP / overheal targets).
		s.applyClericHealLocked(caster, target, amount, healMetaPrimaryAbility())
	}

	// Offensive resolve step (symmetric to HealAmount). Two delivery modes:
	//   - Projectile abilities (def.Projectile set) launch a homing bolt that
	//     carries the damage and applies it ON IMPACT (arcane_bolt). The bolt
	//     rides the same pipeline basic-attack shots use, so mitigation, the
	//     death pipeline, threat, and determinism all apply at landing.
	//   - Otherwise the damage is applied INSTANTLY (hitscan) through the shared
	//     authoritative pipeline — the prior behaviour.
	// 0 / absent DamageAmount ⇒ no damage (inert for non-offensive abilities).
	if eff.Damage > 0 && target.HP > 0 {
		switch {
		case eff.ChainCount > 0:
			// Chaining ability (chain_lightning): the bolt hits the primary
			// target then arcs to further enemies. Reuses the existing beam-
			// bounce mechanic (executeProcEffectLocked → fireProcBeamLocked),
			// so bounce selection, falloff, and determinism are inherited.
			s.fireAbilityChainLocked(caster, target, def, eff)
		case def.Projectile != "":
			s.fireAbilityProjectileLocked(caster, target, def, eff)
		default:
			s.applyUnitDamageWithSourceLocked(target, eff.Damage, DamageSource{
				AttackerUnitID: caster.ID,
				Kind:           "ability",
				DamageType:     def.DamageType.OrPhysical(),
			})
		}
	}

	// Instant area pull resolve step for UNIT-targeted pull abilities: drag
	// hostiles within radius of the target's position for the effective
	// duration (allies/caster never displaced; inert when PullStrength == 0).
	// The traveling-orb variant (arcane_orb) is point-targeted and resolves via
	// resolveAbilityCastAtPointLocked instead — not here.
	if eff.PullStrength > 0 && eff.Radius > 0 && eff.Duration > 0 {
		s.applyPullInRadiusLocked(caster, target.X, target.Y, eff.Radius, eff.PullStrength, eff.Duration)
	}

	if def.EffectOnTarget != "" {
		s.playEffectOnUnitLocked(target, def.EffectOnTarget)
	}

	if def.SummonUnitType != "" {
		s.spawnSummonedUnitLocked(caster, def)
	}

	// Perk hook: fire once per resolved target so perks like battle_prayer
	// can stamp cross-unit buffs on every ally the ability touches.
	s.onPerkAbilityResolvedLocked(caster, def, target)
}

// fireAbilityChainLocked delivers a chaining offensive ability (chain_lightning)
// through the existing beam-bounce mechanic. It builds a ProcEffectParams from
// the ability's EFFECTIVE values (damage, chain count) plus the def's bounce
// tuning and fires it via executeProcEffectLocked — the same path equipment's
// lightning_chain proc uses. That reuse means the primary hit, the up-to-
// ChainCount deterministic hops, per-hop falloff, kill credit, and the
// no-recurse discipline are all inherited; nothing about chaining is
// reimplemented here.
//
// The emitter defaults to the beam projectile def "lightning_bolt" (what the
// lightning_chain proc arcs on) when the ability names no projectile.
//
// Caller holds s.mu.
func (s *GameState) fireAbilityChainLocked(caster, target *Unit, def AbilityDef, eff EffectiveSpell) {
	emitter := def.Projectile
	if emitter == "" {
		emitter = "lightning_bolt"
	}
	s.executeProcEffectLocked(procSourceFromUnit(caster), target, ProcEffectParams{
		Damage:              eff.Damage,
		DamageType:          def.DamageType.OrPhysical(),
		ProjectileID:        emitter,
		BounceCount:         eff.ChainCount,
		BounceRange:         def.BounceRange,
		BounceDamageFalloff: def.BounceDamageFalloff,
	})
}

// cancelUnitCastLocked aborts an in-progress cast (target lost, etc.): no
// mana spent, no effect, records the reason for feedback, then clears state.
func (s *GameState) cancelUnitCastLocked(unit *Unit, reason string) {
	if unit == nil {
		return
	}
	unit.LastCastFailure = reason
	s.clearUnitCastLocked(unit)
}

// clearUnitCastLocked clears cast bookkeeping and releases the Part 5 cast
// lock (Casting flag / "Casting" status). Safe to call when not casting.
func (s *GameState) clearUnitCastLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.CastAbilityID = ""
	unit.CastTargetID = 0
	unit.CastTimeRemaining = 0
	unit.CastIsPoint = false
	unit.CastPointX = 0
	unit.CastPointY = 0
	s.endUnitCastingLocked(unit)
}
