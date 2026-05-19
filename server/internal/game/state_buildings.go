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

	if def.RequiresTownhallTier > 0 &&
		s.townhallTierForPlayerLocked(playerID) < def.RequiresTownhallTier {
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
	// Stamp tier=1 on newly constructed townhalls so upgrade cap and tier-up
	// logic always find a baseline value in metadata["tier"].
	if buildingType == "townhall" {
		if _, hasTier := metadata["tier"]; !hasTier {
			metadata["tier"] = float64(1)
		}
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

// updateWorkerBuildStateLocked is called each tick for a worker that has
// BuildTargetID set but is not yet in Building state. When the worker has
// arrived at the approach cell, it transitions into either inside-builder or
// outside-helper mode.
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

	underConstruction := getMetadataBool(building.Metadata, "underConstruction")
	existingInside := s.findInsideBuilderLocked(building.ID)

	if underConstruction && existingInside == nil {
		// This is the first worker on an under-construction building — become
		// the inside builder.
		unit.Building = true
		unit.InsideBuilder = true
		unit.Visible = false
		unit.Status = "Building"
		s.snapUnitToBuildingInteriorLocked(unit, building)
	} else {
		// Either the building is not under construction (repair case) or there
		// is already an inside builder; this worker stays as an outside helper.
		unit.Building = true
		unit.InsideBuilder = false
		unit.Visible = true
		unit.Status = "Repairing"
	}
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

// findInsideBuilderLocked scans s.Units for the unit that is the inside
// builder for buildingID. Returns nil if none exists.
// Must be called under s.mu lock.
func (s *GameState) findInsideBuilderLocked(buildingID string) *Unit {
	for _, u := range s.Units {
		if u.BuildTargetID == buildingID && u.InsideBuilder && u.HP > 0 {
			return u
		}
	}
	return nil
}

// promoteHelperToInsideBuilderLocked finds the outside helper with the
// lowest unit ID assigned to buildingID and promotes them to inside builder.
// Only considers workers that have arrived at the build site
// (Building == true) — workers still walking toward the building are not
// eligible, otherwise they would be teleported into the footprint mid-path.
// Returns the promoted unit, or nil if there are no eligible helpers.
// Must be called under s.mu write lock.
func (s *GameState) promoteHelperToInsideBuilderLocked(buildingID string, building *protocol.BuildingTile) *Unit {
	var best *Unit
	for _, u := range s.Units {
		if u.BuildTargetID != buildingID || u.InsideBuilder || u.HP <= 0 {
			continue
		}
		if !u.Building {
			continue
		}
		if best == nil || u.ID < best.ID {
			best = u
		}
	}
	if best == nil {
		return nil
	}
	best.InsideBuilder = true
	best.Visible = false
	best.Status = "Building"
	s.snapUnitToBuildingInteriorLocked(best, building)
	return best
}

// snapUnitToBuildingInteriorLocked places the unit at the building center.
// Must be called under s.mu write lock.
func (s *GameState) snapUnitToBuildingInteriorLocked(unit *Unit, building *protocol.BuildingTile) {
	center := s.buildingCenterLocked(building)
	unit.X = center.X
	unit.Y = center.Y
	unit.TargetX = center.X
	unit.TargetY = center.Y
}

// clearUnitBuildStateLocked clears all build-related state on unit and sets
// it to Visible and Idle. Does NOT set a path or position — callers must handle
// that if the unit is InsideBuilder.
// Must be called under s.mu write lock.
func (s *GameState) clearUnitBuildStateLocked(unit *Unit) {
	unit.BuildTargetID = ""
	unit.Building = false
	unit.InsideBuilder = false
	unit.RepairChargeAccumulator = 0
	unit.Visible = true
	unit.Status = "Idle"
}

// kickWorkerOffBuildLocked evicts a single worker from a building. If the unit
// was the inside builder it is snapped to a perimeter exit cell first.
// blocked is the current blocked-cell set (caller's responsibility to obtain
// it via getBlockedCellsLocked before the loop).
// Must be called under s.mu write lock.
func (s *GameState) kickWorkerOffBuildLocked(unit *Unit, building *protocol.BuildingTile, blocked map[gridPoint]bool) {
	if unit.InsideBuilder {
		// Move the unit out of the footprint before clearing state so it does
		// not materialise inside a blocked cell.
		unitPos := &protocol.Vec2{X: unit.X, Y: unit.Y}
		exits := s.getBuildingApproachPositionsLocked(*building, 1, blocked, unitPos)
		if len(exits) > 0 {
			unit.X = exits[0].X
			unit.Y = exits[0].Y
			unit.TargetX = exits[0].X
			unit.TargetY = exits[0].Y
		}
	}
	s.clearUnitBuildStateLocked(unit)
}

// tickBuildingRepairsLocked advances construction and repair for all buildings
// that have HP < MaxHP, applying the resource-charge algorithm for paying workers.
// Must be called under s.mu write lock.
func (s *GameState) tickBuildingRepairsLocked(dt float64) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]

		hp, maxHp, ok := getBuildingHP(building)
		if !ok || hp >= maxHp {
			continue
		}

		underConstruction := getMetadataBool(building.Metadata, "underConstruction")

		// Promotion: if no inside builder but we are under construction, pick
		// the lowest-ID helper.
		inside := s.findInsideBuilderLocked(building.ID)
		if inside == nil && underConstruction {
			inside = s.promoteHelperToInsideBuilderLocked(building.ID, building)
		}

		// Collect outside helpers (those with BuildTargetID but not InsideBuilder).
		// Re-scan after promotion so the promoted unit is not double-counted.
		// Workers en route to the build site (Building == false) are skipped —
		// they neither contribute HP nor count toward builder presence until
		// they arrive and updateWorkerBuildStateLocked transitions them.
		var helpers []*Unit
		for _, u := range s.Units {
			if u.BuildTargetID == building.ID && !u.InsideBuilder && u.Building && u.HP > 0 {
				helpers = append(helpers, u)
			}
		}

		if inside == nil && len(helpers) == 0 {
			building.Metadata["builderCount"] = 0
			continue
		}

		builderCount := len(helpers)
		if inside != nil {
			builderCount++
		}
		building.Metadata["builderCount"] = builderCount

		hpPerSecond := maxHp / 15.0 // fallback: match original barracks rate
		if v, ok := building.Metadata["hpPerSecond"]; ok {
			if f, ok2 := v.(float64); ok2 && f > 0 {
				hpPerSecond = f
			}
		}

		// Apply HP from inside builder (free during construction).
		if inside != nil {
			hpThisTick := hpPerSecond * dt
			if underConstruction {
				// Free — apply directly.
				hp = math.Min(maxHp, hp+hpThisTick)
				building.Metadata["hp"] = hp
				inside.Status = "Building"
			} else {
				// Building is complete but damaged — inside-builder pays too.
				player := s.Players[inside.OwnerID]
				hp = s.applyChargedHPLocked(inside, building, hp, maxHp, hpThisTick, player, "Building", "Building (Paused)")
			}
		}

		// Apply HP from outside helpers — all pay.
		for _, h := range helpers {
			if hp >= maxHp {
				break
			}
			hpThisTick := hpPerSecond * dt
			player := s.Players[h.OwnerID]
			hp = s.applyChargedHPLocked(h, building, hp, maxHp, hpThisTick, player, "Repairing", "Repairing (Paused)")
		}

		// Update the stored hp (applyChargedHPLocked also writes into building.Metadata["hp"]).
		// If hp reached maxHp, complete the build/repair.
		if hp >= maxHp {
			delete(building.Metadata, "underConstruction")
			delete(building.Metadata, "builderCount")
			// Defensive: pendingStart is normally cleared on first worker
			// arrival (updateWorkerBuildStateLocked). If construction completes
			// without that path firing for any reason, drop it here so the
			// finished building never renders as the 40%-alpha ghost preview.
			delete(building.Metadata, "pendingStart")
			// Clear all assigned workers.
			for _, u := range s.Units {
				if u.BuildTargetID == building.ID {
					// Inside builder exits the footprint.
					if u.InsideBuilder {
						blocked := s.getBlockedCellsLocked()
						unitPos := &protocol.Vec2{X: u.X, Y: u.Y}
						exits := s.getBuildingApproachPositionsLocked(*building, 1, blocked, unitPos)
						if len(exits) > 0 {
							u.X = exits[0].X
							u.Y = exits[0].Y
							u.TargetX = exits[0].X
							u.TargetY = exits[0].Y
						}
					}
					s.clearUnitBuildStateLocked(u)
				}
			}
		}
	}
}

