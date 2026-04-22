package game

import (
	"fmt"
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────────────────

// newGoldPerkState returns a minimal GameState with a single Vanguard owned by
// "p1". Useful for tests that only need the owner's perk; callers add allies
// and enemies as needed.
func newGoldPerkState(t *testing.T) (s *GameState, vanguard *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	vanguard = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	vanguard.MaxHP = 500
	vanguard.HP = 500
	return s, vanguard
}

// spawnAlly adds an alive, visible ally for playerID at the given position.
func spawnAlly(t *testing.T, s *GameState, playerID string, x, y float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", playerID, "#2ecc71", protocol.Vec2{X: x, Y: y})
	u.MaxHP = 500
	u.HP = 500
	u.Visible = true
	return u
}

// spawnEnemy adds an alive, visible enemy for playerID "enemy" at the given position.
func spawnEnemy(t *testing.T, s *GameState, x, y float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{X: x, Y: y})
	u.MaxHP = 500
	u.HP = 500
	u.Visible = true
	return u
}

// ─────────────────────────────────────────────────────────────────────────────
// guardian_aura — baseline
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_BoostsAllyArmor verifies that an allied unit within the
// Vanguard's aura radius receives flat and percent armor bonuses.
func TestGuardianAura_BoostsAllyArmor(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}

	ally := spawnAlly(t, s, "p1", vanguard.X+50, vanguard.Y)

	s.rebuildGuardianAuraCacheLocked()

	// Verify cache contains the ally with expected flat and percent bonuses.
	aura := s.guardianAuraCache[ally.ID]
	wantFlat := int(def.Config["bonusArmor"])
	wantPct := def.Config["armorPercent"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("ally aura FlatArmor: got %d, want %d", aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("ally aura PercentArmor: got %.3f, want %.3f", aura.PercentArmor, wantPct)
	}

	// Verify effectiveArmorLocked uses both bonuses.
	wantEffective := int(math.Floor(float64(ally.Armor)*(1.0+wantPct))) + wantFlat
	gotEffective := s.effectiveArmorLocked(ally)
	if gotEffective != wantEffective {
		t.Errorf("ally effectiveArmor with aura: got %d, want %d", gotEffective, wantEffective)
	}
}

// TestGuardianAura_DoesNotAffectOwner verifies the Vanguard owner is NOT
// included in the cache (owner does not benefit from their own aura).
func TestGuardianAura_DoesNotAffectOwner(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	s.rebuildGuardianAuraCacheLocked()

	if _, ok := s.guardianAuraCache[vanguard.ID]; ok {
		t.Error("owner should NOT be in guardianAuraCache")
	}
}

// TestGuardianAura_DoesNotAffectEnemies verifies that enemies in range are
// not added to the cache.
func TestGuardianAura_DoesNotAffectEnemies(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	enemy := spawnEnemy(t, s, vanguard.X+30, vanguard.Y)
	s.rebuildGuardianAuraCacheLocked()

	if _, ok := s.guardianAuraCache[enemy.ID]; ok {
		t.Error("enemy should NOT be in guardianAuraCache")
	}
}

// TestGuardianAura_OutsideRadius verifies that an ally beyond the effective
// radius receives no aura protection.
func TestGuardianAura_OutsideRadius(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	def := perkDefByID("guardian_aura")

	// Place ally beyond base radius (no synergy to boost effective radius).
	ally := spawnAlly(t, s, "p1", vanguard.X+def.Config["radius"]+50, vanguard.Y)
	s.rebuildGuardianAuraCacheLocked()

	if _, ok := s.guardianAuraCache[ally.ID]; ok {
		t.Error("ally outside radius should NOT be in guardianAuraCache")
	}
}

// TestGuardianAura_DeadVanguardDropsAura verifies that when the Vanguard dies,
// the next tick's cache rebuild excludes it and the ally's protection drops.
func TestGuardianAura_DeadVanguardDropsAura(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "guardian_aura")
	ally := spawnAlly(t, s, "p1", vanguard.X+30, vanguard.Y)
	s.rebuildGuardianAuraCacheLocked()

	if _, ok := s.guardianAuraCache[ally.ID]; !ok {
		t.Fatal("ally should be in cache before vanguard death")
	}

	// Kill the vanguard.
	vanguard.HP = 0
	s.rebuildGuardianAuraCacheLocked()

	if _, ok := s.guardianAuraCache[ally.ID]; ok {
		t.Error("ally should NOT be in cache after vanguard death")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// guardian_aura — synergy
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_TwoVanguardsFormation verifies the two-Vanguard formation:
// V1 and V2 same owner, distance 80 (< base 100). Each has companions=1,
// effR=130, effFlat=20, effPercent=0.25. An ally at distance 120 from V1
// (outside base 100, inside effective 130) receives the synergy bonuses.
func TestGuardianAura_TwoVanguardsFormation(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}

	grantPerk(v1, "guardian_aura")
	v2 := spawnAlly(t, s, "p1", v1.X+80, v1.Y) // distance 80 < base 100
	grantPerk(v2, "guardian_aura")

	// Ally is 120 from V1 — beyond base 100 but within effective 130.
	ally := spawnAlly(t, s, "p1", v1.X+120, v1.Y)

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally at distance 120 should be in cache due to synergy boost (effR=130)")
	}
	// companions=1: effFlat = 15 + 1*5 = 20, effPercent = 0.20 + 1*0.05 = 0.25
	wantFlat := int(def.Config["bonusArmor"]) + int(def.Config["synergyArmorBonus"])     // 20
	wantPct := def.Config["armorPercent"] + def.Config["synergyArmorPercentBonus"]       // 0.25
	if aura.FlatArmor != wantFlat {
		t.Errorf("synergy FlatArmor: got %d, want %d", aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("synergy PercentArmor: got %.3f, want %.3f", aura.PercentArmor, wantPct)
	}
}

// TestGuardianAura_OutOfRangeNoFormation verifies that V1 and V2 too far apart
// (distance 200 > base 150) each operate independently with base values.
func TestGuardianAura_OutOfRangeNoFormation(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")

	v2 := spawnAlly(t, s, "p1", v1.X+200, v1.Y) // distance 200 > base 150
	grantPerk(v2, "guardian_aura")

	// Ally close to V1, within base radius.
	ally := spawnAlly(t, s, "p1", v1.X+50, v1.Y)

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally within base radius of V1 should still be in cache")
	}
	// No companions — should be base values only.
	wantFlat := int(def.Config["bonusArmor"])
	wantPct := def.Config["armorPercent"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("no synergy: expected base FlatArmor %d, got %d", wantFlat, aura.FlatArmor)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("no synergy: expected base PercentArmor %.3f, got %.3f", wantPct, aura.PercentArmor)
	}
}

// TestGuardianAura_ThreeVanguardCluster verifies three Vanguards all within
// baseR of each other: each has companions=2, effR=160, effDR=0.25.
func TestGuardianAura_ThreeVanguardCluster(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")

	// Equilateral triangle side ~80 (all within base 100).
	v2 := spawnAlly(t, s, "p1", v1.X+80, v1.Y)
	grantPerk(v2, "guardian_aura")
	v3 := spawnAlly(t, s, "p1", v1.X+40, v1.Y+69) // height of equilateral with side 80
	grantPerk(v3, "guardian_aura")

	// Ally close to V1, within base radius.
	ally := spawnAlly(t, s, "p1", v1.X+30, v1.Y)

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally within base radius of V1 should be in cache")
	}
	// companions=2: effFlat = 15 + 2*5 = 25, effPercent = 0.20 + 2*0.05 = 0.30
	wantFlat := int(def.Config["bonusArmor"]) + 2*int(def.Config["synergyArmorBonus"])
	wantPct := def.Config["armorPercent"] + 2*def.Config["synergyArmorPercentBonus"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("3-vanguard cluster FlatArmor: got %d, want %d", aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("3-vanguard cluster PercentArmor: got %.3f, want %.3f", aura.PercentArmor, wantPct)
	}
}

// TestGuardianAura_PartialLineFormation places V1 at x=200, V2 at x=290, V3 at
// x=380 (spacing 90, base radius 150). V1 sees only V2 (companions=1), V2 sees
// both V1 and V3 (companions=2), V3 sees only V2 (companions=1).
//
// Configuration (base radius=150, effR at 1 companion=180, at 2 companions=210):
//   V1 at x=200, effR=180 (1 companion)
//   V2 at x=290, effR=210 (2 companions)
//   V3 at x=380, effR=180 (1 companion)
//
// To read V1's 1-companion aura in isolation we place allyFarLeft at x=30:
//   V1 dist=170 ≤ 180 ✓  V2 dist=260 > 210 ✓  V3 dist=350 > 180 ✓
// Symmetrically, allyFarRight at x=550 is covered only by V3:
//   V3 dist=170 ≤ 180 ✓  V2 dist=260 > 210 ✓  V1 dist=350 > 180 ✓
func TestGuardianAura_PartialLineFormation(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	// V1 at x=200, V2 at x=290 (dist 90 < base 100), V3 at x=380 (dist 90 from V2, 180 from V1).
	v1.X = 200
	v1.Y = 400
	grantPerk(v1, "guardian_aura")

	v2 := spawnAlly(t, s, "p1", 290, 400)
	grantPerk(v2, "guardian_aura")

	v3 := spawnAlly(t, s, "p1", 380, 400)
	grantPerk(v3, "guardian_aura")

	s.rebuildGuardianAuraCacheLocked()

	// Verify companion counts by checking a unit very close to each Vanguard.
	// companions=2 for V2 (sees V1 and V3), companions=1 for V1 and V3.
	// effFlat: 1 companion = 15+5=20, 2 companions = 15+10=25
	// effPercent: 1 companion = 0.20+0.05=0.25, 2 companions = 0.20+0.10=0.30
	flatV2 := int(def.Config["bonusArmor"]) + 2*int(def.Config["synergyArmorBonus"])
	pctV2 := def.Config["armorPercent"] + 2*def.Config["synergyArmorPercentBonus"]

	// An ally directly beside V2 should see V2's synergy values (2 companions).
	allyNearV2 := spawnAlly(t, s, "p1", 291, 400)
	s.rebuildGuardianAuraCacheLocked()
	aura, ok := s.guardianAuraCache[allyNearV2.ID]
	if !ok {
		t.Fatal("allyNearV2 should be in cache")
	}
	if aura.FlatArmor != flatV2 {
		t.Errorf("allyNearV2 FlatArmor: got %d, want %d (V2 has 2 companions)", aura.FlatArmor, flatV2)
	}
	if math.Abs(aura.PercentArmor-pctV2) > 0.001 {
		t.Errorf("allyNearV2 PercentArmor: got %.3f, want %.3f (V2 has 2 companions)", aura.PercentArmor, pctV2)
	}

	// V1 companion count: 1 (V2 at distance 90 within base 150). Not V3 (distance 180 > 150).
	// effR of V1 = 150 + 1*30 = 180. Ally at x=30 (distance 170 from V1, inside 180).
	// V2 at x=290 — distance 260 > V2's effR=210. V3 at x=380 — distance 350 > V3's effR=180.
	flatV1 := int(def.Config["bonusArmor"]) + 1*int(def.Config["synergyArmorBonus"])
	pctV1 := def.Config["armorPercent"] + 1*def.Config["synergyArmorPercentBonus"]
	allyFarLeft := spawnAlly(t, s, "p1", 30, 400)
	s.rebuildGuardianAuraCacheLocked()
	auraLeft, okLeft := s.guardianAuraCache[allyFarLeft.ID]
	if !okLeft {
		t.Fatal("allyFarLeft should be in cache (distance 170 from V1, inside effR 180)")
	}
	if auraLeft.FlatArmor != flatV1 {
		t.Errorf("allyFarLeft FlatArmor: got %d, want %d (V1 with 1 companion)", auraLeft.FlatArmor, flatV1)
	}
	if math.Abs(auraLeft.PercentArmor-pctV1) > 0.001 {
		t.Errorf("allyFarLeft PercentArmor: got %.3f, want %.3f (V1 with 1 companion)", auraLeft.PercentArmor, pctV1)
	}

	// Symmetrically verify V3 (1 companion) covers an ally to its right.
	// V3 at x=380, ally at x=550 (distance 170, inside effR 180).
	allyFarRight := spawnAlly(t, s, "p1", 550, 400)
	s.rebuildGuardianAuraCacheLocked()
	auraRight, okRight := s.guardianAuraCache[allyFarRight.ID]
	if !okRight {
		t.Fatal("allyFarRight should be in cache (distance 170 from V3, inside effR 180)")
	}
	if auraRight.FlatArmor != flatV1 { // same as V1 (1 companion)
		t.Errorf("allyFarRight FlatArmor: got %d, want %d (V3 with 1 companion)", auraRight.FlatArmor, flatV1)
	}
	if math.Abs(auraRight.PercentArmor-pctV1) > 0.001 {
		t.Errorf("allyFarRight PercentArmor: got %.3f, want %.3f (V3 with 1 companion)", auraRight.PercentArmor, pctV1)
	}
}

