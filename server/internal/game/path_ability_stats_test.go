package game

import (
	"encoding/json"
	"math"
	"testing"
)

// TestPathAbilityStats_ApplyAtRank is the end-to-end proof that a path's
// per-rank ability stats actually reach an ability — not just that they load.
// They fold through collectAbilityStatSourcesLocked, the same chokepoint units
// and items use, so every ability picks them up with no per-ability wiring.
func TestPathAbilityStats_ApplyAtRank(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Register a throwaway path with a gold-only radius bonus.
	pathCatalogMu.Lock()
	pathAbilityStatsByPath["zz_test_path"] = map[string]map[string]AbilityStatMod{
		unitRankSilver: {"radius": {Pct: 0.10}},
		unitRankGold:   {"radius": {Pct: 0.50}},
	}
	pathCatalogMu.Unlock()
	defer func() {
		pathCatalogMu.Lock()
		delete(pathAbilityStatsByPath, "zz_test_path")
		pathCatalogMu.Unlock()
	}()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	// Tolerance, not equality: these are float64 folds (100 x 1.10 is
	// 110.00000000000001 in binary floating point). Determinism is not at risk —
	// identical inputs always give identical bits.
	radius := func() float64 {
		raw := s.applyAbilityStatsToConfigLocked(caster, ActionCreateZone, mustJSON(map[string]any{"radius": 100.0}))
		v, _ := decodedConfig(t, raw)["radius"].(float64)
		return v
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

	// Pathless: nothing applies.
	if got := radius(); !near(got, 100) {
		t.Errorf("pathless radius = %v, want 100", got)
	}

	// On the path but at a rank the path doesn't author: still nothing.
	caster.ProgressionPath = "zz_test_path"
	caster.Rank = unitRankBronze
	if got := radius(); !near(got, 100) {
		t.Errorf("bronze radius = %v, want 100 (bronze authors no block)", got)
	}

	// Silver and gold each apply their OWN absolute block — gold is not
	// silver+gold stacked.
	caster.Rank = unitRankSilver
	if got := radius(); !near(got, 110) {
		t.Errorf("silver radius = %v, want 110", got)
	}
	caster.Rank = unitRankGold
	if got := radius(); !near(got, 150) {
		t.Errorf("gold radius = %v, want 150 (absolute per rank, not cumulative)", got)
	}
}

// TestValidatePathAbilityStatsByRank_RejectsARegression is the load-time twin of
// the editor's hard floor: the blocks are absolute per rank, so a gold value
// below silver's would silently make a PROMOTED unit weaker.
func TestValidatePathAbilityStatsByRank_RejectsARegression(t *testing.T) {
	ok := map[string]map[string]AbilityStatMod{
		unitRankBronze: {"radius": {Pct: 0.10}},
		unitRankSilver: {"radius": {Pct: 0.20}},
		unitRankGold:   {"radius": {Pct: 0.35}},
	}
	if err := validatePathAbilityStatsByRank("p", ok); err != nil {
		t.Errorf("a monotonic block was rejected: %v", err)
	}

	regress := map[string]map[string]AbilityStatMod{
		unitRankSilver: {"radius": {Pct: 0.30}},
		unitRankGold:   {"radius": {Pct: 0.15}},
	}
	if err := validatePathAbilityStatsByRank("p", regress); err == nil {
		t.Error("gold weaker than silver was accepted")
	}

	// A rank that OMITS a stat inherits the earlier value rather than dropping
	// to zero — that is what "absolute per rank" means in practice, and it must
	// not be reported as a regression.
	sparse := map[string]map[string]AbilityStatMod{
		unitRankBronze: {"radius": {Pct: 0.10}},
		unitRankGold:   {"duration": {Flat: 2}},
	}
	if err := validatePathAbilityStatsByRank("p", sparse); err != nil {
		t.Errorf("a rank omitting a stat was treated as a regression: %v", err)
	}

	if err := validatePathAbilityStatsByRank("p", map[string]map[string]AbilityStatMod{
		"platinum": {"radius": {Pct: 0.1}},
	}); err == nil {
		t.Error("an unknown rank key was accepted")
	}
	if err := validatePathAbilityStatsByRank("p", map[string]map[string]AbilityStatMod{
		unitRankGold: {"raduis": {Pct: 0.1}},
	}); err == nil {
		t.Error("a misspelled stat id was accepted")
	}
}

func mustJSON(v map[string]any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// TestPathRankBaseStats_ApplyAtRank proves per-rank base stats reach the unit.
// They exist because the 12 typed rank fields are MULTIPLIERS, which is
// meaningless for a stat whose base is 0 — no multiple of zero ability power is
// ever more than zero. So these are absolute, and they overwrite Unit.BaseStats
// so every existing read site picks them up with no new plumbing.
func TestPathRankBaseStats_ApplyAtRank(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	key := pathModifierKey("zz_bs_path", unitRankGold)
	pathCatalogMu.Lock()
	pathModifiersByKey[key] = pathModifierDef{
		Path: "zz_bs_path", Rank: unitRankGold,
		MaxHPMultiplier: 1, MaxMPMultiplier: 1, HealthRegenMultiplier: 1,
		DamageMultiplier: 1, AttackSpeedMultiplier: 1, MoveSpeedMultiplier: 1,
		AttackRangeMultiplier: 1,
		BaseStats:             map[string]float64{statAbilityPower: 40},
	}
	pathCatalogMu.Unlock()
	defer func() {
		pathCatalogMu.Lock()
		delete(pathModifiersByKey, key)
		pathCatalogMu.Unlock()
	}()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.BaseStats = map[string]float64{statAbilityPower: 5} // the unit's own base
	if got := unitBaseStat(caster, statAbilityPower); got != 5 {
		t.Fatalf("unit base ability power = %v, want 5", got)
	}

	caster.ProgressionPath = "zz_bs_path"
	caster.Rank = unitRankGold
	s.applyRankModifiersLocked(caster, false)

	if got := unitBaseStat(caster, statAbilityPower); got != 40 {
		t.Errorf("gold ability power = %v, want 40 (absolute, overwrites the unit base)", got)
	}
	// A stat the rank does NOT author is left exactly as the unit authored it.
	caster.BaseStats[statLifesteal] = 0.2
	s.applyRankModifiersLocked(caster, false)
	if got := unitBaseStat(caster, statLifesteal); got != 0.2 {
		t.Errorf("lifesteal = %v, want 0.2 (untouched by a rank that does not author it)", got)
	}
}

func TestValidatePathRankBaseStats(t *testing.T) {
	mk := func(byRank map[string]map[string]float64) map[string]pathRankStatsJSON {
		out := map[string]pathRankStatsJSON{}
		for rank, stats := range byRank {
			out[rank] = pathRankStatsJSON{BaseStats: stats}
		}
		return out
	}

	if err := validatePathRankBaseStats("p", mk(map[string]map[string]float64{
		unitRankBronze: {statAbilityPower: 5},
		unitRankSilver: {statAbilityPower: 12},
		unitRankGold:   {statAbilityPower: 30},
	})); err != nil {
		t.Errorf("a rising block was rejected: %v", err)
	}

	if err := validatePathRankBaseStats("p", mk(map[string]map[string]float64{
		unitRankSilver: {statAbilityPower: 30},
		unitRankGold:   {statAbilityPower: 10},
	})); err == nil {
		t.Error("gold weaker than silver was accepted")
	}

	// Only stats a unit may carry a base for — the same rule UnitDef.BaseStats
	// follows, which excludes anything with a typed field (no double source).
	if err := validatePathRankBaseStats("p", mk(map[string]map[string]float64{
		unitRankGold: {"damage": 50},
	})); err == nil {
		t.Error("a typed-field stat was accepted as a per-rank base stat")
	}
}

// TestPathRankStats_InheritIndependentOfPromotionOrder is the regression for a
// bug that only ever showed on a unit that did NOT walk up the ranks.
//
// Both per-rank blocks are absolute with "a rank that omits a stat inherits it".
// That inheritance used to be an accident: applyRankModifiersLocked wrote only
// the CURRENT row's baseStats and never cleared what it wrote, so a unit that
// promoted bronze -> silver kept bronze's value, while a unit created directly
// at silver (debug spawn, a load that restores rank before applying modifiers)
// silently got nothing. pathAbilityStatsFor had the same shape and was worse —
// it returned only the current rank's block, so a bronze-authored ability stat
// vanished on promotion for EVERY unit.
func TestPathRankStats_InheritIndependentOfPromotionOrder(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Authored at BRONZE ONLY, in both blocks — the shape the Trapper uses.
	pathCatalogMu.Lock()
	for _, rank := range pathRankOrder {
		def := pathModifierDef{
			Path: "zz_inherit", Rank: rank,
			MaxHPMultiplier: 1, MaxMPMultiplier: 1, HealthRegenMultiplier: 1,
			DamageMultiplier: 1, AttackSpeedMultiplier: 1, MoveSpeedMultiplier: 1,
			AttackRangeMultiplier: 1,
		}
		if rank == unitRankBronze {
			def.BaseStats = map[string]float64{statAbilityPower: 15}
		}
		pathModifiersByKey[pathModifierKey("zz_inherit", rank)] = def
	}
	pathAbilityStatsByPath["zz_inherit"] = map[string]map[string]AbilityStatMod{
		unitRankBronze: {"radius": {Flat: 20}},
	}
	pathCatalogMu.Unlock()
	defer func() {
		pathCatalogMu.Lock()
		for _, rank := range pathRankOrder {
			delete(pathModifiersByKey, pathModifierKey("zz_inherit", rank))
		}
		delete(pathAbilityStatsByPath, "zz_inherit")
		pathCatalogMu.Unlock()
	}()

	for _, rank := range pathRankOrder {
		// A FRESH unit per rank, promoted straight to it — never through bronze.
		caster := spawnProjTestUnit(t, s, "p1", 0, 0)
		caster.ProgressionPath = "zz_inherit"
		caster.Rank = rank
		s.applyRankModifiersLocked(caster, false)

		if got := unitBaseStat(caster, statAbilityPower); got != 15 {
			t.Errorf("%s: ability power = %v, want 15 inherited from bronze", rank, got)
		}
		raw := s.applyAbilityStatsToConfigLocked(caster, ActionCreateZone, mustJSON(map[string]any{"radius": 100.0}))
		if got, _ := decodedConfig(t, raw)["radius"].(float64); math.Abs(got-120) > 1e-9 {
			t.Errorf("%s: radius = %v, want 120 inherited from bronze", rank, got)
		}
	}
}

// Base rank is not a promotion rank. It must not pick up bronze's numbers just
// because bronze is first in the fold order.
func TestPathRankStats_BaseRankInheritsNothing(t *testing.T) {
	key := pathModifierKey("zz_base", unitRankBronze)
	pathCatalogMu.Lock()
	pathModifiersByKey[key] = pathModifierDef{
		Path: "zz_base", Rank: unitRankBronze,
		BaseStats: map[string]float64{statAbilityPower: 15},
	}
	pathAbilityStatsByPath["zz_base"] = map[string]map[string]AbilityStatMod{
		unitRankBronze: {"radius": {Flat: 20}},
	}
	pathCatalogMu.Unlock()
	defer func() {
		pathCatalogMu.Lock()
		delete(pathModifiersByKey, key)
		delete(pathAbilityStatsByPath, "zz_base")
		pathCatalogMu.Unlock()
	}()

	// Sanity: bronze DOES see them, so a nil at base rank means the guard, not
	// an empty registry.
	if got := accumulatedRankBaseStats("zz_base", unitRankBronze); len(got) != 1 {
		t.Fatalf("bronze base stats = %v, want the authored entry", got)
	}
	if got := accumulatedRankBaseStats("zz_base", unitRankBase); got != nil {
		t.Errorf("base-rank base stats = %v, want nil", got)
	}
	if got := pathAbilityStatsFor("zz_base", unitRankBase); got != nil {
		t.Errorf("base-rank ability stats = %v, want nil", got)
	}
}
