package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// setArchMagePoolForTest temporarily overrides the arch_mage path's bronze
// ability pool (authored on arch_mage.json in production) and restores the
// prior state on cleanup. Members must be REGISTERED abilities. Mutates the
// path-catalog global directly (pathAbilityPoolsByPath, path_defs.go) under
// pathCatalogMu, mirroring how abilityPoolFor now reads pool data.
func setArchMagePoolForTest(t *testing.T, bronze []string) {
	t.Helper()
	pathCatalogMu.Lock()
	prev, had := pathAbilityPoolsByPath["arch_mage"]
	pathAbilityPoolsByPath["arch_mage"] = map[string][]string{"bronze": bronze}
	pathCatalogMu.Unlock()
	t.Cleanup(func() {
		pathCatalogMu.Lock()
		if had {
			pathAbilityPoolsByPath["arch_mage"] = prev
		} else {
			delete(pathAbilityPoolsByPath, "arch_mage")
		}
		pathCatalogMu.Unlock()
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

func TestAbilityPoolRoll_AssignsOneAndRecomputeIncludes(t *testing.T) {
	setArchMagePoolForTest(t, []string{"arcane_bolt", "heal", "greater_heal"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")

	s.rollUnitPoolAbilitiesLocked(u)
	pick := u.PoolAbilitiesByRank["bronze"]
	if pick != "arcane_bolt" && pick != "heal" && pick != "greater_heal" {
		t.Fatalf("bronze pick = %q; want one of the pool", pick)
	}
	s.assignUnitPathAbilitiesLocked(u)
	if !containsString(u.Abilities, pick) {
		t.Errorf("recompute Abilities %v missing the rolled pick %q", u.Abilities, pick)
	}
}

func TestAbilityPoolRoll_ExcludesKnown(t *testing.T) {
	setArchMagePoolForTest(t, []string{"heal", "arcane_bolt"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")
	u.Abilities = []string{"heal"} // already knows heal

	s.rollUnitPoolAbilitiesLocked(u)
	if got := u.PoolAbilitiesByRank["bronze"]; got != "arcane_bolt" {
		t.Errorf("pick = %q; want arcane_bolt (heal is already known and excluded)", got)
	}
}

func TestAbilityPoolRoll_DeterministicAndPerUnit(t *testing.T) {
	pool := []string{"arcane_bolt", "heal", "greater_heal"}

	roll := func() []string {
		setArchMagePoolForTest(t, pool)
		s := newProjectileTestState(t) // fixed seed 42
		s.mu.Lock()
		defer s.mu.Unlock()
		picks := make([]string, 0, 30)
		for i := 0; i < 30; i++ {
			u := makeArchMage(s, float64(i*20), 100, "bronze")
			s.rollUnitPoolAbilitiesLocked(u)
			picks = append(picks, u.PoolAbilitiesByRank["bronze"])
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

func TestAbilityPoolRoll_IdempotentNoRedraw(t *testing.T) {
	setArchMagePoolForTest(t, []string{"arcane_bolt", "heal", "greater_heal"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")

	s.rollUnitPoolAbilitiesLocked(u)
	first := u.PoolAbilitiesByRank["bronze"]
	s.rollUnitPoolAbilitiesLocked(u) // must NOT re-roll
	if u.PoolAbilitiesByRank["bronze"] != first {
		t.Errorf("re-roll changed the pick: %q → %q", first, u.PoolAbilitiesByRank["bronze"])
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

func TestAbilityPoolRoll_ExhaustedPoolAssignsNothing(t *testing.T) {
	setArchMagePoolForTest(t, []string{"heal"})
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := makeArchMage(s, 100, 100, "bronze")
	u.Abilities = []string{"heal"} // the only candidate is already known

	s.rollUnitPoolAbilitiesLocked(u)
	if _, ok := u.PoolAbilitiesByRank["bronze"]; ok {
		t.Errorf("exhausted pool recorded %q; want nothing", u.PoolAbilitiesByRank["bronze"])
	}
}

// The Arch Mage pool is shared across ranks: arch_mage.json authors the same
// 5-ability list under both "bronze" and "silver" (each rank's pool is
// self-contained — see abilityPoolFor), so Bronze and Silver together still
// grant two DISTINCT abilities drawn from the same candidate set (the no-dup
// roll logic in ability_pool_roll.go excludes the Bronze pick from Silver's
// draw).
func TestArchMagePool_SharedAcrossBronzeAndSilver(t *testing.T) {
	bronze := abilityPoolFor("arch_mage", "bronze")
	silver := abilityPoolFor("arch_mage", "silver")
	if len(silver) < len(bronze) {
		t.Fatalf("silver pool (%v) must include the whole bronze pool (%v)", silver, bronze)
	}
	for _, id := range bronze {
		if !containsStr(silver, id) {
			t.Errorf("shared pool: silver missing bronze ability %q (silver=%v)", id, silver)
		}
	}
	if !containsStr(bronze, "meteor") {
		t.Errorf("meteor should be in the shared pool; bronze=%v", bronze)
	}

	// A unit promoted to silver gets two distinct pool abilities (one per rank).
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := spawnProjTestUnit(t, s, "p1", 100, 100)
	u.ProgressionPath = "arch_mage"
	u.Rank = "silver"
	u.Abilities = nil
	s.rollUnitPoolAbilitiesLocked(u)
	b, s2 := u.PoolAbilitiesByRank["bronze"], u.PoolAbilitiesByRank["silver"]
	if b == "" || s2 == "" {
		t.Fatalf("expected a bronze AND a silver pool pick; got bronze=%q silver=%q", b, s2)
	}
	if b == s2 {
		t.Errorf("bronze and silver picks must be distinct; both = %q", b)
	}
}

// An empty/exhausted pool must consume NO RNG (determinism: it must not perturb
// the rngPerks stream). Roll with an empty pool then draw; compare to a control
// state that only draws.
func TestAbilityPoolRoll_EmptyPoolDrawsNoRNG(t *testing.T) {
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
	withRoll.rollUnitPoolAbilitiesLocked(u) // empty pool ⇒ should draw nothing

	if got, want := withRoll.rngPerks.Float64(), control.rngPerks.Float64(); got != want {
		t.Errorf("empty-pool roll perturbed the RNG stream: %v vs control %v", got, want)
	}
}