// TestGuardianAura_AsymmetricCoverage verifies the asymmetry rule:
// V2 is boosted (effR=130) and can cover ally A at distance 115, but A is a
// separate non-boosted Vanguard and does not count as V2's companion (115 > base 100).
func TestGuardianAura_AsymmetricCoverage(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")

	// V1 is close to V2 (distance 80) → V2 gets companion=1, effR=130.
	v1.X = 200
	v1.Y = 400
	grantPerk(v1, "guardian_aura")

	v2 := spawnAlly(t, s, "p1", 280, 400) // 80 from V1
	grantPerk(v2, "guardian_aura")

	// allyA is at distance 115 from V2 — outside base 100 but inside V2's effR 130.
	// allyA does NOT have guardian_aura, so it's not a source at all.
	allyA := spawnAlly(t, s, "p1", 395, 400)

	s.rebuildGuardianAuraCacheLocked()

	// allyA should be under V2's boosted aura (V2 has 1 companion).
	aura, ok := s.guardianAuraCache[allyA.ID]
	if !ok {
		t.Fatal("allyA at distance 115 from V2 should be in cache (V2 effR=130)")
	}
	wantFlat := int(def.Config["bonusArmor"]) + int(def.Config["synergyArmorBonus"])
	wantPct := def.Config["armorPercent"] + def.Config["synergyArmorPercentBonus"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("allyA FlatArmor: got %d, want %d (V2 with 1 companion)", aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("allyA PercentArmor: got %.3f, want %.3f (V2 with 1 companion)", aura.PercentArmor, wantPct)
	}

	// V2's companion count: only V1 is within V2's BASE radius (80 < 100).
	// allyA is NOT a guardian_aura source, so it doesn't count regardless.
	// Verify an ally near V2 sees the same 1-companion values (not inflated by allyA).
	allyNearV2 := spawnAlly(t, s, "p1", 281, 400)
	s.rebuildGuardianAuraCacheLocked()
	auraNearV2, ok2 := s.guardianAuraCache[allyNearV2.ID]
	if !ok2 {
		t.Fatal("allyNearV2 should be in cache")
	}
	if auraNearV2.FlatArmor != wantFlat {
		t.Errorf("allyNearV2 FlatArmor: got %d, want %d (not inflated by allyA)", auraNearV2.FlatArmor, wantFlat)
	}
}

// TestGuardianAura_EnemyVanguardsDoNotFormation verifies that two Vanguards owned
// by different players do not form a synergy even if in range.
func TestGuardianAura_EnemyVanguardsDoNotFormation(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")

	// Enemy Vanguard with the same perk, within base radius.
	v2 := s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{X: v1.X + 50, Y: v1.Y})
	v2.MaxHP = 500
	v2.HP = 500
	v2.Visible = true
	grantPerk(v2, "guardian_aura")

	// Ally close to V1.
	ally := spawnAlly(t, s, "p1", v1.X+30, v1.Y)

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally within base radius should still be in cache")
	}
	// V1 has 0 companions (V2 is enemy), so ally should see base values only.
	wantFlat := int(def.Config["bonusArmor"])
	wantPct := def.Config["armorPercent"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("enemy Vanguard should not boost synergy FlatArmor: got %d, want %d",
			aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("enemy Vanguard should not boost synergy PercentArmor: got %.3f, want %.3f",
			aura.PercentArmor, wantPct)
	}
}

// TestGuardianAura_MaxNotSum verifies that an ally inside two allied-Vanguard
// auras receives max(effDR), not the sum.
func TestGuardianAura_MaxNotSum(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")

	// V2 is close to V1 so V1 gets synergy; ally is close to both.
	v2 := spawnAlly(t, s, "p1", v1.X+80, v1.Y)
	grantPerk(v2, "guardian_aura")

	// Ally within both radii.
	ally := spawnAlly(t, s, "p1", v1.X+40, v1.Y)

	s.rebuildGuardianAuraCacheLocked()

	// Both V1 and V2 have companions=1. Max per dimension of two equal values = the value.
	// If the cache were summing, FlatArmor would be 40 (2×20) — which would be caught here.
	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally should be in cache")
	}
	// companions=1: effFlat = 15+5 = 20, effPercent = 0.20+0.05 = 0.25
	wantFlat := int(def.Config["bonusArmor"]) + int(def.Config["synergyArmorBonus"])
	wantPct := def.Config["armorPercent"] + def.Config["synergyArmorPercentBonus"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("max not sum FlatArmor: got %d, want max=%d (not sum=%d)",
			aura.FlatArmor, wantFlat, 2*wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("max not sum PercentArmor: got %.3f, want max=%.3f (not sum=%.3f)",
			aura.PercentArmor, wantPct, 2*wantPct)
	}
	// Explicit sum-guard.
	if aura.FlatArmor > wantFlat+1 {
		t.Errorf("cache is summing FlatArmor instead of taking max: got %d", aura.FlatArmor)
	}
}

// TestGuardianAura_StacksWithBrace verifies that stacking guardian_aura + brace
// each contribute to effectiveArmorLocked additively. Both bonuses add to the
// recipient's flat armor simultaneously.
func TestGuardianAura_StacksWithBrace(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	braceDef := perkDefByID("brace")
	if def == nil || braceDef == nil {
		t.Fatal("perk defs not found")
	}

	grantPerk(vanguard, "brace")

	// Place 2+ enemies within brace radius.
	threshold := int(braceDef.Config["enemyThreshold"])
	for i := 0; i < threshold; i++ {
		e := spawnEnemy(t, s, vanguard.X+braceDef.Config["radius"]*0.5, vanguard.Y+float64(i)*5)
		_ = e
	}

	// Inject a guardian_aura value directly for this unit (simulates being under an aura).
	s.guardianAuraCache[vanguard.ID] = guardianAuraValue{
		FlatArmor:    int(def.Config["bonusArmor"]),
		PercentArmor: def.Config["armorPercent"],
	}

	// Expected: base + brace flat bonus, plus percent of base from aura.
	wantFlat := int(braceDef.Config["bonusArmor"]) + int(def.Config["bonusArmor"])
	wantPercent := def.Config["armorPercent"]
	wantEffective := int(math.Floor(float64(vanguard.Armor)*(1.0+wantPercent))) + wantFlat

	got := s.effectiveArmorLocked(vanguard)
	if got != wantEffective {
		t.Errorf("stacked armor: got %d, want %d (base=%d, flat=%d, percent=%.2f)",
			got, wantEffective, vanguard.Armor, wantFlat, wantPercent)
	}
}

