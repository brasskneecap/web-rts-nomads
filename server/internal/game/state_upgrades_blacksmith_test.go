package game

import (
	"fmt"
	"testing"

	"webrts/server/pkg/protocol"
)

// newBlacksmithUpgradeTestState builds a GameState with a player "p1" that owns
// a fully-built town hall (tier 1 → upgrade cap 3) and one fully-built
// blacksmith. Returns the blacksmith's building ID. Lock is NOT held on return.
func newBlacksmithUpgradeTestState(t *testing.T) (s *GameState, playerID, blacksmithID string) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{
		ID: "p1",
		Resources: map[string]int{
			"gold": 9999,
			"wood": 9999,
		},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Upgrades:                      map[UpgradeTrack]int{},
		Vault:                         []*VaultItem{},
	}

	owner := "p1"

	// Fully-built town hall at tier 1 (cap 3).
	s.nextBuildingID++
	s.addBuildingLocked(protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		ID:           fmt.Sprintf("townhall-%d", s.nextBuildingID),
		BuildingType: "townhall",
		Width:        2,
		Height:       2,
		Occupied:     true,
		Visible:      true,
		OwnerID:      &owner,
		Metadata:     map[string]interface{}{"tier": float64(1)},
	})

	blacksmithID = addBlacksmithLocked(s, "p1", 10, 10)
	return s, "p1", blacksmithID
}

// addBlacksmithLocked places a fully-built blacksmith (upgrade-purchase) owned
// by playerID at the given grid coord and returns its ID. Caller holds s.mu.
func addBlacksmithLocked(s *GameState, playerID string, x, y int) string {
	owner := playerID
	s.nextBuildingID++
	id := fmt.Sprintf("blacksmith-%d", s.nextBuildingID)
	s.addBuildingLocked(protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: x, Y: y},
		ID:           id,
		BuildingType: "blacksmith",
		Width:        2,
		Height:       2,
		Occupied:     true,
		Visible:      true,
		OwnerID:      &owner,
		Capabilities: []string{"upgrade-purchase"},
		Metadata:     map[string]interface{}{},
	})
	return id
}

// TestBlacksmithUpgrade_ChargesWoodEqualToGold verifies a purchase deducts wood
// equal to the gold cost and registers research without applying the level.
func TestBlacksmithUpgrade_ChargesWoodEqualToGold(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.mu.Lock()
	def, _ := upgradeTrackDefByID(UpgradeTrackSoldier)
	cost := upgradeCostForLevel(def, 1)
	goldBefore := s.Players[pid].Resources["gold"]
	woodBefore := s.Players[pid].Resources["wood"]
	s.mu.Unlock()

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := goldBefore - s.Players[pid].Resources["gold"]; got != cost {
		t.Errorf("gold deduction: expected %d, got %d", cost, got)
	}
	if got := woodBefore - s.Players[pid].Resources["wood"]; got != cost {
		t.Errorf("wood deduction: expected %d (== gold cost), got %d", cost, got)
	}
	if lvl := s.Players[pid].Upgrades[UpgradeTrackSoldier]; lvl != 0 {
		t.Errorf("level should still be 0 immediately after purchase (timed), got %d", lvl)
	}
	au, ok := s.ActiveUpgrades[bid]
	if !ok || au.Track != UpgradeTrackSoldier || au.GoldPaid != cost || au.WoodPaid != cost {
		t.Errorf("registry entry missing/wrong for building %q: %+v", bid, au)
	}
}

// TestBlacksmithUpgrade_BlockedWhenInsufficientWood verifies a purchase is a
// no-op when the player has enough gold but not enough wood.
func TestBlacksmithUpgrade_BlockedWhenInsufficientWood(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.mu.Lock()
	def, _ := upgradeTrackDefByID(UpgradeTrackSoldier)
	cost := upgradeCostForLevel(def, 1)
	s.Players[pid].Resources["gold"] = cost
	s.Players[pid].Resources["wood"] = cost - 1 // one short
	s.mu.Unlock()

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))

	s.mu.RLock()
	defer s.mu.RUnlock()
	if g := s.Players[pid].Resources["gold"]; g != cost {
		t.Errorf("gold should be untouched on failed purchase, got %d want %d", g, cost)
	}
	if w := s.Players[pid].Resources["wood"]; w != cost-1 {
		t.Errorf("wood should be untouched on failed purchase, got %d want %d", w, cost-1)
	}
	if _, ok := s.ActiveUpgrades[bid]; ok {
		t.Error("no research should be registered on failed purchase")
	}
}

// TestBlacksmithUpgrade_CompletesAfterTimer verifies the level applies only
// after the timer elapses, then the registry entry clears.
func TestBlacksmithUpgrade_CompletesAfterTimer(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))

	s.mu.Lock()
	s.tickBlacksmithUpgradesLocked(blacksmithUpgradeResearchSeconds - 0.5)
	lvlMid := s.Players[pid].Upgrades[UpgradeTrackSoldier]
	s.mu.Unlock()
	if lvlMid != 0 {
		t.Errorf("level should still be 0 before timer elapses, got %d", lvlMid)
	}

	s.mu.Lock()
	s.tickBlacksmithUpgradesLocked(1.0)
	lvlDone := s.Players[pid].Upgrades[UpgradeTrackSoldier]
	_, stillResearching := s.ActiveUpgrades[bid]
	s.mu.Unlock()

	if lvlDone != 1 {
		t.Errorf("level should be 1 after timer completes, got %d", lvlDone)
	}
	if stillResearching {
		t.Error("registry entry should be cleared after completion")
	}
}

