package game

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// Flow-control actions (Phase 3, Task 6):
// store_targets, filter_targets, wait, conditional, repeat, trigger_event.
//
// conditional and repeat carry their branch actions in their typed Config
// (Then / Actions) rather than a.Children — a.Children auto-fires after
// EVERY action via executeActionLocked's on_action_complete recursion, which
// would make a conditional's branch run unconditionally. Gating stays
// self-contained: each Execute calls s.executeActionLocked directly on its
// own branch actions.
// ─────────────────────────────────────────────────────────────────────────────

// maxRepeatCount bounds the repeat action's iteration count so a
// hand-authored or malformed program cannot spin the synchronous executor
// forever.
const maxRepeatCount = 64

// evaluateConditionsLocked returns true iff ALL conditions pass (empty =
// true). Phase 3 supports a minimal set (see evaluateOneConditionLocked);
// unknown condition shapes conservatively FAIL (return false) so an
// unrecognized gate never silently runs a branch. This is a seed the later
// phase extends. Caller holds s.mu (kept a method for symmetry with the
// other *Locked evaluators even though the minimal Phase 3 set doesn't
// currently need GameState).
func (s *GameState) evaluateConditionsLocked(ctx *RuntimeAbilityContext, conds []AbilityConditionDef) bool {
	for _, c := range conds {
		if !evaluateOneConditionLocked(ctx, c) {
			return false
		}
	}
	return true
}

// evaluateOneConditionLocked evaluates a single AbilityConditionDef. Phase 3
// minimal set:
//   - Left.Key == "selected_count" with Op in {eq,ne,lt,lte,gt,gte}: compares
//     len(ctx.Selected) against Right (unmarshaled as a number).
//   - Op == "has": true if ctx.Named[Left.Key] exists and is non-empty.
//   - Op == "not": true if it does NOT exist / is empty.
//   - Anything else: false (documented conservative-fail).
func evaluateOneConditionLocked(ctx *RuntimeAbilityContext, c AbilityConditionDef) bool {
	switch c.Op {
	case "eq", "ne", "lt", "lte", "gt", "gte":
		if c.Left.Key != "selected_count" {
			return false
		}
		var want float64
		if err := json.Unmarshal(c.Right, &want); err != nil {
			return false
		}
		got := float64(len(ctx.Selected))
		switch c.Op {
		case "eq":
			return got == want
		case "ne":
			return got != want
		case "lt":
			return got < want
		case "lte":
			return got <= want
		case "gt":
			return got > want
		case "gte":
			return got >= want
		}
		return false
	case "has":
		v, ok := ctx.Named[c.Left.Key]
		return ok && namedContextValueNonEmpty(v)
	case "not":
		v, ok := ctx.Named[c.Left.Key]
		return !ok || !namedContextValueNonEmpty(v)
	default:
		return false
	}
}

// namedContextValueNonEmpty reports whether a ContextValue carries a usable
// (non-empty) binding, for the "has"/"not" condition operators.
func namedContextValueNonEmpty(v ContextValue) bool {
	switch v.Kind {
	case ctxUnitSet:
		return len(v.UnitIDs) > 0
	case ctxUnitID:
		return v.UnitID != 0
	case ctxPosition:
		return true
	default:
		return false
	}
}

// ── store_targets ───────────────────────────────────────────────────────

// storeTargetsConfig binds the action's incoming target set under a named
// context key (ctx.Named[As]) without altering the target set itself.
type storeTargetsConfig struct {
	As string `json:"as"`
	// Merge, when true, unions the incoming target IDs into the existing
	// named ctxUnitSet at As (dedup, preserving first-seen order) instead of
	// replacing it. Lets a chain accumulate a visited set across hops.
	Merge bool `json:"merge,omitempty"`
}

func (storeTargetsConfig) actionConfig() {}

// ── filter_targets ──────────────────────────────────────────────────────

// filterTargetsConfig filters the action's INCOMING target set (not the
// scene) by relation/alive-state, then orders and caps it — the same
// filter/order/cap semantics as a TargetQueryDef, applied via
// applyTargetFiltersLocked. Note: it applies the same default alive-only +
// per-candidate hostile-invisible filter as scene queries, and — when
// Ordering is left unset — falls back to sorting by unit.ID, so "empty
// relations = keep all" is NOT the same as "order-preserving passthrough".
type filterTargetsConfig struct {
	Relations  []TargetRelation `json:"relations,omitempty"`
	AliveState string           `json:"aliveState,omitempty"`
	MaxCount   int              `json:"maxCount,omitempty"`
	Ordering   TargetOrdering   `json:"ordering,omitempty"`
}

