package game

import (
	"math"
	mrand "math/rand"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Marksman test scaffolding
//
// Mirrors the bronze_perks_test.go helper pattern but spawns archers (the
// only unit eligible for the Marksman path). Tests grant perks directly via
// grantPerk and force ProgressionPath / Rank so we don't have to win the
// rng coin-flip every test run.
// ─────────────────────────────────────────────────────────────────────────────

func newMarksmanState(t *testing.T, seed int64) (s *GameState, attacker, target *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker = s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if attacker == nil {
		t.Fatal("archer spawn failed")
	}
	target = s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 600, Y: 400})
	if target == nil {
		t.Fatal("enemy archer spawn failed")
	}

	attacker.ProgressionPath = unitPathMarksman
	attacker.Rank = unitRankBronze
	attacker.AttackTargetID = target.ID
	attacker.AttackCooldown = 0
	attacker.AttackRange = 300
	attacker.BaseAttackRange = 300
	target.AttackTargetID = 0 // target stays passive so combat is one-sided
	return s, attacker, target
}

// alwaysZeroSource is a math/rand.Source whose Int63() always returns 0,
// which makes Float64() always return 0. Wrapping it into rngPerks forces
// every Float64()-based roll (crit, proc chance) to succeed — used by
// forceCrit so multi-roll code paths (pierce, explosion AoE) crit on every
// victim, not just the first one.
type alwaysZeroSource struct{}

func (alwaysZeroSource) Int63() int64 { return 0 }
func (alwaysZeroSource) Seed(int64)   {}

// alwaysOneSource mirrors alwaysZeroSource for the upper bound: Int63()
// returns a value safely below math.MaxInt64 so Float64() yields ~0.998
// — well above any plausible crit chance, but NOT close enough to 1.0 to
// trigger math/rand's "round-to-1.0 retry" loop (which would hang the
// test). Every chance roll fails as a result.
type alwaysOneSource struct{}

func (alwaysOneSource) Int63() int64 {
	// math.MaxInt64 - 2^54 = 511 × 2^54 → exact in float64, so
	// float64(value) / float64(1<<63) = 511 / 512 = 0.998046875.
	return math.MaxInt64 - (int64(1) << 54)
}
func (alwaysOneSource) Seed(int64) {}

// forceCrit replaces rngPerks with a stream that returns 0 from Float64() on
// every call — so every crit roll lands. Lets multi-victim code paths
// (pierce, explosive_tips) reliably crit on every target without depending
// on a specific seed sequence.
func forceCrit(s *GameState) {
	s.rngPerks = mrand.New(alwaysZeroSource{})
}

// forceNoCrit replaces rngPerks with a stream that returns ~1.0 from
// Float64() on every call — so every crit roll fails.
func forceNoCrit(s *GameState) {
	s.rngPerks = mrand.New(alwaysOneSource{})
}

// ─────────────────────────────────────────────────────────────────────────────
// Bronze passives
// ─────────────────────────────────────────────────────────────────────────────

// pathBaseAttackRange returns the resolved base AttackRange for a
// (path, rank) cell — taking the flat override when present, else the
// catalog base × the path multiplier. Lets range-perk tests stay correct
// when path tuning shifts in JSON.
func pathBaseAttackRange(unit *Unit, path, rank string) float64 {
	pathDef := pathModifierFor(path, rank)
	if pathDef.AttackRange > 0 {
		return pathDef.AttackRange
	}
	mult := pathDef.AttackRangeMultiplier
	if mult <= 0 {
		mult = 1.0
	}
	return unit.BaseAttackRange * mult
}

func TestMarksman_EagleSpirit_BoostsRangeAndCrit(t *testing.T) {
	s, attacker, _ := newMarksmanState(t, 1)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "eagle_spirit")
	s.applyRankModifiersLocked(attacker, false)

	eagleCfg := perkDefByID("eagle_spirit").Config

	// Range = path-resolved base × (1 + eagle_spirit attackRangeBonus).
	wantRange := pathBaseAttackRange(attacker, unitPathMarksman, unitRankBronze) * (1.0 + eagleCfg["attackRangeBonus"])
	if math.Abs(attacker.AttackRange-wantRange) > 0.5 {
		t.Errorf("AttackRange = %.1f, want %.1f", attacker.AttackRange, wantRange)
	}
	// Crit chance = baseline + eagle_spirit critChanceBonus.
	wantCrit := defaultCritChance + eagleCfg["critChanceBonus"]
	if got := s.unitCritChanceLocked(attacker, nil); math.Abs(got-wantCrit) > 1e-6 {
		t.Errorf("CritChance = %.3f, want %.3f", got, wantCrit)
	}
}

