package game

// Section 15 — Determinism tests for all four Cleric Bronze perks.
//
// Each test runs the same seeded scenario twice and asserts that per-tick
// observable state (buff values, mana totals, damage applied, target IDs)
// is bitwise-identical between the two runs.
//
// The pattern mirrors TestShieldBash_Determinism in bronze_perks_test.go:
// two setups, same seed, same command sequence, compare leaf values.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// 15.1 — battle_prayer buff applications are identical across replays
// ─────────────────────────────────────────────────────────────────────────────

// TestDeterminism_BattlePrayerBuffApplicationsAcrossReplays runs two identical
// seeded states through the same heal sequence and asserts per-tick
// BattlePrayerRemaining is identical.
func TestDeterminism_BattlePrayerBuffApplicationsAcrossReplays(t *testing.T) {
	const seed = int64(1234)

	type record struct {
		remaining  float64
		multiplier float64
	}

	runScenario := func() record {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()

		cleric := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		cleric.Visible = true
		cleric.HP = cleric.MaxHP
		cleric.AttackRange = 1000
		cleric.MaxMana = 200
		cleric.CurrentMana = 200
		if cleric.AutoCastEnabled == nil {
			cleric.AutoCastEnabled = make(map[string]bool)
		}
		if cleric.AbilityCooldowns == nil {
			cleric.AbilityCooldowns = make(map[string]float64)
		}

		grantPerk(cleric, "battle_prayer")

		ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 450, Y: 400})
		ally.MaxHP = 500
		ally.HP = 200
		ally.Visible = true
		allyID := ally.ID

		bpDef := perkDefByID("battle_prayer")
		if bpDef == nil {
			s.mu.Unlock()
			t.Fatal("battle_prayer perk def not found")
		}

		healAbilityDef, _ := getAbilityDef("heal")
		s.onPerkAbilityResolvedLocked(cleric, healAbilityDef, ally)

		a := s.unitsByID[allyID]
		rec := record{
			remaining:  a.PerkState.BattlePrayerRemaining,
			multiplier: a.PerkState.BattlePrayerMultiplier,
		}
		s.mu.Unlock()

		// Advance a few ticks.
		for i := 0; i < 10; i++ {
			s.Update(0.05)
		}

		s.mu.RLock()
		a = s.unitsByID[allyID]
		if a != nil {
			rec.remaining = a.PerkState.BattlePrayerRemaining
			rec.multiplier = a.PerkState.BattlePrayerMultiplier
		}
		s.mu.RUnlock()
		return rec
	}

	r1 := runScenario()
	r2 := runScenario()

	if r1.remaining != r2.remaining {
		t.Errorf("BattlePrayerRemaining diverged: %.6f vs %.6f", r1.remaining, r2.remaining)
	}
	if r1.multiplier != r2.multiplier {
		t.Errorf("BattlePrayerMultiplier diverged: %.6f vs %.6f", r1.multiplier, r2.multiplier)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 15.2 — mana_conduit bonus is identical across replays
// ─────────────────────────────────────────────────────────────────────────────

// TestDeterminism_ManaConduitBonusAcrossReplays runs a seeded mana_conduit
// scenario twice and asserts identical CurrentMana after N ticks.
func TestDeterminism_ManaConduitBonusAcrossReplays(t *testing.T) {
	const seed = int64(5678)

	runScenario := func() (mana int, accumulator float64) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()

		cleric := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		cleric.Visible = true
		cleric.HP = cleric.MaxHP
		cleric.MaxMana = 200
		cleric.CurrentMana = 10 // start low so gains are visible
		cleric.ManaRegenAccumulator = 0
		grantPerk(cleric, "mana_conduit")
		clericID := cleric.ID

		mcDef := perkDefByID("mana_conduit")
		if mcDef == nil {
			s.mu.Unlock()
			t.Fatal("mana_conduit perk def not found")
		}
		radius := mcDef.Config["radiusPixels"]

		// Place two injured allies in radius.
		for i := 0; i < 2; i++ {
			a := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{
				X: cleric.X + radius*0.4 + float64(i*10),
				Y: cleric.Y,
			})
			a.MaxHP = 500
			a.HP = 250
			a.Visible = true
		}
		s.mu.Unlock()

		const ticks = 20
		for i := 0; i < ticks; i++ {
			s.Update(0.05)
		}

		s.mu.RLock()
		c := s.unitsByID[clericID]
		mana = c.CurrentMana
		accumulator = c.ManaRegenAccumulator
		s.mu.RUnlock()
		return mana, accumulator
	}

	m1, a1 := runScenario()
	m2, a2 := runScenario()

	if m1 != m2 {
		t.Errorf("CurrentMana diverged: %d vs %d", m1, m2)
	}
	if math.Abs(a1-a2) > 1e-9 {
		t.Errorf("ManaRegenAccumulator diverged: %.10f vs %.10f", a1, a2)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 15.3 — sanctuary mitigation is identical across replays
// ─────────────────────────────────────────────────────────────────────────────

// TestDeterminism_SanctuaryMitigationAcrossReplays runs a projectile hit through
// a Sanctuary aura twice and asserts identical applied damage both times.
func TestDeterminism_SanctuaryMitigationAcrossReplays(t *testing.T) {
	const seed = int64(9012)

	runScenario := func() int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		defer s.mu.Unlock()

		cleric := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		cleric.Visible = true
		grantPerk(cleric, "sanctuary")

		sanctuaryDef := perkDefByID("sanctuary")
		if sanctuaryDef == nil {
			t.Fatal("sanctuary perk def not found")
		}
		radius := sanctuaryDef.Config["radiusPixels"]

		ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{
			X: cleric.X + radius*0.5,
			Y: cleric.Y,
		})
		ally.MaxHP = 500
		ally.HP = 500
		ally.Armor = 0
		ally.Visible = true
		startHP := ally.HP

		s.applyUnitDamageWithSourceLocked(ally, 100, DamageSource{Kind: "projectile"})
		return startHP - ally.HP
	}

	d1 := runScenario()
	d2 := runScenario()

	if d1 != d2 {
		t.Errorf("sanctuary damage diverged: %d vs %d", d1, d2)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 15.4 — greater_heal target set is identical across replays
// ─────────────────────────────────────────────────────────────────────────────

// TestDeterminism_GreaterHealTargetSetAcrossReplays runs greater_heal target
// selection twice and asserts the sorted target-ID slice is identical.
func TestDeterminism_GreaterHealTargetSetAcrossReplays(t *testing.T) {
	const seed = int64(3456)

	type targetSet struct {
		ids []int
	}

	runScenario := func() targetSet {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()
		defer s.mu.Unlock()

		cleric := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		cleric.Visible = true
		cleric.HP = cleric.MaxHP
		cleric.AttackRange = 1000
		cleric.MaxMana = 200
		cleric.CurrentMana = 200

		if len(cleric.Abilities) > 0 && cleric.Abilities[0] == "heal" {
			// Grant-pipeline test: promote to (cleric, bronze) so the
			// path-ability grant runs the heal → greater_heal swap.
			promoteToBronzeCleric(s, cleric)
		}

		def, ok := getAbilityDef("greater_heal")
		if !ok {
			t.Fatal("greater_heal def not found")
		}
		if def.TargetCount < 2 {
			t.Skipf("greater_heal TargetCount = %d; need >= 2 for this test", def.TargetCount)
		}

		// Five allies with distinct HP% values. Tie in HP% between two of them.
		hpPcts := []int{30, 30, 50, 70, 80} // two at 30% — tie broken by ID
		allies := make([]*Unit, len(hpPcts))
		for i, pct := range hpPcts {
			a := s.spawnPlayerUnitLocked("soldier", "p1", "#aabb00", protocol.Vec2{
				X: 430 + float64(i*10),
				Y: 400,
			})
			a.MaxHP = 100
			a.HP = pct
			a.Visible = true
			allies[i] = a
		}

		primary := allies[0]
		targets := s.buildCastTargetSetLocked(cleric, def, primary)

		ids := make([]int, len(targets))
		for i, tgt := range targets {
			ids[i] = tgt.ID
		}
		return targetSet{ids: ids}
	}

	ts1 := runScenario()
	ts2 := runScenario()

	if len(ts1.ids) != len(ts2.ids) {
		t.Fatalf("target set size diverged: %d vs %d", len(ts1.ids), len(ts2.ids))
	}
	for i := range ts1.ids {
		if ts1.ids[i] != ts2.ids[i] {
			t.Errorf("target set diverged at index %d: %d vs %d", i, ts1.ids[i], ts2.ids[i])
		}
	}
}
