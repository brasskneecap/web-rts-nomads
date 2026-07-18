package game

import "testing"

// ═════════════════════════════════════════════════════════════════════════════
// launch_beam — the beam analogue of launch_projectile. This is Task 1 of the
// composable-beam migration plan: prove the generic primitive (spawn a
// momentary beam at a resolved target, then run a nested on_beam_impact
// trigger a beat later) with a single, non-chaining beam. Later tasks migrate
// chain_lightning and siphon_life onto this same seam.
// ═════════════════════════════════════════════════════════════════════════════

// TestLaunchBeam_ImpactDamageAfterDelay proves the full spawn -> deferred
// impact pipeline: a synthetic on_cast_complete trigger launches a beam at
// the initial target; its damage must NOT land immediately (it is deferred by
// ImpactDelaySeconds, exactly like a momentary proc beam's PendingDamage), and
// must land EXACTLY ONCE once tickBeamsLocked advances past the delay — never
// again on a later tick.
func TestLaunchBeam_ImpactDamageAfterDelay(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)
	target.HP = 200

	beamCfg := beamConfig{
		Variant:            "test_beam",
		ImpactDelaySeconds: 0.1,
		Triggers: []AbilityTriggerDef{
			{
				ID:   "impact",
				Type: TriggerOnBeamImpact,
				Actions: []AbilityActionDef{
					{
						ID:     "dmg",
						Type:   ActionDealDamage,
						Target: &TargetQueryDef{Source: SrcCurrentEvent},
						Config: marshalConfig(dealDamageConfig{Amount: 40}),
					},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "beam", Type: ActionBeam, Target: &TargetQueryDef{Source: SrcInitialTarget}, Config: marshalConfig(beamCfg)},
			}},
		},
	}

	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     "test_launch_beam",
		InitialTarget: target.ID,
		Named:         map[string]ContextValue{},
	}
	ctx.program = prog

	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if len(s.Beams) != 1 {
		t.Fatalf("want exactly 1 beam spawned, got %d", len(s.Beams))
	}
	if target.HP != 200 {
		t.Fatalf("target.HP = %d immediately after launch; want unchanged 200 (impact damage is deferred)", target.HP)
	}

	// Advance past the 0.1s delay: the deferred impact must fire exactly once.
	s.tickBeamsLocked(0.2)
	if target.HP != 160 {
		t.Fatalf("target.HP = %d after delay elapsed; want 160 (200 - 40 impact damage)", target.HP)
	}

	// Advance again: the impact must not re-fire (no double-apply).
	s.tickBeamsLocked(0.2)
	if target.HP != 160 {
		t.Fatalf("target.HP = %d after a second tick; want unchanged 160 (impact must fire exactly once)", target.HP)
	}
}

// TestLaunchBeam_ImpactFiresThroughExpirySafetyNet proves tickBeamsLocked's
// expiry safety net (beam.go): when a beam's flash (DurationMs) expires
// BEFORE its ImpactDelaySeconds countdown reaches zero, the impact must still
// fire — exactly once — through the RemainingSeconds<=0 branch, mirroring
// applyBeamPendingDamageLocked's identical "never silently drop a rolled
// proc" safety net for the legacy PendingDamage path. A single tick that
// crosses the (short) flash duration but not yet the (longer) impact delay
// must land the damage; the beam is then dropped (RemainingSeconds<=0
// removes it from s.Beams), so a further tick trivially cannot re-apply it.
func TestLaunchBeam_ImpactFiresThroughExpirySafetyNet(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)
	target.HP = 200

	beamCfg := beamConfig{
		Variant: "test_beam",
		// DurationMs (50ms flash) is deliberately shorter than
		// ImpactDelaySeconds (200ms): the flash expires long before the
		// countdown-driven impact would ever fire on its own.
		DurationMs:         50,
		ImpactDelaySeconds: 0.2,
		Triggers: []AbilityTriggerDef{
			{
				ID:   "impact",
				Type: TriggerOnBeamImpact,
				Actions: []AbilityActionDef{
					{
						ID:     "dmg",
						Type:   ActionDealDamage,
						Target: &TargetQueryDef{Source: SrcCurrentEvent},
						Config: marshalConfig(dealDamageConfig{Amount: 40}),
					},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "beam", Type: ActionBeam, Target: &TargetQueryDef{Source: SrcInitialTarget}, Config: marshalConfig(beamCfg)},
			}},
		},
	}

	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     "test_launch_beam",
		InitialTarget: target.ID,
		Named:         map[string]ContextValue{},
	}
	ctx.program = prog

	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)

	if len(s.Beams) != 1 {
		t.Fatalf("want exactly 1 beam spawned, got %d", len(s.Beams))
	}

	// A single 0.1s tick crosses the 0.05s flash duration (expiring it) but
	// NOT the 0.2s impact delay (0.1 < 0.2) — the countdown branch alone
	// would never fire the impact here; only the expiry safety net does.
	s.tickBeamsLocked(0.1)
	if target.HP != 160 {
		t.Fatalf("target.HP = %d after the flash expired; want 160 (200 - 40 impact damage, landed via the expiry safety net)", target.HP)
	}
	if len(s.Beams) != 0 {
		t.Fatalf("want the expired beam removed from s.Beams, got %d remaining", len(s.Beams))
	}

	// Further ticks must not re-apply the damage (the beam is already gone).
	s.tickBeamsLocked(0.1)
	if target.HP != 160 {
		t.Fatalf("target.HP = %d after a further tick; want unchanged 160 (impact must fire exactly once)", target.HP)
	}
}
