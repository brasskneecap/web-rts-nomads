package game

import "webrts/server/pkg/protocol"

// ─── Runtime structs ─────────────────────────────────────────────────────────

// VaultItem is one entry in a player's vault inventory. Equipment always has
// Stacks=1 and gets a unique InstanceID. Consumables may stack multiple copies
// into a single entry up to MaxStacks.
type VaultItem struct {
	InstanceID int64  `json:"instanceId"`
	ItemID     string `json:"itemId"`
	Stacks     int    `json:"stacks"`
}

// EquippedItem is one item occupying a unit's equipment slot. Mirrors VaultItem
// but lives on the unit rather than in the vault. Stacks > 1 is possible for
// consumable items that have been equipped.
type EquippedItem struct {
	InstanceID int64  `json:"instanceId"`
	ItemID     string `json:"itemId"`
	Stacks     int    `json:"stacks"`
}

// UnitEquipmentBonus accumulates the flat stat bonuses from all equipped items.
// Recomputed by recomputeUnitEquipmentBonusLocked whenever the unit's loadout
// changes. Applied inside applyRankModifiersLocked after path/rank multipliers.
type UnitEquipmentBonus struct {
	HP          int
	Damage      int
	Armor       int
	AttackSpeed float64
	MoveSpeed   float64
	HealthRegen float64
	MaxShield   int
}

// ─── Capacity / presence helpers ─────────────────────────────────────────────

// vaultCapacityForPlayerLocked returns the number of vault slots available to
// a player, gated by their townhall tier. Tier 1 → 5, Tier 2 → 10, Tier 3 → 15.
// If the TH is destroyed (tier 0), returns 0 — new purchases are blocked even
// if the vault already has items. Must be called under s.mu.
func (s *GameState) vaultCapacityForPlayerLocked(playerID string) int {
	tier := s.townhallTierForPlayerLocked(playerID)
	switch tier {
	case 1:
		return 5
	case 2:
		return 10
	case 3:
		return 15
	default:
		return 0
	}
}

// playerHasMarketplaceLocked returns true if the player owns at least one
// fully-built (not under-construction) building with the "item-purchase"
// capability. Must be called under s.mu.
func (s *GameState) playerHasMarketplaceLocked(playerID string) bool {
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
			if cap == "item-purchase" {
				return true
			}
		}
	}
	return false
}

// ─── Vault mutation helpers ───────────────────────────────────────────────────

// nextItemInstanceID increments and returns the next unique instance ID under
// the lock. Callers must already hold s.mu.
func (s *GameState) allocItemInstanceIDLocked() int64 {
	s.nextItemInstanceID++
	return s.nextItemInstanceID
}

// itemMaxStacks returns the effective maximum stack count for def. If
// def.MaxStacks is 0 (unset), non-consumables always return 1. For
// consumables, 0 is also treated as 1 (non-stackable consumable).
func itemMaxStacks(def *ItemDef) int {
	if def.MaxStacks > 0 {
		return def.MaxStacks
	}
	return 1
}

// addItemToVaultLocked attempts to add one unit of def to the player's vault.
// Returns true on success, false if the vault is at capacity and no stack slot
// is available.
//
// Stacking rules:
//   - Equipment: always creates a new VaultItem with a unique InstanceID; counts
//     against capacity regardless of existing entries.
//   - Consumable: if an existing entry with the same ItemID has room to stack,
//     increment Stacks (no new entry, no capacity check). Otherwise, a new entry
//     is created — that counts against capacity.
//
// Must be called under s.mu.
func (s *GameState) addItemToVaultLocked(player *Player, def *ItemDef) bool {
	if def.Kind == ItemKindConsumable {
		// Try to stack onto an existing entry first.
		maxSt := itemMaxStacks(def)
		for _, vi := range player.Vault {
			if vi.ItemID == def.ID && vi.Stacks < maxSt {
				vi.Stacks++
				return true
			}
		}
		// No stackable slot found; need a new entry — check capacity.
		capacity := s.vaultCapacityForPlayerLocked(player.ID)
		if len(player.Vault) >= capacity {
			return false
		}
		player.Vault = append(player.Vault, &VaultItem{
			InstanceID: s.allocItemInstanceIDLocked(),
			ItemID:     def.ID,
			Stacks:     1,
		})
		return true
	}

	// Equipment always needs a new slot.
	capacity := s.vaultCapacityForPlayerLocked(player.ID)
	if len(player.Vault) >= capacity {
		return false
	}
	player.Vault = append(player.Vault, &VaultItem{
		InstanceID: s.allocItemInstanceIDLocked(),
		ItemID:     def.ID,
		Stacks:     1,
	})
	return true
}

