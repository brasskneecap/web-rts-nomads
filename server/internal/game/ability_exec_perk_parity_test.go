package game

import (
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Composable-executor / legacy PERK parity (pre-migration parity investigation)
//
// The golden-equivalence tests in ability_compile_golden_test.go deliberately
// use PERK-FREE casters (see that file's package comment) so they could not
// have caught the three parity gaps this file targets:
//
//  1. Divine Healer (perkClericHealOutputMultiplierLocked) is folded by the
//     legacy heal step but was NOT applied by the executor's restore_health
//     action.
//  2. onPerkAbilityResolvedLocked (battle_prayer / bolstering_prayer) fires
//     once per resolved target on the legacy path but never fired at all on
//     the composable path.
//  3. resolveAbilityProgramCastLocked used to discard a caller-supplied,
//     customized EffectiveSpell (unstable_magic's free/reduced-effectiveness
//     proc) and re-derive its own — silently re-charging mana and dropping
//     the reduced-effectiveness damage scaling for a composable ability.
//
// Cases 1-2 extend the existing golden-equivalence pattern (twin identically
// seeded scenes, legacy vs. compiled-executor, same catalog ability) with a
// perk-equipped caster. Case 3 cannot use that same twin-scene shape as-is:
// there is no *compiled* ability to compare against a hand-authored program,
// because the bug is specifically about the entry point
// (resolveAbilityCastAtPointLocked / resolveAbilityProgramCastLocked)
// silently discarding a caller-built EffectiveSpell — a concept that only
// exists once a v2 program is in play. Instead, TestUnstableMagic_* below
// builds true LEGACY-vs-EXECUTOR twin scenes for that exact call shape: the
// same "shatter" catalog def is registered under two test-only ids — one
// left at SchemaVersion 0 (legacy) and one compiled to SchemaVersion 2 (via
// compileLegacyAbility) — and BOTH are driven through the real
// fireUnstableMagicLocked entry point, so the comparison is still a genuine
// legacy-vs-executor equivalence check, just keyed on two ids instead of one.
// ═════════════════════════════════════════════════════════════════════════════

// buildGoldenHealScenePerk mirrors buildGoldenHealScene (ability_compile_golden_test.go)
// but additionally grants perkID to the caster (identically on both scenes a
// caller builds), so the divine_healer / battle_prayer parity tests exercise
// the exact same spawn shape the existing heal golden test does. Lock held on
// return; caller must s.mu.Unlock().
func buildGoldenHealScenePerk(t *testing.T, perkID string) (s *GameState, caster, ally *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	if perkID != "" {
		grantPerk(caster, perkID)
	}

	ally = teamCombatUnit(t, s, "p1", 40, 0)
	ally.HP, ally.MaxHP = 50, 100

	return s, caster, ally
}

// runHealThroughLegacy resolves "heal" on target via the real legacy entry
// point (resolveAbilityCastLocked), mirroring TestAbilityCompileGolden_Heal.
func runHealThroughLegacy(s *GameState, caster, target *Unit, def AbilityDef) {
	targets := s.buildCastTargetSetLocked(caster, def, target)
	s.resolveAbilityCastLocked(caster, def, targets)
}

// runHealThroughExecutor resolves "heal" on target via the compiled program +
// runProgramTriggersLocked, mirroring TestAbilityCompileGolden_Heal's executor
// leg exactly (same ctx shape, same manual mana spend).
func runHealThroughExecutor(s *GameState, caster, target *Unit, def AbilityDef) {
	prog := compileLegacyAbility(def)
	eff := s.effectiveSpellLocked(caster, def)
	s.spendUnitManaLocked(caster, eff.ManaCost)
	ctx := &RuntimeAbilityContext{
		CasterID:      caster.ID,
		AbilityID:     def.ID,
		InitialTarget: target.ID,
		abilityDef:    &def,
		program:       prog,
		Named:         map[string]ContextValue{},
	}
	s.runProgramTriggersLocked(ctx, prog.Triggers, TriggerOnCastComplete)
}

// ── (1) Divine Healer ───────────────────────────────────────────────────────

// TestAbilityCompileGolden_Heal_DivineHealerScalesExecutorHeal proves the
// executor's restore_health action scales its heal amount by
// perkClericHealOutputMultiplierLocked exactly like legacy
// resolveAbilityCastOnTargetLocked does. Before the fix, the executor scene's
// ally would only gain the raw (unscaled) HealAmount while the legacy scene's
// ally gains the divine_healer-multiplied amount — a real, silent gameplay
// regression the instant "heal" (or any heal-category ability) goes v2.
func TestAbilityCompileGolden_Heal_DivineHealerScalesExecutorHeal(t *testing.T) {
	// heal is now schemaVersion:2 in the live catalog (composable-abilities
	// migration); this test is specifically about the GENERIC executor
	// perk-scaling seam (restore_health's effectiveAbilityHealLocked call),
	// exercised via compileLegacyAbility on a legacy-shaped def, not about
	// whatever program the catalog happens to ship. The frozen fixture
	// (ability_legacy_fixtures_test.go) is the pre-migration heal shape and
	// is what both runHealThroughLegacy and runHealThroughExecutor compile
	// from below.
	def := legacyHealFixture()
	dhDef := perkDefByID("divine_healer")
	if dhDef == nil {
		t.Fatal(`perkDefByID("divine_healer") = nil`)
	}

	sLegacy, casterL, allyL := buildGoldenHealScenePerk(t, "divine_healer")
	defer sLegacy.mu.Unlock()
	sExec, casterE, allyE := buildGoldenHealScenePerk(t, "divine_healer")
	defer sExec.mu.Unlock()

	mult := dhDef.ConfigForRank(casterL.Rank)["healMultiplier"]
	if mult <= 1.0 {
		t.Fatalf("divine_healer healMultiplier = %v, want > 1.0 for this test to be meaningful", mult)
	}
	wantHeal := int(float64(def.HealAmount)*mult + 0.5) // round-half-up, matches math.Round for positive values

	preLegacyHP, preExecHP := allyL.HP, allyE.HP

	runHealThroughLegacy(sLegacy, casterL, allyL, def)
	runHealThroughExecutor(sExec, casterE, allyE, def)

	if gotL := allyL.HP - preLegacyHP; gotL != wantHeal {
		t.Fatalf("legacy fixture drifted: legacy heal = %d, want %d (def.HealAmount %d * divine_healer %v)", gotL, wantHeal, def.HealAmount, mult)
	}
	if gotE := allyE.HP - preExecHP; gotE != wantHeal {
		t.Fatalf("executor did not honour divine_healer: executor heal = %d, want %d (parity with legacy heal)", gotE, wantHeal)
	}

	assertScenesEquivalent(t, sLegacy, sExec, "heal/divine_healer")
}

// ── (2) onPerkAbilityResolvedLocked (battle_prayer) ─────────────────────────

// TestAbilityCompileGolden_Heal_BattlePrayerFiresOnExecutorPath proves the
// executor's restore_health action fires onPerkAbilityResolvedLocked exactly
// once per healed target, same as legacy resolveAbilityCastOnTargetLocked.
// Before the fix, the executor scene's ally would have zero BattlePrayer
// buff fields while the legacy scene's ally has them stamped — the hook
// never fired on the composable path at all.
func TestAbilityCompileGolden_Heal_BattlePrayerFiresOnExecutorPath(t *testing.T) {
	// See the frozen-fixture note on TestAbilityCompileGolden_Heal_DivineHealerScalesExecutorHeal
	// above — heal is schemaVersion:2 in the live catalog now; this test
	// exercises the generic executor perk-hook seam via a legacy-shaped def.
	def := legacyHealFixture()
	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal(`perkDefByID("battle_prayer") = nil`)
	}

	sLegacy, casterL, allyL := buildGoldenHealScenePerk(t, "battle_prayer")
	defer sLegacy.mu.Unlock()
	sExec, casterE, allyE := buildGoldenHealScenePerk(t, "battle_prayer")
	defer sExec.mu.Unlock()

	cfg := bpDef.ConfigForRank(casterL.Rank)
	wantDuration := cfg["buffDurationSeconds"]
	wantMult := cfg["attackSpeedMultiplier"]
	if wantDuration <= 0 || wantMult <= 0 {
		t.Fatalf("battle_prayer config drifted: duration=%v mult=%v, want both > 0", wantDuration, wantMult)
	}

	runHealThroughLegacy(sLegacy, casterL, allyL, def)
	runHealThroughExecutor(sExec, casterE, allyE, def)

	if allyL.PerkState.BattlePrayerRemaining != wantDuration || allyL.PerkState.BattlePrayerMultiplier != wantMult {
		t.Fatalf("legacy fixture drifted: legacy ally BattlePrayer = (remaining=%v,mult=%v), want (remaining=%v,mult=%v)",
			allyL.PerkState.BattlePrayerRemaining, allyL.PerkState.BattlePrayerMultiplier, wantDuration, wantMult)
	}
	if allyE.PerkState.BattlePrayerRemaining != wantDuration || allyE.PerkState.BattlePrayerMultiplier != wantMult {
		t.Fatalf("executor did not fire onPerkAbilityResolvedLocked: executor ally BattlePrayer = (remaining=%v,mult=%v), want (remaining=%v,mult=%v) (parity with legacy)",
			allyE.PerkState.BattlePrayerRemaining, allyE.PerkState.BattlePrayerMultiplier, wantDuration, wantMult)
	}

	assertScenesEquivalent(t, sLegacy, sExec, "heal/battle_prayer")
}

