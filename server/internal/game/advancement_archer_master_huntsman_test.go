package game

// Tests for the Archer "Master Huntsman" capstone advancement (node 8). Three
// bundled effects:
//   1. unitBonusArrows  → +N arrows per attack, fanned out through the
//      split-shot pipeline; stacks with the split_shot perk's own extras.
//   2. unitTrapEffectMul → ×(1+percent/100) to every trap's EffectMultiplier.
//   3. unitTrapRadiusMul → ×(1+percent/100) to every trap's RadiusMultiplier.
//
// All expected values are derived from the catalog node (GetAdvancementDef) and
// the perk catalog, so retuning advancements.json / silver.json rebalances
// behavior without breaking these tests.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

const masterHuntsmanID = "archer_master_huntsman"

// nodeEffectAmount returns the Amount of the first effect of `kind` on node `id`.
func nodeEffectAmount(t *testing.T, id, kind string) int {
	t.Helper()
	node, ok := GetAdvancementDef(id)
	if !ok {
		t.Fatalf("advancement %q not in catalog", id)
	}
	for _, e := range node.Effects {
		if e.Kind == kind {
			return e.Amount
		}
	}
	t.Fatalf("node %q has no %q effect", id, kind)
	return 0
}

// nodeEffectPercent returns the Percent of the first effect of `kind` on node `id`.
func nodeEffectPercent(t *testing.T, id, kind string) float64 {
	t.Helper()
	node, ok := GetAdvancementDef(id)
	if !ok {
		t.Fatalf("advancement %q not in catalog", id)
	}
	for _, e := range node.Effects {
		if e.Kind == kind {
			return e.Percent
		}
	}
	t.Fatalf("node %q has no %q effect", id, kind)
	return 0
}

// spawnHuntsmanArcher spawns a player-owned archer for a player who owns the
// Master Huntsman advancement, plus a passive enemy archer in range. The
// attacker is wired for a one-sided fire test (mirrors newMarksmanState).
func spawnHuntsmanArcher(t *testing.T, seed int64) (s *GameState, attacker, target *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, []string{masterHuntsmanID}, nil)
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
	attacker.AttackRange = 300
	attacker.BaseAttackRange = 300
	return s, attacker, target
}

// ─── Effect plumbing: advancement → effective def → spawned unit ──────────────

func TestMasterHuntsman_FieldsSeededOnSpawnedArcher(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	wantArrows := nodeEffectAmount(t, masterHuntsmanID, "unitBonusArrows")
	wantTrapEffect := nodeEffectPercent(t, masterHuntsmanID, "unitTrapEffectMul") / 100
	wantTrapRadius := nodeEffectPercent(t, masterHuntsmanID, "unitTrapRadiusMul") / 100

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, []string{masterHuntsmanID}, nil)
	s.mu.Lock()
	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	s.mu.Unlock()
	if archer == nil {
		t.Fatal("archer spawn failed")
	}

	if archer.BonusArrows != wantArrows {
		t.Errorf("BonusArrows = %d, want %d (catalog)", archer.BonusArrows, wantArrows)
	}
	if math.Abs(archer.TrapEffectBonus-wantTrapEffect) > 1e-9 {
		t.Errorf("TrapEffectBonus = %v, want %v (catalog)", archer.TrapEffectBonus, wantTrapEffect)
	}
	if math.Abs(archer.TrapRadiusBonus-wantTrapRadius) > 1e-9 {
		t.Errorf("TrapRadiusBonus = %v, want %v (catalog)", archer.TrapRadiusBonus, wantTrapRadius)
	}
}

func TestMasterHuntsman_NoAdvancement_FieldsZero(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)
	s.mu.Lock()
	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	s.mu.Unlock()
	if archer == nil {
		t.Fatal("archer spawn failed")
	}
	if archer.BonusArrows != 0 || archer.TrapEffectBonus != 0 || archer.TrapRadiusBonus != 0 {
		t.Errorf("baseline archer: want all bonus fields 0, got arrows=%d effect=%v radius=%v",
			archer.BonusArrows, archer.TrapEffectBonus, archer.TrapRadiusBonus)
	}
}

// ─── Bonus arrow firing ───────────────────────────────────────────────────────

