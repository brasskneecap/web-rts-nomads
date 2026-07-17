package game

import "testing"

// chainLightningDef returns the live catalog "chain_lightning" ability with
// its mechanic magnitudes RECOVERED from the compiled Program
// (abilityMechanicsShadow) — chain_lightning is schemaVersion:2 as of the
// composable-abilities migration, so the raw catalog def's DamageAmount/
// ChainCount/BounceRange/BounceDamageFalloff/Projectile/etc. are cleared to
// their zero values and the shipped Program (a single launch_projectile
// action) is the sole authority for them. The recovered values are exactly
// what a real cast actually uses, so test sanity-checks below still derive
// their expectations from "the catalog" rather than a hardcoded number. Same
// pattern as shatterDef (shatter_test.go) / fireballDef (fireball_test.go).
func chainLightningDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("chain_lightning")
	if !ok {
		t.Fatal(`getAbilityDef("chain_lightning") missing`)
	}
	return abilityMechanicsShadow(def)
}

// castChainLightning casts at primary and advances beams until deferred damage
// lands. Returns start HP map for delta assertions.
func castChainLightning(t *testing.T, s *GameState, caster, primary *Unit) {
	t.Helper()
	def := chainLightningDef(t)
	if ok, r := s.beginAbilityCastLocked(caster, "chain_lightning", primary); !ok {
		t.Fatalf("beginAbilityCastLocked chain_lightning: %s", r)
	}
	s.tickUnitCastLocked(caster, def.CastTime) // resolve → spawn chain beams
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		s.tickBeamsLocked(0.05)
	}
}

func TestChainLightning_ArcsWithFalloff(t *testing.T) {
	def := chainLightningDef(t)
	if def.ChainCount < 2 {
		t.Fatalf("test assumes chainCount >= 2; got %d", def.ChainCount)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"chain_lightning"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100
	caster.Damage = 0

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	b1 := spawnProjTestUnit(t, s, enemyPlayerID, 400, 100) // 100px from primary
	b2 := spawnProjTestUnit(t, s, enemyPlayerID, 500, 100) // 100px from b1
	far := spawnProjTestUnit(t, s, enemyPlayerID, 300, 700) // far from the chain
	chain := []*Unit{primary, b1, b2, far}
	start := map[int]int{}
	for _, e := range chain {
		e.MoveSpeed = 0
		e.Damage = 0
		start[e.ID] = e.HP
	}

	castChainLightning(t, s, caster, primary)

	dPrimary := start[primary.ID] - primary.HP
	d1 := start[b1.ID] - b1.HP
	d2 := start[b2.ID] - b2.HP
	if dPrimary <= 0 || d1 <= 0 || d2 <= 0 {
		t.Fatalf("chain should hit primary+2 bounces; deltas primary=%d b1=%d b2=%d", dPrimary, d1, d2)
	}
	if far.HP != start[far.ID] {
		t.Errorf("far unit took damage but is out of bounce range")
	}
	// Per-hop falloff: each hop loses BounceDamageFalloff, so damage strictly
	// decreases along the chain.
	if !(dPrimary > d1 && d1 > d2) {
		t.Errorf("expected falloff primary>b1>b2; got %d,%d,%d", dPrimary, d1, d2)
	}
}

// Bounce target selection is deterministic across seeded runs.
func TestChainLightning_DeterministicSelection(t *testing.T) {
	run := func() (int, int, int) {
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		caster := spawnProjTestUnit(t, s, "p1", 100, 100)
		caster.Abilities = []string{"chain_lightning"}
		caster.AttackRange = 500
		caster.CurrentMana = 100
		caster.MaxMana = 100
		caster.Damage = 0
		primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
		b1 := spawnProjTestUnit(t, s, enemyPlayerID, 400, 100)
		b2 := spawnProjTestUnit(t, s, enemyPlayerID, 500, 100)
		for _, e := range []*Unit{primary, b1, b2} {
			e.MoveSpeed = 0
			e.Damage = 0
		}
		sp, s1, s2 := primary.HP, b1.HP, b2.HP
		castChainLightning(t, s, caster, primary)
		return sp - primary.HP, s1 - b1.HP, s2 - b2.HP
	}
	a1, a2, a3 := run()
	b1, b2, b3 := run()
	if a1 != b1 || a2 != b2 || a3 != b3 {
		t.Errorf("non-deterministic chain outcome: run A (%d,%d,%d) vs run B (%d,%d,%d)", a1, a2, a3, b1, b2, b3)
	}
}
