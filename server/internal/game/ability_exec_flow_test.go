package game

import (
	"encoding/json"
	"testing"
	"time"

	"webrts/server/pkg/protocol"
)

// ── store_targets ────────────────────────────────────────────────────────

func TestActionStoreTargets_BindsSelectionUnderNamedKey(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ally := teamCombatUnit(t, s, "p1", 10, 0)

	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "sel", Type: ActionSelectTargets,
					Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCaster,
						Relations: []TargetRelation{RelSelf, RelAlly}, Radius: 1000, Ordering: OrderUnitID}},
				{ID: "store", Type: ActionStoreTargets, Config: json.RawMessage(`{"as":"saved"}`)},
			},
		}},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Named: map[string]ContextValue{}, Trace: tr}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	want := map[int]bool{caster.ID: true, ally.ID: true}
	v, ok := ctx.Named["saved"]
	if !ok {
		t.Fatalf("ctx.Named[%q] missing; want the selected set stored", "saved")
	}
	if len(v.UnitIDs) != len(want) {
		t.Fatalf("stored UnitIDs = %v; want set %v", v.UnitIDs, want)
	}
	for _, id := range v.UnitIDs {
		if !want[id] {
			t.Fatalf("stored UnitIDs = %v; unexpected id %d", v.UnitIDs, id)
		}
	}
	if !traceHas(tr, "targets_stored") {
		t.Fatalf("missing targets_stored trace event: %+v", tr.Events)
	}
}

// TestActionStoreTargets_Merge_UnionsAcrossHopsDedupedInOrder proves the
// chain-lightning accumulator primitive: two successive store_targets
// actions with Merge:true against the same key must union their incoming
// target ids (existing-then-new, deduped, deterministic order) rather than
// replacing the stored set each time. Hop 1's query (origin: caster) and
// hop 2's query (origin: current_event_position, set far away) are
// deliberately DISJOINT — e1 is only reachable from hop 1, e2/e3 only from
// hop 2 — so a wrongly-disabled/replaced merge is distinguishable from a
// real union (a bug that silently replaced would drop e1).
func TestActionStoreTargets_Merge_UnionsAcrossHopsDedupedInOrder(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	e1 := teamCombatUnit(t, s, "p2", 10, 0)
	e2 := teamCombatUnit(t, s, "p2", 500, 0)
	e3 := teamCombatUnit(t, s, "p2", 510, 0)

	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				// Hop 1: select e1 only (radius 50 around caster), merge into "visited".
				{ID: "sel1", Type: ActionSelectTargets,
					Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCaster,
						Relations: []TargetRelation{RelEnemy}, Radius: 50, Ordering: OrderUnitID}},
				{ID: "store1", Type: ActionStoreTargets, Config: json.RawMessage(`{"as":"visited","merge":true}`)},
				// Hop 2: select e2+e3 only (radius 50 around the current-event
				// position, far from caster/e1), merge again.
				{ID: "sel2", Type: ActionSelectTargets,
					Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCurrentEventPos,
						Relations: []TargetRelation{RelEnemy}, Radius: 50, Ordering: OrderUnitID}},
				{ID: "store2", Type: ActionStoreTargets, Config: json.RawMessage(`{"as":"visited","merge":true}`)},
			},
		}},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		Named:         map[string]ContextValue{},
		Trace:         tr,
		EventPosition: protocol.Vec2{X: 505, Y: 0}, // near e2/e3, far from e1
	}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	v, ok := ctx.Named["visited"]
	if !ok {
		t.Fatalf("ctx.Named[%q] missing; want the merged set stored", "visited")
	}
	want := []int{e1.ID, e2.ID, e3.ID}
	if len(v.UnitIDs) != len(want) {
		t.Fatalf("stored UnitIDs = %v; want %v (existing-then-new, deduped)", v.UnitIDs, want)
	}
	for i, id := range want {
		if v.UnitIDs[i] != id {
			t.Fatalf("stored UnitIDs = %v; want %v (order matters: existing e1 then new e2,e3)", v.UnitIDs, want)
		}
	}
}

