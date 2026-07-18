package game

import "testing"

// ═════════════════════════════════════════════════════════════════════════════
// on_cast_start wiring tests.
//
// on_cast_start now fires from all three cast-begin entry points
// (beginAbilityCastLocked, beginAbilityCastAtPointLocked,
// beginAbilityChannelLocked) — see fireCastStartTriggerLocked (ability_cast.go)
// for the placement rationale and the documented unpaired-on-interrupt
// hazard. These tests exercise the ordering guarantees and prove production
// (legacy-compiled) abilities never emit this trigger.
//
// Every test uses an empty-actions on_cast_start trigger: runProgramTriggersLocked
// records a "trigger_fired" trace event the instant a trigger's conditions
// pass, BEFORE its actions (if any) run — so an empty action list is a valid,
// minimal probe for "did this trigger fire" and carries no gameplay side
// effects to confound assertions made about the surrounding cast.
// ═════════════════════════════════════════════════════════════════════════════

// traceFireIndices returns the index (within tr.Events) of every
// "trigger_fired" event whose Path equals triggerID, in recorded order.
func traceFireIndices(tr *AbilityExecutionTrace, triggerID string) []int {
	var out []int
	for i, e := range tr.Events {
		if e.Type == "trigger_fired" && e.Path == triggerID {
			out = append(out, i)
		}
	}
	return out
}

// ── unit-target path, non-instant cast ──────────────────────────────────────

// unitCastStartTestDef builds a schemaVersion 2, unit-targeted, enemy-castable
// ability with an empty-actions on_cast_start trigger ("start") and an
// empty-actions on_cast_complete trigger ("complete"). castTime is the
// authored cast time; manaCost is 0 so mana never blocks the gates under test.
func unitCastStartTestDef(id string, castTime float64) AbilityDef {
	return AbilityDef{
		ID:               id,
		Type:             AbilitySpell,
		SchemaVersion:    2,
		CanTargetEnemies: true,
		CastRange:        CastRange(300),
		CastTime:         castTime,
		Program: &AbilityProgram{
			Entry: AbilityEntryDef{Type: EntryUnit, Range: CastRange(300), Relations: []TargetRelation{RelEnemy}},
			Triggers: []AbilityTriggerDef{
				{ID: "start", Type: TriggerOnCastStart, Actions: []AbilityActionDef{}},
				{ID: "complete", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{}},
			},
		},
	}
}

// TestOnCastStart_UnitTarget_FiresOnceAtBeginBeforeComplete proves on_cast_start
// fires exactly once at the moment a non-instant unit-targeted cast begins,
// strictly before on_cast_complete (which has not fired at all yet — the cast
// timer hasn't elapsed).
func TestOnCastStart_UnitTarget_FiresOnceAtBeginBeforeComplete(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	def := unitCastStartTestDef("cast_start_unit_test", 1.0)
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, def.ID)

	target := teamCombatUnit(t, s, "p2", 100, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	ok, reason := s.beginAbilityCastLocked(caster, def.ID, target)
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	starts := traceFireIndices(tr, "start")
	completes := traceFireIndices(tr, "complete")
	if len(starts) != 1 {
		t.Fatalf("on_cast_start fired %d times at begin, want exactly 1", len(starts))
	}
	if len(completes) != 0 {
		t.Fatalf("on_cast_complete fired %d times before the cast timer elapsed, want 0", len(completes))
	}
	if caster.CastAbilityID != def.ID {
		t.Fatalf("caster.CastAbilityID = %q, want %q (cast should still be in progress)", caster.CastAbilityID, def.ID)
	}
}

// TestOnCastStart_UnitTarget_CompleteFiresAfterCastTimeElapses continues the
// above scenario forward: once the cast timer elapses, on_cast_complete fires
// exactly once, on_cast_start still exactly once, and start's recorded index
// is strictly before complete's.
func TestOnCastStart_UnitTarget_CompleteFiresAfterCastTimeElapses(t *testing.T) {
	s := setupHostileTargetingPair(t)

	def := unitCastStartTestDef("cast_start_unit_test2", 1.0)
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, def.ID)

	target := teamCombatUnit(t, s, "p2", 100, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr

	ok, reason := s.beginAbilityCastLocked(caster, def.ID, target)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	advance(s, 40) // 40 * 0.05s = 2s, past the 1.0s cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	defer func() { s.previewTrace = nil }()

	starts := traceFireIndices(tr, "start")
	completes := traceFireIndices(tr, "complete")
	if len(starts) != 1 {
		t.Fatalf("on_cast_start fired %d times total, want exactly 1", len(starts))
	}
	if len(completes) != 1 {
		t.Fatalf("on_cast_complete fired %d times after the cast resolved, want exactly 1", len(completes))
	}
	if starts[0] >= completes[0] {
		t.Fatalf("on_cast_start (trace idx %d) did not fire strictly before on_cast_complete (trace idx %d)", starts[0], completes[0])
	}
	if caster.CastAbilityID != "" {
		t.Fatalf("caster.CastAbilityID = %q, want empty (cast should be cleared after completion)", caster.CastAbilityID)
	}
}

