package game

import "fmt"

// EditorUnitSaveRequest is the body of POST /units.
type EditorUnitSaveRequest struct {
	Unit UnitDef `json:"unit"`
}

// SaveEditorUnit validates then persists an authored unit def. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorUnit(req EditorUnitSaveRequest) error {
	unit := req.Unit
	if !unitIDPattern.MatchString(unit.Type) {
		return editorValidationError{fmt.Errorf("unit type %q must match %s", unit.Type, unitIDPattern)}
	}
	if !unitIDPattern.MatchString(unit.Faction) {
		return editorValidationError{fmt.Errorf("unit faction %q must match %s", unit.Faction, unitIDPattern)}
	}
	if err := validateUnitDef(&unit); err != nil {
		return editorValidationError{err}
	}
	// Cross-catalog check: does every pathChances entry reference a real,
	// usable promotion path? validateUnitDef above only checks the
	// pathChances map's internal shape (weights >= 0, sum > 0) — it can't
	// see the path catalog. validateUnitPathChances already returns
	// editorValidationError, so no double-wrap here.
	if err := validateUnitPathChances(&unit); err != nil {
		return err
	}
	return SaveUnitDef(&unit)
}

// DeleteEditorUnit removes an override; embed-backed types reset to default.
func DeleteEditorUnit(unitType string) (existed bool, err error) {
	return DeleteUnitOverride(unitType)
}
