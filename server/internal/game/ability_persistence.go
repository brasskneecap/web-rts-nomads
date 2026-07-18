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

var abilityIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// abilityIconsSubdirName holds uploaded ability icons; skipped by the def walk.
const abilityIconsSubdirName = "_icons"

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

// writeAbilityDefFile writes def to <dir>/<id>/<id>.json, creating the
// directory as needed. Shared by SaveAbilityDef and ResetAbilityDef's
// restore-on-reset path (see ResetAbilityDef's doc comment for why a reset
// must rewrite the file rather than simply deleting it).
func writeAbilityDefFile(dir string, def *AbilityDef) error {
	outDir := filepath.Join(dir, def.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644)
}

// ─── Undo: the state each ability was in before its most recent save ────────
//
// abilityUndo holds one level of history per ability: the def as it stood
// BEFORE the last SaveAbilityDef. The editor's Reset restores this, so
// "reset" means "undo my last save" rather than "throw away everything back
// to the shipped default" — which is what an author actually wants after a
// bad edit. Mirrors itemUndo (item_persistence.go) exactly; see that
// comment for the full rationale, not repeated here.
//
// In memory, deliberately: an undo step is scoped to the editing session.
// When there is no undo step (fresh server, or a second Reset in a row),
// Reset falls back to the shipped catalog default.
var (
	abilityUndoMu sync.RWMutex
	abilityUndo   = map[string]*AbilityDef{}
)

// snapshotAbilityForUndo records the ability's current def as its undo step.
// Called before a save overwrites it. A def that does not exist yet (a
// brand-new ability) records nothing — there is no prior state to go back to.
func snapshotAbilityForUndo(id string) {
	prior, ok := getAbilityDef(id)
	if !ok {
		return
	}
	abilityUndoMu.Lock()
	abilityUndo[id] = &prior
	abilityUndoMu.Unlock()
}

// takeAbilityUndo pops the ability's undo step, if any. One level: after it
// is used, a further Reset falls back to the shipped default.
func takeAbilityUndo(id string) (*AbilityDef, bool) {
	abilityUndoMu.Lock()
	defer abilityUndoMu.Unlock()
	def, ok := abilityUndo[id]
	if ok {
		delete(abilityUndo, id)
	}
	return def, ok
}

// SaveAbilityDef validates and writes an authored ability def to
// <dir>/<id>/<id>.json, then registers it in the overlay. The ability's prior
// state is recorded as an undo step first (see abilityUndo).
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
	snapshotAbilityForUndo(def.ID)
	if err := writeAbilityDefFile(dir, def); err != nil {
		return err
	}
	runtimeAbilitiesMu.Lock()
	runtimeAbilities[def.ID] = *def
	runtimeAbilitiesMu.Unlock()
	return nil
}

// maxAbilityIconBytes caps uploaded icon size (ability icons are small sprites).
const maxAbilityIconBytes = 256 * 1024

// SaveAbilityIcon validates and stores an uploaded PNG for the ability, and
// forces the ability's Icon key to its id so the client's server-URL fallback
// resolves unambiguously.
func SaveAbilityIcon(id string, data []byte) error {
	if !abilityIDPattern.MatchString(id) {
		return fmt.Errorf("ability id %q must match %s", id, abilityIDPattern)
	}
	def, ok := getAbilityDef(id)
	if !ok {
		return fmt.Errorf("ability %q not found", id)
	}
	if len(data) > maxAbilityIconBytes {
		return fmt.Errorf("icon exceeds %d bytes", maxAbilityIconBytes)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("icon is not a valid PNG: %w", err)
	}
	dir, err := resolveAbilitiesDir()
	if err != nil {
		return err
	}
	iconDir := filepath.Join(dir, abilityIconsSubdirName)
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(iconDir, id+".png"), data, 0o644); err != nil {
		return err
	}
	if def.Icon != id {
		updated := def
		updated.Icon = id
		return SaveAbilityDef(&updated)
	}
	return nil
}

