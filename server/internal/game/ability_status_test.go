package game

import (
	"encoding/json"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// AbilityStatus subsystem — on_status_tick / on_status_expire
//
// Mirrors ability_zone_occupancy_test.go's structure closely: AbilityStatus is
// modeled directly on AbilityZone (see ability_status.go's file doc comment),
// so these tests reuse the same test helpers (setupHostileTargetingPair,
// teamCombatUnit, currentEventDamageTrigger, traceTriggerFireCount, traceHas)
// and the same re-entrancy/determinism concerns.
//
// Authored-only, exactly like on_zone_enter/on_zone_exit/on_unit_death: no
// catalog ability compiles on_status_tick/on_status_expire (see
// TestCatalog_NoAbilityUsesStatusTickExpireTriggers at the bottom) and the
// LEGACY apply_status(slow/stun/burn) path is untouched byte-for-byte (see
// TestAbilityCompileGolden_Shatter, unaffected by anything in this file).
// ═════════════════════════════════════════════════════════════════════════════

// spawnTestStatus builds and registers an AbilityStatus directly via
// spawnAbilityStatusLocked (bypassing the apply_status action's JSON/config
// plumbing, which most of these tests don't need — they drive
// Remaining/TickInterval/Triggers directly for precise control, mirroring
// spawnTestZone's role for AbilityZone). Caller holds s.mu.
func spawnTestStatus(s *GameState, caster, target *Unit, remaining, tickInterval float64, triggers []AbilityTriggerDef) *AbilityStatus {
	st := &AbilityStatus{
		AbilityID:    "test_status",
		CasterID:     caster.ID,
		TargetUnitID: target.ID,
		Remaining:    remaining,
		TickInterval: tickInterval,
		Triggers:     triggers,
	}
	s.spawnAbilityStatusLocked(st)
	return st
}

// ── on_status_tick cadence ──────────────────────────────────────────────────

// TestAbilityStatus_TicksOnIntervalAndFiresOnStatusTick exercises the core
// cadence: an authored status fires its on_status_tick trigger every
// TickInterval seconds, via the SAME executor every other trigger uses (no
// hand-rolled damage). Unlike AbilityZone (whose first tick fires
// IMMEDIATELY, tickTimer=0 — see spawnAbilityZoneLocked), a status's first
// tick fires after one full TickInterval (see spawnAbilityStatusLocked's doc
// comment for why): Duration=1.5, TickInterval=0.5 -> ticks due at t=0.5,
// 1.0, 1.5 -> exactly 3 ticks, with the last coinciding with natural expiry.
func TestAbilityStatus_TicksOnIntervalAndFiresOnStatusTick(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.HP, target.MaxHP = 100, 100

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	spawnTestStatus(s, caster, target, 1.5, 0.5, []AbilityTriggerDef{
		currentEventDamageTrigger("tick", TriggerOnTick, 10),
	})

	for i := 0; i < 15; i++ { // 15 x 0.1s = 1.5s total
		s.tickAbilityStatusesLocked(0.1)
	}

	if traceTriggerFireCount(tr, "tick") != 3 {
		t.Fatalf("on_status_tick fired %d times, want exactly 3", traceTriggerFireCount(tr, "tick"))
	}
	if target.HP != 70 {
		t.Fatalf("target.HP = %d, want 70 (100 - 3*10)", target.HP)
	}
	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("status should have expired: %d remaining", len(s.AbilityStatuses))
	}
}

// TestAbilityStatus_TickBindsAfflictedUnit is the binding requirement: a
// on_status_tick trigger's select_targets{source:"current_event"} ->
// deal_damage must resolve to EXACTLY the unit the status is attached to, not
// some other unit that also happens to be nearby.
func TestAbilityStatus_TickBindsAfflictedUnit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	afflicted := teamCombatUnit(t, s, "p2", 50, 0)
	bystander := teamCombatUnit(t, s, "p2", 55, 0) // close enough an area query would also hit it
	afflicted.HP, afflicted.MaxHP = 100, 100
	bystander.HP, bystander.MaxHP = 100, 100

	spawnTestStatus(s, caster, afflicted, 1, 0.5, []AbilityTriggerDef{
		currentEventDamageTrigger("tick", TriggerOnTick, 10),
	})

	s.tickAbilityStatusesLocked(0.5)

	if afflicted.HP != 90 {
		t.Fatalf("afflicted.HP = %d, want 90 (tick damages exactly the bound unit)", afflicted.HP)
	}
	if bystander.HP != 100 {
		t.Fatalf("bystander.HP = %d, want 100 (current_event must bind ONLY the afflicted unit)", bystander.HP)
	}
}

