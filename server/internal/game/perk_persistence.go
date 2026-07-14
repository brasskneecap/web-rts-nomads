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

// Perk pools live UNDER the units tree, one level deeper than paths
// (catalog/units/<faction>/<unit>/paths/<path>/perks/<rank>.json — see
// perk_defs.go), so the writable perk catalog shares its root with the unit
// and path catalogs. resolvePerksDir is a thin alias of resolveUnitsDir, same
// as path_persistence.go's resolvePathsDir.
func resolvePerksDir() (string, error) {
	return resolveUnitsDir()
}

var (
	// runtimePerkPoolsMu guards runtimePerkPools below — the writable perk
	// overlay. A DIFFERENT lock from perkDefsMu (which guards the derived
	// perkDefsByID registry rebuilt from embedded+overlay); overlay writes
	// and registry rebuilds are separate critical sections, matching the
	// path subsystem's pathCatalogMu / runtimePathsMu split.
	runtimePerkPoolsMu sync.RWMutex
	// runtimePerkPools holds one authored perk-entry array per overlay pool
	// key (perkPoolKey(unitType, pathName, rank)). An overlay entry for a
	// key that also exists in the embedded catalog WINS over the embedded
	// pool entirely (whole-file replace, not a field merge) — mirrors
	// path_persistence.go's runtimePaths.
	runtimePerkPools = map[string][]perkEntryJSON{}
)

// rebuildPerkRegistry rebuilds perkDefsByID from embeddedPerkPools merged
// with the current runtimePerkPools overlay. An overlay pool WINS over an
// embedded pool at the same (unit,path,rank) key — entirely, not a field
// merge, since a perks/<rank>.json file is authored as a whole array.
//
// The merge + registration pass builds into a FRESH local map
// (perkDefsByID's replacement) with zero effect on what readers see; only
// the final assignment happens under perkDefsMu.Lock(), so a reader taking
// perkDefsMu.RLock() (perkDefLookup / snapshotPerkDefs) never observes a
// partially rebuilt registry.
//
// NEVER panics: this runs at runtime (Save/Delete/Load, all reachable after
// the server has started). A pool that fails to convert (buildPerkDefsFromPool
// error — malformed config) is skipped with a slog.Warn. A perk id that
// collides with one already placed into the fresh map by an earlier
// (sorted) pool key is also skipped with a slog.Warn — SavePerkPool/
// SaveEditorPerkPool already reject that state before it can be written
// through the editor, so this is a defensive backstop (e.g. a hand-edited
// overlay file found by LoadPersistedPerksIntoOverlay), not the primary
// guard.
func rebuildPerkRegistry() {
	runtimePerkPoolsMu.RLock()
	overlayPools := make(map[string][]perkEntryJSON, len(runtimePerkPools))
	for k, v := range runtimePerkPools {
		overlayPools[k] = v
	}
	runtimePerkPoolsMu.RUnlock()

	merged := make(map[string][]perkEntryJSON, len(embeddedPerkPools)+len(overlayPools))
	for k, v := range embeddedPerkPools {
		merged[k] = v
	}
	for k, v := range overlayPools {
		merged[k] = v // whole-pool replace, same key = same (unit,path,rank)
	}

	// Deterministic registration order (sorted pool keys) — doesn't change
	// final map contents when every pool is well-formed and collision-free,
	// but keeps behavior reproducible rather than depending on Go's
	// randomized map iteration order, per the project's determinism rule.
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fresh := make(map[string]*PerkDef, len(merged)*4)
	for _, key := range keys {
		unitType, pathName, rank, ok := splitPerkPoolKey(key)
		if !ok {
			slog.Warn("perk catalog: skipped malformed pool key on rebuild", "key", key)
			continue
		}
		defs, err := buildPerkDefsFromPool(unitType, pathName, rank, merged[key])
		if err != nil {
			slog.Warn("perk catalog: skipped invalid pool on rebuild", "key", key, "err", err)
			continue
		}
		for _, def := range defs {
			if _, exists := fresh[def.ID]; exists {
				slog.Warn("perk catalog: duplicate perk id skipped on rebuild", "id", def.ID, "pool", key)
				continue
			}
			fresh[def.ID] = def
		}
	}

	perkDefsMu.Lock()
	perkDefsByID = fresh
	perkDefsMu.Unlock()
}

