package game

import (
	"os"
	"regexp"
	"testing"
)

// enumSourceCheck describes one enum whose ProgramEnums() slice must be
// verified against its backing const declarations in ability_program.go.
type enumSourceCheck struct {
	constType string // Go type name, e.g. "TriggerType"
	enumKey   string // ProgramEnums() map key, e.g. "triggerTypes"
}

// TestProgramEnumsMatchSourceConsts scans ability_program.go for every const
// declaration of each hand-listed enum type in ProgramEnums() and asserts the
// set of string values matches exactly (both directions) — the same drift
// guard TestAllActionTypesMatchesSourceConsts provides for actionTypes, but
// for the enums ProgramEnums() lists by hand. It FAILS if someone adds a new
// const to one of these types but forgets to add it to ProgramEnums() (or
// vice-versa). Verified to fail against a deliberately omitted dummy const
// during authoring.
//
// actionTypes is excluded: it already reuses allActionTypes directly, which
// TestAllActionTypesMatchesSourceConsts guards separately. conditionOps is
// excluded: ConditionType is currently a placeholder with no backing consts
// (see ability_program.go) — add a guard here once real ConditionType consts
// land.
func TestProgramEnumsMatchSourceConsts(t *testing.T) {
	// CWD during tests is the package dir, so a bare filename resolves.
	src, err := os.ReadFile("ability_program.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}

	checks := []enumSourceCheck{
		{"AbilityEntryType", "entryTypes"},
		{"TargetRelation", "relations"},
		{"TriggerType", "triggerTypes"},
		{"TargetSource", "targetSources"},
		{"TargetOrigin", "targetOrigins"},
		{"TargetOrdering", "targetOrderings"},
		{"ZoneAnchor", "zoneAnchors"},
	}

	enums := ProgramEnums()

	for _, c := range checks {
		c := c
		t.Run(c.enumKey, func(t *testing.T) {
			// Match e.g. `SrcCaster            TargetSource = "caster"`.
			re := regexp.MustCompile(`(?m)^\s*(\w+)\s+` + c.constType + `\s*=\s*"([^"]+)"`)
			sourceVals := map[string]string{} // value -> const name
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				name, val := m[1], m[2]
				if prev, dup := sourceVals[val]; dup {
					t.Errorf("duplicate %s value %q on consts %s and %s", c.constType, val, prev, name)
				}
				sourceVals[val] = name
			}
			if len(sourceVals) == 0 {
				t.Fatalf("scanned zero %s consts from ability_program.go — regex likely stale", c.constType)
			}

			sliceVals := map[string]bool{}
			for _, v := range enums[c.enumKey] {
				if sliceVals[v] {
					t.Errorf("ProgramEnums()[%q] lists %q more than once", c.enumKey, v)
				}
				sliceVals[v] = true
			}

			// Both directions: every source const must be in the slice, and
			// every slice entry must correspond to a real source const.
			for val, name := range sourceVals {
				if !sliceVals[val] {
					t.Errorf("%s const %s (%q) is declared in ability_program.go but missing from ProgramEnums()[%q]", c.constType, name, val, c.enumKey)
				}
			}
			for val := range sliceVals {
				if _, ok := sourceVals[val]; !ok {
					t.Errorf("ProgramEnums()[%q] contains %q which has no matching %s const in ability_program.go", c.enumKey, val, c.constType)
				}
			}
			// Dedup catch: ProgramEnums()[key] must have no duplicate entries.
			if len(enums[c.enumKey]) != len(sliceVals) {
				t.Errorf("ProgramEnums()[%q] has %d entries but %d unique values (duplicate present)", c.enumKey, len(enums[c.enumKey]), len(sliceVals))
			}
		})
	}
}
