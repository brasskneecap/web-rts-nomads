package game

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"webrts/server/pkg/protocol"
)

type Unit struct {
	ID      int
	OwnerID string
	Color   string
	X       float64
	Y       float64
	HP      int
	MaxHP   int

	TargetX float64
	TargetY float64
	Moving  bool
	Path    []protocol.Vec2
	OrderID int64
}

const (
	unitMoveSpeed          = 100.0
	unitRadius             = 10.0
	unitFormationSpacing   = 28.0
	unitSeparationDistance = 22.0
)

type Player struct {
	ID    string
	Color string
}

type GameState struct {
	mu sync.RWMutex

	Tick int

	MapConfig protocol.MapConfig
	MapID     string
	MapWidth  float64
	MapHeight float64

	Units   []*Unit
	Players map[string]*Player

	nextUnitID int
	nextOrderID int64
	rng        *rand.Rand
}

func NewGameState(mapConfig protocol.MapConfig) *GameState {
	state := &GameState{
		Units:      []*Unit{},
		Players:    map[string]*Player{},
		nextUnitID: 1,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	state.SetMapConfig(mapConfig)
	return state
}

func (s *GameState) SetMapConfig(mapConfig protocol.MapConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setMapConfigLocked(mapConfig)
}

func (s *GameState) setMapConfigLocked(mapConfig protocol.MapConfig) {
	s.MapConfig = cloneMapConfig(mapConfig)
	s.MapID = s.MapConfig.ID
	s.MapWidth = s.MapConfig.Width
	s.MapHeight = s.MapConfig.Height
}

func (s *GameState) GetMapConfig() protocol.MapConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.MapConfig
}

func (s *GameState) Snapshot() protocol.MatchSnapshotMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	units := make([]protocol.UnitSnapshot, 0, len(s.Units))
	for _, unit := range s.Units {
		snapshot := protocol.UnitSnapshot{
			ID:      unit.ID,
			OwnerID: unit.OwnerID,
			Color:   unit.Color,
			X:       unit.X,
			Y:       unit.Y,
			HP:      unit.HP,
			MaxHP:   unit.MaxHP,
			Moving:  unit.Moving,
		}

		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}

		units = append(units, snapshot)
	}

	return protocol.MatchSnapshotMessage{
		Type:      "match_snapshot",
		Tick:      s.Tick,
		ServerNow: time.Now().UnixMilli(),
		Map:       s.MapConfig,
		Units: units,
	}
}

func (s *GameState) IncrementTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tick++
}

func (s *GameState) Update(dt float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blocked := s.buildBlockedCells()

	for _, unit := range s.Units {
		if !unit.Moving {
			continue
		}

		if len(unit.Path) == 0 {
			unit.Moving = false
			continue
		}

		nextWaypoint := unit.Path[0]
		dx := nextWaypoint.X - unit.X
		dy := nextWaypoint.Y - unit.Y
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist == 0 {
			unit.X = nextWaypoint.X
			unit.Y = nextWaypoint.Y
			unit.Path = unit.Path[1:]
			unit.Moving = len(unit.Path) > 0
			continue
		}

		step := unitMoveSpeed * dt
		if step >= dist {
			unit.X = nextWaypoint.X
			unit.Y = nextWaypoint.Y
			unit.Path = unit.Path[1:]
			unit.Moving = len(unit.Path) > 0
			continue
		}

		nextX := unit.X + (dx/dist)*step
		nextY := unit.Y + (dy/dist)*step
		nextCell := s.worldToGrid(nextX, nextY)
		if !s.isWalkable(nextCell, blocked) {
			if !s.repathUnitLocked(unit, blocked) {
				unit.Path = nil
				unit.Moving = false
			}
			continue
		}

		unit.X = nextX
		unit.Y = nextY
	}

	s.applyUnitSeparationLocked(blocked)
}

