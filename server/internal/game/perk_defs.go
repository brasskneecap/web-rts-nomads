package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK DEFINITIONS — DATA LAYER
//
// This file owns the PerkDef type and the perk catalog loaded from JSON.
// It is intentionally kept free of runtime game logic so it matches the
// same shape as unit_defs.go and building_defs.go.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  WHERE THINGS LIVE                                                      │
// │                                                                         │
// │    PERK DEFINITIONS (data, tuning, eligibility)                         │
// │      → catalog/perks/<unitType>/<path>/<rank>.json                      │
// │        Each file holds the array of perk entries for that slot.         │
// │        Adding a perk means appending to the right file;                 │
// │        UnitType / Path / Rank are inferred from the file path.          │
// │                                                                         │
// │    PERK RUNTIME BEHAVIOUR (effects, hooks, state)                       │
// │      → perks.go   (assignment + all seven hook functions)               │
// │                                                                         │
// │    PERK ICONS (HUD artwork)                                             │
// │      → catalog/action-icons.json  (id: "perk-<name>")                   │
// └─────────────────────────────────────────────────────────────────────────┘
//
// Eligibility fields (UnitType, Path, Rank) accept "" as a wildcard — a perk
// with an empty Path applies to every path, etc. The assignment system in
// perks.go calls eligiblePerksForUnitAtRank() (via perkPoolForRankLocked) to
// build the pool automatically, so no assignment-side code needs to change
// when new perks are added to the JSON.
// ═════════════════════════════════════════════════════════════════════════════

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed all:catalog/perks
var perkDefsFS embed.FS

// PerkDef is the static definition of a perk loaded from the catalog.
//
// Fields:
//   - ID          — unique string key; used by runtime handlers to dispatch behaviour
//   - DisplayName — human-readable name shown in UI
//   - Description — one-line flavour/tooltip text
//   - UnitType    — eligible unit type, e.g. "soldier". Empty = any.
//   - Path        — eligible promotion path, e.g. "berserker". Empty = any.
//   - Rank        — eligible rank tier, e.g. "bronze". Empty = any.
//   - Config      — perk-specific tuning values. Keys and their meanings are
//                   documented in the JSON file alongside each perk entry.
type PerkDef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	// Icon is the action-icon ID used to render this perk in the HUD.
	// Matches an entry in catalog/action-icons.json ("perk-<name>").
	Icon     string             `json:"icon,omitempty"`
	UnitType string             `json:"unitType,omitempty"`
	Path     string             `json:"path,omitempty"`
	Rank     string             `json:"rank,omitempty"`
	Config   map[string]float64 `json:"config"`
}

// perkDefsByID is the in-memory index populated from the perk catalog at startup.
// The hierarchy on disk is flattened here so all callers work against a
// simple id→def map.
var perkDefsByID map[string]*PerkDef

// perkEntryJSON is the shape of a single perk entry in a per-rank JSON file.
// It carries only the perk-specific fields; UnitType, Path, and Rank are
// injected from the file path during parsing.
type perkEntryJSON struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName"`
	Description string             `json:"description,omitempty"`
	Icon        string             `json:"icon,omitempty"`
	Config      map[string]float64 `json:"config"`
}

func init() {
	// On-disk layout:
	//   catalog/perks/<unitType>/<path>/<rank>.json  →  [perkEntry, perkEntry, ...]
	//
	// unitType, path, and rank are derived from the file path and written
	// into each PerkDef — no redundancy in the source JSON.
	perkDefsByID = make(map[string]*PerkDef)

	err := fs.WalkDir(perkDefsFS, "catalog/perks", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}

		rel := strings.TrimPrefix(p, "catalog/perks/")
		parts := strings.Split(rel, "/")
		if len(parts) != 3 {
			panic("catalog/perks: expected <unit>/<path>/<rank>.json, got " + rel)
		}
		unitType := parts[0]
		pathName := parts[1]
		rank := strings.TrimSuffix(parts[2], path.Ext(parts[2]))

		data, err := perkDefsFS.ReadFile(p)
		if err != nil {
			panic(p + ": " + err.Error())
		}
		var entries []perkEntryJSON
		if err := json.Unmarshal(data, &entries); err != nil {
			panic(p + ": " + err.Error())
		}
		for _, entry := range entries {
			def := &PerkDef{
				ID:          entry.ID,
				DisplayName: entry.DisplayName,
				Description: entry.Description,
				Icon:        entry.Icon,
				UnitType:    unitType,
				Path:        pathName,
				Rank:        rank,
				Config:      entry.Config,
			}
			perkDefsByID[def.ID] = def
		}
		return nil
	})
	if err != nil {
		panic("catalog/perks: " + err.Error())
	}
}

// perkDefByID looks up a perk definition by its ID.
// Returns nil if not found.
func perkDefByID(id string) *PerkDef {
	return perkDefsByID[id]
}

// ─────────────────────────────────────────────────────────────────────────────
// EXTENSION POINT — PERK POOL FILTER
//
// eligiblePerksForUnitAtRank returns every perk in the catalog whose
// eligibility fields match the unit's UnitType, ProgressionPath and the
// given rank. An empty field in the definition matches any value (wildcard).
//
// This is the sole filter used by the assignment pipeline (via
// perkPoolForRankLocked in perks.go). Adding a new perk to the correct
// catalog/perks/<unit>/<path>/<rank>.json file is sufficient to include it
// in the eligible pool — no code changes needed here or in the assignment
// function.
//
// To restrict a perk to multiple paths or ranks, add multiple PerkDef entries
// sharing the same ID — or extend this function with set-based eligibility —
// but keep it as the single place that defines "what qualifies".
// ─────────────────────────────────────────────────────────────────────────────
func eligiblePerksForUnitAtRank(unit *Unit, rank string) []*PerkDef {
	var eligible []*PerkDef
	for _, def := range perkDefsByID {
		if def.UnitType != "" && def.UnitType != unit.UnitType {
			continue
		}
		if def.Path != "" && def.Path != unit.ProgressionPath {
			continue
		}
		if def.Rank != "" && def.Rank != rank {
			continue
		}
		eligible = append(eligible, def)
	}
	return eligible
}

// ListPerkDefs returns all perk definitions sorted by ID.
// Used by the /catalog/perks HTTP endpoint (mirrors ListUnitDefs / ListBuildingDefs).
func ListPerkDefs() []PerkDef {
	defs := make([]PerkDef, 0, len(perkDefsByID))
	for _, def := range perkDefsByID {
		defs = append(defs, *def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
