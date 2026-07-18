package game

import (
	"encoding/json"
	"testing"
)

// traceHas reports whether tr recorded at least one event of type typ.
func traceHas(tr *AbilityExecutionTrace, typ string) bool {
	if tr == nil {
		return false
	}
	for _, e := range tr.Events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// TestExecuteGreaterHealFlow is the first end-to-end executor test: a
// Greater-Heal-shaped program (select_targets → restore_health) run through
// runProgramTriggersLocked against real spawned units. Guards the executor
// loop, target-set threading via Outputs/Input, and the restore_health
// action's use of applyClericHealLocked. NOT wired into the live cast path —
// this test calls the executor directly.
func TestExecuteGreaterHealFlow(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.HP, caster.MaxHP = 100, 100

	a1 := teamCombatUnit(t, s, "p1", 40, 0)
	a1.HP, a1.MaxHP = 20, 100

	a2 := teamCombatUnit(t, s, "p1", 80, 0)
	a2.HP, a2.MaxHP = 60, 100

	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelSelf, RelAlly}},
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "sel", Type: ActionSelectTargets, Outputs: map[string]string{"targets": "heals"},
				Target: &TargetQueryDef{Source: SrcAllInScene, Origin: OriginCaster,
					Relations: []TargetRelation{RelSelf, RelAlly}, Radius: 300,
					Ordering: OrderLowestHealthPct, MaxCount: 3, IncludeInitialTarget: true}},
			{ID: "heal", Type: ActionRestoreHealth,
				Input:  map[string]ContextRef{"targets": {Key: "heals"}},
				Config: json.RawMessage(`{"amount":15,"school":"holy"}`)},
		}}},
	}

	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, AbilityID: "greater_heal", Named: map[string]ContextValue{}, Trace: tr}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if a1.HP != 35 || a2.HP != 75 {
		t.Fatalf("heals wrong: a1=%d (want 35) a2=%d (want 75)", a1.HP, a2.HP)
	}
	if !traceHas(tr, "targets_selected") || !traceHas(tr, "healing_applied") {
		t.Fatalf("missing trace events: %+v", tr.Events)
	}
}
