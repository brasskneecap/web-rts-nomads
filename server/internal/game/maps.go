package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"webrts/server/pkg/protocol"
)

// hydrateObstacles fills in per-obstacle fields from the matching obstacle def
// loaded from catalog/obstacles. Obstacles on disk typically carry only
// position and type; this expands each tile with its id, capabilities,
// resource pool, and HP so the runtime can treat obstacles as first-class
// hydrateBuildings reconciles per-building footprint fields with the matching
// building def. Map JSONs historically inlined width/height per entry, which
// drifts when a def changes (e.g. the townhall collision box shrinking to a
// 3x2 base with a 3x3 sprite). Existing map data is forced to match the def
// so pathing, placement, and spawn logic stay consistent with the catalog.
// Fields the def doesn't own (x, y, metadata, ids) are left alone.
func hydrateBuildings(buildings []protocol.BuildingTile) {
	for i := range buildings {
		b := &buildings[i]
		def, ok := getBuildingDef(b.BuildingType)
		if !ok {
			continue
		}
		if def.Width > 0 {
			b.Width = def.Width
		}
		if def.Height > 0 {
			b.Height = def.Height
		}
		// Capabilities are owned by the def; always use the def's list so map
		// JSONs don't need to be updated when a capability is added to a def.
		if len(def.Capabilities) > 0 {
			b.Capabilities = append([]string(nil), def.Capabilities...)
		}
	}
}

// selectable/harvestable/destructible entities.
func hydrateObstacles(obstacles []protocol.ObstacleTile) {
	for i := range obstacles {
		o := &obstacles[i]
		def, ok := getObstacleDef(o.Obstacle)
		if !ok {
			continue
		}
		if o.ID == "" {
			o.ID = fmt.Sprintf("%s-%d-%d", o.Obstacle, o.X, o.Y)
		}
		if o.Width == 0 {
			o.Width = def.Width
			if o.Width == 0 {
				o.Width = 1
			}
		}
		if o.Height == 0 {
			o.Height = def.Height
			if o.Height == 0 {
				o.Height = 1
			}
		}
		if len(o.Capabilities) == 0 && len(def.Capabilities) > 0 {
			o.Capabilities = append([]string(nil), def.Capabilities...)
		}
		if o.ResourceType == "" && def.ResourceType != "" {
			o.ResourceType = def.ResourceType
		}
		if o.ResourceAmount == 0 && def.ResourceAmount > 0 {
			o.ResourceAmount = def.ResourceAmount
		}
		if o.MaxHp == 0 && def.MaxHp > 0 {
			o.MaxHp = def.MaxHp
		}
		if o.Hp == 0 {
			o.Hp = o.MaxHp
		}
	}
}

const (
	placedUnitDefaultAggroRange = 150.0
	placedUnitDefaultLeashRange = 200.0
)

// hydratePlacedUnits validates and normalises the PlacedUnits slice of a map
// config. Invalid entries are silently dropped with a log warning so a bad
// authored entry never crashes the server. Runtime defaults (AggroRange,
// LeashRange) are applied in-place; they are NOT written back to disk.
func hydratePlacedUnits(units []protocol.PlacedUnit, cfg protocol.MapConfig) []protocol.PlacedUnit {
	if len(units) == 0 {
		return []protocol.PlacedUnit{}
	}
	out := make([]protocol.PlacedUnit, 0, len(units))
	for _, entry := range units {
		if entry.PlayerSlot == "" {
			slog.Warn("hydratePlacedUnits: dropping entry with missing playerSlot",
				"unitType", entry.UnitType, "x", entry.X, "y", entry.Y)
			continue
		}
		_, ok := getUnitDef(entry.UnitType)
		if !ok {
			slog.Warn("hydratePlacedUnits: dropping entry with unknown unitType",
				"playerSlot", entry.PlayerSlot, "unitType", entry.UnitType, "x", entry.X, "y", entry.Y)
			continue
		}
		gridW := int(cfg.Width / cfg.CellSize)
		gridH := int(cfg.Height / cfg.CellSize)
		if cfg.GridCols > 0 {
			gridW = cfg.GridCols
		}
		if cfg.GridRows > 0 {
			gridH = cfg.GridRows
		}
		if entry.X < 0 || entry.X >= gridW || entry.Y < 0 || entry.Y >= gridH {
			slog.Warn("hydratePlacedUnits: dropping out-of-bounds entry",
				"playerSlot", entry.PlayerSlot, "unitType", entry.UnitType, "x", entry.X, "y", entry.Y,
				"gridW", gridW, "gridH", gridH)
			continue
		}
		if entry.ID == "" {
			entry.ID = fmt.Sprintf("placed-unit-%s-%d-%d", entry.PlayerSlot, entry.X, entry.Y)
		}
		if entry.AggroRange == 0 {
			entry.AggroRange = placedUnitDefaultAggroRange
		}
		if entry.LeashRange == 0 {
			entry.LeashRange = placedUnitDefaultLeashRange
		}
		out = append(out, entry)
	}
	return out
}

