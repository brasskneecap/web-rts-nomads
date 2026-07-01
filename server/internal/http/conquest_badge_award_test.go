package httpserver

import (
	"net/http"
	"testing"
)

func TestConquestBadge_FirstCompletionAwards(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest", "levelId": "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardConquestBadges": 2},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if p := readProfileBody(t, rec); p.ConquestBadges != 2 {
		t.Errorf("ConquestBadges: want 2, got %d", p.ConquestBadges)
	}
}

func TestConquestBadge_RepeatAwardsNothing(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)
	body := map[string]any{
		"campaignId": "forest", "levelId": "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardConquestBadges": 2}},
	}
	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	second := postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	if p := readProfileBody(t, second); p.ConquestBadges != 2 {
		t.Errorf("ConquestBadges after repeat: want 2, got %d", p.ConquestBadges)
	}
}

func TestConquestBadge_CoexistsWithDominionReward(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest", "levelId": "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardDominionPoints": 30, "rewardConquestBadges": 1},
		},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 30 || p.ConquestBadges != 1 {
		t.Errorf("want DP=30 badges=1, got DP=%d badges=%d", p.DominionPoints, p.ConquestBadges)
	}
}
