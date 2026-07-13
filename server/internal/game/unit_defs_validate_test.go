package game

import "testing"

func validUnitDefForTest() UnitDef {
	return UnitDef{
		Type:        "test_unit",
		Faction:     "human",
		Name:        "Test Unit",
		HP:          100,
		Damage:      10,
		AttackRange: 1,
		AttackSpeed: 1,
		MoveSpeed:   2,
	}
}

func TestValidateUnitDef_ValidPasses(t *testing.T) {
	def := validUnitDefForTest()
	if err := validateUnitDef(&def); err != nil {
		t.Fatalf("expected valid def to pass, got %v", err)
	}
}

func TestValidateUnitDef_Rejections(t *testing.T) {
	cases := map[string]func(*UnitDef){
		"unknown damage type":   func(d *UnitDef) { d.DamageType = "not_a_real_type" },
		"unknown projectile":    func(d *UnitDef) { d.Projectile = "not_a_real_projectile" },
		"unknown building":      func(d *UnitDef) { d.RequiresBuildings = []string{"not_a_real_building"} },
		"bad targetable type":   func(d *UnitDef) { d.TargetableTypes = []string{"submarine"} },
		"dp chance > 1":         func(d *UnitDef) { d.DominionPointDropChance = 1.5 },
		"negative dp amount":    func(d *UnitDef) { d.DominionPointAmount = -1 },
		"negative projScale":    func(d *UnitDef) { d.ProjectileScale = -1 },
		"channel end < start":   func(d *UnitDef) { d.ChannelLoop = &ChannelLoopRange{Start: 5, End: 2} },
		"negative mana":         func(d *UnitDef) { d.MaxMana = -1 },
		"pathChances sum zero":  func(d *UnitDef) { d.PathChances = map[string]float64{"a": 0} },
		"unknown combatProfile": func(d *UnitDef) { d.CombatProfile = "not_a_profile" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			def := validUnitDefForTest()
			mutate(&def)
			if err := validateUnitDef(&def); err == nil {
				t.Fatalf("expected %s to be rejected, got nil", name)
			}
		})
	}
}