// TestGuardianAura_DeterminismReplay verifies that two GameState instances
// with the same seed produce identical guardianAuraCache at every rebuild call
// after identical unit placement. This exercises the determinism guarantee.
func TestGuardianAura_DeterminismReplay(t *testing.T) {
	setup := func() (*GameState, *Unit, *Unit) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 99)
		s.mu.Lock()
		defer s.mu.Unlock()

		v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
		grantPerk(v, "guardian_aura")
		v2 := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 360, Y: 300})
		grantPerk(v2, "guardian_aura")
		s.spawnPlayerUnitLocked("soldier", "p1", "#f1c40f", protocol.Vec2{X: 330, Y: 350})
		return s, v, v2
	}

	s1, _, _ := setup()
	s2, _, _ := setup()

	for tick := 0; tick < 5; tick++ {
		s1.mu.Lock()
		s1.rebuildGuardianAuraCacheLocked()
		cache1 := make(map[int]guardianAuraValue, len(s1.guardianAuraCache))
		for k, v := range s1.guardianAuraCache {
			cache1[k] = v
		}
		s1.mu.Unlock()

		s2.mu.Lock()
		s2.rebuildGuardianAuraCacheLocked()
		cache2 := make(map[int]guardianAuraValue, len(s2.guardianAuraCache))
		for k, v := range s2.guardianAuraCache {
			cache2[k] = v
		}
		s2.mu.Unlock()

		if len(cache1) != len(cache2) {
			t.Fatalf("tick %d: cache length mismatch: s1=%d s2=%d", tick, len(cache1), len(cache2))
		}
		for id, v1 := range cache1 {
			v2, ok := cache2[id]
			if !ok {
				t.Fatalf("tick %d: unitID %d in s1 cache but not s2", tick, id)
			}
			if v1.FlatArmor != v2.FlatArmor {
				t.Fatalf("tick %d: unitID %d FlatArmor mismatch: s1=%d s2=%d", tick, id, v1.FlatArmor, v2.FlatArmor)
			}
			if math.Abs(v1.PercentArmor-v2.PercentArmor) > 1e-12 {
				t.Fatalf("tick %d: unitID %d PercentArmor mismatch: s1=%.15f s2=%.15f", tick, id, v1.PercentArmor, v2.PercentArmor)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// pain_share
// ─────────────────────────────────────────────────────────────────────────────

// newPainShareState returns a state with a Vanguard ("p1") with pain_share and
// an ally also on "p1" within the configured radius.
func newPainShareState(t *testing.T) (s *GameState, vanguard, ally *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 77)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	if def == nil {
		t.Fatal("pain_share perk def not found")
	}

	vanguard = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	vanguard.MaxHP = 500
	vanguard.HP = 500
	grantPerk(vanguard, "pain_share")

	// Ally within redirect radius.
	ally = s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{
		X: 400 + def.Config["radius"]*0.5,
		Y: 400,
	})
	ally.MaxHP = 500
	ally.HP = 500
	ally.Visible = true
	return s, vanguard, ally
}

// TestPainShare_RedirectsDamage verifies the fundamental mechanic: an ally
// within radius takes less damage while the Vanguard absorbs the redirect.
func TestPainShare_RedirectsDamage(t *testing.T) {
	s, vanguard, ally := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	const rawDamage = 100
	redirectPct := def.Config["redirectPercent"]
	expectedRedirect := maxInt(1, int(math.Round(float64(rawDamage)*redirectPct)))

	vanguardHPBefore := vanguard.HP
	allyHPBefore := ally.HP

	s.applyUnitDamageLocked(ally, rawDamage)

	allyDamage := allyHPBefore - ally.HP
	vanguardDamage := vanguardHPBefore - vanguard.HP

	expectedAllyDamage := rawDamage - expectedRedirect

	if diff := allyDamage - expectedAllyDamage; diff > 1 || diff < -1 {
		t.Errorf("ally damage: got %d, want ~%d (redirect=%.0f%%)", allyDamage, expectedAllyDamage, redirectPct*100)
	}
	if vanguardDamage <= 0 {
		t.Error("Vanguard should have absorbed some damage from redirect")
	}
	if diff := vanguardDamage - expectedRedirect; diff > 1 || diff < -1 {
		t.Errorf("vanguard absorbed: got %d, want ~%d", vanguardDamage, expectedRedirect)
	}
}

// TestPainShare_OutsideRadiusNoRedirect verifies that an ally outside the radius
// takes full damage with no redirect.
func TestPainShare_OutsideRadiusNoRedirect(t *testing.T) {
	s, vanguard, _ := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")

	// Spawn a second ally far outside the redirect radius.
	farAlly := s.spawnPlayerUnitLocked("soldier", "p1", "#f1c40f", protocol.Vec2{
		X: vanguard.X + def.Config["radius"]*2,
		Y: vanguard.Y,
	})
	farAlly.MaxHP = 500
	farAlly.HP = 500
	farAlly.Visible = true

	const rawDamage = 100
	vanguardHPBefore := vanguard.HP
	allyHPBefore := farAlly.HP

	s.applyUnitDamageLocked(farAlly, rawDamage)

	allyDamage := allyHPBefore - farAlly.HP
	vanguardDamage := vanguardHPBefore - vanguard.HP

	if allyDamage != rawDamage {
		t.Errorf("ally outside radius should take full damage: got %d, want %d", allyDamage, rawDamage)
	}
	if vanguardDamage != 0 {
		t.Errorf("Vanguard should not absorb redirect for ally outside radius: got %d", vanguardDamage)
	}
}

// TestPainShare_DeadVanguardNoRedirect verifies a dead Vanguard is not an
// eligible redirect target.
func TestPainShare_DeadVanguardNoRedirect(t *testing.T) {
	s, vanguard, ally := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	vanguard.HP = 0 // dead

	const rawDamage = 100
	allyHPBefore := ally.HP
	s.applyUnitDamageLocked(ally, rawDamage)
	allyDamage := allyHPBefore - ally.HP

	if allyDamage != rawDamage {
		t.Errorf("dead Vanguard: ally should take full damage, got %d, want %d", allyDamage, rawDamage)
	}
}

// TestPainShare_RecursionGuard verifies that PainShareActive prevents circular
// redirect loops. When ally takes damage, the nearest Vanguard (v2) absorbs.
// Because v1 is within v2's pain_share radius, v1 further absorbs part of v2's
// redirected share — this is a finite, well-defined chain (ally→v2→v1). The
// PainShareActive guard blocks the circular path (v1→v2 is skipped because v2
// is active). Test verifies: (a) no infinite loop, (b) ally takes < raw damage,
// (c) PainShareActive is cleared on all units after the call.
func TestPainShare_RecursionGuard(t *testing.T) {
	s, v1, ally := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")

	// v2 is CLOSER to ally than v1 (v1 at dist 50, v2 at dist 30).
	// v1 is within v2's redirect radius (v1 at 400, v2 at 420 — dist 20 < 100).
	v2 := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{
		X: ally.X - def.Config["radius"]*0.3, // dist 30 from ally; dist 20 from v1
		Y: ally.Y,
	})
	v2.MaxHP = 500
	v2.HP = 500
	v2.Visible = true
	grantPerk(v2, "pain_share")

	const rawDamage = 100
	allyHPBefore := ally.HP

	// Must not panic or infinite-loop:
	s.applyUnitDamageLocked(ally, rawDamage)

	// Ally should take less than raw damage (redirect reduced it).
	allyDamage := allyHPBefore - ally.HP
	if allyDamage >= rawDamage {
		t.Errorf("ally should take less than raw damage: got %d, want < %d", allyDamage, rawDamage)
	}

	// PainShareActive must be cleared after the call stack unwinds.
	if v1.PerkState.PainShareActive {
		t.Error("v1.PainShareActive should be false after redirect call completes")
	}
	if v2.PerkState.PainShareActive {
		t.Error("v2.PainShareActive should be false after redirect call completes")
	}
}

// TestPainShare_VanguardAbsorbsThroughOwnMitigation verifies that redirected
// damage runs through the Vanguard's perk mitigation stack (flat reduction,
// shield, percentage DR). Note: armor is applied before applyUnitDamageLocked
// by the caller in the combat path — the redirect path calls applyUnitDamageLocked
// directly, so armor is NOT re-applied. This test verifies shield absorption.
func TestPainShare_VanguardAbsorbsThroughOwnMitigation(t *testing.T) {
	s, vanguard, ally := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	redirectPct := def.Config["redirectPercent"]

	// Give the Vanguard a shield so the redirected damage is absorbed by it.
	vanguard.Shield = 50
	const rawDamage = 100
	redirected := maxInt(1, int(math.Round(float64(rawDamage)*redirectPct))) // 30

	// Shield absorbs first, so the Vanguard's HP should not change (shield = 50 > 30).
	vanguardHPBefore := vanguard.HP
	vanguardShieldBefore := vanguard.Shield
	s.applyUnitDamageLocked(ally, rawDamage)
	vanguardHPDamage := vanguardHPBefore - vanguard.HP
	shieldAbsorbed := vanguardShieldBefore - vanguard.Shield

	if vanguardHPDamage != 0 {
		t.Errorf("Vanguard HP should be unchanged (shield absorbed redirect): HP dropped by %d", vanguardHPDamage)
	}
	if diff := shieldAbsorbed - redirected; diff > 1 || diff < -1 {
		t.Errorf("shield absorbed: got %d, want ~%d (redirectPct=%.0f%%)", shieldAbsorbed, redirected, redirectPct*100)
	}
}

// TestPainShare_DoesNotTriggerRetaliation verifies that the redirected damage
// absorbed by the Vanguard does NOT invoke onPerkDamageTakenLocked (confirmed by
// the code path: applyUnitDamageLocked is called recursively, which does not
// call onPerkDamageTakenLocked). Test by giving the Vanguard retaliation and
// checking the attacker is NOT damaged by retaliation from the redirect.
func TestPainShare_DoesNotTriggerRetaliation(t *testing.T) {
	s, vanguard, ally := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Give the Vanguard retaliation and armor so a reflection would be detectable.
	grantPerk(vanguard, "retaliation")
	vanguard.Armor = 10

	// Simulate an attacker dealing damage to the ally.
	attacker := spawnEnemy(t, s, ally.X+30, ally.Y)
	attackerHPBefore := attacker.HP

	// Apply damage to ally — pain_share redirects to Vanguard; applyUnitDamageLocked
	// is called recursively for the Vanguard but onPerkDamageTakenLocked is NOT
	// called for that redirect call (it's only called from tickUnitCombatLocked
	// after the full damage pipeline).
	s.applyUnitDamageLocked(ally, 50)

	// Attacker should not have taken any reflected damage because retaliation
	// was not triggered through the redirect path.
	attackerDamage := attackerHPBefore - attacker.HP
	if attackerDamage != 0 {
		t.Errorf("retaliation should NOT trigger from pain_share redirect: attacker took %d damage", attackerDamage)
	}
}

// TestPainShare_ThreeVanguardsOneAbsorber verifies that when three pain_share
// Vanguards are in range of an ally, only the nearest (by Euclidean distance)
// directly absorbs the redirect from the ally's damage. The Vanguards are
// placed far from each other (> redirect radius apart) so they cannot chain-
// redirect to one another, isolating the "nearest wins" selection.
func TestPainShare_ThreeVanguardsOneAbsorber(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	radius := def.Config["radius"] // 100

	// Ally at origin.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 500, Y: 500})
	ally.MaxHP = 500
	ally.HP = 500
	ally.Visible = true

	// v1 at distance 80 (nearer than v2, v3).
	// v2 at distance 90.
	// v3 at distance 95.
	// All three are SPREAD OUT FROM EACH OTHER by >200 units so no chain-redirect.
	// Use perpendicular directions to ensure inter-Vanguard distance > radius.
	v1 := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{
		X: ally.X + radius*0.8, // dist 80 from ally; far from v2/v3
		Y: ally.Y,
	})
	v1.MaxHP = 500
	v1.HP = 500
	v1.Visible = true
	grantPerk(v1, "pain_share")

	v2 := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{
		X: ally.X, // x same as ally
		Y: ally.Y + radius*0.9, // dist 90 from ally; v1 is at dist sqrt(80²+90²)≈120 > 100
	})
	v2.MaxHP = 500
	v2.HP = 500
	v2.Visible = true
	grantPerk(v2, "pain_share")

	v3 := s.spawnPlayerUnitLocked("soldier", "p1", "#e67e22", protocol.Vec2{
		X: ally.X - radius*0.95, // dist 95 from ally; v1 is at dist sqrt(80²+95²)≈124 > 100, v2 at sqrt(0+185²)=185 > 100
		Y: ally.Y,
	})
	v3.MaxHP = 500
	v3.HP = 500
	v3.Visible = true
	grantPerk(v3, "pain_share")

	const rawDamage = 100
	v1HPBefore, v2HPBefore, v3HPBefore := v1.HP, v2.HP, v3.HP
	s.applyUnitDamageLocked(ally, rawDamage)

	v1Damage := v1HPBefore - v1.HP
	v2Damage := v2HPBefore - v2.HP
	v3Damage := v3HPBefore - v3.HP

	// v1 is nearest (dist 80) — should absorb the redirect from ally.
	if v1Damage <= 0 {
		t.Error("nearest Vanguard (v1, dist 80) should absorb the redirect")
	}
	// v2 and v3 should NOT absorb (they are farther and not in each other's redirect chain).
	if v2Damage != 0 {
		t.Errorf("v2 (dist 90, not nearest) should not absorb redirect from ally: got %d", v2Damage)
	}
	if v3Damage != 0 {
		t.Errorf("v3 (dist 95, not nearest) should not absorb redirect from ally: got %d", v3Damage)
	}
}

