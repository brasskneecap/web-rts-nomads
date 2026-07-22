package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ─── Writable tileset overlay ────────────────────────────────────────────────
//
// Mirrors the campaign/item persistence systems (campaign_persistence.go,
// item_persistence.go): editor saves write a JSON file (and, for images, a
// PNG) into a writable dir and register it into an in-memory overlay that
// WINS over the embedded tileset defs in every reader (ListTilesetDefs,
// GetTilesetDef). Loaded once at startup by LoadPersistedTilesetsIntoOverlay;
// per-file failures are logged skips so the server always starts.

var (
	runtimeTilesetsMu sync.RWMutex
	runtimeTilesets   = map[string]TilesetDef{}
)

// tilesetIDPattern is the id discipline for author-created tilesets. Embedded
// defs predate it and are exempt (validated by their own loader).
var tilesetIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// tilesetImagesSubdirName is the subdirectory (inside the tilesets catalog
// dir) that uploaded tileset sheet PNGs are written to.
const tilesetImagesSubdirName = "images"

// maxTilesetImageBytes caps uploaded tileset sheet size (sheets are larger
// than item icons, but still bounded to keep the overlay dir sane).
const maxTilesetImageBytes = 4 * 1024 * 1024

// currentTilesetDefs returns the embedded def baseline with runtime editor
// saves overlaid on top (overlay wins). Callers treat the result as
// read-only. Cheap; the catalog is tiny.
func currentTilesetDefs() map[string]TilesetDef {
	merged := make(map[string]TilesetDef, len(tilesetDefsByID))
	for id, d := range tilesetDefsByID {
		merged[id] = d
	}
	runtimeTilesetsMu.RLock()
	for id, d := range runtimeTilesets {
		merged[id] = d
	}
	runtimeTilesetsMu.RUnlock()
	return merged
}

func resolveTilesetsDir() (string, error) {
	if dir := os.Getenv("TILESET_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "tilesets")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("tilesets directory not found at %s; set TILESET_CATALOG_DIR env var to override", dir)
}

// tilesetSaveError is an author-fixable tileset save/delete failure (→ HTTP
// 400) rather than infrastructure (→ 500).
type tilesetSaveError struct{ msg string }

func (e *tilesetSaveError) Error() string { return e.msg }
func errTilesetSave(msg string) error     { return &tilesetSaveError{msg: msg} }

// IsTilesetValidationError reports whether err is an author-fixable tileset
// save/delete failure (→ HTTP 400) rather than infrastructure (→ 500).
func IsTilesetValidationError(err error) bool {
	var e *tilesetSaveError
	return errors.As(err, &e)
}

// SaveTilesetDef validates and persists a tileset definition, then registers
// it into the runtime overlay so it is visible without a restart. Returns a
// tilesetSaveError (→ HTTP 400) for bad input.
func SaveTilesetDef(def TilesetDef) error {
	id := strings.TrimSpace(def.ID)
	if id == "" {
		return errTilesetSave("tileset id is required")
	}
	if !tilesetIDPattern.MatchString(id) {
		return errTilesetSave(fmt.Sprintf("tileset id %q must match %s", id, tilesetIDPattern))
	}
	if strings.TrimSpace(def.Name) == "" {
		return errTilesetSave("tileset name is required")
	}
	if strings.TrimSpace(def.Image) == "" {
		return errTilesetSave("tileset image is required")
	}
	if def.Cols <= 0 {
		return errTilesetSave("tileset cols must be > 0")
	}
	if def.Rows <= 0 {
		return errTilesetSave("tileset rows must be > 0")
	}
	if def.TileWidth <= 0 {
		return errTilesetSave("tileset tileWidth must be > 0")
	}
	if def.TileHeight <= 0 {
		return errTilesetSave("tileset tileHeight must be > 0")
	}
	dir, err := resolveTilesetsDir()
	if err != nil {
		return err
	}
	def.ID = id
	if werr := writeTilesetDefToDisk(dir, def); werr != nil {
		return werr
	}
	runtimeTilesetsMu.Lock()
	runtimeTilesets[id] = def
	runtimeTilesetsMu.Unlock()
	return nil
}

// writeTilesetDefToDisk serializes def to <dir>/<id>.json.
func writeTilesetDefToDisk(dir string, def TilesetDef) error {
	safeID := sanitizeMapFilename(def.ID)
	if safeID == "" {
		return errTilesetSave(fmt.Sprintf("tileset id %q is not a valid filename", def.ID))
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(dir, safeID+".json"), raw, 0644)
}

// SaveTilesetImage validates and stores an uploaded PNG sheet for the
// tileset, returning the image key (relative filename) the def's Image field
// should reference.
func SaveTilesetImage(id string, data []byte) (string, error) {
	id = strings.TrimSpace(id)
	if !tilesetIDPattern.MatchString(id) {
		return "", errTilesetSave(fmt.Sprintf("tileset id %q must match %s", id, tilesetIDPattern))
	}
	if len(data) > maxTilesetImageBytes {
		return "", errTilesetSave(fmt.Sprintf("tileset image exceeds %d bytes", maxTilesetImageBytes))
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", errTilesetSave(fmt.Sprintf("tileset image is not a valid PNG: %v", err))
	}
	dir, err := resolveTilesetsDir()
	if err != nil {
		return "", err
	}
	imgDir := filepath.Join(dir, tilesetImagesSubdirName)
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		return "", err
	}
	key := id + ".png"
	if err := os.WriteFile(filepath.Join(imgDir, key), data, 0o644); err != nil {
		return "", err
	}
	return key, nil
}

