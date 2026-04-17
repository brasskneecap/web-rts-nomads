package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK DEFINITIONS — DATA LAYER
//
// This file owns the PerkDef type and the perk catalog loaded from JSON.
// It is intentionally kept free of runtime game logic so it matches the
// same shape as unit_defs.go and building_defs.go.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  TO ADD / EDIT / REMOVE A PERK DEFINITION:                             │
// │      edit  catalog/perk-defs.json                                      │
// │                                                                         │
// │  TO ADD A PERK'S RUNTIME BEHAVIOUR:                                    │
// │      edit  perks.go  (the runtime handler file)                        │
// └─────────────────────────────────────────────────────────────────────────┘
//
// Eligibility fields (UnitType, Path, Rank) accept "" as a wildcard — a perk
// with an empty Path applies to every path, etc. The assignment system in
// perks.go uses eligiblePerksForUnit() to build the pool automatically, so no
// assignment-side code needs to change when new perks are added to the JSON.
// ═════════════════════════════════════════════════════════════════════════════

import (
	_ "embed"
	"encoding/json"
	"sort"
)

//go:embed catalog/perk-defs.json
var perkDefsJSON []byte

// PerkDef is the static definition of a perk loaded from catalog/perk-defs.json.
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
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName"`
	Description string             `json:"description,omitempty"`
	// Icon is the action-icon ID used to render this perk in the HUD.
	// Matches an entry in catalog/action-icons.json ("perk-<name>").
	Icon     string             `json:"icon,omitempty"`
	UnitType string             `json:"unitType,omitempty"`
	Path     string             `json:"path,omitempty"`
	Rank     string             `json:"rank,omitempty"`
	Config   map[string]float64 `json:"config"`
}

// perkDefsByID is the in-memory index populated from perk-defs.json at startup.
// The hierarchy is flattened here so all callers work against a simple id→def map.
var perkDefsByID map[string]*PerkDef

// perkEntryJSON is the shape of a single perk entry inside the catalog JSON.
// It carries only the perk-specific fields; UnitType, Path, and Rank are
// injected from the entry's position in the hierarchy during parsing.
type perkEntryJSON struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName"`
	Description string             `json:"description,omitempty"`
	Icon        string             `json:"icon,omitempty"`
	Config      map[string]float64 `json:"config"`
}

func init() {
	// JSON shape:
	//   { "units": { "<unitType>": { "paths": { "<path>": { "<rank>": [...] } } } } }
	//
	// unitType, path, and rank are derived from position in the hierarchy and
	// written into each PerkDef — no redundancy in the source JSON.
	var catalog struct {
		Units map[string]struct {
			Paths map[string]map[string][]perkEntryJSON `json:"paths"`
		} `json:"units"`
	}
	if err := json.Unmarshal(perkDefsJSON, &catalog); err != nil {
		panic("perk-defs.json: " + err.Error())
	}

	perkDefsByID = make(map[string]*PerkDef)
	for unitType, unitBlock := range catalog.Units {
		for path, rankMap := range unitBlock.Paths {
			for rank, entries := range rankMap {
				for _, entry := range entries {
					def := &PerkDef{
						ID:          entry.ID,
						DisplayName: entry.DisplayName,
						Description: entry.Description,
						Icon:        entry.Icon,
						UnitType:    unitType,
						Path:        path,
						Rank:        rank,
						Config:      entry.Config,
					}
					perkDefsByID[def.ID] = def
				}
			}
		}
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
// eligiblePerksForUnit returns every perk in the catalog whose eligibility
// fields match the unit's current UnitType, ProgressionPath, and Rank.
// An empty field in the definition matches any value (wildcard).
//
// This is the sole filter used by assignUnitPerkLocked. Adding a new perk to
// perk-defs.json with the correct UnitType/Path/Rank is sufficient to include
// it in the eligible pool — no code changes needed here or in the assignment
// function.
//
// To restrict a perk to multiple paths or ranks you would add multiple PerkDef
// entries sharing the same ID prefix, or extend this function with set-based
// eligibility — but keep it as the single place that defines "what qualifies".
// ─────────────────────────────────────────────────────────────────────────────
func eligiblePerksForUnit(unit *Unit) []*PerkDef {
	var eligible []*PerkDef
	for _, def := range perkDefsByID {
		if def.UnitType != "" && def.UnitType != unit.UnitType {
			continue
		}
		if def.Path != "" && def.Path != unit.ProgressionPath {
			continue
		}
		if def.Rank != "" && def.Rank != unit.Rank {
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