// TestPainShare_DeterminismReplay verifies that identical seeds and unit
// placements produce identical redirect math tick-for-tick.
func TestPainShare_DeterminismReplay(t *testing.T) {
	setup := func() (*GameState, *Unit, *Unit) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 55)
		s.mu.Lock()
		defer s.mu.Unlock()

		v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		v.MaxHP = 500
		v.HP = 500
		grantPerk(v, "pain_share")

		a := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 450, Y: 400})
		a.MaxHP = 500
		a.HP = 500
		a.Visible = true
		return s, v, a
	}

	for trial := 0; trial < 3; trial++ {
		s1, v1, a1 := setup()
		s2, v2, a2 := setup()

		s1.mu.Lock()
		v1HPBefore, a1HPBefore := v1.HP, a1.HP
		s1.applyUnitDamageLocked(a1, 100)
		v1Damage := v1HPBefore - v1.HP
		a1Damage := a1HPBefore - a1.HP
		s1.mu.Unlock()

		s2.mu.Lock()
		v2HPBefore, a2HPBefore := v2.HP, a2.HP
		s2.applyUnitDamageLocked(a2, 100)
		v2Damage := v2HPBefore - v2.HP
		a2Damage := a2HPBefore - a2.HP
		s2.mu.Unlock()

		if v1Damage != v2Damage || a1Damage != a2Damage {
			t.Errorf("trial %d: non-deterministic redirect: s1(v=%d,a=%d) vs s2(v=%d,a=%d)",
				trial, v1Damage, a1Damage, v2Damage, a2Damage)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// rallying_banner
// ─────────────────────────────────────────────────────────────────────────────

// tickStationarySeconds ticks the perk state loop for dt seconds with the unit
// stationary, enough to reach the threshold.
func tickUntilStationary(s *GameState, unit *Unit, thresholdSec float64, dt float64) {
	elapsed := 0.0
	for elapsed < thresholdSec+dt {
		s.tickUnitPerkStateLocked(unit, dt)
		elapsed += dt
	}
}

// TestRallyingBanner_PlantsAfterThreshold verifies the banner appears in
// s.Banners after the unit holds position for the threshold duration.
func TestRallyingBanner_PlantsAfterThreshold(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	if def == nil {
		t.Fatal("rallying_banner perk def not found")
	}

	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) != 1 {
		t.Fatalf("expected 1 banner after threshold, got %d", len(s.Banners))
	}
	b := s.Banners[0]
	if b.OwnerUnitID != vanguard.ID {
		t.Errorf("banner owner mismatch: got %d, want %d", b.OwnerUnitID, vanguard.ID)
	}
	if math.Abs(b.RemainingSeconds-def.Config["bannerDurationSeconds"]) > 0.15 {
		t.Errorf("banner remaining: got %.2f, want ~%.2f", b.RemainingSeconds, def.Config["bannerDurationSeconds"])
	}
}

// TestRallyingBanner_GrantsArmorAndAttackSpeedToAllies verifies that an ally
// inside the banner radius gains bonus armor and attack speed.
func TestRallyingBanner_GrantsArmorAndAttackSpeedToAllies(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")

	// Plant the banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) == 0 {
		t.Fatal("expected banner to be planted")
	}

	// Spawn ally inside banner radius.
	ally := spawnAlly(t, s, "p1", vanguard.X+def.Config["bannerRadius"]*0.5, vanguard.Y)
	baseArmor := ally.Armor
	baseAS := ally.AttackSpeed

	armorBonus := s.perkBonusArmorFromBannersLocked(ally)
	if armorBonus != int(def.Config["bannerArmorBonus"]) {
		t.Errorf("armor bonus: got %d, want %d", armorBonus, int(def.Config["bannerArmorBonus"]))
	}
	if got := s.effectiveArmorLocked(ally); got != baseArmor+armorBonus {
		t.Errorf("effectiveArmorLocked with banner: got %d, want %d", got, baseArmor+armorBonus)
	}

	asBonus := s.perkAttackSpeedBonusFromBannersLocked(ally)
	if math.Abs(asBonus-def.Config["bannerAttackSpeedBonus"]) > 0.001 {
		t.Errorf("attack speed bonus: got %.3f, want %.3f", asBonus, def.Config["bannerAttackSpeedBonus"])
	}
	_ = baseAS
}

// TestRallyingBanner_DoesNotAffectEnemies verifies enemies get no benefit.
func TestRallyingBanner_DoesNotAffectEnemies(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	enemy := spawnEnemy(t, s, vanguard.X+def.Config["bannerRadius"]*0.5, vanguard.Y)

	if s.perkBonusArmorFromBannersLocked(enemy) != 0 {
		t.Error("enemy should get no armor from banner")
	}
	if s.perkAttackSpeedBonusFromBannersLocked(enemy) != 0 {
		t.Error("enemy should get no attack speed from banner")
	}
}

// TestRallyingBanner_DoesNotPlantWhileMoving verifies no banner is planted
// while the unit is moving, regardless of elapsed time.
func TestRallyingBanner_DoesNotPlantWhileMoving(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = true

	elapsed := 0.0
	for elapsed < def.Config["stationaryThresholdSeconds"]+1.0 {
		s.tickUnitPerkStateLocked(vanguard, 0.05)
		elapsed += 0.05
	}

	if len(s.Banners) != 0 {
		t.Errorf("no banner should be planted while moving, got %d", len(s.Banners))
	}
}

// TestRallyingBanner_PersistsAfterMoving verifies the banner stays in s.Banners
// after the owner moves.
func TestRallyingBanner_PersistsAfterMoving(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted before movement test")
	}

	// Move the unit.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if len(s.Banners) == 0 {
		t.Error("banner should persist after owner moves")
	}
	// Tick banners with player still present — should not expire yet.
	s.tickBannersLocked(0.1)
	if len(s.Banners) == 0 {
		t.Error("banner should still be active (duration not elapsed)")
	}
}

// TestRallyingBanner_PersistsAfterOwnerDies verifies the banner outlives the
// owner unit. Death is simulated by setting HP=0 (the banner has no HP check).
func TestRallyingBanner_PersistsAfterOwnerDies(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add the player to the Players map so tickBannersLocked doesn't drop the banner.
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted")
	}

	// Kill the owner.
	vanguard.HP = 0

	// Tick banners — banner persists because player is still in s.Players.
	s.tickBannersLocked(0.1)
	if len(s.Banners) == 0 {
		t.Error("banner should persist after owner unit dies (only player leaving drops it)")
	}
}

// TestRallyingBanner_ExpiresCleanly verifies the banner is removed from
// s.Banners after its duration elapses.
func TestRallyingBanner_ExpiresCleanly(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted")
	}

	// Drain the banner's duration.
	duration := def.Config["bannerDurationSeconds"]
	elapsed := 0.0
	dt := 0.1
	for elapsed < duration+dt {
		s.tickBannersLocked(dt)
		elapsed += dt
	}

	if len(s.Banners) != 0 {
		t.Errorf("expired banner should be removed, got %d banners", len(s.Banners))
	}
}

// TestRallyingBanner_OwnerPlayerLeaves verifies that tickBannersLocked drops
// the banner when the owner's player is removed from s.Players.
func TestRallyingBanner_OwnerPlayerLeaves(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted")
	}

	// Remove the player.
	delete(s.Players, "p1")
	s.tickBannersLocked(0.1)

	if len(s.Banners) != 0 {
		t.Errorf("banner should be dropped when owner player leaves, got %d", len(s.Banners))
	}
}

// TestRallyingBanner_CooldownPreventsImmediateReplant verifies that after planting
// a banner, moving and standing stationary again does NOT plant a second banner
// until the 12s cooldown has elapsed. Replaces the old one-shot-per-stationary-
// period test now that cooldown is the gate rather than a boolean flag.
func TestRallyingBanner_CooldownPreventsImmediateReplant(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	cooldown := def.Config["cooldownSeconds"] // 12s

	// Plant the first banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) != 1 {
		t.Fatalf("expected 1 banner after first plant, got %d", len(s.Banners))
	}

	// Move, then immediately stand stationary again (cooldown not yet elapsed).
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	vanguard.Moving = false

	// Tick stationary for threshold — cooldown still > 0, no second banner.
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) != 1 {
		t.Errorf("second banner should NOT plant before cooldown expires, got %d banners", len(s.Banners))
	}

	// Verify cooldown is decaying (not reset by movement).
	if vanguard.PerkState.BannerCooldownRemaining >= cooldown {
		t.Errorf("cooldown should have decayed from max, got %.2f", vanguard.PerkState.BannerCooldownRemaining)
	}
}

// TestRallyingBanner_SharedStationaryCounter_WithBulwark verifies that when a
// unit has both bulwark and rallying_banner, both proc at threshold simultaneously.
// After moving, BulwarkShieldGranted resets (bulwark re-arms) while the banner
// cooldown continues decaying — movement does NOT reset the banner cooldown.
// A second banner cannot plant until the 12s cooldown has elapsed.
func TestRallyingBanner_SharedStationaryCounter_WithBulwark(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "bulwark")
	grantPerk(vanguard, "rallying_banner")
	bulwarkDef := perkDefByID("bulwark")
	bannerDef := perkDefByID("rallying_banner")

	// Both thresholds are the same (3 s) in the catalog.
	threshold := math.Max(
		bulwarkDef.Config["stationaryThresholdSeconds"],
		bannerDef.Config["stationaryThresholdSeconds"],
	)

	vanguard.Moving = false
	tickUntilStationary(s, vanguard, threshold, 0.05)

	if vanguard.Shield != int(bulwarkDef.Config["maxShield"]) {
		t.Errorf("bulwark shield not granted: got %d, want %d", vanguard.Shield, int(bulwarkDef.Config["maxShield"]))
	}
	if len(s.Banners) == 0 {
		t.Error("rallying_banner should have planted a banner")
	}
	// Cooldown should be set after first plant.
	if vanguard.PerkState.BannerCooldownRemaining <= 0 {
		t.Errorf("BannerCooldownRemaining should be > 0 after first plant, got %.2f",
			vanguard.PerkState.BannerCooldownRemaining)
	}

	// Move — BulwarkShieldGranted resets; StationarySeconds resets; cooldown keeps decaying.
	cooldownBeforeMove := vanguard.PerkState.BannerCooldownRemaining
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if vanguard.PerkState.BulwarkShieldGranted {
		t.Error("BulwarkShieldGranted should reset on movement")
	}
	if vanguard.PerkState.StationarySeconds != 0 {
		t.Errorf("StationarySeconds should reset to 0 on movement, got %.2f", vanguard.PerkState.StationarySeconds)
	}
	// Cooldown decayed by dt=0.05 during the moving tick (not reset).
	wantCooldown := cooldownBeforeMove - 0.05
	if wantCooldown < 0 {
		wantCooldown = 0
	}
	if math.Abs(vanguard.PerkState.BannerCooldownRemaining-wantCooldown) > 0.001 {
		t.Errorf("BannerCooldownRemaining should decay by dt when moving: got %.3f, want %.3f",
			vanguard.PerkState.BannerCooldownRemaining, wantCooldown)
	}

	// Stand stationary again — cooldown still > 0, no second banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, threshold, 0.05)
	if len(s.Banners) != 1 {
		t.Errorf("second banner should NOT plant before cooldown expires, got %d banners", len(s.Banners))
	}
	// Bulwark re-arms regardless (independent of banner cooldown).
	if vanguard.Shield != int(bulwarkDef.Config["maxShield"]) {
		t.Error("bulwark should re-arm after reaching stationary threshold again")
	}

	// Move to reset StationarySeconds, then drain the remaining cooldown while
	// moving so the plant condition (StationarySeconds >= threshold) cannot fire
	// during the drain and immediately re-set the cooldown.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05) // resets StationarySeconds to 0
	remaining := vanguard.PerkState.BannerCooldownRemaining
	steps := int(math.Ceil(remaining/0.05)) + 2
	for i := 0; i < steps; i++ {
		vanguard.Moving = true
		s.tickUnitPerkStateLocked(vanguard, 0.05)
	}
	if vanguard.PerkState.BannerCooldownRemaining > 0.001 {
		t.Fatalf("cooldown should have drained to 0, got %.3f", vanguard.PerkState.BannerCooldownRemaining)
	}

	// Now stand stationary once more — second banner should plant.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, bannerDef.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) < 2 {
		t.Errorf("expected 2nd banner after cooldown expires and stationary threshold met, got %d banners", len(s.Banners))
	}
}

