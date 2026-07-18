package game

import (
	"encoding/json"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Golden equivalence tests for launch_projectile (arcane_bolt / fireball /
// chain_lightning — migrated to schemaVersion:2 in the live catalog).
//
// Same discipline as ability_compile_golden_test.go's five subjects: each
// test compares the FROZEN pre-migration fixture (ability_legacy_fixtures_
// test.go), driven through the legacy branches, against the REAL SHIPPED
// catalog def (requireMigratedV2), driven through the SAME production entry
// point (resolveAbilityCastLocked) — whose own SchemaVersion>=2 branch is
// what routes the executor leg to the executor. Nothing here is bespoke test
// wiring.
//
// Neither arcane_bolt's nor fireball's damage lands synchronously: both
// deliver it via a homing Projectile, ticked to impact via
// tickProjectilesLocked (mirroring fireball_test.go/adept_arcane_bolt_repro_
// test.go's own advance-to-impact pattern). chain_lightning never spawns a
// Projectile — it resolves as Beams with a short deferred-damage delay
// (ticked via tickBeamsLocked, mirroring chain_lightning_test.go).
// ═════════════════════════════════════════════════════════════════════════════

func buildGoldenProjectileScene(t *testing.T, mod *SpellModifier) (s *GameState, caster, enemy *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	if mod != nil {
		caster.SpellModifiers = []SpellModifier{*mod}
	}

	enemy = teamCombatUnit(t, s, "p2", 200, 0) // within castRange 400
	enemy.HP, enemy.MaxHP = 2000, 2000         // survives even the +50% modified hit
	enemy.MoveSpeed = 0

	return s, caster, enemy
}

// runToProjectileImpact advances s (lock held) until every in-flight
// Projectile has landed, or maxTicks is exhausted.
func runToProjectileImpact(s *GameState, maxTicks int) {
	for i := 0; i < maxTicks && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
}

// runToBeamImpact advances s (lock held) until every in-flight Beam
// (chain_lightning's deferred-damage bounce chain) has landed, or maxTicks is
// exhausted. Returns the number of ticks actually consumed — the authored
// chain unfolds SEQUENTIALLY (each hop's beam impact fires on a LATER tick
// than the one that spawned it, unlike legacy's single-call bounce loop), so
// callers that want to prove the chain fully resolved need both the tick
// count (for visibility) and a len(s.Beams)==0 check after the loop.
func runToBeamImpact(s *GameState, maxTicks int) int {
	i := 0
	for ; i < maxTicks && len(s.Beams) > 0; i++ {
		s.tickBeamsLocked(0.05)
	}
	return i
}

// ── arcane_bolt (single-target projectile, no splash, no chain) ────────────

func TestAbilityCompileGolden_ArcaneBolt(t *testing.T) {
	legacyDef := legacyArcaneBoltFixture()
	execDef := requireMigratedV2(t, "arcane_bolt")

	run := func(t *testing.T, mod *SpellModifier, label string) (wantDamage int) {
		sLegacy, casterL, enemyL := buildGoldenProjectileScene(t, mod)
		defer sLegacy.mu.Unlock()
		sExec, casterE, enemyE := buildGoldenProjectileScene(t, mod)
		defer sExec.mu.Unlock()

		preHPL, preHPE := enemyL.HP, enemyE.HP

		targetsL := sLegacy.buildCastTargetSetLocked(casterL, legacyDef, enemyL)
		sLegacy.resolveAbilityCastLocked(casterL, legacyDef, targetsL)
		runToProjectileImpact(sLegacy, 80)

		targetsE := sExec.buildCastTargetSetLocked(casterE, execDef, enemyE)
		sExec.resolveAbilityCastLocked(casterE, execDef, targetsE)
		runToProjectileImpact(sExec, 80)

		wantDamage = preHPL - enemyL.HP
		if wantDamage <= 0 {
			t.Fatalf("%s: legacy bolt never landed (enemy HP %d -> %d)", label, preHPL, enemyL.HP)
		}
		if got := preHPE - enemyE.HP; got != wantDamage {
			t.Fatalf("%s: executor bolt damage = %d, want %d (legacy)", label, got, wantDamage)
		}
		if len(sLegacy.Projectiles) != 0 || len(sExec.Projectiles) != 0 {
			t.Errorf("%s: projectile(s) still in flight after impact loop: legacy=%d exec=%d", label, len(sLegacy.Projectiles), len(sExec.Projectiles))
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
		return wantDamage
	}

	t.Run("unmodified_caster", func(t *testing.T) {
		got := run(t, nil, "arcane_bolt/unmodified")
		if got != legacyDef.DamageAmount {
			t.Fatalf("unmodified-caster damage = %d, want base damageAmount %d (no modifier active)", got, legacyDef.DamageAmount)
		}
	})
	t.Run("modified_caster", func(t *testing.T) {
		m := goldenDamageModifier(string(legacyDef.DamageType)) // "arcane" — matches arcane_bolt's own school
		got := run(t, &m, "arcane_bolt/modified")
		if got <= legacyDef.DamageAmount {
			t.Fatalf("modified-caster damage = %d, want > base damageAmount %d (the +50%% modifier should have scaled it up)", got, legacyDef.DamageAmount)
		}
	})
}

// ── fireball (splash: hits primary + nearby, excludes far enemy) ───────────

func TestAbilityCompileGolden_Fireball(t *testing.T) {
	legacyDef := legacyFireballFixture()
	execDef := requireMigratedV2(t, "fireball")
	if legacyDef.Radius <= 0 {
		t.Fatalf("fireball fixture drifted: Radius = %v, want > 0 (test is meaningless without splash)", legacyDef.Radius)
	}

	// buildGoldenFireballScene spawns a caster, a primary target, a near
	// enemy inside the splash radius, and a far enemy outside it. Returns
	// with s.mu held.
	build := func(t *testing.T, mod *SpellModifier) (s *GameState, caster, primary, near, far *Unit) {
		t.Helper()
		s = newProjectileTestState(t)
		s.mu.Lock()
		setTeam(s, "p1", 0)
		setTeam(s, "p2", 1)

		caster = teamCombatUnit(t, s, "p1", 0, 0)
		caster.MaxMana, caster.CurrentMana = 100, 100
		if mod != nil {
			caster.SpellModifiers = []SpellModifier{*mod}
		}

		primary = teamCombatUnit(t, s, "p2", 200, 0)
		primary.HP, primary.MaxHP = 2000, 2000
		primary.MoveSpeed = 0

		near = teamCombatUnit(t, s, "p2", 200+legacyDef.Radius*0.4, 0) // well inside the splash radius
		near.HP, near.MaxHP = 2000, 2000
		near.MoveSpeed = 0

		far = teamCombatUnit(t, s, "p2", 200, legacyDef.Radius*3) // well outside the splash radius
		far.HP, far.MaxHP = 2000, 2000
		far.MoveSpeed = 0

		return s, caster, primary, near, far
	}

	run := func(t *testing.T, mod *SpellModifier, label string) (wantPrimaryDamage int) {
		sLegacy, casterL, primaryL, nearL, farL := build(t, mod)
		defer sLegacy.mu.Unlock()
		sExec, casterE, primaryE, nearE, farE := build(t, mod)
		defer sExec.mu.Unlock()

		prePrimaryL, preNearL, preFarL := primaryL.HP, nearL.HP, farL.HP
		prePrimaryE, preNearE, preFarE := primaryE.HP, nearE.HP, farE.HP

		targetsL := sLegacy.buildCastTargetSetLocked(casterL, legacyDef, primaryL)
		sLegacy.resolveAbilityCastLocked(casterL, legacyDef, targetsL)
		runToProjectileImpact(sLegacy, 80)

		targetsE := sExec.buildCastTargetSetLocked(casterE, execDef, primaryE)
		sExec.resolveAbilityCastLocked(casterE, execDef, targetsE)
		runToProjectileImpact(sExec, 80)

		wantPrimaryDamage = prePrimaryL - primaryL.HP
		if wantPrimaryDamage <= 0 {
			t.Fatalf("%s: legacy bolt never landed on primary (HP %d -> %d)", label, prePrimaryL, primaryL.HP)
		}
		if preNearL-nearL.HP <= 0 {
			t.Fatalf("%s: legacy fixture drifted: near-splash enemy untouched (HP %d -> %d)", label, preNearL, nearL.HP)
		}
		if farL.HP != preFarL {
			t.Fatalf("%s: legacy fixture drifted: far enemy took damage (HP %d -> %d); should be outside splash radius", label, preFarL, farL.HP)
		}

		if got := prePrimaryE - primaryE.HP; got != wantPrimaryDamage {
			t.Fatalf("%s: executor primary damage = %d, want %d (legacy)", label, got, wantPrimaryDamage)
		}
		if got := preNearE - nearE.HP; got != preNearL-nearL.HP {
			t.Fatalf("%s: executor near-splash damage = %d, want %d (legacy)", label, got, preNearL-nearL.HP)
		}
		if farE.HP != preFarE {
			t.Fatalf("%s: executor far enemy HP = %d, want unchanged %d; should be outside splash radius", label, farE.HP, preFarE)
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
		return wantPrimaryDamage
	}

	t.Run("unmodified_caster", func(t *testing.T) {
		got := run(t, nil, "fireball/unmodified")
		if got != legacyDef.DamageAmount {
			t.Fatalf("unmodified-caster primary damage = %d, want base damageAmount %d (no modifier active)", got, legacyDef.DamageAmount)
		}
	})
	t.Run("modified_caster", func(t *testing.T) {
		m := goldenDamageModifier(string(legacyDef.DamageType)) // "fire" — matches fireball's own school
		got := run(t, &m, "fireball/modified")
		if got <= legacyDef.DamageAmount {
			t.Fatalf("modified-caster primary damage = %d, want > base damageAmount %d (the +50%% modifier should have scaled it up)", got, legacyDef.DamageAmount)
		}
	})
}

// ── chain_lightning (primary hit + falloff bounces, never spawns a Projectile) ──

func TestAbilityCompileGolden_ChainLightning(t *testing.T) {
	legacyDef := legacyChainLightningFixture()
	execDef := requireMigratedV2(t, "chain_lightning")
	if legacyDef.ChainCount < 2 {
		t.Fatalf("chain_lightning fixture drifted: ChainCount = %d, want >= 2 (test is meaningless with fewer than 2 bounces)", legacyDef.ChainCount)
	}

	// buildGoldenChainScene spawns a caster and a primary + two bounce-range
	// enemies daisy-chained by BounceRange, plus a far enemy outside range.
	// Returns with s.mu held.
	build := func(t *testing.T, mod *SpellModifier) (s *GameState, caster, primary, b1, b2, far *Unit) {
		t.Helper()
		s = newProjectileTestState(t)
		s.mu.Lock()
		setTeam(s, "p1", 0)
		setTeam(s, "p2", 1)

		caster = teamCombatUnit(t, s, "p1", 0, 0)
		caster.MaxMana, caster.CurrentMana = 100, 100
		if mod != nil {
			caster.SpellModifiers = []SpellModifier{*mod}
		}

		hop := legacyDef.BounceRange * 0.5 // well within bounce range each hop
		primary = teamCombatUnit(t, s, "p2", 200, 0)
		primary.HP, primary.MaxHP = 2000, 2000
		primary.MoveSpeed = 0

		b1 = teamCombatUnit(t, s, "p2", 200+hop, 0)
		b1.HP, b1.MaxHP = 2000, 2000
		b1.MoveSpeed = 0

		b2 = teamCombatUnit(t, s, "p2", 200+2*hop, 0)
		b2.HP, b2.MaxHP = 2000, 2000
		b2.MoveSpeed = 0

		far = teamCombatUnit(t, s, "p2", 200, legacyDef.BounceRange*5)
		far.HP, far.MaxHP = 2000, 2000
		far.MoveSpeed = 0

		return s, caster, primary, b1, b2, far
	}

	// chainBounceDamage returns the RAW bounce damage at bounce index k, computed
	// exactly as the runtime does (loopVarValue — additive/percent + rounding +
	// stepFirst), read from the migrated program's loop config. Deriving from the
	// authored data (not hardcoded numbers) keeps the test robust to balance
	// changes (step mode, iterations, stepFirst — per no-hardcoded-tunables).
	chainBounceDamage := func(t *testing.T, def AbilityDef, k int) int {
		t.Helper()
		for _, trig := range def.Program.Triggers {
			for _, act := range trig.Actions {
				if act.Type != ActionLoop {
					continue
				}
				var lc loopConfig
				if err := json.Unmarshal(act.Config, &lc); err != nil || len(lc.Vars) == 0 {
					t.Fatalf("chain_lightning loop action missing its variable: %v", err)
				}
				iter := k
				if lc.StepFirst {
					iter = k + 1
				}
				return int(loopVarValue(lc.Vars[0], iter))
			}
		}
		t.Fatalf("chain_lightning has no loop action")
		return 0
	}

	// chainPrimaryDamage reads the primary hit's amount — the first deal_damage
	// action in on_cast_complete, BEFORE the bounce loop.
	chainPrimaryDamage := func(t *testing.T, def AbilityDef) int {
		t.Helper()
		for _, trig := range def.Program.Triggers {
			for _, act := range trig.Actions {
				if act.Type != ActionDealDamage {
					continue
				}
				var dc dealDamageConfig
				if err := json.Unmarshal(act.Config, &dc); err != nil {
					t.Fatalf("chain_lightning primary deal_damage config: %v", err)
				}
				return dc.Amount
			}
		}
		t.Fatalf("chain_lightning has no primary deal_damage action")
		return 0
	}

	// chain_lightning has since been REBALANCED away from its legacy incarnation
	// (author-tuned: percent falloff, more bounces — the loop config is the
	// authority now), so this no longer asserts executor==legacy equivalence.
	// The legacy fixture is still run as an independent "legacy path still works"
	// regression check; the executor is validated against its OWN authored config
	// (primary + loopVarValue per bounce, folded) plus the structural invariants
	// (strict falloff, victim set/order, far untouched, iteration cap).
	run := func(t *testing.T, mod *SpellModifier, label string) (wantPrimary, want1, want2 int) {
		sLegacy, casterL, primaryL, b1L, b2L, farL := build(t, mod)
		defer sLegacy.mu.Unlock()
		sExec, casterE, primaryE, b1E, b2E, farE := build(t, mod)
		defer sExec.mu.Unlock()

		prePL, pre1L, pre2L, preFarL := primaryL.HP, b1L.HP, b2L.HP, farL.HP
		prePE, pre1E, pre2E, preFarE := primaryE.HP, b1E.HP, b2E.HP, farE.HP

		targetsL := sLegacy.buildCastTargetSetLocked(casterL, legacyDef, primaryL)
		sLegacy.resolveAbilityCastLocked(casterL, legacyDef, targetsL)
		if len(sLegacy.Projectiles) != 0 {
			t.Fatalf("%s: legacy chain_lightning spawned a Projectile — it must resolve as Beams only", label)
		}
		legacyTicks := runToBeamImpact(sLegacy, 40)
		if len(sLegacy.Beams) != 0 {
			t.Fatalf("%s: legacy chain_lightning left %d beam(s) unresolved after %d ticks", label, len(sLegacy.Beams), legacyTicks)
		}

		targetsE := sExec.buildCastTargetSetLocked(casterE, execDef, primaryE)
		sExec.resolveAbilityCastLocked(casterE, execDef, targetsE)
		if len(sExec.Projectiles) != 0 {
			t.Fatalf("%s: executor chain_lightning spawned a Projectile — it must resolve as Beams only", label)
		}
		// The AUTHORED chain is a `loop` action: the primary hit resolves
		// synchronously at cast, then each bounce is SCHEDULED ~0.12s after the
		// last (the body's wait) and fired by tickPendingLoopsLocked as simTime
		// advances — sequential-over-time, unlike legacy's single inline
		// fireAbilityChainLocked call. Proving parity requires driving that
		// scheduler until every iteration has fired; execTicks > 0 is the direct
		// evidence the bounces actually spanned multiple ticks.
		execTicks := 0
		for ; execTicks < 40 && len(sExec.pendingLoops) > 0; execTicks++ {
			sExec.simTime += 0.05
			sExec.tickPendingLoopsLocked()
		}
		if len(sExec.pendingLoops) != 0 {
			t.Fatalf("%s: executor chain_lightning left %d loop iteration(s) pending after %d ticks", label, len(sExec.pendingLoops), execTicks)
		}
		if execTicks <= 1 {
			t.Fatalf("%s: executor chain resolved in %d tick(s), want > 1 — the sequential-hop timing this test exercises did not happen", label, execTicks)
		}
		t.Logf("%s: legacy resolved in %d tick(s) (single inline call); executor resolved in %d tick(s) (scheduled per-hop loop)", label, legacyTicks, execTicks)

		wantPrimary = prePL - primaryL.HP
		want1 = pre1L - b1L.HP
		want2 = pre2L - b2L.HP
		if wantPrimary <= 0 || want1 <= 0 || want2 <= 0 {
			t.Fatalf("%s: legacy fixture drifted: expected primary+2 bounces hit; deltas primary=%d b1=%d b2=%d", label, wantPrimary, want1, want2)
		}
		if !(wantPrimary > want1 && want1 > want2) {
			t.Fatalf("%s: legacy fixture drifted: expected strict per-hop falloff primary>b1>b2; got %d,%d,%d", label, wantPrimary, want1, want2)
		}
		if farL.HP != preFarL {
			t.Fatalf("%s: legacy fixture drifted: far enemy took damage; should be outside bounce range", label)
		}

		gotPrimary := prePE - primaryE.HP
		got1 := pre1E - b1E.HP
		got2 := pre2E - b2E.HP

		// The authored chain deals a fixed primary hit (a direct deal_damage
		// BEFORE the loop) then fold(bounceDamage) per bounce, where the loop's
		// damage variable steps each iteration (additive or percent). Expectations
		// derive from the program's own primary amount + the runtime's own
		// loopVarValue, folded through the SAME seam deal_damage uses
		// (effectiveAbilityDamageLocked) — so this holds for an unmodified and a
		// modified caster, and for whatever step mode / stepFirst the catalog sets.
		primaryAmt := chainPrimaryDamage(t, execDef)
		wantHops := []int{
			sExec.effectiveAbilityDamageLocked(casterE, execDef, primaryAmt),
			sExec.effectiveAbilityDamageLocked(casterE, execDef, chainBounceDamage(t, execDef, 0)),
			sExec.effectiveAbilityDamageLocked(casterE, execDef, chainBounceDamage(t, execDef, 1)),
		}
		for i, got := range []int{gotPrimary, got1, got2} {
			if got != wantHops[i] {
				t.Fatalf("%s: executor hop-%d damage = %d, want %d (primary=%d)", label, i, got, wantHops[i], primaryAmt)
			}
		}
		if farE.HP != preFarE {
			t.Fatalf("%s: executor far enemy HP = %d, want unchanged %d; should be outside bounce range", label, farE.HP, preFarE)
		}
		if !(gotPrimary > got1 && got1 > got2) {
			t.Fatalf("%s: executor per-hop falloff primary>b1>b2 violated: %d,%d,%d", label, gotPrimary, got1, got2)
		}
		return wantPrimary, want1, want2
	}

	t.Run("unmodified_caster", func(t *testing.T) {
		primary, _, _ := run(t, nil, "chain_lightning/unmodified")
		if primary != legacyDef.DamageAmount {
			t.Fatalf("unmodified-caster primary damage = %d, want base damageAmount %d (no modifier active)", primary, legacyDef.DamageAmount)
		}
	})
	t.Run("modified_caster", func(t *testing.T) {
		m := goldenDamageModifier(string(legacyDef.DamageType)) // "lightning" — matches chain_lightning's own school
		primary, _, _ := run(t, &m, "chain_lightning/modified")
		if primary <= legacyDef.DamageAmount {
			t.Fatalf("modified-caster primary damage = %d, want > base damageAmount %d (the +50%% modifier should have scaled it up)", primary, legacyDef.DamageAmount)
		}
	})
}
