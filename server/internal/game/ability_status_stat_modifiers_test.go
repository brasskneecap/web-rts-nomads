package game

import (
	"fmt"
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Status-carried stat modifiers — the THIRD PerkStatModifier emitter.
//
// Covers: (1) apply_status_duration + change_stat decoding/carrying
// PerkStatModifier entries onto the spawned AbilityStatus (the "duration is
// its own action" model — see ability_status_duration.go's file doc
// comment); (2) the program-validator's bar for a change_stat modifier,
// mirroring validatePerkStatModifier's own bar plus the AuraOnly call, AND
// the "must be nested under an apply_status_duration" placement rule; (3)
// unitStatusStatModifiersLocked, the status-sourced sibling of
// unitPerkStatModifiersLocked; (4) the two fold sites this task wires
// (effectiveArmorLocked, healUnitLocked's healingReceived multiply),
// including byte-identical regression proof for the "no status authors this
// stat" case that is true for every existing test and match today.
// ═════════════════════════════════════════════════════════════════════════════

// testStatModStatusCounter gives each spawnTestStatusWithMods call a unique
// AbilityID so two calls in the same test are treated as two INDEPENDENT
// statuses (statusStackKey) rather than the same status refreshing itself —
// spawnAbilityStatusLocked's default "refresh" stacking collapses same-key
// (AbilityID,Name) applications onto one instance, which is the correct
// production behavior but not what a "two DIFFERENT statuses composing"
// test wants.
var testStatModStatusCounter int

// spawnTestStatusWithMods spawns an AbilityStatus directly (bypassing the
// apply_status_duration/change_stat action JSON/config plumbing, mirroring
// spawnTestStatus in ability_status_test.go) carrying the given
// StatModifiers. No Triggers — exercises the pure-stat-modifier fold-site
// behavior (unitStatusStatModifiersLocked / effectiveArmorLocked /
// healUnitLocked) independent of how the status was authored.
func spawnTestStatusWithMods(s *GameState, target *Unit, remaining float64, mods []PerkStatModifier) *AbilityStatus {
	testStatModStatusCounter++
	st := &AbilityStatus{
		AbilityID:     fmt.Sprintf("test_stat_mod_status_%d", testStatModStatusCounter),
		TargetUnitID:  target.ID,
		Remaining:     remaining,
		StatModifiers: mods,
	}
	s.spawnAbilityStatusLocked(st)
	return st
}

// ── apply_status_duration + change_stat: decode / carry ─────────────────────

// TestChangeStatConfig_DecodeAndCarry drives a REAL apply_status_duration ->
// change_stat pair (the shape mark_of_weakness's catalog JSON uses) and
// proves the spawned AbilityStatus carries the resulting PerkStatModifier
// entries verbatim.
func TestChangeStatConfig_DecodeAndCarry(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	tr := runApplyStatusDurationWithChildren(t, s, caster.ID, 5, []AbilityActionDef{
		{ID: "armor", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -50})},
		{ID: "heal", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statHealingReceived, Op: statOpMultiply, Value: 0.7})},
	}, []int{enemy.ID})

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("want 1 AbilityStatus spawned, got %d", len(s.AbilityStatuses))
	}
	st := s.AbilityStatuses[0]
	if st.TargetUnitID != enemy.ID {
		t.Fatalf("status target = %d, want %d", st.TargetUnitID, enemy.ID)
	}
	if len(st.StatModifiers) != 2 {
		t.Fatalf("spawned status carries %d statModifiers, want 2: %+v", len(st.StatModifiers), st.StatModifiers)
	}
	if st.StatModifiers[0] != (PerkStatModifier{Stat: statArmor, Op: statOpAdd, Value: -50}) {
		t.Errorf("statModifiers[0] = %+v, want armor add -50", st.StatModifiers[0])
	}
	if st.StatModifiers[1] != (PerkStatModifier{Stat: statHealingReceived, Op: statOpMultiply, Value: 0.7}) {
		t.Errorf("statModifiers[1] = %+v, want healingReceived multiply 0.7", st.StatModifiers[1])
	}
	if !traceHas(tr, "status_duration_applied") {
		t.Fatalf("missing status_duration_applied trace event: %+v", tr.Events)
	}
	if !traceHas(tr, "stat_changed") {
		t.Fatalf("missing stat_changed trace event: %+v", tr.Events)
	}
}

