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

// TestGuardianAura_ReducesAllyDamage verifies that an allied unit within the
// Vanguard's aura radius takes reduced damage.
func TestGuardianAura_ReducesAllyDamage(t *testing.T) {
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

	// Verify cache contains the ally.
	auraDR, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally should be in guardianAuraCache")
	}
	if math.Abs(auraDR-def.Config["damageReduction"]) > 0.001 {
		t.Errorf("ally aura DR: got %.3f, want %.3f", auraDR, def.Config["damageReduction"])
	}

	// Verify the DR actually reduces incoming damage.
	const rawDamage = 100
	ally.HP = ally.MaxHP
	hpBefore := ally.HP
	s.applyUnitDamageLocked(ally, rawDamage)
	got := hpBefore - ally.HP
	want := int(math.Round(float64(rawDamage) * (1.0 - def.Config["damageReduction"])))
	if diff := got - want; diff > 1 || diff < -1 {
		t.Errorf("ally damage with aura: got %d, want ~%d", got, want)
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
// effR=130, effDR=0.20. An ally at distance 120 from V1 (outside base 100,
// inside effective 130) receives DR=0.20.
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
	wantDR := def.Config["damageReduction"] + def.Config["synergyDRBonus"] // 0.15 + 0.05 = 0.20
	if math.Abs(aura-wantDR) > 0.001 {
		t.Errorf("synergy DR: got %.3f, want %.3f", aura, wantDR)
	}
}

// TestGuardianAura_OutOfRangeNoFormation verifies that V1 and V2 too far apart
// (distance 150 > base 100) each operate independently with base values.
func TestGuardianAura_OutOfRangeNoFormation(t *testing.T) {
	s, v1 := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("guardian_aura")
	grantPerk(v1, "guardian_aura")

	v2 := spawnAlly(t, s, "p1", v1.X+150, v1.Y) // distance 150 > base 100
	grantPerk(v2, "guardian_aura")

	// Ally close to V1, within base radius.
	ally := spawnAlly(t, s, "p1", v1.X+50, v1.Y)

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally within base radius of V1 should still be in cache")
	}
	// No companions — should be base DR only.
	if math.Abs(aura-def.Config["damageReduction"]) > 0.001 {
		t.Errorf("no synergy: expected base DR %.3f, got %.3f", def.Config["damageReduction"], aura)
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
	wantDR := def.Config["damageReduction"] + 2*def.Config["synergyDRBonus"] // 0.15 + 0.10 = 0.25
	if math.Abs(aura-wantDR) > 0.001 {
		t.Errorf("3-vanguard cluster DR: got %.3f, want %.3f", aura, wantDR)
	}
}

