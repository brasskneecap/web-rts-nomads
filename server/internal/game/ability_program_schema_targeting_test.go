package game

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Drift guard: every registered ActionDescriptor's declared targeting shape
// (does it carry a `target_query`-control SchemaField?) must match what its
// Execute actually reads. wantTargetQuery is a hand-maintained verdict table
// (same discipline as the perk wired/inert set) derived from reading each
// Execute — see the per-entry comment for the file/reasoning. A registered
// action type missing from this table, or one whose declared shape doesn't
// match the verdict, fails loudly rather than silently drifting.
// ─────────────────────────────────────────────────────────────────────────────

func TestActionTargetingShape_MatchesExecuteUsage(t *testing.T) {
	wantTargetQuery := map[ActionType]bool{
		// Its whole job IS resolving a query and handing back the result —
		// the canonical "full query" consumer.
		ActionSelectTargets: true,
		// Loops over every resolved target and fires one homing projectile at
		// each (ability_exec_projectile.go Execute, "to_target" branch) — the
		// compiler always gives it its own direct TargetQueryDef
		// (SrcInitialTarget) rather than chaining off a preceding
		// select_targets action.
		ActionLaunchProjectile: true,
		// The unified beam action (ability_exec_beam.go): momentary loops over
		// every resolved target; channeled reads targets[0] as the ONE unit it
		// channels at. Both the chain compiler and compileChannelBeamAction give
		// it its own direct TargetQueryDef — same reasoning as
		// ActionLaunchProjectile above.
		ActionBeam: true,

		// Every action below reads its incoming `targets` slice (or ignores
		// targets entirely) but is normally fed via a preceding
		// select_targets action's Outputs + Input["targets"], or falls back
		// to previous_action_targets (ctx.Selected) — never its own direct
		// TargetQueryDef in any compiled or hand-authored program in this
		// codebase. Declaring a target_query field here would just offer a
		// second, redundant way to shape the SAME targets a preceding action
		// already produced.
		ActionDealDamage:    false, // deal_damage's Execute (ability_program_registry.go)
		ActionRestoreHealth: false, // restore_health's Execute (ability_program_registry.go)
		ActionApplyStatus:   false, // apply_status's Execute (ability_exec_actions.go)
		ActionRemoveStatus:  false, // remove_status's Execute (ability_exec_actions.go)
		ActionApplyForce:    false, // apply_force's Execute (ability_exec_actions.go)

		// Ignore `targets` entirely — they act on the caster / spawn at a
		// position / re-pick targets at a later tick, never on a query the
		// author could shape here.
		ActionModifyResource:   false, // acts on the caster only
		ActionSummonUnit:       false, // spawns near the caster only
		ActionPlaceTrap:        false, // plants at the caster's position via plantTrapLocked; ignores targets (ability_exec_place_trap.go)
		ActionCreateZone:       false, // spawns at PositionRef, not a unit query
		ActionChargeFireVolley: false, // enqueues a volley; targets re-picked at launch

		// Flow-control / presentation actions: none resolve a unit query of
		// their own.
		ActionStoreTargets:     false, // stores ctx.Selected under a name, doesn't shape it
		ActionFilterTargets:    false, // has its OWN dedicated relations/aliveState/maxCount/ordering fields (not target_query) matching filterTargetsConfig exactly
		ActionWait:             false,
		ActionConditional:      false,
		ActionRepeat:           false,
		ActionSetContext:       false, // writes a scalar into ctx.Named, passes targets through untouched (ability_exec_flow.go)
		ActionLoop:             false, // runs its own body; ignores the incoming target set (ability_exec_loop.go)
		ActionTriggerEvent:     false,
		ActionPlayPresentation: false, // attaches via Input["attach"] or renders at a position
	}

	for typ, desc := range actionRegistry {
		want, ok := wantTargetQuery[typ]
		if !ok {
			t.Errorf("action type %q has a registered descriptor but no verdict in wantTargetQuery — add one (read its Execute first)", typ)
			continue
		}
		has := schemaHasTargetQueryField(desc.Schema)
		if has != want {
			t.Errorf("action %q: declares target_query field = %v, want %v", typ, has, want)
		}
	}

	// The inverse direction: every verdict in the table must name an
	// actually-registered action type, so a removed/renamed action can't
	// leave a stale, never-checked entry behind.
	for typ := range wantTargetQuery {
		if _, ok := actionRegistry[typ]; !ok {
			t.Errorf("wantTargetQuery has verdict for %q, but no descriptor is registered for it", typ)
		}
	}
}