// applyChargedHPLocked applies up to hpThisTick HP to the building for the
// given worker, charging 1g+1w per 5 HP crossed. Returns the new building HP.
// activeStatus is set when the worker contributes HP; pausedStatus when the
// player cannot afford the charge and the worker is blocked.
// Must be called under s.mu write lock.
func (s *GameState) applyChargedHPLocked(
	worker *Unit,
	building *protocol.BuildingTile,
	currentHP, maxHP float64,
	hpThisTick float64,
	player *Player,
	activeStatus, pausedStatus string,
) float64 {
	hp := currentHP
	remaining := hpThisTick

	for remaining > 0 && hp < maxHP {
		needed := 5.0 - worker.RepairChargeAccumulator
		if remaining < needed {
			// Accumulate partial progress; no charge yet.
			worker.RepairChargeAccumulator += remaining
			hp = math.Min(maxHP, hp+remaining)
			building.Metadata["hp"] = hp
			worker.Status = activeStatus
			return hp
		}
		// We would cross the 5-HP threshold. Check if the player can afford it.
		if player == nil || player.Resources["gold"] < 1 || player.Resources["wood"] < 1 {
			// Paused — stop contributing entirely this tick.
			worker.Status = pausedStatus
			return hp
		}
		// Deduct resources, commit the HP that gets us exactly to the boundary.
		player.Resources["gold"]--
		player.Resources["wood"]--
		hp = math.Min(maxHP, hp+needed)
		building.Metadata["hp"] = hp
		worker.RepairChargeAccumulator = 0
		remaining -= needed
	}

	if remaining <= 0 || hp >= maxHP {
		worker.Status = activeStatus
	}
	return hp
}

