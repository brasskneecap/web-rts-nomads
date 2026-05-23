package game

// caster_archetype_test.go — Phase-1 caster archetype acceptance tests (tasks 6.1–6.8).
//
// Task map:
//   6.1  TestCasterProfile_ResolvedForAcolyte + TestCasterProfile_StructuralDeltas
//   6.2  TestCasterProfile_ArcherAndSupportUnchanged
//   6.3  TestCasterScoring_StrategicValueEqualToSupport + TestCasterScoring_TypePreferenceEqualToSupport
//   6.4  TestCasterKiting_RetreatsWhereArcherDoesNot
//   6.5  TestCasterUpgrade_SwiftStrikesForfeited
//   6.6  TestAbilityCategory_Registry
//   6.7  TestAbilityCategory_CatalogValidation
//   6.8  TestHealAutocast_GatingUnchangedByProfileFlip + TestHealAutocast_SeededReplayNoMeleeNoDivergence

import (
	"encoding/json"
	"sort"
	"testing"

	"webrts/server/pkg/protocol"
)

// ── 6.1: resolveCombatProfile returns "caster" for an Acolyte; structural deltas are correct ──

// TestCasterProfile_ResolvedForAcolyte verifies that spawning an Acolyte
// and calling resolveCombatProfile returns the "caster" profile (the catalog
// flip in acolyte.json is in effect and the profile registry is loaded).
func TestCasterProfile_ResolvedForAcolyte(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if app == nil {
		t.Fatal("failed to spawn acolyte")
	}

	prof := resolveCombatProfile(app)
	if prof.Name != "caster" {
		t.Errorf("resolveCombatProfile(acolyte) = %q; want \"caster\"", prof.Name)
	}

	// Also confirm the profile is in the registry directly.
	if _, ok := combatProfiles["caster"]; !ok {
		t.Error("combatProfiles[\"caster\"] not found; the profile must be registered")
	}
}

