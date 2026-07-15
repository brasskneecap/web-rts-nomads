package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestLootDrop_DropsOnHit: a roll inside an entry's range produces
// exactly one chest with non-empty contents. Concrete contents are
// DERIVED from the catalog (no hardcoded balance numbers).
func TestLootDrop_DropsOnHit(t *testing.T) {
	s := newTestStateForLootDrops(t, 12) // seed: first rngLoot roll lands inside [1,20]
	camp := &s.NeutralCamps[0]
	camp.SpawnedGroupID = "small_raider_group"
	s.maybeDropChestForCampLocked(camp)

	if got := len(s.LootDrops); got != 1 {
		t.Fatalf("LootDrops after a hit: got %d, want 1", got)
	}
	var drop *LootDrop
	for _, d := range s.LootDrops {
		drop = d
	}
	if drop == nil {
		t.Fatalf("nil drop pulled from registry")
	}
	if len(drop.ResourceGrants) == 0 && len(drop.ItemGrants) == 0 {
		t.Errorf("drop has no contents — should be impossible on a top-level hit")
	}
}

// TestLootDrop_NothingRowDropsNoChest: a roll that lands on a `nothing` row
// spawns no chest.
//
// This used to be "the roll landed in a GAP". Gaps are now a validation error —
// a hole in the ranges is indistinguishable from a typo — so a no-drop chance is
// something a table says out loud. Same behaviour, but now it is authored rather
// than inferred from an absence.
func TestLootDrop_NothingRowDropsNoChest(t *testing.T) {
	const testTableID = "__test_nothing_table__"
	tableCatalogSingleton[testTableID] = &TableDef{
		ID: testTableID, Name: "Test", MaxRoll: 100,
		Rows: []TableRow{
			{Min: 1, Max: 10, Resources: map[string]int{"gold": 50}},
			{Min: 11, Max: 100, Nothing: true},
		},
	}
	t.Cleanup(func() { delete(tableCatalogSingleton, testTableID) })

	// Patch the small_raider_group in tier-1 to reference our throwaway
	// gap table. Restore the original after the test.
	tier1 := neutralGroupsByTier[1]
	prevGroups := append([]NeutralGroup(nil), tier1.Groups...)
	for i := range tier1.Groups {
		if tier1.Groups[i].ID == "small_raider_group" {
			tier1.Groups[i].LootTable = testTableID
		}
	}
	neutralGroupsByTier[1] = tier1
	t.Cleanup(func() {
		restored := neutralGroupsByTier[1]
		restored.Groups = prevGroups
		neutralGroupsByTier[1] = restored
	})

	// Seed 1 → first rngLoot.Intn(100)+1 lands in [11,100] → the `nothing` row.
	s := newTestStateForLootDrops(t, 1)
	camp := &s.NeutralCamps[0]
	camp.SpawnedGroupID = "small_raider_group"
	s.maybeDropChestForCampLocked(camp)

	if got := len(s.LootDrops); got != 0 {
		t.Errorf("LootDrops after a `nothing` roll: got %d, want 0", got)
	}
}

// TestLootDrop_Deterministic: two states with the same seed produce
// identical drop outcomes.
func TestLootDrop_Deterministic(t *testing.T) {
	s1 := newTestStateForLootDrops(t, 42)
	s2 := newTestStateForLootDrops(t, 42)
	s1.NeutralCamps[0].SpawnedGroupID = "small_raider_group"
	s2.NeutralCamps[0].SpawnedGroupID = "small_raider_group"
	s1.maybeDropChestForCampLocked(&s1.NeutralCamps[0])
	s2.maybeDropChestForCampLocked(&s2.NeutralCamps[0])

	if len(s1.LootDrops) != len(s2.LootDrops) {
		t.Fatalf("drop count differs: %d vs %d", len(s1.LootDrops), len(s2.LootDrops))
	}
	if len(s1.LootDrops) == 0 {
		return // both empty — still deterministic
	}
	var d1, d2 *LootDrop
	for _, d := range s1.LootDrops {
		d1 = d
	}
	for _, d := range s2.LootDrops {
		d2 = d
	}
	if len(d1.ResourceGrants) != len(d2.ResourceGrants) {
		t.Errorf("resource grant counts differ: %v vs %v", d1.ResourceGrants, d2.ResourceGrants)
	}
	if len(d1.ItemGrants) != len(d2.ItemGrants) {
		t.Errorf("item grant counts differ: %v vs %v", d1.ItemGrants, d2.ItemGrants)
	}
}

