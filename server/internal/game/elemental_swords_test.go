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
		id             string
		wantElement    DamageType
		wantProjectile string
	}{
		{"fire_sword", DamageFire, "fire_bolt"},
		{"frost_sword", DamageCold, "frost_bolt"},
		{"lightning_sword", DamageLightning, "lightning_bolt"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getItemDef(tc.id)
			if !ok {
				t.Fatalf("%s: not found in catalog", tc.id)
			}

			// Positive flat damage modifier (exact value is a catalog tunable).
			if def.Modifiers == nil || def.Modifiers.Damage <= 0 {
				t.Fatalf("%s: expected a positive Modifiers.Damage, got %+v", tc.id, def.Modifiers)
			}

			// onHitElemental must contain an entry of the correct element with a
			// positive amount. The amount itself is a balance tunable.
			found := false
			for _, e := range def.OnHitElemental {
				if e.Type == tc.wantElement && e.Amount > 0 {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s: expected an onHitElemental entry of type %s with a positive amount, got %+v", tc.id, tc.wantElement, def.OnHitElemental)
			}

			// onHitProc structural wiring: the proc must fire the element's own
			// projectile for positive damage at a valid probability. Damage and
			// chance are catalog-owned tunables, so assert invariants not numbers.
			if def.OnHitProc == nil {
				t.Fatalf("%s: OnHitProc is nil", tc.id)
			}
			params, ok := def.OnHitProc.ResolveParams()
			if !ok {
				t.Fatalf("%s: onHitProc.effect %q is not a registered proc effect", tc.id, def.OnHitProc.Effect)
			}
			if params.Damage <= 0 {
				t.Errorf("%s: OnHitProc.Damage want > 0, got %d", tc.id, params.Damage)
			}
			if params.DamageType != tc.wantElement {
				t.Errorf("%s: OnHitProc.DamageType want %s, got %s", tc.id, tc.wantElement, params.DamageType)
			}
			if params.ProjectileID != tc.wantProjectile {
				t.Errorf("%s: OnHitProc.ProjectileID want %q, got %q", tc.id, tc.wantProjectile, params.ProjectileID)
			}
			if def.OnHitProc.Chance <= 0 || def.OnHitProc.Chance > 1 {
				t.Errorf("%s: OnHitProc.Chance %v is not a valid probability in (0,1]", tc.id, def.OnHitProc.Chance)
			}
		})
	}
}

func TestFireSword_EndToEnd(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatalf("fire_sword not found")
	}
	if def.Modifiers == nil || def.Modifiers.Damage <= 0 {
		t.Fatalf("fire_sword should grant positive physical damage, got %+v", def.Modifiers)
	}
	if def.OnHitProc == nil {
		t.Fatalf("fire_sword has no onHitProc")
	}
	params, ok := def.OnHitProc.ResolveParams()
	if !ok || params.Damage <= 0 || params.DamageType != DamageFire || params.ProjectileID != "fire_bolt" {
		t.Fatalf("fire_sword proc unexpected: resolved=%+v ok=%v", params, ok)
	}
	if def.OnHitProc.Chance <= 0 || def.OnHitProc.Chance > 1 {
		t.Fatalf("fire_sword proc chance %v is not a valid probability in (0,1]", def.OnHitProc.Chance)
	}

	// The fire on-hit amount is a catalog tunable; derive the expected values
	// below from the def rather than pinning them so a rebalance can't break
	// this mechanic test.
	var wantFire int
	for _, e := range def.OnHitElemental {
		if e.Type == DamageFire {
			wantFire = e.Amount
			break
		}
	}
	if wantFire <= 0 {
		t.Fatalf("fire_sword should have a positive fire on-hit amount, got %+v", def.OnHitElemental)
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

	if attacker.EquipmentBonus.OnHitElemental[DamageFire] != wantFire {
		t.Fatalf("equipped fire_sword: fire on-hit = %d, want %d", attacker.EquipmentBonus.OnHitElemental[DamageFire], wantFire)
	}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	s.resetMinorDamageEventsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical 10 + fire on-hit as a separate instance → 100 - 10 - wantFire.
	const physical = 10
	wantHP := target.MaxHP - physical - wantFire
	s.resolveAttackHitLocked(attacker, target, physical, &deadUnitIDs)
	if target.HP != wantHP {
		t.Fatalf("expected HP %d (100 - %d physical - %d fire), got %d", wantHP, physical, wantFire, target.HP)
	}
	// The sword's flat fire component renders as its own side-popup (minor
	// event), not a tint on the main number.
	if e := findMinorEvent(s, target.ID, "fire"); e == nil || e.Damage != wantFire {
		t.Fatalf("expected a fire minor (side) popup of %d from the sword's elemental instance; queue: %+v", wantFire, s.minorDamageEventsThisTick)
	}
}
