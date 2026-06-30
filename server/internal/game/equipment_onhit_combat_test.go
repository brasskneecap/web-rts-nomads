package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestOnHitElemental_AppliesSeparateTypedInstance(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE1E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = "" // physical basic attack
	// Give the attacker a +5 fire on-hit bonus directly.
	attacker.EquipmentBonus.OnHitElemental = map[DamageType]int{DamageFire: 5}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical hit of 8; armor 0 so the physical lands as 8 and fire as 5 → HP 87.
	s.resolveAttackHitLocked(attacker, target, 8, &deadUnitIDs)

	if target.HP != 87 {
		t.Fatalf("expected HP 87 (100 - 8 physical - 5 fire), got %d", target.HP)
	}
	// The fire instance must emit a "fire" colored hint (proof it was a typed,
	// separate damage event — physical emits none).
	if hint := findHint(s, target.ID, "fire"); hint == nil {
		t.Fatalf("expected a fire damage-type hint from the elemental on-hit instance; queue: %+v", s.damageTypeHintsThisTick)
	}
}
