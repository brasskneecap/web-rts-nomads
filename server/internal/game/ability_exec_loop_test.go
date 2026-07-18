package game

import (
	"encoding/json"
	"strconv"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Loop primitives: set_context (scalar counter) + scalar-comparison conditions.
// These are the building blocks the looped chain_lightning form uses to cap
// bounces and decay damage. Tested directly here; the end-to-end cap + falloff
// live in chain_lightning_test.go.
// ─────────────────────────────────────────────────────────────────────────────

// runSetContext runs a single set_context action against a fresh ctx seeded
// with the given Named map and returns the resulting scalar bound at key.
func runSetContext(t *testing.T, s *GameState, caster *Unit, seed map[string]ContextValue, cfg string, key string) (float64, bool) {
	t.Helper()
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID:   "t",
		Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{
			{ID: "set", Type: ActionSetContext, Config: json.RawMessage(cfg)},
		},
	}}}
	if seed == nil {
		seed = map[string]ContextValue{}
	}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Named: seed, Trace: &AbilityExecutionTrace{}}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
	v, ok := ctx.Named[key]
	if !ok || v.Kind != ctxScalar {
		return 0, false
	}
	return v.Scalar, true
}

func TestActionSetContext_SetAndAdd(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)

	// set: writes the literal value regardless of prior binding.
	if got, ok := runSetContext(t, s, caster, nil, `{"key":"hop","op":"set","value":4}`, "hop"); !ok || got != 4 {
		t.Fatalf("set: got (%v, %v); want (4, true)", got, ok)
	}

	// add with no prior binding treats the counter as 0.
	if got, ok := runSetContext(t, s, caster, nil, `{"key":"hop","op":"add","value":1}`, "hop"); !ok || got != 1 {
		t.Fatalf("add-from-unset: got (%v, %v); want (1, true)", got, ok)
	}

	// add onto an existing scalar increments it.
	seed := map[string]ContextValue{"hop": {Kind: ctxScalar, Scalar: 2}}
	if got, ok := runSetContext(t, s, caster, seed, `{"key":"hop","op":"add","value":1}`, "hop"); !ok || got != 3 {
		t.Fatalf("add-onto-2: got (%v, %v); want (3, true)", got, ok)
	}

	// empty op defaults to set.
	if got, ok := runSetContext(t, s, caster, nil, `{"key":"hop","value":7}`, "hop"); !ok || got != 7 {
		t.Fatalf("empty-op: got (%v, %v); want (7, true)", got, ok)
	}
}

// conditionalRunsThen builds a conditional gated on `hop <op> right` with the
// given hop scalar seeded and reports whether the Then branch ran (detected via
// a store_targets that binds a marker key).
func conditionalRunsThen(t *testing.T, s *GameState, caster *Unit, hop float64, op string, right int) bool {
	t.Helper()
	cond := AbilityConditionDef{Type: "scalar", Left: ContextRef{Key: "hop"}, Op: op, Right: json.RawMessage(strconv.Itoa(right))}
	condCfg, _ := json.Marshal(conditionalConfig{
		Conditions: []AbilityConditionDef{cond},
		Then: []AbilityActionDef{
			{ID: "mark", Type: ActionSetContext, Config: json.RawMessage(`{"key":"ran","op":"set","value":1}`)},
		},
	})
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "t", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{ID: "gate", Type: ActionConditional, Config: condCfg}},
	}}}
	ctx := &RuntimeAbilityContext{
		CasterID: caster.ID,
		Named:    map[string]ContextValue{"hop": {Kind: ctxScalar, Scalar: hop}},
		Trace:    &AbilityExecutionTrace{},
	}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
	v, ok := ctx.Named["ran"]
	return ok && v.Kind == ctxScalar && v.Scalar == 1
}

