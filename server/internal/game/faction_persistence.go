package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	runtimeFactionsMu sync.RWMutex
	runtimeFactions   = map[string]FactionDef{}
)

// errFactionHasUnits marks the "faction still owns units" rejection so
// DeleteEditorFaction can classify it as a 400 (a state the author can fix)
// rather than a 500. The message is authored in full at the raise site — do
// not wrap the sentinel with %w, or fmt.Errorf appends the sentinel's own
// text to the user-visible string. Raise via factionHasUnitsError{...} so
// errors.Is(err, errFactionHasUnits) keeps working while Error() returns only
// the authored message.
var errFactionHasUnits = errors.New("faction still has units")

// factionHasUnitsError carries the full, user-facing message while still
// satisfying errors.Is(err, errFactionHasUnits) via Is/Unwrap. This is the
// only error type raised for the "still has units" condition.
type factionHasUnitsError struct{ msg string }

func (e factionHasUnitsError) Error() string        { return e.msg }
func (e factionHasUnitsError) Is(target error) bool { return target == errFactionHasUnits }
func (e factionHasUnitsError) Unwrap() error        { return errFactionHasUnits }

// SaveFactionDef validates and writes <dir>/<id>/faction.json, then registers
// the record in the overlay. MkdirAll is what lets a faction exist before it
// owns any units — the directory is the faction.
func SaveFactionDef(def *FactionDef) error {
	if !unitIDPattern.MatchString(def.ID) {
		return fmt.Errorf("faction id %q must match %s", def.ID, unitIDPattern)
	}
	normalized := normalizeFactionDef(def.ID, *def)
	dir, err := resolveUnitsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, normalized.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, factionMetaFileName), raw, 0o644); err != nil {
		// outDir was just created by MkdirAll above and the write into it failed,
		// so it is still empty — clean it up. Otherwise an inert empty directory
		// is left in the source tree and the next build embeds it, turning a
		// failed save into a real faction with a synthesized record.
		_ = os.Remove(outDir)
		return err
	}
	runtimeFactionsMu.Lock()
	runtimeFactions[normalized.ID] = normalized
	runtimeFactionsMu.Unlock()
	return nil
}

// FactionUnitTypes returns the unit types currently claiming a faction, sorted.
// Empty ⇒ the faction is safe to delete.
func FactionUnitTypes(id string) []string {
	var types []string
	for _, def := range ListUnitDefs() {
		if def.Faction == id {
			types = append(types, def.Type)
		}
	}
	sort.Strings(types)
	return types
}

// DeleteFactionOverride removes a faction's record, and its directory if that
// leaves it empty.
//
// It refuses while any unit still claims the faction. Two reasons: those units
// would vanish from every faction filter, and in the dev tree the writable dir
// IS the source tree — removing a populated faction directory would delete real
// catalog content. The directory is only ever removed when empty, never
// recursively.
func DeleteFactionOverride(id string) (existed bool, err error) {
	if !unitIDPattern.MatchString(id) {
		return false, nil // never a valid faction id; also blocks path traversal
	}
	if owned := FactionUnitTypes(id); len(owned) > 0 {
		return false, factionHasUnitsError{msg: fmt.Sprintf(
			"faction %q still has %d unit(s): %s — move or delete them before deleting the faction",
			id, len(owned), strings.Join(owned, ", "))}
	}
	dir, derr := resolveUnitsDir()
	if derr != nil {
		return false, derr
	}
	factionDir := filepath.Join(dir, id)
	removed := false
	if rerr := os.Remove(filepath.Join(factionDir, factionMetaFileName)); rerr == nil {
		removed = true
	} else if !errors.Is(rerr, fs.ErrNotExist) {
		return false, rerr
	}
	if entries, rerr := os.ReadDir(factionDir); rerr == nil && len(entries) == 0 {
		_ = os.Remove(factionDir)
	}
	runtimeFactionsMu.Lock()
	_, inOverlay := runtimeFactions[id]
	delete(runtimeFactions, id)
	runtimeFactionsMu.Unlock()
	return removed || inOverlay, nil
}

// FactionIsEmbedded reports whether a faction directory exists in the embed.
func FactionIsEmbedded(id string) bool {
	_, ok := embeddedFactions[id]
	return ok
}

// LoadPersistedFactionsIntoOverlay overlays writable faction records onto the
// embed at startup.
func LoadPersistedFactionsIntoOverlay() {
	dir, err := resolveUnitsDir()
	if err != nil {
		slog.Info("persisted factions: no writable units dir; using embedded factions only", "err", err)
		return
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		slog.Warn("persisted factions: could not read units dir; using embedded factions only", "dir", dir, "err", rerr)
		return
	}
	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// A directory name SaveFactionDef could never produce (e.g. mixed case,
		// hyphens) would register a faction that DeleteFactionOverride can never
		// delete (its id-pattern check returns false, nil → permanent 404) and
		// that would show in the editor forever. Skip it.
		if !unitIDPattern.MatchString(entry.Name()) {
			continue
		}
		raw, ferr := os.ReadFile(filepath.Join(dir, entry.Name(), factionMetaFileName))
		if ferr != nil {
			if !errors.Is(ferr, fs.ErrNotExist) {
				slog.Warn("persisted factions: could not read record", "faction", entry.Name(), "err", ferr)
			}
			continue // a faction directory with no record is still a valid faction
		}
		var def FactionDef
		if uerr := json.Unmarshal(raw, &def); uerr != nil {
			slog.Warn("persisted factions: skipped record", "faction", entry.Name(), "err", uerr)
			continue
		}
		normalized := normalizeFactionDef(entry.Name(), def)
		runtimeFactionsMu.Lock()
		runtimeFactions[normalized.ID] = normalized
		runtimeFactionsMu.Unlock()
		loaded++
	}
	if loaded > 0 {
		slog.Info("persisted factions: overlaid on embedded catalog", "count", loaded, "dir", dir)
	}
}
