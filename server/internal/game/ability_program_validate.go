package game

import "fmt"

// allActionTypes is the canonical list of every ActionType. isKnownActionType
// derives from it so the two cannot drift (guarded by
// TestKnownActionTypesCoversAllConsts). A "known but no descriptor" action
// type is skipped silently by validation (not flagged, not decoded) so
// abilities can author it ahead of the descriptor landing in a later task.
var allActionTypes = []ActionType{
	ActionSelectTargets, ActionStoreTargets, ActionFilterTargets, ActionDealDamage,
	ActionRestoreHealth, ActionApplyStatus, ActionApplyStatusDuration, ActionChangeStat, ActionApplyMark, ActionApplyColorOverlay,
	ActionRemoveStatus, ActionCreateZone,
	ActionLaunchProjectile, ActionBeam, ActionChargeFireVolley, ActionSummonUnit, ActionPlaceTrap, ActionMoveUnit, ActionApplyForce,
	ActionModifyResource, ActionTriggerEvent, ActionPlayPresentation, ActionPlaySound,
	ActionChangeRenderLayer, ActionCameraShake, ActionWait, ActionConditional,
	ActionRepeat, ActionSetContext, ActionLoop, ActionCustom,
}

// knownActionTypes is the lookup set derived from allActionTypes; the set of
// every ActionType the data model recognizes, regardless of whether a
// descriptor has been registered for it yet.
var knownActionTypes = func() map[ActionType]bool {
	m := make(map[ActionType]bool, len(allActionTypes))
	for _, t := range allActionTypes {
		m[t] = true
	}
	return m
}()

// isKnownActionType reports whether t is one of the ActionType enum consts
// defined on the data model, independent of whether an ActionDescriptor has
// been registered for it.
func isKnownActionType(t ActionType) bool {
	return knownActionTypes[t]
}

// allTriggerTypes is the canonical list of every TriggerType. isKnownTriggerType
// derives from it so the two cannot drift (guarded by
// TestProgramEnumsMatchSourceConsts, which checks ProgramEnums()'s
// "triggerTypes" entry — reused from this slice, mirroring how ProgramEnums()
// reuses allActionTypes for "actionTypes"). Also the target of
// AbilityRider.Trigger validation (perk_defs.go): a rider grafts its actions
// onto an existing TriggerType, so its trigger must be one of these.
var allTriggerTypes = []TriggerType{
	TriggerOnCastStart, TriggerOnCastComplete, TriggerOnAnimationMarker,
	TriggerOnProjectileImpact, TriggerOnBeamImpact, TriggerOnTick,
	TriggerOnZoneEnter, TriggerOnZoneExit, TriggerOnStatusExpire,
	TriggerOnDamageDealt, TriggerOnUnitDeath, TriggerOnActionComplete, TriggerOnChargeFull, TriggerCustom,
}

// knownTriggerTypes is the lookup set derived from allTriggerTypes.
var knownTriggerTypes = func() map[TriggerType]bool {
	m := make(map[TriggerType]bool, len(allTriggerTypes))
	for _, t := range allTriggerTypes {
		m[t] = true
	}
	return m
}()

// isKnownTriggerType reports whether t is one of the TriggerType enum consts
// defined on the data model. Used by AbilityRider validation (perk_defs.go).
func isKnownTriggerType(t TriggerType) bool {
	return knownTriggerTypes[t]
}

// validationWalker accumulates issues and duplicate-id tracking state while
// walking an AbilityProgram's trigger/action tree. Triggers and actions
// share one id namespace, matching the requirement that a repeated action id
// is flagged the same way as a repeated trigger id.
type validationWalker struct {
	issues    []ValidationIssue
	seenIDs   map[string]bool
	numAction int
}

// validateAbilityProgram runs structural (Phase 2) validation over prog and
// returns every issue found. It performs no semantic/behavioral checks —
// see the TODO block below for what is intentionally deferred.
func validateAbilityProgram(prog *AbilityProgram) []ValidationIssue {
	w := &validationWalker{seenIDs: map[string]bool{}}

	for i, trig := range prog.Triggers {
		// A channeled beam may only START from a ROOT on_cast_complete trigger
		// (channels can only begin from the cast-begin gating path — see
		// ability_channel.go's ORDERING DECISION). Root on_cast_complete
		// triggers pass channeledBeamAllowed=true; everything else (other root
		// trigger types, named triggers, presentations, and every NESTED
		// trigger) passes false, so a channeled beam anywhere else is flagged.
		// insideStatusDuration starts false everywhere — it is only ever set
		// true by walkAction's own ActionApplyStatusDuration case, below.
		w.walkTrigger(trig, fmt.Sprintf("triggers[%d]", i), trig.Type == TriggerOnCastComplete, false, nil)
	}
	for key, trig := range prog.NamedTriggers {
		w.walkTrigger(trig, fmt.Sprintf("namedTriggers[%s]", key), false, false, nil)
	}
	for p, pres := range prog.Presentations {
		for i, trig := range pres.Triggers {
			w.walkTrigger(trig, fmt.Sprintf("presentations[%d].triggers[%d]", p, i), false, false, nil)
		}
	}

	if w.numAction == 0 {
		w.issues = append(w.issues, ValidationIssue{
			Path:     "",
			Code:     "no_behavior",
			Message:  "ability has no actions",
			Severity: "warning",
		})
	}

	return w.issues
}

