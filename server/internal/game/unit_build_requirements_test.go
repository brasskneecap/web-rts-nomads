package game

import (
	"testing"
)

// TestRequiresBuildings_FieldExistsOnUnitDef verifies the new field is
// readable on a loaded UnitDef. A missing field means later tasks can't
// compile.
func TestRequiresBuildings_FieldExistsOnUnitDef(t *testing.T) {
	def, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}
	// At this point in the plan the archer.json change has not landed
	// yet, so the field exists but is empty. Reading it confirms the
	// type compiles.
	_ = def.RequiresBuildings
}

// TestArcher_RequiresBlacksmith verifies the archer catalog declares the
// blacksmith requirement. Regression guard against an accidental JSON
// edit that drops the field.
func TestArcher_RequiresBlacksmith(t *testing.T) {
	def, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}
	if len(def.RequiresBuildings) != 1 || def.RequiresBuildings[0] != "blacksmith" {
		t.Errorf("archer.RequiresBuildings = %v; want [\"blacksmith\"]", def.RequiresBuildings)
	}
}

// TestSoldier_NoRequirements verifies the soldier (and by implication
// other unrequired units) is not gated. Regression guard against
// accidentally adding requirements to other units.
func TestSoldier_NoRequirements(t *testing.T) {
	def, ok := getUnitDef("soldier")
	if !ok {
		t.Fatal("soldier unit def not registered")
	}
	if len(def.RequiresBuildings) != 0 {
		t.Errorf("soldier.RequiresBuildings = %v; want []", def.RequiresBuildings)
	}
}
