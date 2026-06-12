package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"webrts/server/pkg/protocol"
)

// This file holds the grouped on-disk / editor-API representations of a map's
// terrain and tiles, mirroring the obstacle grouping in obstacle_groups.go.
// Both reduce file size by storing each repeated cell's "kind" once plus a list
// of [x, y] locations; the runtime / in-game wire keeps the flat slices.

// ── Terrain ────────────────────────────────────────────────────────────────
//
// Grouped form: { "<terrainType>": [[x,y], ...] }. Terrain has no per-cell
// metadata beyond its type, so each value is just the coordinate list.

// groupTerrain collapses flat terrain tiles into per-type coordinate lists.
func groupTerrain(tiles []protocol.TerrainTile) map[string][][2]int {
	groups := make(map[string][][2]int)
	for _, t := range tiles {
		groups[t.Terrain] = append(groups[t.Terrain], [2]int{t.X, t.Y})
	}
	return groups
}

// expandTerrainGroups flattens per-type coordinate lists back into terrain
// tiles. Types are iterated in sorted order, locations in stored order.
func expandTerrainGroups(groups map[string][][2]int) []protocol.TerrainTile {
	types := make([]string, 0, len(groups))
	for t := range groups {
		types = append(types, t)
	}
	sort.Strings(types)

	out := make([]protocol.TerrainTile, 0)
	for _, t := range types {
		for _, loc := range groups[t] {
			out = append(out, protocol.TerrainTile{
				GridCoord: protocol.GridCoord{X: loc[0], Y: loc[1]},
				Terrain:   t,
			})
		}
	}
	return out
}

// terrainStore is a JSON shim accepting BOTH the grouped object form and the
// legacy flat-array form, always yielding a flat slice. Marshals grouped.
type terrainStore struct {
	flat []protocol.TerrainTile
}

func (s *terrainStore) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		s.flat = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		return json.Unmarshal(b, &s.flat)
	case '{':
		var groups map[string][][2]int
		if err := json.Unmarshal(b, &groups); err != nil {
			return err
		}
		s.flat = expandTerrainGroups(groups)
		return nil
	default:
		return fmt.Errorf("terrain: expected JSON array or object, got %q", trimmed[0])
	}
}

// ── Tiles ──────────────────────────────────────────────────────────────────
//
// Grouped form: [ { "sheet": "...", "sx": N, "sy": N, "locations": [[x,y],...] } ].
// A tile's identity is the (sheet, sx, sy) tuple, so groups are an array rather
// than an object keyed by a single name.

// TileGroup is the authored representation of every tile sharing one
// (sheet, sx, sy), plus its [x, y] locations.
type TileGroup struct {
	protocol.TileCoord
	Locations [][2]int `json:"locations"`
}

// tileKey identifies a distinct tile for grouping.
type tileKey struct {
	sheet  string
	sx, sy int
}

// groupTiles collapses flat tile instances into per-(sheet,sx,sy) groups,
// ordered deterministically by sheet then sx then sy. Locations keep input
// order.
func groupTiles(tiles []protocol.TileInstance) []*TileGroup {
	index := make(map[tileKey]*TileGroup)
	order := make([]tileKey, 0)
	for _, t := range tiles {
		k := tileKey{t.Sheet, t.SX, t.SY}
		g, ok := index[k]
		if !ok {
			g = &TileGroup{TileCoord: t.TileCoord}
			index[k] = g
			order = append(order, k)
		}
		g.Locations = append(g.Locations, [2]int{t.X, t.Y})
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.sheet != b.sheet {
			return a.sheet < b.sheet
		}
		if a.sx != b.sx {
			return a.sx < b.sx
		}
		return a.sy < b.sy
	})
	out := make([]*TileGroup, 0, len(order))
	for _, k := range order {
		out = append(out, index[k])
	}
	return out
}

// expandTileGroups flattens tile groups back into tile instances, groups in
// stored order and locations in stored order.
func expandTileGroups(groups []TileGroup) []protocol.TileInstance {
	out := make([]protocol.TileInstance, 0)
	for _, g := range groups {
		for _, loc := range g.Locations {
			out = append(out, protocol.TileInstance{
				GridCoord: protocol.GridCoord{X: loc[0], Y: loc[1]},
				TileCoord: g.TileCoord,
			})
		}
	}
	return out
}

// tileStore is a JSON shim for the "tiles" field accepting BOTH the grouped
// array-of-groups form and the legacy flat-array form (distinguished per
// element: a "locations" key means a group, otherwise a single x/y tile),
// always yielding a flat slice. Marshals grouped.
type tileStore struct {
	flat []protocol.TileInstance
}

func (s *tileStore) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		s.flat = nil
		return nil
	}
	if trimmed[0] != '[' {
		return fmt.Errorf("tiles: expected JSON array, got %q", trimmed[0])
	}

	var elems []json.RawMessage
	if err := json.Unmarshal(b, &elems); err != nil {
		return err
	}
	out := make([]protocol.TileInstance, 0, len(elems))
	for _, raw := range elems {
		// A group carries "locations"; a legacy tile carries "x"/"y".
		var probe struct {
			Locations json.RawMessage `json:"locations"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return err
		}
		if len(probe.Locations) > 0 {
			var g TileGroup
			if err := json.Unmarshal(raw, &g); err != nil {
				return err
			}
			out = append(out, expandTileGroups([]TileGroup{g})...)
			continue
		}
		var tile protocol.TileInstance
		if err := json.Unmarshal(raw, &tile); err != nil {
			return err
		}
		out = append(out, tile)
	}
	s.flat = out
	return nil
}
