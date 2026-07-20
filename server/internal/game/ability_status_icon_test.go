package game

import (
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// apply_mark (icon + iconKind) — data-driven HUD icon selection for a status
// spawned by an enclosing apply_status_duration.
//
// Covers: (1) apply_mark decoding/writing Icon+IconKind onto ctx.CurrentStatus
// (the enclosing apply_status_duration's spawned AbilityStatus); (2) the
// program-validator's Icon/IconKind pairing rule AND the "must be nested
// under an apply_status_duration" placement rule; (3) generic emission into
// activeBuffIconsLocked/activeDebuffIconsLocked, replacing the hand-wired
// mark_of_weakness case that used to live in perks_icons.go; (4)
// determinism/dedupe across multiple statuses sharing an icon id; (5) the
// mark_of_weakness end-to-end regression proving its debuff icon still
// renders after the hand-wired Go was deleted.
// ═════════════════════════════════════════════════════════════════════════════

// runApplyStatusDurationWithChildren wraps an apply_status_duration action
// whose config.triggers contains a single on_action_complete trigger running
// childActions, and runs it via runOneActionProgram. Mirrors mark_of_weakness's
// real authoring shape (apply_status_duration -> on_action_complete ->
// [apply_mark, change_stat, ...]) without hand-building an AbilityProgram
// per test.
func runApplyStatusDurationWithChildren(t *testing.T, s *GameState, casterID int, duration float64, childActions []AbilityActionDef, targets []int) *AbilityExecutionTrace {
	t.Helper()
	cfg := applyStatusDurationConfig{
		Duration: duration,
		Triggers: []AbilityTriggerDef{
			{ID: "on_apply", Type: TriggerOnActionComplete, Actions: childActions},
		},
	}
	return runOneActionProgram(t, s, casterID, 0, ActionApplyStatusDuration, string(marshalConfig(cfg)), targets)
}

// statusDurationProgram builds a single-trigger AbilityProgram whose
// on_cast_complete action is an apply_status_duration carrying cfg.Triggers
// = [{on_action_complete, childActions}] — the shape validateAbilityProgram
// needs to exercise walkAction's insideStatusDuration propagation into
// change_stat/apply_mark's placement check. cfg.Triggers is overwritten
// (any value the caller set is replaced) so callers only need to supply
// Duration/Stacking/MaxStacks/Name.
func statusDurationProgram(cfg applyStatusDurationConfig, childActions []AbilityActionDef) *AbilityProgram {
	cfg.Triggers = []AbilityTriggerDef{
		{ID: "on_apply", Type: TriggerOnActionComplete, Actions: childActions},
	}
	return &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "status", Type: ActionApplyStatusDuration, Config: marshalConfig(cfg)},
			}},
		},
	}
}

// ── decode / carry ───────────────────────────────────────────────────────────

// TestApplyMarkConfig_DecodeAndCarry drives a REAL apply_status_duration ->
// apply_mark pair and proves the spawned AbilityStatus carries Icon/IconKind
// verbatim.
func TestApplyMarkConfig_DecodeAndCarry(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	markCfg := applyMarkConfig{Icon: "debuff-weakened", IconKind: "debuff"}
	runApplyStatusDurationWithChildren(t, s, caster.ID, 5, []AbilityActionDef{
		{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(markCfg)},
	}, []int{enemy.ID})

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("want 1 AbilityStatus spawned, got %d", len(s.AbilityStatuses))
	}
	st := s.AbilityStatuses[0]
	if st.Icon != "debuff-weakened" {
		t.Errorf("st.Icon = %q, want %q", st.Icon, "debuff-weakened")
	}
	if st.IconKind != "debuff" {
		t.Errorf("st.IconKind = %q, want %q", st.IconKind, "debuff")
	}
}

// ── validation ───────────────────────────────────────────────────────────────

func TestValidateProgram_ApplyMark_IconWithoutIconKindRejected(t *testing.T) {
	prog := statusDurationProgram(applyStatusDurationConfig{Duration: 5}, []AbilityActionDef{
		{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(applyMarkConfig{Icon: "debuff-weakened"})},
	})
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_property"); got == nil {
		t.Fatalf("want invalid_property (icon without valid iconKind) at %q, got issues: %+v", wantPath, issues)
	}
}

