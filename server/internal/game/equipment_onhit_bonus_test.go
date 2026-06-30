package game

import "testing"

// equipForTest directly sets a unit's slot and recomputes — bypasses the vault/equip
// handlers since this test only exercises bonus aggregation.
func equipForTest(s *GameState, u *Unit, itemID string) {
	u.InventorySize++
	u.Equipped = append(u.Equipped, &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: itemID, Stacks: 1})
	s.recomputeUnitEquipmentBonusLocked(u)
}

func TestEquipmentBonus_AggregatesElementalAndProc(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x0E1)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := &Unit{ID: s.nextUnitID, OwnerID: "p1", UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(u)

	// One fire ring → +5 fire on-hit, no proc.
	equipForTest(s, u, "fire_ring")
	if got := u.EquipmentBonus.OnHitElemental[DamageFire]; got != 5 {
		t.Fatalf("after fire_ring: fire on-hit = %d, want 5", got)
	}
	if len(u.EquipmentBonus.OnHitProcs) != 0 {
		t.Fatalf("fire_ring should carry no proc, got %d", len(u.EquipmentBonus.OnHitProcs))
	}

	// Add a fire sword → fire on-hit stacks to 10 (ring 5 + sword 5) and one proc appears.
	equipForTest(s, u, "fire_sword")
	if got := u.EquipmentBonus.OnHitElemental[DamageFire]; got != 10 {
		t.Fatalf("after fire_ring+fire_sword: fire on-hit = %d, want 10", got)
	}
	if got := u.EquipmentBonus.Damage; got != 5 {
		t.Fatalf("fire_sword physical modifier should add 5 damage, got %d", got)
	}
	if len(u.EquipmentBonus.OnHitProcs) != 1 {
		t.Fatalf("fire_sword should carry exactly one proc, got %d", len(u.EquipmentBonus.OnHitProcs))
	}
	p := u.EquipmentBonus.OnHitProcs[0]
	if p.Damage != 25 || p.DamageType != DamageFire || p.ProjectileID != "fire_bolt" || p.Chance <= 0 {
		t.Fatalf("fire_sword proc unexpected: %+v", p)
	}
}