// ReadAbilityIcon returns the uploaded PNG for id, if any.
func ReadAbilityIcon(id string) ([]byte, bool) {
	if !abilityIDPattern.MatchString(id) {
		return nil, false // also blocks path traversal
	}
	dir, err := resolveAbilitiesDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, abilityIconsSubdirName, id+".png"))
	if err != nil {
		return nil, false
	}
	return data, true
}

// AbilityIsEmbedded reports whether an ability id ships in the embedded catalog.
func AbilityIsEmbedded(id string) bool {
	_, ok := abilityDefsByID[id]
	return ok
}

// DeleteAbilityOverride removes an author-created ability: its def file, its
// uploaded icon and its overlay entry. Resetting a SHIPPED ability is a
// different operation — see ResetAbilityDef, which restores rather than
// deletes. As a safety net (this function is not meant to be called directly
// on an embedded id — callers should route those through ResetAbilityDef —
// but defends against it anyway, mirroring DeleteItemOverride): if id turns
// out to be embedded, the shipped def file is rewritten immediately after
// removal so the embed's on-disk source is never actually lost. existed
// reports whether anything was found to remove.
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
	// Remove the uploaded icon too, if any.
	_ = os.Remove(filepath.Join(dir, abilityIconsSubdirName, id+".png"))
	runtimeAbilitiesMu.Lock()
	_, inOverlay := runtimeAbilities[id]
	delete(runtimeAbilities, id)
	runtimeAbilitiesMu.Unlock()
	abilityUndoMu.Lock()
	delete(abilityUndo, id)
	abilityUndoMu.Unlock()

	// A shipped ability must never lose its def file — restore the default
	// even if this was called on one by mistake (see doc comment above).
	if embedded, isEmbedded := abilityDefsByID[id]; isEmbedded && removed {
		if werr := writeAbilityDefFile(dir, &embedded); werr != nil {
			return true, werr
		}
	}
	return removed || inOverlay, nil
}

// ResetAbilityDef undoes the ability's last save: it restores the def to the
// state it was in before that save and returns "undo". With no undo step
// recorded (a fresh server process, or a second reset in a row) it falls back
// to the shipped catalog default and returns "default".
//
// It always leaves a def file on disk, for the same reason ResetItemDef does
// (see that function's doc comment): resolveAbilitiesDir returns the SAME
// directory that `//go:embed all:catalog/abilities` reads from in a dev
// tree, so simply deleting the override file — rather than rewriting it with
// the restored def — would remove the ability from the NEXT build, even
// though the running process would keep serving it from the copy already
// embedded in the binary and look fine in the meantime.
//
// ok is false when the id names no ability at all.
func ResetAbilityDef(id string) (mode string, ok bool, err error) {
	if !abilityIDPattern.MatchString(id) {
		return "", false, nil // never a valid id; also blocks path traversal
	}
	dir, derr := resolveAbilitiesDir()
	if derr != nil {
		return "", false, derr
	}

	restore, mode := func() (*AbilityDef, string) {
		if undo, has := takeAbilityUndo(id); has {
			return undo, "undo"
		}
		if embedded, isEmbedded := abilityDefsByID[id]; isEmbedded {
			return &embedded, "default"
		}
		return nil, ""
	}()
	if restore == nil {
		// An author-created ability with no undo step has no earlier state to
		// go back to; deleting it is the caller's job (DeleteAbilityOverride).
		return "", false, nil
	}

	if rerr := os.Remove(filepath.Join(dir, id, id+".json")); rerr != nil && !os.IsNotExist(rerr) {
		return "", false, rerr
	}
	if werr := writeAbilityDefFile(dir, restore); werr != nil {
		return "", false, werr
	}

	// Re-register so the restored def is live without a restart. An ability
	// that matches the embedded default is dropped from the overlay entirely
	// rather than re-registered as an "override" of itself.
	runtimeAbilitiesMu.Lock()
	if _, isEmbedded := abilityDefsByID[id]; isEmbedded && mode == "default" {
		delete(runtimeAbilities, id)
	} else {
		runtimeAbilities[id] = *restore
	}
	runtimeAbilitiesMu.Unlock()
	return mode, true, nil
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
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == abilityIconsSubdirName {
			return filepath.SkipDir
		}
		if d.IsDir() {
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
