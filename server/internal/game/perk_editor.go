package game

import (
	"encoding/json"
	"fmt"
)

// ─── Editor wrappers: perk pool saves/deletes ───────────────────────────────
//
// Mirrors path_editor.go's shape. perk_persistence.go's SavePerkPool /
// DeletePerkPool do the mechanical work (validate, write, rebuild the
// registry); this file wraps content-validation failures as
// editorValidationError so the HTTP handler returns 400, matching every
// other *_editor.go file.

// SaveEditorPerkPool validates then persists an authored rank's perk pool.
// Validation failures (bad id pattern, bad rank, duplicate perk id — either
// within the incoming array or against a different (unit,path,rank)
// location elsewhere in the catalog) are wrapped as editorValidationError.
func SaveEditorPerkPool(unitType, pathName, rank string, entries []perkEntryJSON) error {
	if !unitIDPattern.MatchString(unitType) {
		return editorValidationError{fmt.Errorf("unit type %q must match %s", unitType, unitIDPattern)}
	}
	if !unitIDPattern.MatchString(pathName) {
		return editorValidationError{fmt.Errorf("path id %q must match %s", pathName, unitIDPattern)}
	}
	if !unitIDPattern.MatchString(rank) {
		return editorValidationError{fmt.Errorf("rank %q must match %s", rank, unitIDPattern)}
	}
	if _, ok := validRankName[rank]; !ok {
		return editorValidationError{fmt.Errorf("rank %q must be one of bronze/silver/gold", rank)}
	}
	// Editor-only ordering guard (deliberately NOT in validatePerkPoolEntries,
	// which LoadPersistedPerksIntoOverlay also calls and which must stay
	// tolerant of a perks/ dir surviving without its sibling path file): a
	// perk pool must be authored against a path that already exists on this
	// unit. Without this, the editor could silently write an orphaned
	// .../paths/<pathName>/perks/<rank>.json that no path ever references —
	// the same "path first, then whatever references it" ordering invariant
	// path_editor.go documents for pathChances.
	if !containsString(pathsForUnitType(unitType), pathName) {
		return editorValidationError{fmt.Errorf(
			"no path %q exists on unit %q; create the path before authoring its perks", pathName, unitType)}
	}
	if err := validatePerkPoolEntries(unitType, pathName, rank, entries); err != nil {
		return editorValidationError{err}
	}
	return SavePerkPool(unitType, pathName, rank, entries)
}

// SaveEditorPerkPoolFromJSON is SaveEditorPerkPool's exported entrypoint for
// callers outside the game package (the http editor routes): perkEntryJSON
// is unexported, so http can't decode a request body into a []perkEntryJSON
// directly. Unmarshal failures are wrapped as editorValidationError (bad
// author input, not an infrastructure problem) so the HTTP layer maps them
// to 400 like every other validation failure.
func SaveEditorPerkPoolFromJSON(unitType, pathName, rank string, entriesJSON json.RawMessage) error {
	var entries []perkEntryJSON
	if err := json.Unmarshal(entriesJSON, &entries); err != nil {
		return editorValidationError{fmt.Errorf("invalid perk pool JSON: %w", err)}
	}
	return SaveEditorPerkPool(unitType, pathName, rank, entries)
}

// DeleteEditorPerkPool removes a perk pool override, reverting to the
// embedded default when one exists (see PerkPoolIsEmbedded).
func DeleteEditorPerkPool(unitType, pathName, rank string) (existed bool, err error) {
	return DeletePerkPool(unitType, pathName, rank)
}
