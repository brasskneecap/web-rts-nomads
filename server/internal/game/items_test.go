package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// newItemTestState creates a GameState with a single player "p1" already
// ensured, and a blacksmith building owned by that player.
func newItemTestState(t *testing.T) (*GameState, string) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	const playerID = "p1"
	s.EnsurePlayer(playerID)

	// Inject a blacksmith building directly into the state.
	s.mu.Lock()
	bid := "bs-1"
	owner := playerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:           bid,
		BuildingType: "blacksmith",
		Width:        2,
		Height:       2,
		Visible:      true,
		OwnerID:      &owner,
		Capabilities: []string{"item-purchase"},
		Metadata:     map[string]interface{}{},
	})
	// Re-index buildingsByID.
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if s.buildingsByID == nil {
			s.buildingsByID = map[string]*protocol.BuildingTile{}
		}
		s.buildingsByID[b.ID] = b
	}
	s.mu.Unlock()

	return s, playerID
}

// spawnBronzeUnit spawns a soldier for playerID and advances it to bronze rank.
func spawnBronzeUnit(t *testing.T, s *GameState, playerID string) *Unit {
	t.Helper()
	s.mu.Lock()
	player := s.Players[playerID]
	unit := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 400, Y: 400})
	// Force-promote to bronze.
	unit.Rank = unitRankBronze
	unit.ProgressionPath = unitPathVanguard
	s.setInventorySizeForRankLocked(unit)
	unit.Equipped = make([]*EquippedItem, unit.InventorySize)
	s.applyRankModifiersLocked(unit, false)
	s.mu.Unlock()
	return unit
}

// ─── Catalog loading ─────────────────────────────────────────────────────────

// TestItemCatalog_AllTenItemsLoaded verifies the embedded catalog has all 10
// items and that both equipment and consumable kinds are represented.
func TestItemCatalog_AllTenItemsLoaded(t *testing.T) {
	defs := ListItemDefs()
	if len(defs) != 10 {
		t.Fatalf("expected 10 item defs, got %d", len(defs))
	}

	byID := make(map[string]*ItemDef, len(defs))
	for _, d := range defs {
		byID[d.ID] = d
	}

	weapons := []string{
		"weapon_common_sword", "cimitar", "flame_sword",
		"ice_sword", "shadow_blade",
	}
	for _, id := range weapons {
		def, ok := byID[id]
		if !ok {
			t.Errorf("missing weapon %q", id)
			continue
		}
		if def.Kind != ItemKindEquipment {
			t.Errorf("%s: expected kind equipment, got %q", id, def.Kind)
		}
		if def.Modifiers == nil || def.Modifiers.Damage <= 0 {
			t.Errorf("%s: expected positive damage modifier", id)
		}
	}

	potions := []string{
		"potion_common_heal", "potion_uncommon_heal", "potion_rare_heal",
		"potion_epic_heal", "potion_legendary_heal",
	}
	for _, id := range potions {
		def, ok := byID[id]
		if !ok {
			t.Errorf("missing potion %q", id)
			continue
		}
		if def.Kind != ItemKindConsumable {
			t.Errorf("%s: expected kind consumable, got %q", id, def.Kind)
		}
		if def.Consumable == nil || def.Consumable.Type != "heal" {
			t.Errorf("%s: expected heal consumable", id)
		}
		if def.MaxStacks != 99 {
			t.Errorf("%s: expected MaxStacks=99, got %d", id, def.MaxStacks)
		}
	}
}

// ─── Vault capacity ───────────────────────────────────────────────────────────

// TestVaultCapacity_TierGating verifies the vault capacity steps match tier.
func TestVaultCapacity_TierGating(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayer("p1")

	s.mu.RLock()
	cap0 := s.vaultCapacityForPlayerLocked("p1")
	s.mu.RUnlock()

	// Default map gives tier-1 townhall; expect capacity 5.
	if cap0 != 5 {
		t.Errorf("expected capacity 5 for tier-1 TH, got %d", cap0)
	}

	// Forcibly set tier to 2.
	s.mu.Lock()
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType == "townhall" && b.OwnerID != nil && *b.OwnerID == "p1" {
			b.Metadata["tier"] = float64(2)
		}
	}
	cap2 := s.vaultCapacityForPlayerLocked("p1")
	s.mu.Unlock()

	if cap2 != 10 {
		t.Errorf("expected capacity 10 for tier-2 TH, got %d", cap2)
	}
}