func (s *GameState) MoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validUnits := make([]*Unit, 0, len(unitIDs))
	unitMap := make(map[int]*Unit, len(s.Units))
	blocked := s.buildBlockedCells()

	for _, unit := range s.Units {
		unitMap[unit.ID] = unit
	}

	for _, unitID := range unitIDs {
		unit, ok := unitMap[unitID]
		if !ok {
			continue
		}
		if unit.OwnerID != playerID {
			continue
		}
		validUnits = append(validUnits, unit)
	}

	if len(validUnits) == 0 {
		return
	}

	if len(validUnits) == 1 {
		unit := validUnits[0]
		orderID := s.nextMovementOrderIDLocked()
		s.resetUnitMovementLocked(unit, orderID)
		s.assignUnitPath(unit, dest, blocked, nil)
		return
	}

	clampedDest := protocol.Vec2{
		X: clampFloat(dest.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(dest.Y, unitRadius, s.MapHeight-unitRadius),
	}
	anchorGoal := s.worldToGrid(clampedDest.X, clampedDest.Y)
	anchorCell, ok := s.findNearestWalkable(anchorGoal, blocked)
	if !ok {
		return
	}

	anchor := s.clampPointToCell(clampedDest, anchorCell)
	targets := buildFormationTargets(validUnits, anchor, unitFormationSpacing)
	orderID := s.nextMovementOrderIDLocked()

	for i, unit := range validUnits {
		target := targets[i]
		s.resetUnitMovementLocked(unit, orderID)

		s.assignUnitPath(unit, protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}, blocked, nil)
	}
}

func (s *GameState) EnsurePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.Players[playerID]; exists {
		return
	}

	color := s.randomColor()
	s.Players[playerID] = &Player{
		ID:    playerID,
		Color: color,
	}

	home := s.claimTownhallForPlayerLocked(playerID)
	s.spawnUnitsForPlayerLocked(playerID, color, 5, home)
}

func (s *GameState) RemovePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Players, playerID)

	filtered := make([]*Unit, 0, len(s.Units))
	for _, unit := range s.Units {
		if unit.OwnerID != playerID {
			filtered = append(filtered, unit)
		}
	}

	s.Units = filtered
	s.releaseTownhallForPlayerLocked(playerID)
}

func (s *GameState) spawnUnitsForPlayerLocked(playerID, color string, count int, home *protocol.BuildingTile) {
	if count <= 0 {
		return
	}

	playerIndex := len(s.Players) - 1
	blocked := s.buildBlockedCells()
	spawnPositions := make([]protocol.Vec2, 0, count)

	if home != nil {
		spawnPositions = s.getTownhallSpawnPositionsLocked(*home, count, blocked)
	}

	if len(spawnPositions) < count {
		spawnPositions = append(spawnPositions, s.getFallbackSpawnPositionsLocked(playerIndex, count-len(spawnPositions), blocked)...)
	}
	if len(spawnPositions) == 0 {
		return
	}

	for i := 0; i < count; i++ {
		spawn := spawnPositions[minInt(i, len(spawnPositions)-1)]

		unit := &Unit{
			ID:      s.nextUnitID,
			OwnerID: playerID,
			Color:   color,
			X:       spawn.X,
			Y:       spawn.Y,
			HP:      100,
			MaxHP:   100,
		}

		s.nextUnitID++
		s.Units = append(s.Units, unit)
	}
}

func (s *GameState) assignUnitPath(unit *Unit, dest protocol.Vec2, blocked map[gridPoint]bool, reservedGoals map[gridPoint]bool) {
	clampedDest := protocol.Vec2{
		X: clampFloat(dest.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(dest.Y, unitRadius, s.MapHeight-unitRadius),
	}

	start := s.worldToGrid(unit.X, unit.Y)
	resolvedStart, ok := s.findNearestWalkable(start, blocked)
	if ok {
		start = resolvedStart
	}
	goal := s.worldToGrid(clampedDest.X, clampedDest.Y)

	resolvedGoal, ok := s.findNearestWalkableAvailable(goal, blocked, reservedGoals)
	if !ok {
		unit.Path = nil
		unit.Moving = false
		return
	}

	path := s.findPath(start, resolvedGoal, blocked)
	if len(path) == 0 {
		unit.Path = nil
		unit.Moving = false
		return
	}

	if len(path) > 0 && distanceSquared(unit.X, unit.Y, path[0].X, path[0].Y) < 4 {
		path = path[1:]
	}

	if firstStep := s.buildPathEntryPoint(unit, start); firstStep != nil {
		path = append([]protocol.Vec2{*firstStep}, path...)
	}

	finalTarget := s.clampPointToCell(clampedDest, resolvedGoal)
	if len(path) == 0 {
		path = []protocol.Vec2{finalTarget}
	} else {
		path[len(path)-1] = finalTarget
	}
	path = simplifyLeadingWaypoints(unit, path, finalTarget)

	if reservedGoals != nil {
		reservedGoals[resolvedGoal] = true
	}

	unit.TargetX = finalTarget.X
	unit.TargetY = finalTarget.Y
	unit.Path = path
	unit.Moving = len(path) > 0
}

func (s *GameState) repathUnitLocked(unit *Unit, blocked map[gridPoint]bool) bool {
	if !unit.Moving {
		return false
	}

	dest := protocol.Vec2{X: unit.TargetX, Y: unit.TargetY}
	s.assignUnitPath(unit, dest, blocked, nil)
	return unit.Moving
}

func (s *GameState) claimTownhallForPlayerLocked(playerID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" {
			continue
		}
		if building.OwnerID != nil && *building.OwnerID == playerID {
			building.Occupied = true
			building.Visible = true
			return building
		}
	}

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" || building.Occupied {
			continue
		}

		ownerID := playerID
		building.OwnerID = &ownerID
		building.Occupied = true
		building.Visible = true
		return building
	}

	return nil
}

