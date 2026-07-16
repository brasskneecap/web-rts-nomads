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

var projectileIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

var (
	runtimeProjectilesMu sync.RWMutex
	runtimeProjectiles   = map[string]ProjectileDef{}
)

func resolveProjectilesDir() (string, error) {
	if dir := os.Getenv("PROJECTILE_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "projectiles")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("projectiles directory not found at %s; set PROJECTILE_CATALOG_DIR env var to override", dir)
}

// SaveProjectileDef validates+normalizes and writes an authored projectile def
// to <dir>/<id>/<id>.json, then registers it in the overlay.
func SaveProjectileDef(def *ProjectileDef) error {
	if !projectileIDPattern.MatchString(def.ID) {
		return fmt.Errorf("projectile id %q must match %s", def.ID, projectileIDPattern)
	}
	if err := validateProjectileDef(def); err != nil {
		return err
	}
	dir, err := resolveProjectilesDir()
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
	runtimeProjectilesMu.Lock()
	runtimeProjectiles[def.ID] = *def
	runtimeProjectilesMu.Unlock()
	return nil
}

func ProjectileIsEmbedded(id string) bool {
	_, ok := projectileDefsByID[id]
	return ok
}

func DeleteProjectileOverride(id string) (existed bool, err error) {
	if !projectileIDPattern.MatchString(id) {
		return false, nil
	}
	dir, derr := resolveProjectilesDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, id))
	}
	runtimeProjectilesMu.Lock()
	_, inOverlay := runtimeProjectiles[id]
	delete(runtimeProjectiles, id)
	runtimeProjectilesMu.Unlock()
	return removed || inOverlay, nil
}

func LoadPersistedProjectilesIntoOverlay() {
	dir, err := resolveProjectilesDir()
	if err != nil {
		slog.Info("persisted projectiles: no writable projectiles dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedProjectilesFromDir(dir); n > 0 {
		slog.Info("persisted projectiles: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

func loadPersistedProjectilesFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedProjectileFile(path)
		if perr != nil {
			slog.Warn("persisted projectiles: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeProjectilesMu.Lock()
		runtimeProjectiles[def.ID] = *def
		runtimeProjectilesMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedProjectileFile(path string) (*ProjectileDef, error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d ProjectileDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("projectile has empty id")
	}
	if verr := validateProjectileDef(&d); verr != nil {
		return nil, verr
	}
	return &d, nil
}
