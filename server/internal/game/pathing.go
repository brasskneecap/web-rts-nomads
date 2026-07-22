package game

import (
	"container/heap"
	"math"

	"webrts/server/pkg/protocol"
)

type gridPoint struct {
	X int
	Y int
}

type pathNode struct {
	point    gridPoint
	priority float64
	index    int
}

type priorityQueue []*pathNode

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].priority < pq[j].priority
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x any) {
	node := x.(*pathNode)
	node.index = len(*pq)
	*pq = append(*pq, node)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	node := old[n-1]
	old[n-1] = nil
	node.index = -1
	*pq = old[:n-1]
	return node
}

func (s *GameState) buildBlockedCells() map[gridPoint]bool {
	blocked := make(map[gridPoint]bool)

	addTerrainBlocks(blocked, &s.MapConfig)

	for _, obstacle := range s.MapConfig.Obstacles {
		blocked[gridPoint{X: obstacle.X, Y: obstacle.Y}] = true
	}

	for _, building := range s.MapConfig.Buildings {
		if !building.Visible {
			continue
		}
		// enemy-spawnpoints are logical spawners, not real structures —
		// units must be able to walk freely over them. Maps can leave
		// Visible=true so the editor still renders the marker.
		if building.BuildingType == "enemy-spawnpoint" {
			continue
		}

		for y := building.Y; y < building.Y+building.Height; y++ {
			for x := building.X; x < building.X+building.Width; x++ {
				blocked[gridPoint{X: x, Y: y}] = true
			}
		}
	}

	return blocked
}

// addTerrainBlocks marks every cell whose visually-rendered tile is a Wang
// transition (non-pure) tile as blocked.
//
// Per-cell visual = tiles[] raw override if one exists, otherwise the auto-
// tiled coord computed from terrain[] + defaultTile. A cell is walkable iff
// its visual coord is one of the two "pure interior" Wang slots:
//   (sx=64, sy=32) — mask 15, all 4 corners the "1" state
//   (sx=0,  sy=96) — mask 0,  all 4 corners the "0" state
// These are the same slots in every Wang sheet, so a tiles[] override pointing
// to (0, 96) on a *-elevation-25 sheet is a pure-interior walkable surface even
// if the auto-tile underneath would have rendered a cliff transition.
//
// Mirrors computeWangMask in client terrainTileset.ts; if the algorithm
// changes there, update both. If MapConfig.DefaultTile isn't a recognized
// base-`tileset` grass/dirt coord (e.g. a flat *-elevation-0 sheet), the
// auto-tile pass is skipped — the unpainted ground is uniformly walkable — but
// tiles[] overrides are still evaluated so painted cliffs continue to block.
// Mirrors the client's isTerrainCellBlocked.
func addTerrainBlocks(blocked map[gridPoint]bool, cfg *protocol.MapConfig) {
	gridCols := cfg.GridCols
	gridRows := cfg.GridRows

	// tiles[] overrides decide their own cell regardless of the default ground,
	// so a painted cliff (-25 sheet) blocks even on a flat, walkable default.
	tileOverrides := make(map[gridPoint]protocol.TileCoord, len(cfg.Tiles))
	for _, t := range cfg.Tiles {
		tileOverrides[gridPoint{X: t.X, Y: t.Y}] = t.TileCoord
	}

	// The Wang auto-tiler (and its transition-cliff blocking) only applies when
	// the default ground is a recognized base-`tileset` grass/dirt terrain. For
	// a flat-sheet default (inferDefaultTerrain == ""), the unpainted ground is
	// uniform walkable, so only tile overrides can block.
	defaultTerrain := inferDefaultTerrain(cfg.DefaultTile)
	overrides := make(map[gridPoint]string, len(cfg.Terrain))
	for _, t := range cfg.Terrain {
		if t.X < 0 || t.X >= gridCols || t.Y < 0 || t.Y >= gridRows {
			continue
		}
		overrides[gridPoint{X: t.X, Y: t.Y}] = t.Terrain
	}
	terrainAt := func(x, y int) string {
		if x < 0 || x >= gridCols || y < 0 || y >= gridRows {
			return defaultTerrain
		}
		if t, ok := overrides[gridPoint{X: x, Y: y}]; ok {
			return t
		}
		return defaultTerrain
	}

	for y := 0; y < gridRows; y++ {
		for x := 0; x < gridCols; x++ {
			cell := gridPoint{X: x, Y: y}
			if override, ok := tileOverrides[cell]; ok {
				if !isWalkableGroundTile(override) {
					blocked[cell] = true
				}
				continue
			}
			if defaultTerrain == "" {
				continue // flat-sheet default: unpainted ground is walkable
			}
			mask := computeWangMask(x, y, defaultTerrain, terrainAt)
			if mask != 0 && mask != 15 {
				blocked[cell] = true
			}
		}
	}
}

