package game

import (
	"math"
	"sort"
	"strconv"
	"strings"
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

// blacksmithUpgradeMaxQueue caps how many upgrades a single blacksmith can have
// queued at once (the in-progress upgrade + everything stacked behind it).
// Mirrors unitProductionMaxQueue for barracks training. The per-track cap
// (tier-gated) usually bites first; this is a hard upper bound on queue depth.
const blacksmithUpgradeMaxQueue = 8

// ActiveUpgrade is one in-progress OR queued building-driven upgrade, stored as
// an entry in a blacksmith's queue (GameState.ActiveUpgrades[buildingID]). Only
// the queue head (index 0) researches: its Remaining counts down each tick, and
// when it reaches 0 the player's Upgrades[Track] is set to TargetLevel, existing
// units are retro-buffed, and the head is popped so the next entry begins.
// TargetLevel is fixed at enqueue time (current level + already-queued count of
// the same track + 1). GoldPaid/WoodPaid are retained so a cancel can issue a
// full refund and so queue reconciliation can rebate the difference when a level
// shifts down. The invariant GoldPaid == upgradeCostForLevel(track, TargetLevel)
// is maintained across enqueue, completion, and cancel.
type ActiveUpgrade struct {
	PlayerID    string
	Track       UpgradeTrack
	Remaining   float64
	Total       float64
	TargetLevel int
	GoldPaid    int
	WoodPaid    int
}

// playerTrackBuildingLocked returns the blacksmith ID whose queue contains the
// given track for playerID (in progress OR merely queued), and the number of
// that track's entries in that queue. Because a track is locked to a single
// blacksmith per player, at most one building can match. Returns ("", 0, false)
// when the track is idle for that player. Iterates in sorted building-ID order
// so the result is deterministic. Must be called under s.mu.
func (s *GameState) playerTrackBuildingLocked(playerID string, track UpgradeTrack) (string, int, bool) {
	ids := make([]string, 0, len(s.ActiveUpgrades))
	for id := range s.ActiveUpgrades {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		count := 0
		for _, au := range s.ActiveUpgrades[id] {
			if au != nil && au.PlayerID == playerID && au.Track == track {
				count++
			}
		}
		if count > 0 {
			return id, count, true
		}
	}
	return "", 0, false
}

// playerTrackResearchLocked returns the actively-researching entry for the
// given track (the head of its home blacksmith's queue, but only when that head
// IS this track — a track queued behind a different track is not yet
// researching). Returns the source building ID and the head entry, or
// ("", nil, false) when the track is not currently at the front of any queue.
// Must be called under s.mu.
func (s *GameState) playerTrackResearchLocked(playerID string, track UpgradeTrack) (string, *ActiveUpgrade, bool) {
	id, _, ok := s.playerTrackBuildingLocked(playerID, track)
	if !ok {
		return "", nil, false
	}
	queue := s.ActiveUpgrades[id]
	if len(queue) > 0 && queue[0] != nil && queue[0].Track == track {
		return id, queue[0], true
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
		if len(s.ActiveUpgrades[b.ID]) > 0 {
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

// findAnyUpgradeBuildingLocked returns the lowest-ID finished blacksmith owned
// by playerID regardless of whether it is busy. Used as the global panel's
// fallback target when every blacksmith already has a queue: a new track still
// needs a home, so it stacks behind the lowest-ID blacksmith's queue. Returns
// ("", false) when the player owns no blacksmith. Deterministic. Must be called
// under s.mu.
func (s *GameState) findAnyUpgradeBuildingLocked(playerID string) (string, bool) {
	candidates := make([]string, 0)
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !isUpgradePurchaseBuildingLocked(b, playerID) {
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

// handlePurchaseUpgradeLocked validates and enqueues a single upgrade purchase.
// Validation failures are silent no-ops.
//
//  1. A target blacksmith is resolved:
//     - If the track already lives at one of the player's blacksmiths (in
//       progress or queued), it stays there: the new level stacks behind it.
//       This enforces the cross-blacksmith lock — a track is confined to one
//       blacksmith. An explicit buildingID that names a DIFFERENT blacksmith is
//       rejected (the track is locked elsewhere).
//     - Otherwise an explicit buildingID (action bar / selected blacksmith) must
//       be a finished, owned upgrade-purchase building.
//     - Otherwise (empty buildingID, global panel) auto-assign to the lowest-ID
//       idle blacksmith, falling back to the lowest-ID blacksmith overall.
//  2. The blacksmith's queue must be below blacksmithUpgradeMaxQueue.
//  3. The PROJECTED level (current + already-queued of this track + 1) must be
//     at or below the tier-gated cap.
//  4. Player must have enough gold AND wood (wood cost == gold cost) for the
//     projected level.
//
// On success: deduct gold and wood and append an ActiveUpgrade to the target
// blacksmith's queue with a blacksmithUpgradeResearchSeconds timer. The level is
// NOT applied here — it advances (and existing units are retro-buffed) only when
// this entry reaches the queue head and its timer completes in
// tickBlacksmithUpgradesLocked. Must be called under s.mu.
func (s *GameState) handlePurchaseUpgradeLocked(playerID, buildingID string, track UpgradeTrack) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}
	def, ok := upgradeTrackDefByID(track)
	if !ok {
		return
	}

	cap := s.upgradeCapForPlayerLocked(playerID)
	if cap <= 0 {
		return
	}

	// Resolve the target blacksmith, honouring the cross-blacksmith track lock.
	homeID, queuedOfTrack, hasHome := s.playerTrackBuildingLocked(playerID, track)
	switch {
	case hasHome:
		// The track is locked to its home blacksmith; stack there. An explicit
		// request to research it at a different blacksmith is rejected.
		if buildingID != "" && buildingID != homeID {
			return
		}
		buildingID = homeID
	case buildingID != "":
		// Action bar: the named building must be a valid blacksmith.
		b := s.getBuildingByIDLocked(buildingID)
		if !isUpgradePurchaseBuildingLocked(b, playerID) {
			return
		}
	default:
		// Global panel: prefer an idle blacksmith, else stack at the lowest-ID one.
		id, found := s.findIdleUpgradeBuildingLocked(playerID)
		if !found {
			id, found = s.findAnyUpgradeBuildingLocked(playerID)
			if !found {
				return
			}
		}
		buildingID = id
	}

	if len(s.ActiveUpgrades[buildingID]) >= blacksmithUpgradeMaxQueue {
		return
	}

	// Projected level accounts for same-track entries already queued ahead.
	nextLevel := player.Upgrades[track] + queuedOfTrack + 1
	if nextLevel > cap {
		return
	}
	cost := upgradeCostForLevel(def, nextLevel)
	// Upgrades cost equal amounts of gold and wood.
	if player.Resources["gold"] < cost || player.Resources["wood"] < cost {
		return
	}

	player.Resources["gold"] -= cost
	player.Resources["wood"] -= cost
	if s.ActiveUpgrades == nil {
		s.ActiveUpgrades = map[string][]*ActiveUpgrade{}
	}
	s.ActiveUpgrades[buildingID] = append(s.ActiveUpgrades[buildingID], &ActiveUpgrade{
		PlayerID:    playerID,
		Track:       track,
		Remaining:   blacksmithUpgradeResearchSeconds,
		Total:       blacksmithUpgradeResearchSeconds,
		TargetLevel: nextLevel,
		GoldPaid:    cost,
		WoodPaid:    cost,
	})
}

// handleCancelUpgradeAtLocked cancels the queued upgrade at (buildingID,
// queueIndex) and refunds the gold + wood paid for that entry. Index 0 is the
// in-progress upgrade; higher indices are queued behind it. After removal the
// rest of the queue is reconciled: any later same-track entries shift their
// TargetLevel (and cost) down, and the price difference is rebated. Silent
// no-op on a bad building, out-of-range index, or an entry owned by another
// player. Must be called under s.mu.
func (s *GameState) handleCancelUpgradeAtLocked(playerID, buildingID string, queueIndex int) {
	queue := s.ActiveUpgrades[buildingID]
	if queueIndex < 0 || queueIndex >= len(queue) {
		return
	}
	entry := queue[queueIndex]
	if entry == nil || entry.PlayerID != playerID {
		return
	}

	if player, ok := s.Players[playerID]; ok {
		if player.Resources == nil {
			player.Resources = map[string]int{}
		}
		player.Resources["gold"] += entry.GoldPaid
		player.Resources["wood"] += entry.WoodPaid
	}

	// Splice the entry out, preserving the leading unit's in-progress timer.
	s.ActiveUpgrades[buildingID] = append(queue[:queueIndex], queue[queueIndex+1:]...)
	if len(s.ActiveUpgrades[buildingID]) == 0 {
		delete(s.ActiveUpgrades, buildingID)
	}
	s.reconcileUpgradeQueueLocked(playerID, buildingID)
}

// reconcileUpgradeQueueLocked re-derives the TargetLevel (and matching cost) of
// every entry in a blacksmith's queue after a mid-queue removal. Walks the queue
// in order, assigning each track its next sequential level above the player's
// current level. When an entry's level drops (a same-track entry ahead of it was
// cancelled), the now-cheaper price difference is rebated to the player and the
// stored GoldPaid/WoodPaid are corrected, preserving the
// GoldPaid == cost(TargetLevel) invariant. Must be called under s.mu.
func (s *GameState) reconcileUpgradeQueueLocked(playerID, buildingID string) {
	queue := s.ActiveUpgrades[buildingID]
	if len(queue) == 0 {
		return
	}
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	nextByTrack := map[UpgradeTrack]int{}
	for _, au := range queue {
		if au == nil {
			continue
		}
		def, ok := upgradeTrackDefByID(au.Track)
		if !ok {
			continue
		}
		base, seen := nextByTrack[au.Track]
		if !seen {
			base = player.Upgrades[au.Track]
		}
		level := base + 1
		nextByTrack[au.Track] = level
		if au.TargetLevel == level {
			continue
		}
		au.TargetLevel = level
		correctCost := upgradeCostForLevel(def, level)
		rebate := au.GoldPaid - correctCost
		if rebate != 0 {
			player.Resources["gold"] += rebate
			player.Resources["wood"] += rebate
		}
		au.GoldPaid = correctCost
		au.WoodPaid = correctCost
	}
}

// tickBlacksmithUpgradesLocked advances the HEAD of every blacksmith's upgrade
// queue by dt. When the head's timer reaches 0, the owning player's
// Upgrades[Track] is set to TargetLevel, existing units of that type are
// retro-buffed, and the head is popped so the next queued entry begins next
// tick. Iterated in sorted building-ID order so completion is deterministic.
// Must be called under s.mu.
func (s *GameState) tickBlacksmithUpgradesLocked(dt float64) {
	ids := make([]string, 0, len(s.ActiveUpgrades))
	for id := range s.ActiveUpgrades {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		queue := s.ActiveUpgrades[id]
		if len(queue) == 0 {
			delete(s.ActiveUpgrades, id)
			continue
		}
		au := queue[0]
		if au == nil {
			s.popUpgradeQueueHeadLocked(id)
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
		s.popUpgradeQueueHeadLocked(id)
	}
}

// popUpgradeQueueHeadLocked removes the front entry of a blacksmith's upgrade
// queue, deleting the registry key when the queue empties. Must be called under
// s.mu.
func (s *GameState) popUpgradeQueueHeadLocked(buildingID string) {
	queue := s.ActiveUpgrades[buildingID]
	if len(queue) <= 1 {
		delete(s.ActiveUpgrades, buildingID)
		return
	}
	s.ActiveUpgrades[buildingID] = queue[1:]
}

// PurchaseUpgrade is the public entry point for upgrade purchases. An empty
// buildingID auto-assigns to an idle blacksmith (global panel); a non-empty
// buildingID targets that specific blacksmith (action bar). Acquires s.mu.
func (s *GameState) PurchaseUpgrade(playerID, buildingID, track string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePurchaseUpgradeLocked(playerID, buildingID, UpgradeTrack(track))
}

// CancelUpgrade is the public entry point for cancelling the in-progress upgrade
// (queue head) at a building, with a full refund. Acquires s.mu.
func (s *GameState) CancelUpgrade(playerID, buildingID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleCancelUpgradeAtLocked(playerID, buildingID, 0)
}

// CancelUpgradeAt cancels a single queued upgrade by index (0 = in progress) at
// a building, refunds it, and reconciles the rest of that blacksmith's queue.
// Acquires s.mu.
func (s *GameState) CancelUpgradeAt(playerID, buildingID string, queueIndex int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleCancelUpgradeAtLocked(playerID, buildingID, queueIndex)
}

// handleUpgradeTownHallLocked validates and begins a town hall tier-up.
// Requirements:
//   - Building exists, is type "townhall", owned by playerID, fully built.
//   - No active tier-up already in progress (no "tierUpRemaining" key).
//   - Current tier below the max (the townhall upgrade chain length).
//   - Player can afford the upgrade.
//
// Cost and duration for each transition come from the next tier's catalog def
// (keep.json, castle.json) via its upgradeCost / upgradeSeconds fields — the
// chain is resolved by upgradeChainFor("townhall").
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

	// Walk the catalog-defined tier chain (townhall → keep → castle). The chain
	// length is the max tier; the def at index currentTier is the next tier and
	// carries the cost + duration for this transition.
	chain := upgradeChainFor("townhall")
	if currentTier >= len(chain) {
		return // already at max tier
	}
	targetDef := chain[currentTier]
	goldCost := targetDef.UpgradeCost["gold"]
	woodCost := targetDef.UpgradeCost["wood"]
	duration := targetDef.UpgradeSeconds
	if duration <= 0 {
		return // misconfigured tier def
	}

	player, ok := s.Players[playerID]
	if !ok {
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
	queue := s.ActiveUpgrades[building.ID]
	if len(queue) == 0 || queue[0] == nil {
		delete(building.Metadata, "upgradeInProgress")
		delete(building.Metadata, "upgradeTrack")
		delete(building.Metadata, "upgradeRemainingSeconds")
		delete(building.Metadata, "upgradeTotalSeconds")
		delete(building.Metadata, "upgradeTargetLevel")
		delete(building.Metadata, "upgradeQueueLength")
		delete(building.Metadata, "queuedUpgradeTracks")
		delete(building.Metadata, "queuedUpgradeLevels")
		return
	}

	head := queue[0]
	building.Metadata["upgradeInProgress"] = true
	building.Metadata["upgradeTrack"] = string(head.Track)
	building.Metadata["upgradeRemainingSeconds"] = head.Remaining
	building.Metadata["upgradeTotalSeconds"] = head.Total
	building.Metadata["upgradeTargetLevel"] = float64(head.TargetLevel)
	building.Metadata["upgradeQueueLength"] = len(queue)
	building.Metadata["queuedUpgradeTracks"] = joinUpgradeTracks(queue)
	building.Metadata["queuedUpgradeLevels"] = joinUpgradeLevels(queue)
}

// joinUpgradeTracks renders a queue's tracks as a comma-separated list in queue
// order (head first), mirroring joinProductionUnitTypes for the SelectionHud.
func joinUpgradeTracks(queue []*ActiveUpgrade) string {
	parts := make([]string, 0, len(queue))
	for _, au := range queue {
		if au == nil {
			continue
		}
		parts = append(parts, string(au.Track))
	}
	return strings.Join(parts, ",")
}

// joinUpgradeLevels renders a queue's target levels as a comma-separated list in
// queue order (head first), aligned 1:1 with joinUpgradeTracks.
func joinUpgradeLevels(queue []*ActiveUpgrade) string {
	parts := make([]string, 0, len(queue))
	for _, au := range queue {
		if au == nil {
			continue
		}
		parts = append(parts, strconv.Itoa(au.TargetLevel))
	}
	return strings.Join(parts, ",")
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

		// How many of this track are queued (in progress + waiting), and which
		// blacksmith owns the track's line. Queuing more stacks at that home.
		homeID, queuedCount, _ := s.playerTrackBuildingLocked(playerID, def.Track)

		// Live research progress: only when this track is at the head of its
		// home blacksmith's queue (a track queued behind another is not yet
		// researching, so its progress bar reads empty).
		var researchTotal, researchRemaining float64
		researchBuildingID := ""
		if bid, au, ok := s.playerTrackResearchLocked(playerID, def.Track); ok {
			researchTotal = au.Total
			researchRemaining = au.Remaining
			researchBuildingID = bid
		}

		// Projected level if everything currently queued completes. The next
		// purchasable level stacks one above that.
		projectedLevel := level + queuedCount
		nextLevel := projectedLevel + 1

		nextCost := 0
		canAfford := false
		if projectedLevel < cap {
			nextCost = upgradeCostForLevel(def, nextLevel)
			// Upgrades cost equal gold and wood.
			canAfford = hasBlacksmith &&
				player.Resources["gold"] >= nextCost &&
				player.Resources["wood"] >= nextCost
		}
		// canStart gates the purchase/queue button: affordable and a target
		// blacksmith exists. The track's home (if any) always accepts another;
		// otherwise any owned blacksmith does. The per-blacksmith queue cap is
		// enforced at purchase time, not surfaced here.
		canStart := canAfford

		snapshots = append(snapshots, protocol.PlayerUpgradeSnapshot{
			Track:               string(def.Track),
			DisplayName:         def.DisplayName,
			Level:               level,
			Cap:                 cap,
			QueuedCount:         queuedCount,
			NextCostGold:        nextCost,
			NextCostWood:        nextCost,
			CanAfford:           canAfford,
			CanStart:            canStart,
			HasBlacksmith:       hasBlacksmith,
			ResearchTotal:       researchTotal,
			ResearchRemaining:   researchRemaining,
			ResearchBuildingID:  researchBuildingID,
			QueueBuildingID:     homeID,
			HPPerLevel:          def.HPPerLevel,
			DamagePerLevel:      def.DamagePerLevel,
			ArmorPerLevel:       def.ArmorPerLevel,
			AttackSpeedPerLevel: def.AttackSpeedPerLevel,
			MoveSpeedPerLevel:   def.MoveSpeedPerLevel,
		})
	}
	return snapshots
}
