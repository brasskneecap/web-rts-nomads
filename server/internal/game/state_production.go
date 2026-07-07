package game

import (
	"math"
	"sort"
	"strings"
	"webrts/server/pkg/protocol"
)

type UnitProduction struct {
	PlayerID         string
	UnitType         string
	RemainingSeconds float64
	TotalSeconds     float64
}

// unitProductionMaxQueue caps how many units a single building can have queued
// at once (the in-progress unit + everything stacked behind it). Players that
// hit the cap can't enqueue further units until one finishes or the front of
// the queue is cancelled.
const unitProductionMaxQueue = 8

func (s *GameState) TrainUnit(playerID, buildingID, unitType string) {
	if _, ok := getUnitDef(unitType); !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}
	if building.Metadata != nil && building.Metadata["underConstruction"] == true {
		return
	}
	if !containsString(building.SpawnUnitTypes, unitType) {
		return
	}
	if !s.playerMeetsUnitRequirementsLocked(playerID, unitType) {
		return
	}
	if len(s.Productions[buildingID]) >= unitProductionMaxQueue {
		return
	}
	player, ok := s.Players[playerID]
	if !ok {
		return
	}
	if !s.canAffordUnitCostLocked(player, unitType) {
		return
	}
	if !s.canAffordMeatCostLocked(playerID, unitType) {
		return
	}

	s.payUnitCostLocked(player, unitType)
	s.beginUnitProductionLocked(player, *building, unitType)
}

