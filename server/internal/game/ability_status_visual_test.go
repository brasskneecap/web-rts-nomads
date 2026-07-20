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