// walkTrigger validates one trigger (duplicate id, tick-interval requirement)
// and then walks its actions, recursing into any child triggers nested
// inside those actions. insideStatusDuration is true only while walking the
// config.triggers of an apply_status_duration action (or anything reachable
// from there WITHOUT crossing into another container action's own
// config.triggers or children — see walkAction's ActionApplyStatusDuration
// case and the propagation notes at each recursive call site below) — it
// gates ActionChangeStat/ActionApplyMark's "must be nested under an
// apply_status_duration" placement rule.
func (w *validationWalker) walkTrigger(trig AbilityTriggerDef, path string, channeledBeamAllowed bool, insideStatusDuration bool, loopVars map[string]bool) {
	w.checkDuplicateID(trig.ID, path)

	// NOTE: an on_tick trigger carries NO tick-interval of its own. The tick
	// CADENCE is owned entirely by the enclosing ticking container's config
	// (createZoneConfig.TickInterval, applyStatusDurationConfig.TickInterval,
	// the projectile/beam config), which is where it is authored AND validated —
	// see each container's own Validate. A former, redundant trigger-level
	// timing.tickInterval requirement lived here; it was removed once on_tick
	// unified the four *_tick types, because it duplicated the container's
	// interval and surfaced two "Tick Interval" fields in the editor for one
	// cadence.

	// DamageScope is an on_damage_dealt-ONLY field (see AbilityTriggerDef.DamageScope's
	// doc comment). An authored scope on any other trigger type would be
	// silently inert — this project rejects inert authorable fields outright
	// rather than letting them ship and do nothing.
	if trig.DamageScope != nil {
		if trig.Type != TriggerOnDamageDealt {
			w.issues = append(w.issues, ValidationIssue{
				Path:     path,
				Code:     "invalid_damage_scope_placement",
				Message:  "damageScope is only valid on an on_damage_dealt trigger",
				Severity: "error",
			})
		} else {
			for _, c := range trig.DamageScope.Categories {
				if !isKnownDamageCategory(c) {
					w.issues = append(w.issues, ValidationIssue{
						Path:     path,
						Code:     "unknown_damage_category",
						Message:  "unknown damage category \"" + string(c) + "\"",
						Severity: "error",
					})
				}
			}
			// Self-contradiction guard: ability-attributed damage always
			// carries Category "ability" (see DamageTriggerScope.AbilityID's
			// doc comment), so an AbilityID filter paired with a non-empty
			// Categories list that excludes "ability" describes a damage
			// instance that can never occur. Reject the combination outright
			// rather than silently ignoring one half of it.
			if trig.DamageScope.AbilityID != "" && len(trig.DamageScope.Categories) > 0 {
				hasAbilityCategory := false
				for _, c := range trig.DamageScope.Categories {
					if c == DamageCategoryAbility {
						hasAbilityCategory = true
						break
					}
				}
				if !hasAbilityCategory {
					w.issues = append(w.issues, ValidationIssue{
						Path:     path,
						Code:     "contradictory_damage_scope",
						Message:  "damageScope.abilityId requires categories to include \"ability\" (or be empty) — ability-attributed damage is never any other category",
						Severity: "error",
					})
				}
			}
		}
	}

	for i, action := range trig.Actions {
		w.walkAction(action, fmt.Sprintf("%s.actions[%d]", path, i), channeledBeamAllowed, insideStatusDuration, loopVars)
	}
}