// ─── Purchase item ─────────────────────────────────────────────────────────────

// TestPurchaseItem_EquipmentAddsToVault verifies buying a sword adds it to the
// player's vault and deducts gold.
func TestPurchaseItem_EquipmentAddsToVault(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.mu.RLock()
	goldBefore := s.Players[playerID].Resources["gold"]
	s.mu.RUnlock()

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")

	s.mu.RLock()
	defer s.mu.RUnlock()
	player := s.Players[playerID]
	if len(player.Vault) != 1 {
		t.Fatalf("expected 1 vault item, got %d", len(player.Vault))
	}
	if player.Vault[0].ItemID != "weapon_common_sword" {
		t.Errorf("expected weapon_common_sword, got %q", player.Vault[0].ItemID)
	}
	if player.Vault[0].Stacks != 1 {
		t.Errorf("expected stacks=1, got %d", player.Vault[0].Stacks)
	}
	goldAfter := player.Resources["gold"]
	if goldAfter != goldBefore-100 {
		t.Errorf("expected gold deducted by 100, before=%d after=%d", goldBefore, goldAfter)
	}
}

// TestPurchaseItem_ConsumableStacks verifies buying the same potion twice
// results in one vault entry with Stacks=2.
func TestPurchaseItem_ConsumableStacks(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.PurchaseItem(playerID, "bs-1", "potion_common_heal")
	s.PurchaseItem(playerID, "bs-1", "potion_common_heal")

	s.mu.RLock()
	defer s.mu.RUnlock()
	player := s.Players[playerID]
	if len(player.Vault) != 1 {
		t.Fatalf("expected 1 vault entry (stacked), got %d", len(player.Vault))
	}
	if player.Vault[0].Stacks != 2 {
		t.Errorf("expected stacks=2, got %d", player.Vault[0].Stacks)
	}
}

// TestPurchaseItem_InsufficientGold_NoOp verifies the purchase silently does
// nothing when the player can't afford the item.
func TestPurchaseItem_InsufficientGold_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)

	// Drain player gold.
	s.mu.Lock()
	s.Players[playerID].Resources["gold"] = 0
	s.mu.Unlock()

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Players[playerID].Vault) != 0 {
		t.Errorf("expected empty vault after failed purchase, got %d items", len(s.Players[playerID].Vault))
	}
}

// TestPurchaseItem_VaultAtCapacity_NoOp verifies purchases are rejected when
// the vault is full.
func TestPurchaseItem_VaultAtCapacity_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)

	// Fill vault to capacity (tier-1 = 5 slots).
	s.mu.Lock()
	player := s.Players[playerID]
	for i := 0; i < 5; i++ {
		s.nextItemInstanceID++
		player.Vault = append(player.Vault, &VaultItem{
			InstanceID: s.nextItemInstanceID,
			ItemID:     "weapon_common_sword",
			Stacks:     1,
		})
	}
	s.mu.Unlock()

	goldBefore := player.Resources["gold"]
	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(player.Vault) != 5 {
		t.Errorf("expected vault still at 5, got %d", len(player.Vault))
	}
	if player.Resources["gold"] != goldBefore {
		t.Errorf("expected no gold deducted, gold changed from %d to %d", goldBefore, player.Resources["gold"])
	}
}

// TestPurchaseItem_TownhallDestroyed_NoOp verifies purchases fail when the TH
// is gone (tier == 0), even if vault has space.
func TestPurchaseItem_TownhallDestroyed_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)

	// Destroy the townhall by making it invisible.
	s.mu.Lock()
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType == "townhall" && b.OwnerID != nil && *b.OwnerID == playerID {
			b.Visible = false
		}
	}
	s.mu.Unlock()

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Players[playerID].Vault) != 0 {
		t.Errorf("expected empty vault when TH destroyed, got %d items", len(s.Players[playerID].Vault))
	}
}

