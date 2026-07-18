package game

import (
	"math"
	"testing"
)

// arcaneOrbDef returns the live catalog "arcane_orb" def with its mechanic
// magnitudes (Radius, PullStrength, DamagePerSecond, Projectile,
// ProjectileScale) RECOVERED from the compiled Program — arcane_orb is
// schemaVersion:2 as of the composable-abilities migration, so those fields
// are cleared on the raw def (Program is the sole authority) and must be
// read via abilityMechanicsShadow, same as fireballDef/shatterDef. CastRange/
// TargetsPoint are cast-setup fields that survive conversion untouched, so
// they read correctly either way.
func arcaneOrbDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("arcane_orb")
	if !ok {
		t.Fatal(`getAbilityDef("arcane_orb") missing`)
	}
	if !def.TargetsPoint {
		t.Fatal("arcane_orb should be point-targeted (targetsPoint)")
	}
	return abilityMechanicsShadow(def)
}

func setupOrbCaster(t *testing.T, s *GameState, x, y float64) *Unit {
	t.Helper()
	c := spawnProjTestUnit(t, s, "p1", x, y)
	c.Abilities = []string{"arcane_orb"}
	c.AttackRange = 500
	c.CurrentMana = 100
	c.MaxMana = 100
	c.Damage = 0
	return c
}

func findArcaneOrb(s *GameState) *Projectile {
	for _, p := range s.Projectiles {
		if p.ArcaneOrb {
			return p
		}
	}
	return nil
}

// Casting arcane_orb at a GROUND POINT launches a traveling orb — no instant
// pull, no homing bolt.
func TestArcaneOrb_PointCastSpawnsTravelingOrb(t *testing.T) {
	def := arcaneOrbDef(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := setupOrbCaster(t, s, 100, 100)
	// Click a bare ground point to the east (no unit there).
	if ok, r := s.beginAbilityCastAtPointLocked(caster, "arcane_orb", 400, 100); !ok {
		t.Fatalf("point cast failed: %s", r)
	}
	orb := findArcaneOrb(s)
	if orb == nil {
		t.Fatal("no arcane orb spawned by point cast")
	}
	if orb.ArcaneOrbPullStrength != def.PullStrength || orb.ArcaneOrbRadius != def.Radius {
		t.Errorf("orb pull params = (%v,%v); want (%v,%v)", orb.ArcaneOrbPullStrength, orb.ArcaneOrbRadius, def.PullStrength, def.Radius)
	}
	// Flies east (toward the click) over the full cast-range distance.
	if orb.PierceDirX < 0.99 || math.Abs(orb.PierceLength-def.CastRange.Resolve(caster)) > 1 {
		t.Errorf("orb dir=(%.2f,%.2f) len=%v; want east, len %v", orb.PierceDirX, orb.PierceDirY, orb.PierceLength, def.CastRange.Resolve(caster))
	}
	// The unit-target path must reject a point ability.
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 200, 100)
	if ok, _ := s.beginAbilityCastLocked(caster, "arcane_orb", enemy); ok {
		t.Error("arcane_orb must not be castable via the unit-target path")
	}
}

