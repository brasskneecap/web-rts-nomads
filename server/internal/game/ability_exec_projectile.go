package game

import (
	"encoding/json"
	"math"
	"strconv"
)

// ─────────────────────────────────────────────────────────────────────────────
// launch_projectile executor — composable delivery primitive redesign.
//
// launch_projectile is now SPAWN + TRAVEL ONLY: damage, damage type, splash,
// and status all live in a nested on_projectile_impact trigger carried on
// the compiled config's Triggers field (the create_zone precedent —
// config.triggers, not Children, so it never auto-fires via
// on_action_complete; it fires only when the spawned Projectile actually
// LANDS, driven by landProjectileLocked -> fireProjectileImpactLocked,
// projectile.go). This is what makes an impact composable: "on impact, play
// a presentation, deal damage, apply a status" is now three ordinary,
// independently-authorable actions under one on_projectile_impact trigger,
// instead of baked Amount/Type/Radius fields on this action.
//
// chain_lightning is the ONE exception, KEPT UNCHANGED: its bounce isn't a
// projectile delivery at all — fireAbilityChainLocked resolves the whole
// bounce chain inline as Beams at fire time, and never spawns a Projectile.
// Its Amount/Type/ChainCount/BounceRange/BounceDamageFalloff/MinorDamage stay
// baked directly on this action's config and keep delegating to
// fireAbilityChainLocked exactly as before this redesign — gated on
// cfg.ChainCount > 0. Do NOT try to compose it (see launchProjectileConfig's
// doc comment, ability_compile.go).
//
// TRAVEL MODES:
//   - "to_target" (default/empty): homes on the resolved unit target every
//     tick, exactly like the pre-redesign behavior. Spawn is
//     fireProjectileWithImpactActionsLocked (projectile.go).
//   - "direction": flies in a straight line and keeps going (arcane_orb's
//     straight-line flight, reusing its PierceLength/PierceDirX/PierceDirY
//     geometry) rather than homing on a live unit every tick. The aim point
//     is fixed once, at LAUNCH time: the resolved target's position (this
//     action's OWN incoming `targets` — its declared "source" query, an
//     Input["targets"] ref, or a preceding select_targets' output, same
//     resolution every other action uses) when one resolved, else
//     ctx.CastPoint (a point-cast with nothing resolved to aim at). See
//     launchDirectionalProjectileLocked (projectile.go) for the full design,
//     including the "what does impact mean for a through-flying bolt"
//     decision. Not used by any shipped catalog ability today (arcane_bolt/
//     fireball are both "to_target"); implemented and tested so a future
//     ability can opt in.
//
// ─────────────────────────────────────────────────────────────────────────────
// CROSS-TICK OP BUDGET (RuntimeAbilityContext.sharedOpsRemaining)
//
// A fresh RuntimeAbilityContext with a fresh opsUsed=0 is built for every
// projectile impact (it fires on a LATER tick than the cast that launched
// it — see fireProjectileImpactLocked). Naively resetting the op budget for
// every impact would mean a projectile whose impact launches ANOTHER
// projectile (a future composable ability; none shipped today) has NO
// cumulative bound in the tick loop, under s.mu — and depth alone does not
// fix it: N projectiles each independently getting a fresh maxExecutionOps
// budget, each relaunching M more, is N*M*maxExecutionOps work, not
// maxExecutionOps.
//
// Fix: every projectile carries ImpactOpsBudget, a *int SHARED (by pointer,
// not by value) with every other projectile descended — directly or
// transitively — from the SAME original cast. Execute below seeds it once,
// at the top of a lineage, from the LAUNCHING ctx's own remaining budget
// (ctx.sharedOpsRemaining if this launch is itself running inside another
// projectile's impact, else maxExecutionOps-ctx.opsUsed); every projectile
// spawned from a single Execute call (however many targets it hits) shares
// that SAME pointer. At impact time, fireProjectileImpactLocked builds its
// ctx with sharedOpsRemaining pointed at that same shared counter, so
// ctx.opsExhausted()/consumeOp() (ability_exec.go) decrement ONE shared total
// instead of resetting — bounding the ENTIRE lineage's work (breadth AND
// depth) by one number, regardless of how many ticks it spans or how many
// projectiles branch off. See TestLaunchProjectile_ImpactRelaunchChain_
// SharedBudgetTerminates for the termination proof.
// ─────────────────────────────────────────────────────────────────────────────
//
// DOUBLE-FOLD HAZARD — read before touching amount computation anywhere
// downstream of this action:
// legacy folds a spell's modifiers ONCE, at effectiveSpellLocked. The
// composable model folds ONCE too, but now that fold happens inside
// deal_damage's Execute (ability_program_registry.go), which gates on
// ctx.abilityDef != nil — NOT here. This action must NEVER fold cfg.Amount
// (the chain shim is the sole surviving exception, since it delegates to the
// legacy fireAbilityChainLocked seam which expects an already-folded
// EffectiveSpell.Damage, exactly as before this redesign). For the
// composable (non-chain) path, fireProjectileImpactLocked's ctx MUST set
// abilityDef (re-resolved via getAbilityDef) so deal_damage folds exactly
// once when the impact fires — see that function's doc comment. Golden tests
// with a modified caster (TestAbilityCompileGolden_ArcaneBolt/Fireball's
// modified_caster sub-tests) are what catch a double-fold or a missing fold.
// ─────────────────────────────────────────────────────────────────────────────

