package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Paths live UNDER the units tree (catalog/units/<faction>/<unit>/paths/
// <path>/<path>.json — see path_defs.go), so the writable path catalog
// shares its root directory with the unit catalog. resolvePathsDir is a
// thin alias of resolveUnitsDir (unit_persistence.go) rather than a
// separate env var, so a single UNIT_CATALOG_DIR configures both.
func resolvePathsDir() (string, error) {
	return resolveUnitsDir()
}

var (
	// runtimePathsMu guards runtimePaths / runtimePathUnit below — the
	// writable path overlay. A DIFFERENT lock from pathCatalogMu (which
	// guards the 10 DERIVED maps rebuilt from embedded+overlay); overlay
	// writes and derived-map rebuilds are separate critical sections by
	// design, matching Task 1/2's "pathCatalogMu guards the catalog, not
	// game state" precedent.
	runtimePathsMu sync.RWMutex
	// runtimePaths holds one authored pathCatalogFile per overlay path id.
	// An overlay entry for an id that also exists in the embedded catalog
	// WINS over the embedded file entirely (whole-file replace, not a
	// field-by-field merge) — see rebuildDerivedPathMaps.
	runtimePaths = map[string]*pathCatalogFile{}
	// runtimePathUnit records which unit type owns each overlay path id
	// (needed to reconstruct the on-disk directory and to answer "which
	// unit's paths/ list should include this id" during rebuild).
	runtimePathUnit = map[string]string{}
)

// rebuildDerivedPathMaps rebuilds all 13 derived path-catalog maps
// (pathModifiersByKey, pathBoundsByPath, ... pathsByUnitType — see
// path_defs.go) from embeddedPathFiles/embeddedPathUnit merged with the
// current runtimePaths overlay. An overlay id WINS over an embedded id of
// the same path — entirely, not a field merge, since a path JSON is
// authored as a whole file.
//
// The whole merge + registration pass is built into FRESH local maps
// (newPathDerivedMaps) with zero effect on what readers see; only the final
// assignment of all 12 fresh maps into the package globals happens under
// pathCatalogMu.Lock(), so a reader taking pathCatalogMu.RLock() (any of the
// Task-1 accessors) never observes a partially rebuilt catalog — it sees
// either the complete old set or the complete new set, never a mix.
//
// NEVER panics: this runs at runtime (Save/Delete/Load, all reachable after
// the server has started), so a bad overlay entry (e.g. a hand-edited file
// found by LoadPersistedPathsIntoOverlay) is skipped with a slog.Warn rather
// than crashing the process. Embedded entries were already validated at
// init and are not re-validated here.
func rebuildDerivedPathMaps() {
	runtimePathsMu.RLock()
	overlayFiles := make(map[string]*pathCatalogFile, len(runtimePaths))
	overlayUnit := make(map[string]string, len(runtimePathUnit))
	for id, f := range runtimePaths {
		overlayFiles[id] = f
		overlayUnit[id] = runtimePathUnit[id]
	}
	runtimePathsMu.RUnlock()

	type mergedEntry struct {
		unitKey string
		file    *pathCatalogFile
	}
	merged := make(map[string]mergedEntry, len(embeddedPathFiles)+len(overlayFiles))
	for id, f := range embeddedPathFiles {
		merged[id] = mergedEntry{unitKey: embeddedPathUnit[id], file: f}
	}
	for id, f := range overlayFiles {
		// Overlay entries are runtime, potentially user-authored (e.g. a
		// hand-edited file discovered by LoadPersistedPathsIntoOverlay) —
		// validate before it ever reaches the fresh maps. A bad entry is
		// skipped, not fatal; the server keeps running on whatever catalog
		// state was valid before this rebuild attempt.
		if err := validatePathFile(f, f.Path); err != nil {
			slog.Warn("path catalog: skipped invalid overlay path on rebuild", "path", id, "err", err)
			continue
		}
		merged[id] = mergedEntry{unitKey: overlayUnit[id], file: f}
	}

	fresh := newPathDerivedMaps()
	// Deterministic registration order (sorted path ids) — doesn't change
	// the final map contents (each id registers exactly once, keyed by
	// itself), but keeps behavior reproducible rather than depending on Go's
	// randomized map iteration order, per the project's determinism rule.
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := merged[id]
		if err := registerPathFileInto(fresh, entry.unitKey, entry.file); err != nil {
			// Cannot happen in practice: merged is keyed by path id, so each
			// id is registered into `fresh` exactly once — there is no
			// cross-file collision left to detect at this point. Logged
			// defensively rather than ignored, in case that invariant is
			// ever violated by future code; the entry is simply dropped
			// from this rebuild rather than crashing the process.
			slog.Warn("path catalog: rebuild could not register entry", "path", id, "err", err)
			continue
		}
	}

	pathCatalogMu.Lock()
	pathModifiersByKey = fresh.modifiersByKey
	pathBoundsByPath = fresh.boundsByPath
	pathAttackOriginByPath = fresh.attackOriginByPath
	pathShadowByPath = fresh.shadowByPath
	pathVisionRangeByPath = fresh.visionRangeByPath
	pathProjectileByPath = fresh.projectileByPath
	pathDamageTypeByPath = fresh.damageTypeByPath
	pathAttackTypeByPath = fresh.attackTypeByPath
	pathProjectileScaleByPath = fresh.projectileScaleByPath
	pathAbilitiesByPath = fresh.abilitiesByPath
	pathPerkRefsByPath = fresh.perkRefsByPath
	pathAbilityPoolsByPath = fresh.abilityPoolsByPath
	pathChannelLoopByPath = fresh.channelLoopByPath
	pathsByUnitType = fresh.pathsByUnitType
	pathCatalogMu.Unlock()
}

