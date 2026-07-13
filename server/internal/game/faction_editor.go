package game

import (
	"errors"
	"fmt"
)

// EditorFactionSaveRequest is the body of POST /factions.
type EditorFactionSaveRequest struct {
	Faction FactionDef `json:"faction"`
}

// SaveEditorFaction validates then persists a faction record. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorFaction(req EditorFactionSaveRequest) error {
	faction := req.Faction
	if !unitIDPattern.MatchString(faction.ID) {
		return editorValidationError{fmt.Errorf("faction id %q must match %s", faction.ID, unitIDPattern)}
	}
	return SaveFactionDef(&faction)
}

// DeleteEditorFaction removes a faction record. "Still has units" is a
// validation error, not a 500 — it is a state the author can fix, and the
// message names the units so they can fix it. Any other error (bad
// UNIT_CATALOG_DIR, a disk failure) is a genuine infrastructure problem and
// must pass through unwrapped so the handler reports it as a 500, not as if
// the caller had typed something wrong.
func DeleteEditorFaction(id string) (existed bool, err error) {
	existed, err = DeleteFactionOverride(id)
	if err != nil {
		if errors.Is(err, errFactionHasUnits) {
			return false, editorValidationError{err}
		}
		return false, err
	}
	return existed, nil
}
