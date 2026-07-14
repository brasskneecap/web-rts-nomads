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

// itemProcDisk strips ItemProc's wire-enrichment MarshalJSON (a defined type
// has no methods), so DISK files keep the authored reference+overrides form.
// Writing the enriched wire form would re-read resolved values as frozen
// overrides — see the items.go MarshalJSON doc comment.
type itemProcDisk ItemProc

// itemDefDisk shadows the proc list with the method-less element type; every
// other field marshals identically to ItemDef. Marshal-only: reading a def
// back goes through ItemDef's own UnmarshalJSON (which also folds the legacy
// singular proc keys), so this type must never be a decode target.
type itemDefDisk struct {
	ItemDef
	Overridden bool           `json:"overridden,omitempty"` // never persisted (always zero on write path)
	Procs      []itemProcDisk `json:"procs,omitempty"`
}

// renderItemDefJSON serializes a def in the AUTHORED form for disk.
func renderItemDefJSON(def *ItemDef) ([]byte, error) {
	d := itemDefDisk{ItemDef: *def}
	// Runtime-only provenance flags never reach disk.
	d.ItemDef.Overridden = false
	d.ItemDef.Custom = false
	d.ItemDef.IconUploadedAt = 0
	for _, p := range def.Procs {
		d.Procs = append(d.Procs, itemProcDisk(p))
	}
	// Zero the embedded copy so the shadow field is the only emitter.
	d.ItemDef.Procs = nil
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

// writeItemDefFile writes def to <dir>/<category>/<tier>/<id>.json in AUTHORED
// form, creating the directory as needed. Shared by SaveItemDef and the
// restore-on-reset path in DeleteItemOverride.
func writeItemDefFile(dir string, def *ItemDef) error {
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
	// Trailing newline: these files are committed, and a missing one makes every
	// editor save show up as a "\ No newline at end of file" diff.
	return os.WriteFile(filepath.Join(outDir, def.ID+".json"), append(raw, '\n'), 0o644)
}

// ─── Undo: the state each item was in before its most recent save ───────────
//
// itemUndo holds one level of history per item: the def as it stood BEFORE the
// last SaveItemDef. The editor's Reset restores this, so "reset" means "undo my
// last save" rather than "throw away everything back to the shipped default" —
// which is what an author actually wants after a bad edit.
//
// In memory, deliberately: an undo step is scoped to the editing session, and
// persisting it would mean writing shadow copies of every item into a directory
// that is also the embed source. When there is no undo step (fresh server, or a
// second Reset in a row), Reset falls back to the shipped catalog default.
var (
	itemUndoMu sync.RWMutex
	itemUndo   = map[string]*ItemDef{}
)

// snapshotItemForUndoLocked records the item's current def as its undo step.
// Called before a save overwrites it. A def that does not exist yet (a brand-new
// item) records nothing — there is no prior state to go back to.
func snapshotItemForUndo(id string) {
	prior, ok := getItemDef(id)
	if !ok {
		return
	}
	snap := *prior
	// Runtime-only provenance is re-derived on read; never carry it into a def
	// we might write back to disk.
	snap.Overridden = false
	snap.Custom = false
	snap.IconUploadedAt = 0
	itemUndoMu.Lock()
	itemUndo[id] = &snap
	itemUndoMu.Unlock()
}

// takeItemUndo pops the item's undo step, if any. One level: after it is used,
// a further Reset falls back to the shipped default.
func takeItemUndo(id string) (*ItemDef, bool) {
	itemUndoMu.Lock()
	defer itemUndoMu.Unlock()
	def, ok := itemUndo[id]
	if ok {
		delete(itemUndo, id)
	}
	return def, ok
}

// SaveItemDef validates, writes <dir>/<category>/<tier>/<id>.json in authored
// form, and registers the def into the overlay (live without restart). The
// item's prior state is recorded as an undo step first (see itemUndo).
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
	snapshotItemForUndo(def.ID)
	// Remove any previous override saved under a different category/tier so an
	// edited item never exists at two paths.
	removeItemOverrideFiles(dir, def.ID)
	if err := writeItemDefFile(dir, def); err != nil {
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

// ItemIconUploadedAt returns the mtime (unix seconds) of the uploaded icon for
// id, or 0 when the author has not uploaded one. Two jobs: it tells the client
// that an uploaded icon EXISTS (so it can prefer it over the bundled art it
// ships with — otherwise a shipped item could never have its icon replaced),
// and it versions the URL so the browser refetches after a re-upload instead of
// serving the cached one.
func ItemIconUploadedAt(id string) int64 {
	if !itemIDPattern.MatchString(id) {
		return 0 // also blocks path traversal
	}
	dir, err := resolveItemsDir()
	if err != nil {
		return 0
	}
	info, err := os.Stat(filepath.Join(dir, itemIconsSubdirName, id+".png"))
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}

// ItemIsEmbedded reports whether id ships in the embedded catalog.
func ItemIsEmbedded(id string) bool {
	_, ok := itemCatalogSingleton[id]
	return ok
}

// ResetItemDef undoes the item's last save: it restores the def to the state it
// was in before that save and returns "undo". With no undo step recorded (a
// fresh server process, or a second reset in a row) it falls back to the shipped
// catalog default and returns "default".
//
// It always leaves a def file on disk. That is not cosmetic: in a dev tree the
// writable dir IS the embed source (resolveItemsDir), so deleting the file
// destroys the item — the running process keeps serving it from the copy already
// embedded in the binary and looks fine, but the next build embeds from disk and
// the item is gone, taking down startup ("item list ... is not a known item").
//
// ok is false when the id names no item at all.
func ResetItemDef(id string) (mode string, ok bool, err error) {
	if !itemIDPattern.MatchString(id) {
		return "", false, nil // never a valid id; also blocks path traversal
	}
	dir, derr := resolveItemsDir()
	if derr != nil {
		return "", false, derr
	}

	restore, mode := func() (*ItemDef, string) {
		if undo, has := takeItemUndo(id); has {
			return undo, "undo"
		}
		if embedded, isEmbedded := itemCatalogSingleton[id]; isEmbedded {
			return embedded, "default"
		}
		return nil, ""
	}()
	if restore == nil {
		// An author-created item with no undo step has no earlier state to go
		// back to; deleting it is the caller's job (DeleteItemOverride).
		return "", false, nil
	}

	removeItemOverrideFiles(dir, id)
	if werr := writeItemDefFile(dir, restore); werr != nil {
		return "", false, werr
	}

	// Re-register so the restored def is live without a restart. An item that
	// matches the embedded default is dropped from the overlay entirely rather
	// than re-registered as an "override" of itself.
	runtimeItemsMu.Lock()
	if _, isEmbedded := itemCatalogSingleton[id]; isEmbedded && mode == "default" {
		delete(runtimeItems, id)
	} else {
		reg := *restore
		reg.Overridden = true
		runtimeItems[id] = &reg
	}
	runtimeItemsMu.Unlock()
	return mode, true, nil
}

// DeleteItemOverride removes an author-created item: its def file(s), its
// uploaded icon and its overlay entry. Resetting a SHIPPED item is a different
// operation — see ResetItemDef, which restores rather than deletes (deleting a
// shipped def in a dev tree destroys it, see above). existed reports whether
// anything was found to remove.
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
	itemUndoMu.Lock()
	delete(itemUndo, id)
	itemUndoMu.Unlock()

	// A shipped item must never lose its def file — restore the default even if
	// this was called on one by mistake.
	if embedded, isEmbedded := itemCatalogSingleton[id]; isEmbedded && removed {
		if werr := writeItemDefFile(dir, embedded); werr != nil {
			return true, werr
		}
	}
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
