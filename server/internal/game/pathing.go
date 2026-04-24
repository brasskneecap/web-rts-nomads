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
