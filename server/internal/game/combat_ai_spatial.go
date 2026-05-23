package game

import (
	"math"

	"webrts/server/pkg/protocol"
)

type combatSpatialKey struct {
	X int
	Y int
}

type combatSpatialIndex struct {
	bucketSize float64
	cells      map[combatSpatialKey][]*Unit
}

// buildingSpatialEntry holds the data needed for radius queries against a
// building without forcing call sites to recompute its center every lookup.
type buildingSpatialEntry struct {
	Building *protocol.BuildingTile
	X        float64
	Y        float64
}

// buildingSpatialIndex mirrors combatSpatialIndex for buildings. Buildings
// rarely move, so this is built once per combat tick (instead of per scored
// candidate, which is the historical hot path inside structureDefenseScore-
// Locked) and reused.
type buildingSpatialIndex struct {
	bucketSize float64
	cells      map[combatSpatialKey][]buildingSpatialEntry
}

func newBuildingSpatialIndex(bucketSize float64) *buildingSpatialIndex {
	return &buildingSpatialIndex{
		bucketSize: bucketSize,
		cells:      map[combatSpatialKey][]buildingSpatialEntry{},
	}
}

func (i *buildingSpatialIndex) add(b *protocol.BuildingTile, x, y float64) {
	key := combatSpatialKey{
		X: int(math.Floor(x / i.bucketSize)),
		Y: int(math.Floor(y / i.bucketSize)),
	}
	i.cells[key] = append(i.cells[key], buildingSpatialEntry{Building: b, X: x, Y: y})
}

// query returns every building whose center lies within radius of (x, y).
func (i *buildingSpatialIndex) query(x, y, radius float64) []buildingSpatialEntry {
	minX := int(math.Floor((x - radius) / i.bucketSize))
	maxX := int(math.Floor((x + radius) / i.bucketSize))
	minY := int(math.Floor((y - radius) / i.bucketSize))
	maxY := int(math.Floor((y + radius) / i.bucketSize))
	radiusSq := radius * radius
	results := make([]buildingSpatialEntry, 0, 4)
	for by := minY; by <= maxY; by++ {
		for bx := minX; bx <= maxX; bx++ {
			for _, e := range i.cells[combatSpatialKey{X: bx, Y: by}] {
				if distanceSquared(x, y, e.X, e.Y) <= radiusSq {
					results = append(results, e)
				}
			}
		}
	}
	return results
}

func (s *GameState) countNearbyHostilesLocked(target *Unit, radius float64, index *combatSpatialIndex) int {
	count := 0
	for _, hostile := range index.query(target.X, target.Y, radius) {
		if !s.playersAreHostileLocked(hostile.OwnerID, target.OwnerID) || hostile.HP <= 0 {
			continue
		}
		count++
	}
	return count
}

func (s *GameState) countHostilesAroundPointLocked(ownerID string, x, y, radius float64, index *combatSpatialIndex) int {
	count := 0
	for _, hostile := range index.query(x, y, radius) {
		if !s.playersAreHostileLocked(hostile.OwnerID, ownerID) || hostile.HP <= 0 {
			continue
		}
		count++
	}
	return count
}

func newCombatSpatialIndex(bucketSize float64) *combatSpatialIndex {
	return &combatSpatialIndex{
		bucketSize: bucketSize,
		cells:      map[combatSpatialKey][]*Unit{},
	}
}

func (i *combatSpatialIndex) add(unit *Unit) {
	key := combatSpatialKey{
		X: int(math.Floor(unit.X / i.bucketSize)),
		Y: int(math.Floor(unit.Y / i.bucketSize)),
	}
	i.cells[key] = append(i.cells[key], unit)
}

func (i *combatSpatialIndex) query(x, y, radius float64) []*Unit {
	minX := int(math.Floor((x - radius) / i.bucketSize))
	maxX := int(math.Floor((x + radius) / i.bucketSize))
	minY := int(math.Floor((y - radius) / i.bucketSize))
	maxY := int(math.Floor((y + radius) / i.bucketSize))
	radiusSq := radius * radius
	results := make([]*Unit, 0, 8)
	for by := minY; by <= maxY; by++ {
		for bx := minX; bx <= maxX; bx++ {
			for _, unit := range i.cells[combatSpatialKey{X: bx, Y: by}] {
				if distanceSquared(x, y, unit.X, unit.Y) <= radiusSq {
					results = append(results, unit)
				}
			}
		}
	}
	return results
}

func clamp01(v float64) float64 {
	return clampFloat(v, 0, 1)
}
