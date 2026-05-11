package game

import (
	"math"
	"sort"
	"webrts/server/pkg/protocol"
)

func (s *GameState) MoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validUnits := make([]*Unit, 0, len(unitIDs))
	blocked := s.getBlockedCellsLocked()

	for _, unitID := range unitIDs {
		unit, ok := s.unitsByID[unitID]
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
		unit.Order = OrderState{Type: OrderMove, DestX: dest.X, DestY: dest.Y}
		unit.CombatAnchorX = dest.X
		unit.CombatAnchorY = dest.Y
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

	// Stamp the shared OrderID on every group member up front, before any
	// pathfinding runs. buildPathingObstaclesLocked excludes same-OrderID
	// peers; without this pre-pass, the first unit in the loop would
	// pathfind while later peers still carried their previous OrderID and
	// got treated as out-of-group obstacles, producing detours through
	// the formation.
	for _, unit := range validUnits {
		s.resetUnitMovementLocked(unit, orderID)
	}

	for i, unit := range validUnits {
		target := targets[i]
		unit.Order = OrderState{Type: OrderMove, DestX: target.X, DestY: target.Y}
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y

		s.assignUnitPath(unit, protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}, blocked, nil)
	}
}

func (s *GameState) AttackMoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validUnits := make([]*Unit, 0, len(unitIDs))
	blocked := s.getBlockedCellsLocked()

	for _, unitID := range unitIDs {
		unit, ok := s.unitsByID[unitID]
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
		unit.Order = OrderState{Type: OrderAttackMove, DestX: dest.X, DestY: dest.Y}
		unit.CombatAnchorX = dest.X
		unit.CombatAnchorY = dest.Y
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

	// Two-pass shared-OrderID assignment so the first unit's pathfind sees
	// later peers as same-group rather than as out-of-group obstacles.
	// See MoveUnits for the full rationale.
	for _, unit := range validUnits {
		s.resetUnitMovementLocked(unit, orderID)
	}

	for i, unit := range validUnits {
		target := targets[i]
		unit.Order = OrderState{Type: OrderAttackMove, DestX: target.X, DestY: target.Y}
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y

		s.assignUnitPath(unit, protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}, blocked, nil)
	}
}

