package game

import (
	"log/slog"
	"sort"
	"strconv"

	"webrts/server/pkg/protocol"
)

// lootPickupRadiusCells is the world-pixel distance from a chest center
// within which a unit on OrderPickupLoot collects the chest, expressed as a
// fraction of one cell. Tuned to half a cell — generous enough that movement
// steering doesn't oscillate at the boundary; tight enough that two units
// approaching from different sides can't both "arrive" in the same tick.
const lootPickupRadiusCells = 0.5

// tickLootDropsLocked drains chest pickups each tick. For every unit on
// OrderPickupLoot, validates the chest still exists and the unit is close
// enough; on success grants contents to the owning player and despawns the
// chest.
//
// Stale-id pickup attempts (chest already collected by a faster ally) quietly
// fall back to OrderIdle — no error, no toast.
//
// Must be called under s.mu write lock.
func (s *GameState) tickLootDropsLocked() {
	if len(s.Units) == 0 {
		return
	}
	cellSize := s.MapConfig.CellSize
	pickupDistSq := (lootPickupRadiusCells * cellSize) * (lootPickupRadiusCells * cellSize)

	for _, unit := range s.Units {
		if unit == nil || unit.Order.Type != OrderPickupLoot || unit.PickupLootID == "" {
			continue
		}
		drop, ok := s.LootDrops[unit.PickupLootID]
		if !ok || drop == nil {
			// Chest already collected; clear and idle. AI_RULES rule 3.
			unit.PickupLootID = ""
			unit.Order = OrderState{Type: OrderIdle}
			continue
		}
		if unit.HP <= 0 {
			// Dead carrier — clear the order; the chest stays for allies.
			unit.PickupLootID = ""
			continue
		}
		player := s.Players[unit.OwnerID]
		if player == nil {
			unit.PickupLootID = ""
			unit.Order = OrderState{Type: OrderIdle}
			continue
		}
		dx := unit.X - drop.X
		dy := unit.Y - drop.Y
		if dx*dx+dy*dy > pickupDistSq {
			// Still in transit; movement system is steering toward
			// (drop.X, drop.Y). Leave the order alone.
			continue
		}
		// Collect.
		notif := s.grantLootDropToPlayerLocked(player, drop)
		notif.CollectingUnitID = unit.ID
		delete(s.LootDrops, drop.ID)
		unit.PickupLootID = ""
		unit.Order = OrderState{Type: OrderIdle}
		s.enqueueLootNotificationLocked(notif)
	}
}

// grantLootDropToPlayerLocked transfers a chest's pre-rolled contents to the
// player. Resources are granted unconditionally; items that don't fit the
// vault are dropped (lost) and reported via OverflowItemIDs on the returned
// LootCollectedNotification.
//
// Must be called under s.mu write lock.
func (s *GameState) grantLootDropToPlayerLocked(player *Player, drop *LootDrop) protocol.LootCollectedNotification {
	notif := protocol.LootCollectedNotification{
		Type:       "loot_collected",
		PlayerID:   player.ID,
		LootDropID: drop.ID,
	}
	if len(drop.ResourceGrants) > 0 {
		notif.Resources = make(map[string]int, len(drop.ResourceGrants))
		for k, v := range drop.ResourceGrants {
			s.grantResourceToPlayerLocked(player, k, v)
			notif.Resources[k] = v
		}
	}
	for _, itemID := range drop.ItemGrants {
		def, ok := getItemDef(itemID)
		if !ok {
			slog.Warn("grantLootDropToPlayerLocked: unknown item id (catalog drift)",
				"playerID", player.ID, "itemID", itemID)
			continue
		}
		ok = s.addItemToVaultLocked(player, def)
		if ok {
			notif.ItemIDs = append(notif.ItemIDs, itemID)
		} else {
			notif.OverflowItemIDs = append(notif.OverflowItemIDs, itemID)
		}
	}
	return notif
}

// enqueueLootNotificationLocked appends a notification to the per-tick queue.
// The match broadcast loop drains this slice after pushing snapshots so each
// notification reaches the relevant player exactly once.
//
// Must be called under s.mu write lock.
func (s *GameState) enqueueLootNotificationLocked(n protocol.LootCollectedNotification) {
	s.pendingLootNotifications = append(s.pendingLootNotifications, n)
}

// DrainPendingLootNotifications returns the current notification queue and
// clears it on s. Called by the match broadcast loop after snapshots have
// been pushed.
//
// Acquires s.mu write lock internally so the caller does not need to hold it.
func (s *GameState) DrainPendingLootNotifications() []protocol.LootCollectedNotification {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingLootNotifications) == 0 {
		return nil
	}
	out := s.pendingLootNotifications
	s.pendingLootNotifications = nil
	return out
}

// drainPendingLootNotificationsLocked is the lock-held variant for test use.
// Must be called under s.mu write lock.
func (s *GameState) drainPendingLootNotificationsLocked() []protocol.LootCollectedNotification {
	if len(s.pendingLootNotifications) == 0 {
		return nil
	}
	out := s.pendingLootNotifications
	s.pendingLootNotifications = nil
	return out
}