// validatePerkPoolEntries checks one incoming rank's perk array in
// isolation against the current merged catalog: every entry.ID must match
// unitIDPattern, no id may repeat WITHIN the array, and no id may already
// belong to a DIFFERENT (unitType,pathName,rank) location — perk ids are
// global (spec §7.2). Re-saving the SAME location (editing that pool's own
// entries) is fine and is not rejected here.
//
// Returns a plain error (no editorValidationError wrap) — this is the
// persistence-layer validator, called directly by SavePerkPool and reused
// (wrapped) by SaveEditorPerkPool, mirroring how validatePathFile is shared
// between SavePathDef and SaveEditorPath.
func validatePerkPoolEntries(unitType, pathName, rank string, entries []perkEntryJSON) error {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !unitIDPattern.MatchString(entry.ID) {
			return fmt.Errorf("perk id %q must match %s", entry.ID, unitIDPattern)
		}
		if _, dup := seen[entry.ID]; dup {
			return fmt.Errorf("perk id %q appears more than once in this pool", entry.ID)
		}
		seen[entry.ID] = struct{}{}

		if existing, ok := perkDefLookup(entry.ID); ok {
			if existing.UnitType != unitType || existing.Path != pathName || existing.Rank != rank {
				return fmt.Errorf("perk id %q is already defined at %s; perk ids must be globally unique",
					entry.ID, perkPoolKey(existing.UnitType, existing.Path, existing.Rank))
			}
		}
	}
	return nil
}

// SavePerkPool validates then persists an authored rank's perk pool to
// <faction>/<unitType>/paths/<pathName>/perks/<rank>.json, registers it in
// the writable overlay, and rebuilds the perk registry so the change is
// visible immediately. faction comes from the owning unit's UnitDef.
func SavePerkPool(unitType, pathName, rank string, entries []perkEntryJSON) error {
	if !unitIDPattern.MatchString(unitType) {
		return fmt.Errorf("unit type %q must match %s", unitType, unitIDPattern)
	}
	if !unitIDPattern.MatchString(pathName) {
		return fmt.Errorf("path id %q must match %s", pathName, unitIDPattern)
	}
	if !unitIDPattern.MatchString(rank) {
		return fmt.Errorf("rank %q must match %s", rank, unitIDPattern)
	}
	if _, ok := validRankName[rank]; !ok {
		return fmt.Errorf("rank %q must be one of bronze/silver/gold", rank)
	}
	if err := validatePerkPoolEntries(unitType, pathName, rank, entries); err != nil {
		return err
	}

	unitDef, ok := getUnitDef(unitType)
	if !ok {
		return fmt.Errorf("unit type %q not found", unitType)
	}
	dir, err := resolvePerksDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, unitDef.Faction, unitType, unitPathsSubdirName, pathName, perkPoolDirName)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, rank+".json"), raw, 0o644); err != nil {
		return err
	}

	key := perkPoolKey(unitType, pathName, rank)
	runtimePerkPoolsMu.Lock()
	runtimePerkPools[key] = clonePerkEntries(entries)
	runtimePerkPoolsMu.Unlock()
	rebuildPerkRegistry()
	return nil
}

// PerkPoolIsEmbedded reports whether a (unit,path,rank) pool exists in the
// embedded catalog (as opposed to being purely overlay-authored). Used by
// the future editor HTTP handler to decide whether a "delete" should be
// reported as "reset to shipped default" vs. "removed".
func PerkPoolIsEmbedded(unitType, pathName, rank string) bool {
	_, ok := embeddedPerkPools[perkPoolKey(unitType, pathName, rank)]
	return ok
}

// DeletePerkPool removes the on-disk override file (if any) and the overlay
// entry for (unitType,pathName,rank), then rebuilds the perk registry. Only
// the single <rank>.json is removed — the perks/ directory itself is left
// alone, since sibling ranks' files live there too. If the pool also exists
// in the embedded catalog, this reverts it to the shipped definition rather
// than making it disappear.
func DeletePerkPool(unitType, pathName, rank string) (existed bool, err error) {
	if !unitIDPattern.MatchString(unitType) || !unitIDPattern.MatchString(pathName) || !unitIDPattern.MatchString(rank) {
		return false, nil // never valid; also blocks path traversal
	}

	dir, derr := resolvePerksDir()
	if derr != nil {
		return false, derr
	}
	removedFromDisk, rerr := removePerkPoolFile(dir, unitType, pathName, rank)
	if rerr != nil {
		return false, rerr
	}

	key := perkPoolKey(unitType, pathName, rank)
	runtimePerkPoolsMu.Lock()
	_, inOverlay := runtimePerkPools[key]
	delete(runtimePerkPools, key)
	runtimePerkPoolsMu.Unlock()

	rebuildPerkRegistry()
	return removedFromDisk || inOverlay, nil
}