// TestBlacksmithUpgrade_OneUpgradePerBlacksmith verifies a busy blacksmith
// rejects a second (different-track) purchase targeting the same building.
func TestBlacksmithUpgrade_OneUpgradePerBlacksmith(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))

	s.mu.RLock()
	goldAfterFirst := s.Players[pid].Resources["gold"]
	s.mu.RUnlock()

	// Same building, different track — building is busy, must be ignored.
	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackArcher))

	s.mu.RLock()
	defer s.mu.RUnlock()
	if g := s.Players[pid].Resources["gold"]; g != goldAfterFirst {
		t.Errorf("busy blacksmith should not charge again: %d → %d", goldAfterFirst, g)
	}
	if au := s.ActiveUpgrades[bid]; au == nil || au.Track != UpgradeTrackSoldier {
		t.Errorf("busy blacksmith entry should remain the soldier research, got %+v", au)
	}
}

// TestBlacksmithUpgrade_TrackLockedAcrossBlacksmiths verifies that while a
// track is researching at one blacksmith, a second blacksmith cannot start the
// SAME track (it is locked), but CAN start a different track concurrently.
func TestBlacksmithUpgrade_TrackLockedAcrossBlacksmiths(t *testing.T) {
	s, pid, bid1 := newBlacksmithUpgradeTestState(t)
	s.mu.Lock()
	bid2 := addBlacksmithLocked(s, pid, 20, 20)
	s.mu.Unlock()

	// Start soldier at blacksmith #1.
	s.PurchaseUpgrade(pid, bid1, string(UpgradeTrackSoldier))

	s.mu.RLock()
	goldAfterFirst := s.Players[pid].Resources["gold"]
	s.mu.RUnlock()

	// Soldier at blacksmith #2 must be rejected (track locked).
	s.PurchaseUpgrade(pid, bid2, string(UpgradeTrackSoldier))
	s.mu.RLock()
	if _, busy := s.ActiveUpgrades[bid2]; busy {
		s.mu.RUnlock()
		t.Fatal("blacksmith #2 should not be able to research a locked track")
	}
	if g := s.Players[pid].Resources["gold"]; g != goldAfterFirst {
		s.mu.RUnlock()
		t.Errorf("locked-track purchase should not charge: %d → %d", goldAfterFirst, g)
	}
	s.mu.RUnlock()

	// Archer at blacksmith #2 is a DIFFERENT track — allowed concurrently.
	s.PurchaseUpgrade(pid, bid2, string(UpgradeTrackArcher))
	s.mu.RLock()
	au1 := s.ActiveUpgrades[bid1]
	au2 := s.ActiveUpgrades[bid2]
	s.mu.RUnlock()
	if au1 == nil || au1.Track != UpgradeTrackSoldier {
		t.Errorf("blacksmith #1 should be researching soldier, got %+v", au1)
	}
	if au2 == nil || au2.Track != UpgradeTrackArcher {
		t.Errorf("blacksmith #2 should be researching archer concurrently, got %+v", au2)
	}

	// Both complete and apply to the player.
	s.mu.Lock()
	s.tickBlacksmithUpgradesLocked(blacksmithUpgradeResearchSeconds + 1.0)
	soldierLvl := s.Players[pid].Upgrades[UpgradeTrackSoldier]
	archerLvl := s.Players[pid].Upgrades[UpgradeTrackArcher]
	s.mu.Unlock()
	if soldierLvl != 1 || archerLvl != 1 {
		t.Errorf("both upgrades should complete: soldier=%d archer=%d", soldierLvl, archerLvl)
	}
}

// TestBlacksmithUpgrade_CancelRefunds verifies cancelling refunds the full
// gold + wood paid and clears the registry entry.
func TestBlacksmithUpgrade_CancelRefunds(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.mu.RLock()
	goldBefore := s.Players[pid].Resources["gold"]
	woodBefore := s.Players[pid].Resources["wood"]
	s.mu.RUnlock()

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))
	s.CancelUpgrade(pid, bid)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if g := s.Players[pid].Resources["gold"]; g != goldBefore {
		t.Errorf("gold should be fully refunded: %d → %d (want %d)", goldBefore, g, goldBefore)
	}
	if w := s.Players[pid].Resources["wood"]; w != woodBefore {
		t.Errorf("wood should be fully refunded: %d → %d (want %d)", woodBefore, w, woodBefore)
	}
	if _, ok := s.ActiveUpgrades[bid]; ok {
		t.Error("registry entry should be cleared after cancel")
	}
	if lvl := s.Players[pid].Upgrades[UpgradeTrackSoldier]; lvl != 0 {
		t.Errorf("cancelled upgrade must not apply a level, got %d", lvl)
	}
}

