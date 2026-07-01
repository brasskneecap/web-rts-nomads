package game

import "testing"

// A valid non-negative reward survives parse+validate and is preserved on the
// returned ObjectiveDef.
func TestObjectiveReward_PositivePreserved(t *testing.T) {
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardDominionPoints: 25,
	})
	if def.RewardDominionPoints != 25 {
		t.Fatalf("RewardDominionPoints: want 25, got %d", def.RewardDominionPoints)
	}
}

// A negative reward is rejected at catalog-load time (panics, matching the
// other objective validation guards).
func TestObjectiveReward_NegativeRejected(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for negative rewardDominionPoints, got none")
		}
	}()
	parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardDominionPoints: -5,
	})
}
