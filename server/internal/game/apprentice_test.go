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
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		t.Fatal("failed to spawn apprentice")
	}

	if u.MaxMana != 50 || u.CurrentMana != 50 {
		t.Errorf("mana pool: MaxMana=%d CurrentMana=%d; want 50/50 (starts full)", u.MaxMana, u.CurrentMana)
	}
	if u.ManaRegenPerSecond != 1.0 {
		t.Errorf("ManaRegenPerSecond=%v; want 1.0", u.ManaRegenPerSecond)
	}
	if u.ProjectileID != "fire_bolt" {
		t.Errorf("ProjectileID=%q; want fire_bolt", u.ProjectileID)
	}
	if u.AttackDamageType != DamageFire {
		t.Errorf("AttackDamageType=%q; want fire", u.AttackDamageType)
	}
	if len(u.Abilities) != 1 || u.Abilities[0] != "heal" {
		t.Errorf("Abilities=%v; want [heal]", u.Abilities)
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
	s := newProjectileTestState(t)
	s.mu.Lock()
	app := s.spawnPlayerUnitLocked("apprentice", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	app.Visible = true
	// Stationary hostile inside the apprentice's 220 range.
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

	if proj.Variant != "fire_bolt" {
		t.Errorf("projectile Variant=%q; want fire_bolt (client renders the fire bolt sprite)", proj.Variant)
	}
	if proj.ImpactEffect != "fizzle" {
		t.Errorf("projectile ImpactEffect=%q; want fizzle", proj.ImpactEffect)
	}
	if proj.FollowEffect != "" {
		t.Errorf("projectile FollowEffect=%q; want \"\" (the bolt itself is the in-flight visual)", proj.FollowEffect)
	}
	if proj.DamageType != DamageFire {
		t.Errorf("projectile DamageType=%q; want fire", proj.DamageType)
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
	if queuedEffectFor(s, "fizzle", enemyID) == nil {
		t.Error("a 'fizzle' effect should have played on the enemy when a fire_bolt landed")
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
