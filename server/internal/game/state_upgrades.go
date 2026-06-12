package game

import (
	"math"
	"sort"
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

// blacksmithUpgradeResearchSeconds is how long a blacksmith upgrade takes to
// complete after purchase. Resources are deducted up front; the level only
// advances once this many seconds have elapsed (see
// tickBlacksmithUpgradesLocked). Flat for now across all tracks and levels.
const blacksmithUpgradeResearchSeconds = 15.0

// ActiveUpgrade is one in-progress building-driven upgrade, stored in the
// global GameState.ActiveUpgrades registry keyed by the SOURCE building ID.
// Remaining counts down each tick; when it reaches 0 the player's
// Upgrades[Track] is set to TargetLevel and existing units are retro-buffed.
// GoldPaid/WoodPaid are retained so a cancel can issue a full refund.
type ActiveUpgrade struct {
	PlayerID    string
	Track       UpgradeTrack
	Remaining   float64
	Total       float64
	TargetLevel int
	GoldPaid    int
	WoodPaid    int
}

// playerTrackResearchLocked scans the registry for an in-progress upgrade of
// the given track owned by playerID. Returns the source building ID and the
// entry, or ("", nil, false) when the track is idle for that player. Iterates
// in sorted building-ID order so the result is deterministic. Must be called
// under s.mu.
func (s *GameState) playerTrackResearchLocked(playerID string, track UpgradeTrack) (string, *ActiveUpgrade, bool) {
	ids := make([]string, 0, len(s.ActiveUpgrades))
	for id := range s.ActiveUpgrades {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		au := s.ActiveUpgrades[id]
		if au != nil && au.PlayerID == playerID && au.Track == track {
			return id, au, true
		}
	}
	return "", nil, false
}

// isUpgradePurchaseBuildingLocked reports whether b is a finished, owned
// (by playerID), upgrade-purchase building eligible to research upgrades.
// Must be called under s.mu.
func isUpgradePurchaseBuildingLocked(b *protocol.BuildingTile, playerID string) bool {
	if b == nil || !b.Visible {
		return false
	}
	if b.OwnerID == nil || *b.OwnerID != playerID {
		return false
	}
	if getMetadataBool(b.Metadata, "underConstruction") {
		return false
	}
	for _, cap := range b.Capabilities {
		if cap == "upgrade-purchase" {
			return true
		}
	}
	return false
}

// findIdleUpgradeBuildingLocked returns the lowest-ID finished blacksmith owned
// by playerID that is NOT currently researching anything, or ("", false) when
// every blacksmith is busy (or the player owns none). Deterministic. Used by
// the global Blacksmith panel's auto-assign purchase path. Must be called
// under s.mu.
func (s *GameState) findIdleUpgradeBuildingLocked(playerID string) (string, bool) {
	candidates := make([]string, 0)
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !isUpgradePurchaseBuildingLocked(b, playerID) {
			continue
		}
		if _, busy := s.ActiveUpgrades[b.ID]; busy {
			continue
		}
		candidates = append(candidates, b.ID)
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.Strings(candidates)
	return candidates[0], true
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
	if !ok {
		return
	}
	// Per-unit-type upgrade tracks (workshop / armoury). Guard on the
	// Upgrades map being non-nil — wave-upgrade buffs and the profile
	// damage multiplier below run regardless.
	if player.Upgrades != nil {
		track := UpgradeTrack(unit.UnitType)
		trackDef, hasDef := upgradeTrackDefByID(track)
		if hasDef {
			level := player.Upgrades[track]
			if level > 0 {
				unit.BaseMaxHP += trackDef.HPPerLevel * level
				unit.BaseDamage += trackDef.DamagePerLevel * level
				unit.BaseArmor += trackDef.ArmorPerLevel * level
				unit.BaseAttackSpeed += trackDef.AttackSpeedPerLevel * float64(level)
				unit.BaseMoveSpeed += trackDef.MoveSpeedPerLevel * float64(level)
			}
		}
	}

	// Apply cumulative wave upgrade multipliers so units spawned mid-run
	// receive the same stat bonuses as units alive when the upgrade was chosen.
	for _, buff := range player.UpgradeState.WaveStatBuffs {
		if !unitMatchesWaveStatBuff(buff, unit) {
			continue
		}
		applyStatMultiplierToUnit(UpgradeDef{
			Effect: UpgradeEffect{Stat: buff.Stat, Multiplier: buff.Multiplier},
		}, unit)
	}

	// Bake the player's profile damage multiplier into BaseDamage so the
	// unit's displayed stat (and every downstream attack path) reads the
	// buffed number. Physical/magic split is driven by the unit's
	// AttackDamageType — empty/unset defaults to physical via OrPhysical().
	// Match-start application means the bonus is locked at spawn and won't
	// change if the player toggles the upgrade off mid-match.
	mult := 1.0
	if unit.AttackDamageType.OrPhysical() == DamagePhysical {
		mult = player.PhysicalDamageMultiplier
	} else {
		mult = player.MagicDamageMultiplier
	}
	if mult != 1.0 && unit.BaseDamage > 0 {
		unit.BaseDamage = int(math.Round(float64(unit.BaseDamage) * mult))
	}
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

// handlePurchaseUpgradeLocked validates and begins a single upgrade purchase.
// Validation failures are silent no-ops.
//  1. A target blacksmith is resolved: the given buildingID (must be a
//     finished, owned, idle upgrade-purchase building) or — when buildingID is
//     empty (global panel) — the lowest-ID idle blacksmith the player owns.
//  2. Current level must be below the cap (tier-gated).
//  3. The track must not already be researching at ANY of the player's
//     buildings (one upgrade of a kind at a time, locked across blacksmiths).
//  4. Player must have enough gold AND wood (wood cost == gold cost).
//
// On success: deduct gold and wood and register an ActiveUpgrade on the target
// building with a blacksmithUpgradeResearchSeconds timer. The level is NOT
// applied here — it advances (and existing units are retro-buffed) only when
// the timer completes in tickBlacksmithUpgradesLocked. Must be called under s.mu.
func (s *GameState) handlePurchaseUpgradeLocked(playerID, buildingID string, track UpgradeTrack) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}
	def, ok := upgradeTrackDefByID(track)
	if !ok {
		return
	}

	// Resolve the target blacksmith.
	if buildingID == "" {
		// Global panel: auto-assign to any idle blacksmith.
		id, found := s.findIdleUpgradeBuildingLocked(playerID)
		if !found {
			return
		}
		buildingID = id
	} else {
		// Action bar: the named building must be a valid, idle blacksmith.
		b := s.getBuildingByIDLocked(buildingID)
		if !isUpgradePurchaseBuildingLocked(b, playerID) {
			return
		}
		if _, busy := s.ActiveUpgrades[buildingID]; busy {
			return
		}
	}

	cap := s.upgradeCapForPlayerLocked(playerID)
	if cap <= 0 {
		return
	}
	// Lock: the track must not already be researching anywhere for this player.
	if _, _, inProgress := s.playerTrackResearchLocked(playerID, track); inProgress {
		return
	}
	currentLevel := player.Upgrades[track]
	if currentLevel >= cap {
		return
	}
	nextLevel := currentLevel + 1
	cost := upgradeCostForLevel(def, nextLevel)
	// Upgrades cost equal amounts of gold and wood.
	if player.Resources["gold"] < cost || player.Resources["wood"] < cost {
		return
	}

	player.Resources["gold"] -= cost
	player.Resources["wood"] -= cost
	if s.ActiveUpgrades == nil {
		s.ActiveUpgrades = map[string]*ActiveUpgrade{}
	}
	s.ActiveUpgrades[buildingID] = &ActiveUpgrade{
		PlayerID:    playerID,
		Track:       track,
		Remaining:   blacksmithUpgradeResearchSeconds,
		Total:       blacksmithUpgradeResearchSeconds,
		TargetLevel: nextLevel,
		GoldPaid:    cost,
		WoodPaid:    cost,
	}
}