// removeItemFromVaultByInstanceLocked finds the VaultItem with the given
// instanceID and removes one unit from it.
//   - For consumables with Stacks > 1: decrements Stacks, returns a copy with
//     Stacks=1, and leaves the entry in the vault.
//   - Otherwise (equipment or last stack): removes the entry and returns it.
//
// Returns (nil, false) if no matching entry is found.
// Must be called under s.mu.
func (s *GameState) removeItemFromVaultByInstanceLocked(player *Player, instanceID int64) (*VaultItem, bool) {
	for i, vi := range player.Vault {
		if vi.InstanceID != instanceID {
			continue
		}
		if vi.Stacks > 1 {
			vi.Stacks--
			// Return a detached copy representing the single unit removed.
			return &VaultItem{InstanceID: vi.InstanceID, ItemID: vi.ItemID, Stacks: 1}, true
		}
		// Last stack (or equipment): remove the entry.
		player.Vault = append(player.Vault[:i], player.Vault[i+1:]...)
		return vi, true
	}
	return nil, false
}

// ─── Inventory size / bonus recomputation ────────────────────────────────────

// setInventorySizeForRankLocked sets unit.InventorySize based on rank and
// grows unit.Equipped to match if it has grown. Never shrinks (rank can't
// decrease). Must be called under s.mu.
func (s *GameState) setInventorySizeForRankLocked(unit *Unit) {
	var size int
	switch unit.Rank {
	case unitRankBronze:
		size = 1
	case unitRankSilver:
		size = 2
	case unitRankGold:
		size = 3
	default:
		size = 0
	}
	unit.InventorySize = size
	for len(unit.Equipped) < size {
		unit.Equipped = append(unit.Equipped, nil)
	}
}

// recomputeUnitEquipmentBonusLocked zeroes unit.EquipmentBonus and rebuilds it
// from the unit's currently equipped items, then rebakes derived stats via
// applyRankModifiersLocked. Must be called under s.mu.
//
// HealthRegen is handled separately from other bonuses because
// applyRankModifiersLocked does not zero and recompute HealthRegenPerSecond
// from a Base* value (unlike Damage, MaxHP, Armor, etc.). Instead, we compute
// the delta between the old and new regen bonus and apply it directly to
// unit.HealthRegenPerSecond so perk-applied regen is preserved.
func (s *GameState) recomputeUnitEquipmentBonusLocked(unit *Unit) {
	oldRegenBonus := unit.EquipmentBonus.HealthRegen

	unit.EquipmentBonus = UnitEquipmentBonus{}
	for _, slot := range unit.Equipped {
		if slot == nil {
			continue
		}
		def, ok := s.itemCatalog[slot.ItemID]
		if !ok || def.Modifiers == nil {
			continue
		}
		unit.EquipmentBonus.Damage += def.Modifiers.Damage
		unit.EquipmentBonus.HP += def.Modifiers.HP
		unit.EquipmentBonus.Armor += def.Modifiers.Armor
		unit.EquipmentBonus.AttackSpeed += def.Modifiers.AttackSpeed
		unit.EquipmentBonus.MoveSpeed += def.Modifiers.MoveSpeed
		unit.EquipmentBonus.HealthRegen += def.Modifiers.HealthRegen
		unit.EquipmentBonus.MaxShield += def.Modifiers.MaxShield
	}

	// Apply delta to HealthRegenPerSecond directly.
	regenDelta := unit.EquipmentBonus.HealthRegen - oldRegenBonus
	if regenDelta != 0 {
		unit.HealthRegenPerSecond += regenDelta
		if unit.HealthRegenPerSecond < 0 {
			unit.HealthRegenPerSecond = 0
		}
	}

	// Rebake derived stats (Damage, MaxHP, Armor, AttackSpeed, MoveSpeed,
	// AttackRange). preserveHealthPercent=true so equipping an HP item
	// preserves the unit's HP fraction rather than capping at old MaxHP.
	s.applyRankModifiersLocked(unit, true)
}

// ─── Consumable application ───────────────────────────────────────────────────