// ── (3) unstable_magic honours the caller-supplied EffectiveSpell ──────────

// buildUnstableMagicShatterScene registers abilityID as a copy of the catalog
// "shatter" ability (optionally compiled to SchemaVersion 2), spawns a caster
// carrying unstable_magic (rolled elsewhere; this test calls
// fireUnstableMagicLocked directly to avoid RNG-driven flakiness — see the
// call site) with abilityID as its ONLY learned pool spell (so
// randomLearnedSpellLocked deterministically picks it regardless of the perk
// RNG), and a lone enemy standing at the point the proc will target. Lock
// held on return; caller must s.mu.Unlock().
func buildUnstableMagicShatterScene(t *testing.T, abilityID string, v2 bool) (s *GameState, caster, enemy *Unit) {
	t.Helper()
	// shatter is now schemaVersion:2 in the live catalog; this test builds
	// its OWN v1/v2 pair from the frozen pre-migration shape specifically to
	// compare the legacy and executor paths against each other for the same
	// starting def (see ability_legacy_fixtures_test.go).
	shatterDef := legacyShatterFixture()
	testDef := shatterDef
	testDef.ID = abilityID
	if v2 {
		testDef.SchemaVersion = 2
		testDef.Program = compileLegacyAbility(shatterDef)
	}
	registerRuntimeTestAbility(t, testDef)

	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	grantPerk(caster, "unstable_magic")
	caster.PoolSpellsByRank = map[string]string{"1": abilityID}

	enemy = teamCombatUnit(t, s, "p2", 150, 0) // within shatter's burst radius of itself
	enemy.HP, enemy.MaxHP = 500, 500

	return s, caster, enemy
}

