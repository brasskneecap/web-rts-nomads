package game

import (
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Phase 6b, Task 2 — on_animation_marker scheduler
//
// scheduleMarkerTriggersLocked / tickAbilityMarkersLocked let a
// play_presentation action's on_animation_marker triggers fire on a LATER
// tick (fireAtSimTime = s.simTime-at-enqueue + Timing.DelaySeconds) instead
// of synchronously, matching how a real animation's marker actually lands
// mid-playback. Every test here drives the scheduler directly via
// scheduleMarkerTriggersLocked (mirroring how play_presentation's Execute
// will call it) rather than through the full cast path, keeping these tests
// focused on the scheduler's own timing/dedup/resolution behavior.
// ═════════════════════════════════════════════════════════════════════════════

// markerTestAbility builds a minimal SchemaVersion>=2 ability whose single
// presentation has one on_animation_marker trigger ("impact", delaySeconds)
// running one deal_damage action against ctx.InitialTarget (Input:
// {"targets": {Key: "initial_target"}}, the same ContextRef resolution path
// resolveTargetRef already covers).
func markerTestAbility(id string, delaySeconds float64, amount int) AbilityDef {
	return AbilityDef{
		ID:            id,
		SchemaVersion: 2,
		Program: &AbilityProgram{
			Presentations: []PresentationInstanceDef{
				{
					ID: "p",
					Triggers: []AbilityTriggerDef{
						{
							ID:     "impact",
							Type:   TriggerOnAnimationMarker,
							Timing: &TriggerTiming{Marker: "impact", DelaySeconds: delaySeconds},
							Actions: []AbilityActionDef{
								{
									ID:     "dmg",
									Type:   ActionDealDamage,
									Input:  map[string]ContextRef{"targets": {Key: "initial_target"}},
									Config: marshalConfig(dealDamageConfig{Amount: amount, Type: DamagePhysical}),
								},
							},
						},
					},
				},
			},
		},
	}
}

// TestMarkerScheduler_FiresAfterDelay proves a scheduled marker does NOT fire
// before its delay has elapsed and DOES fire on the tick simTime crosses it.
func TestMarkerScheduler_FiresAfterDelay(t *testing.T) {
	s := setupHostileTargetingPair(t)

	// Far enough apart that regular auto-combat never engages them, so the
	// only HP change possible is the scheduled marker's deal_damage.
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 2000, 2000)
	enemyMaxHP := enemy.HP

	def := markerTestAbility("marker_fires_after_delay", 0.3, 50)
	registerRuntimeTestAbility(t, def)

	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     def.ID,
		InitialTarget: enemy.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	s.scheduleMarkerTriggersLocked(ctx, def.Program.Presentations[0])
	if len(s.pendingMarkers) != 1 {
		t.Fatalf("len(s.pendingMarkers) = %d; want 1 right after scheduling", len(s.pendingMarkers))
	}
	s.mu.Unlock()

	// 5 ticks * 0.05s = 0.25s < 0.3s delay: no damage yet.
	tickN(s, 5)
	s.mu.RLock()
	hpBeforeFire := enemy.HP
	s.mu.RUnlock()
	if hpBeforeFire != enemyMaxHP {
		t.Fatalf("enemy.HP = %d before delay elapsed; want unchanged %d (simTime=0.25 < delay 0.3)", hpBeforeFire, enemyMaxHP)
	}

	// One more tick: simTime = 0.30s >= 0.3s delay -> fires.
	tickN(s, 1)
	s.mu.RLock()
	hpAfterFire := enemy.HP
	s.mu.RUnlock()
	if hpAfterFire != enemyMaxHP-50 {
		t.Fatalf("enemy.HP = %d after delay elapsed; want %d (maxHP %d - 50 marker damage)", hpAfterFire, enemyMaxHP-50, enemyMaxHP)
	}
}