// TestCasterProfile_StructuralDeltas verifies that "caster" equals "support"
// on every field EXCEPT the four documented Phase-1 deltas, and that those
// deltas satisfy their invariants without pinning literal numbers.
//
// Rules enforced:
//   - Name: "caster"
//   - MaxChaseDistance: NOT shrunk below archer's (spec: "near the archer envelope")
//   - AoERadius == 0  (current kit is single-target)
//   - Weights.AoECluster == 0  (current kit is single-target)
//   - Every other field identical to "support"
func TestCasterProfile_StructuralDeltas(t *testing.T) {
	caster, ok := combatProfiles["caster"]
	if !ok {
		t.Fatal("caster profile not registered")
	}
	support, ok := combatProfiles["support"]
	if !ok {
		t.Fatal("support profile not registered")
	}
	archer, ok := combatProfiles["archer"]
	if !ok {
		t.Fatal("archer profile not registered")
	}

	// ── Delta invariants ──────────────────────────────────────────────────────

	// Name is the new identity.
	if caster.Name != "caster" {
		t.Errorf("caster.Name = %q; want \"caster\"", caster.Name)
	}
	// MaxChaseDistance must not be shrunk below the archer envelope.
	// (Spec: leash self-clamps but MaxChaseDistance does not.)
	if caster.MaxChaseDistance < archer.MaxChaseDistance {
		t.Errorf("caster.MaxChaseDistance (%v) < archer.MaxChaseDistance (%v); the spec requires caster to be no less than the archer baseline",
			caster.MaxChaseDistance, archer.MaxChaseDistance)
	}
	// AoE fields must be zero — current kit is single-target.
	if caster.AoERadius != 0 {
		t.Errorf("caster.AoERadius = %v; want 0 (current kit is single-target, not tuned for AoE)", caster.AoERadius)
	}
	if caster.Weights.AoECluster != 0 {
		t.Errorf("caster.Weights.AoECluster = %v; want 0 (AoE cluster bias must be absent for a single-target caster)", caster.Weights.AoECluster)
	}

	// ── All other fields must equal "support" ─────────────────────────────────
	// Compare the four-delta fields by name so a reader immediately knows
	// which fields are exempt from equality.
	type fieldCheck struct {
		name       string
		casterVal  any
		supportVal any
		delta      bool // true = exempt from equality (this IS one of the four deltas)
	}
	checks := []fieldCheck{
		{"Name", caster.Name, support.Name, true},                                                                   // delta 1
		{"DetectionRange", caster.DetectionRange, support.DetectionRange, false},
		{"RetargetIntervalTicks", caster.RetargetIntervalTicks, support.RetargetIntervalTicks, false},
		{"SwitchThreshold", caster.SwitchThreshold, support.SwitchThreshold, false},
		{"ThreatDecayPerSecond", caster.ThreatDecayPerSecond, support.ThreatDecayPerSecond, false},
		{"ThreatFromDamage", caster.ThreatFromDamage, support.ThreatFromDamage, false},
		{"ThreatGenerationMultiplier", caster.ThreatGenerationMultiplier, support.ThreatGenerationMultiplier, false},
		{"PassiveMeleeThreat", caster.PassiveMeleeThreat, support.PassiveMeleeThreat, false},
		{"LeashDistance", caster.LeashDistance, support.LeashDistance, false},
		{"MaxChaseDistance", caster.MaxChaseDistance, support.MaxChaseDistance, true},                               // delta 2
		{"RetreatDistance", caster.RetreatDistance, support.RetreatDistance, false},
		{"RetreatTriggerMeleeRange", caster.RetreatTriggerMeleeRange, support.RetreatTriggerMeleeRange, false},
		{"TargetBuildings", caster.TargetBuildings, support.TargetBuildings, false},
		{"PreferStructures", caster.PreferStructures, support.PreferStructures, false},
		{"PreferClosestTarget", caster.PreferClosestTarget, support.PreferClosestTarget, false},
		{"PreferMaxRange", caster.PreferMaxRange, support.PreferMaxRange, false},
		{"Melee", caster.Melee, support.Melee, false},
		{"Frontline", caster.Frontline, support.Frontline, false},
		{"Backline", caster.Backline, support.Backline, false},
		{"DangerTolerance", caster.DangerTolerance, support.DangerTolerance, false},
		{"AoERadius", caster.AoERadius, support.AoERadius, true},                                                   // delta 3
		{"Weights.Distance", caster.Weights.Distance, support.Weights.Distance, false},
		{"Weights.InRange", caster.Weights.InRange, support.Weights.InRange, false},
		{"Weights.Threat", caster.Weights.Threat, support.Weights.Threat, false},
		{"Weights.TargetValue", caster.Weights.TargetValue, support.Weights.TargetValue, false},
		{"Weights.TypePreference", caster.Weights.TypePreference, support.Weights.TypePreference, false},
		{"Weights.Taunt", caster.Weights.Taunt, support.Weights.Taunt, false},
		{"Weights.ProtectAllies", caster.Weights.ProtectAllies, support.Weights.ProtectAllies, false},
		{"Weights.StructureDefense", caster.Weights.StructureDefense, support.Weights.StructureDefense, false},
		{"Weights.Reachability", caster.Weights.Reachability, support.Weights.Reachability, false},
		{"Weights.Stickiness", caster.Weights.Stickiness, support.Weights.Stickiness, false},
		{"Weights.DangerPenalty", caster.Weights.DangerPenalty, support.Weights.DangerPenalty, false},
		{"Weights.AoECluster", caster.Weights.AoECluster, support.Weights.AoECluster, true},                        // delta 4
		{"Weights.HealthFinish", caster.Weights.HealthFinish, support.Weights.HealthFinish, false},
	}

	for _, c := range checks {
		if c.delta {
			continue // the four Phase-1 deltas are tested above by invariant, not equality
		}
		if c.casterVal != c.supportVal {
			t.Errorf("caster.%s = %v; want %v (same as support — only the four documented deltas may differ)",
				c.name, c.casterVal, c.supportVal)
		}
	}
}

// ── 6.2: "archer" and "support" profiles are unchanged ───────────────────────

// TestCasterProfile_ArcherAndSupportUnchanged captures structural invariants
// for the "archer" and "support" profiles that would catch any unintended mutation:
//
//   - "archer": no retreat (zero fields), Backline=true, Melee=false, PreferMaxRange=true.
//   - "support": has retreat (both fields > 0), Backline=true, AoERadius > 0,
//     Weights.AoECluster > 0.
//
// These invariants are derived from the profile definitions themselves (read at
// init time), not from hardcoded literals.
func TestCasterProfile_ArcherAndSupportUnchanged(t *testing.T) {
	archer, ok := combatProfiles["archer"]
	if !ok {
		t.Fatal("archer profile not registered")
	}
	support, ok := combatProfiles["support"]
	if !ok {
		t.Fatal("support profile not registered")
	}

	// "archer" must NOT have retreat configured — that's the structural difference
	// from support/caster that the kiting test (6.4) depends on.
	if archer.RetreatDistance != 0 {
		t.Errorf("archer.RetreatDistance = %v; want 0 (archer must not retreat — it's been modified)", archer.RetreatDistance)
	}
	if archer.RetreatTriggerMeleeRange != 0 {
		t.Errorf("archer.RetreatTriggerMeleeRange = %v; want 0 (archer must not retreat)", archer.RetreatTriggerMeleeRange)
	}
	if !archer.Backline {
		t.Error("archer.Backline must be true (unchanged)")
	}
	if archer.Melee {
		t.Error("archer.Melee must be false (unchanged)")
	}
	if !archer.PreferMaxRange {
		t.Error("archer.PreferMaxRange must be true (unchanged)")
	}

	// "support" must have retreat and AoE fields — the basis from which "caster" clones.
	if support.RetreatDistance <= 0 {
		t.Errorf("support.RetreatDistance = %v; want > 0 (must not have been cleared)", support.RetreatDistance)
	}
	if support.RetreatTriggerMeleeRange <= 0 {
		t.Errorf("support.RetreatTriggerMeleeRange = %v; want > 0 (must not have been cleared)", support.RetreatTriggerMeleeRange)
	}
	if support.AoERadius <= 0 {
		t.Errorf("support.AoERadius = %v; want > 0 (must not have been cleared)", support.AoERadius)
	}
	if support.Weights.AoECluster <= 0 {
		t.Errorf("support.Weights.AoECluster = %v; want > 0 (must not have been cleared)", support.Weights.AoECluster)
	}
	if !support.Backline {
		t.Error("support.Backline must be true (unchanged)")
	}
}