// TestUnstableMagic_HonoursCallerEffectiveSpell_ManaAndDamage is a legacy-vs-
// executor twin-scene test for fireUnstableMagicLocked's TargetsPoint branch
// (perks_arch_mage.go): abilityID_v1 (SchemaVersion 0) exercises the existing
// legacy resolve path unchanged; abilityID_v2 (SchemaVersion 2, compiled from
// the identical shatter def) exercises resolveAbilityCastAtPointLocked's
// composable branch. Both are driven through the SAME fireUnstableMagicLocked
// call. Before the fix: the v2 scene would (a) spend real mana instead of
// staying a free proc, and (b) deal FULL (unscaled) damage instead of the
// reduced-effectiveness amount — because resolveAbilityProgramCastLocked
// discarded the caller's zeroed-mana, effectiveness-scaled EffectiveSpell and
// re-derived its own from scratch.
func TestUnstableMagic_HonoursCallerEffectiveSpell_ManaAndDamage(t *testing.T) {
	umDef := perkDefByID("unstable_magic")
	if umDef == nil {
		t.Fatal(`perkDefByID("unstable_magic") = nil`)
	}

	sLegacy, casterL, enemyL := buildUnstableMagicShatterScene(t, "unstable_shatter_v1_test", false)
	defer sLegacy.mu.Unlock()
	sExec, casterE, enemyE := buildUnstableMagicShatterScene(t, "unstable_shatter_v2_test", true)
	defer sExec.mu.Unlock()

	effectiveness := umDef.ConfigForRank(casterL.Rank)["effectiveness"]
	if effectiveness <= 0 || effectiveness >= 1 {
		t.Fatalf("unstable_magic effectiveness = %v, want in (0,1) for this test to be meaningful", effectiveness)
	}

	// Independently derive the expected per-hit damage from the SAME formula
	// fireUnstableMagicLocked uses (effectiveSpellLocked then
	// scaleEffectiveSpellDamage), never a hardcoded balance number.
	baseEff := sLegacy.effectiveSpellLocked(casterL, mustGetAbilityDef(t, "unstable_shatter_v1_test"))
	wantDamage := scaleEffectiveSpellDamage(baseEff, effectiveness).Damage
	if wantDamage <= 0 || wantDamage >= baseEff.Damage {
		t.Fatalf("wantDamage = %d, want in (0, %d) — the scaled damage must be strictly less than the full amount for this test to be meaningful", wantDamage, baseEff.Damage)
	}

	preManaL, preManaE := casterL.CurrentMana, casterE.CurrentMana
	preHPL, preHPE := enemyL.HP, enemyE.HP

	sLegacy.fireUnstableMagicLocked(casterL, enemyL, effectiveness)
	sExec.fireUnstableMagicLocked(casterE, enemyE, effectiveness)

	// Free proc: mana must be untouched on BOTH scenes.
	if casterL.CurrentMana != preManaL {
		t.Fatalf("legacy fixture drifted: legacy caster mana %d -> %d, want unchanged (free proc)", preManaL, casterL.CurrentMana)
	}
	if casterE.CurrentMana != preManaE {
		t.Fatalf("executor spent mana on a free proc: executor caster mana %d -> %d, want unchanged %d (resolveAbilityProgramCastLocked must honour the caller's zeroed ManaCost)",
			preManaE, casterE.CurrentMana, preManaE)
	}

	// Reduced-effectiveness damage: both scenes must take EXACTLY wantDamage,
	// not the full un-scaled amount.
	if gotL := preHPL - enemyL.HP; gotL != wantDamage {
		t.Fatalf("legacy fixture drifted: legacy enemy took %d damage, want %d (effectiveness-scaled)", gotL, wantDamage)
	}
	if gotE := preHPE - enemyE.HP; gotE != wantDamage {
		t.Fatalf("executor dealt unscaled damage: executor enemy took %d damage, want %d (effectiveness-scaled, parity with legacy) — resolveAbilityProgramCastLocked must honour the caller's scaled EffectiveSpell",
			gotE, wantDamage)
	}
}

