package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Golden equivalence tests (Phase 4, Task 4; rewritten for the 2026-07
// migration that flipped heal/greater_heal/shatter/raise_skeleton/meteor to
// schemaVersion:2 in the live catalog — see ability_legacy_fixtures_test.go)
//
// Each test below builds TWO independent, identically-seeded GameStates
// ("twin scenes") for one of the five migrated abilities: resolves the
// ability through the LEGACY cast-resolution path (resolveAbilityCastLocked /
// resolveAbilityCastAtPointLocked), driven by a FROZEN pre-migration fixture
// def (ability_legacy_fixtures_test.go), on one scene; and resolves it
// through the SAME production entry points, driven by the ACTUAL shipped
// catalog def (getAbilityDef — schemaVersion 2 with a compiled Program), on
// its twin. Both legs call the identical resolveAbilityCastLocked /
// resolveAbilityCastAtPointLocked functions — the only difference is which
// def they're handed — so the SchemaVersion>=2 routing branch inside those
// functions is exercised for real on the executor leg, not bypassed by
// hand-built test wiring. That is what makes this the actual migration
// guarantee: "the program shipped in the catalog behaves exactly like the
// legacy ability it replaced," not "two hand-authored test harnesses agree
// with each other."
//
// The resulting GAMEPLAY state (HP, mana, slow tracks, summon counts/
// ownership) is asserted IDENTICAL between the two scenes. Each ability runs
// under BOTH an unmodified caster and a caster carrying a +50% school-matched
// damage SpellModifier (the modifier only changes shatter's/meteor's damage —
// for the heals and the summon it's inert, and running it anyway proves there
// is no spurious divergence introduced by the modifier-folding seam).
//
// Twin scenes spawn identically, in the same order, from a fresh
// GameState (nextUnitID starts at 1 — see NewGameStateWithSeed /
// state.go). That makes unit-ID matching between the two scenes EXACT, not
// heuristic: the caster and every ally/enemy get the same ID in both scenes,
// and units summoned mid-test (raise_skeleton) do too, since the summon seam
// (spawnSummonedUnitLocked) is called with an identical caster position from
// an identical nextUnitID counter on both sides.
//
// PERK-FREE casters: every caster here is a plain "soldier" spawned via
// teamCombatUnit, with no PerkIDs/PerkState. The legacy path additionally
// fires onPerkAbilityResolvedLocked (a no-op with no perks owned) and plays
// VFX via playEffectOnUnitLocked / playEffectAtPointLocked — the executor
// leg's play_presentation action IS registered/executable as of Phase 6b, but
// with no perks owned it too is a gameplay no-op. Both are presentation/
// side-channel only and never touch gameplay state, so equivalence below is
// asserted on gameplay fields ONLY — never on s.effects / VFX / the execution
// trace. (See ability_exec_perk_parity_test.go for the perk-equipped-caster
// parity tests that DO exercise the VFX/perk-hook seam.)
// ═════════════════════════════════════════════════════════════════════════════

// goldenDamageModifier returns a +50% multiply damage modifier matched to
// `school`, in the same shape TestExecutorDamageScalingMatchesLegacy
// (ability_exec_modifier_test.go, Phase 4 Task 3) exercises. Passed to a
// caster's SpellModifiers to prove parity holds under modification too.
func goldenDamageModifier(school string) SpellModifier {
	return SpellModifier{
		Target:    SpellModTarget{School: school},
		Field:     SpellModFieldDamage,
		Operation: SpellModMultiply,
		Value:     1.5,
	}
}

// assertScenesEquivalent compares every unit present in either scene by ID
// (exact, not heuristic — see the file doc comment) and fails on any
// gameplay-state mismatch: owner, HP, current mana, and both slow tracks
// (physical + cold). label identifies the ability/variant under test in
// failure output. Never compares VFX/trace/presentation state — see the file
// doc comment for why that is intentionally out of scope.
func assertScenesEquivalent(t *testing.T, sLegacy, sExec *GameState, label string) {
	t.Helper()
	if len(sLegacy.Units) != len(sExec.Units) {
		t.Fatalf("%s: unit count mismatch: legacy=%d exec=%d", label, len(sLegacy.Units), len(sExec.Units))
	}
	seenInLegacy := make(map[int]bool, len(sLegacy.Units))
	for _, lu := range sLegacy.Units {
		if lu == nil {
			continue
		}
		seenInLegacy[lu.ID] = true
		eu := sExec.getUnitByIDLocked(lu.ID)
		if eu == nil {
			t.Errorf("%s: unit id=%d (owner=%s) present in legacy scene but missing from exec scene", label, lu.ID, lu.OwnerID)
			continue
		}
		if lu.OwnerID != eu.OwnerID {
			t.Errorf("%s: unit id=%d owner mismatch: legacy=%s exec=%s", label, lu.ID, lu.OwnerID, eu.OwnerID)
		}
		if lu.HP != eu.HP {
			t.Errorf("%s: unit id=%d HP mismatch: legacy=%d exec=%d", label, lu.ID, lu.HP, eu.HP)
		}
		if lu.CurrentMana != eu.CurrentMana {
			t.Errorf("%s: unit id=%d mana mismatch: legacy=%d exec=%d", label, lu.ID, lu.CurrentMana, eu.CurrentMana)
		}
		if lu.SlowedRemaining != eu.SlowedRemaining || lu.SlowedMultiplier != eu.SlowedMultiplier {
			t.Errorf("%s: unit id=%d physical-slow mismatch: legacy=(remaining=%v,mult=%v) exec=(remaining=%v,mult=%v)",
				label, lu.ID, lu.SlowedRemaining, lu.SlowedMultiplier, eu.SlowedRemaining, eu.SlowedMultiplier)
		}
		if lu.ColdSlowedRemaining != eu.ColdSlowedRemaining || lu.ColdSlowedMultiplier != eu.ColdSlowedMultiplier {
			t.Errorf("%s: unit id=%d cold-slow mismatch: legacy=(remaining=%v,mult=%v) exec=(remaining=%v,mult=%v)",
				label, lu.ID, lu.ColdSlowedRemaining, lu.ColdSlowedMultiplier, eu.ColdSlowedRemaining, eu.ColdSlowedMultiplier)
		}
	}
	for _, eu := range sExec.Units {
		if eu != nil && !seenInLegacy[eu.ID] {
			t.Errorf("%s: unit id=%d (owner=%s) present in exec scene but missing from legacy scene", label, eu.ID, eu.OwnerID)
		}
	}
}

// requireMigratedV2 fails the test immediately if the live catalog def for id
// is not the schemaVersion:2 / compiled-Program shape this file assumes.
// Guards against the exact failure mode the migration's central problem
// warned about: a catalog edit that silently reverts an ability to legacy
// would otherwise make the "executor" leg below silently re-exercise the
// legacy path and the test would pass vacuously.
func requireMigratedV2(t *testing.T, id string) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef(id)
	if !ok {
		t.Fatalf("getAbilityDef(%q) = _, false", id)
	}
	if def.SchemaVersion < 2 || def.Program == nil {
		t.Fatalf("catalog %q must be schemaVersion>=2 with a compiled Program for this test to prove anything: schemaVersion=%d program=%v", id, def.SchemaVersion, def.Program)
	}
	return def
}