// ─── Equip / Unequip ─────────────────────────────────────────────────────────

// TestEquipItem_EquipsSwordAndAppliesBonus verifies that equipping a sword adds
// the damage bonus to the unit's effective Damage stat.
func TestEquipItem_EquipsSwordAndAppliesBonus(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")

	s.mu.RLock()
	player := s.Players[playerID]
	var instanceID int64
	if len(player.Vault) > 0 {
		instanceID = player.Vault[0].InstanceID
	}
	damageBefore := unit.Damage
	s.mu.RUnlock()

	s.EquipItem(playerID, unit.ID, 0, instanceID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(player.Vault) != 0 {
		t.Errorf("expected vault empty after equip, got %d entries", len(player.Vault))
	}
	if unit.Equipped[0] == nil {
		t.Fatal("expected slot 0 to be occupied")
	}
	if unit.Equipped[0].ItemID != "weapon_common_sword" {
		t.Errorf("expected weapon_common_sword in slot, got %q", unit.Equipped[0].ItemID)
	}
	if unit.Damage != damageBefore+5 {
		t.Errorf("expected damage +5, before=%d after=%d", damageBefore, unit.Damage)
	}
}

// TestEquipItem_SlotOccupied_NoOp verifies that equipping onto an already
// occupied slot is rejected silently.
func TestEquipItem_SlotOccupied_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid1 := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid1)

	// Buy a second sword and try to equip to same slot.
	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid2 := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid2)

	s.mu.RLock()
	defer s.mu.RUnlock()
	// First sword still in slot 0.
	if unit.Equipped[0].InstanceID != iid1 {
		t.Errorf("slot 0 should still hold iid1=%d, got iid=%d", iid1, unit.Equipped[0].InstanceID)
	}
	// Second sword still in vault.
	if len(s.Players[playerID].Vault) != 1 {
		t.Errorf("second sword should remain in vault, got %d entries", len(s.Players[playerID].Vault))
	}
}