// RequestAbilityCast routes arcane_orb to the point path using TargetX/Y.
func TestArcaneOrb_RequestRoutesToPoint(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := setupOrbCaster(t, s, 100, 100)
	cid := caster.ID
	s.mu.Unlock()
	if ok, r := s.RequestAbilityCast("p1", cid, "arcane_orb", 0, 500, 100); !ok {
		t.Fatalf("RequestAbilityCast(point) failed: %s", r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if findArcaneOrb(s) == nil {
		t.Error("point RequestAbilityCast did not spawn an orb")
	}
}

// As the orb travels it drags AND damages a nearby hostile; allies untouched.
func TestArcaneOrb_MovingVortexDragsAndDamages(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := setupOrbCaster(t, s, 100, 100)
	bystander := spawnProjTestUnit(t, s, enemyPlayerID, 300, 150) // just off the east path
	ally := spawnProjTestUnit(t, s, "p1", 300, 60)
	for _, u := range []*Unit{bystander, ally} {
		u.MoveSpeed = 0
		u.Damage = 0
	}
	byStart := struct{ x, y float64 }{bystander.X, bystander.Y}
	byHP := bystander.HP
	allyStart := struct{ x, y float64 }{ally.X, ally.Y}
	if ok, r := s.beginAbilityCastAtPointLocked(caster, "arcane_orb", 500, 100); !ok {
		s.mu.Unlock()
		t.Fatalf("point cast failed: %s", r)
	}
	s.mu.Unlock()

	for i := 0; i < 50; i++ {
		s.Update(0.05)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if math.Hypot(bystander.X-byStart.x, bystander.Y-byStart.y) < 5 {
		t.Error("bystander was not dragged by the moving vortex")
	}
	if bystander.HP >= byHP {
		t.Errorf("bystander took no vortex damage (HP %d→%d)", byHP, bystander.HP)
	}
	if math.Hypot(ally.X-allyStart.x, ally.Y-allyStart.y) > 0.5 || ally.HP < ally.MaxHP {
		t.Error("ally was pulled or damaged; must be immune")
	}
}

// A pullStrength modifier scales the orb's pull without mutating the base def.
func TestArcaneOrb_PullStrengthModifier(t *testing.T) {
	def := arcaneOrbDef(t)
	base := def.PullStrength
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := setupOrbCaster(t, s, 100, 100)
	caster.SpellModifiers = []SpellModifier{
		{Target: SpellModTarget{SpellID: "arcane_orb"}, Field: SpellModFieldPullStrength, Operation: SpellModMultiply, Value: 2},
	}
	if ok, r := s.beginAbilityCastAtPointLocked(caster, "arcane_orb", 500, 100); !ok {
		t.Fatalf("point cast failed: %s", r)
	}
	orb := findArcaneOrb(s)
	if orb == nil {
		t.Fatal("no orb spawned")
	}
	if orb.ArcaneOrbPullStrength != base*2 {
		t.Errorf("orb pull strength = %v; want %v", orb.ArcaneOrbPullStrength, base*2)
	}
	if def.PullStrength != base {
		t.Errorf("base def mutated: %v; want %v", def.PullStrength, base)
	}
}

// The orb's vortex damage is a damage-over-time at exactly the authored
// per-second rate (16/s), applied on a fixed cadence rather than per-tick.
func TestArcaneOrb_DamageOverTimeRate(t *testing.T) {
	def := arcaneOrbDef(t)
	dps := int(def.DamagePerSecond) // authored DoT per-second rate
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := setupOrbCaster(t, s, 120, 100)
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 120, 100)
	enemy.MoveSpeed = 0
	enemy.MaxHP = 10000
	enemy.HP = 10000
	start := enemy.HP

	// Hand-build a near-stationary, no-pull orb with a large radius so the
	// enemy stays in range for the whole window — isolates the DoT rate.
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:                       "orb_test",
		OwnerUnitID:              caster.ID,
		OwnerPlayerID:            "p1",
		OriginX:                  120,
		OriginY:                  100,
		TargetX:                  1120,
		TargetY:                  100,
		TotalSeconds:             1000, // ~1 px/s ⇒ negligible drift over 1s
		RemainingSeconds:         1000,
		ArcaneOrb:                true,
		ArcaneOrbRadius:          500,
		ArcaneOrbPullStrength:    0, // no pull: keep the enemy stationary
		ArcaneOrbDamagePerSecond: float64(dps),
		ArcaneOrbDamageType:      DamageArcane,
		PierceLength:             1000,
		PierceDirX:               1,
	})

	// Advance exactly 1 second (20 × 0.05).
	for i := 0; i < 20; i++ {
		s.tickProjectilesLocked(0.05)
	}
	took := start - enemy.HP
	if took != dps {
		t.Errorf("1s of vortex dealt %d damage; want exactly %d (authored DoT rate)", took, dps)
	}
}
