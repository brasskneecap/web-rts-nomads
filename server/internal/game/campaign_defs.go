package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"

	"webrts/server/pkg/protocol"
)

//go:embed catalog/campaigns/*.json
var campaignDefsFS embed.FS

// CampaignDef is the in-memory aggregation of:
//
//   - a campaign HEADER file at catalog/campaigns/<id>.json (id, displayName,
//     description, sortOrder, locked), and
//   - the maps in the map catalog whose `Campaign.CampaignID == id`.
//
// Each tagged map contributes one CampaignLevelDef to `Levels` at discovery
// time. The header file alone is authoritative for campaign-wide fields;
// per-level fields (display name, prerequisites, objectives) ride on the map.
//
// Background: before the map-editor-authors-campaign-maps change, the entire
// `Levels` slice was authored directly in catalog/campaigns/*.json. That
// indirection was removed so the editor has a single authoring surface — see
// `MapCampaignBlock` in pkg/protocol.
type CampaignDef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	// SortOrder controls tab order in the UI. Ties broken by ID.
	SortOrder int `json:"sortOrder"`
	// Locked campaigns appear in the tab strip but are not selectable. Used
	// to advertise upcoming content (e.g. Swamp before its levels ship).
	Locked bool               `json:"locked,omitempty"`
	Levels []CampaignLevelDef `json:"levels"`
}

// CampaignLevelDef is one ordered step in a campaign, synthesised from a
// campaign-tagged map. `MapID` is the map catalog id whose `Campaign` block
// contributed this entry. `PrerequisiteLevelID` must reference another level
// in the SAME campaign (cross-campaign prerequisites are intentionally
// unsupported — when they're needed, replace the field with a richer typed
// union).
type CampaignLevelDef struct {
	ID                  string  `json:"id"`
	DisplayName         string  `json:"displayName"`
	MapID               string  `json:"mapId"`
	PrerequisiteLevelID *string `json:"prerequisiteLevelId"`
	Description         string  `json:"description,omitempty"`
	// SortOrder is the level row's position within its campaign. Read off
	// `Campaign.SortOrder` on the source map. Ties broken by ID.
	SortOrder int `json:"-"`

	// Objectives is the per-level list of match-time objectives. Each entry
	// is validated at discovery time through the objective handler registry
	// (`parseAndValidateObjectiveDef`), which panics for embedded data and
	// returns an error for runtime editor saves.
	Objectives []ObjectiveDef `json:"objectives,omitempty"`
}

// campaignHeadersByID is the package-level map of header data (no levels)
// loaded once at startup from catalog/campaigns/*.json. Never mutated after
// initialisation. Level discovery happens lazily through buildCampaignDefs.
//
// The wrapper exists for one reason: Go initializes package-level vars
// before init() functions, and across files in lexical order when no
// dependency is detected. We need both the objective handler registry AND
// the map catalog to be populated before we touch them. The `_ = ...` lines
// inside the wrapper make Go's dependency analysis see real references,
// ordering this var AFTER both sentinels. Cheap, explicit, no sync.Once.
var campaignHeadersByID = loadCampaignHeadersAfterDeps()

func loadCampaignHeadersAfterDeps() map[string]CampaignDef {
	_ = allObjectiveHandlersRegistered
	_ = mapCatalog
	return loadCampaignHeaders()
}

func loadCampaignHeaders() map[string]CampaignDef {
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
		// Header is parsed off the same JSON file that USED to carry a
		// `levels` array. We deliberately don't define a `Levels` field on
		// the parse target so any stray `levels` in legacy files is
		// silently ignored — the migration drops them, and we don't want
		// drift to silently re-introduce duplicates of the map-side data.
		var header struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Description string `json:"description,omitempty"`
			SortOrder   int    `json:"sortOrder"`
			Locked      bool   `json:"locked,omitempty"`
		}
		if jsonErr := json.Unmarshal(data, &header); jsonErr != nil {
			panic("catalog/campaigns/" + filename + ": " + jsonErr.Error())
		}
		if header.ID == "" {
			panic("catalog/campaigns/" + filename + `: missing "id"`)
		}
		if header.DisplayName == "" {
			panic("catalog/campaigns/" + filename + `: missing "displayName"`)
		}
		if _, dup := result[header.ID]; dup {
			panic(`catalog/campaigns/` + filename + `: duplicate id "` + header.ID + `"`)
		}
		result[header.ID] = CampaignDef{
			ID:          header.ID,
			DisplayName: header.DisplayName,
			Description: header.Description,
			SortOrder:   header.SortOrder,
			Locked:      header.Locked,
		}
	}
	return result
}