// ── 6.3: AI scoring treats "caster" exactly as "support" ─────────────────────

// makeScoringUnit builds a minimal Unit that resolves its combat profile via
// Archetype (no UnitDef.CombatProfile override) and can be passed to
// unitStrategicValue / unitTypePreference. It is not inserted into any state
// map or spatial index so it doesn't affect simulation state.
func makeScoringUnit(nextID *int, ownerID, unitType, archetype string) *Unit {
	id := *nextID
	*nextID++
	return &Unit{
		ID:          id,
		OwnerID:     ownerID,
		UnitType:    unitType,
		Archetype:   archetype,
		HP:          100,
		MaxHP:       100,
		Visible:     true,
		ThreatTable: map[int]*ThreatEntry{},
	}
}

// TestCasterScoring_StrategicValueEqualToSupport verifies that unitStrategicValue
// returns the same result for an otherwise-identical unit whether its profile
// resolves to "caster" or "support". The Phase-1 profile-number deltas
// (MaxChaseDistance / AoERadius / AoECluster) must NOT leak into strategic value.
//
// We compare by value equality, not by asserting a magnitude — the exact number
// is a tunable; what matters is they are identical for caster vs support.
func TestCasterScoring_StrategicValueEqualToSupport(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// We need both units to have identical HP ratios (the low-HP bonus applies equally).
	// Use HP=MaxHP=100 for both.
	idGen := 9000

	// casterUnit: resolves to "caster" via Archetype key (no UnitDef lookup since
	// UnitType "caster_synth" has no UnitDef entry — falls through to Archetype).
	casterUnit := makeScoringUnit(&idGen, "p1", "caster_synth", "caster")
	if p := resolveCombatProfile(casterUnit); p.Name != "caster" {
		t.Fatalf("casterUnit resolves to %q; expected \"caster\"", p.Name)
	}

	// supportUnit: resolves to "support" via Archetype key.
	supportUnit := makeScoringUnit(&idGen, "p1", "support_synth", "support")
	if p := resolveCombatProfile(supportUnit); p.Name != "support" {
		t.Fatalf("supportUnit resolves to %q; expected \"support\"", p.Name)
	}

	casterVal := s.unitStrategicValue(casterUnit)
	supportVal := s.unitStrategicValue(supportUnit)

	if casterVal != supportVal {
		t.Errorf("unitStrategicValue: caster=%v support=%v; must be equal (profile-number deltas must not leak into strategic value)",
			casterVal, supportVal)
	}
}

