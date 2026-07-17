package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// Phase 4, Task 5 — LIVE CAST PATH WIRING
//
// These tests prove that a SchemaVersion>=2 ability (with a compiled/authored
// Program) is routed through the composable executor when cast via the real
// beginAbilityCastLocked / beginAbilityCastAtPointLocked entry points, and —
// critically — that a legacy ability (SchemaVersion<2, Program nil) is
// completely unaffected: it must resolve exactly as it did before this task.
//
// Registration mechanism: abilities are injected directly into the
// runtimeAbilities overlay map (the same seam getAbilityDef reads first,
// see ability_defs.go:625-634) under runtimeAbilitiesMu, mirroring the
// pattern ability_persistence_test.go uses (e.g. TestAbilityDiskRoundTrip's
// direct `delete(runtimeAbilities, ...)`). This avoids disk I/O
// (SaveAbilityDef) entirely — tests register a fully-formed in-memory
// AbilityDef and clean it up via t.Cleanup.
// ═════════════════════════════════════════════════════════════════════════════

// registerRuntimeTestAbility injects def into the runtimeAbilities overlay so
// getAbilityDef(def.ID) resolves it, and schedules removal so the ability
// does not leak into other tests sharing the package-global overlay map.
func registerRuntimeTestAbility(t *testing.T, def AbilityDef) {
	t.Helper()
	runtimeAbilitiesMu.Lock()
	runtimeAbilities[def.ID] = def
	runtimeAbilitiesMu.Unlock()
	t.Cleanup(func() {
		runtimeAbilitiesMu.Lock()
		delete(runtimeAbilities, def.ID)
		runtimeAbilitiesMu.Unlock()
	})
}

// ── Test A: SchemaVersion 2 unit-target heal routes through the executor ────