// TestRallyingBanner_StacksMultiBanner verifies that an ally covered by two
// banners from two different Vanguards receives the SUM of both contributions.
func TestRallyingBanner_StacksMultiBanner(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(v1, "rallying_banner")
	def := perkDefByID("rallying_banner")

	// V2 with the same perk.
	v2 := spawnAlly(t, s, "p1", v1.X+30, v1.Y)
	grantPerk(v2, "rallying_banner")

	// Plant from both.
	for _, u := range []*Unit{v1, v2} {
		u.Moving = false
		tickUntilStationary(s, u, def.Config["stationaryThresholdSeconds"], 0.05)
	}
	if len(s.Banners) < 2 {
		t.Fatalf("expected at least 2 banners, got %d", len(s.Banners))
	}

	// Ally inside both banner radii.
	ally := spawnAlly(t, s, "p1", v1.X+def.Config["bannerRadius"]*0.3, v1.Y)

	armor := s.perkBonusArmorFromBannersLocked(ally)
	if armor != 2*int(def.Config["bannerArmorBonus"]) {
		t.Errorf("multi-banner armor: got %d, want %d (stacked)", armor, 2*int(def.Config["bannerArmorBonus"]))
	}

	as := s.perkAttackSpeedBonusFromBannersLocked(ally)
	if math.Abs(as-2*def.Config["bannerAttackSpeedBonus"]) > 0.001 {
		t.Errorf("multi-banner AS: got %.3f, want %.3f (stacked)", as, 2*def.Config["bannerAttackSpeedBonus"])
	}
}

// bannerSummary returns a deterministic string representation of a banner slice
// for comparison in determinism tests — uses actual field values, not pointer addresses.
func bannerSummary(banners []*Banner) string {
	s := fmt.Sprintf("count=%d", len(banners))
	for i, b := range banners {
		s += fmt.Sprintf(" [%d]{id=%d ownerUnit=%d ownerPlayer=%s x=%.2f y=%.2f r=%.2f remaining=%.6f armor=%d as=%.3f}",
			i, b.ID, b.OwnerUnitID, b.OwnerPlayerID, b.X, b.Y, b.Radius, b.RemainingSeconds, b.ArmorBonus, b.AttackSpeedBonus)
	}
	return s
}

