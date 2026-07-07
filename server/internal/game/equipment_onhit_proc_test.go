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
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
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
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
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
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
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

// TestOnHitProc_RangedArrowLandingPersistsBolt drives the FULL ranged path: an
// in-flight arrow reaches its target inside tickProjectilesLocked, whose
// landing fires the on-hit proc. The spawned proc bolt is appended to
// s.Projectiles DURING that tick's compaction loop, so it must survive the
// final list rebuild. Regression for the bug where `s.Projectiles = kept`
// discarded any projectile appended while landing another — which made ranged
// attackers (archers) never emit their frost/fire/lightning proc bolts even
// though the roll succeeded.
func TestOnHitProc_RangedArrowLandingPersistsBolt(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C2)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	// One in-flight arrow that reaches the target this tick (RemainingSeconds
	// goes <= 0 after the dt decrement, so tickProjectilesLocked lands it).
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:               "arrow_test",
		OwnerUnitID:      attacker.ID,
		OwnerPlayerID:    attacker.OwnerID,
		TargetUnitID:     target.ID,
		Damage:           5,
		RemainingSeconds: 0.01,
		TotalSeconds:     1,
	})

	s.tickProjectilesLocked(0.1) // arrow lands → on-hit proc fires

	boltCount := 0
	for _, p := range s.Projectiles {
		if p.SkipOnHitEffects && p.DamageType == DamageFire {
			boltCount++
		}
	}
	if boltCount != 1 {
		t.Fatalf("the on-hit proc bolt must persist after the arrow landed in tickProjectilesLocked; found %d proc bolts, s.Projectiles has %d total", boltCount, len(s.Projectiles))
	}
}

// TestOnHitProc_ProjectileScale verifies the proc-authored ProjectileScale
// controls the fired bolt's render size, and that omitting it (0) falls back to
// the firing unit's ProjectileScale — the prior behavior.
func TestOnHitProc_ProjectileScale(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C3)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.ProjectileScale = 1.0
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 50}
	s.nextUnitID++
	s.addUnitLocked(target)

	// Explicit proc scale overrides the attacker's.
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt", ProjectileScale: 3}}}
	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc bolt, got %d", len(s.Projectiles))
	}
	if got := s.Projectiles[0].Scale; got != 3 {
		t.Fatalf("proc-authored scale should win: want 3, got %v", got)
	}

	// Omitted proc scale (0) inherits the firing unit's ProjectileScale.
	s.Projectiles = nil
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
	s.rollEquipmentProcsLocked(attacker, target)
	if got := s.Projectiles[0].Scale; got != 1.0 {
		t.Fatalf("omitted proc scale should inherit attacker scale 1.0, got %v", got)
	}
}

func TestOnHitProc_Deterministic(t *testing.T) {
	run := func() int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x5EED)
		s.mu.Lock()
		defer s.mu.Unlock()
		attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
		attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.5, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
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