func TestMarksman_HawkSpirit_BoostsAttackSpeedAndDamage(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 2)
	s.mu.Lock()
	defer s.mu.Unlock()

	baseAS := attacker.AttackSpeed
	grantPerk(attacker, "hawk_spirit")

	hawkCfg := perkDefByID("hawk_spirit").Config

	// Attack-speed bonus shows in perkAttackSpeedBonusLocked.
	if got, want := s.perkAttackSpeedBonusLocked(attacker), hawkCfg["attackSpeedBonus"]; math.Abs(got-want) > 1e-6 {
		t.Errorf("AS bonus = %.3f, want %.3f", got, want)
	}
	// Damage multiplier folds in via perkBonusDamageMultiplierLocked.
	if got, want := s.perkBonusDamageMultiplierLocked(attacker, target), hawkCfg["damageMultiplier"]; math.Abs(got-want) > 1e-6 {
		t.Errorf("damage bonus = %.3f, want %.3f", got, want)
	}
	if attacker.AttackSpeed != baseAS {
		t.Errorf("attack speed mutated unexpectedly: %.3f vs base %.3f", attacker.AttackSpeed, baseAS)
	}
}

func TestMarksman_VultureSpirit_BoostsCritAndDamage(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 3)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "vulture_spirit")

	vultureCfg := perkDefByID("vulture_spirit").Config

	wantCrit := defaultCritChance + vultureCfg["critChanceBonus"]
	if got := s.unitCritChanceLocked(attacker, nil); math.Abs(got-wantCrit) > 1e-6 {
		t.Errorf("CritChance = %.3f, want %.3f", got, wantCrit)
	}
	if got, want := s.perkBonusDamageMultiplierLocked(attacker, target), vultureCfg["damageMultiplier"]; math.Abs(got-want) > 1e-6 {
		t.Errorf("damage bonus = %.3f, want %.3f", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hunter's Mark stacking + crit math
// ─────────────────────────────────────────────────────────────────────────────

func TestHuntersMark_DiminishingReturnsAcrossSources(t *testing.T) {
	s, _, target := newMarksmanState(t, 4)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("hunters_mark")
	if def == nil {
		t.Fatal("hunters_mark def not loaded")
	}
	base := def.Config["critChanceBonus"]
	additional := def.Config["additionalStackBonus"]

	// 0 stacks → 0.
	if got := s.huntersMarkCritBonusLocked(target); got != 0 {
		t.Errorf("0 stacks: bonus = %.3f, want 0", got)
	}
	// 1 stack → base.
	target.PerkState.applyHuntersMarkStack("hunter-unit-1", 1, def.Config["durationSeconds"], maxHuntersMarkStacks)
	if got := s.huntersMarkCritBonusLocked(target); math.Abs(got-base) > 1e-6 {
		t.Errorf("1 stack: bonus = %.4f, want %.4f", got, base)
	}
	// 2 stacks (different source) → base + additional.
	target.PerkState.applyHuntersMarkStack("hunter-unit-2", 2, def.Config["durationSeconds"], maxHuntersMarkStacks)
	if got, want := s.huntersMarkCritBonusLocked(target), base+additional; math.Abs(got-want) > 1e-6 {
		t.Errorf("2 stacks: bonus = %.4f, want %.4f", got, want)
	}
	// Same-source re-application refreshes, does NOT add a stack.
	target.PerkState.applyHuntersMarkStack("hunter-unit-1", 1, def.Config["durationSeconds"], maxHuntersMarkStacks)
	if n := target.PerkState.huntersMarkCount(); n != 2 {
		t.Errorf("after refresh: stack count = %d, want 2", n)
	}
	// Third source lands.
	target.PerkState.applyHuntersMarkStack("hunter-unit-3", 3, def.Config["durationSeconds"], maxHuntersMarkStacks)
	if got, want := s.huntersMarkCritBonusLocked(target), base+2*additional; math.Abs(got-want) > 1e-6 {
		t.Errorf("3 stacks: bonus = %.4f, want %.4f", got, want)
	}
	// Cap holds — 4th source dropped.
	if landed := target.PerkState.applyHuntersMarkStack("hunter-unit-4", 4, def.Config["durationSeconds"], maxHuntersMarkStacks); landed {
		t.Errorf("4th distinct source landed; cap should reject it")
	}
}

func TestHuntersMark_DecayDropsExpiredStacks(t *testing.T) {
	s, _, target := newMarksmanState(t, 5)
	s.mu.Lock()
	defer s.mu.Unlock()

	target.PerkState.applyHuntersMarkStack("hunter-unit-1", 1, 0.30, maxHuntersMarkStacks)
	target.PerkState.applyHuntersMarkStack("hunter-unit-2", 2, 1.00, maxHuntersMarkStacks)
	if target.PerkState.huntersMarkCount() != 2 {
		t.Fatalf("expected 2 stacks, got %d", target.PerkState.huntersMarkCount())
	}

	// Decay 0.40s — first stack should be gone.
	target.PerkState.decayHuntersMarkStacks(0.40)
	if got := target.PerkState.huntersMarkCount(); got != 1 {
		t.Errorf("after 0.40s: stacks = %d, want 1", got)
	}
	// Decay another 0.70s — second stack expires too.
	target.PerkState.decayHuntersMarkStacks(0.70)
	if got := target.PerkState.huntersMarkCount(); got != 0 {
		t.Errorf("after total 1.10s: stacks = %d, want 0", got)
	}
}

func TestHuntersMark_StampedAtHitTime(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 6)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "hunters_mark")
	// Hunter's Mark is now applied at hit time (after the arrow lands), not
	// at fire time — so the test exercises the on-hit damage hook directly.
	s.onPerkAttackDamageAppliedLocked(attacker, target, 5)

	if got := target.PerkState.huntersMarkCount(); got != 1 {
		t.Errorf("expected 1 Hunter's Mark stack on target after on-hit, got %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Crit
// ─────────────────────────────────────────────────────────────────────────────

func TestCrit_DefaultMultiplier_Is2x(t *testing.T) {
	s, attacker, _ := newMarksmanState(t, 7)
	s.mu.Lock()
	defer s.mu.Unlock()

	if got := s.unitCritMultiplierLocked(attacker); got != defaultCritMultiplier {
		t.Errorf("default crit mult = %.2f, want %.2f", got, defaultCritMultiplier)
	}
}

func TestCrit_BullseyeOverridesMultiplier(t *testing.T) {
	s, attacker, _ := newMarksmanState(t, 8)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "bullseye")
	wantMult := perkDefByID("bullseye").Config["critMultiplier"]
	if got := s.unitCritMultiplierLocked(attacker); math.Abs(got-wantMult) > 1e-6 {
		t.Errorf("bullseye crit mult = %.2f, want %.2f", got, wantMult)
	}
}

func TestCrit_RollClampedAndDeterministic(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 9)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "vulture_spirit") // +10% crit
	forceCrit(s)
	if got := s.rollCritDamage(attacker, target); got != defaultCritMultiplier {
		t.Errorf("forced crit roll: got %.2f, want %.2f", got, defaultCritMultiplier)
	}
	forceNoCrit(s)
	if got := s.rollCritDamage(attacker, target); got != 1.0 {
		t.Errorf("forced no-crit roll: got %.2f, want 1.00", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Bullseye
// ─────────────────────────────────────────────────────────────────────────────

func TestMarksman_Bullseye_DoublesRangeAndBoostsCrit(t *testing.T) {
	s, attacker, _ := newMarksmanState(t, 10)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "bullseye")
	s.applyRankModifiersLocked(attacker, false)

	bullseyeCfg := perkDefByID("bullseye").Config

	// path-resolved base × (1 + bullseye attackRangeBonus)
	wantRange := pathBaseAttackRange(attacker, unitPathMarksman, unitRankBronze) * (1.0 + bullseyeCfg["attackRangeBonus"])
	if math.Abs(attacker.AttackRange-wantRange) > 0.5 {
		t.Errorf("Bullseye range = %.1f, want %.1f", attacker.AttackRange, wantRange)
	}
	wantCrit := defaultCritChance + bullseyeCfg["critChanceBonus"]
	if got := s.unitCritChanceLocked(attacker, nil); math.Abs(got-wantCrit) > 1e-6 {
		t.Errorf("Bullseye crit chance = %.3f, want %.3f", got, wantCrit)
	}
	wantMult := bullseyeCfg["critMultiplier"]
	if got := s.unitCritMultiplierLocked(attacker); math.Abs(got-wantMult) > 1e-6 {
		t.Errorf("Bullseye crit mult = %.2f, want %.2f", got, wantMult)
	}
}

func TestMarksman_BullseyeStacksWithEagleSpiritOnRange(t *testing.T) {
	s, attacker, _ := newMarksmanState(t, 11)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "eagle_spirit")
	grantPerk(attacker, "bullseye")
	s.applyRankModifiersLocked(attacker, false)

	eagleBonus := perkDefByID("eagle_spirit").Config["attackRangeBonus"]
	bullseyeBonus := perkDefByID("bullseye").Config["attackRangeBonus"]
	want := pathBaseAttackRange(attacker, unitPathMarksman, unitRankBronze) * (1.0 + eagleBonus + bullseyeBonus)
	if math.Abs(attacker.AttackRange-want) > 0.5 {
		t.Errorf("range = %.1f, want %.1f", attacker.AttackRange, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Split Shot
// ─────────────────────────────────────────────────────────────────────────────

func TestMarksman_SplitShot_FiresExtraProjectiles(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 12)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Two extra hostile archers near the primary target so split shot has
	// something to splash to.
	extra1 := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 580, Y: 420})
	extra2 := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 560, Y: 380})
	if extra1 == nil || extra2 == nil {
		t.Fatal("extra archer spawn failed")
	}
	grantPerk(attacker, "split_shot")
	forceNoCrit(s)
	// Fire-time path: fireProjectileLocked spawns the primary AND triggers
	// onMarksmanProjectileFiredLocked which fires the split arrows. Three
	// projectiles total: primary + 2 extras.
	s.fireProjectileLocked(attacker, primary, 5)

	got := 0
	for _, p := range s.Projectiles {
		if p.OwnerUnitID == attacker.ID {
			got++
		}
	}
	if got != 3 {
		t.Errorf("total split-shot projectiles = %d, want 3 (primary + 2 extras)", got)
	}
}

