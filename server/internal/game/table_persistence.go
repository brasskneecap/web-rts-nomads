package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ─── Writable table overlay (mirrors list_persistence.go) ───────────────────

var (
	runtimeTablesMu sync.RWMutex
	runtimeTables   = map[string]*TableDef{}
)

func resolveTablesDir() (string, error) {
	if dir := os.Getenv("TABLE_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "tables")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("tables directory not found at %s; set TABLE_CATALOG_DIR env var to override", dir)
}

// SaveTableDef validates, writes <dir>/<id>.json, and registers the table live.
func SaveTableDef(def *TableDef) error {
	if def.ID == "" {
		return fmt.Errorf("table id is required")
	}
	if !itemIDPattern.MatchString(def.ID) {
		return fmt.Errorf("table id %q must match %s", def.ID, itemIDPattern)
	}
	if err := validateTableDef(def); err != nil {
		return err
	}
	dir, err := resolveTablesDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	runtimeTablesMu.Lock()
	runtimeTables[def.ID] = &reg
	runtimeTablesMu.Unlock()
	return nil
}

// DeleteTableOverride removes the writable table file + overlay entry.
func DeleteTableOverride(id string) (existed bool, err error) {
	dir, derr := resolveTablesDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id+".json")); rerr == nil {
		removed = true
	}
	runtimeTablesMu.Lock()
	_, inOverlay := runtimeTables[id]
	delete(runtimeTables, id)
	runtimeTablesMu.Unlock()
	return removed || inOverlay, nil
}

// LoadPersistedTablesIntoOverlay — startup hook, best-effort. One bad file is a
// logged skip, never a failed boot.
func LoadPersistedTablesIntoOverlay() {
	dir, err := resolveTablesDir()
	if err != nil {
		slog.Info("persisted tables: no writable tables dir; using embedded catalog only", "err", err)
		return
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		slog.Info("persisted tables: tables dir unreadable; using embedded catalog only", "err", rerr)
		return
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		def, perr := parsePersistedTableFile(filepath.Join(dir, e.Name()))
		if perr != nil {
			slog.Warn("persisted tables: skipped file", "file", e.Name(), "err", perr)
			continue
		}
		runtimeTablesMu.Lock()
		runtimeTables[def.ID] = def
		runtimeTablesMu.Unlock()
		loaded++
	}
	if loaded > 0 {
		slog.Info("persisted tables: overlaid on embedded catalog", "count", loaded, "dir", dir)
	}
}

func parsePersistedTableFile(path string) (*TableDef, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d TableDef
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, fmt.Errorf("table has empty id")
	}
	if err := validateTableDef(&d); err != nil {
		return nil, err
	}
	return &d, nil
}