// ── on_status_expire ─────────────────────────────────────────────────────

// TestAbilityStatus_ExpireFiresExactlyOnceOnNaturalTimeout is the paired
// termination case: on natural Remaining timeout, on_status_expire fires
// exactly once and the status is dropped.
func TestAbilityStatus_ExpireFiresExactlyOnceOnNaturalTimeout(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.HP, target.MaxHP = 1000, 1000

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	spawnTestStatus(s, caster, target, 1.0, 0.5, []AbilityTriggerDef{
		currentEventDamageTrigger("expire", TriggerOnStatusExpire, 5),
	})

	for i := 0; i < 20; i++ { // well past the 1.0s Remaining
		s.tickAbilityStatusesLocked(0.1)
	}

	if traceTriggerFireCount(tr, "expire") != 1 {
		t.Fatalf("on_status_expire fired %d times, want exactly 1", traceTriggerFireCount(tr, "expire"))
	}
	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("status should have been removed after expiry, got %d remaining", len(s.AbilityStatuses))
	}
	if target.HP != 995 {
		t.Fatalf("target.HP = %d, want 995 (expire's damage action fired exactly once)", target.HP)
	}
}

// TestAbilityStatus_ExpireFiresOnceWhenTargetDies is the target-death design
// call, tested both ways it can happen: found dead at the top of a tick call
// (some unrelated damage source killed it earlier the same Update pass), and
// killed BY the status's own on_status_tick action mid-loop. Either way,
// on_status_expire fires EXACTLY ONCE, the same tick, and on_status_tick never
// fires against an already-dead target — see ability_status.go's EXPIRY
// SEMANTICS doc comment for why this pairing (not "only on natural timeout")
// was chosen.
func TestAbilityStatus_ExpireFiresOnceWhenTargetDies(t *testing.T) {
	t.Run("dead_before_tick_call", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		target := teamCombatUnit(t, s, "p2", 50, 0)

		tr := &AbilityExecutionTrace{}
		s.previewTrace = tr
		defer func() { s.previewTrace = nil }()

		spawnTestStatus(s, caster, target, 5, 1, []AbilityTriggerDef{
			currentEventDamageTrigger("tick", TriggerOnTick, 1),
			currentEventDamageTrigger("expire", TriggerOnStatusExpire, 1),
		})

		target.HP = 0 // died from some unrelated cause earlier this Update pass

		s.tickAbilityStatusesLocked(0.1)

		if traceTriggerFireCount(tr, "tick") != 0 {
			t.Fatalf("on_status_tick fired %d times against an already-dead target, want 0", traceTriggerFireCount(tr, "tick"))
		}
		if traceTriggerFireCount(tr, "expire") != 1 {
			t.Fatalf("on_status_expire fired %d times for a dead target, want exactly 1", traceTriggerFireCount(tr, "expire"))
		}
		if len(s.AbilityStatuses) != 0 {
			t.Fatalf("status must be dropped once its target is found dead, got %d remaining", len(s.AbilityStatuses))
		}
	})

	t.Run("dies_mid_tick_from_its_own_tick_action", func(t *testing.T) {
		s := setupHostileTargetingPair(t)
		defer s.mu.Unlock()

		caster := teamCombatUnit(t, s, "p1", 0, 0)
		target := teamCombatUnit(t, s, "p2", 50, 0)
		target.HP, target.MaxHP = 5, 100 // dies to the very first tick's damage

		tr := &AbilityExecutionTrace{}
		s.previewTrace = tr
		defer func() { s.previewTrace = nil }()

		spawnTestStatus(s, caster, target, 5, 0.5, []AbilityTriggerDef{
			currentEventDamageTrigger("tick", TriggerOnTick, 100), // lethal
			currentEventDamageTrigger("expire", TriggerOnStatusExpire, 1),
		})

		s.tickAbilityStatusesLocked(0.5)

		if traceTriggerFireCount(tr, "tick") != 1 {
			t.Fatalf("on_status_tick fired %d times, want exactly 1 (the lethal one — no further ticks against a corpse)", traceTriggerFireCount(tr, "tick"))
		}
		if traceTriggerFireCount(tr, "expire") != 1 {
			t.Fatalf("on_status_expire fired %d times after the tick killed its own target, want exactly 1", traceTriggerFireCount(tr, "expire"))
		}
		if len(s.AbilityStatuses) != 0 {
			t.Fatalf("status must be dropped the same tick its target dies mid-tick, got %d remaining", len(s.AbilityStatuses))
		}
	})
}