// spawnExtraEnemiesNear plants n distinct passive enemy archers near pos so the
// split-shot fan-out has distinct in-range targets for each extra arrow.
func spawnExtraEnemiesNear(t *testing.T, s *GameState, n int, x, y float64) {
	t.Helper()
	for i := 0; i < n; i++ {
		dx := float64((i%3)*30 - 30)
		dy := float64((i/3)*30 - 30)
		u := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: x + dx, Y: y + dy})
		if u == nil {
			t.Fatalf("extra enemy spawn %d failed", i)
		}
	}
}

func countAttackerProjectiles(s *GameState, attackerID int) int {
	got := 0
	for _, p := range s.Projectiles {
		if p.OwnerUnitID == attackerID {
			got++
		}
	}
	return got
}

// TestMasterHuntsman_PerklessArcher_FiresBonusArrow verifies a perkless archer
// (no Marksman perks at all) fires 1 + BonusArrows projectiles — exercising the
// relaxed perk-list early-out in onMarksmanProjectileFiredLocked.
func TestMasterHuntsman_PerklessArcher_FiresBonusArrow(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	bonus := nodeEffectAmount(t, masterHuntsmanID, "unitBonusArrows")

	s, attacker, primary := spawnHuntsmanArcher(t, 21)
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(attacker.PerkIDs) != 0 {
		t.Fatalf("precondition: attacker should be perkless, has %v", attacker.PerkIDs)
	}
	spawnExtraEnemiesNear(t, s, bonus, 600, 420)
	forceNoCrit(s)
	s.fireProjectileLocked(attacker, primary, 50)

	want := 1 + bonus
	if got := countAttackerProjectiles(s, attacker.ID); got != want {
		t.Errorf("perkless Huntsman archer fired %d projectiles, want %d (primary + %d bonus)", got, want, bonus)
	}
}

// TestMasterHuntsman_StacksWithSplitShot verifies that the bonus arrow stacks on
// top of split_shot's own extras: 1 + (splitExtras + bonusArrows) projectiles.
func TestMasterHuntsman_StacksWithSplitShot(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	bonus := nodeEffectAmount(t, masterHuntsmanID, "unitBonusArrows")
	splitExtras := int(perkDefByID("split_shot").Config["extraShots"])

	s, attacker, primary := spawnHuntsmanArcher(t, 22)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "split_shot")
	spawnExtraEnemiesNear(t, s, splitExtras+bonus, 600, 420)
	forceNoCrit(s)
	s.fireProjectileLocked(attacker, primary, 50)

	want := 1 + splitExtras + bonus
	if got := countAttackerProjectiles(s, attacker.ID); got != want {
		t.Errorf("Huntsman + split_shot fired %d projectiles, want %d (primary + %d split + %d bonus)",
			got, want, splitExtras, bonus)
	}
}

// TestMasterHuntsman_BaselineArcher_NoBonusArrow is the regression guard: an
// archer WITHOUT the advancement and without split_shot fires exactly 1 arrow.
func TestMasterHuntsman_BaselineArcher_NoBonusArrow(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 23)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	attacker := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	primary := s.spawnPlayerUnitLocked("archer", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 600, Y: 400})
	if attacker == nil || primary == nil {
		t.Fatal("spawn failed")
	}
	attacker.AttackRange = 300
	forceNoCrit(s)
	s.fireProjectileLocked(attacker, primary, 50)

	if got := countAttackerProjectiles(s, attacker.ID); got != 1 {
		t.Errorf("baseline archer fired %d projectiles, want 1", got)
	}
}

// ─── Trap effect / radius doubling ────────────────────────────────────────────

// TestMasterHuntsman_TrapEffectAndRadiusScaled verifies a Huntsman archer's
// planted-trap stats are scaled by (1+percent/100) for both damage and radius,
// derived from the caltrops perk's authored config and the advancement node.
func TestMasterHuntsman_TrapEffectAndRadiusScaled(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	caltrops := perkDefByID("caltrops")
	if caltrops == nil {
		t.Skip("caltrops perk not in catalog")
	}
	wantEffectMult := 1 + nodeEffectPercent(t, masterHuntsmanID, "unitTrapEffectMul")/100
	wantRadiusMult := 1 + nodeEffectPercent(t, masterHuntsmanID, "unitTrapRadiusMul")/100

	s, attacker, _ := spawnHuntsmanArcher(t, 24)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "caltrops")
	stats, ok := s.DebugEffectiveTrapStats(attacker)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false for caltrops owner")
	}

	cfg := caltrops.ConfigForRank(attacker.Rank)
	wantRadius := cfg["radius"] * wantRadiusMult
	wantDPS := cfg["damagePerSecond"] * wantEffectMult

	if math.Abs(stats.Radius-wantRadius) > 1e-6 {
		t.Errorf("trap Radius = %v, want %v (caltrops base %v × %v)", stats.Radius, wantRadius, cfg["radius"], wantRadiusMult)
	}
	if math.Abs(stats.DamagePerSecond-wantDPS) > 1e-6 {
		t.Errorf("trap DamagePerSecond = %v, want %v (caltrops base %v × %v)", stats.DamagePerSecond, wantDPS, cfg["damagePerSecond"], wantEffectMult)
	}
}

