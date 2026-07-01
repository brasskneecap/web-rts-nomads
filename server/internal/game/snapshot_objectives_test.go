package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// findObjective is a small test helper for picking an ObjectiveSnapshot out
// of the snapshot's Victory.Objectives list by id. Returns a zero value when
// not found; callers should always verify the test setup loaded what they
// expect via len checks.
func findObjective(objs []protocol.ObjectiveSnapshot, id string) protocol.ObjectiveSnapshot {
	for _, o := range objs {
		if o.ID == id {
			return o
		}
	}
	return protocol.ObjectiveSnapshot{}
}

// TestSnapshotVictory_PerViewerPlayerScope is the §10.4 multiplayer snapshot
// test. It pins the key wire-format invariant: in a multiplayer campaign
// lobby, two viewers see the SAME team-scope objective progress but
// DIFFERENT player-scope progress (their own per-player state).
//
// Scenario:
//   - Two players p1 and p2 in the same campaign lobby.
//   - One team-scope objective (collect 500 gold).
//   - One player-scope objective (build 1 barracks).
//   - p1 has built a barracks; p2 has not.
//   - Both players have deposited some gold (p1: 300, p2: 200, team total: 500).
//
// Assertions:
//   - p1's snapshot shows the team gold objective COMPLETE (team total >= 500).
//   - p2's snapshot shows the same team gold objective COMPLETE (identical view).
//   - p1's snapshot shows the build_buildings objective COMPLETE for p1.
//   - p2's snapshot shows the build_buildings objective INCOMPLETE for p2.
//   - Both snapshots include both players' Metrics blocks for end-of-round columns.
func TestSnapshotVictory_PerViewerPlayerScope(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.Players["p1"] = &Player{ID: "p1", TeamID: 0, Metrics: NewMatchMetrics(), Resources: map[string]int{}}
	s.Players["p2"] = &Player{ID: "p2", TeamID: 0, Metrics: NewMatchMetrics(), Resources: map[string]int{}}

	installObjective(t, s, "team_gold", "collect_resource", "team", false,
		collectResourceConfig{Resource: "gold", Amount: 500})
	installObjective(t, s, "player_barracks", "build_buildings", "player", false,
		buildBuildingsConfig{BuildingType: "barracks", Count: 1})

	// Seed metrics: p1 has deposited 300 gold + built a barracks; p2 has
	// deposited 200 gold and built nothing.
	s.Players["p1"].Metrics.RecordGoldEarned(300)
	s.Players["p1"].Metrics.RecordBuildingBuilt("barracks")
	s.Players["p2"].Metrics.RecordGoldEarned(200)

	// Run the evaluator once. Team-scope collect_resource sees the sum
	// (500 → completes). Player-scope build_buildings evaluates per player
	// (p1 → 1 barracks → completes; p2 → 0 → incomplete).
	s.mu.Lock()
	s.evaluateObjectivesLocked()
	s.mu.Unlock()

	snapP1 := s.SnapshotForPlayer("p1")
	snapP2 := s.SnapshotForPlayer("p2")

	// Both snapshots must include Victory (campaign match has objectives).
	if snapP1.Victory == nil {
		t.Fatal("p1 snapshot should include Victory snapshot when objectives are installed")
	}
	if snapP2.Victory == nil {
		t.Fatal("p2 snapshot should include Victory snapshot when objectives are installed")
	}

	// Team-scope objective: identical to both viewers, both see complete.
	teamP1 := findObjective(snapP1.Victory.Objectives, "team_gold")
	teamP2 := findObjective(snapP2.Victory.Objectives, "team_gold")
	if !teamP1.Completed || !teamP2.Completed {
		t.Errorf("team_gold should be completed for both viewers; got p1=%v p2=%v",
			teamP1.Completed, teamP2.Completed)
	}
	if teamP1.Current != teamP2.Current {
		t.Errorf("team-scope progress must be identical across viewers; p1=%d p2=%d",
			teamP1.Current, teamP2.Current)
	}
	if teamP1.Current != 500 {
		t.Errorf("team_gold Current should equal team total 500, got %d", teamP1.Current)
	}
	if teamP1.Scope != "team" {
		t.Errorf("team_gold Scope wire value: want \"team\", got %q", teamP1.Scope)
	}

	// Player-scope objective: each viewer sees THEIR own state.
	playerP1 := findObjective(snapP1.Victory.Objectives, "player_barracks")
	playerP2 := findObjective(snapP2.Victory.Objectives, "player_barracks")
	if !playerP1.Completed {
		t.Errorf("p1 should see player_barracks COMPLETED (p1 built a barracks); got %+v", playerP1)
	}
	if playerP2.Completed {
		t.Errorf("p2 should see player_barracks INCOMPLETE (p2 has no barracks); got %+v", playerP2)
	}
	if playerP1.Scope != "player" {
		t.Errorf("player_barracks Scope wire value: want \"player\", got %q", playerP1.Scope)
	}

	// Both snapshots must include both players' Metrics blocks so the
	// end-of-round recap (§15) can render side-by-side columns regardless
	// of which player is viewing.
	if len(snapP1.Players) != 2 || len(snapP2.Players) != 2 {
		t.Fatalf("each snapshot should carry 2 PlayerSnapshot entries; p1=%d p2=%d",
			len(snapP1.Players), len(snapP2.Players))
	}
	for _, viewer := range []protocol.MatchSnapshotMessage{snapP1, snapP2} {
		seenP1, seenP2 := false, false
		for _, ps := range viewer.Players {
			switch ps.PlayerID {
			case "p1":
				seenP1 = true
				if ps.Metrics.TotalGoldEarned != 300 {
					t.Errorf("PlayerSnapshot[p1].Metrics.TotalGoldEarned: want 300, got %d",
						ps.Metrics.TotalGoldEarned)
				}
				if ps.Metrics.BuildingsBuilt != 1 {
					t.Errorf("PlayerSnapshot[p1].Metrics.BuildingsBuilt: want 1, got %d",
						ps.Metrics.BuildingsBuilt)
				}
			case "p2":
				seenP2 = true
				if ps.Metrics.TotalGoldEarned != 200 {
					t.Errorf("PlayerSnapshot[p2].Metrics.TotalGoldEarned: want 200, got %d",
						ps.Metrics.TotalGoldEarned)
				}
				if ps.Metrics.BuildingsBuilt != 0 {
					t.Errorf("PlayerSnapshot[p2].Metrics.BuildingsBuilt: want 0, got %d",
						ps.Metrics.BuildingsBuilt)
				}
			}
		}
		if !seenP1 || !seenP2 {
			t.Errorf("snapshot must include both p1 and p2 Players entries: seenP1=%v seenP2=%v",
				seenP1, seenP2)
		}
	}
}

