package game

import (
	"encoding/json"
	"math"
	"strconv"
)

// ═════════════════════════════════════════════════════════════════════════════
// LEGACY -> COMPOSABLE COMPILER (Phase 4, Task 1)
//
// compileLegacyAbility converts a flat legacy AbilityDef (schemaVersion
// absent/1) into a composable *AbilityProgram (schemaVersion 2 shape). It is
// a PURE function: no GameState, no lock, no I/O, no mutation of def. Nothing
// in the live cast path calls it yet — it exists for golden equivalence
// tests (a later task) and, eventually, the editor's "Convert to Composable
// Ability" flow (design doc §6.2).
//
// The mechanic precedence below mirrors §4 of the composable-abilities design
// doc: each legacy ability uses exactly one "shape" (heal, offensive/instant,
// offensive/point-aoe, offensive/delayed-impact, offensive/projectile,
// offensive/DoT-vortex, summon, channel, charge-fire-passive). A def that
// matches none of these produces a program with an empty on_cast_complete
// trigger (validateAbilityProgram flags that as the "no_behavior" warning,
// not an error).
// ═════════════════════════════════════════════════════════════════════════════

// CompileLegacyAbilityForEditor exposes compileLegacyAbility to callers
// outside package game (specifically the /catalog/abilities HTTP handler) so
// the editor can display a legacy ability's composable flow without the
// author converting it. Pure; returns a fresh compiled program every call and
// never mutates def.
func CompileLegacyAbilityForEditor(def AbilityDef) *AbilityProgram {
	return compileLegacyAbility(def)
}

// compileLegacyAbility builds the composable equivalent of def. Read-only
// over def; returns a fresh *AbilityProgram every call.
//
// Charge-fire passives (arcane_missiles) are special-cased BEFORE the normal
// on_cast_complete shape below: they are NEVER cast (IsPassive() rejects any
// cast attempt in beginAbilityCastLocked), so compiling them into an
// on_cast_complete trigger would produce a program whose only trigger can
// never fire. Their real event is "the owning unit's Arcane Charge crossed
// ChargeRequired" — TriggerOnChargeFull — see compileChargeFireProgram.
func compileLegacyAbility(def AbilityDef) *AbilityProgram {
	if def.IsChargeFirePassive() {
		return compileChargeFireProgram(def)
	}
	actions, presentations := compileCastActions(def)
	prog := &AbilityProgram{
		Entry: compileEntryLegacy(def),
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: actions},
		},
	}
	if len(presentations) > 0 {
		prog.Presentations = presentations
	}
	return prog
}

// compileEntryLegacy derives the AbilityEntryDef from the legacy targeting
// flags: TargetsPoint wins first (point-targeted abilities keep their point
// entry even when a CanTarget* flag is also set, e.g. shatter/meteor set
// canTargetEnemies for auto-cast candidate scanning), then any CanTarget*
// flag selects unit-targeting, then IsPassive(), else no-target.
func compileEntryLegacy(def AbilityDef) AbilityEntryDef {
	entry := AbilityEntryDef{
		Range:     def.CastRange,
		Relations: relationsFromFlags(def),
	}
	switch {
	case def.TargetsPoint:
		entry.Type = EntryGroundPoint
	case def.CanTargetSelf || def.CanTargetAllies || def.CanTargetEnemies:
		entry.Type = EntryUnit
	case def.IsPassive():
		entry.Type = EntryPassive
	default:
		entry.Type = EntryNoTarget
	}
	return entry
}

// relationsFromFlags maps the legacy CanTarget* flags onto TargetRelation
// values, in self/ally/enemy order. Never returns neutral (no legacy field
// maps to it).
func relationsFromFlags(def AbilityDef) []TargetRelation {
	var rel []TargetRelation
	if def.CanTargetSelf {
		rel = append(rel, RelSelf)
	}
	if def.CanTargetAllies {
		rel = append(rel, RelAlly)
	}
	if def.CanTargetEnemies {
		rel = append(rel, RelEnemy)
	}
	return rel
}

// compileCastActions builds the on_cast_complete action sequence (plus any
// top-level Presentations the sequence references, e.g. meteor's impact
// marker) for def, in mechanic precedence order (most-specific first):
// channel > summon > heal > offensive. A def matching none of these (e.g. a
// pure buff with no modeled mechanic yet) yields no actions.
//
// Charge-fire passives never reach here — compileLegacyAbility special-cases
// them into an on_charge_full program (compileChargeFireProgram) before this
// is called, since they have no on_cast_complete event at all.
func compileCastActions(def AbilityDef) ([]AbilityActionDef, []PresentationInstanceDef) {
	switch {
	case def.ChannelType != "":
		return []AbilityActionDef{compileChannelBeamAction(def)}, nil
	case def.SummonUnitType != "":
		return []AbilityActionDef{compileSummonAction(def)}, nil
	case def.HealAmount > 0:
		return compileHealActions(def), nil
	case def.DamageAmount > 0 || def.DamagePerSecond > 0:
		return compileOffensiveActions(def)
	default:
		return nil, nil
	}
}

// ── channel (siphon_life) ──────────────────────────────────────────────────