// TestMarkerScheduler_FiresOnce proves a fired marker is drained from
// s.pendingMarkers and never re-applies its damage on later ticks.
func TestMarkerScheduler_FiresOnce(t *testing.T) {
	s := setupHostileTargetingPair(t)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 2000, 2000)
	enemyMaxHP := enemy.HP

	def := markerTestAbility("marker_fires_once", 0.1, 50)
	registerRuntimeTestAbility(t, def)

	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     def.ID,
		InitialTarget: enemy.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	s.scheduleMarkerTriggersLocked(ctx, def.Program.Presentations[0])
	s.mu.Unlock()

	tickN(s, 5) // 0.25s: well past the 0.1s delay, fires once.
	s.mu.RLock()
	hpAfterFire := enemy.HP
	pendingAfterFire := len(s.pendingMarkers)
	s.mu.RUnlock()
	if hpAfterFire != enemyMaxHP-50 {
		t.Fatalf("enemy.HP = %d after first fire; want %d", hpAfterFire, enemyMaxHP-50)
	}
	if pendingAfterFire != 0 {
		t.Fatalf("len(s.pendingMarkers) = %d after fire; want 0 (drained)", pendingAfterFire)
	}

	tickN(s, 20) // another 1.0s of ticks must not re-apply damage.
	s.mu.RLock()
	hpFinal := enemy.HP
	s.mu.RUnlock()
	if hpFinal != enemyMaxHP-50 {
		t.Fatalf("enemy.HP = %d after further ticks; want unchanged %d (marker must fire exactly once)", hpFinal, enemyMaxHP-50)
	}
}

// TestMarkerScheduler_DropsWhenAbilityGone proves a scheduled marker whose
// abilityID cannot be resolved (never registered, or removed before fire
// time) is silently dropped at fire time: no panic, no damage, and the
// pending entry is still drained.
func TestMarkerScheduler_DropsWhenAbilityGone(t *testing.T) {
	s := setupHostileTargetingPair(t)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 2000, 2000)
	enemyMaxHP := enemy.HP

	// Deliberately NOT registered via registerRuntimeTestAbility, and not a
	// real catalog id either.
	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     "marker_ability_does_not_exist",
		InitialTarget: enemy.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	pres := PresentationInstanceDef{
		ID: "p",
		Triggers: []AbilityTriggerDef{
			{
				ID:     "impact",
				Type:   TriggerOnAnimationMarker,
				Timing: &TriggerTiming{Marker: "impact", DelaySeconds: 0.1},
				Actions: []AbilityActionDef{
					{
						ID:     "dmg",
						Type:   ActionDealDamage,
						Input:  map[string]ContextRef{"targets": {Key: "initial_target"}},
						Config: marshalConfig(dealDamageConfig{Amount: 50, Type: DamagePhysical}),
					},
				},
			},
		},
	}
	s.scheduleMarkerTriggersLocked(ctx, pres)
	s.mu.Unlock()

	tickN(s, 10) // well past the 0.1s delay

	s.mu.RLock()
	hp := enemy.HP
	pending := len(s.pendingMarkers)
	s.mu.RUnlock()
	if hp != enemyMaxHP {
		t.Fatalf("enemy.HP = %d; want unchanged %d (unresolvable ability must not deal damage)", hp, enemyMaxHP)
	}
	if pending != 0 {
		t.Fatalf("len(s.pendingMarkers) = %d; want 0 (dropped, still drained)", pending)
	}
}