// TestRallyingBanner_DeterminismReplay verifies identical seed/unit placement
// produces identical s.Banners (by field values) tick-for-tick.
func TestRallyingBanner_DeterminismReplay(t *testing.T) {
	setup := func() (*GameState, *Unit) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 13)
		s.mu.Lock()
		defer s.mu.Unlock()

		s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
		v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		v.MaxHP = 500
		v.HP = 500
		grantPerk(v, "rallying_banner")
		v.Moving = false
		return s, v
	}

	s1, v1 := setup()
	s2, v2 := setup()

	for tick := 0; tick < 80; tick++ {
		dt := 0.05

		s1.mu.Lock()
		s1.tickUnitPerkStateLocked(v1, dt)
		s1.tickBannersLocked(dt)
		b1 := bannerSummary(s1.Banners)
		s1.mu.Unlock()

		s2.mu.Lock()
		s2.tickUnitPerkStateLocked(v2, dt)
		s2.tickBannersLocked(dt)
		b2 := bannerSummary(s2.Banners)
		s2.mu.Unlock()

		if b1 != b2 {
			t.Fatalf("tick %d: banner state mismatch:\n  s1: %s\n  s2: %s", tick, b1, b2)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Snapshot / protocol
// ─────────────────────────────────────────────────────────────────────────────

// TestBannerSnapshot_RoundTrip plants a banner and verifies the snapshot
// message contains the expected BannerSnapshot fields.
func TestBannerSnapshot_RoundTrip(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")

	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) == 0 {
		s.mu.Unlock()
		t.Fatal("expected banner to be planted before snapshot")
	}

	bannerID := s.Banners[0].ID
	bannerX := s.Banners[0].X
	bannerY := s.Banners[0].Y
	bannerRadius := s.Banners[0].Radius
	bannerRemaining := s.Banners[0].RemainingSeconds
	s.mu.Unlock()

	snap := s.Snapshot()

	if len(snap.Banners) != 1 {
		t.Fatalf("expected 1 banner in snapshot, got %d", len(snap.Banners))
	}
	b := snap.Banners[0]
	if b.ID != bannerID {
		t.Errorf("banner ID: got %d, want %d", b.ID, bannerID)
	}
	if b.OwnerID != "p1" {
		t.Errorf("banner OwnerID: got %q, want %q", b.OwnerID, "p1")
	}
	if b.X != bannerX || b.Y != bannerY {
		t.Errorf("banner position: got (%.1f, %.1f), want (%.1f, %.1f)", b.X, b.Y, bannerX, bannerY)
	}
	if b.Radius != bannerRadius {
		t.Errorf("banner radius: got %.1f, want %.1f", b.Radius, bannerRadius)
	}
	if math.Abs(b.RemainingSeconds-bannerRemaining) > 0.01 {
		t.Errorf("banner remaining: got %.3f, want %.3f", b.RemainingSeconds, bannerRemaining)
	}

	// Zero-value check: snapshot without banners omits the field.
	s2 := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 99)
	s2.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	snap2 := s2.Snapshot()
	if len(snap2.Banners) != 0 {
		t.Errorf("expected empty banners in snapshot without banners, got %d", len(snap2.Banners))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase-separation invariant: companion detection uses BASE radius only
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_PhaseSeparation_Asymmetric proves the phase-separation rule:
// V1 has 1 companion; V2 has 0 companions. Distance V1<->V2 is 130px —
// OUTSIDE the base radius (100) but INSIDE V1's effective radius (130).
// V2 must NOT count as V1's companion even though V2 sits inside V1's effR.
//
// Geometry (all on Y=400):
//   V1 at x=200; compC at x=250 (dist 50 from V1 < 100 → V1's companion).
//   V2 at x=330 (dist 130 from V1, > 100; dist 80 from compC, < 100).
//   compC is within V2's base radius so V2 has 1 companion too.
//
// To isolate V1's companion count we place V2 at x=500, which is:
//   - 300px from V1 → NOT V1's companion.
//   - 250px from compC → NOT compC's companion (compC only sees V1).
//   - V2 has NO companions.
//
// V1: companions=1, effR=130, effDR=0.20.
// V2: companions=0, effR=100, effDR=0.15.
//
// An ally at x=320 (dist 120 from V1) should be inside V1's effR (130) but
// OUTSIDE V2's effR (100, since it's 180px from V2). It should receive
// exactly V1's effDR=0.20, NOT 0.25 (which would indicate V1 incorrectly
// counted V2 as a companion using effR instead of baseR).
func TestGuardianAura_PhaseSeparation_Asymmetric(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}
	// base radius = 100, synergyRadiusBonus = 30, synergyArmorBonus = 5, synergyArmorPercentBonus = 0.05

	// V1 at x=200.
	v1 := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 200, Y: 400})
	v1.MaxHP = 500
	v1.HP = 500
	grantPerk(v1, "guardian_aura")

	// compC at x=250: dist 50 from V1 (< 100 → V1's companion).
	// dist 250 from V2 (> 100 → NOT V2's companion).
	compC := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 250, Y: 400})
	compC.MaxHP = 500
	compC.HP = 500
	compC.Visible = true
	grantPerk(compC, "guardian_aura")

	// V2 at x=500: 300px from V1 (> 100 → NOT V1's companion), 250px from compC (> 100 → NOT compC's companion).
	// V2 is isolated → companions=0, effR=100.
	v2 := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{X: 500, Y: 400})
	v2.MaxHP = 500
	v2.HP = 500
	v2.Visible = true
	grantPerk(v2, "guardian_aura")

	// Ally at x=320: dist 120 from V1 — OUTSIDE base radius (100), INSIDE V1's effR (130).
	// dist 180 from V2 — OUTSIDE V2's effR (100).
	// Should receive ONLY V1's aura: effFlat = 15+1*5=20, effPercent = 0.20+1*0.05=0.25.
	// If companion detection used effR instead of baseR, V1 would count V2 as a
	// companion (V2 at x=500 is within V1's effR of 130? No: dist 300 > 130. Safe.)
	// Structural assertion: ally at x=320 should see 1-companion values only.
	// If we see 2-companion values it means V1 computed companions=2 somehow.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#e74c3c", protocol.Vec2{X: 320, Y: 400})
	ally.MaxHP = 200
	ally.HP = 200
	ally.Visible = true

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally at dist 120 from V1 should be in cache (V1 effR=130)")
	}
	// V1 has exactly 1 companion (compC). effFlat = 15+5=20, effPercent = 0.20+0.05=0.25.
	wantFlat := int(def.Config["bonusArmor"]) + 1*int(def.Config["synergyArmorBonus"]) // 20
	wantPct := def.Config["armorPercent"] + 1*def.Config["synergyArmorPercentBonus"]   // 0.25
	if aura.FlatArmor != wantFlat {
		t.Errorf("phase-separation: ally FlatArmor=%d, want %d (V1 has 1 companion via baseR only)",
			aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("phase-separation: ally PercentArmor=%.3f, want %.3f (V1 has 1 companion via baseR only)",
			aura.PercentArmor, wantPct)
	}

	// Secondary: V2 is isolated (companions=0, effR=100). An ally 95px from V2
	// should receive base values only.
	allyNearV2 := s.spawnPlayerUnitLocked("soldier", "p1", "#1abc9c", protocol.Vec2{X: 595, Y: 400})
	allyNearV2.MaxHP = 200
	allyNearV2.HP = 200
	allyNearV2.Visible = true
	s.rebuildGuardianAuraCacheLocked()

	auraV2, okV2 := s.guardianAuraCache[allyNearV2.ID]
	if !okV2 {
		t.Fatal("allyNearV2 should be in cache (95px from V2, within V2's base effR=100)")
	}
	wantBaseFlat := int(def.Config["bonusArmor"])
	wantBasePct := def.Config["armorPercent"]
	if auraV2.FlatArmor != wantBaseFlat {
		t.Errorf("isolated V2 FlatArmor: got %d, want base %d (0 companions)", auraV2.FlatArmor, wantBaseFlat)
	}
	if math.Abs(auraV2.PercentArmor-wantBasePct) > 0.001 {
		t.Errorf("isolated V2 PercentArmor: got %.3f, want base %.3f (0 companions)", auraV2.PercentArmor, wantBasePct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Map-order independence: same units, different spawn order
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_MapOrderIndependence verifies that spawning the same units
// in two different slice orders produces identical guardianAuraCache content.
// The algorithm must be commutative across s.Units slice order.
func TestGuardianAura_MapOrderIndependence(t *testing.T) {
	buildState := func(orderAFirst bool) map[int]guardianAuraValue {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 77)
		s.mu.Lock()
		defer s.mu.Unlock()

		// Two Vanguards 80 px apart (within base 100); one ally 50 px from each.
		positions := []struct {
			x, y float64
			perk bool
		}{
			{200, 400, true},  // V1
			{280, 400, true},  // V2 (80 px from V1)
			{240, 400, false}, // ally (between them)
		}
		if !orderAFirst {
			positions[0], positions[1] = positions[1], positions[0]
		}

		var ally *Unit
		for _, p := range positions {
			u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: p.x, Y: p.y})
			u.MaxHP = 500
			u.HP = 500
			u.Visible = true
			if p.perk {
				grantPerk(u, "guardian_aura")
			} else {
				ally = u
			}
		}
		s.rebuildGuardianAuraCacheLocked()
		result := make(map[int]guardianAuraValue)
		// Capture by relative identity (ally's cache entry), not unit ID (which
		// changes with spawn order). We return the aura value the ally received.
		if ally != nil {
			if av, ok := s.guardianAuraCache[ally.ID]; ok {
				result[0] = av // key 0 = "the ally"
			}
		}
		return result
	}

	cacheOrderA := buildState(true)
	cacheOrderB := buildState(false)

	avA, okA := cacheOrderA[0]
	avB, okB := cacheOrderB[0]
	if okA != okB {
		t.Fatalf("ally presence in cache differs by spawn order: orderA=%v orderB=%v", okA, okB)
	}
	if okA {
		if avA.FlatArmor != avB.FlatArmor {
			t.Errorf("aura FlatArmor differs by spawn order: orderA=%d orderB=%d", avA.FlatArmor, avB.FlatArmor)
		}
		if math.Abs(avA.PercentArmor-avB.PercentArmor) > 1e-12 {
			t.Errorf("aura PercentArmor differs by spawn order: orderA=%.15f orderB=%.15f",
				avA.PercentArmor, avB.PercentArmor)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// pain_share: chain terminates, total damage conserved
// ─────────────────────────────────────────────────────────────────────────────

// TestPainShare_ChainTerminates deals 1000 damage to an ally surrounded by 5
// pain_share Vanguards all within each other's redirect radius. Verifies:
//   - no stack overflow (terminates)
//   - total HP lost across all units equals exactly 1000
//   - PainShareActive cleared on every unit after resolution
func TestPainShare_ChainTerminates(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 13)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	if def == nil {
		t.Fatal("pain_share perk def not found")
	}

	// All units clustered within the redirect radius (100px) of each other.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 500, Y: 500})
	ally.MaxHP = 2000
	ally.HP = 2000
	ally.Visible = true

	vanguards := make([]*Unit, 5)
	offsets := [][2]float64{{20, 0}, {-20, 0}, {0, 20}, {0, -20}, {10, 10}}
	for i, off := range offsets {
		v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{
			X: ally.X + off[0],
			Y: ally.Y + off[1],
		})
		v.MaxHP = 2000
		v.HP = 2000
		v.Visible = true
		grantPerk(v, "pain_share")
		vanguards[i] = v
	}

	totalHPBefore := ally.HP
	for _, v := range vanguards {
		totalHPBefore += v.HP
	}

	const rawDamage = 1000
	// Must not panic or infinite-loop:
	s.applyUnitDamageLocked(ally, rawDamage)

	totalHPAfter := ally.HP
	for _, v := range vanguards {
		totalHPAfter += v.HP
	}

	totalLost := totalHPBefore - totalHPAfter
	// Rounding may cause the sum to be off by ±(chain depth) due to int rounding
	// at each hop. Allow tolerance of chain depth (5 hops max here).
	if totalLost < rawDamage-5 || totalLost > rawDamage+5 {
		t.Errorf("total HP lost=%d across all units; want ~%d (damage conservation)", totalLost, rawDamage)
	}

	// PainShareActive must be clear after resolution.
	for i, v := range vanguards {
		if v.PerkState.PainShareActive {
			t.Errorf("vanguard[%d].PainShareActive should be false after chain resolves", i)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Banner: floating-point expiry accuracy over 160 ticks
// ─────────────────────────────────────────────────────────────────────────────

// TestBanner_FloatingPointAccuracy verifies banner expiry is robust to float64
// accumulation drift. Repeated subtraction of dt=0.05 leaves RemainingSeconds
// at ~2e-14 instead of exactly 0 after the duration's worth of ticks; the
// epsilon guard in tickBannersLocked treats sub-nanosecond residual as expired
// so the banner drops at the expected tick rather than one tick late. Tick
// count is derived from the perk's configured duration so the test tracks any
// future balance tuning.
func TestBanner_FloatingPointAccuracy(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	if def == nil {
		t.Fatal("rallying_banner perk def not found")
	}

	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted before accuracy test")
	}

	const dt = 0.05
	durationSec := def.Config["bannerDurationSeconds"]
	exactTicks := int(math.Round(durationSec / dt))
	for i := 0; i < exactTicks; i++ {
		s.tickBannersLocked(dt)
	}

	if len(s.Banners) != 0 {
		t.Errorf("banner should have expired at tick %d (%.1fs duration); got %d banners remaining", exactTicks, durationSec, len(s.Banners))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Banner: player-leave cleanup
// ─────────────────────────────────────────────────────────────────────────────

// TestBanner_PlayerLeaveCleanup plants a banner, removes the owner player from
// s.Players, then ticks once. Banner must be gone — verifies the owner-player-
// leave path in tickBannersLocked directly.
func TestBanner_PlayerLeaveCleanup(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted before player-leave test")
	}

	// Remove the owner player.
	delete(s.Players, "p1")
	// Tick with a small dt so the banner would NOT expire by time alone.
	s.tickBannersLocked(0.01)

	if len(s.Banners) != 0 {
		t.Errorf("banner should be removed immediately when owner player leaves; got %d banners", len(s.Banners))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration: pain_share + guardian_aura double-cover ordering
// ─────────────────────────────────────────────────────────────────────────────

// TestPainShare_PlusGuardianAura_OrderCorrect verifies pipeline ordering when
// both pain_share and guardian_aura cover an ally:
//   - guardian_aura increases ally's effective armor (applied by the combat
//     caller via applyArmorMitigation BEFORE applyUnitDamageLocked)
//   - pain_share redirect runs at step 2 INSIDE applyUnitDamageLocked
//     (on whatever post-armor damage the caller passed in)
//
// Test: caller passes post-armor rawDamage=100, then pain_share redirects 30%.
// The ally takes 70 (after redirect). The Vanguard takes the redirected 30.
// Guardian_aura's contribution to this test is at the armor layer only — the
// aura increases effective armor which reduces raw damage before this function.
// Here we test that pain_share redirect order is correct: redirect on the
// value passed to applyUnitDamageLocked (already post-armor).
func TestPainShare_PlusGuardianAura_OrderCorrect(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 91)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	if def == nil {
		t.Fatal("pain_share perk def not found")
	}
	auraDef := perkDefByID("guardian_aura")
	if auraDef == nil {
		t.Fatal("guardian_aura perk def not found")
	}

	// Ally at origin.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 500, Y: 500})
	ally.MaxHP = 1000
	ally.HP = 1000
	ally.Visible = true

	// pain_share Vanguard within redirect radius.
	psV := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 550, Y: 500})
	psV.MaxHP = 1000
	psV.HP = 1000
	psV.Visible = true
	grantPerk(psV, "pain_share")

	// guardian_aura Vanguard — covers ally (within base 100px).
	gaV := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{X: 530, Y: 500})
	gaV.MaxHP = 1000
	gaV.HP = 1000
	gaV.Visible = true
	grantPerk(gaV, "guardian_aura")

	// Build the aura cache.
	s.rebuildGuardianAuraCacheLocked()

	// Confirm ally is in the aura cache with expected bonuses.
	auraEntry := s.guardianAuraCache[ally.ID]
	wantFlat := int(auraDef.Config["bonusArmor"])
	if auraEntry.FlatArmor != wantFlat {
		t.Fatalf("ally aura FlatArmor: got %d, want %d", auraEntry.FlatArmor, wantFlat)
	}

	// Simulate what the combat caller does: armor mitigation first, then applyUnitDamageLocked.
	// With aura, ally's effective armor is higher, reducing the post-armor damage.
	const attackerRawDamage = 100
	effectiveAllylArmor := s.effectiveArmorLocked(ally)
	postArmorDamage := applyArmorMitigation(attackerRawDamage, effectiveAllylArmor)

	// Inside applyUnitDamageLocked, pain_share redirects 30% of the post-armor value.
	redirectPct := def.Config["redirectPercent"]
	redirected := maxInt(1, int(math.Round(float64(postArmorDamage)*redirectPct)))
	wantAllyDamage := postArmorDamage - redirected

	psVHPBefore := psV.HP
	allyHPBefore := ally.HP
	s.applyUnitDamageLocked(ally, postArmorDamage)
	gotAllyDamage := allyHPBefore - ally.HP
	psVDamage := psVHPBefore - psV.HP

	if diff := gotAllyDamage - wantAllyDamage; diff > 1 || diff < -1 {
		t.Errorf("pain_share+guardian_aura: ally took %d HP, want ~%d (post-armor=%d, redirect=%d)",
			gotAllyDamage, wantAllyDamage, postArmorDamage, redirected)
	}
	if psVDamage <= 0 {
		t.Errorf("pain_share Vanguard should have absorbed redirected damage, got %d", psVDamage)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration: interlock stacks with banner armor
// ─────────────────────────────────────────────────────────────────────────────

// TestInterlock_StacksWithRallyingBanner verifies that a unit with interlock
// standing near an ally AND inside a rallying_banner receives:
//   effectiveArmor = base + interlock bonus + banner armor bonus
func TestInterlock_StacksWithRallyingBanner(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	// Plant a banner at vanguard's position.
	grantPerk(vanguard, "rallying_banner")
	bannerDef := perkDefByID("rallying_banner")
	if bannerDef == nil {
		t.Fatal("rallying_banner perk def not found")
	}
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, bannerDef.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted")
	}

	interlockDef := perkDefByID("interlock")
	if interlockDef == nil {
		t.Fatal("interlock perk def not found")
	}

	// Spawn a unit with interlock inside the banner radius, with a nearby ally.
	unit := spawnAlly(t, s, "p1", vanguard.X+bannerDef.Config["bannerRadius"]*0.3, vanguard.Y)
	unit.Armor = 5
	grantPerk(unit, "interlock")

	// The vanguard itself counts as the nearby ally for interlock (within interlock radius).
	// Confirm ally is within interlock radius.
	interlockRadius := interlockDef.Config["radius"]
	dx := unit.X - vanguard.X
	if math.Abs(dx) >= interlockRadius {
		t.Fatalf("test setup: unit not within interlock radius (dx=%.1f, radius=%.1f)", dx, interlockRadius)
	}

	baseArmor := unit.Armor
	interlockBonus := int(interlockDef.Config["bonusArmor"])
	bannerBonus := int(bannerDef.Config["bannerArmorBonus"])
	wantEffective := baseArmor + interlockBonus + bannerBonus

	got := s.effectiveArmorLocked(unit)
	if got != wantEffective {
		t.Errorf("effectiveArmor with interlock+banner: got %d, want %d (base=%d + interlock=%d + banner=%d)",
			got, wantEffective, baseArmor, interlockBonus, bannerBonus)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// God-run: four Vanguards all with guardian_aura (per feedback memory)
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_FourVanguardCluster_GodRun verifies that 4 allied Vanguards
// all clustered (within base 100px of each other) each compute companions=3,
// effFlat=15+3*5=30, effPercent=0.20+3*0.05=0.35. An ally in the cluster
// receives these bonuses, stacking with brace for a powerful god-run.
// Tests the "god run" synergy path the design explicitly allows.
func TestGuardianAura_FourVanguardCluster_GodRun(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}

	// 4 Vanguards in a tight cluster (all within 50px of center → all within 100px of each other).
	center := protocol.Vec2{X: 400, Y: 400}
	offsets := [][2]float64{{-20, 0}, {20, 0}, {0, -20}, {0, 20}}
	vanguards := make([]*Unit, 4)
	for i, off := range offsets {
		v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{
			X: center.X + off[0],
			Y: center.Y + off[1],
		})
		v.MaxHP = 500
		v.HP = 500
		v.Visible = true
		grantPerk(v, "guardian_aura")
		vanguards[i] = v
	}

	// Ally at center.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", center)
	ally.MaxHP = 500
	ally.HP = 500
	ally.Visible = true

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally should be in aura cache (inside all 4 Vanguard auras)")
	}
	// companions=3: effFlat = 15+3*5=30, effPercent = 0.20+3*0.05=0.35
	wantFlat := int(def.Config["bonusArmor"]) + 3*int(def.Config["synergyArmorBonus"])
	wantPct := def.Config["armorPercent"] + 3*def.Config["synergyArmorPercentBonus"]
	if aura.FlatArmor != wantFlat {
		t.Errorf("4-Vanguard god-run FlatArmor: got %d, want %d", aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("4-Vanguard god-run PercentArmor: got %.3f, want %.3f", aura.PercentArmor, wantPct)
	}

	// Verify effectiveArmorLocked includes both bonuses.
	// effectiveArmor = floor(ally.Armor * (1 + 0.35)) + 30
	wantEffective := int(math.Floor(float64(ally.Armor)*(1.0+wantPct))) + wantFlat
	gotEffective := s.effectiveArmorLocked(ally)
	if gotEffective != wantEffective {
		t.Errorf("4-Vanguard god-run effectiveArmor: got %d, want %d (base=%d, flat=%d, pct=%.2f)",
			gotEffective, wantEffective, ally.Armor, wantFlat, wantPct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// guardian_aura: percent armor scaling and additive stacking
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_PercentArmorScales verifies the percent bonus scales
// proportionally with the recipient's base armor: effectiveArmor =
// floor(base × (1 + armorPercent)) + bonusArmor (no companions → base values).
func TestGuardianAura_PercentArmorScales(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 55)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}
	flatBonus := int(def.Config["bonusArmor"])     // 15
	pctBonus := def.Config["armorPercent"]          // 0.20

	// Single Vanguard at origin; no companions.
	v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	v.MaxHP = 500
	v.HP = 500
	v.Visible = true
	grantPerk(v, "guardian_aura")

	// Three recipients with distinct base armor values.
	cases := []struct {
		baseArmor int
		label     string
	}{
		{18, "light (18)"},
		{54, "medium (54)"},
		{150, "heavy (150)"},
	}

	for _, tc := range cases {
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 420, Y: 400})
		u.Armor = tc.baseArmor
		u.MaxHP = 500
		u.HP = 500
		u.Visible = true

		s.rebuildGuardianAuraCacheLocked()

		aura, ok := s.guardianAuraCache[u.ID]
		if !ok {
			t.Fatalf("recipient (%s) should be in aura cache", tc.label)
		}
		if aura.FlatArmor != flatBonus {
			t.Errorf("recipient (%s) FlatArmor: got %d, want %d", tc.label, aura.FlatArmor, flatBonus)
		}
		if math.Abs(aura.PercentArmor-pctBonus) > 0.001 {
			t.Errorf("recipient (%s) PercentArmor: got %.3f, want %.3f", tc.label, aura.PercentArmor, pctBonus)
		}

		wantEffective := int(math.Floor(float64(tc.baseArmor)*(1.0+pctBonus))) + flatBonus
		gotEffective := s.effectiveArmorLocked(u)
		if gotEffective != wantEffective {
			t.Errorf("recipient (%s) effectiveArmor: got %d, want %d (floor(%d×%.2f)+%d)",
				tc.label, gotEffective, wantEffective, tc.baseArmor, 1.0+pctBonus, flatBonus)
		}

		// Remove for next iteration.
		s.removeUnitByIDLocked(u.ID)
		delete(s.guardianAuraCache, u.ID)
	}
}

// TestGuardianAura_PercentSynergy_4Vanguards verifies the expected effective
// armor for a recipient with base armor 54 inside a 4-Vanguard cluster:
// companions=3 → effFlat=30, effPercent=0.35 →
// effectiveArmor = floor(54 × 1.35) + 30 = floor(72.9) + 30 = 72 + 30 = 102.
func TestGuardianAura_PercentSynergy_4Vanguards(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 56)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}

	center := protocol.Vec2{X: 400, Y: 400}
	offsets := [][2]float64{{-15, 0}, {15, 0}, {0, -15}, {0, 15}}
	for _, off := range offsets {
		v := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{
			X: center.X + off[0], Y: center.Y + off[1],
		})
		v.MaxHP = 500
		v.HP = 500
		v.Visible = true
		grantPerk(v, "guardian_aura")
	}

	recipient := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", center)
	recipient.Armor = 54
	recipient.MaxHP = 500
	recipient.HP = 500
	recipient.Visible = true

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[recipient.ID]
	if !ok {
		t.Fatal("recipient should be in aura cache")
	}

	// companions=3 for every Vanguard → max flat = 30, max pct = 0.35.
	wantFlat := int(def.Config["bonusArmor"]) + 3*int(def.Config["synergyArmorBonus"]) // 30
	wantPct := def.Config["armorPercent"] + 3*def.Config["synergyArmorPercentBonus"]   // 0.35
	if aura.FlatArmor != wantFlat {
		t.Errorf("4-Vanguard synergy FlatArmor: got %d, want %d", aura.FlatArmor, wantFlat)
	}
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("4-Vanguard synergy PercentArmor: got %.3f, want %.3f", aura.PercentArmor, wantPct)
	}

	// floor(54 × 1.35) + 30 = floor(72.9) + 30 = 72 + 30 = 102
	wantEffective := int(math.Floor(float64(54)*(1.0+wantPct))) + wantFlat
	gotEffective := s.effectiveArmorLocked(recipient)
	if gotEffective != wantEffective {
		t.Errorf("4-Vanguard synergy effectiveArmor: got %d, want %d", gotEffective, wantEffective)
	}
}

