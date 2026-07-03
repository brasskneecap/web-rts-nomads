package game

import (
	"math"
	"sort"
	"webrts/server/pkg/protocol"
)

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
		return s.claimSpecificTownhallForPlayerLocked(playerID, building.ID)
	}

	return nil
}

func (s *GameState) claimSpecificTownhallForPlayerLocked(playerID, buildingID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" || building.ID != buildingID {
			continue
		}
		if building.Occupied && (building.OwnerID == nil || *building.OwnerID != playerID) {
			return nil
		}

		ownerID := playerID
		building.OwnerID = &ownerID
		building.Occupied = true
		building.Visible = true
		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}
		def, _ := getBuildingDef("townhall")
		building.Metadata["hp"] = def.MaxHp
		building.Metadata["maxHp"] = def.MaxHp
		building.SpawnUnitTypes = append([]string{}, def.SpawnUnitTypes...)
		// Visibility changed: blocked-cells derived from Visible buildings may differ.
		s.invalidateBlockedCellsLocked()
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
		delete(s.Productions, building.ID)
		// Visibility changed: blocked-cells cache is now stale.
		s.invalidateBlockedCellsLocked()
	}
}

func (s *GameState) claimPlayerStartLocked(playerID string) (*protocol.BuildingTile, *protocol.BuildingTile) {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType == "townhall" && building.OwnerID != nil && *building.OwnerID == playerID {
			return building, s.getLinkedSpawnPointForTownhallLocked(*building)
		}
	}

	spawnPoints := make([]*protocol.BuildingTile, 0)
	for i := range s.MapConfig.Buildings {
		if s.MapConfig.Buildings[i].BuildingType == "spawn-point" {
			spawnPoints = append(spawnPoints, &s.MapConfig.Buildings[i])
		}
	}
	sort.Slice(spawnPoints, func(i, j int) bool {
		return getSpawnFillOrder(spawnPoints[i]) < getSpawnFillOrder(spawnPoints[j])
	})
	for _, spawnPoint := range spawnPoints {
		townhall := s.resolveSpawnPointTownhallLocked(*spawnPoint, false)
		if townhall == nil {
			continue
		}

		claimed := s.claimSpecificTownhallForPlayerLocked(playerID, townhall.ID)
		if claimed != nil {
			return claimed, spawnPoint
		}
	}

	home := s.claimTownhallForPlayerLocked(playerID)
	if home == nil {
		return nil, nil
	}
	return home, s.getLinkedSpawnPointForTownhallLocked(*home)
}

func getSpawnFillOrder(spawnPoint *protocol.BuildingTile) float64 {
	if spawnPoint.Metadata == nil {
		return 0
	}
	if v, ok := getMetadataFloat(spawnPoint.Metadata, "fillOrder"); ok {
		return v
	}
	return 0
}


func (s *GameState) getLinkedSpawnPointForTownhallLocked(home protocol.BuildingTile) *protocol.BuildingTile {
	homeCenter := protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}

	var nearestUnassigned *protocol.BuildingTile
	bestDistance := math.Inf(1)

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "spawn-point" {
			continue
		}

		if linkedTownhallID, ok := getMetadataString(building.Metadata, "townhallId"); ok && linkedTownhallID != "" {
			if linkedTownhallID == home.ID {
				return building
			}
			continue
		}

		center := protocol.Vec2{
			X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
			Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
		}
		dist := distanceSquared(center.X, center.Y, homeCenter.X, homeCenter.Y)
		if dist < bestDistance {
			bestDistance = dist
			nearestUnassigned = building
		}
	}

	return nearestUnassigned
}

func (s *GameState) resolveSpawnPointTownhallLocked(spawnPoint protocol.BuildingTile, allowOccupied bool) *protocol.BuildingTile {
	if linkedTownhallID, ok := getMetadataString(spawnPoint.Metadata, "townhallId"); ok && linkedTownhallID != "" {
		building := s.getBuildingByIDLocked(linkedTownhallID)
		if building == nil || building.BuildingType != "townhall" {
			return nil
		}
		if !allowOccupied && building.Occupied {
			return nil
		}
		return building
	}

	spawnCenter := protocol.Vec2{
		X: (float64(spawnPoint.X) + float64(spawnPoint.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(spawnPoint.Y) + float64(spawnPoint.Height)/2) * s.MapConfig.CellSize,
	}

	var nearest *protocol.BuildingTile
	bestDistance := math.Inf(1)
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType != "townhall" {
			continue
		}
		if !allowOccupied && building.Occupied {
			continue
		}

		center := protocol.Vec2{
			X: (float64(building.X) + float64(building.Width)/2) * s.MapConfig.CellSize,
			Y: (float64(building.Y) + float64(building.Height)/2) * s.MapConfig.CellSize,
		}
		dist := distanceSquared(center.X, center.Y, spawnCenter.X, spawnCenter.Y)
		if dist < bestDistance {
			bestDistance = dist
			nearest = building
		}
	}

	return nearest
}

