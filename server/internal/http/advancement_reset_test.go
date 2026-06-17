package httpserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"webrts/server/internal/profile"
)

// resetResponseBody is the decoded shape of the reset endpoint's JSON.
type resetResponseBody struct {
	DominionPoints       int                           `json:"dominionPoints"`
	AcquiredAdvancements []profile.AcquiredAdvancement `json:"acquiredAdvancements"`
}

// TestAdvancementReset_RefundsAndClears verifies a reset refunds every acquired
// advancement's paid cost and empties the acquired list.
func TestAdvancementReset_RefundsAndClears(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	// seedAdvancementPlayer records CostPaid: 50 per acquired ID.
	seedAdvancementPlayer(t, pm, 50, []string{"soldier_hp_1", "soldier_armor_1"})

	rec := postJSON(t, mux, "/api/profile/advancements/reset", testPlayerID, map[string]string{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp resetResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// 50 starting + 2 × 50 refunded = 150.
	if resp.DominionPoints != 150 {
		t.Errorf("dominionPoints after reset: want 150 (50 + 2×50 refund), got %d", resp.DominionPoints)
	}
	if len(resp.AcquiredAdvancements) != 0 {
		t.Errorf("acquiredAdvancements after reset: want empty, got %d", len(resp.AcquiredAdvancements))
	}

	// A second reset is a clean no-op (nothing left to refund).
	rec2 := postJSON(t, mux, "/api/profile/advancements/reset", testPlayerID, map[string]string{})
	if rec2.Code != http.StatusOK {
		t.Fatalf("second reset status: want 200, got %d", rec2.Code)
	}
	var resp2 resetResponseBody
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.DominionPoints != 150 {
		t.Errorf("dominionPoints after second reset: want 150 (no double refund), got %d", resp2.DominionPoints)
	}
}

// TestAdvancementReset_NoAcquired_NoChange verifies resetting with nothing
// acquired returns the points unchanged and an empty list.
func TestAdvancementReset_NoAcquired_NoChange(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 100, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/reset", testPlayerID, map[string]string{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var resp resetResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DominionPoints != 100 {
		t.Errorf("dominionPoints: want 100 (unchanged), got %d", resp.DominionPoints)
	}
	if len(resp.AcquiredAdvancements) != 0 {
		t.Errorf("acquiredAdvancements: want empty, got %d", len(resp.AcquiredAdvancements))
	}
}

// TestAdvancementReset_InActiveMatch_Returns409 verifies the same "not in active
// match" guard the purchase endpoint enforces.
func TestAdvancementReset_InActiveMatch_Returns409(t *testing.T) {
	dir := t.TempDir()
	pm := profile.NewManager(dir)
	mux := http.NewServeMux()
	registerAdvancementRoutes(mux, pm, alwaysInMatchStub{})
	seedAdvancementPlayer(t, pm, 50, []string{"soldier_hp_1"})

	rec := postJSON(t, mux, "/api/profile/advancements/reset", testPlayerID, map[string]string{})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: want 409 (player_in_match), got %d — body: %s", rec.Code, rec.Body.String())
	}
}

// TestAdvancementReset_GetMethod_NotAllowed verifies non-POST is rejected.
func TestAdvancementReset_GetMethod_NotAllowed(t *testing.T) {
	mux, _ := newAdvancementTestMux(t)
	rec := getRequest(t, mux, "/api/profile/advancements/reset", testPlayerID)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET reset: want 405, got %d", rec.Code)
	}
}
