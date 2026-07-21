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

// resolveHealthRegenRate returns the unit def's authored passive HP-per-second
// regen, falling back to the global default when the def does not author one.
//
// An authored 0 is HONORED, not treated as "unset" — that is a unit which never
// regenerates. This is the entire reason UnitDef.HealthRegenRate is a pointer.
func resolveHealthRegenRate(def UnitDef) float64 {
	if def.HealthRegenRate != nil {
		return *def.HealthRegenRate
	}
	return defaultHealthRegenPerSecond
}

func (s *GameState) spawnPlayerUnitLocked(unitType, playerID, color string, spawn protocol.Vec2) *Unit {
	def, ok := getUnitDef(unitType)
	if !ok {
		return nil
	}
	// If the player has advancement-driven overrides for this unit type, use
	// the effective def (a pre-computed copy with stat deltas baked in) instead
	// of the raw catalog def. The effective def is computed once at match start
	// by applyAdvancementsToEffectiveDefsLocked and never mutated thereafter.
	if player, playerOK := s.Players[playerID]; playerOK {
		if effective, hasOverride := player.EffectiveUnitDefs[unitType]; hasOverride {
			def = effective
		}
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
		BaseArmor:          0,
		BaseAttackSpeed:    def.AttackSpeed,
		BaseMoveSpeed:      def.MoveSpeed,
		// Per-unit-type base values for fieldless stats (critChance, …). `def` is
		// the advancement-effective def (see spawnPlayerUnitLocked), so an
		// advancement that raised a base stat flows through here too. Copied so a
		// later per-unit mutation can't scribble on the shared catalog def's map.
		BaseStats:          copyBaseStats(def.BaseStats),
		Damage:             def.Damage,
		Armor:              def.Armor,
		AttackRange:        def.AttackRange,
		BaseAttackRange:    def.AttackRange,
		AttackSpeed:        def.AttackSpeed,
		MoveSpeed:          def.MoveSpeed,
		SplashRadius:       def.SplashRadius,
		BaseVisionRange:    baseVision,
		VisionRange:        baseVision,
		HealthRegenPerSecond:     resolveHealthRegenRate(def),
		BaseHealthRegenPerSecond: resolveHealthRegenRate(def),
		// Spellcaster kit (zero values for non-casters). CurrentMana starts
		// full per the Acolyte spec.
		MaxMana:            def.MaxMana,
		CurrentMana:        def.MaxMana,
		ManaRegenPerSecond: def.ManaRegenRate,
		ProjectileID:       def.Projectile,
		AttackDamageType:   def.DamageType,
		AttackType:         def.AttackType,
		ProjectileScale:    def.ProjectileScale,
		Abilities:          append([]string{}, def.Abilities...),
		Rank:               unitRankBase,
		XP:                 def.SpawnExp,
		BonusArrows:        def.BonusArrows,
		TrapEffectBonus:    def.TrapEffectBonus,
		TrapRadiusBonus:    def.TrapRadiusBonus,
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
		// Metrics: bump UnitsTrained on every successful player-owned unit
		// spawn. Includes authored starting units and upgrade-granted extras,
		// per the design's "every unit a player ever owned" reading.
		if player, ok := s.Players[playerID]; ok {
			player.Metrics.RecordUnitTrained(unitType)
		}
	}
	s.applyRankModifiersLocked(unit, false)
	// Initialise inventory slots for player-owned units from the unit's
	// current rank. Normal spawns are base rank → 1 slot (workers get none).
	// Callers that override the rank AFTER spawn (e.g. DebugSpawnUnit) must
	// re-run setInventorySizeForRankLocked themselves — this pass only sees
	// the rank the unit has at spawn time.
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
	def, ok := getUnitDef("raider")
	if !ok {
		// raider is a shipped catalog entry; this must never happen.
		panic("spawnRaiderUnitLocked: raider not found in unit catalog — check catalog/units/raider/raider/raider.json")
	}
	return s.spawnUnitFromDefLocked(def, "raider", playerID, color, spawn)
}

func (s *GameState) spawnEnemyUnitLocked(unitType string, spawn protocol.Vec2) *Unit {
	if def, ok := getUnitDef(unitType); ok {
		return s.spawnUnitFromDefLocked(def, unitType, enemyPlayerID, enemyPlayerColor, spawn)
	}
	return nil
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
	return nil
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
		// Anchor to the authored cell's region so displacement (authored cell
		// occupied/blocked at spawn time) can't relocate the unit into a
		// sealed pocket. Region 0 (authored cell itself blocked) degrades to
		// the unconstrained search.
		spawnCell, ok := s.findNearestWalkableInRegionLocked(cell, s.walkableRegionAtLocked(cell), blocked, nil)
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
			continue
		}
		s.applyPlacedUnitInstanceLocked(unit, entry)
	}
}