// ── (4) unstable_magic honours the caller-supplied EffectiveSpell — UNIT-TARGETED branch ──

// legacyFireballFixtureForProcTest is a frozen snapshot of
// catalog/abilities/fireball/fireball.json taken specifically for this test.
// fireball is schemaVersion 0 (legacy) as of this writing — unlike shatter/
// meteor/heal/etc. (ability_legacy_fixtures_test.go), it has not been migrated
// yet, so reading getAbilityDef("fireball") live would work today. It is
// frozen here anyway (matching the same convention) because the very next
// planned task flips fireball/chain_lightning to schemaVersion:2 in the live
// catalog: freezing the pre-migration shape now means this test keeps
// proving the general "unit-targeted proc honours the caller's
// EffectiveSpell" invariant across that migration instead of silently
// losing its legacy leg the moment it lands.
func legacyFireballFixtureForProcTest() AbilityDef {
	return AbilityDef{
		ID:                     "fireball",
		DisplayName:            "Fireball",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryOffensive,
		ManaCost:               18,
		DamageAmount:           90,
		Radius:                 100,
		CastRange:              400,
		CastTime:               0.6,
		Cooldown:               6,
		DamageType:             DamageFire,
		Projectile:             "fire_bolt",
		ProjectileScale:        2.5,
		Tags:                   []string{"aoe", "projectile", "damage"},
		CanTargetSelf:          false,
		CanTargetAllies:        false,
		CanTargetEnemies:       true,
		CasterAnimation:        "Attacking",
		Icon:                   "TODO/abilities/fireball.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "closest_enemy_in_range",
		DefaultAutoCast:        true,
	}
}

