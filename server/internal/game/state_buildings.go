package game

import (
	"fmt"
	"math"
	"sort"
	"webrts/server/pkg/protocol"
)

// addBuildingLocked appends the building to s.MapConfig.Buildings, registers
// it in s.buildingsByID, and invalidates the blocked-cells cache.
// Must be called under s.mu write lock.
func (s *GameState) addBuildingLocked(b protocol.BuildingTile) {
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, b)
	// append may reallocate the backing array, invalidating all existing
	// pointers stored in buildingsByID. Re-index the entire slice.
	if s.buildingsByID == nil {
		s.buildingsByID = make(map[string]*protocol.BuildingTile, len(s.MapConfig.Buildings))
	}
	for i := range s.MapConfig.Buildings {
		s.buildingsByID[s.MapConfig.Buildings[i].ID] = &s.MapConfig.Buildings[i]
	}
	s.invalidateBlockedCellsLocked()
}

// removeBuildingLocked removes the building with the given ID from
// s.MapConfig.Buildings, unregisters it from s.buildingsByID, and
// invalidates the blocked-cells cache. It also clears the production queue
// for that building.
// Must be called under s.mu write lock.
func (s *GameState) removeBuildingLocked(id string) {
	delete(s.Productions, id)
	delete(s.buildingsByID, id)
	filtered := make([]protocol.BuildingTile, 0, len(s.MapConfig.Buildings)-1)
	for _, b := range s.MapConfig.Buildings {
		if b.ID != id {
			filtered = append(filtered, b)
		}
	}
	s.MapConfig.Buildings = filtered
	// Re-index the surviving buildings because the slice backing array
	// changed; existing pointers in buildingsByID are now stale.
	for i := range s.MapConfig.Buildings {
		s.buildingsByID[s.MapConfig.Buildings[i].ID] = &s.MapConfig.Buildings[i]
	}
	s.invalidateBlockedCellsLocked()
}

func (s *GameState) BuildBuilding(playerID, buildingType string, unitIDs []int, gridX, gridY int) {
	def, ok := getBuildingDef(buildingType)
	if !ok {
		return
	}
	if !def.IsBuildable() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	for resource, cost := range def.ResourceCost {
		if player.Resources[resource] < cost {
			return
		}
	}

	gridW, gridH := def.Width, def.Height

	if gridX < 0 || gridY < 0 || gridX+gridW > s.MapConfig.GridCols || gridY+gridH > s.MapConfig.GridRows {
		return
	}

	blocked := s.getBlockedCellsLocked()
	for dy := 0; dy < gridH; dy++ {
		for dx := 0; dx < gridW; dx++ {
			if blocked[gridPoint{X: gridX + dx, Y: gridY + dy}] {
				return
			}
		}
	}

	for _, unit := range s.Units {
		if !unit.Visible {
			continue
		}
		cell := s.worldToGrid(unit.X, unit.Y)
		if cell.X >= gridX && cell.X < gridX+gridW && cell.Y >= gridY && cell.Y < gridY+gridH {
			return
		}
	}

	for resource, cost := range def.ResourceCost {
		player.Resources[resource] -= cost
	}

	metadata := map[string]interface{}{
		"underConstruction": true,
		"pendingStart":      true,
		"hp":                1.0,
		"maxHp":             def.MaxHp,
		"hpPerSecond":       def.HpPerSecond(),
	}
	for k, v := range def.Metadata {
		metadata[k] = v
	}

	s.nextBuildingID++
	ownerID := playerID
	building := protocol.BuildingTile{
		GridCoord:      protocol.GridCoord{X: gridX, Y: gridY},
		ID:             fmt.Sprintf("%s-%d", buildingType, s.nextBuildingID),
		BuildingType:   buildingType,
		Width:          gridW,
		Height:         gridH,
		Occupied:       true,
		Visible:        true,
		OwnerID:        &ownerID,
		Capabilities:   append([]string{}, def.Capabilities...),
		SpawnUnitTypes: append([]string{}, def.SpawnUnitTypes...),
		Metadata:       metadata,
	}

	// addBuildingLocked appends the building and invalidates the blocked-cell
	// cache so the second getBlockedCellsLocked call below reflects the new
	// building's footprint.
	s.addBuildingLocked(building)
	buildingID := building.ID

	blocked = s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	assigned := 0
	for _, unitID := range unitIDs {
		if assigned >= maxBuildersPerBuilding {
			break
		}
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || unit.UnitType != "worker" {
			continue
		}
		approachPoints := s.getBuildingApproachPositionsLocked(building, 1, blocked, &protocol.Vec2{X: unit.X, Y: unit.Y})
		if len(approachPoints) == 0 {
			continue
		}
		s.resetUnitMovementLocked(unit, orderID)
		unit.BuildTargetID = buildingID
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
		assigned++
	}
}