// normalizeZones fills in per-zone defaults and makes the authored adjacency
// graph symmetric (if A lists B, ensure B lists A). Mutates and returns the
// slice. Safe to call on both the embedded-catalog load path and the editor
// save path — it never panics, so a malformed interactive save cannot crash
// the server (hard validation is reserved for validateZones at catalog load).
func normalizeZones(zones []protocol.Zone) []protocol.Zone {
	if len(zones) == 0 {
		return zones
	}
	for i := range zones {
		z := &zones[i]
		if z.Cells == nil {
			z.Cells = [][2]int{}
		}
		if z.StartingOwner == "" {
			z.StartingOwner = protocol.ZoneCaptureNeutralOwner
		}
		if z.Name == "" {
			z.Name = z.ID
		}
	}
	// Adjacency is a DIRECTED prerequisite list now (zone -> required zones),
	// not a symmetric graph, so no reciprocal-edge normalisation.
	return zones
}

// validateZones enforces the zone invariants at catalog load, panicking (with
// the map filename) on a violation — catalogs are static, so a bad entry is a
// build error, mirroring how the objective loader treats catalog data. Checks:
// unique zone ids, single-owner cell membership, adjacency targets exist, and
// each capture mechanic's type is registered and its config valid.
func validateZones(filename string, zones []protocol.Zone) {
	if len(zones) == 0 {
		return
	}
	seen := make(map[string]bool, len(zones))
	for i := range zones {
		z := zones[i]
		if z.ID == "" {
			panic("catalog/maps/" + filename + ": zone with empty id")
		}
		if seen[z.ID] {
			panic("catalog/maps/" + filename + ": duplicate zone id " + z.ID)
		}
		seen[z.ID] = true
	}
	cellOwner := map[gridPoint]string{}
	for i := range zones {
		z := zones[i]
		for _, c := range z.Cells {
			gp := gridPoint{X: c[0], Y: c[1]}
			if other, dup := cellOwner[gp]; dup {
				panic(fmt.Sprintf("catalog/maps/%s: cell [%d,%d] claimed by zones %q and %q",
					filename, c[0], c[1], other, z.ID))
			}
			cellOwner[gp] = z.ID
		}
	}
	for i := range zones {
		z := zones[i]
		for _, adjID := range z.Adjacent {
			if !seen[adjID] {
				panic(fmt.Sprintf("catalog/maps/%s: zone %s adjacency references unknown zone %q",
					filename, z.ID, adjID))
			}
		}
		// CaptureCells (presence capture sub-zone) must be a subset of the zone.
		for _, c := range z.CaptureCells {
			if cellOwner[gridPoint{X: c[0], Y: c[1]}] != z.ID {
				panic(fmt.Sprintf("catalog/maps/%s: zone %s capture cell [%d,%d] is not inside the zone",
					filename, z.ID, c[0], c[1]))
			}
		}
		parseAndValidateZoneCapture(filename, z.ID, z.Capture)
	}
}

// validateZoneTriggers checks that every enemy-spawnpoint's triggerCaptureZoneId
// (when set) names a zone that exists in the same map. Panics at catalog load
// naming the offending building + zone id.
func validateZoneTriggers(filename string, buildings []protocol.BuildingTile, zones []protocol.Zone) {
	if len(buildings) == 0 {
		return
	}
	zoneIDs := make(map[string]bool, len(zones))
	for _, z := range zones {
		zoneIDs[z.ID] = true
	}
	for _, b := range buildings {
		if b.BuildingType != "enemy-spawnpoint" || b.Metadata == nil {
			continue
		}
		raw, ok := b.Metadata["triggerCaptureZoneId"]
		if !ok {
			continue
		}
		zoneID, _ := raw.(string)
		if zoneID == "" {
			continue
		}
		if !zoneIDs[zoneID] {
			panic(fmt.Sprintf("catalog/maps/%s: enemy-spawnpoint %s triggerCaptureZoneId references unknown zone %q",
				filename, b.ID, zoneID))
		}
	}
}

