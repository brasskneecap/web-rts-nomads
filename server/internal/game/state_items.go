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

// EquipmentProc is one equipped on-hit proc, resolved at equip time so the
// per-hit path never re-reads catalogs: Chance is the trigger's roll (against
// the seeded perk RNG) and Params is the fully-resolved effect payload.
type EquipmentProc struct {
	Chance float64
	Params ProcEffectParams
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
	// DodgeChance / BlockChance sum the equipped items' additive evasion
	// contributions (see ItemModifiers). Read by evasionForUnit.
	DodgeChance float64
	BlockChance float64
	// OnHitElemental sums per-element flat damage applied as a SEPARATE typed
	// instance on each landed basic attack. nil when no equipped item grants any.
	OnHitElemental map[DamageType]int
	// OnHitProcs is one entry per equipped item that defines an onHitProc.
	OnHitProcs []EquipmentProc
	// OnStruckProcs is one entry per equipped item that defines an
	// onStruckProc — rolled when a basic attack lands ON the wearer.
	OnStruckProcs []EquipmentProc
}

// ─── Capacity / presence helpers ─────────────────────────────────────────────

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

// addItemToVaultLocked adds one unit of def to the player's vault. The vault is
// unbounded, so this always succeeds; the bool return is retained for callers
// that branch on it (e.g. loot pickup) and is always true.
//
// Stacking rules:
//   - Consumable: if an existing entry with the same ItemID has room to stack,
//     increment Stacks. Otherwise a new entry is created.
//   - Equipment: always creates a new VaultItem with a unique InstanceID.
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

// vaultItemCountLocked returns the total number of units of itemID held in the
// player's vault (summing stacks). Must be called under s.mu.
func vaultItemCountLocked(player *Player, itemID string) int {
	n := 0
	for _, vi := range player.Vault {
		if vi.ItemID == itemID {
			n += vi.Stacks
		}
	}
	return n
}

// removeOneItemFromVaultByItemIDLocked removes a single unit of itemID from the
// player's vault: decrements a stack if one has Stacks>1, else drops the entry.
// Returns false if no matching entry exists. Must be called under s.mu.
func (s *GameState) removeOneItemFromVaultByItemIDLocked(player *Player, itemID string) bool {
	for i, vi := range player.Vault {
		if vi.ItemID != itemID {
			continue
		}
		if vi.Stacks > 1 {
			vi.Stacks--
			return true
		}
		player.Vault = append(player.Vault[:i], player.Vault[i+1:]...)
		return true
	}
	return false
}

// ─── Inventory size / bonus recomputation ────────────────────────────────────