func TestMarksman_SplitShot_FallsBackToPrimaryWhenNoExtras(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 13)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "split_shot")
	forceNoCrit(s)
	s.fireProjectileLocked(attacker, primary, 5)

	// All projectiles end up aimed at primary: 1 primary + 2 fallback = 3.
	primaryCount := 0
	for _, p := range s.Projectiles {
		if p.OwnerUnitID == attacker.ID && p.TargetUnitID == primary.ID {
			primaryCount++
		}
	}
	if primaryCount != 3 {
		t.Errorf("fallback projectiles at primary = %d, want 3 (primary + 2 fallback)", primaryCount)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pierce
// ─────────────────────────────────────────────────────────────────────────────

func TestMarksman_Pierce_HitsLineEnemies(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 14)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "pierce")
	forceNoCrit(s)

	// Second hostile directly behind primary along the line of fire from
	// (400,400) → (600,400). At (680,400) — within attack range (300px from
	// attacker, since primary is at +200 and behind is at +280).
	behind := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 680, Y: 400})
	if behind == nil {
		t.Fatal("behind archer spawn failed")
	}
	primaryHpBefore := primary.HP
	behindHpBefore := behind.HP

	// Fire a pierce arrow. fireProjectileLocked routes it through
	// firePierceProjectileLocked because the attacker owns pierce.
	s.fireProjectileLocked(attacker, primary, 20)
	if len(s.Projectiles) == 0 {
		t.Fatal("no projectile spawned")
	}
	if !s.Projectiles[0].Pierce {
		t.Fatal("projectile is not flagged Pierce")
	}

	// Tick the projectile through its full flight. defaultProjectileSpeed is
	// 500 px/s, attack range is 300 px, so flight time is 0.6s. Use a coarse
	// dt to traverse in a few steps.
	for i := 0; i < 20 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}

	if primary.HP >= primaryHpBefore {
		t.Errorf("pierce did not damage primary: hp %d → %d", primaryHpBefore, primary.HP)
	}
	if behind.HP >= behindHpBefore {
		t.Errorf("pierce did not damage behind target: hp %d → %d", behindHpBefore, behind.HP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Double Shot
// ─────────────────────────────────────────────────────────────────────────────

func TestMarksman_DoubleShot_ArmsTimerOnAttack(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 15)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "double_shot")
	// Force the proc roll to land — Double Shot is now an RNG proc per
	// attack. forceCrit replaces rngPerks with an always-zero stream so
	// every Float64() < procChance.
	forceCrit(s)
	// Fire-time arming — fireProjectileLocked dispatches through
	// onMarksmanProjectileFiredLocked which sets the pending fields.
	s.fireProjectileLocked(attacker, target, 5)

	if attacker.PerkState.DoubleShotPendingSeconds <= 0 {
		t.Errorf("DoubleShotPendingSeconds = %.3f, want > 0", attacker.PerkState.DoubleShotPendingSeconds)
	}
	if attacker.PerkState.DoubleShotPendingTargetID != target.ID {
		t.Errorf("DoubleShotPendingTargetID = %d, want %d", attacker.PerkState.DoubleShotPendingTargetID, target.ID)
	}
}