// travelModeToTarget / travelModeDirection are launchProjectileConfig's
// TravelMode values. An empty string is treated identically to
// travelModeToTarget (the pre-redesign default behavior).
const (
	travelModeToTarget  = "to_target"
	travelModeDirection = "direction"
)

// launchProjectileSpawnOriginOptions is the Options list offered by
// launch_projectile's spawnOrigin schema field: ONLY the TargetOrigin values
// s.resolveOriginLocked (ability_exec_targeting.go) actually resolves to a
// real, non-caster-fallback position. OriginProjectilePos/OriginStatusOwner/
// OriginSummonedUnit are deliberately withheld — resolveOriginLocked's own
// doc comment documents they silently fall back to the caster's position
// (no projectile/status/summon runtime context is threaded into
// RuntimeAbilityContext yet, its TODO(phase-3b)) — offering one here would
// be exactly the "inert dropdown option" bug per-action schema declarations
// exist to prevent (see SchemaField's doc comment, ability_program_registry.go).
// OriginNamedContextValue is withheld too: it requires an OriginRef this
// action's config has no field for, so selecting it would silently resolve
// to the caster (no ref -> resolveOriginLocked's ref==nil branch) — an
// equally inert option without one.
var launchProjectileSpawnOriginOptions = []string{
	string(OriginCaster), string(OriginCastPoint), string(OriginImpactPosition),
	string(OriginCurrentEventPos), string(OriginZoneCenter),
	string(OriginInitialTarget), string(OriginInitialTargetPos),
}

// isValidLaunchProjectileSpawnOrigin reports whether origin is either unset
// (the byte-identical caster default) or one of
// launchProjectileSpawnOriginOptions' offered values.
func isValidLaunchProjectileSpawnOrigin(origin TargetOrigin) bool {
	if origin == "" {
		return true
	}
	for _, o := range launchProjectileSpawnOriginOptions {
		if string(origin) == o {
			return true
		}
	}
	return false
}

