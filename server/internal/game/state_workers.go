package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

func (s *GameState) GatherWithUnits(playerID string, unitIDs []int, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible || building.ResourceAmount <= 0 {
		return
	}
	if building.BuildingType != "goldmine" && building.BuildingType != "tree" {
		return
	}

	blocked := s.getBlockedCellsLocked()
	if len(s.getBuildingApproachPositionsLocked(*building, 1, blocked, nil)) == 0 {
		return
	}

	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "gather") {
			continue
		}

		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		approachPoints := s.getBuildingApproachPositionsLocked(*building, 1, blocked, unitPos)
		if len(approachPoints) == 0 {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.GatherTargetID = buildingID
		unit.GatherBuildingType = building.BuildingType
		unit.ReturnTargetID = ""
		unit.Gathering = false
		unit.Returning = false
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
	}
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
		townhall = s.findOwnedTownhallLocked(unit.OwnerID)
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

func (s *GameState) findNearestAvailableTreeLocked(excludeID string, unitX, unitY float64, blocked map[gridPoint]bool) *protocol.BuildingTile {
	var best *protocol.BuildingTile
	bestDist := math.MaxFloat64

	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "tree" || b.ID == excludeID {
			continue
		}
		if b.ResourceAmount <= 0 {
			continue
		}
		if s.countWorkersInsideBuildingLocked(b.ID) >= treeWorkerCap {
			continue
		}
		if len(s.getBuildingApproachPositionsLocked(*b, 1, blocked, nil)) == 0 {
			continue
		}
		centerX := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		centerY := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		dist := distanceSquared(unitX, unitY, centerX, centerY)
		if dist < bestDist {
			bestDist = dist
			best = b
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
	approachPoints := s.getBuildingApproachPositionsLocked(*next, 1, blocked, unitPos)
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
		return
	}

	resourceNode := s.getBuildingByIDLocked(unit.GatherTargetID)
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

	isTree := resourceNode.BuildingType == "tree"

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
		gathered := minInt(gatherAmountForUnitResource(unit.UnitType, resourceNode.ResourceType), resourceNode.ResourceAmount)
		if gathered > 0 {
			unit.CarriedResourceType = resourceNode.ResourceType
			unit.CarriedAmount = gathered
			resourceNode.ResourceAmount -= gathered
		}

		if !isTree {
			if exitPoint := s.getUnitExitPositionForBuildingLocked(*resourceNode, unit); exitPoint != nil {
				unit.X = exitPoint.X
				unit.Y = exitPoint.Y
				unit.TargetX = exitPoint.X
				unit.TargetY = exitPoint.Y
			}
		}

		// Remove the building once its resource pool is empty.
		if resourceNode.ResourceAmount <= 0 {
			s.removeBuildingByIDLocked(resourceNode.ID)
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
			townhall = s.findOwnedTownhallLocked(unit.OwnerID)
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
			liveNode := s.getBuildingByIDLocked(unit.GatherTargetID)
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
			approachPoints := s.getBuildingApproachPositionsLocked(*liveNode, 1, blocked, unitPos)
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

	if !s.isUnitNearBuildingLocked(unit, *resourceNode, s.MapConfig.CellSize*1.5) {
		if isTree {
			unit.Status = "Heading To Tree"
		} else {
			unit.Status = "Heading To Mine"
		}
		if !unit.Moving {
			unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
			approachPoints := s.getBuildingApproachPositionsLocked(*resourceNode, 1, blocked, unitPos)
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
	if s.countWorkersInsideBuildingLocked(resourceNode.ID) >= workerCap {
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
	townhall := s.findOwnedTownhallLocked(unit.OwnerID)
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
