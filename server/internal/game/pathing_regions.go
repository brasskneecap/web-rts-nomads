package game

// Walkable-region connectivity index.
//
// The blocked-cells map says whether a single coarse cell is walkable; it says
// nothing about whether that cell is CONNECTED to anywhere useful. Spawn
// placement that only checks per-cell walkability can drop a unit into a
// sealed pocket (a walkable cell fully enclosed by trees/buildings) — the unit
// then stands there for the rest of the match because every path out fails.
//
// This index labels each walkable cell with the ID of its 4-connected
// component so placement code can ask "is this cell in the same region as my
// anchor?" in O(1). 4-connectivity matches the fine A*'s no-corner-cutting
// rule (a diagonal step is rejected when both orthogonal side cells are
// blocked), so two regions this index considers separate are genuinely not
// walkable between.
//
// Rebuilt lazily from the same source as the blocked-cells cache and
// invalidated by the same hook (invalidateBlockedCellsLocked), so the two can
// never disagree. Build cost is one O(gridCols×gridRows) flood fill — a few
// thousand cells on shipped maps — paid only when a building or obstacle
// actually changes.
type walkableRegions struct {
	// regionOf maps each walkable cell to its 1-based region ID. Blocked or
	// out-of-bounds cells are absent (lookup yields 0).
	regionOf map[gridPoint]int
	// sizes[id] is the cell count of region id; index 0 is unused.
	sizes []int
}

// buildWalkableRegions flood-fills the coarse grid into 4-connected walkable
// components. Region IDs are assigned in row-major discovery order, so the
// labeling is deterministic for a given blocked map.
func (s *GameState) buildWalkableRegions(blocked map[gridPoint]bool) *walkableRegions {
	cols, rows := s.MapConfig.GridCols, s.MapConfig.GridRows
	wr := &walkableRegions{
		regionOf: make(map[gridPoint]int),
		sizes:    []int{0},
	}
	directions := []gridPoint{{X: 1, Y: 0}, {X: -1, Y: 0}, {X: 0, Y: 1}, {X: 0, Y: -1}}

	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			seed := gridPoint{X: x, Y: y}
			if blocked[seed] || wr.regionOf[seed] != 0 {
				continue
			}
			id := len(wr.sizes)
			wr.sizes = append(wr.sizes, 0)
			queue := []gridPoint{seed}
			wr.regionOf[seed] = id
			for len(queue) > 0 {
				current := queue[0]
				queue = queue[1:]
				wr.sizes[id]++
				for _, d := range directions {
					next := gridPoint{X: current.X + d.X, Y: current.Y + d.Y}
					if next.X < 0 || next.Y < 0 || next.X >= cols || next.Y >= rows {
						continue
					}
					if blocked[next] || wr.regionOf[next] != 0 {
						continue
					}
					wr.regionOf[next] = id
					queue = append(queue, next)
				}
			}
		}
	}
	return wr
}

// getWalkableRegionsLocked returns the cached region index, rebuilding it if
// stale. Staleness is keyed to the blocked-cells cache: invalidateBlocked-
// CellsLocked clears both. Must be called under s.mu lock.
func (s *GameState) getWalkableRegionsLocked() *walkableRegions {
	if s.walkableRegionsCache == nil || !s.blockedCellsValid {
		s.walkableRegionsCache = s.buildWalkableRegions(s.getBlockedCellsLocked())
	}
	return s.walkableRegionsCache
}

// walkableRegionAtLocked returns the region ID of the given cell, or 0 when
// the cell is blocked or out of bounds. Must be called under s.mu lock.
func (s *GameState) walkableRegionAtLocked(cell gridPoint) int {
	return s.getWalkableRegionsLocked().regionOf[cell]
}

// walkableRegionSizeLocked returns the cell count of the given region, or 0
// for an unknown ID. Must be called under s.mu lock.
func (s *GameState) walkableRegionSizeLocked(regionID int) int {
	wr := s.getWalkableRegionsLocked()
	if regionID <= 0 || regionID >= len(wr.sizes) {
		return 0
	}
	return wr.sizes[regionID]
}

// findNearestWalkableInRegionLocked is the placement variant of
// findNearestWalkableAvailable: it returns the nearest walkable, unreserved
// cell that belongs to the given region, so spawn/release code can guarantee
// the chosen cell is connected to its anchor (rally point, spawnpoint,
// camp center) instead of a sealed pocket the plain BFS would tunnel into.
// regionID 0 means "no anchor known" and degrades to the unconstrained
// search. Must be called under s.mu lock.
func (s *GameState) findNearestWalkableInRegionLocked(start gridPoint, regionID int, blocked map[gridPoint]bool, reserved map[gridPoint]bool) (gridPoint, bool) {
	if regionID == 0 {
		return s.findNearestWalkableAvailable(start, blocked, reserved)
	}
	wr := s.getWalkableRegionsLocked()

	start = s.clampGridPoint(start)
	if s.isWalkable(start, blocked) && !reserved[start] && wr.regionOf[start] == regionID {
		return start, true
	}

	queue := []gridPoint{start}
	visited := map[gridPoint]bool{start: true}
	directions := []gridPoint{{X: 1, Y: 0}, {X: -1, Y: 0}, {X: 0, Y: 1}, {X: 0, Y: -1}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, d := range directions {
			next := gridPoint{X: current.X + d.X, Y: current.Y + d.Y}
			if visited[next] {
				continue
			}
			if next.X < 0 || next.Y < 0 || next.X >= s.MapConfig.GridCols || next.Y >= s.MapConfig.GridRows {
				continue
			}
			if s.isWalkable(next, blocked) && !reserved[next] && wr.regionOf[next] == regionID {
				return next, true
			}
			visited[next] = true
			queue = append(queue, next)
		}
	}
	return gridPoint{}, false
}

// bestSpawnRegionLocked picks, among candidate cells, the region a spawn batch
// should be constrained to: the represented region with the largest total
// size (ties break to the lower region ID for determinism). Cells with no
// region (blocked) are ignored. Returns 0 when no candidate has a region.
//
// "Largest represented region" is deliberately threshold-free: on an open map
// it picks the main field; inside a player-walled base it picks the base
// interior (the wall makes the interior its own region, and spawning inside
// is the intended behavior); a 1-2 cell sealed pocket loses to either.
func (s *GameState) bestSpawnRegionLocked(candidates []gridPoint) int {
	wr := s.getWalkableRegionsLocked()
	best := 0
	bestSize := -1
	for _, c := range candidates {
		id := wr.regionOf[c]
		if id == 0 {
			continue
		}
		size := wr.sizes[id]
		if size > bestSize || (size == bestSize && id < best) {
			best = id
			bestSize = size
		}
	}
	return best
}
