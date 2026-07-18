package game

import "fmt"

// EditorEffectSaveRequest is the body of POST /effects.
type EditorEffectSaveRequest struct {
	Effect EffectDef `json:"effect"`
}

// SaveEditorEffect validates then persists an authored effect def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorEffect(req EditorEffectSaveRequest) error {
	effect := req.Effect
	if !effectIDPattern.MatchString(effect.ID) {
		return editorValidationError{fmt.Errorf("effect id %q must match %s", effect.ID, effectIDPattern)}
	}
	if err := validateEffectDef(&effect); err != nil {
		return editorValidationError{err}
	}
	return SaveEffectDef(&effect)
}

// DeleteEditorEffect removes an override; embed-backed ids reset to default.
func DeleteEditorEffect(id string) (existed bool, err error) {
	return DeleteEffectOverride(id)
}