func (filterTargetsConfig) actionConfig() {}

// ── wait ─────────────────────────────────────────────────────────────────

// waitConfig is a Phase 3 no-op placeholder: there is no tick-scheduler /
// preview clock threaded into the synchronous executor yet, so Execute only
// traces the requested delay and passes targets through unchanged.
type waitConfig struct {
	Seconds float64 `json:"seconds"`
}

func (waitConfig) actionConfig() {}

// ── conditional ─────────────────────────────────────────────────────────

// conditionalConfig gates Then (run only if Conditions all pass) — see the
// package doc comment above for why the branch lives in Config, not
// a.Children.
type conditionalConfig struct {
	Conditions []AbilityConditionDef `json:"conditions,omitempty"`
	Then       []AbilityActionDef    `json:"then,omitempty"`
}

func (conditionalConfig) actionConfig() {}

// ── repeat ───────────────────────────────────────────────────────────────

// repeatConfig runs Actions Count times (capped at maxRepeatCount) — see the
// package doc comment above for why the branch lives in Config, not
// a.Children.
type repeatConfig struct {
	Count   int                `json:"count"`
	Actions []AbilityActionDef `json:"actions,omitempty"`
}

func (repeatConfig) actionConfig() {}

// ── trigger_event ───────────────────────────────────────────────────────

// triggerEventConfig invokes a program-level named trigger (ctx.program.
// NamedTriggers[Trigger]) by ID. Guarded by maxTriggerDepth (see
// RuntimeAbilityContext.depth in ability_exec.go) against self/mutual
// recursion.
type triggerEventConfig struct {
	Trigger string `json:"trigger"`
}