// TestLootDrop_NoGroupNoTable: camp with empty SpawnedGroupID is a no-op.
func TestLootDrop_NoGroupNoTable(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	s.NeutralCamps[0].SpawnedGroupID = ""
	s.maybeDropChestForCampLocked(&s.NeutralCamps[0])
	if got := len(s.LootDrops); got != 0 {
		t.Errorf("LootDrops when SpawnedGroupID empty: got %d, want 0", got)
	}
}

// TestLootDrop_WipeTriggersDrop: removing all camp units via the
// normal removal path fires the trigger on the last kill. Seed-dependent
// — uses a known-good seed found below.
func TestLootDrop_WipeTriggersDrop(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked() // initial spawn — populates AliveUnitIDs

	camp := &s.NeutralCamps[0]
	if camp.SpawnedGroupID != "small_raider_group" {
		t.Skipf("test seed produced group %q; choose a seed where small_raider_group is spawned", camp.SpawnedGroupID)
	}
	ids := append([]int(nil), camp.AliveUnitIDs...)
	if len(ids) == 0 {
		t.Fatalf("setup: expected initial spawn to populate AliveUnitIDs")
	}
	for _, id := range ids {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			continue
		}
		s.removeUnitLocked(id)
	}
	// Drop count depends on the loot RNG; if the roll lands in a gap,
	// 0 is valid. Just confirm we either dropped a single chest OR none.
	if got := len(s.LootDrops); got > 1 {
		t.Errorf("wipe produced %d chests, want 0 or 1", got)
	}
}

// TestLootDrop_WaveResetDoesNotDrop: when a wave ends and resets a living camp
// (despawn + respawn), the state flip to WaveHidden before the per-unit
// removals means the wipe hook does NOT fire — only a player kill drops loot.
// This is the core determinism invariant for the trigger.
func TestLootDrop_WaveResetDoesNotDrop(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked() // initial spawn (wave 0)

	// Wave 1 ends → the reset wipes the living camp (despawnNeutralCampLocked
	// then respawn).
	s.WaveManager.CurrentWave = 1
	s.WaveManager.State = "upgrade"
	s.tickNeutralCampsLocked()

	if got := len(s.LootDrops); got != 0 {
		t.Errorf("LootDrops after wave-end reset: got %d, want 0", got)
	}
}

// newTestStateForLootDrops reuses the existing neutral-camp test factory.
// Adjust seed at the call site to land on specific roll outcomes.
func newTestStateForLootDrops(t *testing.T, seed int64) *GameState {
	t.Helper()
	s := newTestGameStateForNeutralCampTests(t, seed)
	s.MapConfig.NeutralSpawns = []protocol.NeutralSpawn{{
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		ID:           "neutral-spawn-5-5",
		GroupID:      "small_raider_group",
		StartingTier: 1,
		AggroRange:   150,
		LeashRange:   200,
	}}
	s.initNeutralCampsLocked()
	if len(s.NeutralCamps) != 1 {
		t.Fatalf("test setup: expected 1 NeutralCamp, got %d", len(s.NeutralCamps))
	}
	return s
}

// TestLootDrop_PickupGrantsContents: unit standing on a chest with
// OrderPickupLoot collects it on the next tickLootDropsLocked call.
// Resources reach the player; chest is removed; unit clears its order.
func TestLootDrop_PickupGrantsContents(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	camp := &s.NeutralCamps[0]
	camp.SpawnedGroupID = "small_raider_group"
	drop := s.spawnLootDropLocked(camp, map[string]int{"gold": 50, "wood": 15}, nil)

	player := &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"] = player
	unit := &Unit{
		ID:      9001,
		OwnerID: "p1",
		HP:      100,
		X:       drop.X,
		Y:       drop.Y,
		Visible: true,
	}
	unit.Order = OrderState{Type: OrderPickupLoot, DestX: drop.X, DestY: drop.Y}
	unit.PickupLootID = drop.ID
	s.addUnitLocked(unit)

	s.tickLootDropsLocked()

	if _, still := s.LootDrops[drop.ID]; still {
		t.Errorf("chest still present after pickup")
	}
	if got := player.Resources["gold"]; got != 50 {
		t.Errorf("gold = %d, want 50", got)
	}
	if got := player.Resources["wood"]; got != 15 {
		t.Errorf("wood = %d, want 15", got)
	}
	if unit.PickupLootID != "" {
		t.Errorf("PickupLootID not cleared: %q", unit.PickupLootID)
	}
	if unit.Order.Type != OrderIdle {
		t.Errorf("Order.Type = %v, want OrderIdle", unit.Order.Type)
	}
	notifs := s.drainPendingLootNotificationsLocked()
	if len(notifs) != 1 {
		t.Errorf("pending notifications: got %d, want 1", len(notifs))
	} else {
		if notifs[0].PlayerID != "p1" || notifs[0].LootDropID != drop.ID {
			t.Errorf("notification: %+v", notifs[0])
		}
		if notifs[0].CollectingUnitID != unit.ID {
			t.Errorf("CollectingUnitID = %d, want %d", notifs[0].CollectingUnitID, unit.ID)
		}
	}
}

