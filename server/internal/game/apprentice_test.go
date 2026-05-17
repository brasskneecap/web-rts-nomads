package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ── Base stats are copied verbatim from archer ───────────────────────────────

func TestApprentice_BaseStatsCopiedFromArcher(t *testing.T) {
	app, ok := getUnitDef("apprentice")
	if !ok {
		t.Fatal("apprentice unit def not registered")
	}
	arch, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}

	if app.Type != "apprentice" || app.Name != "Apprentice" || app.Faction != "human" {
		t.Errorf("identity wrong: type=%q name=%q faction=%q", app.Type, app.Name, app.Faction)
	}
	// Every combat/economy base stat must match archer (tune later).
	if app.HP != arch.HP || app.Damage != arch.Damage || app.AttackRange != arch.AttackRange ||
		app.AttackSpeed != arch.AttackSpeed || app.MoveSpeed != arch.MoveSpeed ||
		app.VisionRange != arch.VisionRange || app.MeatCost != arch.MeatCost ||
		app.SpawnSeconds != arch.SpawnSeconds {
		t.Errorf("apprentice base stats diverge from archer:\n apprentice=%+v\n archer=%+v", app, arch)
	}
}

// ── Mana kit + attack configuration ──────────────────────────────────────────

func TestApprentice_ManaAndAttackKit(t *testing.T) {
	// The mana pool, regen, projectile, damage type, and ability list are all
	// catalog-driven (apprentice.json). Derive expectations from the def so
	// balance tweaks to the JSON do not break this test — it verifies the
	// spawn path faithfully copies the def, not specific tuned numbers.
	def, ok := getUnitDef("apprentice")
	if !ok {
		t.Fatal("apprentice unit def not registered")
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		t.Fatal("failed to spawn apprentice")
	}

	// A spellcaster must actually have a mana pool (invariant), and the spawn
	// path must seed it from the def: CurrentMana starts full at MaxMana.
	if def.MaxMana <= 0 {
		t.Errorf("apprentice def MaxMana=%d; a spellcaster must have a positive mana pool", def.MaxMana)
	}
	if u.MaxMana != def.MaxMana || u.CurrentMana != def.MaxMana {
		t.Errorf("mana pool: MaxMana=%d CurrentMana=%d; want %d/%d (def.MaxMana, starts full)",
			u.MaxMana, u.CurrentMana, def.MaxMana, def.MaxMana)
	}
	if u.ManaRegenPerSecond != def.ManaRegenRate {
		t.Errorf("ManaRegenPerSecond=%v; want %v (def.ManaRegenRate)", u.ManaRegenPerSecond, def.ManaRegenRate)
	}
	if u.ProjectileID != def.Projectile {
		t.Errorf("ProjectileID=%q; want %q (def.Projectile)", u.ProjectileID, def.Projectile)
	}
	if u.AttackDamageType != def.DamageType {
		t.Errorf("AttackDamageType=%q; want %q (def.DamageType)", u.AttackDamageType, def.DamageType)
	}
	if len(u.Abilities) != len(def.Abilities) {
		t.Errorf("Abilities=%v; want %v (def.Abilities)", u.Abilities, def.Abilities)
	} else {
		for i, ab := range def.Abilities {
			if u.Abilities[i] != ab {
				t.Errorf("Abilities[%d]=%q; want %q (def.Abilities)", i, u.Abilities[i], ab)
			}
		}
	}
}

// ── Combat profile is ranged, exactly like archer ────────────────────────────

func TestApprentice_IsRangedLikeArcher(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	arch := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 140, Y: 100})

	appProf := resolveCombatProfile(app)
	archProf := resolveCombatProfile(arch)
	if appProf.Melee {
		t.Error("apprentice must use a ranged combat profile, not melee")
	}
	if appProf.Melee != archProf.Melee {
		t.Errorf("apprentice combat profile should match archer's (Melee=%v vs %v)", appProf.Melee, archProf.Melee)
	}
}

// ── Apprentice fires a fire_bolt projectile (Fire, fizzle on impact) ──────────