// TestUnequipItem_ReturnsItemToVault verifies that unequipping a sword returns
// it to the vault and removes the damage bonus.
func TestUnequipItem_ReturnsItemToVault(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	s.mu.RLock()
	damageBonused := unit.Damage
	s.mu.RUnlock()

	s.UnequipItem(playerID, unit.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unit.Equipped[0] != nil {
		t.Error("expected slot 0 to be nil after unequip")
	}
	if len(s.Players[playerID].Vault) != 1 {
		t.Errorf("expected item back in vault, got %d entries", len(s.Players[playerID].Vault))
	}
	if unit.Damage != damageBonused-5 {
		t.Errorf("expected damage -5 after unequip, bonused=%d after=%d", damageBonused, unit.Damage)
	}
}

// ─── Consumable use ───────────────────────────────────────────────────────────

// TestUseConsumable_HealRestoresHP verifies the heal effect adds HP up to MaxHP.
func TestUseConsumable_HealRestoresHP(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.mu.Lock()
	unit.HP = unit.MaxHP/2
	s.mu.Unlock()

	s.PurchaseItem(playerID, "bs-1", "potion_common_heal")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	s.mu.RLock()
	hpBefore := unit.HP
	s.mu.RUnlock()

	s.UseConsumable(playerID, unit.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	expected := hpBefore + 50
	if expected > unit.MaxHP {
		expected = unit.MaxHP
	}
	if unit.HP != expected {
		t.Errorf("expected HP %d after heal, got %d", expected, unit.HP)
	}
}

// TestUseConsumable_ClearsSlotWhenLastStack verifies that using the last stack
// of a consumable nils out the slot.
func TestUseConsumable_ClearsSlotWhenLastStack(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "potion_common_heal")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	s.UseConsumable(playerID, unit.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unit.Equipped[0] != nil {
		t.Error("expected slot 0 nil after using last potion stack")
	}
}

// TestUseConsumable_FullHP_StillConsumes verifies the potion is consumed even
// when the unit is already at full HP.
func TestUseConsumable_FullHP_StillConsumes(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	// Force unit to full HP for this test.
	s.mu.Lock()
	unit.HP = unit.MaxHP
	s.mu.Unlock()

	s.PurchaseItem(playerID, "bs-1", "potion_common_heal")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	s.UseConsumable(playerID, unit.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	// Slot should be nil (consumed even at full HP).
	if unit.Equipped[0] != nil {
		t.Error("expected slot 0 nil after consuming at full HP")
	}
}

// ─── Inventory size by rank ──────────────────────────────────────────────────

// TestSetInventorySizeForRank_GrowsByRank verifies slot count is 0/1/2/3 for
// base/bronze/silver/gold respectively.
func TestSetInventorySizeForRank_GrowsByRank(t *testing.T) {
	cases := []struct {
		rank     string
		wantSize int
	}{
		{unitRankBase, 0},
		{unitRankBronze, 1},
		{unitRankSilver, 2},
		{unitRankGold, 3},
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
	s.EnsurePlayer("p1")

	for _, tc := range cases {
		t.Run(tc.rank, func(t *testing.T) {
			s.mu.Lock()
			player := s.Players["p1"]
			unit := s.spawnPlayerUnitLocked("soldier", "p1", player.Color, protocol.Vec2{X: 100, Y: 100})
			unit.Rank = tc.rank
			unit.ProgressionPath = unitPathVanguard
			unit.Equipped = nil
			s.setInventorySizeForRankLocked(unit)
			gotSize := unit.InventorySize
			gotLen := len(unit.Equipped)
			s.mu.Unlock()

			if gotSize != tc.wantSize {
				t.Errorf("rank=%s: InventorySize want %d got %d", tc.rank, tc.wantSize, gotSize)
			}
			if gotLen != tc.wantSize {
				t.Errorf("rank=%s: len(Equipped) want %d got %d", tc.rank, tc.wantSize, gotLen)
			}
		})
	}
}

// ─── Snapshot helpers ─────────────────────────────────────────────────────────

// TestPlayerVaultSnapshot_MatchesVaultContents verifies vault snapshot output
// matches the player's vault slice after a purchase.
func TestPlayerVaultSnapshot_MatchesVaultContents(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.PurchaseItem(playerID, "bs-1", "potion_common_heal")

	s.mu.RLock()
	defer s.mu.RUnlock()
	snaps := s.playerVaultSnapshotsLocked(playerID)
	if len(snaps) != 2 {
		t.Fatalf("expected 2 vault snapshots, got %d", len(snaps))
	}
	itemIDs := map[string]bool{}
	for _, sn := range snaps {
		itemIDs[sn.ItemID] = true
	}
	if !itemIDs["weapon_common_sword"] || !itemIDs["potion_common_heal"] {
		t.Errorf("snapshot items mismatch: %v", itemIDs)
	}
}

// TestUnitInventorySnapshot_NilForBaseRankUnit verifies that units at base rank
// (no inventory) return nil from unitInventorySnapshotLocked.
func TestUnitInventorySnapshot_NilForBaseRankUnit(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 3)
	s.EnsurePlayer("p1")

	s.mu.Lock()
	player := s.Players["p1"]
	unit := s.spawnPlayerUnitLocked("soldier", "p1", player.Color, protocol.Vec2{X: 200, Y: 200})
	snap := s.unitInventorySnapshotLocked(unit)
	s.mu.Unlock()

	if snap != nil {
		t.Errorf("expected nil inventory for base-rank unit, got size=%d", snap.Size)
	}
}

// TestUnitInventorySnapshot_SlotsMatchEquipped verifies snapshot slot count and
// content for a bronze-rank unit with one item equipped.
func TestUnitInventorySnapshot_SlotsMatchEquipped(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.unitInventorySnapshotLocked(unit)
	if snap == nil {
		t.Fatal("expected non-nil inventory snapshot for bronze unit")
	}
	if snap.Size != 1 {
		t.Errorf("expected size 1, got %d", snap.Size)
	}
	if len(snap.Slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(snap.Slots))
	}
	if snap.Slots[0] == nil {
		t.Fatal("expected slot 0 to be populated")
	}
	if snap.Slots[0].ItemID != "weapon_common_sword" {
		t.Errorf("expected weapon_common_sword in slot snapshot, got %q", snap.Slots[0].ItemID)
	}
}

// ─── Blacksmith building detection ───────────────────────────────────────────

// TestPlayerHasBlacksmith_TrueWhenBuilt verifies detection of an owned blacksmith.
func TestPlayerHasBlacksmith_TrueWhenBuilt(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.mu.RLock()
	has := s.playerHasBlacksmithLocked(playerID)
	s.mu.RUnlock()

	if !has {
		t.Error("expected playerHasBlacksmithLocked to return true")
	}
}

// TestPlayerHasBlacksmith_FalseWhenUnderConstruction verifies under-construction
// buildings are not counted.
func TestPlayerHasBlacksmith_FalseWhenUnderConstruction(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.mu.Lock()
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType == "blacksmith" {
			b.Metadata["underConstruction"] = true
		}
	}
	has := s.playerHasBlacksmithLocked(playerID)
	s.mu.Unlock()

	if has {
		t.Error("expected playerHasBlacksmithLocked to return false for under-construction building")
	}
}

// ─── Transfer item ────────────────────────────────────────────────────────────

// spawnSilverUnit spawns a soldier for playerID and advances it to silver rank
// (2 inventory slots).
func spawnSilverUnit(t *testing.T, s *GameState, playerID string) *Unit {
	t.Helper()
	s.mu.Lock()
	player := s.Players[playerID]
	unit := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 500, Y: 500})
	unit.Rank = unitRankSilver
	unit.ProgressionPath = unitPathVanguard
	s.setInventorySizeForRankLocked(unit)
	unit.Equipped = make([]*EquippedItem, unit.InventorySize)
	s.applyRankModifiersLocked(unit, false)
	s.mu.Unlock()
	return unit
}