// walkAction validates one action (duplicate id, known type, decode +
// descriptor validation) and recurses into any child triggers.
// channeledBeamAllowed is true only for a direct action of a root
// on_cast_complete trigger — the one place a channeled beam may start.
// insideStatusDuration is true only while walking an apply_status_duration
// action's own config.triggers (see walkTrigger's doc comment) — it gates
// the "change_stat/apply_mark must be nested under an apply_status_duration"
// placement rule below.
func (w *validationWalker) walkAction(action AbilityActionDef, path string, channeledBeamAllowed bool, insideStatusDuration bool, loopVars map[string]bool) {
	w.numAction++
	w.checkDuplicateID(action.ID, path)

	// change_stat / apply_mark are duration-AGNOSTIC effect actions: they
	// bind to "the current status" via RuntimeAbilityContext.CurrentStatus,
	// which is only ever bound while apply_status_duration's Execute is
	// running its own config.triggers (ability_status_duration.go). Authored
	// anywhere else, ctx.CurrentStatus is nil and the action would silently
	// no-op at runtime — rejected outright here instead (this project's
	// standing "no inert authorable fields" rule, same bar
	// isAuraOnlyStat/PerCompanion are held to on a stat modifier elsewhere).
	if (action.Type == ActionChangeStat || action.Type == ActionApplyMark || action.Type == ActionApplyColorOverlay) && !insideStatusDuration {
		w.issues = append(w.issues, ValidationIssue{
			Path:     path,
			Code:     "invalid_placement",
			Message:  string(action.Type) + " must live in an apply_status_duration's On Apply (on_action_complete) trigger — it binds to the enclosing status's lifetime and is inert in an On Duration Tick / On Complete trigger or anywhere else",
			Severity: "error",
		})
	}

	if !isKnownActionType(action.Type) {
		w.issues = append(w.issues, ValidationIssue{
			Path:     path,
			Code:     "unsupported_action_type",
			Message:  "unknown action type \"" + string(action.Type) + "\"",
			Severity: "error",
		})
	} else if d, ok := lookupActionDescriptor(action.Type); ok {
		// Replace any in-scope loop-variable references with placeholder numbers
		// so a body field authored as a variable (e.g. "flatOffset":"a") decodes
		// for static validation — the runtime substitutes real values per
		// iteration (resolveConfigVars, ability_exec_loop.go).
		cfg, err := d.Decode(substituteLoopVarPlaceholders(action.Config, loopVars))
		if err != nil {
			w.issues = append(w.issues, ValidationIssue{
				Path:     path,
				Code:     "invalid_config",
				Message:  err.Error(),
				Severity: "error",
			})
		} else {
			for _, issue := range d.Validate(cfg, ValidationScope{InsideStatusDuration: insideStatusDuration}) {
				if issue.Path == "" {
					issue.Path = path
				} else {
					issue.Path = path + "." + issue.Path
				}
				w.issues = append(w.issues, issue)
			}

			// A channeled beam may only START from a root on_cast_complete
			// trigger (channeledBeamAllowed). Anywhere else it would try to
			// start a channel from a call site that can't gate it — flag it
			// loudly instead of letting it silently no-op.
			if action.Type == ActionBeam && !channeledBeamAllowed {
				if bc, ok := cfg.(beamConfig); ok && bc.Channeled {
					w.issues = append(w.issues, ValidationIssue{
						Path:     path,
						Code:     "invalid_channeled_beam_placement",
						Message:  "a channeled beam can only be the channel-start action of a root on_cast_complete trigger",
						Severity: "error",
					})
				}
			}

			// create_zone, apply_status(authored), launch_projectile
			// (non-chain), beam, and apply_status_duration are the ActionTypes
			// whose config carries a decoded, live trigger container (json
			// "triggers" — see ability_zone.go / ability_status.go /
			// ability_compile.go / ability_exec_beam.go /
			// ability_status_duration.go). Nested triggers can never host a
			// channel start, so they always pass channeledBeamAllowed=false.
			// Only recurse when the decode above succeeded: on failure cfg is
			// the zero value and walking it would validate garbage instead of
			// reporting invalid_config once (which the branch above already
			// did). insideStatusDuration is forced false for create_zone/
			// apply_status/launch_projectile/beam's own nested triggers even
			// when this action itself is textually nested inside an
			// apply_status_duration trigger: each of those fires its nested
			// triggers from a BRAND NEW RuntimeAbilityContext built at tick/
			// impact time (fireAbilityZoneTickLocked, fireAbilityStatusTickLocked,
			// projectile/beam impact), which never carries the enclosing
			// status's ctx.CurrentStatus binding — so a change_stat/apply_mark
			// reachable only through one of those would be inert at runtime
			// despite being lexically inside an apply_status_duration.
			switch action.Type {
			case ActionCreateZone:
				if zc, ok := cfg.(createZoneConfig); ok {
					for i, child := range zc.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i), false, false, loopVars)
					}
				}
			case ActionApplyStatus:
				if ac, ok := cfg.(applyStatusConfig); ok {
					for i, child := range ac.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i), false, false, loopVars)
					}
				}
			case ActionApplyStatusDuration:
				// The ONE place insideStatusDuration is set true — but ONLY for
				// the container's On Apply (on_action_complete) child triggers.
				// That is the sole moment Execute binds ctx.CurrentStatus (it
				// runs on_action_complete at spawn); the On Duration Tick /
				// On Complete (on_status_tick / on_status_expire) triggers fire
				// LATER from a fresh per-event context (tickAbilityStatusesLocked
				// -> buildStatusEventContextLocked) with no CurrentStatus, so a
				// status-bound effect (change_stat/apply_mark/nested apply_status)
				// there would be inert — insideStatusDuration=false makes the
				// placement check below reject it, matching the "no inert
				// authorable fields" rule.
				if adc, ok := cfg.(applyStatusDurationConfig); ok {
					for i, child := range adc.Triggers {
						onApply := child.Type == TriggerOnActionComplete
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i), false, onApply, loopVars)
					}
				}
			case ActionLaunchProjectile:
				if lc, ok := cfg.(launchProjectileConfig); ok {
					for i, child := range lc.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i), false, false, loopVars)
					}
				}
			case ActionBeam:
				if bc, ok := cfg.(beamConfig); ok {
					for i, child := range bc.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i), false, false, loopVars)
					}
				}
			case ActionLoop:
				// The loop's variables come into scope for its body, so a body
				// field authored as a variable reference (e.g. "amount":"a")
				// validates. Walk each body action under path.body[i]. The loop
				// runs its body inline against the SAME ctx (no new
				// RuntimeAbilityContext is built), so insideStatusDuration
				// propagates through unchanged.
				if lc, ok := cfg.(loopConfig); ok {
					bodyVars := map[string]bool{}
					for k := range loopVars {
						bodyVars[k] = true
					}
					for _, v := range lc.Vars {
						if len(v.Name) == 1 {
							bodyVars[v.Name] = true
						}
					}
					for i := range lc.Body {
						w.walkAction(lc.Body[i], fmt.Sprintf("%s.body[%d]", path, i), false, insideStatusDuration, bodyVars)
					}
				}
			}
		}
	}
	// else: known action type with no registered descriptor yet — skip
	// silently (no decode, no flag). See task doc for rationale.

	// action.Children (on_action_complete et al.) fire through the SAME ctx
	// as their parent action (executeActionLocked runs them inline, no new
	// RuntimeAbilityContext) — so insideStatusDuration propagates through
	// unchanged, unlike the config.triggers cases above which each fire from
	// a fresh ctx.
	for i, child := range action.Children {
		w.walkTrigger(child, fmt.Sprintf("%s.children[%d]", path, i), false, insideStatusDuration, loopVars)
	}
}