// setInventorySizeForRankLocked sets unit.InventorySize based on rank and
// grows unit.Equipped to match if it has grown. Never shrinks (rank can't
// decrease). Combat units carry 1 slot from the moment they spawn (base and
// bronze), 2 at silver, 3 at gold. Workers are non-combatants — they never
// gain XP or rank (see unitCanGainXPLocked) and carry no inventory, which
// also keeps them out of the vault's unit list. Must be called under s.mu.
func (s *GameState) setInventorySizeForRankLocked(unit *Unit) {
	if unit.UnitType == "worker" {
		return
	}
	var size int
	switch unit.Rank {
	case unitRankBase, unitRankBronze:
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
// HealthRegen used to need a special case here: it had no Base* counterpart, so
// this function applied the old→new bonus delta straight onto
// unit.HealthRegenPerSecond. That is gone — BaseHealthRegenPerSecond now exists,
// and applyRankModifiersLocked recomputes regen from base × rank + equipment like
// every other stat. Re-introducing a delta here would double-count it.
func (s *GameState) recomputeUnitEquipmentBonusLocked(unit *Unit) {
	unit.EquipmentBonus = UnitEquipmentBonus{}
	for _, slot := range unit.Equipped {
		if slot == nil {
			continue
		}
		def, ok := s.itemCatalog[slot.ItemID]
		if !ok {
			continue
		}
		if def.Modifiers != nil {
			unit.EquipmentBonus.Damage += def.Modifiers.Damage
			unit.EquipmentBonus.HP += def.Modifiers.HP
			unit.EquipmentBonus.Armor += def.Modifiers.Armor
			unit.EquipmentBonus.AttackSpeed += def.Modifiers.AttackSpeed
			unit.EquipmentBonus.MoveSpeed += def.Modifiers.MoveSpeed
			unit.EquipmentBonus.HealthRegen += def.Modifiers.HealthRegen
			unit.EquipmentBonus.MaxShield += def.Modifiers.MaxShield
			unit.EquipmentBonus.DodgeChance += def.Modifiers.DodgeChance
			unit.EquipmentBonus.BlockChance += def.Modifiers.BlockChance
		}
		for _, e := range def.OnHitElemental {
			if e.Amount == 0 {
				continue
			}
			if unit.EquipmentBonus.OnHitElemental == nil {
				unit.EquipmentBonus.OnHitElemental = make(map[DamageType]int)
			}
			unit.EquipmentBonus.OnHitElemental[e.Type.OrPhysical()] += e.Amount
		}
		if p := def.OnHitProc; p != nil {
			if params, ok := p.ResolveParams(); ok {
				unit.EquipmentBonus.OnHitProcs = append(unit.EquipmentBonus.OnHitProcs, EquipmentProc{Chance: p.Chance, Params: params})
			}
		}
		if p := def.OnStruckProc; p != nil {
			if params, ok := p.ResolveParams(); ok {
				unit.EquipmentBonus.OnStruckProcs = append(unit.EquipmentBonus.OnStruckProcs, EquipmentProc{Chance: p.Chance, Params: params})
			}
		}
	}

	// Rebake derived stats (Damage, MaxHP, Armor, AttackSpeed, MoveSpeed,
	// AttackRange, HealthRegenPerSecond). preserveHealthPercent=true so equipping
	// an HP item preserves the unit's HP fraction rather than capping at old MaxHP.
	s.applyRankModifiersLocked(unit, true)
}

// ─── Consumable application ───────────────────────────────────────────────────

// applyConsumableToUnitLocked applies one consumable effect instance of the
// given type and amount to unit. Amount is pre-computed by the caller (the
// AoE use handler divides the def amount across targets when Split is on).
// Applying is unconditional — a full-HP unit still "receives" a heal (by
// spec, the item is consumed regardless). Must be called under s.mu.
func (s *GameState) applyConsumableToUnitLocked(unit *Unit, effectType string, amount int) {
	switch effectType {
	case "heal":
		healed := unit.HP + amount
		if healed > unit.MaxHP {
			healed = unit.MaxHP
		}
		unit.HP = healed
	case "grant_xp":
		// Runs the full XP pipeline: rank-up thresholds, path/perk/ability
		// assignment, and stat rebake all apply exactly as combat XP would.
		// Ineligible units never reach here — consumableTargetEligibleLocked
		// excludes them from the target set (and therefore from the split).
		s.addUnitXPLocked(unit, amount)
		// Future consumable types: add cases here.
	}
}

// consumableTargetEligibleLocked reports whether unit can actually benefit
// from the given consumable effect type. Ineligible units are excluded from
// the AoE target set entirely, so they neither receive the effect NOR count
// toward the split — an XP potion dropped on two soldiers and a worker
// splits between the two soldiers only. Future types follow the same rule
// (e.g. a mana potion would require MaxMana > 0). Must be called under s.mu.
func (s *GameState) consumableTargetEligibleLocked(unit *Unit, effectType string) bool {
	switch effectType {
	case "grant_xp":
		return s.unitCanGainXPLocked(unit)
	default:
		// "heal" and unknown future types: any allied living unit qualifies.
		return true
	}
}

// handleUseItemAtLocked uses a consumable from the player's vault as a
// ground-targeted AoE at world point (x, y): every allied living field unit
// within the def's Range that can benefit from the effect (see
// consumableTargetEligibleLocked) is affected. With Split on (the default)
// the def's Amount is divided evenly across the eligible units hit (integer
// division); with Split off every unit hit receives the full Amount.
// Clicking with no eligible target in range is a no-op and does NOT consume
// the item. Validation failures are silent no-ops. Must be called under s.mu.
func (s *GameState) handleUseItemAtLocked(playerID string, instanceID int64, x, y float64) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	// Resolve the vault entry and its def without removing it yet — the item
	// is only consumed when at least one unit is actually hit.
	var itemID string
	for _, vi := range player.Vault {
		if vi.InstanceID == instanceID {
			itemID = vi.ItemID
			break
		}
	}
	if itemID == "" {
		return
	}
	def, ok := s.itemCatalog[itemID]
	if !ok || def.Kind != ItemKindConsumable || def.Consumable == nil {
		return
	}

	radius := def.Consumable.EffectiveRange()
	radiusSq := radius * radius
	targets := make([]*Unit, 0, 8)
	for _, u := range s.Units {
		if u == nil || u.OwnerID != playerID || u.HP <= 0 || !u.Visible || u.MiningInside {
			continue
		}
		if !s.consumableTargetEligibleLocked(u, def.Consumable.Type) {
			continue
		}
		if distanceSquared(u.X, u.Y, x, y) <= radiusSq {
			targets = append(targets, u)
		}
	}
	if len(targets) == 0 {
		return
	}

	amount := def.Consumable.Amount
	if def.Consumable.SplitEnabled() {
		amount /= len(targets)
	}
	for _, u := range targets {
		s.applyConsumableToUnitLocked(u, def.Consumable.Type, amount)
	}

	s.removeItemFromVaultByInstanceLocked(player, instanceID)
}

// handleUseItemOnUnitLocked uses a consumable from the player's vault directly
// on a single unit (the Vault "Items" drag-onto-a-unit-card path). Unlike the
// ground-targeted AoE, there is no split: the unit receives the def's full
// Amount. Validation failures — unknown/foreign/dead unit, non-consumable item,
// or a unit that cannot benefit from the effect (see
// consumableTargetEligibleLocked) — are silent no-ops that do NOT consume the
// item. Must be called under s.mu.
func (s *GameState) handleUseItemOnUnitLocked(playerID string, instanceID int64, unitID int) {
	player, ok := s.Players[playerID]
	if !ok {
		return
	}

	unit := s.getUnitByIDLocked(unitID)
	if unit == nil || unit.OwnerID != playerID || unit.HP <= 0 || !unit.Visible || unit.MiningInside {
		return
	}

	// Resolve the vault entry + def without consuming yet — the item is only
	// spent when the effect actually applies.
	var itemID string
	for _, vi := range player.Vault {
		if vi.InstanceID == instanceID {
			itemID = vi.ItemID
			break
		}
	}
	if itemID == "" {
		return
	}
	def, ok := s.itemCatalog[itemID]
	if !ok || def.Kind != ItemKindConsumable || def.Consumable == nil {
		return
	}

	// The unit must be able to benefit (e.g. an XP potion on a max-rank or
	// non-XP unit is a no-op and is not consumed) — same rule the AoE path uses.
	if !s.consumableTargetEligibleLocked(unit, def.Consumable.Type) {
		return
	}

	s.applyConsumableToUnitLocked(unit, def.Consumable.Type, def.Consumable.Amount)
	s.removeItemFromVaultByInstanceLocked(player, instanceID)
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

	// Building must exist, be visible, have item-purchase capability, and
	// not be under construction.
	building := s.getBuildingByIDLocked(buildingID)
	if building == nil || !building.Visible {
		return
	}
	if getMetadataBool(building.Metadata, "underConstruction") {
		return
	}
	if !hasItemPurchaseCapability(building) {
		return
	}

	// Ownership / discovery / lock gates, and select which inventory this
	// purchase draws from:
	//   - Neutral shops are PER-PLAYER: purchaser must have discovered the
	//     building (FOW KnownBuildings cache) AND it must not be guard-locked;
	//     the stock comes from the buyer's own Player.NeutralShopInventories.
	//   - Player-owned shops share BuildingTile.ShopInventory: purchaser must be
	//     the owner.
	if building.OwnerID == nil {
		return
	}
	var inv []protocol.ShopStockEntry
	if *building.OwnerID == neutralPlayerID {
		fow := s.FOW[playerID]
		if fow == nil {
			return
		}
		if _, discovered := fow.KnownBuildings[building.ID]; !discovered {
			return
		}
		if s.shopLockedLocked(building) {
			return
		}
		inv = player.NeutralShopInventories[building.ID]
	} else if *building.OwnerID == playerID {
		inv = building.ShopInventory
	} else {
		return
	}

	// Item must be in the resolved inventory with quantity remaining. An entry
	// with quantity 0 stays in the list so the client can render it greyed-out,
	// but the purchase is rejected.
	stockIdx := -1
	for i, entry := range inv {
		if entry.ItemID == itemID {
			stockIdx = i
			break
		}
	}
	if stockIdx < 0 || inv[stockIdx].Quantity <= 0 {
		return
	}

	// Item must exist in catalog.
	def, ok := s.itemCatalog[itemID]
	if !ok {
		return
	}

	// Afford check.
	if player.Resources["gold"] < def.CostGold {
		return
	}

	player.Resources["gold"] -= def.CostGold
	s.addItemToVaultLocked(player, def)
	// Decrement the resolved inventory's stock for this item. The entry stays in
	// the list at quantity 0 so the client can render it greyed-out rather than
	// removing the slot entirely. `inv` aliases either the per-player slice
	// (map value) or the building's slice, so the element mutation persists.
	inv[stockIdx].Quantity--
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
	// Consumables are not equippable: they live in the player's item bar and
	// are used as a ground-targeted AoE (handleUseItemAtLocked). Unit slots
	// hold equipment only.
	if def.Kind == ItemKindConsumable {
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
	if _, ok := s.itemCatalog[slot.ItemID]; !ok {
		// Unknown item — remove from slot silently, no vault add.
		unit.Equipped[slotIdx] = nil
		s.recomputeUnitEquipmentBonusLocked(unit)
		return
	}

	// The vault is unbounded, so the item can always be returned — no room check.

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

	if def.Consumable == nil {
		return
	}
	// Legacy unit-slot use path: applies the full amount to the holder.
	// Unreachable in practice since consumables can no longer be equipped
	// (see handleEquipItemLocked) — consumables are used via
	// handleUseItemAtLocked's ground-targeted AoE instead.
	s.applyConsumableToUnitLocked(unit, def.Consumable.Type, def.Consumable.Amount)

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

// UseItemAt is the public entry point for using a consumable from the vault
// as a ground-targeted AoE at world point (x, y). Acquires s.mu and delegates
// to handleUseItemAtLocked.
func (s *GameState) UseItemAt(playerID string, instanceID int64, x, y float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleUseItemAtLocked(playerID, instanceID, x, y)
}

// UseItemOnUnit is the public entry point for using a consumable from the vault
// directly on a single unit (the Vault "Items" drag-onto-a-unit-card path).
// Acquires s.mu and delegates to handleUseItemOnUnitLocked.
func (s *GameState) UseItemOnUnit(playerID string, instanceID int64, unitID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleUseItemOnUnitLocked(playerID, instanceID, unitID)
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

// ─── Resource grant helpers ───────────────────────────────────────────────────

// grantResourceToPlayerLocked adds an amount of a single resource type
// to player.Resources. Centralizes the mutation so future hooks
// (telemetry, achievements, resource caps) live in one place.
//
// amount <= 0 is a no-op. key need not pre-exist in the map.
//
// Must be called under s.mu write lock.
func (s *GameState) grantResourceToPlayerLocked(player *Player, key string, amount int) {
	if player == nil || amount <= 0 || key == "" {
		return
	}
	if player.Resources == nil {
		player.Resources = map[string]int{}
	}
	player.Resources[key] += amount
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
