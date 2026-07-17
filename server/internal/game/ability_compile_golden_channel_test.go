package game

import "testing"

// ═════════════════════════════════════════════════════════════════════════════
// Golden equivalence test for channel_beam (siphon_life — migrated to
// schemaVersion:2 in the live catalog; the LAST ability migrated, finishing
// the composable-abilities catalog).
//
// Like meteor's and arcane_orb's golden tests, siphon_life's effect is not
// resolved synchronously at cast time — it persists across many future
// ticks, driven entirely by Unit.Channel* state (ability_channel.go). Both
// legs therefore call the SAME production entry point
// (beginAbilityCastLocked, which dispatches into beginAbilityChannelLocked
// via def.IsChannelAbility()) and are then driven by the real
// GameState.Update loop (tickUnitChannelLocked), rather than resolved with
// one synchronous call like the shatter/heal/projectile golden tests.
//
// Legacy leg drives the FROZEN pre-migration fixture (legacySiphonLifeFixture,
// ability_legacy_fixtures_test.go), registered under a scratch ability id via
// registerRuntimeTestAbility so beginAbilityCastLocked/beginAbilityChannelLocked's
// own getAbilityDef-by-id lookups resolve it. Unlike the heal/shatter/meteor
// golden tests, this can't bypass the id lookup by handing the fixture
// AbilityDef directly to a `def AbilityDef` parameter — beginAbilityCastLocked
// and beginAbilityChannelLocked both take an `abilityID string`, matching
// their real caller (RequestAbilityCast). Executor leg drives the ACTUAL
// shipped catalog def under its real id ("siphon_life").
//
// Both casters are spawned already injured (HP = MaxHP/2) so the self-heal
// path fires on every tick, and both scenes run to mana exhaustion so the
// STOP path (castFailNotEnoughMana, beam despawn) is proven equivalent too —
// not just the steady-state damage/heal/mana-decay ticks.
// ═════════════════════════════════════════════════════════════════════════════

// buildGoldenChannelScene spawns an injured caster (self-heal path) and a
// hostile target within siphon_life's 220 cast range, both with Damage=0 so
// no incidental basic-attack combat perturbs the scene — same neutering
// discipline as buildGoldenArcaneOrbScene. Lock held on return; caller must
// s.mu.Unlock() before ticking via GameState.Update.
func buildGoldenChannelScene(t *testing.T, abilityID string, maxMana int) (s *GameState, caster, target *Unit) {
	t.Helper()
	s = newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)
	setTeam(s, "p2", 1)

	caster = teamCombatUnit(t, s, "p1", 100, 100)
	caster.Damage = 0
	caster.HP = caster.MaxHP / 2 // injured — exercises the self-heal path
	caster.MaxMana, caster.CurrentMana = maxMana, maxMana
	caster.Abilities = []string{abilityID}

	target = teamCombatUnit(t, s, "p2", 200, 100) // 100px away, within castRange 220
	target.Damage = 0
	target.HP, target.MaxHP = 500, 500

	return s, caster, target
}