// TestTransferItem_HappyPath_MovesItemBetweenUnits verifies that an equipped
// item is atomically moved from unit A slot 0 to unit B slot 0.
func TestTransferItem_HappyPath_MovesItemBetweenUnits(t *testing.T) {
	s, playerID := newItemTestState(t)
	unitA := spawnBronzeUnit(t, s, playerID)
	unitB := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unitA.ID, 0, iid)

	s.mu.RLock()
	damageBefore := unitA.Damage
	s.mu.RUnlock()

	s.TransferItem(playerID, unitA.ID, 0, unitB.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if unitA.Equipped[0] != nil {
		t.Error("expected unitA slot 0 to be empty after transfer")
	}
	if unitB.Equipped[0] == nil {
		t.Fatal("expected unitB slot 0 to be occupied after transfer")
	}
	if unitB.Equipped[0].ItemID != "weapon_common_sword" {
		t.Errorf("expected weapon_common_sword in unitB slot 0, got %q", unitB.Equipped[0].ItemID)
	}
	// unitA loses the bonus; unitB gains it.
	if unitA.Damage != damageBefore-5 {
		t.Errorf("unitA: expected damage restored (-%d), got damage=%d", 5, unitA.Damage)
	}
	if unitB.Damage != damageBefore {
		// unitB started with the same base damage as unitA (same type/rank).
		t.Errorf("unitB: expected damage +5, got damage=%d (damageBefore=%d)", unitB.Damage, damageBefore)
	}
}

// TestTransferItem_SameUnit_DifferentSlot verifies reordering within the same
// unit (silver rank, 2 slots): equip in slot 0, transfer to slot 1.
func TestTransferItem_SameUnit_DifferentSlot(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnSilverUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	s.TransferItem(playerID, unit.ID, 0, unit.ID, 1)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if unit.Equipped[0] != nil {
		t.Error("expected slot 0 to be empty after same-unit transfer")
	}
	if unit.Equipped[1] == nil {
		t.Fatal("expected slot 1 to be occupied after same-unit transfer")
	}
	if unit.Equipped[1].ItemID != "weapon_common_sword" {
		t.Errorf("expected weapon_common_sword in slot 1, got %q", unit.Equipped[1].ItemID)
	}
}