// TestLoopAction_StepsVarsAndCaps runs a no-wait loop synchronously: its body
// deals damage referencing variable `a`, which steps down each iteration, and
// the loop runs exactly `iterations` times. 65 + 60 + 55 = 180 proves both the
// stepping and the cap (a 4th pass would overshoot).
func TestLoopAction_StepsVarsAndCaps(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 10, 0)
	enemy.HP, enemy.MaxHP = 1000, 1000
	start := enemy.HP

	loopCfg := `{"iterations":3,"vars":[{"name":"a","start":65,"step":-5}],"body":[` +
		`{"id":"dmg","type":"deal_damage","target":{"source":"initial_target"},"config":{"amount":"a","type":"lightning"}}` +
		`]}`
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "t", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{ID: "loop", Type: ActionLoop, Config: json.RawMessage(loopCfg)}},
	}}}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, InitialTarget: enemy.ID, Named: map[string]ContextValue{}, Trace: &AbilityExecutionTrace{}}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if got := start - enemy.HP; got != 180 {
		t.Fatalf("no-wait loop dealt %d total; want 65+60+55=180 (var stepping + cap at 3)", got)
	}
	// A no-wait loop runs entirely in one call — nothing left scheduled.
	if len(s.pendingLoops) != 0 {
		t.Fatalf("no-wait loop left %d iterations scheduled; want 0 (all run synchronously)", len(s.pendingLoops))
	}
}

// TestLoopAction_PercentStepCompounds proves a percent-mode variable steps
// MULTIPLICATIVELY and rounds: start 100, step -10% → 100, 90, 81 (each 90% of
// the last), total 271.
func TestLoopAction_PercentStepCompounds(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 10, 0)
	enemy.HP, enemy.MaxHP = 1000, 1000
	start := enemy.HP

	loopCfg := `{"iterations":3,"vars":[{"name":"a","start":100,"step":-10,"stepMode":"percent"}],"body":[` +
		`{"id":"dmg","type":"deal_damage","target":{"source":"initial_target"},"config":{"amount":"a","type":"lightning"}}` +
		`]}`
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "t", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{ID: "loop", Type: ActionLoop, Config: json.RawMessage(loopCfg)}},
	}}}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, InitialTarget: enemy.ID, Named: map[string]ContextValue{}, Trace: &AbilityExecutionTrace{}}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if got := start - enemy.HP; got != 271 {
		t.Fatalf("percent-step loop dealt %d total; want 100+90+81=271 (×0.9 compounding, rounded)", got)
	}
}

// TestLoopAction_StepFirstAppliesToFirstIteration: with stepFirst, iteration 0
// is already stepped once — start 100, step -10 (flat), 3 iters → 90+80+70=240
// (vs 100+90+80=270 without it).
func TestLoopAction_StepFirstAppliesToFirstIteration(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 10, 0)
	enemy.HP, enemy.MaxHP = 1000, 1000
	start := enemy.HP

	loopCfg := `{"iterations":3,"stepFirst":true,"vars":[{"name":"a","start":100,"step":-10}],"body":[` +
		`{"id":"dmg","type":"deal_damage","target":{"source":"initial_target"},"config":{"amount":"a","type":"lightning"}}` +
		`]}`
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "t", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{ID: "loop", Type: ActionLoop, Config: json.RawMessage(loopCfg)}},
	}}}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, InitialTarget: enemy.ID, Named: map[string]ContextValue{}, Trace: &AbilityExecutionTrace{}}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if got := start - enemy.HP; got != 240 {
		t.Fatalf("stepFirst loop dealt %d total; want 90+80+70=240 (first iteration stepped)", got)
	}
}