// TestActionApplyStatus_Legacy_NoStatModifiers_StillRoutesToPrimitives is the
// existing golden-parity guard, re-asserted post-change: a legacy-shaped
// apply_status config (Triggers empty — the ONLY field apply_status
// decodes/carries now) must still take the old three-case switch,
// completely untouched by this task's removal of
// StatModifiers/Icon/IconKind from applyStatusConfig.
func TestActionApplyStatus_Legacy_NoStatModifiers_StillRoutesToPrimitives(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatus,
		`{"status":"slow","multiplier":0.5,"duration":3}`, []int{enemy.ID})

	if enemy.SlowedRemaining <= 0 {
		t.Fatalf("enemy.SlowedRemaining = %v; want > 0 (legacy slow path unaffected)", enemy.SlowedRemaining)
	}
	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("legacy apply_status (no triggers) must never spawn an AbilityStatus, got %d", len(s.AbilityStatuses))
	}
}

// ── validation ───────────────────────────────────────────────────────────

func TestChangeStat_Validate(t *testing.T) {
	d, ok := lookupActionDescriptor(ActionChangeStat)
	if !ok {
		t.Fatal("change_stat descriptor not registered")
	}

	tests := []struct {
		name    string
		sm      PerkStatModifier
		wantErr bool
	}{
		{"valid armor add", PerkStatModifier{Stat: statArmor, Op: statOpAdd, Value: -50}, false},
		{"valid healingReceived multiply", PerkStatModifier{Stat: statHealingReceived, Op: statOpMultiply, Value: 0.7}, false},
		{"unknown stat", PerkStatModifier{Stat: "not_a_real_stat", Op: statOpAdd, Value: 1}, true},
		{"bad op", PerkStatModifier{Stat: statArmor, Op: "divide", Value: 1}, true},
		{"auraOnly stat: armorPercent", PerkStatModifier{Stat: statArmorPercent, Op: statOpAdd, Value: 0.2}, true},
		{"auraOnly stat: projectileDamageReduction", PerkStatModifier{Stat: statProjectileDamageReduction, Op: statOpAdd, Value: 0.25}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := changeStatConfig{Stat: tt.sm.Stat, Op: tt.sm.Op, Value: tt.sm.Value, Stage: tt.sm.Stage}
			issues := d.Validate(cfg, ValidationScope{})
			gotErr := false
			for _, iss := range issues {
				if iss.Severity == "error" {
					gotErr = true
				}
			}
			if gotErr != tt.wantErr {
				t.Fatalf("Validate(%+v) errored=%v, want %v (issues: %+v)", tt.sm, gotErr, tt.wantErr, issues)
			}
		})
	}
}

// TestValidateProgram_ChangeStat_OutsideStatusDuration_Rejected proves
// change_stat authored anywhere OTHER than an apply_status_duration's
// config.triggers is rejected — it would bind to a nil ctx.CurrentStatus at
// runtime and silently do nothing (this project's "no inert authorable
// fields" rule).
func TestValidateProgram_ChangeStat_OutsideStatusDuration_Rejected(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "cs", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -10})},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_placement"); got == nil {
		t.Fatalf("want invalid_placement (change_stat outside apply_status_duration) at %q, got issues: %+v", wantPath, issues)
	}
}

// TestValidateProgram_ChangeStat_InsideStatusDuration_Accepted is the
// positive companion: change_stat nested inside an apply_status_duration's
// config.triggers is NOT flagged by the placement rule.
func TestValidateProgram_ChangeStat_InsideStatusDuration_Accepted(t *testing.T) {
	prog := statusDurationProgram(applyStatusDurationConfig{Duration: 5}, []AbilityActionDef{
		{ID: "cs", Type: ActionChangeStat, Config: marshalConfig(changeStatConfig{Stat: statArmor, Op: statOpAdd, Value: -10})},
	})
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "invalid_placement"); got != nil {
		t.Fatalf("change_stat nested under apply_status_duration should not be flagged, got issue: %+v", got)
	}
}

// TestValidateProgram_ApplyStatusDuration_RequiresDuration proves
// apply_status_duration's own config bar: duration <= 0 is rejected.
func TestValidateProgram_ApplyStatusDuration_RequiresDuration(t *testing.T) {
	d, ok := lookupActionDescriptor(ActionApplyStatusDuration)
	if !ok {
		t.Fatal("apply_status_duration descriptor not registered")
	}
	issues := d.Validate(applyStatusDurationConfig{Duration: 0}, ValidationScope{})
	gotErr := false
	for _, iss := range issues {
		if iss.Severity == "error" {
			gotErr = true
		}
	}
	if !gotErr {
		t.Fatalf("Duration: 0 should be rejected, got issues: %+v", issues)
	}
}

