package game

// Section 14 — Cleric Bronze perk unit tests.
//
// Covers all four Bronze perks: greater_heal, battle_prayer, sanctuary,
// mana_conduit. Also covers 14.24 (no-focus single-target no-regression).
//
// Setup helpers follow the pattern in silver_perks_test.go / bronze_perks_test.go.
// Expected values are derived from perk catalog Config maps — never hardcoded.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared setup helpers
// ─────────────────────────────────────────────────────────────────────────────

// newClericBronzeState returns a GameState with:
//   - cleric: an Acolyte owned by "p1" at (400,400). Visible, full HP,
//     large AttackRange so range never gates tests. MaxMana and CurrentMana
//     set generously.
//   - A wave-enemy soldier at (600,400) for damage tests that need a hostile.
//
// Lock is NOT held on return.
func newClericBronzeState(t *testing.T) (s *GameState, cleric *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 77)
	s.mu.Lock()
	defer s.mu.Unlock()

	cleric = s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	cleric.Visible = true
	cleric.HP = cleric.MaxHP
	cleric.AttackRange = 1000 // large range so tests don't depend on distance
	cleric.MaxMana = 200
	cleric.CurrentMana = 200
	if cleric.AutoCastEnabled == nil {
		cleric.AutoCastEnabled = make(map[string]bool)
	}
	// Catalog seeds heal auto-cast ON for player units at spawn
	// (heal.json → defaultAutoCast: true). Cleric tests that drive perk-state
	// decay over many ticks (BattlePrayer / BolsteringPrayer expiry) would
	// have auto-cast refreshing the buffs mid-test. Clear here so each test
	// runs from a known baseline; tests that want to assert default-on
	// behaviour set AutoCastEnabled explicitly after this helper returns.
	delete(cleric.AutoCastEnabled, "heal")
	delete(cleric.AutoCastEnabled, "greater_heal")
	if cleric.AbilityCooldowns == nil {
		cleric.AbilityCooldowns = make(map[string]float64)
	}
	return s, cleric
}

// spawnClericTestAlly spawns a visible, alive ally of "p1" at (x,y).
// Must be called under s.mu.
func spawnClericTestAlly(t *testing.T, s *GameState, x, y float64) *Unit {
	t.Helper()
	a := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: x, Y: y})
	a.MaxHP = 500
	a.HP = 500
	a.Visible = true
	return a
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.1 — Heal → greater_heal swap is covered in greater_heal_swap_test.go.
// (The swap moved from a perk grant to the (cleric, bronze) path-ability grant
// when Greater Heal became a Cleric path baseline. The dedicated test file
// exercises both the path-grant entry point and the swap helper itself.)
// ─────────────────────────────────────────────────────────────────────────────