// TestMarksman_DoubleShot_DoesNotArmWhenProcFails verifies that the second
// shot is GATED by the proc chance. With the rng biased to fail every roll,
// the deferred-fire fields stay zero and no second arrow ever spawns.
func TestMarksman_DoubleShot_DoesNotArmWhenProcFails(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 215)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "double_shot")
	// Force the proc roll to fail every call.
	forceNoCrit(s)
	s.fireProjectileLocked(attacker, target, 5)

	if attacker.PerkState.DoubleShotPendingSeconds != 0 {
		t.Errorf("DoubleShotPendingSeconds = %.3f, want 0 (proc failed)", attacker.PerkState.DoubleShotPendingSeconds)
	}
	if attacker.PerkState.DoubleShotPendingTargetID != 0 {
		t.Errorf("DoubleShotPendingTargetID = %d, want 0 (proc failed)", attacker.PerkState.DoubleShotPendingTargetID)
	}
}

func TestMarksman_DoubleShot_FiresAfterDelay(t *testing.T) {
	s, attacker, target := newMarksmanState(t, 16)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "double_shot")
	// forceCrit makes every Float64 roll succeed so the proc chance lands
	// AND any per-shot crit roll lands. Both rolls share the rngPerks stream.
	forceCrit(s)
	s.fireProjectileLocked(attacker, target, 5)

	projsBefore := len(s.Projectiles)

	// Tick the perk-state past the delay.
	s.tickUnitPerkStateLocked(attacker, 1.0)

	if len(s.Projectiles) <= projsBefore {
		t.Errorf("expected a second projectile after delay; before=%d after=%d", projsBefore, len(s.Projectiles))
	}
	if attacker.PerkState.DoubleShotPendingTargetID != 0 {
		t.Errorf("pending target id should clear after firing; got %d", attacker.PerkState.DoubleShotPendingTargetID)
	}
	// The deferred shot's primary projectile should be flagged DoubleShotSecond
	// for the client's combined-damage rendering.
	foundSecond := false
	for _, p := range s.Projectiles {
		if p.DoubleShotSecond {
			foundSecond = true
			break
		}
	}
	if !foundSecond {
		t.Errorf("expected at least one projectile flagged DoubleShotSecond")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Explosive Tips
// ─────────────────────────────────────────────────────────────────────────────

func TestMarksman_ExplosiveTips_DamagesNearbyEnemies(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 17)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "explosive_tips")

	// One nearby enemy inside the explosion radius (default 70). Place 40px
	// from primary so it falls inside.
	nearby := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 640, Y: 400})
	if nearby == nil {
		t.Fatal("nearby archer spawn failed")
	}
	nearbyHpBefore := nearby.HP

	s.onPerkAttackDamageAppliedLocked(attacker, primary, 5)

	if nearby.HP >= nearbyHpBefore {
		t.Errorf("explosive_tips did not damage nearby enemy: hp %d → %d", nearbyHpBefore, nearby.HP)
	}
	// Without hunters_mark owned, the explosion deals damage but does NOT
	// apply a Hunter's Mark stack — that synergy is gated on owning both
	// perks. See TestMarksman_ExplosiveTips_AppliesHuntersMarkOnlyWhenOwned.
	if nearby.PerkState.huntersMarkCount() != 0 {
		t.Errorf("explosive_tips applied Hunter's Mark without hunters_mark perk owned: stacks = %d, want 0", nearby.PerkState.huntersMarkCount())
	}
}

