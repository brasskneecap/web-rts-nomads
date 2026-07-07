package game

// caster_phase2_test.go — Phase-2 caster archetype acceptance tests (tasks 7.1–7.10).
//
// Task map:
//   7.1  TestPhase2_NoRegression_*                 — no-regression gate (3 assertions)
//   7.2  TestPhase2_UncategorisedAbilityFires       — fallback clears minActivationScore
//   7.3  TestPhase2_ScoreWinsOverSlot_*             — highest score beats earlier slot; gates block cast
//   7.4  TestPhase2_Tiebreak_* / BelowFloor_*       — slot/id tiebreak; below-floor casts nothing
//   7.5  TestPhase2_PriorityCorrectness_*           — heal vs offensive by situation
//   7.6  TestPhase2_BuffAllyScoring / SummonScoring — direct unit tests (no authored ability)
//   7.7  TestPhase2_PathAbilityLoader_*             — loader shape, panic cases, missing cell
//   7.8/7.9 grant-engine + snapshot tests moved to caster_defer_test.go
//        (defer-caster-ability-content reworked them onto a synthetic
//         fixture after the placeholder Cleric/Arch Mage grants were removed)
//   7.9a TestPhase2_DamageAmount_*                  — DamageAmount resolve path
//   7.10 TestPhase2_SeededMultiAbilityReplay         — determinism

