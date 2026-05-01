package game

import "webrts/server/pkg/protocol"

type PlayerStartUnit struct {
	UnitType string
	Count    int
}

func (s *GameState) spawnUnitsForPlayerLocked(playerID, color string, home *protocol.BuildingTile, loadout []PlayerStartUnit) {
	totalCount := 0
	for _, entry := range loadout {
		if entry.Count > 0 {
			totalCount += entry.Count
		}
	}
	if totalCount <= 0 {
		return
	}

	playerIndex := len(s.Players) - 1
	blocked := s.getBlockedCellsLocked()
	spawnPositions := make([]protocol.Vec2, 0, totalCount)

	if home != nil {
		spawnPositions = s.getTownhallSpawnPositionsLocked(*home, totalCount, blocked)
	}

	if len(spawnPositions) < totalCount {
		spawnPositions = append(spawnPositions, s.getFallbackSpawnPositionsLocked(playerIndex, totalCount-len(spawnPositions), blocked)...)
	}
	if len(spawnPositions) == 0 {
		return
	}

	spawnIndex := 0
	for _, entry := range loadout {
		if entry.Count <= 0 {
			continue
		}
		for i := 0; i < entry.Count; i++ {
			spawn := spawnPositions[minInt(spawnIndex, len(spawnPositions)-1)]
			s.spawnPlayerUnitLocked(entry.UnitType, playerID, color, spawn)
			spawnIndex++
		}
	}
}

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
	s.applyRankModifiersLocked(unit, false)
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
