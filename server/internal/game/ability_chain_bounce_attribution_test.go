package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Chain-bounce ability attribution — closes the KNOWN GAP documented at
// DamageSource.SourceAbilityID (damage_pipeline.go): chain_lightning-shaped
// abilities (launch_projectile + chainCount>0) route through the equipment-
// proc beam-bounce mechanism (executeProcEffectLocked -> fireProcBeamLocked ->
// Beam -> applyBeamPendingDamageLocked). Before this fix that path had no
// ability-attribution field, so a bounce-killed victim never fired the
// killing ability's on_unit_death trigger even though a directly-killed
// primary target did.
//
// Threaded: ProcSource.SourceAbilityID (proc_effects.go) -> Beam.SourceAbilityID
// (beam.go) -> applyBeamPendingDamageLocked's DamageSource -> drainPendingDeathsLocked
// -> fireOnUnitDeathLocked. Set at the one ability call site,
// fireAbilityChainLocked (ability_cast.go), from def.ID. Left empty at every
// non-ability proc call site (procSourceFromUnit, state_combat.go's two
// on-hit sites) so equipment/item procs are unaffected.
// ═════════════════════════════════════════════════════════════════════════════

// buildChainBounceTestAbility compiles a legacy-shaped, chain_lightning-like
// AbilityDef (DamageAmount + Projectile + ChainCount) into a SchemaVersion:2
// Program via the SAME compiler the "convert to composable" flow uses, then
// appends an on_unit_death trigger. Legacy mechanic fields are zeroed after
// compiling (mirroring TestLiveCast_SchemaV2_UnitHeal_RoutesToExecutor's
// discipline) so the ONLY way damage can land is through the compiled
// launch_projectile action — proving this exercises the executor path, not a
// legacy fallback.
func buildChainBounceTestAbility(t *testing.T, id string, baseDamage int, falloff int, bounceRange float64, triggers ...AbilityTriggerDef) AbilityDef {
	t.Helper()
	legacy := AbilityDef{
		ID:                  id,
		Type:                AbilitySpell,
		CanTargetEnemies:    true,
		CastRange:           CastRange(500),
		CastTime:            0,
		ManaCost:            0,
		DamageAmount:        baseDamage,
		DamageType:          DamagePhysical,
		Projectile:          "lightning_bolt",
		ChainCount:          1,
		BounceRange:         bounceRange,
		BounceDamageFalloff: falloff,
		TargetCount:         1,
	}
	v2 := legacy
	v2.SchemaVersion = 2
	v2.Program = compileLegacyAbility(legacy)
	v2.Program.Triggers = append(v2.Program.Triggers, triggers...)
	// Zero the legacy mechanic fields: only the compiled Program's Config
	// carries them from here on (see ConvertLegacyAbility's discipline).
	v2.DamageAmount = 0
	v2.ChainCount = 0
	v2.BounceRange = 0
	v2.BounceDamageFalloff = 0
	v2.Projectile = ""
	return v2
}

