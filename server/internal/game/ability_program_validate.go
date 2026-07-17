package game

import "fmt"

// allActionTypes is the canonical list of every ActionType. isKnownActionType
// derives from it so the two cannot drift (guarded by
// TestKnownActionTypesCoversAllConsts). A "known but no descriptor" action
// type is skipped silently by validation (not flagged, not decoded) so
// abilities can author it ahead of the descriptor landing in a later task.
var allActionTypes = []ActionType{
	ActionSelectTargets, ActionStoreTargets, ActionFilterTargets, ActionDealDamage,
	ActionRestoreHealth, ActionApplyStatus, ActionRemoveStatus, ActionCreateZone,
	ActionLaunchProjectile, ActionChargeFireVolley, ActionChannelBeam, ActionSummonUnit, ActionMoveUnit, ActionApplyForce,
	ActionModifyResource, ActionTriggerEvent, ActionPlayPresentation, ActionPlaySound,
	ActionChangeRenderLayer, ActionCameraShake, ActionWait, ActionConditional,
	ActionRepeat, ActionCustom,
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
		w.walkTrigger(trig, fmt.Sprintf("triggers[%d]", i))
	}
	for key, trig := range prog.NamedTriggers {
		w.walkTrigger(trig, fmt.Sprintf("namedTriggers[%s]", key))
	}
	for p, pres := range prog.Presentations {
		for i, trig := range pres.Triggers {
			w.walkTrigger(trig, fmt.Sprintf("presentations[%d].triggers[%d]", p, i))
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
// inside those actions.
func (w *validationWalker) walkTrigger(trig AbilityTriggerDef, path string) {
	w.checkDuplicateID(trig.ID, path)

	if trig.Type == TriggerOnZoneTick || trig.Type == TriggerOnStatusTick {
		if trig.Timing == nil || trig.Timing.TickInterval <= 0 {
			w.issues = append(w.issues, ValidationIssue{
				Path:     path,
				Code:     "invalid_tick_interval",
				Message:  "tick trigger requires timing.tickInterval > 0",
				Severity: "error",
			})
		}
	}

	for i, action := range trig.Actions {
		w.walkAction(action, fmt.Sprintf("%s.actions[%d]", path, i))
	}
}

// walkAction validates one action (duplicate id, known type, decode +
// descriptor validation) and recurses into any child triggers.
func (w *validationWalker) walkAction(action AbilityActionDef, path string) {
	w.numAction++
	w.checkDuplicateID(action.ID, path)

	if !isKnownActionType(action.Type) {
		w.issues = append(w.issues, ValidationIssue{
			Path:     path,
			Code:     "unsupported_action_type",
			Message:  "unknown action type \"" + string(action.Type) + "\"",
			Severity: "error",
		})
	} else if d, ok := lookupActionDescriptor(action.Type); ok {
		cfg, err := d.Decode(action.Config)
		if err != nil {
			w.issues = append(w.issues, ValidationIssue{
				Path:     path,
				Code:     "invalid_config",
				Message:  err.Error(),
				Severity: "error",
			})
		} else {
			for _, issue := range d.Validate(cfg, ValidationScope{}) {
				if issue.Path == "" {
					issue.Path = path
				} else {
					issue.Path = path + "." + issue.Path
				}
				w.issues = append(w.issues, issue)
			}

			// create_zone, apply_status(authored), and launch_projectile
			// (non-chain) are the ActionTypes whose config carries a decoded,
			// live trigger container (createZoneConfig.Triggers /
			// applyStatusConfig.Triggers / launchProjectileConfig.Triggers,
			// json "triggers" — see ability_zone.go / ability_status.go /
			// ability_compile.go). Only recurse when the decode above
			// succeeded: on failure cfg is the zero value and walking it would
			// validate garbage instead of reporting invalid_config once (which
			// the branch above already did).
			switch action.Type {
			case ActionCreateZone:
				if zc, ok := cfg.(createZoneConfig); ok {
					for i, child := range zc.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i))
					}
				}
			case ActionApplyStatus:
				if ac, ok := cfg.(applyStatusConfig); ok {
					for i, child := range ac.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i))
					}
				}
			case ActionLaunchProjectile:
				if lc, ok := cfg.(launchProjectileConfig); ok {
					for i, child := range lc.Triggers {
						w.walkTrigger(child, fmt.Sprintf("%s.config.triggers[%d]", path, i))
					}
				}
			}
		}
	}
	// else: known action type with no registered descriptor yet — skip
	// silently (no decode, no flag). See task doc for rationale.

	for i, child := range action.Children {
		w.walkTrigger(child, fmt.Sprintf("%s.children[%d]", path, i))
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