func schemaHasTargetQueryField(s ActionFieldSchema) bool {
	for _, f := range s.Fields {
		if f.Control == "target_query" {
			return true
		}
	}
	return false
}

// TestActionTargetingShape_TargetQueryFieldsDeclared asserts each
// target_query-declaring action declares EXACTLY its own hand-maintained
// verdict shape (wantFields below), not merely "some non-empty shape" — the
// three declarers are deliberately NOT identical: select_targets (the
// free-form, possibly-many-unit scene query) declares the full
// targetQueryFieldsFull set, while launch_projectile/channel_beam (each
// resolves to exactly ONE unit to fly at/channel — see
// targetQueryFieldsSourceOnly's doc comment, ability_program_registry.go)
// declare only targetQueryFieldsSourceOnly. Asserting exact equality (not
// just "non-empty" or "no dead fields") is what makes this a real drift
// guard: a future change that widens launch_projectile back to the full set
// (re-introducing the "advertises a field the action can't meaningfully use"
// bug this correction fixed) fails this test immediately instead of passing
// silently. Also asserts the dead/unenforced TargetQueryDef fields
// (minCount/filters/requireLineOfSight) are never declared by anything.
func TestActionTargetingShape_TargetQueryFieldsDeclared(t *testing.T) {
	dead := map[string]bool{"minCount": true, "filters": true, "requireLineOfSight": true}

	wantFields := map[ActionType][]string{
		ActionSelectTargets:    targetQueryFieldsFull,
		ActionLaunchProjectile: targetQueryFieldsSourceOnly,
		ActionBeam:             targetQueryFieldsSourceOnly,
	}
	for typ, want := range wantFields {
		desc, ok := lookupActionDescriptor(typ)
		if !ok {
			t.Fatalf("action %q has no registered descriptor", typ)
		}
		var field *SchemaField
		for i := range desc.Schema.Fields {
			if desc.Schema.Fields[i].Control == "target_query" {
				field = &desc.Schema.Fields[i]
			}
		}
		if field == nil {
			t.Fatalf("action %q: no target_query field found", typ)
		}
		if !slices.Equal(field.TargetQueryFields, want) {
			t.Errorf("action %q: target_query field declares %v, want exactly %v", typ, field.TargetQueryFields, want)
		}
	}

	// select_targets' full shape and launch_projectile/channel_beam's narrow
	// shape must actually differ — guards against someone "fixing" the test
	// above by pointing targetQueryFieldsSourceOnly back at
	// targetQueryFieldsFull instead of genuinely narrowing the declaration.
	if slices.Equal(targetQueryFieldsFull, targetQueryFieldsSourceOnly) {
		t.Fatal("targetQueryFieldsFull and targetQueryFieldsSourceOnly must NOT be equal — launch_projectile/channel_beam are supposed to declare a narrower shape than select_targets")
	}

	// Every registered action's schema, across the board: no field may name a
	// dead TargetQueryDef sub-field.
	for typ, desc := range actionRegistry {
		for _, f := range desc.Schema.Fields {
			for _, k := range f.TargetQueryFields {
				if dead[k] {
					t.Errorf("action %q field %q declares dead/unenforced TargetQueryFields entry %q", typ, f.Key, k)
				}
			}
		}
	}
}

