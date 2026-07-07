package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestAdept_RequiresTemple is a regression guard on the catalog wiring: the
// adept must gate on a temple, and the chapel/temple must offer it as a
// trainable unit.
func TestAdept_RequiresTemple(t *testing.T) {
	def, ok := getUnitDef("adept")
	if !ok {
		t.Fatal("adept unit def not registered")
	}
	if len(def.RequiresBuildings) != 1 || def.RequiresBuildings[0] != "temple" {
		t.Errorf("adept.RequiresBuildings = %v; want [\"temple\"]", def.RequiresBuildings)
	}
	// The chapel must list the adept so it shows (greyed) before the upgrade.
	chapel, ok := buildingDefsByType["chapel"]
	if !ok {
		t.Fatal("chapel building def not registered")
	}
	if !containsString(chapel.SpawnUnitTypes, "adept") {
		t.Errorf("chapel.SpawnUnitTypes = %v; want it to include \"adept\"", chapel.SpawnUnitTypes)
	}
}

// TestBuildingRequirementTier_TempleResolvesToChapelTier2 verifies the tier-link
// resolution: a "temple" requirement is really "a chapel at tier ≥ 2", while a
// plain "chapel" requirement stays tier 1. Values are derived from the catalog
// chain, not hardcoded tiers.
func TestBuildingRequirementTier_TempleResolvesToChapelTier2(t *testing.T) {
	chain := upgradeChainFor("chapel")
	templeTier := 0
	for i, d := range chain {
		if d.Type == "temple" {
			templeTier = i + 1
		}
	}
	if templeTier == 0 {
		t.Fatal("temple is not part of the chapel upgrade chain")
	}

	root, tier := buildingRequirementTier("temple")
	if root != "chapel" || tier != templeTier {
		t.Errorf("buildingRequirementTier(temple) = (%q, %d); want (chapel, %d)", root, tier, templeTier)
	}

	if root, tier := buildingRequirementTier("chapel"); root != "chapel" || tier != 1 {
		t.Errorf("buildingRequirementTier(chapel) = (%q, %d); want (chapel, 1)", root, tier)
	}
	// A plain, non-tier building type is unchanged.
	if root, tier := buildingRequirementTier("blacksmith"); root != "blacksmith" || tier != 1 {
		t.Errorf("buildingRequirementTier(blacksmith) = (%q, %d); want (blacksmith, 1)", root, tier)
	}
}

// addChapelAtTier injects a fully-built chapel (spawning acolyte + adept) at the
// given tier owned by playerID and returns its ID. Caller holds s.mu.
func addChapelAtTier(s *GameState, id, playerID string, tier int) string {
	owner := playerID
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:             id,
		BuildingType:   "chapel",
		Width:          2,
		Height:         2,
		Visible:        true,
		OwnerID:        &owner,
		Capabilities:   []string{"unit-spawner"},
		SpawnUnitTypes: []string{"acolyte", "adept"},
		Metadata:       map[string]interface{}{"tier": float64(tier)},
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	last := &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
	s.buildingsByID[last.ID] = last
	return id
}

// TestPlayerHasBuildingType_TempleTierGate verifies the tier gate: a tier-1
// chapel does NOT satisfy a temple requirement, a tier-2 chapel does, and the
// plain chapel requirement is satisfied by either.
func TestPlayerHasBuildingType_TempleTierGate(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// No chapel at all.
	if s.playerHasBuildingTypeLocked(p1, "temple") {
		t.Error("no chapel; temple requirement should be unmet")
	}

	// Tier-1 chapel: satisfies "chapel" but NOT "temple".
	id := addChapelAtTier(s, "chapel-1", p1, 1)
	if !s.playerHasBuildingTypeLocked(p1, "chapel") {
		t.Error("tier-1 chapel should satisfy a chapel requirement")
	}
	if s.playerHasBuildingTypeLocked(p1, "temple") {
		t.Error("tier-1 chapel should NOT satisfy a temple requirement")
	}
	if s.playerMeetsUnitRequirementsLocked(p1, "adept") {
		t.Error("adept should be locked at a tier-1 chapel")
	}

	// Upgrade the chapel to tier 2 (temple).
	s.buildingsByID[id].Metadata["tier"] = float64(2)
	if !s.playerHasBuildingTypeLocked(p1, "temple") {
		t.Error("tier-2 chapel (temple) should satisfy a temple requirement")
	}
	if !s.playerMeetsUnitRequirementsLocked(p1, "adept") {
		t.Error("adept should be unlocked at a tier-2 chapel (temple)")
	}
	// Acolyte (requires plain chapel) stays available at a temple too.
	if !s.playerMeetsUnitRequirementsLocked(p1, "acolyte") {
		t.Error("acolyte should remain trainable at a temple")
	}
}

// TestTrainAdept_GatedByTempleUpgrade drives the full TrainUnit path: training an
// adept at a tier-1 chapel is a no-op; once the chapel reaches tier 2 the adept
// queues and its cost is charged.
func TestTrainAdept_GatedByTempleUpgrade(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	s.Players[p1].Resources = map[string]int{"gold": 10000, "wood": 10000}
	id := addChapelAtTier(s, "chapel-1", p1, 1)
	s.mu.Unlock()

	// Tier-1 chapel: adept blocked.
	trainAndAssertNoOp(t, s, p1, id, "adept")

	// Upgrade to tier 2 and try again.
	s.mu.Lock()
	s.buildingsByID[id].Metadata["tier"] = float64(2)
	preGold := s.Players[p1].Resources["gold"]
	preWood := s.Players[p1].Resources["wood"]
	adeptDef, _ := getUnitDef("adept")
	s.mu.Unlock()

	s.TrainUnit(p1, id, "adept")

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := len(s.Productions[id]); got != 1 {
		t.Fatalf("expected 1 production queued after temple upgrade; got %d", got)
	}
	if s.Productions[id][0].UnitType != "adept" {
		t.Errorf("queued unit = %q; want adept", s.Productions[id][0].UnitType)
	}
	if want := preGold - adeptDef.ResourceCost["gold"]; s.Players[p1].Resources["gold"] != want {
		t.Errorf("gold = %d; want %d", s.Players[p1].Resources["gold"], want)
	}
	if want := preWood - adeptDef.ResourceCost["wood"]; s.Players[p1].Resources["wood"] != want {
		t.Errorf("wood = %d; want %d", s.Players[p1].Resources["wood"], want)
	}
}