// compileChannelBeamAction builds the single channeled beam action for a
// channel-type def (siphon_life's shape): a beam action with Channeled=true.
// Carries its own Target query (SrcInitialTarget, matching
// compileProjectileActions' single-target cast precedent) rather than a
// preceding select_targets action.
//
// Every field the channel lifecycle reads off an AbilityDef (via channelSpecFor)
// is baked into the config, because a converted (schemaVersion 2) ability has
// its legacy mechanic fields cleared (ConvertLegacyAbility) — Config is the
// only source of truth once converted. ChannelType round-trips so
// describeAbility's abilityMechanicsShadow recovery can restore the shadow
// def's ChannelType (ability_describe.go).
//
// cfg.Triggers additionally carries the on_beam_tick trigger
// (compileChannelBeamTickTrigger below): the authored per-tick DAMAGE step
// tickUnitChannelLocked fires every channel tick, so damage runs through the
// real deal_damage action (fold via ctx.damageEffectivenessMultiplier) instead
// of a hardcoded computation. The channel LIFECYCLE (start/stop/mana/heal
// distribution/perks) stays in Go, reading the magnitudes via channelSpecFor.
func compileChannelBeamAction(def AbilityDef) AbilityActionDef {
	cfg := beamConfig{
		Channeled:           true,
		ChannelType:         def.ChannelType,
		TickIntervalSeconds: def.TickIntervalSeconds,
		ManaCostPerTick:     def.ManaCostPerTick,
		DamagePerTick:       def.DamagePerTick,
		HealingMultiplier:   def.HealingMultiplier,
		AllyHealRadius:      def.AllyHealRadius,
		Triggers:            []AbilityTriggerDef{compileChannelBeamTickTrigger(def)},
	}
	return AbilityActionDef{
		ID:     "channel",
		Type:   ActionBeam,
		Target: &TargetQueryDef{Source: SrcInitialTarget},
		Config: marshalConfig(cfg),
	}
}

// compileChannelBeamTickTrigger builds the on_beam_tick trigger fired once
// per channel tick: a single deal_damage action for def.DamagePerTick against
// the channel's one target. Targets via SrcInitialTarget -- the per-tick
// RuntimeAbilityContext tickUnitChannelLocked builds sets InitialTarget to
// the channel's (re-resolved, already-validated) target every tick, mirroring
// beginAbilityChannelLocked's own ctx build (ability_channel.go) -- so this
// needs no preceding select_targets action, same as channel_beam's own
// Target query above.
//
// The channel LIFECYCLE (mana cost, heal distribution, all four Siphoner
// perk hooks) intentionally stays out of this trigger and out of the
// composable executor entirely -- see tickUnitChannelLocked's doc comment
// for why: they all read tickDamage (this action's applied amount, via
// ctx.lastAppliedDamage) as an INPUT, not as an authored program step.
func compileChannelBeamTickTrigger(def AbilityDef) AbilityTriggerDef {
	return AbilityTriggerDef{
		ID:   "beam_tick",
		Type: TriggerOnBeamTick,
		Actions: []AbilityActionDef{
			{
				ID:     "dmg",
				Type:   ActionDealDamage,
				Target: &TargetQueryDef{Source: SrcInitialTarget},
				Config: marshalConfig(dealDamageConfig{Amount: def.DamagePerTick, Type: def.DamageType}),
			},
		},
	}
}

// ── charge-fire passive (arcane_missiles) ──────────────────────────────────

// compileChargeFireProgram builds the composable equivalent of a charge-fire
// passive: a single on_charge_full trigger carrying one charge_fire_volley
// action with the ability's full spec baked into its config. See
// spell_charge.go's file doc comment for how this trigger is fired
// (fireChargeFullLocked, the third RuntimeAbilityContext builder) and why the
// staggered, re-pick-at-launch volley itself stays a hard-coded tick loop
// rather than being decomposed into repeat/wait/select_targets actions.
func compileChargeFireProgram(def AbilityDef) *AbilityProgram {
	return &AbilityProgram{
		Entry: compileEntryLegacy(def),
		Triggers: []AbilityTriggerDef{
			{ID: "charge_full", Type: TriggerOnChargeFull, Actions: []AbilityActionDef{compileChargeFireAction(def)}},
		},
	}
}

// compileChargeFireAction builds the single charge_fire_volley action for a
// charge-fire passive, baking every field spell_charge.go's queue/launch
// steps read (chargeRequired, manaToChargeRatio, missileCount,
// damagePerMissile, missileDelayMs, projectile/projectileScale/
// projectileSpeed, damage type, minorDamage) into its Config — see
// chargeFireVolleyConfig (spell_charge.go) for why Config must be the sole
// authority once the ability converts (its raw AbilityDef fields clear, same
// discipline as launch_projectile/launch_vortex).
func compileChargeFireAction(def AbilityDef) AbilityActionDef {
	cfg := chargeFireVolleyConfig{
		ChargeRequired:        def.ChargeRequired,
		ManaToChargeRatio:     def.ManaToChargeRatio,
		MissileCount:          def.MissileCount,
		DamagePerMissile:      def.DamagePerMissile,
		MissileDelayMs:        def.MissileDelayMs,
		Projectile:            def.Projectile,
		ProjectileScale:       def.ProjectileScale,
		ProjectileSpeed:       def.ProjectileSpeed,
		Type:                  def.DamageType,
		MinorDamage:           def.MinorDamage,
		Targeting:             def.Targeting,
		AllowDuplicateTargets: def.AllowDuplicateTargets,
	}
	return AbilityActionDef{ID: "charge", Type: ActionChargeFireVolley, Config: marshalConfig(cfg)}
}

// ── summon ──────────────────────────────────────────────────────────────

func compileSummonAction(def AbilityDef) AbilityActionDef {
	return AbilityActionDef{
		ID:     "summon",
		Type:   ActionSummonUnit,
		Config: marshalConfig(summonUnitConfig{UnitType: def.SummonUnitType, Count: def.SummonCount}),
	}
}

// ── heal ────────────────────────────────────────────────────────────────