// TestLoopAction_WaitSchedulesIterationsOverTime proves the timed path: a body
// with a wait runs iteration 0 now and defers the rest, one per scheduler tick
// spaced by the wait.
func TestLoopAction_WaitSchedulesIterationsOverTime(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 10, 0)
	enemy.HP, enemy.MaxHP = 1000, 1000
	start := enemy.HP

	loopCfg := `{"iterations":3,"vars":[{"name":"a","start":10,"step":0}],"body":[` +
		`{"id":"dmg","type":"deal_damage","target":{"source":"initial_target"},"config":{"amount":"a","type":"lightning"}},` +
		`{"id":"gap","type":"wait","config":{"seconds":0.1}}` +
		`]}`
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "t", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{ID: "loop", Type: ActionLoop, Config: json.RawMessage(loopCfg)}},
	}}}
	// program is needed so fireLoopIterationLocked can re-resolve the def by id —
	// register a throwaway ability whose program is this one (restored after).
	prev, had := runtimeAbilities["loop_test"]
	runtimeAbilities["loop_test"] = AbilityDef{ID: "loop_test", SchemaVersion: 2, Program: prog}
	t.Cleanup(func() {
		if had {
			runtimeAbilities["loop_test"] = prev
		} else {
			delete(runtimeAbilities, "loop_test")
		}
	})
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "loop_test", InitialTarget: enemy.ID, Named: map[string]ContextValue{}, Trace: &AbilityExecutionTrace{}, program: prog}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	// With a body wait, EVERY iteration is scheduled (the wait spaces them after
	// whatever preceded the loop), so nothing lands at cast time.
	if got := start - enemy.HP; got != 0 {
		t.Fatalf("after cast: dealt %d; want 0 (all iterations scheduled, none synchronous)", got)
	}
	if len(s.pendingLoops) != 1 {
		t.Fatalf("after cast: %d pending; want 1 (iteration 0 scheduled)", len(s.pendingLoops))
	}
	// Drive the scheduler until drained: all 3 iterations fire, spaced by the wait.
	for i := 0; i < 30 && len(s.pendingLoops) > 0; i++ {
		s.simTime += 0.05
		s.tickPendingLoopsLocked()
	}
	if got := start - enemy.HP; got != 30 {
		t.Fatalf("after draining: dealt %d total; want 3×10=30", got)
	}
}

func TestResolveConfigVars_ReplacesBareVariableLetters(t *testing.T) {
	ctx := &RuntimeAbilityContext{Named: map[string]ContextValue{
		"a": {Kind: ctxScalar, Scalar: 42},
	}}
	out := ctx.resolveConfigVars(json.RawMessage(`{"amount":"a","type":"lightning","flatOffset":"b"}`))

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("resolved config is not valid JSON: %v", err)
	}
	if m["amount"] != float64(42) {
		t.Errorf("amount = %v, want the bound variable value 42", m["amount"])
	}
	if m["type"] != "lightning" {
		t.Errorf("non-variable string field was mangled: type = %v", m["type"])
	}
	// "b" isn't bound in this context, so it stays a literal string (unresolved),
	// not silently turned into a number.
	if m["flatOffset"] != "b" {
		t.Errorf("unbound letter should pass through untouched, got %v", m["flatOffset"])
	}
}

func TestResolveConfigVars_NoVarsInScopeIsNoOp(t *testing.T) {
	ctx := &RuntimeAbilityContext{Named: map[string]ContextValue{
		"hit": {Kind: ctxUnitSet, UnitIDs: []int{1, 2}}, // not a single-letter scalar
	}}
	in := json.RawMessage(`{"amount":"a"}`)
	out := ctx.resolveConfigVars(in)
	if string(out) != string(in) {
		t.Errorf("with no loop vars in scope, config must be returned unchanged; got %s", out)
	}
}

func TestScalarCondition_GatesOnNamedCounter(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()
	caster := teamCombatUnit(t, s, "p1", 0, 0)

	// `hop < 3`: true at 2, false at 3 — the exact cap gate chain_lightning uses.
	if !conditionalRunsThen(t, s, caster, 2, "lt", 3) {
		t.Errorf("hop=2 < 3 should run the branch")
	}
	if conditionalRunsThen(t, s, caster, 3, "lt", 3) {
		t.Errorf("hop=3 < 3 should NOT run the branch (cap reached)")
	}
	// A few more operators for good measure.
	if !conditionalRunsThen(t, s, caster, 3, "gte", 3) {
		t.Errorf("hop=3 >= 3 should run the branch")
	}
	if !conditionalRunsThen(t, s, caster, 3, "eq", 3) {
		t.Errorf("hop=3 == 3 should run the branch")
	}
}