// SavePathDef validates and writes an authored promotion-path file to
// <dir>/<faction>/<unitType>/paths/<file.Path>/<file.Path>.json, registers
// it in the writable overlay, and rebuilds the derived maps so the change
// is visible immediately. faction comes from the owning unit's UnitDef —
// callers pass the (already-existing) unit type the path belongs under, not
// a faction directly.
func SavePathDef(unitType string, file *pathCatalogFile) error {
	if !unitIDPattern.MatchString(unitType) {
		return fmt.Errorf("unit type %q must match %s", unitType, unitIDPattern)
	}
	if !unitIDPattern.MatchString(file.Path) {
		return fmt.Errorf("path id %q must match %s", file.Path, unitIDPattern)
	}
	if err := validatePathFile(file, file.Path); err != nil {
		return err
	}
	unitDef, ok := getUnitDef(unitType)
	if !ok {
		return fmt.Errorf("unit type %q not found", unitType)
	}
	dir, err := resolvePathsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, unitDef.Faction, unitType, unitPathsSubdirName, file.Path)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, file.Path+".json"), raw, 0o644); err != nil {
		return err
	}
	runtimePathsMu.Lock()
	// Store a clone, not the caller's own pointer — mirrors SavePerkPool's
	// clonePerkEntries. Without this, a caller that mutates *file after
	// SavePathDef returns would silently corrupt the overlay entry (and
	// anything rebuilt from it) out from under whoever else might be
	// reading it.
	runtimePaths[file.Path] = clonePathCatalogFile(file)
	runtimePathUnit[file.Path] = unitType
	runtimePathsMu.Unlock()
	rebuildDerivedPathMaps()
	return nil
}

// PathIsEmbedded reports whether a path id exists in the embedded catalog
// (as opposed to being purely overlay-authored). Used by the future editor
// HTTP handler to decide whether a "delete" on a given id should be
// reported as "reset to shipped default" (embedded id) vs. "removed"
// (overlay-only id).
func PathIsEmbedded(pathID string) bool {
	_, ok := embeddedPathFiles[pathID]
	return ok
}

// DeletePathOverride removes the on-disk override directory (if any) and
// the overlay entry for pathID, then rebuilds the derived maps. If pathID
// also exists in the embedded catalog, this reverts it to the shipped
// definition rather than making it disappear — reported via PathIsEmbedded
// by the caller, not by this function's return value.
func DeletePathOverride(pathID string) (existed bool, err error) {
	if !unitIDPattern.MatchString(pathID) {
		return false, nil // never a valid override id; also blocks path traversal
	}

	var removedFromDisk bool
	if unitType, ok := resolvePathOwningUnit(pathID); ok {
		dir, derr := resolvePathsDir()
		if derr != nil {
			return false, derr
		}
		removedFromDisk, err = removePathOverrideDir(dir, unitType, pathID)
		if err != nil {
			return false, err
		}
	}

	runtimePathsMu.Lock()
	_, inOverlay := runtimePaths[pathID]
	delete(runtimePaths, pathID)
	delete(runtimePathUnit, pathID)
	runtimePathsMu.Unlock()

	rebuildDerivedPathMaps()
	return removedFromDisk || inOverlay, nil
}