// ── unitStatusStatModifiersLocked ───────────────────────────────────────────

func TestUnitStatusStatModifiersLocked(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	const eps = 1e-9

	target := teamCombatUnit(t, s, "p2", 50, 0)

	// Nil unit -> nil.
	if got := s.unitStatusStatModifiersLocked(nil, statArmor); got != nil {
		t.Fatalf("nil unit: got %+v, want nil", got)
	}
	// Unknown stat -> nil.
	if got := s.unitStatusStatModifiersLocked(target, "not_a_real_stat"); got != nil {
		t.Fatalf("unknown stat: got %+v, want nil", got)
	}
	// No active statuses at all -> nil.
	if got := s.unitStatusStatModifiersLocked(target, statArmor); got != nil {
		t.Fatalf("no active statuses: got %+v, want nil", got)
	}

	// One active status carrying {armor, add, -50}.
	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{
		{Stat: statArmor, Op: statOpAdd, Value: -50},
	})
	got := s.unitStatusStatModifiersLocked(target, statArmor)
	base, ok := got[statStageBase]
	if !ok {
		t.Fatalf("want base stage present, got %+v", got)
	}
	if math.Abs(base.Add-(-50)) > eps || math.Abs(base.Mul-1) > eps {
		t.Fatalf("single status: base = %+v, want {Add:-50 Mul:1}", base)
	}

	// A second, independent active status also carrying an armor modifier
	// composes: adds sum, muls multiply.
	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{
		{Stat: statArmor, Op: statOpAdd, Value: -10},
		{Stat: statArmor, Op: statOpMultiply, Value: 0.5},
	})
	got = s.unitStatusStatModifiersLocked(target, statArmor)
	base = got[statStageBase]
	if math.Abs(base.Add-(-60)) > eps || math.Abs(base.Mul-0.5) > eps {
		t.Fatalf("two statuses composed: base = %+v, want {Add:-60 Mul:0.5}", base)
	}

	// A status targeting a DIFFERENT unit must not leak in.
	bystander := teamCombatUnit(t, s, "p2", 55, 0)
	spawnTestStatusWithMods(s, bystander, 5, []PerkStatModifier{{Stat: statArmor, Op: statOpAdd, Value: -999}})
	gotBystander := s.unitStatusStatModifiersLocked(bystander, statArmor)
	if math.Abs(gotBystander[statStageBase].Add-(-999)) > eps {
		t.Fatalf("bystander's own modifier missing: %+v", gotBystander)
	}
	gotTargetStillTwo := s.unitStatusStatModifiersLocked(target, statArmor)
	if math.Abs(gotTargetStillTwo[statStageBase].Add-(-60)) > eps {
		t.Fatalf("target's pool changed after a DIFFERENT unit's status was spawned: %+v", gotTargetStillTwo)
	}

	// A status targeting a different STAT is ignored when querying armor.
	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{{Stat: statHealingReceived, Op: statOpMultiply, Value: 0.7}})
	gotArmorUnchanged := s.unitStatusStatModifiersLocked(target, statArmor)
	if math.Abs(gotArmorUnchanged[statStageBase].Add-(-60)) > eps {
		t.Fatalf("unrelated stat's status leaked into armor query: %+v", gotArmorUnchanged)
	}
	gotHeal := s.unitStatusStatModifiersLocked(target, statHealingReceived)
	if math.Abs(gotHeal[statStageBase].Mul-0.7) > eps {
		t.Fatalf("healingReceived query: got %+v, want Mul 0.7", gotHeal[statStageBase])
	}

	// An EXPIRED status contributes nothing: advance past Remaining via the
	// real tick loop, then re-query.
	s.AbilityStatuses = nil // clear the fixtures above
	target.HP, target.MaxHP = 100, 100
	spawnTestStatusWithMods(s, target, 0.2, []PerkStatModifier{{Stat: statArmor, Op: statOpAdd, Value: -30}})
	if got := s.unitStatusStatModifiersLocked(target, statArmor); got == nil {
		t.Fatal("setup: status should be active and contributing before it expires")
	}
	s.tickAbilityStatusesLocked(0.5) // past the 0.2s Remaining
	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("setup: status should have expired, %d remaining", len(s.AbilityStatuses))
	}
	if got := s.unitStatusStatModifiersLocked(target, statArmor); got != nil {
		t.Fatalf("expired status still contributing: %+v", got)
	}
}