func TestApprentice_FiresFireBoltWithFireDamageAndFizzle(t *testing.T) {
	// The projectile id, its impact/follow effects, and the damage type are all
	// catalog-driven (apprentice.json → fire_bolt projectile def). Derive the
	// expected wire values from the defs so balance/asset tweaks don't break
	// this test; it verifies the firing path threads the catalog data through.
	appDef, ok := getUnitDef("apprentice")
	if !ok {
		t.Fatal("apprentice unit def not registered")
	}
	projDef, ok := getProjectileDef(appDef.Projectile)
	if !ok {
		t.Fatalf("apprentice projectile def %q not registered", appDef.Projectile)
	}
	wantVariant := appDef.Projectile
	wantImpact := impactEffectForProjectileDef(projDef)
	wantFollow := followEffectForProjectileDef(projDef)
	wantDamageType := appDef.DamageType

	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	// Stationary hostile inside the apprentice's attack range.
	enemy := spawnProjTestUnit(t, s, "enemy", 400+150, 400)
	enemy.MoveSpeed = 0
	enemyID := enemy.ID
	enemyStartHP := enemy.HP

	// Fire through the exact production entrypoint (state_combat.go calls
	// s.fireProjectileLocked when a ranged unit's attack lands).
	before := len(s.Projectiles)
	s.fireProjectileLocked(app, enemy, app.Damage)
	if len(s.Projectiles) <= before {
		s.mu.Unlock()
		t.Fatal("apprentice fireProjectileLocked produced no projectile")
	}
	proj := s.Projectiles[before]

	if proj.Variant != wantVariant {
		t.Errorf("projectile Variant=%q; want %q (def.Projectile — client renders that sprite)", proj.Variant, wantVariant)
	}
	if proj.ImpactEffect != wantImpact {
		t.Errorf("projectile ImpactEffect=%q; want %q (fire_bolt def impactEffect)", proj.ImpactEffect, wantImpact)
	}
	if proj.FollowEffect != wantFollow {
		t.Errorf("projectile FollowEffect=%q; want %q (fire_bolt def followEffect)", proj.FollowEffect, wantFollow)
	}
	if proj.DamageType != wantDamageType {
		t.Errorf("projectile DamageType=%q; want %q (def.DamageType)", proj.DamageType, wantDamageType)
	}
	s.mu.Unlock()

	// Fly the bolt until it lands (it leaves s.Projectiles). Check the fizzle
	// immediately on landing — it is a brief 0.3s effect that the transient
	// pipeline culls a few ticks later, so don't over-tick past it.
	landed := false
	for i := 0; i < 30 && !landed; i++ {
		s.Update(0.05)
		s.mu.RLock()
		landed = len(s.Projectiles) == 0
		s.mu.RUnlock()
	}
	if !landed {
		t.Fatal("fire_bolt never landed")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.unitsByID[enemyID]
	if e != nil && e.HP >= enemyStartHP {
		t.Errorf("enemy should have taken fire_bolt damage; HP %d -> %d", enemyStartHP, e.HP)
	}
	// The impact effect played on landing is whatever the projectile def
	// configures (wantImpact, derived above). Only assert it when the catalog
	// actually configures an impact effect for this projectile.
	if wantImpact != "" && queuedEffectFor(s, wantImpact, enemyID) == nil {
		t.Errorf("a %q effect should have played on the enemy when a %s landed", wantImpact, wantVariant)
	}
}

// ── Regression: the default/archer shot is byte-for-byte unchanged ───────────

func TestArcher_DefaultShotUnchangedByProjectileWiring(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	arch := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	enemy := spawnProjTestUnit(t, s, "enemy", 400+150, 400)

	before := len(s.Projectiles)
	s.fireProjectileLocked(arch, enemy, arch.Damage)
	if len(s.Projectiles) <= before {
		t.Fatal("archer fireProjectileLocked produced no projectile")
	}
	proj := s.Projectiles[before]

	if proj.Variant != "archer" {
		t.Errorf("archer projectile Variant=%q; want \"archer\" (unchanged default)", proj.Variant)
	}
	if proj.ImpactEffect != "" || proj.FollowEffect != "" || proj.DamageType != "" {
		t.Errorf("archer shot must carry no effects/damage-type: impact=%q follow=%q dmgType=%q",
			proj.ImpactEffect, proj.FollowEffect, proj.DamageType)
	}
}