// buildUnstableMagicFireballScene mirrors buildUnstableMagicShatterScene, but
// for fireball — a UNIT-targeted (TargetsPoint == false) projectile ability,
// exercising fireUnstableMagicLocked's OTHER branch (the one that used to
// bypass the SchemaVersion check into resolveAbilityCastOnTargetLocked
// directly).
//
// Unlike buildUnstableMagicShatterScene, the v2 leg here is built through the
// REAL ConvertLegacyAbility (not a hand-rolled SchemaVersion+Program stamp):
// that is what actually reproduces the production bug. ConvertLegacyAbility
// clears every legacy mechanic field (DamageAmount, Radius, Projectile, ...)
// on the converted def, per the SchemaVersion 2 contract — so
// effectiveSpellLocked(caster, testDefV2) legitimately produces eff.Damage ==
// 0 for the v2 def, exactly like a real migrated ability. A test that instead
// left DamageAmount non-zero on the "v2" copy (while only stamping
// SchemaVersion+Program) would pass even with the pre-fix bypass — eff.Damage
// would already carry the correct scaled number regardless of which resolver
// function got called, masking the routing bug entirely (verified while
// building this test). Routing through ConvertLegacyAbility instead makes the
// two legs diverge exactly the way they do in production: the legacy resolver
// reads eff.Damage (0, post-clear) while the executor resolver reads the
// compiled Program's baked config.Amount (captured at compile time, before
// clearing) scaled by the caller's DamageEffectivenessMultiplier.
//
// Lock held on return; caller must s.mu.Unlock().
func buildUnstableMagicFireballScene(t *testing.T, abilityID string, v2 bool) (s *GameState, caster, enemy *Unit) {
	t.Helper()
	fireballDef := legacyFireballFixtureForProcTest()
	testDef := fireballDef
	testDef.ID = abilityID
	registerRuntimeTestAbility(t, testDef)
	if v2 {
		conv, _, err := ConvertLegacyAbility(abilityID)
		if err != nil {
			t.Fatalf("ConvertLegacyAbility(%q) = _, _, %v", abilityID, err)
		}
		registerRuntimeTestAbility(t, conv)
	}

	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)

	caster = teamCombatUnit(t, s, "p1", 0, 0)
	caster.MaxMana, caster.CurrentMana = 100, 100
	grantPerk(caster, "unstable_magic")
	caster.PoolSpellsByRank = map[string]string{"1": abilityID}

	enemy = teamCombatUnit(t, s, "p2", 150, 0) // within fireball's cast range
	enemy.HP, enemy.MaxHP = 500, 500

	return s, caster, enemy
}