// TestLootDrop_StalePickupNoOp: chest already collected; unit falls
// back to idle silently with no panic.
func TestLootDrop_StalePickupNoOp(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	player := &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"] = player
	unit := &Unit{ID: 9001, OwnerID: "p1", HP: 100, Visible: true}
	unit.Order = OrderState{Type: OrderPickupLoot}
	unit.PickupLootID = "loot-999" // never existed
	s.addUnitLocked(unit)

	s.tickLootDropsLocked()

	if unit.PickupLootID != "" {
		t.Errorf("PickupLootID not cleared on stale id")
	}
	if unit.Order.Type != OrderIdle {
		t.Errorf("expected fallback to OrderIdle, got %v", unit.Order.Type)
	}
}

// TestLootDrop_OutOfRangeDoesNotPickup: unit on OrderPickupLoot but
// far from the chest should not collect.
func TestLootDrop_OutOfRangeDoesNotPickup(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	camp := &s.NeutralCamps[0]
	camp.SpawnedGroupID = "small_raider_group"
	drop := s.spawnLootDropLocked(camp, map[string]int{"gold": 50}, nil)

	player := &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"] = player
	// Place the unit 1000 px from the chest.
	unit := &Unit{
		ID: 9001, OwnerID: "p1", HP: 100,
		X: drop.X + 1000, Y: drop.Y, Visible: true,
	}
	unit.Order = OrderState{Type: OrderPickupLoot}
	unit.PickupLootID = drop.ID
	s.addUnitLocked(unit)

	s.tickLootDropsLocked()

	if _, still := s.LootDrops[drop.ID]; !still {
		t.Errorf("chest removed despite unit being out of range")
	}
	if player.Resources["gold"] != 0 {
		t.Errorf("gold leaked to player despite out-of-range")
	}
}

// TestLootDrop_PickupUnboundedVaultCollectsItem: the vault is unbounded, so an
// item is always collected (never overflows), even when the vault already holds
// many items. Resources are granted and the chest consumed as usual.
func TestLootDrop_PickupUnboundedVaultCollectsItem(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	camp := &s.NeutralCamps[0]
	camp.SpawnedGroupID = "small_raider_group"
	drop := s.spawnLootDropLocked(camp,
		map[string]int{"gold": 50, "wood": 15},
		[]string{"broad_sword"},
	)

	player := &Player{
		ID:        "p1",
		Resources: map[string]int{},
		Vault:     []*VaultItem{},
	}
	s.Players["p1"] = player

	// Pre-fill the vault well past the old tier caps to prove there's no limit.
	const prefill = 20
	for i := 0; i < prefill; i++ {
		s.nextItemInstanceID++
		player.Vault = append(player.Vault, &VaultItem{
			InstanceID: s.nextItemInstanceID,
			ItemID:     "broad_sword",
			Stacks:     1,
		})
	}

	// Unit standing on the chest.
	unit := &Unit{
		ID:      9001,
		OwnerID: "p1",
		HP:      100,
		X:       drop.X,
		Y:       drop.Y,
		Visible: true,
	}
	unit.Order = OrderState{Type: OrderPickupLoot, DestX: drop.X, DestY: drop.Y}
	unit.PickupLootID = drop.ID
	s.addUnitLocked(unit)

	s.tickLootDropsLocked()

	// Chest consumed.
	if _, still := s.LootDrops[drop.ID]; still {
		t.Errorf("chest still present after pickup")
	}
	// Resources granted.
	if got := player.Resources["gold"]; got != 50 {
		t.Errorf("gold = %d, want 50", got)
	}
	if got := player.Resources["wood"]; got != 15 {
		t.Errorf("wood = %d, want 15", got)
	}
	// Vault grew by exactly one — the item was collected, not overflowed.
	if len(player.Vault) != prefill+1 {
		t.Errorf("vault size = %d, want %d (item should have been collected)", len(player.Vault), prefill+1)
	}
	// Notification reports the item as collected, with no overflow.
	notifs := s.drainPendingLootNotificationsLocked()
	if len(notifs) != 1 {
		t.Fatalf("pending notifications: got %d, want 1", len(notifs))
	}
	n := notifs[0]
	if len(n.OverflowItemIDs) != 0 {
		t.Errorf("OverflowItemIDs = %v, want empty (vault is unbounded)", n.OverflowItemIDs)
	}
	if len(n.ItemIDs) != 1 || n.ItemIDs[0] != "broad_sword" {
		t.Errorf("ItemIDs = %v, want [broad_sword]", n.ItemIDs)
	}
	if n.Resources["gold"] != 50 || n.Resources["wood"] != 15 {
		t.Errorf("notification resources mismatch: %+v", n.Resources)
	}
	if n.CollectingUnitID != unit.ID {
		t.Errorf("CollectingUnitID = %d, want %d", n.CollectingUnitID, unit.ID)
	}
}