// compileHealActions builds select_targets -> restore_health (-> optional
// play_presentation vfx) for a healing ability. A TargetCount <= 1 ability
// (Heal) selects from the initial cast target directly; a TargetCount > 1
// ability (Greater Heal) scans allies/self around the caster ordered by
// lowest health percentage, matching the multi-target heal resolver's
// existing behavior.
func compileHealActions(def AbilityDef) []AbilityActionDef {
	sel := AbilityActionDef{
		ID:      "sel",
		Type:    ActionSelectTargets,
		Outputs: map[string]string{"targets": "healTargets"},
	}
	if def.TargetCount <= 1 {
		sel.Target = &TargetQueryDef{Source: SrcInitialTarget}
	} else {
		sel.Target = &TargetQueryDef{
			Source:               SrcAllInScene,
			Origin:               OriginCaster,
			Relations:            relationsFromFlags(def),
			Radius:               float64(def.CastRange),
			Ordering:             OrderLowestHealthPct,
			MaxCount:             def.TargetCount,
			IncludeInitialTarget: true,
		}
	}

	actions := []AbilityActionDef{
		sel,
		{
			ID:     "heal",
			Type:   ActionRestoreHealth,
			Input:  map[string]ContextRef{"targets": {Key: "healTargets"}},
			Config: marshalConfig(restoreHealthConfig{Amount: def.HealAmount, School: def.DamageType}),
		},
	}

	if def.EffectOnTarget != "" {
		actions = append(actions, AbilityActionDef{
			ID:     "vfx",
			Type:   ActionPlayPresentation,
			Input:  map[string]ContextRef{"attach": {Key: "healTargets"}},
			Config: marshalConfig(playPresentationOnTargetConfig{Asset: def.EffectOnTarget, OncePerTarget: true}),
		})
	}
	return actions
}

// playPresentationOnTargetConfig documents the shape used when a
// play_presentation action attaches to a set of resolved targets (heal's
// EffectOnTarget) rather than a world position. Its ActionDescriptor
// (ability_exec_presentation.go, Phase 6b Task 1) decodes into the fuller
// playPresentationAtPointConfig, which carries every field this executor
// needs (Asset is shared; OncePerTarget's value doesn't change Execute's
// behavior — looping the target set once per unit already IS "once per
// target") — this struct exists to keep the COMPILER's on-target output shape
// self-documenting and round-trippable, not because it is decoded directly.
type playPresentationOnTargetConfig struct {
	Asset         string `json:"asset"`
	OncePerTarget bool   `json:"oncePerTarget,omitempty"`
}

// playPresentationAtPointConfig is the play_presentation config shape used
// for a world-anchored effect (shatter's ground burst, meteor's falling
// sprite). Also the type its ActionDescriptor.Decode unmarshals into for
// BOTH compiled shapes (see playPresentationOnTargetConfig and
// ability_exec_presentation.go).
type playPresentationAtPointConfig struct {
	Asset          string     `json:"asset"`
	Position       ContextRef `json:"position"`
	Scale          float64    `json:"scale,omitempty"`
	RenderLayer    string     `json:"renderLayer,omitempty"` // not yet consumed by Execute (schema/round-trip only)
	PresentationID string     `json:"presentationId,omitempty"`
}

// ── offensive ───────────────────────────────────────────────────────────

// compileOffensiveActions dispatches an offensive ability (DamageAmount > 0
// or DamagePerSecond > 0) to its mechanic-specific builder. The
// DamagePerSecond-only check runs FIRST: arcane_orb sets both a Projectile
// (its travelling orb visual) and DamagePerSecond with no DamageAmount, but
// its behavior (a moving pull+DoT vortex) is nothing like the
// launch-then-hit projectile shape below, so it must not fall into that
// branch just because Projectile is also set.
func compileOffensiveActions(def AbilityDef) ([]AbilityActionDef, []PresentationInstanceDef) {
	switch {
	case def.DamagePerSecond > 0 && def.DamageAmount <= 0:
		return compileTickingProjectileActions(def), nil
	case def.Projectile != "":
		return compileProjectileActions(def), nil
	case def.ImpactDelaySeconds > 0:
		return compileMeteorActions(def)
	case def.TargetsPoint && def.Radius > 0:
		return compileShatterActions(def), nil
	default:
		return compileInstantSingleTargetActions(def), nil
	}
}