func (triggerEventConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionStoreTargets,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c storeTargetsConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(storeTargetsConfig)
			if c.As == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "store_targets requires as", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "as", Label: "Store As", Control: "text", Section: "Basic"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(storeTargetsConfig)
			if ctx.Named == nil {
				ctx.Named = map[string]ContextValue{}
			}
			stored := append([]int(nil), targets...)
			if c.Merge {
				if existing, ok := ctx.Named[c.As]; ok && existing.Kind == ctxUnitSet {
					// Union existing-then-new, deduped, deterministic order:
					// walk the existing slice first (already in its stored
					// order), then the incoming slice, skipping anything
					// already seen. Never range a map to build the output.
					seen := make(map[int]struct{}, len(existing.UnitIDs)+len(stored))
					merged := make([]int, 0, len(existing.UnitIDs)+len(stored))
					for _, id := range existing.UnitIDs {
						if _, dup := seen[id]; dup {
							continue
						}
						seen[id] = struct{}{}
						merged = append(merged, id)
					}
					for _, id := range stored {
						if _, dup := seen[id]; dup {
							continue
						}
						seen[id] = struct{}{}
						merged = append(merged, id)
					}
					stored = merged
				}
			}
			ctx.Named[c.As] = ContextValue{Kind: ctxUnitSet, UnitIDs: stored}
			ctx.trace("targets_stored", ctx.currentActionPath, map[string]any{"as": c.As, "count": len(stored), "merge": c.Merge})
			return targets
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionFilterTargets,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c filterTargetsConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue { return nil },
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "relations", Label: "Relations", Control: "multiselect", Section: "Targeting"},
			{Key: "aliveState", Label: "Alive State", Control: "enum", Section: "Targeting"},
			{Key: "maxCount", Label: "Max Count", Control: "number", Section: "Targeting"},
			{Key: "ordering", Label: "Ordering", Control: "enum", Section: "Targeting"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(filterTargetsConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			candidates := make([]*Unit, 0, len(targets))
			for _, id := range targets {
				if u := s.getUnitByIDLocked(id); u != nil {
					candidates = append(candidates, u)
				}
			}
			q := TargetQueryDef{
				Relations:  c.Relations,
				AliveState: c.AliveState,
				MaxCount:   c.MaxCount,
				Ordering:   c.Ordering,
			}
			filtered := s.applyTargetFiltersLocked(ctx, caster, candidates, q)
			ctx.trace("targets_filtered", ctx.currentActionPath, map[string]any{"count": len(filtered)})
			return filtered
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionWait,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c waitConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue { return nil },
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "seconds", Label: "Seconds", Control: "duration", Section: "Timing"},
		}},
		// TODO(phase-6): real timed wait once the tick-scheduler / preview clock lands.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(waitConfig)
			ctx.trace("wait", ctx.currentActionPath, map[string]any{"seconds": c.Seconds})
			return targets
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionConditional,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c conditionalConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue { return nil },
		// conditional's "conditions"/"then" used to declare nested_triggers
		// fields here. Unlike create_zone/apply_status/launch_projectile's
		// config.triggers (a slice of AbilityTriggerDef, real cards in the
		// flow view — see programTree.ts's CONFIG_TRIGGER_ACTION_TYPES),
		// Conditions ([]AbilityConditionDef) and Then ([]AbilityActionDef)
		// are a GENUINELY DIFFERENT shape the flow view has no rendering for
		// at all — nestedTriggersFor only ever walks children/config.triggers
		// (AbilityTriggerDef slices), never a raw action or condition list.
		// So removing the field here isn't closing a redundancy, it's
		// removing a stub that was already non-functional (SchemaField.vue's
		// nested_triggers control has only ever rendered a static "edit in
		// flow view" note — there was no such view to edit it in for these
		// two keys). See the report to the frontend follow-up task: branch
		// authoring needs its own real editing surface, not this one.
		Schema: ActionFieldSchema{Fields: []SchemaField{}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(conditionalConfig)
			if s.evaluateConditionsLocked(ctx, c.Conditions) {
				ctx.trace("conditional_taken", ctx.currentActionPath, map[string]any{"count": len(c.Then)})
				for i := range c.Then {
					if ctx.opsExhausted() {
						break
					}
					s.executeActionLocked(ctx, &c.Then[i], "conditional.then")
				}
			} else {
				ctx.trace("condition_failed", ctx.currentActionPath, nil)
			}
			return targets
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionRepeat,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c repeatConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(repeatConfig)
			if c.Count < 0 {
				return []ValidationIssue{{Code: "invalid_property", Message: "repeat requires count >= 0", Severity: "error"}}
			}
			return nil
		},
		// "actions" ([]AbilityActionDef) used to declare a nested_triggers
		// field here too — see conditional's identical note just above: it's
		// the same genuinely-unrendered shape, not a redundancy with the flow
		// view's config.triggers cards.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "count", Label: "Count", Control: "number", Section: "Properties"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(repeatConfig)
			n := c.Count
			if n > maxRepeatCount {
				n = maxRepeatCount
				ctx.trace("repeat_capped", ctx.currentActionPath, map[string]any{"requested": c.Count, "cappedTo": maxRepeatCount})
			}
			if n < 0 {
				n = 0
			}
			for i := 0; i < n; i++ {
				if ctx.opsExhausted() {
					break
				}
				for ai := range c.Actions {
					s.executeActionLocked(ctx, &c.Actions[ai], "repeat")
				}
			}
			ctx.trace("repeat", ctx.currentActionPath, map[string]any{"count": n})
			return targets
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionTriggerEvent,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c triggerEventConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(triggerEventConfig)
			if c.Trigger == "" {
				return []ValidationIssue{{Code: "empty_required_property", Message: "trigger_event requires trigger", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "trigger", Label: "Trigger", Control: "text", Section: "Basic"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(triggerEventConfig)
			if ctx.program == nil {
				ctx.trace("no_program", ctx.currentActionPath, map[string]any{"trigger": c.Trigger})
				return nil
			}
			trg, ok := ctx.program.NamedTriggers[c.Trigger]
			if !ok {
				ctx.trace("unknown_named_trigger", ctx.currentActionPath, map[string]any{"trigger": c.Trigger})
				return nil
			}
			if ctx.depth >= maxTriggerDepth {
				ctx.trace("recursion_guard", ctx.currentActionPath, map[string]any{"trigger": c.Trigger, "depth": ctx.depth})
				return nil
			}
			ctx.depth++
			defer func() { ctx.depth-- }() // robustness: restores depth even if anything below ever panics
			for i := range trg.Actions {
				if ctx.opsExhausted() {
					break
				}
				s.executeActionLocked(ctx, &trg.Actions[i], "namedTrigger["+c.Trigger+"]")
			}
			ctx.trace("named_trigger_invoked", ctx.currentActionPath, map[string]any{"trigger": c.Trigger})
			return nil
		},
	})
}