func (s *GameState) removeBuildingByIDLocked(buildingID string) {
	s.removeBuildingLocked(buildingID)
}

func (s *GameState) updateWorkerBuildStateLocked(unit *Unit) {
	if unit.Moving || unit.Building {
		return
	}
	building := s.getBuildingByIDLocked(unit.BuildTargetID)
	if building == nil {
		unit.BuildTargetID = ""
		unit.Status = "Idle"
		return
	}
	// First worker to arrive flips the building out of ghost/pending state so
	// HP progress and the construction-animation sprite take over from the
	// transparent final-sprite preview on the client.
	delete(building.Metadata, "pendingStart")
	unit.Building = true
	unit.Status = "Building"
}

const maxBuildersPerBuilding = 3

func getBuildingHP(building *protocol.BuildingTile) (hp, maxHp float64, ok bool) {
	if building.Metadata == nil {
		return 0, 0, false
	}
	h, hOk := building.Metadata["hp"].(float64)
	m, mOk := building.Metadata["maxHp"].(float64)
	if !hOk || !mOk || m <= 0 {
		return 0, 0, false
	}
	return h, m, true
}

// cancelOrphanedPendingBuildingsLocked scans for pending-start buildings
// (placed but no worker has arrived to begin construction) whose assigned
// workers have all been reassigned / killed / lost their BuildTargetID.
// These get torn back down and the placement cost is refunded to the owner.
// Once construction actually starts (first worker arrives, pendingStart is
// cleared), the building is committed and this check no-ops for it.
func (s *GameState) cancelOrphanedPendingBuildingsLocked() {
	// Collect IDs first so we don't mutate s.MapConfig.Buildings while
	// scanning it via index — removeBuildingLocked rebuilds the slice.
	var canceled []string
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.Metadata == nil {
			continue
		}
		pending, _ := b.Metadata["pendingStart"].(bool)
		if !pending {
			continue
		}
		hasWorker := false
		for _, unit := range s.Units {
			if unit.BuildTargetID == b.ID {
				hasWorker = true
				break
			}
		}
		if hasWorker {
			continue
		}
		canceled = append(canceled, b.ID)
	}

	for _, id := range canceled {
		b := s.getBuildingByIDLocked(id)
		if b == nil {
			continue
		}
		if b.OwnerID != nil {
			if player, ok := s.Players[*b.OwnerID]; ok {
				if def, ok := getBuildingDef(b.BuildingType); ok {
					for resource, cost := range def.ResourceCost {
						player.Resources[resource] += cost
					}
				}
			}
		}
		s.removeBuildingLocked(id)
	}
}

func (s *GameState) tickBuildingRepairsLocked(dt float64) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]

		hp, maxHp, ok := getBuildingHP(building)
		if !ok || hp >= maxHp {
			continue
		}

		builderCount := 0
		for _, unit := range s.Units {
			if unit.BuildTargetID == building.ID && unit.Building {
				builderCount++
			}
		}
		if builderCount == 0 {
			building.Metadata["builderCount"] = 0
			continue
		}

		building.Metadata["builderCount"] = builderCount

		hpPerSecond := maxHp / 15.0 // fallback: match original barracks rate
		if v, ok := building.Metadata["hpPerSecond"]; ok {
			if f, ok := v.(float64); ok && f > 0 {
				hpPerSecond = f
			}
		}
		newHp := math.Min(maxHp, hp+hpPerSecond*float64(builderCount)*dt)
		building.Metadata["hp"] = newHp

		if newHp >= maxHp {
			// Building complete / fully repaired
			delete(building.Metadata, "underConstruction")
			delete(building.Metadata, "builderCount")
			for _, unit := range s.Units {
				if unit.BuildTargetID == building.ID {
					unit.BuildTargetID = ""
					unit.Building = false
					unit.Status = "Idle"
				}
			}
		}
	}
}

func (s *GameState) RepairBuilding(playerID string, unitIDs []int, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	hp, maxHp, ok := getBuildingHP(building)
	if !ok || hp >= maxHp {
		return // no HP pool or already at full health
	}

	// Count existing builders not in the incoming unit list
	unitIDSet := make(map[int]bool, len(unitIDs))
	for _, id := range unitIDs {
		unitIDSet[id] = true
	}
	existingBuilders := 0
	for _, unit := range s.Units {
		if unit.BuildTargetID == buildingID && !unitIDSet[unit.ID] {
			existingBuilders++
		}
	}

	blocked := s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	added := 0
	for _, unitID := range unitIDs {
		if existingBuilders+added >= maxBuildersPerBuilding {
			break
		}
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || unit.UnitType != "worker" {
			continue
		}
		unit.GatherTargetID = ""
		unit.MiningInside = false
		unit.Building = false

		approachPoints := s.getBuildingApproachPositionsLocked(*building, 1, blocked, &protocol.Vec2{X: unit.X, Y: unit.Y})
		if len(approachPoints) == 0 {
			continue
		}
		s.resetUnitMovementLocked(unit, orderID)
		unit.BuildTargetID = buildingID
		s.assignUnitPath(unit, approachPoints[0], blocked, nil)
		added++
	}
}

