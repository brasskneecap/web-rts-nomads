package game

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestKnownActionTypesCoversAllConsts(t *testing.T) {
	for _, at := range allActionTypes {
		if !isKnownActionType(at) {
			t.Errorf("allActionTypes contains %q but isKnownActionType is false", at)
		}
	}
	if len(allActionTypes) != len(knownActionTypes) {
		t.Errorf("allActionTypes (%d) and knownActionTypes (%d) out of sync", len(allActionTypes), len(knownActionTypes))
	}
}

// TestAllActionTypesMatchesSourceConsts scans ability_program.go for every
// ActionType const declaration and asserts its string values are exactly the
// set carried by allActionTypes. Unlike the derived-map coverage check this
// reads the actual source, so it FAILS if someone adds an ActionType const
// but forgets to list it in allActionTypes (or vice-versa) — the real drift
// this guard exists to prevent. Verified to fail against a deliberately
// omitted dummy const during authoring.
func TestAllActionTypesMatchesSourceConsts(t *testing.T) {
	// CWD during tests is the package dir, so a bare filename resolves.
	src, err := os.ReadFile("ability_program.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}

	// Match e.g. `ActionSelectTargets     ActionType = "select_targets"`.
	re := regexp.MustCompile(`(?m)^\s*(Action\w+)\s+ActionType\s*=\s*"([^"]+)"`)
	sourceVals := map[string]string{} // value -> const name
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		name, val := m[1], m[2]
		if prev, dup := sourceVals[val]; dup {
			t.Errorf("duplicate ActionType value %q on consts %s and %s", val, prev, name)
		}
		sourceVals[val] = name
	}
	if len(sourceVals) == 0 {
		t.Fatal("scanned zero ActionType consts from ability_program.go — regex likely stale")
	}

	sliceVals := map[string]bool{}
	for _, at := range allActionTypes {
		if sliceVals[string(at)] {
			t.Errorf("allActionTypes lists %q more than once", at)
		}
		sliceVals[string(at)] = true
	}

	// Both directions: every source const must be in the slice, and every
	// slice entry must correspond to a real source const.
	for val, name := range sourceVals {
		if !sliceVals[val] {
			t.Errorf("ActionType const %s (%q) is declared in ability_program.go but missing from allActionTypes", name, val)
		}
	}
	for val := range sliceVals {
		if _, ok := sourceVals[val]; !ok {
			t.Errorf("allActionTypes contains %q which has no matching ActionType const in ability_program.go", val)
		}
	}
	// Dedup catch: allActionTypes must have no duplicate entries.
	if len(allActionTypes) != len(sliceVals) {
		t.Errorf("allActionTypes has %d entries but %d unique values (duplicate present)", len(allActionTypes), len(sliceVals))
	}
}

