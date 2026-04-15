package game

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"

	"webrts/server/pkg/protocol"
)

type MapCatalogEntry struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	SortOrder   int                `json:"sortOrder"`
	Map         protocol.MapConfig `json:"map"`
}

type MapCatalogSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	GridCols    int    `json:"gridCols"`
	GridRows    int    `json:"gridRows"`
}

//go:embed catalog/maps/*.json
var mapCatalogFS embed.FS

var (
	mapCatalog       = mustLoadMapCatalog()
	mapCatalogByID   = indexMapCatalog(mapCatalog)
	defaultCatalogID = mapCatalog[0].ID
)

func ListMapCatalogSummaries() []MapCatalogSummary {
	summaries := make([]MapCatalogSummary, 0, len(mapCatalog))

	for _, entry := range mapCatalog {
		summaries = append(summaries, MapCatalogSummary{
			ID:          entry.ID,
			Name:        entry.Name,
			Description: entry.Description,
			GridCols:    entry.Map.GridCols,
			GridRows:    entry.Map.GridRows,
		})
	}

	return summaries
}

func DefaultMapID() string {
	return defaultCatalogID
}

func GetMapConfigByID(mapID string) protocol.MapConfig {
	entry, ok := mapCatalogByID[mapID]
	if !ok {
		entry = mapCatalogByID[defaultCatalogID]
	}

	return cloneMapConfig(entry.Map)
}

func GetMapCatalogEntryByID(mapID string) (MapCatalogEntry, bool) {
	entry, ok := mapCatalogByID[mapID]
	if !ok {
		return MapCatalogEntry{}, false
	}

	cloned := entry
	cloned.Map = cloneMapConfig(entry.Map)
	return cloned, true
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
	cloned.Obstacles = append([]protocol.ObstacleTile(nil), mapConfig.Obstacles...)
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
