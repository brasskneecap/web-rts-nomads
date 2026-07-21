package game

import (
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// apply_color_overlay — the status-authored full-body tint (the general form of
// the chill/blue overlay). Sibling of apply_mark: On-Apply-only, writes onto
// ctx.CurrentStatus, serialized to the client via unitOverlayColorLocked.
// ═════════════════════════════════════════════════════════════════════════════

// TestApplyColorOverlay_SetsStatusColorAndSerializes proves a bound
// apply_color_overlay in On Apply tints the afflicted unit's status and that the
// color reaches the wire via the snapshot helper.
func TestApplyColorOverlay_SetsStatusColorAndSerializes(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	runApplyStatusDurationWithChildren(t, s, caster.ID, 5, []AbilityActionDef{
		{ID: "tint", Type: ActionApplyColorOverlay, Config: marshalConfig(applyColorOverlayConfig{Color: "#33cc55"})},
	}, []int{target.ID})

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("want 1 status spawned, got %d", len(s.AbilityStatuses))
	}
	if got := s.AbilityStatuses[0].OverlayColor; got != "#33cc55" {
		t.Fatalf("status OverlayColor = %q; want #33cc55", got)
	}
	if got := s.unitOverlayColorLocked(target); got != "#33cc55" {
		t.Fatalf("unitOverlayColorLocked = %q; want #33cc55 (the wire value)", got)
	}
	// A unit with no color-overlay status and no cold slow resolves to "".
	if got := s.unitOverlayColorLocked(caster); got != "" {
		t.Fatalf("caster has no overlay; got %q, want \"\"", got)
	}
}

// TestOverlayColor_StatusPrecedence proves the one-place tint decision: with no
// authored overlay a unit has no tint (the cold-slow track and its default-blue
// fallback were retired), and an authored apply_color_overlay status paints the
// unit its chosen color.
func TestOverlayColor_StatusPrecedence(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	u := teamCombatUnit(t, s, "p2", 50, 0)

	// No overlay status → no tint (a plain slow no longer tints anything).
	s.ApplySlowLocked(u.ID, 0.5, 3)
	if got := s.unitOverlayColorLocked(u); got != "" {
		t.Fatalf("slowed-but-unauthored unit overlay = %q; want \"\" (no cold-track fallback)", got)
	}

	// An authored status overlay paints the unit.
	spawnTestStatusWithMods(s, u, 3, nil)
	s.AbilityStatuses[len(s.AbilityStatuses)-1].OverlayColor = "#33cc55"
	if got := s.unitOverlayColorLocked(u); got != "#33cc55" {
		t.Fatalf("overlay with authored status = %q; want #33cc55", got)
	}
}

// TestApplyColorOverlay_Validate covers the placement + color-format rules.
func TestApplyColorOverlay_Validate(t *testing.T) {
	tint := func(color string) AbilityActionDef {
		return AbilityActionDef{ID: "tint", Type: ActionApplyColorOverlay, Config: marshalConfig(applyColorOverlayConfig{Color: color})}
	}

	// On Apply + valid hex → clean.
	onApply := applyStatusDurationConfig{
		Duration: 5,
		Triggers: []AbilityTriggerDef{{ID: "apply", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{tint("#96d6ff")}}},
	}
	if got := statusDurationConfigIssues(t, onApply); hasError(got) {
		t.Fatalf("apply_color_overlay in On Apply with a valid color should be clean; issues=%+v", got)
	}

	// Bad color → rejected.
	badColor := applyStatusDurationConfig{
		Duration: 5,
		Triggers: []AbilityTriggerDef{{ID: "apply", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{tint("blue")}}},
	}
	if got := statusDurationConfigIssues(t, badColor); !issuesContain(got, "must be a hex value") {
		t.Fatalf("apply_color_overlay with a non-hex color should be rejected; issues=%+v", got)
	}

	// Top level (no container) → rejected (inert there, like change_stat/apply_mark).
	prog := &AbilityProgram{Triggers: []AbilityTriggerDef{{
		ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{tint("#96d6ff")},
	}}}
	if got := validateAbilityProgram(prog); !issuesContain(got, "must live in an apply_status_duration's On Apply") {
		t.Fatalf("apply_color_overlay at top level should be rejected; issues=%+v", got)
	}
}
