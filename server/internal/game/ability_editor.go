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

// DeleteEditorAbility removes an override; embed-backed ids reset to default.
func DeleteEditorAbility(id string) (existed bool, err error) {
	return DeleteAbilityOverride(id)
}