func (s *GameState) releaseTownhallForPlayerLocked(playerID string) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" || building.OwnerID == nil || *building.OwnerID != playerID {
			continue
		}

		building.OwnerID = nil
		building.Occupied = false
		building.Visible = false
	}
}

func (s *GameState) getTownhallSpawnPositionsLocked(home protocol.BuildingTile, count int, blocked map[gridPoint]bool) []protocol.Vec2 {
	if count <= 0 {
		return nil
	}

	homeCenter := protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}
	candidates := make([]gridPoint, 0, (home.Width+2)*(home.Height+2))
	seen := make(map[gridPoint]bool)

	for y := home.Y - 1; y <= home.Y+home.Height; y++ {
		for x := home.X - 1; x <= home.X+home.Width; x++ {
			isPerimeter := x == home.X-1 || x == home.X+home.Width || y == home.Y-1 || y == home.Y+home.Height
			if !isPerimeter {
				continue
			}

			cell := gridPoint{X: x, Y: y}
			if seen[cell] || !s.isWalkable(cell, blocked) {
				continue
			}

			seen[cell] = true
			candidates = append(candidates, cell)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		a := s.gridToWorldCenter(candidates[i])
		b := s.gridToWorldCenter(candidates[j])
		return distanceSquared(a.X, a.Y, homeCenter.X, homeCenter.Y) < distanceSquared(b.X, b.Y, homeCenter.X, homeCenter.Y)
	})

	positions := make([]protocol.Vec2, 0, minInt(count, len(candidates)))
	for _, cell := range candidates {
		if len(positions) >= count {
			break
		}

		cellCenter := s.gridToWorldCenter(cell)
		offsetX := cellCenter.X - homeCenter.X
		offsetY := cellCenter.Y - homeCenter.Y
		dist := math.Hypot(offsetX, offsetY)
		if dist > 0 {
			scale := math.Min(s.MapConfig.CellSize*0.18, dist)
			cellCenter.X += (offsetX / dist) * scale
			cellCenter.Y += (offsetY / dist) * scale
		}

		positions = append(positions, protocol.Vec2{
			X: clampFloat(cellCenter.X, unitRadius, s.MapWidth-unitRadius),
			Y: clampFloat(cellCenter.Y, unitRadius, s.MapHeight-unitRadius),
		})
	}

	return positions
}

func (s *GameState) getFallbackSpawnPositionsLocked(playerIndex, count int, blocked map[gridPoint]bool) []protocol.Vec2 {
	paddingX := 220.0
	paddingY := 220.0
	spawnBlockWidth := 260.0
	spawnBlockHeight := 220.0

	spawnsPerRow := int(math.Max(1, math.Floor((s.MapWidth-paddingX*2)/spawnBlockWidth)))
	colIndex := playerIndex % spawnsPerRow
	rowIndex := playerIndex / spawnsPerRow

	baseX := paddingX + float64(colIndex)*spawnBlockWidth
	baseY := paddingY + float64(rowIndex)*spawnBlockHeight

	baseX = math.Min(baseX, s.MapWidth-180)
	baseY = math.Min(baseY, s.MapHeight-180)

	cols := int(math.Ceil(math.Sqrt(float64(count))))
	reserved := make(map[gridPoint]bool, count)
	positions := make([]protocol.Vec2, 0, count)

	for i := 0; i < count; i++ {
		col := i % cols
		row := i / cols

		target := protocol.Vec2{
			X: baseX + float64(col)*unitFormationSpacing,
			Y: baseY + float64(row)*unitFormationSpacing,
		}

		spawnCell, ok := s.findNearestWalkableAvailable(s.worldToGrid(target.X, target.Y), blocked, reserved)
		if !ok {
			continue
		}

		reserved[spawnCell] = true
		positions = append(positions, s.clampPointToCell(target, spawnCell))
	}

	return positions
}