func (s *GameState) getBuildingApproachPositionsLocked(building protocol.BuildingTile, count int, blocked map[gridPoint]bool, origin *protocol.Vec2) []protocol.Vec2 {
	if count <= 0 {
		return nil
	}

	candidates := make([]gridPoint, 0, (building.Width+2)*(building.Height+2))
	seen := make(map[gridPoint]bool)

	sortOrigin := protocol.Vec2{
		X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
	}
	if origin != nil {
		sortOrigin = *origin
	}

	for y := building.Y - 1; y <= building.Y+building.Height; y++ {
		for x := building.X - 1; x <= building.X+building.Width; x++ {
			isPerimeter := x == building.X-1 || x == building.X+building.Width || y == building.Y-1 || y == building.Y+building.Height
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
		return distanceSquared(a.X, a.Y, sortOrigin.X, sortOrigin.Y) < distanceSquared(b.X, b.Y, sortOrigin.X, sortOrigin.Y)
	})

	positions := make([]protocol.Vec2, 0, minInt(count, len(candidates)))
	for _, cell := range candidates {
		positions = append(positions, s.gridToWorldCenter(cell))
		if len(positions) >= count {
			break
		}
	}

	return positions
}

func (s *GameState) getUnitExitPositionForBuildingLocked(building protocol.BuildingTile, unit *Unit) *protocol.Vec2 {
	unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
	positions := s.getBuildingApproachPositionsLocked(building, 1, s.getBlockedCellsLocked(), unitPos)
	if len(positions) == 0 {
		return nil
	}
	position := positions[0]
	return &position
}

func (s *GameState) refreshBuildingRuntimeMetadataLocked() {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}

		if building.BuildingType == "goldmine" {
			building.Metadata["currentWorkers"] = s.countWorkersInsideResourceNodeLocked(building.ID)
			building.Metadata["maxWorkers"] = goldmineWorkerCap
		}

		if queue := s.Productions[building.ID]; len(queue) > 0 {
			activeProduction := queue[0]
			building.Metadata["producingUnitType"] = activeProduction.UnitType
			building.Metadata["productionRemainingSeconds"] = activeProduction.RemainingSeconds
			building.Metadata["productionTotalSeconds"] = activeProduction.TotalSeconds
			building.Metadata["productionQueueLength"] = len(queue)
			building.Metadata["queuedUnitTypes"] = joinProductionUnitTypes(queue)
		} else {
			delete(building.Metadata, "producingUnitType")
			delete(building.Metadata, "productionRemainingSeconds")
			delete(building.Metadata, "productionTotalSeconds")
			delete(building.Metadata, "productionQueueLength")
			delete(building.Metadata, "queuedUnitTypes")
		}

		if building.BuildingType == "enemy-spawnpoint" {
			if timer, exists := s.EnemySpawnTimers[building.ID]; exists {
				if timer.RemainingDelay > 0 {
					building.Metadata["spawnTimerRemaining"] = timer.RemainingDelay
					building.Metadata["spawnTimerTotal"] = timer.TotalDelay
					building.Metadata["spawnTimerPhase"] = "delay"
				} else {
					building.Metadata["spawnTimerRemaining"] = timer.RemainingInterval
					building.Metadata["spawnTimerTotal"] = timer.TotalInterval
					building.Metadata["spawnTimerPhase"] = "interval"
				}
			}
		}
	}
}

func (s *GameState) destroyBuildingLocked(buildingID string) {
	// Clear any enemy attack references to this building
	for _, unit := range s.Units {
		if unit.AttackBuildingTargetID == buildingID {
			unit.AttackBuildingTargetID = ""
			unit.Attacking = false
			unit.Status = "Idle"
		}
	}
	// Drop any lingering banked damage-XP entry. The combat path pays out
	// before queuing destruction, so this is only defensive.
	delete(s.buildingDamageDealt, buildingID)

	// Check for a linked "destroyBuilding" victory objective before removal
	// (the building pointer becomes invalid after removeBuildingLocked).
	s.markBuildingObjectiveCompleteLocked(buildingID)

	// Remove the building from the map and invalidate blocked-cells cache.
	s.removeBuildingLocked(buildingID)
}
