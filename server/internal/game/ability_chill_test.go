package game

import (
	"strings"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// apply_status(chill|slow) — the legacy generic-slow CC primitive.
//
// The dedicated cold-slow track was retired: "chill" and "slow" are now the
// SAME generic move/attack slow (SlowedRemaining, combat_ai_cc.go), and chill's
// distinct icy tint comes from an apply_color_overlay composition, not a CC
// track. Covers: (1) chill/slow routing onto the one slow track using their own
// config duration on the standalone (legacy) path; (2) a nested apply_status
// deriving its duration from the enclosing apply_status_duration (Shatter's
// authored shape); (3) validation of the context-dependent duration rule.
// ═════════════════════════════════════════════════════════════════════════════

// TestActionApplyStatus_Chill_Standalone_SetsSlowTrack proves "chill" lands on
// the generic slow track using its own config duration on the standalone
// (legacy-compiler-shaped) path.
func TestActionApplyStatus_Chill_Standalone_SetsSlowTrack(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatus,
		`{"status":"chill","multiplier":0.5,"duration":3}`, []int{enemy.ID})

	if enemy.SlowedRemaining != 3 {
		t.Fatalf("enemy.SlowedRemaining = %v; want 3 (chill lands on the generic slow track)", enemy.SlowedRemaining)
	}
	if enemy.SlowedMultiplier != 0.5 {
		t.Fatalf("enemy.SlowedMultiplier = %v; want 0.5", enemy.SlowedMultiplier)
	}
}

// TestActionApplyStatus_Chill_NestedDerivesDurationFromContainer is the shape
// Shatter uses: an apply_status_duration owns the lifetime, and the nested
// apply_status carries NO duration of its own — it derives the container's. The
// container status is spawned; the slow rides the generic track for exactly the
// container duration.
func TestActionApplyStatus_Chill_NestedDerivesDurationFromContainer(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	runApplyStatusDurationWithChildren(t, s, caster.ID, 4, []AbilityActionDef{
		{ID: "chill", Type: ActionApplyStatus, Config: marshalConfig(applyStatusConfig{Status: "chill", Multiplier: 0.5})},
	}, []int{enemy.ID})

	if enemy.SlowedRemaining != 4 {
		t.Fatalf("enemy.SlowedRemaining = %v; want 4 (derived from the container duration)", enemy.SlowedRemaining)
	}
	if enemy.SlowedMultiplier != 0.5 {
		t.Fatalf("enemy.SlowedMultiplier = %v; want 0.5", enemy.SlowedMultiplier)
	}
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("want 1 container AbilityStatus spawned, got %d", len(s.AbilityStatuses))
	}
}

// ── validation ─────────────────────────────────────────────────────────────

// applyStatusValidationIssues runs the full program validator over a program
// whose sole meaningful action is `inner`, either nested inside an
// apply_status_duration (nested=true) or standing alone at the top level
// (nested=false), and returns just the issues anchored on that action.
func applyStatusValidationIssues(t *testing.T, inner AbilityActionDef, nested bool) []ValidationIssue {
	t.Helper()
	var actions []AbilityActionDef
	if nested {
		actions = []AbilityActionDef{{
			ID:   "dur",
			Type: ActionApplyStatusDuration,
			Config: marshalConfig(applyStatusDurationConfig{
				Duration: 3,
				Triggers: []AbilityTriggerDef{{ID: "on_apply", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{inner}}},
			}),
		}}
	} else {
		actions = []AbilityActionDef{inner}
	}
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{ID: "cast", Type: TriggerOnCastComplete, Actions: actions}}}
	return validateAbilityProgram(prog)
}

func issuesContain(issues []ValidationIssue, substr string) bool {
	for _, i := range issues {
		if strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}

func TestApplyStatus_Validate_DurationContext(t *testing.T) {
	// Nested apply_status carrying an explicit duration → rejected (the
	// container owns the lifetime; a config duration here would be inert).
	nestedWithDuration := AbilityActionDef{ID: "chill", Type: ActionApplyStatus, Config: marshalConfig(applyStatusConfig{Status: "chill", Multiplier: 0.5, Duration: 3})}
	if got := applyStatusValidationIssues(t, nestedWithDuration, true); !issuesContain(got, "derives its duration from the container") {
		t.Fatalf("nested apply_status with duration should be rejected; issues=%+v", got)
	}

	// Nested apply_status WITHOUT a duration → valid.
	nestedNoDuration := AbilityActionDef{ID: "chill", Type: ActionApplyStatus, Config: marshalConfig(applyStatusConfig{Status: "chill", Multiplier: 0.5})}
	if got := applyStatusValidationIssues(t, nestedNoDuration, true); hasError(got) {
		t.Fatalf("nested apply_status(chill) without a duration should be valid; issues=%+v", got)
	}

	// Standalone apply_status WITHOUT a duration → rejected (no container to
	// derive one from).
	standaloneNoDuration := AbilityActionDef{ID: "chill", Type: ActionApplyStatus, Config: marshalConfig(applyStatusConfig{Status: "chill", Multiplier: 0.5})}
	if got := applyStatusValidationIssues(t, standaloneNoDuration, false); !issuesContain(got, "requires duration > 0") {
		t.Fatalf("standalone apply_status without a duration should be rejected; issues=%+v", got)
	}

	// Standalone apply_status WITH a duration → valid (the legacy shape).
	standaloneWithDuration := AbilityActionDef{ID: "slow", Type: ActionApplyStatus, Config: marshalConfig(applyStatusConfig{Status: "slow", Multiplier: 0.5, Duration: 3})}
	if got := applyStatusValidationIssues(t, standaloneWithDuration, false); hasError(got) {
		t.Fatalf("standalone apply_status(slow) with a duration should be valid; issues=%+v", got)
	}
}
