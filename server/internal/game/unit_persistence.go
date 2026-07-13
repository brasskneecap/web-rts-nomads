package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var unitIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// unitPathsSubdirName is skipped on any catalog walk — promotion paths are a
// separate catalog dimension owned by path_defs.go, not by this editor.
const unitPathsSubdirName = "paths"

var (
	runtimeUnitsMu sync.RWMutex
	runtimeUnits   = map[string]UnitDef{}
)

// resolveUnitsDir returns the writable units catalog dir: UNIT_CATALOG_DIR if
// set, else the dev source tree.
func resolveUnitsDir() (string, error) {
	if dir := os.Getenv("UNIT_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "units")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("units directory not found at %s; set UNIT_CATALOG_DIR env var to override", dir)
}

// SaveUnitDef validates and writes an authored unit def to
// <dir>/<faction>/<type>/<type>.json, then registers it in the overlay.
func SaveUnitDef(def *UnitDef) error {
	if !unitIDPattern.MatchString(def.Type) {
		return fmt.Errorf("unit type %q must match %s", def.Type, unitIDPattern)
	}
	if !unitIDPattern.MatchString(def.Faction) {
		return fmt.Errorf("unit faction %q must match %s", def.Faction, unitIDPattern)
	}
	if err := validateUnitDef(def); err != nil {
		return err
	}
	dir, err := resolveUnitsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, def.Faction, def.Type)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	// Remove any previous override under a different faction so an edited unit
	// never exists at two paths.
	removeUnitOverrideFiles(dir, def.Type)
	if err := os.WriteFile(filepath.Join(outDir, def.Type+".json"), raw, 0o644); err != nil {
		return err
	}
	runtimeUnitsMu.Lock()
	runtimeUnits[def.Type] = *def
	runtimeUnitsMu.Unlock()
	return nil
}

// UnitIsEmbedded reports whether a unit type exists in the embedded catalog.
func UnitIsEmbedded(unitType string) bool {
	_, ok := unitDefsByType[unitType]
	return ok
}

// DeleteUnitOverride removes the override file(s) + overlay entry for a type.
func DeleteUnitOverride(unitType string) (existed bool, err error) {
	if !unitIDPattern.MatchString(unitType) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveUnitsDir()
	if derr != nil {
		return false, derr
	}
	removed := removeUnitOverrideFiles(dir, unitType)
	runtimeUnitsMu.Lock()
	_, inOverlay := runtimeUnits[unitType]
	delete(runtimeUnits, unitType)
	runtimeUnitsMu.Unlock()
	return removed || inOverlay, nil
}

// removeUnitOverrideFiles deletes every <type>/<type>.json for the given type
// under dir, skipping the paths/ subdir. Returns whether anything was removed.
func removeUnitOverrideFiles(dir, unitType string) bool {
	removed := false
	target := unitType + ".json"
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == unitPathsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		// Match ONLY <faction>/<type>/<type>.json. Without the parent-dir check,
		// deleting a unit legally named "faction" would remove every
		// faction.json record in the catalog.
		if d.Name() == target && filepath.Base(filepath.Dir(path)) == unitType {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
				// Drop the now-empty <type>/ dir too. os.Remove on a directory
				// only succeeds when it is EMPTY, so a unit that still has a
				// paths/ subdir (or anything else) keeps its directory. Never
				// recurse this delete — it must never destroy content it
				// doesn't own.
				_ = os.Remove(filepath.Dir(path))
			}
		}
		return nil
	})
	return removed
}

// LoadPersistedUnitsIntoOverlay overlays writable unit defs onto the embed at startup.
func LoadPersistedUnitsIntoOverlay() {
	dir, err := resolveUnitsDir()
	if err != nil {
		slog.Info("persisted units: no writable units dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedUnitsFromDir(dir); n > 0 {
		slog.Info("persisted units: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedUnitsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == unitPathsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == factionMetaFileName {
			return nil // owned by the faction registry, not a unit
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedUnitFile(path)
		if perr != nil {
			slog.Warn("persisted units: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeUnitsMu.Lock()
		runtimeUnits[def.Type] = *def
		runtimeUnitsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedUnitFile(path string) (def *UnitDef, err error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d UnitDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.Type == "" {
		return nil, fmt.Errorf("unit has empty type")
	}
	if verr := validateUnitDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
