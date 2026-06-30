package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestFireSword_EndToEnd(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatalf("fire_sword not found")
	}
	if def.Modifiers == nil || def.Modifiers.Damage != 5 {
		t.Fatalf("fire_sword should grant +5 physical damage, got %+v", def.Modifiers)
	}
	if def.OnHitProc == nil || def.OnHitProc.Damage != 25 || def.OnHitProc.DamageType != DamageFire || def.OnHitProc.ProjectileID != "fire_bolt" {
		t.Fatalf("fire_sword proc unexpected: %+v", def.OnHitProc)
	}
	if def.OnHitProc.Chance < 0.049 || def.OnHitProc.Chance > 0.051 {
		t.Fatalf("fire_sword proc chance should be ~0.05, got %v", def.OnHitProc.Chance)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF12E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = ""
	// Equip the fire sword via direct slot set + recompute.
	attacker.InventorySize++
	attacker.Equipped = append(attacker.Equipped, &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: "fire_sword", Stacks: 1})
	s.recomputeUnitEquipmentBonusLocked(attacker)

	if attacker.EquipmentBonus.OnHitElemental[DamageFire] != 5 {
		t.Fatalf("equipped fire_sword: fire on-hit = %d, want 5", attacker.EquipmentBonus.OnHitElemental[DamageFire])
	}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical 10 + 5 fire separate instance → HP 85.
	s.resolveAttackHitLocked(attacker, target, 10, &deadUnitIDs)
	if target.HP != 85 {
		t.Fatalf("expected HP 85 (100 - 10 physical - 5 fire), got %d", target.HP)
	}
	if hint := findHint(s, target.ID, "fire"); hint == nil {
		t.Fatalf("expected a fire damage-type hint from the sword's elemental instance")
	}
}