// ── stacking / refresh model ─────────────────────────────────────────────

// TestAbilityStatus_StackingRefresh_ExtendsRemainingWithoutDuplicating covers
// the default ("refresh") stacking model: re-applying the SAME
// (AbilityID,Name) to the SAME target never creates a second instance, and
// only ever extends Remaining to the longer of the two durations
// (refresh-longer — the same convention ApplyStunLocked/applySlowToTrack
// already use for the legacy CC primitives).
func TestAbilityStatus_StackingRefresh_ExtendsRemainingWithoutDuplicating(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	spawnTestStatus(s, caster, target, 3, 1, nil)
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("setup: expected 1 status, got %d", len(s.AbilityStatuses))
	}

	// Shorter reapplication: must not shrink Remaining, must not duplicate.
	spawnTestStatus(s, caster, target, 1, 1, nil)
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("refresh stacking must not duplicate, got %d instances", len(s.AbilityStatuses))
	}
	if s.AbilityStatuses[0].Remaining != 3 {
		t.Fatalf("shorter reapplication must not shrink Remaining: got %v, want 3", s.AbilityStatuses[0].Remaining)
	}

	// Longer reapplication: must extend.
	spawnTestStatus(s, caster, target, 10, 1, nil)
	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("refresh stacking must not duplicate, got %d instances", len(s.AbilityStatuses))
	}
	if s.AbilityStatuses[0].Remaining != 10 {
		t.Fatalf("longer reapplication must extend Remaining: got %v, want 10", s.AbilityStatuses[0].Remaining)
	}
}

// TestAbilityStatus_StackingStack_CreatesIndependentInstancesUpToMaxStacks
// covers the "stack" model: each application is a fully independent
// AbilityStatus instance (own Remaining/tickTimer), up to MaxStacks sharing
// the same (AbilityID,Name,target) key; further applications beyond the cap
// are dropped.
func TestAbilityStatus_StackingStack_CreatesIndependentInstancesUpToMaxStacks(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	spawn := func() {
		s.spawnAbilityStatusLocked(&AbilityStatus{
			AbilityID: "test_stack_status", CasterID: caster.ID, TargetUnitID: target.ID,
			Remaining: 5, TickInterval: 1, Stacking: "stack", MaxStacks: 2,
		})
	}

	spawn()
	spawn()
	if len(s.AbilityStatuses) != 2 {
		t.Fatalf("want 2 independent stacks, got %d", len(s.AbilityStatuses))
	}

	spawn() // exceeds MaxStacks=2
	if len(s.AbilityStatuses) != 2 {
		t.Fatalf("stack cap not enforced: got %d, want 2", len(s.AbilityStatuses))
	}
}

// ── apply_status action integration (the authored/legacy discriminator) ──

// TestActionApplyStatus_Authored_SpawnsAbilityStatus drives the REAL
// apply_status ActionDescriptor (not spawnAbilityStatusLocked directly) with
// a config carrying Triggers, proving Execute's discriminator (see
// applyStatusConfig's doc comment) routes to the AbilityStatus path and never
// touches the legacy CC tracks.
func TestActionApplyStatus_Authored_SpawnsAbilityStatus(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 50, 0)

	cfg := `{"status":"custom_poison","duration":5,"tickInterval":1,"triggers":[
		{"id":"tick","type":"on_tick","actions":[]}
	]}`
	tr := runOneActionProgram(t, s, caster.ID, 0, ActionApplyStatus, cfg, []int{enemy.ID})

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("want 1 AbilityStatus spawned, got %d", len(s.AbilityStatuses))
	}
	st := s.AbilityStatuses[0]
	if st.TargetUnitID != enemy.ID {
		t.Fatalf("status target = %d, want %d", st.TargetUnitID, enemy.ID)
	}
	if !traceHas(tr, "status_applied") {
		t.Fatalf("missing status_applied trace event: %+v", tr.Events)
	}
	if enemy.SlowedRemaining != 0 || enemy.StunnedRemaining != 0 {
		t.Fatalf("authored apply_status must not touch legacy CC tracks: slow=%v stun=%v", enemy.SlowedRemaining, enemy.StunnedRemaining)
	}
}