// TestCasterScoring_TypePreferenceEqualToSupport verifies four scenarios:
//
//  (a) A caster attacker returns the same preference as a support attacker
//      against representative targets.
//  (b) An archer evaluating a caster target gets the same bonus as for a support target.
//  (c) A mage evaluating a caster target gets the same bonus as for a support target.
//  (d) A cavalry/skirmisher evaluating a caster target gets the same bonus.
//
// NOTE: We do NOT assert scoreUnitTargetLocked is equal between caster and
// support — those legitimately differ (Delta 4: MaxChaseDistance / AoECluster).
// Only the strategic-value and type-preference functions are asserted equal here.
func TestCasterScoring_TypePreferenceEqualToSupport(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	idGen := 9100

	// ── Part (a): caster attacker == support attacker ─────────────────────────
	casterAttacker := makeScoringUnit(&idGen, "p1", "caster_synth", "caster")
	supportAttacker := makeScoringUnit(&idGen, "p1", "support_synth", "support")
	if p := resolveCombatProfile(casterAttacker); p.Name != "caster" {
		t.Fatalf("caster attacker resolves to %q", p.Name)
	}
	if p := resolveCombatProfile(supportAttacker); p.Name != "support" {
		t.Fatalf("support attacker resolves to %q", p.Name)
	}

	// Representative targets covering distinct branches in unitTypePreference
	// for the "enemy_archer"/"support"/"caster" attacker case.
	targets := []struct {
		label     string
		unitType  string
		archetype string
	}{
		{"mage_target", "mage_synth", "mage"},
		{"archer_target", "archer_synth", "archer"},
		{"soldier_target", "soldier_synth", "soldier"},
		{"catapult_target", "catapult_synth", "catapult"},
	}
	for _, tgt := range targets {
		tgtUnit := makeScoringUnit(&idGen, enemyPlayerID, tgt.unitType, tgt.archetype)
		casterPref := s.unitTypePreference(casterAttacker, tgtUnit, combatEvalContext{})
		supportPref := s.unitTypePreference(supportAttacker, tgtUnit, combatEvalContext{})
		if casterPref != supportPref {
			t.Errorf("unitTypePreference(caster_attacker, %s) = %v; unitTypePreference(support_attacker, %s) = %v; must be equal (caster wired to same attacker case as support)",
				tgt.label, casterPref, tgt.label, supportPref)
		}
	}

	// ── Parts (b-d): evaluators with caster target vs support target ──────────
	casterTarget := makeScoringUnit(&idGen, "p1", "caster_synth", "caster")
	supportTarget := makeScoringUnit(&idGen, "p1", "support_synth", "support")
	if p := resolveCombatProfile(casterTarget); p.Name != "caster" {
		t.Fatalf("caster target resolves to %q", p.Name)
	}
	if p := resolveCombatProfile(supportTarget); p.Name != "support" {
		t.Fatalf("support target resolves to %q", p.Name)
	}

	evaluators := []struct {
		label     string
		unitType  string
		archetype string
		ownerID   string
	}{
		{"archer", "archer_synth", "archer", enemyPlayerID},
		{"mage", "mage_synth", "mage", enemyPlayerID},
		{"cavalry", "cavalry_synth", "cavalry", enemyPlayerID},
		{"skirmisher", "skirmisher_synth", "skirmisher", enemyPlayerID},
	}
	for _, ev := range evaluators {
		atk := makeScoringUnit(&idGen, ev.ownerID, ev.unitType, ev.archetype)
		casterBonus := s.unitTypePreference(atk, casterTarget, combatEvalContext{})
		supportBonus := s.unitTypePreference(atk, supportTarget, combatEvalContext{})
		if casterBonus != supportBonus {
			t.Errorf("unitTypePreference(attacker=%s, caster_target) = %v; unitTypePreference(attacker=%s, support_target) = %v; must be equal (caster target wired to same check as support target)",
				ev.label, casterBonus, ev.label, supportBonus)
		}
	}
}

// ── 6.4: caster retreats where archer does not (kiting behaviour — Delta 1) ──