// launchProjectileConfig is the compiled config for launch_projectile.
// ActionLaunchProjectile has a registered ActionDescriptor
// (ability_exec_projectile.go). As of the launch_projectile redesign (the
// "composable delivery primitive" phase) this action is SPAWN + TRAVEL ONLY
// for arcane_bolt/fireball's shape: damage/splash live in a nested
// on_projectile_impact trigger carried in Triggers below (the create_zone
// precedent — config.triggers, not Children, so it never auto-fires via
// on_action_complete; it fires only when the spawned Projectile actually
// lands — see fireProjectileImpactLocked, projectile.go).
//
// chain_lightning never reaches this config at all: its bounce is not a
// projectile delivery, so it compiles onto a fully authored chain of nested
// launch_beam actions instead — see compileChainLightningActions below.
type launchProjectileConfig struct {
	Projectile string `json:"projectile"`
	// TravelMode selects how the bolt gets from launch to impact:
	// "to_target" (default/empty — homes on the resolved unit target, current
	// behavior) or "direction" (flies toward the aim point and keeps going,
	// like arcane_orb's straight-line flight; see
	// launchDirectionalProjectileLocked, projectile.go, for the impact
	// semantics this chooses).
	TravelMode string `json:"travelMode,omitempty"`
	// Distance is the flight length for TravelMode "direction". Carries the
	// CastRange type (not a plain float64) so the "match_attack_range"
	// sentinel round-trips exactly like the legacy def's own CastRange field
	// — arcane_orb's compiled shape bakes def.CastRange here (see
	// compileTickingProjectileActions) and Execute resolves it against the
	// live caster (c.Distance.Resolve(caster)), mirroring
	// spawnArcaneOrbLocked's own CastRange.Resolve call. 0/unresolved derives
	// it from the caster-to-aim-point distance instead (mirrors
	// spawnArcaneOrbLocked's identical fallback). Unused for "to_target".
	Distance CastRange `json:"distance,omitempty"`
	// ProjectileScale is a presentation/rendering passthrough
	// (Projectile.Scale), baked in so Config is the sole authority once an
	// ability converts (schemaVersion 2 clears the legacy def fields).
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	// ProjectileSpeed overrides the flight speed that would otherwise come
	// from the named Projectile's own ProjectileDef.Speed (or
	// defaultProjectileSpeed/arcaneOrbDefaultSpeed when the def is
	// unregistered). 0/absent ⇒ no override, the pre-existing behavior for
	// every "to_target" bolt (arcane_bolt/fireball/chain_lightning never set
	// this). Only arcane_orb's ticking-vortex shim (TickInterval > 0, below)
	// sets this today, mirroring the legacy def's own ProjectileSpeed field
	// (folded through SpellModFieldProjectileSpeed — see the FOLD PARITY note
	// on the TickInterval-shape fields below).
	ProjectileSpeed float64 `json:"projectileSpeed,omitempty"`

	// SpawnOrigin selects the world position this bolt spawns FROM — NOT who
	// it flies at/toward, which stays `target`/`targets` as always. Reuses
	// TargetQueryDef's TargetOrigin enum (ability_program.go) and resolves via
	// the SAME s.resolveOriginLocked (ability_exec_targeting.go) every
	// TargetQueryDef.Origin already uses, rather than inventing a parallel
	// position vocabulary. The empty value ("") is the byte-identical
	// default: resolveOriginLocked's own default case returns the caster's
	// position for an empty/unrecognized origin, exactly the pre-existing
	// "bolt always leaves from the caster" behavior — compileLegacyAbility
	// never sets this field, so every legacy/compiled ability is unaffected.
	// Lets a composed ability whose impact splits into more bolts (a
	// "hit an enemy, then splits to nearby enemies" shape) spawn the split
	// bolts at the HIT enemy's position instead of back at the original
	// caster. See spawnOriginOptions (ability_exec_projectile.go) for which
	// concrete TargetOrigin values the schema actually offers, and why the
	// rest are deliberately withheld.
	SpawnOrigin TargetOrigin `json:"spawnOrigin,omitempty"`
	// SpawnOriginRef supplies the named-context key when SpawnOrigin is
	// "named_context_value" — a bolt arcing FROM a saved "cursor" unit/position
	// (the current chain target). nil for every other origin. Mirrors
	// beamConfig.SpawnOriginRef exactly: launch_projectile and beam share the
	// same Spawn Origin selection (spawnOriginOptions) and the same ref field.
	SpawnOriginRef *ContextRef `json:"spawnOriginRef,omitempty"`

	// ── arcane_orb's ticking-vortex shim (retired launch_vortex's fields) ──
	// TickInterval > 0 selects the vortex shape: a "direction" travelMode
	// bolt that NEVER fires on_projectile_impact (it flies its full Distance
	// regardless of what it passes near) and instead fires
	// on_projectile_tick repeatedly while airborne — see
	// tickArcaneOrbProjectileLocked (projectile.go) for the exact firing
	// cadence (apply_force/select_targets every simulation tick so the pull
	// tracks the orb's live position; deal_damage throttled to a fixed
	// cadence via its own Timing.TickInterval — see AbilityActionDef.Timing's
	// doc comment). 0/absent (every "to_target" and impact-shaped "direction"
	// bolt) means "not a ticking vortex" — the pre-existing behavior.
	//
	// NO SIBLING SCALARS HERE — SINGLE SOURCE OF TRUTH: this config used to
	// ALSO carry Radius/PullStrength/DamagePerSecond/Type top-level fields
	// mirroring the nested Triggers' magnitudes, and Execute read the
	// TOP-LEVEL copies while the flow view rendered the NESTED ones — so
	// editing the nested (visible, "authored") select_targets radius /
	// apply_force strength / deal_damage amount / apply_force mode did
	// NOTHING at runtime. That was a live bug (apply_force's "push" Mode
	// lived only in the dead nested copy and was never reachable). Fixed: the
	// magnitudes now live in EXACTLY ONE place — the nested on_projectile_tick
	// trigger below — and Execute reads them from there (see
	// executeTickingVortexShimLocked's vortexMagnitudesFromTrigger call).
	// Radius/PullStrength/ProjectileSpeed are still folded through the
	// caster's spell modifiers exactly once, at LAUNCH time (frozen for the
	// whole flight, never re-folded per tick — apply_force itself has no
	// fold seam of its own, unlike deal_damage, so this is the only place
	// they can be scaled), via the SAME applySpellModField helper /
	// collectSpellModifiersLocked(caster, *ctx.abilityDef) mods collection
	// legacy uses. deal_damage's amount is NOT frozen here: it folds itself,
	// once per firing, through the ordinary ctx.abilityDef seam every other
	// deal_damage action already uses — see fireProjectileTickLocked's
	// ROUNDING DECISION doc comment (projectile.go) for the exact arithmetic
	// this implies relative to legacy's single frozen-then-rounded DoT chunk.
	TickInterval float64 `json:"tickInterval,omitempty"`

	// Triggers carries the compiled on_projectile_impact trigger(s), OR (for
	// the TickInterval>0 vortex shape) the on_projectile_tick trigger shown
	// in the flow view — this action's nested-trigger slot. Populated for
	// every composable projectile compiled through this config (arcane_bolt/
	// fireball's migrated shape, and arcane_orb's vortex shape). chain_lightning
	// never reaches this config at all (see the type doc comment above).
	Triggers []AbilityTriggerDef `json:"triggers,omitempty"`
}

// projectileImpactTriggerID names the single on_projectile_impact trigger
// compileProjectileImpactTrigger emits, mirroring meteorPresentationID's role
// for the delayed-impact shape.
const projectileImpactTriggerID = "impact"