// TestSnapshotVictory_CustomGameOmitsVictory verifies that a match with
// zero installed objectives (Custom Game / find-game) produces a snapshot
// with `Victory == nil`. Client code uses this to hide the in-match
// objective panel.
func TestSnapshotVictory_CustomGameOmitsVictory(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.Players["p1"] = &Player{ID: "p1", Metrics: NewMatchMetrics(), Resources: map[string]int{}}
	// Intentionally do NOT install any objectives (Custom Game).

	snap := s.SnapshotForPlayer("p1")
	if snap.Victory != nil {
		t.Errorf("Custom Game snapshot should have nil Victory; got %+v", snap.Victory)
	}
}

// TestObjectiveSnapshot_CarriesRewardDominionPoints verifies that the per-tick
// ObjectiveSnapshot wire type carries the RewardDominionPoints value from the
// objective's Def. This is the §Task-2 acceptance test for the first-ever
// map-objective DP reward surfacing feature.
func TestObjectiveSnapshot_CarriesRewardDominionPoints(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.Players["p1"] = &Player{ID: "p1", TeamID: 0, Metrics: NewMatchMetrics(), Resources: map[string]int{}}

	// Directly install an objectiveRuntime with RewardDominionPoints set on
	// the Def. We bypass installObjective / parseAndValidateObjectiveDef
	// intentionally: this test exercises snapshot projection only, not
	// evaluation, so no handler or parsedConfig is needed.
	s.Objectives = []objectiveRuntime{
		{
			Def: ObjectiveDef{
				ID:                   "clear_camps",
				Type:                 "kill_camps",
				Scope:                ObjectiveScopeTeam,
				RewardDominionPoints: 40,
			},
			TeamState: ObjectiveState{ObjectiveID: "clear_camps", Scope: ObjectiveScopeTeam},
		},
	}

	snap := s.buildVictorySnapshotForViewerLocked("")
	if snap == nil {
		t.Fatal("expected a victory snapshot, got nil")
	}
	if len(snap.Objectives) == 0 {
		t.Fatalf("expected at least one objective in snapshot, got none")
	}
	got := findObjective(snap.Objectives, "clear_camps")
	if got.ID == "" {
		t.Fatalf("clear_camps objective not found in snapshot")
	}
	if got.RewardDominionPoints != 40 {
		t.Fatalf("RewardDominionPoints: want 40, got %d", got.RewardDominionPoints)
	}
}

// TestSnapshotVictory_NewlyJoinedPlayerSeesInitialPlayerScopeState verifies
// the lazy-init path for player-scope objectives: if a player joins after
// the evaluator has already populated PlayerStates for OTHER players but
// not them, the snapshot still returns a valid initial state (Current=0,
// Required from the handler) rather than nil/empty.
func TestSnapshotVictory_NewlyJoinedPlayerSeesInitialPlayerScopeState(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.Players["existing"] = &Player{ID: "existing", Metrics: NewMatchMetrics(), Resources: map[string]int{}}

	installObjective(t, s, "build_thing", "build_buildings", "player", false,
		buildBuildingsConfig{BuildingType: "barracks", Count: 3})

	// Evaluator populates "existing"'s player state.
	s.mu.Lock()
	s.evaluateObjectivesLocked()
	s.mu.Unlock()

	// "latecomer" joins now and immediately gets a snapshot — they have
	// no entry in runtime.PlayerStates yet.
	snap := s.SnapshotForPlayer("latecomer")
	if snap.Victory == nil {
		t.Fatal("snapshot should include Victory snapshot")
	}
	o := findObjective(snap.Victory.Objectives, "build_thing")
	if o.ID == "" {
		t.Fatalf("snapshot missing build_thing objective")
	}
	if o.Current != 0 {
		t.Errorf("newly-joined player should see Current=0, got %d", o.Current)
	}
	if o.RequiredCount != 3 {
		t.Errorf("newly-joined player should see RequiredCount from handler config: want 3, got %d",
			o.RequiredCount)
	}
	if o.Completed {
		t.Errorf("newly-joined player should not see Completed")
	}
}