// applyPlacedUnitInstanceLocked stamps a placed unit's authored rank, items,
// and perks onto a freshly spawned unit. Each is optional; unknown refs were
// already dropped at hydrate (see hydratePlacedUnits). Caller holds s.mu.
//
// Order: rank first, then items, then perks.
//
//   - Rank: mirrors the "set rank directly, no XP" pipeline debug_spawn.go
//     uses (assignUnitPathOnRankUpLocked -> rollUnitPoolAbilitiesLocked ->
//     assignUnitPathAbilitiesLocked -> applyRankModifiersLocked), so a
//     placed Silver Cleric gets the same path roll / ability pool / path
//     abilities a naturally-promoted Silver Cleric would, not just the raw
//     stat multipliers. applyRankModifiersLocked does NOT resize
//     InventorySize/Equipped (that's setInventorySizeForRankLocked, normally
//     called from onUnitRankUpLocked on the natural rank-up path or directly
//     by debug_spawn.go) — call it explicitly so a Silver+ placed unit has
//     enough slots for its authored items before we equip them.
//   - Items: equipped via equipItemDirectLocked (fills the first free slot,
//     else appends — placed-unit authoring intent wins over the slot cap).
//     Equipment bonus is recomputed once after all items are equipped.
//   - Perks: appended verbatim (no eligibility/prerequisite filtering — same
//     freedom the debug-spawn tool has) with applyPerkGrantedHooksLocked run
//     per perk, then path abilities are re-derived so perk-granted abilities
//     (PerkDef.GrantsAbilities) land on unit.Abilities, mirroring
//     debug_spawn.go's second assignUnitPathAbilitiesLocked call.
func (s *GameState) applyPlacedUnitInstanceLocked(unit *Unit, entry protocol.PlacedUnit) {
	// entry.Rank != unit.Rank skips the whole block when the authored rank
	// already matches the unit's just-spawned rank (unit.Rank == unitRankBase
	// from spawnUnitFromDefLocked) — e.g. an authored rank of "base", or the
	// common case of entry.Rank == "" being caught by the first condition.
	// This is a no-op-avoidance guard, not a correctness requirement:
	// assignUnitPathOnRankUpLocked has its own internal `unit.Rank ==
	// unitRankBase` guard (progression.go) that would no-op anyway, so a
	// base-rank placed unit correctly skips path assignment either way.
	if entry.Rank != "" && entry.Rank != unit.Rank {
		unit.Rank = entry.Rank
		s.assignUnitPathOnRankUpLocked(unit)
		s.rollUnitPoolAbilitiesLocked(unit)
		s.assignUnitPathAbilitiesLocked(unit)
		s.applyRankModifiersLocked(unit, false)
		s.setInventorySizeForRankLocked(unit)
	}
	for _, itemID := range entry.Items {
		if _, ok := getItemDef(itemID); !ok {
			continue
		}
		s.equipItemDirectLocked(unit, itemID)
	}
	if len(entry.Items) > 0 {
		s.recomputeUnitEquipmentBonusLocked(unit)
	}
	for _, perkID := range entry.Perks {
		unit.PerkIDs = append(unit.PerkIDs, perkID)
		s.applyPerkGrantedHooksLocked(unit, perkID)
	}
	if len(entry.Perks) > 0 {
		s.assignUnitPathAbilitiesLocked(unit)
	}
}