type MapCatalogEntry struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	SortOrder   int                `json:"sortOrder"`
	Map         protocol.MapConfig `json:"map"`
}

type MapCatalogSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	GridCols        int    `json:"gridCols"`
	GridRows        int    `json:"gridRows"`
	SpawnPointCount int    `json:"spawnPointCount"`
	// CampaignID, when non-empty, signals the map is tagged as a campaign
	// level (via its `Map.Campaign` block). The Custom Game lobby filters
	// these out of its map dropdown so campaign content can't be played
	// piecemeal in custom games — duplicate-and-rename the map without the
	// campaign tag to use the geometry in custom games.
	//
	// The editor's "Load Existing Map" dropdown deliberately does NOT
	// filter on this — campaign maps are still maps and must be editable.
	CampaignID string `json:"campaignId,omitempty"`
}

//go:embed catalog/maps/*.json
var mapCatalogFS embed.FS

var (
	mapCatalog       = func() []MapCatalogEntry { _ = unitDefsByType; _ = allZoneCaptureHandlersRegistered; return mustLoadMapCatalog() }()
	mapCatalogByID   = indexMapCatalog(mapCatalog)
	defaultCatalogID = mapCatalog[0].ID

	runtimeMapsMu sync.RWMutex
	runtimeMaps   = map[string]MapCatalogEntry{}
)