// ── effectiveArmorLocked fold site ──────────────────────────────────────────

// TestEffectiveArmorLocked_NoStatusStatModifiers_ByteIdentical is the
// regression proof: with no status authoring an "armor" StatModifiers entry
// (true for every existing test and match today), effectiveArmorLocked's
// result is unaffected by this task's change — computed twice, before and
// after spawning an UNRELATED status (one that carries no StatModifiers at
// all, the shape every pre-existing AbilityStatus takes).
func TestEffectiveArmorLocked_NoStatusStatModifiers_ByteIdentical(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	before := s.effectiveArmorLocked(target)

	// Spawn a status with no StatModifiers (e.g. a pure on_status_tick DoT) —
	// must not perturb armor at all.
	spawnTestStatus(s, caster, target, 5, 1, nil)

	after := s.effectiveArmorLocked(target)
	if before != after {
		t.Fatalf("effectiveArmorLocked changed with no status armor modifier active: before=%d after=%d", before, after)
	}
}

// TestEffectiveArmorLocked_StatusArmorModifier_Reduces drives the REAL
// effectiveArmorLocked function with a synthetic active status carrying
// {armor, add, -50} and asserts the result drops by exactly that amount
// (clamped at 0 by the function's existing floor).
func TestEffectiveArmorLocked_StatusArmorModifier_Reduces(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	target := teamCombatUnit(t, s, "p2", 50, 0)
	before := s.effectiveArmorLocked(target)

	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{
		{Stat: statArmor, Op: statOpAdd, Value: -50},
	})

	after := s.effectiveArmorLocked(target)
	want := before - 50
	if want < 0 {
		want = 0
	}
	if after != want {
		t.Fatalf("effectiveArmorLocked with active {armor,add,-50} status = %d, want %d (before=%d)", after, want, before)
	}
}

// ── healUnitLocked / healingReceived fold site ──────────────────────────────

// TestHealUnitLocked_NoActiveHealingReceivedStatus_ByteIdentical is the
// regression proof for the heal path: with no status authoring a
// "healingReceived" StatModifiers entry, healUnitLocked's result is
// unaffected by this task's change.
func TestHealUnitLocked_NoActiveHealingReceivedStatus_ByteIdentical(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.MaxHP = 500
	target.HP = 400

	s.healUnitLocked(target, 50)
	if target.HP != 450 {
		t.Fatalf("healUnitLocked(50) with no active healingReceived status: HP = %d, want 450", target.HP)
	}
}

// TestHealUnitLocked_StatusHealingReceivedModifier_Scales drives the REAL
// healUnitLocked with a synthetic active status carrying
// {healingReceived, multiply, 0.7} and asserts the incoming heal is scaled
// to 70%.
func TestHealUnitLocked_StatusHealingReceivedModifier_Scales(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.MaxHP = 500
	target.HP = 400

	spawnTestStatusWithMods(s, target, 5, []PerkStatModifier{
		{Stat: statHealingReceived, Op: statOpMultiply, Value: 0.7},
	})

	s.healUnitLocked(target, 100)
	// 100 * 0.7 = 70, rounded (healUnitLocked's own math.Round convention).
	if target.HP != 470 {
		t.Fatalf("healUnitLocked(100) with active {healingReceived,multiply,0.7} status: HP = %d, want 470", target.HP)
	}
}

// ── registry ─────────────────────────────────────────────────────────────

// TestStatRegistry_HealingReceived_Registered guards the new stat's
// registration: known, AllowMultiply (mark_of_weakness's authoring idiom),
// not AuraOnly (it has a real top-level fold site — healUnitLocked — unlike
// armorPercent/projectileDamageReduction).
func TestStatRegistry_HealingReceived_Registered(t *testing.T) {
	if !isKnownStat(statHealingReceived) {
		t.Fatal("healingReceived is not a registered stat")
	}
	d, ok := statRegistryByID[statHealingReceived]
	if !ok {
		t.Fatal("healingReceived missing from statRegistryByID")
	}
	if !d.AllowMultiply {
		t.Error("healingReceived.AllowMultiply = false, want true (mark_of_weakness authors it as a multiply)")
	}
	if d.AuraOnly {
		t.Error("healingReceived.AuraOnly = true, want false (has a real top-level fold site: healUnitLocked)")
	}
	if isAuraOnlyStat(statHealingReceived) {
		t.Error("isAuraOnlyStat(healingReceived) = true, want false")
	}
}