// TestMasterHuntsman_TrapBonusComposesWithWiderNets verifies the advancement's
// radius bonus composes multiplicatively with the wider_nets perk rather than
// overriding it.
func TestMasterHuntsman_TrapBonusComposesWithWiderNets(t *testing.T) {
	if _, ok := getUnitDef("archer"); !ok {
		t.Skip("archer not in unit catalog")
	}
	caltrops := perkDefByID("caltrops")
	widerNets := perkDefByID("wider_nets")
	if caltrops == nil || widerNets == nil {
		t.Skip("caltrops or wider_nets perk not in catalog")
	}
	wantRadiusMult := 1 + nodeEffectPercent(t, masterHuntsmanID, "unitTrapRadiusMul")/100
	widerNetsMult := widerNets.Config["radiusMultiplier"]

	s, attacker, _ := spawnHuntsmanArcher(t, 25)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(attacker, "caltrops")
	grantPerk(attacker, "wider_nets")
	stats, ok := s.DebugEffectiveTrapStats(attacker)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	cfg := caltrops.ConfigForRank(attacker.Rank)
	wantRadius := cfg["radius"] * widerNetsMult * wantRadiusMult
	if math.Abs(stats.Radius-wantRadius) > 1e-6 {
		t.Errorf("trap Radius = %v, want %v (base %v × widerNets %v × advancement %v)",
			stats.Radius, wantRadius, cfg["radius"], widerNetsMult, wantRadiusMult)
	}
}

// ─── Catalog validation for the three new effect kinds ────────────────────────

func TestAdvancementValidation_UnitBonusArrows_ZeroAmount_Panics(t *testing.T) {
	mustPanicLoader(t, "unitBonusArrows amount <= 0", func() {
		loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
			UnitType: "archer",
			Nodes: []UnitAdvancementNode{
				{ID: "x", Kind: "major", Cost: 10, Effects: []UnitAdvancementEffect{{Kind: "unitBonusArrows", Amount: 0}}},
			},
		})
	})
}

func TestAdvancementValidation_UnitTrapEffectMul_ZeroPercent_Panics(t *testing.T) {
	mustPanicLoader(t, "unitTrapEffectMul percent == 0", func() {
		loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
			UnitType: "archer",
			Nodes: []UnitAdvancementNode{
				{ID: "x", Kind: "major", Cost: 10, Effects: []UnitAdvancementEffect{{Kind: "unitTrapEffectMul", Percent: 0}}},
			},
		})
	})
}

func TestAdvancementValidation_UnitTrapRadiusMul_ZeroPercent_Panics(t *testing.T) {
	mustPanicLoader(t, "unitTrapRadiusMul percent == 0", func() {
		loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
			UnitType: "archer",
			Nodes: []UnitAdvancementNode{
				{ID: "x", Kind: "major", Cost: 10, Effects: []UnitAdvancementEffect{{Kind: "unitTrapRadiusMul", Percent: 0}}},
			},
		})
	})
}

// TestAdvancementValidation_MasterHuntsmanBundle_Valid verifies the shipped
// three-effect bundle loads without panicking.
func TestAdvancementValidation_MasterHuntsmanBundle_Valid(t *testing.T) {
	loadAdvancementDefsFromRaw("test.json", advancementTrackFile{
		UnitType: "archer",
		Nodes: []UnitAdvancementNode{
			{ID: "qa_huntsman", Kind: "major", Cost: 300, Effects: []UnitAdvancementEffect{
				{Kind: "unitBonusArrows", Amount: 1},
				{Kind: "unitTrapEffectMul", Percent: 100},
				{Kind: "unitTrapRadiusMul", Percent: 100},
			}},
		},
	})
}
