package game

import (
	"encoding/json"
	"fmt"
)

// ═════════════════════════════════════════════════════════════════════════════
// "DURATION IS ITS OWN ACTION" — apply_status_duration / change_stat / apply_mark
//
// This file replaces an earlier (this-session) design that crammed a status's
// lifetime AND its stat/icon effects into apply_status's own config
// (StatModifiers/Icon/IconKind fields, since removed from applyStatusConfig —
// see that struct's doc comment, ability_exec_actions.go). The core principle
// now: a status's LIFETIME is owned by exactly one container action
// (apply_status_duration); every effect nested inside it is duration-AGNOSTIC
// and becomes timed purely by being nested there, which reverts it on expiry
// because it was writing onto the SAME AbilityStatus object the container
// spawned and ticks down (ability_status.go).
//
//   - apply_status_duration (this file): config = name/duration/stacking/
//     maxStacks ONLY. Spawns one AbilityStatus per live target (reusing
//     spawnAbilityStatusLocked exactly like apply_status's authored path
//     did), binds it as ctx.CurrentStatus (RuntimeAbilityContext,
//     ability_exec.go), and runs its own config.triggers (an
//     on_action_complete trigger, by authoring convention — see
//     mark_of_weakness's catalog JSON) once PER TARGET so each target's own
//     status is the one bound while its nested effects run.
//   - change_stat (this file): one PerkStatModifier-shaped stat change,
//     appended onto ctx.CurrentStatus.StatModifiers. No duration of its own.
//   - apply_mark (this file): sets ctx.CurrentStatus.Icon/IconKind — the
//     overhead HUD indicator, sibling to play_presentation. No duration of
//     its own.
//
// Both change_stat and apply_mark are REJECTED by validation anywhere
// outside an apply_status_duration's config.triggers (walkAction's
// insideStatusDuration check, ability_program_validate.go) — authored
// elsewhere, ctx.CurrentStatus would be nil and the action would silently
// do nothing, which this project's "no inert authorable fields" rule
// forbids shipping.
//
// WHY NOT AbilityActionDef.Children (the generic on_action_complete
// mechanism executeActionLocked already auto-fires after ANY action's
// Execute returns, ability_exec.go): that mechanism fires ONCE per action
// execution, using whatever ctx state Execute left behind when it returned —
// it cannot fire once per spawned status when apply_status_duration's
// `targets` resolves to more than one unit. This file instead threads
// config.triggers (decoded from the action's own JSON "config" key, mirroring
// create_zone/apply_status/beam/launch_projectile's identical "config
// carries a live nested-trigger container" precedent — see
// ability_program_validate.go's walkAction switch) and calls
// runProgramTriggersLocked directly, once per target, inside Execute's own
// loop — so a multi-target apply_status_duration correctly binds a DIFFERENT
// ctx.CurrentStatus for each target's own nested run. a.Children is left
// unset on every apply_status_duration action authored today (the generic
// post-Execute firing is therefore a harmless no-op: zero triggers to run).
// ═════════════════════════════════════════════════════════════════════════════

// ── apply_status_duration ───────────────────────────────────────────────────