// TestActionApplyStatus_Legacy_StillRoutesToPrimitives is the parity
// guard closest to the golden tests: an apply_status action with NO Triggers
// (the shape every legacy-compiled ability, e.g. shatter, emits) must still
// take the old three-case switch, completely unaffected by the authored path
// existing alongside it.
func TestActionApplyStatus_Legacy_StillRoutesToPrimitives(t *testing.T) {
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
		t.Fatalf("legacy apply_status must never spawn an AbilityStatus, got %d", len(s.AbilityStatuses))
	}
}

// ── re-entrancy ───────────────────────────────────────────────────────────

// TestAbilityStatus_ReentrantApplyStatusDoesNotCorruptAbilityStatuses is the
// re-entrancy check: an on_status_tick trigger whose action applies ANOTHER
// authored apply_status action must not corrupt or lose either status even
// though tickAbilityStatusesLocked is still mid-iteration over
// s.AbilityStatuses when the nested apply_status action runs. Mirrors
// TestAbilityZoneOccupancy_ReentrantCreateZoneDoesNotCorruptAbilityZones.
func TestAbilityStatus_ReentrantApplyStatusDoesNotCorruptAbilityStatuses(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)
	target.HP, target.MaxHP = 1000, 1000

	nestedCfg, err := json.Marshal(applyStatusConfig{
		Status: "spawned_child", Name: "child", Duration: 5, TickInterval: 1,
		Triggers: []AbilityTriggerDef{{ID: "childtick", Type: TriggerOnTick, Actions: nil}},
	})
	if err != nil {
		t.Fatal(err)
	}

	outer := &AbilityStatus{
		AbilityID: "outer_status", CasterID: caster.ID, TargetUnitID: target.ID,
		Remaining: 10, TickInterval: 1,
		Triggers: []AbilityTriggerDef{
			{
				ID:   "tick",
				Type: TriggerOnTick,
				Actions: []AbilityActionDef{
					{ID: "sel", Type: ActionSelectTargets, Outputs: map[string]string{"targets": "hit"},
						Target: &TargetQueryDef{Source: SrcCurrentEvent}},
					{ID: "spawn_child", Type: ActionApplyStatus,
						Input:  map[string]ContextRef{"targets": {Key: "hit"}},
						Config: nestedCfg},
				},
			},
		},
	}
	s.spawnAbilityStatusLocked(outer)

	if len(s.AbilityStatuses) != 1 {
		t.Fatalf("setup: expected 1 status before tick, got %d", len(s.AbilityStatuses))
	}

	s.tickAbilityStatusesLocked(1.0) // fires outer's on_status_tick; nested apply_status appends a child mid-loop

	if len(s.AbilityStatuses) != 2 {
		t.Fatalf("s.AbilityStatuses = %d after nested apply_status fired, want 2 (outer survives, child spawned)", len(s.AbilityStatuses))
	}
	foundOuter, foundChild := false, false
	for _, st := range s.AbilityStatuses {
		switch st.ID {
		case outer.ID:
			foundOuter = true
		default:
			foundChild = true
			if st.Name != "child" {
				t.Errorf("child status Name = %q, want %q (config not corrupted)", st.Name, "child")
			}
		}
	}
	if !foundOuter {
		t.Fatal("outer status lost from s.AbilityStatuses after nested apply_status")
	}
	if !foundChild {
		t.Fatal("child status never made it into s.AbilityStatuses")
	}

	// A further tick must not panic or lose the outer status. (Whether the
	// nested apply_status's own refresh-vs-duplicate bookkeeping collapses or
	// grows the child count across this same-tick re-entrant boundary is not
	// asserted here — only that nothing panics and outer survives.)
	s.tickAbilityStatusesLocked(1.0)
	foundOuterAgain := false
	for _, st := range s.AbilityStatuses {
		if st.ID == outer.ID {
			foundOuterAgain = true
		}
	}
	if !foundOuterAgain {
		t.Fatal("outer status lost from s.AbilityStatuses after a further tick")
	}
	if len(s.AbilityStatuses) < 2 {
		t.Fatalf("s.AbilityStatuses shrank unexpectedly after a further tick: %d", len(s.AbilityStatuses))
	}
}