// TestTransferItem_DestOccupied_NoOp verifies that transferring to an occupied
// slot leaves both slots unchanged.
func TestTransferItem_DestOccupied_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)
	unitA := spawnBronzeUnit(t, s, playerID)
	unitB := spawnBronzeUnit(t, s, playerID)

	// Equip a sword on each unit.
	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iidA := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unitA.ID, 0, iidA)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iidB := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unitB.ID, 0, iidB)

	// Try to transfer unitA slot 0 → unitB slot 0 (occupied).
	s.TransferItem(playerID, unitA.ID, 0, unitB.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if unitA.Equipped[0] == nil || unitA.Equipped[0].InstanceID != iidA {
		t.Error("unitA slot 0 should be unchanged after no-op transfer")
	}
	if unitB.Equipped[0] == nil || unitB.Equipped[0].InstanceID != iidB {
		t.Error("unitB slot 0 should be unchanged after no-op transfer")
	}
}

// TestTransferItem_WrongPlayer_NoOp verifies that a transfer is rejected when
// the units belong to a different player.
func TestTransferItem_WrongPlayer_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unit.ID, 0, iid)

	// A second player who does not own the unit tries to transfer.
	// EnsurePlayer must be called before spawnBronzeUnit accesses the player map.
	s.EnsurePlayer("p2")
	// Keep a direct reference so we can assert on Equipped without an index
	// search that might land on a non-inventory unit.
	p2Unit := spawnBronzeUnit(t, s, "p2")

	s.TransferItem("p2", unit.ID, 0, p2Unit.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	// p1's unit should still have the sword; p2's unit should be empty.
	if unit.Equipped[0] == nil || unit.Equipped[0].InstanceID != iid {
		t.Error("p1 unit slot 0 should be unchanged after wrong-player transfer attempt")
	}
	if p2Unit.Equipped[0] != nil {
		t.Error("p2 unit slot 0 should remain empty after rejected transfer")
	}
}

// TestTransferItem_DeadUnit_NoOp verifies that the transfer is rejected when
// the source unit has HP=0.
func TestTransferItem_DeadUnit_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)
	unitA := spawnBronzeUnit(t, s, playerID)
	unitB := spawnBronzeUnit(t, s, playerID)

	s.PurchaseItem(playerID, "bs-1", "weapon_common_sword")
	s.mu.RLock()
	iid := s.Players[playerID].Vault[0].InstanceID
	s.mu.RUnlock()
	s.EquipItem(playerID, unitA.ID, 0, iid)

	// Kill unitA.
	s.mu.Lock()
	unitA.HP = 0
	s.mu.Unlock()

	s.TransferItem(playerID, unitA.ID, 0, unitB.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unitA.Equipped[0] == nil || unitA.Equipped[0].InstanceID != iid {
		t.Error("unitA slot 0 should be unchanged after dead-unit transfer attempt")
	}
	if unitB.Equipped[0] != nil {
		t.Error("unitB slot 0 should remain empty after rejected transfer")
	}
}

// TestTransferItem_SourceSlotEmpty_NoOp verifies that transferring from an
// empty slot is a silent no-op.
func TestTransferItem_SourceSlotEmpty_NoOp(t *testing.T) {
	s, playerID := newItemTestState(t)
	unitA := spawnBronzeUnit(t, s, playerID)
	unitB := spawnBronzeUnit(t, s, playerID)

	// Do NOT equip anything — slot 0 of unitA is empty.
	s.TransferItem(playerID, unitA.ID, 0, unitB.ID, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if unitA.Equipped[0] != nil {
		t.Error("unitA slot 0 should remain nil")
	}
	if unitB.Equipped[0] != nil {
		t.Error("unitB slot 0 should remain nil")
	}
}
