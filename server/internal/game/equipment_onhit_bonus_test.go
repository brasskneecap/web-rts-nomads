package game

import "testing"

// equipForTest directly sets a unit's slot and recomputes — bypasses the vault/equip
// handlers since this test only exercises bonus aggregation.
func equipForTest(s *GameState, u *Unit, itemID string) {
	u.InventorySize++
	u.Equipped = append(u.Equipped, &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: itemID, Stacks: 1})
	s.recomputeUnitEquipmentBonusLocked(u)
}

// onHitElementalAmount returns the flat on-hit bonus an item grants for the given
// damage type (0 if none). Used to derive expected aggregates from the catalog
// rather than pinning literals that move when items are rebalanced.
func onHitElementalAmount(def *ItemDef, dt DamageType) int {
	for _, e := range def.OnHitElemental {
		if e.Type == dt {
			return e.Amount
		}
	}
	return 0
}

func TestEquipmentBonus_AggregatesElementalAndProc(t *testing.T) {
	fireRing, ok := getItemDef("fire_ring")
	if !ok {
		t.Fatal("fire_ring not found in catalog")
	}
	fireSword, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword not found in catalog")
	}
	ringFire := onHitElementalAmount(fireRing, DamageFire)
	swordFire := onHitElementalAmount(fireSword, DamageFire)
	if fireSword.Modifiers == nil {
		t.Fatal("fire_sword should carry stat modifiers")
	}
	swordDamage := fireSword.Modifiers.Damage
	if fireSword.OnHitProc == nil {
		t.Fatal("fire_sword should carry an on-hit proc")
	}
	wantProc, ok := fireSword.OnHitProc.ResolveParams()
	if !ok {
		t.Fatal("fire_sword on-hit proc failed to resolve params")
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x0E1)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := &Unit{ID: s.nextUnitID, OwnerID: "p1", UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(u)

	// One fire ring → its fire on-hit bonus, no proc.
	equipForTest(s, u, "fire_ring")
	if got := u.EquipmentBonus.OnHitElemental[DamageFire]; got != ringFire {
		t.Fatalf("after fire_ring: fire on-hit = %d, want %d (fire_ring bonus)", got, ringFire)
	}
	if len(u.EquipmentBonus.OnHitProcs) != 0 {
		t.Fatalf("fire_ring should carry no proc, got %d", len(u.EquipmentBonus.OnHitProcs))
	}

	// Add a fire sword → fire on-hit stacks additively (ring + sword) and one proc appears.
	equipForTest(s, u, "fire_sword")
	if got := u.EquipmentBonus.OnHitElemental[DamageFire]; got != ringFire+swordFire {
		t.Fatalf("after fire_ring+fire_sword: fire on-hit = %d, want %d (ring %d + sword %d)",
			got, ringFire+swordFire, ringFire, swordFire)
	}
	if got := u.EquipmentBonus.Damage; got != swordDamage {
		t.Fatalf("fire_sword physical modifier should add %d damage, got %d", swordDamage, got)
	}
	if len(u.EquipmentBonus.OnHitProcs) != 1 {
		t.Fatalf("fire_sword should carry exactly one proc, got %d", len(u.EquipmentBonus.OnHitProcs))
	}
	p := u.EquipmentBonus.OnHitProcs[0]
	if p.Params.Damage != wantProc.Damage || p.Params.DamageType != wantProc.DamageType || p.Params.ProjectileID != wantProc.ProjectileID || p.Chance <= 0 {
		t.Fatalf("fire_sword proc unexpected: got %+v, want params %+v with chance > 0", p, wantProc)
	}
}