// resolvePathOwningUnit finds which unit type a path id belongs to,
// checking the overlay first (a saved override always knows its own owning
// unit) and falling back to the embedded catalog topology. embeddedPathUnit
// is read without a lock — it is populated once at process init and never
// mutated afterward (same convention as reading unitDefsByType directly).
func resolvePathOwningUnit(pathID string) (string, bool) {
	runtimePathsMu.RLock()
	unitType, ok := runtimePathUnit[pathID]
	runtimePathsMu.RUnlock()
	if ok {
		return unitType, true
	}
	unitType, ok = embeddedPathUnit[pathID]
	return unitType, ok
}

// removePathOverrideDir deletes the <faction>/<unitType>/paths/<pathID>/
// directory (and everything under it, including its perks/ subdir) from the
// writable catalog tree rooted at dir. Traversal-safe: it walks dir and only
// removes a directory whose own name is exactly pathID, sitting directly
// under a directory named "paths" (unitPathsSubdirName), which itself sits
// directly under a directory named unitType — matched by basename equality
// only, never by constructing a path from caller-controlled strings. Both
// pathID and unitType are already constrained to unitIDPattern by callers.
func removePathOverrideDir(dir, unitType, pathID string) (removed bool, err error) {
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() || d.Name() != pathID {
			return nil
		}
		parent := filepath.Dir(path)
		if filepath.Base(parent) != unitPathsSubdirName {
			return nil
		}
		grandparent := filepath.Dir(parent)
		if filepath.Base(grandparent) != unitType {
			return nil
		}
		if rerr := os.RemoveAll(path); rerr != nil {
			return rerr
		}
		removed = true
		return filepath.SkipDir
	})
	return removed, walkErr
}

// LoadPersistedPathsIntoOverlay overlays writable path files onto the
// embedded catalog at startup, mirroring LoadPersistedUnitsIntoOverlay.
// Tolerates a missing writable dir (embedded-only deployment).
func LoadPersistedPathsIntoOverlay() {
	dir, err := resolvePathsDir()
	if err != nil {
		slog.Info("persisted paths: no writable units dir; using embedded catalog only", "err", err)
		return
	}
	n := loadPersistedPathsFromDir(dir)
	rebuildDerivedPathMaps()
	if n > 0 {
		slog.Info("persisted paths: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

// loadPersistedPathsFromDir walks dir looking for files at exactly
// <faction>/<unit>/paths/<path>/<path>.json — i.e. the file's basename
// (minus extension) must equal its parent directory's name, and the parent
// directory's parent must be named "paths". This naturally excludes unit
// def files, faction.json, and perk files (which live one level deeper,
// under .../paths/<path>/perks/*.json) without needing to special-case any
// of them.
func loadPersistedPathsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			return nil
		}
		pathID := strings.TrimSuffix(name, filepath.Ext(name))
		parent := filepath.Dir(path)
		if filepath.Base(parent) != pathID {
			return nil
		}
		pathsDir := filepath.Dir(parent)
		if filepath.Base(pathsDir) != unitPathsSubdirName {
			return nil
		}
		unitType := filepath.Base(filepath.Dir(pathsDir))

		file, perr := parsePersistedPathFile(path)
		if perr != nil {
			slog.Warn("persisted paths: skipped file", "file", path, "err", perr)
			return nil
		}
		if verr := validatePathFile(file, pathID); verr != nil {
			slog.Warn("persisted paths: skipped invalid file", "file", path, "err", verr)
			return nil
		}
		runtimePathsMu.Lock()
		runtimePaths[file.Path] = file
		runtimePathUnit[file.Path] = unitType
		runtimePathsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedPathFile(path string) (*pathCatalogFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file pathCatalogFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Path == "" {
		return nil, fmt.Errorf("path file has empty %q field", "path")
	}
	return &file, nil
}