// compileProjectileActions builds the single launch_projectile action for a
// def with a non-empty Projectile. The action carries its own Target query
// (SrcInitialTarget, matching compileInstantSingleTargetActions' "sel" query)
// rather than a preceding select_targets action, since arcane_bolt/fireball/
// chain_lightning are all single-target casts — this doubles as the "target
// still alive" guard (the query's default AliveState excludes HP<=0), mirroring
// legacy's `eff.Damage > 0 && target.HP > 0` gate in resolveAbilityCastOnTargetLocked.
//
// chain_lightning (ChainCount > 0) is compiled onto a fully authored chain of
// nested launch_beam actions — see compileChainLightningActions — rather
// than the launchProjectileConfig baked-amount shim it used to keep here.
// Every other projectile ability (arcane_bolt/fireball) gets its damage
// (+ splash) composed into a nested on_projectile_impact trigger instead.
func compileProjectileActions(def AbilityDef) []AbilityActionDef {
	if def.ChainCount > 0 {
		return compileChainLightningActions(def)
	}
	cfg := launchProjectileConfig{
		Projectile:      def.Projectile,
		ProjectileScale: def.ProjectileScale,
		Triggers:        []AbilityTriggerDef{compileProjectileImpactTrigger(def)},
	}
	return []AbilityActionDef{{
		ID:     "proj",
		Type:   ActionLaunchProjectile,
		Target: &TargetQueryDef{Source: SrcInitialTarget},
		Config: marshalConfig(cfg),
	}}
}

// chainLightningHitSetKey names the ctxUnitSet (ctx.Named) that
// compileChainLightningActions' chain accumulates already-struck victims
// into, across hops, via store_targets(merge:true) — fed to each subsequent
// hop's select_targets query as ExcludeRef so the chain can never bounce back
// onto a unit it has already hit (or the caster, via ExcludeSource). Named
// once here so every level of the recursive builder below uses the exact
// same key.
const chainLightningHitSetKey = "hit"

// compileChainLightningActions builds chain_lightning's authored bounce chain
// as nested launch_beam actions — the composable replacement for the
// pre-redesign launchProjectileConfig.ChainCount shim (executeChainLightning
// ShimLocked, ability_exec_projectile.go, which is kept dormant, unused by
// any compiled program after this change, for a later retirement task).
//
// Shape: level k=0 is the primary hit (spawns from the caster, targets
// ctx.InitialTarget); level k=1..ChainCount is bounce hop k (spawns from the
// PREVIOUS hop's impact position, targets the single victim its parent's
// select_targets query resolved). Each level's on_beam_impact trigger deals
// (DamageAmount - BounceDamageFalloff*k) damage to the unit the beam actually
// hit (SrcCurrentEvent), then — if a deeper hop would still deal positive
// damage — records that victim into the shared "hit" set and selects the
// nearest not-yet-hit enemy within BounceRange of it before recursing into
// the next level's launch_beam.
//
// PARITY WITH LEGACY (fireProcBeamLocked / nearestChainBounceTargetLocked,
// projectile.go / perks_siphoner.go): legacy folds the caster's spell
// modifiers into EffectiveSpell.Damage exactly ONCE — at cast time, off the
// RAW DamageAmount — then subtracts an UNFOLDED BounceDamageFalloff*hop from
// that single folded value for every hop (dmg := p.Damage - falloff*hop).
// deal_damage's own Execute (ability_program_registry.go) instead folds its
// OWN Config.Amount independently, every time it runs, through the identical
// applySpellModField/effectiveAbilityDamageLocked seam. Folding is additive
// (+add) then multiplicative (*mul); +add distributes over an integer
// subtraction exactly (fold(x)-c == fold(x-c) whenever hasMul is false, which
// covers every additive-only modifier) — but a MULTIPLICATIVE modifier does
// NOT distribute the same way (fold(x-c) != fold(x)-c when mul != 1), which
// is exactly what a naive "Amount: DamageAmount - falloff*k" per hop gets
// wrong under the golden test's own +50%-multiply modified_caster case.
// RECONCILED via dealDamageConfig.FlatOffset (see its doc comment): every
// hop's deal_damage sets Amount to the SAME def.DamageAmount as the primary
// (so it folds through the identical modifiers to the identical base,
// producing the identical folded P every time) and FlatOffset to the
// UNFOLDED -(BounceDamageFalloff*k), applied as the very last step after
// folding — reproducing legacy's "subtract a flat, unscaled amount from the
// already-scaled primary hit" exactly, instead of "scale a pre-reduced raw
// amount".
func compileChainLightningActions(def AbilityDef) []AbilityActionDef {
	return []AbilityActionDef{compileChainLightningHop(def, 0)}
}