// applyStatusDurationConfig is the decoded config for apply_status_duration.
// Deliberately narrow — name/duration/stacking/maxStacks ONLY, "the
// advanced/timing section" per this task's design brief. Every gameplay
// EFFECT (stat changes, overhead icon, future on-apply presentation, ...)
// is authored as a nested action inside Triggers instead of a config field
// here, so this container never grows a parallel StatModifiers/Icon pair
// the way applyStatusConfig once did.
type applyStatusDurationConfig struct {
	// Name disambiguates multiple distinctly-named statuses authored by the
	// SAME ability (AbilityStatus.Name / statusStackKey, ability_status.go).
	// Empty (the common case — mark_of_weakness leaves it unset) means the
	// dedup/stacking key is the ability id alone.
	Name string `json:"name,omitempty"`
	// Duration seeds the spawned AbilityStatus's initial Remaining.
	Duration float64 `json:"duration"`
	// TickInterval is the cadence (seconds) the spawned AbilityStatus fires its
	// on_status_tick trigger(s) at — the "On Duration Tick" moment (burn's
	// per-second damage, etc.). 0/omitted ⇒ the status never ticks (a
	// mark_of_weakness-shaped container with only an On Apply trigger). Required
	// (>0) whenever any config.triggers is an on_status_tick — see Validate.
	// Mirrors createZoneConfig.TickInterval / the runtime cadence driver on
	// AbilityStatus (ability_status.go).
	TickInterval float64 `json:"tickInterval,omitempty"`
	// Stacking / MaxStacks configure AbilityStatus.Stacking/MaxStacks — see
	// those fields' doc comments (ability_status.go) for the refresh-vs-stack
	// model. Same vocabulary/defaults as apply_status's identical fields.
	Stacking  string `json:"stacking,omitempty"`
	MaxStacks int    `json:"maxStacks,omitempty"`
	// Triggers carries this container's child triggers across THREE moments:
	//   - on_action_complete ("On Apply") — run once per live target
	//     immediately at spawn, with ctx.CurrentStatus bound to that target's
	//     status (the only moment status-bound effects — change_stat/apply_mark/
	//     nested apply_status — may live; see this file's doc comment).
	//   - on_status_tick ("On Duration Tick") — fired every TickInterval by the
	//     shared ticker (tickAbilityStatusesLocked) from a FRESH per-event
	//     context (no CurrentStatus binding), for transient per-tick actions
	//     (deal_damage, …).
	//   - on_status_expire ("On Complete") — fired exactly once when the status
	//     ends (natural timeout or target death), also from a fresh context.
	// The On Apply triggers run here in Execute; the tick/expire triggers are
	// stored on the spawned AbilityStatus so the ticker drives them (Execute
	// stores the whole slice — runProgramTriggersLocked filters by type at each
	// site, so an On Apply trigger stored on the status simply never re-fires
	// from the ticker).
	Triggers []AbilityTriggerDef `json:"triggers,omitempty"`
}

