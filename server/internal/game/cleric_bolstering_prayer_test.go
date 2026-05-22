package game

// Bolstering Prayer (cleric bronze) test suite — armor-flavor counterpart to
// the Battle Prayer family in cleric_bronze_perks_test.go. Mechanics mirror
// Battle Prayer exactly (same trigger, target set, refresh-max semantics,
// cross-unit decay, recast-threshold autocast); these tests assert that
// equivalence and also cover the new armor-aggregation path.
//
// Tunable values (buffDurationSeconds, armorBonus, recastThresholdPercent)
// are read from the catalog via perkDefByID — no literals in the assertions.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Apply buff on single heal
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_AppliesBuffOnHeal casts Heal on an ally and asserts the
// armor-buff fields are set to the catalog-configured values.
func TestBolsteringPrayer_AppliesBuffOnHeal(t *testing.T) {
	s, app, ally := healSetup(t)
	def := healDef(t)
	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}

	s.mu.Lock()
	allyID := ally.ID
	grantPerk(app, "bolstering_prayer")
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25) // past cast time

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]

	cfg := bpDef.ConfigForRank(app.Rank)
	wantDuration := cfg["buffDurationSeconds"]
	wantArmor := cfg["armorBonus"]

	// The buff decays during the tick advance; assert it was stamped at the full
	// duration and has only decayed by at most the advance window (25 × 0.05s = 1.25s).
	const advanceDt = 25 * 0.05
	if a.PerkState.BolsteringPrayerRemaining > wantDuration {
		t.Errorf("BolsteringPrayerRemaining = %.3f exceeds configured %.3f", a.PerkState.BolsteringPrayerRemaining, wantDuration)
	}
	if a.PerkState.BolsteringPrayerRemaining < wantDuration-advanceDt {
		t.Errorf("BolsteringPrayerRemaining = %.3f decayed too far below %.3f (advance budget %.3f)",
			a.PerkState.BolsteringPrayerRemaining, wantDuration, advanceDt)
	}
	if a.PerkState.BolsteringPrayerRemaining <= 0 {
		t.Errorf("BolsteringPrayerRemaining = %.3f; buff should not have expired after cast + short advance", a.PerkState.BolsteringPrayerRemaining)
	}
	if a.PerkState.BolsteringPrayerArmor != wantArmor {
		t.Errorf("BolsteringPrayerArmor = %.3f, want %.3f (armorBonus)", a.PerkState.BolsteringPrayerArmor, wantArmor)
	}

	_ = def
}