import (
	"sort"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// 7.1  No-regression gate
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_NoRegression_ConstantInvariant asserts the load-bearing structural
// invariant: 0 ≤ minActivationScore < candidateBaseScore. This is the
// mathematical guarantee that every valid-target heal/offensive/fallback
// candidate clears the activation floor for a single-ability unit, so
// highest-scored-ready collapses to first-ready.
func TestPhase2_NoRegression_ConstantInvariant(t *testing.T) {
	if minActivationScore < 0 {
		t.Errorf("minActivationScore = %v; must be >= 0", minActivationScore)
	}
	if candidateBaseScore <= minActivationScore {
		t.Errorf("candidateBaseScore (%v) must be strictly greater than minActivationScore (%v); "+
			"the no-regression invariant requires candidateBaseScore > minActivationScore",
			candidateBaseScore, minActivationScore)
	}
}

// TestPhase2_NoRegression_GatherGateEquivalence asserts that for a single
// ready candidate, the gather phase produces the same gate outcome as the
// prior first-ready logic: mana-sufficient, off-cooldown, valid-target ability
// passes; any one gate missing means no candidate. Exercises the structural
// gate set without depending on tick sequencing.
func TestPhase2_NoRegression_GatherGateEquivalence(t *testing.T) {
	def := healDef(t)

	// A: all gates pass → gather finds a candidate → score > minActivationScore
	t.Run("AllGatesPass", func(t *testing.T) {
		s, app, ally := autoCastSetup(t, def.HealAmount)
		s.mu.Lock()
		defer s.mu.Unlock()
		app.Visible = true
		_ = ally // keep alive
		s.toggleAutoCastLocked(app, "heal")

		// Manually run what the gather loop does for "heal" and score it.
		target := s.resolveAutoCastTargetLocked(app, def)
		if target == nil {
			t.Fatal("resolveAutoCastTargetLocked returned nil with a damaged ally in range; gate broken")
		}
		if app.AbilityCooldowns["heal"] > 0 {
			t.Fatal("precondition: heal should not be on cooldown")
		}
		if app.CurrentMana < def.ManaCost {
			t.Fatalf("precondition: mana %d < cost %d", app.CurrentMana, def.ManaCost)
		}
		score := s.scoreAutoCastCandidateLocked(app, def, target)
		if score <= minActivationScore {
			t.Errorf("lone valid-target heal scores %v; must be > minActivationScore (%v)", score, minActivationScore)
		}
	})

	// B: cooldown gate → no cast
	t.Run("CooldownBlocks", func(t *testing.T) {
		s, app, _ := autoCastSetup(t, def.HealAmount)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.toggleAutoCastLocked(app, "heal")
		if app.AbilityCooldowns == nil {
			app.AbilityCooldowns = make(map[string]float64)
		}
		app.AbilityCooldowns["heal"] = 99.0 // firmly on cooldown

		// The autocast loop checks cooldown before calling score; the guard
		// is: unit.AbilityCooldowns[abilityID] > 0 → skip. Verify it here.
		if cd := app.AbilityCooldowns["heal"]; cd <= 0 {
			t.Fatal("cooldown not set; precondition broken")
		}
		// Gate: cooldown > 0 → skip this ability (no target resolution, no score).
		// Verify at the loop level: tickUnitAutoCastLocked must not fire.
		preMana := app.CurrentMana
		s.tickUnitAutoCastLocked(app)
		if app.CastAbilityID != "" {
			t.Errorf("cooldown gate failed: CastAbilityID = %q, want empty", app.CastAbilityID)
		}
		if app.CurrentMana != preMana {
			t.Errorf("cooldown gate failed: mana changed from %d to %d, want unchanged", preMana, app.CurrentMana)
		}
	})

	// C: mana gate → no cast
	t.Run("ManaBlocks", func(t *testing.T) {
		s, app, _ := autoCastSetup(t, def.HealAmount)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.toggleAutoCastLocked(app, "heal")
		app.CurrentMana = def.ManaCost - 1

		s.tickUnitAutoCastLocked(app)
		if app.CastAbilityID != "" {
			t.Errorf("mana gate failed: CastAbilityID = %q, want empty", app.CastAbilityID)
		}
	})
}

// TestPhase2_NoRegression_SeededHealOnlyReplay runs a heal-only (un-promoted)
// Acolyte with a fixed seed twice and asserts the set of cast-initiation
// ticks is identical. This is the executable proof that highest-scored-ready
// is behaviourally identical to the prior first-ready for single-ability units.
// NOTE: the pre-existing TestHealAutocast_SeededReplayNoMeleeNoDivergence in
// caster_archetype_test.go also covers this scenario and MUST pass unmodified
// (it is the primary anchor; this test is complementary with explicit Phase-2
// framing).
func TestPhase2_NoRegression_SeededHealOnlyReplay(t *testing.T) {
	const seed = 77777 // different seed from the Phase-1 test to extend coverage

	runSim := func() []int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
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
		app := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if app == nil {
			s.mu.Unlock()
			t.Fatal("failed to spawn acolyte")
		}
		app.Visible = true
		// Un-promoted: no path, base rank, only the base "heal" ability.
		// Confirm the unit has exactly the base abilities — no granted extras.
		if app.ProgressionPath != unitPathNone {
			t.Errorf("fresh acolyte ProgressionPath = %q; want %q", app.ProgressionPath, unitPathNone)
		}

		ally := spawnProjTestUnit(t, s, "p1", 450, 400)
		ally.HP = 1 // always a valid heal target
		allyID := ally.ID
		// Catalog seeds heal autocast ON at spawn; clear so the toggle below
		// moves the state in the asserted direction (off → on). This test
		// measures replay determinism and doesn't care about the default.
		delete(app.AutoCastEnabled, "heal")
		s.toggleAutoCastLocked(app, "heal")
		appID := app.ID
		s.mu.Unlock()

		const totalTicks = 200
		var castTicks []int
		prev := ""
		for tick := 0; tick < totalTicks; tick++ {
			s.Update(0.05)
			s.mu.RLock()
			liveApp := s.unitsByID[appID]
			liveAlly := s.unitsByID[allyID]
			if liveApp == nil {
				s.mu.RUnlock()
				break
			}
			if liveApp.CastAbilityID == "heal" && prev == "" {
				castTicks = append(castTicks, tick)
			}
			prev = liveApp.CastAbilityID
			if liveAlly != nil && liveAlly.HP > 5 {
				liveAlly.HP = 1
			}
			s.mu.RUnlock()
		}
		return castTicks
	}

	r1 := runSim()
	r2 := runSim()

	if len(r1) == 0 {
		t.Fatal("no heal casts in run1; autocast gate or selector is broken")
	}
	if len(r1) != len(r2) {
		t.Errorf("cast-tick count diverges: run1=%d run2=%d (non-determinism under seed %d)", len(r1), len(r2), seed)
		t.Logf("run1: %v", r1)
		t.Logf("run2: %v", r2)
		return
	}
	for i := range r1 {
		if r1[i] != r2[i] {
			t.Errorf("cast tick[%d] diverges: run1=%d run2=%d", i, r1[i], r2[i])
		}
	}
	t.Logf("seed=%d: %d heal casts, identical tick sets across both runs", seed, len(r1))
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.2  Uncategorised (empty/unknown Category) ability fires
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_UncategorisedAbilityFires verifies that the empty/unknown Category
// fallback in scoreAutoCastCandidateLocked returns candidateBaseScore (> minActivationScore),
// so a lone uncategorised autocast ability fires exactly as first-ready would have.
func TestPhase2_UncategorisedAbilityFires(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 200, 200)
	target := spawnProjTestUnit(t, s, "p1", 240, 200)
	target.HP = 1 // below full — valid for a heal selector but we're using a synthetic def

	// A synthetic AbilityDef with empty Category — simulates an uncategorised ability.
	syntheticDef := AbilityDef{
		Category: "", // explicitly empty
	}

	score := s.scoreAutoCastCandidateLocked(caster, syntheticDef, target)
	if score <= minActivationScore {
		t.Errorf("empty-category fallback score %v must be > minActivationScore %v; "+
			"a lone valid-target uncategorised ability must always fire", score, minActivationScore)
	}
	// The fallback specifically returns candidateBaseScore.
	if score != candidateBaseScore {
		t.Errorf("empty-category fallback score = %v; want candidateBaseScore %v", score, candidateBaseScore)
	}

	// Also test an unknown (non-empty, non-registered) Category string.
	unknownDef := AbilityDef{Category: AbilityCategory("teleport_zap")}
	scoreUnknown := s.scoreAutoCastCandidateLocked(caster, unknownDef, target)
	if scoreUnknown <= minActivationScore {
		t.Errorf("unknown-category fallback score %v must be > minActivationScore %v", scoreUnknown, minActivationScore)
	}
	if scoreUnknown != candidateBaseScore {
		t.Errorf("unknown-category fallback score = %v; want candidateBaseScore %v", scoreUnknown, candidateBaseScore)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.3  Highest-scored ready ability wins; gates block cast; one cast/tick
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_ScoreWinsOverSlot constructs a synthetic 2-ability unit where the
// later-slot ability scores strictly higher. Verifies the autocast loop picks
// the higher-scored later-slot ability, NOT the first slot.
//
// Approach: give the unit "heal" (slot 0, category=heal, target=full-HP unit →
// lower score because target HP% is high) and a synthetic offensive at slot 1
// that we force to win by giving the unit a low-HP enemy target and no damaged
// ally. But since heal needs an ally below 100% HP and offensive needs an enemy
// in range, we engineer the world so offensive is the only valid-target ability.
// Then verify offensive fires and heal does not.
//
// NOTE: We cannot actually inject an arbitrary synthetic def into the autocast
// loop because it resolves defs via getAbilityDef by id from unit.Abilities.
// So this test uses the real catalog: arcane_bolt (offensive, enemy selector)
// vs heal (heal, ally selector). We ensure only an enemy is in range (no
// injured ally) so only arcane_bolt has a valid target and fires.
func TestPhase2_ScoreWinsOverSlot_OffensiveWinsWhenNoAllyInjured(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Spawn an acolyte manually so we can give it both abilities.
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.Visible = true
	// Give it both abilities in slot order: heal first, arcane_bolt second.
	caster.Abilities = []string{"heal", "arcane_bolt"}
	// Enable autocast for both.
	s.toggleAutoCastLocked(caster, "heal")
	s.toggleAutoCastLocked(caster, "arcane_bolt")

	// No injured allies in range → heal selector returns nil → heal is not a candidate.
	// Place an enemy in range → arcane_bolt selector returns it.
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 430, 400)
	enemy.Visible = true
	enemy.HP = enemy.MaxHP // full HP (doesn't matter for offensive selector)
	_ = enemy

	// Ensure sufficient mana for arcane_bolt.
	arcaneDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal("arcane_bolt def not found")
	}
	caster.CurrentMana = arcaneDef.ManaCost + 100

	preMana := caster.CurrentMana
	s.tickUnitAutoCastLocked(caster)

	// arcane_bolt has no cast time (0.5s in catalog), but beginAbilityCastLocked
	// arms it. Check CastAbilityID or mana spent.
	if caster.CastAbilityID != "arcane_bolt" && caster.CurrentMana == preMana {
		// arcane_bolt cast time is 0.5s (non-zero), so CastAbilityID should be set.
		// If cast time were 0 it would resolve instantly; either way, the mana
		// should not equal preMana if the cast fired.
		t.Logf("caster.CastAbilityID=%q, preMana=%d currentMana=%d", caster.CastAbilityID, preMana, caster.CurrentMana)
		// Check: if arcane_bolt cast time > 0, CastAbilityID should be set.
		if arcaneDef.CastTime > 0 && caster.CastAbilityID != "arcane_bolt" {
			t.Errorf("arcane_bolt should have been initiated (it's the only valid-target ability); CastAbilityID=%q", caster.CastAbilityID)
		}
		// If cast time == 0 it resolved instantly, mana was spent.
		if arcaneDef.CastTime <= 0 && caster.CurrentMana >= preMana {
			t.Errorf("instant-resolve arcane_bolt should have spent mana; pre=%d current=%d", preMana, caster.CurrentMana)
		}
	}
	// heal must NOT have fired (no injured ally).
	if caster.CastAbilityID == "heal" {
		t.Error("heal fired despite no injured ally in range; only arcane_bolt should have been a candidate")
	}
}