// TestBlacksmithUpgrade_AutoAssignToIdleBlacksmith verifies an empty buildingID
// (global panel) assigns the research to an idle blacksmith.
func TestBlacksmithUpgrade_AutoAssignToIdleBlacksmith(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.PurchaseUpgrade(pid, "", string(UpgradeTrackSoldier))

	s.mu.RLock()
	defer s.mu.RUnlock()
	au, ok := s.ActiveUpgrades[bid]
	if !ok || au.Track != UpgradeTrackSoldier {
		t.Errorf("auto-assign should register research on the idle blacksmith %q, got %+v", bid, au)
	}
}

// TestBlacksmithUpgrade_StampsBuildingVisualMetadata verifies the source
// blacksmith carries the training-animation metadata while researching, and
// that it clears once research completes. A SECOND idle blacksmith must NOT be
// stamped (only the building doing the work animates).
func TestBlacksmithUpgrade_StampsBuildingVisualMetadata(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)
	s.mu.Lock()
	bid2 := addBlacksmithLocked(s, pid, 20, 20)
	s.mu.Unlock()

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))

	s.mu.Lock()
	s.refreshBuildingRuntimeMetadataLocked()
	b := s.getBuildingByIDLocked(bid)
	other := s.getBuildingByIDLocked(bid2)
	inProgress, _ := b.Metadata["upgradeInProgress"].(bool)
	track, _ := b.Metadata["upgradeTrack"].(string)
	total, _ := b.Metadata["upgradeTotalSeconds"].(float64)
	_, otherStamped := other.Metadata["upgradeInProgress"]
	s.mu.Unlock()

	if !inProgress {
		t.Error("source blacksmith should have upgradeInProgress=true")
	}
	if track != string(UpgradeTrackSoldier) {
		t.Errorf("upgradeTrack: want soldier, got %q", track)
	}
	if total != blacksmithUpgradeResearchSeconds {
		t.Errorf("upgradeTotalSeconds: want %v, got %v", blacksmithUpgradeResearchSeconds, total)
	}
	if otherStamped {
		t.Error("the idle second blacksmith must NOT be stamped while only the first researches")
	}

	// After completion: metadata cleared.
	s.mu.Lock()
	s.tickBlacksmithUpgradesLocked(blacksmithUpgradeResearchSeconds + 1.0)
	s.refreshBuildingRuntimeMetadataLocked()
	b = s.getBuildingByIDLocked(bid)
	_, stillPresent := b.Metadata["upgradeInProgress"]
	s.mu.Unlock()
	if stillPresent {
		t.Error("upgradeInProgress should be cleared after research completes")
	}
}

// TestBlacksmithUpgrade_SnapshotReportsResearchAndWoodCost verifies the
// per-player snapshot carries wood cost (== gold) and live research progress +
// the source building ID.
func TestBlacksmithUpgrade_SnapshotReportsResearchAndWoodCost(t *testing.T) {
	s, pid, bid := newBlacksmithUpgradeTestState(t)

	s.mu.RLock()
	before := s.playerUpgradeSnapshotsLocked(pid)
	s.mu.RUnlock()
	soldierBefore := findTrackSnapshot(before, UpgradeTrackSoldier)
	if soldierBefore == nil {
		t.Fatal("no soldier upgrade snapshot")
	}
	if soldierBefore.NextCostWood != soldierBefore.NextCostGold {
		t.Errorf("NextCostWood (%d) should equal NextCostGold (%d)",
			soldierBefore.NextCostWood, soldierBefore.NextCostGold)
	}
	if soldierBefore.ResearchTotal != 0 {
		t.Errorf("ResearchTotal should be 0 before purchase, got %v", soldierBefore.ResearchTotal)
	}
	if !soldierBefore.CanStart {
		t.Error("CanStart should be true with an idle blacksmith and resources")
	}

	s.PurchaseUpgrade(pid, bid, string(UpgradeTrackSoldier))

	s.mu.RLock()
	after := s.playerUpgradeSnapshotsLocked(pid)
	s.mu.RUnlock()
	soldierAfter := findTrackSnapshot(after, UpgradeTrackSoldier)
	if soldierAfter == nil {
		t.Fatal("no soldier upgrade snapshot after purchase")
	}
	if soldierAfter.ResearchTotal != blacksmithUpgradeResearchSeconds {
		t.Errorf("ResearchTotal should be %v while researching, got %v",
			blacksmithUpgradeResearchSeconds, soldierAfter.ResearchTotal)
	}
	if soldierAfter.ResearchBuildingID != bid {
		t.Errorf("ResearchBuildingID: want %q, got %q", bid, soldierAfter.ResearchBuildingID)
	}
	if soldierAfter.CanStart {
		t.Error("CanStart should be false while the track is researching")
	}
}

func findTrackSnapshot(snaps []protocol.PlayerUpgradeSnapshot, track UpgradeTrack) *protocol.PlayerUpgradeSnapshot {
	for i := range snaps {
		if snaps[i].Track == string(track) {
			return &snaps[i]
		}
	}
	return nil
}
