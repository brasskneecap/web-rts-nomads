package game

import "testing"

func floorValidUnit() UnitDef {
	return UnitDef{
		Type: "floor_test", Faction: "human", Name: "Floor Test",
		HP: 100, MoveSpeed: 60, Damage: 10, AttackRange: 32, AttackSpeed: 1,
	}
}

func TestValidateUnitDef_StatFloors(t *testing.T) {
	cases := map[string]func(*UnitDef){
		"zero hp":                       func(d *UnitDef) { d.HP = 0 },
		"negative hp":                   func(d *UnitDef) { d.HP = -1 },
		"zero move speed":               func(d *UnitDef) { d.MoveSpeed = 0 },
		"attacker with no range":        func(d *UnitDef) { d.AttackRange = 0 },
		"attacker with no attack speed": func(d *UnitDef) { d.AttackSpeed = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			def := floorValidUnit()
			mutate(&def)
			if err := validateUnitDef(&def); err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}

// A unit authored with no attack at all (damage omitted ⇒ 0) must pass the
// attack floors. This is the ONLY thing that exempts it — the validator never
// reads NonCombat.
func TestValidateUnitDef_ZeroDamageNeedsNoAttackFields(t *testing.T) {
	def := UnitDef{
		Type: "gatherer", Faction: "human", Name: "Gatherer",
		HP: 60, MoveSpeed: 55,
	}
	if err := validateUnitDef(&def); err != nil {
		t.Fatalf("a damage-less unit must pass the attack floors, got %v", err)
	}
}

// THE GUARD. Every def the game actually ships must satisfy the new rules. A
// failure here means a floor is stricter than real content — relax the floor,
// do NOT "fix" the catalog.
func TestValidateUnitDef_EveryEmbeddedUnitPasses(t *testing.T) {
	for _, def := range ListUnitDefs() {
		def := def
		t.Run(def.Type, func(t *testing.T) {
			if err := validateUnitDef(&def); err != nil {
				t.Fatalf("shipped unit %q fails validation: %v — the new floor is too strict for real content", def.Type, err)
			}
		})
	}
}