// isWalkableGroundTile reports whether a tiles[] override renders a flat,
// walkable ground surface (as opposed to a cliff / edge / decoration, which
// blocks movement).
//
//   - flat (-0) elevation sheets: uniform single-terrain (or a flat blend)
//     with no cliffs — every tile is walkable ground.
//   - every other (Wang 4×4) sheet, including the -25 cliff sheets: the two
//     pure-interior slots — (col2,row1) grass and (col0,row3) dirt — are
//     walkable; the cliff/edge tiles block. Mirrors the client's
//     isWalkableGroundTile in terrainTileset.ts — keep the two in sync.
func isWalkableGroundTile(c protocol.TileCoord) bool {
	// Flat (-0) elevation sheets are uniform single-terrain (or a flat blend)
	// with no cliffs — every tile is walkable ground. The matching -25 sheets
	// carry the cliffs and fall through to the interior-slot rule below. Keep
	// in sync with the client's isWalkableGroundTile in terrainTileset.ts.
	switch c.Tileset {
	case "corrupt-corrupt-elevation-0", "dirt-dirt-elevation-0", "dirt-grass-elevation-0",
		"grass-grass-elevation-0", "snow-snow-elevation-0":
		return true
	}
	// Wang sheets: pure-interior slots — (64,32)=col2,row1 grass; (0,96)=col0,row3 dirt.
	if c.Col == 2 && c.Row == 1 {
		return true
	}
	if c.Col == 0 && c.Row == 3 {
		return true
	}
	return false
}

// inferDefaultTerrain reverse-looks-up the canonical pure-tile coords from
// the client's TERRAIN_TILE_COORDS. Returns "" if the default tile is
// custom/unknown.
func inferDefaultTerrain(coord *protocol.TileCoord) string {
	if coord == nil {
		return "grass"
	}
	if coord.Tileset != "tileset" {
		return ""
	}
	switch {
	case coord.Col == 2 && coord.Row == 1:
		return "grass"
	case coord.Col == 0 && coord.Row == 3:
		return "dirt"
	}
	return ""
}

// computeWangMask: bit 0=TL, 1=TR, 2=BL, 3=BR; bit set = corner is grass.
// When grass is the default, a corner is grass iff all 4 surrounding cells
// are grass (overlay dirt expands into corners). When dirt is the default,
// a corner is grass iff any of the 4 is grass (overlay grass expands).
func computeWangMask(cx, cy int, defaultTerrain string, terrainAt func(x, y int) string) int {
	cornerIsGrass := func(ax, ay, bx, by, ccx, ccy, dx, dy int) bool {
		if defaultTerrain == "grass" {
			return terrainAt(ax, ay) == "grass" &&
				terrainAt(bx, by) == "grass" &&
				terrainAt(ccx, ccy) == "grass" &&
				terrainAt(dx, dy) == "grass"
		}
		return terrainAt(ax, ay) == "grass" ||
			terrainAt(bx, by) == "grass" ||
			terrainAt(ccx, ccy) == "grass" ||
			terrainAt(dx, dy) == "grass"
	}

	mask := 0
	if cornerIsGrass(cx-1, cy-1, cx, cy-1, cx-1, cy, cx, cy) {
		mask |= 1
	}
	if cornerIsGrass(cx, cy-1, cx+1, cy-1, cx, cy, cx+1, cy) {
		mask |= 2
	}
	if cornerIsGrass(cx-1, cy, cx, cy, cx-1, cy+1, cx, cy+1) {
		mask |= 4
	}
	if cornerIsGrass(cx, cy, cx+1, cy, cx, cy+1, cx+1, cy+1) {
		mask |= 8
	}
	return mask
}

