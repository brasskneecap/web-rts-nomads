package game

import (
	"encoding/json"
)

// ability_exec_actions.go registers the remaining Phase 3 action executors:
// summon_unit, apply_force, apply_status, remove_status, modify_resource.
// Each adapts an EXISTING gameplay seam (spawnSummonedUnitLocked,
// applyPullLocked, applyProcSlowLocked/ApplyStunLocked/applyAbilityBurnLocked,
// spendUnitManaLocked/addUnitManaLocked) rather than reimplementing it. Follows
// the deal_damage/restore_health/select_targets pattern in
// ability_program_registry.go exactly. These ARE reachable from the live cast
// path today for any schemaVersion:2 catalog ability (e.g. shatter's compiled
// apply_status(slow) rider) via resolveAbilityProgramCastLocked.
//
// apply_status specifically has a SECOND, authored-only behavior as of the
// AbilityStatus subsystem (ability_status.go): see applyStatusConfig's doc
// comment for the legacy-vs-authored discriminator and design rationale.

// ── summon_unit ──────────────────────────────────────────────────────────

type summonUnitConfig struct {
	UnitType string `json:"unitType"`
	Count    int    `json:"count"`
}

func (summonUnitConfig) actionConfig() {}

// ── apply_force ──────────────────────────────────────────────────────────

// applyForceOriginOptions is the Options list offered by apply_force's origin
// schema field: every TargetOrigin s.resolveOriginLocked resolves to a real,
// non-caster-fallback position (mirrors launchProjectileSpawnOriginOptions,
// ability_exec_projectile.go — see that var's doc comment for why
// OriginStatusOwner/OriginSummonedUnit/OriginNamedContextValue are
// deliberately withheld). OriginProjectilePos is INCLUDED here (unlike
// launch_projectile's spawnOrigin list): arcane_orb's on_projectile_tick
// firing is what makes it real (RuntimeAbilityContext.ProjectilePosition —
// see ability_exec.go), so it is no longer an inert option for this action.
var applyForceOriginOptions = []string{
	string(OriginCaster), string(OriginCastPoint), string(OriginImpactPosition),
	string(OriginCurrentEventPos), string(OriginZoneCenter), string(OriginProjectilePos),
	string(OriginInitialTarget), string(OriginInitialTargetPos),
}

// isValidApplyForceOrigin reports whether origin is either unset (the
// byte-identical caster-position default) or one of
// applyForceOriginOptions' offered values.
func isValidApplyForceOrigin(origin TargetOrigin) bool {
	if origin == "" {
		return true
	}
	for _, o := range applyForceOriginOptions {
		if string(origin) == o {
			return true
		}
	}
	return false
}

// applyForceModePull / applyForceModePush are applyForceConfig.Mode's enum
// values (mirrors travelModeToTarget/travelModeDirection's plain-string-const
// convention, ability_exec_projectile.go). applyForceModePull is also the
// zero value's meaning: an empty/absent Mode (every apply_force action
// authored/compiled before Mode existed, including arcane_orb's compiled
// on_projectile_tick trigger) decodes to "" and Execute treats that
// identically to an explicit "pull" — byte-identical to before Mode existed.
const (
	applyForceModePull = "pull"
	applyForceModePush = "push"
)

// isValidApplyForceMode reports whether mode is either unset (byte-identical
// pull default) or one of applyForceModePull/applyForceModePush.
func isValidApplyForceMode(mode string) bool {
	return mode == "" || mode == applyForceModePull || mode == applyForceModePush
}

