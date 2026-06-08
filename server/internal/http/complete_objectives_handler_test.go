package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"webrts/server/internal/profile"
)

const completeObjectivesPath = "/api/profile/campaign/complete-objectives"

// readProfileBody decodes the response body into a PlayerProfile. Tests use
// this to read the canonical state right back off the wire instead of
// asking the manager (which would skip JSON round-trip validation).
func readProfileBody(t *testing.T, rec *httptest.ResponseRecorder) profile.PlayerProfile {
	t.Helper()
	var p profile.PlayerProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal response: %v\nbody=%s", err, rec.Body.String())
	}
	return p
}

// TestCompleteObjectives_FirstCallRecordsIDs verifies the happy path:
// merging objective IDs into the profile's CompletedCampaignObjectives map
// produces a sorted-deduped set under the "<campaignId>/<levelId>" key.
func TestCompleteObjectives_FirstCallRecordsIDs(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"clear_t1_camps", "build_barracks"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	got := readProfileBody(t, rec).CompletedCampaignObjectives["forest/forest_01"]
	want := []string{"build_barracks", "clear_t1_camps"} // sorted alphabetically
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CompletedCampaignObjectives[forest/forest_01]: want %v, got %v", want, got)
	}
}

// TestCompleteObjectives_IdempotentRepeat verifies that re-posting the same
// objective IDs is a no-op (no duplicates, no reordering). The contract
// matters because the recap overlay's dismiss handler may fire twice
// without warning if the user double-clicks Return to Menu.
func TestCompleteObjectives_IdempotentRepeat(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	body := map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"clear_t1_camps", "build_barracks"},
	}

	first := postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	second := postJSON(t, mux, completeObjectivesPath, testPlayerID, body)

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses: want 200/200, got %d/%d", first.Code, second.Code)
	}
	firstGot := readProfileBody(t, first).CompletedCampaignObjectives["forest/forest_01"]
	secondGot := readProfileBody(t, second).CompletedCampaignObjectives["forest/forest_01"]
	if !reflect.DeepEqual(firstGot, secondGot) {
		t.Errorf("repeat call mutated state: first=%v second=%v", firstGot, secondGot)
	}
}

// TestCompleteObjectives_MergesNewWithExisting verifies that a second call
// with disjoint IDs merges the new entries into the existing set without
// clobbering. End-of-round writes should be additive across replays.
func TestCompleteObjectives_MergesNewWithExisting(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	first := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"build_barracks"},
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first call status: want 200, got %d", first.Code)
	}

	second := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"clear_t1_camps", "rank_units"},
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second call status: want 200, got %d", second.Code)
	}

	got := readProfileBody(t, second).CompletedCampaignObjectives["forest/forest_01"]
	want := []string{"build_barracks", "clear_t1_camps", "rank_units"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged set: want %v, got %v", want, got)
	}
}

// TestCompleteObjectives_EmptyArrayIsValidNoop verifies that a POST with an
// empty objectiveIds array returns 200 without changing state. This is the
// "defeat with no completions" case — the client always POSTs at match end
// to keep the recap-dismiss flow uniform.
func TestCompleteObjectives_EmptyArrayIsValidNoop(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200 for empty objectiveIds, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	got := readProfileBody(t, rec).CompletedCampaignObjectives["forest/forest_01"]
	if len(got) != 0 {
		t.Errorf("empty POST should leave the set empty, got %v", got)
	}
}

// TestCompleteObjectives_KeyedByCampaignAndLevel verifies the storage key is
// "<campaignId>/<levelId>" — writing two different levels writes two
// independent entries.
func TestCompleteObjectives_KeyedByCampaignAndLevel(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"a"},
	})
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_02",
		"objectiveIds": []string{"b"},
	})

	p := readProfileBody(t, rec)
	if got := p.CompletedCampaignObjectives["forest/forest_01"]; !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("forest/forest_01: want [a], got %v", got)
	}
	if got := p.CompletedCampaignObjectives["forest/forest_02"]; !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("forest/forest_02: want [b], got %v", got)
	}
}

// TestCompleteObjectives_MissingPlayerIDRejected guards the auth check: a
// request without the X-Player-ID header must fail. The extractPlayerID
// helper already does this for every endpoint; we test it here so a
// future refactor that bypasses extractPlayerID fails loudly.
func TestCompleteObjectives_MissingPlayerIDRejected(t *testing.T) {
	mux, _ := newTestMux(t)

	body, _ := json.Marshal(map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"a"},
	})
	req := httptest.NewRequest(http.MethodPost, completeObjectivesPath, nil)
	req.Body = nil
	req2 := httptest.NewRequest(http.MethodPost, completeObjectivesPath, nil)
	req2.Body = nil
	_ = req
	_ = body

	// Simpler form: build a single request without the header.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, completeObjectivesPath, nil)
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 for missing X-Player-ID, got %d", rec.Code)
	}
}

// TestCompleteObjectives_MissingCampaignIDRejected verifies the empty-string
// validation on campaignId. Same expected error code (400) and error body
// shape as other handlers.
func TestCompleteObjectives_MissingCampaignIDRejected(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "",
		"levelId":      "forest_01",
		"objectiveIds": []string{"a"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 for empty campaignId, got %d", rec.Code)
	}
}

// TestCompleteObjectives_MissingLevelIDRejected verifies the matching check
// on levelId.
func TestCompleteObjectives_MissingLevelIDRejected(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "",
		"objectiveIds": []string{"a"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 for empty levelId, got %d", rec.Code)
	}
}

// TestCompleteObjectives_InvalidJSONRejected pins the 400 response for a
// malformed body. The handler must not panic / crash the server.
func TestCompleteObjectives_InvalidJSONRejected(t *testing.T) {
	mux, _ := newTestMux(t)

	req := httptest.NewRequest(http.MethodPost, completeObjectivesPath, nil)
	req.Header.Set("X-Player-ID", testPlayerID)
	req.Body = http.NoBody
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 for missing body, got %d", rec.Code)
	}
}

// TestCompleteObjectives_GETMethodNotAllowed sanity-checks the method gate.
func TestCompleteObjectives_GETMethodNotAllowed(t *testing.T) {
	mux, _ := newTestMux(t)

	rec := getRequest(t, mux, completeObjectivesPath, testPlayerID)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status: want 405, got %d", rec.Code)
	}
}
