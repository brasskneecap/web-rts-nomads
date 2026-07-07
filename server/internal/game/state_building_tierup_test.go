package game

import (
	"fmt"
	"testing"

	"webrts/server/pkg/protocol"
)

// addTierRootBuildingLocked places a fully-built, owned tier-1 building of the
// given root type and returns its ID. Caller holds s.mu.
func addTierRootBuildingLocked(s *GameState, playerID, buildingType string, x, y int) string {
	owner := playerID
	s.nextBuildingID++
	id := fmt.Sprintf("%s-%d", buildingType, s.nextBuildingID)
	s.addBuildingLocked(protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: x, Y: y},
		ID:           id,
		BuildingType: buildingType,
		Width:        2,
		Height:       2,
		Occupied:     true,
		Visible:      true,
		OwnerID:      &owner,
		Metadata:     map[string]interface{}{"tier": float64(1)},
	})
	return id
}

// newTierUpTestState builds a GameState with a resource-rich player "p1". Lock
// is NOT held on return.
func newTierUpTestState(t *testing.T) (s *GameState, playerID string) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = &Player{
		ID:                            "p1",
		Resources:                     map[string]int{"gold": 99999, "wood": 99999},
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Upgrades:                      map[UpgradeTrack]int{},
		Vault:                         []*VaultItem{},
	}
	return s, "p1"
}

// TestBuildingTierUp_Generic drives the generalized tier-up mechanism for every
// tier root in the catalog and asserts it charges the next tier's catalog cost,
// runs for its catalog duration, and promotes metadata["tier"] on completion —
// without pinning any balance numbers. Covers townhall→keep and chapel→temple.
func TestBuildingTierUp_Generic(t *testing.T) {
	roots := []struct {
		rootType string
		x, y     int
	}{
		{"townhall", 5, 5},
		{"chapel", 20, 20},
	}

	for _, r := range roots {
		t.Run(r.rootType, func(t *testing.T) {
			s, playerID := newTierUpTestState(t)

			// Expected cost/duration come straight from the catalog's next-tier
			// def — never hardcoded here.
			chain := upgradeChainFor(r.rootType)
			if len(chain) < 2 {
				t.Fatalf("%s is not a tier-upgradeable root (chain=%d)", r.rootType, len(chain))
			}
			nextDef := chain[1]
			wantGold := nextDef.UpgradeCost["gold"]
			wantWood := nextDef.UpgradeCost["wood"]
			wantSeconds := nextDef.UpgradeSeconds
			if wantSeconds <= 0 || (wantGold == 0 && wantWood == 0) {
				t.Fatalf("%s tier def missing upgradeCost/upgradeSeconds", nextDef.Type)
			}

			s.mu.Lock()
			id := addTierRootBuildingLocked(s, playerID, r.rootType, r.x, r.y)
			goldBefore := s.Players[playerID].Resources["gold"]
			woodBefore := s.Players[playerID].Resources["wood"]
			s.mu.Unlock()

			// Begin the tier-up.
			s.UpgradeBuildingTier(playerID, id)

			s.mu.Lock()
			b := s.getBuildingByIDLocked(id)
			if b == nil {
				s.mu.Unlock()
				t.Fatalf("building %s vanished", id)
			}
			// Resources deducted by exactly the catalog cost.
			if got := goldBefore - s.Players[playerID].Resources["gold"]; got != wantGold {
				s.mu.Unlock()
				t.Fatalf("gold charged = %d, want %d", got, wantGold)
			}
			if got := woodBefore - s.Players[playerID].Resources["wood"]; got != wantWood {
				s.mu.Unlock()
				t.Fatalf("wood charged = %d, want %d", got, wantWood)
			}
			// tierUp metadata stamped; tier not yet advanced.
			if _, inProgress := b.Metadata["tierUpRemaining"]; !inProgress {
				s.mu.Unlock()
				t.Fatalf("expected tierUpRemaining to be set after starting upgrade")
			}
			if tier, _ := getMetadataFloat(b.Metadata, "tier"); tier != 1 {
				s.mu.Unlock()
				t.Fatalf("tier should still be 1 mid-upgrade, got %v", tier)
			}

			// A second request while in progress must be a no-op (no double charge).
			goldMid := s.Players[playerID].Resources["gold"]
			s.mu.Unlock()
			s.UpgradeBuildingTier(playerID, id)
			s.mu.Lock()
			if s.Players[playerID].Resources["gold"] != goldMid {
				s.mu.Unlock()
				t.Fatalf("second upgrade request charged again; expected no-op")
			}

			// Advance the tier-up tick past the catalog duration.
			s.tickBuildingTierUpsLocked(wantSeconds + 0.5)
			b = s.getBuildingByIDLocked(id)
			if _, stillGoing := b.Metadata["tierUpRemaining"]; stillGoing {
				s.mu.Unlock()
				t.Fatalf("tierUpRemaining should be cleared after completion")
			}
			if tier, _ := getMetadataFloat(b.Metadata, "tier"); tier != 2 {
				s.mu.Unlock()
				t.Fatalf("tier should be 2 after completion, got %v", tier)
			}
			s.mu.Unlock()
		})
	}
}

// TestBuildingTierUp_RejectsNonTierBuilding verifies the generic mechanism does
// nothing for a building whose type does not root an upgrade chain (e.g. a
// barracks), so no metadata is stamped and no resources are spent.
func TestBuildingTierUp_RejectsNonTierBuilding(t *testing.T) {
	if len(upgradeChainFor("barracks")) > 1 {
		t.Skip("barracks unexpectedly roots a tier chain; pick another non-tier type")
	}
	s, playerID := newTierUpTestState(t)

	s.mu.Lock()
	id := addTierRootBuildingLocked(s, playerID, "barracks", 5, 5)
	goldBefore := s.Players[playerID].Resources["gold"]
	s.mu.Unlock()

	s.UpgradeBuildingTier(playerID, id)

	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.getBuildingByIDLocked(id)
	if _, inProgress := b.Metadata["tierUpRemaining"]; inProgress {
		t.Fatalf("non-tier building should not start an upgrade")
	}
	if s.Players[playerID].Resources["gold"] != goldBefore {
		t.Fatalf("non-tier upgrade should not charge resources")
	}
}
