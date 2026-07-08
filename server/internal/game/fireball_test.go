package game

import "testing"

func fireballDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("fireball")
	if !ok {
		t.Fatal(`getAbilityDef("fireball") missing`)
	}
	return def
}

// landFireballOn casts fireball at primary and advances projectiles until the
// bolt lands. Enemies are frozen (no move/attack) so impact geometry is stable.
func landFireballOn(t *testing.T, s *GameState, caster, primary *Unit) {
	t.Helper()
	def := fireballDef(t)
	if ok, r := s.beginAbilityCastLocked(caster, "fireball", primary); !ok {
		t.Fatalf("beginAbilityCastLocked fireball: %s", r)
	}
	s.tickUnitCastLocked(caster, def.CastTime) // resolve cast → fire bolt
	for i := 0; i < 80 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 0 {
		t.Fatal("fireball bolt never landed")
	}
}

func TestFireball_SplashDamagesClusterNotFar(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	def := fireballDef(t)
	if def.Radius < 60 {
		t.Fatalf("test assumes fireball radius >= 60; got %v", def.Radius)
	}

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"fireball"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100
	caster.Damage = 0 // no basic-attack pollution

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	near1 := spawnProjTestUnit(t, s, enemyPlayerID, 340, 100) // 40px from primary
	near2 := spawnProjTestUnit(t, s, enemyPlayerID, 300, 155) // 55px from primary
	far := spawnProjTestUnit(t, s, enemyPlayerID, 300, 420)   // 320px from primary
	all := []*Unit{primary, near1, near2, far}
	start := map[int]int{}
	for _, e := range all {
		e.MoveSpeed = 0
		e.Damage = 0
		start[e.ID] = e.HP
	}

	landFireballOn(t, s, caster, primary)

	for _, e := range []*Unit{primary, near1, near2} {
		if e.HP >= start[e.ID] {
			t.Errorf("unit at (%v,%v) HP %d not reduced from %d — splash should have hit", e.X, e.Y, e.HP, start[e.ID])
		}
	}
	if far.HP != start[far.ID] {
		t.Errorf("far unit HP %d changed from %d — it is outside the splash radius", far.HP, start[far.ID])
	}
}

// A low-HP splash victim is killed through the shared authoritative pipeline
// (HP driven to <= 0 by the splash), not a parallel path.
func TestFireball_SplashKillRoutesThroughPipeline(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"fireball"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100
	caster.Damage = 0

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	victim := spawnProjTestUnit(t, s, enemyPlayerID, 300, 150) // 50px, inside radius
	for _, e := range []*Unit{primary, victim} {
		e.MoveSpeed = 0
		e.Damage = 0
	}
	victim.HP = 5 // less than fireball damage

	landFireballOn(t, s, caster, primary)

	if victim.HP > 0 {
		t.Errorf("low-HP splash victim HP = %d; want <= 0 (killed via shared pipeline)", victim.HP)
	}
}
