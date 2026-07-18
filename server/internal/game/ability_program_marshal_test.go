package game

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAbilityProgramRoundTripPreservesUnknownKeys(t *testing.T) {
	src := `{
		"entry": {"type":"unit","relations":["self"],"range":"match_attack_range"},
		"futureTopLevelKey": {"x": 1},
		"triggers": [{"id":"t","type":"on_cast_complete","actions":[
			{"id":"a","type":"deal_damage","config":{"amount":10,"futureCfgKey":"keepme"}}
		]}]
	}`
	var p AbilityProgram
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "futureTopLevelKey") {
		t.Errorf("lost unknown top-level key: %s", s)
	}
	if !strings.Contains(s, "keepme") {
		t.Errorf("lost unknown action config key: %s", s)
	}
}

func TestAbilityProgramRoundTripStable(t *testing.T) {
	src := `{"entry":{"type":"self","range":0},"unknownX":1,"triggers":[]}`
	var p AbilityProgram
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Remainder must hold ONLY the unknown key, not any known field.
	if _, ok := p.Remainder["unknownX"]; !ok {
		t.Fatalf("unknownX not captured in Remainder: %+v", p.Remainder)
	}
	if _, ok := p.Remainder["entry"]; ok {
		t.Fatalf("known key 'entry' leaked into Remainder")
	}
	if _, ok := p.Remainder["triggers"]; ok {
		t.Fatalf("known key 'triggers' leaked into Remainder")
	}
	// Marshal twice; the two encodings must be identical (idempotent, no key dupes).
	out1, _ := json.Marshal(p)
	var p2 AbilityProgram
	if err := json.Unmarshal(out1, &p2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	out2, _ := json.Marshal(p2)
	if string(out1) != string(out2) {
		t.Fatalf("round-trip not idempotent:\n1: %s\n2: %s", out1, out2)
	}
}