func (s *GameState) getTownhallSpawnPositionsLocked(home protocol.BuildingTile, count int, blocked map[gridPoint]bool) []protocol.Vec2 {
	if count <= 0 {
		return nil
	}

	homeCenter := protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}
	spawnOrigin := s.getTownhallSpawnOriginLocked(home)
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

	// Connectivity guard: a perimeter cell can be walkable yet sealed off
	// (wedged between the building and trees). Constrain the batch to the
	// best-connected region represented among the candidates so a released
	// unit is never trapped. Keeping the filter candidate-relative (rather
	// than "must reach the map's largest region") preserves intentional
	// enclosures: inside a fully-walled base the interior IS the best
	// represented region, so units still spawn there.
	if bestRegion := s.bestSpawnRegionLocked(candidates); bestRegion != 0 {
		connected := candidates[:0]
		for _, cell := range candidates {
			if s.walkableRegionAtLocked(cell) == bestRegion {
				connected = append(connected, cell)
			}
		}
		candidates = connected
	}

	sort.Slice(candidates, func(i, j int) bool {
		a := s.gridToWorldCenter(candidates[i])
		b := s.gridToWorldCenter(candidates[j])
		return distanceSquared(a.X, a.Y, spawnOrigin.X, spawnOrigin.Y) < distanceSquared(b.X, b.Y, spawnOrigin.X, spawnOrigin.Y)
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

func (s *GameState) getTownhallSpawnOriginLocked(home protocol.BuildingTile) protocol.Vec2 {
	if home.Metadata != nil {
		x, xOk := getMetadataFloat(home.Metadata, "spawnPointX")
		y, yOk := getMetadataFloat(home.Metadata, "spawnPointY")
		if xOk && yOk {
			return protocol.Vec2{
				X: clampFloat(x, unitRadius, s.MapWidth-unitRadius),
				Y: clampFloat(y, unitRadius, s.MapHeight-unitRadius),
			}
		}
	}

	return protocol.Vec2{
		X: (float64(home.X) + float64(home.Width)/2) * s.MapConfig.CellSize,
		Y: (float64(home.Y) + float64(home.Height)/2) * s.MapConfig.CellSize,
	}
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

func (s *GameState) findOwnedTownhallLocked(ownerID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.BuildingType == "townhall" && building.Visible && building.OwnerID != nil && *building.OwnerID == ownerID {
			return building
		}
	}
	return nil
}

// findNearestDepositPointLocked returns the closest owned deposit-point building
// to the given world position, so workers always return to the nearest townhall.
func (s *GameState) findNearestDepositPointLocked(ownerID string, x, y float64) *protocol.BuildingTile {
	var best *protocol.BuildingTile
	bestDistSq := math.MaxFloat64

	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !b.Visible || b.OwnerID == nil || *b.OwnerID != ownerID {
			continue
		}
		if !containsString(b.Capabilities, "deposit-point") {
			continue
		}
		centerX := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		centerY := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		d := distanceSquared(x, y, centerX, centerY)
		if d < bestDistSq {
			bestDistSq = d
			best = b
		}
	}

	return best
}

func (s *GameState) getNearestPlayerTownhallCenterLocked(x, y float64) *protocol.Vec2 {
	var best *protocol.Vec2
	bestDistSq := math.MaxFloat64

	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || !b.Occupied || !b.Visible {
			continue
		}
		cx := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		cy := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		distSq := distanceSquared(x, y, cx, cy)
		if distSq < bestDistSq {
			bestDistSq = distSq
			pos := protocol.Vec2{X: cx, Y: cy}
			best = &pos
		}
	}

	return best
}

// getNearestPlayerTownhallBuildingLocked returns the live, occupied,
// non-enemy townhall building geographically nearest to (x,y), or nil. The
// building-returning companion to getNearestPlayerTownhallCenterLocked, used
// to seed an enemy's sticky ObjectiveBuildingID at spawn. Deterministic: ties
// resolve to the lower building ID.
func (s *GameState) getNearestPlayerTownhallBuildingLocked(x, y float64) *protocol.BuildingTile {
	var best *protocol.BuildingTile
	bestDistSq := math.MaxFloat64
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || !b.Occupied || !b.Visible {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID == enemyPlayerID {
			continue
		}
		// Don't seed an attack objective on a pending-start ghost — it isn't
		// attackable until a worker begins construction.
		if buildingPendingStart(b) {
			continue
		}
		hp, _, ok := getBuildingHP(b)
		if !ok || hp <= 0 {
			continue
		}
		cx := (float64(b.X) + float64(b.Width)/2) * s.MapConfig.CellSize
		cy := (float64(b.Y) + float64(b.Height)/2) * s.MapConfig.CellSize
		distSq := distanceSquared(x, y, cx, cy)
		if distSq < bestDistSq || (distSq == bestDistSq && (best == nil || b.ID < best.ID)) {
			bestDistSq = distSq
			best = b
		}
	}
	return best
}