type applyForceConfig struct {
	Strength float64 `json:"strength"`
	Duration float64 `json:"duration"`
	// Origin selects the world position units are pulled/pushed relative to.
	// Empty (every apply_force action authored/compiled before this field
	// existed) resolves via s.resolveOriginLocked's default case — the
	// caster's own position — EXACTLY the prior hardcoded behavior, so no
	// existing caller changes. arcane_orb's compiled on_projectile_tick
	// trigger sets this to "projectile_position" so hostiles are pulled
	// toward the orb's live position, not back toward the caster.
	Origin TargetOrigin `json:"origin,omitempty"`
	// Mode selects the displacement DIRECTION relative to Origin: "" or
	// "pull" (the pre-existing, only-ever-shipped behavior — drags targets
	// TOWARD Origin and snaps to it on arrival) or "push" (shoves targets
	// AWAY from Origin, with no snap — see applyPushLocked/
	// tickUnitPullLocked for why push cannot reuse pull's overshoot clamp).
	// Every apply_force action authored/compiled before Mode existed decodes
	// this as "" ⇒ pull, so no existing caller (arcane_orb included) changes
	// behavior.
	Mode string `json:"mode,omitempty"`
}

func (applyForceConfig) actionConfig() {}

// ── apply_status ─────────────────────────────────────────────────────────

// applyStatusConfig is the decoded config for the apply_status action: a CC
// EFFECT (chill / slow / stun / burn), in two placements discriminated at
// Execute time by whether ctx.CurrentStatus is bound:
//
//   - LEGACY standalone (ctx.CurrentStatus == nil — every apply_status the
//     legacy compiler emits, e.g. shatter's compiled slow rider): Status plus
//     Multiplier/DPS/School and its OWN Duration route to the pre-existing
//     generic CC primitive seam (applyProcSlowLocked / ApplyStunLocked /
//     applyAbilityBurnLocked / ApplyColdSlowLocked) — byte-identical to before
//     this subsystem existed, proven by the golden equivalence tests
//     (TestAbilityCompileGolden_Shatter et al.).
//   - NESTED (ctx.CurrentStatus bound — the authored-today shape): nested
//     inside an apply_status_duration's On Apply trigger, exactly like
//     change_stat/apply_mark. It carries NO duration of its own (the editor
//     drops the field; validation rejects one there); Execute derives the CC
//     effect's duration from ctx.CurrentStatus.Remaining. Shatter uses this:
//     apply_status_duration -> apply_status(chill).
//
// "chill" is the cold-slow that drives the client's icy-blue overlay
// (ColdSlowedRemaining track, combat_ai_cc.go) and stacks multiplicatively with
// the physical "slow" track.
//
// RETIRED DESIGNS (do NOT re-add):
//   - Name/TickInterval/Stacking/MaxStacks/Triggers, which once let apply_status
//     ITSELF spawn a first-class ticking AbilityStatus (an "authored status").
//     That role now belongs ENTIRELY to apply_status_duration
//     (ability_status_duration.go), which owns a status's lifetime + its
//     On Apply / On Duration Tick / On Complete triggers. apply_status is a
//     plain CC effect again — a ticking/authored status is built via the
//     container, never this action. (Removed once apply_status_duration made
//     this path a dormant, never-shipped duplicate.)
//   - StatModifiers/Icon/IconKind (an even earlier design) — replaced by
//     change_stat/apply_mark nested in apply_status_duration.
type applyStatusConfig struct {
	Status     string     `json:"status"`
	Multiplier float64    `json:"multiplier"`
	Duration   float64    `json:"duration"`
	DPS        float64    `json:"dps"`
	School     DamageType `json:"school"`
}

func (applyStatusConfig) actionConfig() {}

// ── remove_status ────────────────────────────────────────────────────────

type removeStatusConfig struct {
	Status string `json:"status"`
}

func (removeStatusConfig) actionConfig() {}

// ── modify_resource ──────────────────────────────────────────────────────

type modifyResourceConfig struct {
	Resource string `json:"resource"`
	Amount   int    `json:"amount"`
}