func ListMapCatalogSummaries() []MapCatalogSummary {
	merged := make(map[string]MapCatalogEntry, len(mapCatalog))
	for _, entry := range mapCatalog {
		merged[entry.ID] = entry
	}

	runtimeMapsMu.RLock()
	for id, entry := range runtimeMaps {
		merged[id] = entry
	}
	runtimeMapsMu.RUnlock()

	summaries := make([]MapCatalogSummary, 0, len(merged))
	for _, entry := range merged {
		spawnCount := 0
		for _, b := range entry.Map.Buildings {
			if b.BuildingType == "spawn-point" {
				spawnCount++
			}
		}
		campaignID := ""
		if entry.Map.Campaign != nil {
			campaignID = entry.Map.Campaign.CampaignID
		}
		summaries = append(summaries, MapCatalogSummary{
			ID:              entry.ID,
			Name:            entry.Name,
			Description:     entry.Description,
			GridCols:        entry.Map.GridCols,
			GridRows:        entry.Map.GridRows,
			SpawnPointCount: spawnCount,
			CampaignID:      campaignID,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})

	return summaries
}

func DefaultMapID() string {
	return defaultCatalogID
}

func GetMapConfigByID(mapID string) protocol.MapConfig {
	runtimeMapsMu.RLock()
	entry, ok := runtimeMaps[mapID]
	runtimeMapsMu.RUnlock()
	if !ok {
		entry, ok = mapCatalogByID[mapID]
	}
	if !ok {
		entry = mapCatalogByID[defaultCatalogID]
	}
	cfg := cloneMapConfig(entry.Map)
	// If the active entry lost its placed units (e.g. stale runtimeMaps from an
	// editor save that didn't include them), restore from the embedded catalog so
	// game matches always use the authored unit layout.
	if len(cfg.PlacedUnits) == 0 {
		if canonical, exists := mapCatalogByID[mapID]; exists && len(canonical.Map.PlacedUnits) > 0 {
			cfg.PlacedUnits = append([]protocol.PlacedUnit(nil), canonical.Map.PlacedUnits...)
		}
	}
	return cfg
}

func GetMapCatalogEntryByID(mapID string) (MapCatalogEntry, bool) {
	runtimeMapsMu.RLock()
	entry, ok := runtimeMaps[mapID]
	runtimeMapsMu.RUnlock()
	if !ok {
		entry, ok = mapCatalogByID[mapID]
	}
	if !ok {
		return MapCatalogEntry{}, false
	}
	cloned := entry
	cloned.Map = cloneMapConfig(entry.Map)
	// Restore placed units from the embedded catalog when the active entry has
	// none — prevents a stale editor save from making the editor appear empty,
	// which would then cause the next save to also omit placed units.
	if len(cloned.Map.PlacedUnits) == 0 {
		if canonical, exists := mapCatalogByID[cloned.ID]; exists && len(canonical.Map.PlacedUnits) > 0 {
			cloned.Map.PlacedUnits = append([]protocol.PlacedUnit(nil), canonical.Map.PlacedUnits...)
		}
	}
	return cloned, true
}

// SaveMapOptions controls optional behavior of a map catalog save.
type SaveMapOptions struct {
	// ReassignLevel, when true, resolves a campaign level-ownership conflict by
	// clearing the previous owner map's campaign block as part of the same
	// save, instead of rejecting it. No effect when there is no conflict.
	ReassignLevel bool
}

// LevelConflict describes a (campaignId, levelId) pair already owned by a
// different map. SaveMapCatalogEntryWithOptions returns one (writing nothing)
// when a campaign-tagged save collides with an existing level and the caller
// did not opt into reassignment, so the HTTP layer can offer the user a
// reassign confirmation.
type LevelConflict struct {
	CampaignID   string `json:"campaignId"`
	LevelID      string `json:"levelId"`
	OwnerMapID   string `json:"ownerMapId"`
	OwnerMapName string `json:"ownerMapName"`
}

// SaveMapCatalogEntry writes a map catalog entry to disk and immediately
// registers it in the runtime overlay so it is available without a restart.
// A campaign level-ownership conflict is returned as an error (the historical
// behavior). Callers that want to resolve the conflict by reassigning the
// level use SaveMapCatalogEntryWithOptions with ReassignLevel set.
func SaveMapCatalogEntry(entry MapCatalogEntry) error {
	_, conflict, err := SaveMapCatalogEntryWithOptions(entry, SaveMapOptions{})
	if err != nil {
		return err
	}
	if conflict != nil {
		return errCampaignSave(
			`level id "` + conflict.LevelID + `" is already used by map "` +
				conflict.OwnerMapID + `" in campaign "` + conflict.CampaignID +
				`" — pick a different levelId or edit that map instead`)
	}
	return nil
}

// SaveMapCatalogEntryWithOptions validates and persists a map catalog entry,
// optionally reassigning a campaign level away from its current owner.
//
// Return shapes:
//   - (err != nil): validation failed or a disk write errored; nothing or a
//     partial write may have happened (see ordering note below).
//   - (conflict != nil): the entry claims a (campaignId, levelId) already owned
//     by a DIFFERENT map and opts.ReassignLevel was false — NOTHING is written.
//   - (reassignedFrom != ""): the save succeeded and cleared that map's
//     campaign block as part of the reassign.
//
// Validates the `Map.Campaign` block (campaign id, objective configs) before
// any disk write. Cross-map coherence beyond the single-level uniqueness check
// (prereq chains, etc.) is surfaced at the next /api/catalog/campaigns read.
//
// Reassign atomicity: the previous owner's cleared block is written to disk
// FIRST so a crash between the two file writes loses the level (recoverable)
// rather than leaving two maps claiming it (which panics campaign discovery on
// the next read). The two in-memory overlay updates are applied under a single
// lock so a concurrent campaign read never observes the transient duplicate.
func SaveMapCatalogEntryWithOptions(entry MapCatalogEntry, opts SaveMapOptions) (reassignedFrom string, conflict *LevelConflict, err error) {
	if err := validateMapCampaignBlockBasics(entry.ID, entry.Map.Campaign); err != nil {
		return "", nil, err
	}

	owner := MapCatalogEntry{}
	hasConflict := false
	if entry.Map.Campaign != nil {
		owner, hasConflict = findConflictingLevelOwner(entry.ID, entry.Map.Campaign)
	}
	if hasConflict && !opts.ReassignLevel {
		block := entry.Map.Campaign
		return "", &LevelConflict{
			CampaignID:   block.CampaignID,
			LevelID:      block.LevelID,
			OwnerMapID:   owner.ID,
			OwnerMapName: owner.Name,
		}, nil
	}

	dir, derr := resolveMapsDir()
	if derr != nil {
		return "", nil, derr
	}

	// Prepare the previous owner with its campaign block cleared.
	var clearedOwner *MapCatalogEntry
	if hasConflict && opts.ReassignLevel {
		full, ok := GetMapCatalogEntryByID(owner.ID)
		if !ok {
			return "", nil, fmt.Errorf("reassign: previous owner map %q not found", owner.ID)
		}
		full.Map.Campaign = nil
		clearedOwner = &full
	}

	// Disk writes (cleared owner first — see atomicity note above).
	if clearedOwner != nil {
		if werr := writeMapEntryToDisk(dir, *clearedOwner); werr != nil {
			return "", nil, werr
		}
	}
	if werr := writeMapEntryToDisk(dir, entry); werr != nil {
		return "", nil, werr
	}

	// Hydrate for the overlay AFTER serializing to disk (disk keeps the
	// authored form; the overlay carries the def-expanded form).
	hydrateEntryInPlace(&entry)
	runtimeMapsMu.Lock()
	if clearedOwner != nil {
		hydrateEntryInPlace(clearedOwner)
		runtimeMaps[clearedOwner.ID] = *clearedOwner
	}
	runtimeMaps[entry.ID] = entry
	runtimeMapsMu.Unlock()

	if clearedOwner != nil {
		reassignedFrom = clearedOwner.ID
	}
	return reassignedFrom, nil, nil
}

// writeMapEntryToDisk serializes one catalog entry to <dir>/<sanitizedId>.json.
// Marshals the entry as-is (callers write the authored form before hydration).
func writeMapEntryToDisk(dir string, entry MapCatalogEntry) error {
	safeID := sanitizeMapFilename(entry.ID)
	if safeID == "" {
		return fmt.Errorf("map id %q is not a valid filename", entry.ID)
	}
	raw, err := RenderCatalogEntryJSON(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, safeID+".json"), raw, 0644)
}

