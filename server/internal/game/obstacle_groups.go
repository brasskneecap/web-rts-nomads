package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"webrts/server/pkg/protocol"
)

// ObstacleGroup is the authored, on-disk / editor-API representation of every
// obstacle of a single type within one map: the shared metadata stored ONCE,
// plus the list of [x, y] grid locations. Runtime obstacle instances are
// expanded from this (one ObstacleTile per location) and their ids are
// regenerated as "<type>-<x>-<y>" by hydrateObstacles — the coordinates are the
// identity, so no per-obstacle id is stored.
//
// The format assumes all obstacles of a given type in one map share metadata
// (true across the shipped catalog). The legacy flat-array reader in
// obstacleStore.UnmarshalJSON keeps older / hand-authored maps loadable.
type ObstacleGroup struct {
	Width          int                    `json:"width,omitempty"`
	Height         int                    `json:"height,omitempty"`
	Capabilities   []string               `json:"capabilities,omitempty"`
	ResourceType   string                 `json:"resourceType,omitempty"`
	ResourceAmount int                    `json:"resourceAmount,omitempty"`
	MaxHp          float64                `json:"maxHp,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	// Locations is the list of grid cells holding an obstacle of this type,
	// each [x, y]. Stored in encounter order so a save round-trips stably.
	Locations [][2]int `json:"locations"`
}

// groupObstacles collapses a flat obstacle slice into per-type groups for
// serialization. Metadata is taken from the first instance of each type (all
// instances of a type share it); per-instance id and runtime hp are dropped.
// Locations preserve input order.
func groupObstacles(obstacles []protocol.ObstacleTile) map[string]*ObstacleGroup {
	groups := make(map[string]*ObstacleGroup)
	for i := range obstacles {
		o := &obstacles[i]
		g, ok := groups[o.Obstacle]
		if !ok {
			g = &ObstacleGroup{
				Width:          o.Width,
				Height:         o.Height,
				Capabilities:   append([]string(nil), o.Capabilities...),
				ResourceType:   o.ResourceType,
				ResourceAmount: o.ResourceAmount,
				MaxHp:          o.MaxHp,
				Metadata:       o.Metadata,
			}
			groups[o.Obstacle] = g
		}
		g.Locations = append(g.Locations, [2]int{o.X, o.Y})
	}
	return groups
}

// expandObstacleGroups flattens per-type groups back into runtime obstacle
// tiles. Types are iterated in sorted order and locations in stored order so
// expansion is deterministic. Def-owned fields (and the id) are filled later by
// hydrateObstacles.
func expandObstacleGroups(groups map[string]ObstacleGroup) []protocol.ObstacleTile {
	types := make([]string, 0, len(groups))
	for t := range groups {
		types = append(types, t)
	}
	sort.Strings(types)

	out := make([]protocol.ObstacleTile, 0)
	for _, t := range types {
		g := groups[t]
		for _, loc := range g.Locations {
			out = append(out, protocol.ObstacleTile{
				GridCoord:      protocol.GridCoord{X: loc[0], Y: loc[1]},
				Obstacle:       t,
				Width:          g.Width,
				Height:         g.Height,
				Capabilities:   append([]string(nil), g.Capabilities...),
				ResourceType:   g.ResourceType,
				ResourceAmount: g.ResourceAmount,
				MaxHp:          g.MaxHp,
				Metadata:       g.Metadata,
			})
		}
	}
	return out
}

// obstacleStore is a JSON shim for a map's "obstacles" field that accepts BOTH
// the new grouped object form and the legacy flat-array form, always yielding a
// flat slice. It marshals to the grouped form. Used only by the catalog
// (disk + editor API) layer via MapCatalogEntry; the runtime/in-game MapConfig
// keeps its plain []ObstacleTile.
type obstacleStore struct {
	flat []protocol.ObstacleTile
}

func (s *obstacleStore) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		s.flat = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		// Legacy flat array of obstacle tiles.
		return json.Unmarshal(b, &s.flat)
	case '{':
		var groups map[string]ObstacleGroup
		if err := json.Unmarshal(b, &groups); err != nil {
			return err
		}
		s.flat = expandObstacleGroups(groups)
		return nil
	default:
		return fmt.Errorf("obstacles: expected JSON array or object, got %q", trimmed[0])
	}
}

// catalogEntryWire is the on-disk / editor-API JSON shape of a MapCatalogEntry:
// identical to MapCatalogEntry except the nested map's "obstacles" field is the
// grouped form. mapAlias drops the MapConfig JSON methods (there are none today,
// but this also guards against marshal recursion) and its embedded "obstacles"
// is shadowed by the explicit grouped/store field.
type catalogEntryWire struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	SortOrder   int         `json:"sortOrder"`
	Map         mapConfigIn `json:"map"`
}

// mapAlias is a defined type over MapConfig used as an embedded field. Being a
// distinct type it carries no methods (guarding against marshal recursion) while
// keeping every MapConfig field + json tag. Its embedded "obstacles" field is
// shadowed by the explicit grouped/store field in mapConfigOut/mapConfigIn.
type mapAlias protocol.MapConfig

// mapConfigOut marshals a MapConfig with grouped terrain, tiles, and obstacles.
// Each explicit field shadows the embedded flat field of the same JSON name.
type mapConfigOut struct {
	mapAlias
	Terrain   map[string][][2]int       `json:"terrain"`
	Tiles     []*TileGroup              `json:"tiles,omitempty"`
	Obstacles map[string]*ObstacleGroup `json:"obstacles"`
}

// mapConfigIn unmarshals a MapConfig whose terrain/tiles/obstacles may be in the
// grouped form or a legacy flat array, into flat slices.
type mapConfigIn struct {
	mapAlias
	Terrain   terrainStore  `json:"terrain"`
	Tiles     tileStore     `json:"tiles"`
	Obstacles obstacleStore `json:"obstacles"`
}

// MarshalJSON writes the entry with grouped obstacles. This drives both the
// on-disk catalog files (writeMapEntryToDisk) and the editor's GET /maps/{id}.
// The in-game welcome path marshals protocol.MapConfig directly and is
// unaffected — obstacles stay flat on the gameplay wire.
func (e MapCatalogEntry) MarshalJSON() ([]byte, error) {
	mapOut := mapConfigOut{
		mapAlias:  mapAlias(e.Map),
		Terrain:   groupTerrain(e.Map.Terrain),
		Tiles:     groupTiles(e.Map.Tiles),
		Obstacles: groupObstacles(e.Map.Obstacles),
	}
	// The embedded flat slices are shadowed by the grouped fields for JSON, but
	// clear them so there is no ambiguity.
	mapOut.mapAlias.Terrain = nil
	mapOut.mapAlias.Tiles = nil
	mapOut.mapAlias.Obstacles = nil

	out := struct {
		ID          string       `json:"id"`
		Name        string       `json:"name"`
		Description string       `json:"description"`
		SortOrder   int          `json:"sortOrder"`
		Map         mapConfigOut `json:"map"`
	}{e.ID, e.Name, e.Description, e.SortOrder, mapOut}
	return json.Marshal(out)
}

// RenderCatalogEntryJSON serializes a catalog entry to the pretty on-disk form:
// 2-space indented like the rest of the catalog, but with every coordinate-pair
// list (obstacle/tile "locations" and terrain coordinate arrays) collapsed onto
// a single line ([[3,0],[4,0],...]) so the file stays compact. Used by
// writeMapEntryToDisk and the one-shot migration so both produce identical
// output.
func RenderCatalogEntryJSON(entry MapCatalogEntry) ([]byte, error) {
	indented, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, err
	}
	return compactCoordArrays(indented), nil
}

// compactCoordArrays rewrites indented catalog JSON so every array-of-arrays
// (i.e. a coordinate-pair list) renders on a single line. In a map file the
// only arrays whose first element is itself an array are coordinate lists
// (string and object arrays start with '"' / '{'), so detecting "an array whose
// first non-whitespace element is '['" uniquely and safely identifies them.
// String literals are skipped so a '[' inside a value never triggers a match.
func compactCoordArrays(indented []byte) []byte {
	var out bytes.Buffer
	n := len(indented)
	i := 0
	for i < n {
		c := indented[i]

		// Skip string literals verbatim.
		if c == '"' {
			out.WriteByte(c)
			i++
			for i < n {
				ch := indented[i]
				out.WriteByte(ch)
				if ch == '\\' && i+1 < n {
					out.WriteByte(indented[i+1])
					i += 2
					continue
				}
				i++
				if ch == '"' {
					break
				}
			}
			continue
		}

		if c == '[' {
			j := i + 1
			for j < n && isJSONSpace(indented[j]) {
				j++
			}
			if j < n && indented[j] == '[' {
				// Array of arrays → coordinate list. Find the matching ']'.
				k := scanMatchingBracket(indented, i)
				var compact bytes.Buffer
				if err := json.Compact(&compact, indented[i:k+1]); err != nil {
					out.Write(indented[i : k+1])
				} else {
					out.Write(compact.Bytes())
				}
				i = k + 1
				continue
			}
		}

		out.WriteByte(c)
		i++
	}
	return out.Bytes()
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == '\r'
}

// scanMatchingBracket returns the index of the ']' matching the '[' at start,
// honoring string literals so brackets inside strings are ignored.
func scanMatchingBracket(b []byte, start int) int {
	depth := 0
	inStr := false
	for k := start; k < len(b); k++ {
		ch := b[k]
		if inStr {
			if ch == '\\' {
				k++
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(b) - 1
}

// UnmarshalJSON reads an entry whose obstacles are grouped (or a legacy flat
// array) and expands them into the flat MapConfig.Obstacles the runtime uses.
func (e *MapCatalogEntry) UnmarshalJSON(b []byte) error {
	var in catalogEntryWire
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	e.ID = in.ID
	e.Name = in.Name
	e.Description = in.Description
	e.SortOrder = in.SortOrder
	e.Map = protocol.MapConfig(in.Map.mapAlias)
	e.Map.Terrain = in.Map.Terrain.flat
	e.Map.Tiles = in.Map.Tiles.flat
	e.Map.Obstacles = in.Map.Obstacles.flat
	return nil
}