// TestMarkerScheduler_TwoMarkers_FireInOrderByDelay proves two independently
// scheduled markers, enqueued LONGER-delay first, each land only after THEIR
// OWN delay has elapsed on two distinct enemies, and the shorter-delay
// marker's damage lands strictly before the longer-delay marker's — i.e.
// firing is gated by each entry's own fireAtSimTime, not by enqueue (slice)
// order.
func TestMarkerScheduler_TwoMarkers_FireInOrderByDelay(t *testing.T) {
	s := setupHostileTargetingPair(t)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemyShort := teamCombatUnit(t, s, "p2", 2000, 2000)
	enemyLong := teamCombatUnit(t, s, "p2", 2200, 2000)
	enemyShortMaxHP := enemyShort.HP
	enemyLongMaxHP := enemyLong.HP

	shortDef := markerTestAbility("marker_two_short", 0.1, 30)
	longDef := markerTestAbility("marker_two_long", 0.3, 40)
	registerRuntimeTestAbility(t, shortDef)
	registerRuntimeTestAbility(t, longDef)

	// Enqueue the LONGER delay first to prove firing order tracks
	// fireAtSimTime, not enqueue order.
	longCtx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     longDef.ID,
		InitialTarget: enemyLong.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	s.scheduleMarkerTriggersLocked(longCtx, longDef.Program.Presentations[0])

	shortCtx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     shortDef.ID,
		InitialTarget: enemyShort.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	s.scheduleMarkerTriggersLocked(shortCtx, shortDef.Program.Presentations[0])

	if len(s.pendingMarkers) != 2 {
		t.Fatalf("len(s.pendingMarkers) = %d; want 2 right after scheduling both", len(s.pendingMarkers))
	}
	s.mu.Unlock()

	// Well past the short delay (0.1s) but well short of the long delay
	// (0.3s): only the short marker has fired.
	tickN(s, 4) // 0.20s
	s.mu.RLock()
	shortHPMid := enemyShort.HP
	longHPMid := enemyLong.HP
	s.mu.RUnlock()
	if shortHPMid != enemyShortMaxHP-30 {
		t.Fatalf("enemyShort.HP = %d at simTime~0.20; want %d (short-delay marker must have fired)", shortHPMid, enemyShortMaxHP-30)
	}
	if longHPMid != enemyLongMaxHP {
		t.Fatalf("enemyLong.HP = %d at simTime~0.20; want unchanged %d (long-delay marker must not have fired yet)", longHPMid, enemyLongMaxHP)
	}

	// Well past the long delay too: both have now fired, and neither
	// re-fires.
	tickN(s, 10) // simTime ~0.70
	s.mu.RLock()
	shortHPFinal := enemyShort.HP
	longHPFinal := enemyLong.HP
	pendingFinal := len(s.pendingMarkers)
	s.mu.RUnlock()
	if shortHPFinal != enemyShortMaxHP-30 {
		t.Fatalf("enemyShort.HP = %d after both fired; want unchanged %d", shortHPFinal, enemyShortMaxHP-30)
	}
	if longHPFinal != enemyLongMaxHP-40 {
		t.Fatalf("enemyLong.HP = %d after long-delay marker's delay elapsed; want %d", longHPFinal, enemyLongMaxHP-40)
	}
	if pendingFinal != 0 {
		t.Fatalf("len(s.pendingMarkers) = %d after both fired; want 0 (drained)", pendingFinal)
	}
}

// TestMarkerScheduler_ZeroDelay_FiresPromptly pins the exact tick a
// zero-delay marker fires on: DelaySeconds == 0 makes fireAtSimTime equal to
// s.simTime AT SCHEDULING TIME, so the very first Update after scheduling
// (tickN(s, 1)) is guaranteed to have advanced simTime past it and fires the
// marker. This locks in the "no earlier than the first marker tick"
// half of scheduleMarkerTriggersLocked's timing doc so it can't silently
// regress to skipping an extra tick or firing before any tick has run.
func TestMarkerScheduler_ZeroDelay_FiresPromptly(t *testing.T) {
	s := setupHostileTargetingPair(t)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 2000, 2000)
	enemyMaxHP := enemy.HP

	def := markerTestAbility("marker_zero_delay", 0, 25)
	registerRuntimeTestAbility(t, def)

	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     def.ID,
		InitialTarget: enemy.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	s.scheduleMarkerTriggersLocked(ctx, def.Program.Presentations[0])
	s.mu.Unlock()

	tickN(s, 1)
	s.mu.RLock()
	hp := enemy.HP
	pending := len(s.pendingMarkers)
	s.mu.RUnlock()
	if hp != enemyMaxHP-25 {
		t.Fatalf("enemy.HP = %d after the first tick post-scheduling; want %d (zero-delay marker must fire on the first marker tick)", hp, enemyMaxHP-25)
	}
	if pending != 0 {
		t.Fatalf("len(s.pendingMarkers) = %d after first tick; want 0 (drained on fire)", pending)
	}
}

