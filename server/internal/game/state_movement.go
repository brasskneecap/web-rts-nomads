package game

import (
	"math"
	"sort"
	"webrts/server/pkg/protocol"
)

func (s *GameState) MoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer profileStart("cmd.MoveUnits")()

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

	// Build the per-plane sub-cell blocked map ONCE for the whole group.
	// All units share OrderID so buildUnitPathBlockedLocked excludes the
	// same set, producing an identical map regardless of "self".
	groundSubBlocked, flyerSubBlocked := s.buildGroupSubBlockedLocked(validUnits, blocked)

	// Stamp order + anchor up front, then hand the whole group off to the
	// leader-follower pather. One A* drives the whole group's route; followers
	// reuse it and validate their first leg via cheap LoS sampling.
	clampedTargets := make([]protocol.Vec2, len(validUnits))
	for i, unit := range validUnits {
		target := targets[i]
		unit.Order = OrderState{Type: OrderMove, DestX: target.X, DestY: target.Y}
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y
		clampedTargets[i] = protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}
	}
	s.assignGroupPathsLocked(validUnits, clampedTargets, blocked, groundSubBlocked, flyerSubBlocked)
}

// buildGroupSubBlockedLocked builds the sub-cell blocked map(s) for a
// shared-OrderID group exactly once. Returns (groundMap, flyerMap); either
// may be nil if no unit of that plane exists in the group. The caller picks
// the right map per unit based on unit.Flyer. Lets multi-unit handlers skip
// K-1 redundant rebuilds of an identical map.
func (s *GameState) buildGroupSubBlockedLocked(units []*Unit, blocked map[gridPoint]bool) (ground, flyer map[gridPoint]bool) {
	var groundExemplar, flyerExemplar *Unit
	for _, u := range units {
		if u == nil {
			continue
		}
		if u.Flyer {
			if flyerExemplar == nil {
				flyerExemplar = u
			}
		} else if groundExemplar == nil {
			groundExemplar = u
		}
		if groundExemplar != nil && flyerExemplar != nil {
			break
		}
	}
	if groundExemplar != nil {
		ground = s.buildUnitPathBlockedLocked(groundExemplar, blocked)
	}
	if flyerExemplar != nil {
		flyer = s.buildUnitPathBlockedLocked(flyerExemplar, nil)
	}
	return ground, flyer
}

// lineWalkableLocked checks line-of-sight between two world points by sampling
// terrain walkability at half-cell intervals. Used to verify a follower's
// first leg before reusing the leader's spine path in leader-follower group
// moves. Flyers always pass (they ignore terrain). Cost is O(distance /
// cell_size) — orders of magnitude cheaper than A*.
func (s *GameState) lineWalkableLocked(startX, startY, endX, endY float64, blocked map[gridPoint]bool, flyer bool) bool {
	if flyer {
		return true
	}
	dx := endX - startX
	dy := endY - startY
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist == 0 {
		return true
	}
	cellSize := s.MapConfig.CellSize
	if cellSize <= 0 {
		return true
	}
	step := cellSize / 2
	n := int(math.Ceil(dist / step))
	if n < 1 {
		n = 1
	}
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		px := startX + dx*t
		py := startY + dy*t
		cell := s.worldToGrid(px, py)
		if !s.isWalkable(cell, blocked) {
			return false
		}
	}
	return true
}