func (launchProjectileConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionLaunchProjectile,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c launchProjectileConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(launchProjectileConfig)
			var out []ValidationIssue
			if c.Projectile == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile requires projectile", Severity: "error"})
			}
			switch c.TravelMode {
			case "", travelModeToTarget, travelModeDirection:
			default:
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "unknown travelMode " + c.TravelMode, Severity: "error"})
			}
			// c.Distance is a CastRange: any negative value is the
			// "match_attack_range" sentinel (CastRange.MatchesAttackRange), not
			// an error — no CastRange field anywhere else in this codebase
			// rejects negative values either. Nothing to validate here.
			if !isValidLaunchProjectileSpawnOrigin(c.SpawnOrigin) {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "unknown spawnOrigin " + string(c.SpawnOrigin), Severity: "error"})
			}
			if c.ChainCount > 0 {
				// chain_lightning's kept shim — same requirements as before this
				// redesign (its damage is baked here, never composed).
				if c.Amount <= 0 {
					out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile (chain) requires amount > 0", Severity: "error"})
				}
				if c.Type != "" && !IsValidDamageType(c.Type) {
					out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
				}
			}
			if c.TickInterval > 0 {
				// arcane_orb's ticking-vortex shim — see launchProjectileConfig's
				// TickInterval doc comment. The magnitudes now live SOLELY in the
				// nested on_projectile_tick trigger (no top-level sibling scalars
				// to check instead) — validate those.
				trig, hasTrig := extractVortexTickTrigger(c.Triggers)
				if !hasTrig {
					out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile (vortex) requires an on_projectile_tick trigger", Severity: "error"})
				} else {
					mag := vortexMagnitudesFromTrigger(trig)
					if mag.radius <= 0 {
						out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile (vortex) requires its on_projectile_tick select_targets radius > 0", Severity: "error"})
					}
					if mag.pullStrength <= 0 {
						out = append(out, ValidationIssue{Code: "empty_required_property", Message: "launch_projectile (vortex) requires its on_projectile_tick apply_force strength > 0", Severity: "error"})
					}
					if mag.damageType != "" && !IsValidDamageType(mag.damageType) {
						out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(mag.damageType), Severity: "error"})
					}
				}
			}
			return out
		},
		// Field-visibility notes (see SchemaField/FieldCondition's doc comments,
		// ability_program_registry.go):
		//   - "target" (target_query): meaningful in BOTH travel modes — every
		//     resolved id in `targets` gets its own homing bolt in "to_target"
		//     mode, and the first resolved id is the fixed-at-launch aim point
		//     in "direction" mode (see the TRAVEL MODES doc above and
		//     launchDirectionalProjectileLocked, projectile.go). No ShowWhen:
		//     hiding it for "direction" would make a resolved-target-aimed
		//     directional bolt impossible to author.
		//   - "distance": only launchDirectionalProjectileLocked reads
		//     c.Distance (projectile.go); the "to_target" branch never does.
		//     Gated on travelMode == "direction".
		//   - "spawnOrigin": meaningful in BOTH travel modes too (spawn point
		//     for "to_target"; spawn point AND flight-direction anchor for
		//     "direction" — see SpawnOrigin's doc comment, ability_compile.go).
		//     No ShowWhen for the same reason as "target".
		//   - "amount"/"type"/"bounceRange"/"bounceDamageFalloff": read ONLY by
		//     executeChainLightningShimLocked, the ChainCount>0 branch kept
		//     unchanged for chain_lightning (see the file doc comment) — dead
		//     weight on every normal (non-chaining) bolt. Gated on
		//     chainCount > 0. chainCount ITSELF stays unconditionally visible:
		//     it's the toggle that turns chaining on, so gating it on its own
		//     value would make it impossible to ever set above 0.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "projectile", Label: "Projectile", Control: "asset", Section: "Presentation"},
			{Key: "travelMode", Label: "Travel Mode", Control: "enum", Options: []string{travelModeToTarget, travelModeDirection}, Section: "Properties"},
			{
				Key: "target", Label: "Target", Control: "target_query", Section: "Targeting",
				TargetQueryFields: targetQueryFieldsSourceOnly,
			},
			{
				Key: "distance", Label: "Distance", Control: "number", Section: "Properties",
				ShowWhen: &FieldCondition{Key: "travelMode", Op: "eq", Value: json.RawMessage(strconv.Quote(travelModeDirection))},
			},
			{Key: "spawnOrigin", Label: "Spawn Origin", Control: "enum", Options: launchProjectileSpawnOriginOptions, Section: "Properties"},
			{Key: "projectileScale", Label: "Projectile Scale", Control: "number", Section: "Presentation"},
			{Key: "chainCount", Label: "Chain Count", Control: "number", Section: "Properties"},
			{
				Key: "amount", Label: "Amount (chain only)", Control: "number", Section: "Properties",
				ShowWhen: &FieldCondition{Key: "chainCount", Op: "gt", Value: json.RawMessage("0")},
			},
			{
				Key: "type", Label: "Damage Type (chain only)", Control: "enum", Section: "Properties",
				ShowWhen: &FieldCondition{Key: "chainCount", Op: "gt", Value: json.RawMessage("0")},
			},
			{
				Key: "bounceRange", Label: "Bounce Range", Control: "number", Section: "Properties",
				ShowWhen: &FieldCondition{Key: "chainCount", Op: "gt", Value: json.RawMessage("0")},
			},
			{
				Key: "bounceDamageFalloff", Label: "Bounce Damage Falloff", Control: "number", Section: "Properties",
				ShowWhen: &FieldCondition{Key: "chainCount", Op: "gt", Value: json.RawMessage("0")},
			},
			// arcane_orb's ticking-vortex shim: tickInterval is the toggle (stays
			// unconditionally visible, like chainCount) and is the only vortex
			// knob left on THIS action — radius/pullStrength/damagePerSecond are
			// NOT declared here any more. They used to be (as inert top-level
			// sibling scalars Execute never read once the nested
			// on_projectile_tick trigger existed — a real "editing this field
			// does nothing" bug, the exact failure mode SchemaField declarations
			// exist to prevent). The real, live knobs are the nested trigger's
			// own select_targets radius / apply_force strength+mode / deal_damage
			// amount+type, already rendered as editable FlowTriggerCards under
			// this action via config.triggers (the same recursive-card mechanism
			// apply_status's authored triggers use — see that config's Schema
			// doc comment).
			{Key: "tickInterval", Label: "Tick Interval (vortex only)", Control: "duration", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(launchProjectileConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}

			if c.ChainCount > 0 {
				return executeChainLightningShimLocked(s, ctx, caster, c, targets)
			}

			if c.TickInterval > 0 {
				return executeTickingVortexShimLocked(s, ctx, caster, c, targets)
			}

			// Shared cross-tick op budget for this lineage — see the file doc's
			// CROSS-TICK OP BUDGET section. Reused verbatim if this launch is
			// itself running inside a shared-budget impact ctx (a relaunch);
			// otherwise minted fresh from however much of ctx's own budget
			// remains.
			var budget *int
			if ctx.sharedOpsRemaining != nil {
				budget = ctx.sharedOpsRemaining
			} else {
				remaining := maxExecutionOps - ctx.opsUsed
				budget = &remaining
			}

			// Extract the compiled on_projectile_impact trigger's actions (the
			// nested-trigger slot — see launchProjectileConfig's doc comment).
			// Absent/empty is a valid, if pointless, authoring: the bolt spawns
			// and travels but deals no damage on impact.
			var impactActions []AbilityActionDef
			for _, trig := range c.Triggers {
				if trig.Type == TriggerOnProjectileImpact {
					impactActions = trig.Actions
					break
				}
			}

			if c.TravelMode == travelModeDirection {
				s.launchDirectionalProjectileLocked(caster, ctx, c, impactActions, budget, targets)
				return nil
			}

			// SpawnOrigin resolves to the caster's own position for the
			// unset/"caster" default (resolveOriginLocked's own default case) —
			// the byte-identical geometry every ability compiled before this
			// field existed keeps. See launchProjectileConfig.SpawnOrigin's doc
			// comment (ability_compile.go).
			originPos := s.resolveOriginLocked(ctx, c.SpawnOrigin, nil)

			hit := make([]int, 0, len(targets))
			for _, id := range targets {
				target := s.getUnitByIDLocked(id)
				// Mirror legacy's guard: resolveAbilityCastOnTargetLocked/the
				// compiled Target query already filter dead targets, but a
				// hand-authored program could feed this action a stale/dead id via
				// Input["targets"] — re-check defensively, same discipline as
				// every other registered action's per-target loop.
				if target == nil || target.HP <= 0 {
					continue
				}
				s.fireProjectileWithImpactActionsLocked(caster, target, originPos, c.Projectile, c.ProjectileScale, ctx.AbilityID, impactActions, budget, ctx.effectiveDamageMultiplier())
				hit = append(hit, id)
				ctx.trace("projectile_launched", ctx.currentActionPath, map[string]any{
					"target": id, "travelMode": travelModeToTarget,
				})
			}
			return hit
		},
	})
}

