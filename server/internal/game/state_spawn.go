package game

import (
	"log/slog"

	"webrts/server/pkg/protocol"
)


func (s *GameState) spawnPlayerUnitLocked(unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	def, ok := getUnitDef(unitType)
	if !ok {
		return nil
	}
	return s.spawnUnitFromDefLocked(def, unitType, playerID, color, spawn)
}

func (s *GameState) spawnUnitFromDefLocked(def UnitDef, unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	unit := &Unit{
		ID:                 s.nextUnitID,
		OwnerID:            playerID,
		Color:              color,
		UnitType:           unitType,
		Archetype:          resolveUnitArchetype(def, unitType),
		Name:               def.Name,
		Capabilities:       append([]string{}, def.Capabilities...),
		NonCombat:          def.NonCombat,
		Visible:            true,
		Status:             "Idle",
		X:                  spawn.X,
		Y:                  spawn.Y,
		HP:                 def.HP,
		MaxHP:              def.HP,
		BaseMaxHP:          def.HP,
		BaseDamage:         def.Damage,
		BaseAttackSpeed:    def.AttackSpeed,
		BaseMoveSpeed:      def.MoveSpeed,
		Damage:             def.Damage,
		AttackRange:        def.AttackRange,
		BaseAttackRange:    def.AttackRange,
		AttackSpeed:        def.AttackSpeed,
		MoveSpeed:          def.MoveSpeed,
		HealthRegenPerSecond: defaultHealthRegenPerSecond,
		Rank:               unitRankBase,
		ProgressionPath:    unitPathNone,
		CombatAnchorX:      spawn.X,
		CombatAnchorY:      spawn.Y,
		ThreatTable:        map[int]*ThreatEntry{},
		TankedDamageByUnit: map[int]float64{},
		DamageDealtByUnit:  map[int]int{},
	}

	s.nextUnitID++
	s.addUnitLocked(unit)
	s.initializeCombatUnitLocked(unit)
	// Apply permanent player upgrades before rank modifiers so that the upgrade
	// bonuses to Base* stats are included in the first applyRankModifiersLocked
	// pass. Only applies to player-owned units (enemy player has no upgrades).
	if playerID != enemyPlayerID {
		s.applyPlayerUpgradesAtSpawnLocked(unit)
	}
	s.applyRankModifiersLocked(unit, false)
	// Initialise inventory slots for player-owned units. At spawn the rank is
	// always "base" (InventorySize = 0), but calling here ensures future code
	// paths that spawn higher-rank units (e.g. debug_spawn) work correctly.
	if playerID != enemyPlayerID {
		s.setInventorySizeForRankLocked(unit)
		unit.Equipped = make([]*EquippedItem, unit.InventorySize)
	}
	return unit
}

func (s *GameState) spawnRaiderUnitLocked(playerID, color string, spawn protocol.Vec2) *Unit {
	unit := &Unit{
		ID:                 s.nextUnitID,
		OwnerID:            playerID,
		Color:              color,
		UnitType:           "raider",
		Archetype:          "raider",
		Name:               "Raider",
		Capabilities:       []string{"move", "attack"},
		Visible:            true,
		Status:             "Idle",
		X:                  spawn.X,
		Y:                  spawn.Y,
		HP:                 raiderHP,
		MaxHP:              raiderMaxHP,
		BaseMaxHP:          raiderMaxHP,
		BaseDamage:         raiderDamage,
		BaseAttackSpeed:    raiderAttackSpeed,
		BaseMoveSpeed:      raiderMoveSpeed,
		MoveSpeed:          raiderMoveSpeed,
		Damage:             raiderDamage,
		AttackRange:        raiderAttackRange,
		BaseAttackRange:    raiderAttackRange,
		AttackSpeed:        raiderAttackSpeed,
		HealthRegenPerSecond: defaultHealthRegenPerSecond,
		Rank:               unitRankBase,
		ProgressionPath:    unitPathNone,
		CombatAnchorX:      spawn.X,
		CombatAnchorY:      spawn.Y,
		ThreatTable:        map[int]*ThreatEntry{},
		TankedDamageByUnit: map[int]float64{},
		DamageDealtByUnit:  map[int]int{},
	}

	s.nextUnitID++
	s.addUnitLocked(unit)
	s.initializeCombatUnitLocked(unit)
	s.applyRankModifiersLocked(unit, false)
	return unit
}

func (s *GameState) spawnEnemyUnitLocked(unitType string, spawn protocol.Vec2) *Unit {
	if def, ok := getUnitDef(unitType); ok {
		return s.spawnUnitFromDefLocked(def, unitType, enemyPlayerID, enemyPlayerColor, spawn)
	}
	switch unitType {
	case "raider":
		return s.spawnRaiderUnitLocked(enemyPlayerID, enemyPlayerColor, spawn)
	default:
		return s.spawnRaiderUnitLocked(enemyPlayerID, enemyPlayerColor, spawn)
	}
}

func resolveUnitArchetype(def UnitDef, unitType string) string {
	if def.Archetype != "" {
		return def.Archetype
	}
	return unitType
}