func (s *GameState) worldToGrid(x, y float64) gridPoint {
	cellSize := s.MapConfig.CellSize
	gridX := int(math.Floor(x / cellSize))
	gridY := int(math.Floor(y / cellSize))

	return s.clampGridPoint(gridPoint{X: gridX, Y: gridY})
}

func (s *GameState) gridToWorldCenter(point gridPoint) protocol.Vec2 {
	cellSize := s.MapConfig.CellSize
	return protocol.Vec2{
		X: (float64(point.X) + 0.5) * cellSize,
		Y: (float64(point.Y) + 0.5) * cellSize,
	}
}

func (s *GameState) clampGridPoint(point gridPoint) gridPoint {
	point.X = maxInt(0, minInt(point.X, s.MapConfig.GridCols-1))
	point.Y = maxInt(0, minInt(point.Y, s.MapConfig.GridRows-1))
	return point
}

func (s *GameState) isWalkable(point gridPoint, blocked map[gridPoint]bool) bool {
	if point.X < 0 || point.Y < 0 || point.X >= s.MapConfig.GridCols || point.Y >= s.MapConfig.GridRows {
		return false
	}

	return !blocked[point]
}

func (s *GameState) findNearestWalkable(start gridPoint, blocked map[gridPoint]bool) (gridPoint, bool) {
	return s.findNearestWalkableAvailable(start, blocked, nil)
}

func (s *GameState) findNearestWalkableAvailable(start gridPoint, blocked map[gridPoint]bool, reserved map[gridPoint]bool) (gridPoint, bool) {
	start = s.clampGridPoint(start)
	if s.isWalkable(start, blocked) && !reserved[start] {
		return start, true
	}

	queue := []gridPoint{start}
	visited := map[gridPoint]bool{start: true}
	directions := []gridPoint{
		{X: 1, Y: 0},
		{X: -1, Y: 0},
		{X: 0, Y: 1},
		{X: 0, Y: -1},
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, direction := range directions {
			next := gridPoint{X: current.X + direction.X, Y: current.Y + direction.Y}
			if visited[next] {
				continue
			}
			if next.X < 0 || next.Y < 0 || next.X >= s.MapConfig.GridCols || next.Y >= s.MapConfig.GridRows {
				continue
			}
			if s.isWalkable(next, blocked) && !reserved[next] {
				return next, true
			}

			visited[next] = true
			queue = append(queue, next)
		}
	}

	return gridPoint{}, false
}

// softBlockedPenalty is the extra A* cost charged for stepping into a cell
// that is "soft-blocked" — currently used for cells occupied by other units.
// The base cost of an orthogonal step is 1 and a diagonal step is √2 ≈ 1.41,
// so a penalty of 2 means the path will route around a single soft-blocked
// cell when a 1-2 cell detour exists, but happily walks through it when
// going around would cost more (long walls of units, narrow corridors).
// Tunable from one place — raise to make units more eager to detour, lower
// to make them more eager to push through.
const softBlockedPenalty = 2.0

