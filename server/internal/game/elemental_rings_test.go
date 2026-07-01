package game

import "testing"

func TestElementalRings_Load(t *testing.T) {
	cases := []struct {
		id   string
		elem DamageType
	}{
		{"fire_ring", DamageFire},
		{"ice_ring", DamageFrost},
		{"lightning_ring", DamageLightning},
	}
	for _, tc := range cases {
		def, ok := getItemDef(tc.id)
		if !ok {
			t.Fatalf("item def %q not found", tc.id)
		}
		if def.Kind != ItemKindEquipment {
			t.Errorf("%s: kind = %q, want equipment", tc.id, def.Kind)
		}
		if def.SlotKind != ItemSlotKindAny {
			t.Errorf("%s: slotKind = %q, want any", tc.id, def.SlotKind)
		}
		var amount int
		for _, e := range def.OnHitElemental {
			if e.Type == tc.elem {
				amount = e.Amount
			}
		}
		if amount != 5 {
			t.Errorf("%s: onHitElemental %v = %d, want 5", tc.id, tc.elem, amount)
		}
	}
}
