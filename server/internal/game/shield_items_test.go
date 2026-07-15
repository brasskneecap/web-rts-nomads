package game

import (
	"encoding/json"
	"testing"
)

// TestShieldItems_CatalogWiring guards the six new defs: identity, tier,
// category, and stat invariants (positive armor; block/dodge in (0,1)).
// Numbers are catalog-owned tunables — assert invariants, not values.
func TestShieldItems_CatalogWiring(t *testing.T) {
	cases := []struct {
		id        string
		tier      ItemTier
		wantBlock bool
		wantDodge bool
	}{
		{"rusty_shield", ItemTierCommon, true, false},
		{"steel_shield", ItemTierUncommon, true, false},
		{"fire_shield", ItemTierRare, true, false},
		{"frost_shield", ItemTierRare, true, false},
		{"lightning_shield", ItemTierRare, true, false},
		{"elven_cloak", ItemTierUncommon, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getItemDef(tc.id)
			if !ok {
				t.Fatalf("%s not in catalog", tc.id)
			}
			if def.Tier != tc.tier {
				t.Errorf("tier = %s, want %s", def.Tier, tc.tier)
			}
			if def.Modifiers == nil || def.Modifiers.Armor <= 0 {
				t.Fatalf("%s must grant positive armor, got %+v", tc.id, def.Modifiers)
			}
			if tc.wantBlock && !(def.Modifiers.BlockChance > 0 && def.Modifiers.BlockChance < 1) {
				t.Errorf("blockChance = %v, want in (0,1)", def.Modifiers.BlockChance)
			}
			if tc.wantDodge && !(def.Modifiers.DodgeChance > 0 && def.Modifiers.DodgeChance < 1) {
				t.Errorf("dodgeChance = %v, want in (0,1)", def.Modifiers.DodgeChance)
			}
		})
	}
}

// TestElementalShields_StruckProcWiring: each elemental shield references its
// element's shipped proc effect on the onStruck trigger at a valid chance.
func TestElementalShields_StruckProcWiring(t *testing.T) {
	wiring := map[string]string{
		"fire_shield":      "fire_bolt_ignite",
		"frost_shield":     "frost_bolt_chill",
		"lightning_shield": "lightning_chain",
	}
	for id, effect := range wiring {
		def, ok := getItemDef(id)
		if !ok {
			t.Fatalf("%s not in catalog", id)
		}
		p := firstProcFor(t, def, ProcOnStruck)
		if p.Effect != effect {
			t.Errorf("%s effect = %q, want %q", id, p.Effect, effect)
		}
		if p.Chance <= 0 || p.Chance > 1 {
			t.Errorf("%s chance %v not a valid probability in (0,1]", id, p.Chance)
		}
		if _, ok := p.ResolveParams(); !ok {
			t.Errorf("%s onStruck proc does not resolve", id)
		}
	}
}

// TestShieldRecipes_Wiring: steel_shield + element ring → element shield,
// mirroring the sword recipes.
func TestShieldRecipes_Wiring(t *testing.T) {
	wiring := map[string][2]string{
		"fire_shield":      {"steel_shield", "fire_ring"},
		"frost_shield":     {"steel_shield", "ice_ring"},
		"lightning_shield": {"steel_shield", "lightning_ring"},
	}
	for id, inputs := range wiring {
		def, ok := getItemDef(id)
		if !ok {
			t.Fatalf("item %s not in catalog", id)
		}
		if !def.IsCraftable() {
			t.Fatalf("item %s should be craftable", id)
		}
		c := def.Crafting
		if len(c.Inputs) != 2 || c.Inputs[0] != inputs[0] || c.Inputs[1] != inputs[1] {
			t.Errorf("item %s crafting inputs = %v, want %v", id, c.Inputs, inputs)
		}
		if c.CraftCostGold <= 0 {
			t.Errorf("item %s craftCostGold = %d, want > 0", id, c.CraftCostGold)
		}
		if c.RecipeCostGold <= 0 {
			t.Errorf("item %s recipeCostGold = %d, want > 0", id, c.RecipeCostGold)
		}
	}
}

// TestOnStruckProc_MarshalEmitsResolvedPayload guards the client wire
// contract for the struck-proc mirror, same as the onHit proc wire test.
func TestOnStruckProc_MarshalEmitsResolvedPayload(t *testing.T) {
	def, ok := getItemDef("fire_shield")
	if !ok {
		t.Fatal("fire_shield not in catalog")
	}
	proc := firstProcFor(t, def, ProcOnStruck)
	data, err := json.Marshal(proc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	params, _ := proc.ResolveParams()
	if wire["trigger"] != string(ProcOnStruck) {
		t.Errorf("wire trigger = %v, want %v", wire["trigger"], ProcOnStruck)
	}
	if wire["effect"] != proc.Effect {
		t.Errorf("wire effect = %v, want %v", wire["effect"], proc.Effect)
	}
	if wire["damage"] != float64(params.Damage) {
		t.Errorf("wire damage = %v, want %v (resolved)", wire["damage"], params.Damage)
	}
	if wire["damageType"] != string(params.DamageType) {
		t.Errorf("wire damageType = %v, want %v", wire["damageType"], params.DamageType)
	}
}