// reentrantMarkerAbility builds a two-presentation ability: "p1"'s marker
// (delay1) fires a play_presentation action whose PresentationID points at
// "p2" — the same play_presentation.Execute path (ability_exec_presentation.go)
// that resolves PresentationID and re-enters scheduleMarkerTriggersLocked for
// any on_animation_marker trigger it finds. "p2"'s marker (delay2) then runs
// a deal_damage action against ctx.InitialTarget. p1 itself deals no damage,
// so any HP change can only come from p2 having fired.
func reentrantMarkerAbility(id string, delay1, delay2 float64, amount int) AbilityDef {
	return AbilityDef{
		ID:            id,
		SchemaVersion: 2,
		Program: &AbilityProgram{
			Presentations: []PresentationInstanceDef{
				{
					ID: "p1",
					Triggers: []AbilityTriggerDef{
						{
							ID:     "impact1",
							Type:   TriggerOnAnimationMarker,
							Timing: &TriggerTiming{Marker: "impact1", DelaySeconds: delay1},
							Actions: []AbilityActionDef{
								{
									ID:   "chain",
									Type: ActionPlayPresentation,
									Config: marshalConfig(playPresentationAtPointConfig{
										Position:       ContextRef{Key: "castPoint"},
										PresentationID: "p2",
									}),
								},
							},
						},
					},
				},
				{
					ID: "p2",
					Triggers: []AbilityTriggerDef{
						{
							ID:     "impact2",
							Type:   TriggerOnAnimationMarker,
							Timing: &TriggerTiming{Marker: "impact2", DelaySeconds: delay2},
							Actions: []AbilityActionDef{
								{
									ID:     "dmg",
									Type:   ActionDealDamage,
									Input:  map[string]ContextRef{"targets": {Key: "initial_target"}},
									Config: marshalConfig(dealDamageConfig{Amount: amount, Type: DamagePhysical}),
								},
							},
						},
					},
				},
			},
		},
	}
}

