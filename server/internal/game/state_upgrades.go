package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

// UpgradeTrack identifies a per-player permanent upgrade line. The constant
// value equals the UnitType string — this is the lookup contract used by
// applyPlayerUpgradesAtSpawnLocked and reapplyUpgradesToOwnedUnitsByTypeLocked.
type UpgradeTrack string

const (
	UpgradeTrackSoldier UpgradeTrack = "soldier"
	UpgradeTrackArcher  UpgradeTrack = "archer"
)

// UpgradeTrackDef describes one upgrade line: per-level stat bonuses and cost
// scaling. BaseCostGold × CostGrowth^(nextLevel-1) gives the cost to purchase
// level nextLevel.
type UpgradeTrackDef struct {
	Track               UpgradeTrack
	DisplayName         string
	BaseCostGold        int
	CostGrowth          float64
	HPPerLevel          int
	DamagePerLevel      int
	ArmorPerLevel       int
	AttackSpeedPerLevel float64
	MoveSpeedPerLevel   float64
}

// upgradeTrackDefs is the authoritative list of all upgrade tracks shipped in
// v1. Two tracks: soldier and archer. Appending to this slice adds a new track
// to all downstream functions automatically (snapshots, cap checks, etc.).
var upgradeTrackDefs = []UpgradeTrackDef{
	{
		Track:               UpgradeTrackSoldier,
		DisplayName:         "Soldier",
		BaseCostGold:        150,
		CostGrowth:          1.6,
		HPPerLevel:          25,
		DamagePerLevel:      2,
		ArmorPerLevel:       1,
		AttackSpeedPerLevel: 0.05,
		MoveSpeedPerLevel:   3.0,
	},
	{
		Track:               UpgradeTrackArcher,
		DisplayName:         "Archer",
		BaseCostGold:        175,
		CostGrowth:          1.6,
		HPPerLevel:          8,
		DamagePerLevel:      2,
		ArmorPerLevel:       0,
		AttackSpeedPerLevel: 0.10,
		MoveSpeedPerLevel:   5.0,
	},
}

// upgradeTrackDefByID returns the UpgradeTrackDef for the given track, and
// whether it was found.
func upgradeTrackDefByID(track UpgradeTrack) (UpgradeTrackDef, bool) {
	for _, def := range upgradeTrackDefs {
		if def.Track == track {
			return def, true
		}
	}
	return UpgradeTrackDef{}, false
}

// upgradeCostForLevel returns the gold cost to purchase nextLevel
// (= currentLevel + 1). Formula: round(baseCostGold × CostGrowth^(nextLevel-1)).
func upgradeCostForLevel(def UpgradeTrackDef, nextLevel int) int {
	if nextLevel <= 0 {
		return 0
	}
	raw := float64(def.BaseCostGold) * math.Pow(def.CostGrowth, float64(nextLevel-1))
	return int(math.Round(raw))
}

// upgradeCapForPlayerLocked returns the maximum upgrade level a player may
// purchase, based on the highest fully-built town hall tier they own.
// Tier 1 → cap 3, Tier 2 → cap 6, Tier 3 → cap 9, no townhall → cap 0.
// Must be called under s.mu.
func (s *GameState) upgradeCapForPlayerLocked(playerID string) int {
	tier := s.townhallTierForPlayerLocked(playerID)
	switch tier {
	case 1:
		return 3
	case 2:
		return 6
	case 3:
		return 9
	default:
		return 0
	}
}

// playerHasBlacksmithLocked returns true if the player owns at least one
// fully-built (not under-construction) building with the "upgrade-purchase"
// capability. Must be called under s.mu.
func (s *GameState) playerHasBlacksmithLocked(playerID string) bool {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !b.Visible {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		if getMetadataBool(b.Metadata, "underConstruction") {
			continue
		}
		for _, cap := range b.Capabilities {
			if cap == "upgrade-purchase" {
				return true
			}
		}
	}
	return false
}

// townhallTierForPlayerLocked returns the highest tier integer among all live,
// fully-built townhalls owned by playerID. Reads metadata["tier"] (float64);
// defaults to 1 when absent. Under-construction townhalls are excluded.
// Returns 0 if the player owns no qualifying townhall. Must be called under s.mu.
func (s *GameState) townhallTierForPlayerLocked(playerID string) int {
	highest := 0
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !b.Visible {
			continue
		}
		if b.BuildingType != "townhall" {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		if getMetadataBool(b.Metadata, "underConstruction") {
			continue
		}
		tier := 1
		if v, ok := getMetadataFloat(b.Metadata, "tier"); ok && v >= 1 {
			tier = int(v)
		}
		if tier > highest {
			highest = tier
		}
	}
	return highest
}