func TestValidateProgram_ApplyMark_IconWithInvalidIconKindRejected(t *testing.T) {
	prog := statusDurationProgram(applyStatusDurationConfig{Duration: 5}, []AbilityActionDef{
		{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(applyMarkConfig{Icon: "debuff-weakened", IconKind: "not_a_kind"})},
	})
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_property"); got == nil {
		t.Fatalf("want invalid_property (invalid iconKind) at %q, got issues: %+v", wantPath, issues)
	}
}

func TestValidateProgram_ApplyMark_ValidBuffIconAccepted(t *testing.T) {
	prog := statusDurationProgram(applyStatusDurationConfig{Duration: 5}, []AbilityActionDef{
		{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(applyMarkConfig{Icon: "buff-example", IconKind: "buff"})},
	})
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_property"); got != nil {
		t.Fatalf("valid buff icon should not be flagged, got issue: %+v", got)
	}
}

func TestValidateProgram_ApplyMark_ValidDebuffIconAccepted(t *testing.T) {
	prog := statusDurationProgram(applyStatusDurationConfig{Duration: 5}, []AbilityActionDef{
		{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(applyMarkConfig{Icon: "debuff-weakened", IconKind: "debuff"})},
	})
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_property"); got != nil {
		t.Fatalf("valid debuff icon should not be flagged, got issue: %+v", got)
	}
}

// TestValidateProgram_ApplyMark_OutsideStatusDuration_Rejected proves apply_mark
// authored anywhere OTHER than an apply_status_duration's config.triggers is
// rejected — it would bind to a nil ctx.CurrentStatus at runtime and silently
// do nothing.
func TestValidateProgram_ApplyMark_OutsideStatusDuration_Rejected(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "mark", Type: ActionApplyMark, Config: marshalConfig(applyMarkConfig{Icon: "debuff-weakened", IconKind: "debuff"})},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_placement"); got == nil {
		t.Fatalf("want invalid_placement (apply_mark outside apply_status_duration) at %q, got issues: %+v", wantPath, issues)
	}
}

// ── generic emission: buff channel ──────────────────────────────────────────

func TestActiveBuffIcons_AuthoredStatus_BuffKindAppearsInBuffList(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	spawnAbilityStatusIconOnly(s, unit, "buff-example", "buff")

	buffs := iconIDs(s.activeBuffIconsLocked(unit))
	if len(buffs) != 1 || buffs[0] != "buff-example" {
		t.Errorf("activeBuffIconsLocked = %v, want [buff-example]", buffs)
	}
	debuffs := iconIDs(s.activeDebuffIconsLocked(unit))
	if len(debuffs) != 0 {
		t.Errorf("activeDebuffIconsLocked = %v, want empty (buff-kind status must not leak into the debuff channel)", debuffs)
	}
}

func TestActiveDebuffIcons_AuthoredStatus_DebuffKindAppearsInDebuffList(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	spawnAbilityStatusIconOnly(s, unit, "debuff-example", "debuff")

	debuffs := iconIDs(s.activeDebuffIconsLocked(unit))
	if len(debuffs) != 1 || debuffs[0] != "debuff-example" {
		t.Errorf("activeDebuffIconsLocked = %v, want [debuff-example]", debuffs)
	}
	buffs := iconIDs(s.activeBuffIconsLocked(unit))
	if len(buffs) != 0 {
		t.Errorf("activeBuffIconsLocked = %v, want empty (debuff-kind status must not leak into the buff channel)", buffs)
	}
}

// TestActiveIcons_AuthoredStatus_NoIcon_NoOverheadIcon is the regression /
// byte-identical guard: a status with an empty Icon (every status spawned
// before this field existed, and every status that only carries
// Triggers/StatModifiers today) contributes nothing to either channel.
func TestActiveIcons_AuthoredStatus_NoIcon_NoOverheadIcon(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	spawnTestStatusWithMods(s, unit, 5, []PerkStatModifier{{Stat: statArmor, Op: statOpAdd, Value: -10}})

	if got := s.activeBuffIconsLocked(unit); got != nil {
		t.Errorf("activeBuffIconsLocked = %v, want nil", got)
	}
	if got := s.activeDebuffIconsLocked(unit); got != nil {
		t.Errorf("activeDebuffIconsLocked = %v, want nil", got)
	}
}