func (s *GameState) clampPointToCell(point protocol.Vec2, cell gridPoint) protocol.Vec2 {
	cellMinX := float64(cell.X) * s.MapConfig.CellSize
	cellMinY := float64(cell.Y) * s.MapConfig.CellSize
	cellMaxX := cellMinX + s.MapConfig.CellSize
	cellMaxY := cellMinY + s.MapConfig.CellSize

	minX := cellMinX + unitRadius
	maxX := cellMaxX - unitRadius
	minY := cellMinY + unitRadius
	maxY := cellMaxY - unitRadius

	if minX > maxX {
		minX = (cellMinX + cellMaxX) / 2
		maxX = minX
	}
	if minY > maxY {
		minY = (cellMinY + cellMaxY) / 2
		maxY = minY
	}

	return protocol.Vec2{
		X: clampFloat(point.X, minX, maxX),
		Y: clampFloat(point.Y, minY, maxY),
	}
}

func (s *GameState) buildPathEntryPoint(unit *Unit, start gridPoint) *protocol.Vec2 {
	entryPoint := s.clampPointToCell(protocol.Vec2{X: unit.X, Y: unit.Y}, start)
	if distanceSquared(unit.X, unit.Y, entryPoint.X, entryPoint.Y) < 64 {
		return nil
	}

	return &entryPoint
}

func (s *GameState) nextMovementOrderIDLocked() int64 {
	s.nextOrderID++
	return s.nextOrderID
}

func (s *GameState) resetUnitMovementLocked(unit *Unit, orderID int64) {
	unit.OrderID = orderID
	unit.Path = nil
	unit.Moving = false
	unit.TargetX = unit.X
	unit.TargetY = unit.Y
}

func (s *GameState) applyUnitSeparationLocked(blocked map[gridPoint]bool) {
	minDistance := unitSeparationDistance
	minDistanceSq := minDistance * minDistance

	for i := 0; i < len(s.Units); i++ {
		for j := i + 1; j < len(s.Units); j++ {
			a := s.Units[i]
			b := s.Units[j]
			dx := b.X - a.X
			dy := b.Y - a.Y
			distSq := dx*dx + dy*dy

			if a.Moving && b.Moving && a.OrderID != 0 && a.OrderID == b.OrderID {
				continue
			}

			if distSq >= minDistanceSq {
				continue
			}

			dist := math.Sqrt(distSq)
			if dist < 0.001 {
				angle := float64((a.ID+b.ID)%16) * (math.Pi / 8)
				dx = math.Cos(angle)
				dy = math.Sin(angle)
				dist = 1
			}

			overlapScale := 0.5
			if a.Moving || b.Moving {
				overlapScale = 0.18
			}

			overlap := (minDistance - dist) * overlapScale
			pushX := (dx / dist) * overlap
			pushY := (dy / dist) * overlap

			s.tryMoveUnitByOffsetLocked(a, -pushX, -pushY, blocked)
			s.tryMoveUnitByOffsetLocked(b, pushX, pushY, blocked)
		}
	}
}

func (s *GameState) tryMoveUnitByOffsetLocked(unit *Unit, offsetX, offsetY float64, blocked map[gridPoint]bool) {
	nextX := clampFloat(unit.X+offsetX, unitRadius, s.MapWidth-unitRadius)
	nextY := clampFloat(unit.Y+offsetY, unitRadius, s.MapHeight-unitRadius)
	if !s.isWalkable(s.worldToGrid(nextX, nextY), blocked) {
		return
	}

	unit.X = nextX
	unit.Y = nextY
}