// TilesetImagePath resolves an uploaded tileset image key to its on-disk
// path. Returns false if the key is unsafe or the file does not exist.
func TilesetImagePath(key string) (string, bool) {
	if key == "" || strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return "", false
	}
	dir, err := resolveTilesetsDir()
	if err != nil {
		return "", false
	}
	path := filepath.Join(dir, tilesetImagesSubdirName, key)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// mapsReferencingTileset returns, sorted, the ids of maps whose tiles[]
// reference tilesetID via TileCoord.Tileset.
func mapsReferencingTileset(tilesetID string) []string {
	var ids []string
	for _, entry := range currentMapCatalogSnapshot() {
		for _, tile := range entry.Map.Tiles {
			if tile.Tileset == tilesetID {
				ids = append(ids, entry.ID)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// DeleteTilesetDef removes an author-created tileset definition. Guard:
// refuses if any map still references the tileset (deleting it would orphan
// tile references) — the caller must repaint those tiles first.
//
// Returns whether an overlay entry existed and a tilesetSaveError (→ 400) for
// a guard failure.
func DeleteTilesetDef(id string) (existed bool, err error) {
	if refs := mapsReferencingTileset(id); len(refs) > 0 {
		return false, errTilesetSave(
			"tileset is still used by maps: " + strings.Join(refs, ", ") +
				" — repaint those tiles first")
	}
	dir, derr := resolveTilesetsDir()
	if derr != nil {
		return false, derr
	}
	safeID := sanitizeMapFilename(id)
	if safeID == "" {
		return false, errTilesetSave("invalid tileset id")
	}
	runtimeTilesetsMu.Lock()
	_, existed = runtimeTilesets[id]
	delete(runtimeTilesets, id)
	runtimeTilesetsMu.Unlock()

	if rmErr := os.Remove(filepath.Join(dir, safeID+".json")); rmErr != nil && !os.IsNotExist(rmErr) {
		return existed, rmErr
	}
	return existed, nil
}

// LoadPersistedTilesetsIntoOverlay loads editor-saved tileset defs at
// startup so they survive a restart. Best-effort, never fatal (mirrors
// LoadPersistedCampaignsIntoOverlay). In dev the writable dir IS the embed
// source dir, so re-reading the built-in defs is a harmless no-op overlay.
func LoadPersistedTilesetsIntoOverlay() {
	dir, err := resolveTilesetsDir()
	if err != nil {
		slog.Info("persisted tilesets: no writable dir; using embedded defs only", "err", err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Info("persisted tilesets: read dir failed; using embedded defs only", "err", err)
		return
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if rerr != nil {
			slog.Warn("persisted tilesets: skip unreadable file", "file", entry.Name(), "err", rerr)
			continue
		}
		var def TilesetDef
		if jerr := json.Unmarshal(data, &def); jerr != nil {
			slog.Warn("persisted tilesets: skip malformed file", "file", entry.Name(), "err", jerr)
			continue
		}
		if def.ID == "" || def.Image == "" || def.Cols <= 0 || def.Rows <= 0 || def.TileWidth <= 0 || def.TileHeight <= 0 {
			slog.Warn("persisted tilesets: skip file missing required fields", "file", entry.Name())
			continue
		}
		runtimeTilesetsMu.Lock()
		runtimeTilesets[def.ID] = def
		runtimeTilesetsMu.Unlock()
		count++
	}
	if count > 0 {
		slog.Info("persisted tilesets: overlaid on embedded defs", "count", count, "dir", dir)
	}
}
