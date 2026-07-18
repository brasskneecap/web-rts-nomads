package game

import (
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Golden equivalence test for launch_vortex (arcane_orb — migrated to
// schemaVersion:2 in the live catalog).
//
// Like meteor's golden test, arcane_orb's effect is NOT resolved synchronously
// at cast-resolve time: both the legacy leg (spawnArcaneOrbLocked, called
// directly by resolveAbilityCastAtPointLocked's pre-migration branch) and the
// executor leg (the SAME spawnArcaneOrbLocked, called from launch_vortex's
// Execute — ability_exec_vortex.go) spawn a traveling Projectile that only
// pulls and damages hostiles in its radius as tickArcaneOrbProjectileLocked
// (projectile.go) advances it. Both scenes are therefore ticked via the real
// GameState.Update (which drives tickProjectilesLocked), exactly like
// TestAbilityCompileGolden_Meteor — not resolved with a single synchronous
// call like the shatter/heal/projectile golden tests.
//
// Both legs call resolveAbilityCastAtPointLocked directly (bypassing
// beginAbilityCastAtPointLocked's getAbilityDef-by-id lookup, which would
// return the REAL shipped v2 def for "arcane_orb" on both legs and defeat the
// point of comparing against the frozen pre-migration fixture) — same
// pattern TestAbilityCompileGolden_Shatter/Meteor already use.
// ═════════════════════════════════════════════════════════════════════════════

// buildGoldenArcaneOrbScene spawns a caster and a point cast due east, an
// enemy ("bystander") offset from the path but well within the orb's radius,
// and an ally at the same offset on the other side (must never be pulled or
// damaged — pull/damage is hostility-gated). Every unit's Damage is zeroed so
// no incidental basic-attack combat perturbs the scene while the bystander is
// dragged around — isolates the vortex's own pull+DoT physics, matching
// TestArcaneOrb_MovingVortexDragsAndDamages' same neutering discipline.
// Returns with s.mu held; caller must s.mu.Unlock() (both legs are later
// ticked via GameState.Update, which takes the lock itself, so both scenes
// must be unlocked before ticking — see the meteor golden test for the same
// two-phase pattern).
func buildGoldenArcaneOrbScene(t *testing.T, mod *SpellModifier) (s *GameState, caster, bystander, ally *Unit, castX, castY float64) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	caster.Damage = 0
	if mod != nil {
		caster.SpellModifiers = []SpellModifier{*mod}
	}

	bystander = teamCombatUnit(t, s, "p2", 200, 50) // offset from the east path, inside radius 130
	bystander.HP, bystander.MaxHP = 2000, 2000      // survives even the modified-caster damage
	bystander.Damage = 0

	ally = teamCombatUnit(t, s, "p1", 200, -50) // same team as caster — must be immune
	ally.Damage = 0

	// Click far east; the orb's travel distance is always CastRange (not the
	// raw click distance — see spawnArcaneOrbLocked's doc comment), only the
	// CLICK DIRECTION matters here.
	return s, caster, bystander, ally, 1000, 0
}

func TestAbilityCompileGolden_ArcaneOrb(t *testing.T) {
	legacyDef := legacyArcaneOrbFixture()
	execDef := requireMigratedV2(t, "arcane_orb")
	if legacyDef.PullStrength <= 0 || legacyDef.Radius <= 0 || legacyDef.DamagePerSecond <= 0 {
		t.Fatalf("fixture drifted: PullStrength=%v Radius=%v DamagePerSecond=%v, want all > 0", legacyDef.PullStrength, legacyDef.Radius, legacyDef.DamagePerSecond)
	}

	// runToOrbEnd advances s (lock held) until the arcane orb Projectile is
	// gone, or maxTicks is exhausted — mirrors runToProjectileImpact.
	runToOrbEnd := func(s *GameState, maxTicks int) {
		for i := 0; i < maxTicks && findArcaneOrb(s) != nil; i++ {
			s.Update(0.05)
		}
	}

	run := func(t *testing.T, mod *SpellModifier, label string) (legacyDamage int) {
		sLegacy, casterL, bystanderL, allyL, cx, cy := buildGoldenArcaneOrbScene(t, mod)
		sExec, casterE, bystanderE, allyE, _, _ := buildGoldenArcaneOrbScene(t, mod)

		preByL, preAllyL := bystanderL.HP, allyL.HP
		preByE, preAllyE := bystanderE.HP, allyE.HP
		byStartL := struct{ x, y float64 }{bystanderL.X, bystanderL.Y}
		byStartE := struct{ x, y float64 }{bystanderE.X, bystanderE.Y}

		// Legacy: frozen pre-migration fixture through the flat-field resolver.
		effL := sLegacy.effectiveSpellLocked(casterL, legacyDef)
		sLegacy.resolveAbilityCastAtPointLocked(casterL, legacyDef, effL, cx, cy)
		if findArcaneOrb(sLegacy) == nil {
			sLegacy.mu.Unlock()
			t.Fatalf("%s: legacy fixture drifted: no orb spawned", label)
		}

		// Executor: the ACTUAL shipped catalog def (schemaVersion 2) through
		// the SAME production entry point. resolveAbilityCastAtPointLocked's
		// own SchemaVersion>=2 branch routes this to the executor — nothing
		// here is bespoke test wiring.
		effE := sExec.effectiveSpellLocked(casterE, execDef)
		sExec.resolveAbilityCastAtPointLocked(casterE, execDef, effE, cx, cy)
		if findArcaneOrb(sExec) == nil {
			sLegacy.mu.Unlock()
			sExec.mu.Unlock()
			t.Fatalf("%s: executor orb never spawned", label)
		}

		// Both scenes must be unlocked before ticking via GameState.Update,
		// which takes s.mu itself (same two-phase pattern as the meteor
		// golden test).
		sLegacy.mu.Unlock()
		sExec.mu.Unlock()

		travelSeconds := legacyDef.CastRange.Resolve(casterL) / legacyDef.ProjectileSpeed
		maxTicks := int(travelSeconds/0.05) + 40 // generous slack past full travel
		runToOrbEnd(sLegacy, maxTicks)
		runToOrbEnd(sExec, maxTicks)

		sLegacy.mu.Lock()
		defer sLegacy.mu.Unlock()
		sExec.mu.Lock()
		defer sExec.mu.Unlock()

		if len(sLegacy.Projectiles) != 0 {
			t.Errorf("%s: legacy orb should have finished travelling: %d projectile(s) remain", label, len(sLegacy.Projectiles))
		}
		if len(sExec.Projectiles) != 0 {
			t.Errorf("%s: executor orb should have finished travelling: %d projectile(s) remain", label, len(sExec.Projectiles))
		}

		legacyDamage = preByL - bystanderL.HP
		if legacyDamage <= 0 {
			t.Fatalf("%s: legacy fixture drifted: bystander took no vortex damage (HP %d -> %d)", label, preByL, bystanderL.HP)
		}
		if math.Hypot(bystanderL.X-byStartL.x, bystanderL.Y-byStartL.y) < 5 {
			t.Errorf("%s: legacy fixture drifted: bystander was not dragged by the moving vortex", label)
		}
		if allyL.HP != preAllyL {
			t.Errorf("%s: legacy fixture drifted: ally.HP = %d, want unchanged %d (wrong relation, must be immune)", label, allyL.HP, preAllyL)
		}

		execDamage := preByE - bystanderE.HP
		if execDamage != legacyDamage {
			t.Errorf("%s: executor bystander damage = %d, want %d (legacy)", label, execDamage, legacyDamage)
		}
		if math.Hypot(bystanderE.X-byStartE.x, bystanderE.Y-byStartE.y) < 5 {
			t.Errorf("%s: executor fixture drifted: bystander was not dragged by the moving vortex", label)
		}
		if allyE.HP != preAllyE {
			t.Errorf("%s: executor ally.HP = %d, want unchanged %d (wrong relation, must be immune)", label, allyE.HP, preAllyE)
		}
		// Both bystanders must have been dragged the SAME distance (proves the
		// pull-strength fold, not just the damage fold, matches).
		if got, want := math.Hypot(bystanderE.X-byStartE.x, bystanderE.Y-byStartE.y), math.Hypot(bystanderL.X-byStartL.x, bystanderL.Y-byStartL.y); math.Abs(got-want) > 0.01 {
			t.Errorf("%s: executor bystander displacement = %v, want %v (legacy)", label, got, want)
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
		return legacyDamage
	}

	var unmodifiedDamage int
	t.Run("unmodified_caster", func(t *testing.T) {
		unmodifiedDamage = run(t, nil, "arcane_orb/unmodified")
	})
	t.Run("modified_caster", func(t *testing.T) {
		m := goldenDamageModifier(string(legacyDef.DamageType)) // "arcane" — matches arcane_orb's own school; also scales DamagePerSecond (see resolveEffectiveSpell's shared "damage" field doc comment)
		got := run(t, &m, "arcane_orb/modified")
		if got <= unmodifiedDamage {
			t.Fatalf("modified-caster total vortex damage = %d, want > unmodified-caster total %d (the +50%% modifier should have scaled DamagePerSecond up)", got, unmodifiedDamage)
		}
	})
}