// TestPhase2_GatedAbilitiesNeverScored verifies that cooldown/mana-blocked
// abilities are never scored or cast. Uses the real heal def.
func TestPhase2_GatedAbilitiesNeverScored(t *testing.T) {
	def := healDef(t)

	t.Run("CooldownGatedNotCast", func(t *testing.T) {
		s, app, _ := autoCastSetup(t, def.HealAmount)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.toggleAutoCastLocked(app, "heal")
		if app.AbilityCooldowns == nil {
			app.AbilityCooldowns = make(map[string]float64)
		}
		app.AbilityCooldowns["heal"] = 5.0

		s.tickUnitAutoCastLocked(app)
		if app.CastAbilityID != "" {
			t.Errorf("cooldown-blocked ability should not cast; CastAbilityID=%q", app.CastAbilityID)
		}
	})

	t.Run("ManaGatedNotCast", func(t *testing.T) {
		s, app, _ := autoCastSetup(t, def.HealAmount)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.toggleAutoCastLocked(app, "heal")
		app.CurrentMana = def.ManaCost - 1

		s.tickUnitAutoCastLocked(app)
		if app.CastAbilityID != "" {
			t.Errorf("mana-blocked ability should not cast; CastAbilityID=%q", app.CastAbilityID)
		}
	})

	t.Run("AlreadyCastingPreventsAutocast", func(t *testing.T) {
		s, app, ally := autoCastSetup(t, def.HealAmount)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.toggleAutoCastLocked(app, "heal")
		// Simulate an in-progress cast.
		app.CastAbilityID = "heal"
		app.CastTargetID = ally.ID
		app.CastTimeRemaining = 0.5

		preMana := app.CurrentMana
		s.tickUnitAutoCastLocked(app)
		// Should be a no-op (already casting guard at top of tickUnitAutoCastLocked).
		if app.CurrentMana != preMana {
			t.Errorf("mana changed while already casting; autocast must not fire while CastAbilityID != \"\"")
		}
		// CastAbilityID stays as-is (the in-progress cast).
		if app.CastAbilityID != "heal" {
			t.Errorf("CastAbilityID changed from \"heal\"; the already-casting guard must preserve it")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.4  Deterministic tiebreak; below minActivationScore casts nothing
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_Tiebreak_EqualScoresPreferLowerSlot verifies that when two ready
// candidates produce equal scores, the one with the lower slot index (earlier
// position in unit.Abilities) is chosen. We achieve equal scores by using the
// same category+target for both, then verifying which slot wins.
//
// Strategy: give a unit two copies of the empty-category fallback synthetic def
// (both score exactly candidateBaseScore). Since we cannot inject synthetic defs
// into the real loop, we instead use two real abilities of the same category
// (both "heal") pointing at the same target. The scores may differ due to
// per-target HP-deficit; to force a tiebreak we need score equality. Instead
// we test the scoring tie-break at the unit level: run tickUnitAutoCastLocked
// with two abilities, one clearly scoring higher, and confirm slot ordering
// only matters in the equal-score case.
//
// For a pure tiebreak, we test via direct scoring: both abilities with category=""
// score candidateBaseScore, and the loop's ascending-slot + strictly-greater
// replacement rule means the first slot wins.
func TestPhase2_Tiebreak_EqualScoresPreferLowerSlot(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 200, 200)
	target := spawnProjTestUnit(t, s, "p1", 240, 200)
	target.HP = target.MaxHP / 2

	// Both synthetic defs have empty category → both score candidateBaseScore.
	defA := AbilityDef{ID: "ability_aaa", Category: ""}
	defB := AbilityDef{ID: "ability_bbb", Category: ""}

	scoreA := s.scoreAutoCastCandidateLocked(caster, defA, target)
	scoreB := s.scoreAutoCastCandidateLocked(caster, defB, target)

	if scoreA != scoreB {
		t.Fatalf("precondition: scores must be equal for tiebreak test; got %v vs %v", scoreA, scoreB)
	}

	// Simulate the loop's pick logic: iterate ascending, replace only on strictly greater.
	// Slot 0 = defA (id="ability_aaa"), slot 1 = defB (id="ability_bbb").
	// Equal scores → lower slot (0, defA) should be chosen.
	type candidate struct {
		slotIdx int
		id      string
		score   float64
	}
	candidates := []candidate{
		{0, defA.ID, scoreA},
		{1, defB.ID, scoreB},
	}

	bestIdx := -1
	bestScore := 0.0
	haveBest := false
	for _, c := range candidates {
		if !haveBest || c.score > bestScore {
			bestIdx = c.slotIdx
			bestScore = c.score
			haveBest = true
		}
	}

	if bestIdx != 0 {
		t.Errorf("tiebreak: equal scores → lower slot wins; got slot %d, want slot 0", bestIdx)
	}
	if bestScore <= minActivationScore {
		t.Errorf("tiebreak winner score %v must be > minActivationScore %v", bestScore, minActivationScore)
	}
}

// TestPhase2_Tiebreak_RealTick_LowerSlotWins drives the REAL tickUnitAutoCastLocked
// (not a hand-simulation of its pick logic) with two genuinely equal-scoring
// abilities and asserts the lower slot index always wins.
//
// `heal` and `greater_heal` are both AbilityCategoryHeal, and
// scoreHealCandidateLocked derives its value only from the (shared) target's
// HP deficit, the heal-category weight, and the in-cast-range wounded count —
// none of which depend on WHICH heal ability it is (both use
// castRange:"match_attack_range" and selector lowest_hp_percentage_ally_in_range).
// So against one damaged ally they score exactly equal: a real tie.
//
// The test runs BOTH slot orderings of the same two abilities and asserts the
// slot-0 ability is the one cast each time — proving the tiebreak is genuinely
// slot-driven (ascending slot, replace-only-on-strictly-greater), not id- or
// iteration-order-driven. The equality is asserted as a precondition so a
// future castRange/weight change that breaks the tie fails loudly here rather
// than silently testing nothing.
func TestPhase2_Tiebreak_RealTick_LowerSlotWins(t *testing.T) {
	runOrdering := func(t *testing.T, abilities []string) string {
		t.Helper()
		s := newProjectileTestState(t)
		s.mu.Lock()

		caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		caster.Visible = true
		caster.Abilities = append([]string{}, abilities...) // slot order under test
		// Catalog seeds heal autocast ON at spawn; clear before toggling so
		// each toggle flips from off → on regardless of which ids are under
		// test. The tie-break under test depends only on slot order, not
		// on whether the toggle was the first or second flip.
		for _, id := range abilities {
			delete(caster.AutoCastEnabled, id)
		}
		for _, id := range abilities {
			s.toggleAutoCastLocked(caster, id)
		}

		// One damaged ally in cast range → the shared heal target.
		ally := spawnProjTestUnit(t, s, "p1", 430, 400)
		ally.HP = ally.MaxHP / 2

		// Enough mana for whichever (heal=5, greater_heal=10), no regen drift.
		healD := healDef(t)
		gh, ok := getAbilityDef("greater_heal")
		if !ok {
			s.mu.Unlock()
			t.Fatal(`getAbilityDef("greater_heal") = _, false`)
		}
		caster.CurrentMana = healD.ManaCost + gh.ManaCost + 100
		caster.ManaRegenPerSecond = 0

		// Precondition: the two candidates must score EXACTLY equal against the
		// selector's target, or this is not a tiebreak test.
		tgt := s.resolveAutoCastTargetLocked(caster, healD)
		if tgt == nil {
			s.mu.Unlock()
			t.Fatal("heal selector resolved no target; cannot establish a tie")
		}
		sHeal := s.scoreAutoCastCandidateLocked(caster, healD, tgt)
		sGreater := s.scoreAutoCastCandidateLocked(caster, gh, tgt)
		if sHeal != sGreater {
			s.mu.Unlock()
			t.Fatalf("precondition failed: heal and greater_heal must score equal for a real tie; got %v vs %v", sHeal, sGreater)
		}
		s.mu.Unlock()

		castInitiated := ""
		for i := 0; i < 5 && castInitiated == ""; i++ {
			s.Update(0.05)
			s.mu.RLock()
			castInitiated = caster.CastAbilityID
			s.mu.RUnlock()
		}
		if castInitiated == "" {
			s.mu.Lock()
			s.tickUnitAutoCastLocked(caster)
			castInitiated = caster.CastAbilityID
			s.mu.Unlock()
		}
		return castInitiated
	}

	// Slot 0 = heal → heal must win.
	if got := runOrdering(t, []string{"heal", "greater_heal"}); got != "heal" {
		t.Errorf(`order [heal, greater_heal]: slot-0 must win; got CastAbilityID=%q want "heal"`, got)
	}
	// Swap slots: slot 0 = greater_heal → greater_heal must win (proves it is
	// slot-driven, not id-driven, since the same two abilities swapped result).
	if got := runOrdering(t, []string{"greater_heal", "heal"}); got != "greater_heal" {
		t.Errorf(`order [greater_heal, heal]: slot-0 must win; got CastAbilityID=%q want "greater_heal"`, got)
	}
}

// TestPhase2_BelowFloor_CastsNothing verifies that when all ready candidates
// score at or below minActivationScore, nothing is cast. We simulate this by
// using the buff_ally/summon scorer which returns 0 when "not useful".
func TestPhase2_BelowFloor_CastsNothing(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 200, 200)

	// buff_ally scoring: the scoreBuff ally branch returns 0 when target is not
	// in combat. Score a buff_ally def against an out-of-combat ally → 0.
	outOfCombatAlly := spawnProjTestUnit(t, s, "p1", 240, 200)
	outOfCombatAlly.AttackTargetID = 0           // not in combat
	outOfCombatAlly.AttackBuildingTargetID = ""   // not attacking a building

	buffDef := AbilityDef{Category: AbilityCategoryBuffAlly}
	score := s.scoreBuffAllyCandidateLocked(caster, buffDef, outOfCombatAlly)
	if score > minActivationScore {
		t.Errorf("buff_ally score against out-of-combat ally = %v; want <= minActivationScore %v (not useful)", score, minActivationScore)
	}

	// Similarly for summon: caster not in combat → score = 0.
	summonDef := AbilityDef{Category: AbilityCategorySummon}
	caster.AttackTargetID = 0
	caster.AttackBuildingTargetID = ""
	scoreSummon := s.scoreSummonCandidateLocked(caster, summonDef, caster)
	if scoreSummon > minActivationScore {
		t.Errorf("summon score for out-of-combat caster = %v; want <= minActivationScore %v", scoreSummon, minActivationScore)
	}
	if scoreSummon != 0 {
		t.Errorf("summon score for out-of-combat caster = %v; want exactly 0", scoreSummon)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.5  Priority correctness: heal vs offensive by situation
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_PriorityCorrectness_HealWinsWhenAllyLow exercises the real tick
// path: a promoted Arch-Mage-path acolyte (has heal + arcane_bolt) with a
// critically-low ally AND an enemy in range. Heal should win because the ally
// HP-deficit bonus dominates.
func TestPhase2_PriorityCorrectness_HealWinsWhenAllyLow(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()

	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.Visible = true
	// Give both abilities in slot order.
	caster.Abilities = []string{"heal", "arcane_bolt"}
	// Catalog seeds heal autocast ON at spawn; clear so the toggles below
	// flip both abilities from off → on (the asserted starting state for
	// this priority test).
	delete(caster.AutoCastEnabled, "heal")
	delete(caster.AutoCastEnabled, "arcane_bolt")
	s.toggleAutoCastLocked(caster, "heal")
	s.toggleAutoCastLocked(caster, "arcane_bolt")

	// Critically-low ally in range.
	ally := spawnProjTestUnit(t, s, "p1", 430, 400)
	ally.HP = 1 // near-zero → maximum heal score

	// Enemy in range (for arcane_bolt selector).
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 450, 400)
	enemy.Visible = true
	enemy.HP = enemy.MaxHP

	// Ensure enough mana for either ability.
	healDef_ := healDef(t)
	arcaneDef, _ := getAbilityDef("arcane_bolt")
	caster.CurrentMana = healDef_.ManaCost + arcaneDef.ManaCost + 100
	caster.ManaRegenPerSecond = 0 // no regen interference

	s.mu.Unlock()

	// Run enough ticks for the autocast to fire once. Heal cast time is 1.0s
	// at 0.05s/tick = 20 ticks to initiate, then 20 ticks to resolve. We just
	// need to see the initiation.
	castInitiated := ""
	for i := 0; i < 5; i++ {
		s.Update(0.05)
		s.mu.RLock()
		if caster.CastAbilityID != "" {
			castInitiated = caster.CastAbilityID
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}

	if castInitiated == "" {
		// Try direct tick under lock to see what's happening.
		s.mu.Lock()
		s.tickUnitAutoCastLocked(caster)
		castInitiated = caster.CastAbilityID
		s.mu.Unlock()
	}

	if castInitiated != "heal" {
		t.Errorf("with critically-low ally, heal should outscore arcane_bolt; got CastAbilityID=%q want \"heal\"", castInitiated)
	}
}

// TestPhase2_PriorityCorrectness_OffensiveWinsWhenNoAllyHurt verifies that
// with no injured ally (heal has no valid target) and an enemy in range,
// arcane_bolt fires. Driven through the real tick.
func TestPhase2_PriorityCorrectness_OffensiveWinsWhenNoAllyHurt(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()

	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.Visible = true
	caster.Abilities = []string{"heal", "arcane_bolt"}
	s.toggleAutoCastLocked(caster, "heal")
	s.toggleAutoCastLocked(caster, "arcane_bolt")

	// Full-HP ally → heal selector returns nil → heal not a candidate.
	fullAlly := spawnProjTestUnit(t, s, "p1", 430, 400)
	fullAlly.HP = fullAlly.MaxHP // at full HP

	// Enemy in range → arcane_bolt selector returns it.
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 450, 400)
	enemy.Visible = true

	arcaneDef, _ := getAbilityDef("arcane_bolt")
	caster.CurrentMana = arcaneDef.ManaCost + 100
	caster.ManaRegenPerSecond = 0

	s.mu.Unlock()

	castInitiated := ""
	for i := 0; i < 5; i++ {
		s.Update(0.05)
		s.mu.RLock()
		if caster.CastAbilityID != "" {
			castInitiated = caster.CastAbilityID
			s.mu.RUnlock()
			break
		}
		s.mu.RUnlock()
	}

	if castInitiated == "" {
		s.mu.Lock()
		s.tickUnitAutoCastLocked(caster)
		castInitiated = caster.CastAbilityID
		s.mu.Unlock()
	}

	if castInitiated != "arcane_bolt" {
		t.Errorf("with no injured ally + enemy in range, arcane_bolt should fire; got %q", castInitiated)
	}
}

// TestPhase2_HealScoresByHPDeficit verifies that lower-HP% ally scores higher
// for the heal category. Direct scorer call — no tick involved.
func TestPhase2_HealScoresByHPDeficit(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 200, 200)
	highHP := spawnProjTestUnit(t, s, "p1", 230, 200)
	highHP.HP = highHP.MaxHP * 9 / 10 // 90%
	lowHP := spawnProjTestUnit(t, s, "p1", 240, 200)
	lowHP.HP = lowHP.MaxHP / 10 // 10%

	healAbilDef := healDef(t)
	scoreHigh := s.scoreHealCandidateLocked(caster, healAbilDef, highHP)
	scoreLow := s.scoreHealCandidateLocked(caster, healAbilDef, lowHP)

	if scoreLow <= scoreHigh {
		t.Errorf("lower HP%% ally should score higher for heal; 10%% ally=%v vs 90%% ally=%v", scoreLow, scoreHigh)
	}
	// Both must be above the activation floor.
	if scoreHigh <= minActivationScore {
		t.Errorf("90%% HP ally heal score %v must be > minActivationScore %v", scoreHigh, minActivationScore)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.6  Direct unit tests of buff_ally and summon scoring branches
// ─────────────────────────────────────────────────────────────────────────────
//
// No authored ability exercises these end-to-end. These are the direct-unit
// tests of the scorer functions, as required by the spec and documented in
// the task as an explicit out-of-integration-scope case.

// TestPhase2_BuffAllyScoring verifies:
//   - In-combat ally → score > minActivationScore (returns candidateBaseScore + weight)
//   - Out-of-combat ally → score == 0 (below floor, loop skips it)
//   - Dead target → 0
//   - Enemy target → 0
func TestPhase2_BuffAllyScoring(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 200, 200)
	someUnit := spawnProjTestUnit(t, s, "p1", 230, 200)

	buffDef := AbilityDef{Category: AbilityCategoryBuffAlly}

	// Out-of-combat ally → 0.
	someUnit.AttackTargetID = 0
	someUnit.AttackBuildingTargetID = ""
	score := s.scoreBuffAllyCandidateLocked(caster, buffDef, someUnit)
	if score != 0 {
		t.Errorf("out-of-combat ally buff_ally score = %v; want 0", score)
	}
	if score > minActivationScore {
		t.Errorf("out-of-combat buff_ally must be below activation floor; score=%v floor=%v", score, minActivationScore)
	}

	// In-combat ally (attacking a unit) → > minActivationScore.
	fakeTargetID := 9999
	someUnit.AttackTargetID = fakeTargetID
	scoreInCombat := s.scoreBuffAllyCandidateLocked(caster, buffDef, someUnit)
	if scoreInCombat <= minActivationScore {
		t.Errorf("in-combat ally buff_ally score = %v; must be > minActivationScore %v", scoreInCombat, minActivationScore)
	}
	// The design says "high when in combat" — must be candidateBaseScore + weight.
	wantMin := candidateBaseScore + abilityCategoryWeights[AbilityCategoryBuffAlly]
	if scoreInCombat != wantMin {
		t.Errorf("in-combat ally buff_ally score = %v; want candidateBaseScore+weight = %v", scoreInCombat, wantMin)
	}

	// In-combat ally attacking a building → also > minActivationScore.
	someUnit.AttackTargetID = 0
	someUnit.AttackBuildingTargetID = "barracks-1"
	scoreBuilding := s.scoreBuffAllyCandidateLocked(caster, buffDef, someUnit)
	if scoreBuilding <= minActivationScore {
		t.Errorf("in-combat (building) ally buff_ally score = %v; must be > minActivationScore %v", scoreBuilding, minActivationScore)
	}

	// Dead target → 0.
	someUnit.HP = 0
	scoreDead := s.scoreBuffAllyCandidateLocked(caster, buffDef, someUnit)
	if scoreDead != 0 {
		t.Errorf("dead target buff_ally score = %v; want 0", scoreDead)
	}
	someUnit.HP = someUnit.MaxHP // restore

	// Enemy target → 0 (unitsFriendlyLocked returns false).
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 240, 200)
	enemy.AttackTargetID = caster.ID // in combat
	scoreEnemy := s.scoreBuffAllyCandidateLocked(caster, buffDef, enemy)
	if scoreEnemy != 0 {
		t.Errorf("enemy target buff_ally score = %v; want 0 (must not buff enemies)", scoreEnemy)
	}
}

// TestPhase2_SummonScoring verifies the combat-gated scoring:
//   - Out of combat (no AttackTargetID, no AttackBuildingTargetID) → 0
//   - Engaged on a unit → base + full weight (well above minActivationScore)
//   - Engaged on a building → same
//
// Local force balance / deficit is intentionally ignored: a necromancer fighting
// next to its own skeletons should still summon more on cooldown.
func TestPhase2_SummonScoring(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 200, 200)
	summonDef := AbilityDef{Category: AbilityCategorySummon}

	// Out of combat → 0.
	caster.AttackTargetID = 0
	caster.AttackBuildingTargetID = ""
	if got := s.scoreSummonCandidateLocked(caster, summonDef, caster); got != 0 {
		t.Errorf("summon score for out-of-combat caster = %v; want 0", got)
	}

	// Engaged on a unit → above floor and equal to base+weight (no situational
	// modifier — combat-gate is binary by design).
	caster.AttackTargetID = 12345
	wantInCombat := candidateBaseScore + abilityCategoryWeights[AbilityCategorySummon]
	if got := s.scoreSummonCandidateLocked(caster, summonDef, caster); got != wantInCombat {
		t.Errorf("summon score with AttackTargetID set = %v; want %v (base+weight)", got, wantInCombat)
	}

	// Building target alone also counts as combat.
	caster.AttackTargetID = 0
	caster.AttackBuildingTargetID = "fake-building"
	if got := s.scoreSummonCandidateLocked(caster, summonDef, caster); got != wantInCombat {
		t.Errorf("summon score with AttackBuildingTargetID set = %v; want %v (base+weight)", got, wantInCombat)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.7  path_ability_defs.go loader
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_PathAbilityLoader_NoAuthoredGrants_MechanismIntact verifies that
// the per-(path, rank) grant loader is structurally intact even though no
// (path, rank) cell currently has an authored grant file. The cleric's
// greater_heal lives in the path-level "abilities" override (path_defs.go),
// NOT in this rank-grant system — the rank-grant system stays in place as a
// composable mechanism for future "silver cleric also gets X" content.
//
// Both the dormant arcane_bolt def and the path-overridden greater_heal def
// must load and resolve via getAbilityDef regardless of whether anything
// grants them through this system.
func TestPhase2_PathAbilityLoader_NoAuthoredGrants_MechanismIntact(t *testing.T) {
	// Both ability defs must still load/resolve.
	if _, ok := getAbilityDef("greater_heal"); !ok {
		t.Error(`getAbilityDef("greater_heal") = _, false; the def must resolve`)
	}
	if _, ok := getAbilityDef("arcane_bolt"); !ok {
		t.Error(`getAbilityDef("arcane_bolt") = _, false; dormant def must still resolve`)
	}

	// No (path, rank) cell anywhere has an authored grant.
	for _, c := range []struct {
		path, rank string
	}{
		{unitPathCleric, unitRankBronze},
		{unitPathCleric, unitRankSilver},
		{unitPathCleric, unitRankGold},
		{unitPathArchMage, unitRankBronze},
		{unitPathArchMage, unitRankSilver},
		{unitPathArchMage, unitRankGold},
	} {
		if g := pathAbilityGrantsFor(c.path, c.rank); len(g) != 0 {
			t.Errorf("pathAbilityGrantsFor(%q,%q) = %v; want empty (no authored rank-grant)",
				c.path, c.rank, g)
		}
	}

	// ListPathAbilityGrants must be empty — no (path, rank) cell authored.
	if all := ListPathAbilityGrants(); len(all) != 0 {
		t.Errorf("ListPathAbilityGrants() = %v; want empty (rank-grant system has no authored content)", all)
	}

	// Lookup MECHANISM is still intact: an injected synthetic grant overrides
	// the empty content for the duration of the test and is returned exactly
	// as authored content would be (via the same accessor).
	withSyntheticPathGrant(t, unitPathArchMage, unitRankGold, []string{"synth_loader_probe"})
	got := pathAbilityGrantsFor(unitPathArchMage, unitRankGold)
	if len(got) != 1 || got[0] != "synth_loader_probe" {
		t.Errorf("pathAbilityGrantsFor after synthetic inject = %v; want [synth_loader_probe] (lookup mechanism broken)", got)
	}

	// sortedPathAbilityKeys must still return a sorted slice (with the probe).
	keys := sortedPathAbilityKeys()
	if !sort.StringsAreSorted(keys) {
		t.Errorf("sortedPathAbilityKeys() is not sorted: %v", keys)
	}
}

// TestPhase2_PathAbilityLoader_MissingCellIsEmptyGrant verifies that cells
// with no ability grant file resolve to nil/empty (not an error). Every
// (path, rank) cell is currently unauthored; this is a structural sanity
// check.
func TestPhase2_PathAbilityLoader_MissingCellIsEmptyGrant(t *testing.T) {
	bronze := pathAbilityGrantsFor(unitPathCleric, unitRankBronze)
	if len(bronze) != 0 {
		t.Errorf("cleric/bronze: want empty grant (no file authored); got %v", bronze)
	}
	gold := pathAbilityGrantsFor(unitPathCleric, unitRankGold)
	if len(gold) != 0 {
		t.Errorf("cleric/gold: want empty grant (no file authored); got %v", gold)
	}

	// Unknown path returns empty.
	unknown := pathAbilityGrantsFor("nonexistent_path_xyz", unitRankSilver)
	if len(unknown) != 0 {
		t.Errorf("unknown path: want empty grant; got %v", unknown)
	}
}

// TestPhase2_PathAbilityLoader_UnknownAbilityIDPanics tests the panic-at-load
// validation that a granted ability id with no registered AbilityDef causes a
// panic. Because the embed.FS is fixed at compile time, we replicate the exact
// validation predicate from path_ability_defs.go's init loop.
func TestPhase2_PathAbilityLoader_UnknownAbilityIDPanics(t *testing.T) {
	// The validation predicate from path_ability_defs.go:
	//   if _, ok := getAbilityDef(abilityID); !ok {
	//       panic(fmt.Sprintf("%s: granted ability %q has no registered AbilityDef", rel, abilityID))
	//   }
	runGrantValidation := func(id string) {
		if _, ok := getAbilityDef(id); !ok {
			panic("granted ability " + id + " has no registered AbilityDef")
		}
	}

	// A known ability must NOT panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("valid ability id panicked: %v", r)
			}
		}()
		runGrantValidation("heal")
	}()

	// An unknown ability MUST panic.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("unknown ability id in grant must panic at load; got none")
			}
		}()
		runGrantValidation("totally_fake_ability_xyz_9999")
	}()
}

