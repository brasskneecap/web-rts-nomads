package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

// DepositWithUnits is the player-directed "drop off your carried resources at
// THIS specific deposit-point" command. Mirrors GatherWithUnits but targets a
// friendly deposit-point building instead of a resource source. Workers
// without carried resources in unitIDs are silently skipped — the client
// routes empty-handed selection members through a separate move command.
//
// On arrival the existing tick loop (updateWorkerTaskLocked) deposits the load
// and, if the worker still has a live GatherTargetID, redirects back to the
// mine/tree so the gather job resumes.
func (s *GameState) DepositWithUnits(playerID string, unitIDs []int, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer profileStart("cmd.DepositWithUnits")()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}
	if !containsString(building.Capabilities, "deposit-point") {
		return
	}

	blocked := s.getBlockedCellsLocked()
	if len(s.getBuildingApproachPositionsLocked(*building, 1, blocked, nil)) == 0 {
		return
	}

	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID {
			continue
		}
		if !unitHasCapability(unit.UnitType, "gather") {
			continue
		}
		if unit.CarriedAmount <= 0 {
			continue
		}

		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		approachPoints := s.getBuildingApproachPositionsLocked(*building, 1, blocked, unitPos)
		if len(approachPoints) == 0 {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.ReturnTargetID = building.ID
		unit.Returning = true
		unit.Gathering = false
		unit.MiningInside = false
		unit.MiningRemaining = 0
		unit.Visible = true
		if unit.CarriedResourceType == "wood" {
			unit.Status = "Returning Wood"
		} else {
			unit.Status = "Returning Gold"
		}
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

func (s *GameState) GatherWithUnits(playerID string, unitIDs []int, targetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer profileStart("cmd.GatherWithUnits")()

	node := s.getResourceNodeByIDLocked(targetID)
	if node == nil || node.ResourceAmount <= 0 {
		return
	}
	if !containsString(node.Capabilities, "resource-source") {
		return
	}
	if !node.IsTree() && !node.IsGoldmine() {
		return
	}

	blocked := s.getBlockedCellsLocked()
	nodeTile := node.asBuildingTile()
	if len(s.getBuildingApproachPositionsLocked(nodeTile, 1, blocked, nil)) == 0 {
		return
	}

	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "gather") {
			continue
		}

		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		approachPoints := s.getBuildingApproachPositionsLocked(nodeTile, 1, blocked, unitPos)
		if len(approachPoints) == 0 {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.GatherTargetID = node.ID
		unit.GatherBuildingType = resourceNodeKindTag(node)
		unit.ReturnTargetID = ""
		unit.Gathering = false
		unit.Returning = false
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

// resourceNodeKindTag returns the legacy string tag used in Unit.GatherBuildingType.
// Kept compatible with existing "tree" / "goldmine" string comparisons; the
// pipeline can still read it back without another lookup.
func resourceNodeKindTag(n *resourceNode) string {
	if n == nil {
		return ""
	}
	if n.IsTree() {
		return "tree"
	}
	if n.IsGoldmine() {
		return "goldmine"
	}
	return ""
}

func (s *GameState) clearUnitGatherStateLocked(unit *Unit) {
	unit.GatherTargetID = ""
	unit.GatherBuildingType = ""
	unit.ReturnTargetID = ""
	unit.MiningInside = false
	unit.MiningRemaining = 0
	unit.Gathering = false
	unit.Returning = false
	unit.Visible = true
	unit.Status = "Idle"
}

// completeReturnDepositLocked handles a worker who is returning to deposit but the
// resource node is already depleted or gone. The worker deposits their carried load
// and then idles instead of looping back to the resource.
func (s *GameState) completeReturnDepositLocked(unit *Unit, blocked map[gridPoint]bool) {
	townhall := s.getBuildingByIDLocked(unit.ReturnTargetID)
	if townhall == nil {
		townhall = s.findNearestDepositPointLocked(unit.OwnerID, unit.X, unit.Y)
		if townhall != nil {
			unit.ReturnTargetID = townhall.ID
		}
	}
	if townhall == nil {
		s.clearUnitGatherStateLocked(unit)
		return
	}

	if s.isUnitNearBuildingLocked(unit, *townhall, s.MapConfig.CellSize*1.5) && !unit.Moving {
		if player, ok := s.Players[unit.OwnerID]; ok && unit.CarriedAmount > 0 {
			player.Resources[unit.CarriedResourceType] += unit.CarriedAmount
		}
		unit.CarriedAmount = 0
		unit.CarriedResourceType = ""
		unit.Returning = false
		unit.Gathering = false
		if unit.GatherBuildingType == "tree" {
			s.redirectUnitToTreeLocked(unit, blocked)
		} else {
			s.clearUnitGatherStateLocked(unit)
		}
		return
	}

	if !unit.Moving {
		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		approachPoints := s.getBuildingApproachPositionsLocked(*townhall, 1, blocked, unitPos)
		if len(approachPoints) > 0 {
			s.assignUnitPath(unit, approachPoints[0], blocked, nil)
		}
	}
}

// findNearestAvailableTreeLocked returns the closest tree obstacle that still
// has wood, has an available worker slot, and has a reachable approach cell.
// Returns a resourceNode view backed by the underlying obstacle pointer.
func (s *GameState) findNearestAvailableTreeLocked(excludeID string, unitX, unitY float64, blocked map[gridPoint]bool) *resourceNode {
	var best *resourceNode
	bestDist := math.MaxFloat64

	for i := range s.MapConfig.Obstacles {
		o := &s.MapConfig.Obstacles[i]
		if o.Obstacle != "tree" || o.ID == excludeID {
			continue
		}
		if o.ResourceAmount <= 0 {
			continue
		}
		if s.countWorkersInsideResourceNodeLocked(o.ID) >= treeWorkerCap {
			continue
		}
		node := resourceNodeFromObstacle(o)
		nodeTile := node.asBuildingTile()
		if len(s.getBuildingApproachPositionsLocked(nodeTile, 1, blocked, nil)) == 0 {
			continue
		}
		w := float64(node.Width)
		if w <= 0 {
			w = 1
		}
		h := float64(node.Height)
		if h <= 0 {
			h = 1
		}
		centerX := (float64(o.X) + w/2) * s.MapConfig.CellSize
		centerY := (float64(o.Y) + h/2) * s.MapConfig.CellSize
		dist := distanceSquared(unitX, unitY, centerX, centerY)
		if dist < bestDist {
			bestDist = dist
			best = node
		}
	}

	return best
}

func (s *GameState) redirectUnitToTreeLocked(unit *Unit, blocked map[gridPoint]bool) {
	next := s.findNearestAvailableTreeLocked(unit.GatherTargetID, unit.X, unit.Y, blocked)
	if next == nil {
		s.clearUnitGatherStateLocked(unit)
		return
	}

	unit.GatherTargetID = next.ID
	unit.GatherBuildingType = "tree"
	unit.ReturnTargetID = ""
	unit.Returning = false
	unit.Gathering = false
	unit.MiningInside = false
	unit.MiningRemaining = 0
	unit.Status = "Heading To Tree"

	unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
	nodeTile := next.asBuildingTile()
	approachPoints := s.getBuildingApproachPositionsLocked(nodeTile, 1, blocked, unitPos)
	if len(approachPoints) > 0 {
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

func (s *GameState) updateWorkerTaskLocked(unit *Unit, dt float64, blocked map[gridPoint]bool) {
	if !unitHasCapability(unit.UnitType, "gather") {
		return
	}

	if unit.AttackTargetID != 0 {
		return
	}

	if unit.BuildTargetID != "" {
		s.updateWorkerBuildStateLocked(unit)
		return
	}

	if unit.GatherTargetID == "" {
		// Player-directed deposit (or stranded carrier from a prior move order):
		// drive to ReturnTargetID and complete the hand-off. Without this branch
		// the worker would freeze mid-route because the rest of this function
		// keys off GatherTargetID.
		if unit.Returning && unit.CarriedAmount > 0 {
			s.completeReturnDepositLocked(unit, blocked)
		}
		return
	}

	resourceNode := s.getResourceNodeByIDLocked(unit.GatherTargetID)
	nodeAlive := resourceNode != nil && resourceNode.ResourceAmount > 0

	if !nodeAlive {
		if unit.Returning && unit.CarriedAmount > 0 {
			// Node is gone but the worker has resources to deposit.
			// completeReturnDepositLocked will redirect to a new tree afterwards
			// (if GatherBuildingType is "tree") rather than idling.
			s.completeReturnDepositLocked(unit, blocked)
		} else if unit.GatherBuildingType == "tree" {
			s.redirectUnitToTreeLocked(unit, blocked)
		} else {
			s.clearUnitGatherStateLocked(unit)
		}
		return
	}

	isTree := resourceNode.IsTree()
	nodeTile := resourceNode.asBuildingTile()

	if unit.MiningInside {
		if isTree {
			unit.Status = "Chopping Wood"
		} else {
			unit.Status = "Mining Gold"
		}
		unit.MiningRemaining -= dt
		if unit.MiningRemaining > 0 {
			return
		}

		unit.MiningInside = false
		unit.Gathering = false
		unit.Visible = true
		desired := gatherAmountForUnitResource(unit.UnitType, resourceNode.ResourceType)
		gathered := resourceNode.consumeResource(desired)
		if gathered > 0 {
			unit.CarriedResourceType = resourceNode.ResourceType
			unit.CarriedAmount = gathered
		}

		if !isTree {
			if exitPoint := s.getUnitExitPositionForBuildingLocked(nodeTile, unit); exitPoint != nil {
				unit.X = exitPoint.X
				unit.Y = exitPoint.Y
				unit.TargetX = exitPoint.X
				unit.TargetY = exitPoint.Y
			}
		}

		// Remove the entity once its resource pool is empty.
		if resourceNode.ResourceAmount <= 0 {
			s.removeResourceNodeLocked(resourceNode)
		}

		s.sendWorkerToDepositLocked(unit, blocked)
		return
	}

	if unit.Returning {
		if isTree {
			unit.Status = "Returning Wood"
		} else {
			unit.Status = "Returning Gold"
		}
		townhall := s.getBuildingByIDLocked(unit.ReturnTargetID)
		if townhall == nil {
			townhall = s.findNearestDepositPointLocked(unit.OwnerID, unit.X, unit.Y)
			if townhall != nil {
				unit.ReturnTargetID = townhall.ID
			}
		}
		if townhall == nil {
			unit.Returning = false
			return
		}

		if s.isUnitNearBuildingLocked(unit, *townhall, s.MapConfig.CellSize*1.5) && !unit.Moving {
			if player, ok := s.Players[unit.OwnerID]; ok && unit.CarriedResourceType != "" && unit.CarriedAmount > 0 {
				player.Resources[unit.CarriedResourceType] += unit.CarriedAmount
			}
			unit.CarriedAmount = 0
			unit.CarriedResourceType = ""
			unit.Returning = false
			unit.Gathering = false

			// Re-check the node; another worker may have depleted it this tick.
			liveNode := s.getResourceNodeByIDLocked(unit.GatherTargetID)
			if liveNode == nil || liveNode.ResourceAmount <= 0 {
				if isTree {
					s.redirectUnitToTreeLocked(unit, blocked)
				} else {
					s.clearUnitGatherStateLocked(unit)
				}
				return
			}

			if isTree {
				unit.Status = "Returning To Tree"
			} else {
				unit.Status = "Returning To Mine"
			}

			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			liveTile := liveNode.asBuildingTile()
			approachPoints := s.getBuildingApproachPositionsLocked(liveTile, 1, blocked, unitPos)
			if len(approachPoints) > 0 {
				s.assignUnitPath(unit, approachPoints[0], blocked, nil)
			}
			return
		}

		if !unit.Moving {
			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			approachPoints := s.getBuildingApproachPositionsLocked(*townhall, 1, blocked, unitPos)
			if len(approachPoints) > 0 {
				s.assignUnitPath(unit, approachPoints[0], blocked, nil)
			}
		}
		return
	}

	// Workers must be within ~0.6 cells of the resource node's AABB to start
	// chopping/mining. Cardinal-adjacent perimeter cells sit ~0.5 cells out
	// and diagonal corners sit ~0.71 cells out — both still register, but
	// anything farther forces the worker to keep approaching instead of
	// chopping at an angle from a distance.
	if !s.isUnitNearBuildingLocked(unit, nodeTile, s.MapConfig.CellSize*0.6) {
		if isTree {
			unit.Status = "Heading To Tree"
		} else {
			unit.Status = "Heading To Mine"
		}
		if !unit.Moving {
			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			approachPoints := s.getBuildingApproachPositionsLocked(nodeTile, 1, blocked, unitPos)
			if len(approachPoints) > 0 {
				s.assignUnitPath(unit, approachPoints[0], blocked, nil)
			}
		}
		return
	}

	if unit.Moving {
		if isTree {
			unit.Status = "Heading To Tree"
		} else {
			unit.Status = "Heading To Mine"
		}
		return
	}

	workerCap := goldmineWorkerCap
	if isTree {
		workerCap = treeWorkerCap
	}
	if s.countWorkersInsideResourceNodeLocked(resourceNode.ID) >= workerCap {
		if isTree {
			s.redirectUnitToTreeLocked(unit, blocked)
		} else {
			unit.Status = "Waiting For Mine Slot"
		}
		return
	}

	choppingDuration := goldmineMiningSeconds
	if isTree {
		choppingDuration = treeChoppingSeconds
	}
	unit.Gathering = true
	unit.MiningInside = true
	unit.MiningRemaining = choppingDuration
	if !isTree {
		unit.Visible = false
	}
	unit.Moving = false
	unit.Path = nil
	if isTree {
		unit.Status = "Chopping Wood"
	} else {
		unit.Status = "Mining Gold"
	}
}

func (s *GameState) sendWorkerToDepositLocked(unit *Unit, blocked map[gridPoint]bool) {
	townhall := s.findNearestDepositPointLocked(unit.OwnerID, unit.X, unit.Y)
	if townhall == nil {
		unit.Status = "Idle"
		return
	}

	unit.ReturnTargetID = townhall.ID
	unit.Returning = true
	unit.Gathering = false
	if unit.CarriedResourceType == "wood" {
		unit.Status = "Returning Wood"
	} else {
		unit.Status = "Returning Gold"
	}

	unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
	approachPoints := s.getBuildingApproachPositionsLocked(*townhall, 1, blocked, unitPos)
	if len(approachPoints) > 0 {
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
}

func gatherAmountForUnitResource(unitType, resourceType string) int {
	def, ok := getUnitDef(unitType)
	if !ok {
		return defaultGatherAmountForResource(resourceType)
	}

	switch resourceType {
	case "gold":
		if def.GoldGatherAmount > 0 {
			return def.GoldGatherAmount
		}
	case "wood":
		if def.WoodGatherAmount > 0 {
			return def.WoodGatherAmount
		}
	}

	return defaultGatherAmountForResource(resourceType)
}

func defaultGatherAmountForResource(resourceType string) int {
	switch resourceType {
	case "gold":
		return defaultGoldGatherAmount
	case "wood":
		return defaultWoodGatherAmount
	default:
		return defaultWoodGatherAmount
	}
}