// TestUnstableMagic_HonoursCallerEffectiveSpell_UnitTargetedManaAndDamage is
// the unit-targeted twin (fireball) of
// TestUnstableMagic_HonoursCallerEffectiveSpell_ManaAndDamage (shatter,
// TargetsPoint branch): abilityID_v1 (SchemaVersion 0) exercises the existing
// legacy resolve path unchanged; abilityID_v2 (SchemaVersion 2, compiled from
// the identical fireball def) exercises the composable branch. Both are
// driven through the SAME fireUnstableMagicLocked call, through its
// def.TargetsPoint == false branch.
//
// Before the fix (perks_arch_mage.go calling resolveAbilityCastOnTargetLocked
// directly, bypassing the SchemaVersion check resolveAbilityCastAtPointLocked
// already had): the v2 scene would deal ZERO damage — the legacy-only
// applier reads def.DamageAmount, which ConvertLegacyAbility clears to 0 on
// migration — instead of routing through the executor and honouring the
// caller's effectiveness-scaled EffectiveSpell. It would also never spend
// (or, here, never correctly no-op) mana through the shared seam.
func TestUnstableMagic_HonoursCallerEffectiveSpell_UnitTargetedManaAndDamage(t *testing.T) {
	umDef := perkDefByID("unstable_magic")
	if umDef == nil {
		t.Fatal(`perkDefByID("unstable_magic") = nil`)
	}

	sLegacy, casterL, enemyL := buildUnstableMagicFireballScene(t, "unstable_fireball_v1_test", false)
	defer sLegacy.mu.Unlock()
	sExec, casterE, enemyE := buildUnstableMagicFireballScene(t, "unstable_fireball_v2_test", true)
	defer sExec.mu.Unlock()

	effectiveness := umDef.ConfigForRank(casterL.Rank)["effectiveness"]
	if effectiveness <= 0 || effectiveness >= 1 {
		t.Fatalf("unstable_magic effectiveness = %v, want in (0,1) for this test to be meaningful", effectiveness)
	}

	// Independently derive the expected per-hit damage from the SAME formula
	// fireUnstableMagicLocked uses (effectiveSpellLocked then
	// scaleEffectiveSpellDamage), never a hardcoded balance number.
	baseEff := sLegacy.effectiveSpellLocked(casterL, mustGetAbilityDef(t, "unstable_fireball_v1_test"))
	wantDamage := scaleEffectiveSpellDamage(baseEff, effectiveness).Damage
	if wantDamage <= 0 || wantDamage >= baseEff.Damage {
		t.Fatalf("wantDamage = %d, want in (0, %d) — the scaled damage must be strictly less than the full amount for this test to be meaningful", wantDamage, baseEff.Damage)
	}

	preManaL, preManaE := casterL.CurrentMana, casterE.CurrentMana
	preHPL, preHPE := enemyL.HP, enemyE.HP

	sLegacy.fireUnstableMagicLocked(casterL, enemyL, effectiveness)
	sExec.fireUnstableMagicLocked(casterE, enemyE, effectiveness)

	// fireball is a projectile ability: damage lands on impact, not
	// synchronously — advance both scenes' projectiles until the bolt lands
	// (mirrors landFireballOn, fireball_test.go).
	for i := 0; i < 80 && len(sLegacy.Projectiles) > 0; i++ {
		sLegacy.tickProjectilesLocked(0.05)
	}
	for i := 0; i < 80 && len(sExec.Projectiles) > 0; i++ {
		sExec.tickProjectilesLocked(0.05)
	}
	if len(sLegacy.Projectiles) != 0 {
		t.Fatal("legacy fixture drifted: legacy fireball proc bolt never landed")
	}
	if len(sExec.Projectiles) != 0 {
		t.Fatal("executor fireball proc bolt never landed — unit-targeted proc did not route through a mechanic that fires a projectile")
	}

	// Free proc: mana must be untouched on BOTH scenes.
	if casterL.CurrentMana != preManaL {
		t.Fatalf("legacy fixture drifted: legacy caster mana %d -> %d, want unchanged (free proc)", preManaL, casterL.CurrentMana)
	}
	if casterE.CurrentMana != preManaE {
		t.Fatalf("executor spent mana on a free proc: executor caster mana %d -> %d, want unchanged %d (resolveAbilityCastWithEffLocked must honour the caller's zeroed ManaCost)",
			preManaE, casterE.CurrentMana, preManaE)
	}

	// Reduced-effectiveness damage: both scenes must take EXACTLY wantDamage,
	// not the full un-scaled amount and NOT zero.
	if gotL := preHPL - enemyL.HP; gotL != wantDamage {
		t.Fatalf("legacy fixture drifted: legacy enemy took %d damage, want %d (effectiveness-scaled)", gotL, wantDamage)
	}
	if gotE := preHPE - enemyE.HP; gotE != wantDamage {
		t.Fatalf("executor dealt wrong damage: executor enemy took %d damage, want %d (effectiveness-scaled, parity with legacy) — the unit-targeted proc must route through the SchemaVersion branch (resolveAbilityCastWithEffLocked) instead of bypassing it into resolveAbilityCastOnTargetLocked directly",
			gotE, wantDamage)
	}
}

// mustGetAbilityDef looks up id via getAbilityDef, failing the test on miss.
func mustGetAbilityDef(t *testing.T, id string) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef(id)
	if !ok {
		t.Fatalf("getAbilityDef(%q) = _, false", id)
	}
	return def
}
