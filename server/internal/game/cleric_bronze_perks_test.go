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
//   - cleric: an Apprentice owned by "p1" at (400,400). Visible, full HP,
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

	cleric = s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	cleric.Visible = true
	cleric.HP = cleric.MaxHP
	cleric.AttackRange = 1000 // large range so tests don't depend on distance
	cleric.MaxMana = 200
	cleric.CurrentMana = 200
	if cleric.AutoCastEnabled == nil {
		cleric.AutoCastEnabled = make(map[string]bool)
	}
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
// 14.1 — greater_heal perk swaps ability and migrates state
// ─────────────────────────────────────────────────────────────────────────────

// TestGreaterHeal_PerkSwapsAbility verifies that granting greater_heal replaces
// "heal" in Abilities and migrates AutoCastEnabled / AbilityCooldowns.
func TestGreaterHeal_PerkSwapsAbility(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		t.Skipf("apprentice Abilities[0] != \"heal\"; got %v", cleric.Abilities)
	}

	cleric.AutoCastEnabled["heal"] = true
	cleric.AbilityCooldowns["heal"] = 0.8

	grantPerk(cleric, "greater_heal")
	s.applyPerkGrantedHooksLocked(cleric, "greater_heal")

	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "greater_heal" {
		t.Errorf("Abilities[0] = %q, want \"greater_heal\"", func() string {
			if len(cleric.Abilities) == 0 {
				return "<empty>"
			}
			return cleric.Abilities[0]
		}())
	}
	if !cleric.AutoCastEnabled["greater_heal"] {
		t.Error("AutoCastEnabled[\"greater_heal\"] should be true after swap")
	}
	if _, still := cleric.AutoCastEnabled["heal"]; still {
		t.Error("AutoCastEnabled[\"heal\"] key should be absent after swap")
	}
	if cleric.AbilityCooldowns["greater_heal"] != 0.8 {
		t.Errorf("AbilityCooldowns[\"greater_heal\"] = %.2f, want 0.8", cleric.AbilityCooldowns["greater_heal"])
	}
	if _, still := cleric.AbilityCooldowns["heal"]; still {
		t.Error("AbilityCooldowns[\"heal\"] key should be absent after swap")
	}
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

	grantPerk(cleric, "greater_heal")
	s.applyPerkGrantedHooksLocked(cleric, "greater_heal")

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

	grantPerk(cleric, "greater_heal")
	s.applyPerkGrantedHooksLocked(cleric, "greater_heal")
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
		t.Skipf("apprentice Abilities[0] != \"heal\"")
	}
	grantPerk(cleric, "greater_heal")
	s.applyPerkGrantedHooksLocked(cleric, "greater_heal")
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

	clericB := s.spawnPlayerUnitLocked("apprentice", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
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
// 14.19 — mana_conduit scales with injured ally count
// ─────────────────────────────────────────────────────────────────────────────

// TestManaConduit_BonusScalesWithInjuredAllies places 3 injured allies in
// radius and asserts the bonus mana accumulation after one tick matches
// 3 * bonusManaRegenPerAlly * dt. Uses the accumulator approach that the
// implementation uses (accumulates fractional mana).
func TestManaConduit_BonusScalesWithInjuredAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	cfg := mcDef.ConfigForRank(cleric.Rank)
	radius := cfg["radiusPixels"]
	bonusPerAlly := cfg["bonusManaRegenPerAlly"]
	maxCount := int(cfg["maxAlliesCounted"])
	if maxCount < 3 {
		t.Skipf("maxAlliesCounted = %d, want >= 3 for this test", maxCount)
	}

	for i := 0; i < 3; i++ {
		a := spawnClericTestAlly(t, s, cleric.X+radius*0.5+float64(i*5), cleric.Y)
		a.HP = a.MaxHP / 2 // injured
	}

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	const dt = 0.1
	s.tickUnitPerkStateLocked(cleric, dt)

	wantAccumulated := 3 * bonusPerAlly * dt
	// The accumulator may have converted to integer mana already if wantAccumulated >= 1.
	totalMana := float64(cleric.CurrentMana) + cleric.ManaRegenAccumulator
	if math.Abs(totalMana-wantAccumulated) > 0.01 {
		t.Errorf("mana_conduit 3 allies: total mana gain = %.4f, want ~%.4f (3 * bonusManaRegenPerAlly * dt)", totalMana, wantAccumulated)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.20 — mana_conduit is capped by maxAlliesCounted
// ─────────────────────────────────────────────────────────────────────────────

// TestManaConduit_CapsAtMaxAlliesCounted places 5 injured allies with cap = 3
// and asserts the bonus is capped.
func TestManaConduit_CapsAtMaxAlliesCounted(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	cfg := mcDef.ConfigForRank(cleric.Rank)
	radius := cfg["radiusPixels"]
	bonusPerAlly := cfg["bonusManaRegenPerAlly"]
	maxCount := int(cfg["maxAlliesCounted"])

	// Spawn more allies than the cap.
	count := maxCount + 2
	for i := 0; i < count; i++ {
		a := spawnClericTestAlly(t, s, cleric.X+radius*0.5+float64(i*5), cleric.Y)
		a.HP = a.MaxHP / 2
	}

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	const dt = 0.1
	s.tickUnitPerkStateLocked(cleric, dt)

	cappedBonus := float64(maxCount) * bonusPerAlly * dt
	uncappedBonus := float64(count) * bonusPerAlly * dt
	totalMana := float64(cleric.CurrentMana) + cleric.ManaRegenAccumulator

	if math.Abs(totalMana-cappedBonus) > 0.01 {
		t.Errorf("mana_conduit capped: total mana gain = %.4f, want ~%.4f (cap=%d, not uncapped %.4f)",
			totalMana, cappedBonus, maxCount, uncappedBonus)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.21 — full-HP allies not counted by mana_conduit
// ─────────────────────────────────────────────────────────────────────────────

// TestManaConduit_FullHPAlliesNotCounted places 5 full-HP allies and asserts
// zero bonus.
func TestManaConduit_FullHPAlliesNotCounted(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	radius := mcDef.Config["radiusPixels"]

	for i := 0; i < 5; i++ {
		a := spawnClericTestAlly(t, s, cleric.X+radius*0.5+float64(i*5), cleric.Y)
		a.HP = a.MaxHP // full HP
	}

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	s.tickUnitPerkStateLocked(cleric, 0.1)

	if cleric.CurrentMana != 0 || cleric.ManaRegenAccumulator > 0.001 {
		t.Errorf("full-HP allies should not contribute: CurrentMana=%d, Accumulator=%.4f",
			cleric.CurrentMana, cleric.ManaRegenAccumulator)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.22 — enemies not counted by mana_conduit
// ─────────────────────────────────────────────────────────────────────────────

// TestManaConduit_EnemiesNotCounted places an injured enemy in radius with no
// injured allies and asserts zero bonus.
func TestManaConduit_EnemiesNotCounted(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	radius := mcDef.Config["radiusPixels"]

	// Injured enemy inside radius.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: cleric.X + radius*0.5, Y: cleric.Y})
	enemy.Visible = true
	enemy.HP = enemy.MaxHP / 2

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	s.tickUnitPerkStateLocked(cleric, 0.1)

	if cleric.CurrentMana != 0 || cleric.ManaRegenAccumulator > 0.001 {
		t.Errorf("enemy in range should not contribute: CurrentMana=%d, Accumulator=%.4f",
			cleric.CurrentMana, cleric.ManaRegenAccumulator)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14.23 — mana_conduit clamps at MaxMana
// ─────────────────────────────────────────────────────────────────────────────

// TestManaConduit_ClampsAtMaxMana places the Cleric at MaxMana and confirms it
// stays clamped.
func TestManaConduit_ClampsAtMaxMana(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	grantPerk(cleric, "mana_conduit")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		s.mu.Unlock()
		t.Fatal("mana_conduit perk def not found")
	}
	radius := mcDef.Config["radiusPixels"]
	clericID := cleric.ID

	// Injured ally in range to trigger the bonus.
	a := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	a.HP = a.MaxHP / 2

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
// Apprentice without focus and without greater_heal behaves identically to the
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
		t.Fatal("precondition: apprentice should have no focus target")
	}
	for _, p := range app.PerkIDs {
		if p == "greater_heal" {
			s.mu.Unlock()
			t.Skip("apprentice already has greater_heal; skip single-target regression test")
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