func TestActionEnabledDefaultsTrueWhenAbsent(t *testing.T) {
	var a AbilityActionDef
	if err := json.Unmarshal([]byte(`{"id":"a","type":"deal_damage"}`), &a); err != nil {
		t.Fatal(err)
	}
	if !a.IsEnabled() {
		t.Error("action with absent disabled key should be enabled")
	}
	var b AbilityActionDef
	if err := json.Unmarshal([]byte(`{"id":"b","type":"deal_damage","disabled":true}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.IsEnabled() {
		t.Error(`action with "disabled":true should be disabled`)
	}
}

func hasCode(issues []ValidationIssue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func TestValidateProgramStructural(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelSelf}},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "a1", Type: "no_such_action"},
				{ID: "a1", Type: ActionDealDamage, Config: json.RawMessage(`{"amount":0}`)},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	if !hasCode(issues, "unsupported_action_type") {
		t.Error("want unsupported_action_type for bogus type")
	}
	if !hasCode(issues, "duplicate_id") {
		t.Error("want duplicate_id for repeated a1")
	}
	if !hasCode(issues, "empty_required_property") {
		t.Error("want deal_damage amount issue")
	}
}

func TestValidateKnownActionWithoutDescriptorIsAllowed(t *testing.T) {
	// play_presentation is a KNOWN action type with no descriptor yet — must not
	// be flagged as unsupported and must not error.
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint},
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "a", Type: ActionPlayPresentation, Config: json.RawMessage(`{"asset":"meteor"}`)},
		}}},
	}
	if issues := validateAbilityProgram(prog); len(issues) != 0 {
		t.Fatalf("known-but-descriptorless action should be clean, got: %+v", issues)
	}
}

func TestValidateProgramTickInterval(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint},
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnZoneTick,
			Timing: &TriggerTiming{TickInterval: 0}, Actions: []AbilityActionDef{{ID: "a", Type: ActionSelectTargets}}}},
	}
	if !hasCode(validateAbilityProgram(prog), "invalid_tick_interval") {
		t.Error("want invalid_tick_interval for tickInterval<=0")
	}
}

// issueAt returns the first issue in issues with the given path and code, or
// nil if none matches.
func issueAt(issues []ValidationIssue, path, code string) *ValidationIssue {
	for i, iss := range issues {
		if iss.Path == path && iss.Code == code {
			return &issues[i]
		}
	}
	return nil
}

// TestValidateProgram_NestedZoneTrigger_TickIntervalChecked verifies that a
// trigger nested inside a create_zone action's config.triggers gets the same
// invalid_tick_interval check as a root trigger, at the path grammar the
// client's indexPathFor mirrors: "<action path>.config.triggers[i]".
func TestValidateProgram_NestedZoneTrigger_TickIntervalChecked(t *testing.T) {
	zoneCfg := createZoneConfig{
		Radius:       10,
		Duration:     5,
		TickInterval: 1,
		Triggers: []AbilityTriggerDef{
			{
				ID:     "burn",
				Type:   TriggerOnZoneTick,
				Timing: &TriggerTiming{TickInterval: 0}, // invalid
				Actions: []AbilityActionDef{
					{ID: "bdmg", Type: ActionDealDamage, Config: marshalConfig(dealDamageConfig{Amount: 1})},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "zone", Type: ActionCreateZone, Config: marshalConfig(zoneCfg)},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[0].config.triggers[0]"
	if got := issueAt(issues, wantPath, "invalid_tick_interval"); got == nil {
		t.Fatalf("want invalid_tick_interval at %q, got issues: %+v", wantPath, issues)
	}
}

// TestValidateProgram_NestedZoneAction_DuplicateIDDetected verifies that ids
// inside config.triggers share the single id namespace with root
// triggers/actions, so a nested action id colliding with a root action id is
// flagged.
func TestValidateProgram_NestedZoneAction_DuplicateIDDetected(t *testing.T) {
	zoneCfg := createZoneConfig{
		Radius:       10,
		Duration:     5,
		TickInterval: 1,
		Triggers: []AbilityTriggerDef{
			{
				ID:     "burn",
				Type:   TriggerOnZoneTick,
				Timing: &TriggerTiming{TickInterval: 1},
				Actions: []AbilityActionDef{
					// Collides with the root "dmg" action id below.
					{ID: "dmg", Type: ActionDealDamage, Config: marshalConfig(dealDamageConfig{Amount: 1})},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "dmg", Type: ActionDealDamage, Config: marshalConfig(dealDamageConfig{Amount: 1})},
				{ID: "zone", Type: ActionCreateZone, Config: marshalConfig(zoneCfg)},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[1].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "duplicate_id"); got == nil {
		t.Fatalf("want duplicate_id at %q, got issues: %+v", wantPath, issues)
	}
}

// TestValidateProgram_MalformedCreateZoneConfig_SingleInvalidConfigNoRecursion
// verifies that a create_zone action whose Config fails to decode reports
// invalid_config exactly once and does not attempt to recurse into a garbage
// (zero-value) config's Triggers.
func TestValidateProgram_MalformedCreateZoneConfig_SingleInvalidConfigNoRecursion(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				// radius is a string, not a float64 -> json.Unmarshal error.
				{ID: "zone", Type: ActionCreateZone, Config: json.RawMessage(`{"radius":"oops"}`)},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	var invalidConfigCount int
	for _, iss := range issues {
		if iss.Code == "invalid_config" {
			invalidConfigCount++
		}
	}
	if invalidConfigCount != 1 {
		t.Fatalf("invalid_config count = %d, want exactly 1; issues: %+v", invalidConfigCount, issues)
	}
	// No path should carry the nested config.triggers grammar - confirms
	// recursion did not run against the failed decode.
	for _, iss := range issues {
		if strings.Contains(iss.Path, "config.triggers") {
			t.Errorf("unexpected recursion into failed-decode config, path=%q", iss.Path)
		}
	}
}

// TestValidateProgram_CompiledMeteor_NoNewErrors is the regression guard:
// compileLegacyAbility's meteor output has always relied on the burn zone
// trigger's config.triggers subtree being unwalked. Now that walkAction
// recurses into it, the compiled program (which sets TickInterval
// specifically to satisfy invalid_tick_interval, see
// compileMeteorZoneConfig/ability_compile.go) must still validate clean of
// errors (a "no_behavior" warning cannot appear here since meteor plainly has
// actions, but we assert on errors specifically to be robust to unrelated
// future warnings).
func TestValidateProgram_CompiledMeteor_NoNewErrors(t *testing.T) {
	def := meteorDef(t)
	prog := compileLegacyAbility(def)
	if prog == nil {
		t.Fatal("compileLegacyAbility(meteor) returned nil")
	}
	issues := validateAbilityProgram(prog)
	if hasError(issues) {
		t.Fatalf("compiled meteor program has validation errors after nested-trigger recursion: %+v", issues)
	}
}

func TestValidateProgramNoBehaviorWarning(t *testing.T) {
	prog := &AbilityProgram{Entry: AbilityEntryDef{Type: EntrySelf}, Triggers: []AbilityTriggerDef{}}
	issues := validateAbilityProgram(prog)
	if !hasCode(issues, "no_behavior") {
		t.Error("want no_behavior warning for empty program")
	}
	// no_behavior must be a warning, not an error
	for _, i := range issues {
		if i.Code == "no_behavior" && i.Severity != "warning" {
			t.Errorf("no_behavior severity = %q, want warning", i.Severity)
		}
	}
}