// TestActiveIcons_AuthoredStatus_DeterminismAndDedupe proves that two
// independent "stack"-mode statuses sharing the same icon id merge into ONE
// icon entry with Stacks==2 via addIcon's existing dedupe-and-sum semantics
// (see activeDebuffIconsLocked's trailing loop doc comment) — no bespoke
// stack-counting logic needed for the generic path.
func TestActiveIcons_AuthoredStatus_DeterminismAndDedupe(t *testing.T) {
	s, unit := newDebuffIconState(t)
	defer s.mu.Unlock()

	// Two DIFFERENT AbilityIDs so spawnAbilityStatusLocked treats them as
	// independent instances rather than refreshing a single one (statusStackKey
	// keys on AbilityID+Name).
	s.AbilityStatuses = append(s.AbilityStatuses,
		&AbilityStatus{ID: "s1", AbilityID: "test_dedupe_a", TargetUnitID: unit.ID, Remaining: 5, Icon: "debuff-shared", IconKind: "debuff"},
		&AbilityStatus{ID: "s2", AbilityID: "test_dedupe_b", TargetUnitID: unit.ID, Remaining: 5, Icon: "debuff-shared", IconKind: "debuff"},
	)

	got := s.activeDebuffIconsLocked(unit)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 deduped icon entry, got %d: %+v", len(got), got)
	}
	if got[0].ID != "debuff-shared" || got[0].Stacks != 2 {
		t.Errorf("got %+v, want {ID:debuff-shared Stacks:2}", got[0])
	}

	// Determinism: repeated calls produce byte-identical output (stable
	// append order, no map iteration involved).
	again := s.activeDebuffIconsLocked(unit)
	if len(again) != 1 || again[0] != got[0] {
		t.Errorf("repeated call diverged: first=%+v second=%+v", got[0], again[0])
	}
}

// spawnAbilityStatusIconOnly spawns a minimal AbilityStatus carrying only an
// Icon/IconKind pair (no Triggers, no StatModifiers) directly via
// spawnAbilityStatusLocked, mirroring spawnTestStatus/spawnTestStatusWithMods.
// Caller holds s.mu.
func spawnAbilityStatusIconOnly(s *GameState, target *Unit, icon, iconKind string) *AbilityStatus {
	testStatModStatusCounter++
	st := &AbilityStatus{
		AbilityID:    "test_icon_only_status",
		Name:         "n",
		TargetUnitID: target.ID,
		Remaining:    5,
		Icon:         icon,
		IconKind:     iconKind,
	}
	s.spawnAbilityStatusLocked(st)
	return st
}

// ── mark_of_weakness end-to-end ─────────────────────────────────────────────

// TestMarkOfWeakness_OverheadIcon_ViaGenericEmission drives a REAL cast of
// the granted mark_of_weakness ability and proves its overhead debuff icon
// still appears via the GENERIC apply_status Icon/IconKind path now that the
// hand-wired case in activeDebuffIconsLocked has been deleted — the ability's
// own JSON now authors icon:"debuff-mark-of-weakness"/iconKind:"debuff"
// directly.
func TestMarkOfWeakness_OverheadIcon_ViaGenericEmission(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)

	if got := s.activeDebuffIconsLocked(anchor); got != nil {
		t.Fatalf("setup: unmarked unit should have no debuff icons yet, got %v", got)
	}

	castMarkOfWeakness(t, s, siphoner, anchor)

	got := iconIDs(s.activeDebuffIconsLocked(anchor))
	if len(got) != 1 || got[0] != "debuff-mark-of-weakness" {
		t.Errorf("activeDebuffIconsLocked(marked unit) = %v, want [debuff-mark-of-weakness]", got)
	}
	// Must not leak into the buff channel.
	if buffs := s.activeBuffIconsLocked(anchor); len(buffs) != 0 {
		t.Errorf("activeBuffIconsLocked(marked unit) = %v, want empty", buffs)
	}
}

// TestMarkOfWeakness_OverheadIcon_ClearsOnExpiry proves the icon disappears
// once the AbilityStatus expires — the generic loop reads s.AbilityStatuses
// live every call, so there's no separate icon-clearing step needed.
func TestMarkOfWeakness_OverheadIcon_ClearsOnExpiry(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantMarkOfWeaknessAbility(s, siphoner)
	cfg := markOfWeaknessCfg(t)
	duration := cfg["durationSeconds"]

	castMarkOfWeakness(t, s, siphoner, anchor)
	if got := iconIDs(s.activeDebuffIconsLocked(anchor)); len(got) != 1 {
		t.Fatalf("setup: expected the icon right after casting, got %v", got)
	}

	s.tickAbilityStatusesLocked(duration + 0.1)

	if got := s.activeDebuffIconsLocked(anchor); got != nil {
		t.Errorf("activeDebuffIconsLocked after expiry = %v, want nil", got)
	}
}