// LootDrop is a server-authoritative ground-loot chest. Contents are
// pre-rolled at spawn time so save/replay determinism is independent of
// when the player chooses to pick the chest up. The chest persists in
// world space until a friendly unit walks within pickup range; on
// collection contents transfer to player.Resources / player.Vault.
//
// All references to this struct from other state (e.g. Unit.PickupLootID)
// are by string ID per AI_RULES — never persist a *LootDrop on another
// struct that survives the tick.
type LootDrop struct {
	ID             string
	X, Y           float64
	SourceCampID   string // for debug only; not used by gameplay
	ResourceGrants map[string]int
	ItemGrants     []string
	IconKey        string
}

// chestIconKeyDefault is the v1 sprite key. Tier-varying visuals are
// out of scope; future work can vary by source camp tier.
const chestIconKeyDefault = "treasure_chest"

// lootDropSnapshotsLocked returns the wire view of every chest, sorted
// by ID for deterministic snapshot output.
//
// Must be called under s.mu read lock.
func (s *GameState) lootDropSnapshotsLocked() []protocol.LootDropSnapshot {
	if len(s.LootDrops) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.LootDrops))
	for id := range s.LootDrops {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]protocol.LootDropSnapshot, 0, len(ids))
	for _, id := range ids {
		d := s.LootDrops[id]
		out = append(out, protocol.LootDropSnapshot{
			ID:        d.ID,
			X:         d.X,
			Y:         d.Y,
			IconKey:   d.IconKey,
			Resources: d.ResourceGrants,
			ItemIDs:   d.ItemGrants,
		})
	}
	return out
}

// spawnLootDropLocked allocates a chest ID, computes world coords from
// the camp center, and inserts the drop into s.LootDrops.
//
// Must be called under s.mu write lock.
func (s *GameState) spawnLootDropLocked(camp *NeutralCamp, resources map[string]int, items []string) *LootDrop {
	cellSize := s.MapConfig.CellSize
	s.nextLootDropID++
	id := "loot-" + strconv.Itoa(s.nextLootDropID)
	drop := &LootDrop{
		ID:             id,
		X:              float64(camp.X)*cellSize + cellSize/2,
		Y:              float64(camp.Y)*cellSize + cellSize/2,
		SourceCampID:   camp.PlacementID,
		ResourceGrants: resources,
		ItemGrants:     items,
		IconKey:        chestIconKeyDefault,
	}
	if s.LootDrops == nil {
		s.LootDrops = map[string]*LootDrop{}
	}
	s.LootDrops[id] = drop
	return drop
}

// maybeDropChestForCampLocked is called when a neutral camp transitions
// from AliveUnitIDs>0 to 0 due to player damage (NOT due to wave-start
// despawn — see the State guard in onUnitRemovedFromCampLocked).
//
// Rolls the camp's loot table once on s.rngLoot:
//   - top-level 1..100
//   - in-range entry → resolve packaged item
//     - resource_bundle: grants collected verbatim
//     - item_subtable: roll 1..max(entries); on hit, append item id
//   - gap on top-level → no chest
//   - sub-table gap + no resource side → no chest (indistinguishable
//     from "no drop", don't litter the world)
//
// Must be called under s.mu write lock.
func (s *GameState) maybeDropChestForCampLocked(camp *NeutralCamp) {
	if camp == nil || camp.SpawnedGroupID == "" {
		return
	}
	// A camp wiped by the enemy wave faction (EnemiesFightNeutrals maps) drops
	// no loot — only players earn camp loot.
	if camp.LastKillerWasEnemy {
		return
	}
	tier := resolveNeutralTier(camp.CurrentTier)
	if tier == 0 {
		return
	}
	group, ok := getNeutralGroup(tier, camp.SpawnedGroupID)
	if !ok {
		return
	}
	// A group drops from a TABLE (a weighted roll over lists, resource grants and
	// no-drop outcomes) or straight from a LIST (roll it, always get an item) —
	// never both; the catalog loader rejects a group that sets two sources.
	if group.LootList != "" {
		s.dropListChestForCampLocked(camp, group.LootList)
		return
	}
	if group.LootTable == "" {
		return
	}
	table, ok := getTableDef(group.LootTable)
	if !ok {
		slog.Warn("maybeDropChestForCampLocked: loot table missing (catalog drift)",
			"campID", camp.PlacementID, "lootTable", group.LootTable)
		return
	}

	result := s.rollTableLocked(table)
	if result.Empty() {
		// The roll landed on a `nothing` row. A real outcome, not a failure.
		return
	}
	s.spawnLootDropLocked(camp, result.Resources, result.Items)
}

// dropListChestForCampLocked drops one item from a list — by weight if the list
// is weighted, evenly if it is uniform.
//
// A list ALWAYS yields an item, so a camp whose loot source is a list always
// drops a chest. To give a camp a chance of dropping nothing (or of dropping
// gold), give it a TABLE: whether anything drops at all is a table's decision,
// not a pool's.
//
// Rolls on s.rngLoot like every other loot decision, so a fixed seed still
// reproduces the drop exactly. Must be called under s.mu write lock.
func (s *GameState) dropListChestForCampLocked(camp *NeutralCamp, listID string) {
	list, ok := getListDef(listID)
	if !ok {
		slog.Warn("dropListChestForCampLocked: loot list missing (catalog drift)",
			"campID", camp.PlacementID, "lootList", listID)
		return
	}
	item := s.rollListLocked(list)
	if item == "" {
		return // validateListDef forbids an empty list; defensive.
	}
	s.spawnLootDropLocked(camp, nil, []string{item})
}