// ── heal (single-target) ────────────────────────────────────────────────────

// buildGoldenHealScene spawns a caster and one injured ally within the
// caster's attack range (heal.castRange is the "match_attack_range"
// sentinel; teamCombatUnit sets AttackRange=90). Lock held on return; caller
// must s.mu.Unlock(). mod, if non-nil, is attached to the caster's
// SpellModifiers before either path runs (so mana-spend and heal amount see
// it exactly as a real cast would).
func buildGoldenHealScene(t *testing.T, mod *SpellModifier) (s *GameState, caster, ally *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	if mod != nil {
		caster.SpellModifiers = []SpellModifier{*mod}
	}

	ally = teamCombatUnit(t, s, "p1", 40, 0)
	ally.HP, ally.MaxHP = 50, 100

	return s, caster, ally
}

func TestAbilityCompileGolden_Heal(t *testing.T) {
	legacyDef := legacyHealFixture()
	catalogDef := requireMigratedV2(t, "heal")

	run := func(t *testing.T, mod *SpellModifier, label string) {
		sLegacy, casterL, allyL := buildGoldenHealScene(t, mod)
		defer sLegacy.mu.Unlock()
		sExec, casterE, allyE := buildGoldenHealScene(t, mod)
		defer sExec.mu.Unlock()

		preHP := allyL.HP

		// Legacy: frozen pre-migration fixture through the flat-field resolver.
		targetsL := sLegacy.buildCastTargetSetLocked(casterL, legacyDef, allyL)
		sLegacy.resolveAbilityCastLocked(casterL, legacyDef, targetsL)

		// Executor: the ACTUAL shipped catalog def (schemaVersion 2) through the
		// SAME production entry point. resolveAbilityCastLocked's own
		// SchemaVersion>=2 branch routes this to the executor — nothing here is
		// bespoke test wiring.
		targetsE := sExec.buildCastTargetSetLocked(casterE, catalogDef, allyE)
		sExec.resolveAbilityCastLocked(casterE, catalogDef, targetsE)

		// Sanity: the heal actually happened on the legacy side (catches a
		// vacuous pass where both paths silently did nothing). Derived from the
		// frozen fixture's HealAmount, never a bare hardcoded number.
		wantHP := preHP + legacyDef.HealAmount
		if wantHP > allyL.MaxHP {
			wantHP = allyL.MaxHP
		}
		if allyL.HP != wantHP {
			t.Fatalf("%s: legacy fixture drifted: ally.HP = %d, want %d (pre %d + healAmount %d, clamped to maxHP %d)",
				label, allyL.HP, wantHP, preHP, legacyDef.HealAmount, allyL.MaxHP)
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
	}

	t.Run("unmodified_caster", func(t *testing.T) { run(t, nil, "heal/unmodified") })
	t.Run("modified_caster", func(t *testing.T) {
		// Inert for heal: "damage" is not a field any compiled heal action
		// reads. Run anyway to prove no spurious divergence.
		m := goldenDamageModifier(string(legacyDef.DamageType))
		run(t, &m, "heal/modified")
	})
}

// ── greater_heal (multi-target) ─────────────────────────────────────────────

// buildGoldenGreaterHealScene spawns a caster and two allies, ALL THREE
// injured (distinct HP% values) and within cast range, with NO focus target
// set. This is a deliberately NARROWED scenario (see the package-level
// comment on TestAbilityCompileGolden_GreaterHeal for why): with all three
// candidates injured, buildCastTargetSetLocked's "exclude full-HP allies
// without a focus target" filter never triggers, and the compiled
// select_targets query's IncludeInitialTarget forcing never needs to reach
// past the natural top-3 selection — both algorithms reduce to "sort the
// same 3-candidate pool ascending by HP%, cap at TargetCount(3)" and produce
// the identical set. Lock held on return; caller must s.mu.Unlock().
func buildGoldenGreaterHealScene(t *testing.T, mod *SpellModifier) (s *GameState, caster, primary, ally2 *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.HP, caster.MaxHP = 60, 100 // injured (60%) — a valid self-heal candidate
	caster.MaxMana, caster.CurrentMana = 100, 100
	if mod != nil {
		caster.SpellModifiers = []SpellModifier{*mod}
	}

	primary = teamCombatUnit(t, s, "p1", 40, 0) // 40px from caster (< AttackRange 90)
	primary.HP, primary.MaxHP = 20, 100         // most injured (20%) — the clicked target

	ally2 = teamCombatUnit(t, s, "p1", 80, 0) // 80px from caster (< AttackRange 90)
	ally2.HP, ally2.MaxHP = 50, 100           // 50%

	return s, caster, primary, ally2
}

// TestAbilityCompileGolden_GreaterHeal proves executor/legacy equivalence for
// the multi-target heal shape, INCLUDING target-selection parity between the
// compiled select_targets query and buildCastTargetSetLocked.
//
// NARROWED-EQUIVALENCE CAVEAT: buildCastTargetSetLocked has a "focus-widened
// pool" behavior (autocast_selectors.go) where, if the caster owns a
// heal-buff perk AND has a live FocusTargetID, full-HP allies are pulled
// into the candidate pool so AoE slots don't go to waste; without a focus
// target, full-HP allies are excluded outright. The compiled select_targets
// query (ability_compile.go's compileHealActions) has no equivalent concept
// — it has no "exclude full-HP unless focused" filter at all, because
// TargetQueryDef has no focus-target field. Compiler tuning cannot close this
// gap (there's no query field to express it), so this test's scenario is
// deliberately narrowed to sidestep it: no full-HP candidate, no focus
// target. Under those conditions both algorithms provably reduce to the same
// operation (see buildGoldenGreaterHealScene's doc comment) and equivalence
// holds exactly. The full-HP-exclusion / focus-widening behavior itself
// remains legacy-only and is NOT claimed equivalent here.
//
// ANCHOR-COLLAPSE NOTE (migration risk called out explicitly): the shipped
// catalog def's TargetCount normalises to 1 (cleared on conversion, see
// ConvertLegacyAbility), so on the executor leg buildCastTargetSetLocked
// returns only [primary] instead of a natural 3-target anchor set. This does
// NOT change behavior: resolveAbilityCastLocked's SchemaVersion>=2 branch
// only ever reads targets[0] as the InitialTarget (see its own doc comment)
// and discards the rest even when a legacy def's TargetCount produces a
// longer slice — the REAL multi-target fan-out for a v2 ability is entirely
// the compiled select_targets query's job (MaxCount:3, IncludeInitialTarget:
// true, baked into the shipped Program independent of def.TargetCount). This
// test's len(targets)==3 sanity check therefore only applies to the LEGACY
// leg; the executor leg is asserted via the healed-HP checks below instead,
// and TestLiveCast_GreaterHeal_MultiTargetFanOutPreserved
// (ability_cast_program_test.go) is the direct end-to-end proof that a real
// cast through beginAbilityCastLocked still heals all three allies.
func TestAbilityCompileGolden_GreaterHeal(t *testing.T) {
	legacyDef := legacyGreaterHealFixture()
	catalogDef := requireMigratedV2(t, "greater_heal")

	run := func(t *testing.T, mod *SpellModifier, label string) {
		sLegacy, casterL, primaryL, ally2L := buildGoldenGreaterHealScene(t, mod)
		defer sLegacy.mu.Unlock()
		sExec, casterE, primaryE, ally2E := buildGoldenGreaterHealScene(t, mod)
		defer sExec.mu.Unlock()

		preCasterHP, prePrimaryHP, preAlly2HP := casterL.HP, primaryL.HP, ally2L.HP

		// Legacy.
		targetsL := sLegacy.buildCastTargetSetLocked(casterL, legacyDef, primaryL)
		if len(targetsL) != 3 {
			t.Fatalf("%s: fixture drifted: legacy selected %d targets, want 3 (narrowed-scenario assumption: all 3 candidates injured, in range)", label, len(targetsL))
		}
		sLegacy.resolveAbilityCastLocked(casterL, legacyDef, targetsL)

		// Executor: real catalog def through the real entry point. See the
		// ANCHOR-COLLAPSE NOTE above for why targetsE has length 1 here and why
		// that's fine — the compiled select_targets query does the real
		// 3-target fan-out independently of this slice's length.
		targetsE := sExec.buildCastTargetSetLocked(casterE, catalogDef, primaryE)
		sExec.resolveAbilityCastLocked(casterE, catalogDef, targetsE)

		// Sanity: all three actually got healed on the legacy side, derived
		// from the frozen fixture's HealAmount (never a hardcoded balance number).
		clampedWant := func(pre int, u *Unit) int {
			want := pre + legacyDef.HealAmount
			if want > u.MaxHP {
				want = u.MaxHP
			}
			return want
		}
		if want := clampedWant(preCasterHP, casterL); casterL.HP != want {
			t.Fatalf("%s: legacy fixture drifted: caster(self-heal).HP = %d, want %d", label, casterL.HP, want)
		}
		if want := clampedWant(prePrimaryHP, primaryL); primaryL.HP != want {
			t.Fatalf("%s: legacy fixture drifted: primary.HP = %d, want %d", label, primaryL.HP, want)
		}
		if want := clampedWant(preAlly2HP, ally2L); ally2L.HP != want {
			t.Fatalf("%s: legacy fixture drifted: ally2.HP = %d, want %d", label, ally2L.HP, want)
		}
		// Sanity: the executor side ALSO healed all three (proves the
		// anchor-collapse noted above truly is harmless in practice, not just
		// in theory) — same clamped-want formula, same frozen HealAmount.
		if want := clampedWant(preCasterHP, casterE); casterE.HP != want {
			t.Fatalf("%s: executor caster(self-heal).HP = %d, want %d (anchor collapse should not have dropped the self-heal slot)", label, casterE.HP, want)
		}
		if want := clampedWant(prePrimaryHP, primaryE); primaryE.HP != want {
			t.Fatalf("%s: executor primary.HP = %d, want %d", label, primaryE.HP, want)
		}
		if want := clampedWant(preAlly2HP, ally2E); ally2E.HP != want {
			t.Fatalf("%s: executor ally2.HP = %d, want %d (anchor collapse should not have dropped the 3rd target slot)", label, ally2E.HP, want)
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
	}

	t.Run("unmodified_caster", func(t *testing.T) { run(t, nil, "greater_heal/unmodified") })
	t.Run("modified_caster", func(t *testing.T) {
		// Inert for greater_heal: same reasoning as heal/modified above.
		m := goldenDamageModifier(string(legacyDef.DamageType))
		run(t, &m, "greater_heal/modified")
	})
}

// ── shatter (instant point-AoE + slow) ──────────────────────────────────────

// buildGoldenShatterScene spawns a caster, an ally standing inside where the
// burst radius will be (must NOT be hit — wrong relation), an enemy inside
// the burst radius (must be hit + chilled), and an enemy outside the burst
// radius (must be untouched). The cast point (150,0) is well within the
// caster's 400px cast range, so clampPointToRange is a no-op on both paths —
// the point the legacy resolver clamps to and the ctx.CastPoint fed to the
// executor are identical. Lock held on return; caller must s.mu.Unlock().
func buildGoldenShatterScene(t *testing.T, mod *SpellModifier) (s *GameState, caster, enemyNear, enemyFar, allyNear *Unit, castX, castY float64) {
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

	allyNear = teamCombatUnit(t, s, "p1", 150, 10) // inside the burst radius, same team — must not be hit

	enemyNear = teamCombatUnit(t, s, "p2", 150, 50) // dist from (150,0) = 50 < radius 110
	enemyNear.HP, enemyNear.MaxHP = 500, 500        // survives even the modified-caster damage

	enemyFar = teamCombatUnit(t, s, "p2", 150, 300) // dist from (150,0) = 300 > radius 110
	enemyFar.HP, enemyFar.MaxHP = 500, 500

	return s, caster, enemyNear, enemyFar, allyNear, 150, 0
}

func TestAbilityCompileGolden_Shatter(t *testing.T) {
	legacyDef := legacyShatterFixture()
	catalogDef := requireMigratedV2(t, "shatter")

	run := func(t *testing.T, mod *SpellModifier, label string) (sLegacy, sExec *GameState, enemyNearL, enemyFarL, allyNearL *Unit, wantDamage int) {
		var casterL *Unit
		var cx, cy float64
		sLegacy, casterL, enemyNearL, enemyFarL, allyNearL, cx, cy = buildGoldenShatterScene(t, mod)
		var casterE *Unit
		sExec, casterE, _, _, _, _, _ = buildGoldenShatterScene(t, mod)

		preAllyHP, preFarHP := allyNearL.HP, enemyFarL.HP

		// Legacy: frozen pre-migration fixture.
		effL := sLegacy.effectiveSpellLocked(casterL, legacyDef)
		wantDamage = effL.Damage // the SCALED per-hit damage — asserted below, never hardcoded
		sLegacy.resolveAbilityCastAtPointLocked(casterL, legacyDef, effL, cx, cy)

		// Executor: real catalog def through the real entry point.
		effE := sExec.effectiveSpellLocked(casterE, catalogDef)
		sExec.resolveAbilityCastAtPointLocked(casterE, catalogDef, effE, cx, cy)

		// Sanity on the legacy side: the enemy inside the radius took EXACTLY
		// the effective (modifier-scaled) damage and was chilled; the ally
		// (wrong relation) and the far enemy (outside the radius) were
		// untouched. All derived from the frozen fixture/eff fields, never
		// hardcoded.
		if wantEnemyHP := enemyNearL.MaxHP - wantDamage; enemyNearL.HP != wantEnemyHP {
			t.Fatalf("%s: legacy fixture drifted: enemyNear.HP = %d, want %d (maxHP %d - effective damage %d)",
				label, enemyNearL.HP, wantEnemyHP, enemyNearL.MaxHP, wantDamage)
		}
		if enemyNearL.ColdSlowedRemaining <= 0 {
			t.Fatalf("%s: legacy fixture drifted: enemyNear.ColdSlowedRemaining = %v, want > 0", label, enemyNearL.ColdSlowedRemaining)
		}
		if enemyNearL.ColdSlowedMultiplier != legacyDef.SlowMultiplier {
			t.Fatalf("%s: legacy fixture drifted: enemyNear.ColdSlowedMultiplier = %v, want %v (frozen fixture SlowMultiplier)",
				label, enemyNearL.ColdSlowedMultiplier, legacyDef.SlowMultiplier)
		}
		if allyNearL.HP != preAllyHP {
			t.Fatalf("%s: legacy fixture drifted: allyNear.HP = %d, want unchanged %d (wrong relation, must not be hit)", label, allyNearL.HP, preAllyHP)
		}
		if enemyFarL.HP != preFarHP {
			t.Fatalf("%s: legacy fixture drifted: enemyFar.HP = %d, want unchanged %d (outside burst radius)", label, enemyFarL.HP, preFarHP)
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
		return sLegacy, sExec, enemyNearL, enemyFarL, allyNearL, wantDamage
	}

	t.Run("unmodified_caster", func(t *testing.T) {
		sLegacy, sExec, _, _, _, wantDamage := run(t, nil, "shatter/unmodified")
		defer sLegacy.mu.Unlock()
		defer sExec.mu.Unlock()
		if wantDamage != legacyDef.DamageAmount {
			t.Fatalf("unmodified-caster effective damage = %d, want base damageAmount %d (no modifier active)", wantDamage, legacyDef.DamageAmount)
		}
	})
	t.Run("modified_caster", func(t *testing.T) {
		m := goldenDamageModifier(string(legacyDef.DamageType)) // "cold" — matches shatter's own school
		sLegacy, sExec, _, _, _, wantDamage := run(t, &m, "shatter/modified")
		defer sLegacy.mu.Unlock()
		defer sExec.mu.Unlock()
		// Proves Task 3's scaling seam actually fired: the effective damage
		// under the +50% multiply modifier must exceed the unmodified base.
		if wantDamage <= legacyDef.DamageAmount {
			t.Fatalf("modified-caster effective damage = %d, want > base damageAmount %d (the +50%% modifier should have scaled it up)", wantDamage, legacyDef.DamageAmount)
		}
	})
}

// ── raise_skeleton (summon) ─────────────────────────────────────────────────

// buildGoldenRaiseSkeletonScene spawns a lone caster with a self-targeted
// summon ability. Lock held on return; caller must s.mu.Unlock().
func buildGoldenRaiseSkeletonScene(t *testing.T, mod *SpellModifier) (s *GameState, caster *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)

	caster = teamCombatUnit(t, s, "p1", 200, 200)
	caster.MaxMana, caster.CurrentMana = 100, 100
	if mod != nil {
		caster.SpellModifiers = []SpellModifier{*mod}
	}
	return s, caster
}

func TestAbilityCompileGolden_RaiseSkeleton(t *testing.T) {
	legacyDef := legacyRaiseSkeletonFixture()
	catalogDef := requireMigratedV2(t, "raise_skeleton")

	run := func(t *testing.T, mod *SpellModifier, label string) {
		sLegacy, casterL := buildGoldenRaiseSkeletonScene(t, mod)
		defer sLegacy.mu.Unlock()
		sExec, casterE := buildGoldenRaiseSkeletonScene(t, mod)
		defer sExec.mu.Unlock()

		preCountL, preCountE := len(sLegacy.Units), len(sExec.Units)

		// Legacy: self-targeted, matching beginAbilityCastLocked's
		// buildCastTargetSetLocked(caster, def, caster) call shape.
		targetsL := sLegacy.buildCastTargetSetLocked(casterL, legacyDef, casterL)
		sLegacy.resolveAbilityCastLocked(casterL, legacyDef, targetsL)

		// Executor: real catalog def through the real entry point.
		targetsE := sExec.buildCastTargetSetLocked(casterE, catalogDef, casterE)
		sExec.resolveAbilityCastLocked(casterE, catalogDef, targetsE)

		gotL := len(sLegacy.Units) - preCountL
		gotE := len(sExec.Units) - preCountE
		if gotL != legacyDef.SummonCount {
			t.Fatalf("%s: legacy spawned %d units, want frozen fixture SummonCount %d", label, gotL, legacyDef.SummonCount)
		}
		if gotE != legacyDef.SummonCount {
			t.Fatalf("%s: exec spawned %d units, want frozen fixture SummonCount %d", label, gotE, legacyDef.SummonCount)
		}

		for _, u := range sLegacy.Units {
			if u != nil && u.ID != casterL.ID && u.OwnerID != casterL.OwnerID {
				t.Errorf("%s: legacy-summoned unit id=%d owner=%s, want caster's owner %s", label, u.ID, u.OwnerID, casterL.OwnerID)
			}
		}
		for _, u := range sExec.Units {
			if u != nil && u.ID != casterE.ID && u.OwnerID != casterE.OwnerID {
				t.Errorf("%s: exec-summoned unit id=%d owner=%s, want caster's owner %s", label, u.ID, u.OwnerID, casterE.OwnerID)
			}
		}

		assertScenesEquivalent(t, sLegacy, sExec, label)
	}

	t.Run("unmodified_caster", func(t *testing.T) { run(t, nil, "raise_skeleton/unmodified") })
	t.Run("modified_caster", func(t *testing.T) {
		// Inert for raise_skeleton: no damage/heal amount is involved in a
		// summon. Run anyway to prove no spurious divergence.
		m := goldenDamageModifier(string(legacyDef.DamageType))
		run(t, &m, "raise_skeleton/modified")
	})
}

// ── meteor (delayed-impact point-AoE + lingering burn zone) ────────────────
//
// Meteor is the one migrated ability whose equivalence is genuinely
// TIME-DEPENDENT rather than resolvable with a single synchronous call:
//
//   - The legacy path (spawnGroundHazardLocked / ground_hazard.go) spawns a
//     GroundHazard whose one-time impact fires after ImpactDelaySeconds of
//     tickGroundHazardsLocked, then whose first burn tick fires
//     IMMEDIATELY on the same tick as impact (burnTickTimer reset to 0 —
//     see ground_hazard.go's tickGroundHazardsLocked), repeating every
//     BurnTickIntervalSeconds after that.
//   - The executor path schedules the impact via the on_animation_marker
//     scheduler (ability_marker.go) at the same ImpactDelaySeconds, whose
//     "sel"+"dmg" actions deal the impact hit, followed by a create_zone
//     action that ALSO fires its first on_zone_tick immediately (see
//     spawnAbilityZoneLocked, which arms tickTimer at 0 rather than
//     TickInterval specifically to match GroundHazard's immediate-first-tick
//     pacing), repeating every TickInterval after that.
//
// Both schedules therefore land on the SAME immediate-then-every-interval
// cadence relative to their own creation instant, and GroundHazard's
// "reset burnTickTimer to 0 the instant impact fires" accumulator-overshoot
// banking (which makes a clean Duration/Interval division fire ONE MORE
// burn tick than the naive floor — 9 ticks, not 8, for meteor's 4.0s/0.5s
// knobs) is reproduced identically by AbilityZone's tickTimer=0 arming (see
// tickAbilityZonesLocked's zoneTickEpsilon-guarded loop, which fires the same
// N+1 count under the same reasoning). The only residual difference is
// structural, not a damage gap: the zone doesn't exist yet when
// tickAbilityZonesLocked runs on the SAME tick the impact marker fires (zone
// creation happens via tickAbilityMarkersLocked, which runs AFTER
// tickAbilityZonesLocked in Update's ordering — see state.go), so the
// earliest the zone can be ticked is one simulation dt after legacy's
// same-tick fire. That is a sub-tick timing shift with no effect on total
// tick COUNT or total damage over the full window this test ticks through
// (proven below: gotL == gotE, asserted as an exact equality, not a pinned
// gap).
//
// Both scenes place the caster far from the impact point (mirroring
// TestGroundHazard_DelaysImpactThenBurns in ground_hazard_test.go) so no
// combat-AI engagement between caster and target perturbs either scene
// during the ~5s of ticking this test needs.
func buildGoldenMeteorScene(t *testing.T, mod *SpellModifier) (s *GameState, caster, target *Unit, castX, castY float64) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster = s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	caster.Visible = true
	caster.MaxMana, caster.CurrentMana = 100, 100
	if mod != nil {
		caster.SpellModifiers = []SpellModifier{*mod}
	}

	target = spawnEnemy(t, s, 800, 800) // far from caster: no AI engagement over the tick window
	target.MaxHP, target.HP = 2000, 2000
	// Passive HP regen (defaultHealthRegenPerSecond, state.go) would otherwise
	// claw back a stray HP or two over the ~5s/100-tick window this test
	// runs, making the exact damage-delta assertions below tick-count/timing
	// sensitive for a reason that has nothing to do with meteor. Zeroed on
	// both scenes identically so it cannot confound the comparison.
	target.HealthRegenPerSecond = 0
	target.BaseHealthRegenPerSecond = 0

	return s, caster, target, 800, 800
}

func TestAbilityCompileGolden_Meteor(t *testing.T) {
	legacyDef := legacyMeteorFixture()
	catalogDef := requireMigratedV2(t, "meteor")

	// Both scenes place the caster ~990px from the target (mirroring
	// TestGroundHazard_DelaysImpactThenBurns) specifically to stay outside
	// every combat AI profile's DetectionRange (max 430px as of this writing
	// — see combat_ai_profiles.go) for the ~5s/100-tick window this test
	// needs, so neither unit moves or fights and the burn radius stays
	// centred on a target that never wanders off. resolveAbilityCastAtPointLocked
	// clamps the cast point to the caster's resolved CastRange
	// (clampPointToRange), which would otherwise pull the hazard/zone center
	// far away from the target at that distance — CastRange is cast-setup
	// gating exercised by beginAbilityCastAtPointLocked (bypassed here, same
	// as every other golden test in this file, which calls the resolve*
	// entry point directly), not part of what impact/burn equivalence is
	// proving, so it is widened on both LOCAL copies below. Nothing else
	// about either def is touched.
	legacyDef.CastRange = 2000
	catalogDef.CastRange = 2000

	if legacyDef.BurnTickIntervalSeconds <= 0 {
		t.Fatalf("fixture drifted: burnTickIntervalSeconds must be > 0")
	}
	wantBurnTicks := int(legacyDef.BurnDurationSeconds/legacyDef.BurnTickIntervalSeconds + 0.5)
	if float64(wantBurnTicks)*legacyDef.BurnTickIntervalSeconds != legacyDef.BurnDurationSeconds {
		t.Fatalf("fixture drifted: burnDurationSeconds %v is not a clean multiple of burnTickIntervalSeconds %v — this test's total-tick-count-equivalence assumption no longer holds",
			legacyDef.BurnDurationSeconds, legacyDef.BurnTickIntervalSeconds)
	}

	run := func(t *testing.T, mod *SpellModifier, label string) {
		sLegacy, casterL, targetL, cx, cy := buildGoldenMeteorScene(t, mod)
		sExec, casterE, targetE, _, _ := buildGoldenMeteorScene(t, mod)

		preHPL, preHPE := targetL.HP, targetE.HP

		effL := sLegacy.effectiveSpellLocked(casterL, legacyDef)
		wantImpact := effL.Damage
		sLegacy.resolveAbilityCastAtPointLocked(casterL, legacyDef, effL, cx, cy)

		effE := sExec.effectiveSpellLocked(casterE, catalogDef)
		sExec.resolveAbilityCastAtPointLocked(casterE, catalogDef, effE, cx, cy)

		// Neither path deals damage SYNCHRONOUSLY at resolve time — both are
		// delayed-impact by construction (GroundHazard fall phase / the
		// on_animation_marker scheduler). Catches a vacuous pass where damage
		// landed immediately instead of on the expected schedule.
		if targetL.HP != preHPL {
			t.Fatalf("%s: legacy dealt damage synchronously at resolve time (HP %d -> %d); meteor must be delayed-impact", label, preHPL, targetL.HP)
		}
		if targetE.HP != preHPE {
			t.Fatalf("%s: executor dealt damage synchronously at resolve time (HP %d -> %d); meteor must be delayed-impact", label, preHPE, targetE.HP)
		}

		// buildGoldenMeteorScene returns with s.mu held (matching every other
		// buildGolden*Scene helper in this file); GameState.Update acquires it
		// itself, so both scenes must be unlocked before the tick loop below —
		// unlike the synchronous golden tests, meteor's twin scenes are ticked
		// via the real GameState.Update, not driven by more *Locked calls.
		sLegacy.mu.Unlock()
		sExec.mu.Unlock()

		// Advance both scenes past impact (0.6s) and the full 4.0s burn window,
		// plus slack for the executor's one-interval-later burn start (see the
		// file doc comment) and float accumulation. 0.05s/tick (matches
		// `advance` elsewhere in this package).
		const dt = 0.05
		ticks := int((legacyDef.ImpactDelaySeconds+legacyDef.BurnDurationSeconds+legacyDef.BurnTickIntervalSeconds*3)/dt) + 20
		for i := 0; i < ticks; i++ {
			sLegacy.Update(dt)
			sExec.Update(dt)
		}

		gotL := preHPL - targetL.HP
		gotE := preHPE - targetE.HP

		// Both paths fire ONE MORE burn tick than a clean Duration/Interval
		// division predicts (the immediate-first-tick accumulator-overshoot
		// banking — see the file doc comment — applies identically to
		// GroundHazard and AbilityZone now), so both totals are asserted
		// against the SAME formula.
		wantTotal := wantImpact + (wantBurnTicks+1)*legacyDef.BurnDamagePerTick
		if gotL != wantTotal {
			t.Fatalf("%s: legacy fixture drifted: target took %d total damage, want %d (impact %d + %d burn ticks [the clean %d plus GroundHazard's one-extra accumulator tick] * %d)",
				label, gotL, wantTotal, wantImpact, wantBurnTicks+1, wantBurnTicks, legacyDef.BurnDamagePerTick)
		}

		// The migration's last known gap: this now asserts TRUE equivalence
		// (not a pinned, documented shortfall). If this regresses, it means
		// AbilityZone's immediate-first-tick arming (spawnAbilityZoneLocked)
		// has drifted out of parity with GroundHazard again.
		if gotE != wantTotal {
			t.Fatalf("%s: executor total damage = %d, want %d (impact %d + %d burn ticks * %d) — same as legacy; a mismatch here means AbilityZone's immediate-first-tick parity with GroundHazard has regressed",
				label, gotE, wantTotal, wantImpact, wantBurnTicks+1, legacyDef.BurnDamagePerTick)
		}

		if len(sLegacy.GroundHazards) != 0 {
			t.Errorf("%s: legacy hazard should be culled by now: %d remaining", label, len(sLegacy.GroundHazards))
		}
		if len(sExec.AbilityZones) != 0 {
			t.Errorf("%s: executor zone should be culled by now: %d remaining", label, len(sExec.AbilityZones))
		}

		// Full equivalence, including target HP (unlike the pre-fix version
		// of this test, which excluded target.HP from assertScenesEquivalent
		// because of the then-known one-tick burn gap).
		assertScenesEquivalent(t, sLegacy, sExec, label)
	}

	t.Run("unmodified_caster", func(t *testing.T) {
		run(t, nil, "meteor/unmodified")
	})
	t.Run("modified_caster", func(t *testing.T) {
		m := goldenDamageModifier(string(legacyDef.DamageType)) // "fire" — matches meteor's own school
		run(t, &m, "meteor/modified")
	})
}