func (applyStatusDurationConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionApplyStatusDuration,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyStatusDurationConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyStatusDurationConfig)
			var out []ValidationIssue
			if c.Duration <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_status_duration requires duration > 0", Severity: "error"})
			}
			if c.Stacking != "" && c.Stacking != "refresh" && c.Stacking != "stack" {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: fmt.Sprintf("unknown stacking %q (want \"refresh\" or \"stack\")", c.Stacking), Severity: "error"})
			}
			// TickInterval is the runtime cadence driver for the On Duration Tick
			// moment, so it must be set whenever a child on_status_tick trigger
			// exists (mirrors createZoneConfig / the legacy apply_status authored
			// path's identical requirement). A container with only On Apply /
			// On Complete triggers needs no cadence.
			hasTick := false
			for _, trig := range c.Triggers {
				if trig.Type == TriggerOnTick {
					hasTick = true
					break
				}
			}
			if hasTick && c.TickInterval <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_status_duration requires tickInterval > 0 when it has an on_status_tick trigger", Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "name", Label: "Name", Control: "text", Section: "Advanced"},
			{Key: "duration", Label: "Duration", Control: "duration", Section: "Timing"},
			{Key: "tickInterval", Label: "Tick Interval", Control: "duration", Section: "Timing"},
			{Key: "stacking", Label: "Stacking", Control: "enum", Options: []string{"refresh", "stack"}, Section: "Advanced"},
			{Key: "maxStacks", Label: "Max Stacks", Control: "number", Section: "Advanced"},
			// config.triggers is NOT re-declared here as an inspector field —
			// same "the flow view already renders config.triggers as real,
			// recursive FlowTriggerCards" reasoning as create_zone/apply_status
			// (see those actions' Schema doc notes, ability_zone.go /
			// ability_exec_actions.go).
		}},
		// Execute spawns one AbilityStatus PER live target and, for each,
		// binds it as ctx.CurrentStatus and runs config.Triggers
		// (on_action_complete) immediately — so nested change_stat/apply_mark
		// see exactly the status that target's own application just created,
		// never a sibling target's. Restores the caller's previous
		// CurrentStatus (nil at top level; another apply_status_duration's,
		// if this one is itself nested inside one — not a shape any catalog
		// ability uses today, but correct regardless) on return via defer.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(applyStatusDurationConfig)
			prevStatus := ctx.CurrentStatus
			defer func() { ctx.CurrentStatus = prevStatus }()

			applied := make([]int, 0, len(targets))
			for _, id := range targets {
				u := s.getUnitByIDLocked(id)
				if u == nil || u.HP <= 0 {
					continue
				}
				st := &AbilityStatus{
					AbilityID:    ctx.AbilityID,
					Name:         c.Name,
					CasterID:     ctx.CasterID,
					TargetUnitID: id,
					Remaining:    c.Duration,
					TickInterval: c.TickInterval,
					// Store the child triggers so the shared ticker
					// (tickAbilityStatusesLocked) fires the on_status_tick /
					// on_status_expire ones on cadence / at end. The On Apply
					// (on_action_complete) triggers are run below at spawn; they
					// stay in this slice harmlessly because runProgramTriggersLocked
					// filters by type, so the ticker never re-fires them.
					Triggers:  c.Triggers,
					Stacking:  c.Stacking,
					MaxStacks: c.MaxStacks,
				}
				// spawnAbilityStatusLocked's refresh/stack-cap paths may
				// discard st (extending an existing live status instead, or
				// dropping the application outright) — binding CurrentStatus
				// to st regardless (not to whatever it refreshed) is what
				// gives change_stat/apply_mark's writes the correct "no
				// double-apply on refresh" behavior for free; see
				// RuntimeAbilityContext.CurrentStatus's doc comment
				// (ability_exec.go).
				s.spawnAbilityStatusLocked(st)
				ctx.CurrentStatus = st
				s.runProgramTriggersLocked(ctx, c.Triggers, TriggerOnActionComplete)
				applied = append(applied, id)
				ctx.trace("status_duration_applied", ctx.currentActionPath, map[string]any{"unit": id, "name": c.Name, "duration": c.Duration})
			}
			return applied
		},
	})
}

// ── change_stat ──────────────────────────────────────────────────────────────

// changeStatConfig is the decoded config for change_stat: one
// PerkStatModifier-shaped stat change, applied to whichever apply_status_duration
// currently encloses this action.
type changeStatConfig struct {
	Stat  string  `json:"stat"`
	Op    string  `json:"op"`
	Value float64 `json:"value"`
	// Stage — see PerkStatModifier.Stage's doc comment (perk_defs.go).
	// Omitted/"" folds into statStageBase exactly like a perk's own entry.
	Stage string `json:"stage,omitempty"`
}