// executeChainLightningShimLocked runs the ChainCount>0 branch: identical to
// this action's pre-redesign Execute for chain_lightning, unchanged. Folds
// cfg.Amount EXACTLY ONCE (the DOUBLE-FOLD hazard applies here, not to the
// composable non-chain path above — see the file doc comment) and delegates
// to fireAbilityChainLocked, which resolves the whole bounce chain inline as
// Beams and never spawns a Projectile.
func executeChainLightningShimLocked(s *GameState, ctx *RuntimeAbilityContext, caster *Unit, c launchProjectileConfig, targets []int) []int {
	amount := c.Amount
	if ctx.abilityDef != nil {
		amount = s.effectiveAbilityDamageLocked(caster, *ctx.abilityDef, c.Amount)
	}
	if m := ctx.effectiveDamageMultiplier(); m != 1.0 {
		amount = int(math.Round(float64(amount) * m))
	}
	if amount <= 0 {
		ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "non_positive_damage", "amount": amount})
		return nil
	}

	def := AbilityDef{
		ID:                  ctx.AbilityID,
		DamageType:          c.Type,
		Projectile:          c.Projectile,
		ProjectileScale:     c.ProjectileScale,
		MinorDamage:         c.MinorDamage,
		BounceRange:         c.BounceRange,
		BounceDamageFalloff: c.BounceDamageFalloff,
	}
	eff := EffectiveSpell{Damage: amount, ChainCount: c.ChainCount}

	hit := make([]int, 0, len(targets))
	for _, id := range targets {
		target := s.getUnitByIDLocked(id)
		if target == nil || target.HP <= 0 {
			continue
		}
		s.fireAbilityChainLocked(caster, target, def, eff)
		hit = append(hit, id)
		ctx.trace("projectile_launched", ctx.currentActionPath, map[string]any{
			"target": id, "amount": amount, "chainCount": eff.ChainCount,
		})
	}
	return hit
}