// castAndLandChainBounce casts abilityID at primary and ticks beams until the
// deferred bounce damage has landed, then drains the death queue.
func castAndLandChainBounce(t *testing.T, s *GameState, caster, primary *Unit, abilityID string) {
	t.Helper()
	ok, reason := s.beginAbilityCastLocked(caster, abilityID, primary)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(%q) failed: %q", abilityID, reason)
	}
	// CastTime is 0 ⇒ resolves synchronously inside beginAbilityCastLocked;
	// only the deferred beam damage still needs ticks to land.
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		s.tickBeamsLocked(0.05)
	}
	s.drainPendingDeathsLocked()
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1 (THE GAP): an authored ability using launch_projectile with
// chainCount>0 fires on_unit_death for a victim killed by a BOUNCE, not just
// the primary target. Primary survives (huge HP); the bounce hop's
// falloff-reduced damage is what kills the second victim — proving
// attribution flows all the way through the bounce hop specifically, not
// just the primary hit.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_FiresForChainBounceKill(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	const (
		baseDamage  = 500
		falloff     = 10
		bounceRange = 150.0
	)
	ability := buildChainBounceTestAbility(t, "test_chain_bounce_on_death", baseDamage, falloff, bounceRange, onUnitDeathTrigger("on_kill"))
	registerRuntimeTestAbility(t, ability)

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{ability.ID}
	caster.MaxMana, caster.CurrentMana = 100, 100

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	primary.MaxHP, primary.HP = 1_000_000, 1_000_000 // survives the primary hit
	primary.MoveSpeed = 0

	bounceVictim := spawnProjTestUnit(t, s, enemyPlayerID, 400, 100) // 100px from primary, within bounceRange
	bounceVictim.MaxHP, bounceVictim.HP = 5, 5                       // dies to the falloff-reduced bounce hit (500-10=490)
	bounceVictim.MoveSpeed = 0

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	castAndLandChainBounce(t, s, caster, primary, ability.ID)

	if primary.HP <= 0 {
		t.Fatalf("setup error: primary should have survived the direct hit (HP=%d)", primary.HP)
	}
	if s.getUnitByIDLocked(bounceVictim.ID) != nil {
		t.Fatalf("setup error: bounce victim (ID=%d) should have died from the chain bounce", bounceVictim.ID)
	}
	if got := traceTriggerFireCount(tr, "on_kill"); got != 1 {
		t.Fatalf("on_unit_death fired %d times for the bounce-killed victim, want exactly 1 (chain-bounce attribution gap)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: a non-ability proc (equipment on-hit / item proc) that chain-bounce
// kills does NOT fire any watched ability's on_unit_death — the zero-value
// ProcSource.SourceAbilityID path stays correct. Drives the exact same
// beam-bounce mechanism (executeProcEffectLocked -> fireProcBeamLocked) an
// equipment lightning_chain proc would use, via procSourceFromUnit (no
// ability id supplied), proving the widening is additive-only.
// ─────────────────────────────────────────────────────────────────────────────
func TestAbilityOnUnitDeath_DoesNotFireForNonAbilityProcBounceKill(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	watched := programAbility("test_watched_for_proc", onUnitDeathTrigger("on_kill"))
	registerRuntimeTestAbility(t, watched)

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true

	primary := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 100, Y: 0})
	primary.MaxHP, primary.HP = 1_000_000, 1_000_000
	primary.Visible = true

	bounceVictim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 150, Y: 0})
	bounceVictim.MaxHP, bounceVictim.HP = 5, 5
	bounceVictim.Visible = true

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	// Equipment-shaped proc fire: no ability id in the ProcSource (the
	// zero-value contract).
	src := procSourceFromUnit(attacker)
	s.executeProcEffectLocked(src, primary, ProcEffectParams{
		Damage: 500, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		BounceCount: 1, BounceRange: 200, BounceDamageFalloff: 10,
	})
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		s.tickBeamsLocked(0.05)
	}
	s.drainPendingDeathsLocked()

	if primary.HP <= 0 {
		t.Fatalf("setup error: primary should have survived the direct hit (HP=%d)", primary.HP)
	}
	if s.getUnitByIDLocked(bounceVictim.ID) != nil {
		t.Fatalf("setup error: bounce victim should have died from the chain bounce")
	}
	if got := traceTriggerFireCount(tr, "on_kill"); got != 0 {
		t.Fatalf("watched ability's on_unit_death fired %d times for a non-ability proc's bounce kill, want 0 (SourceAbilityID must stay empty for equipment/item procs)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: direct unit test of the AUTHORED plumbing — every launch_beam hop
// compileChainLightningActions emits (primary + every bounce) stamps its
// ability id onto the spawned Beam so deal_damage's Execute
// (ability_program_registry.go) folds it into DamageSource.SourceAbilityID
// when the hop's on_beam_impact fires. This is the composable-path analogue
// of the old direct fireAbilityChainLocked unit test (which stamped
// ProcSource.SourceAbilityID onto every Beam.SourceAbilityID up front, since
// that legacy seam resolves the whole chain inline in one call): the
// authored chain instead spawns ONE beam per hop, sequentially, a tick apart
// (see compileChainLightningActions' doc comment, ability_compile.go), so
// this test walks the chain tick-by-tick and checks every beam it observes —
// proving the stamp holds on EVERY hop, not just the primary — via
// Beam.AbilityIDForCtx (the composable-launch_beam field, distinct from the
// legacy Beam.SourceAbilityID field fireAbilityChainLocked's proc route
// still uses — see Test 2 above).
// ─────────────────────────────────────────────────────────────────────────────
func TestChainLightningAuthoredChain_StampsAbilityIDOnEveryHop(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	const (
		baseDamage  = 100
		falloff     = 10
		bounceRange = 200.0
	)
	ability := buildChainBounceTestAbility(t, "test_stamp_ability", baseDamage, falloff, bounceRange)
	registerRuntimeTestAbility(t, ability)

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{ability.ID}
	caster.MaxMana, caster.CurrentMana = 100, 100

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	primary.MaxHP, primary.HP = 1_000_000, 1_000_000
	primary.MoveSpeed = 0

	bounce := spawnProjTestUnit(t, s, enemyPlayerID, 400, 100) // 100px from primary, within bounceRange
	bounce.MaxHP, bounce.HP = 1_000_000, 1_000_000
	bounce.MoveSpeed = 0

	ok, reason := s.beginAbilityCastLocked(caster, ability.ID, primary)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(%q) failed: %q", ability.ID, reason)
	}

	seenBeamIDs := map[string]bool{}
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		for _, b := range s.Beams {
			if !seenBeamIDs[b.ID] {
				seenBeamIDs[b.ID] = true
				if b.AbilityIDForCtx != ability.ID {
					t.Errorf("beam %s (target=%d) AbilityIDForCtx = %q, want %q", b.ID, b.TargetUnitID, b.AbilityIDForCtx, ability.ID)
				}
			}
		}
		s.tickBeamsLocked(0.05)
	}
	if len(s.Beams) != 0 {
		t.Fatalf("chain left %d beam(s) unresolved after 40 ticks", len(s.Beams))
	}
	if len(seenBeamIDs) != 2 {
		t.Fatalf("expected exactly 2 beams (primary + 1 bounce) across the chain's lifetime, observed %d", len(seenBeamIDs))
	}
}