// applyPlayerUpgradesAtSpawnLocked adds the full accumulated upgrade bonus for
// the player's current upgrade level to a freshly spawned unit. Called on a
// unit whose base stats are at def (catalog) values, so the full
// level × PerLevel amount is correct. No-op when level is 0 or the unit's
// UnitType has no matching track. Must be called under s.mu.
func (s *GameState) applyPlayerUpgradesAtSpawnLocked(unit *Unit) {
	player, ok := s.Players[unit.OwnerID]
	if !ok || player.Upgrades == nil {
		return
	}
	track := UpgradeTrack(unit.UnitType)
	def, hasDef := upgradeTrackDefByID(track)
	if !hasDef {
		return
	}
	level := player.Upgrades[track]
	if level <= 0 {
		return
	}
	unit.BaseMaxHP += def.HPPerLevel * level
	unit.BaseDamage += def.DamagePerLevel * level
	unit.BaseArmor += def.ArmorPerLevel * level
	unit.BaseAttackSpeed += def.AttackSpeedPerLevel * float64(level)
	unit.BaseMoveSpeed += def.MoveSpeedPerLevel * float64(level)
}

// reapplyUpgradesToOwnedUnitsByTypeLocked retroactively applies ONE additional
// level's worth of stat bonuses to all alive units owned by playerID whose
// UnitType matches track. Called immediately after Player.Upgrades[track] is
// incremented by 1, so exactly one PerLevel delta is added to each unit's base
// stats. applyRankModifiersLocked then rebakes derived stats.
//
// HP percentage is preserved: the existing HP fraction before the max HP
// increase is maintained after rebaking. Must be called under s.mu.
func (s *GameState) reapplyUpgradesToOwnedUnitsByTypeLocked(playerID string, track UpgradeTrack) {
	def, ok := upgradeTrackDefByID(track)
	if !ok {
		return
	}
	for _, unit := range s.Units {
		if unit.OwnerID != playerID || unit.UnitType != string(track) {
			continue
		}
		if unit.HP <= 0 {
			continue
		}
		// Preserve HP fraction before max HP changes.
		hpFrac := 1.0
		if unit.MaxHP > 0 {
			hpFrac = float64(unit.HP) / float64(unit.MaxHP)
		}

		// Add one level's worth of deltas to base stats.
		unit.BaseMaxHP += def.HPPerLevel
		unit.BaseDamage += def.DamagePerLevel
		unit.BaseArmor += def.ArmorPerLevel
		unit.BaseAttackSpeed += def.AttackSpeedPerLevel
		unit.BaseMoveSpeed += def.MoveSpeedPerLevel

		// Rebake derived stats (Damage, MaxHP, AttackSpeed, MoveSpeed, Armor).
		s.applyRankModifiersLocked(unit, false)

		// Restore HP at the preserved fraction.
		unit.HP = int(math.Round(hpFrac * float64(unit.MaxHP)))
		if unit.HP < 1 {
			unit.HP = 1
		}
		if unit.HP > unit.MaxHP {
			unit.HP = unit.MaxHP
		}
	}
}

// handlePurchaseUpgradeLocked validates and executes a single upgrade purchase.
// Validation failures are silent no-ops.
//  1. Player must own a fully-built blacksmith.
//  2. Current level must be below the cap (tier-gated).
//  3. Player must have enough gold.
//
// On success: deduct gold, increment Player.Upgrades[track], retroactively
// buff existing units. Must be called under s.mu.
func (s *GameState) handlePurchaseUpgradeLocked(playerID string, track UpgradeTrack) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}
	def, ok := upgradeTrackDefByID(track)
	if !ok {
		return
	}
	if !s.playerHasBlacksmithLocked(playerID) {
		return
	}
	cap := s.upgradeCapForPlayerLocked(playerID)
	if cap <= 0 {
		return
	}
	currentLevel := player.Upgrades[track]
	if currentLevel >= cap {
		return
	}
	nextLevel := currentLevel + 1
	cost := upgradeCostForLevel(def, nextLevel)
	if player.Resources["gold"] < cost {
		return
	}

	player.Resources["gold"] -= cost
	player.Upgrades[track] = nextLevel
	s.reapplyUpgradesToOwnedUnitsByTypeLocked(playerID, track)
}

// PurchaseUpgrade is the public entry point for upgrade purchases. It acquires
// s.mu and delegates to handlePurchaseUpgradeLocked.
func (s *GameState) PurchaseUpgrade(playerID string, track string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePurchaseUpgradeLocked(playerID, UpgradeTrack(track))
}