// TestActionStoreTargets_MergeFalse_StillReplaces guards the default
// behavior: Merge left false (or explicitly false) must behave exactly like
// before this primitive existed — replace, not union. Uses the same disjoint
// hop 1/hop 2 target sets as the merge test above, so a REPLACE result
// ([e2,e3], e1 dropped) is distinguishable from a (wrong) UNION result
// ([e1,e2,e3]).
func TestActionStoreTargets_MergeFalse_StillReplaces(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	_ = teamCombatUnit(t, s, "p2", 10, 0) // e1: must NOT survive into the final replace
	e2 := teamCombatUnit(t, s, "p2", 500, 0)
	e3 := teamCombatUnit(t, s, "p2", 510, 0)

	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "sel1", Type: ActionSelectTargets,
					Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCaster,
						Relations: []TargetRelation{RelEnemy}, Radius: 50, Ordering: OrderUnitID}},
				{ID: "store1", Type: ActionStoreTargets, Config: json.RawMessage(`{"as":"visited"}`)},
				{ID: "sel2", Type: ActionSelectTargets,
					Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCurrentEventPos,
						Relations: []TargetRelation{RelEnemy}, Radius: 50, Ordering: OrderUnitID}},
				{ID: "store2", Type: ActionStoreTargets, Config: json.RawMessage(`{"as":"visited"}`)},
			},
		}},
	}
	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		Named:         map[string]ContextValue{},
		EventPosition: protocol.Vec2{X: 505, Y: 0},
	}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	v, ok := ctx.Named["visited"]
	if !ok {
		t.Fatalf("ctx.Named[%q] missing", "visited")
	}
	want := []int{e2.ID, e3.ID}
	if len(v.UnitIDs) != len(want) {
		t.Fatalf("stored UnitIDs = %v; want %v (second store_targets REPLACED, not merged)", v.UnitIDs, want)
	}
	for i, id := range want {
		if v.UnitIDs[i] != id {
			t.Fatalf("stored UnitIDs = %v; want %v", v.UnitIDs, want)
		}
	}
}

// ── filter_targets ───────────────────────────────────────────────────────

func TestActionFilterTargets_KeepsOnlyMatchingRelation(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ally := teamCombatUnit(t, s, "p1", 10, 0)
	e1 := teamCombatUnit(t, s, "p2", 20, 0)
	e2 := teamCombatUnit(t, s, "p2", 30, 0)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionFilterTargets,
		`{"relations":["enemy"]}`, []int{caster.ID, ally.ID, e1.ID, e2.ID})

	// runOneActionProgram doesn't return the executor's bound Selected, so
	// rebuild via a program that captures the filter output into Named.
	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "filt", Type: ActionFilterTargets, Config: json.RawMessage(`{"relations":["enemy"]}`),
					Outputs: map[string]string{"targets": "filtered"}},
			},
		}},
	}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Selected: []int{caster.ID, ally.ID, e1.ID, e2.ID}, Named: map[string]ContextValue{}, Trace: tr}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	want := map[int]bool{e1.ID: true, e2.ID: true}
	got := ctx.Named["filtered"].UnitIDs
	if len(got) != len(want) {
		t.Fatalf("filtered = %v; want only enemies %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("filtered = %v; unexpected id %d (caster/ally must be excluded)", got, id)
		}
	}
	if !traceHas(tr, "targets_filtered") {
		t.Fatalf("missing targets_filtered trace event: %+v", tr.Events)
	}
}

// ── repeat ───────────────────────────────────────────────────────────────