// ── determinism ───────────────────────────────────────────────────────────

// TestAbilityStatus_DeterministicFireOrderAcrossIdenticalSeeds builds two
// independent, identically-seeded scenes (newProjectileTestState always seeds
// 42) with two statuses spawned in the same order, ticks both the same way,
// and asserts the recorded trace event sequence is byte-identical: firing
// order is s.AbilityStatuses' append order (never map-iteration order — see
// ability_status.go's doc comment), so two identical runs must agree exactly.
func TestAbilityStatus_DeterministicFireOrderAcrossIdenticalSeeds(t *testing.T) {
	build := func(t *testing.T) (*GameState, *AbilityExecutionTrace) {
		s := setupHostileTargetingPair(t)
		caster := teamCombatUnit(t, s, "p1", 0, 0)
		targetA := teamCombatUnit(t, s, "p2", 50, 0)
		targetB := teamCombatUnit(t, s, "p2", 60, 0)
		targetA.HP, targetA.MaxHP = 1000, 1000
		targetB.HP, targetB.MaxHP = 1000, 1000

		tr := &AbilityExecutionTrace{}
		s.previewTrace = tr

		spawnTestStatus(s, caster, targetA, 5, 0.5, []AbilityTriggerDef{currentEventDamageTrigger("tickA", TriggerOnTick, 1)})
		spawnTestStatus(s, caster, targetB, 5, 0.5, []AbilityTriggerDef{currentEventDamageTrigger("tickB", TriggerOnTick, 1)})
		return s, tr
	}

	s1, tr1 := build(t)
	defer s1.mu.Unlock()
	s2, tr2 := build(t)
	defer s2.mu.Unlock()

	for i := 0; i < 10; i++ {
		s1.tickAbilityStatusesLocked(0.1)
		s2.tickAbilityStatusesLocked(0.1)
	}

	if len(tr1.Events) == 0 {
		t.Fatal("no events recorded; test setup broken")
	}
	if len(tr1.Events) != len(tr2.Events) {
		t.Fatalf("event count differs across identical runs: %d vs %d", len(tr1.Events), len(tr2.Events))
	}
	for i := range tr1.Events {
		if tr1.Events[i].Type != tr2.Events[i].Type || tr1.Events[i].Path != tr2.Events[i].Path {
			t.Fatalf("event %d differs across identical seeded runs: %+v vs %+v", i, tr1.Events[i], tr2.Events[i])
		}
	}
}

// ── burn key fix (legacy "burn" primitive) ─────────────────────────────────

// TestApplyAbilityBurnLocked_DifferentAbilitiesSameCasterDoNotCollide is the
// bug fix this task required: before applyAbilityBurnLocked existed,
// apply_status's "burn" case called applyProcBurnLocked directly, keyed
// ONLY by attacker ("weaponburn:<casterID>") — so two DIFFERENT abilities
// cast by the SAME caster shared one stack and refreshed each other instead
// of burning independently. applyAbilityBurnLocked keys per ABILITY INSTANCE
// instead, so this must now produce two independent stacks.
func TestApplyAbilityBurnLocked_DifferentAbilitiesSameCasterDoNotCollide(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	s.applyAbilityBurnLocked(target.ID, 5, 3, caster.ID, "fire_ability_a")
	s.applyAbilityBurnLocked(target.ID, 5, 3, caster.ID, "fire_ability_b")

	if len(target.PerkState.BurnStacks) != 2 {
		t.Fatalf("target.PerkState.BurnStacks = %d, want 2 (two DIFFERENT abilities from the SAME caster must not collide onto one stack)",
			len(target.PerkState.BurnStacks))
	}
}

