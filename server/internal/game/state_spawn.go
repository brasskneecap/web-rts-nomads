package game

import (
	"encoding/json"
	"log/slog"

	"webrts/server/pkg/protocol"
)

// resolveTargetableTypes returns the effective TargetableTypes for a unit
// def. Explicit authored values win. When absent, projectile attacks default
// to both ground and flyer (a ranged shot naturally arcs up); every other
// attack — melee or otherwise — defaults to ground only and must explicitly
// opt in to anti-air.
func resolveTargetableTypes(def UnitDef) []string {
	if len(def.TargetableTypes) > 0 {
		return append([]string(nil), def.TargetableTypes...)
	}
	if len(def.AttackVisual) > 0 {
		var visual struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(def.AttackVisual, &visual); err == nil && visual.Kind == "projectile" {
			return []string{TargetClassGround, TargetClassFlyer}
		}
	}
	return []string{TargetClassGround}
}


func (s *GameState) spawnPlayerUnitLocked(unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	def, ok := getUnitDef(unitType)
	if !ok {
		return nil
	}
	return s.spawnUnitFromDefLocked(def, unitType, playerID, color, spawn)
}

func (s *GameState) spawnUnitFromDefLocked(def UnitDef, unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	baseVision := def.VisionRange
	if baseVision == 0 {
		baseVision = defaultVisionRange
	}
	unit := &Unit{
		ID:                 s.nextUnitID,
		OwnerID:            playerID,
		Color:              color,
		UnitType:           unitType,
		Archetype:          resolveUnitArchetype(def, unitType),
		Name:               def.Name,
		Capabilities:       append([]string{}, def.Capabilities...),
		NonCombat:          def.NonCombat,
		Flyer:              def.Flyer,
		TargetableTypes:    resolveTargetableTypes(def),
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
		SplashRadius:       def.SplashRadius,
		BaseVisionRange:    baseVision,
		VisionRange:        baseVision,
		HealthRegenPerSecond: defaultHealthRegenPerSecond,
		// Spellcaster kit (zero values for non-casters). CurrentMana starts
		// full per the Acolyte spec.
		MaxMana:            def.MaxMana,
		CurrentMana:        def.MaxMana,
		ManaRegenPerSecond: def.ManaRegenRate,
		ProjectileID:       def.Projectile,
		AttackDamageType:   def.DamageType,
		ProjectileScale:    def.ProjectileScale,
		Abilities:          append([]string{}, def.Abilities...),
		Rank:               unitRankBase,
		XPValue:            resolveUnitXPValue(def),
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
	// pass. Only applies to real player-owned units — the enemy AI and the
	// neutral camp faction have no upgrade tracks, and their Player entries
	// leave PhysicalDamageMultiplier/MagicDamageMultiplier at zero, which would
	// otherwise zero BaseDamage and silently disable their combat AI
	// (unitUsesCombatAI gates on Damage > 0).
	if playerID != enemyPlayerID && playerID != neutralPlayerID {
		s.applyPlayerUpgradesAtSpawnLocked(unit)
	}
	s.applyRankModifiersLocked(unit, false)
	// Initialise inventory slots for player-owned units. At spawn the rank is
	// always "base" (InventorySize = 0), but calling here ensures future code
	// paths that spawn higher-rank units (e.g. debug_spawn) work correctly.
	if playerID != enemyPlayerID && playerID != neutralPlayerID {
		s.setInventorySizeForRankLocked(unit)
		unit.Equipped = make([]*EquippedItem, unit.InventorySize)
	}
	// Seed default auto-cast for any spawned ability whose def declares
	// DefaultAutoCast. Applies to enemy units too — they are AI-controlled
	// and must use their abilities (e.g. the necromancer's raise_skeleton);
	// player toggles never reach them so there is no choice to preserve.
	// Idempotent: only adds entries that don't already exist.
	s.seedDefaultAutoCastLocked(unit)
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
		TargetableTypes:    []string{TargetClassGround},
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
		BaseVisionRange:    defaultVisionRange,
		VisionRange:        defaultVisionRange,
		HealthRegenPerSecond: defaultHealthRegenPerSecond,
		Rank:               unitRankBase,
		XPValue:            gameplayTuning().Experience.SplitDefaultXP,
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

// spawnNeutralUnitLocked materializes a single unit under the neutral player
// slot. Mirrors spawnEnemyUnitLocked but uses neutralPlayerID/neutralPlayerColor
// so the unit is owned by the virtual neutral faction rather than the enemy
// faction. The caller (spawnGroupForCampLocked) is responsible for calling
// ensureNeutralPlayerLocked before the spawn loop and for setting guard-mode
// fields after this returns. Returns nil when unitType is unknown.
func (s *GameState) spawnNeutralUnitLocked(unitType string, spawn protocol.Vec2) *Unit {
	if def, ok := getUnitDef(unitType); ok {
		return s.spawnUnitFromDefLocked(def, unitType, neutralPlayerID, neutralPlayerColor, spawn)
	}
	// Fallback for legacy hardcoded types (e.g. bare "raider" without a catalog entry).
	switch unitType {
	case "raider":
		return s.spawnRaiderUnitLocked(neutralPlayerID, neutralPlayerColor, spawn)
	default:
		return nil
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

// claimLabeledBuildingsForPlayerLocked walks every authored player-class
// building (other than townhalls and spawn-points, which have their own claim
// paths) whose metadata.playerLabel matches the label of the townhall slot
// claimed by playerID, and assigns ownership: OwnerID, Visible, Occupied,
// hp/maxHp from the catalog def, and SpawnUnitTypes copied from the def for
// any unit-spawner. Idempotent — buildings that are already owned by another
// player or that lack a matching playerLabel are skipped. Must be called
// under s.mu write lock.
func (s *GameState) claimLabeledBuildingsForPlayerLocked(playerID string) {
	playerLabel := s.findPlayerLabelLocked(playerID)
	if playerLabel == "" {
		return
	}
	changed := false
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType == "townhall" || b.BuildingType == "spawn-point" {
			continue
		}
		if b.OwnerID != nil {
			continue
		}
		lbl, ok := getMetadataString(b.Metadata, "playerLabel")
		if !ok || lbl != playerLabel {
			continue
		}
		def, ok := getBuildingDef(b.BuildingType)
		if !ok {
			continue
		}
		owner := playerID
		b.OwnerID = &owner
		b.Visible = true
		b.Occupied = true
		if b.Metadata == nil {
			b.Metadata = map[string]interface{}{}
		}
		b.Metadata["hp"] = def.MaxHp
		b.Metadata["maxHp"] = def.MaxHp
		// pre-built ⇒ never underConstruction / pendingStart
		delete(b.Metadata, "underConstruction")
		delete(b.Metadata, "pendingStart")
		if len(b.SpawnUnitTypes) == 0 && len(def.SpawnUnitTypes) > 0 {
			b.SpawnUnitTypes = append([]string{}, def.SpawnUnitTypes...)
		}
		if len(b.Capabilities) == 0 && len(def.Capabilities) > 0 {
			b.Capabilities = append([]string{}, def.Capabilities...)
		}
		changed = true
	}
	if changed {
		s.invalidateBlockedCellsLocked()
	}
}

// spawnPlacedUnitsForPlayerLocked spawns authored player-owned placed units
// whose PlayerLabel matches the label of the townhall slot claimed by playerID.
// Must be called under s.mu write lock.
func (s *GameState) spawnPlacedUnitsForPlayerLocked(playerID, color string) {
	if len(s.MapConfig.PlacedUnits) == 0 {
		return
	}
	playerLabel := s.findPlayerLabelLocked(playerID)
	if playerLabel == "" {
		// Player has no labelled slot — no authored units to place.
		return
	}
	blocked := s.getBlockedCellsLocked()
	cellSize := s.MapConfig.CellSize
	for _, entry := range s.MapConfig.PlacedUnits {
		if entry.PlayerSlot != playerLabel {
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
	// Shared OrderID across all painted enemies so they exclude each other from
	// the fine pathmap (state_movement.go same-OrderID rule). Without this,
	// dense painted clusters saturate the pathmap with 22px separation circles
	// and tickGuardReturnLocked spams A* every tick on every unit.
	placedOrderID := s.nextMovementOrderIDLocked()
	for _, entry := range s.MapConfig.PlacedUnits {
		if entry.PlayerSlot != "enemy" {
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
		unit.OrderID = placedOrderID
		unit.GuardMode = true
		unit.GuardAnchorX = spawnPos.X
		unit.GuardAnchorY = spawnPos.Y
		// Floor authored aggro at guardMinAggroRange so a player unit walking
		// near a guard reliably triggers passive acquisition rather than
		// having to either step into AttackRange or take a hit first. Authored
		// values above the floor are respected.
		unit.GuardAggroRange = entry.AggroRange
		if unit.GuardAggroRange < guardMinAggroRange {
			unit.GuardAggroRange = guardMinAggroRange
		}
		// Leash must cover at least the aggro radius — otherwise a target
		// inside aggro but past leash is acquired (selectBestTargetLocked uses
		// AggroRange) and immediately dropped (shouldDropCurrentTargetLocked
		// uses LeashRange), the visible chase/drop juggling. Authored leash
		// above the aggro floor is respected.
		unit.GuardLeashRange = entry.LeashRange
		if unit.GuardLeashRange < unit.GuardAggroRange {
			unit.GuardLeashRange = unit.GuardAggroRange
		}
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

// spawnExtraStartingWorkersLocked spawns extra worker units for a player whose
// profile upgrade ExtraStartingWorkers > 0. Units are placed on the nearest
// walkable cell to the townhall center using the existing findNearestWalkable
// search. If townhall is nil or no walkable cell can be found within the search
// radius, a warning is logged and that worker is skipped — the match continues
// without crashing. Must be called under s.mu write lock.
func (s *GameState) spawnExtraStartingWorkersLocked(player *Player, townhall *protocol.BuildingTile, color string) {
	if player == nil || player.ExtraStartingWorkers <= 0 {
		return
	}
	if townhall == nil {
		slog.Warn("spawnExtraStartingWorkersLocked: no townhall for player; skipping extra workers",
			"playerID", player.ID, "count", player.ExtraStartingWorkers)
		return
	}

	cellSize := s.MapConfig.CellSize
	// Compute world-space center of the townhall building.
	centerX := (float64(townhall.X) + float64(townhall.Width)/2) * cellSize
	centerY := (float64(townhall.Y) + float64(townhall.Height)/2) * cellSize
	blocked := s.getBlockedCellsLocked()

	for i := 0; i < player.ExtraStartingWorkers; i++ {
		center := s.worldToGrid(centerX, centerY)
		spawnCell, ok := s.findNearestWalkable(center, blocked)
		if !ok {
			slog.Warn("spawnExtraStartingWorkersLocked: no walkable cell found for extra worker; skipping",
				"playerID", player.ID, "workerIndex", i)
			continue
		}
		spawnPos := s.gridToWorldCenter(spawnCell)
		unit := s.spawnPlayerUnitLocked("worker", player.ID, color, spawnPos)
		if unit == nil {
			slog.Warn("spawnExtraStartingWorkersLocked: spawnPlayerUnitLocked returned nil; skipping",
				"playerID", player.ID, "workerIndex", i)
		}
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