// assignGroupPathsLocked computes paths for a shared-OrderID group using a
// leader-follower model: one full A* for a representative leader (the unit
// closest to its formation slot — covers similar ground to the rest),
// followers reuse the leader's middle waypoints and substitute their own
// formation slot as the endpoint. Drops cost from O(K) sub-cell A*s to one,
// plus K cheap LoS checks. Followers whose line-of-sight to the leader's
// first waypoint is blocked fall back to a per-unit A* — preserving
// correctness for spread-out selections without re-introducing the O(K)
// worst case for tight ones.
//
// units and destinations must be parallel slices of equal length. Caller
// owns Order / CombatAnchor / OrderID assignment; this helper only writes
// Path, Moving, TargetX/Y, and the stuck-sample fields.
func (s *GameState) assignGroupPathsLocked(units []*Unit, destinations []protocol.Vec2, blocked map[gridPoint]bool, groundSubBlocked map[gridPoint]bool, flyerSubBlocked map[gridPoint]bool) {
	if len(units) == 0 || len(units) != len(destinations) {
		return
	}

	subFor := func(u *Unit) map[gridPoint]bool {
		if u != nil && u.Flyer {
			return flyerSubBlocked
		}
		return groundSubBlocked
	}

	if len(units) == 1 {
		s.assignUnitPathWithSubBlocked(units[0], destinations[0], blocked, subFor(units[0]), nil)
		return
	}

	// Leader heuristic: the unit whose current position is closest to its own
	// formation slot. That unit's A* route is the most representative of the
	// distance the rest of the group needs to cover, so followers reusing its
	// middle waypoints will track the right corridor.
	leaderIdx := 0
	bestDistSq := math.MaxFloat64
	for i, u := range units {
		if u == nil {
			continue
		}
		d := distanceSquared(u.X, u.Y, destinations[i].X, destinations[i].Y)
		if d < bestDistSq {
			bestDistSq = d
			leaderIdx = i
		}
	}

	leader := units[leaderIdx]
	s.assignUnitPathWithSubBlocked(leader, destinations[leaderIdx], blocked, subFor(leader), nil)

	// Leader couldn't path — fall back to per-unit A* for everyone else and
	// give up the optimization for this command. Failure here usually means
	// the destination is truly unreachable from this group's area.
	if !leader.Moving || len(leader.Path) == 0 {
		for i, unit := range units {
			if unit == nil || i == leaderIdx {
				continue
			}
			s.assignUnitPathWithSubBlocked(unit, destinations[i], blocked, subFor(unit), nil)
		}
		return
	}

	leaderPath := leader.Path
	firstWaypoint := leaderPath[0]

	for i, unit := range units {
		if unit == nil || i == leaderIdx {
			continue
		}

		// LoS gate: follower's first leg is start → leader's w1. If clear, we
		// can safely splice the rest of the leader's path onto a follower-
		// specific endpoint. If blocked (follower is on the wrong side of an
		// obstacle relative to the leader), fall back to a full A* for this
		// unit only — preserves correctness, costs one A* in the rare case.
		if !s.lineWalkableLocked(unit.X, unit.Y, firstWaypoint.X, firstWaypoint.Y, blocked, unit.Flyer) {
			s.assignUnitPathWithSubBlocked(unit, destinations[i], blocked, subFor(unit), nil)
			continue
		}

		// Copy the leader's waypoints and substitute this unit's formation slot
		// at the end. Copy (not slice-share) so per-unit Path mutations during
		// movement don't trample the leader's path.
		newPath := make([]protocol.Vec2, len(leaderPath))
		copy(newPath, leaderPath)
		newPath[len(newPath)-1] = destinations[i]
		unit.Path = newPath
		unit.Moving = true
		unit.TargetX = destinations[i].X
		unit.TargetY = destinations[i].Y
		unit.StuckSampleX = unit.X
		unit.StuckSampleY = unit.Y
		unit.StuckSampleAccum = 0
	}
}

func (s *GameState) AttackMoveUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer profileStart("cmd.AttackMoveUnits")()

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

	// Share one sub-cell blocked map across the group — see MoveUnits.
	groundSubBlocked, flyerSubBlocked := s.buildGroupSubBlockedLocked(validUnits, blocked)

	// Leader-follower group pathing — see assignGroupPathsLocked.
	clampedTargets := make([]protocol.Vec2, len(validUnits))
	for i, unit := range validUnits {
		target := targets[i]
		unit.Order = OrderState{Type: OrderAttackMove, DestX: target.X, DestY: target.Y}
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y
		clampedTargets[i] = protocol.Vec2{
			X: clampFloat(target.X, 0, s.MapWidth),
			Y: clampFloat(target.Y, 0, s.MapHeight),
		}
	}
	s.assignGroupPathsLocked(validUnits, clampedTargets, blocked, groundSubBlocked, flyerSubBlocked)
}

func (s *GameState) assignUnitPath(unit *Unit, dest protocol.Vec2, blocked map[gridPoint]bool, reservedGoals map[gridPoint]bool) {
	s.assignUnitPathWithSubBlocked(unit, dest, blocked, nil, reservedGoals)
}

