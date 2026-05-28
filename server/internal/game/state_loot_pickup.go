package game

import "webrts/server/pkg/protocol"

// PickupLootWithUnits is the player-issued "right-click chest with selection"
// command. Validates each unit (alive, owned by player) and the target chest
// (exists). Assigns OrderPickupLoot and paths each unit toward the chest; the
// actual collection happens in tickLootDropsLocked when the first unit reaches
// proximity.
//
// Multi-select: all units walk toward the same chest; the first arrival
// collects it; the rest see drop == nil next tick and fall back to OrderIdle.
// Same fan-out / first-arrival pattern as GatherWithUnits.
//
// Acquires s.mu.
func (s *GameState) PickupLootWithUnits(playerID string, unitIDs []int, lootDropID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer profileStart("cmd.PickupLootWithUnits")()

	drop, ok := s.LootDrops[lootDropID]
	if !ok || drop == nil {
		// Stale id — silently no-op (matches gather-on-stale-node behavior).
		return
	}
	dest := protocol.Vec2{X: drop.X, Y: drop.Y}

	blocked := s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	for _, uid := range unitIDs {
		unit := s.getUnitByIDLocked(uid)
		if unit == nil || unit.OwnerID != playerID || unit.HP <= 0 {
			continue
		}
		s.resetUnitMovementLocked(unit, orderID)
		unit.Order = OrderState{
			Type:  OrderPickupLoot,
			DestX: dest.X,
			DestY: dest.Y,
		}
		unit.PickupLootID = drop.ID
		s.assignUnitPath(unit, dest, blocked, nil)
	}
}