// TestGuardianAura_FlatAndPercentMaxIndependently verifies that when a recipient
// is covered by two auras simultaneously, the cache stores the maximum of each
// dimension independently — not the sum.
//
// Geometry: V1 and V2 are placed 200px apart (outside each other's base radius
// of 100px) so they don't count as companions. Both cover the recipient via their
// base radii. Each aura provides base values (companions=0): flat=15, pct=0.20.
//
// Crucially: the result must be flat=15 (max of 15,15), NOT flat=30 (sum).
// Similarly pct=0.20 (max), NOT pct=0.40 (sum).
func TestGuardianAura_FlatAndPercentMaxIndependently(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 57)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}
	baseFlat := int(def.Config["bonusArmor"]) // 15
	basePct := def.Config["armorPercent"]      // 0.20

	// V1 at x=200 and V2 at x=400 — 200px apart, outside each other's base radius (100).
	// Neither counts the other as a companion → companions=0, effFlat=15, effPct=0.20, effR=100.
	v1 := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 200, Y: 400})
	v1.MaxHP = 500
	v1.HP = 500
	v1.Visible = true
	grantPerk(v1, "guardian_aura")

	v2 := s.spawnPlayerUnitLocked("soldier", "p1", "#9b59b6", protocol.Vec2{X: 400, Y: 400})
	v2.MaxHP = 500
	v2.HP = 500
	v2.Visible = true
	grantPerk(v2, "guardian_aura")

	// Recipient at x=300: exactly 100px from both V1 and V2 — within both base radii.
	recipient := s.spawnPlayerUnitLocked("soldier", "p1", "#e74c3c", protocol.Vec2{X: 300, Y: 400})
	recipient.MaxHP = 300
	recipient.HP = 300
	recipient.Visible = true

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[recipient.ID]
	if !ok {
		t.Fatal("recipient should be in aura cache (within 100px of both V1 and V2)")
	}

	// Both auras provide base values; max per dimension = base values (not doubled).
	if aura.FlatArmor != baseFlat {
		t.Errorf("max-not-sum FlatArmor: got %d, want %d (NOT sum=%d)",
			aura.FlatArmor, baseFlat, 2*baseFlat)
	}
	if math.Abs(aura.PercentArmor-basePct) > 0.001 {
		t.Errorf("max-not-sum PercentArmor: got %.3f, want %.3f (NOT sum=%.3f)",
			aura.PercentArmor, basePct, 2*basePct)
	}
	// Explicit sum check: result must not be the doubled value.
	if aura.FlatArmor == 2*baseFlat {
		t.Errorf("FlatArmor is doubled (%d) — appears to be sum instead of max", aura.FlatArmor)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Armor percent: additive stacking
// ─────────────────────────────────────────────────────────────────────────────

// TestArmorPercent_StackingAdditive verifies that two percent-armor sources
// stack additively, not multiplicatively. A unit covered by two guardian_aura
// Vanguards (each contributing armorPercent=0.20) gets +40% not +44%.
// Effective armor = floor(base × 1.40) + 2*flatBonus.
//
// Note: two separate auras fan-out independently; the cache stores max per
// dimension, so for additive stacking to be visible we need a single aura
// source whose companions compound (each companion adds to the same aura's
// pct via synergyArmorPercentBonus). We verify two companions produce
// pct=0.20+0.05+0.05=0.30 (additive), NOT 0.20*1.05*1.05≈0.2205 (multiplicative).
func TestArmorPercent_StackingAdditive(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 58)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	if def == nil {
		t.Fatal("guardian_aura perk def not found")
	}

	// V1 with 2 companions (all within base radius of each other).
	v1 := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 200, Y: 400})
	v1.MaxHP = 500
	v1.HP = 500
	v1.Visible = true
	grantPerk(v1, "guardian_aura")

	comp1 := s.spawnPlayerUnitLocked("soldier", "p1", "#2980b9", protocol.Vec2{X: 240, Y: 400})
	comp1.MaxHP = 500
	comp1.HP = 500
	comp1.Visible = true
	grantPerk(comp1, "guardian_aura")

	comp2 := s.spawnPlayerUnitLocked("soldier", "p1", "#1a5276", protocol.Vec2{X: 220, Y: 430})
	comp2.MaxHP = 500
	comp2.HP = 500
	comp2.Visible = true
	grantPerk(comp2, "guardian_aura")

	// Recipient close to V1.
	recipient := s.spawnPlayerUnitLocked("soldier", "p1", "#e74c3c", protocol.Vec2{X: 210, Y: 400})
	recipient.Armor = 100
	recipient.MaxHP = 500
	recipient.HP = 500
	recipient.Visible = true

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[recipient.ID]
	if !ok {
		t.Fatal("recipient should be in aura cache")
	}

	// V1 has 2 companions → pct = 0.20 + 2*0.05 = 0.30 (additive).
	// Multiplicative would be 0.20 * (1.05)^2 ≈ 0.2205.
	wantPct := def.Config["armorPercent"] + 2*def.Config["synergyArmorPercentBonus"] // 0.30
	multiplicativePct := def.Config["armorPercent"] *
		(1 + def.Config["synergyArmorPercentBonus"]) *
		(1 + def.Config["synergyArmorPercentBonus"]) // ≈ 0.2205
	if math.Abs(aura.PercentArmor-wantPct) > 0.001 {
		t.Errorf("additive stacking: PercentArmor=%.4f, want %.4f (additive), multiplicative would be %.4f",
			aura.PercentArmor, wantPct, multiplicativePct)
	}
	// Verify effective armor uses additive formula.
	wantEffective := int(math.Floor(float64(100)*(1.0+wantPct))) + int(def.Config["bonusArmor"])+2*int(def.Config["synergyArmorBonus"])
	gotEffective := s.effectiveArmorLocked(recipient)
	if gotEffective != wantEffective {
		t.Errorf("additive stacking effectiveArmor: got %d, want %d", gotEffective, wantEffective)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// rallying_banner: cooldown mechanics
// ─────────────────────────────────────────────────────────────────────────────

// TestRallyingBanner_CooldownDecaysContinuously verifies that BannerCooldownRemaining
// decreases by exactly dt per tick regardless of movement state.
func TestRallyingBanner_CooldownDecaysContinuously(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")

	// Plant first banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted")
	}
	if vanguard.PerkState.BannerCooldownRemaining <= 0 {
		t.Fatal("BannerCooldownRemaining should be set after plant")
	}

	startCooldown := vanguard.PerkState.BannerCooldownRemaining

	// 3 stationary ticks.
	for i := 0; i < 3; i++ {
		vanguard.Moving = false
		s.tickUnitPerkStateLocked(vanguard, 0.05)
	}
	wantAfterStationary := startCooldown - 3*0.05
	if math.Abs(vanguard.PerkState.BannerCooldownRemaining-wantAfterStationary) > 0.001 {
		t.Errorf("cooldown after 3 stationary ticks: got %.3f, want %.3f",
			vanguard.PerkState.BannerCooldownRemaining, wantAfterStationary)
	}

	// 3 moving ticks — should keep decaying at same rate.
	for i := 0; i < 3; i++ {
		vanguard.Moving = true
		s.tickUnitPerkStateLocked(vanguard, 0.05)
	}
	wantAfterMoving := wantAfterStationary - 3*0.05
	if math.Abs(vanguard.PerkState.BannerCooldownRemaining-wantAfterMoving) > 0.001 {
		t.Errorf("cooldown after 3 moving ticks: got %.3f, want %.3f",
			vanguard.PerkState.BannerCooldownRemaining, wantAfterMoving)
	}
}

