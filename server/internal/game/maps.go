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
}

//go:embed catalog/maps/*.json
var mapCatalogFS embed.FS

var (
	mapCatalog       = func() []MapCatalogEntry { _ = unitDefsByType; return mustLoadMapCatalog() }()
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
		summaries = append(summaries, MapCatalogSummary{
			ID:              entry.ID,
			Name:            entry.Name,
			Description:     entry.Description,
			GridCols:        entry.Map.GridCols,
			GridRows:        entry.Map.GridRows,
			SpawnPointCount: spawnCount,
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

// SaveMapCatalogEntry writes a map catalog entry to disk and immediately
// registers it in the runtime overlay so it is available without a restart.
func SaveMapCatalogEntry(entry MapCatalogEntry) error {
	dir, err := resolveMapsDir()
	if err != nil {
		return err
	}

	safeID := sanitizeMapFilename(entry.ID)
	if safeID == "" {
		return fmt.Errorf("map id %q is not a valid filename", entry.ID)
	}

	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, safeID+".json"), raw, 0644); err != nil {
		return err
	}

	hydrateObstacles(entry.Map.Obstacles)
	hydrateBuildings(entry.Map.Buildings)
	if entry.Map.PlacedUnits == nil {
		entry.Map.PlacedUnits = []protocol.PlacedUnit{}
	}
	entry.Map.PlacedUnits = hydratePlacedUnits(entry.Map.PlacedUnits, entry.Map)

	runtimeMapsMu.Lock()
	runtimeMaps[entry.ID] = entry
	runtimeMapsMu.Unlock()

	return nil
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