// ─────────────────────────────────────────────────────────────────────────────
// Buff applied to all greater_heal targets
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_BuffAppliedToAllGreaterHealTargets grants the perk and
// runs a 3-target greater_heal, asserting all three allies receive the buff.
func TestBolsteringPrayer_BuffAppliedToAllGreaterHealTargets(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	if len(cleric.Abilities) == 0 || cleric.Abilities[0] != "heal" {
		s.mu.Unlock()
		t.Skipf("apprentice Abilities[0] != \"heal\"")
	}
	promoteToBronzeCleric(s, cleric)
	grantPerk(cleric, "bolstering_prayer")

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

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}
	wantDuration := bpDef.Config["buffDurationSeconds"]
	const advanceDt = 25 * 0.05

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, id := range []int{a1ID, a2ID, a3ID} {
		u := s.unitsByID[id]
		if u == nil {
			continue
		}
		if u.PerkState.BolsteringPrayerRemaining <= 0 {
			t.Errorf("ally id %d: BolsteringPrayerRemaining = %.3f; buff should not have expired", id, u.PerkState.BolsteringPrayerRemaining)
		}
		if u.PerkState.BolsteringPrayerRemaining > wantDuration {
			t.Errorf("ally id %d: BolsteringPrayerRemaining = %.3f exceeds configured %.3f", id, u.PerkState.BolsteringPrayerRemaining, wantDuration)
		}
		if u.PerkState.BolsteringPrayerRemaining < wantDuration-advanceDt {
			t.Errorf("ally id %d: BolsteringPrayerRemaining = %.3f decayed too far from %.3f (advance budget %.3f)",
				id, u.PerkState.BolsteringPrayerRemaining, wantDuration, advanceDt)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Refresh-max semantics, never additive
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_RefreshNotStack applies the buff then re-applies. The
// duration must max-refresh, not be additive. The armor field must not exceed
// the configured value.
func TestBolsteringPrayer_RefreshNotStack(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "bolstering_prayer")

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)
	fullDuration := cfg["buffDurationSeconds"]
	fullArmor := cfg["armorBonus"]

	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.HP = ally.MaxHP / 2
	ally.PerkState.BolsteringPrayerRemaining = 1.0 // partially expired
	ally.PerkState.BolsteringPrayerArmor = fullArmor

	healAbilityDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}
	s.onPerkAbilityResolvedLocked(cleric, healAbilityDef, ally)

	if ally.PerkState.BolsteringPrayerRemaining != fullDuration {
		t.Errorf("BolsteringPrayerRemaining = %.3f, want %.3f (refresh-max, not additive)", ally.PerkState.BolsteringPrayerRemaining, fullDuration)
	}
	if ally.PerkState.BolsteringPrayerArmor > fullArmor {
		t.Errorf("BolsteringPrayerArmor = %.3f exceeds configured %.3f (must not stack)", ally.PerkState.BolsteringPrayerArmor, fullArmor)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Cross-unit decay in state.go Update()
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_DecaysInUpdateLoop applies the buff and advances ticks
// to confirm decay and armor reset on expiry.
func TestBolsteringPrayer_DecaysInUpdateLoop(t *testing.T) {
	s, cleric := newClericBronzeState(t)

	s.mu.Lock()
	grantPerk(cleric, "bolstering_prayer")

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		s.mu.Unlock()
		t.Fatal("bolstering_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)
	fullDuration := cfg["buffDurationSeconds"]
	fullArmor := cfg["armorBonus"]

	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.HP = ally.MaxHP / 2
	allyID := ally.ID
	ally.PerkState.BolsteringPrayerRemaining = fullDuration
	ally.PerkState.BolsteringPrayerArmor = fullArmor
	s.mu.Unlock()

	const dt = 0.05

	// Advance 2 ticks (should decay but not expire).
	for i := 0; i < 2; i++ {
		s.Update(dt)
	}

	s.mu.RLock()
	a := s.unitsByID[allyID]
	if a == nil {
		s.mu.RUnlock()
		t.Fatal("ally removed unexpectedly")
	}
	remaining2 := a.PerkState.BolsteringPrayerRemaining
	s.mu.RUnlock()

	wantRemaining2 := fullDuration - 2*dt
	if math.Abs(remaining2-wantRemaining2) > 0.001 {
		t.Errorf("BolsteringPrayerRemaining after 2 ticks = %.4f, want ~%.4f", remaining2, wantRemaining2)
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
	if a.PerkState.BolsteringPrayerRemaining != 0 {
		t.Errorf("BolsteringPrayerRemaining after expiry = %.4f, want 0", a.PerkState.BolsteringPrayerRemaining)
	}
	if a.PerkState.BolsteringPrayerArmor != 0 {
		t.Errorf("BolsteringPrayerArmor after expiry = %.4f, want 0 (armor field must reset)", a.PerkState.BolsteringPrayerArmor)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Effective armor includes the buff
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_GrantsArmorBonus stamps the buff directly on a unit and
// asserts effectiveArmorLocked includes the bonus while active and excludes it
// after the cross-unit decay zeroes the field.
func TestBolsteringPrayer_GrantsArmorBonus(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}
	wantArmor := int(math.Round(bpDef.Config["armorBonus"]))

	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.Armor = 5 // baseline so we can assert additivity
	baseEffective := s.effectiveArmorLocked(ally)

	// Stamp the buff directly.
	ally.PerkState.BolsteringPrayerRemaining = bpDef.Config["buffDurationSeconds"]
	ally.PerkState.BolsteringPrayerArmor = bpDef.Config["armorBonus"]

	withBuff := s.effectiveArmorLocked(ally)
	if withBuff-baseEffective != wantArmor {
		t.Errorf("effectiveArmorLocked delta = %d (with - base = %d - %d), want %d",
			withBuff-baseEffective, withBuff, baseEffective, wantArmor)
	}

	// Zero out the buff state directly (simulates expiry mid-frame).
	ally.PerkState.BolsteringPrayerRemaining = 0
	ally.PerkState.BolsteringPrayerArmor = 0
	afterExpiry := s.effectiveArmorLocked(ally)
	if afterExpiry != baseEffective {
		t.Errorf("effectiveArmorLocked after expiry = %d, want baseline %d (no residual buff bonus)",
			afterExpiry, baseEffective)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Buff applies to non-Cleric ally (cross-unit semantics)
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_AppliesToNonClericAlly stamps the buff on a perkless
// Soldier and confirms effectiveArmorLocked reflects it.
func TestBolsteringPrayer_AppliesToNonClericAlly(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}
	wantArmor := int(math.Round(bpDef.Config["armorBonus"]))

	soldier := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{X: 450, Y: 400})
	soldier.Visible = true
	if len(soldier.PerkIDs) != 0 {
		t.Skipf("soldier has perks (%v); can't isolate bolstering_prayer in isolation", soldier.PerkIDs)
	}

	base := s.effectiveArmorLocked(soldier)
	soldier.PerkState.BolsteringPrayerRemaining = bpDef.Config["buffDurationSeconds"]
	soldier.PerkState.BolsteringPrayerArmor = bpDef.Config["armorBonus"]

	got := s.effectiveArmorLocked(soldier)
	if got-base != wantArmor {
		t.Errorf("non-Cleric soldier: effectiveArmorLocked delta = %d, want %d", got-base, wantArmor)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Recast threshold triggers full-HP cast (generalised path)
// ─────────────────────────────────────────────────────────────────────────────

// TestBolsteringPrayer_RecastThresholdTriggersFullHPCast verifies that a Cleric
// with bolstering_prayer + focus on a full-HP ally with BolsteringPrayerRemaining
// below the threshold has a non-nil autocast target (the focus, for refresh).
// Mirror of the battle_prayer recast test; covers the generalised heal-buff
// registry path in autocast_selectors.go.
func TestBolsteringPrayer_RecastThresholdTriggersFullHPCast(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "bolstering_prayer")

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)

	focusUnit := spawnClericTestAlly(t, s, 450, 400)
	focusUnit.HP = focusUnit.MaxHP
	focusUnit.PerkState.BolsteringPrayerRemaining = 0.0 // stale

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

	wantThresholdSeconds := cfg["recastThresholdPercent"] * cfg["buffDurationSeconds"]
	if wantThresholdSeconds <= 0 {
		t.Errorf("recastThresholdPercent * buffDurationSeconds = %.4f; threshold must be > 0", wantThresholdSeconds)
	}
}

// TestBolsteringPrayer_FreshBuffNoRecast verifies that a fresh buff (above the
// recast threshold) does not trigger a recast on a full-HP focus.
func TestBolsteringPrayer_FreshBuffNoRecast(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "bolstering_prayer")

	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}
	cfg := bpDef.ConfigForRank(cleric.Rank)

	freshRemaining := cfg["buffDurationSeconds"] * 0.80

	focusUnit := spawnClericTestAlly(t, s, 450, 400)
	focusUnit.HP = focusUnit.MaxHP
	focusUnit.PerkState.BolsteringPrayerRemaining = freshRemaining

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
// Independence from battle_prayer (cross-perk stacking)
// ─────────────────────────────────────────────────────────────────────────────

// TestBolstering_And_BattlePrayer_StackIndependently verifies that a unit healed
// (in sequence) by a Cleric with battle_prayer and a Cleric with bolstering_prayer
// receives BOTH buffs — they live on independent PerkState fields and never
// interfere. Both decay independently to 0.
func TestBolstering_And_BattlePrayer_StackIndependently(t *testing.T) {
	s, clericA := newClericBronzeState(t)

	s.mu.Lock()
	grantPerk(clericA, "battle_prayer")

	// Second Cleric, same player, owning bolstering_prayer.
	clericB := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	clericB.HP = clericB.MaxHP
	clericB.AttackRange = 1000
	clericB.MaxMana = 200
	clericB.CurrentMana = 200
	if clericB.AutoCastEnabled == nil {
		clericB.AutoCastEnabled = make(map[string]bool)
	}
	// Catalog seeds heal autocast ON at spawn; clear so the decay loop
	// below isn't continually refreshing the prayer buffs by re-casting heal.
	delete(clericB.AutoCastEnabled, "heal")
	if clericB.AbilityCooldowns == nil {
		clericB.AbilityCooldowns = make(map[string]float64)
	}
	grantPerk(clericB, "bolstering_prayer")

	// One injured ally that both Clerics will heal.
	ally := spawnClericTestAlly(t, s, 450, 400)
	ally.HP = ally.MaxHP / 2
	allyID := ally.ID

	healAbilityDef, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability def not found")
	}

	// Resolve both casts in-place via the post-cast hook — this isolates the
	// buff-application logic from cast-time / cooldown / mana concerns.
	s.onPerkAbilityResolvedLocked(clericA, healAbilityDef, ally)
	s.onPerkAbilityResolvedLocked(clericB, healAbilityDef, ally)
	s.mu.Unlock()

	bpBattle := perkDefByID("battle_prayer")
	bpBolster := perkDefByID("bolstering_prayer")
	if bpBattle == nil || bpBolster == nil {
		t.Fatal("perk defs missing")
	}
	cfgA := bpBattle.ConfigForRank(clericA.Rank)
	cfgB := bpBolster.ConfigForRank(clericB.Rank)

	s.mu.RLock()
	a := s.unitsByID[allyID]
	gotBPRem := a.PerkState.BattlePrayerRemaining
	gotBPMult := a.PerkState.BattlePrayerMultiplier
	gotBolRem := a.PerkState.BolsteringPrayerRemaining
	gotBolArm := a.PerkState.BolsteringPrayerArmor
	s.mu.RUnlock()

	if gotBPRem != cfgA["buffDurationSeconds"] {
		t.Errorf("BattlePrayerRemaining = %.3f, want %.3f", gotBPRem, cfgA["buffDurationSeconds"])
	}
	if gotBPMult != cfgA["attackSpeedMultiplier"] {
		t.Errorf("BattlePrayerMultiplier = %.3f, want %.3f", gotBPMult, cfgA["attackSpeedMultiplier"])
	}
	if gotBolRem != cfgB["buffDurationSeconds"] {
		t.Errorf("BolsteringPrayerRemaining = %.3f, want %.3f", gotBolRem, cfgB["buffDurationSeconds"])
	}
	if gotBolArm != cfgB["armorBonus"] {
		t.Errorf("BolsteringPrayerArmor = %.3f, want %.3f", gotBolArm, cfgB["armorBonus"])
	}

	// Advance past both durations and confirm both decay to 0 independently.
	maxDuration := cfgA["buffDurationSeconds"]
	if cfgB["buffDurationSeconds"] > maxDuration {
		maxDuration = cfgB["buffDurationSeconds"]
	}
	const dt = 0.05
	totalTicks := int(maxDuration/dt) + 5
	for i := 0; i < totalTicks; i++ {
		s.Update(dt)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	a = s.unitsByID[allyID]
	if a == nil {
		t.Fatal("ally removed during decay window")
	}
	if a.PerkState.BattlePrayerRemaining != 0 || a.PerkState.BattlePrayerMultiplier != 0 {
		t.Errorf("BattlePrayer not cleared after decay: rem=%.3f mult=%.3f", a.PerkState.BattlePrayerRemaining, a.PerkState.BattlePrayerMultiplier)
	}
	if a.PerkState.BolsteringPrayerRemaining != 0 || a.PerkState.BolsteringPrayerArmor != 0 {
		t.Errorf("BolsteringPrayer not cleared after decay: rem=%.3f arm=%.3f", a.PerkState.BolsteringPrayerRemaining, a.PerkState.BolsteringPrayerArmor)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Determinism (task 7.5)
// ─────────────────────────────────────────────────────────────────────────────

// TestDeterminism_BolsteringPrayerBuffApplicationsAcrossReplays runs two
// identical seeded scenarios where a Cleric with bolstering_prayer heals the
// same ally, advancing the same number of ticks, and asserts every tick's
// BolsteringPrayerRemaining / BolsteringPrayerArmor pair matches between runs.
func TestDeterminism_BolsteringPrayerBuffApplicationsAcrossReplays(t *testing.T) {
	const seed = 1313
	bpDef := perkDefByID("bolstering_prayer")
	if bpDef == nil {
		t.Fatal("bolstering_prayer perk def not found")
	}

	type snap struct {
		remaining float64
		armor     float64
	}
	type runResult struct {
		snaps []snap
	}

	runScenario := func() runResult {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		defer s.mu.Unlock()

		cleric := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		cleric.Visible = true
		cleric.HP = cleric.MaxHP
		cleric.AttackRange = 1000
		cleric.MaxMana = 200
		cleric.CurrentMana = 200
		grantPerk(cleric, "bolstering_prayer")

		ally := spawnClericTestAlly(t, s, 450, 400)
		ally.HP = ally.MaxHP / 2
		allyID := ally.ID

		healAbilityDef, _ := getAbilityDef("heal")
		s.onPerkAbilityResolvedLocked(cleric, healAbilityDef, ally)

		// Snapshot 5 ticks worth of decay.
		const dt = 0.05
		const ticks = 5
		out := runResult{snaps: make([]snap, 0, ticks+1)}
		// snapshot 0 (post-cast, pre-decay)
		out.snaps = append(out.snaps, snap{
			remaining: ally.PerkState.BolsteringPrayerRemaining,
			armor:     ally.PerkState.BolsteringPrayerArmor,
		})
		s.mu.Unlock()
		for i := 0; i < ticks; i++ {
			s.Update(dt)
			s.mu.RLock()
			a := s.unitsByID[allyID]
			out.snaps = append(out.snaps, snap{
				remaining: a.PerkState.BolsteringPrayerRemaining,
				armor:     a.PerkState.BolsteringPrayerArmor,
			})
			s.mu.RUnlock()
		}
		s.mu.Lock()
		return out
	}

	r1 := runScenario()
	r2 := runScenario()

	if len(r1.snaps) != len(r2.snaps) {
		t.Fatalf("snap count differs: %d vs %d", len(r1.snaps), len(r2.snaps))
	}
	for i := range r1.snaps {
		if r1.snaps[i] != r2.snaps[i] {
			t.Errorf("snap[%d] differs: run1=%+v run2=%+v", i, r1.snaps[i], r2.snaps[i])
		}
	}
}
