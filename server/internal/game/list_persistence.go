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

// ─── Writable list overlay (mirrors item_persistence.go) ────────────────────
//
// Lists are the one catalog that could be SAVED but never RELOADED: the old
// SaveItemListDef / SaveRecipeListDef wrote a file and registered it live, but
// nothing read those files back at boot, so an authored list silently vanished
// on restart. Since this change is what finally makes lists authorable from the
// editor, LoadPersistedListsIntoOverlay is load-bearing, not a nicety.

var (
	runtimeListsMu sync.RWMutex
	runtimeLists   = map[string]*ListDef{}
)

func resolveListsDir() (string, error) {
	if dir := os.Getenv("LIST_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "lists")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("lists directory not found at %s; set LIST_CATALOG_DIR env var to override", dir)
}

// SaveListDef validates, writes <dir>/<id>.json, and registers the list live so
// a match started right after the save already sees it.
func SaveListDef(def *ListDef) error {
	if def.ID == "" {
		return fmt.Errorf("list id is required")
	}
	if !itemIDPattern.MatchString(def.ID) {
		return fmt.Errorf("list id %q must match %s", def.ID, itemIDPattern)
	}
	if err := validateListDef(def); err != nil {
		return err
	}
	dir, err := resolveListsDir()
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
	runtimeListsMu.Lock()
	runtimeLists[def.ID] = &reg
	runtimeListsMu.Unlock()
	return nil
}

// listIsEmbedded reports whether id ships in the embedded catalog. Mirrors
// ItemIsEmbedded — used by DeleteEditorList to tell a TRUE delete (an
// override-only list that will actually stop resolving) from a delete that
// merely un-shadows the shipped def, which resurfaces and keeps every
// existing reference valid.
func listIsEmbedded(id string) bool {
	_, ok := listCatalogSingleton[id]
	return ok
}

// DeleteListOverride removes the writable list file + overlay entry. existed is
// false when the id names nothing.
func DeleteListOverride(id string) (existed bool, err error) {
	dir, derr := resolveListsDir()
	if derr != nil {
		return false, derr
	}
	removed := false
	if rerr := os.Remove(filepath.Join(dir, id+".json")); rerr == nil {
		removed = true
	}
	runtimeListsMu.Lock()
	_, inOverlay := runtimeLists[id]
	delete(runtimeLists, id)
	runtimeListsMu.Unlock()
	return removed || inOverlay, nil
}

// LoadPersistedListsIntoOverlay — startup hook, best-effort. One bad file is a
// logged skip, never a failed boot.
func LoadPersistedListsIntoOverlay() {
	dir, err := resolveListsDir()
	if err != nil {
		slog.Info("persisted lists: no writable lists dir; using embedded catalog only", "err", err)
		return
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		slog.Info("persisted lists: lists dir unreadable; using embedded catalog only", "err", rerr)
		return
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		def, perr := parsePersistedListFile(path)
		if perr != nil {
			slog.Warn("persisted lists: skipped file", "file", e.Name(), "err", perr)
			continue
		}
		runtimeListsMu.Lock()
		runtimeLists[def.ID] = def
		runtimeListsMu.Unlock()
		loaded++
	}
	if loaded > 0 {
		slog.Info("persisted lists: overlaid on embedded catalog", "count", loaded, "dir", dir)
	}
}

func parsePersistedListFile(path string) (*ListDef, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d ListDef
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, fmt.Errorf("list has empty id")
	}
	if err := validateListDef(&d); err != nil {
		return nil, err
	}
	return &d, nil
}