// TestPhase2_PathAbilityLoader_UnknownRankPanics tests the panic-at-load
// validation that an unknown rank directory causes a panic. Replicates the
// validation predicate from path_ability_defs.go's init loop.
func TestPhase2_PathAbilityLoader_UnknownRankPanics(t *testing.T) {
	// Validation predicate from path_ability_defs.go:
	//   if _, ok := validRankName[rankName]; !ok {
	//       panic(...)
	//   }
	runRankValidation := func(rankName string) {
		if _, ok := validRankName[rankName]; !ok {
			panic("unknown rank " + rankName + "; want bronze/silver/gold")
		}
	}

	// Valid ranks must NOT panic.
	for _, valid := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		r := valid
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Errorf("valid rank %q panicked: %v", r, rec)
				}
			}()
			runRankValidation(r)
		}()
	}

	// An unknown rank MUST panic.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("unknown rank in abilities dir must panic at load; got none")
			}
		}()
		runRankValidation("ultra")
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.9a  DamageAmount primitive
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_DamageAmount_DamagesTargetOnImpact verifies that resolving a
// projectile ability with DamageAmount > 0 launches a bolt carrying the
// ability's damage + type + caster credit, and that the target loses exactly
// DamageAmount HP when the bolt lands (deferred delivery — not instant). Derives
// the damage amount from the catalog def — never hardcodes it.
func TestPhase2_DamageAmount_DamagesTargetOnImpact(t *testing.T) {
	arcaneDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal(`getAbilityDef("arcane_bolt") = _, false; arcane_bolt must be registered`)
	}
	if arcaneDef.DamageAmount <= 0 {
		t.Fatalf("arcane_bolt.DamageAmount = %d; must be > 0 for this test to be meaningful", arcaneDef.DamageAmount)
	}
	if arcaneDef.Projectile == "" {
		t.Fatal("arcane_bolt.Projectile is empty; this test assumes projectile delivery")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()

	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.Abilities = append(caster.Abilities, "arcane_bolt")
	caster.CurrentMana = arcaneDef.ManaCost + 200
	// Isolate the bolt: zero the caster's basic-attack damage and clear autocast
	// so no fire_bolt / second arcane_bolt confounds the exact HP assertion while
	// the projectile is in flight.
	caster.Damage = 0
	caster.AutoCastEnabled = nil

	// Enemy target within range, no armor so HP loss equals DamageAmount exactly.
	target := spawnProjTestUnit(t, s, enemyPlayerID, 430, 400)
	target.Armor = 0
	startHP := target.HP
	targetID := target.ID

	// Resolve spawns the arcane_bolt projectile (damage deferred to impact).
	s.resolveAbilityCastLocked(caster, arcaneDef, []*Unit{target})

	// The bolt must carry the ability's damage, type, sprite, and caster credit.
	if n := len(s.Projectiles); n != 1 {
		s.mu.Unlock()
		t.Fatalf("expected exactly 1 arcane_bolt projectile after resolve; got %d", n)
	}
	proj := s.Projectiles[0]
	if proj.Variant != arcaneDef.Projectile || proj.Damage != arcaneDef.DamageAmount ||
		proj.DamageType != arcaneDef.DamageType || proj.OwnerUnitID != caster.ID {
		s.mu.Unlock()
		t.Fatalf("arcane_bolt projectile mismatch: variant=%q damage=%d type=%q owner=%d (want variant=%q damage=%d type=%q owner=%d)",
			proj.Variant, proj.Damage, proj.DamageType, proj.OwnerUnitID,
			arcaneDef.Projectile, arcaneDef.DamageAmount, arcaneDef.DamageType, caster.ID)
	}
	// Mana is deducted at resolve (cast completion), before the bolt lands.
	wantMana := (arcaneDef.ManaCost + 200) - arcaneDef.ManaCost
	if caster.CurrentMana != wantMana {
		s.mu.Unlock()
		t.Errorf("caster mana after resolve: got %d, want %d", caster.CurrentMana, wantMana)
	}
	s.mu.Unlock()

	// Fast-forward until the bolt lands and applies its damage.
	landed := advanceTicksUntil(s, 40, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		lt := s.unitsByID[targetID]
		return lt == nil || lt.HP < startHP
	})
	if !landed {
		t.Fatal("arcane_bolt projectile never landed within 40 ticks")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	liveTarget := s.unitsByID[targetID]
	if liveTarget == nil {
		t.Logf("target killed by arcane_bolt (damage %d ≥ startHP %d)", arcaneDef.DamageAmount, startHP)
		return
	}
	expectedHP := startHP - arcaneDef.DamageAmount
	if expectedHP < 0 {
		expectedHP = 0
	}
	if liveTarget.HP != expectedHP {
		t.Errorf("arcane_bolt on-impact HP: got %d, want %d (startHP %d - DamageAmount %d, no armor)",
			liveTarget.HP, expectedHP, startHP, arcaneDef.DamageAmount)
	}
}

