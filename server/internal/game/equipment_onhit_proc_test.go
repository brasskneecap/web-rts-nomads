package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestOnHitProc_FiresBoltDeterministically(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C0)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500}
	s.nextUnitID++
	s.addUnitLocked(target)

	// chance 1.0 → a proc projectile must spawn on every hit.
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	before := len(s.Projectiles)
	deadUnitIDs := []int{}
	s.resolveAttackHitLocked(attacker, target, 1, &deadUnitIDs)
	if len(s.Projectiles) != before+1 {
		t.Fatalf("chance 1.0 should spawn exactly one proc projectile, got %d new", len(s.Projectiles)-before)
	}
	proc := s.Projectiles[len(s.Projectiles)-1]
	if !proc.SkipOnHitEffects || proc.Damage != 25 || proc.DamageType != DamageFire {
		t.Fatalf("proc projectile fields unexpected: %+v", proc)
	}

	// chance 0.0 → never spawns.
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	before = len(s.Projectiles)
	s.resolveAttackHitLocked(attacker, target, 1, &deadUnitIDs)
	if len(s.Projectiles) != before {
		t.Fatalf("chance 0.0 should spawn no proc projectile, got %d new", len(s.Projectiles)-before)
	}
}

func TestOnHitProc_ProjectileDoesNotReProc(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C1)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500}
	s.nextUnitID++
	s.addUnitLocked(target)

	// Fire one proc, then land it manually. Landing must apply 25 fire damage and
	// must NOT spawn another proc projectile (SkipOnHitEffects bypasses the hub).
	deadUnitIDs := []int{}
	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc projectile, got %d", len(s.Projectiles))
	}
	proc := s.Projectiles[0]
	hpBefore := target.HP
	s.landProjectileLocked(proc, target, &deadUnitIDs)
	if target.HP != hpBefore-25 {
		t.Fatalf("proc landing should deal 25, HP went %d→%d", hpBefore, target.HP)
	}
	if len(s.Projectiles) != 1 {
		t.Fatalf("landing a proc projectile must not spawn another projectile, have %d", len(s.Projectiles))
	}
}

func TestOnHitProc_Deterministic(t *testing.T) {
	run := func() int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x5EED)
		s.mu.Lock()
		defer s.mu.Unlock()
		attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
		attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.5, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
		target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000}
		s.nextUnitID++
		s.addUnitLocked(target)
		count := 0
		for i := 0; i < 200; i++ {
			before := len(s.Projectiles)
			s.rollEquipmentProcsLocked(attacker, target)
			if len(s.Projectiles) > before {
				count++
			}
		}
		return count
	}
	a, b := run(), run()
	if a != b {
		t.Fatalf("proc rolls not deterministic under fixed seed: %d vs %d", a, b)
	}
}