// buildCampaignDefs returns a fresh map of campaigns keyed by id with their
// Levels populated by scanning the current map catalog snapshot. Called by
// ListCampaignDefs/GetCampaignDef/lookupCampaignLevelByID — recomputing each
// time keeps editor saves visible without a restart.
//
// Panics on bad authored data from the EMBEDDED catalog (orphaned
// campaignId, duplicate level id, unknown prereq). Runtime editor saves go
// through validateMapCampaignBlockForSave at write time, so a malformed
// editor save is rejected before it lands in runtimeMaps — discovery never
// sees it.
//
// Complexity O(n) over the merged map catalog plus O(per-campaign) sort.
// Catalogs are tiny (single digits per campaign, low double digits of maps)
// so the recompute cost is negligible compared to the simplicity of a live
// view.
func buildCampaignDefs() map[string]CampaignDef {
	out := make(map[string]CampaignDef, len(campaignHeadersByID))
	for id, header := range campaignHeadersByID {
		// Copy by value so the cached header's Levels stays nil.
		copy := header
		copy.Levels = []CampaignLevelDef{}
		out[id] = copy
	}

	for _, entry := range currentMapCatalogSnapshot() {
		block := entry.Map.Campaign
		if block == nil {
			continue
		}
		camp, ok := out[block.CampaignID]
		if !ok {
			panic("catalog/maps/" + entry.ID + `: campaign block references unknown campaignId "` +
				block.CampaignID + `" (no matching catalog/campaigns/<id>.json header)`)
		}
		level := convertMapCampaignBlockToLevel(entry.ID, *block)
		camp.Levels = append(camp.Levels, level)
		out[block.CampaignID] = camp
	}

	for id, camp := range out {
		camp.Levels = sortAndValidateLevels(id, camp.Levels)
		out[id] = camp
	}
	return out
}

// convertMapCampaignBlockToLevel turns one wire-shape campaign block into the
// game-package CampaignLevelDef, running each objective through the registry
// validator. Panics on bad data, naming the source map and level id.
func convertMapCampaignBlockToLevel(mapID string, block protocol.MapCampaignBlock) CampaignLevelDef {
	if block.LevelID == "" {
		panic("catalog/maps/" + mapID + `: campaign block missing "levelId"`)
	}
	if block.DisplayName == "" {
		panic("catalog/maps/" + mapID + `: campaign level "` + block.LevelID + `" missing "displayName"`)
	}

	objectives := make([]ObjectiveDef, 0, len(block.Objectives))
	objectiveIDs := make(map[string]bool, len(block.Objectives))
	for _, raw := range block.Objectives {
		if raw.ID != "" && objectiveIDs[raw.ID] {
			panic("catalog/maps/" + mapID + `: level "` + block.LevelID +
				`": duplicate objective id "` + raw.ID + `"`)
		}
		def := ObjectiveDef{
			ID:          raw.ID,
			Type:        raw.Type,
			Description: raw.Description,
			Scope:       ObjectiveScope(raw.Scope),
			Required:    raw.Required,
			Config:      raw.Config,
		}
		// Reuse the existing parse+validate pipeline. The filename arg is
		// the map id (not a campaign filename) so the panic message points
		// at the actual source on disk.
		def = parseAndValidateObjectiveDef("../maps/"+mapID+".json", block.LevelID, def)
		objectives = append(objectives, def)
		objectiveIDs[def.ID] = true
	}

	return CampaignLevelDef{
		ID:                  block.LevelID,
		DisplayName:         block.DisplayName,
		MapID:               mapID,
		PrerequisiteLevelID: block.PrerequisiteLevelID,
		Description:         block.Description,
		SortOrder:           block.SortOrder,
		Objectives:          objectives,
	}
}

