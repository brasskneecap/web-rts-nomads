package game

import (
	"encoding/json"
	"testing"
)

// runOneActionProgram builds a single-trigger, single-action program of the
// given ActionType + raw config JSON, targeting the resolved unit ids in
// targets via a "targets" Input/Outputs handoff through a select-less
// literal binding. Since the executor's Input resolution only understands
// named context keys and the "selected"/"initial_target" sentinels (see
// resolveTargetRef), the simplest way to hand a literal target set to a
// single action under test is to seed ctx.Selected before running — that's
// exactly what resolveActionTargetsLocked falls back to when the action has
// no Target query and no Input["targets"].
func runOneActionProgram(t *testing.T, s *GameState, casterID int, initialTarget int, actionType ActionType, cfg string, selected []int) *AbilityExecutionTrace {
	t.Helper()
	prog := &AbilityProgram{
		Triggers: []AbilityTriggerDef{{
			ID:   "t",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "a", Type: actionType, Config: json.RawMessage(cfg)},
			},
		}},
	}
	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID:      casterID,
		AbilityID:     "test_" + string(actionType),
		InitialTarget: initialTarget,
		Selected:      selected,
		Named:         map[string]ContextValue{},
		Trace:         tr,
	}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
	return tr
}

// ── apply_force ──────────────────────────────────────────────────────────

func TestActionApplyForce_PullsTargetTowardCaster(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 200, 0)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionApplyForce,
		`{"strength":100,"duration":2}`, []int{enemy.ID})

	if enemy.PullRemaining <= 0 {
		t.Fatalf("enemy.PullRemaining = %v; want > 0 after apply_force", enemy.PullRemaining)
	}
	if enemy.PullCenterX != caster.X || enemy.PullCenterY != caster.Y {
		t.Fatalf("enemy pull center = (%v,%v); want caster pos (%v,%v)", enemy.PullCenterX, enemy.PullCenterY, caster.X, caster.Y)
	}
	if !traceHas(tr, "force_applied") {
		t.Fatalf("missing force_applied trace event: %+v", tr.Events)
	}
}

// ── apply_status: slow ───────────────────────────────────────────────────

func TestActionApplyStatus_Slow_Cold_SetsColdSlowTrack(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatus,
		`{"status":"slow","multiplier":0.5,"duration":3,"school":"cold"}`, []int{enemy.ID})

	if enemy.ColdSlowedRemaining <= 0 {
		t.Fatalf("enemy.ColdSlowedRemaining = %v; want > 0 for cold school", enemy.ColdSlowedRemaining)
	}
	if enemy.SlowedRemaining > 0 {
		t.Fatalf("enemy.SlowedRemaining = %v; want 0 (cold school must not touch physical track)", enemy.SlowedRemaining)
	}
	if !traceHas(tr, "status_applied") {
		t.Fatalf("missing status_applied trace event: %+v", tr.Events)
	}
}

func TestActionApplyStatus_Slow_Physical_SetsPhysicalSlowTrack(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatus,
		`{"status":"slow","multiplier":0.5,"duration":3}`, []int{enemy.ID})

	if enemy.SlowedRemaining <= 0 {
		t.Fatalf("enemy.SlowedRemaining = %v; want > 0 when school omitted (defaults physical)", enemy.SlowedRemaining)
	}
	if enemy.ColdSlowedRemaining > 0 {
		t.Fatalf("enemy.ColdSlowedRemaining = %v; want 0 (physical/omitted school must not touch cold track)", enemy.ColdSlowedRemaining)
	}
}

// ── apply_status: stun ───────────────────────────────────────────────────

func TestActionApplyStatus_Stun_SetsStunnedRemaining(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatus,
		`{"status":"stun","duration":1.5}`, []int{enemy.ID})

	if enemy.StunnedRemaining <= 0 {
		t.Fatalf("enemy.StunnedRemaining = %v; want > 0 after stun apply_status", enemy.StunnedRemaining)
	}
}

// ── remove_status ────────────────────────────────────────────────────────

func TestActionRemoveStatus_Slow_ClearsBothTracks(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)
	s.ApplySlowLocked(enemy.ID, 0.5, 5)
	s.ApplyColdSlowLocked(enemy.ID, 0.5, 5)
	if enemy.SlowedRemaining <= 0 || enemy.ColdSlowedRemaining <= 0 {
		t.Fatalf("setup failed: slow tracks not primed (%v, %v)", enemy.SlowedRemaining, enemy.ColdSlowedRemaining)
	}

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionRemoveStatus,
		`{"status":"slow"}`, []int{enemy.ID})

	if enemy.SlowedRemaining != 0 || enemy.ColdSlowedRemaining != 0 {
		t.Fatalf("slow tracks not cleared: SlowedRemaining=%v ColdSlowedRemaining=%v", enemy.SlowedRemaining, enemy.ColdSlowedRemaining)
	}
	if !traceHas(tr, "status_removed") {
		t.Fatalf("missing status_removed trace event: %+v", tr.Events)
	}
}

func TestActionRemoveStatus_Stun_ClearsStunnedRemaining(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)
	s.ApplyStunLocked(enemy.ID, 5)
	if enemy.StunnedRemaining <= 0 {
		t.Fatalf("setup failed: stun not primed")
	}

	runOneActionProgram(t, s, caster.ID, 0, ActionRemoveStatus, `{"status":"stun"}`, []int{enemy.ID})

	if enemy.StunnedRemaining != 0 {
		t.Fatalf("enemy.StunnedRemaining = %v; want 0 after remove_status stun", enemy.StunnedRemaining)
	}
}

// ── summon_unit ──────────────────────────────────────────────────────────

func TestActionSummonUnit_SpawnsOwnedUnits(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	if _, ok := getUnitDef("skeleton_soldier"); !ok {
		t.Fatal(`getUnitDef("skeleton_soldier") = _, false; want a registered unit def (test relies on this catalog entry existing)`)
	}
	before := len(s.Units)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionSummonUnit,
		`{"unitType":"skeleton_soldier","count":2}`, nil)

	after := len(s.Units)
	if after-before != 2 {
		t.Fatalf("len(s.Units) delta = %d; want 2", after-before)
	}
	found := 0
	for _, u := range s.Units {
		if u.OwnerID == caster.OwnerID && u.UnitType == "skeleton_soldier" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("found %d skeleton_soldier units owned by %q; want 2", found, caster.OwnerID)
	}
	if !traceHas(tr, "unit_summoned") {
		t.Fatalf("missing unit_summoned trace event: %+v", tr.Events)
	}
}

// ── modify_resource ──────────────────────────────────────────────────────

func TestActionModifyResource_Mana_NegativeAmountSpendsMana(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana = 50
	caster.CurrentMana = 20

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionModifyResource,
		`{"resource":"mana","amount":-5}`, nil)

	if caster.CurrentMana != 15 {
		t.Fatalf("caster.CurrentMana = %d; want 15 after spending 5", caster.CurrentMana)
	}
	if !traceHas(tr, "resource_modified") {
		t.Fatalf("missing resource_modified trace event: %+v", tr.Events)
	}
}

func TestActionModifyResource_Mana_PositiveAmountRestoresUpToMax(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana = 50
	caster.CurrentMana = 45

	runOneActionProgram(t, s, caster.ID, 0, ActionModifyResource,
		`{"resource":"mana","amount":10}`, nil)

	if caster.CurrentMana != 50 {
		t.Fatalf("caster.CurrentMana = %d; want clamped to MaxMana 50", caster.CurrentMana)
	}
}
