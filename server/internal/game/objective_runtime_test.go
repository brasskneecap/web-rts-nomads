package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestSetCampaignLevelLocked_LoadsForestObjectives verifies the catalog →
// runtime path: setting a real campaign level id pulls every authored
// objective onto GameState.Objectives with team state initialised from the
// handler config (Required reflects the cfg's count/amount/wavesToSurvive
// field).
//
// Intentionally agnostic about which specific objective ids the level
// carries — those drift through normal play tuning (e.g. dropping a wave
// requirement from 10 → 6 during balance work). We assert only the
// invariants the catalog pipeline is supposed to maintain: at least one
// objective loads, every objective starts uncompleted/unfailed, and every
// objective's Required value got initialised to a positive number by the
// handler.
func TestSetCampaignLevelLocked_LoadsForestObjectives(t *testing.T) {
	state := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})

	state.SetCampaignLevelLocked("forest_01")

	if state.CampaignLevelID != "forest_01" {
		t.Errorf("CampaignLevelID: want forest_01, got %q", state.CampaignLevelID)
	}
	if len(state.Objectives) == 0 {
		t.Fatal("Objectives should be non-empty after loading forest_01")
	}

	for _, runtime := range state.Objectives {
		if runtime.Def.ID == "" {
			t.Errorf("loaded objective has empty id: %+v", runtime.Def)
		}
		if runtime.TeamState.Required <= 0 {
			t.Errorf("objective %q TeamState.Required should be > 0 after init, got %d",
				runtime.Def.ID, runtime.TeamState.Required)
		}
		if runtime.TeamState.Completed || runtime.TeamState.Failed {
			t.Errorf("objective %q should start fresh (not completed/failed), got %+v",
				runtime.Def.ID, runtime.TeamState)
		}
	}
}

// TestSetCampaignLevelLocked_EmptyLevelID_Noop verifies that a Custom Game
// match (empty levelID) leaves Objectives empty without complaining.
func TestSetCampaignLevelLocked_EmptyLevelID_Noop(t *testing.T) {
	state := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})

	state.SetCampaignLevelLocked("")

	if state.CampaignLevelID != "" {
		t.Errorf("CampaignLevelID: want empty, got %q", state.CampaignLevelID)
	}
	if len(state.Objectives) != 0 {
		t.Errorf("Objectives should be empty for non-campaign match, got %d", len(state.Objectives))
	}
}

// TestSetCampaignLevelLocked_UnknownLevelID_LogsAndContinues verifies that
// a stale or typo'd level id from the client doesn't fail the match start —
// the level just runs without objectives.
func TestSetCampaignLevelLocked_UnknownLevelID_LogsAndContinues(t *testing.T) {
	state := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})

	state.SetCampaignLevelLocked("nope_does_not_exist")

	if state.CampaignLevelID != "nope_does_not_exist" {
		t.Errorf("CampaignLevelID should reflect what was requested, got %q", state.CampaignLevelID)
	}
	if len(state.Objectives) != 0 {
		t.Errorf("Objectives should be empty when level id is unknown, got %d", len(state.Objectives))
	}
}

// TestEnsurePlayerState_LazyInit verifies player-scope per-player state
// allocation: the first ensurePlayerState call allocates the map and the
// per-player entry; the second returns the existing entry.
func TestEnsurePlayerState_LazyInit(t *testing.T) {
	def, _ := loadObjective(t, "obj", "build_buildings", "player", false,
		buildBuildingsConfig{BuildingType: "barracks", Count: 1})

	runtime := newObjectiveRuntime(def)
	if runtime.PlayerStates != nil {
		t.Fatal("PlayerStates should start nil before any ensurePlayerState call")
	}

	state1 := runtime.ensurePlayerState("p1")
	if state1 == nil {
		t.Fatal("ensurePlayerState returned nil")
	}
	if state1.Required != 1 {
		t.Errorf("player state Required not initialised from handler config: got %d, want 1", state1.Required)
	}

	// Mutate and commit; second call must see the mutation.
	state1.Current = 1
	runtime.storePlayerState("p1", *state1)

	state2 := runtime.ensurePlayerState("p1")
	if state2.Current != 1 {
		t.Errorf("storePlayerState did not persist mutation: got Current=%d, want 1", state2.Current)
	}

	// Different player gets a fresh entry.
	statePlayer2 := runtime.ensurePlayerState("p2")
	if statePlayer2.Current != 0 {
		t.Errorf("new player state should start at Current=0, got %d", statePlayer2.Current)
	}
}
