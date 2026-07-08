package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// setArchMagePoolForTest temporarily installs a bronze pool for the arch_mage
// archetype (the embedded catalog ships it empty until Group 10) and restores
// the prior state on cleanup. Members must be REGISTERED abilities.
func setArchMagePoolForTest(t *testing.T, bronze []string) {
	t.Helper()
	prev, had := spellPoolsByArchetype["arch_mage"]
	spellPoolsByArchetype["arch_mage"] = map[string][]string{"bronze": bronze}
	t.Cleanup(func() {
		if had {
			spellPoolsByArchetype["arch_mage"] = prev
		} else {
			delete(spellPoolsByArchetype, "arch_mage")
		}
	})
}

// makeArchMage spawns a unit already promoted onto the arch_mage path at the
// given rank (bypassing XP — this exercises the roll/recompute directly).
func makeArchMage(s *GameState, x, y float64, rank string) *Unit {
	u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: x, Y: y})
	u.ProgressionPath = "arch_mage"
	u.Rank = rank
	return u
}

func TestSpellPoolRoll_AssignsOneAndRecomputeIncludes(t *testing.T) {
	setArchMagePoolForTest(t, []string{"arcane_bolt", "heal", "greater_heal"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")

	s.rollUnitPoolSpellsLocked(u)
	pick := u.PoolSpellsByRank["bronze"]
	if pick != "arcane_bolt" && pick != "heal" && pick != "greater_heal" {
		t.Fatalf("bronze pick = %q; want one of the pool", pick)
	}
	s.assignUnitPathAbilitiesLocked(u)
	if !containsString(u.Abilities, pick) {
		t.Errorf("recompute Abilities %v missing the rolled pick %q", u.Abilities, pick)
	}
}

func TestSpellPoolRoll_ExcludesKnown(t *testing.T) {
	setArchMagePoolForTest(t, []string{"heal", "arcane_bolt"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")
	u.Abilities = []string{"heal"} // already knows heal

	s.rollUnitPoolSpellsLocked(u)
	if got := u.PoolSpellsByRank["bronze"]; got != "arcane_bolt" {
		t.Errorf("pick = %q; want arcane_bolt (heal is already known and excluded)", got)
	}
}

func TestSpellPoolRoll_DeterministicAndPerUnit(t *testing.T) {
	pool := []string{"arcane_bolt", "heal", "greater_heal"}

	roll := func() []string {
		setArchMagePoolForTest(t, pool)
		s := newProjectileTestState(t) // fixed seed 42
		s.mu.Lock()
		defer s.mu.Unlock()
		picks := make([]string, 0, 30)
		for i := 0; i < 30; i++ {
			u := makeArchMage(s, float64(i*20), 100, "bronze")
			s.rollUnitPoolSpellsLocked(u)
			picks = append(picks, u.PoolSpellsByRank["bronze"])
		}
		return picks
	}

	a, b := roll(), roll()
	if len(a) != len(b) {
		t.Fatalf("length mismatch %d vs %d", len(a), len(b))
	}
	distinct := map[string]bool{}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d: %q vs %q", i, a[i], b[i])
		}
		distinct[a[i]] = true
	}
	if len(distinct) < 2 {
		t.Errorf("expected per-unit heterogeneity (>=2 distinct picks across 30 units); got %v", distinct)
	}
}

func TestSpellPoolRoll_IdempotentNoRedraw(t *testing.T) {
	setArchMagePoolForTest(t, []string{"arcane_bolt", "heal", "greater_heal"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")

	s.rollUnitPoolSpellsLocked(u)
	first := u.PoolSpellsByRank["bronze"]
	s.rollUnitPoolSpellsLocked(u) // must NOT re-roll
	if u.PoolSpellsByRank["bronze"] != first {
		t.Errorf("re-roll changed the pick: %q → %q", first, u.PoolSpellsByRank["bronze"])
	}
	s.assignUnitPathAbilitiesLocked(u)
	ab1 := append([]string(nil), u.Abilities...)
	s.assignUnitPathAbilitiesLocked(u) // idempotent recompute
	if len(ab1) != len(u.Abilities) {
		t.Fatalf("recompute not idempotent: %v vs %v", ab1, u.Abilities)
	}
	// the pick appears exactly once
	count := 0
	for _, id := range u.Abilities {
		if id == first {
			count++
		}
	}
	if count != 1 {
		t.Errorf("pick %q appears %d times; want exactly 1", first, count)
	}
}

func TestSpellPoolRoll_ExhaustedPoolAssignsNothing(t *testing.T) {
	setArchMagePoolForTest(t, []string{"heal"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")
	u.Abilities = []string{"heal"} // the only candidate is already known

	s.rollUnitPoolSpellsLocked(u)
	if _, ok := u.PoolSpellsByRank["bronze"]; ok {
		t.Errorf("exhausted pool recorded %q; want nothing", u.PoolSpellsByRank["bronze"])
	}
}

// An empty/exhausted pool must consume NO RNG (determinism: it must not perturb
// the rngPerks stream). Roll with an empty pool then draw; compare to a control
// state that only draws.
func TestSpellPoolRoll_EmptyPoolDrawsNoRNG(t *testing.T) {
	setArchMagePoolForTest(t, []string{}) // empty bronze pool

	withRoll := newProjectileTestState(t)
	control := newProjectileTestState(t)
	withRoll.mu.Lock()
	control.mu.Lock()
	defer withRoll.mu.Unlock()
	defer control.mu.Unlock()

	u := makeArchMage(withRoll, 100, 100, "bronze")
	uc := makeArchMage(control, 100, 100, "bronze")
	_ = uc
	withRoll.rollUnitPoolSpellsLocked(u) // empty pool ⇒ should draw nothing

	if got, want := withRoll.rngPerks.Float64(), control.rngPerks.Float64(); got != want {
		t.Errorf("empty-pool roll perturbed the RNG stream: %v vs control %v", got, want)
	}
}
