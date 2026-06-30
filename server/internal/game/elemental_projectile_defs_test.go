package game

import "testing"

func TestElementalBoltProjectileDefs_Load(t *testing.T) {
	for _, id := range []string{"fire_bolt", "frost_bolt", "lightning_bolt"} {
		def, ok := getProjectileDef(id)
		if !ok {
			t.Fatalf("projectile def %q not found in catalog", id)
		}
		if def.Speed <= 0 {
			t.Fatalf("projectile def %q has non-positive speed %v", id, def.Speed)
		}
	}
}
