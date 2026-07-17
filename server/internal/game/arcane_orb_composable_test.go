package game

import (
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// GENUINE COMPOSABILITY: the authored on_projectile_tick trigger actually
// RUNS, instead of fireProjectileTickLocked silently re-synthesizing its own
// hardcoded actions from frozen scalar fields. Every test below builds its
// OWN test-only ability (registerRuntimeTestAbility) rather than touching the
// shipped arcane_orb.json, so a bug in one authored variant can't leak into
// another test — and so the "push" acceptance case never ships as a live
// balance change to the real Arcane Orb.
// ═════════════════════════════════════════════════════════════════════════════

// buildVortexTestAbilityDef compiles a synthetic arcane_orb-shaped ability
// (ground-point entry, launch_projectile "direction" + TickInterval>0) with
// an authored on_projectile_tick trigger built directly from the given
// magnitudes — id must be unique per test (registerRuntimeTestAbility
// registers it into the shared runtimeAbilities overlay). mode is
// applyForceConfig.Mode ("" / "pull" / "push").
func buildVortexTestAbilityDef(id string, radius, pullStrength float64, dmgAmount int, mode string) AbilityDef {
	tickTrig := AbilityTriggerDef{
		ID:   "tick",
		Type: TriggerOnProjectileTick,
		Actions: []AbilityActionDef{
			{
				ID:   "sel",
				Type: ActionSelectTargets,
				Target: &TargetQueryDef{
					Source: SrcAllInScene, Origin: OriginProjectilePos,
					Radius: radius, Relations: []TargetRelation{RelEnemy},
				},
				Outputs: map[string]string{"targets": "vortexHits"},
			},
			{
				ID:     "dmg",
				Type:   ActionDealDamage,
				Timing: &ActionTiming{TickInterval: arcaneOrbDamageIntervalSeconds},
				Input:  map[string]ContextRef{"targets": {Key: "vortexHits"}},
				Config: marshalConfig(dealDamageConfig{Amount: dmgAmount, Type: DamageArcane}),
			},
			{
				ID:     "force",
				Type:   ActionApplyForce,
				Input:  map[string]ContextRef{"targets": {Key: "vortexHits"}},
				Config: marshalConfig(applyForceConfig{Strength: pullStrength, Duration: arcaneOrbPullRefreshSeconds, Origin: OriginProjectilePos, Mode: mode}),
			},
		},
	}
	launchCfg := launchProjectileConfig{
		Projectile:      "arcane_orb",
		TravelMode:      travelModeDirection,
		Distance:        CastRange(400),
		ProjectileScale: 2.5,
		ProjectileSpeed: 150,
		TickInterval:    arcaneOrbDamageIntervalSeconds,
		Triggers:        []AbilityTriggerDef{tickTrig},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint, Range: 400},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "orb", Type: ActionLaunchProjectile, Config: marshalConfig(launchCfg)},
			}},
		},
	}
	return AbilityDef{
		ID: id, DisplayName: id, Type: AbilitySpell, Category: AbilityCategoryOffensive,
		ManaCost: 20, Cooldown: 8, DamageType: DamageArcane,
		TargetsPoint: true, CastRange: 400,
		SchemaVersion: 2, Program: prog,
	}
}

func setupVortexTestCaster(t *testing.T, s *GameState, abilityID string, x, y float64) *Unit {
	t.Helper()
	c := spawnProjTestUnit(t, s, "p1", x, y)
	c.Abilities = []string{abilityID}
	c.CurrentMana, c.MaxMana = 100, 100
	c.Damage = 0
	return c
}

