package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// Equipment procs now CAST an ability at what they hit (castAbilityAsProcLocked)
// rather than firing a bespoke effect bolt. fire_bolt is a convenient proc
// ability: casting it launches a projectile, so "a proc fired" is observable as
// a new projectile in flight.

func TestOnHitProc_CastsAbilityDeterministically(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C0)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 50}
	s.nextUnitID++
	s.addUnitLocked(target)

	// chance 1.0 → the proc ability is cast on every hit (fire_bolt launches a bolt).
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Ability: "fire_bolt"}}
	before := len(s.Projectiles)
	deadUnitIDs := []int{}
	s.resolveAttackHitLocked(attacker, target, 1, &deadUnitIDs)
	if len(s.Projectiles) <= before {
		t.Fatalf("chance 1.0 should cast the proc ability (launching a bolt), got %d new projectiles", len(s.Projectiles)-before)
	}

	// chance 0.0 → never casts.
	s.Projectiles = nil
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.0, Ability: "fire_bolt"}}
	s.resolveAttackHitLocked(attacker, target, 1, &deadUnitIDs)
	if len(s.Projectiles) != 0 {
		t.Fatalf("chance 0.0 should cast nothing, got %d projectiles", len(s.Projectiles))
	}
}

// TestOnHitProc_RangedArrowLandingPersistsBolt drives the FULL ranged path: an
// in-flight arrow reaches its target inside tickProjectilesLocked, whose landing
// fires the on-hit proc — which now CASTS an ability that launches its own bolt.
// That bolt is appended to s.Projectiles DURING the tick's compaction loop, so
// it must survive the final list rebuild (regression for `s.Projectiles = kept`
// discarding a projectile appended while landing another).
func TestOnHitProc_RangedArrowLandingPersistsBolt(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C2)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Ability: "fire_bolt"}}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	// One in-flight arrow that reaches the target this tick.
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:               "arrow_test",
		OwnerUnitID:      attacker.ID,
		OwnerPlayerID:    attacker.OwnerID,
		TargetUnitID:     target.ID,
		Damage:           5,
		RemainingSeconds: 0.01,
		TotalSeconds:     1,
	})

	s.tickProjectilesLocked(0.1) // arrow lands → on-hit proc casts fire_bolt → launches a bolt

	// The arrow was consumed on landing, so any projectile still in flight is the
	// proc-cast bolt appended DURING the compaction loop — it must survive.
	for _, p := range s.Projectiles {
		if p.ID == "arrow_test" {
			t.Fatalf("the arrow should have landed and been removed, still present")
		}
	}
	if len(s.Projectiles) < 1 {
		t.Fatalf("the proc-cast bolt must persist after the arrow landed in tickProjectilesLocked; s.Projectiles is empty")
	}
}

func TestOnHitProc_Deterministic(t *testing.T) {
	run := func() int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x5EED)
		s.mu.Lock()
		defer s.mu.Unlock()
		attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
		attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.5, Ability: "fire_bolt"}}
		target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 50}
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
