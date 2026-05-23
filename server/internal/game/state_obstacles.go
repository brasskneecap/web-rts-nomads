package game

import (
	"webrts/server/pkg/protocol"
)

// getObstacleByIDLocked returns the obstacle tile with the given id, or nil if
// none exists. Must be called under s.mu read or write lock.
func (s *GameState) getObstacleByIDLocked(id string) *protocol.ObstacleTile {
	if id == "" {
		return nil
	}
	return s.obstaclesByID[id]
}

// removeObstacleByIDLocked drops the obstacle from s.MapConfig.Obstacles,
// unregisters it from s.obstaclesByID, and invalidates the blocked-cells
// cache. Safe to call for an unknown id.
// Must be called under s.mu write lock.
func (s *GameState) removeObstacleByIDLocked(id string) {
	if id == "" {
		return
	}
	if _, ok := s.obstaclesByID[id]; !ok {
		return
	}
	delete(s.obstaclesByID, id)
	filtered := make([]protocol.ObstacleTile, 0, len(s.MapConfig.Obstacles)-1)
	for _, o := range s.MapConfig.Obstacles {
		if o.ID != id {
			filtered = append(filtered, o)
		}
	}
	s.MapConfig.Obstacles = filtered
	// Re-index because the slice backing array may have moved.
	for i := range s.MapConfig.Obstacles {
		o := &s.MapConfig.Obstacles[i]
		if o.ID == "" {
			continue
		}
		s.obstaclesByID[o.ID] = o
	}
	s.invalidateBlockedCellsLocked()
}

// refreshObstacleRuntimeMetadataLocked publishes transient, per-tick fields
// (e.g. worker occupancy on tree obstacles) into obstacle metadata so the
// client HUD can display them. Mirrors refreshBuildingRuntimeMetadataLocked
// for the obstacle path. Must be called under s.mu write lock.
func (s *GameState) refreshObstacleRuntimeMetadataLocked() {
	for i := range s.MapConfig.Obstacles {
		o := &s.MapConfig.Obstacles[i]
		if o.Obstacle != "tree" {
			continue
		}
		if o.Metadata == nil {
			o.Metadata = map[string]interface{}{}
		}
		o.Metadata["currentWorkers"] = s.countWorkersInsideResourceNodeLocked(o.ID)
		o.Metadata["maxWorkers"] = treeWorkerCap
	}
}

// countWorkersInsideResourceNodeLocked returns the number of units currently
// chopping/mining the given resource node id (obstacle or building). O(1) —
// reads s.workersInsideResource, which is maintained incrementally by
// setUnitMiningInsideLocked at every MiningInside flip plus removeUnitByID-
// Locked for dying miners. The historical per-tick s.Units scan added ~1.5ms
// on combat ticks for a UI tooltip; this lookup is constant-time.
func (s *GameState) countWorkersInsideResourceNodeLocked(id string) int {
	if id == "" || s.workersInsideResource == nil {
		return 0
	}
	return s.workersInsideResource[id]
}

// resourceNode is an internal, pointer-backed view over either an ObstacleTile
// or a BuildingTile that exposes the fields the gather pipeline needs. The
// pipeline never copies a resourceNode by value because it carries a pointer
// back to the underlying tile; mutations through the pointer write back to
// MapConfig directly. When the node's ResourceAmount hits zero, the pipeline
// removes the backing entity via the appropriate remove*ByIDLocked helper.
type resourceNode struct {
	ID             string
	X, Y           int
	Width, Height  int
	ResourceType   string
	ResourceAmount int
	Capabilities   []string
	IsObstacle     bool
	// obstacle and building are pointers into MapConfig; exactly one is non-nil.
	obstacle *protocol.ObstacleTile
	building *protocol.BuildingTile
}