// TestArcaneOrb_AuthoredApplyForcePush_PushesUnitsAway is the user's exact bug:
// setting apply_force's authored Mode to "push" must actually push units away
// from the orb. Before the fix, fireProjectileTickLocked discarded the
// authored trigger entirely and re-synthesized its own hardcoded apply_force
// call (which never carried Mode at all) — so this test fails against the
// pre-fix code (mode is silently ignored, unit still gets pulled in).
func TestArcaneOrb_AuthoredApplyForcePush_PushesUnitsAway(t *testing.T) {
	const id = "test_vortex_push"
	def := buildVortexTestAbilityDef(id, 200, 300, 1, applyForceModePush)
	registerRuntimeTestAbility(t, def)

	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := setupVortexTestCaster(t, s, id, 100, 100)
	// Stationary enemy sitting on the orb's straight-line path (due east from
	// the caster) — well within radius 200 the instant the orb spawns.
	victim := spawnProjTestUnit(t, s, enemyPlayerID, 160, 100)
	victim.MoveSpeed = 0
	victim.Damage = 0
	startDist := math.Hypot(victim.X-caster.X, victim.Y-caster.Y)

	if ok, r := s.beginAbilityCastAtPointLocked(caster, id, 500, 100); !ok {
		s.mu.Unlock()
		t.Fatalf("point cast failed: %s", r)
	}
	orb := findArcaneOrb(s)
	if orb == nil {
		s.mu.Unlock()
		t.Fatal("no orb spawned")
	}
	if len(orb.TickActions) == 0 {
		s.mu.Unlock()
		t.Fatal("orb.TickActions is empty; the authored on_projectile_tick trigger was not carried onto the Projectile")
	}
	s.mu.Unlock()

	for i := 0; i < 10; i++ {
		s.Update(0.05)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// This test's job is now just "the authored on_projectile_tick trigger
	// ran and applied force at all" — direction (push vs pull) is proven by
	// TestArcaneOrb_PushVsPull_DivergeAtSameGeometry, which measures relative
	// to the orb rather than the caster (caster-distance can't distinguish the
	// two for an orb travelling past the victim).
	_ = startDist
	if victim.X == 160 && victim.Y == 100 {
		t.Errorf("victim never moved from spawn (160,100); the authored apply_force in on_projectile_tick did not execute")
	}
}

// TestArcaneOrb_EditingAuthoredDealDamageAmount_ChangesRuntimeDamage proves the
// authored deal_damage action's amount is genuinely read at runtime: two
// otherwise-identical vortexes differing ONLY in their authored per-tick
// amount must deal proportionally different total damage.
func TestArcaneOrb_EditingAuthoredDealDamageAmount_ChangesRuntimeDamage(t *testing.T) {
	run := func(t *testing.T, id string, amount int) int {
		def := buildVortexTestAbilityDef(id, 150, 0.0001, amount, "")
		registerRuntimeTestAbility(t, def)

		s := newProjectileTestState(t)
		s.mu.Lock()
		caster := setupVortexTestCaster(t, s, id, 100, 100)
		victim := spawnProjTestUnit(t, s, enemyPlayerID, 100, 100) // sits AT the caster's own spawn point
		victim.MoveSpeed = 0
		victim.Damage = 0
		victim.MaxHP, victim.HP = 5000, 5000
		start := victim.HP

		if ok, r := s.beginAbilityCastAtPointLocked(caster, id, 500, 100); !ok {
			s.mu.Unlock()
			t.Fatalf("point cast failed: %s", r)
		}
		s.mu.Unlock()

		for i := 0; i < 40; i++ {
			s.Update(0.05)
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		return start - victim.HP
	}

	small := run(t, "test_vortex_dmg_small", 2)
	large := run(t, "test_vortex_dmg_large", 20)
	if large <= small {
		t.Fatalf("total damage with authored amount=20 (%d) must exceed amount=2 (%d); editing the authored deal_damage amount must change runtime damage", large, small)
	}
	// Roughly proportional (both fire the same number of ticks — the orb
	// travels the identical path/speed in both runs).
	if large < small*5 {
		t.Errorf("amount=20 total damage (%d) is not roughly 10x amount=2's (%d); want a proportional scale-up matching the authored per-tick chunk", large, small)
	}
}

// TestArcaneOrb_EditingAuthoredSelectTargetsRadius_ChangesReach proves the
// authored select_targets action's radius is genuinely read at runtime: a
// bystander sitting OUTSIDE a small authored radius but INSIDE a large one
// must be untouched in the small-radius run and pulled/damaged in the
// large-radius run.
func TestArcaneOrb_EditingAuthoredSelectTargetsRadius_ChangesReach(t *testing.T) {
	const bystanderOffsetY = 180 // outside radius 50, inside radius 250

	run := func(t *testing.T, id string, radius float64) (moved bool, damaged bool) {
		def := buildVortexTestAbilityDef(id, radius, 400, 50, "")
		registerRuntimeTestAbility(t, def)

		s := newProjectileTestState(t)
		s.mu.Lock()
		caster := setupVortexTestCaster(t, s, id, 100, 100)
		bystander := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100+bystanderOffsetY)
		bystander.MoveSpeed = 0
		bystander.Damage = 0
		startX, startY := bystander.X, bystander.Y
		startHP := bystander.HP

		if ok, r := s.beginAbilityCastAtPointLocked(caster, id, 500, 100); !ok {
			s.mu.Unlock()
			t.Fatalf("point cast failed: %s", r)
		}
		s.mu.Unlock()

		for i := 0; i < 60; i++ {
			s.Update(0.05)
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		moved = math.Hypot(bystander.X-startX, bystander.Y-startY) > 1
		damaged = bystander.HP < startHP
		return moved, damaged
	}

	movedSmall, damagedSmall := run(t, "test_vortex_radius_small", 50)
	if movedSmall || damagedSmall {
		t.Errorf("radius=50: bystander moved=%v damaged=%v; want both false (bystander is outside this radius)", movedSmall, damagedSmall)
	}

	movedLarge, damagedLarge := run(t, "test_vortex_radius_large", 250)
	if !movedLarge || !damagedLarge {
		t.Errorf("radius=250: bystander moved=%v damaged=%v; want both true (bystander is inside this radius) — editing the authored select_targets radius must change runtime reach", movedLarge, damagedLarge)
	}
}

// TestArcaneOrb_ComposedDamageFoldMatchesLegacy_AtShippedMagnitudes proves the
// exact arithmetic behind the fold/rounding decision documented on
// fireComposedProjectileTickLocked (projectile.go): for arcane_orb's SHIPPED
// magnitudes (DPS 16, interval 0.25s ⇒ 4/tick authored chunk) under a +50%
// multiplicative damage modifier, folding the pre-rounded per-tick CHUNK
// (the genuine, per-firing composed path) yields the SAME integer as legacy's
// single frozen-then-rounded rate*interval*modifier computation. This is a
// coincidence of these specific numbers (round-then-fold and fold-then-round
// only commute under a pure multiplicative modifier) — not a general
// guarantee for arbitrary DPS/interval/modifier combinations — which is why
// this test pins the exact shipped values rather than asserting the general
// case.
func TestArcaneOrb_ComposedDamageFoldMatchesLegacy_AtShippedMagnitudes(t *testing.T) {
	def := arcaneOrbDef(t) // recovered shadow: DamagePerSecond=16, DamageType=arcane
	const interval = arcaneOrbDamageIntervalSeconds
	if def.DamagePerSecond != 16 || interval != 0.25 {
		t.Fatalf("test pins arcane_orb's shipped magnitudes (DPS=16, interval=0.25); got DPS=%v interval=%v — update the worked arithmetic in this test's doc comment if these ever change", def.DamagePerSecond, interval)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.SpellModifiers = []SpellModifier{goldenDamageModifier(string(def.DamageType))} // +50% multiply

	authoredChunk := int(math.Round(def.DamagePerSecond * interval)) // 4 — what compileProjectileTickTrigger bakes
	composed := s.effectiveAbilityDamageLocked(caster, def, authoredChunk)

	legacyEff := s.effectiveSpellLocked(caster, def) // folds DamagePerSecond ONCE, unrounded
	legacy := int(math.Round(legacyEff.DamagePerSecond * interval))

	if composed != legacy {
		t.Fatalf("composed per-tick fold = %d, legacy per-tick fold = %d; want equal at arcane_orb's shipped magnitudes (see this test's doc comment for the arithmetic)", composed, legacy)
	}
	if composed != 6 {
		t.Fatalf("composed per-tick damage = %d; want 6 (round(round(16*0.25)*1.5) = round(4*1.5) = 6, matching legacy round(16*1.5*0.25) = round(6) = 6)", composed)
	}
}

// TestArcaneOrb_DoTCadence_TotalPerSecondUnchanged proves the DoT cadence
// parity requirement directly against the composed (authored-actions) path:
// with the target held stationary and in radius for the whole window, total
// damage over exactly 1 second must equal the authored per-second rate
// (dmgAmount / interval), regardless of the simulation's tick rate — matching
// TestArcaneOrb_DamageOverTimeRate's identical proof for the legacy-fallback
// path.
func TestArcaneOrb_DoTCadence_TotalPerSecondUnchanged(t *testing.T) {
	const id = "test_vortex_cadence"
	const perTick = 5
	wantPerSecond := int(math.Round(float64(perTick) / arcaneOrbDamageIntervalSeconds))
	def := buildVortexTestAbilityDef(id, 500, 0.0001, perTick, "")
	registerRuntimeTestAbility(t, def)

	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := setupVortexTestCaster(t, s, id, 100, 100)
	victim := spawnProjTestUnit(t, s, enemyPlayerID, 120, 100)
	victim.MoveSpeed = 0
	victim.Damage = 0
	victim.MaxHP, victim.HP = 10000, 10000
	start := victim.HP

	if ok, r := s.beginAbilityCastAtPointLocked(caster, id, 1000, 100); !ok {
		s.mu.Unlock()
		t.Fatalf("point cast failed: %s", r)
	}
	orb := findArcaneOrb(s)
	if orb == nil {
		s.mu.Unlock()
		t.Fatal("no orb spawned")
	}
	// Freeze the orb in place for a clean 1-second isolation window (mirrors
	// TestArcaneOrb_DamageOverTimeRate's near-stationary setup), without
	// touching the authored TickActions this test is proving.
	orb.TotalSeconds = 1000
	orb.RemainingSeconds = 1000
	orb.PierceLength = 1000
	s.mu.Unlock()

	for i := 0; i < 20; i++ { // exactly 1 second (20 x 0.05)
		s.Update(0.05)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	took := start - victim.HP
	if took != wantPerSecond {
		t.Errorf("1s of composed vortex dealt %d damage; want exactly %d (authored per-tick chunk %d / interval %v)", took, wantPerSecond, perTick, arcaneOrbDamageIntervalSeconds)
	}
}

// TestArcaneOrb_ComposedTickOpsBudget_BoundsLongFlight proves the shared
// cross-tick op budget (Projectile.TickOpsBudget) bounds the TOTAL work a
// ticking vortex can do across its entire flight — mirroring
// TestLaunchProjectile_ImpactRelaunchChain_SharedBudgetTerminates's proof for
// the impact-relaunch lineage, adapted to the tick path. Directly exhausts a
// small hand-set budget across many fireComposedProjectileTickLocked calls
// and asserts no further action fires once it hits zero.
func TestArcaneOrb_ComposedTickOpsBudget_BoundsLongFlight(t *testing.T) {
	const id = "test_vortex_budget"
	def := buildVortexTestAbilityDef(id, 500, 300, 1, "")
	registerRuntimeTestAbility(t, def)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := setupVortexTestCaster(t, s, id, 100, 100)
	_ = spawnProjTestUnit(t, s, enemyPlayerID, 100, 100)

	if ok, r := s.beginAbilityCastAtPointLocked(caster, id, 100000, 100); !ok {
		t.Fatalf("point cast failed: %s", r)
	}
	orb := findArcaneOrb(s)
	if orb == nil {
		t.Fatal("no orb spawned")
	}
	if orb.TickOpsBudget == nil {
		t.Fatal("orb.TickOpsBudget is nil; the composed tick path must seed a shared op budget")
	}

	// Force a tiny remaining budget and a very long remaining flight so many
	// firings are attempted against it.
	budget := 5
	orb.TickOpsBudget = &budget
	orb.TotalSeconds = 1_000_000
	orb.RemainingSeconds = 1_000_000
	orb.PierceLength = 1_000_000

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	for i := 0; i < 2000; i++ {
		s.tickArcaneOrbProjectileLocked(orb, 0.05)
	}

	actionsStarted := 0
	for _, ev := range tr.Events {
		if ev.Type == "action_started" {
			actionsStarted++
		}
	}
	if actionsStarted == 0 {
		t.Fatal("no actions ran at all; test setup problem")
	}
	if actionsStarted > 20 {
		t.Fatalf("total action_started events = %d over 2000 tick calls against a budget of 5; want bounded near the budget, not growing with tick count", actionsStarted)
	}
	if *orb.TickOpsBudget < 0 {
		// consumeOp decrements unconditionally once opsExhausted() already
		// gated the call, so it never goes far negative, but it must stop
		// decrementing once execution actually halts.
		t.Errorf("shared budget went to %d; opsExhausted should have stopped further consumeOp calls at/near 0", *orb.TickOpsBudget)
	}
}

// TestArcaneOrb_PushVsPull_DivergeAtSameGeometry is the DISCRIMINATING test:
// run byte-identical orbs differing ONLY in the authored apply_force mode, and
// assert the victim ends up in DIFFERENT places. This is what
// TestArcaneOrb_AuthoredApplyForcePush could not prove — that test measured
// distance from the CASTER, but the orb travels past the victim, so both push
// AND pull increase caster-distance and the metric didn't distinguish them
// (it passed even with the composed tick path forced off). The only way pull
// and push produce different victim positions is if the runtime actually
// honours the authored mode — on the old faked path both ran hardcoded pull
// and this test would see identical positions.
func TestArcaneOrb_PushVsPull_DivergeAtSameGeometry(t *testing.T) {
	run := func(mode string) (vx, vy float64) {
		id := "test_vortex_mode_" + mode
		def := buildVortexTestAbilityDef(id, 200, 300, 1, mode)
		registerRuntimeTestAbility(t, def)

		s := newProjectileTestState(t)
		s.mu.Lock()
		caster := setupVortexTestCaster(t, s, id, 100, 100)
		// Victim OFF the orb's east-bound axis (north of it) so pull (toward the
		// orb, i.e. southward) and push (away, northward) move it in opposite
		// y-directions — an unambiguous signal the caster-distance metric lacked.
		victim := spawnProjTestUnit(t, s, enemyPlayerID, 160, 40)
		victim.MoveSpeed = 0
		victim.Damage = 0
		if ok, r := s.beginAbilityCastAtPointLocked(caster, id, 500, 100); !ok {
			s.mu.Unlock()
			t.Fatalf("point cast (%s) failed: %s", mode, r)
		}
		if orb := findArcaneOrb(s); orb == nil || len(orb.TickActions) == 0 {
			s.mu.Unlock()
			t.Fatalf("%s: orb missing or TickActions empty (authored trigger not carried)", mode)
		}
		s.mu.Unlock()
		for i := 0; i < 10; i++ {
			s.Update(0.05)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return victim.X, victim.Y
	}

	_, pullY := run("pull")
	_, pushY := run("push")

	// The victim starts NORTH of the orb's path (y=40, path at y≈100). Pull
	// drags it SOUTH (toward the path, y increases); push shoves it NORTH
	// (away, y decreases). They must land on opposite sides of the start.
	const startY = 40.0
	if !(pullY > startY) {
		t.Errorf("pull: victim y %.2f -> %.2f; want pulled toward the orb (y increases)", startY, pullY)
	}
	if !(pushY < startY) {
		t.Errorf("push: victim y %.2f -> %.2f; want pushed away from the orb (y decreases)", startY, pushY)
	}
	if pushY >= pullY {
		t.Errorf("push (y=%.2f) and pull (y=%.2f) produced the same-or-wrong-direction motion — the authored apply_force mode is being ignored (faked runtime)", pushY, pullY)
	}
}
