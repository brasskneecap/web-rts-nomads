package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ─── Editor wrappers: promotion-path saves/deletes + pathChances integrity ──
//
// path_persistence.go's SavePathDef/DeletePathOverride do the mechanical
// work (validate a single file, write it, rebuild the derived maps) but
// don't know about UnitDef.PathChances — a SEPARATE catalog surface that
// references paths by id. A dangling pathChances reference (a unit pointing
// at a path directory that doesn't exist) is exactly what makes init()'s
// boot-time cross-validation panic (see path_defs.go's init(), the
// "Cross-validate UnitDef.PathChances" block). This file is the editor-time
// gate that makes that state impossible to ever write:
//
//   - SaveEditorPath / DeleteEditorPath wrap the persistence layer with the
//     "is this path still referenced?" check (delete side).
//   - validateUnitPathChances, wired into SaveEditorUnit (unit_editor.go),
//     is the "does every reference resolve to something real and usable?"
//     check (save side).
//
// Ordering invariant (do not break): the "Add Path" UX writes the path file
// FIRST, then the pathChances row referencing it — the intermediate state
// (path exists, unreferenced) is always valid. Nothing in this file ever
// writes a pathChances row before its path exists.

// SaveEditorPath validates then persists an authored promotion-path file.
// Validation failures are wrapped as editorValidationError so the HTTP
// handler returns 400. Mirrors SaveEditorUnit / SaveEditorItem's
// validate-then-apply shape.
func SaveEditorPath(unitType string, file *pathCatalogFile) error {
	if !unitIDPattern.MatchString(unitType) {
		return editorValidationError{fmt.Errorf("unit type %q must match %s", unitType, unitIDPattern)}
	}
	if !unitIDPattern.MatchString(file.Path) {
		return editorValidationError{fmt.Errorf("path id %q must match %s", file.Path, unitIDPattern)}
	}
	if err := validatePathFile(file, file.Path); err != nil {
		return editorValidationError{err}
	}
	// A path id belongs to exactly one unit. Without this guard, saving the
	// same path id under a different unit than its current owner would
	// silently re-parent it — the overlay-wins rebuild moves the path to
	// the new unit's pathsByUnitType and drops it from the original owner,
	// orphaning any existing pathChances reference on that original unit at
	// runtime (validateUnitPathChances only re-checks a unit when THAT unit
	// is re-saved, so it would not catch this). A brand-new path id (no
	// existing owner) or a re-save under its own current owner are both
	// legitimate and pass through.
	if existingOwner, ok := resolvePathOwningUnit(file.Path); ok && existingOwner != unitType {
		return editorValidationError{fmt.Errorf(
			"path %q already belongs to unit %q; a path id is owned by exactly one unit and cannot be re-parented to %q",
			file.Path, existingOwner, unitType)}
	}
	return SavePathDef(unitType, file)
}

// SaveEditorPathFromJSON is SaveEditorPath's exported entrypoint for
// callers outside the game package (the http editor routes): pathCatalogFile
// is unexported, so http can't decode a request body into one directly.
// Unmarshal failures are wrapped as editorValidationError (bad author input,
// not an infrastructure problem) so the HTTP layer maps them to 400 like
// every other validation failure.
func SaveEditorPathFromJSON(unitType string, pathJSON json.RawMessage) error {
	var file pathCatalogFile
	if err := json.Unmarshal(pathJSON, &file); err != nil {
		return editorValidationError{fmt.Errorf("invalid path JSON: %w", err)}
	}
	return SaveEditorPath(unitType, &file)
}

// DeleteEditorPath rejects deleting a path that is still referenced by any
// unit's pathChances — deleting it out from under a live reference would
// reintroduce the exact dangling-reference state that panics init() at the
// next boot. The rejection names every referencing unit so the author knows
// what to fix first.
func DeleteEditorPath(pathID string) (existed bool, err error) {
	if refs := unitsReferencingPath(pathID); len(refs) > 0 {
		sort.Strings(refs)
		return false, editorValidationError{fmt.Errorf(
			"path %q is still referenced by pathChances on: %s. Remove those rows first.", pathID, strings.Join(refs, ", "))}
	}
	return DeletePathOverride(pathID)
}