// TestMarksman_Pierce_RollsCritPerVictim pins the per-victim crit roll for
// pierce: every enemy along the line rolls independently, so a Marksman with
// 99% crit chance lands red-circle visuals on each victim. Uses forceCrit to
// bias the rng-perks stream so every roll succeeds.
func TestMarksman_Pierce_RollsCritPerVictim(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 200)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "pierce")
	// Place secondary directly behind primary along the fire line so both
	// land in the corridor.
	behind := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 680, Y: 400})
	if behind == nil {
		t.Fatal("behind archer spawn failed")
	}

	forceCrit(s)
	s.fireProjectileLocked(attacker, primary, 20)
	if len(s.Projectiles) == 0 || !s.Projectiles[0].Pierce {
		t.Fatal("pierce projectile not spawned")
	}
	for i := 0; i < 20 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}

	// With forceCrit biasing every roll into a crit, both the primary and
	// the secondary should generate crit events.
	primaryCrit := false
	secondaryCrit := false
	for _, e := range s.critEventsThisTick {
		if e.UnitID == primary.ID {
			primaryCrit = true
		}
		if e.UnitID == behind.ID {
			secondaryCrit = true
		}
	}
	if !primaryCrit {
		t.Errorf("expected pierce primary to record a crit event with forced rolls")
	}
	if !secondaryCrit {
		t.Errorf("expected pierce secondary to record a crit event with forced rolls")
	}
}