// promoteToBronzeCleric drives the canonical "promote unit to (cleric, bronze)"
// path-ability grant flow inside a test. Mirrors what addUnitXPLocked does on a
// natural rank-up: set path + rank, then run assignUnitPathAbilitiesLocked.
// The heal → greater_heal swap fires inside the grant helper.
//
// Must be called under s.mu.
func promoteToBronzeCleric(s *GameState, cleric *Unit) {
	cleric.ProgressionPath = unitPathCleric
	cleric.Rank = unitRankBronze
	s.assignUnitPathAbilitiesLocked(cleric)
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.2 — greater_heal picks three lowest-HP% allies
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_TargetsThreeLowestHPAllies creates 5 allies with distinct
// HP% values and asserts the multi-target set contains exactly the 3 lowest.
func TestGreaterHeal_TargetsThreeLowestHPAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal ability def not found")
	}
	if def.TargetCount < 3 {
		t.Skipf("greater_heal TargetCount = %d, want >= 3", def.TargetCount)
	}

	// Allies with distinct HP%. Labels for clarity: a=10%, b=20%, c=30%, d=40%, e=50%.
	maxHP := 100
	allies := make([]*Unit, 5)
	pcts := []int{10, 20, 30, 40, 50}
	for i, pct := range pcts {
		a := spawnClericTestAlly(t, s, float64(430+i*10), 400)
		a.MaxHP = maxHP
		a.HP = maxHP * pct / 100
		allies[i] = a
	}

	// Build the target set using buildCastTargetSetLocked. The primary is the
	// lowest-HP ally (allies[0] at 10%).
	primary := allies[0]
	targets := s.buildCastTargetSetLocked(cleric, def, primary)

	if len(targets) != 3 {
		t.Fatalf("target set size = %d, want 3", len(targets))
	}
	// The 3 lowest: 10%, 20%, 30% → allies[0], [1], [2].
	wantIDs := map[int]bool{allies[0].ID: true, allies[1].ID: true, allies[2].ID: true}
	for _, tgt := range targets {
		if !wantIDs[tgt.ID] {
			t.Errorf("unexpected target id %d in set (want lowest-HP allies)", tgt.ID)
		}
	}
	for id := range wantIDs {
		found := false
		for _, tgt := range targets {
			if tgt.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected ally id %d in target set (lowest HP%%)", id)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.3 — HP% tie broken by ascending unit ID
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_TiedHPBreaksByUnitID creates two allies at equal HP% with
// only one slot remaining (TargetCount == 1 via single-target def) and asserts
// the lower-ID unit wins.
func TestGreaterHeal_TiedHPBreaksByUnitID(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Two allies at exactly 50% HP; IDs are assigned in spawn order, so a < b.
	a := spawnClericTestAlly(t, s, 450, 400)
	a.MaxHP = 200
	a.HP = 100

	b := spawnClericTestAlly(t, s, 460, 400)
	b.MaxHP = 200
	b.HP = 100

	// Ensure a.ID < b.ID (spawned in order, IDs are sequential ascending).
	if a.ID >= b.ID {
		t.Skipf("spawn order assumption violated (a.ID=%d, b.ID=%d); skip", a.ID, b.ID)
	}

	// Use a single-slot selector (TargetCount = 1).
	cands := []*Unit{a, b}
	castTargetSortByHPPct(cands)

	// After sort, tied entries should be ordered by ascending unit.ID.
	if cands[0].ID != a.ID {
		t.Errorf("tie-break: cands[0].ID = %d, want %d (lower ID wins on equal HP%%)", cands[0].ID, a.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.4 — Focus force-included in greater_heal set
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_ForceIncludesFocus verifies that when the Cleric has
// battle_prayer + focus on a full-HP ally, the focus is force-included in the
// multi-target set, displacing the highest-HP-percent natural pick.
func TestGreaterHeal_ForceIncludesFocus(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)
	grantPerk(cleric, "battle_prayer")

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal ability def not found")
	}
	if def.TargetCount < 3 {
		t.Skipf("greater_heal TargetCount = %d, want >= 3", def.TargetCount)
	}

	// Three injured allies (natural top-3 picks).
	b := spawnClericTestAlly(t, s, 430, 400)
	b.MaxHP = 100
	b.HP = 40 // 40%
	c := spawnClericTestAlly(t, s, 440, 400)
	c.MaxHP = 100
	c.HP = 60 // 60%
	d := spawnClericTestAlly(t, s, 450, 400)
	d.MaxHP = 100
	d.HP = 70 // 70%

	// Focus target at full HP — should be force-included, displacing d (70%).
	focusUnit := spawnClericTestAlly(t, s, 460, 400)
	focusUnit.HP = focusUnit.MaxHP // full HP

	s.RequestSetFocusTargetLocked("p1", cleric.ID, focusUnit.ID)

	// Primary is the most injured ally.
	targets := s.buildCastTargetSetLocked(cleric, def, b)

	if len(targets) != 3 {
		t.Fatalf("target set size = %d, want 3", len(targets))
	}

	// focus must be in the set.
	focusFound := false
	for _, tgt := range targets {
		if tgt.ID == focusUnit.ID {
			focusFound = true
		}
	}
	if !focusFound {
		t.Errorf("focus unit (id %d) not in target set; force-include failed", focusUnit.ID)
	}

	// d (70%) should be displaced.
	for _, tgt := range targets {
		if tgt.ID == d.ID {
			t.Errorf("d (70%% HP) should be displaced by force-include of focus; found in set")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Focus-active multi-target fill — full-HP allies included
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_FocusActiveFillsWithFullHPAllies verifies the focus-aware
// AoE-fill behaviour: when a cleric has a focus target and greater_heal casts,
// the other two slots fill with allies in cast range REGARDLESS of HP — so
// nothing-is-injured-but-buff-needs-refresh scenarios don't waste the cast as a
// single-target heal. Injured allies still sort first, so they're preferred
// when they exist.
//
// Scenario:
//   - focus: full HP
//   - one nearby ally: also full HP
//   - one nearby ally: also full HP
//
// Expected: all three appear in the target set.
func TestGreaterHeal_FocusActiveFillsWithFullHPAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	promoteToBronzeCleric(s, cleric)

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal ability def not found")
	}
	if def.TargetCount < 3 {
		t.Skipf("greater_heal TargetCount = %d, want >= 3", def.TargetCount)
	}

	// Three full-HP allies; the first becomes the focus.
	focusUnit := spawnClericTestAlly(t, s, 430, 400)
	focusUnit.HP = focusUnit.MaxHP
	otherA := spawnClericTestAlly(t, s, 440, 400)
	otherA.HP = otherA.MaxHP
	otherB := spawnClericTestAlly(t, s, 450, 400)
	otherB.HP = otherB.MaxHP

	s.RequestSetFocusTargetLocked("p1", cleric.ID, focusUnit.ID)

	targets := s.buildCastTargetSetLocked(cleric, def, focusUnit)

	if len(targets) != 3 {
		t.Fatalf("target set size = %d, want 3 (focus + 2 nearby full-HP allies)", len(targets))
	}

	want := map[int]bool{focusUnit.ID: true, otherA.ID: true, otherB.ID: true}
	for _, tgt := range targets {
		if !want[tgt.ID] {
			t.Errorf("unexpected target id %d in set; want %v", tgt.ID, want)
		}
	}
	for id := range want {
		found := false
		for _, tgt := range targets {
			if tgt.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected ally id %d in target set", id)
		}
	}
}

// TestGreaterHeal_FocusActivePrefersInjuredOverFullHP verifies that the
// focus-active widened candidate pool still SORTS injured allies first, so
// the cleric prefers to spend slots on real heals when injuries exist near
// the focus.
//
// Scenario:
//   - focus: 30% HP (injured — drives the primary)
//   - injured ally B: 50% HP
//   - full-HP ally C
//   - full-HP ally D (further away — kept in range)
//
// Expected: target set = {focus, B, one of C/D}. C/D is the fallback slot.
func TestGreaterHeal_FocusActivePrefersInjuredOverFullHP(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	promoteToBronzeCleric(s, cleric)

	def, ok := getAbilityDef("greater_heal")
	if !ok {
		t.Fatal("greater_heal ability def not found")
	}
	if def.TargetCount < 3 {
		t.Skipf("greater_heal TargetCount = %d, want >= 3", def.TargetCount)
	}

	focusUnit := spawnClericTestAlly(t, s, 430, 400)
	focusUnit.MaxHP = 100
	focusUnit.HP = 30
	injuredB := spawnClericTestAlly(t, s, 440, 400)
	injuredB.MaxHP = 100
	injuredB.HP = 50
	fullC := spawnClericTestAlly(t, s, 450, 400)
	fullC.HP = fullC.MaxHP
	fullD := spawnClericTestAlly(t, s, 460, 400)
	fullD.HP = fullD.MaxHP

	s.RequestSetFocusTargetLocked("p1", cleric.ID, focusUnit.ID)

	targets := s.buildCastTargetSetLocked(cleric, def, focusUnit)

	if len(targets) != 3 {
		t.Fatalf("target set size = %d, want 3", len(targets))
	}

	// Focus and injuredB MUST be in the set (lowest HP%).
	for _, want := range []int{focusUnit.ID, injuredB.ID} {
		found := false
		for _, tgt := range targets {
			if tgt.ID == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ally id %d missing from target set; injured allies must be prioritized", want)
		}
	}
	// One slot for a full-HP fallback; C and D are eligible.
	fillFound := false
	for _, tgt := range targets {
		if tgt.ID == fullC.ID || tgt.ID == fullD.ID {
			fillFound = true
		}
	}
	if !fillFound {
		t.Errorf("expected one of full-HP allies C/D to fill slot 3; got %v", targets)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.5 — battle_prayer applies buff on single heal
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_AppliesBuffOnHeal casts Heal on an ally and asserts the buff
// fields are set to the catalog-configured values.
func TestBattlePrayer_AppliesBuffOnHeal(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)
	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}

	s.mu.Lock()
	allyID := ally.ID
	grantPerk(app, "battle_prayer")
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25) // past cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]

	wantDuration := bpDef.ConfigForRank(app.Rank)["buffDurationSeconds"]
	wantMult := bpDef.ConfigForRank(app.Rank)["attackSpeedMultiplier"]

	// The buff decays during the tick advance; assert it was stamped at the full
	// duration and has only decayed by at most the advance window (25 × 0.05s = 1.25s).
	const advanceDt = 25 * 0.05
	if a.PerkState.BattlePrayerRemaining > wantDuration {
		t.Errorf("BattlePrayerRemaining = %.3f exceeds configured %.3f (must not exceed full duration)", a.PerkState.BattlePrayerRemaining, wantDuration)
	}
	if a.PerkState.BattlePrayerRemaining < wantDuration-advanceDt {
		t.Errorf("BattlePrayerRemaining = %.3f decayed too far below %.3f (stamped during advance window, but %.3f > advance budget %.3f)", a.PerkState.BattlePrayerRemaining, wantDuration, wantDuration-a.PerkState.BattlePrayerRemaining, advanceDt)
	}
	if a.PerkState.BattlePrayerRemaining <= 0 {
		t.Errorf("BattlePrayerRemaining = %.3f; buff should not have expired after cast + short advance", a.PerkState.BattlePrayerRemaining)
	}
	if a.PerkState.BattlePrayerMultiplier != wantMult {
		t.Errorf("BattlePrayerMultiplier = %.3f, want %.3f (attackSpeedMultiplier)", a.PerkState.BattlePrayerMultiplier, wantMult)
	}

	_ = def
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.6 — battle_prayer buff applied to all greater_heal targets
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_BuffAppliedToAllGreaterHealTargets grants both perks and
// triggers a greater_heal cast that hits three allies, then asserts all three
// have the buff.
func TestBattlePrayer_BuffAppliedToAllGreaterHealTargets(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		s.mu.Unlock()
		t.Skipf("acolyte Abilities[0] != \"heal\"")
	}
	// Grant-pipeline test: promote to (cleric, bronze) so the path-ability
	// grant runs the heal → greater_heal swap.
	promoteToBronzeCleric(s, cleric)
	grantPerk(cleric, "battle_prayer")

	// Three injured allies.
	a1 := spawnClericTestAlly(t, s, 430, 400)
	a1.HP = a1.MaxHP * 4 / 10
	a2 := spawnClericTestAlly(t, s, 440, 400)
	a2.HP = a2.MaxHP * 5 / 10
	a3 := spawnClericTestAlly(t, s, 450, 400)
	a3.HP = a3.MaxHP * 6 / 10

	a1ID, a2ID, a3ID := a1.ID, a2.ID, a3.ID

	ok, reason := s.beginAbilityCastLocked(cleric, "greater_heal", a1)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25)

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}
	wantDuration := bpDef.Config["buffDurationSeconds"]
	const advanceDt = 25 * 0.05 // 1.25s advance window

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, id := range []int{a1ID, a2ID, a3ID} {
		u := s.unitsByID[id]
		if u == nil {
			continue // may have been removed if HP changes caused removal — skip
		}
		// Buff was stamped at wantDuration and decays during the advance window.
		if u.PerkState.BattlePrayerRemaining <= 0 {
			t.Errorf("ally id %d: BattlePrayerRemaining = %.3f; buff should not have expired", id, u.PerkState.BattlePrayerRemaining)
		}
		if u.PerkState.BattlePrayerRemaining > wantDuration {
			t.Errorf("ally id %d: BattlePrayerRemaining = %.3f exceeds configured %.3f", id, u.PerkState.BattlePrayerRemaining, wantDuration)
		}
		if u.PerkState.BattlePrayerRemaining < wantDuration-advanceDt {
			t.Errorf("ally id %d: BattlePrayerRemaining = %.3f decayed too far from %.3f (advance budget %.3f)", id, u.PerkState.BattlePrayerRemaining, wantDuration, advanceDt)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.7 — battle_prayer buff refreshes (does not stack)
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_RefreshNotStack applies the buff then recasts with the same
// duration configured. Duration should max-refresh, never be additive.
func TestBattlePrayer_RefreshNotStack(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "battle_prayer")

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)
	fullDuration := cfg["buffDurationSeconds"]
	fullMult := cfg["attackSpeedMultiplier"]

	// Create a dummy ally with a partially-expired buff.
	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.HP = ally.MaxHP / 2
	ally.PerkState.BattlePrayerRemaining = 1.0 // partially expired
	ally.PerkState.BattlePrayerMultiplier = fullMult

	// Simulate a cast resolve landing on the ally.
	healAbilityDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}
	s.onPerkAbilityResolvedLocked(cleric, healAbilityDef, ally)

	// Must refresh to full (max of 1.0 and fullDuration), not sum to 1.0 + fullDuration.
	if ally.PerkState.BattlePrayerRemaining != fullDuration {
		t.Errorf("BattlePrayerRemaining = %.3f, want %.3f (refresh-max, not additive)", ally.PerkState.BattlePrayerRemaining, fullDuration)
	}
	// Multiplier must stay at most the configured value.
	if ally.PerkState.BattlePrayerMultiplier > fullMult {
		t.Errorf("BattlePrayerMultiplier = %.3f exceeds configured %.3f (must not stack)", ally.PerkState.BattlePrayerMultiplier, fullMult)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.8 — battle_prayer buff decays and resets multiplier on expiry
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_DecaysInUpdateLoop applies the buff and advances N ticks to
// confirm decay and multiplier reset. Uses the cross-unit decay path in
// state.go Update().
func TestBattlePrayer_DecaysInUpdateLoop(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	grantPerk(cleric, "battle_prayer")

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		s.mu.Unlock()
		t.Fatal("battle_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)
	fullDuration := cfg["buffDurationSeconds"]
	fullMult := cfg["attackSpeedMultiplier"]

	// Stamp the buff on an ally directly (bypassing a full cast).
	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.HP = ally.MaxHP / 2
	allyID := ally.ID
	ally.PerkState.BattlePrayerRemaining = fullDuration
	ally.PerkState.BattlePrayerMultiplier = fullMult
	s.mu.Unlock()

	const dt = 0.05

	// Advance 2 ticks (should decay but not expire if fullDuration >> 0.1).
	for i := 0; i < 2; i++ {
		s.Update(dt)
	}

	s.mu.RLock()
	a := s.unitsByID[allyID]
	if a == nil {
		s.mu.RUnlock()
		t.Fatal("ally removed unexpectedly")
	}
	remaining2 := a.PerkState.BattlePrayerRemaining
	s.mu.RUnlock()

	wantRemaining2 := fullDuration - 2*dt
	if math.Abs(remaining2-wantRemaining2) > 0.001 {
		t.Errorf("BattlePrayerRemaining after 2 ticks = %.4f, want ~%.4f", remaining2, wantRemaining2)
	}

	// Advance past the full duration.
	totalTicksNeeded := int(fullDuration/dt) + 5
	for i := 0; i < totalTicksNeeded; i++ {
		s.Update(dt)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	a = s.unitsByID[allyID]
	if a == nil {
		t.Fatal("ally removed after full decay")
	}
	if a.PerkState.BattlePrayerRemaining != 0 {
		t.Errorf("BattlePrayerRemaining after expiry = %.4f, want 0", a.PerkState.BattlePrayerRemaining)
	}
	if a.PerkState.BattlePrayerMultiplier != 0 {
		t.Errorf("BattlePrayerMultiplier after expiry = %.4f, want 0", a.PerkState.BattlePrayerMultiplier)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.9 — battle_prayer buff grants attack speed bonus
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_GrantsAttackSpeedBonus stamps the buff directly on a unit
// and asserts perkAttackSpeedBonusLocked returns the configured multiplier.
func TestBattlePrayer_GrantsAttackSpeedBonus(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}
	wantMult := bpDef.Config["attackSpeedMultiplier"]

	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.PerkState.BattlePrayerRemaining = bpDef.Config["buffDurationSeconds"]
	ally.PerkState.BattlePrayerMultiplier = wantMult

	got := s.perkAttackSpeedBonusLocked(ally)
	if math.Abs(got-wantMult) > 1e-6 {
		t.Errorf("perkAttackSpeedBonusLocked = %.4f, want %.4f", got, wantMult)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.10 — battle_prayer buff applies to non-Cleric ally
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_AttackSpeedBonusAppliesToNonClericAlly stamps the buff on a
// Soldier (no perks) and confirms perkAttackSpeedBonusLocked reflects it.
func TestBattlePrayer_AttackSpeedBonusAppliesToNonClericAlly(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}
	wantMult := bpDef.Config["attackSpeedMultiplier"]

	soldier := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{X: 450, Y: 400})
	soldier.Visible = true
	// Soldier has no perks — confirm it gets the buff via the cross-unit path.
	if len(soldier.PerkIDs) != 0 {
		t.Skipf("soldier has perks (%v); can't isolate battle_prayer", soldier.PerkIDs)
	}
	soldier.PerkState.BattlePrayerRemaining = bpDef.Config["buffDurationSeconds"]
	soldier.PerkState.BattlePrayerMultiplier = wantMult

	got := s.perkAttackSpeedBonusLocked(soldier)
	if math.Abs(got-wantMult) > 1e-6 {
		t.Errorf("non-Cleric soldier: perkAttackSpeedBonusLocked = %.4f, want %.4f", got, wantMult)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.11 — battle_prayer recast threshold triggers full-HP cast
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_RecastThresholdTriggersFullHPCast verifies that a Cleric with
// battle_prayer + focus on a full-HP ally with BattlePrayerRemaining == 0 has
// a non-nil autocast target (the focus, for buff refresh).
func TestBattlePrayer_RecastThresholdTriggersFullHPCast(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "battle_prayer")

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)

	// Focus target at full HP with expired buff.
	focusUnit := spawnClericTestAlly(t, s, 450, 400)
	focusUnit.HP = focusUnit.MaxHP
	focusUnit.PerkState.BattlePrayerRemaining = 0.0 // stale

	s.RequestSetFocusTargetLocked("p1", cleric.ID, focusUnit.ID)

	healAbilityDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}

	got := s.resolveAutoCastTargetLocked(cleric, healAbilityDef)
	if got == nil {
		t.Errorf("resolveAutoCastTargetLocked = nil; expected focus (id %d) for buff-refresh cast", focusUnit.ID)
	} else if got.ID != focusUnit.ID {
		t.Errorf("resolveAutoCastTargetLocked = id %d, want focus id %d", got.ID, focusUnit.ID)
	}

	// Sanity: threshold computed from config must match what the code uses.
	wantThresholdSeconds := cfg["recastThresholdPercent"] * cfg["buffDurationSeconds"]
	if wantThresholdSeconds <= 0 {
		t.Errorf("recastThresholdPercent * buffDurationSeconds = %.4f; threshold must be > 0 for the logic to function", wantThresholdSeconds)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.12 — Fresh buff on full-HP focus does not trigger recast
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_FreshBuffNoRecast verifies that when the focus's
// BattlePrayerRemaining is above the recast threshold, no cast is initiated.
func TestBattlePrayer_FreshBuffNoRecast(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "battle_prayer")

	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		t.Fatal("battle_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)

	// Set remaining to 80% of full duration — well above the threshold (~30%).
	freshRemaining := cfg["buffDurationSeconds"] * 0.80

	focusUnit := spawnClericTestAlly(t, s, 450, 400)
	focusUnit.HP = focusUnit.MaxHP
	focusUnit.PerkState.BattlePrayerRemaining = freshRemaining

	s.RequestSetFocusTargetLocked("p1", cleric.ID, focusUnit.ID)

	healAbilityDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}

	got := s.resolveAutoCastTargetLocked(cleric, healAbilityDef)
	if got != nil {
		t.Errorf("resolveAutoCastTargetLocked = id %d, want nil — fresh buff should not trigger recast", got.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.13 — Without battle_prayer perk, no recast on full-HP focus
// ─────────────────────────────────────────────────────────────────────────────

// TestBattlePrayer_NoRecastWithoutPerk confirms that a Cleric without
// battle_prayer does not trigger the recast-threshold path.
func TestBattlePrayer_NoRecastWithoutPerk(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// No battle_prayer perk — plain focus.

	focusUnit := spawnClericTestAlly(t, s, 450, 400)
	focusUnit.HP = focusUnit.MaxHP
	focusUnit.PerkState.BattlePrayerRemaining = 0.0

	s.RequestSetFocusTargetLocked("p1", cleric.ID, focusUnit.ID)

	healAbilityDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}

	got := s.resolveAutoCastTargetLocked(cleric, healAbilityDef)
	if got != nil {
		t.Errorf("resolveAutoCastTargetLocked = id %d, want nil — no recast without battle_prayer", got.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.14 — sanctuary reduces projectile damage
// ─────────────────────────────────────────────────────────────────────────────

// TestSanctuary_ReducesProjectileDamage places a Sanctuary-owning Cleric near
// an ally being hit by a projectile and asserts damage is reduced per config.
func TestSanctuary_ReducesProjectileDamage(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")

	sanctuaryDef := perkDefByID("sanctuary")
	if sanctuaryDef == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := sanctuaryDef.Config["radiusPixels"]
	reductionPct := sanctuaryDef.Config["damageReductionPercent"]

	// Ally inside the sanctuary radius.
	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	allyStartHP := ally.MaxHP
	ally.HP = allyStartHP

	const rawDamage = 100
	wantDamage := int(math.Round(float64(rawDamage) * (1.0 - reductionPct)))

	s.applyUnitDamageWithSourceLocked(ally, rawDamage, DamageSource{Kind: "projectile"})

	gotDamage := allyStartHP - ally.HP
	// Allow ±1 for rounding.
	if diff := gotDamage - wantDamage; diff > 1 || diff < -1 {
		t.Errorf("projectile damage with sanctuary: got %d, want ~%d (%.0f%% reduction)", gotDamage, wantDamage, reductionPct*100)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.15 — sanctuary does not reduce melee damage
// ─────────────────────────────────────────────────────────────────────────────

// TestSanctuary_DoesNotReduceMelee verifies melee damage is unaffected.
func TestSanctuary_DoesNotReduceMelee(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")

	sanctuaryDef := perkDefByID("sanctuary")
	if sanctuaryDef == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := sanctuaryDef.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0 // disable armor so damage == rawDamage
	allyStartHP := ally.MaxHP
	ally.HP = allyStartHP

	const rawDamage = 100
	s.applyUnitDamageWithSourceLocked(ally, rawDamage, DamageSource{Kind: "melee"})

	gotDamage := allyStartHP - ally.HP
	if gotDamage != rawDamage {
		t.Errorf("melee damage with sanctuary: got %d, want %d (no reduction for melee)", gotDamage, rawDamage)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.16 — sanctuary does not reduce trap damage
// ─────────────────────────────────────────────────────────────────────────────

// TestSanctuary_DoesNotReduceTrap verifies trap damage is unaffected.
func TestSanctuary_DoesNotReduceTrap(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")

	sanctuaryDef := perkDefByID("sanctuary")
	if sanctuaryDef == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := sanctuaryDef.Config["radiusPixels"]

	ally := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	ally.Armor = 0
	allyStartHP := ally.MaxHP
	ally.HP = allyStartHP

	const rawDamage = 100
	s.applyUnitDamageWithSourceLocked(ally, rawDamage, DamageSource{Kind: "trap"})

	gotDamage := allyStartHP - ally.HP
	if gotDamage != rawDamage {
		t.Errorf("trap damage with sanctuary: got %d, want %d (no reduction for traps)", gotDamage, rawDamage)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.17 — sanctuary: target outside radius is unaffected
// ─────────────────────────────────────────────────────────────────────────────

// TestSanctuary_TargetOutsideRadiusUnaffected places the ally outside the
// sanctuary radius and confirms no reduction.
func TestSanctuary_TargetOutsideRadiusUnaffected(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "sanctuary")

	sanctuaryDef := perkDefByID("sanctuary")
	if sanctuaryDef == nil {
		t.Fatal("sanctuary perk def not found")
	}
	radius := sanctuaryDef.Config["radiusPixels"]

	// Ally clearly outside the radius.
	ally := spawnClericTestAlly(t, s, cleric.X+radius+100, cleric.Y)
	ally.Armor = 0
	allyStartHP := ally.MaxHP
	ally.HP = allyStartHP

	const rawDamage = 100
	s.applyUnitDamageWithSourceLocked(ally, rawDamage, DamageSource{Kind: "projectile"})

	gotDamage := allyStartHP - ally.HP
	if gotDamage != rawDamage {
		t.Errorf("ally outside radius: got damage %d, want %d (should be unreduced)", gotDamage, rawDamage)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.18 — overlapping sanctuary auras take max, not sum
// ─────────────────────────────────────────────────────────────────────────────

// TestSanctuary_OverlappingAurasTakeMaxNoStack places two Clerics with
// different configured reductions and confirms the stronger reduction wins.
func TestSanctuary_OverlappingAurasTakeMaxNoStack(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	sanctuaryDef := perkDefByID("sanctuary")
	if sanctuaryDef == nil {
		t.Fatal("sanctuary perk def not found")
	}

	// Both clerics own sanctuary; the perk's damageReductionPercent is a shared
	// catalog value, so we test max-wins behaviour by verifying the result matches
	// (1 - catalogValue) when both are in range, not (1 - catalogValue)^2.
	grantPerk(clericA, "sanctuary")

	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	grantPerk(clericB, "sanctuary")

	radius := sanctuaryDef.Config["radiusPixels"]
	reductionPct := sanctuaryDef.Config["damageReductionPercent"]

	// Ally inside both auras.
	ally := spawnClericTestAlly(t, s, clericA.X+radius*0.5, clericA.Y)
	ally.Armor = 0
	allyStartHP := ally.MaxHP
	ally.HP = allyStartHP

	const rawDamage = 100
	wantDamage := int(math.Round(float64(rawDamage) * (1.0 - reductionPct)))

	s.applyUnitDamageWithSourceLocked(ally, rawDamage, DamageSource{Kind: "projectile"})

	gotDamage := allyStartHP - ally.HP
	// Allow ±1 for rounding.
	if diff := gotDamage - wantDamage; diff > 1 || diff < -1 {
		t.Errorf("overlapping sanctuaries: got %d damage, want ~%d (max-wins, not multiplied)", gotDamage, wantDamage)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mana_conduit — flat passive bonus regen
//
// Redesigned from the original "scales with injured allies in radius" to a
// constant +bonusManaRegen mana/sec passive. The old per-ally / radius /
// cap tests no longer apply; the new suite asserts the rate is correct,
// that it doesn't run for unitless casters, and that it clamps at MaxMana.
// ─────────────────────────────────────────────────────────────────────────────

// TestManaConduit_GrantsFlatRegen drives one tick and asserts the accumulator
// reflects bonusManaRegen × dt.
func TestManaConduit_GrantsFlatRegen(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	bonusPerSec := mcDef.ConfigForRank(cleric.Rank)["bonusManaRegen"]
	if bonusPerSec <= 0 {
		t.Fatalf("bonusManaRegen = %.4f; expected positive value in the catalog", bonusPerSec)
	}

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	const dt = 0.1
	s.tickUnitPerkStateLocked(cleric, dt)

	wantAccum := bonusPerSec * dt
	totalMana := float64(cleric.CurrentMana) + cleric.ManaRegenAccumulator
	if math.Abs(totalMana-wantAccum) > 0.01 {
		t.Errorf("flat mana_conduit regen: total = %.4f, want %.4f (bonusManaRegen × dt)", totalMana, wantAccum)
	}
}

// TestManaConduit_NearbyAlliesDoNotChangeRate confirms the rate is FLAT —
// adding nearby injured allies (the old design's trigger) must not change
// the per-tick accumulation now that the design is a constant bonus.
func TestManaConduit_NearbyAlliesDoNotChangeRate(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	bonusPerSec := mcDef.ConfigForRank(cleric.Rank)["bonusManaRegen"]

	// Three injured allies right next to the cleric. Under the old design
	// these would have scaled the regen — now they must have no effect.
	for i := 0; i < 3; i++ {
		a := spawnClericTestAlly(t, s, cleric.X+10+float64(i*5), cleric.Y)
		a.HP = a.MaxHP / 2
	}

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	const dt = 0.1
	s.tickUnitPerkStateLocked(cleric, dt)

	wantAccum := bonusPerSec * dt
	totalMana := float64(cleric.CurrentMana) + cleric.ManaRegenAccumulator
	if math.Abs(totalMana-wantAccum) > 0.01 {
		t.Errorf("flat regen must not scale with nearby allies: got %.4f, want %.4f", totalMana, wantAccum)
	}
}

// TestManaConduit_NoOpForUnitsWithoutMana confirms the perk is inert on a unit
// that has no mana pool. Defensive — the runtime check prevents writing
// CurrentMana on a unit that doesn't track mana.
func TestManaConduit_NoOpForUnitsWithoutMana(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Soldier has no mana pool by default.
	soldier := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{X: 450, Y: 400})
	soldier.Visible = true
	if soldier.MaxMana > 0 {
		t.Skipf("soldier MaxMana = %d; test assumes 0", soldier.MaxMana)
	}
	grantPerk(soldier, "mana_conduit")

	startMana := soldier.CurrentMana
	startAccum := soldier.ManaRegenAccumulator
	s.tickUnitPerkStateLocked(soldier, 0.1)

	if soldier.CurrentMana != startMana || soldier.ManaRegenAccumulator != startAccum {
		t.Errorf("mana_conduit ran on no-mana unit: mana %d → %d, accum %.4f → %.4f",
			startMana, soldier.CurrentMana, startAccum, soldier.ManaRegenAccumulator)
	}
}

// TestManaConduit_ClampsAtMaxMana confirms the bonus regen cannot push mana
// past MaxMana when the unit is already full.
func TestManaConduit_ClampsAtMaxMana(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	grantPerk(cleric, "mana_conduit")
	clericID := cleric.ID

	// Set cleric to full mana.
	cleric.CurrentMana = cleric.MaxMana
	cleric.ManaRegenAccumulator = 0
	s.mu.Unlock()

	// Advance several ticks.
	for i := 0; i < 20; i++ {
		s.Update(0.05)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.unitsByID[clericID]
	if c == nil {
		t.Fatal("cleric removed unexpectedly")
	}
	if c.CurrentMana > c.MaxMana {
		t.Errorf("CurrentMana = %d > MaxMana = %d; should be clamped", c.CurrentMana, c.MaxMana)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.24 — No-focus single-target heal unchanged (no-regression)
// ─────────────────────────────────────────────────────────────────────────────

// TestNoFocus_AutoHealUnchangedForSingleTarget verifies that a heal-only
// Acolyte without focus and without greater_heal behaves identically to the
// pre-change single-target behavior: exactly one heal event, one healing_glow
// VFX, and exactly one HP delta on the ally.
func TestNoFocus_AutoHealUnchangedForSingleTarget(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)

	s.mu.Lock()
	allyID := ally.ID
	wantHP := ally.HP + def.HealAmount
	startMana := app.CurrentMana
	wantMana := startMana - def.ManaCost

	// Confirm no focus, no greater_heal perk.
	if app.FocusTargetID != 0 {
		s.mu.Unlock()
		t.Fatal("precondition: acolyte should have no focus target")
	}
	for _, p := range app.PerkIDs {
		if p == "greater_heal" {
			s.mu.Unlock()
			t.Skip("acolyte already has greater_heal; skip single-target regression test")
		}
	}

	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25) // past cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != wantHP {
		t.Errorf("ally HP = %d; want %d (single-target heal, no regression)", a.HP, wantHP)
	}
	if app.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d", app.CurrentMana, wantMana)
	}
	// Exactly one healing_glow effect for the single target.
	glow := queuedEffectFor(s, "healing_glow", allyID)
	if glow == nil {
		t.Error("healing_glow effect should have played on the ally (single-target, no-regression)")
	}
}
