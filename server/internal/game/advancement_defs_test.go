package game

import (
	"testing"
)

// TestListUnitAdvancementTracks_SoldierTrackPresent verifies the soldier track
// is loaded with the correct structure (unitType, nodes, kind, effects slice).
func TestListUnitAdvancementTracks_SoldierTrackPresent(t *testing.T) {
	tracks := ListUnitAdvancementTracks()
	if len(tracks) == 0 {
		t.Fatal("ListUnitAdvancementTracks: want at least 1 track, got 0")
	}

	var soldierTrack *UnitAdvancementTrack
	for i := range tracks {
		if tracks[i].UnitType == "soldier" {
			soldierTrack = &tracks[i]
			break
		}
	}
	if soldierTrack == nil {
		t.Fatal("soldier track not found in catalog")
	}
	if len(soldierTrack.Nodes) == 0 {
		t.Fatal("soldier track has no nodes")
	}
	node := soldierTrack.Nodes[0]
	if node.ID != "soldier_hp_1" {
		t.Errorf("node[0].id: want %q, got %q", "soldier_hp_1", node.ID)
	}
	if node.Kind != "minor" {
		t.Errorf("node[0].kind: want %q, got %q", "minor", node.Kind)
	}
	// Cost is a balance tunable owned by the catalog JSON; assert it is set, not its value.
	if node.Cost <= 0 {
		t.Errorf("node[0].cost: want > 0, got %d", node.Cost)
	}
	if len(node.Effects) == 0 {
		t.Fatal("node[0].effects must be non-empty")
	}
	eff := node.Effects[0]
	if eff.Kind != "unitStatAdd" {
		t.Errorf("effects[0].kind: want %q, got %q", "unitStatAdd", eff.Kind)
	}
	if eff.Stat != "maxHp" {
		t.Errorf("effects[0].stat: want %q, got %q", "maxHp", eff.Stat)
	}
	// Amount is a balance tunable owned by the catalog JSON; assert it is set, not its value.
	if eff.Amount <= 0 {
		t.Errorf("effects[0].amount: want > 0, got %d", eff.Amount)
	}
}

// TestListUnitAdvancementTracks_SortedByUnitType verifies tracks are ordered
// alphabetically by UnitType (deterministic API output).
func TestListUnitAdvancementTracks_SortedByUnitType(t *testing.T) {
	tracks := ListUnitAdvancementTracks()
	for i := 1; i < len(tracks); i++ {
		if tracks[i].UnitType < tracks[i-1].UnitType {
			t.Errorf("tracks not sorted: tracks[%d].unitType %q < tracks[%d].unitType %q",
				i, tracks[i].UnitType, i-1, tracks[i-1].UnitType)
		}
	}
}

// TestGetAdvancementDef_KnownID verifies the flat lookup returns the correct node.
func TestGetAdvancementDef_KnownID(t *testing.T) {
	node, ok := GetAdvancementDef("soldier_hp_1")
	if !ok {
		t.Fatal("GetAdvancementDef: soldier_hp_1 not found")
	}
	if node.ID != "soldier_hp_1" {
		t.Errorf("id: want %q, got %q", "soldier_hp_1", node.ID)
	}
	if node.Kind != "minor" {
		t.Errorf("kind: want %q, got %q", "minor", node.Kind)
	}
	if len(node.Effects) == 0 {
		t.Fatal("effects must be non-empty")
	}
}

// TestGetAdvancementDef_UnknownID verifies that a missing ID returns false.
func TestGetAdvancementDef_UnknownID(t *testing.T) {
	_, ok := GetAdvancementDef("does_not_exist")
	if ok {
		t.Error("GetAdvancementDef: expected false for unknown id, got true")
	}
}

// TestGetAdvancementPrerequisiteID_FirstNodeHasNoPrereq verifies that the
// first node in a track (index 0) returns "" (no prerequisite).
func TestGetAdvancementPrerequisiteID_FirstNodeHasNoPrereq(t *testing.T) {
	prereq := GetAdvancementPrerequisiteID("soldier_hp_1")
	if prereq != "" {
		t.Errorf("prerequisite for first node: want %q, got %q", "", prereq)
	}
}

// TestGetAdvancementPrerequisiteID_UnknownIDReturnsEmpty verifies that an
// unrecognised ID returns "".
func TestGetAdvancementPrerequisiteID_UnknownIDReturnsEmpty(t *testing.T) {
	prereq := GetAdvancementPrerequisiteID("totally_fake_id")
	if prereq != "" {
		t.Errorf("prerequisite for unknown id: want %q, got %q", "", prereq)
	}
}

// TestListUnitAdvancementTracks_ReturnsCopy verifies that mutating the returned
// slice does not affect subsequent calls (defensive copy).
func TestListUnitAdvancementTracks_ReturnsCopy(t *testing.T) {
	first := ListUnitAdvancementTracks()
	if len(first) == 0 {
		t.Skip("no tracks loaded, skip copy test")
	}
	// Corrupt the returned slice.
	first[0].UnitType = "MUTATED"

	second := ListUnitAdvancementTracks()
	if len(second) > 0 && second[0].UnitType == "MUTATED" {
		t.Error("ListUnitAdvancementTracks returned a reference to internal state, not a copy")
	}
}

// TestApplyAdvancementsToEffectiveDefs_SingleEffectApplied verifies that
// applying soldier_hp_1 boosts the soldier HP by 50.
func TestApplyAdvancementsToEffectiveDefs_SingleEffectApplied(t *testing.T) {
	catalogDef, ok := getUnitDef("soldier")
	if !ok {
		t.Skip("soldier not in unit catalog, skip")
	}
	baseHP := catalogDef.HP

	player := &Player{
		AcquiredAdvancements: []string{"soldier_hp_1"},
	}
	applyAdvancementsToEffectiveDefsLocked(player)

	effectiveDef, found := player.EffectiveUnitDefs["soldier"]
	if !found {
		t.Fatal("EffectiveUnitDefs: soldier entry not created after applying advancement")
	}
	wantHP := baseHP + advNodeAmount(t, "soldier_hp_1")
	if effectiveDef.HP != wantHP {
		t.Errorf("effective soldier HP: want %d, got %d", wantHP, effectiveDef.HP)
	}
}

// TestApplyAdvancementsToEffectiveDefs_NoAdvancements verifies that a player
// with no advancements has no EffectiveUnitDefs entries written.
func TestApplyAdvancementsToEffectiveDefs_NoAdvancements(t *testing.T) {
	player := &Player{
		AcquiredAdvancements: nil,
	}
	applyAdvancementsToEffectiveDefsLocked(player)
	if len(player.EffectiveUnitDefs) != 0 {
		t.Errorf("EffectiveUnitDefs: want empty, got %v", player.EffectiveUnitDefs)
	}
}

// TestApplyAdvancementsToEffectiveDefs_SkipsUnknownID verifies that an ID not
// in the catalog is skipped gracefully without panic.
func TestApplyAdvancementsToEffectiveDefs_SkipsUnknownID(t *testing.T) {
	player := &Player{
		AcquiredAdvancements: []string{"catalog_entry_removed"},
	}
	// Must not panic.
	applyAdvancementsToEffectiveDefsLocked(player)
	if len(player.EffectiveUnitDefs) != 0 {
		t.Errorf("EffectiveUnitDefs: want empty for unknown id, got %v", player.EffectiveUnitDefs)
	}
}