func (modifyResourceConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionSummonUnit,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c summonUnitConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(summonUnitConfig)
			if c.UnitType == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "summon_unit requires unitType", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "unitType", Label: "Unit Type", Control: "text", Section: "Properties"},
			{Key: "count", Label: "Count", Control: "number", Kind: abilityStatKindCount, Section: "Properties"},
		}},
		// Execute fans the summon out via the existing spawnSummonedUnitLocked
		// seam (raise_skeleton's row-fanout behavior) — no reimplementation.
		// Summons aren't a target output for the rest of the program, so this
		// always returns nil regardless of Count/success.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(summonUnitConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			count := c.Count
			if count < 1 {
				count = 1
			}
			shim := AbilityDef{SummonUnitType: c.UnitType, SummonCount: count}
			s.spawnSummonedUnitLocked(caster, shim)
			ctx.trace("unit_summoned", ctx.currentActionPath, map[string]any{"unitType": c.UnitType, "count": count})
			return nil
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionApplyForce,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyForceConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyForceConfig)
			var out []ValidationIssue
			if c.Strength <= 0 || c.Duration <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_force requires strength > 0 and duration > 0", Severity: "error"})
			}
			if !isValidApplyForceOrigin(c.Origin) {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "unknown origin " + string(c.Origin), Severity: "error"})
			}
			if !isValidApplyForceMode(c.Mode) {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "unknown mode " + c.Mode, Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "strength", Label: "Strength", Control: "number", Section: "Properties"},
			{Key: "duration", Label: "Duration", Control: "duration", Kind: abilityStatKindDuration, Section: "Timing"},
			{Key: "origin", Label: "Origin", Control: "enum", Options: applyForceOriginOptions, Section: "Targeting"},
			{Key: "mode", Label: "Mode", Control: "enum", Options: []string{applyForceModePull, applyForceModePush}, Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(applyForceConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			// Empty c.Origin resolves via resolveOriginLocked's default case —
			// the caster's own position — byte-identical to the prior hardcoded
			// caster.X/Y for every apply_force action authored before this field
			// existed (none ship in the catalog yet, so this is a pure addition,
			// not a behavior change for anything live).
			origin := s.resolveOriginLocked(ctx, c.Origin, nil)
			push := c.Mode == applyForceModePush
			affected := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil || u.HP <= 0 {
					continue
				}
				if push {
					s.applyPushLocked(u, origin.X, origin.Y, c.Strength, c.Duration)
				} else {
					s.applyPullLocked(u, origin.X, origin.Y, c.Strength, c.Duration)
				}
				affected = append(affected, id)
				mode := applyForceModePull
				if push {
					mode = applyForceModePush
				}
				ctx.trace("force_applied", ctx.currentActionPath, map[string]any{"unit": id, "strength": c.Strength, "duration": c.Duration, "mode": mode})
			}
			return affected
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionApplyStatus,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyStatusConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, scope ValidationScope) []ValidationIssue {
			c := cfg.(applyStatusConfig)
			var out []ValidationIssue
			if c.Status == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_status requires a status", Severity: "error"})
			}
			// Duration is CONTEXT-dependent (see applyStatusConfig's doc comment):
			//   - Nested inside an apply_status_duration (scope.InsideStatusDuration):
			//     the CC effect's lifetime is owned by the container, so a config
			//     duration here is inert — reject it (this project's standing "no
			//     inert authorable fields" rule) rather than silently ignore it.
			//     A nested apply_status is a pure CC effect and must not also
			//     declare its own on_status_tick/expire triggers.
			//   - Standalone (legacy compiler output / authored top-level): the
			//     config duration is the only source, so it is still required.
			if scope.InsideStatusDuration {
				if c.Duration != 0 {
					out = append(out, ValidationIssue{Code: "invalid_property", Message: "apply_status nested under apply_status_duration derives its duration from the container — remove the duration field", Severity: "error"})
				}
			} else if c.Duration <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_status requires duration > 0 (or nest it under an apply_status_duration to derive one)", Severity: "error"})
			}
			return out
		},
		// apply_status is a plain CC effect (chill/slow/stun/burn). It is
		// authored nested inside an apply_status_duration (which owns the
		// lifetime), so there is no "duration" editor control here; the legacy
		// compiler still bakes a Duration into its standalone output's JSON.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			// "chill" is the cold-slow effect — it drives the client's icy
			// blue overlay via the ColdSlowedRemaining track (combat_ai_cc.go,
			// coldSlowedRemaining on the wire); "slow" is the non-tinting
			// physical track. The two stack multiplicatively (slowFactorLocked).
			{Key: "status", Label: "Status", Control: "enum", Options: []string{"chill", "slow", "stun", "burn"}, Section: "Properties"},
			{Key: "multiplier", Label: "Multiplier", Control: "percentage", Section: "Properties"},
			{Key: "dps", Label: "DPS", Control: "number", Section: "Properties"},
			{Key: "school", Label: "School", Control: "enum", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(applyStatusConfig)

			// CC path: route onto the pre-existing generic CC primitives.
			//
			// Effective duration is CONTEXT-dependent:
			//   - Nested inside an apply_status_duration (ctx.CurrentStatus != nil):
			//     the container owns the lifetime, so the CC effect runs for the
			//     status's Remaining — the "duration is its own action" model (see
			//     ability_status_duration.go). ctx.CurrentStatus.Remaining is the
			//     container's full authored duration here: apply_status_duration
			//     runs its config.triggers immediately at spawn, before any
			//     countdown (spawnAbilityStatusLocked seeds Remaining = duration).
			//   - Standalone (ctx.CurrentStatus == nil — the legacy compiler's
			//     output and any top-level authored use): the config's own
			//     Duration is the only source, byte-identical to before.
			//
			// "chill" and "slow" are the same generic move/attack slow (the
			// dedicated cold-slow track was retired — chill's distinct icy tint
			// now comes from an apply_color_overlay composition, not a separate
			// CC track). Both land on the one slow track. School is no longer
			// meaningful for CC routing. The "burn" case calls
			// applyAbilityBurnLocked (not applyProcBurnLocked) — see that
			// function's doc comment for the key-collision bug fix.
			duration := c.Duration
			if ctx.CurrentStatus != nil {
				duration = ctx.CurrentStatus.Remaining
			}
			applied := make([]int, 0, len(targets))
			for _, id := range targets {
				switch c.Status {
				case "chill", "slow":
					s.ApplySlowLocked(id, c.Multiplier, duration)
				case "stun":
					s.ApplyStunLocked(id, duration)
				case "burn":
					s.applyAbilityBurnLocked(id, c.DPS, duration, ctx.CasterID, ctx.AbilityID)
				default:
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "unknown_status", "status": c.Status})
					continue
				}
				applied = append(applied, id)
				ctx.trace("status_applied", ctx.currentActionPath, map[string]any{"unit": id, "status": c.Status})
			}
			return applied
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionRemoveStatus,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c removeStatusConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(removeStatusConfig)
			if c.Status == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "remove_status requires status", Severity: "error"}}
			}
			return nil
		},
		// Phase 3 can only clear the generic CC tracks (slow/stun) that
		// apply_status can set. Author-defined/custom statuses are a later phase.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "status", Label: "Status", Control: "enum", Options: []string{"slow", "stun"}, Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(removeStatusConfig)
			removed := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil {
					continue
				}
				switch c.Status {
				case "slow":
					u.SlowedRemaining = 0
					u.SlowedMultiplier = 0
				case "stun":
					u.StunnedRemaining = 0
				default:
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "unknown_status", "status": c.Status})
					continue
				}
				removed = append(removed, id)
				ctx.trace("status_removed", ctx.currentActionPath, map[string]any{"unit": id, "status": c.Status})
			}
			return removed
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionModifyResource,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c modifyResourceConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(modifyResourceConfig)
			if c.Resource == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "modify_resource requires resource", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "resource", Label: "Resource", Control: "enum", Options: []string{"mana"}, Section: "Properties"},
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
		}},
		// Execute acts on the CASTER, not the resolved target set — a resource
		// is a property of the unit paying/gaining it, not of an external target.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(modifyResourceConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			if c.Resource != "mana" {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "unknown_resource", "resource": c.Resource})
				return nil
			}
			switch {
			case c.Amount < 0:
				if !s.spendUnitManaLocked(caster, -c.Amount) {
					ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "insufficient_mana", "amount": c.Amount})
					return nil
				}
			case c.Amount > 0:
				s.addUnitManaLocked(caster, c.Amount)
			}
			ctx.trace("resource_modified", ctx.currentActionPath, map[string]any{"resource": c.Resource, "amount": c.Amount})
			return nil
		},
	})
}