// TestNoActionDeclaresNestedTriggersControl guards problem #1: create_zone /
// apply_status / launch_projectile's own on_zone_tick/on_status_tick/
// on_projectile_impact triggers are real, recursive, editable FlowTriggerCards
// in the flow view (CONFIG_TRIGGER_ACTION_TYPES, programTree.ts) — a second,
// read-only "edit in flow view" stub field in the inspector was pure
// redundancy. conditional's conditions/then and repeat's actions are NOT the
// same redundancy (nothing else in the flow view renders them — see
// ability_exec_flow.go's comments), but the nested_triggers control was
// removed there too since it never rendered anything actionable regardless.
func TestNoActionDeclaresNestedTriggersControl(t *testing.T) {
	for typ, desc := range actionRegistry {
		for _, f := range desc.Schema.Fields {
			if f.Control == "nested_triggers" {
				t.Errorf("action %q field %q still declares the removed nested_triggers control", typ, f.Key)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Conditional visibility (FieldCondition / ShowWhen)
// ─────────────────────────────────────────────────────────────────────────────

func TestFieldConditionMatches(t *testing.T) {
	num := func(n float64) json.RawMessage {
		b, err := json.Marshal(n)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}
	str := func(s string) json.RawMessage { return json.RawMessage(strconv.Quote(s)) }

	tests := []struct {
		name   string
		cond   FieldCondition
		config map[string]any
		want   bool
	}{
		{"gt true", FieldCondition{Key: "chainCount", Op: "gt", Value: num(0)}, map[string]any{"chainCount": float64(3)}, true},
		{"gt false at zero", FieldCondition{Key: "chainCount", Op: "gt", Value: num(0)}, map[string]any{"chainCount": float64(0)}, false},
		{"gt false when key absent (treated as zero)", FieldCondition{Key: "chainCount", Op: "gt", Value: num(0)}, map[string]any{}, false},
		{"ne true", FieldCondition{Key: "chainCount", Op: "ne", Value: num(0)}, map[string]any{"chainCount": float64(2)}, true},
		{"lte true", FieldCondition{Key: "chainCount", Op: "lte", Value: num(2)}, map[string]any{"chainCount": float64(2)}, true},
		{"lt false", FieldCondition{Key: "chainCount", Op: "lt", Value: num(2)}, map[string]any{"chainCount": float64(2)}, false},
		{"gte true", FieldCondition{Key: "chainCount", Op: "gte", Value: num(2)}, map[string]any{"chainCount": float64(2)}, true},
		{"unrecognized op -> false", FieldCondition{Key: "chainCount", Op: "bogus", Value: num(0)}, map[string]any{"chainCount": float64(5)}, false},
		{"unparseable value -> false", FieldCondition{Key: "chainCount", Op: "gt", Value: json.RawMessage(`"nope"`)}, map[string]any{"chainCount": float64(5)}, false},

		// String eq/ne — the launch_projectile travelMode gate's actual shape.
		{"string eq true", FieldCondition{Key: "travelMode", Op: "eq", Value: str("direction")}, map[string]any{"travelMode": "direction"}, true},
		{"string eq false", FieldCondition{Key: "travelMode", Op: "eq", Value: str("direction")}, map[string]any{"travelMode": "to_target"}, false},
		{"string ne true when key absent (treated as \"\")", FieldCondition{Key: "travelMode", Op: "ne", Value: str("direction")}, map[string]any{}, true},
		{"string ne false when key absent and Value is \"\"", FieldCondition{Key: "travelMode", Op: "ne", Value: str("")}, map[string]any{}, false},
		// lt/lte/gt/gte are numeric-only: a string operand can't be ordered,
		// so these conservatively resolve to false rather than panicking or
		// falling back to some ad hoc string-ordering rule.
		{"string operand rejected by numeric op", FieldCondition{Key: "travelMode", Op: "gt", Value: str("direction")}, map[string]any{"travelMode": "direction"}, false},
		// Mismatched scalar KINDS on eq/ne (a number vs. a string) never
		// panic and never accidentally match.
		{"mismatched kinds -> eq false", FieldCondition{Key: "chainCount", Op: "eq", Value: str("3")}, map[string]any{"chainCount": float64(3)}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FieldConditionMatches(tc.cond, tc.config); got != tc.want {
				t.Errorf("FieldConditionMatches(%+v, %v) = %v, want %v", tc.cond, tc.config, got, tc.want)
			}
		})
	}
}

func TestFieldVisible_NilShowWhenAlwaysVisible(t *testing.T) {
	f := SchemaField{Key: "amount", Control: "number"}
	if !FieldVisible(f, map[string]any{}) {
		t.Fatalf("field with nil ShowWhen must always be visible")
	}
}

// TestLaunchProjectile_TravelModeFieldsShowWhen proves target/distance's
// ShowWhen gating on travelMode. chain_lightning no longer compiles onto
// this action at all (compileChainLightningActions builds a fully authored
// chain of nested launch_beam actions instead — ability_compile.go), and the
// chainCount-gated amount/type/bounceRange/bounceDamageFalloff/chainCount
// schema fields that used to live here (the pre-redesign
// launchProjectileConfig.ChainCount shim) were retired along with it — see
// ability_chain_bounce_attribution_test.go for the attribution guarantee
// that shim used to protect.
func TestLaunchProjectile_TravelModeFieldsShowWhen(t *testing.T) {
	desc, ok := lookupActionDescriptor(ActionLaunchProjectile)
	if !ok {
		t.Fatal("launch_projectile has no registered descriptor")
	}
	byKey := map[string]SchemaField{}
	for _, f := range desc.Schema.Fields {
		byKey[f.Key] = f
	}

	// target: visible in BOTH travel modes (no ShowWhen) — direction mode
	// aims at the resolved target's position at launch, so hiding this field
	// for "direction" would make an author unable to wire a resolved-target
	// aim at all. distance stays travelMode==direction-gated: only
	// launchDirectionalProjectileLocked reads it.
	target, ok := byKey["target"]
	if !ok {
		t.Fatal("launch_projectile schema missing target field")
	}
	distance, ok := byKey["distance"]
	if !ok {
		t.Fatal("launch_projectile schema missing distance field")
	}
	toTarget := map[string]any{"travelMode": "to_target"}
	direction := map[string]any{"travelMode": "direction"}
	unset := map[string]any{} // travelMode omitted entirely -> "" -> same as "to_target"

	if target.ShowWhen != nil {
		t.Errorf("target field must have no ShowWhen (visible in both travel modes), got %+v", target.ShowWhen)
	}
	if !FieldVisible(target, toTarget) {
		t.Errorf("target field should be visible for travelMode=to_target")
	}
	if !FieldVisible(target, unset) {
		t.Errorf("target field should be visible when travelMode is unset (defaults to to_target)")
	}
	if !FieldVisible(target, direction) {
		t.Errorf("target field should ALSO be visible for travelMode=direction (aims at the resolved target)")
	}

	if FieldVisible(distance, toTarget) {
		t.Errorf("distance field should be hidden for travelMode=to_target")
	}
	if !FieldVisible(distance, direction) {
		t.Errorf("distance field should be visible for travelMode=direction")
	}
}

// TestSchemaField_ShowWhenRoundTrips proves the wire contract: a SchemaField
// with a ShowWhen serializes with a "showWhen" key carrying {key,op,value},
// and a field with nil ShowWhen omits the key entirely (so an unaffected
// client never sees a spurious null).
func TestSchemaField_ShowWhenRoundTrips(t *testing.T) {
	gated := SchemaField{
		Key: "amount", Label: "Amount (chain only)", Control: "number",
		ShowWhen: &FieldCondition{Key: "chainCount", Op: "gt", Value: json.RawMessage("0")},
	}
	b, err := json.Marshal(gated)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sw, ok := round["showWhen"].(map[string]any)
	if !ok {
		t.Fatalf("showWhen missing/wrong shape in %s", b)
	}
	if sw["key"] != "chainCount" || sw["op"] != "gt" || sw["value"] != float64(0) {
		t.Errorf("showWhen = %+v, want {key:chainCount op:gt value:0}", sw)
	}

	plain := SchemaField{Key: "projectile", Label: "Projectile", Control: "asset"}
	b2, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round2 map[string]any
	if err := json.Unmarshal(b2, &round2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := round2["showWhen"]; present {
		t.Errorf("showWhen must be omitted (not null) when nil, got %s", b2)
	}
	if _, present := round2["targetQueryFields"]; present {
		t.Errorf("targetQueryFields must be omitted when empty, got %s", b2)
	}
}