// A chain bounce beam must VISUALLY ORIGINATE from the PREVIOUS victim, not the
// original caster: Beam.CasterUnitID drives the client's origin-lift sprite
// lookup, so a bounce whose CasterUnitID is the caster lifts its start from the
// caster's chest instead of the previous victim's — making the bounce look like
// it springs from the wrong place instead of continuing the chain from where
// the incoming bolt landed. Legacy set CasterUnitID to the previous victim
// (spawnMomentaryDamageBeamLocked(cursor.ID)); the authored path reproduces
// that via originUnitForSpawnLocked(SpawnOrigin=current_event_position).
func TestChainLightningAuthoredChain_BounceOriginatesFromPreviousVictim(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	ability := buildChainBounceTestAbility(t, "test_bounce_origin", 100, 10, 200.0)
	registerRuntimeTestAbility(t, ability)

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{ability.ID}
	caster.MaxMana, caster.CurrentMana = 100, 100

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	primary.MaxHP, primary.HP = 1_000_000, 1_000_000
	primary.MoveSpeed = 0

	bounce := spawnProjTestUnit(t, s, enemyPlayerID, 400, 100) // within bounceRange of primary
	bounce.MaxHP, bounce.HP = 1_000_000, 1_000_000
	bounce.MoveSpeed = 0

	ok, reason := s.beginAbilityCastLocked(caster, ability.ID, primary)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(%q) failed: %q", ability.ID, reason)
	}

	// Record each beam's visual-origin unit (CasterUnitID) the first time it
	// appears, keyed by the enemy it targets.
	originByTarget := map[int]int{}
	seen := map[string]bool{}
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		for _, b := range s.Beams {
			if !seen[b.ID] {
				seen[b.ID] = true
				originByTarget[b.TargetUnitID] = b.CasterUnitID
			}
		}
		s.tickBeamsLocked(0.05)
	}

	// Hop 0 (caster -> primary): originates from the caster.
	if got := originByTarget[primary.ID]; got != caster.ID {
		t.Errorf("primary-hit beam CasterUnitID = %d, want caster %d", got, caster.ID)
	}
	// Hop 1 (primary -> bounce): must originate from the PREVIOUS VICTIM
	// (primary), NOT the caster — the chain-continuity fix.
	if got := originByTarget[bounce.ID]; got != primary.ID {
		t.Errorf("bounce beam CasterUnitID = %d, want previous victim %d (not caster %d) — chain would render discontinuous", got, primary.ID, caster.ID)
	}
}