// EditorPathEntry is one merged (embedded + overlay, overlay wins) promotion
// path definition, for the path editor's full-detail view. Def carries the
// COMPLETE pathCatalogFile JSON (ranks, bounds, projectile/damageType/
// attackType/projectileScale overrides, abilities, channelLoop) — richer
// than ListPathBounds' bounds-only shape (which exists for the in-game
// client's selection-ring rendering, not for editing).
type EditorPathEntry struct {
	Unit string          `json:"unit"`
	Path string          `json:"path"`
	Def  json.RawMessage `json:"def"`
}

// ListPathDefsFull returns the full merged catalog of promotion-path
// definitions for the path editor's GET, sorted by (unit, path) for a
// deterministic response. An overlay entry for a path id replaces its
// embedded counterpart entirely (same whole-file-replace semantics as
// rebuildDerivedPathMaps), and its owning unit comes from the overlay's own
// runtimePathUnit rather than the embedded topology.
func ListPathDefsFull() []EditorPathEntry {
	type owned struct {
		unit string
		file *pathCatalogFile
	}
	byPath := make(map[string]owned, len(embeddedPathFiles))
	for id, file := range embeddedPathFiles {
		byPath[id] = owned{unit: embeddedPathUnit[id], file: file}
	}
	runtimePathsMu.RLock()
	for id, file := range runtimePaths {
		byPath[id] = owned{unit: runtimePathUnit[id], file: file}
	}
	runtimePathsMu.RUnlock()

	out := make([]EditorPathEntry, 0, len(byPath))
	for id, o := range byPath {
		raw, err := json.Marshal(o.file)
		if err != nil {
			// Cannot happen in practice — every pathCatalogFile reachable
			// here was itself produced by unmarshaling JSON (init's embed
			// load or SaveEditorPathFromJSON), so it's always
			// re-marshalable. Skip defensively rather than panic in an
			// HTTP-reachable path.
			continue
		}
		out = append(out, EditorPathEntry{Unit: o.unit, Path: id, Def: json.RawMessage(raw)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Unit != out[j].Unit {
			return out[i].Unit < out[j].Unit
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// unitsReferencingPath scans the merged (embedded + overlay) unit catalog
// for every unit whose PathChances references pathID. Deterministic
// (sorted) so a rejection message is reproducible.
func unitsReferencingPath(pathID string) []string {
	var refs []string
	for _, def := range ListUnitDefs() {
		if _, ok := def.PathChances[pathID]; ok {
			refs = append(refs, def.Type)
		}
	}
	sort.Strings(refs)
	return refs
}

// pathHasRankCurve reports whether pathID resolves to at least one rank row
// (bronze/silver/gold) in the current path-modifier catalog. A path with no
// rank curve at all is legal to author (validatePathFile allows an empty
// "ranks" table — e.g. a path mid-authoring) but is not yet safe to
// reference from a unit's pathChances: a unit promoted into it would gain
// nothing, silently.
func pathHasRankCurve(pathID string) bool {
	for _, rank := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		if _, ok := pathModifierLookup(pathModifierKey(pathID, rank)); ok {
			return true
		}
	}
	return false
}

// validateUnitPathChances enforces spec §9.1 on EVERY unit save: every
// entry in def.PathChances must reference a path that (1) actually exists
// under this unit, (2) has at least one authored rank row, and (3) carries
// a non-negative weight; and (4) the weights as a whole must sum to more
// than zero so the promotion roll has something to draw from. This is what
// makes a dangling or unusable pathChances reference impossible to save —
// the exact class of state that panics init()'s boot-time cross-validation
// (path_defs.go init(), "Cross-validate UnitDef.PathChances").
//
// NOT an s.mu-Locked function despite reading catalog state — it takes no
// game-state lock at all. It reads the path registry exclusively through
// the Task-1 pathCatalogMu-guarded accessors (pathsForUnitType,
// pathModifierLookup), so it is safe to call from any goroutine at any
// time. Named without a "Locked" suffix to respect the project convention
// that "Locked" means "caller holds s.mu" — this function has nothing to do
// with s.mu.
//
// The "path field == directory name" rule (a path's own Path field must
// match the directory it lives in) is already enforced by validatePathFile
// at path-save time, so every entry the merged registry can possibly return
// here is already correctly named — this function doesn't need to (and
// doesn't) re-check that.
func validateUnitPathChances(def *UnitDef) error {
	if len(def.PathChances) == 0 {
		return nil
	}

	// Deterministic key order: the first offending entry (in sorted order)
	// is reported, rather than whichever Go's randomized map iteration
	// happens to visit first.
	pathIDs := make([]string, 0, len(def.PathChances))
	for pathID := range def.PathChances {
		pathIDs = append(pathIDs, pathID)
	}
	sort.Strings(pathIDs)

	known := pathsForUnitType(def.Type)
	var sum float64
	for _, pathID := range pathIDs {
		weight := def.PathChances[pathID]
		if !containsString(known, pathID) {
			return editorValidationError{fmt.Errorf(
				"no path %q exists on %q. Create the path first, or remove this row.", pathID, def.Type)}
		}
		if !pathHasRankCurve(pathID) {
			return editorValidationError{fmt.Errorf(
				"path %q has no rank curve. A unit promoted into it would gain nothing.", pathID)}
		}
		if weight < 0 {
			return editorValidationError{fmt.Errorf(
				"path %q has a negative weight; weights must be >= 0.", pathID)}
		}
		sum += weight
	}
	if sum <= 0 {
		return editorValidationError{errors.New(
			"path weights must sum to more than 0, or the promotion roll has nothing to draw from.")}
	}
	return nil
}

// validateAllUnitPathChances mirrors init()'s pathChances cross-validation
// block in path_defs.go — the same dangling-reference check that would
// otherwise PANIC at boot — but returns an error instead of crashing the
// process. It exists to prove (see path_promotion_integrity_test.go's
// boot-panic proof) that once every unit save has passed through
// validateUnitPathChances, running this exact boot-time check against the
// resulting merged catalog never finds anything to reject. Reads through
// the Task-1 pathsForUnitType accessor (current embedded+overlay merged
// state), not the raw init-time map.
// ValidateAllUnitPathChances is the exported entrypoint for
// validateAllUnitPathChances, meant to be called once at server boot —
// after BOTH game.LoadPersistedUnitsIntoOverlay() and
// game.LoadPersistedPathsIntoOverlay() have run — so the merged catalog's
// pathChances integrity is observable without ever panicking. A reviewer
// flagged that init()'s own pathChances cross-validation panic only ever
// sees the EMBEDDED catalog; an overlay unit referencing an overlay path
// (or any other overlay-introduced dangling reference) is invisible to
// that boot-time check. The caller (cmd/api/main.go) is expected to
// slog.Warn a non-nil return, never panic on it — see
// validateAllUnitPathChances's own doc comment for why a dangling
// reference is safe to leave unresolved at runtime (it falls back to
// unitPathNone via pathModifierFor's identity fallback).
func ValidateAllUnitPathChances() error {
	return validateAllUnitPathChances()
}

func validateAllUnitPathChances() error {
	for _, def := range ListUnitDefs() {
		if len(def.PathChances) == 0 {
			continue
		}
		known := make(map[string]struct{}, 8)
		for _, p := range pathsForUnitType(def.Type) {
			known[p] = struct{}{}
		}
		pathIDs := make([]string, 0, len(def.PathChances))
		for p := range def.PathChances {
			pathIDs = append(pathIDs, p)
		}
		sort.Strings(pathIDs)
		for _, path := range pathIDs {
			if _, ok := known[path]; !ok {
				return fmt.Errorf("unit %q: pathChances references %q, which is not a path directory under catalog/units/<faction>/%s/paths/",
					def.Type, path, def.Type)
			}
		}
	}
	return nil
}