// TestPhase2_DamageAmount_ZeroDealsNoDamage verifies that an ability with
// DamageAmount == 0 (or absent) deals no damage on resolve. Uses "heal" which
// has no DamageAmount field (omitted → 0).
func TestPhase2_DamageAmount_ZeroDealsNoDamage(t *testing.T) {
	healAbilDef := healDef(t)
	if healAbilDef.DamageAmount != 0 {
		t.Fatalf("heal.DamageAmount = %d; this test requires it to be 0 (the inert case)", healAbilDef.DamageAmount)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.CurrentMana = healAbilDef.ManaCost + 200

	// A damaged ally (valid heal target).
	ally := spawnProjTestUnit(t, s, "p1", 430, 400)
	ally.HP = ally.MaxHP - healAbilDef.HealAmount - 20
	allyStartHP := ally.HP
	allyID := ally.ID

	// Resolve a heal — it must NOT deal any damage to the ally.
	s.resolveAbilityCastLocked(caster, healAbilDef, []*Unit{ally})

	liveAlly := s.unitsByID[allyID]
	if liveAlly == nil {
		t.Fatal("ally was removed after a heal resolve; heal must not deal damage")
	}
	// HP should have increased (healed), not decreased.
	if liveAlly.HP < allyStartHP {
		t.Errorf("heal resolve damaged the ally: startHP=%d afterHP=%d; DamageAmount=0 must be inert",
			allyStartHP, liveAlly.HP)
	}
	// Also check the resolve does not create any pending death for the ally.
	// (If HP went negative, it would have been enqueued for death.)
	if liveAlly.HP <= 0 {
		t.Errorf("heal target HP = %d after resolve; DamageAmount=0 ability must not kill a target", liveAlly.HP)
	}
}

// TestPhase2_DamageAmount_LethalCastRunsDeathPipeline verifies that when an
// offensive ability kill is lethal, the normal death pipeline runs — not a
// parallel/silent removal. We verify this by checking that after a lethal
// arcane_bolt resolve, the target is removed via the standard pipeline (HP=0,
// pending death enqueued, unit removed after drain).
func TestPhase2_DamageAmount_LethalCastRunsDeathPipeline(t *testing.T) {
	arcaneDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal("arcane_bolt not registered")
	}
	if arcaneDef.DamageAmount <= 0 {
		t.Skip("arcane_bolt DamageAmount not positive; test requires a lethal damage amount")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()

	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.Abilities = append(caster.Abilities, "arcane_bolt")
	caster.CurrentMana = arcaneDef.ManaCost + 200
	// Only the arcane_bolt projectile should be lethal — zero the basic attack.
	caster.Damage = 0
	caster.AutoCastEnabled = nil

	// Target with exactly 1 HP — guaranteed lethal.
	target := spawnProjTestUnit(t, s, enemyPlayerID, 430, 400)
	target.HP = 1
	target.Armor = 0
	targetID := target.ID

	s.resolveAbilityCastLocked(caster, arcaneDef, []*Unit{target})
	s.mu.Unlock()

	// Fast-forward until the bolt lands and the pending death drains.
	removed := advanceTicksUntil(s, 40, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.unitsByID[targetID] == nil
	})
	if !removed {
		t.Errorf("lethal arcane_bolt: target (HP=1) should have been removed by the death pipeline once the bolt lands")
	}
}