// hydrateEntryInPlace expands def-owned fields (obstacle/building footprints,
// placed-unit defaults) on an entry destined for the runtime overlay.
func hydrateEntryInPlace(entry *MapCatalogEntry) {
	hydrateObstacles(entry.Map.Obstacles)
	hydrateBuildings(entry.Map.Buildings)
	if entry.Map.PlacedUnits == nil {
		entry.Map.PlacedUnits = []protocol.PlacedUnit{}
	}
	entry.Map.PlacedUnits = hydratePlacedUnits(entry.Map.PlacedUnits, entry.Map)
	entry.Map.Zones = normalizeZones(entry.Map.Zones)
}

// currentMapCatalogSnapshot returns a flat slice of every map known to the
// catalog right now, with runtime editor saves overlaid on top of the
// embedded baseline. Used by campaign discovery (`buildCampaignDefs`) so a
// freshly-saved campaign-tagged map appears in the campaign tree without a
// restart.
//
// The Map field is shared with the embedded/runtime store — discovery treats
// the slice as read-only and never mutates Campaign blocks. Cheap to call;
// catalogs are small (low double-digit map count).
func currentMapCatalogSnapshot() []MapCatalogEntry {
	merged := make(map[string]MapCatalogEntry, len(mapCatalog))
	for _, entry := range mapCatalog {
		merged[entry.ID] = entry
	}
	runtimeMapsMu.RLock()
	for id, entry := range runtimeMaps {
		merged[id] = entry
	}
	runtimeMapsMu.RUnlock()

	out := make([]MapCatalogEntry, 0, len(merged))
	for _, entry := range merged {
		out = append(out, entry)
	}
	return out
}

func resolveMapsDir() (string, error) {
	if dir := os.Getenv("MAP_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "maps")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("maps directory not found at %s; set MAP_CATALOG_DIR env var to override", dir)
}

func sanitizeMapFilename(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mustLoadMapCatalog() []MapCatalogEntry {
	files, err := mapCatalogFS.ReadDir("catalog/maps")
	if err != nil {
		panic(err)
	}

	entries := make([]MapCatalogEntry, 0, len(files))

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		raw, err := mapCatalogFS.ReadFile("catalog/maps/" + file.Name())
		if err != nil {
			panic(err)
		}

		entry, err := parseMapCatalogEntry(file.Name(), raw)
		if err != nil {
			panic(err)
		}

		if entry.Map.ID == "" {
			entry.Map.ID = entry.ID
		}
			if entry.Map.Name == "" {
				entry.Map.Name = entry.Name
			}
			if entry.Map.Description == "" {
				entry.Map.Description = entry.Description
			}
			if entry.Map.Size == "" {
				entry.Map.Size = entry.ID
			}
		if entry.Map.Terrain == nil {
			entry.Map.Terrain = []protocol.TerrainTile{}
		}
		if entry.Map.Obstacles == nil {
			entry.Map.Obstacles = []protocol.ObstacleTile{}
		}
		if entry.Map.Buildings == nil {
			entry.Map.Buildings = []protocol.BuildingTile{}
		}

		hydrateObstacles(entry.Map.Obstacles)
		hydrateBuildings(entry.Map.Buildings)
		entry.Map.PlacedUnits = hydratePlacedUnits(entry.Map.PlacedUnits, entry.Map)
		entry.Map.Zones = normalizeZones(entry.Map.Zones)
		validateZones(file.Name(), entry.Map.Zones)
		validateZoneTriggers(file.Name(), entry.Map.Buildings, entry.Map.Zones)

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SortOrder != entries[j].SortOrder {
			return entries[i].SortOrder < entries[j].SortOrder
		}
		return entries[i].Name < entries[j].Name
	})

	if len(entries) == 0 {
		panic("no map catalog entries found")
	}

	return entries
}