// RepairBuilding assigns workers to repair a damaged completed building.
// Workers already assigned to the same building are left untouched; only the
// new unitIDs are processed. This preserves InsideBuilder and
// RepairChargeAccumulator on existing assignees.
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

	// Count existing workers for the capacity check. Only units NOT in the
	// incoming list count toward the existing slots.
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

		// If this unit is already assigned to this building, leave its state
		// intact (InsideBuilder, RepairChargeAccumulator, Building) — idempotent.
		if unit.BuildTargetID == buildingID {
			added++
			continue
		}

		unit.GatherTargetID = ""
		unit.MiningInside = false
		unit.Building = false
		unit.InsideBuilder = false
		unit.RepairChargeAccumulator = 0

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

// KickBuildersFromBuilding clears all workers assigned to buildingID. The
// inside builder is snapped to a perimeter exit cell. The building remains
// under construction at its current HP. pendingStart is NOT re-set (no refund).
func (s *GameState) KickBuildersFromBuilding(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	blocked := s.getBlockedCellsLocked()
	for _, u := range s.Units {
		if u.BuildTargetID != buildingID {
			continue
		}
		s.kickWorkerOffBuildLocked(u, building, blocked)
	}

	// Remove builderCount from metadata since no workers remain.
	if building.Metadata != nil {
		building.Metadata["builderCount"] = 0
	}
}

// DemolishBuilding destroys an under-construction building, refunds the full
// resource cost to the owner, and clears all assigned workers. Only valid on
// buildings that are currently under construction.
func (s *GameState) DemolishBuilding(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}
	if !getMetadataBool(building.Metadata, "underConstruction") {
		return
	}

	// Refund full resource cost.
	if player, ok := s.Players[playerID]; ok {
		if def, ok := getBuildingDef(building.BuildingType); ok {
			for resource, cost := range def.ResourceCost {
				player.Resources[resource] += cost
			}
		}
	}

	// Clear all assigned workers — same cleanup as kick.
	blocked := s.getBlockedCellsLocked()
	for _, u := range s.Units {
		if u.BuildTargetID != buildingID {
			continue
		}
		s.kickWorkerOffBuildLocked(u, building, blocked)
	}

	s.removeBuildingLocked(buildingID)
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

	// Cardinal-perimeter cells (sharing the building's edge column or row)
	// always sort before diagonal corner cells, with distance-to-origin
	// breaking ties within each group. Workers chopping a tree or building
	// against a barracks face the structure straight-on rather than at a
	// 45° angle whenever a cardinal approach is reachable; corner cells
	// still serve as the fallback when every cardinal is blocked.
	isCardinalCell := func(c gridPoint) bool {
		insideXSpan := c.X >= building.X && c.X < building.X+building.Width
		insideYSpan := c.Y >= building.Y && c.Y < building.Y+building.Height
		return insideXSpan || insideYSpan
	}
	sort.Slice(candidates, func(i, j int) bool {
		ci := isCardinalCell(candidates[i])
		cj := isCardinalCell(candidates[j])
		if ci != cj {
			return ci
		}
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
			// Cancel any in-flight swing against the destroyed building so
			// the attacker drops out of attack state cleanly. Reset
			// AttackCooldown so the next engagement's animation aligns
			// with damage — see the matching block in removeUnitLocked
			// for the timing rationale.
			unit.AttackWindupRemaining = 0
			unit.AttackCooldown = 0
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
