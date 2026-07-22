package game

import (
	"bytes"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ─── Tile library ────────────────────────────────────────────────────────────
//
// A standalone store of individual tile PNG images, sibling to the Tileset
// system (tileset_persistence.go). Tiles live at
// <tilesets catalog dir>/tiles/<id>.png — a sibling subdir to the existing
// images/ subdir used for tileset sheets. There is no JSON metadata sidecar;
// a tile's width/height are read from the PNG itself at list time.
//
// Tiles are not referenced by maps (the build-tileset flow copies pixels into
// a separate sheet), so unlike DeleteTilesetDef there is no reference guard
// on delete.

// tilesLibrarySubdirName is the subdirectory (inside the tilesets catalog
// dir) that individual tile PNGs are written to.
const tilesLibrarySubdirName = "tiles"

// maxTileImageBytes caps an individual uploaded tile image.
const maxTileImageBytes = 2 * 1024 * 1024

// TileAsset describes a single tile image in the library, with dimensions
// read from the PNG itself.
type TileAsset struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// SaveTileImage validates and stores an uploaded PNG tile image, returning
// the image key (relative filename) callers use to reference it.
func SaveTileImage(id string, data []byte) (string, error) {
	id = strings.TrimSpace(id)
	if !tilesetIDPattern.MatchString(id) {
		return "", errTilesetSave(fmt.Sprintf("tile id %q must match %s", id, tilesetIDPattern))
	}
	if len(data) > maxTileImageBytes {
		return "", errTilesetSave(fmt.Sprintf("tile image exceeds %d bytes", maxTileImageBytes))
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", errTilesetSave(fmt.Sprintf("tile image is not a valid PNG: %v", err))
	}
	dir, err := resolveTilesetsDir()
	if err != nil {
		return "", err
	}
	tilesDir := filepath.Join(dir, tilesLibrarySubdirName)
	if err := os.MkdirAll(tilesDir, 0o755); err != nil {
		return "", err
	}
	key := id + ".png"
	if err := os.WriteFile(filepath.Join(tilesDir, key), data, 0o644); err != nil {
		return "", err
	}
	return key, nil
}

// ListTileAssets returns every tile image in the library, sorted by ID
// ascending. Never returns nil, and never panics if the tiles dir is
// missing or unreadable — it logs and returns an empty slice in that case.
func ListTileAssets() []TileAsset {
	assets := []TileAsset{}
	dir, err := resolveTilesetsDir()
	if err != nil {
		slog.Info("tile library: no writable dir; returning empty list", "err", err)
		return assets
	}
	tilesDir := filepath.Join(dir, tilesLibrarySubdirName)
	entries, err := os.ReadDir(tilesDir)
	if err != nil {
		slog.Info("tile library: read dir failed; returning empty list", "err", err)
		return assets
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}
		path := filepath.Join(tilesDir, entry.Name())
		f, ferr := os.Open(path)
		if ferr != nil {
			slog.Warn("tile library: skip unreadable file", "file", entry.Name(), "err", ferr)
			continue
		}
		cfg, derr := png.DecodeConfig(f)
		_ = f.Close()
		if derr != nil {
			slog.Warn("tile library: skip file that fails to decode as PNG", "file", entry.Name(), "err", derr)
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".png")
		assets = append(assets, TileAsset{ID: id, Width: cfg.Width, Height: cfg.Height})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	return assets
}

// TileImagePath resolves a tile image key to its on-disk path. Returns false
// if the key is unsafe or the file does not exist.
func TileImagePath(key string) (string, bool) {
	if key == "" || strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return "", false
	}
	dir, err := resolveTilesetsDir()
	if err != nil {
		return "", false
	}
	path := filepath.Join(dir, tilesLibrarySubdirName, key)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// DeleteTileImage removes a tile image from the library. existed reports
// whether the file was present before the delete. No map-reference guard:
// tiles are not referenced by maps (the build-tileset flow copies pixels
// into a separate sheet).
func DeleteTileImage(id string) (existed bool, err error) {
	dir, derr := resolveTilesetsDir()
	if derr != nil {
		return false, derr
	}
	safeID := sanitizeMapFilename(id)
	if safeID == "" {
		return false, errTilesetSave("invalid tile id")
	}
	path := filepath.Join(dir, tilesLibrarySubdirName, safeID+".png")
	if _, statErr := os.Stat(path); statErr == nil {
		existed = true
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		return existed, rmErr
	}
	return existed, nil
}