func parseMapCatalogEntry(filename string, raw []byte) (MapCatalogEntry, error) {
	type catalogProbe struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		SortOrder   int             `json:"sortOrder"`
		Map         json.RawMessage `json:"map"`
	}

	var probe catalogProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return MapCatalogEntry{}, err
	}

	if len(probe.Map) > 0 {
		var entry MapCatalogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return MapCatalogEntry{}, err
		}
		return entry, nil
	}

	var mapConfig protocol.MapConfig
	if err := json.Unmarshal(raw, &mapConfig); err != nil {
		return MapCatalogEntry{}, err
	}

	baseName := strings.TrimSuffix(filename, ".json")
	name := mapConfig.Name
	if name == "" {
		name = baseName
	}

	return MapCatalogEntry{
		ID:          firstNonEmpty(mapConfig.ID, baseName),
		Name:        name,
		Description: mapConfig.Description,
		SortOrder:   1000,
		Map:         mapConfig,
	}, nil
}

func indexMapCatalog(entries []MapCatalogEntry) map[string]MapCatalogEntry {
	byID := make(map[string]MapCatalogEntry, len(entries))

	for _, entry := range entries {
		byID[entry.ID] = entry
	}

	return byID
}

func cloneMapConfig(mapConfig protocol.MapConfig) protocol.MapConfig {
	cloned := mapConfig
	cloned.Terrain = append([]protocol.TerrainTile(nil), mapConfig.Terrain...)
	cloned.PlacedUnits = append([]protocol.PlacedUnit(nil), mapConfig.PlacedUnits...)
	cloned.Zones = make([]protocol.Zone, len(mapConfig.Zones))
	for i, zone := range mapConfig.Zones {
		clonedZone := zone
		clonedZone.Cells = append([][2]int(nil), zone.Cells...)
		clonedZone.CaptureCells = append([][2]int(nil), zone.CaptureCells...)
		clonedZone.Adjacent = append([]string(nil), zone.Adjacent...)
		clonedZone.Capture.Config = append(json.RawMessage(nil), zone.Capture.Config...)
		cloned.Zones[i] = clonedZone
	}
	cloned.Obstacles = make([]protocol.ObstacleTile, len(mapConfig.Obstacles))
	for i, obstacle := range mapConfig.Obstacles {
		clonedObstacle := obstacle
		clonedObstacle.Capabilities = append([]string(nil), obstacle.Capabilities...)
		if obstacle.Metadata != nil {
			clonedObstacle.Metadata = make(map[string]interface{}, len(obstacle.Metadata))
			for key, value := range obstacle.Metadata {
				clonedObstacle.Metadata[key] = value
			}
		}
		cloned.Obstacles[i] = clonedObstacle
	}
	cloned.Buildings = make([]protocol.BuildingTile, len(mapConfig.Buildings))

	for i, building := range mapConfig.Buildings {
		clonedBuilding := building
		clonedBuilding.Capabilities = append([]string(nil), building.Capabilities...)
		clonedBuilding.SpawnUnitTypes = append([]string(nil), building.SpawnUnitTypes...)

		if building.Metadata != nil {
			clonedBuilding.Metadata = make(map[string]interface{}, len(building.Metadata))
			for key, value := range building.Metadata {
				clonedBuilding.Metadata[key] = value
			}
		}

		cloned.Buildings[i] = clonedBuilding
	}

	return cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
