package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestCraftedSwords_LoadAllThree verifies that all three crafted elemental
// swords load from the catalog with the correct flat-damage modifier,
// on-hit elemental bonus, and proc definition.
func TestCraftedSwords_LoadAllThree(t *testing.T) {
	cases := []struct {
		id              string
		wantElement     DamageType
		wantProjectile  string
	}{
		{"fire_sword", DamageFire, "fire_bolt"},
		{"frost_sword", DamageFrost, "frost_bolt"},
		{"lightning_sword", DamageLightning, "lightning_bolt"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getItemDef(tc.id)
			if !ok {
				t.Fatalf("%s: not found in catalog", tc.id)
			}

			// +5 flat damage modifier.
			if def.Modifiers == nil || def.Modifiers.Damage != 5 {
				t.Fatalf("%s: Modifiers.Damage want 5, got %+v", tc.id, def.Modifiers)
			}

			// onHitElemental must contain exactly one entry of the correct type with amount 5.
			found := false
			for _, e := range def.OnHitElemental {
				if e.Type == tc.wantElement && e.Amount == 5 {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s: expected onHitElemental entry {%s, 5}, got %+v", tc.id, tc.wantElement, def.OnHitElemental)
			}

			// onHitProc must specify damage=25, correct damageType, correct projectileID, chance ~0.05.
			if def.OnHitProc == nil {
				t.Fatalf("%s: OnHitProc is nil", tc.id)
			}
			if def.OnHitProc.Damage != 25 {
				t.Errorf("%s: OnHitProc.Damage want 25, got %d", tc.id, def.OnHitProc.Damage)
			}
			if def.OnHitProc.DamageType != tc.wantElement {
				t.Errorf("%s: OnHitProc.DamageType want %s, got %s", tc.id, tc.wantElement, def.OnHitProc.DamageType)
			}
			if def.OnHitProc.ProjectileID != tc.wantProjectile {
				t.Errorf("%s: OnHitProc.ProjectileID want %q, got %q", tc.id, tc.wantProjectile, def.OnHitProc.ProjectileID)
			}
			if def.OnHitProc.Chance < 0.049 || def.OnHitProc.Chance > 0.051 {
				t.Errorf("%s: OnHitProc.Chance want ~0.05, got %v", tc.id, def.OnHitProc.Chance)
			}
		})
	}
}

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
