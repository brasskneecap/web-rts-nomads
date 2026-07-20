package game

import (
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// "Apply Duration" — the three-moment container (On Apply / On Duration Tick /
// On Complete).
//
// Increment 1a: apply_status_duration now stores its on_status_tick /
// on_status_expire child triggers on the spawned AbilityStatus so the shared
// ticker (tickAbilityStatusesLocked) drives them, with the container's
// tickInterval as the cadence — the enabler for authoring burn (and any
// tick-driven status) as a composition instead of a hardcoded primitive.
// ═════════════════════════════════════════════════════════════════════════════

// tickStatusDurationTrigger builds an on_status_tick child trigger that deals
// `amount` fire damage to the afflicted unit each tick — the burn shape. The
// trigger carries its own timing.tickInterval (authoring metadata the
// validator requires, exactly like on_zone_tick); the container's config
// tickInterval is the actual runtime driver.
func tickStatusDurationTrigger(id string, interval float64, amount int) AbilityTriggerDef {
	trig := currentEventDamageTrigger(id, TriggerOnTick, amount)
	trig.Timing = &TriggerTiming{TickInterval: interval}
	return trig
}

// TestApplyDuration_OnTickDamagesOverDuration is the burn shape end-to-end: a
// container with an On Duration Tick trigger deals damage every tickInterval
// for the whole duration, and an On Complete trigger fires exactly once at the
// end — all through the shared ticker, no hardcoded burn system.
func TestApplyDuration_OnTickDamagesOverDuration(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.HP, target.MaxHP = 100, 100

	cfg := applyStatusDurationConfig{
		Duration:     1.5,
		TickInterval: 0.5,
		Triggers: []AbilityTriggerDef{
			tickStatusDurationTrigger("tick", 0.5, 10),
			currentEventDamageTrigger("expire", TriggerOnStatusExpire, 5),
		},
	}
	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatusDuration, string(marshalConfig(cfg)), []int{target.ID})

	// Applying the container must NOT damage the target on its own (On Apply
	// carries no damage here) and must arm exactly one ticking status.
	if target.HP != 100 {
		t.Fatalf("target.HP = %d right after apply; want 100 (no On Apply damage)", target.HP)
	}
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("want 1 ticking status armed, got %d", len(s.AbilityStatuses))
	}
	if st := s.AbilityStatuses[0]; st.TickInterval != 0.5 {
		t.Fatalf("armed status TickInterval = %v; want 0.5 (from the container)", st.TickInterval)
	}

	for i := 0; i < 15; i++ { // 15 × 0.1s = 1.5s
		s.tickAbilityStatusesLocked(0.1)
	}

	// 3 ticks × 10 = 30, then the On Complete burst of 5 → 100 - 35 = 65.
	if traceTriggerFireCount(tr, "tick") != 3 {
		t.Fatalf("On Duration Tick fired %d times; want 3", traceTriggerFireCount(tr, "tick"))
	}
	if traceTriggerFireCount(tr, "expire") != 1 {
		t.Fatalf("On Complete fired %d times; want exactly 1", traceTriggerFireCount(tr, "expire"))
	}
	if target.HP != 65 {
		t.Fatalf("target.HP = %d; want 65 (3×10 tick + 5 expire)", target.HP)
	}
	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("status should be gone after its duration; still have %d", len(s.AbilityStatuses))
	}
}

// TestApplyDuration_BurnRecipe is the whole point of increments 1a + 3: "burn"
// authored PURELY as a composition, with no hardcoded burn system involved. One
// Apply Duration container — On Apply attaches a persistent fire visual for the
// duration, On Duration Tick deals fire damage each second. This is the shape a
// designer/modder would build (and later save as a reusable template) instead
// of the Go-hardcoded BurnStacks path.
func TestApplyDuration_BurnRecipe(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.HP, target.MaxHP = 100, 100

	cfg := applyStatusDurationConfig{
		Duration:     3,
		TickInterval: 1,
		Triggers: []AbilityTriggerDef{
			// On Apply → persistent fire visual bound to the 3s duration.
			{ID: "apply", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{
				{ID: "fire", Type: ActionPlayPresentation, Config: marshalConfig(playPresentationAtPointConfig{Asset: "burning", BindToStatusDuration: true})},
			}},
			// On Duration Tick → 25 fire damage each second.
			tickStatusDurationTrigger("tick", 1, 25),
		},
	}
	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatusDuration, string(marshalConfig(cfg)), []int{target.ID})

	// The fire visual is attached to the unit for the whole duration at apply.
	hasFire := false
	for i := range s.activeEffects {
		if s.activeEffects[i].Name == "burning" && s.activeEffects[i].AnchorUnitID == target.ID {
			hasFire = true
		}
	}
	if !hasFire {
		t.Fatalf("burn recipe did not attach a fire visual to the target on apply; effects=%+v", s.activeEffects)
	}

	for i := 0; i < 30; i++ { // 30 × 0.1s = 3.0s
		s.tickAbilityStatusesLocked(0.1)
	}

	if target.HP != 25 {
		t.Fatalf("burn recipe target.HP = %d; want 25 (3 ticks × 25 fire)", target.HP)
	}
}

