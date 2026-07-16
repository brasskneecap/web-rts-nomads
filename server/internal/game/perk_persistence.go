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

var (
	runtimePerksMu sync.RWMutex
	runtimePerks   = map[string]PerkDef{}
)

// resolvePerksDir returns the writable perk catalog dir: PERK_CATALOG_DIR if
// set, else the dev source tree at internal/game/catalog/perks. Mirrors
// resolveEffectsDir.
func resolvePerksDir() (string, error) {
	if dir := os.Getenv("PERK_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "perks")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("perks directory not found at %s; set PERK_CATALOG_DIR to override", dir)
}

// rebuildPerkRegistry merges embeddedPerkDefs + runtimePerks (overlay wins) into
// a fresh id->*PerkDef map, swapped under perkDefsMu.Lock(). Sorted-by-id build
// preserves determinism. Readers (perkDefLookup/snapshotPerkDefs/ListPerkDefs)
// are unchanged — they still read perkDefsByID under perkDefsMu.
func rebuildPerkRegistry() {
	runtimePerksMu.RLock()
	overlay := make(map[string]PerkDef, len(runtimePerks))
	for k, v := range runtimePerks {
		overlay[k] = v
	}
	runtimePerksMu.RUnlock()

	merged := make(map[string]PerkDef, len(embeddedPerkDefs)+len(overlay))
	for k, v := range embeddedPerkDefs {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fresh := make(map[string]*PerkDef, len(merged))
	for _, id := range ids {
		def := merged[id]
		fresh[id] = &def
	}
	perkDefsMu.Lock()
	perkDefsByID = fresh
	perkDefsMu.Unlock()
}

// SavePerkDef validates and writes an authored perk def to <dir>/<id>/<id>.json,
// then registers it in the overlay and rebuilds the registry so the change is
// visible immediately.
func SavePerkDef(def *PerkDef) error {
	if !perkIDPattern.MatchString(def.ID) {
		return fmt.Errorf("perk id %q must match %s", def.ID, perkIDPattern)
	}
	if err := validatePerkDef(def); err != nil {
		return err
	}
	dir, err := resolvePerksDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, def.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	runtimePerksMu.Lock()
	runtimePerks[def.ID] = *def
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	return nil
}

// PerkIsEmbedded reports whether a perk id ships in the embedded catalog.
func PerkIsEmbedded(id string) bool {
	_, ok := embeddedPerkDefs[id]
	return ok
}

// DeletePerkOverride removes the override file + overlay entry for an id, then
// rebuilds the registry. An embedded id reverts to its shipped default; a
// purely-authored id disappears.
func DeletePerkOverride(id string) (existed bool, err error) {
	if !perkIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolvePerksDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id)) // best-effort: drop the now-empty dir
	}
	runtimePerksMu.Lock()
	_, inOverlay := runtimePerks[id]
	delete(runtimePerks, id)
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	return removed || inOverlay, nil
}

// LoadPersistedPerksIntoOverlay overlays writable perk defs onto the embed at
// startup. Best-effort; a bad file is skipped, never fatal.
func LoadPersistedPerksIntoOverlay() {
	dir, err := resolvePerksDir()
	if err != nil {
		slog.Info("persisted perks: no writable perks dir; using embedded catalog only", "err", err)
		return
	}
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var def PerkDef
		if json.Unmarshal(raw, &def) != nil || def.ID == "" || validatePerkDef(&def) != nil {
			slog.Warn("persisted perks: skipped file", "file", d.Name())
			return nil
		}
		runtimePerksMu.Lock()
		runtimePerks[def.ID] = def
		runtimePerksMu.Unlock()
		loaded++
		return nil
	})
	if loaded > 0 {
		rebuildPerkRegistry()
		slog.Info("persisted perks: overlaid on embedded catalog", "count", loaded, "dir", dir)
	}
}

// init builds the initial registry from the embedded standalone catalog.
func init() { rebuildPerkRegistry() }