func (s *GameState) findPath(start, goal gridPoint, blocked map[gridPoint]bool, softBlocked map[gridPoint]bool) (result []protocol.Vec2) {
	s.debugPathTracker.recordCoarsePath()
	// A failed coarse search exhausted the whole reachable terrain component
	// before returning nil — far costlier than a successful early-terminating
	// one. Pairing the fail counter with the call counter at the same exit
	// keeps coarse=N(fail=M) apples-to-apples in the debug-path report.
	defer func() {
		if len(result) == 0 {
			s.debugPathTracker.recordCoarseFail()
		}
	}()
	start = s.clampGridPoint(start)
	goal = s.clampGridPoint(goal)

	if start == goal {
		center := s.gridToWorldCenter(goal)
		return []protocol.Vec2{center}
	}

	open := &priorityQueue{}
	heap.Init(open)
	heap.Push(open, &pathNode{
		point:    start,
		priority: heuristicCost(start, goal),
	})

	cameFrom := map[gridPoint]gridPoint{}
	gScore := map[gridPoint]float64{start: 0}
	closed := make(map[gridPoint]bool)

	directions := []struct {
		dx   int
		dy   int
		cost float64
	}{
		{dx: 1, dy: 0, cost: 1},
		{dx: -1, dy: 0, cost: 1},
		{dx: 0, dy: 1, cost: 1},
		{dx: 0, dy: -1, cost: 1},
		{dx: 1, dy: 1, cost: math.Sqrt2},
		{dx: 1, dy: -1, cost: math.Sqrt2},
		{dx: -1, dy: 1, cost: math.Sqrt2},
		{dx: -1, dy: -1, cost: math.Sqrt2},
	}

	for open.Len() > 0 {
		current := heap.Pop(open).(*pathNode).point
		if closed[current] {
			continue
		}
		if current == goal {
			return s.reconstructPath(cameFrom, current)
		}

		closed[current] = true

		for _, direction := range directions {
			next := gridPoint{X: current.X + direction.dx, Y: current.Y + direction.dy}
			if !s.isWalkable(next, blocked) {
				continue
			}

			if direction.dx != 0 && direction.dy != 0 {
				sideA := gridPoint{X: current.X + direction.dx, Y: current.Y}
				sideB := gridPoint{X: current.X, Y: current.Y + direction.dy}
				if !s.isWalkable(sideA, blocked) || !s.isWalkable(sideB, blocked) {
					continue
				}
			}

			stepCost := direction.cost
			if softBlocked[next] {
				stepCost += softBlockedPenalty
			}

			tentative := gScore[current] + stepCost
			best, seen := gScore[next]
			if seen && tentative >= best {
				continue
			}

			cameFrom[next] = current
			gScore[next] = tentative
			heap.Push(open, &pathNode{
				point:    next,
				priority: tentative + heuristicCost(next, goal),
			})
		}
	}

	return nil
}

func (s *GameState) reconstructPath(cameFrom map[gridPoint]gridPoint, current gridPoint) []protocol.Vec2 {
	points := []gridPoint{current}

	for {
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		points = append(points, prev)
		current = prev
	}

	path := make([]protocol.Vec2, 0, len(points))
	for i := len(points) - 1; i >= 0; i-- {
		path = append(path, s.gridToWorldCenter(points[i]))
	}

	return path
}

func heuristicCost(a, b gridPoint) float64 {
	dx := math.Abs(float64(a.X - b.X))
	dy := math.Abs(float64(a.Y - b.Y))
	return math.Hypot(dx, dy)
}

// unitPathSubCellSize is the cell size of the finer grid used for unit
// pathfinding. Picked to divide MapConfig.CellSize (typically 64) evenly so
// each terrain cell maps to an integer number of sub-cells. With 16, four
// sub-cells per terrain side give enough resolution to find paths through
// the gap between two adjacent unit obstacles whose separation circles
// (radius 22) don't fully cover the 64-wide cell — the case the coarse A*
// could not see.
const unitPathSubCellSize = 16.0

// unitPathExpansionFactor bounds findUnitPath's A* work. The node-expansion
// budget is unitPathExpansionFactor * (subCols + subRows), so it scales with
// map size. A reachable route's A* expands roughly proportional to its length
// (well under this on realistic RTS layouts); a blocked/unreachable goal would
// otherwise exhaust the entire reachable sub-grid (tens of thousands of nodes)
// before returning nil — the ~70ms single-tick freezes the tick profiler
// caught in objective-advance, wave spawn, and the attack-approach path. When
// the budget is hit the search reports "no route" and the caller falls back to
// its existing unreachable handling (drift / drop+memo / blocking-hostile).
//
// Single tuning knob. Lower → cheaper worst case but risks abandoning long
// but genuinely reachable routes (debug-path `budgetHit` climbing on open
// maps is the regression signal). Raise → safer reachability, larger residual
// worst case. Re-measure with WEBRTS_TICK_PROFILE after changing.
//
// 16 was too low for forest-1: the terrain-only route from the player-2 wave
// spawnpoints (grid 56-64,19) around the tree wall to townhall-38-6 needs
// 9.5k-13.3k expansions vs the 9.2k budget, so every wave marked the townhall
// unreachable army-wide and pooled on the nearest-hostile fallback instead of
// attacking the base. 32 covers the worst measured route with ~38% headroom.
const unitPathExpansionFactor = 32