// TestRallyingBanner_CanReplantAfterCooldown verifies that once BannerCooldownRemaining
// reaches 0 and the unit stands stationary again, a new banner plants correctly.
func TestRallyingBanner_CanReplantAfterCooldown(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")

	// Plant first banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) == 0 {
		t.Fatal("first banner should be planted")
	}

	// Drain cooldown.
	vanguard.Moving = true
	remaining := vanguard.PerkState.BannerCooldownRemaining
	steps := int(math.Ceil(remaining/0.05)) + 2
	for i := 0; i < steps; i++ {
		s.tickUnitPerkStateLocked(vanguard, 0.05)
	}
	if vanguard.PerkState.BannerCooldownRemaining > 0.001 {
		t.Fatalf("cooldown should have drained; got %.3f", vanguard.PerkState.BannerCooldownRemaining)
	}

	// Now stand stationary — should plant a second banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) < 2 {
		t.Errorf("second banner should plant after cooldown expires; got %d banners", len(s.Banners))
	}
}

// TestRallyingBanner_RecipientGetsIcon verifies that an ally standing inside
// an active banner's radius receives "rallying_banner" in their activeBuffIcons.
func TestRallyingBanner_RecipientGetsIcon(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")

	// Plant a banner.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) == 0 {
		t.Fatal("banner should be planted")
	}

	bannerRadius := s.Banners[0].Radius

	// Ally inside banner radius.
	ally := spawnAlly(t, s, "p1", vanguard.X+bannerRadius*0.4, vanguard.Y)
	icons := iconIDs(s.activeBuffIconsLocked(ally))
	found := false
	for _, icon := range icons {
		if icon == "rallying_banner" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ally inside banner radius should have 'rallying_banner' buff icon; got %v", icons)
	}

	// Ally outside banner radius should NOT have the icon.
	allyOutside := spawnAlly(t, s, "p1", vanguard.X+bannerRadius*1.5, vanguard.Y)
	iconsOut := iconIDs(s.activeBuffIconsLocked(allyOutside))
	for _, icon := range iconsOut {
		if icon == "rallying_banner" {
			t.Errorf("ally outside banner radius should NOT have 'rallying_banner' buff icon")
			break
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// whirlwind_core — RNG-proc bonus AoE on attack
// ─────────────────────────────────────────────────────────────────────────────

// TestWhirlwindCore_ProcAppliesAoEAndArmsAnimTimer exercises the proc path:
// onPerkAttackFiredLocked rolls procChance each call, and on a proc it both
// applies applyWhirlwindHitLocked (full damage to every hostile in radius,
// excluding the primary target) and arms PerkState.WhirlwindAnimRemaining to
// animationSeconds so the client can overlay the spin animation.
//
// 50 attacks at the default 20% proc rate (seeded rngPerks) give effectively
// 1 - 0.8^50 ≈ 100% chance of at least one proc, making the test deterministic
// for any reasonable seed.
func TestWhirlwindCore_ProcAppliesAoEAndArmsAnimTimer(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "whirlwind_core")
	def := perkDefByID("whirlwind_core")
	if def == nil {
		t.Fatal("whirlwind_core perk def not found")
	}
	radius := def.Config["radius"]

	// Two enemies inside the whirlwind radius, one far-away primary target
	// that should not be counted in the AoE (applyWhirlwindHitLocked excludes it).
	nearA := spawnEnemy(t, s, vanguard.X+radius*0.5, vanguard.Y)
	nearB := spawnEnemy(t, s, vanguard.X-radius*0.5, vanguard.Y)
	farPrimary := spawnEnemy(t, s, vanguard.X+radius*3, vanguard.Y)
	hpA := nearA.HP
	hpB := nearB.HP

	procCount := 0
	var dead []int
	for i := 0; i < 50; i++ {
		before := vanguard.PerkState.WhirlwindAnimRemaining
		s.onPerkAttackFiredLocked(vanguard, farPrimary, 10, &dead)
		if vanguard.PerkState.WhirlwindAnimRemaining > before {
			procCount++
		}
	}

	if procCount == 0 {
		t.Fatalf("whirlwind_core did not proc in 50 attacks at procChance=%.2f; RNG or gate broken",
			def.Config["procChance"])
	}
	if nearA.HP >= hpA {
		t.Errorf("nearA HP did not drop after proc: %d → %d", hpA, nearA.HP)
	}
	if nearB.HP >= hpB {
		t.Errorf("nearB HP did not drop after proc: %d → %d", hpB, nearB.HP)
	}
	// No decay ran (we never called Update), so the timer equals
	// animationSeconds from the most recent proc.
	if vanguard.PerkState.WhirlwindAnimRemaining != def.Config["animationSeconds"] {
		t.Errorf("WhirlwindAnimRemaining after last proc: got %.3f, want %.3f",
			vanguard.PerkState.WhirlwindAnimRemaining, def.Config["animationSeconds"])
	}
}

// TestWhirlwindCore_AnimTimerDecaysWithTicks verifies the per-tick decay in
// perks.go advances WhirlwindAnimRemaining down to zero. Uses the public
// tickUnitPerksLocked path by calling Update with a known dt.
func TestWhirlwindCore_AnimTimerDecaysWithTicks(t *testing.T) {
	s, vanguard := newGoldPerkState(t)

	s.mu.Lock()
	grantPerk(vanguard, "whirlwind_core")
	vanguard.PerkState.WhirlwindAnimRemaining = 1.0
	s.mu.Unlock()

	// 30 ticks at dt=0.05 = 1.5s, well past animationSeconds=1.0.
	for i := 0; i < 30; i++ {
		s.Update(0.05)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if vanguard.PerkState.WhirlwindAnimRemaining != 0 {
		t.Errorf("WhirlwindAnimRemaining should decay to 0 after 1.5s of ticks; got %.3f",
			vanguard.PerkState.WhirlwindAnimRemaining)
	}
}

// TestWhirlwindCore_NoProcDoesNotArmTimer is a sanity check that when no proc
// fires (procChance=0 via direct state manipulation of the rngPerks path is
// awkward, so we instead call the AoE applier directly and verify it does
// NOT arm the animation timer on its own — only onPerkAttackFiredLocked does).
func TestWhirlwindCore_NoProcDoesNotArmTimer(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "whirlwind_core")
	def := perkDefByID("whirlwind_core")
	enemy := spawnEnemy(t, s, vanguard.X+def.Config["radius"]*0.5, vanguard.Y)
	hpBefore := enemy.HP

	// Call the applier without going through the proc gate.
	var dead []int
	s.applyWhirlwindHitLocked(vanguard, nil, def.Config["radius"], &dead)

	if enemy.HP >= hpBefore {
		t.Errorf("applyWhirlwindHitLocked did not damage enemy inside radius")
	}
	if vanguard.PerkState.WhirlwindAnimRemaining != 0 {
		t.Errorf("animation timer should only be armed by onPerkAttackFiredLocked, not the applier; got %.3f",
			vanguard.PerkState.WhirlwindAnimRemaining)
	}
}