// vortexTickMagnitudes are the vortex's editable magnitudes as recovered by
// walking an on_projectile_tick trigger's own actions — the SINGLE SOURCE OF
// TRUTH for the vortex shape (see launchProjectileConfig's TickInterval doc
// comment for why these no longer also live as sibling scalars on the
// enclosing launch_projectile config). damageAmount is the AUTHORED per-tick
// chunk exactly as compiled/edited (e.g. compileProjectileTickTrigger bakes
// round(DamagePerSecond*TickInterval)) — NOT a per-second rate; see
// fireProjectileTickLocked's ROUNDING DECISION doc comment (projectile.go)
// for how it's scaled at firing time.
type vortexTickMagnitudes struct {
	radius       float64
	pullStrength float64
	damageAmount int
	damageType   DamageType
}

// extractVortexTickTrigger returns the first TriggerOnProjectileTick trigger
// in triggers (a launch_projectile config's own Triggers field), mirroring
// the on_projectile_impact extraction in this action's base Execute. ok is
// false when none is present (a malformed/incomplete authoring — Validate
// flags this as an error; Execute degrades to a no-op vortex, matching every
// other "declared but not authored" degrade in this executor).
func extractVortexTickTrigger(triggers []AbilityTriggerDef) (trig AbilityTriggerDef, ok bool) {
	for _, t := range triggers {
		if t.Type == TriggerOnProjectileTick {
			return t, true
		}
	}
	return AbilityTriggerDef{}, false
}

// vortexMagnitudesFromTrigger walks trig's own actions (select_targets,
// apply_force, deal_damage) and recovers their authored magnitudes. Used by
// BOTH executeTickingVortexShimLocked/Validate (ability_exec_projectile.go,
// this file) and abilityMechanicsShadow's recovery (ability_describe.go) —
// one shared reader for the one authored shape, so the two call sites can
// never drift on what "radius"/"pullStrength"/"damageAmount" mean here.
// Missing actions/fields degrade to their zero value, same as
// decodeActionConfig's own missing-config degrade.
func vortexMagnitudesFromTrigger(trig AbilityTriggerDef) vortexTickMagnitudes {
	var m vortexTickMagnitudes
	for _, act := range trig.Actions {
		switch act.Type {
		case ActionSelectTargets:
			if act.Target != nil {
				m.radius = act.Target.Radius
			}
		case ActionApplyForce:
			var cfg applyForceConfig
			decodeActionConfig(act.Config, &cfg)
			m.pullStrength = cfg.Strength
		case ActionDealDamage:
			var cfg dealDamageConfig
			decodeActionConfig(act.Config, &cfg)
			m.damageAmount = cfg.Amount
			m.damageType = cfg.Type
		}
	}
	return m
}