func (s *GameState) worldToUnitPathSubGrid(x, y float64) gridPoint {
	return gridPoint{
		X: int(math.Floor(x / unitPathSubCellSize)),
		Y: int(math.Floor(y / unitPathSubCellSize)),
	}
}

func (s *GameState) unitPathSubGridToWorldCenter(p gridPoint) protocol.Vec2 {
	return protocol.Vec2{
		X: (float64(p.X) + 0.5) * unitPathSubCellSize,
		Y: (float64(p.Y) + 0.5) * unitPathSubCellSize,
	}
}

func (s *GameState) unitPathSubGridDims() (cols, rows int) {
	return int(math.Ceil(s.MapWidth / unitPathSubCellSize)),
		int(math.Ceil(s.MapHeight / unitPathSubCellSize))
}

func (s *GameState) isUnitPathSubWalkable(p gridPoint, blocked map[gridPoint]bool, cols, rows int) bool {
	if p.X < 0 || p.Y < 0 || p.X >= cols || p.Y >= rows {
		return false
	}
	return !blocked[p]
}

// findNearestUnitPathSubWalkable BFS-finds the closest walkable sub-cell to
// `start`. Used to recover when the mover's start sub-cell or its goal
// sub-cell is blocked (e.g. spawned overlapping a unit obstacle, or asked
// to move onto an occupied position).
func (s *GameState) findNearestUnitPathSubWalkable(start gridPoint, blocked map[gridPoint]bool) (gridPoint, bool) {
	cols, rows := s.unitPathSubGridDims()
	if s.isUnitPathSubWalkable(start, blocked, cols, rows) {
		return start, true
	}

	queue := []gridPoint{start}
	visited := map[gridPoint]bool{start: true}
	directions := []gridPoint{
		{X: 1, Y: 0},
		{X: -1, Y: 0},
		{X: 0, Y: 1},
		{X: 0, Y: -1},
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, d := range directions {
			next := gridPoint{X: current.X + d.X, Y: current.Y + d.Y}
			if visited[next] {
				continue
			}
			if next.X < 0 || next.Y < 0 || next.X >= cols || next.Y >= rows {
				continue
			}
			if !blocked[next] {
				return next, true
			}
			visited[next] = true
			queue = append(queue, next)
		}
	}
	return gridPoint{}, false
}

