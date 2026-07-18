package game

import "fmt"

// EditorAbilitySaveRequest is the body of POST /abilities.
type EditorAbilitySaveRequest struct {
	Ability AbilityDef `json:"ability"`
}

// SaveEditorAbility validates then persists an authored ability def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorAbility(req EditorAbilitySaveRequest) error {
	ability := req.Ability
	if !abilityIDPattern.MatchString(ability.ID) {
		return editorValidationError{fmt.Errorf("ability id %q must match %s", ability.ID, abilityIDPattern)}
	}
	if err := validateAbilityDef(&ability); err != nil {
		return editorValidationError{err}
	}
	return SaveAbilityDef(&ability)
}

// DeleteEditorAbility is the editor's destructive action, and what it means
// depends on where the ability came from — mirrors DeleteEditorItem
// (item_editor.go) exactly:
//
//   - A SHIPPED ability is RESET, not deleted. It goes back to the state it
//     was in before the author's last save ("reverted"), or — with no undo
//     step recorded — to the shipped catalog default ("reset"). The def file
//     always survives (see ResetAbilityDef's doc comment for why).
//   - An AUTHOR-CREATED ability is genuinely deleted ("deleted").
//
// The returned status is what the client shows, so it must say which
// happened. existed is false when the id names nothing.
//
// Reference guard: unlike items (itemReferences) there is currently NO
// ability-reference scanner — nothing here checks whether a unit loadout,
// perk, spell-pool, or advancement grant still names this ability id before
// deleting an author-created override. That means an authored-ability
// delete is UNGUARDED: a live dangling reference is possible. This is safe
// in the sense that it can never destroy a SHIPPED ability (that branch
// always goes through ResetAbilityDef, which never removes the file), but a
// dangling reference to a deleted author-created id is a real, currently
// unflagged gap. Flagged here for a follow-up scanner, not built in this
// change (scope: editor/persistence three-way delete only).
func DeleteEditorAbility(id string) (status string, existed bool, err error) {
	if AbilityIsEmbedded(id) {
		mode, ok, rerr := ResetAbilityDef(id)
		if rerr != nil || !ok {
			return "", ok, rerr
		}
		if mode == "undo" {
			return "reverted", true, nil
		}
		return "reset", true, nil
	}

	existed, err = DeleteAbilityOverride(id)
	if err != nil || !existed {
		return "", existed, err
	}
	return "deleted", true, nil
}

// EditorAbilityIssues is a READ-ONLY dry-run inspection of def: it mirrors
// every check validateAbilityDef performs (plus the id check SaveEditorAbility
// performs) but, unlike that single-error gate, collects ALL of them as
// structured ValidationIssues with paths so the editor can annotate every
// offending card at once without saving. It is read-only because it never
// calls validateAbilityDef — which normalizes several fields (TargetCount,
// SummonCount, HealingMultiplier, ManaToChargeRatio) IN PLACE on its pointer
// receiver — and validateAbilityProgram, the only other check it delegates
// to, is a pure read of the program tree with no side effects. Taking def by
// value is a convenience for callers, not the safety mechanism: def.Program
// is still a shared pointer, so this guarantee would NOT hold if a future
// change routed EditorAbilityIssues through validateAbilityDef or any other
// mutating helper. An empty return means def is valid; the slice is always
// non-nil so it serializes as `[]` rather than `null`.
func EditorAbilityIssues(def AbilityDef) []ValidationIssue {
	issues := []ValidationIssue{}

	if !abilityIDPattern.MatchString(def.ID) {
		issues = append(issues, ValidationIssue{
			Path:     "identity.id",
			Code:     "invalid_id",
			Message:  "ability id must match " + abilityIDPattern.String(),
			Severity: "error",
		})
	}
	if def.DamageType != "" && !IsValidDamageType(def.DamageType) {
		issues = append(issues, ValidationIssue{
			Path:     "identity.damageType",
			Code:     "invalid_damage_type",
			Message:  "unknown damage type " + string(def.DamageType),
			Severity: "error",
		})
	}
	if def.Category != "" && !IsValidAbilityCategory(def.Category) {
		issues = append(issues, ValidationIssue{
			Path:     "identity.category",
			Code:     "invalid_category",
			Message:  "unknown ability category " + string(def.Category),
			Severity: "error",
		})
	}
	// Legacy burn guards — see validateAbilityDef for the rationale (a burn is
	// carried by the ground hazard spawned at impact, so it requires a
	// positive impact delay and tick interval to have anywhere to live).
	if def.BurnDurationSeconds > 0 && def.ImpactDelaySeconds <= 0 {
		issues = append(issues, ValidationIssue{
			Path:     "mechanics.burn",
			Code:     "burn_requires_impact_delay",
			Message:  "burnDurationSeconds requires impactDelaySeconds > 0",
			Severity: "error",
		})
	}
	if def.BurnDurationSeconds > 0 && def.BurnTickIntervalSeconds <= 0 {
		issues = append(issues, ValidationIssue{
			Path:     "mechanics.burn",
			Code:     "burn_requires_tick_interval",
			Message:  "burnDurationSeconds requires burnTickIntervalSeconds > 0",
			Severity: "error",
		})
	}

	if def.Program != nil {
		issues = append(issues, validateAbilityProgram(def.Program)...)
	}

	return issues
}
