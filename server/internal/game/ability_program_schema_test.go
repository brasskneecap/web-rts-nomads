package game

import "testing"

func TestActionSchemasCoversRegistry(t *testing.T) {
	schemas := ActionSchemas()
	if len(schemas) != len(allActionTypes) {
		t.Fatalf("ActionSchemas len %d != allActionTypes %d", len(schemas), len(allActionTypes))
	}
	byType := map[ActionType]ActionSchema{}
	for _, s := range schemas {
		byType[s.Type] = s
	}
	// deal_damage is registered + runnable + has an "amount" field
	dd, ok := byType[ActionDealDamage]
	if !ok || !dd.Runnable {
		t.Fatalf("deal_damage missing/not runnable: %+v", dd)
	}
	hasAmount := false
	for _, f := range dd.Fields {
		if f.Key == "amount" {
			hasAmount = true
		}
	}
	if !hasAmount {
		t.Fatalf("deal_damage schema missing amount field")
	}
	// play_presentation has a registered descriptor (Phase 6b, Task 1) ->
	// listed, runnable, with its asset/position/scale/renderLayer/
	// presentationId fields plus bindToStatusDuration (the status-bound visual
	// half of a data-authored status — ability_exec_presentation.go).
	pp, ok := byType[ActionPlayPresentation]
	if !ok {
		t.Fatalf("play_presentation should be listed")
	}
	if !pp.Runnable {
		t.Fatalf("play_presentation should be runnable (has a registered executor)")
	}
	wantFields := map[string]bool{"asset": true, "position": true, "scale": true, "renderLayer": true, "presentationId": true, "bindToStatusDuration": true}
	if len(pp.Fields) != len(wantFields) {
		t.Fatalf("play_presentation schema fields = %+v; want keys %v", pp.Fields, wantFields)
	}
	for _, f := range pp.Fields {
		if !wantFields[f.Key] {
			t.Fatalf("play_presentation schema has unexpected field %q: %+v", f.Key, pp.Fields)
		}
	}
}

func TestProgramEnumsNonEmpty(t *testing.T) {
	e := ProgramEnums()
	for _, k := range []string{"entryTypes", "relations", "triggerTypes", "actionTypes", "targetSources", "targetOrigins", "targetOrderings"} {
		if len(e[k]) == 0 {
			t.Errorf("enum %q empty", k)
		}
	}
	// actionTypes must equal allActionTypes (no drift)
	if len(e["actionTypes"]) != len(allActionTypes) {
		t.Errorf("actionTypes drift")
	}
}