// CancelTrainingAt removes a single production-queue entry by index and
// refunds its cost to the player. Index 0 is the currently-training unit
// (the "X" cancel button next to the progress bar); higher indices are the
// queued units behind the leader. Out-of-range indices are ignored.
func (s *GameState) CancelTrainingAt(playerID, buildingID string, queueIndex int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if !containsString(building.Capabilities, "unit-spawner") {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	queue := s.Productions[building.ID]
	if queueIndex < 0 || queueIndex >= len(queue) {
		return
	}

	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	s.refundUnitCostLocked(player, queue[queueIndex].UnitType)
	if queueIndex == 0 {
		s.consumeProductionQueueItemLocked(building.ID)
		return
	}

	// Mid-queue removal: splice the entry out, leaving the leading unit's
	// in-progress timer untouched.
	s.Productions[building.ID] = append(queue[:queueIndex], queue[queueIndex+1:]...)
	if len(s.Productions[building.ID]) == 0 {
		delete(s.Productions, building.ID)
	}
}

// CancelCurrentTraining is the legacy entry point used by the "X" cancel
// button. Equivalent to CancelTrainingAt with queueIndex = 0.
func (s *GameState) CancelCurrentTraining(playerID, buildingID string) {
	s.CancelTrainingAt(playerID, buildingID, 0)
}

func (s *GameState) SetBuildingSpawnPoint(playerID, buildingID string, point protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if !containsString(building.Capabilities, "unit-spawner") {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}

	clampedPoint := protocol.Vec2{
		X: clampFloat(point.X, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(point.Y, unitRadius, s.MapHeight-unitRadius),
	}
	if building.Metadata == nil {
		building.Metadata = map[string]interface{}{}
	}
	building.Metadata["spawnPointX"] = clampedPoint.X
	building.Metadata["spawnPointY"] = clampedPoint.Y
}

func (s *GameState) getUsedMeatForPlayerLocked(playerID string) int {
	used := 0
	for _, unit := range s.Units {
		if unit.OwnerID == playerID {
			if def, ok := getUnitDef(unit.UnitType); ok {
				used += def.MeatCost
			}
		}
	}
	for _, queue := range s.Productions {
		for _, prod := range queue {
			if prod.PlayerID == playerID {
				if def, ok := getUnitDef(prod.UnitType); ok {
					used += def.MeatCost
				}
			}
		}
	}
	return used
}

func (s *GameState) getMaxMeatForPlayerLocked(playerID string) int {
	total := 0
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		underConstruction, _ := b.Metadata["underConstruction"].(bool)
		if underConstruction {
			continue
		}
		if def, ok := getBuildingDef(b.BuildingType); ok {
			if supply, ok := def.Metadata["foodSupply"]; ok {
				switch v := supply.(type) {
				case float64:
					total += int(v)
				case int:
					total += v
				}
			}
		}
	}
	return total
}

func (s *GameState) canAffordMeatCostLocked(playerID, unitType string) bool {
	def, ok := getUnitDef(unitType)
	if !ok {
		return true
	}
	return s.getUsedMeatForPlayerLocked(playerID)+def.MeatCost <= s.getMaxMeatForPlayerLocked(playerID)
}

func (s *GameState) getPlayerResourceStocksLocked(player *Player) []protocol.ResourceStock {
	usedMeat := s.getUsedMeatForPlayerLocked(player.ID)
	maxMeat := s.getMaxMeatForPlayerLocked(player.ID)
	return []protocol.ResourceStock{
		{ID: "gold", Label: "Gold", Amount: player.Resources["gold"], Accent: "#d4a84f"},
		{ID: "wood", Label: "Wood", Amount: player.Resources["wood"], Accent: "#7a9a52"},
		{ID: "food", Label: "Food", Amount: usedMeat, Max: &maxMeat, Accent: "#c96e43"},
	}
}

func (s *GameState) beginUnitProductionLocked(player *Player, building protocol.BuildingTile, unitType string) {
	spawnSeconds := s.getEffectiveUnitSpawnSecondsLocked(player, building, unitType)
	s.Productions[building.ID] = append(s.Productions[building.ID], &UnitProduction{
		PlayerID:         player.ID,
		UnitType:         unitType,
		RemainingSeconds: spawnSeconds,
		TotalSeconds:     spawnSeconds,
	})
}

func (s *GameState) updateUnitProductionsLocked(dt float64) {
	if len(s.Productions) == 0 {
		return
	}

	completed := make([]string, 0, len(s.Productions))

	for buildingID, queue := range s.Productions {
		if len(queue) == 0 {
			completed = append(completed, buildingID)
			continue
		}

		production := queue[0]
		production.RemainingSeconds = math.Max(0, production.RemainingSeconds-dt)
		if production.RemainingSeconds <= 0 {
			completed = append(completed, buildingID)
		}
	}

	for _, buildingID := range completed {
		s.completeUnitProductionLocked(buildingID)
	}
}

func (s *GameState) completeUnitProductionLocked(buildingID string) {
	queue, ok := s.Productions[buildingID]
	if !ok || len(queue) == 0 {
		delete(s.Productions, buildingID)
		return
	}
	production := queue[0]

	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible || building.OwnerID == nil || *building.OwnerID != production.PlayerID {
		s.consumeProductionQueueItemLocked(buildingID)
		return
	}

	player, ok := s.Players[production.PlayerID]
	if !ok {
		s.consumeProductionQueueItemLocked(buildingID)
		return
	}

	spawnPosition, ok := s.getProductionSpawnPositionLocked(*building)
	if !ok {
		return
	}

	unit := s.spawnPlayerUnitLocked(production.UnitType, production.PlayerID, player.Color, spawnPosition)
	if unit != nil {
		rallyPoint := s.getTownhallSpawnOriginLocked(*building)
		if distanceSquared(unit.X, unit.Y, rallyPoint.X, rallyPoint.Y) > unitRadius*unitRadius {
			unit.Status = "Moving To Spawn Point"
			s.assignUnitPath(unit, rallyPoint, s.getBlockedCellsLocked(), nil)
		}
	}

	s.consumeProductionQueueItemLocked(buildingID)
}

func (s *GameState) getProductionSpawnPositionLocked(building protocol.BuildingTile) (protocol.Vec2, bool) {
	blocked := s.getBlockedCellsLocked()
	spawnPositions := s.getTownhallSpawnPositionsLocked(building, 1, blocked)
	if len(spawnPositions) > 0 {
		return spawnPositions[0], true
	}

	rallyPoint := s.getTownhallSpawnOriginLocked(building)
	rallyCell := s.worldToGrid(rallyPoint.X, rallyPoint.Y)
	// Anchor the nearest-walkable search to the rally cell's region so the
	// fallback can't tunnel across a wall into a sealed pocket. Region 0
	// (rally cell itself blocked, e.g. the default building-center origin)
	// degrades to the unconstrained search.
	spawnCell, ok := s.findNearestWalkableInRegionLocked(rallyCell, s.walkableRegionAtLocked(rallyCell), blocked, nil)
	if !ok {
		return protocol.Vec2{}, false
	}

	return s.clampPointToCell(rallyPoint, spawnCell), true
}

func (s *GameState) getEffectiveUnitSpawnSecondsLocked(player *Player, building protocol.BuildingTile, unitType string) float64 {
	spawnSeconds := 1.0
	if def, ok := getUnitDef(unitType); ok && def.SpawnSeconds > 0 {
		spawnSeconds = def.SpawnSeconds
	}

	if building.Metadata != nil {
		if multiplier, ok := getMetadataFloat(building.Metadata, "spawnTimeMultiplier"); ok && multiplier > 0 {
			spawnSeconds *= multiplier
		}
		if multiplier, ok := getMetadataFloat(building.Metadata, "spawnTime"+formatMetadataUnitTypeSuffix(unitType)+"Multiplier"); ok && multiplier > 0 {
			spawnSeconds *= multiplier
		}
	}

	if player.GlobalUnitSpawnTimeMultiplier > 0 {
		spawnSeconds *= player.GlobalUnitSpawnTimeMultiplier
	}
	if multiplier, ok := player.UnitSpawnTimeMultipliers[unitType]; ok && multiplier > 0 {
		spawnSeconds *= multiplier
	}

	// Zone-aura unit production speed: base 1.0 (100% speed); effective speed
	// (1 + add) × mul shortens train time as seconds / speed. Identity ⇒ unchanged.
	if add, mul := s.playerStatModifierLocked(player.ID, statUnitProductionSpeed); add != 0 || mul != 1 {
		if speed := (1.0 + add) * mul; speed > 0 {
			spawnSeconds /= speed
		}
	}

	return math.Max(minUnitSpawnSeconds, spawnSeconds)
}

func (s *GameState) CanAffordUnit(playerID, unitType string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	return s.canAffordUnitCostLocked(player, unitType) && s.canAffordMeatCostLocked(playerID, unitType)
}

func (s *GameState) CanAffordBuilding(playerID, buildingType string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	player, ok := s.Players[playerID]
	if !ok {
		return false
	}
	def, ok := getBuildingDef(buildingType)
	if !ok {
		return true
	}
	for resource, cost := range def.ResourceCost {
		if player.Resources[resource] < cost {
			return false
		}
	}
	return true
}

// unitCostDefLocked resolves the UnitDef a player is actually charged for when
// training unitType: the per-player EffectiveUnitDefs copy (advancement deltas
// such as the worker goldCost reduction baked in) when one exists, otherwise
// the raw catalog def. Mirrors spawnPlayerUnitLocked and
// gatherAmountForUnitResourceLocked so the cost path honors the same
// advancements as the spawn and gather paths. Must hold s.mu.
func (s *GameState) unitCostDefLocked(player *Player, unitType string) (UnitDef, bool) {
	def, ok := getUnitDef(unitType)
	if !ok {
		return UnitDef{}, false
	}
	if player != nil {
		if effective, hasOverride := player.EffectiveUnitDefs[unitType]; hasOverride {
			def = effective
		}
	}
	return def, true
}

func (s *GameState) canAffordUnitCostLocked(player *Player, unitType string) bool {
	def, ok := s.unitCostDefLocked(player, unitType)
	if !ok {
		return false
	}
	for resourceID, amount := range def.ResourceCost {
		if player.Resources[resourceID] < amount {
			return false
		}
	}
	return true
}

func (s *GameState) payUnitCostLocked(player *Player, unitType string) {
	def, ok := s.unitCostDefLocked(player, unitType)
	if !ok {
		return
	}
	for resourceID, amount := range def.ResourceCost {
		player.Resources[resourceID] -= amount
	}
}

func (s *GameState) refundUnitCostLocked(player *Player, unitType string) {
	def, ok := s.unitCostDefLocked(player, unitType)
	if !ok {
		return
	}
	for resourceID, amount := range def.ResourceCost {
		player.Resources[resourceID] += amount
	}
}

func (s *GameState) consumeProductionQueueItemLocked(buildingID string) {
	queue := s.Productions[buildingID]
	if len(queue) <= 1 {
		delete(s.Productions, buildingID)
		return
	}

	s.Productions[buildingID] = queue[1:]
}

func joinProductionUnitTypes(queue []*UnitProduction) string {
	unitTypes := make([]string, 0, len(queue))
	for _, production := range queue {
		unitTypes = append(unitTypes, production.UnitType)
	}

	return strings.Join(unitTypes, ",")
}

// playerHasBuildingTypeLocked returns true if the player owns at least
// one Visible, fully-built (not underConstruction) building satisfying
// the required type. The requirement is tier-aware: a tier link (e.g.
// "temple", "keep") is satisfied by a fully-built building of the chain
// ROOT type at the corresponding tier or higher — a placed building keeps
// its root BuildingType and records tier only in metadata["tier"], so a
// "temple" requirement means "a chapel at tier ≥ 2". Plain types resolve
// to (themselves, tier 1) and behave exactly as before. Must be called
// under s.mu.
func (s *GameState) playerHasBuildingTypeLocked(playerID, buildingType string) bool {
	rootType, needTier := buildingRequirementTier(buildingType)
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !b.Visible {
			continue
		}
		if b.BuildingType != rootType {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		if getMetadataBool(b.Metadata, "underConstruction") {
			continue
		}
		if needTier > 1 {
			tier := 1
			if v, ok := getMetadataFloat(b.Metadata, "tier"); ok && v >= 1 {
				tier = int(v)
			}
			if tier < needTier {
				continue
			}
		}
		return true
	}
	return false
}

// playerMeetsUnitRequirementsLocked returns true if every building type
// in def.RequiresBuildings is satisfied for playerID. Empty list = true.
// Unknown unitType = false (defensive; should be unreachable because
// callers verify the def exists first). Must be called under s.mu.
func (s *GameState) playerMeetsUnitRequirementsLocked(playerID, unitType string) bool {
	def, ok := getUnitDef(unitType)
	if !ok {
		return false
	}
	for _, required := range def.RequiresBuildings {
		if !s.playerHasBuildingTypeLocked(playerID, required) {
			return false
		}
	}
	return true
}

// buildPlayerSnapshotLocked assembles the per-player wire snapshot used
// by every outbound match-snapshot path. Each field is a pure read of
// per-player state; the helper exists so the three Snapshot* functions
// don't drift in lockstep every time the snapshot schema grows. Must
// be called under s.mu.
func (s *GameState) buildPlayerSnapshotLocked(player *Player) protocol.PlayerSnapshot {
	snap := protocol.PlayerSnapshot{
		PlayerID:             player.ID,
		Color:                player.Color,
		TeamID:               player.TeamID,
		Resources:            s.getPlayerResourceStocksLocked(player),
		Upgrades:             s.playerUpgradeSnapshotsLocked(player.ID),
		TownHallTier:         s.townhallTierForPlayerLocked(player.ID),
		Vault:                s.playerVaultSnapshotsLocked(player.ID),
		LockedUnitTypes:      s.lockedUnitTypesForPlayerLocked(player.ID),
		UnitCostOverrides:    unitCostOverridesForPlayer(player),
		ShopRerollsRemaining: player.ShopRerollsRemaining,
		UnlockedRecipeIDs:    append([]string(nil), player.UnlockedRecipeIDs...),
		// Per-player match metrics for the end-of-round comparison columns
		// (§15) and any client-side display that wants to read the live
		// totals (e.g. an in-match resource tracker that shows
		// cumulative-earned alongside current-balance).
		Metrics: toMetricsSnapshot(player.Metrics),
	}
	snap.CommanderAbilities = s.commanderAbilitySnapshotsLocked(player)
	return snap
}

// unitCostOverridesForPlayer returns the effective training costs for every
// unit type whose cost differs from the static catalog because of this
// player's advancements (e.g. worker goldCost). Only differing types are
// emitted, so a player with no cost advancements produces nil and the wire
// field is omitted. Deterministic (sorted by unit type) for stable snapshots.
// EffectiveUnitDefs is computed once at match start and never mutated, so this
// is a cheap read; no lock semantics beyond the snapshot builder's own hold.
func unitCostOverridesForPlayer(player *Player) []protocol.UnitCostOverride {
	if len(player.EffectiveUnitDefs) == 0 {
		return nil
	}

	unitTypes := make([]string, 0, len(player.EffectiveUnitDefs))
	for unitType := range player.EffectiveUnitDefs {
		unitTypes = append(unitTypes, unitType)
	}
	sort.Strings(unitTypes)

	var overrides []protocol.UnitCostOverride
	for _, unitType := range unitTypes {
		eff := player.EffectiveUnitDefs[unitType]
		catalog, ok := getUnitDef(unitType)
		if !ok {
			continue
		}
		if eff.MeatCost == catalog.MeatCost && resourceCostsEqual(eff.ResourceCost, catalog.ResourceCost) {
			continue
		}
		cost := make(map[string]int, len(eff.ResourceCost))
		for resource, amount := range eff.ResourceCost {
			cost[resource] = amount
		}
		overrides = append(overrides, protocol.UnitCostOverride{
			UnitType:     unitType,
			ResourceCost: cost,
			MeatCost:     eff.MeatCost,
		})
	}
	return overrides
}

// resourceCostsEqual reports whether two resource-cost maps hold the same
// non-zero amounts. Zero-valued entries are treated as absent so a map that
// spells out {"wood": 0} compares equal to one that omits wood.
func resourceCostsEqual(a, b map[string]int) bool {
	for resource, amount := range a {
		if amount != b[resource] {
			return false
		}
	}
	for resource, amount := range b {
		if amount != a[resource] {
			return false
		}
	}
	return true
}

// lockedUnitTypesForPlayerLocked returns the set of unit types the
// player currently cannot train due to unmet RequiresBuildings. Called
// once per player per outbound snapshot (≈20 Hz × player count), so
// keep it cheap: iterate unitDefsByType directly (no allocation, no
// pre-sort) and sort only the small locked result for deterministic
// wire output. Returns nil (not an empty slice) when nothing is locked
// so the protocol's omitempty drops the field from the wire. If the
// unit catalog ever grows large, cache the result on Player and
// invalidate on building add/complete/destroy. Must be called under s.mu.
func (s *GameState) lockedUnitTypesForPlayerLocked(playerID string) []string {
	var locked []string
	for unitType, def := range unitDefsByType {
		if len(def.RequiresBuildings) == 0 {
			continue
		}
		if !s.playerMeetsUnitRequirementsLocked(playerID, unitType) {
			locked = append(locked, unitType)
		}
	}
	if len(locked) > 1 {
		sort.Strings(locked)
	}
	return locked
}