func TestAbilityCompileGolden_SiphonLife(t *testing.T) {
	legacyDef := legacySiphonLifeFixture()
	legacyDef.ID = "siphon_life_legacy_golden_test"
	registerRuntimeTestAbility(t, legacyDef)
	catalogDef := requireMigratedV2(t, "siphon_life")
	if catalogDef.CastRange != legacyDef.CastRange {
		t.Fatalf("fixture drifted from catalog: CastRange legacy=%v catalog=%v", legacyDef.CastRange, catalogDef.CastRange)
	}
	if legacyDef.DamagePerTick <= 0 || legacyDef.ManaCostPerTick <= 0 || legacyDef.TickIntervalSeconds <= 0 {
		t.Fatalf("fixture drifted: DamagePerTick=%v ManaCostPerTick=%v TickIntervalSeconds=%v, want all > 0",
			legacyDef.DamagePerTick, legacyDef.ManaCostPerTick, legacyDef.TickIntervalSeconds)
	}

	const startMana = 50 // exactly ManaCostPerTick(1) * 50 ticks — exhausts partway through the run below

	sLegacy, casterL, targetL := buildGoldenChannelScene(t, legacyDef.ID, startMana)
	sExec, casterE, targetE := buildGoldenChannelScene(t, "siphon_life", startMana)

	// Legacy: frozen pre-migration fixture, through the SAME production entry
	// point (beginAbilityCastLocked) the exec leg uses below.
	okL, reasonL := sLegacy.beginAbilityCastLocked(casterL, legacyDef.ID, targetL)
	sLegacy.mu.Unlock()
	if !okL {
		t.Fatalf("legacy fixture failed to start channel: %q", reasonL)
	}

	// Executor: the ACTUAL shipped catalog def (schemaVersion 2), through the
	// identical production entry point. beginAbilityCastLocked's own
	// def.IsChannelAbility() branch routes this to beginAbilityChannelLocked,
	// which fires the channel_beam action's Execute for a v2 def — nothing
	// here is bespoke test wiring.
	okE, reasonE := sExec.beginAbilityCastLocked(casterE, "siphon_life", targetE)
	sExec.mu.Unlock()
	if !okE {
		t.Fatalf("executor catalog def failed to start channel: %q", reasonE)
	}

	// Drive well past the mana budget (50 ticks) so damage/heal/mana-decay
	// fire repeatedly AND the mana-exhaustion stop path (castFailNotEnoughMana,
	// beam despawn) is exercised on both legs.
	interval := legacyDef.TickIntervalSeconds
	for i := 0; i < 70; i++ {
		sLegacy.Update(interval)
		sExec.Update(interval)
	}

	sLegacy.mu.Lock()
	sExec.mu.Lock()
	defer sLegacy.mu.Unlock()
	defer sExec.mu.Unlock()

	// Sanity: the channel actually did something on the legacy side (catches
	// a vacuous pass where both paths silently did nothing).
	if targetL.HP == targetL.MaxHP {
		t.Fatalf("legacy fixture drifted: target took no channel damage (HP still %d)", targetL.HP)
	}
	if casterL.LastCastFailure != castFailNotEnoughMana {
		t.Fatalf("legacy fixture drifted: expected the channel to run out of mana and stop; LastCastFailure=%q", casterL.LastCastFailure)
	}
	if casterL.ChannelAbilityID != "" {
		t.Fatalf("legacy fixture drifted: channel should have stopped after mana exhaustion")
	}

	// Executor must match the legacy leg on every gameplay axis: damage
	// dealt, self-heal applied, mana decayed to the same floor, the same
	// stop reason, and the beam despawned identically.
	if targetE.HP != targetL.HP {
		t.Errorf("target HP mismatch: legacy=%d exec=%d", targetL.HP, targetE.HP)
	}
	if casterE.HP != casterL.HP {
		t.Errorf("caster HP mismatch (self-heal): legacy=%d exec=%d", casterL.HP, casterE.HP)
	}
	if casterE.CurrentMana != casterL.CurrentMana {
		t.Errorf("caster mana mismatch: legacy=%d exec=%d", casterL.CurrentMana, casterE.CurrentMana)
	}
	if casterE.LastCastFailure != casterL.LastCastFailure {
		t.Errorf("stop-reason mismatch: legacy=%q exec=%q", casterL.LastCastFailure, casterE.LastCastFailure)
	}
	if casterE.ChannelAbilityID != casterL.ChannelAbilityID {
		t.Errorf("channel-active mismatch after stop: legacy=%q exec=%q", casterL.ChannelAbilityID, casterE.ChannelAbilityID)
	}
	if len(sLegacy.Beams) != len(sExec.Beams) {
		t.Errorf("beam count mismatch: legacy=%d exec=%d", len(sLegacy.Beams), len(sExec.Beams))
	}

	assertScenesEquivalent(t, sLegacy, sExec, "siphon_life")
}