// handleCancelUpgradeLocked cancels the in-progress upgrade at buildingID and
// issues a FULL refund of the gold + wood that was paid. Silent no-op when the
// building has no active upgrade or the upgrade belongs to another player.
// Must be called under s.mu.
func (s *GameState) handleCancelUpgradeLocked(playerID, buildingID string) {
	au, ok := s.ActiveUpgrades[buildingID]
	if !ok || au == nil || au.PlayerID != playerID {
		return
	}
	if player, ok := s.Players[playerID]; ok {
		if player.Resources == nil {
			player.Resources = map[string]int{}
		}
		player.Resources["gold"] += au.GoldPaid
		player.Resources["wood"] += au.WoodPaid
	}
	delete(s.ActiveUpgrades, buildingID)
}

// tickBlacksmithUpgradesLocked advances every in-progress upgrade in the
// registry by dt. When an entry's timer reaches 0, the owning player's
// Upgrades[Track] is set to TargetLevel, existing units of that type are
// retro-buffed, and the entry is removed. Iterated in sorted building-ID order
// so completion is deterministic. Must be called under s.mu.
func (s *GameState) tickBlacksmithUpgradesLocked(dt float64) {
	ids := make([]string, 0, len(s.ActiveUpgrades))
	for id := range s.ActiveUpgrades {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		au := s.ActiveUpgrades[id]
		if au == nil {
			delete(s.ActiveUpgrades, id)
			continue
		}
		au.Remaining -= dt
		if au.Remaining > 0 {
			continue
		}
		// Complete: apply the level to the player and retro-buff their units.
		if player, ok := s.Players[au.PlayerID]; ok {
			if player.Upgrades == nil {
				player.Upgrades = map[UpgradeTrack]int{}
			}
			player.Upgrades[au.Track] = au.TargetLevel
			s.reapplyUpgradesToOwnedUnitsByTypeLocked(au.PlayerID, au.Track)
		}
		delete(s.ActiveUpgrades, id)
	}
}

