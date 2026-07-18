package game

import "testing"

func TestRuntimeContextAndTrace(t *testing.T) {
	tr := &AbilityExecutionTrace{}
	tr.record(0, "cast_started", "", nil)
	if len(tr.Events) != 1 || tr.Events[0].Type != "cast_started" {
		t.Fatalf("trace not recorded: %+v", tr.Events)
	}
	ctx := &RuntimeAbilityContext{CasterID: 7, Named: map[string]ContextValue{}}
	ctx.Named["x"] = ContextValue{Kind: ctxUnitSet, UnitIDs: []int{1, 2}}
	if got := ctx.Named["x"].UnitIDs; len(got) != 2 {
		t.Fatalf("context set = %v", got)
	}
}

func TestTraceNilSafe(t *testing.T) {
	// A nil trace (production path) must be a no-op, never panic.
	var tr *AbilityExecutionTrace
	tr.record(0, "x", "", nil)
	ctx := &RuntimeAbilityContext{} // Trace is nil
	ctx.trace("y", "", nil)         // must not panic
}