func TestActionRepeat_RunsBranchActionsCountTimes(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 20, 0)
	enemy.HP, enemy.MaxHP = 100, 100

	repeatCfg := repeatConfig{
		Count: 3,
		Actions: []AbilityActionDef{
			{ID: "dmg", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":10}`)},
		},
	}
	b, err := json.Marshal(repeatCfg)
	if err != nil {
		t.Fatalf("marshal repeatConfig: %v", err)
	}

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionRepeat, string(b), []int{enemy.ID})

	if enemy.HP != 70 {
		t.Fatalf("enemy.HP = %d; want 100-30=70 after repeat(3) x deal_damage(10)", enemy.HP)
	}
	if !traceHas(tr, "repeat") {
		t.Fatalf("missing repeat trace event: %+v", tr.Events)
	}
}

// ── conditional ──────────────────────────────────────────────────────────

func TestActionConditional_ConditionHolds_RunsThenBranch(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 20, 0)
	enemy.HP, enemy.MaxHP = 100, 100

	condCfg := conditionalConfig{
		Conditions: []AbilityConditionDef{{Left: ContextRef{Key: "selected_count"}, Op: "eq", Right: json.RawMessage(`1`)}},
		Then: []AbilityActionDef{
			{ID: "dmg", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":10}`)},
		},
	}
	b, err := json.Marshal(condCfg)
	if err != nil {
		t.Fatalf("marshal conditionalConfig: %v", err)
	}

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionConditional, string(b), []int{enemy.ID})

	if enemy.HP != 90 {
		t.Fatalf("enemy.HP = %d; want 90 (then-branch ran)", enemy.HP)
	}
	if !traceHas(tr, "conditional_taken") {
		t.Fatalf("missing conditional_taken trace event: %+v", tr.Events)
	}
}

func TestActionConditional_ConditionFails_SkipsThenBranch(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 20, 0)
	enemy.HP, enemy.MaxHP = 100, 100

	condCfg := conditionalConfig{
		Conditions: []AbilityConditionDef{{Left: ContextRef{Key: "selected_count"}, Op: "eq", Right: json.RawMessage(`99`)}},
		Then: []AbilityActionDef{
			{ID: "dmg", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":10}`)},
		},
	}
	b, err := json.Marshal(condCfg)
	if err != nil {
		t.Fatalf("marshal conditionalConfig: %v", err)
	}

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionConditional, string(b), []int{enemy.ID})

	if enemy.HP != 100 {
		t.Fatalf("enemy.HP = %d; want unchanged 100 (then-branch must not run)", enemy.HP)
	}
	if !traceHas(tr, "condition_failed") {
		t.Fatalf("missing condition_failed trace event: %+v", tr.Events)
	}
}

// ── trigger_event ────────────────────────────────────────────────────────

func TestActionTriggerEvent_InvokesNamedTrigger(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 20, 0)
	enemy.HP, enemy.MaxHP = 100, 100

	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "boom_call", Type: ActionTriggerEvent, Config: json.RawMessage(`{"trigger":"boom"}`)},
			},
		}},
		NamedTriggers: map[string]AbilityTriggerDef{
			"boom": {
				ID:   "boom",
				Type: TriggerCustom,
				Actions: []AbilityActionDef{
					{ID: "dmg", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":15}`)},
				},
			},
		},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Selected: []int{enemy.ID}, Named: map[string]ContextValue{}, Trace: tr, program: prog}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if enemy.HP != 85 {
		t.Fatalf("enemy.HP = %d; want 85 (named trigger 'boom' invoked deal_damage(15))", enemy.HP)
	}
	if !traceHas(tr, "named_trigger_invoked") {
		t.Fatalf("missing named_trigger_invoked trace event: %+v", tr.Events)
	}
}

