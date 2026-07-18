package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Preview-mode trace hook (Phase 6a, Task 1)
//
// GameState.previewTrace / previewClock let a (later-phase) preview harness
// attach a trace + sim-time clock to the executor without touching any
// production call site's behavior: both fields are nil/0 everywhere except a
// harness run, and record() is nil-receiver-safe, so an unset previewTrace
// means the executor's trace stays completely inert, exactly as before this
// task. See resolveAbilityProgramCastLocked / fireAbilityZoneTickLocked.
// ═════════════════════════════════════════════════════════════════════════════

// traceHasType reports whether tr contains at least one event of type typ.
func traceHasType(tr *AbilityExecutionTrace, typ string) bool {
	for _, e := range tr.Events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// buildTraceTestScene spawns a caster (p1) and a hostile enemy (p2) within
// range, mirroring the golden-test twin-scene helpers (teamCombatUnit /
// setTeam / newProjectileTestState). Lock is held on return; caller must
// s.mu.Unlock().
func buildTraceTestScene(t *testing.T) (s *GameState, caster, enemy *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100

	enemy = teamCombatUnit(t, s, "p2", 40, 0)

	return s, caster, enemy
}

// traceTestAbilityDef is a minimal SchemaVersion 2 composable ability: select
// the initial target, deal 10 fire damage. Zero mana cost so mana bookkeeping
// never interferes with the assertions under test.
func traceTestAbilityDef() AbilityDef {
	return AbilityDef{
		ID:            "trace_x",
		SchemaVersion: 2,
		DamageType:    DamageFire,
		Program: &AbilityProgram{
			Triggers: []AbilityTriggerDef{
				{
					ID:   "t",
					Type: TriggerOnCastComplete,
					Actions: []AbilityActionDef{
						{
							ID:      "sel",
							Type:    ActionSelectTargets,
							Outputs: map[string]string{"targets": "e"},
							Target:  &TargetQueryDef{Source: SrcInitialTarget},
						},
						{
							ID:     "dmg",
							Type:   ActionDealDamage,
							Input:  map[string]ContextRef{"targets": {Key: "e"}},
							Config: []byte(`{"amount":10,"type":"fire"}`),
						},
					},
				},
			},
		},
	}
}

func TestExecutorTraceModeRecordsTimestampedEvents(t *testing.T) {
	s, caster, enemy := buildTraceTestScene(t)
	defer s.mu.Unlock()

	def := traceTestAbilityDef()
	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	s.previewClock = 1.25
	s.resolveAbilityProgramCastLocked(caster, def, s.effectiveSpellLocked(caster, def), enemy, protocol.Vec2{})
	s.previewTrace = nil
	s.previewClock = 0

	if !traceHasType(tr, "damage_applied") {
		t.Fatalf("no damage_applied event recorded: %+v", tr.Events)
	}
	for _, e := range tr.Events {
		if e.Type == "damage_applied" && e.Time != 1.25 {
			t.Errorf("damage_applied event Time = %v, want 1.25", e.Time)
		}
	}
}

// TestLeafTraceEventsCarryActionPath verifies that leaf effect events emitted
// from inside an action's Execute (damage_applied, targets_selected, ...)
// carry the acting action's flow path (e.g. "t.actions[dmg]"), not an empty
// string, so the preview editor can jump from a log row to its flow node.
func TestLeafTraceEventsCarryActionPath(t *testing.T) {
	s, caster, enemy := buildTraceTestScene(t)
	defer s.mu.Unlock()

	def := traceTestAbilityDef()
	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	s.previewClock = 0
	s.resolveAbilityProgramCastLocked(caster, def, s.effectiveSpellLocked(caster, def), enemy, protocol.Vec2{})
	s.previewTrace = nil

	var found bool
	for _, e := range tr.Events {
		if e.Type == "damage_applied" {
			found = true
			if e.Path != "t.actions[dmg]" {
				t.Fatalf("damage_applied path = %q, want t.actions[dmg]", e.Path)
			}
		}
	}
	if !found {
		t.Fatal("no damage_applied event")
	}
	for _, e := range tr.Events {
		if e.Type == "targets_selected" && e.Path != "t.actions[sel]" {
			t.Errorf("targets_selected path = %q, want t.actions[sel]", e.Path)
		}
	}
}

func TestExecutorTraceOffByDefault(t *testing.T) {
	s, caster, enemy := buildTraceTestScene(t)
	defer s.mu.Unlock()

	def := traceTestAbilityDef()
	preHP := enemy.HP

	// previewTrace/previewClock intentionally left unset (nil/0), matching
	// every production call site.
	s.resolveAbilityProgramCastLocked(caster, def, s.effectiveSpellLocked(caster, def), enemy, protocol.Vec2{})

	wantHP := preHP - 10
	if enemy.HP != wantHP {
		t.Fatalf("enemy.HP = %d, want %d (preview hook must not change production behavior)", enemy.HP, wantHP)
	}
	if s.previewTrace != nil {
		t.Fatalf("s.previewTrace = %v, want nil (default off)", s.previewTrace)
	}
}
