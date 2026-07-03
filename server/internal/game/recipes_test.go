package game

import "testing"

func TestRecipeCatalog_LoadsAndValidates(t *testing.T) {
	cases := []struct {
		id     string
		inputs []string
		output string
	}{
		{"fire_sword", []string{"broad_sword", "fire_ring"}, "fire_sword"},
		{"frost_sword", []string{"broad_sword", "ice_ring"}, "frost_sword"},
		{"lightning_sword", []string{"broad_sword", "lightning_ring"}, "lightning_sword"},
	}
	for _, tc := range cases {
		def, ok := getRecipeDef(tc.id)
		if !ok {
			t.Fatalf("recipe %q not found", tc.id)
		}
		if def.Output != tc.output {
			t.Errorf("%s: output = %q, want %q", tc.id, def.Output, tc.output)
		}
		if len(def.Inputs) != len(tc.inputs) {
			t.Fatalf("%s: %d inputs, want %d", tc.id, len(def.Inputs), len(tc.inputs))
		}
		for i, in := range tc.inputs {
			if def.Inputs[i] != in {
				t.Errorf("%s: input[%d] = %q, want %q", tc.id, i, def.Inputs[i], in)
			}
		}
		if def.CostGold <= 0 {
			t.Errorf("%s: costGold must be positive, got %d", tc.id, def.CostGold)
		}
	}
	if len(ListRecipeDefs()) < 3 {
		t.Fatalf("expected >=3 recipes, got %d", len(ListRecipeDefs()))
	}
}

func TestValidateRecipeDef_Rules(t *testing.T) {
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword"}, Output: "fire_sword"}); err == nil {
		t.Error("expected error for <2 inputs")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "no_such_item"}, Output: "fire_sword"}); err == nil {
		t.Error("expected error for unknown input item")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "fire_ring"}, Output: "no_such_item"}); err == nil {
		t.Error("expected error for unknown output item")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "fire_ring"}, CostGold: -1, Output: "fire_sword"}); err == nil {
		t.Error("expected error for negative costGold")
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 0, Output: "fire_sword"}); err != nil {
		t.Errorf("zero costGold should be allowed (ingredient-only recipe): %v", err)
	}
	if err := validateRecipeDef(&RecipeDef{ID: "r", Inputs: []string{"broad_sword", "fire_ring"}, CostGold: 150, Output: "fire_sword"}); err != nil {
		t.Errorf("valid recipe rejected: %v", err)
	}
}
