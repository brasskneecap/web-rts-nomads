package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ─── Writable item catalog overlay ───────────────────────────────────────────
//
// Mirrors the map persistence system (maps.go): editor saves write JSON files
// into a writable dir and register into an in-memory overlay that WINS over
// the embedded catalog in every reader. Loaded once at startup by
// LoadPersistedItemsIntoOverlay; per-file failures are logged skips.

var (
	runtimeItemsMu sync.RWMutex
	runtimeItems   = map[string]*ItemDef{}
)

// itemIDPattern is the editor's id discipline (embed files predate it and are
// exempt — they were validated by their own loaders).
var itemIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// itemIconsSubdirName holds uploaded icon PNGs inside the items dir; it is
// skipped by every def walk (like lists/).
const itemIconsSubdirName = "_icons"

// resolveItemsDir mirrors resolveMapsDir: env override, else the dev source
// catalog dir so editor saves land as ordinary git-visible changes.
func resolveItemsDir() (string, error) {
	if dir := os.Getenv("ITEM_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "items")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("items directory not found at %s; set ITEM_CATALOG_DIR env var to override", dir)
}

// itemProcDisk strips ItemOnHitProc's wire-enrichment MarshalJSON (a defined
// type has no methods), so DISK files keep the authored reference+overrides
// form. Writing the enriched wire form would re-read resolved values as
// frozen overrides — see the items.go MarshalJSON doc comment.
type itemProcDisk ItemOnHitProc

// itemDefDisk shadows the two proc fields with the method-less type; every
// other field marshals identically to ItemDef.
type itemDefDisk struct {
	ItemDef
	Overridden   bool          `json:"overridden,omitempty"` // never persisted (always zero on write path)
	OnHitProc    *itemProcDisk `json:"onHitProc,omitempty"`
	OnStruckProc *itemProcDisk `json:"onStruckProc,omitempty"`
}

// renderItemDefJSON serializes a def in the AUTHORED form for disk.
func renderItemDefJSON(def *ItemDef) ([]byte, error) {
	d := itemDefDisk{ItemDef: *def}
	d.ItemDef.Overridden = false
	if def.OnHitProc != nil {
		p := itemProcDisk(*def.OnHitProc)
		d.OnHitProc = &p
	}
	if def.OnStruckProc != nil {
		p := itemProcDisk(*def.OnStruckProc)
		d.OnStruckProc = &p
	}
	// Zero the embedded copies so the shadow fields are the only emitters.
	d.ItemDef.OnHitProc = nil
	d.ItemDef.OnStruckProc = nil
	return json.MarshalIndent(d, "", "  ")
}

// itemCategorySubdir maps an item's category to its catalog subdirectory,
// matching the embedded layout. Unknown categories go under misc/.
func itemCategorySubdir(def *ItemDef) string {
	switch def.Category {
	case "Weapon":
		return "weapons"
	case "Armor":
		return "armor"
	case "Shield":
		return "shields"
	case "Accessory":
		return "accessories"
	case "Consumable":
		return "consumables"
	default:
		return "misc"
	}
}

// SaveItemDef validates, writes <dir>/<category>/<tier>/<id>.json in authored
// form, and registers the def into the overlay (live without restart).
func SaveItemDef(def *ItemDef) error {
	if !itemIDPattern.MatchString(def.ID) {
		return fmt.Errorf("item id %q must match %s", def.ID, itemIDPattern)
	}
	if err := validateItemDef(def); err != nil {
		return err
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return err
	}
	tier := string(def.Tier)
	if tier == "" {
		tier = string(ItemTierCommon)
	}
	outDir := filepath.Join(dir, itemCategorySubdir(def), tier)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := renderItemDefJSON(def)
	if err != nil {
		return err
	}
	// Remove any previous override saved under a different category/tier so an
	// edited item never exists at two paths.
	removeItemOverrideFiles(dir, def.ID)
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	reg := *def
	reg.Overridden = true
	runtimeItemsMu.Lock()
	runtimeItems[def.ID] = &reg
	runtimeItemsMu.Unlock()
	return nil
}

// maxItemIconBytes caps uploaded icon size (item icons are ~32-64px sprites).
const maxItemIconBytes = 256 * 1024

// SaveItemIcon validates and stores an uploaded PNG for the item, and forces
// the item's iconKey to its id so the client's server-URL fallback resolves
// unambiguously (spec: upload ALWAYS sets iconKey to the item id).
func SaveItemIcon(id string, data []byte) error {
	if !itemIDPattern.MatchString(id) {
		return fmt.Errorf("item id %q must match %s", id, itemIDPattern)
	}
	def, ok := getItemDef(id)
	if !ok {
		return fmt.Errorf("item %q not found", id)
	}
	if len(data) > maxItemIconBytes {
		return fmt.Errorf("icon exceeds %d bytes", maxItemIconBytes)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("icon is not a valid PNG: %w", err)
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return err
	}
	iconDir := filepath.Join(dir, itemIconsSubdirName)
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(iconDir, id+".png"), data, 0o644); err != nil {
		return err
	}
	if def.IconKey != id {
		updated := *def
		updated.IconKey = id
		return SaveItemDef(&updated)
	}
	return nil
}