// applyConsumableEffectLocked applies the effect described by def.Consumable to
// unit. Always consumes even if the unit is already at full HP (by spec).
// Must be called under s.mu.
func (s *GameState) applyConsumableEffectLocked(unit *Unit, def *ItemDef) {
	if def.Consumable == nil {
		return
	}
	switch def.Consumable.Type {
	case "heal":
		healed := unit.HP + def.Consumable.Amount
		if healed > unit.MaxHP {
			healed = unit.MaxHP
		}
		unit.HP = healed
	// Future consumable types: add cases here.
	}
}

// ─── Core action handlers ────────────────────────────────────────────────────

// handlePurchaseItemLocked validates and executes a single item purchase from a
// marketplace. Validation failures are silent no-ops. On success: deduct gold,
// add item to vault. Must be called under s.mu.
func (s *GameState) handlePurchaseItemLocked(playerID, buildingID, itemID string) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	// Building must exist, be owned by this player, have item-purchase
	// capability, and not be under construction.
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if building.OwnerID == nil || *building.OwnerID != playerID {
		return
	}
	if getMetadataBool(building.Metadata, "underConstruction") {
		return
	}
	hasCapability := false
	for _, cap := range building.Capabilities {
		if cap == "item-purchase" {
			hasCapability = true
			break
		}
	}
	if !hasCapability {
		return
	}

	// Item must exist in catalog.
	def, ok := s.itemCatalog[itemID]
	if !ok {
		return
	}

	// RequiredBuilding gate: if the item declares a required building type, the
	// targeted building must be of that type. Items with empty RequiredBuilding
	// are available wherever item-purchase is offered.
	if def.RequiredBuilding != "" && building.BuildingType != def.RequiredBuilding {
		return
	}

	// TH destroyed = no purchases, even if vault has space.
	if s.townhallTierForPlayerLocked(playerID) == 0 {
		return
	}

	// Afford check.
	if player.Resources["gold"] < def.CostGold {
		return
	}

	// Capacity pre-check: if a new vault entry would be needed, verify room.
	// addItemToVaultLocked handles both the stacking and the capacity guard, so
	// we attempt the add first; if it returns false we abort before deducting.
	// To avoid deducting gold before the vault check, we do a dry-run first.
	if def.Kind == ItemKindConsumable {
		maxSt := itemMaxStacks(def)
		canStack := false
		for _, vi := range player.Vault {
			if vi.ItemID == def.ID && vi.Stacks < maxSt {
				canStack = true
				break
			}
		}
		if !canStack {
			capacity := s.vaultCapacityForPlayerLocked(playerID)
			if len(player.Vault) >= capacity {
				return
			}
		}
	} else {
		// Equipment always needs a new slot.
		capacity := s.vaultCapacityForPlayerLocked(playerID)
		if len(player.Vault) >= capacity {
			return
		}
	}

	player.Resources["gold"] -= def.CostGold
	s.addItemToVaultLocked(player, def)
}