// TestApplyAbilityBurnLocked_SameAbilitySameCasterRefreshes proves the fix
// didn't overshoot: the SAME ability re-applied by the SAME caster still
// refreshes in place (refresh-stronger DPS / refresh-longer duration),
// exactly like applyProcBurnLocked's own per-attacker refresh.
func TestApplyAbilityBurnLocked_SameAbilitySameCasterRefreshes(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	s.applyAbilityBurnLocked(target.ID, 5, 3, caster.ID, "fire_ability_a")
	s.applyAbilityBurnLocked(target.ID, 9, 10, caster.ID, "fire_ability_a")

	if len(target.PerkState.BurnStacks) != 1 {
		t.Fatalf("same ability re-applied should refresh in place, got %d stacks", len(target.PerkState.BurnStacks))
	}
	stack := target.PerkState.BurnStacks[0]
	if stack.DPS != 9 || stack.Remaining != 10 {
		t.Fatalf("refresh-stronger/refresh-longer not applied: dps=%v remaining=%v, want dps=9 remaining=10", stack.DPS, stack.Remaining)
	}
}

// TestOnHitProc_BurnStillKeyedPerAttacker is the explicit regression guard
// the task required: equipment weapon-burn (applyProcBurnLocked, called from
// projectile.go/beam.go's landing paths) must stay keyed per-ATTACKER,
// UNCHANGED by this fix — two procs from the SAME attacker still collapse
// into one stack (correct: one wielder, one weapon).
func TestOnHitProc_BurnStillKeyedPerAttacker(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	attacker := teamCombatUnit(t, s, "p1", 0, 0)
	target := teamCombatUnit(t, s, "p2", 50, 0)

	s.applyProcBurnLocked(target.ID, 5, 3, attacker.ID)
	s.applyProcBurnLocked(target.ID, 9, 10, attacker.ID)

	if len(target.PerkState.BurnStacks) != 1 {
		t.Fatalf("equipment weapon-burn from the SAME attacker must still collapse to 1 stack (unchanged), got %d", len(target.PerkState.BurnStacks))
	}
	stack := target.PerkState.BurnStacks[0]
	if stack.SourceKind != burnSourceWeapon {
		t.Errorf("SourceKind = %q, want %q (unchanged)", stack.SourceKind, burnSourceWeapon)
	}
}

// ── production safety ────────────────────────────────────────────────────

// TestCatalog_NoAbilityUsesStatusExpireTrigger is the production-unchanged
// guard: on_status_expire ("On Complete") is authored-only (reachable solely
// through the ability editor) — no catalog ability's compiled program may use
// it, today or as a regression later. (on_status_tick no longer exists as a
// distinct type: the generic on_tick replaced the four *_tick triggers and IS
// legitimately compiler-emitted by zones/projectiles/beams, so it is no longer
// part of this guard.) Mirrors TestCatalog_NoAbilityUsesZoneEnterExitTriggers /
// TestCatalog_NoAbilityUsesOnUnitDeathTrigger.
func TestCatalog_NoAbilityUsesStatusExpireTrigger(t *testing.T) {
	for _, def := range ListAbilityDefs() {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			prog := catalogProgram(def)
			for _, tt := range collectAllTriggerTypesForProductionGuard(prog) {
				if tt == TriggerOnStatusExpire {
					t.Fatalf("ability %q compiles an on_status_expire trigger; it must stay editor-only (never compiler-emitted)", def.ID)
				}
			}
		})
	}
}

// ── no-zones-spawned wiring guard ───────────────────────────────────────

// TestTickAbilityStatusesLocked_NoStatusesIsNoop guards the wiring into the
// live Update() loop: with no statuses spawned (the case for every existing
// test and every match until a status-spawning ability ships), the tick call
// must be a zero-cost no-op. Mirrors TestTickAbilityZonesLocked_NoZonesIsNoop.
func TestTickAbilityStatusesLocked_NoStatusesIsNoop(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	if s.AbilityStatuses != nil {
		t.Fatalf("AbilityStatuses should start nil, got %v", s.AbilityStatuses)
	}
	s.tickAbilityStatusesLocked(0.1) // must not panic
	if len(s.AbilityStatuses) != 0 {
		t.Fatalf("expected no statuses, got %d", len(s.AbilityStatuses))
	}
}