// TestOnCastStart_InstantCast_FiresBeforeCompleteSameTick covers the
// zero-cast-time path: beginAbilityCastLocked resolves synchronously in the
// same call, so both triggers must fire within that single call, start
// strictly first.
func TestOnCastStart_InstantCast_FiresBeforeCompleteSameTick(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	def := unitCastStartTestDef("cast_start_instant_test", 0)
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, def.ID)

	target := teamCombatUnit(t, s, "p2", 100, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	ok, reason := s.beginAbilityCastLocked(caster, def.ID, target)
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	starts := traceFireIndices(tr, "start")
	completes := traceFireIndices(tr, "complete")
	if len(starts) != 1 {
		t.Fatalf("on_cast_start fired %d times, want exactly 1", len(starts))
	}
	if len(completes) != 1 {
		t.Fatalf("on_cast_complete fired %d times, want exactly 1 (instant cast resolves synchronously)", len(completes))
	}
	if starts[0] >= completes[0] {
		t.Fatalf("on_cast_start (trace idx %d) did not fire strictly before on_cast_complete (trace idx %d) within the same instant-cast call", starts[0], completes[0])
	}
}

// ── the hazard test: interrupted cast fires start, never complete ──────────

// TestOnCastStart_InterruptedCast_FiresStartButNeverComplete documents the
// accepted unpaired-on-interrupt hazard (see fireCastStartTriggerLocked's doc
// comment): a cast that begins and is then interrupted (target dies mid-cast,
// same castFailTargetLost path the AI_RULES canonical target-validity guard
// exists for) fires on_cast_start exactly once and on_cast_complete never —
// there is no on_cast_interrupted counterpart. This test is the semantic
// record of that decision, not a bug report.
func TestOnCastStart_InterruptedCast_FiresStartButNeverComplete(t *testing.T) {
	s := setupHostileTargetingPair(t)

	def := unitCastStartTestDef("cast_start_interrupt_test", 1.0)
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, def.ID)

	target := teamCombatUnit(t, s, "p2", 100, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr

	ok, reason := s.beginAbilityCastLocked(caster, def.ID, target)
	if !ok {
		s.mu.Unlock()
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}
	if len(traceFireIndices(tr, "start")) != 1 {
		s.mu.Unlock()
		t.Fatalf("on_cast_start should have fired once at begin")
	}

	// Kill the target mid-cast (well before the 1.0s cast time elapses) —
	// the next tick's re-validation (tickUnitCastLocked ->
	// canAbilityTargetUnitLocked's target.HP<=0 check) cancels the cast via
	// cancelUnitCastLocked(unit, castFailTargetLost), which never fires
	// on_cast_complete.
	target.HP = 0
	s.mu.Unlock()

	advance(s, 4) // 4 * 0.05s = 0.2s, well short of the 1.0s cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	defer func() { s.previewTrace = nil }()

	starts := traceFireIndices(tr, "start")
	completes := traceFireIndices(tr, "complete")
	if len(starts) != 1 {
		t.Fatalf("on_cast_start fired %d times, want exactly 1 (unaffected by the later interrupt)", len(starts))
	}
	if len(completes) != 0 {
		t.Fatalf("on_cast_complete fired %d times, want 0 (interrupted cast must not resolve)", len(completes))
	}
	if caster.CastAbilityID != "" {
		t.Fatalf("caster.CastAbilityID = %q, want empty (interrupt should clear cast state)", caster.CastAbilityID)
	}
	if caster.LastCastFailure != castFailTargetLost {
		t.Fatalf("caster.LastCastFailure = %q, want %q", caster.LastCastFailure, castFailTargetLost)
	}
}

// ── point-cast path coverage ─────────────────────────────────────────────────

// TestOnCastStart_PointCast_Fires proves beginAbilityCastAtPointLocked also
// fires on_cast_start, with CastPoint populated on the context (the point
// path's only addition to the shared context subset).
func TestOnCastStart_PointCast_Fires(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	const abilityID = "cast_start_point_test"
	def := AbilityDef{
		ID:            abilityID,
		Type:          AbilitySpell,
		SchemaVersion: 2,
		TargetsPoint:  true,
		CastRange:     CastRange(600),
		CastTime:      0,
		Program: &AbilityProgram{
			Entry: AbilityEntryDef{Type: EntryGroundPoint, Range: CastRange(600)},
			Triggers: []AbilityTriggerDef{
				{ID: "start", Type: TriggerOnCastStart, Actions: []AbilityActionDef{}},
				{ID: "complete", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{}},
			},
		},
	}
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 400, 400)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, abilityID)

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	ok, reason := s.beginAbilityCastAtPointLocked(caster, abilityID, 500, 400)
	if !ok {
		t.Fatalf("beginAbilityCastAtPointLocked failed: %q", reason)
	}

	starts := traceFireIndices(tr, "start")
	if len(starts) != 1 {
		t.Fatalf("on_cast_start fired %d times for the point-cast path, want exactly 1", len(starts))
	}
}