// handleEquipItemLocked validates and moves an item from the player's vault
// into a unit's equipment slot. Validation failures are silent no-ops.
// Must be called under s.mu.
func (s *GameState) handleEquipItemLocked(playerID string, unitID int, slotIdx int, instanceID int64) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	unit := s.getUnitByIDLocked(unitID)
	if unit == nil || unit.OwnerID != playerID || unit.HP <= 0 {
		return
	}

	// slotIdx must be within the unit's rank-granted inventory.
	if slotIdx < 0 || slotIdx >= unit.InventorySize {
		return
	}
	if len(unit.Equipped) <= slotIdx {
		return
	}
	if unit.Equipped[slotIdx] != nil {
		// Slot already occupied — no implicit swap.
		return
	}

	// Find the item in the vault.
	var vaultEntry *VaultItem
	for _, vi := range player.Vault {
		if vi.InstanceID == instanceID {
			vaultEntry = vi
			break
		}
	}
	if vaultEntry == nil {
		return
	}

	def, ok := s.itemCatalog[vaultEntry.ItemID]
	if !ok {
		return
	}

	// AllowedUnitTypes restriction.
	if len(def.AllowedUnitTypes) > 0 {
		allowed := false
		for _, t := range def.AllowedUnitTypes {
			if t == unit.UnitType {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	// Pull item from vault (decrements stacks or removes entry).
	removed, ok := s.removeItemFromVaultByInstanceLocked(player, instanceID)
	if !ok {
		return
	}

	unit.Equipped[slotIdx] = &EquippedItem{
		InstanceID: removed.InstanceID,
		ItemID:     removed.ItemID,
		Stacks:     removed.Stacks,
	}
	s.recomputeUnitEquipmentBonusLocked(unit)
}

// handleUnequipItemLocked validates and moves an equipped item back into the
// player's vault. Validation failures are silent no-ops. Must be called under s.mu.
func (s *GameState) handleUnequipItemLocked(playerID string, unitID int, slotIdx int) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	unit := s.getUnitByIDLocked(unitID)
	if unit == nil || unit.OwnerID != playerID {
		return
	}
	if slotIdx < 0 || slotIdx >= unit.InventorySize {
		return
	}
	if len(unit.Equipped) <= slotIdx || unit.Equipped[slotIdx] == nil {
		return
	}

	slot := unit.Equipped[slotIdx]
	def, ok := s.itemCatalog[slot.ItemID]
	if !ok {
		// Unknown item — remove from slot silently, no vault add.
		unit.Equipped[slotIdx] = nil
		s.recomputeUnitEquipmentBonusLocked(unit)
		return
	}

	// Verify vault has room to receive the item (same logic as addItemToVaultLocked).
	if def.Kind == ItemKindConsumable {
		maxSt := itemMaxStacks(def)
		canStack := false
		for _, vi := range player.Vault {
			if vi.ItemID == def.ID && vi.Stacks < maxSt {
				canStack = true
				break
			}
		}
		if !canStack {
			capacity := s.vaultCapacityForPlayerLocked(playerID)
			if len(player.Vault) >= capacity {
				return
			}
		}
	} else {
		capacity := s.vaultCapacityForPlayerLocked(playerID)
		if len(player.Vault) >= capacity {
			return
		}
	}

	// Move item from slot back to vault.
	unit.Equipped[slotIdx] = nil
	player.Vault = append(player.Vault, &VaultItem{
		InstanceID: slot.InstanceID,
		ItemID:     slot.ItemID,
		Stacks:     slot.Stacks,
	})
	s.recomputeUnitEquipmentBonusLocked(unit)
}

// handleUseConsumableLocked validates and applies a consumable item in a unit's
// equipment slot. Decrements stacks; clears the slot when the last stack is
// consumed. Validation failures are silent no-ops. Must be called under s.mu.
func (s *GameState) handleUseConsumableLocked(playerID string, unitID int, slotIdx int) {
	unit := s.getUnitByIDLocked(unitID)
	if unit == nil || unit.OwnerID != playerID || unit.HP <= 0 {
		return
	}
	if slotIdx < 0 || slotIdx >= unit.InventorySize {
		return
	}
	if len(unit.Equipped) <= slotIdx || unit.Equipped[slotIdx] == nil {
		return
	}

	slot := unit.Equipped[slotIdx]
	def, ok := s.itemCatalog[slot.ItemID]
	if !ok {
		return
	}
	if def.Kind != ItemKindConsumable {
		return
	}

	s.applyConsumableEffectLocked(unit, def)

	slot.Stacks--
	if slot.Stacks <= 0 {
		unit.Equipped[slotIdx] = nil
	}
}

// ─── Public entry points ─────────────────────────────────────────────────────

// PurchaseItem is the public entry point for item purchases from a marketplace.
// Acquires s.mu and delegates to handlePurchaseItemLocked.
func (s *GameState) PurchaseItem(playerID, buildingID, itemID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePurchaseItemLocked(playerID, buildingID, itemID)
}

// EquipItem is the public entry point for equipping an item from the vault onto
// a unit slot. Acquires s.mu and delegates to handleEquipItemLocked.
func (s *GameState) EquipItem(playerID string, unitID int, slotIdx int, instanceID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleEquipItemLocked(playerID, unitID, slotIdx, instanceID)
}

// UnequipItem is the public entry point for returning an equipped item to the
// vault. Acquires s.mu and delegates to handleUnequipItemLocked.
func (s *GameState) UnequipItem(playerID string, unitID int, slotIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleUnequipItemLocked(playerID, unitID, slotIdx)
}

// UseConsumable is the public entry point for using a consumable item in an
// equipment slot. Acquires s.mu and delegates to handleUseConsumableLocked.
func (s *GameState) UseConsumable(playerID string, unitID int, slotIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleUseConsumableLocked(playerID, unitID, slotIdx)
}

// TransferItem is the public entry point for moving an equipped item from one
// unit's slot to another unit's slot (or a different slot on the same unit).
// Acquires s.mu and delegates to handleTransferItemLocked.
func (s *GameState) TransferItem(playerID string, fromUnitID, fromSlotIdx, toUnitID, toSlotIdx int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleTransferItemLocked(playerID, fromUnitID, fromSlotIdx, toUnitID, toSlotIdx)
}

// handleTransferItemLocked validates and moves an equipped item from one unit's
// slot to another (or a different slot on the same unit). Validation failures
// are silent no-ops. Must be called under s.mu write lock.
func (s *GameState) handleTransferItemLocked(playerID string, fromUnitID, fromSlotIdx, toUnitID, toSlotIdx int) {
	fromUnit := s.getUnitByIDLocked(fromUnitID)
	if fromUnit == nil || fromUnit.HP <= 0 || fromUnit.OwnerID != playerID {
		return
	}

	toUnit := s.getUnitByIDLocked(toUnitID)
	if toUnit == nil || toUnit.HP <= 0 || toUnit.OwnerID != playerID {
		return
	}

	// Validate source slot index and occupancy.
	if fromSlotIdx < 0 || fromSlotIdx >= fromUnit.InventorySize {
		return
	}
	if len(fromUnit.Equipped) <= fromSlotIdx || fromUnit.Equipped[fromSlotIdx] == nil {
		return
	}

	// Validate destination slot index and vacancy.
	if toSlotIdx < 0 || toSlotIdx >= toUnit.InventorySize {
		return
	}
	if len(toUnit.Equipped) <= toSlotIdx {
		return
	}
	if toUnit.Equipped[toSlotIdx] != nil {
		// Destination occupied — no implicit swap (Phase 2).
		return
	}

	// AllowedUnitTypes restriction on the item def.
	item := fromUnit.Equipped[fromSlotIdx]
	def, ok := s.itemCatalog[item.ItemID]
	if !ok {
		return
	}
	if len(def.AllowedUnitTypes) > 0 {
		allowed := false
		for _, t := range def.AllowedUnitTypes {
			if t == toUnit.UnitType {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	// Move the item pointer atomically.
	toUnit.Equipped[toSlotIdx] = fromUnit.Equipped[fromSlotIdx]
	fromUnit.Equipped[fromSlotIdx] = nil

	s.recomputeUnitEquipmentBonusLocked(fromUnit)
	// Only recompute toUnit separately when it is a different unit; if
	// fromUnit == toUnit the first call already covers both slots.
	if fromUnit != toUnit {
		s.recomputeUnitEquipmentBonusLocked(toUnit)
	}
}

// ─── Snapshot helpers ────────────────────────────────────────────────────────

// playerVaultSnapshotsLocked builds the []protocol.VaultItemSnapshot for a
// player's current vault contents. Must be called under s.mu (read lock sufficient).
func (s *GameState) playerVaultSnapshotsLocked(playerID string) []protocol.VaultItemSnapshot {
	player, ok := s.Players[playerID]
	out := make([]protocol.VaultItemSnapshot, 0)
	if !ok {
		return out
	}
	for _, vi := range player.Vault {
		out = append(out, protocol.VaultItemSnapshot{
			InstanceID: vi.InstanceID,
			ItemID:     vi.ItemID,
			Stacks:     vi.Stacks,
		})
	}
	return out
}

// unitInventorySnapshotLocked builds the *protocol.InventorySnapshot for a
// unit. Returns nil when the unit has no inventory (InventorySize == 0).
// Must be called under s.mu (read lock sufficient).
func (s *GameState) unitInventorySnapshotLocked(unit *Unit) *protocol.InventorySnapshot {
	if unit.InventorySize == 0 {
		return nil
	}
	slots := make([]*protocol.ItemSnapshot, unit.InventorySize)
	for i := 0; i < unit.InventorySize; i++ {
		if i >= len(unit.Equipped) || unit.Equipped[i] == nil {
			slots[i] = nil
			continue
		}
		eq := unit.Equipped[i]
		slots[i] = &protocol.ItemSnapshot{
			InstanceID: eq.InstanceID,
			ItemID:     eq.ItemID,
			Stacks:     eq.Stacks,
		}
	}
	return &protocol.InventorySnapshot{
		Size:  unit.InventorySize,
		Slots: slots,
	}
}
