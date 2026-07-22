package game

import (
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Status-bound visual — play_presentation(bindToStatusDuration).
//
// Increment 3: the persistent-visual half of a data-authored status. Authored
// in an apply_status_duration's On Apply trigger, a bound play_presentation
// attaches its effect to the afflicted unit for the status's whole duration
// (burn's fire overlay, etc.) — the sibling of change_stat/apply_mark, and the
// last piece burn needs before it can retire the hardcoded burn system.
// ═════════════════════════════════════════════════════════════════════════════

// TestStatusVisual_BindsEffectToUnitForDuration proves a bound play_presentation
// in On Apply attaches a unit-anchored effect lasting exactly the status
// duration.
func TestStatusVisual_BindsEffectToUnitForDuration(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	const dur = 5.0
	runApplyStatusDurationWithChildren(t, s, caster.ID, dur, []AbilityActionDef{
		{ID: "fire", Type: ActionPlayPresentation, Config: marshalConfig(playPresentationAtPointConfig{Asset: "burning", BindToStatusDuration: true})},
	}, []int{target.ID})

	var bound *effectInstance
	for i := range s.activeEffects {
		if s.activeEffects[i].Name == "burning" && s.activeEffects[i].AnchorUnitID == target.ID {
			bound = &s.activeEffects[i]
			break
		}
	}
	if bound == nil {
		t.Fatalf("no burning effect anchored to the afflicted unit %d; effects=%+v", target.ID, s.activeEffects)
	}
	wantTicks := int(math.Round(dur * gameTicksPerSecond))
	if bound.DurationTicks != wantTicks {
		t.Fatalf("bound effect DurationTicks = %d; want %d (the status's %vs duration)", bound.DurationTicks, wantTicks, dur)
	}
}

// TestStatusVisual_Validate_Placement proves bindToStatusDuration is accepted in
// On Apply but rejected anywhere ctx.CurrentStatus is not bound (top level, and
// an On Duration Tick trigger).
func TestStatusVisual_Validate_Placement(t *testing.T) {
	boundPres := AbilityActionDef{ID: "fire", Type: ActionPlayPresentation, Config: marshalConfig(playPresentationAtPointConfig{Asset: "burning", BindToStatusDuration: true})}

	// On Apply → valid.
	onApply := applyStatusDurationConfig{
		Duration: 5,
		Triggers: []AbilityTriggerDef{{ID: "apply", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{boundPres}}},
	}
	if got := statusDurationConfigIssues(t, onApply); hasError(got) {
		t.Fatalf("bound play_presentation in On Apply should be valid; issues=%+v", got)
	}

	// On Duration Tick → rejected (no CurrentStatus bound there).
	onTick := applyStatusDurationConfig{
		Duration:     5,
		TickInterval: 1,
		Triggers: []AbilityTriggerDef{{
			ID: "tick", Type: TriggerOnTick, Timing: &TriggerTiming{TickInterval: 1},
			Actions: []AbilityActionDef{boundPres},
		}},
	}
	if got := statusDurationConfigIssues(t, onTick); !issuesContain(got, "On Apply (on_action_complete)") {
		t.Fatalf("bound play_presentation in an On Duration Tick trigger should be rejected; issues=%+v", got)
	}

	// Top level (no container at all) → rejected.
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{boundPres},
	}}}
	if got := validateAbilityProgram(prog); !issuesContain(got, "only valid in an apply_status_duration") {
		t.Fatalf("bound play_presentation at top level should be rejected; issues=%+v", got)
	}
}

// TestStatusVisual_DropsWhenTheAfflictedUnitDies is the regression for a burning
// corpse: the flame is the visual of a STATUS, and once its host is dead the
// status does not exist, but tickEffectsLocked deliberately keeps a unit-
// anchored effect alive at the death position so a one-shot impact can finish.
// That default is right for a burst and wrong for a state, so the status-bound
// path opts into RequiresLiveAnchor.
func TestStatusVisual_DropsWhenTheAfflictedUnitDies(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	runApplyStatusDurationWithChildren(t, s, caster.ID, 8.0, []AbilityActionDef{
		{ID: "fire", Type: ActionPlayPresentation, Config: marshalConfig(playPresentationAtPointConfig{Asset: "burning", BindToStatusDuration: true})},
	}, []int{target.ID})

	burning := func() int {
		n := 0
		for i := range s.activeEffects {
			if s.activeEffects[i].Name == "burning" && s.activeEffects[i].AnchorUnitID == target.ID {
				n++
			}
		}
		return n
	}
	if burning() != 1 {
		t.Fatalf("burning effects on the target = %d, want 1 before it dies", burning())
	}

	// Kill it. The unit is still in unitsByID at this point (deaths drain later
	// in the Update pass), which is exactly the window the bug lived in.
	target.HP = 0
	s.Tick++
	s.tickEffectsLocked()

	if got := burning(); got != 0 {
		t.Errorf("burning effects on the dead target = %d, want 0 — the flame outlived its host", got)
	}
}

// The complement: a plain unit-anchored one-shot must STILL finish where the
// unit died. Dropping every anchored effect on death would have been the easy
// fix and would have cut impact sparks off mid-animation.
func TestUnitEffect_OneShotStillFinishesAtTheDeathPosition(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	target := teamCombatUnit(t, s, "p2", 50, 0)
	if !s.playEffectOnUnitLocked(target, "burning") {
		t.Fatal("playEffectOnUnitLocked failed")
	}

	target.HP = 0
	s.Tick++
	s.tickEffectsLocked()

	found := false
	for i := range s.activeEffects {
		if s.activeEffects[i].AnchorUnitID == target.ID {
			found = true
		}
	}
	if !found {
		t.Error("an unbound one-shot effect was dropped when its anchor died; it should play out at the death position")
	}
}

// unitIsAliveLocked is the single definition every attached-to-a-unit system
// asks. It is pinned here because it is about to matter more than it does now:
// when a dying unit stops leaving the field immediately, "the body is still
// resolvable" must not read as "still alive", and the fix belongs in this one
// function rather than at each call site.
func TestUnitIsAliveLocked(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	u := teamCombatUnit(t, s, "p2", 50, 0)

	if !s.unitIsAliveLocked(u) {
		t.Error("a healthy unit is not alive")
	}
	if s.unitIsAliveLocked(nil) {
		t.Error("a nil unit is alive")
	}

	u.HP = 0
	if s.unitIsAliveLocked(u) {
		t.Error("a unit at 0 HP is alive")
	}

	// The pendingDeaths window: lethal damage taken earlier in this Update pass
	// leaves the unit in the registry until drainPendingDeathsLocked runs. Even
	// re-healed mid-window it is on its way out, so nothing new should attach.
	s.enqueueDeathLocked(u, DamageSource{})
	u.HP = 10
	if s.unitIsAliveLocked(u) {
		t.Error("a unit already queued for death this tick is alive")
	}
}