// findPlayerLabelLocked returns the playerLabel metadata value from the
// spawn-point building whose linked townhall is owned by playerID. Returns ""
// when no matching spawn-point exists (e.g. the player joined on a map that
// has no labelled spawn-points, or the player was not matched to one).
func (s *GameState) findPlayerLabelLocked(playerID string) string {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "spawn-point" {
			continue
		}
		pl, ok := getMetadataString(b.Metadata, "playerLabel")
		if !ok || pl == "" {
			continue
		}
		townhall := s.resolveSpawnPointTownhallLocked(*b, true)
		if townhall == nil || townhall.OwnerID == nil || *townhall.OwnerID != playerID {
			continue
		}
		return pl
	}
	return ""
}

// spawnPlacedUnitsForPlayerLocked spawns authored player-owned placed units
// whose PlayerLabel matches the label of the townhall slot claimed by playerID.
// Must be called under s.mu write lock.
func (s *GameState) spawnPlacedUnitsForPlayerLocked(playerID, color string) {
	slog.Info("spawnPlacedUnitsForPlayerLocked", "playerID", playerID, "totalPlacedUnits", len(s.MapConfig.PlacedUnits))
	if len(s.MapConfig.PlacedUnits) == 0 {
		return
	}
	playerLabel := s.findPlayerLabelLocked(playerID)
	slog.Info("spawnPlacedUnitsForPlayerLocked", "playerLabel", playerLabel)
	if playerLabel == "" {
		// Player has no labelled slot — no authored units to place.
		return
	}
	blocked := s.getBlockedCellsLocked()
	cellSize := s.MapConfig.CellSize
	for _, entry := range s.MapConfig.PlacedUnits {
		if entry.Owner != "player" || entry.PlayerLabel != playerLabel {
			continue
		}
		worldX := float64(entry.X)*cellSize + cellSize/2
		worldY := float64(entry.Y)*cellSize + cellSize/2
		cell := s.worldToGrid(worldX, worldY)
		spawnCell, ok := s.findNearestWalkable(cell, blocked)
		if !ok {
			slog.Warn("spawnPlacedUnitsForPlayerLocked: no walkable cell found for placed unit; skipping",
				"playerID", playerID, "unitType", entry.UnitType, "gridX", entry.X, "gridY", entry.Y)
			continue
		}
		spawnPos := s.gridToWorldCenter(spawnCell)
		unit := s.spawnPlayerUnitLocked(entry.UnitType, playerID, color, spawnPos)
		if unit == nil {
			slog.Warn("spawnPlacedUnitsForPlayerLocked: spawnPlayerUnitLocked returned nil; skipping",
				"playerID", playerID, "unitType", entry.UnitType)
		}
	}
}

// spawnPlacedEnemyUnitsLocked spawns authored enemy placed units as stationary
// guards. Must be called under s.mu write lock.
func (s *GameState) spawnPlacedEnemyUnitsLocked() {
	if len(s.MapConfig.PlacedUnits) == 0 {
		return
	}
	blocked := s.getBlockedCellsLocked()
	cellSize := s.MapConfig.CellSize
	for _, entry := range s.MapConfig.PlacedUnits {
		if entry.Owner != "enemy" {
			continue
		}
		worldX := float64(entry.X)*cellSize + cellSize/2
		worldY := float64(entry.Y)*cellSize + cellSize/2
		cell := s.worldToGrid(worldX, worldY)
		spawnCell, ok := s.findNearestWalkable(cell, blocked)
		if !ok {
			slog.Warn("spawnPlacedEnemyUnitsLocked: no walkable cell found for placed enemy; skipping",
				"unitType", entry.UnitType, "gridX", entry.X, "gridY", entry.Y)
			continue
		}
		spawnPos := s.gridToWorldCenter(spawnCell)
		unit := s.spawnEnemyUnitLocked(entry.UnitType, spawnPos)
		if unit == nil {
			slog.Warn("spawnPlacedEnemyUnitsLocked: spawnEnemyUnitLocked returned nil; skipping",
				"unitType", entry.UnitType)
			continue
		}
		unit.GuardMode = true
		unit.GuardAnchorX = spawnPos.X
		unit.GuardAnchorY = spawnPos.Y
		unit.GuardAggroRange = entry.AggroRange
		unit.GuardLeashRange = entry.LeashRange
		unit.IgnoreWaveClear = true
		unit.Order = OrderState{
			Type:  OrderHold,
			HoldX: spawnPos.X,
			HoldY: spawnPos.Y,
		}
		unit.CombatAnchorX = spawnPos.X
		unit.CombatAnchorY = spawnPos.Y
		unit.Status = "Guarding"
	}
}

// ensurePlacedEnemiesSpawnedLocked spawns authored enemy guard units exactly
// once per match. Idempotent — returns immediately when already spawned.
// Must be called under s.mu write lock.
func (s *GameState) ensurePlacedEnemiesSpawnedLocked() {
	if s.PlacedEnemiesSpawned {
		return
	}
	s.ensureEnemyPlayerLocked()
	s.spawnPlacedEnemyUnitsLocked()
	s.PlacedEnemiesSpawned = true
}