func (s *GameState) randomColor() string {
	palette := []string{
		"#e74c3c",
		"#3498db",
		"#2ecc71",
		"#f1c40f",
		"#9b59b6",
		"#e67e22",
		"#1abc9c",
		"#ec4899",
	}

	used := make(map[string]bool)
	for _, player := range s.Players {
		used[player.Color] = true
	}

	available := make([]string, 0, len(palette))
	for _, color := range palette {
		if !used[color] {
			available = append(available, color)
		}
	}

	if len(available) > 0 {
		return available[s.rng.Intn(len(available))]
	}

	return palette[s.rng.Intn(len(palette))]
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func distanceSquared(ax, ay, bx, by float64) float64 {
	dx := ax - bx
	dy := ay - by
	return dx*dx + dy*dy
}

func simplifyLeadingWaypoints(unit *Unit, path []protocol.Vec2, finalTarget protocol.Vec2) []protocol.Vec2 {
	for len(path) > 1 {
		first := path[0]
		second := path[1]
		toFinalX := finalTarget.X - unit.X
		toFinalY := finalTarget.Y - unit.Y
		toFirstX := first.X - unit.X
		toFirstY := first.Y - unit.Y
		toSecondX := second.X - unit.X
		toSecondY := second.Y - unit.Y

		if dotProduct(toFirstX, toFirstY, toFinalX, toFinalY) < 0 && dotProduct(toSecondX, toSecondY, toFinalX, toFinalY) >= 0 {
			path = path[1:]
			continue
		}

		if distanceSquared(unit.X, unit.Y, first.X, first.Y) < 100 {
			path = path[1:]
			continue
		}

		break
	}

	return path
}

func dotProduct(ax, ay, bx, by float64) float64 {
	return ax*bx + ay*by
}

func buildFormationTargets(units []*Unit, anchor protocol.Vec2, spacing float64) []protocol.Vec2 {
	count := len(units)
	if count == 0 {
		return nil
	}
	if count == 1 {
		return []protocol.Vec2{anchor}
	}

	center := averageUnitPosition(units)
	forwardX := anchor.X - center.X
	forwardY := anchor.Y - center.Y
	forwardLength := math.Hypot(forwardX, forwardY)

	if forwardLength < 0.001 {
		forwardX, forwardY = 0, 1
		forwardLength = 1
	}

	forwardX /= forwardLength
	forwardY /= forwardLength
	rightX := forwardY
	rightY := -forwardX

	cols := int(math.Ceil(math.Sqrt(float64(count))))
	rows := int(math.Ceil(float64(count) / float64(cols)))
	totalWidth := float64(cols-1) * spacing
	totalHeight := float64(rows-1) * spacing
	slots := make([]protocol.Vec2, 0, count)

	for i := 0; i < count; i++ {
		col := i % cols
		row := i / cols
		rightOffset := float64(col)*spacing - totalWidth/2
		forwardOffset := float64(row)*spacing - totalHeight/2

		slots = append(slots, protocol.Vec2{
			X: anchor.X + rightX*rightOffset + forwardX*forwardOffset,
			Y: anchor.Y + rightY*rightOffset + forwardY*forwardOffset,
		})
	}

	type formationIndex struct {
		index   int
		right   float64
		forward float64
	}

	unitOrder := make([]formationIndex, 0, count)
	for index, unit := range units {
		relativeX := unit.X - center.X
		relativeY := unit.Y - center.Y
		unitOrder = append(unitOrder, formationIndex{
			index:   index,
			right:   relativeX*rightX + relativeY*rightY,
			forward: relativeX*forwardX + relativeY*forwardY,
		})
	}

	slotOrder := make([]formationIndex, 0, count)
	for index, slot := range slots {
		relativeX := slot.X - anchor.X
		relativeY := slot.Y - anchor.Y
		slotOrder = append(slotOrder, formationIndex{
			index:   index,
			right:   relativeX*rightX + relativeY*rightY,
			forward: relativeX*forwardX + relativeY*forwardY,
		})
	}

	sort.Slice(unitOrder, func(i, j int) bool {
		if math.Abs(unitOrder[i].forward-unitOrder[j].forward) > 8 {
			return unitOrder[i].forward < unitOrder[j].forward
		}
		return unitOrder[i].right < unitOrder[j].right
	})

	sort.Slice(slotOrder, func(i, j int) bool {
		if math.Abs(slotOrder[i].forward-slotOrder[j].forward) > 8 {
			return slotOrder[i].forward < slotOrder[j].forward
		}
		return slotOrder[i].right < slotOrder[j].right
	})

	targets := make([]protocol.Vec2, count)
	for i := 0; i < count; i++ {
		targets[unitOrder[i].index] = slots[slotOrder[i].index]
	}

	return targets
}

func averageUnitPosition(units []*Unit) protocol.Vec2 {
	if len(units) == 0 {
		return protocol.Vec2{}
	}

	var totalX float64
	var totalY float64

	for _, unit := range units {
		totalX += unit.X
		totalY += unit.Y
	}

	return protocol.Vec2{
		X: totalX / float64(len(units)),
		Y: totalY / float64(len(units)),
	}
}