// handleUpgradeTownHallLocked validates and begins a town hall tier-up.
// Requirements:
//   - Building exists, is type "townhall", owned by playerID, fully built.
//   - No active tier-up already in progress (no "tierUpRemaining" key).
//   - Current tier < 3.
//   - Player can afford the upgrade.
//
// Costs: tier 1→2: 400 gold / 250 wood / 45 s.
//
//	tier 2→3: 800 gold / 500 wood / 90 s.
//
// On success: deduct resources, stamp tierUpRemaining/tierUpTotal/tierTargetLevel
// into building metadata. Must be called under s.mu.
func (s *GameState) handleUpgradeTownHallLocked(playerID string, buildingID string) {
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.BuildingType != "townhall" {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}
	if getMetadataBool(building.Metadata, "underConstruction") {
		return
	}
	// Block if a tier-up is already in progress on this building.
	if _, inProgress := building.Metadata["tierUpRemaining"]; inProgress {
		return
	}

	currentTier := 1
	if v, ok := getMetadataFloat(building.Metadata, "tier"); ok && v >= 1 {
		currentTier = int(v)
	}
	if currentTier >= 3 {
		return
	}

	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	// Determine cost and duration for this tier transition.
	var goldCost, woodCost int
	var duration float64
	switch currentTier {
	case 1:
		goldCost, woodCost, duration = 400, 250, 45.0
	case 2:
		goldCost, woodCost, duration = 800, 500, 90.0
	default:
		return
	}

	if player.Resources["gold"] < goldCost {
		return
	}
	if player.Resources["wood"] < woodCost {
		return
	}

	player.Resources["gold"] -= goldCost
	player.Resources["wood"] -= woodCost

	if building.Metadata == nil {
		building.Metadata = map[string]interface{}{}
	}
	building.Metadata["tierUpRemaining"] = duration
	building.Metadata["tierUpTotal"] = duration
	building.Metadata["tierTargetLevel"] = float64(currentTier + 1)
}

// UpgradeTownHall is the public entry point for town hall tier-up commands.
func (s *GameState) UpgradeTownHall(playerID string, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleUpgradeTownHallLocked(playerID, buildingID)
}

// tickTownHallTierUpsLocked advances all in-progress town hall tier-ups by dt.
// When a tier-up completes (tierUpRemaining reaches 0): set metadata["tier"]
// to tierTargetLevel and remove the three tierUp* keys. Must be called under s.mu.
func (s *GameState) tickTownHallTierUpsLocked(dt float64) {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || b.Metadata == nil {
			continue
		}
		remaining, ok := getMetadataFloat(b.Metadata, "tierUpRemaining")
		if !ok {
			continue
		}
		remaining -= dt
		if remaining <= 0 {
			// Tier-up complete: promote the tier.
			targetLevel, _ := getMetadataFloat(b.Metadata, "tierTargetLevel")
			if targetLevel >= 1 {
				b.Metadata["tier"] = targetLevel
			}
			delete(b.Metadata, "tierUpRemaining")
			delete(b.Metadata, "tierUpTotal")
			delete(b.Metadata, "tierTargetLevel")
		} else {
			b.Metadata["tierUpRemaining"] = remaining
		}
	}
}

// playerUpgradeSnapshotsLocked builds the []protocol.PlayerUpgradeSnapshot for
// a player. Emits one entry per upgradeTrackDef entry (always 2 for v1).
// Must be called under s.mu (read lock is sufficient).
func (s *GameState) playerUpgradeSnapshotsLocked(playerID string) []protocol.PlayerUpgradeSnapshot {
	player, ok := s.Players[playerID]
	if !ok {
		return nil
	}
	cap := s.upgradeCapForPlayerLocked(playerID)
	hasBlacksmith := s.playerHasBlacksmithLocked(playerID)

	snapshots := make([]protocol.PlayerUpgradeSnapshot, 0, len(upgradeTrackDefs))
	for _, def := range upgradeTrackDefs {
		level := 0
		if player.Upgrades != nil {
			level = player.Upgrades[def.Track]
		}
		nextLevel := level + 1
		nextCost := 0
		canAfford := false
		if level < cap {
			nextCost = upgradeCostForLevel(def, nextLevel)
			canAfford = hasBlacksmith && player.Resources["gold"] >= nextCost
		}
		snapshots = append(snapshots, protocol.PlayerUpgradeSnapshot{
			Track:               string(def.Track),
			DisplayName:         def.DisplayName,
			Level:               level,
			Cap:                 cap,
			NextCostGold:        nextCost,
			CanAfford:           canAfford,
			HasBlacksmith:       hasBlacksmith,
			HPPerLevel:          def.HPPerLevel,
			DamagePerLevel:      def.DamagePerLevel,
			ArmorPerLevel:       def.ArmorPerLevel,
			AttackSpeedPerLevel: def.AttackSpeedPerLevel,
			MoveSpeedPerLevel:   def.MoveSpeedPerLevel,
		})
	}
	return snapshots
}