// ReadItemIcon returns the uploaded PNG for id, if any.
func ReadItemIcon(id string) ([]byte, bool) {
	if !itemIDPattern.MatchString(id) {
		return nil, false // also blocks path traversal
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, itemIconsSubdirName, id+".png"))
	if err != nil {
		return nil, false
	}
	return data, true
}

// ItemIsEmbedded reports whether id ships in the embedded catalog.
func ItemIsEmbedded(id string) bool {
	_, ok := itemCatalogSingleton[id]
	return ok
}

// DeleteItemOverride removes the writable override file(s) + overlay entry.
// For embedded items this is reset-to-default; for editor-created items it is
// a true delete. existed reports whether any override was found.
func DeleteItemOverride(id string) (existed bool, err error) {
	if !itemIDPattern.MatchString(id) {
		return false, nil // never a valid override id; also blocks path traversal
	}
	dir, derr := resolveItemsDir()
	if derr != nil {
		return false, derr
	}
	removed := removeItemOverrideFiles(dir, id)
	iconErr := os.Remove(filepath.Join(dir, itemIconsSubdirName, id+".png"))
	if iconErr == nil {
		removed = true
	} else if !os.IsNotExist(iconErr) {
		return removed, iconErr
	}
	runtimeItemsMu.Lock()
	_, inOverlay := runtimeItems[id]
	delete(runtimeItems, id)
	runtimeItemsMu.Unlock()
	return removed || inOverlay, nil
}

// removeItemOverrideFiles deletes every <id>.json def file under dir (any
// category/tier), skipping lists/ and _icons/. Returns whether any was removed.
func removeItemOverrideFiles(dir, id string) bool {
	removed := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" || d.Name() == itemIconsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == id+".json" {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
		return nil
	})
	return removed
}

// LoadPersistedItemsIntoOverlay loads editor-saved items at startup. Mirrors
// LoadPersistedMapsIntoOverlay: best-effort, never fatal.
func LoadPersistedItemsIntoOverlay() {
	dir, err := resolveItemsDir()
	if err != nil {
		slog.Info("persisted items: no writable items dir; using embedded catalog only", "err", err)
		return
	}
	if n := loadPersistedItemsFromDir(dir); n > 0 {
		slog.Info("persisted items: overlaid on embedded catalog", "count", n, "dir", dir)
	}
}

// loadPersistedItemsFromDir walks dir for item defs (skipping lists/ and
// _icons/), overlaying each valid one. Files identical to their embedded
// counterpart are still overlaid (harmless; dev dir IS the embed source).
func loadPersistedItemsFromDir(dir string) int {
	loaded := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "lists" || d.Name() == itemIconsSubdirName {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		def, perr := parsePersistedItemFile(path)
		if perr != nil {
			slog.Warn("persisted items: skipped file", "file", d.Name(), "err", perr)
			return nil
		}
		runtimeItemsMu.Lock()
		runtimeItems[def.ID] = def
		runtimeItemsMu.Unlock()
		loaded++
		return nil
	})
	return loaded
}

func parsePersistedItemFile(path string) (def *ItemDef, err error) {
	defer func() {
		if r := recover(); r != nil {
			def = nil
			err = fmt.Errorf("invalid item: %v", r)
		}
	}()
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	var d ItemDef
	if uerr := json.Unmarshal(raw, &d); uerr != nil {
		return nil, uerr
	}
	if d.ID == "" {
		return nil, fmt.Errorf("item has empty id")
	}
	if verr := validateItemDef(&d); verr != nil {
		return nil, verr
	}
	d.Overridden = true
	return &d, nil
}