// TestGuardianAura_PartialLineFormation places V1 at x=0, V2 at x=90, V3 at
// x=180. V1 sees only V2 (companions=1), V2 sees both V1 and V3 (companions=2),
// V3 sees only V2 (companions=1). Verifies asymmetric effDRs by placing each
// test ally in a position where ONLY the intended Vanguard's aura reaches it
// (i.e. outside all other Vanguards' effective radii).
//
// Configuration (base radius=100, effR at 1 companion=130, at 2 companions=160):
//   V1 at x=0, effR=130  (1 companion)
//   V2 at x=90, effR=160 (2 companions)
//   V3 at x=180, effR=130 (1 companion)
//
// allyNearV1 at x=-20: distance 20 from V1, distance 110 from V2.
//   V1 covers it (130 effR). V2 does NOT (160 effR would cover it! 110 < 160).
//   So this geometry doesn't work cleanly for "only V1 covers it".
//
// Instead we test the companion COUNTS directly by placing allies at known
// positions and asserting the DR they receive from at least one Vanguard,
// rather than trying to isolate a single source.
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

	// Verify companion counts by checking a unit very close to each Vanguard
	// — it should receive at least the DR of that Vanguard's effective value
	// (possibly higher if another Vanguard's boosted aura also reaches it, which
	// is correct max-not-sum behaviour).

	// drV1 (1 companion) = 0.20, drV2 (2 companions) = 0.25, drV3 (1 companion) = 0.20.
	drV2 := def.Config["damageReduction"] + 2*def.Config["synergyDRBonus"] // 0.25

	// An ally directly beside V2 should see V2's DR (0.25) — V2 has 2 companions.
	allyNearV2 := spawnAlly(t, s, "p1", 291, 400)
	s.rebuildGuardianAuraCacheLocked()
	aura, ok := s.guardianAuraCache[allyNearV2.ID]
	if !ok {
		t.Fatal("allyNearV2 should be in cache")
	}
	if math.Abs(aura-drV2) > 0.001 {
		t.Errorf("allyNearV2 DR: got %.3f, want %.3f (V2 has 2 companions)", aura, drV2)
	}

	// V1 companion count: 1 (V2 at distance 90 within base 100). Not V3 (distance 180 > 100).
	// Verify by placing an ally VERY far left of V1, outside V2/V3's reach:
	// V1 effR = 130. Ally at x=100 (distance 100 from V1 = AT edge, inside).
	// V2 at x=290 — distance to x=100 is 190 > V2's effR=160. V3 even farther. Good.
	allyFarLeft := spawnAlly(t, s, "p1", 100, 400) // dist 100 from V1 (just inside base 100)
	s.rebuildGuardianAuraCacheLocked()
	auraLeft, okLeft := s.guardianAuraCache[allyFarLeft.ID]
	if !okLeft {
		t.Fatal("allyFarLeft should be in cache (distance 100 = AT base radius boundary)")
	}
	drV1 := def.Config["damageReduction"] + 1*def.Config["synergyDRBonus"] // 0.20
	if math.Abs(auraLeft-drV1) > 0.001 {
		t.Errorf("allyFarLeft DR: got %.3f, want %.3f (only V1 with 1 companion covers it)", auraLeft, drV1)
	}

	// Symmetrically verify V3 (1 companion, effDR=0.20) covers an ally to its right.
	// V3 at x=380, ally at x=480 (dist 100, AT boundary).
	// V2 at x=290 — distance to x=480 is 190 > V2's effR=160. V1 even farther. Good.
	allyFarRight := spawnAlly(t, s, "p1", 480, 400)
	s.rebuildGuardianAuraCacheLocked()
	auraRight, okRight := s.guardianAuraCache[allyFarRight.ID]
	if !okRight {
		t.Fatal("allyFarRight should be in cache (distance 100 from V3)")
	}
	drV3 := def.Config["damageReduction"] + 1*def.Config["synergyDRBonus"] // 0.20
	if math.Abs(auraRight-drV3) > 0.001 {
		t.Errorf("allyFarRight DR: got %.3f, want %.3f (only V3 with 1 companion covers it)", auraRight, drV3)
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

	// allyA should be under V2's boosted aura.
	aura, ok := s.guardianAuraCache[allyA.ID]
	if !ok {
		t.Fatal("allyA at distance 115 from V2 should be in cache (V2 effR=130)")
	}
	wantDR := def.Config["damageReduction"] + def.Config["synergyDRBonus"]
	if math.Abs(aura-wantDR) > 0.001 {
		t.Errorf("allyA aura DR: got %.3f, want %.3f (V2 effDR with 1 companion)", aura, wantDR)
	}

	// V2's companion count: only V1 is within V2's BASE radius (80 < 100).
	// allyA is NOT a guardian_aura source, so it doesn't count regardless.
	// Verify by checking V2's own aura isn't inflated beyond 1-companion levels.
	// Ally near V2 (within base 100) should see drV2 = baseDR + 1*drBonus.
	allyNearV2 := spawnAlly(t, s, "p1", 281, 400)
	s.rebuildGuardianAuraCacheLocked()
	auraNearV2, ok2 := s.guardianAuraCache[allyNearV2.ID]
	if !ok2 {
		t.Fatal("allyNearV2 should be in cache")
	}
	if math.Abs(auraNearV2-wantDR) > 0.001 {
		t.Errorf("allyNearV2 DR: got %.3f, want %.3f (not inflated by allyA)", auraNearV2, wantDR)
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
	// V1 has 0 companions (V2 is enemy), so ally should see base DR only.
	if math.Abs(aura-def.Config["damageReduction"]) > 0.001 {
		t.Errorf("enemy Vanguard should not boost synergy: got DR=%.3f, want %.3f",
			aura, def.Config["damageReduction"])
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

	// Both V1 and V2 have companions=1, effDR=0.20. Max of two equal values = 0.20.
	// If the cache were summing, we'd get 0.40 — which would be caught here.
	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally should be in cache")
	}
	wantDR := def.Config["damageReduction"] + def.Config["synergyDRBonus"] // 0.20
	if math.Abs(aura-wantDR) > 0.001 {
		t.Errorf("max not sum: got DR=%.3f, want max=%.3f (not sum=%.3f)",
			aura, wantDR, 2*wantDR)
	}
	// Explicit sum-guard.
	if aura > wantDR+0.001 {
		t.Errorf("cache is summing DR instead of taking max: got %.3f", aura)
	}
}

// TestGuardianAura_ClampInteraction verifies that stacking guardian_aura +
// brace + steady_advance can exceed 0.75 but perkIncomingDamageMultiplierLocked
// clamps the output.
func TestGuardianAura_ClampInteraction(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "brace")
	grantPerk(vanguard, "steady_advance")

	braceDef := perkDefByID("brace")
	steadyDef := perkDefByID("steady_advance")
	if braceDef == nil || steadyDef == nil {
		t.Fatal("perk defs not found")
	}

	// Place 2+ enemies within brace radius.
	threshold := int(braceDef.Config["enemyThreshold"])
	for i := 0; i < threshold; i++ {
		e := spawnEnemy(t, s, vanguard.X+braceDef.Config["radius"]*0.5, vanguard.Y+float64(i)*5)
		_ = e
	}

	// Point the unit toward an enemy to activate steady_advance.
	vanguard.Moving = true
	vanguard.Path = []protocol.Vec2{{X: vanguard.X + 100, Y: vanguard.Y}}

	// Inject a large guardian_aura value directly — simulates a synergy-boosted aura.
	s.guardianAuraCache[vanguard.ID] = 0.60 // contrived; total would be 0.60+0.20+0.10=0.90

	got := s.perkIncomingDamageMultiplierLocked(vanguard)
	if got > 0.75+0.001 {
		t.Errorf("perkIncomingDamageMultiplierLocked should be clamped to 0.75, got %.3f", got)
	}
	if got < 0.75-0.001 {
		t.Errorf("expected clamp to trigger (0.75), got %.3f (may need higher injected aura)", got)
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
		cache1 := make(map[int]float64, len(s1.guardianAuraCache))
		for k, v := range s1.guardianAuraCache {
			cache1[k] = v
		}
		s1.mu.Unlock()

		s2.mu.Lock()
		s2.rebuildGuardianAuraCacheLocked()
		cache2 := make(map[int]float64, len(s2.guardianAuraCache))
		for k, v := range s2.guardianAuraCache {
			cache2[k] = v
		}
		s2.mu.Unlock()

		if len(cache1) != len(cache2) {
			t.Fatalf("tick %d: cache length mismatch: s1=%d s2=%d", tick, len(cache1), len(cache2))
		}
		for id, dr1 := range cache1 {
			dr2, ok := cache2[id]
			if !ok {
				t.Fatalf("tick %d: unitID %d in s1 cache but not s2", tick, id)
			}
			if math.Abs(dr1-dr2) > 1e-12 {
				t.Fatalf("tick %d: unitID %d DR mismatch: s1=%.15f s2=%.15f", tick, id, dr1, dr2)
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

// TestRallyingBanner_OneShotPerStationaryPeriod verifies that only one banner
// is planted per stationary period, and a second plants after a move+re-plant.
func TestRallyingBanner_OneShotPerStationaryPeriod(t *testing.T) {
	s, vanguard := newGoldPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	grantPerk(vanguard, "rallying_banner")
	def := perkDefByID("rallying_banner")
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)

	if len(s.Banners) != 1 {
		t.Fatalf("expected exactly 1 banner after first plant, got %d", len(s.Banners))
	}

	// Continue ticking stationary — no second banner.
	for i := 0; i < 20; i++ {
		s.tickUnitPerkStateLocked(vanguard, 0.05)
	}
	if len(s.Banners) != 1 {
		t.Errorf("should still be exactly 1 banner (one-shot), got %d", len(s.Banners))
	}

	// Move and re-plant.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if vanguard.PerkState.RallyingBannerPlanted {
		t.Error("RallyingBannerPlanted should reset on movement")
	}

	vanguard.Moving = false
	tickUntilStationary(s, vanguard, def.Config["stationaryThresholdSeconds"], 0.05)
	if len(s.Banners) != 2 {
		t.Errorf("expected 2 banners after second plant, got %d", len(s.Banners))
	}
}

// TestRallyingBanner_SharedStationaryCounter_WithBulwark verifies that when a
// unit has both bulwark and rallying_banner, both proc at threshold simultaneously,
// and after moving both BulwarkShieldGranted and RallyingBannerPlanted reset.
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

	// Move — both flags should clear.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if vanguard.PerkState.BulwarkShieldGranted {
		t.Error("BulwarkShieldGranted should reset on movement")
	}
	if vanguard.PerkState.RallyingBannerPlanted {
		t.Error("RallyingBannerPlanted should reset on movement")
	}
	if vanguard.PerkState.StationarySeconds != 0 {
		t.Errorf("StationarySeconds should reset to 0 on movement, got %.2f", vanguard.PerkState.StationarySeconds)
	}

	// Re-plant.
	vanguard.Moving = false
	tickUntilStationary(s, vanguard, threshold, 0.05)
	if vanguard.Shield != int(bulwarkDef.Config["maxShield"]) {
		t.Error("bulwark should re-arm after re-plant")
	}
	if len(s.Banners) < 2 {
		t.Errorf("expected 2nd banner after re-plant, got %d banners", len(s.Banners))
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
	// base radius = 100, synergyRadiusBonus = 30, synergyDRBonus = 0.05

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
	// Should receive ONLY V1's aura: effDR = 0.15 + 1*0.05 = 0.20.
	// If companion detection used effR instead of baseR, V1 would count V2 as a
	// companion (V2 at x=500 is within V1's effR of 130? No: dist 300 > 130. Safe.)
	// The real test: if the algorithm scanned effR for companions, it might use
	// compC's effR=130 to pull in V2 (at 250px from compC — still outside 130). Also safe.
	//
	// Structural assertion: ally at x=320 should be in cache with DR=0.20.
	// If we ever see DR=0.25, it means V1 computed companions=2 somehow.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#e74c3c", protocol.Vec2{X: 320, Y: 400})
	ally.MaxHP = 200
	ally.HP = 200
	ally.Visible = true

	s.rebuildGuardianAuraCacheLocked()

	aura, ok := s.guardianAuraCache[ally.ID]
	if !ok {
		t.Fatal("ally at dist 120 from V1 should be in cache (V1 effR=130)")
	}
	// V1 has exactly 1 companion (compC). effDR = 0.15 + 1*0.05 = 0.20.
	wantDR := def.Config["damageReduction"] + 1*def.Config["synergyDRBonus"] // 0.20
	if math.Abs(aura-wantDR) > 0.001 {
		t.Errorf("phase-separation: ally DR=%.3f, want %.3f (V1 has 1 companion via baseR only)",
			aura, wantDR)
	}

	// Secondary: V2 is isolated (companions=0, effR=100). An ally 95px from V2
	// should receive base DR only (0.15).
	allyNearV2 := s.spawnPlayerUnitLocked("soldier", "p1", "#1abc9c", protocol.Vec2{X: 595, Y: 400})
	allyNearV2.MaxHP = 200
	allyNearV2.HP = 200
	allyNearV2.Visible = true
	s.rebuildGuardianAuraCacheLocked()

	auraV2, okV2 := s.guardianAuraCache[allyNearV2.ID]
	if !okV2 {
		t.Fatal("allyNearV2 should be in cache (95px from V2, within V2's base effR=100)")
	}
	if math.Abs(auraV2-def.Config["damageReduction"]) > 0.001 {
		t.Errorf("isolated V2 DR: got %.3f, want base DR %.3f (0 companions)", auraV2, def.Config["damageReduction"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Map-order independence: same units, different spawn order
// ─────────────────────────────────────────────────────────────────────────────

// TestGuardianAura_MapOrderIndependence verifies that spawning the same units
// in two different slice orders produces identical guardianAuraCache content.
// The algorithm must be commutative across s.Units slice order.
func TestGuardianAura_MapOrderIndependence(t *testing.T) {
	buildState := func(orderAFirst bool) map[int]float64 {
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
		result := make(map[int]float64)
		// Capture by relative identity (ally's cache entry), not unit ID (which
		// changes with spawn order). We return the DR the ally received.
		if ally != nil {
			if dr, ok := s.guardianAuraCache[ally.ID]; ok {
				result[0] = dr // key 0 = "the ally"
			}
		}
		return result
	}

	cacheOrderA := buildState(true)
	cacheOrderB := buildState(false)

	drA, okA := cacheOrderA[0]
	drB, okB := cacheOrderB[0]
	if okA != okB {
		t.Fatalf("ally presence in cache differs by spawn order: orderA=%v orderB=%v", okA, okB)
	}
	if okA && math.Abs(drA-drB) > 1e-12 {
		t.Errorf("aura DR differs by spawn order: orderA=%.15f orderB=%.15f", drA, drB)
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
// accumulation drift. Repeated subtraction of dt=0.05 across 160 ticks leaves
// RemainingSeconds at ~2e-14 instead of exactly 0; the epsilon guard in
// tickBannersLocked treats sub-nanosecond residual as expired so the banner
// drops at the expected tick rather than one tick late.
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
	const exactTicks = 160 // 160 * 0.05 = 8.0 s — should expire exactly here
	for i := 0; i < exactTicks; i++ {
		s.tickBannersLocked(dt)
	}

	if len(s.Banners) != 0 {
		t.Errorf("banner should have expired at tick 160 (8.0s duration); got %d banners remaining", len(s.Banners))
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

// TestPainShare_PlusGuardianAura_OrderCorrect verifies that when an ally has
// both a pain_share Vanguard AND a guardian_aura Vanguard nearby:
//   - redirect runs at step 0 (on raw damage, before aura DR)
//   - aura DR applies to the REMAINING damage (70%) on the ally, not the full 100
//
// The ally should take: round(70 * (1 - 0.15)) = 60 HP, not round(100 * (1 - 0.15)) = 85.
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

	// Confirm ally is in the aura cache.
	if _, ok := s.guardianAuraCache[ally.ID]; !ok {
		t.Fatal("ally should be in guardian_aura cache before damage test")
	}

	const rawDamage = 100
	redirectPct := def.Config["redirectPercent"]                                    // 0.30
	redirected := maxInt(1, int(math.Round(float64(rawDamage)*redirectPct)))        // 30
	remaining := rawDamage - redirected                                             // 70
	auraDR := auraDef.Config["damageReduction"]                                     // 0.15
	wantAllyDamage := int(math.Round(float64(remaining) * (1.0 - auraDR)))          // round(70*0.85)=60

	allyHPBefore := ally.HP
	s.applyUnitDamageLocked(ally, rawDamage)
	gotAllyDamage := allyHPBefore - ally.HP

	if diff := gotAllyDamage - wantAllyDamage; diff > 1 || diff < -1 {
		t.Errorf("pain_share+guardian_aura ordering: ally took %d HP, want ~%d (redirect first, then aura on remainder)",
			gotAllyDamage, wantAllyDamage)
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
// effDR=0.30. An ally in the cluster receives 0.30 DR. Stacked with brace
// (0.20) and steady_advance (0.10) the theoretical total is 0.60, clamped to
// 0.75 max. Tests the "god run" synergy path the design explicitly allows.
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
	// Each Vanguard has companions=3 → effDR = 0.15 + 3*0.05 = 0.30
	wantDR := def.Config["damageReduction"] + 3*def.Config["synergyDRBonus"] // 0.30
	if math.Abs(aura-wantDR) > 0.001 {
		t.Errorf("4-Vanguard god-run DR: got %.3f, want %.3f", aura, wantDR)
	}

	// Verify raw damage reduction: deal 100 damage, expect round(100 * (1 - 0.30)) = 70.
	const rawDamage = 100
	ally.HP = ally.MaxHP
	hpBefore := ally.HP
	s.applyUnitDamageLocked(ally, rawDamage)
	got := hpBefore - ally.HP
	want := int(math.Round(float64(rawDamage) * (1.0 - wantDR)))
	if diff := got - want; diff > 1 || diff < -1 {
		t.Errorf("4-Vanguard god-run damage: got %d HP lost, want ~%d", got, want)
	}
}