// checkDuplicateID records id in the shared trigger/action id namespace,
// emitting duplicate_id the second and every subsequent time id is seen.
// Empty ids are not tracked (an authoring-incomplete id is a separate,
// not-yet-implemented concern).
func (w *validationWalker) checkDuplicateID(id string, path string) {
	if id == "" {
		return
	}
	if w.seenIDs[id] {
		w.issues = append(w.issues, ValidationIssue{
			Path:     path,
			Code:     "duplicate_id",
			Message:  "duplicate id \"" + id + "\"",
			Severity: "error",
		})
		return
	}
	w.seenIDs[id] = true
}

// TODO(phase-2b): deeper semantic validation deferred to a later task:
//   - invalid context references (ContextRef.Key not resolvable in scope)
//   - circular named-trigger invocation detection
//   - animation marker bounds checking against the caster's animation clip
//   - missing/invalid target source combinations (e.g. previous_action_targets
//     used before any prior action produced targets)
//   - descending into status/zone-spawn nested triggers that live inside a
//     STILL-DEAD model type with no decoding ActionDescriptor (StatusDef/
//     ZoneDef/ProjectileSpawnDef, ability_program.go, are unused proposals —
//     create_zone's config.triggers, apply_status's config.triggers, AND
//     launch_projectile's config.triggers (on_projectile_impact) ARE walked,
//     see walkAction above — none of them decode through those dead types;
//     each action's own concrete config struct carries its own Triggers
//     field instead)
//   - duration < tickInterval on zones/statuses
//   - persistent objects (zones/statuses) created without any termination path
//   - outputs referenced before they are produced by a prior action