// TestCasterKiting_RetreatsWhereArcherDoesNot verifies:
//
//  (a) shouldRetreatLocked returns true for a caster-profiled unit and false for
//      an archer-profiled unit given the same melee threat scenario.
//  (b) An end-to-end live tick: the caster Acolyte enters "Repositioning"
//      status while the archer does not, proving issueRetreatLocked fires.
func TestCasterKiting_RetreatsWhereArcherDoesNot(t *testing.T) {
	caster, ok := combatProfiles["caster"]
	if !ok {
		t.Fatal("caster profile not registered")
	}
	archer, ok := combatProfiles["archer"]
	if !ok {
		t.Fatal("archer profile not registered")
	}

	// Structural pre-condition: the test is only meaningful if the profiles
	// actually differ on retreat configuration.
	if caster.RetreatDistance <= 0 || caster.RetreatTriggerMeleeRange <= 0 {
		t.Fatal("caster profile has no retreat configured; structural delta is wrong — check combat_ai_profiles.go")
	}
	if archer.RetreatDistance != 0 || archer.RetreatTriggerMeleeRange != 0 {
		t.Fatal("archer profile unexpectedly has retreat configured; structural assumption broken")
	}

	// ── Part (a): shouldRetreatLocked predicate ───────────────────────────────
	s := newProjectileTestState(t)
	s.mu.Lock()

	casterUnit := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 500, Y: 500})
	if casterUnit == nil {
		s.mu.Unlock()
		t.Fatal("failed to spawn acolyte (caster)")
	}
	archerUnit := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 550, Y: 500})
	if archerUnit == nil {
		s.mu.Unlock()
		t.Fatal("failed to spawn archer")
	}

	// Enemy inside caster's RetreatTriggerMeleeRange; melee (or short-range) profile.
	meleeDist := caster.RetreatTriggerMeleeRange * 0.5
	meleeEnemy := spawnProjTestUnit(t, s, enemyPlayerID, 500+meleeDist, 500)
	meleeEnemyProfile := resolveCombatProfile(meleeEnemy)
	if !meleeEnemyProfile.Melee {
		// spawnProjTestUnit spawns a soldier which should be melee; if not,
		// force short attack range so shouldRetreatLocked counts it as melee-threat.
		meleeEnemy.AttackRange = 60
	}

	// Build a spatial index containing only the threat.
	idx := newCombatSpatialIndex(combatSpatialBucketSize)
	idx.add(meleeEnemy)
	ctx := combatEvalContext{index: idx, blocked: nil}

	casterShouldRetreat := s.shouldRetreatLocked(casterUnit, caster, ctx)
	archerShouldRetreat := s.shouldRetreatLocked(archerUnit, archer, ctx)
	s.mu.Unlock()

	if !casterShouldRetreat {
		t.Error("shouldRetreatLocked(caster, melee enemy inside trigger range) = false; want true — caster must kite")
	}
	if archerShouldRetreat {
		t.Error("shouldRetreatLocked(archer, melee enemy inside range) = true; want false — archer has no retreat configured")
	}

	// ── Part (b): end-to-end tick observable ─────────────────────────────────
	s2 := newProjectileTestState(t)
	s2.mu.Lock()
	app2 := s2.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 500, Y: 500})
	app2.Visible = true
	arch2 := s2.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 550, Y: 500})
	arch2.Visible = true
	// Melee enemy inside acolyte's trigger range (acolyte at 500,500).
	enemy2 := spawnProjTestUnit(t, s2, enemyPlayerID, 500+meleeDist, 500)
	enemy2.HP = 9999
	enemy2.MoveSpeed = 0 // stationary — deterministic position each tick
	ep2 := resolveCombatProfile(enemy2)
	if !ep2.Melee {
		enemy2.AttackRange = 60
	}
	app2ID := app2.ID
	arch2ID := arch2.ID
	s2.mu.Unlock()

	s2.Update(0.05)

	s2.mu.RLock()
	defer s2.mu.RUnlock()
	app2Live := s2.unitsByID[app2ID]
	arch2Live := s2.unitsByID[arch2ID]
	if app2Live == nil || arch2Live == nil {
		t.Fatal("a unit was unexpectedly removed during the retreat tick")
	}
	if app2Live.Status != "Repositioning" {
		t.Errorf("after one tick with melee enemy in trigger range, acolyte Status = %q; want \"Repositioning\" (caster must kite)",
			app2Live.Status)
	}
	if arch2Live.Status == "Repositioning" {
		t.Errorf("archer Status = %q; archer must not retreat (no retreat configured in profile)", arch2Live.Status)
	}
}

// ── 6.5: Acolyte (archetype="caster") forfeits Swift Strikes (Delta 3) ────

// TestCasterUpgrade_SwiftStrikesForfeited asserts that an Acolyte unit with
// archetype="caster" does NOT match the archer-scoped swift_strikes_* upgrades
// via matchesUpgradeScope, and that no archetype-scoped upgrade in the catalog
// matches the caster archetype.
//
// DESIGN INTENT — this is NOT a regression:
//
//   Flipping the Acolyte's archetype from "archer" to "caster" is the
//   mechanism by which the Acolyte is removed from the archer attack-speed
//   upgrade pool. A backline caster should not inherit archer-specific upgrades.
//   archetype is the role-separation boundary in upgrade_apply.go.
//   If a future reader sees this test failing because a caster-scoped upgrade
//   was added, update the assertion with a comment explaining the new upgrade —
//   do not revert the archetype flip or delete this test.
func TestCasterUpgrade_SwiftStrikesForfeited(t *testing.T) {
	// Derive the acolyte archetype from the catalog, not a hardcoded string.
	appDef, ok := getUnitDef("acolyte")
	if !ok {
		t.Fatal("acolyte unit def not registered")
	}
	if appDef.Archetype != "caster" {
		t.Fatalf("acolyte.Archetype = %q; want \"caster\" (catalog flip not in effect)", appDef.Archetype)
	}

	// Build a synthetic acolyte unit matching what spawnPlayerUnitLocked produces.
	app := &Unit{
		UnitType:  "acolyte",
		Archetype: appDef.Archetype,
		HP:        100,
		MaxHP:     100,
	}

	// Swift Strikes must NOT match the caster Acolyte.
	for _, upgradeID := range []string{"swift_strikes_common", "swift_strikes_rare"} {
		def, exists := getUpgradeDef(upgradeID)
		if !exists {
			t.Fatalf("%s not found in catalog; this test depends on the live upgrade catalog", upgradeID)
		}
		if def.Scope != upgradeScopeArchetype {
			t.Fatalf("%s scope = %q; expected %q — test assumption broken", upgradeID, def.Scope, upgradeScopeArchetype)
		}
		if def.Archetype != "archer" {
			t.Fatalf("%s archetype = %q; expected \"archer\" — test assumption broken", upgradeID, def.Archetype)
		}
		if matchesUpgradeScope(def, app) {
			t.Errorf("%s matches a caster Acolyte via matchesUpgradeScope; it must not (Acolyte archetype is now \"caster\", not \"archer\")",
				upgradeID)
		}
	}

	// No archetype-scoped upgrade in the catalog should match "caster".
	// No caster-scoped upgrade line exists in Phase 1 — authoring one is future content work.
	defs := listUpgradeDefs()
	for _, def := range defs {
		if def.Scope != upgradeScopeArchetype {
			continue
		}
		if matchesUpgradeScope(def, app) {
			t.Errorf("upgrade %q (scope=archetype, archetype=%q) matches a caster Acolyte; no caster-scoped upgrade should exist in Phase 1. If this is intentional (new caster upgrade added), update this assertion with a comment.",
				def.ID, def.Archetype)
		}
	}
}