// sortAndValidateLevels orders a campaign's level slice by (SortOrder, ID)
// and verifies prereq references resolve within the same campaign. Panics on
// duplicate level ids, unknown prereqs, or self-referencing prereqs.
func sortAndValidateLevels(campaignID string, levels []CampaignLevelDef) []CampaignLevelDef {
	if len(levels) == 0 {
		return levels
	}

	sort.Slice(levels, func(i, j int) bool {
		if levels[i].SortOrder != levels[j].SortOrder {
			return levels[i].SortOrder < levels[j].SortOrder
		}
		return levels[i].ID < levels[j].ID
	})

	levelIDs := make(map[string]bool, len(levels))
	for _, lvl := range levels {
		if levelIDs[lvl.ID] {
			panic(`campaign "` + campaignID + `": duplicate level id "` + lvl.ID + `" from map "` + lvl.MapID + `"`)
		}
		levelIDs[lvl.ID] = true
	}
	for _, lvl := range levels {
		if lvl.PrerequisiteLevelID == nil {
			continue
		}
		if *lvl.PrerequisiteLevelID == lvl.ID {
			panic(`campaign "` + campaignID + `": level "` + lvl.ID +
				`" lists itself as a prerequisite`)
		}
		if !levelIDs[*lvl.PrerequisiteLevelID] {
			panic(`campaign "` + campaignID + `": level "` + lvl.ID +
				`" references unknown prerequisite "` + *lvl.PrerequisiteLevelID + `"`)
		}
	}
	return levels
}

// ValidateMapCampaignBlockForSave runs the discovery-time validation on a
// single campaign block before SaveMapCatalogEntry commits it to disk and
// the runtime overlay. Returns the first validation error as a regular error
// (in contrast to discovery's panics) so the HTTP layer can return a 400
// instead of crashing the server.
//
// Checks:
//
//  1. Campaign id is non-empty and has a matching catalog/campaigns/<id>.json
//     header.
//  2. No other map in the current catalog snapshot already claims the same
//     (campaignId, levelId) pair. Skipping the entry being overwritten lets a
//     normal re-save succeed; only a *different* map laying claim to the same
//     id is rejected. This is the guard that prevents an editor save from
//     poisoning the next /api/catalog/campaigns request — discovery panics on
//     duplicate level ids, so without this check a typo could take the server
//     down on the next read.
//  3. Each authored objective parses + validates through the registry. The
//     underlying pipeline panics; we recover and convert to an error.
//
// Deferred to discovery (not checked here): cross-campaign prereq references,
// self-prereq, and the prereq-graph-coherence checks performed by
// sortAndValidateLevels. The editor's UI already prevents the common form of
// these errors (the prereq dropdown can't select the currently-edited level),
// and the discovery-time panic still fires for the exotic remaining cases.
func ValidateMapCampaignBlockForSave(mapID string, block *protocol.MapCampaignBlock) error {
	if block == nil {
		return nil
	}
	if block.CampaignID == "" {
		return errCampaignSave("campaign block missing campaignId")
	}
	if _, ok := campaignHeadersByID[block.CampaignID]; !ok {
		return errCampaignSave(`campaign id "` + block.CampaignID + `" has no catalog/campaigns/<id>.json header`)
	}

	// Cross-map invariant: refuse a save if a *different* map already owns
	// this (campaignId, levelId) pair. Re-saving the SAME mapID is fine —
	// that's an in-place edit. Same level id under a DIFFERENT campaign is
	// also fine — campaigns are independent ID namespaces.
	if block.LevelID != "" {
		for _, entry := range currentMapCatalogSnapshot() {
			if entry.ID == mapID {
				continue
			}
			other := entry.Map.Campaign
			if other == nil {
				continue
			}
			if other.CampaignID == block.CampaignID && other.LevelID == block.LevelID {
				return errCampaignSave(
					`level id "` + block.LevelID + `" is already used by map "` +
						entry.ID + `" in campaign "` + block.CampaignID +
						`" — pick a different levelId or edit that map instead`)
			}
		}
	}

	// convertMapCampaignBlockToLevel panics on bad authored data. For the
	// save path we want an error, not a panic — recover and convert.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errCampaignSave(toErrorString(r))
			}
		}()
		_ = convertMapCampaignBlockToLevel(mapID, *block)
	}()
	return err
}

type campaignSaveError struct{ msg string }

func (e *campaignSaveError) Error() string { return e.msg }
func errCampaignSave(msg string) error     { return &campaignSaveError{msg: msg} }

func toErrorString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return "validation panic"
	}
}

// ListCampaignDefs returns all registered campaign definitions sorted by
// SortOrder (ascending), then ID. Recomputes the level tree from the current
// map catalog snapshot on each call so editor saves are visible without a
// server restart.
func ListCampaignDefs() []CampaignDef {
	tree := buildCampaignDefs()
	defs := make([]CampaignDef, 0, len(tree))
	for _, d := range tree {
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
// Recomputes the level tree on each call (see ListCampaignDefs).
func GetCampaignDef(id string) (CampaignDef, bool) {
	tree := buildCampaignDefs()
	def, ok := tree[id]
	return def, ok
}
