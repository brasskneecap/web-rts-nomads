package game

import (
	"encoding/json"
	"math"
	"testing"
)

// TestPerkDef_PerkModifiers_DecodesTargetAndOps verifies a PerkDef decoded from
// JSON populates PerkModifiers with the target perk and its config ops verbatim.
func TestPerkDef_PerkModifiers_DecodesTargetAndOps(t *testing.T) {
	raw := `{
		"id": "test_perk",
		"displayName": "Test Perk",
		"perkModifiers": [
			{
				"target": "chain_siphon",
				"ops": [
					{"targetKey": "chainRange", "op": "mult", "sourceKey": "chainRangeMultiplier"},
					{"targetKey": "additionalTargetCount", "op": "add", "sourceKey": "chainAdditionalTargetCountBonus"}
				]
			}
		]
	}`
	var def PerkDef
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(def.PerkModifiers) != 1 {
		t.Fatalf("want 1 perk modifier, got %d", len(def.PerkModifiers))
	}
	pm := def.PerkModifiers[0]
	if pm.Target != "chain_siphon" || len(pm.Ops) != 2 {
		t.Fatalf("target/ops wrong: %+v", pm)
	}
	if pm.Ops[0] != (PerkConfigOp{TargetKey: "chainRange", Op: "mult", SourceKey: "chainRangeMultiplier"}) {
		t.Errorf("op[0] = %+v", pm.Ops[0])
	}
}

func validPerkWithModifier(pm PerkModifier) *PerkDef {
	return &PerkDef{ID: "test_perk", DisplayName: "Test Perk", PerkModifiers: []PerkModifier{pm}}
}

func TestValidatePerkDef_PerkModifiers_RejectsEmptyTarget(t *testing.T) {
	def := validPerkWithModifier(PerkModifier{Target: "", Ops: []PerkConfigOp{{TargetKey: "a", Op: "add", SourceKey: "b"}}})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for empty target, got nil")
	}
}

func TestValidatePerkDef_PerkModifiers_RejectsBadOp(t *testing.T) {
	def := validPerkWithModifier(PerkModifier{Target: "chain_siphon", Ops: []PerkConfigOp{{TargetKey: "a", Op: "divide", SourceKey: "b"}}})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for unknown op, got nil")
	}
}

func TestValidatePerkDef_PerkModifiers_RejectsEmptyKeys(t *testing.T) {
	def := validPerkWithModifier(PerkModifier{Target: "chain_siphon", Ops: []PerkConfigOp{{TargetKey: "", Op: "add", SourceKey: "b"}}})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for empty targetKey, got nil")
	}
	def = validPerkWithModifier(PerkModifier{Target: "chain_siphon", Ops: []PerkConfigOp{{TargetKey: "a", Op: "add", SourceKey: ""}}})
	if err := validatePerkDef(def); err == nil {
		t.Fatal("want error for empty sourceKey, got nil")
	}
}

func TestValidatePerkDef_PerkModifiers_AcceptsValid(t *testing.T) {
	def := validPerkWithModifier(PerkModifier{Target: "chain_siphon", Ops: []PerkConfigOp{
		{TargetKey: "chainRange", Op: "mult", SourceKey: "chainRangeMultiplier"},
		{TargetKey: "additionalTargetCount", Op: "add", SourceKey: "chainAdditionalTargetCountBonus"},
	}})
	if err := validatePerkDef(def); err != nil {
		t.Fatalf("want valid perk modifier to pass, got: %v", err)
	}
}

// TestApplyPerkModifiers_LayersFromOwnedPerkConfig exercises the generic engine
// directly against real catalog data: a caster owning ascended_corruption has
// its chain_siphon config multiplied/added from ascended_corruption's own
// config, and a target it doesn't modify is left untouched.
func TestApplyPerkModifiers_LayersFromOwnedPerkConfig(t *testing.T) {
	asc := perkDefByID("ascended_corruption")
	if asc == nil {
		t.Fatal("ascended_corruption must exist in the catalog")
	}
	ac := asc.ConfigForRank("")
	s := &GameState{}
	caster := &Unit{PerkIDs: []string{"ascended_corruption"}}

	cfg := map[string]float64{"chainRange": 100, "additionalTargetCount": 1}
	s.applyPerkModifiersLocked(caster, "chain_siphon", cfg)
	if got, want := cfg["chainRange"], 100*ac["chainRangeMultiplier"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("chainRange mult: got %.4f, want %.4f", got, want)
	}
	if got, want := cfg["additionalTargetCount"], 1+ac["chainAdditionalTargetCountBonus"]; math.Abs(got-want) > 1e-9 {
		t.Errorf("additionalTargetCount add: got %.4f, want %.4f", got, want)
	}

	// A perk with no modifier targeting this key leaves the config untouched.
	untouched := map[string]float64{"radius": 50}
	s.applyPerkModifiersLocked(caster, "not_a_target_perk", untouched)
	if untouched["radius"] != 50 {
		t.Errorf("non-targeted config mutated: %v", untouched)
	}

	// Not owning the modifier perk = no-op.
	none := &Unit{PerkIDs: nil}
	cfg2 := map[string]float64{"chainRange": 100}
	s.applyPerkModifiersLocked(none, "chain_siphon", cfg2)
	if cfg2["chainRange"] != 100 {
		t.Errorf("unowned modifier applied: %v", cfg2)
	}
}
