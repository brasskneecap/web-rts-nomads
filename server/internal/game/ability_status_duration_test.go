package game

import "testing"

// ═════════════════════════════════════════════════════════════════════════════
// apply_status_duration — the container action itself.
//
// Covers: (1) multi-target correctness — each target gets its OWN
// AbilityStatus with its own change_stat/apply_mark writes, never
// cross-contaminated via ctx.CurrentStatus; (2) refresh semantics at the raw
// action level (not just mark_of_weakness's end-to-end test) — a second
// apply_status_duration application while one is still live extends
// Remaining WITHOUT re-appending StatModifiers (not doubled); (3) expiry
// reverting the nested effects via the real tickAbilityStatusesLocked loop;
// (4) change_stat/apply_mark's defensive no-op when authored/run with no
// bound ctx.CurrentStatus (reachable only by bypassing validation).
// ═════════════════════════════════════════════════════════════════════════════

func TestApplyStatusDuration_MultiTarget_EachGetsOwnStatusAndModifiers(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemyA := teamCombatUnit(t, s, "p2", 50, 0)
	enemyB := teamCombatUnit(t, s, "p2", 100, 0)

	runApplyStatusDurationWithChildren(t, s, caster.ID, 5, []AbilityActionDef{
		{ID: "armor", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -50})},
	}, []int{enemyA.ID, enemyB.ID})

	if len(s.AbilityStatuses) != 2 {
		t.Fatalf("want 2 independent AbilityStatuses (one per target), got %d", len(s.AbilityStatuses))
	}
	for _, st := range s.AbilityStatuses {
		if len(st.StatModifiers) != 1 {
			t.Fatalf("status for target %d carries %d statModifiers, want exactly 1 (no cross-target leakage): %+v", st.TargetUnitID, len(st.StatModifiers), st.StatModifiers)
		}
		if st.StatModifiers[0] != (PerkStatModifier{Stat: statArmor, Op: statOpAdd, Value: -50}) {
			t.Errorf("target %d statModifiers[0] = %+v, want armor add -50", st.TargetUnitID, st.StatModifiers[0])
		}
	}
	gotTargets := map[int]bool{s.AbilityStatuses[0].TargetUnitID: true, s.AbilityStatuses[1].TargetUnitID: true}
	if !gotTargets[enemyA.ID] || !gotTargets[enemyB.ID] {
		t.Fatalf("expected one status per target %d and %d, got targets %v", enemyA.ID, enemyB.ID, gotTargets)
	}
}

// TestApplyStatusDuration_Refresh_DoesNotDoubleStatModifiers proves the
// reference-semantics argument in RuntimeAbilityContext.CurrentStatus's doc
// comment directly at the action level: a second application while the
// first is still live REFRESHES (extends Remaining) instead of appending a
// second, independent StatModifiers entry onto the same live status.
func TestApplyStatusDuration_Refresh_DoesNotDoubleStatModifiers(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	children := []AbilityActionDef{
		{ID: "armor", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -50})},
	}
	runApplyStatusDurationWithChildren(t, s, caster.ID, 6, children, []int{enemy.ID})
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("after first application: want 1 AbilityStatus, got %d", len(s.AbilityStatuses))
	}
	s.AbilityStatuses[0].Remaining -= 1 // let some time pass, not enough to expire

	runApplyStatusDurationWithChildren(t, s, caster.ID, 6, children, []int{enemy.ID})

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("refresh must keep exactly 1 AbilityStatus, got %d", len(s.AbilityStatuses))
	}
	if len(s.AbilityStatuses[0].StatModifiers) != 1 {
		t.Fatalf("refresh must NOT re-append statModifiers onto the still-live status (would double the armor reduction): got %d entries: %+v",
			len(s.AbilityStatuses[0].StatModifiers), s.AbilityStatuses[0].StatModifiers)
	}
	if s.AbilityStatuses[0].Remaining < 6 {
		t.Errorf("refresh should reset Remaining back to the full duration, got %v", s.AbilityStatuses[0].Remaining)
	}
}

// TestApplyStatusDuration_Expiry_RevertsNestedChangeStatAndApplyMark drives
// the real tickAbilityStatusesLocked loop and proves both nested effects
// (change_stat's armor modifier, apply_mark's overhead icon) disappear once
// the container status expires — they were never independent state, just
// writes onto the SAME AbilityStatus object the container ticks down.
func TestApplyStatusDuration_Expiry_RevertsNestedChangeStatAndApplyMark(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	runApplyStatusDurationWithChildren(t, s, caster.ID, 2, []AbilityActionDef{
		{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(applyMarkConfig{Icon: "debuff-weakened", IconKind: "debuff"})},
		{ID: "armor", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -50})},
	}, []int{enemy.ID})

	armorBefore := s.effectiveArmorLocked(enemy)
	if got := iconIDs(s.activeDebuffIconsLocked(enemy)); len(got) != 1 || got[0] != "debuff-weakened" {
		t.Fatalf("setup: expected the icon right after applying, got %v", got)
	}

	s.tickAbilityStatusesLocked(2.1) // past the 2s duration

	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("status should have expired and been removed, %d remaining", len(s.AbilityStatuses))
	}
	if got := s.effectiveArmorLocked(enemy); got == armorBefore {
		t.Errorf("armor should be back above the debuffed value after expiry: still %d", got)
	}
	if got := s.activeDebuffIconsLocked(enemy); got != nil {
		t.Errorf("debuff icon should be gone after expiry, got %v", got)
	}
}

// ── defensive no-op: no bound ctx.CurrentStatus ─────────────────────────────

// TestChangeStat_Execute_NoCurrentStatus_IsNoop proves change_stat run
// without a bound ctx.CurrentStatus (reachable only by bypassing
// validation, e.g. a hand-built RuntimeAbilityContext) traces a skip instead
// of panicking or writing anywhere.
func TestChangeStat_Execute_NoCurrentStatus_IsNoop(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionChangeStat,
		`{"stat":"armor","op":"add","value":-50}`, nil)

	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("change_stat with no current status must not spawn/mutate any AbilityStatus, got %d", len(s.AbilityStatuses))
	}
	if !traceHas(tr, "action_skipped") {
		t.Fatalf("missing action_skipped trace event: %+v", tr.Events)
	}
}

// TestApplyMark_Execute_NoCurrentStatus_IsNoop is apply_mark's identical
// defensive-no-op guard.
func TestApplyMark_Execute_NoCurrentStatus_IsNoop(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	tr := runOneActionProgram(t, s, caster.ID, 0, ActionApplyMark,
		`{"icon":"debuff-weakened","iconKind":"debuff"}`, nil)

	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("apply_mark with no current status must not spawn/mutate any AbilityStatus, got %d", len(s.AbilityStatuses))
	}
	if !traceHas(tr, "action_skipped") {
		t.Fatalf("missing action_skipped trace event: %+v", tr.Events)
	}
}

// TestApplyStatusDuration_DeadTarget_SkippedNotSpawned proves a dead/missing
// target in the resolved set is skipped (mirrors apply_status's authored-path
// guard) rather than spawning a status for it.
func TestApplyStatusDuration_DeadTarget_SkippedNotSpawned(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)
	enemy.HP = 0

	runApplyStatusDurationWithChildren(t, s, caster.ID, 5, []AbilityActionDef{
		{ID: "armor", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -50})},
	}, []int{enemy.ID})

	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("dead target must not get a spawned AbilityStatus, got %d", len(s.AbilityStatuses))
	}
}