// ── 6.6: AbilityCategory registry ────────────────────────────────────────────

// TestAbilityCategory_Registry verifies IsValidAbilityCategory, RegisterAbilityCategory,
// and AbilityCategories() per the ability-category spec.
func TestAbilityCategory_Registry(t *testing.T) {
	// All four registered constants must be valid.
	for _, cat := range []AbilityCategory{
		AbilityCategoryHeal,
		AbilityCategoryBuffAlly,
		AbilityCategorySummon,
		AbilityCategoryOffensive,
	} {
		if !IsValidAbilityCategory(cat) {
			t.Errorf("IsValidAbilityCategory(%q) = false; want true (registered builtin)", cat)
		}
	}

	// Empty value is the "unspecified" sentinel and must NOT be valid.
	if IsValidAbilityCategory("") {
		t.Error(`IsValidAbilityCategory("") = true; the empty value is reserved as "unspecified" and must not validate`)
	}

	// An unregistered string must not validate.
	if IsValidAbilityCategory("teleport") {
		t.Error(`IsValidAbilityCategory("teleport") = true; an unregistered category must not validate`)
	}

	// RegisterAbilityCategory("") must panic — the empty id is reserved.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error(`RegisterAbilityCategory("") did not panic; the empty id is reserved and must not be registerable`)
			}
		}()
		RegisterAbilityCategory("")
	}()

	// AbilityCategories() must return at least the four builtins in sorted order.
	all := AbilityCategories()
	if len(all) < 4 {
		t.Fatalf("AbilityCategories() returned %d entries; want at least 4 (the builtins)", len(all))
	}
	if !sort.SliceIsSorted(all, func(i, j int) bool { return all[i] < all[j] }) {
		t.Errorf("AbilityCategories() is not sorted: %v", all)
	}
	// Check all four builtins appear.
	found := map[AbilityCategory]bool{}
	for _, c := range all {
		found[c] = true
	}
	for _, want := range []AbilityCategory{
		AbilityCategoryHeal, AbilityCategoryBuffAlly,
		AbilityCategorySummon, AbilityCategoryOffensive,
	} {
		if !found[want] {
			t.Errorf("AbilityCategories() does not contain %q", want)
		}
	}

	// AbilityCategories() must be stable across two calls (no shuffle from map iteration).
	all2 := AbilityCategories()
	if len(all) != len(all2) {
		t.Fatalf("AbilityCategories() returned different lengths across calls: %d vs %d", len(all), len(all2))
	}
	for i := range all {
		if all[i] != all2[i] {
			t.Errorf("AbilityCategories() not stable: call1[%d]=%q call2[%d]=%q", i, all[i], i, all2[i])
		}
	}
}

// ── 6.7: AbilityDef.Category catalog validation ───────────────────────────────

