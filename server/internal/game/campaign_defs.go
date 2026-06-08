package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/campaigns/*.json
var campaignDefsFS embed.FS

// CampaignDef is the static definition of a campaign loaded from
// catalog/campaigns/<id>.json. Campaigns are ordered chains of levels that
// reference maps in the map catalog; per-player completion lives on the
// PlayerProfile (CompletedCampaignLevels), not on this struct.
//
// To add a campaign: drop a new <id>.json under catalog/campaigns/. The
// server picks it up at next startup and serves it from
// GET /api/catalog/campaigns. No code changes required for the data path —
// only when introducing a new field on the struct.
type CampaignDef struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName"`
	Description string             `json:"description,omitempty"`
	// SortOrder controls tab order in the UI. Ties broken by ID.
	SortOrder int `json:"sortOrder"`
	// Locked campaigns appear in the tab strip but are not selectable. Used
	// to advertise upcoming content (e.g. Swamp before its levels ship).
	Locked bool               `json:"locked,omitempty"`
	Levels []CampaignLevelDef `json:"levels"`
}

// CampaignLevelDef is one ordered step in a campaign. `MapID` must reference
// a map in the map catalog. `PrerequisiteLevelID` is nil for the first level
// in a chain; for subsequent levels it must point at a level in the SAME
// campaign (cross-campaign prerequisites are intentionally unsupported —
// when they're needed, replace the field with a richer typed union).
type CampaignLevelDef struct {
	ID                  string  `json:"id"`
	DisplayName         string  `json:"displayName"`
	MapID               string  `json:"mapId"`
	PrerequisiteLevelID *string `json:"prerequisiteLevelId"`
	Description         string  `json:"description,omitempty"`

	// Objectives is the per-level list of match-time objectives. Each entry
	// dispatches through the objective handler registry (objective_defs.go)
	// at catalog load time, which validates the config and stores a typed
	// parsed value on the def. May be empty or omitted — a level with no
	// objectives wins on the legacy wave/townhall rule alone.
	Objectives []ObjectiveDef `json:"objectives,omitempty"`
}

// campaignDefsByID is the package-level catalog, loaded once at startup.
// Never mutated after initialization.
//
// The wrapper exists for one reason: Go initializes package-level vars
// before init() functions, and across files in lexical order when no
// dependency is detected. `campaign_defs.go` sorts before `objective_*.go`
// alphabetically, so a naive `var campaignDefsByID = loadCampaignDefs()`
// would run before any objective handler is registered, causing every
// objective in `forest.json` to be rejected as an "unknown type."
//
// The `_ = allObjectiveHandlersRegistered` reference inside the wrapper
// makes Go's dependency analysis see a real reference to a symbol defined
// in `objective_handlers.go`, ordering this var's initialization AFTER the
// handler registration sentinel. Cheap, explicit, no sync.Once.
var campaignDefsByID = loadCampaignDefsAfterHandlersRegister()

func loadCampaignDefsAfterHandlersRegister() map[string]CampaignDef {
	_ = allObjectiveHandlersRegistered
	return loadCampaignDefs()
}

func loadCampaignDefs() map[string]CampaignDef {
	entries, err := fs.ReadDir(campaignDefsFS, "catalog/campaigns")
	if err != nil {
		panic("catalog/campaigns: " + err.Error())
	}
	result := make(map[string]CampaignDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		data, readErr := campaignDefsFS.ReadFile("catalog/campaigns/" + filename)
		if readErr != nil {
			panic("catalog/campaigns/" + filename + ": " + readErr.Error())
		}
		var def CampaignDef
		if jsonErr := json.Unmarshal(data, &def); jsonErr != nil {
			panic("catalog/campaigns/" + filename + ": " + jsonErr.Error())
		}
		def = validateCampaignDef(filename, def)
		if _, dup := result[def.ID]; dup {
			panic(`catalog/campaigns/` + filename + `: duplicate id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// validateCampaignDef normalises and validates one CampaignDef in place,
// returning the populated def. Exported-to-tests via the package-internal
// surface so unit tests can drive every panic path without having to round
// trip through the embed FS.
//
// Panics on: missing id/displayName, duplicate level id, missing level
// mapId, unknown prerequisite level id, self-referencing prerequisite,
// duplicate objective id within a level. The objective registry's own
// validators (objective_defs.go) panic on per-objective shape errors.
func validateCampaignDef(filename string, def CampaignDef) CampaignDef {
	if def.ID == "" {
		panic("catalog/campaigns/" + filename + `: missing "id"`)
	}
	if def.DisplayName == "" {
		panic("catalog/campaigns/" + filename + `: missing "displayName"`)
	}
	// Normalize a nil Levels slice to an empty slice so the JSON
	// serialization writes `[]` rather than `null`.
	if def.Levels == nil {
		def.Levels = []CampaignLevelDef{}
	}

	// First pass: per-level structural checks + objective validation.
	levelIDs := make(map[string]bool, len(def.Levels))
	for i := range def.Levels {
		lvl := &def.Levels[i]
		if lvl.ID == "" {
			panic("catalog/campaigns/" + filename + `: levels[` + itoa(i) + `] missing "id"`)
		}
		if levelIDs[lvl.ID] {
			panic("catalog/campaigns/" + filename + `: duplicate level id "` + lvl.ID + `"`)
		}
		if lvl.MapID == "" {
			panic("catalog/campaigns/" + filename + `: levels[` + itoa(i) + `] ("` + lvl.ID + `") missing "mapId"`)
		}
		levelIDs[lvl.ID] = true

		// Normalize a nil Objectives slice to empty so JSON serialisation
		// writes `[]` rather than `null` and downstream code can range
		// safely. Then pipe each entry through the registry validator,
		// which panics with file + level + objective id on bad data.
		if lvl.Objectives == nil {
			lvl.Objectives = []ObjectiveDef{}
		}
		objectiveIDs := make(map[string]bool, len(lvl.Objectives))
		for j := range lvl.Objectives {
			if lvl.Objectives[j].ID != "" && objectiveIDs[lvl.Objectives[j].ID] {
				panic("catalog/campaigns/" + filename + `: level "` + lvl.ID +
					`": duplicate objective id "` + lvl.Objectives[j].ID + `"`)
			}
			// parseAndValidateObjectiveDef populates the parsed config in
			// place; write the validated copy back to the slice so the
			// stored def carries the typed config.
			lvl.Objectives[j] = parseAndValidateObjectiveDef(filename, lvl.ID, lvl.Objectives[j])
			objectiveIDs[lvl.Objectives[j].ID] = true
		}
	}

	// Second pass: prerequisite resolution (needs the full set of level IDs).
	for _, lvl := range def.Levels {
		if lvl.PrerequisiteLevelID == nil {
			continue
		}
		if !levelIDs[*lvl.PrerequisiteLevelID] {
			panic("catalog/campaigns/" + filename + `: level "` + lvl.ID +
				`" references unknown prerequisite "` + *lvl.PrerequisiteLevelID + `"`)
		}
		if *lvl.PrerequisiteLevelID == lvl.ID {
			panic("catalog/campaigns/" + filename + `: level "` + lvl.ID +
				`" lists itself as a prerequisite`)
		}
	}

	return def
}

// ListCampaignDefs returns all registered campaign definitions sorted by
// SortOrder (ascending), then ID. Stable across runs because the underlying
// data is read-only after startup.
func ListCampaignDefs() []CampaignDef {
	defs := make([]CampaignDef, 0, len(campaignDefsByID))
	for _, d := range campaignDefsByID {
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].SortOrder != defs[j].SortOrder {
			return defs[i].SortOrder < defs[j].SortOrder
		}
		return defs[i].ID < defs[j].ID
	})
	return defs
}

// GetCampaignDef returns the CampaignDef for id and whether it was found.
func GetCampaignDef(id string) (CampaignDef, bool) {
	def, ok := campaignDefsByID[id]
	return def, ok
}