// compileChainLightningHop builds level k of the chain (0 = primary hit, 1..
// ChainCount = bounce hops) as a single launch_beam action, recursing into
// k+1 only while the NEXT hop's damage would still be positive (mirrors
// legacy's `if dmg <= 0 { break }` — that hop, and every deeper one, is never
// emitted at all, not merely skipped at runtime). The raw (unfolded)
// DamageAmount/BounceDamageFalloff arithmetic below decides ONLY which
// levels get emitted at compile time — it cannot see runtime spell
// modifiers, so an extreme modifier could in principle make the compiled
// program's hop count differ from a fresh per-cast recomputation off the
// folded primary (legacy's own break condition IS runtime-folded). Not
// exercised by the shipped catalog fixture or the golden test's +50% case;
// see this task's report for the tradeoff.
func compileChainLightningHop(def AbilityDef, k int) AbilityActionDef {
	variant := def.Projectile
	offset := -def.BounceDamageFalloff * k
	suffix := strconv.Itoa(k) // every id below must be UNIQUE across the whole
	// compiled program (AbilityProgramValidate rejects repeats, even across
	// nesting levels — see the duplicate_id check), so each hop's action/
	// trigger ids carry their level number rather than reusing "dmg"/"store"/
	// "select"/"impact" at every depth.

	var query *TargetQueryDef
	var spawnOrigin TargetOrigin
	if k == 0 {
		query = &TargetQueryDef{Source: SrcInitialTarget}
		// spawnOrigin left "" — launch_beam's own default (caster position),
		// matching procSourceFromUnit(caster)'s OriginX/Y for the primary hit.
	} else {
		query = &TargetQueryDef{Source: SrcPrevActionTargets}
		spawnOrigin = OriginCurrentEventPos
	}

	impactActions := []AbilityActionDef{{
		ID:     "dmg" + suffix,
		Type:   ActionDealDamage,
		Target: &TargetQueryDef{Source: SrcCurrentEvent},
		Config: marshalConfig(dealDamageConfig{Amount: def.DamageAmount, Type: def.DamageType, FlatOffset: offset}),
	}}

	nextAmount := def.DamageAmount - def.BounceDamageFalloff*(k+1)
	if k < def.ChainCount && nextAmount > 0 {
		impactActions = append(impactActions,
			AbilityActionDef{
				ID:     "store" + suffix,
				Type:   ActionStoreTargets,
				Target: &TargetQueryDef{Source: SrcCurrentEvent},
				Config: marshalConfig(storeTargetsConfig{As: chainLightningHitSetKey, Merge: true}),
			},
			AbilityActionDef{
				ID:   "select" + suffix,
				Type: ActionSelectTargets,
				Target: &TargetQueryDef{
					Source:        SrcAllInScene,
					Origin:        OriginCurrentEventPos,
					Relations:     []TargetRelation{RelEnemy},
					Radius:        def.BounceRange,
					Ordering:      OrderClosest,
					MaxCount:      1,
					ExcludeSource: true,
					ExcludeRef:    &ContextRef{Key: chainLightningHitSetKey},
				},
			},
			compileChainLightningHop(def, k+1),
		)
	}

	return AbilityActionDef{
		ID:     "beam" + suffix,
		Type:   ActionBeam,
		Target: query,
		Config: marshalConfig(beamConfig{
			// Channeled defaults to false — a momentary bounce beam.
			Variant:     variant,
			SpawnOrigin: spawnOrigin,
			Triggers: []AbilityTriggerDef{
				{ID: "impact" + suffix, Type: TriggerOnBeamImpact, Actions: impactActions},
			},
		}),
	}
}

// compileProjectileImpactTrigger builds the on_projectile_impact trigger for
// a non-chain projectile ability: single-target damage to the unit the bolt
// actually hit (SrcCurrentEvent — bound to the hit unit by
// fireProjectileImpactLocked, projectile.go) when def.Radius <= 0
// (arcane_bolt's shape), or a splash select_targets(origin: impact_position,
// radius) -> deal_damage when def.Radius > 0 (fireball's shape).
//
// FIREBALL'S SPLASH-EXCLUDING-PRIMARY, made byte-identical without any
// exclusion filter: legacy's applyAbilitySplashDamageLocked explicitly skips
// the primary target (it already took the SAME damage amount directly) so it
// isn't hit twice. Here there is no separate "hit the primary directly" step
// at all — the splash query alone (all enemies within Radius of the impact
// point) already includes the primary, since the primary IS the impact point
// (distance 0 <= Radius), and deal_damage applies the SAME folded amount to
// every unit the query returns. One unified query, one deal_damage: exactly
// the legacy set of (unit, amount) pairs, with no double-hit and no need for
// includeInitialTarget/excludeSource (verified: both already do something
// else — see ability_exec_targeting.go — includeInitialTarget FORCES an
// out-of-radius target in, excludeSource drops the CASTER; neither is "drop
// the primary from a radius match", which this design doesn't need).
func compileProjectileImpactTrigger(def AbilityDef) AbilityTriggerDef {
	var actions []AbilityActionDef
	if def.Radius > 0 {
		actions = []AbilityActionDef{
			{
				ID:   "sel",
				Type: ActionSelectTargets,
				Target: &TargetQueryDef{
					Source:    SrcAllInScene,
					Origin:    OriginImpactPosition,
					Radius:    def.Radius,
					Relations: []TargetRelation{RelEnemy},
				},
				Outputs: map[string]string{"targets": "splashHits"},
			},
			{
				ID:     "dmg",
				Type:   ActionDealDamage,
				Input:  map[string]ContextRef{"targets": {Key: "splashHits"}},
				Config: marshalConfig(dealDamageConfig{Amount: def.DamageAmount, Type: def.DamageType}),
			},
		}
	} else {
		actions = []AbilityActionDef{
			{
				ID:      "sel",
				Type:    ActionSelectTargets,
				Target:  &TargetQueryDef{Source: SrcCurrentEvent},
				Outputs: map[string]string{"targets": "hit"},
			},
			{
				ID:     "dmg",
				Type:   ActionDealDamage,
				Input:  map[string]ContextRef{"targets": {Key: "hit"}},
				Config: marshalConfig(dealDamageConfig{Amount: def.DamageAmount, Type: def.DamageType}),
			},
		}
	}
	return AbilityTriggerDef{ID: projectileImpactTriggerID, Type: TriggerOnProjectileImpact, Actions: actions}
}

// compileTickingProjectileActions builds the single launch_projectile action
// for a def with DamagePerSecond>0 and no DamageAmount (arcane_orb's shape —
// see compileOffensiveActions' dispatch comment): a "direction" travelMode
// bolt with TickInterval>0, replacing the retired launch_vortex action type.
// No preceding select_targets: the orb never resolves a unit target set, it
// travels from the caster toward the cast point and pulls/damages whatever
// enters its radius along the way — see launchProjectileConfig's TickInterval
// doc comment and tickArcaneOrbProjectileLocked (projectile.go).
//
// Distance is baked from def.CastRange (the CastRange type, not a plain
// float64, so the "match_attack_range" sentinel round-trips exactly like the
// legacy def's own CastRange field — see launchProjectileConfig.Distance's
// doc comment).
func compileTickingProjectileActions(def AbilityDef) []AbilityActionDef {
	// Radius/PullStrength/DamagePerSecond/Type are DELIBERATELY NOT baked as
	// sibling scalars on launchProjectileConfig here (unlike before the
	// genuine-composition fix): the nested on_projectile_tick trigger below is
	// now the SOLE source of truth for those magnitudes (its select_targets'
	// radius, apply_force's strength, deal_damage's amount/type) — see
	// launchProjectileConfig's TickInterval doc comment for why a second,
	// unread copy of the same numbers was a live bug (apply_force's Mode was
	// only ever reachable through this now-removed dead copy). Only genuinely
	// projectile-level fields (art, scale, speed, distance, tickInterval,
	// travelMode) stay on this config.
	cfg := launchProjectileConfig{
		Projectile:      def.Projectile,
		TravelMode:      travelModeDirection,
		Distance:        def.CastRange,
		ProjectileScale: def.ProjectileScale,
		ProjectileSpeed: def.ProjectileSpeed,
		TickInterval:    arcaneOrbDamageIntervalSeconds,
		Triggers:        []AbilityTriggerDef{compileProjectileTickTrigger(def)},
	}
	return []AbilityActionDef{{
		ID:     "orb",
		Type:   ActionLaunchProjectile,
		Config: marshalConfig(cfg),
	}}
}