// TestMarkerScheduler_ReEntrantChaining proves a fired marker's actions can
// themselves schedule a SECOND marker — via the real play_presentation
// PresentationID-chaining path, not a synthetic direct call — without the
// second marker being dropped or double-fired. p1's marker fires while
// tickAbilityMarkersLocked is still ranging over its pre-tick snapshot, and
// its play_presentation action calls scheduleMarkerTriggersLocked again for
// p2 DURING that same fire pass; this exercises the remaining /
// s.pendingMarkers[n:] retention tickAbilityMarkersLocked relies on to keep
// anything scheduled mid-loop from being silently discarded.
func TestMarkerScheduler_ReEntrantChaining(t *testing.T) {
	s := setupHostileTargetingPair(t)

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	enemy := teamCombatUnit(t, s, "p2", 2000, 2000)
	enemyMaxHP := enemy.HP

	const delay1, delay2, amount = 0.1, 1.0, 35
	def := reentrantMarkerAbility("marker_reentrant_chain", delay1, delay2, amount)
	registerRuntimeTestAbility(t, def)

	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     def.ID,
		InitialTarget: enemy.ID,
		Named:         map[string]ContextValue{},
		now:           s.simTime,
	}
	s.scheduleMarkerTriggersLocked(ctx, def.Program.Presentations[0])
	if len(s.pendingMarkers) != 1 {
		t.Fatalf("len(s.pendingMarkers) = %d; want 1 right after scheduling p1's marker", len(s.pendingMarkers))
	}
	s.mu.Unlock()

	// Well past p1's 0.1s delay: p1 fires and its play_presentation action
	// re-entrantly schedules p2's marker. p1 carries no damage action, so
	// enemy.HP must still be untouched, and the re-entrantly scheduled p2
	// marker must be the ONLY pending entry — not dropped, not duplicated.
	tickN(s, 4) // simTime ~0.20
	s.mu.RLock()
	hpAfterP1 := enemy.HP
	pendingAfterP1 := len(s.pendingMarkers)
	s.mu.RUnlock()
	if hpAfterP1 != enemyMaxHP {
		t.Fatalf("enemy.HP = %d right after p1 fires; want unchanged %d (p1 has no damage action)", hpAfterP1, enemyMaxHP)
	}
	if pendingAfterP1 != 1 {
		t.Fatalf("len(s.pendingMarkers) = %d right after p1 fires; want 1 (p2's re-entrantly scheduled marker, retained)", pendingAfterP1)
	}

	// Well short of p2's ~1.1s fireAtSimTime: still pending, not fired.
	tickN(s, 10) // simTime ~0.70
	s.mu.RLock()
	hpBeforeP2 := enemy.HP
	pendingBeforeP2 := len(s.pendingMarkers)
	s.mu.RUnlock()
	if hpBeforeP2 != enemyMaxHP {
		t.Fatalf("enemy.HP = %d at simTime~0.70; want unchanged %d (p2's marker must not fire before its own delay elapses)", hpBeforeP2, enemyMaxHP)
	}
	if pendingBeforeP2 != 1 {
		t.Fatalf("len(s.pendingMarkers) = %d at simTime~0.70; want 1 (p2 still pending)", pendingBeforeP2)
	}

	// Well past p2's fireAtSimTime: p2 fires on its own later tick.
	tickN(s, 15) // simTime ~1.45
	s.mu.RLock()
	hpAfterP2 := enemy.HP
	pendingFinal := len(s.pendingMarkers)
	s.mu.RUnlock()
	if hpAfterP2 != enemyMaxHP-amount {
		t.Fatalf("enemy.HP = %d after p2's delay elapsed; want %d (p2's chained deal_damage must fire)", hpAfterP2, enemyMaxHP-amount)
	}
	if pendingFinal != 0 {
		t.Fatalf("len(s.pendingMarkers) = %d after p2 fires; want 0 (drained)", pendingFinal)
	}

	// Further ticks must not re-apply p2's damage.
	tickN(s, 5)
	s.mu.RLock()
	hpFinal := enemy.HP
	s.mu.RUnlock()
	if hpFinal != enemyMaxHP-amount {
		t.Fatalf("enemy.HP = %d after further ticks; want unchanged %d (no double fire)", hpFinal, enemyMaxHP-amount)
	}
}

// TestMarkerScheduler_ProductionNoOp proves ordinary play — no ability ever
// calls scheduleMarkerTriggersLocked — leaves s.pendingMarkers empty across
// every tick, i.e. tickAbilityMarkersLocked is an inert no-op in production
// today (no v2 ability is authored with a marker-triggered presentation
// reachable from live play).
func TestMarkerScheduler_ProductionNoOp(t *testing.T) {
	s := setupHostileTargetingPair(t)
	a := teamCombatUnit(t, s, "p1", 400, 400)
	b := teamCombatUnit(t, s, "p2", 460, 400) // in range: real combat runs
	_ = a
	_ = b
	s.mu.Unlock()

	for i := 0; i < 60; i++ {
		s.Update(0.05)
		s.mu.RLock()
		n := len(s.pendingMarkers)
		s.mu.RUnlock()
		if n != 0 {
			t.Fatalf("len(s.pendingMarkers) = %d at tick %d; want 0 (production never schedules a marker)", n, i)
		}
	}
}