// TestMarksman_Pierce_NoCritWhenChanceIsZero verifies that when the crit roll
// fails for every victim (forceNoCrit), no crit events are produced — pierce
// damage falls back to the base post-mult value with no compounded crit.
func TestMarksman_Pierce_NoCritWhenChanceIsZero(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 201)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "pierce")
	behind := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 680, Y: 400})
	if behind == nil {
		t.Fatal("behind archer spawn failed")
	}

	forceNoCrit(s)
	s.fireProjectileLocked(attacker, primary, 20)
	for i := 0; i < 20 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}

	for _, e := range s.critEventsThisTick {
		if e.UnitID == primary.ID || e.UnitID == behind.ID {
			t.Errorf("expected no crit events when rolls fail; got %+v", e)
		}
	}
}

// TestMarksman_ExplosiveTips_RollsCritPerVictim pins the per-victim crit roll
// for explosive_tips: every blast victim rolls independently so red-circle
// visuals can land on individual victims rather than all-or-nothing across
// the AoE.
func TestMarksman_ExplosiveTips_RollsCritPerVictim(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 202)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "explosive_tips")
	nearby := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 640, Y: 400})
	if nearby == nil {
		t.Fatal("nearby archer spawn failed")
	}

	forceCrit(s)
	s.onPerkAttackDamageAppliedLocked(attacker, primary, 5)

	gotNearby := false
	for _, e := range s.critEventsThisTick {
		if e.UnitID == nearby.ID {
			gotNearby = true
		}
	}
	if !gotNearby {
		t.Errorf("expected explosive_tips blast victim to record a crit event with forced rolls")
	}
}

// TestMarksman_ExplosiveTips_AppliesHuntersMarkOnlyWhenOwned pins the gated
// synergy: explosive_tips' explosion only marks blast victims when the same
// attacker also owns the silver hunters_mark perk. Without hunters_mark the
// explosion is pure splash damage; with it, blast victims pick up a stack
// keyed by huntersMarkExplosionSourceID so they accumulate alongside the
// arrow-mark stack from the same Marksman.
func TestMarksman_ExplosiveTips_AppliesHuntersMarkOnlyWhenOwned(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 117)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "explosive_tips")
	grantPerk(attacker, "hunters_mark")

	nearby := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 640, Y: 400})
	if nearby == nil {
		t.Fatal("nearby archer spawn failed")
	}

	s.onPerkAttackDamageAppliedLocked(attacker, primary, 5)

	if got := nearby.PerkState.huntersMarkCount(); got == 0 {
		t.Errorf("with hunters_mark + explosive_tips owned, blast victim should receive a Hunter's Mark stack; got 0")
	}
}