// compileProjectileTickTrigger builds the on_projectile_tick trigger for a
// ticking (vortex-shaped) launch_projectile action: select the hostiles
// currently within radius of the bolt's live position, pull them toward it,
// and deal this tick's share of damage. AS OF THE GENUINE-COMPOSITION FIX
// (ability_exec_projectile.go's executeTickingVortexShimLocked), this is no
// longer flow-display metadata — it is the trigger the executor actually
// extracts and RUNS every tick (select_targets/apply_force unthrottled for
// positional accuracy; deal_damage throttled to this tick trigger's own
// cadence via its Timing.TickInterval — see AbilityActionDef.Timing's doc
// comment and fireProjectileTickLocked, projectile.go). Editing this trigger's
// authored radius/strength/amount now has real runtime effect.
func compileProjectileTickTrigger(def AbilityDef) AbilityTriggerDef {
	perTick := int(math.Round(def.DamagePerSecond * arcaneOrbDamageIntervalSeconds))
	actions := []AbilityActionDef{
		{
			ID:   "sel",
			Type: ActionSelectTargets,
			Target: &TargetQueryDef{
				Source:    SrcAllInScene,
				Origin:    OriginProjectilePos,
				Radius:    def.Radius,
				Relations: []TargetRelation{RelEnemy},
			},
			Outputs: map[string]string{"targets": "vortexHits"},
		},
		{
			ID:     "dmg",
			Type:   ActionDealDamage,
			Timing: &ActionTiming{TickInterval: arcaneOrbDamageIntervalSeconds},
			Input:  map[string]ContextRef{"targets": {Key: "vortexHits"}},
			Config: marshalConfig(dealDamageConfig{Amount: perTick, Type: def.DamageType}),
		},
		{
			ID:     "force",
			Type:   ActionApplyForce,
			Input:  map[string]ContextRef{"targets": {Key: "vortexHits"}},
			Config: marshalConfig(applyForceConfig{Strength: def.PullStrength, Duration: arcaneOrbPullRefreshSeconds, Origin: OriginProjectilePos}),
		},
	}
	return AbilityTriggerDef{ID: "tick", Type: TriggerOnProjectileTick, Actions: actions}
}

// compileShatterActions builds the instant point-AoE shape (Shatter):
// select enemies in Radius around the cast point, deal damage, optionally
// apply the slow rider, optionally play the ground-burst presentation.
func compileShatterActions(def AbilityDef) []AbilityActionDef {
	actions := []AbilityActionDef{
		{
			ID:   "sel",
			Type: ActionSelectTargets,
			Target: &TargetQueryDef{
				Source:    SrcAllInScene,
				Origin:    OriginCastPoint,
				Radius:    def.Radius,
				Relations: []TargetRelation{RelEnemy},
			},
			Outputs: map[string]string{"targets": "hits"},
		},
		{
			ID:     "dmg",
			Type:   ActionDealDamage,
			Input:  map[string]ContextRef{"targets": {Key: "hits"}},
			Config: marshalConfig(dealDamageConfig{Amount: def.DamageAmount, Type: def.DamageType}),
		},
	}
	if slow, ok := compileSlowRider(def); ok {
		actions = append(actions, slow)
	}
	if def.EffectAtPoint != "" {
		actions = append(actions, AbilityActionDef{
			ID:   "vfx",
			Type: ActionPlayPresentation,
			Config: marshalConfig(playPresentationAtPointConfig{
				Asset:    def.EffectAtPoint,
				Position: ContextRef{Key: "castPoint"},
				Scale:    def.EffectScale,
			}),
		})
	}
	return actions
}

// compileInstantSingleTargetActions builds the plain instant-hit shape used
// by any offensive ability with no projectile, no delayed impact, and no
// point-AoE radius: select the initial cast target, deal damage, optionally
// apply the slow rider.
func compileInstantSingleTargetActions(def AbilityDef) []AbilityActionDef {
	actions := []AbilityActionDef{
		{
			ID:      "sel",
			Type:    ActionSelectTargets,
			Target:  &TargetQueryDef{Source: SrcInitialTarget},
			Outputs: map[string]string{"targets": "hits"},
		},
		{
			ID:     "dmg",
			Type:   ActionDealDamage,
			Input:  map[string]ContextRef{"targets": {Key: "hits"}},
			Config: marshalConfig(dealDamageConfig{Amount: def.DamageAmount, Type: def.DamageType}),
		},
	}
	if slow, ok := compileSlowRider(def); ok {
		actions = append(actions, slow)
	}
	return actions
}