// freezeVortexTickActions clones trigActions (an authored on_projectile_tick
// trigger's actions) and overwrites select_targets' radius / apply_force's
// strength with the ALREADY-FOLDED values computed once at launch — apply_force
// has no fold seam of its own (unlike deal_damage, which folds itself every
// firing via ctx.abilityDef — see fireProjectileTickLocked), so this is the
// only place a caster's Radius/PullStrength spell modifiers can apply.
// deal_damage's own action is copied UNCHANGED (its authored amount is the
// base that folds per-firing, not frozen here) — see
// launchProjectileConfig's TickInterval doc comment. The returned slice is
// carried on the spawned Projectile (Projectile.TickActions) and is what
// fireProjectileTickLocked actually runs every tick.
func freezeVortexTickActions(trigActions []AbilityActionDef, radius, pullStrength float64) []AbilityActionDef {
	out := make([]AbilityActionDef, len(trigActions))
	for i, a := range trigActions {
		out[i] = a
		switch a.Type {
		case ActionSelectTargets:
			if a.Target != nil {
				q := *a.Target
				q.Radius = radius
				out[i].Target = &q
			}
		case ActionApplyForce:
			var cfg applyForceConfig
			decodeActionConfig(a.Config, &cfg)
			cfg.Strength = pullStrength
			out[i].Config = marshalConfig(cfg)
		}
	}
	return out
}

