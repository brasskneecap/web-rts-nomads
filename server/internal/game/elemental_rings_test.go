package game

import "testing"

func TestElementalRings_Load(t *testing.T) {
	cases := []struct {
		id   string
		elem DamageType
	}{
		{"fire_ring", DamageFire},
		{"ice_ring", DamageCold},
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
		var amount int
		var found bool
		for _, e := range def.OnHitElemental {
			if e.Type == tc.elem {
				amount = e.Amount
				found = true
			}
		}
		// The magnitude is a balance tunable owned by the item JSON; assert the
		// ring carries a positive on-hit bonus of the right element, not its value.
		if !found {
			t.Errorf("%s: missing onHitElemental of type %v", tc.id, tc.elem)
		} else if amount <= 0 {
			t.Errorf("%s: onHitElemental %v = %d, want > 0", tc.id, tc.elem, amount)
		}
	}
}