// findUnitPath runs A* on the fine sub-cell grid used for unit movement.
// The sub-grid resolution lets the path slip through gaps between unit
// obstacles that are smaller than the coarse 64-cell terrain grid — e.g.
// two units in adjacent terrain cells leaving a sub-cell-wide corridor
// between their separation circles. Returns world-space waypoints; the
// caller is expected to simplify (line-of-sight collapse) before assigning.
func (s *GameState) findUnitPath(start, goal gridPoint, blocked map[gridPoint]bool) (result []protocol.Vec2) {
	s.debugPathTracker.recordFinePath()
	// The fine sub-cell grid is ~15k cells; a failed search here (no route
	// through the unit-obstacle field — the "chased unit threading a blob"
	// case) is the single most expensive pathing op in the sim. Count it.
	defer func() {
		if len(result) == 0 {
			s.debugPathTracker.recordFineFail()
		}
	}()
	cols, rows := s.unitPathSubGridDims()

	if start == goal {
		return []protocol.Vec2{s.unitPathSubGridToWorldCenter(goal)}
	}

	open := &priorityQueue{}
	heap.Init(open)
	heap.Push(open, &pathNode{
		point:    start,
		priority: heuristicCost(start, goal),
	})

	cameFrom := map[gridPoint]gridPoint{}
	gScore := map[gridPoint]float64{start: 0}
	closed := make(map[gridPoint]bool)

	// Bound the search: a blocked/unreachable goal would otherwise settle the
	// entire reachable sub-grid before returning nil (~70ms). Treat budget
	// exhaustion exactly like "no route" — callers already handle that.
	maxExpansions := unitPathExpansionFactor * (cols + rows)
	expansions := 0

	directions := []struct {
		dx   int
		dy   int
		cost float64
	}{
		{dx: 1, dy: 0, cost: 1},
		{dx: -1, dy: 0, cost: 1},
		{dx: 0, dy: 1, cost: 1},
		{dx: 0, dy: -1, cost: 1},
		{dx: 1, dy: 1, cost: math.Sqrt2},
		{dx: 1, dy: -1, cost: math.Sqrt2},
		{dx: -1, dy: 1, cost: math.Sqrt2},
		{dx: -1, dy: -1, cost: math.Sqrt2},
	}

	for open.Len() > 0 {
		current := heap.Pop(open).(*pathNode).point
		if closed[current] {
			continue
		}
		if current == goal {
			points := []gridPoint{current}
			for {
				prev, ok := cameFrom[current]
				if !ok {
					break
				}
				points = append(points, prev)
				current = prev
			}
			path := make([]protocol.Vec2, 0, len(points))
			for i := len(points) - 1; i >= 0; i-- {
				path = append(path, s.unitPathSubGridToWorldCenter(points[i]))
			}
			return path
		}

		closed[current] = true

		expansions++
		if expansions >= maxExpansions {
			s.debugPathTracker.recordUnitPathBudgetHit()
			return nil
		}

		for _, direction := range directions {
			next := gridPoint{X: current.X + direction.dx, Y: current.Y + direction.dy}
			if !s.isUnitPathSubWalkable(next, blocked, cols, rows) {
				continue
			}

			if direction.dx != 0 && direction.dy != 0 {
				sideA := gridPoint{X: current.X + direction.dx, Y: current.Y}
				sideB := gridPoint{X: current.X, Y: current.Y + direction.dy}
				if !s.isUnitPathSubWalkable(sideA, blocked, cols, rows) || !s.isUnitPathSubWalkable(sideB, blocked, cols, rows) {
					continue
				}
			}

			tentative := gScore[current] + direction.cost
			if best, seen := gScore[next]; seen && tentative >= best {
				continue
			}

			cameFrom[next] = current
			gScore[next] = tentative
			heap.Push(open, &pathNode{
				point:    next,
				priority: tentative + heuristicCost(next, goal),
			})
		}
	}

	return nil
}

// simplifyUnitPath collapses sequential waypoints whose endpoints have
// direct line-of-sight on the sub-cell blocked map. Reduces an A* output
// of dozens of 16-pixel hops to a handful of corner points so the wire
// payload and the per-tick waypoint advancement stay cheap. Operates in
// world-space directly — samples the segment at sub-cell resolution and
// rejects any waypoint whose collapsed segment crosses a blocked sub-cell.
func (s *GameState) simplifyUnitPath(path []protocol.Vec2, blocked map[gridPoint]bool) []protocol.Vec2 {
	if len(path) < 3 {
		return path
	}

	out := make([]protocol.Vec2, 0, len(path))
	out = append(out, path[0])
	cur := 0
	for cur < len(path)-1 {
		far := cur + 1
		for j := cur + 2; j < len(path); j++ {
			if !s.unitPathSegmentClear(path[cur], path[j], blocked) {
				break
			}
			far = j
		}
		out = append(out, path[far])
		cur = far
	}
	return out
}

func (s *GameState) unitPathSegmentClear(a, b protocol.Vec2, blocked map[gridPoint]bool) bool {
	dx := b.X - a.X
	dy := b.Y - a.Y
	dist := math.Hypot(dx, dy)
	if dist < 1 {
		return true
	}
	// Half-sub-cell sampling guarantees we never skip over a blocked cell
	// on the segment between two waypoints.
	steps := int(math.Ceil(dist / (unitPathSubCellSize * 0.5)))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := a.X + dx*t
		y := a.Y + dy*t
		if blocked[s.worldToUnitPathSubGrid(x, y)] {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
