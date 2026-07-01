package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestObjectiveBadgeReward_PositivePreserved(t *testing.T) {
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardConquestBadges: 2,
	})
	if def.RewardConquestBadges != 2 {
		t.Fatalf("RewardConquestBadges: want 2, got %d", def.RewardConquestBadges)
	}
}

func TestObjectiveBadgeReward_NegativeRejected(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for negative rewardConquestBadges, got none")
		}
	}()
	parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardConquestBadges: -1,
	})
}

func TestObjectiveSnapshot_CarriesRewardConquestBadges(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.Players["p1"] = &Player{ID: "p1", TeamID: 0, Metrics: NewMatchMetrics(), Resources: map[string]int{}}

	// Directly install an objectiveRuntime with RewardConquestBadges set on
	// the Def. Bypass installObjective / parseAndValidateObjectiveDef
	// intentionally: this test exercises snapshot projection only, not
	// evaluation, so no handler or parsedConfig is needed.
	s.Objectives = []objectiveRuntime{
		{
			Def: ObjectiveDef{
				ID:                   "clear_camps",
				Type:                 "kill_camps",
				Scope:                ObjectiveScopeTeam,
				RewardConquestBadges: 3,
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
	if got.RewardConquestBadges != 3 {
		t.Fatalf("RewardConquestBadges: want 3, got %d", got.RewardConquestBadges)
	}
}