// TestApplyDuration_BareDealDamageTickHitsAfflictedUnit is the authoring
// convenience that makes the burn recipe "just work": an On Duration Tick whose
// deal_damage names NO target still hits the unit the status is on, because the
// tick context binds the afflicted unit as the default target set (Selected).
// No explicit select_targets{current_event} required.
func TestApplyDuration_BareDealDamageTickHitsAfflictedUnit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.HP, target.MaxHP = 100, 100

	// A bare deal_damage — no select_targets, no Input["targets"].
	bareTick := AbilityTriggerDef{
		ID: "tick", Type: TriggerOnTick, Timing: &TriggerTiming{TickInterval: 1},
		Actions: []AbilityActionDef{
			{ID: "dmg", Type: ActionDealDamage, Config: marshalConfig(dealDamageConfig{Amount: 20, Type: DamageFire})},
		},
	}
	cfg := applyStatusDurationConfig{Duration: 3, TickInterval: 1, Triggers: []AbilityTriggerDef{bareTick}}
	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatusDuration, string(marshalConfig(cfg)), []int{target.ID})

	for i := 0; i < 30; i++ { // 30 × 0.1s = 3.0s → 3 ticks
		s.tickAbilityStatusesLocked(0.1)
	}

	if target.HP != 40 {
		t.Fatalf("bare-deal_damage tick target.HP = %d; want 40 (3 ticks × 20 to the afflicted unit)", target.HP)
	}
}

// TestApplyDuration_Validate_TickInterval covers the two new validation rules
// on the container plus the refined placement rule for status-bound effects.
func TestApplyDuration_Validate_TickInterval(t *testing.T) {
	// An On Duration Tick trigger without a container tickInterval is rejected.
	noInterval := applyStatusDurationConfig{
		Duration: 3,
		Triggers: []AbilityTriggerDef{tickStatusDurationTrigger("tick", 0.5, 10)},
	}
	if got := statusDurationConfigIssues(t, noInterval); !issuesContain(got, "requires tickInterval > 0 when it has an on_status_tick") {
		t.Fatalf("container with an on_status_tick but no tickInterval should be rejected; issues=%+v", got)
	}

	// With the container tickInterval set, the same shape is valid.
	withInterval := applyStatusDurationConfig{
		Duration:     3,
		TickInterval: 0.5,
		Triggers:     []AbilityTriggerDef{tickStatusDurationTrigger("tick", 0.5, 10)},
	}
	if got := statusDurationConfigIssues(t, withInterval); hasError(got) {
		t.Fatalf("container with a tickInterval + on_status_tick should be valid; issues=%+v", got)
	}
}

// TestApplyDuration_ChangeStatOnlyValidInOnApply proves a status-bound effect
// (change_stat) is accepted in the On Apply trigger but rejected in an On
// Duration Tick trigger — where ctx.CurrentStatus is not bound and it would be
// inert.
func TestApplyDuration_ChangeStatOnlyValidInOnApply(t *testing.T) {
	changeStat := AbilityActionDef{ID: "cs", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -10})}

	// On Apply → valid.
	onApply := applyStatusDurationConfig{
		Duration: 3,
		Triggers: []AbilityTriggerDef{{ID: "apply", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{changeStat}}},
	}
	if got := statusDurationConfigIssues(t, onApply); hasError(got) {
		t.Fatalf("change_stat in the On Apply trigger should be valid; issues=%+v", got)
	}

	// On Duration Tick → rejected (inert there).
	onTick := applyStatusDurationConfig{
		Duration:     3,
		TickInterval: 0.5,
		Triggers: []AbilityTriggerDef{{
			ID: "tick", Type: TriggerOnTick, Timing: &TriggerTiming{TickInterval: 0.5},
			Actions: []AbilityActionDef{changeStat},
		}},
	}
	if got := statusDurationConfigIssues(t, onTick); !issuesContain(got, "On Apply (on_action_complete)") {
		t.Fatalf("change_stat in an On Duration Tick trigger should be rejected; issues=%+v", got)
	}
}

// statusDurationConfigIssues validates a program whose sole action is an
// apply_status_duration carrying cfg, and returns its issues.
func statusDurationConfigIssues(t *testing.T, cfg applyStatusDurationConfig) []ValidationIssue {
	t.Helper()
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "cast", Type: TriggerOnCastComplete,
		Actions: []AbilityActionDef{{ID: "dur", Type: ActionApplyStatusDuration, Config: marshalConfig(cfg)}},
	}}}
	return validateAbilityProgram(prog)
}