func resourceNodeFromObstacle(o *protocol.ObstacleTile) *resourceNode {
	if o == nil {
		return nil
	}
	return &resourceNode{
		ID:             o.ID,
		X:              o.X,
		Y:              o.Y,
		Width:          maxInt(1, o.Width),
		Height:         maxInt(1, o.Height),
		ResourceType:   o.ResourceType,
		ResourceAmount: o.ResourceAmount,
		Capabilities:   o.Capabilities,
		IsObstacle:     true,
		obstacle:       o,
	}
}

func resourceNodeFromBuilding(b *protocol.BuildingTile) *resourceNode {
	if b == nil {
		return nil
	}
	return &resourceNode{
		ID:             b.ID,
		X:              b.X,
		Y:              b.Y,
		Width:          maxInt(1, b.Width),
		Height:         maxInt(1, b.Height),
		ResourceType:   b.ResourceType,
		ResourceAmount: b.ResourceAmount,
		Capabilities:   b.Capabilities,
		IsObstacle:     false,
		building:       b,
	}
}

// IsTree returns true when the underlying entity is a tree obstacle. Gather
// logic uses this to toggle chopping vs mining semantics (worker cap, chop
// duration, visibility on mining, redirect-to-next-tree on depletion).
func (n *resourceNode) IsTree() bool {
	return n != nil && n.IsObstacle && n.obstacle != nil && n.obstacle.Obstacle == "tree"
}

// IsGoldmine returns true for goldmine buildings. Kept as an explicit check
// because gather visuals and worker cap differ from tree nodes.
func (n *resourceNode) IsGoldmine() bool {
	return n != nil && !n.IsObstacle && n.building != nil && n.building.BuildingType == "goldmine"
}

// consumeResource subtracts delta from the backing entity's resource pool and
// returns the amount actually consumed (clamped to the remaining pool). The
// caller is responsible for removing the entity when the pool reaches zero.
func (n *resourceNode) consumeResource(delta int) int {
	if n == nil || delta <= 0 {
		return 0
	}
	if n.IsObstacle && n.obstacle != nil {
		taken := delta
		if taken > n.obstacle.ResourceAmount {
			taken = n.obstacle.ResourceAmount
		}
		n.obstacle.ResourceAmount -= taken
		n.ResourceAmount = n.obstacle.ResourceAmount
		return taken
	}
	if n.building != nil {
		taken := delta
		if taken > n.building.ResourceAmount {
			taken = n.building.ResourceAmount
		}
		n.building.ResourceAmount -= taken
		n.ResourceAmount = n.building.ResourceAmount
		return taken
	}
	return 0
}

// asBuildingTile exposes the node as a synthetic BuildingTile for helpers that
// expect a BuildingTile (approach-position finder, unit-near test, exit
// position). Only the geometry fields are populated. Callers must not mutate
// the returned tile — it is a value copy.
func (n *resourceNode) asBuildingTile() protocol.BuildingTile {
	if n == nil {
		return protocol.BuildingTile{}
	}
	return protocol.BuildingTile{
		GridCoord: protocol.GridCoord{X: n.X, Y: n.Y},
		ID:        n.ID,
		Width:     n.Width,
		Height:    n.Height,
	}
}

// getResourceNodeByIDLocked resolves id against both obstacles and buildings,
// returning a resourceNode view or nil when not found. Obstacles are checked
// first because trees (the only obstacle that currently carries resources)
// are the expected hot path.
func (s *GameState) getResourceNodeByIDLocked(id string) *resourceNode {
	if id == "" {
		return nil
	}
	if obs := s.getObstacleByIDLocked(id); obs != nil {
		return resourceNodeFromObstacle(obs)
	}
	if b := s.getBuildingByIDLocked(id); b != nil {
		return resourceNodeFromBuilding(b)
	}
	return nil
}

// removeResourceNodeLocked removes the backing entity for the given node from
// the map. Use when a resource pool has been fully depleted.
func (s *GameState) removeResourceNodeLocked(n *resourceNode) {
	if n == nil {
		return
	}
	if n.IsObstacle {
		s.removeObstacleByIDLocked(n.ID)
	} else {
		s.removeBuildingByIDLocked(n.ID)
	}
}
