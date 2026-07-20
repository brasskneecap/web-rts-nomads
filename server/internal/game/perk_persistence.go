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

// buildPerkRegistry merges the embedded baseline with an overlay snapshot
// (overlay wins) into a fresh id->*PerkDef map, deterministically (ids sorted).
// Pure: it neither locks nor mutates shared state, so it is safe to call from a
// package-level var initializer (single-threaded, before any init() runs) AND
// from rebuildPerkRegistry at runtime — the caller owns synchronization.
func buildPerkRegistry(overlay map[string]PerkDef) map[string]*PerkDef {
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
	return fresh
}

// rebuildPerkRegistry snapshots the writable overlay, rebuilds the merged
// registry, and swaps it in under perkDefsMu. Called at runtime after the
// overlay changes (SavePerkDef / DeletePerkOverride / LoadPersistedPerksIntoOverlay).
func rebuildPerkRegistry() {
	runtimePerksMu.RLock()
	overlay := make(map[string]PerkDef, len(runtimePerks))
	for k, v := range runtimePerks {
		overlay[k] = v
	}
	runtimePerksMu.RUnlock()

	fresh := buildPerkRegistry(overlay)

	perkDefsMu.Lock()
	perkDefsByID = fresh
	perkDefsMu.Unlock()
}

// perkAssocDir returns the on-disk folder segment for a perk association.
// Empty association (generic perk) lives under "generic".
func perkAssocDir(assocPath string) string {
	if assocPath == "" {
		return "generic"
	}
	return assocPath
}

// SavePerkDef validates and writes an authored perk def to
// <dir>/<assoc>/<id>/<id>.json (assoc derived from def.Path, "generic" when
// empty), then registers it in the overlay and rebuilds the registry so the
// change is visible immediately. The on-disk body never carries "path" — that
// association is encoded by the folder and re-derived on load.
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
	assoc := perkAssocDir(def.Path)
	outDir := filepath.Join(dir, assoc, def.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// The folder encodes association; do not also write path into the file body
	// (it is re-derived from the folder on load).
	toWrite := *def
	toWrite.Path = ""
	raw, err := json.MarshalIndent(&toWrite, "", "  ")
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
// purely-authored id disappears. The association folder (<assoc>/<id>/) is
// resolved from whatever currently knows the id's Path (embedded baseline or
// live overlay) so the right file gets targeted.
func DeletePerkOverride(id string) (existed bool, err error) {
	if !perkIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolvePerksDir()
	if derr != nil {
		return false, derr
	}
	assocPath := ""
	if d, ok := embeddedPerkDefs[id]; ok {
		assocPath = d.Path
	}
	runtimePerksMu.RLock()
	if d, ok := runtimePerks[id]; ok {
		assocPath = d.Path
	}
	runtimePerksMu.RUnlock()
	assoc := perkAssocDir(assocPath)

	removed := false
	if rerr := os.Remove(filepath.Join(dir, assoc, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, assoc, id)) // best-effort: drop the now-empty dir
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
		// Association = the folder two levels up: <dir>/<assoc>/<id>/<id>.json.
		assoc := filepath.Base(filepath.Dir(filepath.Dir(path)))
		if assoc == "generic" {
			def.Path = ""
		} else {
			def.Path = assoc
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

// The initial registry is built by perkDefsByID's var initializer in
// perk_defs.go (buildPerkRegistry) so it is ready before any init() runs —
// notably path_defs.go's init, which validates perksByRank ids via
// perkDefLookup. Runtime refreshes go through rebuildPerkRegistry above.
