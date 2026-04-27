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
// These are the same slots in every Wang sheet (grass-dirt, grass-grass-25,
// dirt-dirt-25), so a tiles[] override pointing to (0, 96) on
// grass-dirt-elevation-25.png is a pure-dirt walkable surface even if the
// auto-tile underneath would have rendered a cliff transition.
//
// Mirrors computeWangMask in client terrainTileset.ts; if the algorithm
// changes there, update both. If MapConfig.DefaultTile isn't one of the two
// recognized canonical coords (grass or dirt pure), no terrain blocks are
// added — the map is treated as if everything is walkable, matching the
// editor's "no auto-tile" fallback path.
func addTerrainBlocks(blocked map[gridPoint]bool, cfg *protocol.MapConfig) {
	defaultTerrain := inferDefaultTerrain(cfg.DefaultTile)
	if defaultTerrain == "" {
		return
	}

	gridCols := cfg.GridCols
	gridRows := cfg.GridRows

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

	tileOverrides := make(map[gridPoint]protocol.TileCoord, len(cfg.Tiles))
	for _, t := range cfg.Tiles {
		tileOverrides[gridPoint{X: t.X, Y: t.Y}] = t.TileCoord
	}

	for y := 0; y < gridRows; y++ {
		for x := 0; x < gridCols; x++ {
			cell := gridPoint{X: x, Y: y}
			if override, ok := tileOverrides[cell]; ok {
				if !isPureWangTileCoord(override) {
					blocked[cell] = true
				}
				continue
			}
			mask := computeWangMask(x, y, defaultTerrain, terrainAt)
			if mask != 0 && mask != 15 {
				blocked[cell] = true
			}
		}
	}
}

// isPureWangTileCoord returns true for the two interior cells in any Wang
// 4×4 sheet: (64,32) is the "all-1-corners" tile (pure grass / pure high
// terrain) and (0,96) is the "all-0-corners" tile (pure dirt / pure low
// terrain). Both represent flat walkable surfaces regardless of which sheet
// the override points at.
func isPureWangTileCoord(c protocol.TileCoord) bool {
	if c.SX == 64 && c.SY == 32 {
		return true
	}
	if c.SX == 0 && c.SY == 96 {
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
	if coord.Sheet != "tileset" && coord.Sheet != "grass-dirt-0" {
		return ""
	}
	switch {
	case coord.SX == 64 && coord.SY == 32:
		return "grass"
	case coord.SX == 0 && coord.SY == 96:
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

func (s *GameState) findPath(start, goal gridPoint, blocked map[gridPoint]bool) []protocol.Vec2 {
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

			tentative := gScore[current] + direction.cost
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
