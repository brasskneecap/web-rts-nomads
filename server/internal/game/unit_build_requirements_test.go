package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestRequiresBuildings_FieldExistsOnUnitDef verifies the new field is
// readable on a loaded UnitDef. A missing field means later tasks can't
// compile.
func TestRequiresBuildings_FieldExistsOnUnitDef(t *testing.T) {
	def, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}
	// At this point in the plan the archer.json change has not landed
	// yet, so the field exists but is empty. Reading it confirms the
	// type compiles.
	_ = def.RequiresBuildings
}

// TestArcher_RequiresBlacksmith verifies the archer catalog declares the
// blacksmith requirement. Regression guard against an accidental JSON
// edit that drops the field.
func TestArcher_RequiresBlacksmith(t *testing.T) {
	def, ok := getUnitDef("archer")
	if !ok {
		t.Fatal("archer unit def not registered")
	}
	if len(def.RequiresBuildings) != 1 || def.RequiresBuildings[0] != "blacksmith" {
		t.Errorf("archer.RequiresBuildings = %v; want [\"blacksmith\"]", def.RequiresBuildings)
	}
}

// TestSoldier_NoRequirements verifies the soldier (and by implication
// other unrequired units) is not gated. Regression guard against
// accidentally adding requirements to other units.
func TestSoldier_NoRequirements(t *testing.T) {
	def, ok := getUnitDef("soldier")
	if !ok {
		t.Fatal("soldier unit def not registered")
	}
	if len(def.RequiresBuildings) != 0 {
		t.Errorf("soldier.RequiresBuildings = %v; want []", def.RequiresBuildings)
	}
}

// newRequirementsTestState builds a GameState with player "p1" already
// ensured and no buildings. Tests add buildings as needed.
func newRequirementsTestState(t *testing.T) (*GameState, string) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	const playerID = "p1"
	s.EnsurePlayer(playerID)
	return s, playerID
}

// addBuildingToState injects a building owned by ownerID into the state
// and re-indexes buildingsByID. Caller must hold s.mu.
func addBuildingToState(s *GameState, id, buildingType, ownerID string, underConstruction bool, visible bool) {
	owner := ownerID
	meta := map[string]interface{}{}
	if underConstruction {
		meta["underConstruction"] = true
	}
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, protocol.BuildingTile{
		ID:           id,
		BuildingType: buildingType,
		Width:        2,
		Height:       2,
		Visible:      visible,
		OwnerID:      &owner,
		Capabilities: []string{},
		Metadata:     meta,
	})
	if s.buildingsByID == nil {
		s.buildingsByID = map[string]*protocol.BuildingTile{}
	}
	last := &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]
	s.buildingsByID[last.ID] = last
}

// TestPlayerHasBuildingTypeLocked covers the four corners: present and
// fully built (true), under construction (false), invisible (false),
// wrong type (false).
func TestPlayerHasBuildingTypeLocked(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.playerHasBuildingTypeLocked(p1, "blacksmith") {
		t.Error("no blacksmith yet; want false")
	}

	addBuildingToState(s, "bs-built", "blacksmith", p1, false, true)
	if !s.playerHasBuildingTypeLocked(p1, "blacksmith") {
		t.Error("fully-built blacksmith present; want true")
	}

	// Mid-construction does NOT count.
	addBuildingToState(s, "bs-uc", "blacksmith", "p2", true, true)
	if s.playerHasBuildingTypeLocked("p2", "blacksmith") {
		t.Error("only mid-construction blacksmith; want false")
	}

	// Invisible does NOT count.
	addBuildingToState(s, "bs-inv", "blacksmith", "p3", false, false)
	if s.playerHasBuildingTypeLocked("p3", "blacksmith") {
		t.Error("only invisible blacksmith; want false")
	}

	// Wrong type does NOT match.
	if s.playerHasBuildingTypeLocked(p1, "barracks") {
		t.Error("no barracks present; want false")
	}
}

// TestPlayerMeetsUnitRequirementsLocked verifies the AND semantics of
// RequiresBuildings and the "unknown unit type" defensive branch.
func TestPlayerMeetsUnitRequirementsLocked(t *testing.T) {
	s, p1 := newRequirementsTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Soldier has no requirements → always true.
	if !s.playerMeetsUnitRequirementsLocked(p1, "soldier") {
		t.Error("soldier has no requirements; want true")
	}

	// Archer requires blacksmith. Without one → false.
	if s.playerMeetsUnitRequirementsLocked(p1, "archer") {
		t.Error("no blacksmith; archer requirements should not be met")
	}

	// Mid-construction blacksmith → still false.
	addBuildingToState(s, "bs-uc", "blacksmith", p1, true, true)
	if s.playerMeetsUnitRequirementsLocked(p1, "archer") {
		t.Error("mid-construction blacksmith; archer requirements should not be met")
	}

	// Fully-built blacksmith → true.
	addBuildingToState(s, "bs-built", "blacksmith", p1, false, true)
	if !s.playerMeetsUnitRequirementsLocked(p1, "archer") {
		t.Error("fully-built blacksmith; archer requirements should be met")
	}

	// Unknown unit type → false (defensive).
	if s.playerMeetsUnitRequirementsLocked(p1, "no_such_unit") {
		t.Error("unknown unit type; want false")
	}
}
