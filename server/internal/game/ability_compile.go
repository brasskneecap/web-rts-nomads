package game

import "encoding/json"

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

// channelBeamConfig is the compiled config for channel_beam (siphon_life's
// composable migration). ActionChannelBeam has a registered ActionDescriptor
// (ability_exec_channel.go): its Execute delegates to the SAME seam the
// legacy channel-start path uses (startChannelLocked — see
// ability_channel.go's file doc comment), so every field the channel
// lifecycle reads off an AbilityDef (via channelSpecFor) must be baked in
// here rather than read back off a def at execute time — a converted
// (schemaVersion 2) ability has its legacy mechanic fields cleared (see
// ConvertLegacyAbility), so Config is this action's ONLY source of truth for
// them once that conversion happens. ChannelType round-trips too (mirroring
// chargeFireVolleyConfig's Targeting field): describeAbility's
// abilityMechanicsShadow recovery needs it to restore the shadow def's
// ChannelType so describeLegacyAbility's dispatch switch still selects
// describeChannelAbility — see ability_describe.go.
type channelBeamConfig struct {
	ChannelType         string  `json:"channelType"`
	TickIntervalSeconds float64 `json:"tickIntervalSeconds"`
	ManaCostPerTick     int     `json:"manaCostPerTick,omitempty"`
	DamagePerTick       int     `json:"damagePerTick,omitempty"`
	HealingMultiplier   float64 `json:"healingMultiplier,omitempty"`
	AllyHealRadius      float64 `json:"allyHealRadius,omitempty"`
}

