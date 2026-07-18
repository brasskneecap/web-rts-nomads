package game

import (
	"encoding/json"
	"testing"
)

// chainLoopIterations reads the bounce loop's iteration count (the number of
// bounces beyond the primary hit) from the migrated chain_lightning program, so
// cap-related tests derive from the catalog rather than hardcoding a number.
func chainLoopIterations(t *testing.T) int {
	t.Helper()
	def, ok := getAbilityDef("chain_lightning")
	if !ok || def.Program == nil {
		t.Fatal("chain_lightning has no program")
	}
	for _, trig := range def.Program.Triggers {
		for _, act := range trig.Actions {
			if act.Type != ActionLoop {
				continue
			}
			var lc loopConfig
			if err := json.Unmarshal(act.Config, &lc); err != nil {
				t.Fatalf("chain_lightning loop config: %v", err)
			}
			return lc.Iterations
		}
	}
	t.Fatal("chain_lightning has no loop action")
	return 0
}

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

// castChainLightning casts at primary and drives the loop-iteration scheduler
// until every bounce has landed. The chain is a `loop` action: iteration 0 (the
// primary hit) resolves synchronously at cast; each later bounce is scheduled
// ~0.12s after the last (the body's wait) and fired by tickPendingLoopsLocked as
// simTime advances.
func castChainLightning(t *testing.T, s *GameState, caster, primary *Unit) {
	t.Helper()
	def := chainLightningDef(t)
	if ok, r := s.beginAbilityCastLocked(caster, "chain_lightning", primary); !ok {
		t.Fatalf("beginAbilityCastLocked chain_lightning: %s", r)
	}
	s.tickUnitCastLocked(caster, def.CastTime) // resolve → primary hit + schedule bounces
	for i := 0; i < 60 && len(s.pendingLoops) > 0; i++ {
		s.simTime += 0.05
		s.tickPendingLoopsLocked()
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

// TestChainLightning_HardCapStopsBeyondReach proves the loop's iteration cap: a
// line of enemies each within one bounce radius (220) of the previous — ONE MORE
// than the chain can hit (primary + loop-iterations bounces) — and the chain
// still stops at the cap, leaving the last, perfectly-chainable enemy untouched.
// Derives the cap from the catalog so it survives a balance change to the bounce
// count.
func TestChainLightning_HardCapStopsBeyondReach(t *testing.T) {
	bounces := chainLoopIterations(t)
	totalHits := 1 + bounces // primary + bounces

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"chain_lightning"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100
	caster.Damage = 0

	// One more enemy than the chain can reach: primary + totalHits (index 0 is
	// the primary; index totalHits is the one past the cap), each 100px apart.
	enemies := make([]*Unit, totalHits+1)
	start := map[int]int{}
	for i := range enemies {
		enemies[i] = spawnProjTestUnit(t, s, enemyPlayerID, float64(300+i*100), 100)
		enemies[i].MoveSpeed = 0
		enemies[i].Damage = 0
		start[enemies[i].ID] = enemies[i].HP
	}

	castChainLightning(t, s, caster, enemies[0])

	// The first totalHits enemies each take damage…
	for i := 0; i < totalHits; i++ {
		if start[enemies[i].ID]-enemies[i].HP <= 0 {
			t.Fatalf("expected hit %d/%d to take damage, but it took none", i+1, totalHits)
		}
	}
	// …and the one past the cap takes none, though it was in reach.
	last := enemies[totalHits]
	if last.HP != start[last.ID] {
		t.Errorf("cap breached: the enemy past the %d-hit cap took %d damage", totalHits, start[last.ID]-last.HP)
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