func (changeStatConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionChangeStat,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c changeStatConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		// Held to the SAME bar as a PerkDef.StatModifiers entry
		// (validatePerkStatModifier, perk_defs.go) — one shared vocabulary,
		// one validation bar, regardless of which of the FOUR emitters (perk /
		// aura / status-via-apply_status_duration+change_stat) authors it.
		// PerCompanion is deliberately not even a config field here (aura-only
		// concept — see PerkStatModifier.PerCompanion's doc comment), so
		// there is nothing to reject for it.
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(changeStatConfig)
			var out []ValidationIssue
			sm := PerkStatModifier{Stat: c.Stat, Op: c.Op, Value: c.Value, Stage: c.Stage}
			if err := validatePerkStatModifier(sm); err != nil {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: err.Error(), Severity: "error"})
				return out
			}
			// AuraOnly stats (armorPercent, projectileDamageReduction) have NO
			// top-level fold site — they are read exclusively via the aura
			// cache (unitAuraStatContributionLocked). A status is a DIFFERENT
			// emitter from an aura (targets the afflicted unit directly, not a
			// radius), but it is still not the aura cache, so an AuraOnly stat
			// authored here would be computed by unitStatusStatModifiersLocked
			// and then read by NOTHING. Same silent-no-op shape validatePerkDef
			// already rejects for a top-level perk entry.
			if isAuraOnlyStat(c.Stat) {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: fmt.Sprintf("stat %q has no fold site for change_stat — it is only ever consumed via a PerkAura's radius contribution (see statDef.AuraOnly, stat_modifiers.go)", c.Stat), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "stat", Label: "Stat", Control: "enum", Options: ListStatIDs(), Section: "Properties"},
			{Key: "op", Label: "Operation", Control: "enum", Options: []string{statOpAdd, statOpMultiply}, Section: "Properties"},
			{Key: "value", Label: "Value", Control: "number", Section: "Properties"},
			{Key: "stage", Label: "Stage", Control: "enum", Options: []string{statStageIntrinsic, statStageBase, statStageFinal}, Section: "Advanced"},
		}},
		// Execute never reads `targets` — it operates entirely on
		// ctx.CurrentStatus (the enclosing apply_status_duration's freshly
		// spawned-or-orphaned AbilityStatus), appending one more
		// PerkStatModifier onto it. A nil CurrentStatus (reachable only by
		// bypassing validation) is a defensive no-op trace, not a panic.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, _ []int) []int {
			c := cfg.(changeStatConfig)
			if ctx.CurrentStatus == nil {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_current_status"})
				return nil
			}
			ctx.CurrentStatus.StatModifiers = append(ctx.CurrentStatus.StatModifiers, PerkStatModifier{
				Stat: c.Stat, Op: c.Op, Value: c.Value, Stage: c.Stage,
			})
			ctx.trace("stat_changed", ctx.currentActionPath, map[string]any{"stat": c.Stat, "op": c.Op, "value": c.Value})
			return nil
		},
	})
}

// ── apply_mark ───────────────────────────────────────────────────────────────

// applyMarkConfig is the decoded config for apply_mark: the overhead HUD
// indicator (icon id + buff/debuff channel) shown while the enclosing
// apply_status_duration's status is active. Sibling to play_presentation —
// this is the ONLY thing apply_mark does; it carries no gameplay effect of
// its own.
type applyMarkConfig struct {
	Icon     string `json:"icon"`
	IconKind string `json:"iconKind"`
}

func (applyMarkConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionApplyMark,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c applyMarkConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(applyMarkConfig)
			var out []ValidationIssue
			if c.Icon == "" {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "apply_mark requires icon", Severity: "error"})
			}
			if c.IconKind != "buff" && c.IconKind != "debuff" {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: fmt.Sprintf("iconKind must be \"buff\" or \"debuff\", got %q", c.IconKind), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			// "icon" has no static Options here — see apply_status's identical
			// "icon" field note (ability_exec_actions.go): the editor resolves
			// it via SchemaField.vue's forward-compat rule against
			// ProgramEnums()["icon"] (statusIconIDs(), ability_program_enums.go).
			{Key: "icon", Label: "Overhead Icon", Control: "enum", Section: "Properties"},
			{Key: "iconKind", Label: "Icon Channel", Control: "enum", Options: []string{"buff", "debuff"}, Section: "Properties"},
		}},
		// Execute never reads `targets` — see change_stat's identical note.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, _ []int) []int {
			c := cfg.(applyMarkConfig)
			if ctx.CurrentStatus == nil {
				ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "no_current_status"})
				return nil
			}
			ctx.CurrentStatus.Icon = c.Icon
			ctx.CurrentStatus.IconKind = c.IconKind
			ctx.trace("mark_applied", ctx.currentActionPath, map[string]any{"icon": c.Icon, "iconKind": c.IconKind})
			return nil
		},
	})
}