func TestLiveCast_SchemaV2_UnitHeal_RoutesToExecutor(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	// Clear the catalog-seeded "heal" auto-cast so it can't fire during
	// advance() and mutate ally HP behind this test's assertions (same
	// precaution healSetup takes).
	if app.AutoCastEnabled != nil {
		delete(app.AutoCastEnabled, "heal")
	}
	ally := spawnProjTestUnit(t, s, "p1", 450, 400) // 50px away, within 220 range
	ally.HP = ally.MaxHP - 50                       // missing more than the heal amount, no overheal clamp

	// Build a legacy-shaped def carrying HealAmount so compileLegacyAbility
	// bakes it into the Program's restore_health action, THEN strip
	// HealAmount from the registered def. This is deliberate: if the
	// legacy flat-field HealAmount were left populated, the pre-wiring
	// legacy path (which reads def.HealAmount directly, ignoring Program
	// entirely) would apply the identical +15 and this test would pass
	// whether or not the executor is actually wired in — a false positive.
	// Zeroing HealAmount means the ONLY way this ally gets healed is via
	// the executor's restore_health action. The top-level cast-setup +
	// targeting fields (CanTarget*, CastRange, ManaCost, CastTime,
	// DamageType) stay populated so beginAbilityCastLocked's gates (which
	// read ONLY those top-level fields, never Program) work unchanged.
	const compiledHealAmount = 15
	const manaCost = 5
	legacy := AbilityDef{
		ID:              "heal_v2_test",
		Type:            AbilitySpell,
		CanTargetSelf:   true,
		CanTargetAllies: true,
		CastRange:       CastRange(220),
		ManaCost:        manaCost,
		CastTime:        0,
		DamageType:      DamageHoly,
		HealAmount:      compiledHealAmount,
		TargetCount:     1,
	}
	v2def := legacy
	v2def.SchemaVersion = 2
	v2def.Program = compileLegacyAbility(legacy)
	v2def.HealAmount = 0 // see comment above: proves the executor, not the legacy field, healed
	registerRuntimeTestAbility(t, v2def)

	app.Abilities = append(app.Abilities, v2def.ID)
	app.CurrentMana = 50

	allyID := ally.ID
	wantHP := ally.HP + compiledHealAmount
	startMana := app.CurrentMana
	wantMana := startMana - manaCost

	ok, reason := s.beginAbilityCastLocked(app, v2def.ID, ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	// CastTime is 0 ⇒ the ability resolves synchronously inside
	// beginAbilityCastLocked; no tick advance needed.
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != wantHP {
		t.Errorf("ally HP = %d; want %d (executor's restore_health should have healed +%d)", a.HP, wantHP, compiledHealAmount)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d (%d start - %d manaCost)", app.CurrentMana, wantMana, startMana, manaCost)
	}
}

// ── Test B: legacy (SchemaVersion 0) ability is completely unaffected ──────

// TestLiveCast_LegacyAbility_UnaffectedByWiring proves the Phase 4 Task 5
// wiring never touches a genuinely legacy (SchemaVersion<2, Program nil)
// ability's resolution. It registers its OWN synthetic legacy heal-shaped
// ability rather than relying on the catalog's "heal" — heal (along with
// greater_heal/shatter/raise_skeleton/meteor) is schemaVersion:2 in the live
// catalog as of the composable-abilities migration, so it can no longer
// serve as "a legacy ability" fixture. Building a synthetic one here keeps
// this test's guarantee independent of which catalog abilities happen to
// still be legacy-shaped at any given time.
func TestLiveCast_LegacyAbility_UnaffectedByWiring(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	app.MaxMana, app.CurrentMana = 100, 100
	// Catalog seeds heal auto-cast ON for player units at spawn (defaultAutoCast:
	// true); this test wants ONLY the synthetic legacy ability below to fire, so
	// clear it (same precaution healSetup takes for the same reason).
	if app.AutoCastEnabled != nil {
		delete(app.AutoCastEnabled, "heal")
	}
	ally := spawnProjTestUnit(t, s, "p1", 450, 400) // 50px away, within range

	def := AbilityDef{
		ID:              "legacy_heal_unaffected_test",
		Type:            AbilitySpell,
		CanTargetSelf:   true,
		CanTargetAllies: true,
		CastRange:       CastRange(220),
		ManaCost:        5,
		CastTime:        0,
		DamageType:      DamageHoly,
		HealAmount:      15,
		TargetCount:     1,
	}
	registerRuntimeTestAbility(t, def)
	app.Abilities = append(app.Abilities, def.ID)
	ally.HP = ally.MaxHP - def.HealAmount - 20 // missing > one heal, no overheal clamp

	allyID := ally.ID
	wantHP := ally.HP + def.HealAmount
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost
	ok, reason := s.beginAbilityCastLocked(app, def.ID, ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	// CastTime is 0 ⇒ the ability resolves synchronously inside
	// beginAbilityCastLocked; no tick advance needed (and none wanted — an
	// idle advance would accrue passive mana/HP regen and confound the exact
	// deltas asserted below for a reason that has nothing to do with this
	// test's purpose).

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != wantHP {
		t.Errorf("ally HP = %d; want %d (healed +%d, legacy path)", a.HP, wantHP, def.HealAmount)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d (%d start - %d manaCost, deducted on completion)", app.CurrentMana, wantMana, startMana, def.ManaCost)
	}
	if app.CastAbilityID != "" || app.Casting {
		t.Errorf("cast should be cleared after completion: CastAbilityID=%q Casting=%v", app.CastAbilityID, app.Casting)
	}
}

// ── Test B2: greater_heal's multi-target fan-out survives the migration ────

// TestLiveCast_GreaterHeal_MultiTargetFanOutPreserved is the direct,
// end-to-end proof for the "anchor-collapse" risk flagged when
// greater_heal was migrated to schemaVersion:2: buildCastTargetSetLocked's
// anchor-set selection normalises to a single target once def.TargetCount is
// cleared (as it is on every schemaVersion:2 def — see ConvertLegacyAbility),
// but resolveAbilityCastLocked's SchemaVersion>=2 branch only ever reads
// targets[0] as the InitialTarget and hands the REAL multi-target fan-out to
// the compiled select_targets query (MaxCount:3, IncludeInitialTarget:true,
// baked into the shipped Program independent of def.TargetCount). This test
// drives an actual cast through beginAbilityCastLocked (the real player-
// facing entry point, cast-time included) against the LIVE catalog
// "greater_heal" and asserts all three eligible allies are healed — proving
// the anchor collapse is harmless in production, not just in the golden
// test's synchronous twin-scene comparison (TestAbilityCompileGolden_GreaterHeal).
func TestLiveCast_GreaterHeal_MultiTargetFanOutPreserved(t *testing.T) {
	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal(`getAbilityDef("greater_heal") = _, false`)
	}
	if def.SchemaVersion < 2 || def.Program == nil {
		t.Fatalf("catalog greater_heal must be schemaVersion>=2 with a compiled Program for this test to prove anything: schemaVersion=%d program=%v", def.SchemaVersion, def.Program)
	}
	healAmount := abilityMechanicsShadow(def).HealAmount
	if healAmount <= 0 {
		t.Fatal("recovered HealAmount from the shipped Program is 0 — cannot derive an expected heal")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	setTeam(s, "p1", 0)

	// teamCombatUnit units carry AttackRange 90 (see buildGoldenGreaterHealScene
	// in ability_compile_golden_test.go), which is what greater_heal's
	// castRange:"match_attack_range" resolves to.
	caster := teamCombatUnit(t, s, "p1", 400, 400)
	caster.MaxMana, caster.CurrentMana = 100, 100
	if !containsAbility(caster, "greater_heal") {
		caster.Abilities = append(caster.Abilities, "greater_heal")
	}

	primary := teamCombatUnit(t, s, "p1", 440, 400) // 40px away
	primary.HP, primary.MaxHP = 20, 100             // most injured — the clicked target

	ally2 := teamCombatUnit(t, s, "p1", 480, 400) // 80px away
	ally2.HP, ally2.MaxHP = 50, 100

	prePrimaryHP, preAlly2HP := primary.HP, ally2.HP
	ok, reason := s.beginAbilityCastLocked(caster, "greater_heal", primary)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	advance(s, 40) // past greater_heal's 1.0s cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	clampedWant := func(pre int, u *Unit) int {
		want := pre + healAmount
		if want > u.MaxHP {
			want = u.MaxHP
		}
		return want
	}
	if want := clampedWant(prePrimaryHP, primary); primary.HP != want {
		t.Errorf("primary (clicked target) HP = %d, want %d", primary.HP, want)
	}
	if want := clampedWant(preAlly2HP, ally2); ally2.HP != want {
		t.Errorf("ally2 (AoE slot 3) HP = %d, want %d — anchor collapse must not have dropped this slot", ally2.HP, want)
	}
}

// ── Test C: SchemaVersion 2 point-AoE routes through the executor ──────────

func TestLiveCast_SchemaV2_PointAoE_RoutesToExecutor(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 400, 400)
	caster.MaxMana, caster.CurrentMana = 100, 100

	inRange := teamCombatUnit(t, s, "p2", 500, 400) // 100px from the cast point (500,400) below
	inRange.HP, inRange.MaxHP = 200, 200

	outOfRadius := teamCombatUnit(t, s, "p2", 900, 400) // control: far outside the AoE radius
	outOfRadius.HP, outOfRadius.MaxHP = 200, 200

	abilityID := "point_aoe_v2_test"
	def := AbilityDef{
		ID:            abilityID,
		Type:          AbilitySpell,
		SchemaVersion: 2,
		TargetsPoint:  true,
		CastRange:     CastRange(600),
		ManaCost:      10,
		CastTime:      0,
		DamageType:    DamageHoly,
		Program: &AbilityProgram{
			Entry: AbilityEntryDef{Type: EntryGroundPoint, Range: CastRange(600)},
			Triggers: []AbilityTriggerDef{
				{
					ID:   "cast",
					Type: TriggerOnCastComplete,
					Actions: []AbilityActionDef{
						{
							ID:   "sel",
							Type: ActionSelectTargets,
							Target: &TargetQueryDef{
								Source:    SrcAllInScene,
								Origin:    OriginCastPoint,
								Relations: []TargetRelation{RelEnemy},
								Radius:    150,
							},
							Outputs: map[string]string{"targets": "aoeTargets"},
						},
						{
							ID:     "dmg",
							Type:   ActionDealDamage,
							Input:  map[string]ContextRef{"targets": {Key: "aoeTargets"}},
							Config: marshalConfig(dealDamageConfig{Amount: 40, Type: DamageHoly}),
						},
					},
				},
			},
		},
	}
	registerRuntimeTestAbility(t, def)
	caster.Abilities = append(caster.Abilities, abilityID)

	wantMana := caster.CurrentMana - def.ManaCost
	ok, reason := s.beginAbilityCastAtPointLocked(caster, abilityID, 500, 400)
	if !ok {
		t.Fatalf("beginAbilityCastAtPointLocked failed: %q", reason)
	}

	if inRange.HP != 160 {
		t.Errorf("inRange.HP = %d; want 160 (200 - 40 executor deal_damage)", inRange.HP)
	}
	if outOfRadius.HP != 200 {
		t.Errorf("outOfRadius.HP = %d; want 200 (outside the 150px AoE radius, untouched)", outOfRadius.HP)
	}
	if caster.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d (%d manaCost spent once)", caster.CurrentMana, wantMana, def.ManaCost)
	}
	if reason != "" {
		t.Errorf("unexpected failure reason on success: %q", reason)
	}
}