// compileChannelBeamAction builds the single channel_beam action for a
// channel-type def (siphon_life's shape). Carries its own Target query
// (SrcInitialTarget, matching compileProjectileActions' single-target cast
// precedent) rather than a preceding select_targets action.
func compileChannelBeamAction(def AbilityDef) AbilityActionDef {
	cfg := channelBeamConfig{
		ChannelType:         def.ChannelType,
		TickIntervalSeconds: def.TickIntervalSeconds,
		ManaCostPerTick:     def.ManaCostPerTick,
		DamagePerTick:       def.DamagePerTick,
		HealingMultiplier:   def.HealingMultiplier,
		AllyHealRadius:      def.AllyHealRadius,
	}
	return AbilityActionDef{
		ID:     "channel",
		Type:   ActionChannelBeam,
		Target: &TargetQueryDef{Source: SrcInitialTarget},
		Config: marshalConfig(cfg),
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
		return compileVortexActions(def), nil
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
// ActionLaunchProjectile has a registered ActionDescriptor (Phase 6c;
// ability_exec_projectile.go): it delegates to the SAME seams the legacy
// cast resolver uses (fireAbilityProjectileLocked / fireAbilityChainLocked),
// so every field the projectile/chain mechanic reads from an AbilityDef must
// be baked in here rather than read back off a def at execute time — a
// converted (schemaVersion 2) ability has its legacy mechanic fields cleared
// (see ConvertLegacyAbility), so Config is this action's ONLY source of
// truth once that conversion happens.
type launchProjectileConfig struct {
	Projectile string     `json:"projectile"`
	Amount     int        `json:"amount"`
	Type       DamageType `json:"type,omitempty"`
	// Radius is the impact splash radius (fireball). 0 ⇒ single-target impact.
	// Deliberately NOT modifier-scaled (mirrors dealDamageConfig.Radius, which
	// is likewise never folded by the executor today — no action's Radius is).
	Radius float64 `json:"radius,omitempty"`
	// ProjectileScale / MinorDamage are presentation/rendering passthroughs
	// (Projectile.Scale / Projectile.MinorDamage) baked in for the same
	// "Config is the sole authority post-conversion" reason as Radius.
	ProjectileScale     float64 `json:"projectileScale,omitempty"`
	MinorDamage         bool    `json:"minorDamage,omitempty"`
	ChainCount          int     `json:"chainCount,omitempty"`
	BounceRange         float64 `json:"bounceRange,omitempty"`
	BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
}

// compileProjectileActions builds the single launch_projectile action for a
// def with a non-empty Projectile. The action carries its own Target query
// (SrcInitialTarget, matching compileInstantSingleTargetActions' "sel" query)
// rather than a preceding select_targets action, since arcane_bolt/fireball/
// chain_lightning are all single-target casts — this doubles as the "target
// still alive" guard (the query's default AliveState excludes HP<=0), mirroring
// legacy's `eff.Damage > 0 && target.HP > 0` gate in resolveAbilityCastOnTargetLocked.
func compileProjectileActions(def AbilityDef) []AbilityActionDef {
	cfg := launchProjectileConfig{
		Projectile:      def.Projectile,
		Amount:          def.DamageAmount,
		Type:            def.DamageType,
		Radius:          def.Radius,
		ProjectileScale: def.ProjectileScale,
		MinorDamage:     def.MinorDamage,
	}
	if def.ChainCount > 0 {
		cfg.ChainCount = def.ChainCount
		cfg.BounceRange = def.BounceRange
		cfg.BounceDamageFalloff = def.BounceDamageFalloff
	}
	return []AbilityActionDef{{
		ID:     "proj",
		Type:   ActionLaunchProjectile,
		Target: &TargetQueryDef{Source: SrcInitialTarget},
		Config: marshalConfig(cfg),
	}}
}

// launchVortexConfig is the compiled config for launch_vortex (arcane_orb's
// moving pull+DoT vortex). ActionLaunchVortex has a registered
// ActionDescriptor (ability_exec_vortex.go): it delegates to the SAME seam
// the legacy point-cast resolver uses (spawnArcaneOrbLocked), so every field
// spawnArcaneOrbLocked/tickArcaneOrbProjectileLocked read from an AbilityDef/
// EffectiveSpell must be baked in here rather than read back off a def at
// execute time — a converted (schemaVersion 2) ability has its legacy
// mechanic fields cleared (see ConvertLegacyAbility), so Config is this
// action's ONLY source of truth for them once that conversion happens.
//
// CastRange is the one exception: it is baked here too (rather than read
// live off ctx.abilityDef) so this action's Config is fully self-contained
// even outside the normal resolveAbilityProgramCastLocked entry point (which
// always sets ctx.abilityDef) — mirroring how launchProjectileConfig bakes
// Radius/ProjectileScale/etc. defensively. It carries the CastRange type
// (not a plain float64) so the "match_attack_range" sentinel round-trips
// exactly like the legacy def's own CastRange field.
type launchVortexConfig struct {
	Projectile      string     `json:"projectile"`
	ProjectileScale float64    `json:"projectileScale,omitempty"`
	ProjectileSpeed float64    `json:"projectileSpeed,omitempty"`
	Radius          float64    `json:"radius,omitempty"`
	PullStrength    float64    `json:"pullStrength"`
	DamagePerSecond float64    `json:"damagePerSecond,omitempty"`
	Type            DamageType `json:"type,omitempty"`
	CastRange       CastRange  `json:"castRange"`
}

// compileVortexActions builds the single launch_vortex action for a def with
// DamagePerSecond>0 and no DamageAmount (arcane_orb's shape — see
// compileOffensiveActions' dispatch comment). No preceding select_targets:
// the orb never resolves a unit target set, it travels from the caster
// toward the cast point and pulls/damages whatever enters its radius along
// the way — see ability_exec_vortex.go.
func compileVortexActions(def AbilityDef) []AbilityActionDef {
	cfg := launchVortexConfig{
		Projectile:      def.Projectile,
		ProjectileScale: def.ProjectileScale,
		ProjectileSpeed: def.ProjectileSpeed,
		Radius:          def.Radius,
		PullStrength:    def.PullStrength,
		DamagePerSecond: def.DamagePerSecond,
		Type:            def.DamageType,
		CastRange:       def.CastRange,
	}
	return []AbilityActionDef{{
		ID:     "orb",
		Type:   ActionLaunchVortex,
		Config: marshalConfig(cfg),
	}}
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