// TestActionTriggerEvent_SelfRecursion_TerminatesViaDepthGuard proves the
// mandatory recursion guard: a named trigger that invokes itself must
// terminate (bounded by ctx.depth / maxTriggerDepth), not hang the
// synchronous executor, and must emit a recursion_guard trace event.
func TestActionTriggerEvent_SelfRecursion_TerminatesViaDepthGuard(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "loop_call", Type: ActionTriggerEvent, Config: json.RawMessage(`{"trigger":"loop"}`)},
			},
		}},
		NamedTriggers: map[string]AbilityTriggerDef{
			"loop": {
				ID:   "loop",
				Type: TriggerCustom,
				Actions: []AbilityActionDef{
					{ID: "recurse", Type: ActionTriggerEvent, Config: json.RawMessage(`{"trigger":"loop"}`)},
				},
			},
		},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Named: map[string]ContextValue{}, Trace: tr, program: prog}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runProgramTriggersLocked did not terminate; recursion guard failed to bound self-invoking named trigger")
	}

	if !traceHas(tr, "recursion_guard") {
		t.Fatalf("missing recursion_guard trace event (guard must fire): %+v", tr.Events)
	}
	if ctx.depth != 0 {
		t.Fatalf("ctx.depth = %d after unwind; want 0 (depth must be restored on return)", ctx.depth)
	}
}

// TestExecutor_OpBudget_BoundsExponentialFanout proves the shared
// maxExecutionOps TOTAL-WORK budget: ctx.depth/maxTriggerDepth alone bounds
// recursion STACK DEPTH, not total work, so a bounded-depth multiplier
// fan-out (repeat(64){ trigger_event(self) }) is 64^maxTriggerDepth
// executeActionLocked calls without ever tripping the depth guard. The op
// budget must bound total work regardless of nesting shape, terminate
// quickly, and emit an op_budget_exceeded trace.
func TestExecutor_OpBudget_BoundsExponentialFanout(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	repeatCfg := repeatConfig{
		Count: 64,
		Actions: []AbilityActionDef{
			{ID: "call_boom", Type: ActionTriggerEvent, Config: json.RawMessage(`{"trigger":"boom"}`)},
			// A second, cheap, non-recursive action after the recursive one:
			// repeat's inner per-iteration action loop is deliberately left
			// unguarded (only the outer count loop pre-checks the budget), so
			// once the recursive call_boom exhausts the budget mid-traversal,
			// THIS action is the one that actually trips executeActionLocked's
			// own op-budget gate and proves the op_budget_exceeded trace fires
			// (not just that the loops quietly stop recursing).
			{ID: "noop", Type: ActionStoreTargets, Config: json.RawMessage(`{"as":"unused"}`)},
		},
	}
	repeatCfgJSON, err := json.Marshal(repeatCfg)
	if err != nil {
		t.Fatalf("marshal repeatConfig: %v", err)
	}

	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "start", Type: ActionTriggerEvent, Config: json.RawMessage(`{"trigger":"boom"}`)},
			},
		}},
		NamedTriggers: map[string]AbilityTriggerDef{
			"boom": {
				ID:   "boom",
				Type: TriggerCustom,
				Actions: []AbilityActionDef{
					{ID: "fanout", Type: ActionRepeat, Config: json.RawMessage(repeatCfgJSON)},
				},
			},
		},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Named: map[string]ContextValue{}, Trace: tr, program: prog}

	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runProgramTriggersLocked did not terminate; op budget failed to bound repeat(64){trigger_event(self)} exponential fan-out")
	}
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Fatalf("execution took %v; want well under 1s", elapsed)
	}
	if !traceHas(tr, "op_budget_exceeded") {
		t.Fatalf("missing op_budget_exceeded trace event (budget must fire); recorded %d events", len(tr.Events))
	}
	if ctx.opsUsed < maxExecutionOps {
		t.Fatalf("ctx.opsUsed = %d; want >= maxExecutionOps (%d)", ctx.opsUsed, maxExecutionOps)
	}
	// The budget must bound total work to roughly maxExecutionOps, not let it
	// balloon far past it (each loop breaks promptly once the budget trips).
	if ctx.opsUsed > maxExecutionOps+10000 {
		t.Fatalf("ctx.opsUsed = %d; want bounded near maxExecutionOps (%d), not astronomically higher", ctx.opsUsed, maxExecutionOps)
	}
	t.Logf("op-budget fanout test: elapsed=%v opsUsed=%d (maxExecutionOps=%d)", elapsed, ctx.opsUsed, maxExecutionOps)
}