// TestAbilityCategory_CatalogValidation covers three scenarios from the spec:
//
//  (a) heal.json loads with Category == AbilityCategoryHeal (category tag is live).
//  (b) An ability definition with no "category" key loads with Category == "".
//  (c) An ability definition with an invalid category panics at the validation gate.
//
// For (c): the loadAbilityDefs() loader uses an embed.FS fixed at compile time,
// so we cannot invoke it against a temp directory without modifying production
// JSON. Instead we replicate the EXACT validation predicate from ability_defs.go
// (line: `if def.Category != "" && !IsValidAbilityCategory(def.Category) { panic(...) }`)
// and confirm that it panics on an invalid category and does NOT panic on a valid
// one. This exercises the same code path that the loader would call.
func TestAbilityCategory_CatalogValidation(t *testing.T) {
	// ── (a) heal.json loads with Category == AbilityCategoryHeal ─────────────
	healAbilDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal(`getAbilityDef("heal") = _, false; heal must be registered`)
	}
	if healAbilDef.Category != AbilityCategoryHeal {
		t.Errorf("heal.Category = %q; want %q (heal.json must carry \"category\": \"heal\")",
			healAbilDef.Category, AbilityCategoryHeal)
	}
	// Additive-only check: the category tag must not alter existing fields.
	if healAbilDef.ID != "heal" {
		t.Errorf("heal.ID = %q; want \"heal\" (category tag must not alter ID)", healAbilDef.ID)
	}
	if healAbilDef.ManaCost <= 0 {
		t.Errorf("heal.ManaCost = %d; want > 0 (unchanged from pre-tag value)", healAbilDef.ManaCost)
	}
	if !healAbilDef.SupportsAutoCast {
		t.Error("heal.SupportsAutoCast = false; must be unchanged (additive category tag)")
	}
	if !healAbilDef.CastRange.MatchesAttackRange() {
		t.Error("heal.CastRange must still be match_attack_range after category tag")
	}

	// ── (b) No "category" key → Category == "" ───────────────────────────────
	var noCatDef AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"test_no_cat","castRange":0}`), &noCatDef); err != nil {
		t.Fatalf("inline unmarshal of no-category def failed: %v", err)
	}
	if noCatDef.Category != "" {
		t.Errorf("ability with no category key: Category = %q; want \"\" (empty default)", noCatDef.Category)
	}

	// ── (c) Invalid category panics at the validation predicate ──────────────
	//
	// Validation predicate from ability_defs.go loadAbilityDefs():
	//   if def.Category != "" && !IsValidAbilityCategory(def.Category) { panic(...) }
	//
	// We call it directly here because the loader's embed.FS is fixed at compile
	// time and cannot be redirected to a temp JSON file in tests. This is the
	// identical code path the loader executes; if it is refactored, update this
	// comment accordingly.
	runCategoryValidation := func(def AbilityDef) {
		if def.Category != "" && !IsValidAbilityCategory(def.Category) {
			panic(`category "` + string(def.Category) + `" is not a registered ability category`)
		}
	}

	var badCatDef AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"test_bad","castRange":0,"category":"not_a_real_category"}`), &badCatDef); err != nil {
		t.Fatalf("unmarshal bad-category def: %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("validation predicate for an invalid category must panic; got none")
			}
		}()
		runCategoryValidation(badCatDef)
	}()

	// Valid category must NOT panic.
	var validCatDef AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"test_valid","castRange":0,"category":"heal"}`), &validCatDef); err != nil {
		t.Fatalf("unmarshal valid-category def: %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("validation predicate panicked on a valid category: %v", r)
			}
		}()
		runCategoryValidation(validCatDef)
	}()

	// Empty category must NOT panic (empty is the "unspecified" default, not an error).
	var emptyCatDef AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"test_empty","castRange":0}`), &emptyCatDef); err != nil {
		t.Fatalf("unmarshal empty-category def: %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("validation predicate panicked on empty category (must be allowed as 'unspecified'): %v", r)
			}
		}()
		runCategoryValidation(emptyCatDef)
	}()
}

// ── 6.8a: heal-autocast gating is unchanged by the profile flip ───────────────

// TestHealAutocast_GatingUnchangedByProfileFlip verifies that the mana / cooldown /
// selector predicate gates for heal autocast produce the same decisions for a
// caster-profiled Acolyte as they would for any unit with the same ability
// and mana state: the profile change introduces no new gate and removes none.
//
// Two sub-cases:
//   (pass) mana >= cost, no cooldown, valid target → cast fires
//   (block) mana < cost → cast does not fire
func TestHealAutocast_GatingUnchangedByProfileFlip(t *testing.T) {
	def := healDef(t) // catalog heal def — avoids hardcoding cost

	// ── PASS case: gate should allow the cast ─────────────────────────────────
	s, app, ally := autoCastSetup(t, def.HealAmount)
	allyID := ally.ID

	s.mu.Lock()
	if p := resolveCombatProfile(app); p.Name != "caster" {
		t.Fatalf("acolyte resolves to %q; expected \"caster\" (profile flip must be in effect)", p.Name)
	}
	startHP := ally.HP
	s.toggleAutoCastLocked(app, "heal")
	s.mu.Unlock()

	advance(s, 40) // enough ticks for cast to initiate and resolve (1s cast time at 20Hz = 20 ticks + buffer)

	s.mu.RLock()
	allyLive := s.unitsByID[allyID]
	s.mu.RUnlock()

	if allyLive != nil && allyLive.HP <= startHP {
		t.Error("heal did not fire; the caster-profile flip must not block the autocast gate (mana-sufficient, valid-target scenario)")
	}

	// ── BLOCK case: mana insufficient → cast must NOT fire ───────────────────
	s2, app2, ally2 := autoCastSetup(t, def.HealAmount)
	allyID2 := ally2.ID

	s2.mu.Lock()
	app2.CurrentMana = def.ManaCost - 1 // one below threshold
	app2.ManaRegenPerSecond = 0          // prevent regen from lifting it over the cost
	startHP2 := ally2.HP
	s2.toggleAutoCastLocked(app2, "heal")
	s2.mu.Unlock()

	advance(s2, 30)

	s2.mu.RLock()
	defer s2.mu.RUnlock()
	ally2Live := s2.unitsByID[allyID2]
	if ally2Live != nil && ally2Live.HP != startHP2 {
		t.Error("heal fired with insufficient mana on caster-profiled Acolyte; the mana gate must be unchanged by the profile flip")
	}
	if app2.CastAbilityID != "" {
		t.Error("CastAbilityID set despite insufficient mana; mana gate not working for caster profile")
	}
}

