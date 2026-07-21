package game

import "testing"

// shatterDef returns the live catalog "shatter" ability with its mechanic
// magnitudes RECOVERED from the compiled Program (abilityMechanicsShadow) —
// shatter is schemaVersion:2 as of the composable-abilities migration, so the
// raw catalog def's DamageAmount/Radius/SlowMultiplier/etc. are cleared to 0
// and the shipped Program is the sole authority for them. The recovered
// values are exactly what a real cast actually uses (same seam
// describeAbilityProgram uses for tooltip prose), so tests below still
// derive expectations from "the catalog" rather than a hardcoded number.
func shatterDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("shatter")
	if !ok {
		t.Fatal(`getAbilityDef("shatter") missing`)
	}
	return abilityMechanicsShadow(def)
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
	// Shatter's chill is no longer a recovered SlowMultiplier/SlowDurationSeconds
	// mechanic — it is authored as an apply_status_duration composition
	// (change_stat moveSpeed + change_stat attackSpeed + apply_color_overlay), so
	// the legacy shadow fields are 0 here by design. The slow's behaviour is
	// covered directly by TestShatter_PointCastDamagesAndChillsClusterNotFar.
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
		// Chill is now an apply_status_duration COMPOSITION, not the cold-slow
		// track: a status carrying change_stat(moveSpeed) + change_stat(attackSpeed)
		// + apply_color_overlay. Assert its observable effects — the icy overlay
		// and a real move/attack-speed reduction folded at the (now status-aware)
		// read sites — rather than the retired ColdSlowedRemaining field.
		e.MoveSpeed = 100  // give the move read site a positive base to scale
		e.AttackSpeed = 1.0 // and the attack read site
		if got := s.unitOverlayColorLocked(e); got != "#96d6ff" {
			t.Errorf("in-radius unit at (%v,%v) not tinted: overlay = %q, want #96d6ff", e.X, e.Y, got)
		}
		if mult := s.perkMoveSpeedMultiplierLocked(e); mult >= 1.0 {
			t.Errorf("in-radius unit at (%v,%v) move speed not slowed: multiplier = %v, want < 1", e.X, e.Y, mult)
		}
		if bonus := s.perkAttackSpeedBonusLocked(e); bonus >= 0 {
			t.Errorf("in-radius unit at (%v,%v) attack speed not slowed: bonus = %v, want < 0 (chill reduces it)", e.X, e.Y, bonus)
		}
		// The composition never touches the PHYSICAL slow track — proves we did
		// not invent a parallel slow.
		if e.SlowedRemaining != 0 {
			t.Errorf("chill leaked onto the physical slow track: SlowedRemaining = %v", e.SlowedRemaining)
		}
	}

	if far.HP != start[far.ID] {
		t.Errorf("far unit HP %d changed from %d — outside AoE radius", far.HP, start[far.ID])
	}
	if got := s.unitOverlayColorLocked(far); got != "" {
		t.Errorf("far unit tinted (overlay = %q) — outside AoE radius", got)
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
