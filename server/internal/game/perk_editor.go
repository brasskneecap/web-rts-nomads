package game

import "fmt"

// EditorPerkSaveRequest is the body of POST /perks.
type EditorPerkSaveRequest struct {
	Perk PerkDef `json:"perk"`
}

// SaveEditorPerk validates then persists an authored perk def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorPerk(req EditorPerkSaveRequest) error {
	perk := req.Perk
	if !perkIDPattern.MatchString(perk.ID) {
		return editorValidationError{fmt.Errorf("perk id %q must match %s", perk.ID, perkIDPattern)}
	}
	if err := validatePerkDef(&perk); err != nil {
		return editorValidationError{err}
	}
	return SavePerkDef(&perk)
}

// DeleteEditorPerk removes an override; embed-backed ids reset to default.
func DeleteEditorPerk(id string) (existed bool, err error) {
	return DeletePerkOverride(id)
}
