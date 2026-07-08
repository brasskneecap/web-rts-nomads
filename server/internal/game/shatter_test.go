package game

import "testing"

func shatterDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("shatter")
	if !ok {
		t.Fatal(`getAbilityDef("shatter") missing`)
	}
	return def
}

// Shatter's authored shape matches its design brief: a cold, point-targeted,
// instant (no projectile, zero cast time) AoE that both damages and slows, and
// deals LESS direct damage than Fireball (its trade for stronger control). All
// values are read from the catalog — the test guards the contract, not numbers.
func TestShatter_DefContract(t *testing.T) {
	def := shatterDef(t)
	if def.DamageType != DamageCold {
		t.Errorf("damageType = %q; want %q (cold routes the Chilled slow track)", def.DamageType, DamageCold)
	}
	if !def.TargetsPoint {
		t.Error("targetsPoint = false; Shatter must be point/ground castable")
	}
	if def.Projectile != "" {
		t.Errorf("projectile = %q; want empty (Shatter is instant/hitscan)", def.Projectile)
	}
	if def.CastTime != 0 {
		t.Errorf("castTime = %v; want 0 (point-cast path resolves instantly)", def.CastTime)
	}
	if def.Radius <= 0 {
		t.Errorf("radius = %v; want > 0 (AoE)", def.Radius)
	}
	if def.DamageAmount <= 0 {
		t.Errorf("damageAmount = %v; want > 0", def.DamageAmount)
	}
	if def.SlowMultiplier <= 0 || def.SlowMultiplier >= 1 {
		t.Errorf("slowMultiplier = %v; want in (0,1)", def.SlowMultiplier)
	}
	if def.SlowDurationSeconds <= 0 {
		t.Errorf("slowDurationSeconds = %v; want > 0", def.SlowDurationSeconds)
	}
	// Design intent: less direct damage than Fireball.
	if fb := fireballDef(t); def.DamageAmount >= fb.DamageAmount {
		t.Errorf("shatter damage %d not less than fireball damage %d (design: weaker direct hit, stronger CC)", def.DamageAmount, fb.DamageAmount)
	}
}

// A point cast damages every hostile in the radius and stamps the Chilled
// (cold) slow on each, while a unit outside the radius is untouched.
func TestShatter_PointCastDamagesAndChillsClusterNotFar(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	def := shatterDef(t)

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"shatter"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100
	caster.Damage = 0 // no basic-attack pollution

	// Cast centre well within cast range so the effect point is not clamped.
	cx, cy := 300.0, 100.0
	inside1 := spawnProjTestUnit(t, s, enemyPlayerID, cx, cy)                        // dead centre
	inside2 := spawnProjTestUnit(t, s, enemyPlayerID, cx, cy+def.Radius-10)          // just inside
	far := spawnProjTestUnit(t, s, enemyPlayerID, cx, cy+def.Radius+150)             // outside
	all := []*Unit{inside1, inside2, far}
	start := map[int]int{}
	for _, e := range all {
		e.MoveSpeed = 0
		e.Damage = 0
		start[e.ID] = e.HP
	}

	if ok, r := s.beginAbilityCastAtPointLocked(caster, "shatter", cx, cy); !ok {
		t.Fatalf("beginAbilityCastAtPointLocked shatter: %s", r)
	}

	for _, e := range []*Unit{inside1, inside2} {
		if e.HP >= start[e.ID] {
			t.Errorf("in-radius unit at (%v,%v) HP %d not reduced from %d — AoE should have hit", e.X, e.Y, e.HP, start[e.ID])
		}
		// Chilled = the COLD slow track (drives the icy overlay + move/attack
		// slow). Assert it landed on the cold track, at the authored strength.
		if e.ColdSlowedRemaining <= 0 {
			t.Errorf("in-radius unit at (%v,%v) not chilled: ColdSlowedRemaining = %v", e.X, e.Y, e.ColdSlowedRemaining)
		}
		if e.ColdSlowedMultiplier != def.SlowMultiplier {
			t.Errorf("chill multiplier = %v; want %v (from catalog)", e.ColdSlowedMultiplier, def.SlowMultiplier)
		}
		// Reuse of the existing Chilled seam means the PHYSICAL slow track is
		// never touched — proves we did not invent a parallel slow.
		if e.SlowedRemaining != 0 {
			t.Errorf("cold chill leaked onto the physical slow track: SlowedRemaining = %v", e.SlowedRemaining)
		}
	}

	if far.HP != start[far.ID] {
		t.Errorf("far unit HP %d changed from %d — outside AoE radius", far.HP, start[far.ID])
	}
	if far.ColdSlowedRemaining != 0 {
		t.Errorf("far unit chilled (ColdSlowedRemaining = %v) — outside AoE radius", far.ColdSlowedRemaining)
	}

	// A world-anchored ground burst is queued at the cast point so the cast is
	// visible (this is what the ability's effectAtPoint drives).
	if def.EffectAtPoint != "" {
		found := false
		for _, e := range s.activeEffects {
			if e.Name == def.EffectAtPoint && e.AnchorUnitID == 0 && e.FallbackX == cx && e.FallbackY == cy {
				found = true
			}
		}
		if !found {
			t.Errorf("no world-anchored %q effect queued at cast point (%v,%v); effects=%+v", def.EffectAtPoint, cx, cy, s.activeEffects)
		}
	}
}

// A ground cast onto EMPTY ground (no enemies anywhere near) still plays the
// burst VFX — the cast should never look like it silently did nothing.
func TestShatter_GroundCastOnEmptyGroundStillShowsEffect(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	def := shatterDef(t)
	if def.EffectAtPoint == "" {
		t.Skip("shatter declares no ground effect")
	}

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"shatter"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100

	if ok, r := s.beginAbilityCastAtPointLocked(caster, "shatter", 250, 100); !ok {
		t.Fatalf("beginAbilityCastAtPointLocked shatter: %s", r)
	}
	found := false
	for _, e := range s.activeEffects {
		if e.Name == def.EffectAtPoint && e.AnchorUnitID == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("empty-ground cast queued no %q burst effect", def.EffectAtPoint)
	}
}

// A low-HP unit is killed by the burst through the shared authoritative damage
// pipeline (HP driven <= 0), not a parallel damage path.
func TestShatter_KillRoutesThroughPipeline(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"shatter"}
	caster.AttackRange = 500
	caster.CurrentMana = 100
	caster.MaxMana = 100
	caster.Damage = 0

	victim := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	victim.MoveSpeed = 0
	victim.Damage = 0
	victim.HP = 1 // less than shatter damage

	if ok, r := s.beginAbilityCastAtPointLocked(caster, "shatter", 300, 100); !ok {
		t.Fatalf("beginAbilityCastAtPointLocked shatter: %s", r)
	}
	if victim.HP > 0 {
		t.Errorf("low-HP AoE victim HP = %d; want <= 0 (killed via shared pipeline)", victim.HP)
	}
}