// executeTickingVortexShimLocked runs the TickInterval>0 branch: arcane_orb's
// moving pull+DoT vortex shape. Recovers radius/pullStrength/damage
// magnitudes from the AUTHORED on_projectile_tick trigger (the single source
// of truth — see launchProjectileConfig's TickInterval doc comment, and
// vortexMagnitudesFromTrigger above), folds radius/pullStrength/projectile
// speed through the SAME modifier-fold seam legacy uses (applySpellModField)
// exactly once, at launch (frozen for the whole flight — apply_force has no
// fold seam of its own), builds a lightweight AbilityDef + EffectiveSpell
// shim from the folded values (used for spawn geometry / the legacy-shape
// fallback fields, NOT for the tick math itself any more), and calls
// spawnArcaneOrbLocked — the EXACT SAME function the legacy point-cast
// resolver calls for arcane_orb (resolveAbilityCastAtPointLocked,
// ability_cast.go), now also carrying the frozen, authored tick actions.
// tickArcaneOrbProjectileLocked (projectile.go) then drives the moving
// vortex every tick — for BOTH the legacy-cast leg (Projectile.TickActions
// nil ⇒ fireProjectileTickLocked's legacy-fallback math) and this executor
// leg (Projectile.TickActions populated ⇒ the authored actions actually run).
func executeTickingVortexShimLocked(s *GameState, ctx *RuntimeAbilityContext, caster *Unit, c launchProjectileConfig, targets []int) []int {
	trig, hasTrig := extractVortexTickTrigger(c.Triggers)
	mag := vortexMagnitudesFromTrigger(trig) // zero values when !hasTrig

	// Fold radius/pullStrength/speed EXACTLY ONCE, per field, at launch — see
	// launchProjectileConfig's TickInterval doc comment. Falls back to the raw
	// authored baseline when ctx.abilityDef is unset (e.g. a hand-built test
	// context), matching every sibling action's ctx.abilityDef-nil degrade
	// (deal_damage/restore_health/launch_projectile all skip their own scaling
	// seam the same way).
	radius, pull, speed := mag.radius, mag.pullStrength, c.ProjectileSpeed
	if ctx.abilityDef != nil {
		mods := s.collectSpellModifiersLocked(caster, *ctx.abilityDef)
		radius = applySpellModField(mods, *ctx.abilityDef, SpellModFieldRadius, mag.radius)
		pull = applySpellModField(mods, *ctx.abilityDef, SpellModFieldPullStrength, mag.pullStrength)
		speed = applySpellModField(mods, *ctx.abilityDef, SpellModFieldProjectileSpeed, c.ProjectileSpeed)
	}

	// Mirror legacy's exact spawn gate (resolveAbilityCastAtPointLocked:
	// `eff.PullStrength > 0 && def.Projectile != ""`) — a modifier that
	// zeroes (or reduces below zero) the folded pull strength cancels the
	// orb entirely, same as legacy. Mana was already spent one level up
	// (resolveAbilityProgramCastLocked), matching legacy's own
	// spend-then-maybe-no-op ordering.
	if pull <= 0 || c.Projectile == "" {
		ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_pull_or_projectile", "pullStrength": pull})
		return nil
	}

	// dps is recovered ONLY for the legacy-shape fallback fields threaded onto
	// the spawned Projectile (ArcaneOrbDamagePerSecond etc. — used by
	// fireProjectileTickLocked's fallback math when TickActions is nil, e.g. an
	// authoring with no on_projectile_tick trigger at all) — NOT used to
	// compute the actual per-tick damage any more when TickActions IS
	// populated (see fireProjectileTickLocked's composed branch, which folds
	// mag.damageAmount itself, per firing). Raw (unfolded): the fallback path
	// folds it the same way legacy always has, via effectiveSpellLocked one
	// level up.
	var dps float64
	if c.TickInterval > 0 {
		dps = float64(mag.damageAmount) / c.TickInterval
	}

	// Shim def/eff built purely from the recovered/folded magnitudes — never
	// from ctx.abilityDef's own mechanic fields, which are cleared on a
	// converted (schemaVersion 2) ability.
	def := AbilityDef{
		ID:              ctx.AbilityID,
		Projectile:      c.Projectile,
		ProjectileScale: c.ProjectileScale,
		DamageType:      mag.damageType,
	}
	eff := EffectiveSpell{
		Radius:          radius,
		PullStrength:    pull,
		DamagePerSecond: dps,
		ProjectileSpeed: speed,
	}
	distance := c.Distance.Resolve(caster)

	// Freeze the authored tick actions' radius/strength to their folded values
	// and carry them on the spawned Projectile so fireProjectileTickLocked
	// runs the ACTUAL authored program (select_targets/apply_force/deal_damage
	// — including apply_force's Mode, e.g. "push") instead of re-synthesizing
	// its own copy. Only when a trigger was actually authored — an empty/
	// missing trigger already failed the pull<=0 gate above in every case
	// that matters (Validate requires mag.pullStrength > 0 for any authored
	// trigger), but a hand-built ctx/config in a test could reach here with
	// !hasTrig and a nonzero pull from elsewhere; guard explicitly rather than
	// carry an empty-but-non-nil action slice.
	var tickActions []AbilityActionDef
	var opsBudget *int
	if hasTrig {
		tickActions = freezeVortexTickActions(trig.Actions, radius, pull)
		// Shared cross-tick op budget for this lineage — mirrors the
		// non-vortex branch's identical seeding (see the file doc's CROSS-TICK
		// OP BUDGET section): reused verbatim if this launch is itself running
		// inside a shared-budget ctx, otherwise minted fresh from however much
		// of ctx's own budget remains. Bounds the TOTAL work this orb's
		// composed tick firings can do across its entire flight, however many
		// ticks that spans.
		if ctx.sharedOpsRemaining != nil {
			opsBudget = ctx.sharedOpsRemaining
		} else {
			remaining := maxExecutionOps - ctx.opsUsed
			opsBudget = &remaining
		}
	}

	s.spawnArcaneOrbLocked(caster, ctx.CastPoint.X, ctx.CastPoint.Y, def, eff, distance, c.TickInterval, tickActions, opsBudget)
	ctx.trace("vortex_launched", ctx.currentActionPath, map[string]any{
		"radius": radius, "pullStrength": pull, "damagePerSecond": dps, "speed": speed,
	})
	return nil
}