// compileSlowRider builds the apply_status(slow) action riding on the "hits"
// context key produced by compileShatterActions / compileInstantSingleTargetActions,
// when the legacy def declares an on-hit slow. A SlowMultiplier outside
// (0,1) is the legacy "no slow" sentinel (slowTargetLocked rejects it too),
// so it is omitted rather than compiled into an inert action.
func compileSlowRider(def AbilityDef) (AbilityActionDef, bool) {
	if def.SlowMultiplier <= 0 || def.SlowMultiplier >= 1 {
		return AbilityActionDef{}, false
	}
	return AbilityActionDef{
		ID:    "slow",
		Type:  ActionApplyStatus,
		Input: map[string]ContextRef{"targets": {Key: "hits"}},
		Config: marshalConfig(applyStatusConfig{
			Status:     "slow",
			Multiplier: def.SlowMultiplier,
			Duration:   def.SlowDurationSeconds,
			School:     def.DamageType,
		}),
	}, true
}

// ── meteor: delayed impact + burn zone ─────────────────────────────────────

// meteorPresentationID names the single top-level PresentationInstanceDef a
// compiled delayed-impact ability emits; the on_cast_complete play_presentation
// action's config references it by this id (design doc §5.2's "presentationId").
const meteorPresentationID = "p_meteor"

// compileMeteorActions builds the delayed-impact + burn-zone shape (Meteor):
// an on_cast_complete play_presentation action that kicks off the falling
// sprite, plus a top-level PresentationInstanceDef carrying the "impact"
// animation-marker trigger (select impact-radius enemies -> deal damage ->
// optionally spawn a bursting Burning Crater zone). Matches the design doc
// §5.2 fixture shape exactly (the impact trigger lives on the Presentation,
// not on the play_presentation action's Children) so the fixture and this
// compiler output agree.
func compileMeteorActions(def AbilityDef) ([]AbilityActionDef, []PresentationInstanceDef) {
	castAction := AbilityActionDef{
		ID:   "meteor",
		Type: ActionPlayPresentation,
		Config: marshalConfig(playPresentationAtPointConfig{
			Asset:          def.EffectAtPoint,
			Position:       ContextRef{Key: "castPoint"},
			Scale:          def.EffectScale,
			PresentationID: meteorPresentationID,
		}),
	}

	impactActions := []AbilityActionDef{
		{
			ID:   "sel",
			Type: ActionSelectTargets,
			Target: &TargetQueryDef{
				Source:    SrcAllInScene,
				Origin:    OriginImpactPosition,
				Radius:    def.Radius,
				Relations: []TargetRelation{RelEnemy},
			},
			Outputs: map[string]string{"targets": "hitEnemies"},
		},
		{
			ID:     "dmg",
			Type:   ActionDealDamage,
			Input:  map[string]ContextRef{"targets": {Key: "hitEnemies"}},
			Config: marshalConfig(dealDamageConfig{Amount: def.DamageAmount, Type: def.DamageType}),
		},
	}
	if def.BurnDurationSeconds > 0 {
		impactActions = append(impactActions, AbilityActionDef{
			ID:     "zone",
			Type:   ActionCreateZone,
			Config: marshalConfig(compileMeteorZoneConfig(def)),
		})
	}

	impactTrigger := AbilityTriggerDef{
		ID:      "impact",
		Type:    TriggerOnAnimationMarker,
		Timing:  &TriggerTiming{Marker: "impact", DelaySeconds: def.ImpactDelaySeconds},
		Actions: impactActions,
	}

	presentation := PresentationInstanceDef{
		ID:          meteorPresentationID,
		Asset:       def.EffectAtPoint,
		PositionRef: ContextRef{Key: "castPoint"},
		Scale:       def.EffectScale,
		Triggers:    []AbilityTriggerDef{impactTrigger},
	}

	// impact marker fires via the on_animation_marker scheduler (Phase 6b,
	// Task 2) DelaySeconds after cast, matching ImpactDelaySeconds above.
	return []AbilityActionDef{castAction}, []PresentationInstanceDef{presentation}
}

// compileMeteorZoneConfig builds the create_zone config for the burning
// crater left by a delayed-impact ability's burn fields. TickInterval is set
// from def.BurnTickIntervalSeconds so the compiled on_zone_tick trigger
// passes validateAbilityProgram's tick-trigger requirement (timing.tickInterval > 0).
func compileMeteorZoneConfig(def AbilityDef) createZoneConfig {
	burnTrigger := AbilityTriggerDef{
		ID:     "burn",
		Type:   TriggerOnZoneTick,
		Timing: &TriggerTiming{TickInterval: def.BurnTickIntervalSeconds},
		Actions: []AbilityActionDef{
			{
				ID:   "bsel",
				Type: ActionSelectTargets,
				Target: &TargetQueryDef{
					Source:    SrcAllInScene,
					Origin:    OriginZoneCenter,
					Radius:    def.BurnRadius,
					Relations: []TargetRelation{RelEnemy},
				},
				Outputs: map[string]string{"targets": "burnHits"},
			},
			{
				ID:     "bdmg",
				Type:   ActionDealDamage,
				Input:  map[string]ContextRef{"targets": {Key: "burnHits"}},
				Config: marshalConfig(dealDamageConfig{Amount: def.BurnDamagePerTick, Type: def.DamageType}),
			},
		},
	}
	return createZoneConfig{
		Name:              "Burning Crater",
		PositionRef:       &ContextRef{Key: "impactPosition"},
		Radius:            def.BurnRadius,
		Duration:          def.BurnDurationSeconds,
		TickInterval:      def.BurnTickIntervalSeconds,
		OwnerRef:          &ContextRef{Key: "caster"},
		Presentation:      def.BurnEffectAtPoint,
		PresentationScale: def.EffectScale,
		Triggers:          []AbilityTriggerDef{burnTrigger},
	}
}

// marshalConfig encodes v as an AbilityActionDef.Config. A marshal error here
// would mean a programmer error in one of this file's own static config
// structs (never reachable in practice — every field is a primitive/string/
// slice/nested-struct built from AbilityDef's own already-validated data), so
// this falls back to nil (an empty/omitted config) rather than panicking,
// keeping compileLegacyAbility a pure function with no error return.
func marshalConfig(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