// equipItemDirectLocked puts an item into the unit's next free equipment
// slot without going through the vault/player flow (placed units aren't
// tied to a vault). Fills the first nil slot; if none, appends. Caller holds
// s.mu; caller is responsible for calling recomputeUnitEquipmentBonusLocked
// afterward.
func (s *GameState) equipItemDirectLocked(unit *Unit, itemID string) {
	item := &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: itemID, Stacks: 1}
	for i := range unit.Equipped {
		if unit.Equipped[i] == nil {
			unit.Equipped[i] = item
			return
		}
	}
	unit.Equipped = append(unit.Equipped, item)
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
		// Same region anchoring as player placed units — see above.
		spawnCell, ok := s.findNearestWalkableInRegionLocked(cell, s.walkableRegionAtLocked(cell), blocked, nil)
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
		// Apply authored rank/items/perks after guard-mode fields are set.
		// Verified none of applyPlacedUnitInstanceLocked's callees
		// (applyRankModifiersLocked, setInventorySizeForRankLocked,
		// recomputeUnitEquipmentBonusLocked, assignUnitPathOnRankUpLocked,
		// rollUnitPoolAbilitiesLocked, assignUnitPathAbilitiesLocked,
		// applyPerkGrantedHooksLocked) touch GuardMode/GuardAnchor*/
		// GuardAggroRange/GuardLeashRange/Order/OrderID/CombatAnchor*/Status/
		// IgnoreWaveClear, so this ordering is not load-bearing for
		// correctness today — it is chosen so guard identity is fully
		// established before any instance-data hook runs, keeping guard
		// config authoritative over anything a future rank/perk hook might
		// add. Note spawnEnemyUnitLocked (unlike the player path) does NOT
		// pre-size InventorySize/Equipped (that setup in
		// spawnUnitFromDefLocked is player-only); setInventorySizeForRankLocked
		// inside applyPlacedUnitInstanceLocked has no ownership check, so it
		// still grows them correctly here when entry.Rank is set.
		s.applyPlacedUnitInstanceLocked(unit, entry)
	}
}

// findPlayerSpawnPointLocked returns the spawn-point BuildingTile associated
// with playerID's claimed townhall, or nil when the map has no spawn-point
// linked to that townhall. The association is the same one
// resolveSpawnPointTownhallLocked uses: an explicit metadata.townhallId link
// when present, otherwise nearest townhall by distance. metadata.playerLabel
// is NOT required — maps without authored labels (e.g. enemy-test-small)
// still pair each spawn-point with its spatially-nearest townhall.
// Must be called under s.mu lock.
func (s *GameState) findPlayerSpawnPointLocked(playerID string) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "spawn-point" {
			continue
		}
		townhall := s.resolveSpawnPointTownhallLocked(*b, true)
		if townhall == nil || townhall.OwnerID == nil || *townhall.OwnerID != playerID {
			continue
		}
		return b
	}
	return nil
}

// spawnUnitsForPlayerAtSpawnPointLocked spawns `count` units of `unitType`
// for `player`, placed at walkable cells near the player's authored
// spawn-point building. When the player has no spawn-point on the map, the
// helper logs a warning and returns without spawning (no townhall fallback).
// Used by both start-of-match profile-upgrade grants and the wave-upgrade
// "spawnUnit" effect. Must be called under s.mu write lock.
func (s *GameState) spawnUnitsForPlayerAtSpawnPointLocked(player *Player, unitType string, count int) {
	if player == nil {
		slog.Warn("spawnUnitsForPlayerAtSpawnPointLocked: nil player; skipping",
			"unitType", unitType, "count", count)
		return
	}
	if unitType == "" {
		slog.Warn("spawnUnitsForPlayerAtSpawnPointLocked: empty unitType; skipping",
			"playerID", player.ID, "count", count)
		return
	}
	if count <= 0 {
		return
	}

	sp := s.findPlayerSpawnPointLocked(player.ID)
	if sp == nil {
		slog.Warn("spawnUnitsForPlayerAtSpawnPointLocked: no spawn-point for player; skipping",
			"playerID", player.ID, "unitType", unitType, "count", count)
		return
	}

	cellSize := s.MapConfig.CellSize
	centerX := (float64(sp.X) + float64(sp.Width)/2) * cellSize
	centerY := (float64(sp.Y) + float64(sp.Height)/2) * cellSize
	blocked := s.getBlockedCellsLocked()

	center := s.worldToGrid(centerX, centerY)
	// Anchor to the spawn-point cell's region (spawn-points don't block, so
	// the cell is normally walkable) — extra starting units must land where
	// the rest of the player's units are, not in an adjacent sealed pocket.
	centerRegion := s.walkableRegionAtLocked(center)
	for i := 0; i < count; i++ {
		spawnCell, ok := s.findNearestWalkableInRegionLocked(center, centerRegion, blocked, nil)
		if !ok {
			slog.Warn("spawnUnitsForPlayerAtSpawnPointLocked: no walkable cell found near spawn-point; skipping",
				"playerID", player.ID, "unitType", unitType, "index", i)
			continue
		}
		spawnPos := s.gridToWorldCenter(spawnCell)
		unit := s.spawnPlayerUnitLocked(unitType, player.ID, player.Color, spawnPos)
		if unit == nil {
			slog.Warn("spawnUnitsForPlayerAtSpawnPointLocked: spawnPlayerUnitLocked returned nil; skipping",
				"playerID", player.ID, "unitType", unitType, "index", i)
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
