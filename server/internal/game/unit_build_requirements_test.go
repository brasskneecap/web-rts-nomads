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
