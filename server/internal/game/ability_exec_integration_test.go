package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Phase 3, Task 7 — END-TO-END INTEGRATION TESTS
//
// These three tests are the final proof that Tasks 1-6's executor actually
// runs both fixture abilities' real v2 program JSON (not hand-built
// AbilityProgram Go literals — that's what ability_exec_run_test.go's
// TestExecuteGreaterHealFlow already covers) end to end against real spawned
// units, AND that none of it altered the pre-existing legacy cast path
// (resolveAbilityCastLocked / beginAbilityCastLocked), which remains the only
// thing wired into live play in Phase 3.
// ═════════════════════════════════════════════════════════════════════════════

// ── Test 1: Greater Heal, full v2 program from JSON ──────────────────────────
//
// Same caster/ally arrangement as TestExecuteGreaterHealFlow (that test's
// AbilityProgram is a hand-built Go literal proving the executor loop itself;
// this test decodes the CANONICAL fixture JSON and confirms decode->execute
// end to end). Positions are chosen to satisfy the fixture's real
// "range": "match_attack_range" (-1 sentinel) select query, which resolves to
// the caster's AttackRange (90, set by teamCombatUnit) — a1 at 40px and a2 at
// 80px are both within that 90px radius.
func TestGreaterHealV2Program_EndToEnd(t *testing.T) {
	var def AbilityDef
	if err := json.Unmarshal([]byte(greaterHealV2JSON), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Program == nil {
		t.Fatal("greater_heal v2 fixture: Program is nil")
	}

	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	caster.HP, caster.MaxHP = 100, 100 // full HP: heal must clamp here, not overheal

	a1 := teamCombatUnit(t, s, "p1", 40, 0) // 40px from caster, within AttackRange=90
	a1.HP, a1.MaxHP = 20, 100

	a2 := teamCombatUnit(t, s, "p1", 80, 0) // 80px from caster, within AttackRange=90
	a2.HP, a2.MaxHP = 60, 100

	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID:  caster.ID,
		AbilityID: def.ID,
		Named:     map[string]ContextValue{},
		Trace:     tr,
	}
	ctx.program = def.Program

	s.runProgramTriggersLocked(ctx, def.Program.Triggers, TriggerOnCastComplete)

	// select_targets picks the 3 lowest-health-% self/ally units in range
	// (a1=20%, a2=60%, caster=100%; maxCount=3 keeps all three), then
	// restore_health heals each by the fixture's amount=15, clamped at MaxHP:
	//   a1: 20 + 15 = 35   (< 100, no clamp)
	//   a2: 60 + 15 = 75   (< 100, no clamp)
	//   caster: 100 + 15 -> clamped to 100 (already full)
	if a1.HP != 35 {
		t.Errorf("a1.HP = %d; want 35 (20 + heal amount 15)", a1.HP)
	}
	if a2.HP != 75 {
		t.Errorf("a2.HP = %d; want 75 (60 + heal amount 15)", a2.HP)
	}
	if caster.HP != 100 {
		t.Errorf("caster.HP = %d; want 100 (clamped, already full)", caster.HP)
	}

	if !traceHas(tr, "targets_selected") {
		t.Error("trace missing targets_selected")
	}
	if !traceHas(tr, "healing_applied") {
		t.Error("trace missing healing_applied")
	}
	// play_presentation (healing_glow) has a registered Execute (Phase 6b,
	// Task 1) — it must actually play the on-target effect (attached via
	// Input["attach"]), not be traced as skipped.
	if !traceHas(tr, "presentation_played") {
		t.Errorf("trace missing presentation_played for the on-target play_presentation action: %+v", tr.Events)
	}
	if traceHas(tr, "action_skipped") {
		t.Errorf("play_presentation must no longer be skipped now that it has a registered Execute: %+v", tr.Events)
	}
}

