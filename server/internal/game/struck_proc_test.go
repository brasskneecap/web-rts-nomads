package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// struckProcPair: defender ("p1") wearing a chance-1.0 struck proc, hostile
// attacker with deep HP. Defender evasion disabled so every hit lands.
func struckProcPair(t *testing.T, s *GameState) (defender, attacker *Unit) {
	t.Helper()
	defender = s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	defender.HP, defender.MaxHP = 1_000_000, 1_000_000
	disableEvasion(defender)
	defender.EquipmentBonus.OnStruckProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
	attacker = &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 10, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(attacker)
	return defender, attacker
}

// TestStruckProc_FiresAtAttackerOnLandedHit: a landed basic attack on the
// wearer launches the retaliation bolt AT THE ATTACKER, owned by the wearer.
func TestStruckProc_FiresAtAttackerOnLandedHit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C1)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)

	dead := []int{}
	s.resolveAttackHitLocked(attacker, defender, 10, &dead)

	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 retaliation bolt, got %d projectiles", len(s.Projectiles))
	}
	bolt := s.Projectiles[0]
	if bolt.TargetUnitID != attacker.ID || bolt.OwnerUnitID != defender.ID {
		t.Errorf("bolt target=%d owner=%d, want target=%d owner=%d (defender retaliates at attacker)", bolt.TargetUnitID, bolt.OwnerUnitID, attacker.ID, defender.ID)
	}
	if !bolt.SkipOnHitEffects {
		t.Error("retaliation bolt must skip the on-hit hub (no proc loops)")
	}
}

// TestStruckProc_NotTriggeredByProcDamage: proc-bolt damage landing on the
// wearer must NOT retaliate (SkipOnHitEffects bypasses resolveAttackHitLocked).
func TestStruckProc_NotTriggeredByProcDamage(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C2)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)

	dead := []int{}
	s.landProjectileLocked(&Projectile{ID: "bolt", OwnerUnitID: attacker.ID, TargetUnitID: defender.ID, Damage: 10, DamageType: DamageFire, SkipOnHitEffects: true}, defender, &dead)
	if len(s.Projectiles) != 0 {
		t.Fatalf("proc damage must not trigger retaliation, got %d projectiles", len(s.Projectiles))
	}
}

// TestStruckProc_DeadDefenderDoesNotRetaliate: a hit that kills the wearer
// fires no retaliation.
func TestStruckProc_DeadDefenderDoesNotRetaliate(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C3)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)
	defender.HP, defender.MaxHP = 1, 1

	dead := []int{}
	s.resolveAttackHitLocked(attacker, defender, 1_000_000, &dead)
	if len(s.Projectiles) != 0 {
		t.Fatalf("dead defender must not retaliate, got %d projectiles", len(s.Projectiles))
	}
}

// TestStruckProc_RangedAttackerGetsBoltBack: an arrow landing (full ranged
// path through landProjectileLocked → resolveAttackHitLocked) triggers
// retaliation homing back at the shooter.
func TestStruckProc_RangedAttackerGetsBoltBack(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x57C4)
	s.mu.Lock()
	defer s.mu.Unlock()
	defender, attacker := struckProcPair(t, s)

	dead := []int{}
	s.landProjectileLocked(&Projectile{ID: "arrow", OwnerUnitID: attacker.ID, OwnerPlayerID: attacker.OwnerID, TargetUnitID: defender.ID, Damage: 10}, defender, &dead)
	found := false
	for _, p := range s.Projectiles {
		if p.SkipOnHitEffects && p.TargetUnitID == attacker.ID && p.OwnerUnitID == defender.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("ranged attacker must eat a retaliation bolt after their arrow lands")
	}
}

// TestValidateItemDef_OnStruckProc: onStruckProc obeys the same rules as
// onHitProc (chance range, effect required + registered).
func TestValidateItemDef_OnStruckProc(t *testing.T) {
	good := &ItemDef{ID: "ok", Kind: ItemKindEquipment, OnStruckProc: &ItemOnHitProc{Chance: 0.1, Effect: "fire_bolt_ignite"}}
	if err := validateItemDef(good); err != nil {
		t.Fatalf("valid onStruckProc rejected: %v", err)
	}
	unknown := &ItemDef{ID: "bad", OnStruckProc: &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}}
	if err := validateItemDef(unknown); err == nil {
		t.Error("expected error for unregistered onStruckProc.effect, got nil")
	}
	badChance := &ItemDef{ID: "bad2", OnStruckProc: &ItemOnHitProc{Chance: 1.5, Effect: "fire_bolt_ignite"}}
	if err := validateItemDef(badChance); err == nil {
		t.Error("expected error for onStruckProc.chance > 1, got nil")
	}
}