func TestMarksman_ExplosiveTips_DoesNotChain(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 18)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "explosive_tips")

	// Arm the recursion guard manually as if we were inside a previous
	// explosion call. The hook should bail out.
	attacker.PerkState.ExplosiveTipsActive = true
	startHP := primary.HP
	s.onPerkAttackDamageAppliedLocked(attacker, primary, 5)
	if primary.HP != startHP {
		t.Errorf("explosive_tips fired while guard active; primary HP %d → %d", startHP, primary.HP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pool / assignment
// ─────────────────────────────────────────────────────────────────────────────

// TestMarksman_PierceAndExplosiveTips_CombineExplosionsPerHit verifies that
// when a Marksman owns BOTH pierce and explosive_tips, every enemy the pierce
// arrow passes through triggers its own explosion. The arrow → pierce victim
// → explosion chain runs through resolveAttackHitLocked →
// onPerkAttackDamageAppliedLocked → onMarksmanDamageAppliedLocked →
// fireExplosiveTipsLocked, with the per-attacker recursion guard
// (ExplosiveTipsActive) suppressing only re-explosions from the AoE damage
// itself, NOT subsequent pierce hits in the same projectile tick.
func TestMarksman_PierceAndExplosiveTips_CombineExplosionsPerHit(t *testing.T) {
	s, attacker, primary := newMarksmanState(t, 50)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "pierce")
	grantPerk(attacker, "explosive_tips")
	forceNoCrit(s)

	// Two extra enemies in the corridor: one near the primary, one further
	// down the line. All three should be hit by pierce; each should spawn
	// its own "explosion" effect anchored to that victim.
	behind1 := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 640, Y: 400})
	behind2 := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 680, Y: 400})
	if behind1 == nil || behind2 == nil {
		t.Fatal("behind archer spawn failed")
	}

	startEffects := len(s.activeEffects)
	s.fireProjectileLocked(attacker, primary, 20)
	if len(s.Projectiles) == 0 || !s.Projectiles[0].Pierce {
		t.Fatal("pierce projectile not spawned")
	}

	// Tick the projectile through its full flight.
	for i := 0; i < 20 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}

	// Expect 3 "explosion" effects queued — one per pierce victim — each
	// anchored to a distinct victim unit ID.
	anchors := make(map[int]bool)
	for _, e := range s.activeEffects[startEffects:] {
		if e.Name != "explosion" {
			continue
		}
		anchors[e.AnchorUnitID] = true
	}
	if len(anchors) != 3 {
		t.Errorf("explosion effects anchored to %d distinct victims, want 3", len(anchors))
		for i, e := range s.activeEffects[startEffects:] {
			t.Logf("  effect[%d]: name=%s anchor=%d x=%.0f y=%.0f", i, e.Name, e.AnchorUnitID, e.FallbackX, e.FallbackY)
		}
	}
}

func TestMarksman_BronzePerkAssignedToMarksmanArcher(t *testing.T) {
	validBronze := map[string]bool{
		"eagle_spirit":   true,
		"hawk_spirit":    true,
		"vulture_spirit": true,
	}

	marksmanSeeds := 0
	for seed := int64(1); seed <= 30; seed++ {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if archer == nil {
			s.mu.Unlock()
			t.Skip("archer spawn unavailable")
		}
		s.addUnitXPLocked(archer, 100)
		if archer.ProgressionPath != unitPathMarksman {
			s.mu.Unlock()
			continue
		}
		marksmanSeeds++
		if len(archer.PerkIDs) != 1 {
			t.Errorf("seed %d: expected 1 perk, got %v", seed, archer.PerkIDs)
		} else if !validBronze[archer.PerkIDs[0]] {
			t.Errorf("seed %d: perk %q not in Bronze Marksman pool", seed, archer.PerkIDs[0])
		}
		s.mu.Unlock()
	}
	if marksmanSeeds == 0 {
		t.Fatalf("no seed in [1,30] selected the Marksman path; rng salt or path-assignment likely broken")
	}
}