// ── Test 2: Meteor, impact -> zone gameplay pipeline ──────────────────────────
//
// on_animation_marker FIRING is deferred in Phase 3 (no presentation
// scheduler yet), so this test locates the fixture's "impact" marker trigger
// by hand and invokes it directly through runProgramTriggersLocked, exactly
// as a later-phase scheduler would once it exists. It fires ALL of
// presentations[0]'s triggers (not just "impact") so the sibling
// "cross_unit_plane" -> change_render_layer trigger is exercised too: that
// action has no Execute registered (DEFERRED), so it must only add an
// action_skipped trace event and must NOT affect gameplay.
func TestMeteorV2Program_ImpactAndZone_EndToEnd(t *testing.T) {
	var def AbilityDef
	if err := json.Unmarshal([]byte(meteorV2JSON), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Program == nil {
		t.Fatal("meteor v2 fixture: Program is nil")
	}
	if len(def.Program.Presentations) == 0 {
		t.Fatal("meteor v2 fixture: no presentations")
	}

	// Locate the "impact" on_animation_marker trigger (there are two:
	// cross_unit_plane -> change_render_layer, and impact -> select+damage+zone).
	var impactTrig *AbilityTriggerDef
	for i := range def.Program.Presentations[0].Triggers {
		trg := &def.Program.Presentations[0].Triggers[i]
		if trg.Type == TriggerOnAnimationMarker && trg.Timing != nil && trg.Timing.Marker == "impact" {
			impactTrig = trg
		}
	}
	if impactTrig == nil {
		t.Fatal(`meteor v2 fixture: no on_animation_marker trigger with Timing.Marker == "impact"`)
	}

	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	impactPoint := protocol.Vec2{X: 200, Y: 200}
	// e1 sits exactly on the impact point; e2 is 70px away. Both are within
	// the impact select radius (230) AND the Burning Crater zone radius
	// (120), so both take the impact hit AND every burn tick.
	e1 := teamCombatUnit(t, s, "p2", 200, 200)
	e2 := teamCombatUnit(t, s, "p2", 200, 130)
	e1MaxHP, e2MaxHP := e1.MaxHP, e2.MaxHP

	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID:       caster.ID,
		AbilityID:      def.ID,
		ImpactPosition: impactPoint,
		Named:          map[string]ContextValue{},
		Trace:          tr,
	}
	ctx.program = def.Program

	// Fire every trigger on the presentation instance (cross_unit_plane AND
	// impact) — both are on_animation_marker triggers so both match ttype.
	s.runProgramTriggersLocked(ctx, def.Program.Presentations[0].Triggers, TriggerOnAnimationMarker)

	// Impact: select_targets(origin=impact_position, radius=230, relations=
	// [enemy]) picks e1 and e2; deal_damage amount=140 fire hits both.
	wantAfterImpactE1 := e1MaxHP - 140
	wantAfterImpactE2 := e2MaxHP - 140
	if e1.HP != wantAfterImpactE1 {
		t.Fatalf("e1.HP after impact = %d; want %d (maxHP %d - 140 impact dmg)", e1.HP, wantAfterImpactE1, e1MaxHP)
	}
	if e2.HP != wantAfterImpactE2 {
		t.Fatalf("e2.HP after impact = %d; want %d (maxHP %d - 140 impact dmg)", e2.HP, wantAfterImpactE2, e2MaxHP)
	}

	if len(s.AbilityZones) != 1 {
		t.Fatalf("len(s.AbilityZones) = %d; want 1 (Burning Crater)", len(s.AbilityZones))
	}
	zone := s.AbilityZones[0]
	if zone.Center != impactPoint {
		t.Errorf("zone.Center = %+v; want %+v (impactPosition)", zone.Center, impactPoint)
	}
	if zone.Radius != 120 {
		t.Errorf("zone.Radius = %v; want 120", zone.Radius)
	}
	if zone.Remaining != 4 {
		t.Errorf("zone.Remaining = %v; want 4 (fixture duration)", zone.Remaining)
	}

	if !traceHas(tr, "damage_applied") {
		t.Error("trace missing damage_applied")
	}
	if !traceHas(tr, "zone_created") {
		t.Error("trace missing zone_created")
	}
	// cross_unit_plane's change_render_layer has no Execute registered
	// (DEFERRED) -> must be traced skipped, and (proven above) must not have
	// touched gameplay.
	if !traceHas(tr, "action_skipped") {
		t.Errorf("trace missing action_skipped for the deferred change_render_layer action: %+v", tr.Events)
	}

	// Advance the zone by exactly 8 x 0.5s = 4.0s (the fixture's duration),
	// stepping in dt=0.5 which is exact in binary (unlike 0.1) so no
	// zoneTickEpsilon rounding ambiguity affects tick count.
	//
	// spawnAbilityZoneLocked arms tickTimer = 0 (immediate first tick,
	// matching GroundHazard's same-tick-as-impact burn pacing), so the very
	// first tickAbilityZonesLocked call already has a tick due AND, because
	// dt (0.5) equals TickInterval (0.5) exactly, a second one lands in that
	// same call too (0 - 0.5 = -0.5 fires tick #1, timer -> 0; the loop's
	// next iteration sees exactly 0 <= eps and fires tick #2 immediately,
	// timer -> 0.5) — tickAbilityZonesLocked's loop is built to fire as many
	// ticks as are due within a single call, and here two are due in the
	// first 0.5s slice (one "at t=0", one "at t=0.5"):
	//   step 1 (t=0.5): fires tick #1 AND tick #2 (see above); timer -> 0.5
	//   step 2 (t=1.0): fire tick #3 ... identically through
	//   step 8 (t=4.0): fire tick #9; Remaining 0.5-0.5=0, not > eps -> culled
	// 9 ticks x 12 dmg (fixture amount) = 108 total burn damage on each enemy
	// (both stay well inside the 120px zone radius the whole time). This
	// N+1 total (one more than the clean Duration/Interval=8 floor) mirrors
	// GroundHazard's own accumulator-overshoot extra tick exactly — see
	// TestAbilityCompileGolden_Meteor.
	for i := 0; i < 8; i++ {
		s.tickAbilityZonesLocked(0.5)
	}

	wantFinalE1 := wantAfterImpactE1 - 108
	wantFinalE2 := wantAfterImpactE2 - 108
	if e1.HP != wantFinalE1 {
		t.Errorf("e1.HP after 9 burn ticks = %d; want %d (%d impact-HP - 9*12 burn)", e1.HP, wantFinalE1, wantAfterImpactE1)
	}
	if e2.HP != wantFinalE2 {
		t.Errorf("e2.HP after 9 burn ticks = %d; want %d (%d impact-HP - 9*12 burn)", e2.HP, wantFinalE2, wantAfterImpactE2)
	}
	if len(s.AbilityZones) != 0 {
		t.Errorf("len(s.AbilityZones) = %d; want 0 (zone should have expired after its 4s duration)", len(s.AbilityZones))
	}
}