// PurchaseUpgrade is the public entry point for upgrade purchases. An empty
// buildingID auto-assigns to an idle blacksmith (global panel); a non-empty
// buildingID targets that specific blacksmith (action bar). Acquires s.mu.
func (s *GameState) PurchaseUpgrade(playerID, buildingID, track string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePurchaseUpgradeLocked(playerID, buildingID, UpgradeTrack(track))
}

// CancelUpgrade is the public entry point for cancelling an in-progress upgrade
// at a building (full refund). Acquires s.mu.
func (s *GameState) CancelUpgrade(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleCancelUpgradeLocked(playerID, buildingID)
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

// refreshUpgradeVisualMetadataLocked stamps (or clears) the display-only
// upgrade-in-progress metadata on a single building from the registry entry
// keyed by THIS building's ID. While the building has an ActiveUpgrade it
// writes upgradeInProgress/upgradeTrack/upgradeRemainingSeconds/
// upgradeTotalSeconds; otherwise it deletes those keys. Because the source is
// per-building, only the blacksmith actually performing the research animates —
// not every blacksmith the player owns. Mirrors the production metadata pattern
// in refreshBuildingRuntimeMetadataLocked; drives the client's training
// animation + production-style card. Must be called under s.mu.
func (s *GameState) refreshUpgradeVisualMetadataLocked(building *protocol.BuildingTile) {
	au, ok := s.ActiveUpgrades[building.ID]
	if !ok || au == nil {
		delete(building.Metadata, "upgradeInProgress")
		delete(building.Metadata, "upgradeTrack")
		delete(building.Metadata, "upgradeRemainingSeconds")
		delete(building.Metadata, "upgradeTotalSeconds")
		return
	}

	building.Metadata["upgradeInProgress"] = true
	building.Metadata["upgradeTrack"] = string(au.Track)
	building.Metadata["upgradeRemainingSeconds"] = au.Remaining
	building.Metadata["upgradeTotalSeconds"] = au.Total
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
	// Whether an idle blacksmith exists, computed once — drives the global
	// panel's auto-assign "can start" gate. (A track already researching at a
	// blacksmith does not free another, so this can be true while one track is
	// busy and another is startable.)
	_, hasIdleBlacksmith := s.findIdleUpgradeBuildingLocked(playerID)

	snapshots := make([]protocol.PlayerUpgradeSnapshot, 0, len(upgradeTrackDefs))
	for _, def := range upgradeTrackDefs {
		level := 0
		if player.Upgrades != nil {
			level = player.Upgrades[def.Track]
		}
		nextLevel := level + 1

		// Live research progress for this track, from the global registry.
		var researchTotal, researchRemaining float64
		researchBuildingID := ""
		if bid, au, ok := s.playerTrackResearchLocked(playerID, def.Track); ok {
			researchTotal = au.Total
			researchRemaining = au.Remaining
			researchBuildingID = bid
		}
		researching := researchTotal > 0

		nextCost := 0
		canAfford := false
		if level < cap && !researching {
			nextCost = upgradeCostForLevel(def, nextLevel)
			// Upgrades cost equal gold and wood.
			canAfford = hasBlacksmith &&
				player.Resources["gold"] >= nextCost &&
				player.Resources["wood"] >= nextCost
		}
		// canStart gates the global panel's auto-assign button: affordable,
		// not at cap, not already researching, and at least one idle blacksmith.
		canStart := canAfford && hasIdleBlacksmith

		snapshots = append(snapshots, protocol.PlayerUpgradeSnapshot{
			Track:               string(def.Track),
			DisplayName:         def.DisplayName,
			Level:               level,
			Cap:                 cap,
			NextCostGold:        nextCost,
			NextCostWood:        nextCost,
			CanAfford:           canAfford,
			CanStart:            canStart,
			HasBlacksmith:       hasBlacksmith,
			ResearchTotal:       researchTotal,
			ResearchRemaining:   researchRemaining,
			ResearchBuildingID:  researchBuildingID,
			HPPerLevel:          def.HPPerLevel,
			DamagePerLevel:      def.DamagePerLevel,
			ArmorPerLevel:       def.ArmorPerLevel,
			AttackSpeedPerLevel: def.AttackSpeedPerLevel,
			MoveSpeedPerLevel:   def.MoveSpeedPerLevel,
		})
	}
	return snapshots
}
