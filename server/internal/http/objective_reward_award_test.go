package httpserver

import (
	"net/http"
	"testing"
)

// First-ever completion of a rewarded objective credits its DP to the profile
// (both spendable and lifetime).
func TestObjectiveReward_FirstCompletionAwardsDP(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardDominionPoints": 30},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	p := readProfileBody(t, rec)
	if p.DominionPoints != 30 {
		t.Errorf("DominionPoints: want 30, got %d", p.DominionPoints)
	}
	if p.LifetimeDominionPoints != 30 {
		t.Errorf("LifetimeDominionPoints: want 30, got %d", p.LifetimeDominionPoints)
	}
}

// Re-completing the same objective grants nothing (first-time-ever only).
func TestObjectiveReward_RepeatCompletionAwardsNothing(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	body := map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardDominionPoints": 30}},
	}
	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	second := postJSON(t, mux, completeObjectivesPath, testPlayerID, body)

	p := readProfileBody(t, second)
	if p.DominionPoints != 30 {
		t.Errorf("DominionPoints after repeat: want 30 (awarded once), got %d", p.DominionPoints)
	}
}

// A batch mixing a brand-new objective with an already-completed one credits
// only the new one.
func TestObjectiveReward_OnlyNewObjectivesAwarded(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardDominionPoints": 30}},
	})
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardDominionPoints": 30},
			{"id": "build_barracks", "rewardDominionPoints": 15},
		},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 45 {
		t.Errorf("DominionPoints: want 45, got %d", p.DominionPoints)
	}
}

// A zero-reward objective completes normally but grants no DP.
func TestObjectiveReward_ZeroRewardGrantsNoDP(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardDominionPoints": 0}},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 0 {
		t.Errorf("DominionPoints: want 0, got %d", p.DominionPoints)
	}
	if got := p.CompletedCampaignObjectives["forest/forest_01"]; len(got) != 1 || got[0] != "clear_camps" {
		t.Errorf("objective should still be recorded, got %v", got)
	}
}

// The legacy objectiveIds body shape still records completions (and grants no
// DP, since it carries no reward data).
func TestObjectiveReward_LegacyObjectiveIDsStillWork(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"clear_camps"},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 0 {
		t.Errorf("legacy shape should grant no DP, got %d", p.DominionPoints)
	}
	if got := p.CompletedCampaignObjectives["forest/forest_01"]; len(got) != 1 {
		t.Errorf("legacy shape should still record completion, got %v", got)
	}
}
