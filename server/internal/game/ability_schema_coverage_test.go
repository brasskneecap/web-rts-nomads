package game

import (
	"reflect"
	"strings"
	"testing"
)

// schemaExemptConfigFields are scalar config fields DELIBERATELY absent from
// their action's Schema. Every entry needs a reason, because the default is that
// a field the executor reads must be editable — an unlisted omission is the bug
// this test exists to catch.
var schemaExemptConfigFields = map[ActionType]map[string]string{
	ActionDealDamage: {
		// Compiler-fed, never authored: the meteor/impact compilers set it via
		// Input["targets"], and this action has never resolved a radius of its
		// own. An inspector field would advertise a capability it lacks.
		"radius": "compiler-only; see the field's doc comment",
		// chain_lightning's bounce falloff, emitted by compileChainLightningActions
		// to reproduce legacy's "subtract an unfolded amount from the folded
		// primary". Authored programs express per-hop decay with a loop variable.
		"flatOffset": "compiler-only legacy-parity field",
	},
	ActionApplyStatus: {
		// apply_status is the LEGACY status primitive: no catalog ability authors
		// it, it exists for compileLegacyAbility's output. Authored programs use
		// apply_status_duration, whose duration IS surfaced.
		"duration": "legacy compiler-only action",
	},
}

// TestActionSchema_CoversEveryScalarConfigField enforces this project's standing
// rule that no ability value may come from somewhere the editor cannot show.
//
// It exists because it was violated in exactly the way it is designed to catch:
// deal_damage's adRatio/apRatio shipped with the config struct and the executor
// wired but the two SchemaField entries silently missing, so ability-power
// scaling ran correctly and was completely unauthorable. A test that only
// covered behavior would have passed.
//
// Only SCALAR fields are checked. Slice/map/pointer fields (Triggers, Body,
// Vars, ContextRefs, TargetQueryDefs) are rendered by their own dedicated UI —
// nested cards, target-query editors, loop-variable rows — rather than as a flat
// inspector control.
func TestActionSchema_CoversEveryScalarConfigField(t *testing.T) {
	for _, at := range allActionTypes {
		desc, ok := lookupActionDescriptor(at)
		if !ok || desc.Decode == nil {
			continue // deferred action type with no descriptor yet
		}
		cfg, err := desc.Decode(nil)
		if err != nil || cfg == nil {
			continue
		}
		rt := reflect.TypeOf(cfg)
		if rt.Kind() != reflect.Struct {
			continue
		}

		declared := make(map[string]bool, len(desc.Schema.Fields))
		for _, f := range desc.Schema.Fields {
			declared[f.Key] = true
		}
		exempt := schemaExemptConfigFields[at]

		for i := 0; i < rt.NumField(); i++ {
			tag := rt.Field(i).Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			key := strings.Split(tag, ",")[0]
			if key == "" || declared[key] || exempt[key] != "" {
				continue
			}
			switch rt.Field(i).Type.Kind() {
			case reflect.Int, reflect.Float64, reflect.Bool, reflect.String:
				t.Errorf("action %q config field %q has no SchemaField — the executor reads it, so the editor must be able to set it. "+
					"Add it to the action's Schema, or add it to schemaExemptConfigFields WITH A REASON if it is genuinely compiler-only.",
					at, key)
			}
		}
	}
}

// TestSchemaExemptConfigFields_AreStillReal keeps the exemption list from
// outliving the fields it excuses: an entry naming a field that no longer exists
// is stale, and would silently excuse a NEW field that happened to reuse the
// name.
func TestSchemaExemptConfigFields_AreStillReal(t *testing.T) {
	for at, fields := range schemaExemptConfigFields {
		desc, ok := lookupActionDescriptor(at)
		if !ok {
			t.Errorf("exemption list names unregistered action %q", at)
			continue
		}
		cfg, err := desc.Decode(nil)
		if err != nil || cfg == nil {
			continue
		}
		rt := reflect.TypeOf(cfg)
		present := map[string]bool{}
		for i := 0; i < rt.NumField(); i++ {
			present[strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]] = true
		}
		for key, reason := range fields {
			if reason == "" {
				t.Errorf("%s.%s is exempt with no reason", at, key)
			}
			if !present[key] {
				t.Errorf("%s.%s is exempt but no longer exists — drop the entry", at, key)
			}
		}
	}
}