// removePerkPoolFile deletes exactly
// <faction>/<unitType>/paths/<pathName>/perks/<rank>.json from the writable
// catalog tree rooted at dir. Traversal-safe: it walks dir and only removes
// a FILE named "<rank>.json" whose parent is a directory named "perks"
// (perkPoolDirName), whose parent is a directory named pathName, whose
// parent is a directory named "paths" (unitPathsSubdirName), whose parent
// is a directory named unitType — matched by basename equality only, never
// by constructing a path from caller-controlled strings. All three ids are
// already constrained to unitIDPattern by callers.
func removePerkPoolFile(dir, unitType, pathName, rank string) (removed bool, err error) {
	target := rank + ".json"
	walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || d.Name() != target {
			return nil
		}
		perksDir := filepath.Dir(p)
		if filepath.Base(perksDir) != perkPoolDirName {
			return nil
		}
		pathDir := filepath.Dir(perksDir)
		if filepath.Base(pathDir) != pathName {
			return nil
		}
		pathsDir := filepath.Dir(pathDir)
		if filepath.Base(pathsDir) != unitPathsSubdirName {
			return nil
		}
		unitDir := filepath.Dir(pathsDir)
		if filepath.Base(unitDir) != unitType {
			return nil
		}
		if rerr := os.Remove(p); rerr != nil {
			return rerr
		}
		removed = true
		return nil
	})
	return removed, walkErr
}

// LoadPersistedPerksIntoOverlay overlays writable perk pool files onto the
// embedded catalog at startup, mirroring LoadPersistedPathsIntoOverlay.
// Tolerates a missing writable dir (embedded-only deployment).
func LoadPersistedPerksIntoOverlay() {
	dir, err := resolvePerksDir()
	if err != nil {
		slog.Info("persisted perks: no writable units dir; using embedded catalog only", "err", err)
		return
	}
	n := loadPersistedPerkPoolsFromDir(dir)
	rebuildPerkRegistry()
	if n > 0 {
		slog.Info("persisted perks: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

// loadPersistedPerkPoolsFromDir walks dir looking for files at exactly
// <faction>/<unit>/paths/<path>/perks/<rank>.json — the file's basename
// (minus extension) must be a valid rank name, its parent directory must be
// named "perks", and that "perks" directory's parent supplies the path
// name (one level up) and, one level above that, "paths" + the unit type.
// This naturally excludes the path's own <path>.json (one directory
// shallower) and unit def files, without needing to special-case them.
//
// Per-file duplicate-id checks here are best-effort against whatever the
// registry currently holds (registration only happens once at the end, per
// the "one rebuild at the end" contract) — the actual authority for
// cross-file collisions among freshly loaded files is rebuildPerkRegistry's
// own fresh-map collision guard, which always runs after every file in this
// walk has been registered into the overlay.
func loadPersistedPerkPoolsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
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
		rank := strings.TrimSuffix(name, filepath.Ext(name))
		if _, ok := validRankName[rank]; !ok {
			return nil
		}
		perksDir := filepath.Dir(p)
		if filepath.Base(perksDir) != perkPoolDirName {
			return nil
		}
		pathDir := filepath.Dir(perksDir)
		pathName := filepath.Base(pathDir)
		pathsDir := filepath.Dir(pathDir)
		if filepath.Base(pathsDir) != unitPathsSubdirName {
			return nil
		}
		unitType := filepath.Base(filepath.Dir(pathsDir))

		entries, perr := parsePersistedPerkPoolFile(p)
		if perr != nil {
			slog.Warn("persisted perks: skipped file", "file", p, "err", perr)
			return nil
		}
		if verr := validatePerkPoolEntries(unitType, pathName, rank, entries); verr != nil {
			slog.Warn("persisted perks: skipped invalid pool", "file", p, "err", verr)
			return nil
		}

		key := perkPoolKey(unitType, pathName, rank)
		runtimePerkPoolsMu.Lock()
		runtimePerkPools[key] = clonePerkEntries(entries)
		runtimePerkPoolsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedPerkPoolFile(p string) ([]perkEntryJSON, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var entries []perkEntryJSON
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
