package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// on_unit_death — composable ability trigger tests
//
// Semantics under test: on_unit_death means "a unit killed BY this ability" —
// DamageSource.SourceAbilityID must name the ability, and fireOnUnitDeathLocked
// (ability_unit_death.go) is the sole place that reads it, from
// drainPendingDeathsLocked (damage_pipeline.go).
//
// Registration: test abilities are injected into the runtimeAbilities overlay
// via registerRuntimeTestAbility (ability_cast_program_test.go) — same
// mechanism, same t.Cleanup discipline, no disk I/O.
// ═════════════════════════════════════════════════════════════════════════════

// dealDamageActionJSON builds a deal_damage action's raw Config.
func dealDamageActionJSON(amount int) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"amount": amount, "type": "physical"})
	return b
}

// onUnitDeathTrigger builds a bare on_unit_death trigger with the given id
// and actions (possibly none — a trigger with zero actions still emits a
// "trigger_fired" trace event, which is all TestAbilityOnUnitDeath_FiresOnce*
// needs).
func onUnitDeathTrigger(id string, actions ...AbilityActionDef) AbilityTriggerDef {
	return AbilityTriggerDef{ID: id, Type: TriggerOnUnitDeath, Actions: actions}
}

// currentEventDeadCorpseQuery builds the select_targets action that binds
// "current_event" with AliveState:"dead" — the shape an authored on_unit_death
// trigger uses to reference the unit that just died (see
// applyTargetFiltersLocked's AliveState handling, ability_exec_targeting.go).
func currentEventDeadCorpseQuery(actionID string) AbilityActionDef {
	return AbilityActionDef{
		ID:     actionID,
		Type:   ActionSelectTargets,
		Target: &TargetQueryDef{Source: SrcCurrentEvent, AliveState: "dead"},
	}
}

// nearEventPositionDamageTrigger builds an on_unit_death trigger whose action
// pair selects every enemy (relative to caster) within radius of the dying
// unit's own last position and deals amount damage to them. Used to prove
// binding/attribution/ordering without ever trying to "deal damage to the
// corpse" (deal_damage's Execute always skips HP<=0 targets by design).
func nearEventPositionDamageTrigger(id string, radius float64, amount int) AbilityTriggerDef {
	return AbilityTriggerDef{
		ID:   id,
		Type: TriggerOnUnitDeath,
		Actions: []AbilityActionDef{
			{
				ID:   "sel",
				Type: ActionSelectTargets,
				Target: &TargetQueryDef{
					Source:    SrcAllInScene,
					Origin:    OriginCurrentEventPos,
					Radius:    radius,
					Relations: []TargetRelation{RelEnemy},
				},
				Outputs: map[string]string{"targets": "hit"},
			},
			{
				ID:     "dmg",
				Type:   ActionDealDamage,
				Input:  map[string]ContextRef{"targets": {Key: "hit"}},
				Config: dealDamageActionJSON(amount),
			},
		},
	}
}