func (s *GameState) assignUnitPath(unit *Unit, dest protocol.Vec2, blocked map[gridPoint]bool, reservedGoals map[gridPoint]bool) {
	s.debugPathTracker.recordRepath(unit.ID, unit.X, unit.Y, s.Tick)
	clampedDest := protocol.Vec2{
		X: clampFloat(dest.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(dest.Y, unitRadius, s.MapHeight-unitRadius),
	}

	// Pathfind on the fine sub-cell grid so the route can find gaps
	// between unit obstacles that are smaller than a 64-cell wide. The
	// blocked map combines terrain (each terrain cell expanded to its
	// sub-cells) and unit-obstacle separation circles (sub-cells within
	// unitSeparationDistance of any non-self, non-same-OrderID unit's
	// centre). Same-OrderID peers are excluded so a formation can fan out
	// without walling itself off.
	subBlocked := s.buildUnitPathBlockedLocked(unit, blocked)

	subStart := s.worldToUnitPathSubGrid(unit.X, unit.Y)
	if rs, ok := s.findNearestUnitPathSubWalkable(subStart, subBlocked); ok {
		subStart = rs
	}
	subGoal := s.worldToUnitPathSubGrid(clampedDest.X, clampedDest.Y)
	resolvedSubGoal, ok := s.findNearestUnitPathSubWalkable(subGoal, subBlocked)
	if !ok {
		unit.Path = nil
		unit.Moving = false
		return
	}

	subPath := s.findUnitPath(subStart, resolvedSubGoal, subBlocked)
	if len(subPath) == 0 {
		unit.Path = nil
		unit.Moving = false
		return
	}

	// Collapse the dense sub-cell waypoint list to its turn points so
	// per-tick advancement and the snapshot payload stay cheap.
	path := s.simplifyUnitPath(subPath, subBlocked)

	// Cull leading waypoints the unit has already passed. A fresh A* always
	// begins at the centre of the unit's current sub-cell, so if the unit is
	// moving and sits anywhere between two sub-cell centres, path[0] is
	// behind it — walking to it before going forward reads as a jarring
	// "step back," especially when re-issuing a move while already moving.
	// simplifyUnitPath has already verified line-of-sight between each
	// consecutive pair, so a waypoint the unit has projected past is safe to
	// drop. Test: forward = path[0]→path[1]; if unit-from-path[0] dot
	// forward > 0, the unit is past path[0] along the route.
	for len(path) >= 2 {
		fx := path[1].X - path[0].X
		fy := path[1].Y - path[0].Y
		ux := unit.X - path[0].X
		uy := unit.Y - path[0].Y
		if fx*ux+fy*uy <= 0 {
			break
		}
		path = path[1:]
	}
	// Same idea for the singleton-remaining case — if the only waypoint left
	// is essentially under the unit, drop it so the path ends here.
	if len(path) == 1 && distanceSquared(unit.X, unit.Y, path[0].X, path[0].Y) < 4 {
		path = path[1:]
	}

	// The 64-cell goal cell lookup is still useful for clamping the final
	// landing point inside the destination terrain cell (handles map-edge
	// padding and non-walkable goal cells the same way the old A* did).
	resolvedGoalCell, ok := s.findNearestWalkableAvailable(s.worldToGrid(clampedDest.X, clampedDest.Y), blocked, reservedGoals)
	if !ok {
		unit.Path = nil
		unit.Moving = false
		return
	}
	finalTarget := s.clampPointToCell(clampedDest, resolvedGoalCell)
	if len(path) == 0 {
		path = []protocol.Vec2{finalTarget}
	} else {
		path[len(path)-1] = finalTarget
	}

	if reservedGoals != nil {
		reservedGoals[resolvedGoalCell] = true
	}

	unit.TargetX = finalTarget.X
	unit.TargetY = finalTarget.Y
	unit.Path = path
	unit.Moving = len(path) > 0

	// Fresh path → restart the stuck-progress window from the unit's current
	// position so the watchdog measures progress against the new route.
	unit.StuckSampleX = unit.X
	unit.StuckSampleY = unit.Y
	unit.StuckSampleAccum = 0
}

func (s *GameState) repathUnitLocked(unit *Unit, blocked map[gridPoint]bool) bool {
	if !unit.Moving {
		return false
	}

	dest := protocol.Vec2{X: unit.TargetX, Y: unit.TargetY}
	s.assignUnitPath(unit, dest, blocked, nil)
	return unit.Moving
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

// buildUnitPathBlockedLocked builds a sub-cell blocked map for unit
// pathfinding. Combines two sources:
//
//  1. Terrain blocked cells. Each 64×64 terrain block expands to a square
//     of unitPathSubCellSize sub-cells (e.g. 4×4 = 16 sub-cells when
//     CellSize=64 and unitPathSubCellSize=16) so the sub-cell A* honours
//     all the same impassable terrain the coarse A* did.
//  2. Unit obstacle circles. For every non-self, non-same-OrderID unit,
//     mark each sub-cell whose centre falls within unitSeparationDistance
//     of the unit's position. This reflects the actual unit hitbox at
//     sub-cell resolution rather than blocking a whole 64-cell, which
//     leaves usable corridors between two units in adjacent cells.
//
// Same-OrderID peers (the formation that was told to move together) are
// excluded so a group can fan out into formation slots without walling
// each other off.
func (s *GameState) buildUnitPathBlockedLocked(self *Unit, terrainBlocked map[gridPoint]bool) map[gridPoint]bool {
	sub := make(map[gridPoint]bool, len(terrainBlocked)*36+len(s.Units)*16)

	cellSize := s.MapConfig.CellSize
	if cellSize <= 0 {
		return sub
	}
	perSide := int(cellSize / unitPathSubCellSize)
	if perSide <= 0 {
		perSide = 1
	}
	// Expand by one sub-cell on each side so paths keep at least unitPathSubCellSize
	// of clearance from static obstacles — prevents units from grazing/clipping
	// building corners and tree edges as they walk past.
	for terrainCell := range terrainBlocked {
		baseX := terrainCell.X * perSide
		baseY := terrainCell.Y * perSide
		for dy := -1; dy <= perSide; dy++ {
			for dx := -1; dx <= perSide; dx++ {
				sub[gridPoint{X: baseX + dx, Y: baseY + dy}] = true
			}
		}
	}

	radiusSq := unitSeparationDistance * unitSeparationDistance
	radiusInSub := int(math.Ceil(unitSeparationDistance/unitPathSubCellSize)) + 1
	for _, other := range s.Units {
		if other == self || other == nil || other.HP <= 0 || !other.Visible {
			continue
		}
		if self != nil && self.OrderID != 0 && other.OrderID == self.OrderID {
			continue
		}
		centre := s.worldToUnitPathSubGrid(other.X, other.Y)
		for dy := -radiusInSub; dy <= radiusInSub; dy++ {
			for dx := -radiusInSub; dx <= radiusInSub; dx++ {
				p := gridPoint{X: centre.X + dx, Y: centre.Y + dy}
				worldP := s.unitPathSubGridToWorldCenter(p)
				if distanceSquared(worldP.X, worldP.Y, other.X, other.Y) <= radiusSq {
					sub[p] = true
				}
			}
		}
	}

	return sub
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
	unit.StuckSampleX = unit.X
	unit.StuckSampleY = unit.Y
	unit.StuckSampleAccum = 0
	unit.GatherTargetID = ""
	unit.GatherBuildingType = ""
	unit.ReturnTargetID = ""
	unit.MiningInside = false
	unit.MiningRemaining = 0
	unit.Gathering = false
	unit.Returning = false
	unit.BuildTargetID = ""
	unit.Building = false
	unit.InsideBuilder = false
	unit.RepairChargeAccumulator = 0
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.Attacking = false
	unit.Order = OrderState{Type: OrderIdle}
	unit.Visible = true
	unit.Status = "Idle"
	unit.CurrentTargetScore = 0
	unit.TauntedByUnitID = 0
	unit.TauntRemaining = 0
}

func (s *GameState) applyUnitSeparationLocked(blocked map[gridPoint]bool) {
	minDistance := unitSeparationDistance
	// Resolve only actual visual overlap (centers closer than the combined
	// visual radius). The 22px separation distance leaves a 2px buffer that
	// is useful as a target spacing for formations but causes a perpetual
	// nudge band between unitRadius*2 (20px, the no-overlap threshold) and
	// unitSeparationDistance (22px). For dense clusters (many units stacked
	// at a single spawnpoint, packed waves arriving on an objective, etc.)
	// every pair sits in that band, every pair pushes every tick, the chain
	// reaction never reaches equilibrium, and the result is the visible
	// position jitter / facing rotation jitter on the client.
	visualOverlapDistSq := (unitRadius * 2) * (unitRadius * 2)

	// Bucket units into a spatial index sized to the separation radius so
	// each unit only inspects neighbours in ~9 cells instead of scanning the
	// whole roster. Drops the pass from O(N²) to ~O(N) on average for sparse
	// armies; dense clusters degrade to O(K²) over the local cluster size K
	// rather than total population.
	index := newCombatSpatialIndex(unitSeparationDistance)
	for _, u := range s.Units {
		if u == nil {
			continue
		}
		index.add(u)
	}

	// Accumulate per-unit net push first, then apply once at the end. The
	// previous in-place mutation made the result order-dependent: A pushed by
	// B was then re-tested against C at its new position, C against D, etc.
	// In a cluster the chain reaction never settles — exactly the visible
	// rotation/jitter symptom. Net-force application converges symmetric
	// clusters to a stable equilibrium in a handful of ticks.
	pushX := make(map[int]float64, len(s.Units))
	pushY := make(map[int]float64, len(s.Units))

	for _, a := range s.Units {
		if a == nil {
			continue
		}
		for _, b := range index.query(a.X, a.Y, minDistance) {
			// ID ordering processes each unordered pair exactly once and
			// implicitly skips self (b.ID == a.ID).
			if b == nil || b.ID <= a.ID {
				continue
			}
			dx := b.X - a.X
			dy := b.Y - a.Y
			distSq := dx*dx + dy*dy

			if a.Moving && b.Moving && a.OrderID != 0 && a.OrderID == b.OrderID {
				continue
			}

			// Universal deadband: only resolve actual visual overlap. The
			// 2px formation buffer that was spent on the trigger threshold
			// is what fed the jitter loop; dropping it is the difference
			// between a stable cluster and a perpetually shimmering one.
			if distSq >= visualOverlapDistSq {
				continue
			}

			engagedMelee := s.unitsAreInMutualMeleeLocked(a, b)

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
			if engagedMelee {
				// Let melee units stay in contact once they've committed to each other.
				// Strong separation here creates the visible "staggering" loop where
				// combatants are pushed out of range and then immediately step back in.
				overlapScale = 0.05
			}

			overlap := (minDistance - dist) * overlapScale
			px := (dx / dist) * overlap
			py := (dy / dist) * overlap

			pushX[a.ID] -= px
			pushY[a.ID] -= py
			pushX[b.ID] += px
			pushY[b.ID] += py
		}
	}

	for _, u := range s.Units {
		if u == nil {
			continue
		}
		ox, oy := pushX[u.ID], pushY[u.ID]
		if ox == 0 && oy == 0 {
			continue
		}
		s.tryMoveUnitByOffsetLocked(u, ox, oy, blocked)
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