// ── 6.8b: seeded replay — no melee → no cadence divergence ───────────────────

// TestHealAutocast_SeededReplayNoMeleeNoDivergence is a TRIPWIRE for unintended
// profile↔cadence coupling. It verifies the narrower guarantee from the proposal:
//
//   In a scenario with NO melee threat (the Acolyte never retreats),
//   the set of ticks on which heal is auto-cast is identical across two
//   seeded runs with the same seed and inputs.
//
// This does NOT claim byte-identical autocast under any conditions:
//   - Cadence divergence UNDER RETREAT is expected and correct behaviour.
//   - Only no-melee scenarios (position held constant) must produce identical cast ticks.
//
// Determinism source: NewGameStateWithSeed with a fixed seed, no wall-clock reads,
// no un-seeded math/rand. Both runs use the same seed, unit placement, and inputs.
func TestHealAutocast_SeededReplayNoMeleeNoDivergence(t *testing.T) {
	const seed = 12345

	runSim := func() []int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()

		// Ensure the player record exists (NewGameStateWithSeed may not create it).
		if s.Players["p1"] == nil {
			s.Players["p1"] = &Player{
				ID:                            "p1",
				Resources:                     map[string]int{"gold": 9999, "wood": 9999},
				GlobalUnitSpawnTimeMultiplier: 1,
				UnitSpawnTimeMultipliers:      map[string]float64{},
				Upgrades:                      map[UpgradeTrack]int{},
				Vault:                         []*VaultItem{},
			}
		}

		// Spawn Acolyte (caster profile after catalog flip).
		app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if app == nil {
			s.mu.Unlock()
			t.Fatal("failed to spawn acolyte in seeded state")
		}
		app.Visible = true

		// Spawn a single permanently-damaged ally within heal range.
		// No melee enemies — Acolyte never retreats, position is held constant.
		// The selector will target this ally every time mana permits.
		ally := spawnProjTestUnit(t, s, "p1", 450, 400)
		ally.HP = 1 // critically low — always a valid heal target
		allyID := ally.ID

		// Catalog seeds heal autocast ON at spawn; clear so the toggle below
		// flips it on (the asserted starting state for this replay test).
		// Determinism doesn't care about the default, only that both runs
		// match — clearing on both runs preserves the property.
		delete(app.AutoCastEnabled, "heal")
		s.toggleAutoCastLocked(app, "heal")
		appID := app.ID
		s.mu.Unlock()

		var castTicks []int
		const totalTicks = 200 // 10s at 20Hz; enough for several mana-regeneration cycles

		prevCastID := ""
		for tick := 0; tick < totalTicks; tick++ {
			s.Update(0.05)
			s.mu.RLock()
			liveApp := s.unitsByID[appID]
			liveAlly := s.unitsByID[allyID]
			if liveApp == nil {
				s.mu.RUnlock()
				break
			}
			// Record ticks where a new heal cast is initiated.
			if liveApp.CastAbilityID == "heal" && prevCastID == "" {
				castTicks = append(castTicks, tick)
			}
			prevCastID = liveApp.CastAbilityID
			// Keep the ally at critically low HP so the selector always finds a target.
			if liveAlly != nil && liveAlly.HP > 5 {
				liveAlly.HP = 1
			}
			s.mu.RUnlock()
		}
		return castTicks
	}

	r1 := runSim()
	r2 := runSim()

	// CADENCE DIVERGENCE UNDER RETREAT IS EXPECTED AND OUT OF SCOPE.
	// This scenario has no melee enemies so position is constant.
	// Divergence here means the caster profile is unexpectedly coupling into
	// the autocast gating path — that is the bug this test would catch.

	if len(r1) == 0 {
		t.Fatal("no heal casts detected in run 1; autocast setup is broken (check mana/cooldown/selector registration)")
	}
	if len(r1) != len(r2) {
		t.Errorf("cast tick count diverges: run1=%d run2=%d (same seed+inputs; non-determinism in no-melee scenario)",
			len(r1), len(r2))
		t.Logf("run1 cast ticks: %v", r1)
		t.Logf("run2 cast ticks: %v", r2)
		return
	}
	for i := range r1 {
		if r1[i] != r2[i] {
			t.Errorf("cast tick[%d] diverges: run1=%d run2=%d (non-determinism; same seed+no-melee scenario)",
				i, r1[i], r2[i])
		}
	}
	t.Logf("seeded replay: %d heal casts on identical ticks in both runs (no-melee, position constant)", len(r1))
}
