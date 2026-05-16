package game

import "testing"

// queuedEffectFor returns the first active transient effect with the given
// name anchored to unitID, or nil. Caller holds s.mu.
func queuedEffectFor(s *GameState, name string, unitID int) *effectInstance {
	for i := range s.activeEffects {
		e := &s.activeEffects[i]
		if e.Name == name && e.AnchorUnitID == unitID {
			return e
		}
	}
	return nil
}

// ── Registration & lookup ────────────────────────────────────────────────────

func TestEffectDef_RegistrationAndLookup(t *testing.T) {
	for _, id := range []string{"healing_glow", "fizzle"} {
		def, ok := getEffectDef(id)
		if !ok {
			t.Fatalf("getEffectDef(%q) = _, false; want the registered def", id)
		}
		if def.ID != id {
			t.Errorf("def.ID = %q; want %q", def.ID, id)
		}
		if def.Duration <= 0 {
			t.Errorf("%s Duration = %v; want a positive authored duration", id, def.Duration)
		}
		if def.Anchor != EffectAnchorCenter {
			t.Errorf("%s Anchor = %q; want center", id, def.Anchor)
		}
	}

	if _, ok := getEffectDef("does_not_exist"); ok {
		t.Error(`getEffectDef("does_not_exist") returned ok=true; want false`)
	}

	all := ListEffectDefs()
	seen := map[string]bool{}
	for i, d := range all {
		seen[d.ID] = true
		if i > 0 && all[i-1].ID > d.ID {
			t.Errorf("ListEffectDefs not sorted: %q before %q", all[i-1].ID, d.ID)
		}
	}
	if !seen["healing_glow"] || !seen["fizzle"] {
		t.Errorf("ListEffectDefs() missing expected effects; got %v", seen)
	}
}

func TestEffectAnchor_OrCenterDefaults(t *testing.T) {
	if got := EffectAnchor("").OrCenter(); got != EffectAnchorCenter {
		t.Errorf(`EffectAnchor("").OrCenter() = %q; want center`, got)
	}
	if got := EffectAnchorHead.OrCenter(); got != EffectAnchorHead {
		t.Errorf("explicit anchor must be preserved; got %q want head", got)
	}
}

// ── Playing an effect on a unit drives the shared render pipeline ─────────────

func TestEffect_PlaysOnUnitViaTransientPipeline(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := spawnProjTestUnit(t, s, "p1", 300, 300)
	before := len(s.activeEffects)

	if !s.playEffectOnUnitLocked(u, "healing_glow") {
		t.Fatal(`playEffectOnUnitLocked(unit, "healing_glow") = false; want true`)
	}
	eff := queuedEffectFor(s, "healing_glow", u.ID)
	if eff == nil {
		t.Fatal("no healing_glow effect queued anchored to the unit (should render via EffectSnapshot pipeline)")
	}
	// healing_glow duration 1.0s → 20 ticks at 20Hz.
	if eff.DurationTicks != int(1.0*gameTicksPerSecond) {
		t.Errorf("queued effect DurationTicks = %d; want %d (from def duration 1.0s)", eff.DurationTicks, int(1.0*gameTicksPerSecond))
	}

	// Unknown effect id → false, nothing queued.
	if s.playEffectOnUnitLocked(u, "no_such_effect") {
		t.Error(`playEffectOnUnitLocked(unit, "no_such_effect") = true; want false`)
	}
	if len(s.activeEffects) != before+1 {
		t.Errorf("unknown effect must not queue anything; active effects = %d, want %d", len(s.activeEffects), before+1)
	}

	// nil unit → false, no panic.
	if s.playEffectOnUnitLocked(nil, "healing_glow") {
		t.Error("playEffectOnUnitLocked(nil, ...) = true; want false")
	}
}

// ── follow vs impact effects on a projectile def ─────────────────────────────

func TestProjectile_FollowAndImpactEffects(t *testing.T) {
	fireBolt, ok := getProjectileDef("fire_bolt")
	if !ok {
		t.Fatal("fire_bolt not registered")
	}

	// fire_bolt: the bolt itself is the in-flight visual (no follow effect),
	// and it fizzles on the unit it hits (impact effect).
	if got := followEffectForProjectileDef(fireBolt); got != "" {
		t.Errorf("fire_bolt follow effect = %q; want \"\" (none — the bolt is the visual)", got)
	}
	if got := impactEffectForProjectileDef(fireBolt); got != "fizzle" {
		t.Errorf("fire_bolt impact effect = %q; want \"fizzle\"", got)
	}

	// Fail-safe: an unregistered impact/follow id degrades to "" (no crash).
	bad := ProjectileDef{ID: "x", Speed: 500, FollowEffect: "ghost", ImpactEffect: "ghost"}
	if followEffectForProjectileDef(bad) != "" || impactEffectForProjectileDef(bad) != "" {
		t.Error("unregistered follow/impact effect ids must resolve to \"\"")
	}
}

// ── Impact effect actually plays on the target when the projectile lands ─────

func TestProjectile_ImpactEffectPlaysOnLand(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	target := spawnProjTestUnit(t, s, "enemy", 400, 400)

	proj := &Projectile{
		ID:           "proj_test",
		OwnerUnitID:  attacker.ID,
		TargetUnitID: target.ID,
		Damage:       5,
		ImpactEffect: impactEffectForProjectileDef(mustProjDef(t, "fire_bolt")),
	}
	if proj.ImpactEffect != "fizzle" {
		t.Fatalf("precondition: projectile should carry fizzle, got %q", proj.ImpactEffect)
	}

	var dead []int
	s.landProjectileLocked(proj, target, &dead)

	if queuedEffectFor(s, "fizzle", target.ID) == nil {
		t.Error("landing a fire_bolt should play a 'fizzle' effect anchored to the target it reached")
	}
	// Sanity: damage still landed (impact effect is additive, not a replacement).
	if target.HP >= target.MaxHP {
		t.Errorf("expected the landed projectile to also deal damage; target HP %d/%d", target.HP, target.MaxHP)
	}
}

func mustProjDef(t *testing.T, id string) ProjectileDef {
	t.Helper()
	def, ok := getProjectileDef(id)
	if !ok {
		t.Fatalf("projectile def %q not registered", id)
	}
	return def
}
