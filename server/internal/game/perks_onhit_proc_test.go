package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// onHitProcAttacker spawns a wielder with a positive physical hit and a
// guaranteed fire_bolt on-hit proc, the setup shared by the cleave/whirlwind
// on-hit tests. fire_bolt is a projectile emitter, so a fired proc shows up as
// a new entry in s.Projectiles.
func onHitProcAttacker(t *testing.T, s *GameState) *Unit {
	t.Helper()
	a := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	a.Damage = 10
	a.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
	return a
}

func onHitProcEnemyAt(t *testing.T, s *GameState, x, y float64) *Unit {
	t.Helper()
	u := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: x, Y: y}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

// TestCleaveHit_FiresEquipmentProc asserts the cleaving_rage secondary hit
// triggers the attacker's equipment on-hit proc — a distinct hit should be able
// to trigger effects, matching the primary swing and Marksman split-shot.
func TestCleaveHit_FiresEquipmentProc(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xC1E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := onHitProcAttacker(t, s)
	primary := onHitProcEnemyAt(t, s, 60, 0)
	secondary := onHitProcEnemyAt(t, s, 100, 0) // within cleave splashRadius of primary

	dead := []int{}
	s.applyCleaveHitLocked(attacker, primary, 200 /*splashRadius*/, &dead)

	if len(s.Projectiles) != 1 {
		t.Fatalf("cleave secondary hit should fire the attacker's on-hit proc: want 1 proc projectile, got %d", len(s.Projectiles))
	}
	if got := s.Projectiles[0].TargetUnitID; got != secondary.ID {
		t.Errorf("proc should target the cleaved secondary %d, got %d", secondary.ID, got)
	}
}

// TestWhirlwindHit_FiresEquipmentProcPerHit asserts every whirlwind_core sweep
// hit rolls the attacker's on-hit proc — N enemies swept ⇒ N proc rolls (here,
// guaranteed, N procs). Each hit is its own hit.
func TestWhirlwindHit_FiresEquipmentProcPerHit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xC1F)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := onHitProcAttacker(t, s)
	// Two enemies inside the whirlwind radius, no designated primary (nil ⇒ all
	// in range are swept).
	e1 := onHitProcEnemyAt(t, s, 50, 0)
	e2 := onHitProcEnemyAt(t, s, 80, 0)

	dead := []int{}
	s.applyWhirlwindHitLocked(attacker, nil /*primaryTarget*/, 200 /*radius*/, &dead)

	if len(s.Projectiles) != 2 {
		t.Fatalf("each whirlwind hit should fire the on-hit proc: 2 enemies swept ⇒ want 2 proc projectiles, got %d", len(s.Projectiles))
	}
	hitTargets := map[int]bool{}
	for _, p := range s.Projectiles {
		hitTargets[p.TargetUnitID] = true
	}
	if !hitTargets[e1.ID] || !hitTargets[e2.ID] {
		t.Errorf("both swept enemies should get a proc: targets hit = %v (want %d and %d)", hitTargets, e1.ID, e2.ID)
	}
}

// TestWhirlwindHit_AlsoHitsPrimaryTarget asserts the primary target — the unit
// that triggered the whirlwind — takes a SECOND hit from the sweep (damage plus
// its own on-hit proc), rather than being excluded from the AoE.
func TestWhirlwindHit_AlsoHitsPrimaryTarget(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xC21)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := onHitProcAttacker(t, s)
	primary := onHitProcEnemyAt(t, s, 50, 0) // in whirlwind radius

	hpBefore := primary.HP
	dead := []int{}
	s.applyWhirlwindHitLocked(attacker, primary, 200, &dead)

	if primary.HP >= hpBefore {
		t.Fatalf("whirlwind should hit the primary a second time, but its HP is unchanged (%d)", primary.HP)
	}
	if len(s.Projectiles) != 1 {
		t.Fatalf("the primary's whirlwind hit should fire its on-hit proc: want 1 proc, got %d", len(s.Projectiles))
	}
	if got := s.Projectiles[0].TargetUnitID; got != primary.ID {
		t.Errorf("proc should target the primary %d, got %d", primary.ID, got)
	}
}

// TestWhirlwindHit_PrimaryDeathDeferredToCaller asserts that when the whirlwind
// hit KILLS the primary, this function does NOT award the kill or append it to
// deadUnitIDs — that is resolveAttackHitLocked's job (the fuller death handler).
// Prevents double kill-XP / double removal for the primary.
func TestWhirlwindHit_PrimaryDeathDeferredToCaller(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xC22)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := onHitProcAttacker(t, s)
	attacker.Damage = 100
	// Fragile primary the whirlwind hit will kill; plus a swept bystander that
	// whirlwind DOES own the kill for, as a contrast.
	primary := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 10, MaxHP: 10, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(primary)
	bystander := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 10, MaxHP: 10, X: 60, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(bystander)

	dead := []int{}
	s.applyWhirlwindHitLocked(attacker, primary, 200, &dead)

	for _, id := range dead {
		if id == primary.ID {
			t.Fatalf("whirlwind must not append the primary to deadUnitIDs — resolveAttackHitLocked owns the primary's death")
		}
	}
	// The swept bystander's death IS whirlwind's to handle.
	foundBystander := false
	for _, id := range dead {
		if id == bystander.ID {
			foundBystander = true
		}
	}
	if !foundBystander {
		t.Errorf("whirlwind should still own a swept (non-primary) victim's death; deadUnitIDs = %v", dead)
	}
}

// TestWhirlwindHit_NoProcWithoutEquipment guards the no-op path: a whirlwind
// from an attacker with no on-hit gear fires nothing extra (the equipment hook
// early-returns), so we haven't added cost or spurious projectiles to the
// common case.
func TestWhirlwindHit_NoProcWithoutEquipment(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xC20)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.Damage = 10
	onHitProcEnemyAt(t, s, 50, 0)

	dead := []int{}
	s.applyWhirlwindHitLocked(attacker, nil, 200, &dead)

	if len(s.Projectiles) != 0 {
		t.Fatalf("whirlwind without on-hit gear must not spawn proc projectiles, got %d", len(s.Projectiles))
	}
}