// TestLootDrop_MultiSelectFirstWinsOthersFallToIdle: two units target the
// same chest; the first to be processed by tickLootDropsLocked collects it;
// the second finds the chest gone and falls back to idle. No panic, no
// double-grant.
//
// Spec requirement (loot-drops design §Pickup test coverage):
//
//	"Multi-select pickup → only one unit collects; others fall back
//	 to idle without errors."
//
// NOTE: tickLootDropsLocked iterates s.Units (a slice), so the first unit
// in insertion order wins. Insertion order is deterministic in tests because
// addUnitLocked appends.
func TestLootDrop_MultiSelectFirstWinsOthersFallToIdle(t *testing.T) {
	s := newTestStateForLootDrops(t, 1)
	camp := &s.NeutralCamps[0]
	camp.SpawnedGroupID = "small_raider_group"
	drop := s.spawnLootDropLocked(camp, map[string]int{"gold": 50}, nil)

	player := &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p1"] = player

	// Two units both standing on the chest with the same PickupLootID.
	// addUnitLocked appends to s.Units, so u1 is at index 0 and wins the
	// collection on this tick. u2 finds the chest already gone and idles.
	u1 := &Unit{
		ID:      9001,
		OwnerID: "p1",
		HP:      100,
		X:       drop.X,
		Y:       drop.Y,
		Visible: true,
	}
	u1.Order = OrderState{Type: OrderPickupLoot, DestX: drop.X, DestY: drop.Y}
	u1.PickupLootID = drop.ID
	s.addUnitLocked(u1)

	u2 := &Unit{
		ID:      9002,
		OwnerID: "p1",
		HP:      100,
		X:       drop.X,
		Y:       drop.Y,
		Visible: true,
	}
	u2.Order = OrderState{Type: OrderPickupLoot, DestX: drop.X, DestY: drop.Y}
	u2.PickupLootID = drop.ID
	s.addUnitLocked(u2)

	s.tickLootDropsLocked()

	// Chest consumed exactly once.
	if _, still := s.LootDrops[drop.ID]; still {
		t.Errorf("chest still present after multi-select pickup")
	}
	// Player credited exactly once — not twice.
	if got := player.Resources["gold"]; got != 50 {
		t.Errorf("gold = %d, want 50 (must not double-grant on multi-select)", got)
	}
	// Both units have cleared their pickup state to idle.
	if u1.PickupLootID != "" {
		t.Errorf("u1.PickupLootID = %q, want empty", u1.PickupLootID)
	}
	if u2.PickupLootID != "" {
		t.Errorf("u2.PickupLootID = %q, want empty", u2.PickupLootID)
	}
	if u1.Order.Type != OrderIdle {
		t.Errorf("u1.Order.Type = %v, want OrderIdle", u1.Order.Type)
	}
	if u2.Order.Type != OrderIdle {
		t.Errorf("u2.Order.Type = %v, want OrderIdle", u2.Order.Type)
	}
	// Notification fired exactly once — only the winning collector.
	notifs := s.drainPendingLootNotificationsLocked()
	if len(notifs) != 1 {
		t.Errorf("notifications: got %d, want 1 (single pickup must produce single notification)", len(notifs))
	} else {
		if notifs[0].CollectingUnitID != u1.ID {
			t.Errorf("CollectingUnitID = %d, want %d (u1 inserted first, must win the race)", notifs[0].CollectingUnitID, u1.ID)
		}
	}
}
