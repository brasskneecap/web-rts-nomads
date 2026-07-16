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

var effectIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var (
	runtimeEffectsMu sync.RWMutex
	runtimeEffects   = map[string]EffectDef{}
)

// resolveEffectsDir returns the writable effects catalog dir: EFFECT_CATALOG_DIR
// if set, else the dev source tree.
func resolveEffectsDir() (string, error) {
	if dir := os.Getenv("EFFECT_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "effects")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("effects directory not found at %s; set EFFECT_CATALOG_DIR env var to override", dir)
}

// SaveEffectDef validates and writes an authored effect def to
// <dir>/<id>/<id>.json, then registers it in the overlay.
func SaveEffectDef(def *EffectDef) error {
	if !effectIDPattern.MatchString(def.ID) {
		return fmt.Errorf("effect id %q must match %s", def.ID, effectIDPattern)
	}
	if err := validateEffectDef(def); err != nil {
		return err
	}
	dir, err := resolveEffectsDir()
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
	runtimeEffectsMu.Lock()
	runtimeEffects[def.ID] = *def
	runtimeEffectsMu.Unlock()
	return nil
}

// EffectIsEmbedded reports whether an effect id ships in the embedded catalog.
func EffectIsEmbedded(id string) bool {
	_, ok := effectDefsByID[id]
	return ok
}

// DeleteEffectOverride removes the override file + overlay entry for an id.
func DeleteEffectOverride(id string) (existed bool, err error) {
	if !effectIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveEffectsDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id)) // best-effort: drop the now-empty dir
	}
	runtimeEffectsMu.Lock()
	_, inOverlay := runtimeEffects[id]
	delete(runtimeEffects, id)
	runtimeEffectsMu.Unlock()
	return removed || inOverlay, nil
}

// LoadPersistedEffectsIntoOverlay overlays writable effect defs onto the embed
// at startup. Best-effort; a bad file is skipped, never fatal.
func LoadPersistedEffectsIntoOverlay() {
	dir, err := resolveEffectsDir()
	if err != nil {
		slog.Info("persisted effects: no writable effects dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedEffectsFromDir(dir); n > 0 {
		slog.Info("persisted effects: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedEffectsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedEffectFile(path)
		if perr != nil {
			slog.Warn("persisted effects: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeEffectsMu.Lock()
		runtimeEffects[def.ID] = *def
		runtimeEffectsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedEffectFile(path string) (*EffectDef, error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d EffectDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("effect has empty id")
	}
	if verr := validateEffectDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