// ── channel path coverage ────────────────────────────────────────────────────

// TestOnCastStart_ChannelCast_Fires proves beginAbilityChannelLocked also
// fires on_cast_start for a converted (schemaVersion>=2) channel ability,
// exactly once at channel start.
func TestOnCastStart_ChannelCast_Fires(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	const abilityID = "cast_start_channel_test"
	def := AbilityDef{
		ID:               abilityID,
		Type:             AbilitySpell,
		SchemaVersion:    2,
		CanTargetEnemies: true,
		CastRange:        CastRange(300),
		Program: &AbilityProgram{
			Entry: AbilityEntryDef{Type: EntryUnit, Range: CastRange(300), Relations: []TargetRelation{RelEnemy}},
			Triggers: []AbilityTriggerDef{
				{ID: "start", Type: TriggerOnCastStart, Actions: []AbilityActionDef{}},
				{
					ID:   "cast",
					Type: TriggerOnCastComplete,
					Actions: []AbilityActionDef{
						{
							ID:     "channel",
							Type:   ActionBeam,
							Target: &TargetQueryDef{Source: SrcInitialTarget},
							Config: marshalConfig(beamConfig{
								Channeled:           true,
								ChannelType:         "test_beam",
								TickIntervalSeconds: 1.0,
								ManaCostPerTick:     0,
								DamagePerTick:       0,
							}),
						},
					},
				},
			},
		},
	}
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, abilityID)

	target := teamCombatUnit(t, s, "p2", 100, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	ok, reason := s.beginAbilityChannelLocked(caster, abilityID, target)
	if !ok {
		t.Fatalf("beginAbilityChannelLocked failed: %q", reason)
	}
	if caster.ChannelAbilityID != abilityID {
		t.Fatalf("caster.ChannelAbilityID = %q, want %q (channel should have started)", caster.ChannelAbilityID, abilityID)
	}

	starts := traceFireIndices(tr, "start")
	if len(starts) != 1 {
		t.Fatalf("on_cast_start fired %d times for the channel path, want exactly 1", len(starts))
	}
}

// ── legacy abilities never fire on_cast_start ───────────────────────────────

// TestOnCastStart_LegacyAbility_NeverFires proves a legacy (SchemaVersion<2,
// Program nil) ability is completely unaffected: fireCastStartTriggerLocked
// no-ops for it, matching every other trigger-dispatch call site's guard.
func TestOnCastStart_LegacyAbility_NeverFires(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	def := AbilityDef{
		ID:               "cast_start_legacy_test",
		Type:             AbilitySpell,
		CanTargetEnemies: true,
		CastRange:        CastRange(300),
		CastTime:         0,
		DamageAmount:     10,
	}
	registerRuntimeTestAbility(t, def)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Abilities = append(caster.Abilities, def.ID)

	target := teamCombatUnit(t, s, "p2", 100, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	ok, reason := s.beginAbilityCastLocked(caster, def.ID, target)
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}
	if len(tr.Events) != 0 {
		t.Fatalf("legacy ability cast recorded %d trace events, want 0 (fireCastStartTriggerLocked must no-op for SchemaVersion<2)", len(tr.Events))
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// Production-safety guard: on_cast_start is AUTHORED-ONLY, exactly like
// on_zone_enter/on_zone_exit (see TestCatalog_NoAbilityUsesZoneEnterExitTriggers,
// ability_zone_occupancy_test.go, which this mirrors). compileLegacyAbility
// must never emit it, so no live catalog ability's behavior can change.
// ═════════════════════════════════════════════════════════════════════════════

// TestCatalog_NoAbilityUsesCastStartTrigger walks every registered ability's
// compiled program (legacy abilities via compileLegacyAbility, converted
// abilities via their shipped Program) and fails if any trigger anywhere in
// it (including nested create_zone triggers) is on_cast_start.
func TestCatalog_NoAbilityUsesCastStartTrigger(t *testing.T) {
	for _, def := range ListAbilityDefs() {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			prog := catalogProgram(def)
			for _, tt := range collectAllTriggerTypesForProductionGuard(prog) {
				if tt == TriggerOnCastStart {
					t.Fatalf("ability %q compiles an on_cast_start trigger; on_cast_start must stay editor-only (never compiler-emitted)", def.ID)
				}
			}
		})
	}
}