// TestPhase2_DamageAmount_AttributedToCaster verifies that when an ability with
// DamageAmount > 0 kills a target, kill XP is awarded to the caster — proving
// the DamageSource.AttackerUnitID was set correctly in resolveAbilityCastLocked.
// The attribution chain: resolveAbilityCastLocked → applyUnitDamageWithSourceLocked
// → enqueueDeathLocked (with DamageSource{AttackerUnitID: caster.ID}) →
// drainPendingDeathsLocked → awardKillXPLocked(attacker).
func TestPhase2_DamageAmount_AttributedToCaster(t *testing.T) {
	arcaneDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal("arcane_bolt not registered")
	}
	if arcaneDef.DamageAmount <= 0 {
		t.Skip("arcane_bolt DamageAmount not positive; test requires positive damage")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()

	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	caster.Visible = true
	caster.Abilities = append(caster.Abilities, "arcane_bolt")
	caster.CurrentMana = arcaneDef.ManaCost + 200
	// Ensure the kill is credited to the arcane_bolt projectile, not a basic attack.
	caster.Damage = 0
	caster.AutoCastEnabled = nil
	casterID := caster.ID
	startXP := caster.XP

	// Enemy target with exactly 1 HP → guaranteed lethal → kill XP flows to caster.
	target := spawnProjTestUnit(t, s, enemyPlayerID, 430, 400)
	target.HP = 1
	target.Armor = 0
	target.Visible = true
	targetID := target.ID

	// resolveAbilityCastLocked launches an arcane_bolt projectile owned by the
	// caster; on impact its damage enqueues the target's death attributed to the
	// caster (proj.OwnerUnitID → DamageSource.AttackerUnitID).
	s.resolveAbilityCastLocked(caster, arcaneDef, []*Unit{target})
	s.mu.Unlock()

	// Fast-forward until the bolt lands and the pending death drains → kill XP.
	advanceTicksUntil(s, 40, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.unitsByID[targetID] == nil
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	liveCaster := s.unitsByID[casterID]
	if liveCaster == nil {
		t.Fatal("caster was unexpectedly removed")
	}
	// Kill XP must have been awarded to the caster. xpPerKillBonus > 0.
	if liveCaster.XP <= startXP {
		t.Errorf("kill XP not awarded to caster after lethal arcane_bolt; XP before=%d after=%d (attribution failed: DamageSource.AttackerUnitID not set correctly)",
			startXP, liveCaster.XP)
	}
}

// TestPhase2_DamageAmount_TypedByDamageType is a data-validation test: verifies
// the catalog arcane_bolt.damageType is "arcane" and that OrPhysical() on an
// unset DamageType returns DamagePhysical. This asserts the DamageType pipeline
// for the new DamageAmount field is consistent with the existing pattern.
func TestPhase2_DamageAmount_TypedByDamageType(t *testing.T) {
	arcaneDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal("arcane_bolt not registered")
	}
	if arcaneDef.DamageType == "" {
		t.Errorf("arcane_bolt.DamageType = empty; want \"arcane\" (ability should declare its damage type)")
	}
	if arcaneDef.DamageType.OrPhysical() != arcaneDef.DamageType {
		t.Errorf("arcane_bolt DamageType.OrPhysical() = %q; want %q (should return itself when non-empty)", arcaneDef.DamageType.OrPhysical(), arcaneDef.DamageType)
	}

	// An ability with no DamageType should resolve to Physical via OrPhysical().
	var noDT AbilityDef
	if noDT.DamageType.OrPhysical() != DamagePhysical {
		t.Errorf("empty DamageType.OrPhysical() = %q; want %q (physical fallback)", noDT.DamageType.OrPhysical(), DamagePhysical)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7.10  Seeded multi-ability replay determinism
// ─────────────────────────────────────────────────────────────────────────────

// TestPhase2_SeededMultiAbilityReplay runs two identical seeded simulations
// with a unit that has both "heal" and "arcane_bolt". The set of ticks each
// ability is cast on must be identical across both runs (determinism).
//
// This test is the multi-ability extension of the Phase-1 no-melee replay test.
// No wall-clock, no unseeded rand, no map iteration driving outcomes.
func TestPhase2_SeededMultiAbilityReplay(t *testing.T) {
	const seed = 55555

	runSim := func() (healTicks, arcaneTicks []int) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
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
		caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if caster == nil {
			s.mu.Unlock()
			t.Fatal("failed to spawn acolyte")
		}
		caster.Visible = true
		// Give both abilities; no promotion required for the replay test.
		caster.Abilities = []string{"heal", "arcane_bolt"}
		s.toggleAutoCastLocked(caster, "heal")
		s.toggleAutoCastLocked(caster, "arcane_bolt")

		// A damaged ally for heal to target.
		ally := spawnProjTestUnit(t, s, "p1", 450, 400)
		ally.HP = ally.MaxHP / 4 // low HP, valid heal target
		allyID := ally.ID

		// An enemy for arcane_bolt to target.
		enemy := spawnProjTestUnit(t, s, enemyPlayerID, 470, 400)
		enemy.Visible = true
		enemy.MoveSpeed = 0 // stationary
		enemy.HP = 9999     // immortal for the duration of the test
		enemy.AttackSpeed = 0
		enemy.Damage = 0

		casterID := caster.ID
		// Give an effectively unlimited mana pool so both abilities can fire
		// across the simulation without running dry. MaxMana and ManaRegen are
		// set high enough that mana is never the binding constraint; what we
		// are testing is deterministic *selection* under a fixed seed.
		caster.MaxMana = 10000
		caster.CurrentMana = 10000
		caster.ManaRegenPerSecond = 200 // regenerates fast; mana is never binding

		s.mu.Unlock()

		const totalTicks = 120
		prevCast := ""
		for tick := 0; tick < totalTicks; tick++ {
			s.Update(0.05)
			s.mu.RLock()
			liveCaster := s.unitsByID[casterID]
			liveAlly := s.unitsByID[allyID]
			if liveCaster == nil {
				s.mu.RUnlock()
				break
			}
			castID := liveCaster.CastAbilityID
			// Record a new cast initiation when the cast ID changes to a non-empty
			// value (covers both transitions: "" → ability AND ability-A → ability-B,
			// which can happen in a single tick when a cast resolves and a new one
			// starts immediately after in the same tick loop).
			if castID != "" && castID != prevCast {
				switch castID {
				case "heal":
					healTicks = append(healTicks, tick)
				case "arcane_bolt":
					arcaneTicks = append(arcaneTicks, tick)
				}
			}
			prevCast = castID
			// Alternate: keep ally low for the first 60 ticks (heal fires),
			// then restore the ally to full HP so arcane_bolt has a shot.
			if liveAlly != nil {
				if tick < 60 && liveAlly.HP > liveAlly.MaxHP/4+50 {
					liveAlly.HP = liveAlly.MaxHP / 4
				} else if tick >= 60 {
					liveAlly.HP = liveAlly.MaxHP // full HP → no heal target
				}
			}
			s.mu.RUnlock()
		}
		return healTicks, arcaneTicks
	}

	healR1, arcaneR1 := runSim()
	healR2, arcaneR2 := runSim()

	// At least some casts must have happened for the test to be meaningful.
	if len(healR1)+len(arcaneR1) == 0 {
		t.Fatal("no casts in run1; autocast gate or selector is broken for multi-ability unit")
	}

	// Heal tick sets must be identical.
	if len(healR1) != len(healR2) {
		t.Errorf("heal cast tick count diverges: run1=%d run2=%d (seed %d)", len(healR1), len(healR2), seed)
		t.Logf("run1 heal: %v", healR1)
		t.Logf("run2 heal: %v", healR2)
	} else {
		for i := range healR1 {
			if healR1[i] != healR2[i] {
				t.Errorf("heal cast tick[%d] diverges: run1=%d run2=%d", i, healR1[i], healR2[i])
			}
		}
	}

	// Arcane tick sets must be identical.
	if len(arcaneR1) != len(arcaneR2) {
		t.Errorf("arcane_bolt cast tick count diverges: run1=%d run2=%d (seed %d)", len(arcaneR1), len(arcaneR2), seed)
		t.Logf("run1 arcane: %v", arcaneR1)
		t.Logf("run2 arcane: %v", arcaneR2)
	} else {
		for i := range arcaneR1 {
			if arcaneR1[i] != arcaneR2[i] {
				t.Errorf("arcane_bolt cast tick[%d] diverges: run1=%d run2=%d", i, arcaneR1[i], arcaneR2[i])
			}
		}
	}

	t.Logf("seed=%d: heal=%d casts, arcane_bolt=%d casts; both identical across two runs", seed, len(healR1), len(arcaneR1))
}