// ── Test 3: legacy cast path is unchanged ─────────────────────────────────────
//
// The executor (runProgramTriggersLocked et al., Tasks 1-6) is not wired into
// resolveAbilityCastLocked / beginAbilityCastLocked in Phase 3 — this test
// guards that invariant by exercising the REAL legacy cast path against the
// catalog "heal" ability (catalog/abilities/heal/heal.json), which has no
// "program" key and so decodes with Program == nil. Solid existing coverage
// of this exact path already exists and is left untouched:
//   - TestHeal_RestoresHPAndDeductsMana (ability_cast_test.go) — full cast,
//     HP + mana deltas, healing_glow effect queued.
//   - TestHeal_CannotOverheal (ability_cast_test.go) — clamp-at-MaxHP.
//
// This test is a short, focused re-assertion (not a duplicate of their full
// scope) that legacy resolution runs to completion and heals correctly with
// Program nil, i.e. legacy resolution does not go anywhere near the new
// executor.
func TestLegacyHealCast_UnaffectedByExecutor(t *testing.T) {
	def := healDef(t)
	if def.Program != nil {
		t.Fatal(`catalog "heal" ability now has a Program — this test (and the legacy-path guarantee it checks) needs re-evaluating`)
	}

	s, app, ally := healSetup(t)
	s.mu.Lock()
	allyID := ally.ID
	wantHP := ally.HP + def.HealAmount
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	advance(s, 25) // past the full cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != wantHP {
		t.Errorf("ally HP = %d; want %d (legacy heal path: +%d from catalog HealAmount)", a.HP, wantHP, def.HealAmount)
	}
}
