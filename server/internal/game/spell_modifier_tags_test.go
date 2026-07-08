package game

import (
	"encoding/json"
	"testing"
)

// Tags load from ability JSON and drive HasTag; absence yields an empty slice
// with no matches. Tags are pure modifier-targeting metadata (spell_modifier.go)
// and must not affect anything else about the def.
func TestAbility_TagsLoadAndMatch(t *testing.T) {
	var def AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"x","tags":["aoe","projectile"]}`), &def); err != nil {
		t.Fatalf("unmarshal tags: %v", err)
	}
	if len(def.Tags) != 2 || def.Tags[0] != "aoe" || def.Tags[1] != "projectile" {
		t.Fatalf("Tags = %v; want [aoe projectile]", def.Tags)
	}
	if !def.HasTag("aoe") || !def.HasTag("projectile") {
		t.Error("HasTag should be true for declared tags")
	}
	if def.HasTag("chain") || def.HasTag("") {
		t.Error("HasTag should be false for an undeclared or empty tag")
	}

	var none AbilityDef
	if err := json.Unmarshal([]byte(`{"id":"x"}`), &none); err != nil {
		t.Fatalf("unmarshal no-tags: %v", err)
	}
	if len(none.Tags) != 0 {
		t.Errorf("absent tags = %v; want empty", none.Tags)
	}
	if none.HasTag("aoe") {
		t.Error("a def with no tags must not match any tag")
	}
}
