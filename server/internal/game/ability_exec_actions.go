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

// applyStatusConfig is the decoded config for the apply_status action. It
// serves TWO distinct designs, discriminated at Execute time by whether
// Triggers is non-empty:
//
//   - LEGACY (Triggers empty — every apply_status action compileLegacyAbility
//     has ever emitted, e.g. shatter's slow rider): Status/Multiplier/
//     Duration/DPS/School are the only fields read, and Execute's three-case
//     switch on Status routes to the pre-existing generic CC primitive seam
//     (applyProcSlowLocked / ApplyStunLocked / applyAbilityBurnLocked)
//     EXACTLY as it did before this subsystem existed — byte-identical
//     behavior, proven by the golden equivalence tests
//     (TestAbilityCompileGolden_Shatter et al.). The three CC primitives
//     already have their own hardcoded overhead icons (debuff-stunned,
//     debuff-slowed — activeDebuffIconsLocked).
//   - AUTHORED (Triggers non-empty): Name/TickInterval/Stacking/MaxStacks/
//     Triggers (fields derived from the dead StatusDef model type,
//     ability_program.go) describe a first-class AbilityStatus object
//     (ability_status.go) that fires its own on_status_tick/on_status_expire
//     triggers through the shared executor — Status/Multiplier/DPS/School
//     are ignored on this path (Duration is still read: it seeds the
//     status's initial Remaining). No catalog ability uses this path today
//     (TestCatalog_NoAbilityUsesStatusTickExpireTriggers) — it stays
//     editor-reachable/dormant, same as before this task.
//
// NOTE ON A REMOVED DESIGN: this struct previously ALSO carried
// StatModifiers/Icon/IconKind (a same-session design that let apply_status
// itself own a status's stat changes and overhead icon). That design was
// replaced by the "duration is its own action" model:
// apply_status_duration (ability_status_duration.go) now owns a status's
// LIFETIME, and change_stat/apply_mark (same file) — nested inside it —
// carry those effects instead, duration-agnostic. mark_of_weakness (the
// pilot for both designs) was re-authored onto the new shape; see
// ability_status_duration.go's file doc comment for the full writeup. Do
// NOT re-add StatModifiers/Icon/IconKind here — that reintroduces the two
// designs' overlap this replacement was written to remove.
//
// DESIGN CALL (kept from the original decision): this file could instead
// have added a wholly separate action type (the launch_vortex-beside-
// launch_projectile precedent) for the authored/legacy split above.
// Rejected: a status is ONE concept whether it's a hardcoded slow or an
// author-defined DoT/tick-driven effect — unlike a projectile vs. a
// non-impacting vortex, which are genuinely different mechanics. Branching
// inside one action, gated on Triggers, is additive (every existing
// legacy-compiled action decodes it as nil/empty and takes the exact old
// code path) and keeps that ONE concept as one ActionType.
type applyStatusConfig struct {
	Status     string     `json:"status"`
	Multiplier float64    `json:"multiplier"`
	Duration   float64    `json:"duration"`
	DPS        float64    `json:"dps"`
	School     DamageType `json:"school"`

	// Authored-status-only fields below. Zero/empty for every legacy-compiled
	// apply_status action — compileLegacyAbility/compileSlowRider never set
	// these (enforced by TestCatalog_NoAbilityUsesStatusTickExpireTriggers).

	// Name disambiguates multiple distinctly-named statuses authored by the
	// same ability (AbilityStatus.Name / statusStackKey).
	Name string `json:"name,omitempty"`
	// TickInterval is the cadence the spawned AbilityStatus fires
	// on_status_tick at (AbilityStatus.TickInterval — the runtime cadence
	// driver, mirroring createZoneConfig.TickInterval; an on_status_tick
	// trigger's own Timing.TickInterval, checked separately by the
	// validator's walkTrigger, is authoring metadata only, exactly like
	// on_zone_tick's).
	TickInterval float64 `json:"tickInterval,omitempty"`
	// Stacking / MaxStacks configure AbilityStatus.Stacking/MaxStacks — see
	// those fields' doc comments for the refresh-vs-stack model.
	Stacking  string `json:"stacking,omitempty"`
	MaxStacks int    `json:"maxStacks,omitempty"`
	// Triggers carries the compiled on_status_tick / on_status_expire
	// trigger(s) an authored status fires. Non-empty means authored (see
	// this struct's doc comment for the discriminator).
	Triggers []AbilityTriggerDef `json:"triggers,omitempty"`
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
			{Key: "count", Label: "Count", Control: "number", Section: "Properties"},
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
			{Key: "duration", Label: "Duration", Control: "duration", Section: "Timing"},
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
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyStatusConfig)
			var out []ValidationIssue
			if c.Status == "" || c.Duration <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_status requires status and duration > 0", Severity: "error"})
			}
			// Authored path only (Triggers non-empty): the config's own
			// TickInterval is the runtime cadence driver (mirrors
			// createZoneConfig's identical, unconditional requirement), so it
			// must be set whenever there's anything to tick against — even a
			// status whose only trigger is on_status_expire still needs this to
			// be a well-formed AbilityStatus once spawned. The legacy path
			// (Triggers empty) needs no tickInterval at all — stun/slow have no
			// cadence and burn's ticking is owned entirely by the existing
			// tickTrapperSilverDebuffsLocked loop.
			if len(c.Triggers) > 0 && c.TickInterval <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_status requires tickInterval > 0 when it declares triggers", Severity: "error"})
			}
			return out
		},
		// Phase 3 supported only the three legacy CC primitives (the ones with
		// an existing generic gameplay seam). The AbilityStatus subsystem adds
		// the authored fields below (name/tickInterval/stacking/maxStacks/
		// triggers) so an author can define a custom on_status_tick/expire
		// buff/debuff instead — see applyStatusConfig's doc comment for the
		// discriminator. (Stat changes and overhead icons are authored via
		// apply_status_duration + change_stat/apply_mark instead — see
		// ability_status_duration.go — not via this action at all.)
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "status", Label: "Status", Control: "enum", Options: []string{"slow", "stun", "burn"}, Section: "Properties"},
			{Key: "multiplier", Label: "Multiplier", Control: "percentage", Section: "Properties"},
			{Key: "duration", Label: "Duration", Control: "duration", Section: "Timing"},
			{Key: "dps", Label: "DPS", Control: "number", Section: "Properties"},
			{Key: "school", Label: "School", Control: "enum", Section: "Properties"},
			{Key: "name", Label: "Name", Control: "text", Section: "Advanced"},
			{Key: "tickInterval", Label: "Tick Interval", Control: "duration", Section: "Timing"},
			{Key: "stacking", Label: "Stacking", Control: "enum", Options: []string{"refresh", "stack"}, Section: "Advanced"},
			{Key: "maxStacks", Label: "Max Stacks", Control: "number", Section: "Advanced"},
			// Config's on_status_tick/on_status_expire triggers are NOT
			// re-declared here (a "triggers" nested_triggers field used to sit
			// here): the flow view already renders config.triggers as real,
			// editable, recursive FlowTriggerCards directly under this action
			// (CONFIG_TRIGGER_ACTION_TYPES, programTree.ts) — a second,
			// read-only inspector stub for the same data was pure redundancy.
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(applyStatusConfig)

			// AUTHORED path discriminator (see applyStatusConfig's doc
			// comment): Triggers non-empty. Spawns one AbilityStatus per live
			// target instead of routing to a legacy CC primitive.
			if len(c.Triggers) > 0 {
				applied := make([]int, 0, len(targets))
				for _, id := range targets {
					u := s.getUnitByIDLocked(id)
					if u == nil || u.HP <= 0 {
						continue
					}
					s.spawnAbilityStatusLocked(&AbilityStatus{
						AbilityID:    ctx.AbilityID,
						Name:         c.Name,
						CasterID:     ctx.CasterID,
						TargetUnitID: id,
						Remaining:    c.Duration,
						TickInterval: c.TickInterval,
						Triggers:     c.Triggers,
						Stacking:     c.Stacking,
						MaxStacks:    c.MaxStacks,
					})
					applied = append(applied, id)
					ctx.trace("status_applied", ctx.currentActionPath, map[string]any{"unit": id, "status": c.Status, "authored": true})
				}
				return applied
			}

			// LEGACY path: unchanged three-case switch onto the pre-existing
			// generic CC primitives. Byte-identical to pre-subsystem behavior
			// except the "burn" case now calls applyAbilityBurnLocked instead of
			// applyProcBurnLocked directly — see that function's doc comment
			// for the key-collision bug fix this is (no currently-shipped
			// ability uses apply_status "burn", so this changes zero production
			// behavior today).
			applied := make([]int, 0, len(targets))
			for _, id := range targets {
				switch c.Status {
				case "slow":
					// applyProcSlowLocked routes cold -> chill track; an empty/other
					// school routes physical (see combat_ai_cc.go).
					s.applyProcSlowLocked(id, c.Multiplier, c.Duration, c.School)
				case "stun":
					s.ApplyStunLocked(id, c.Duration)
				case "burn":
					s.applyAbilityBurnLocked(id, c.DPS, c.Duration, ctx.CasterID, ctx.AbilityID)
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
					u.ColdSlowedRemaining = 0
					u.ColdSlowedMultiplier = 0
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
