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

var abilityIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var (
	runtimeAbilitiesMu sync.RWMutex
	runtimeAbilities   = map[string]AbilityDef{}
)

// resolveAbilitiesDir returns the writable abilities catalog dir:
// ABILITY_CATALOG_DIR if set, else the dev source tree.
func resolveAbilitiesDir() (string, error) {
	if dir := os.Getenv("ABILITY_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "abilities")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("abilities directory not found at %s; set ABILITY_CATALOG_DIR env var to override", dir)
}

// SaveAbilityDef validates and writes an authored ability def to
// <dir>/<id>/<id>.json, then registers it in the overlay.
func SaveAbilityDef(def *AbilityDef) error {
	if !abilityIDPattern.MatchString(def.ID) {
		return fmt.Errorf("ability id %q must match %s", def.ID, abilityIDPattern)
	}
	if err := validateAbilityDef(def); err != nil {
		return err
	}
	dir, err := resolveAbilitiesDir()
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
	runtimeAbilitiesMu.Lock()
	runtimeAbilities[def.ID] = *def
	runtimeAbilitiesMu.Unlock()
	return nil
}

// AbilityIsEmbedded reports whether an ability id ships in the embedded catalog.
func AbilityIsEmbedded(id string) bool {
	_, ok := abilityDefsByID[id]
	return ok
}

// DeleteAbilityOverride removes the override file + overlay entry for an id.
// Embed-backed ids reset to their shipped default; overlay-only ids are gone.
func DeleteAbilityOverride(id string) (existed bool, err error) {
	if !abilityIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveAbilitiesDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id)) // best-effort: drop the now-empty dir
	}
	runtimeAbilitiesMu.Lock()
	_, inOverlay := runtimeAbilities[id]
	delete(runtimeAbilities, id)
	runtimeAbilitiesMu.Unlock()
	return removed || inOverlay, nil
}

// LoadPersistedAbilitiesIntoOverlay overlays writable ability defs onto the
// embed at startup. Best-effort; a bad file is skipped, never fatal.
func LoadPersistedAbilitiesIntoOverlay() {
	dir, err := resolveAbilitiesDir()
	if err != nil {
		slog.Info("persisted abilities: no writable abilities dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedAbilitiesFromDir(dir); n > 0 {
		slog.Info("persisted abilities: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedAbilitiesFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedAbilityFile(path)
		if perr != nil {
			slog.Warn("persisted abilities: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeAbilitiesMu.Lock()
		runtimeAbilities[def.ID] = *def
		runtimeAbilitiesMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedAbilityFile(path string) (*AbilityDef, error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d AbilityDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("ability has empty id")
	}
	if verr := validateAbilityDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