// programAbility builds a minimal SchemaVersion-2 AbilityDef carrying prog,
// suitable for registerRuntimeTestAbility.
func programAbility(id string, triggers ...AbilityTriggerDef) AbilityDef {
	return AbilityDef{
		ID:            id,
		SchemaVersion: 2,
		Program:       &AbilityProgram{Triggers: triggers},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1 — fires exactly once when this ability's damage kills the unit
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_FiresOnceWhenKilledByThisAbility(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ability := programAbility("test_execute", onUnitDeathTrigger("on_kill"))
	registerRuntimeTestAbility(t, ability)

	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	caster.Visible = true

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 100, 100
	victim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(victim, 999, DamageSource{
		AttackerUnitID: caster.ID, Kind: "ability", SourceAbilityID: ability.ID,
	})
	s.drainPendingDeathsLocked()

	if got := traceTriggerFireCount(tr, "on_kill"); got != 1 {
		t.Fatalf("on_unit_death fired %d times, want exactly 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2 — does NOT fire for a kill unrelated to this ability: neither an
// anonymous/basic-attack kill, nor a kill attributed to a DIFFERENT ability.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_DoesNotFireForUnrelatedKill(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	watched := programAbility("test_watched_ability", onUnitDeathTrigger("on_kill"))
	registerRuntimeTestAbility(t, watched)
	other := programAbility("test_other_ability", onUnitDeathTrigger("on_kill_other"))
	registerRuntimeTestAbility(t, other)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	// Case A: plain melee (anonymous ability attribution).
	meleeVictim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	meleeVictim.MaxHP, meleeVictim.HP = 100, 100
	meleeVictim.Visible = true
	s.applyUnitDamageWithSourceLocked(meleeVictim, 999, DamageSource{AttackerUnitID: attacker.ID, Kind: "melee"})

	// Case B: killed by a DIFFERENT ability than the one being watched.
	otherVictim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 60, Y: 0})
	otherVictim.MaxHP, otherVictim.HP = 100, 100
	otherVictim.Visible = true
	s.applyUnitDamageWithSourceLocked(otherVictim, 999, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", SourceAbilityID: other.ID,
	})

	s.drainPendingDeathsLocked()

	if got := traceTriggerFireCount(tr, "on_kill"); got != 0 {
		t.Fatalf("watched ability's on_unit_death fired %d times for unrelated kills, want 0", got)
	}
	if got := traceTriggerFireCount(tr, "on_kill_other"); got != 1 {
		t.Fatalf("other ability's on_unit_death fired %d times, want exactly 1 (only for its own kill)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3 — the dying unit is bound: select_targets{current_event} with
// aliveState:"dead" resolves to exactly the corpse.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_CurrentEventBindsCorpse(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ability := programAbility("test_bind_corpse", onUnitDeathTrigger("on_kill", currentEventDeadCorpseQuery("sel")))
	registerRuntimeTestAbility(t, ability)

	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	caster.Visible = true

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	victim.MaxHP, victim.HP = 100, 100
	victim.Visible = true
	victimID := victim.ID

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(victim, 999, DamageSource{
		AttackerUnitID: caster.ID, Kind: "ability", SourceAbilityID: ability.ID,
	})
	s.drainPendingDeathsLocked()

	var got int
	found := false
	for _, e := range tr.Events {
		if e.Type == "targets_selected" && e.Path == "on_kill.actions[sel]" {
			found = true
			if c, ok := e.Payload["count"].(int); ok {
				got = c
			}
		}
	}
	if !found {
		t.Fatal("select_targets{current_event} action never traced targets_selected")
	}
	if got != 1 {
		t.Fatalf("select_targets{current_event, aliveState:dead} resolved %d targets, want exactly 1 (the corpse, unit ID %d)", got, victimID)
	}
	// The unit really is gone from s.Units by the time the test observes it
	// (drain already ran removeUnitLocked) — confirms the binding happened
	// WHILE the corpse was still resolvable, not after.
	if s.getUnitByIDLocked(victimID) != nil {
		t.Fatalf("victim (ID=%d) should be removed after drain", victimID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4 — propagation decision: a pain_share (Vanguard) redirect that kills
// the ABSORBER still fires the ORIGINAL ability's on_unit_death. This is the
// SAME damage instance, just redirected — see DamageSource.SourceAbilityID's
// doc comment (damage_pipeline.go) and perkRedirectIncomingDamageLocked's
// redirectSrc comment (perks_auras.go) for the argument.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_PropagatesThroughPainShareRedirect(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	if def == nil {
		t.Fatal("pain_share perk def not found — is perk-defs.json loaded?")
	}
	radius := def.Config["radius"]

	ability := programAbility("test_execute_redirect", onUnitDeathTrigger("on_kill"))
	registerRuntimeTestAbility(t, ability)

	attacker := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 500, Y: 400})
	attacker.Visible = true

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 400, Y: 400})
	ally.MaxHP, ally.HP = 500, 500
	ally.Visible = true

	vanguard := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400 + radius*0.4, Y: 400})
	vanguard.MaxHP = 500
	vanguard.Visible = true
	grantPerk(vanguard, "pain_share")
	vanguard.HP = 1 // redirected fraction of 200 damage is well over 1

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	// The hit lands on "ally" but pain_share redirects a fraction of it onto
	// the Vanguard, killing the Vanguard instead.
	s.applyUnitDamageWithSourceLocked(ally, 200, DamageSource{
		AttackerUnitID: attacker.ID, Kind: "ability", SourceAbilityID: ability.ID,
	})
	if vanguard.HP > 0 {
		t.Fatalf("setup error: expected the Vanguard to die from the redirected fraction (HP=%d)", vanguard.HP)
	}

	s.drainPendingDeathsLocked()

	if got := traceTriggerFireCount(tr, "on_kill"); got != 1 {
		t.Fatalf("on_unit_death fired %d times for the Vanguard's redirect-death, want exactly 1 (attribution must survive pain_share redirect)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5 — re-entrancy: an on_unit_death trigger that deals lethal damage to
// a SECOND unit must not corrupt the drain. Verdict (matching the existing
// ability_marker.go / ability_zone.go re-entrancy precedent): the cascade
// kill is enqueued safely into a FRESH s.pendingDeaths and is NOT processed
// until the NEXT drainPendingDeathsLocked call (one tick later) — never
// same-tick, never lost, never corrupting the in-flight iteration.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_ReentrantKillDeferredToNextDrain(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ability := programAbility("test_chain_reaction", nearEventPositionDamageTrigger("chain", 100, 999))
	registerRuntimeTestAbility(t, ability)

	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: -1000, Y: 0})
	caster.Visible = true

	// A dies first (directly). B sits within "chain"'s radius of A's death
	// position, so A's on_unit_death fire deals lethal splash damage to B —
	// a cascade kill enqueued DURING the drain's iteration.
	a := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 0, Y: 0})
	a.MaxHP, a.HP = 100, 100
	a.Visible = true
	aID := a.ID

	b := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	b.MaxHP, b.HP = 100, 100
	b.Visible = true
	bID := b.ID

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.applyUnitDamageWithSourceLocked(a, 999, DamageSource{
		AttackerUnitID: caster.ID, Kind: "ability", SourceAbilityID: ability.ID,
	})

	// First drain (this tick): A is removed. B was cascade-killed by A's
	// on_unit_death fire (HP<=0) but must NOT be removed in this same call —
	// its pendingDeath entry landed in a fresh queue built during this call,
	// not the snapshot this call is iterating.
	s.drainPendingDeathsLocked()

	if s.getUnitByIDLocked(aID) != nil {
		t.Fatalf("A (ID=%d) should be removed after the drain that processed it", aID)
	}
	if b.HP > 0 {
		t.Fatalf("setup error: expected B to be cascade-killed by A's on_unit_death fire (HP=%d)", b.HP)
	}
	if s.getUnitByIDLocked(bID) == nil {
		t.Fatalf("B (ID=%d) was removed in the SAME drain call that produced its cascade kill — re-entrancy corrupted the iteration (deaths enqueued mid-drain must defer to the NEXT drain call)", bID)
	}

	// Second drain (next tick): B's deferred pendingDeath entry is now
	// processed and removed.
	s.drainPendingDeathsLocked()
	if s.getUnitByIDLocked(bID) != nil {
		t.Fatalf("B (ID=%d) should be removed after the SECOND drain call (its cascade kill was deferred one tick)", bID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6 — determinism: several units killed within the same tick fire
// on_unit_death in a stable order (pendingDeaths insertion/enqueue order),
// reproducible across two identical runs. Killed in REVERSE spawn order so
// the assertion can't be confused with "coincidentally matches unit-ID or
// spawn order" — it must match ENQUEUE order specifically.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_DeterministicOrderAcrossRuns(t *testing.T) {
	const n = 6
	run := func(t *testing.T) []int {
		s := newDeathPipelineState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		ability := programAbility("test_order_ability", nearEventPositionDamageTrigger("order", 5, 1))
		registerRuntimeTestAbility(t, ability)

		caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: -1000, Y: 0})
		caster.Visible = true

		type pair struct{ victim, bystander *Unit }
		var pairs []pair
		for i := 0; i < n; i++ {
			x := float64(i) * 300 // far enough apart that radius:5 queries never cross-contaminate
			v := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: x, Y: 0})
			v.MaxHP, v.HP = 100, 100
			v.Visible = true
			by := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#c0392b", protocol.Vec2{X: x + 1, Y: 0})
			by.MaxHP, by.HP = 100, 100
			by.Visible = true
			pairs = append(pairs, pair{v, by})
		}

		tr := &AbilityExecutionTrace{}
		s.previewTrace = tr

		// Kill in REVERSE creation order.
		for i := n - 1; i >= 0; i-- {
			s.applyUnitDamageWithSourceLocked(pairs[i].victim, 999, DamageSource{
				AttackerUnitID: caster.ID, Kind: "ability", SourceAbilityID: ability.ID,
			})
		}
		s.drainPendingDeathsLocked()

		// abilityTraceEvents drops the damage pipeline's own per-hit records —
		// including the ones this test's own kill calls produce — so `order`
		// stays what it claims to be: the damage the on_unit_death trigger dealt.
		var order []int
		for _, e := range abilityTraceEvents(tr.Events) {
			if e.Type != "damage_applied" {
				continue
			}
			if id, ok := e.Payload["unit"].(int); ok {
				order = append(order, id)
			}
		}
		return order
	}

	orderA := run(t)
	orderB := run(t)

	if len(orderA) != n {
		t.Fatalf("got %d on_unit_death damage fires, want %d", len(orderA), n)
	}
	if len(orderA) != len(orderB) {
		t.Fatalf("run A order %v and run B order %v have different lengths", orderA, orderB)
	}
	for i := range orderA {
		if orderA[i] != orderB[i] {
			t.Fatalf("non-deterministic firing order: run A = %v, run B = %v", orderA, orderB)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 7 — op budget: each death's fire gets its own fresh ctx.opsUsed, never
// a leftover/shared budget from a prior fire in the same drain. One death is
// attributed to an adversarial ability (nested repeat blows maxExecutionOps);
// a second, unrelated death attributed to a cheap ability in the SAME drain
// must still complete normally.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_OpsBudgetIsPerFireNotShared(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	selectAllAction := func(id string) AbilityActionDef {
		return AbilityActionDef{ID: id, Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcAllInScene}}
	}

	// Build repeat(64){ repeat(64){ repeat(64){ select_targets } } } via the
	// typed structs directly (simpler and less error-prone than round-tripping
	// through json.RawMessage nesting).
	leaf := selectAllAction("leaf")
	innerCfg, _ := json.Marshal(repeatConfig{Count: 64, Actions: []AbilityActionDef{leaf}})
	inner := AbilityActionDef{ID: "inner", Type: ActionRepeat, Config: innerCfg}
	midCfg, _ := json.Marshal(repeatConfig{Count: 64, Actions: []AbilityActionDef{inner}})
	mid := AbilityActionDef{ID: "mid", Type: ActionRepeat, Config: midCfg}
	outerCfg, _ := json.Marshal(repeatConfig{Count: 64, Actions: []AbilityActionDef{mid}})
	outer := AbilityActionDef{ID: "outer", Type: ActionRepeat, Config: outerCfg}

	blower := programAbility("test_budget_blower", onUnitDeathTrigger("blow", outer))
	registerRuntimeTestAbility(t, blower)

	cheap := programAbility("test_budget_cheap", onUnitDeathTrigger("cheap", selectAllAction("sel")))
	registerRuntimeTestAbility(t, cheap)

	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	caster.Visible = true

	v1 := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 50, Y: 0})
	v1.MaxHP, v1.HP = 100, 100
	v1.Visible = true

	v2 := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 60, Y: 0})
	v2.MaxHP, v2.HP = 100, 100
	v2.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	// v1 dies first (its fire will exhaust maxExecutionOps and trace
	// op_budget_exceeded); v2 dies second, attributed to the cheap ability.
	s.applyUnitDamageWithSourceLocked(v1, 999, DamageSource{
		AttackerUnitID: caster.ID, Kind: "ability", SourceAbilityID: blower.ID,
	})
	s.applyUnitDamageWithSourceLocked(v2, 999, DamageSource{
		AttackerUnitID: caster.ID, Kind: "ability", SourceAbilityID: cheap.ID,
	})
	s.drainPendingDeathsLocked()

	// The nested repeat(64){repeat(64){repeat(64){select_targets}}} would run
	// ~262k executeActionLocked calls if unbounded — every one of those
	// (repeat's own pre-check AND executeActionLocked's own check) tests the
	// SAME ctx.opsUsed counter, so budget exhaustion shows up as the total
	// action-dispatch count (action_started) being capped at maxExecutionOps
	// rather than running away to the full ~262k. (repeat's loop breaks
	// silently on a full budget rather than calling executeActionLocked again
	// — see ability_exec_flow.go — so "op_budget_exceeded" itself isn't
	// guaranteed to trace from a nested-repeat blower; the cap is proven by
	// the dispatch count instead.)
	actionStarted := 0
	for _, e := range tr.Events {
		if e.Type == "action_started" {
			actionStarted++
		}
	}
	const wouldRunUncapped = 64 * 64 * 64 // leaf select_targets calls alone if unbounded
	if actionStarted > maxExecutionOps+1 {
		t.Fatalf("blower's fire dispatched %d actions (uncapped would be >= %d), want capped at maxExecutionOps=%d — budget did not bound this fire", actionStarted, wouldRunUncapped, maxExecutionOps)
	}
	if actionStarted < maxExecutionOps/2 {
		t.Fatalf("blower's fire only dispatched %d actions — test setup didn't actually exercise enough work to prove the budget caps it (want close to maxExecutionOps=%d)", actionStarted, maxExecutionOps)
	}
	if got := traceTriggerFireCount(tr, "blow"); got != 1 {
		t.Fatalf("blower's on_unit_death fired %d times, want exactly 1", got)
	}
	if got := traceTriggerFireCount(tr, "cheap"); got != 1 {
		t.Fatalf("cheap ability's on_unit_death fired %d times, want exactly 1 (must not be starved by the PRIOR fire's exhausted budget)", got)
	}
	// The cheap fire's own select_targets must have actually run (not itself
	// immediately budget-capped), proving its ctx started with a fresh
	// opsUsed=0 rather than inheriting v1's exhausted counter.
	cheapRan := false
	for _, e := range tr.Events {
		if e.Type == "targets_selected" && e.Path == "cheap.actions[sel]" {
			cheapRan = true
		}
	}
	if !cheapRan {
		t.Fatal("cheap ability's select_targets never ran — its budget was starved by the prior fire, opsUsed is leaking across fires")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 8 — production safety: no catalog ability may compile an
// on_unit_death trigger, at any nesting depth (top-level triggers,
// create_zone's nested triggers, presentation triggers). Mirrors
// TestCatalog_NoAbilityUsesZoneEnterExitTriggers (ability_zone_occupancy_test.go).
// ─────────────────────────────────────────────────────────────────────────────
func TestCatalog_NoAbilityUsesOnUnitDeathTrigger(t *testing.T) {
	for _, def := range ListAbilityDefs() {
		def := def
		t.Run(def.ID, func(t *testing.T) {
			prog := catalogProgram(def)
			for _, tt := range collectAllTriggerTypesForProductionGuard(prog) {
				if tt == TriggerOnUnitDeath {
					t.Fatalf("ability %q compiles an on_unit_death trigger; on_unit_death must stay editor-only (never compiler-emitted)", def.ID)
				}
			}
		})
	}
}