// assignUnitPathWithSubBlocked is the internal pathing entry. When subBlocked
// is non-nil it is used directly, letting batch commands (MoveUnits and friends)
// build the per-plane sub-cell blocked map once and reuse it across the whole
// group. The blocked map is identical for all units in a shared-OrderID group
// (buildUnitPathBlockedLocked excludes same-OrderID peers and "self"), so per-
// unit rebuilds were pure waste. When subBlocked is nil this falls back to
// building one for this unit alone — same behaviour as before for single-unit
// callers (repaths, retargets, etc.).
func (s *GameState) assignUnitPathWithSubBlocked(unit *Unit, dest protocol.Vec2, blocked map[gridPoint]bool, subBlocked map[gridPoint]bool, reservedGoals map[gridPoint]bool) {
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
	//
	// Flyers ignore terrain entirely — only map bounds and other flyers
	// can constrain their path. buildUnitPathBlockedLocked sees nil terrain
	// and filters out ground-unit obstacles when self.Flyer is true.
	if subBlocked == nil {
		terrainBlockedForPath := blocked
		if unit != nil && unit.Flyer {
			terrainBlockedForPath = nil
		}
		subBlocked = s.buildUnitPathBlockedLocked(unit, terrainBlockedForPath)
	}

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
	// Flyers ignore terrain — pass nil so every in-bounds cell is treated as
	// walkable while reservedGoals (formation slot uniqueness) still applies.
	goalCellBlocked := blocked
	if unit != nil && unit.Flyer {
		goalCellBlocked = nil
	}
	resolvedGoalCell, ok := s.findNearestWalkableAvailable(s.worldToGrid(clampedDest.X, clampedDest.Y), goalCellBlocked, reservedGoals)
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

	unit.PathDiagnostics.RepathCount++
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
//  2. Unit obstacle circles. For every non-self, non-same-OrderID unit in
//     the same plane (ground vs flyer), mark each sub-cell whose centre
//     falls within unitSeparationDistance of the unit's position. This
//     reflects the actual unit hitbox at sub-cell resolution rather than
//     blocking a whole 64-cell, which leaves usable corridors between two
//     units in adjacent cells.
//
// Same-OrderID peers (the formation that was told to move together) are
// excluded so a group can fan out into formation slots without walling
// each other off.
//
// Plane filter: flyers and ground units don't collide with each other.
// callers pass nil terrainBlocked when self is a flyer (flyers ignore
// terrain entirely); inside the loop we also drop any "other" whose plane
// (Flyer flag) differs from self's, so ground paths route around ground
// obstacles but pass straight under flyers and vice versa.
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

	selfFlyer := self != nil && self.Flyer
	radiusSq := unitSeparationDistance * unitSeparationDistance
	radiusInSub := int(math.Ceil(unitSeparationDistance/unitPathSubCellSize)) + 1
	for _, other := range s.Units {
		if other == self || other == nil || other.HP <= 0 || !other.Visible {
			continue
		}
		if self != nil && self.OrderID != 0 && other.OrderID == self.OrderID {
			continue
		}
		// Allied units never block an ally's path. The mover plans straight
		// through stationary friendlies instead of detouring around them or
		// failing to reach a destination they're standing on. unitsFriendlyLocked
		// is the canonical alliance check: same Player.TeamID, and the __enemy__
		// wave AI is never friendly even to itself — so enemy units still block
		// each other and a walled-off wave still has to fight through (preserves
		// the enemy-blocked-objective invariant).
		if self != nil && s.unitsFriendlyLocked(self, other) {
			continue
		}
		// Different planes never collide in pathing (ground passes under flyers).
		if other.Flyer != selfFlyer {
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
	unit.AttackWindupRemaining = 0
	unit.Attacking = false
	unit.AttackDrifting = false
	unit.Order = OrderState{Type: OrderIdle}
	// Any new order clears focus so FocusTargetID and Order.Type never diverge.
	// This covers Move, AttackMove, Hold, Stop, AttackTarget, Patrol — every
	// order path that goes through resetUnitMovementLocked.
	unit.FocusTargetID = 0
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
			// Different planes never separate (ground passes under flyers).
			// Flyers still resolve overlap against other flyers so two rocs
			// don't visually fuse.
			if a.Flyer != b.Flyer {
				continue
			}
			// Allies don't shove each other while either is in motion: a
			// moving unit ghosts straight through idle allies (the physical
			// counterpart to the pathing exclusion in
			// buildUnitPathBlockedLocked — so passing works even in a
			// one-unit-wide corridor where the idle ally has nowhere to be
			// pushed). Two idle allies still fall through to the push below,
			// so a selected blob spreads out instead of permanently stacking
			// on one point. Hostile pairs are unaffected.
			if (a.Moving || b.Moving) && s.unitsFriendlyLocked(a, b) {
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
	// Flyers ignore terrain — only the map-bound clamp above gates the move.
	if !unit.Flyer && !s.isWalkable(s.worldToGrid(nextX, nextY), blocked) {
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

// ── Focus Target follow movement ──────────────────────────────────────────────
//
// focusFollowDistance is the target gap between the follower (Cleric) and the
// focus ally. The Cleric paths to a point focusFollowDistance px behind the
// focus (in the direction from focus toward the Cleric) so they stay near but
// not on top of each other. This value keeps the Cleric inside heal range for
// a typical Apprentice/Cleric (cast range = match_attack_range ≈ 256 px).
//
// focusLeashSlack is the hysteresis band. A new path is only requested when
// the current path's end-point is farther than (focusFollowDistance +
// focusLeashSlack) from the focus's current position, preventing stutter when
// the focus target wobbles by small amounts each tick.
//
// Both constants live here because they are movement-system tuning, not perk
// tuning. They are not data-driven from JSON (matching the precedent for
// unitRadius, unitSeparationDistance, etc.).
const (
	focusFollowDistance = 80.0
	focusLeashSlack     = 24.0
)

// tickFocusFollowMovementLocked updates the follow path for a unit in
// OrderFocusFollow. It:
//  1. Re-resolves and validates the focus target (caller should have already
//     called validateFocusTargetLocked, but we guard defensively).
//  2. Computes the follow destination: a point focusFollowDistance px from
//     the focus in the direction from focus toward the follower.
//  3. Repaths only when the current path's end-point is farther than
//     focusFollowDistance + focusLeashSlack from the focus's current position
//     (squared-distance comparison throughout — no sqrt in hot path).
//
// Caller holds s.mu.
func (s *GameState) tickFocusFollowMovementLocked(unit *Unit, blocked map[gridPoint]bool) {
	if unit == nil || unit.FocusTargetID == 0 {
		return
	}
	focus := s.getUnitByIDLocked(unit.FocusTargetID)
	if focus == nil || focus.HP <= 0 || !focus.Visible {
		// Focus became invalid; clearFocusTargetLocked handles state cleanup.
		// validateFocusTargetLocked (called in the decay loop) will pick this up.
		return
	}

	// Compute follow destination: point focusFollowDistance px from focus,
	// offset toward the follower so the Cleric trails behind the focus.
	dx := unit.X - focus.X
	dy := unit.Y - focus.Y
	distSq := dx*dx + dy*dy

	var destX, destY float64
	if distSq < 1e-6 {
		// Caster is on top of the focus — offset directly "behind" in world-up.
		destX = focus.X
		destY = focus.Y + focusFollowDistance
	} else {
		dist := math.Sqrt(distSq)
		// Normalised direction from focus toward caster, scaled to focusFollowDistance.
		destX = focus.X + (dx/dist)*focusFollowDistance
		destY = focus.Y + (dy/dist)*focusFollowDistance
	}

	// Repath debounce: only repath when the current path's end is farther than
	// (focusFollowDistance + focusLeashSlack) from the focus's CURRENT position.
	// This prevents per-tick repath storms when the focus is stationary or
	// moving within the slack window.
	leashThreshSq := (focusFollowDistance + focusLeashSlack) * (focusFollowDistance + focusLeashSlack)

	if unit.Moving && len(unit.Path) > 0 {
		// Compare the path's final waypoint against the focus's current position.
		last := unit.Path[len(unit.Path)-1]
		ddx := last.X - focus.X
		ddy := last.Y - focus.Y
		if ddx*ddx+ddy*ddy <= leashThreshSq {
			return // existing path still good; don't repath
		}
	} else if !unit.Moving {
		// Unit is stationary; check if it is already close enough to the focus.
		cddx := unit.X - focus.X
		cddy := unit.Y - focus.Y
		if cddx*cddx+cddy*cddy <= leashThreshSq {
			return // already within the slack window; no movement needed
		}
	}

	// Request a new path toward the follow destination.
	s.assignUnitPathWithSubBlocked(unit, protocol.Vec2{X: destX, Y: destY}, blocked, nil, nil)
}
