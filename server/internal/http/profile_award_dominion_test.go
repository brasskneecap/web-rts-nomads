package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"webrts/server/internal/profile"
)

// ─── POST /api/profile/match/award-dominion-points ───────────────────────────

// TestAwardDominionPoints_CreditsOnceThenDedups verifies that posting the same
// matchId twice credits DP exactly once and leaves DominionPoints /
// LifetimeDominionPoints at the first-post value on the second call.
func TestAwardDominionPoints_CreditsOnceThenDedups(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	const matchID = "match-7"
	const amount = 5

	// First POST — should apply the award.
	rec := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"matchId": matchID,
		"amount":  amount,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("first POST: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	// Second POST with identical payload — should be a no-op.
	rec2 := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"matchId": matchID,
		"amount":  amount,
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("second POST: want 200, got %d — body: %s", rec2.Code, rec2.Body.String())
	}

	// Load profile and assert credited exactly once.
	var p *profile.PlayerProfile
	err := pm.WithLocked(testPlayerID, func(prof *profile.PlayerProfile) error {
		p = prof
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked: %v", err)
	}
	if p.DominionPoints != amount {
		t.Errorf("DominionPoints: want %d, got %d", amount, p.DominionPoints)
	}
	if p.LifetimeDominionPoints != amount {
		t.Errorf("LifetimeDominionPoints: want %d, got %d", amount, p.LifetimeDominionPoints)
	}
}

// TestAwardDominionPoints_MissingMatchID_Returns400 verifies that omitting
// matchId produces a 400 with error code "invalid_match_id".
func TestAwardDominionPoints_MissingMatchID_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"amount": 10,
		// matchId intentionally absent
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid_match_id" {
		t.Errorf("error: want %q, got %q", "invalid_match_id", body["error"])
	}
}

// TestAwardDominionPoints_DistinctMatchIDsAccumulateDP verifies that two
// distinct matchIds each credit their respective amounts, so DominionPoints
// accumulates across matches.
func TestAwardDominionPoints_DistinctMatchIDsAccumulateDP(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	first := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"matchId": "match-alpha",
		"amount":  3,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first match POST: want 200, got %d — body: %s", first.Code, first.Body.String())
	}

	second := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"matchId": "match-beta",
		"amount":  7,
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second match POST: want 200, got %d — body: %s", second.Code, second.Body.String())
	}

	var p *profile.PlayerProfile
	_ = pm.WithLocked(testPlayerID, func(prof *profile.PlayerProfile) error {
		p = prof
		return nil
	})
	if p.DominionPoints != 10 {
		t.Errorf("DominionPoints: want 10 (3+7), got %d", p.DominionPoints)
	}
	if p.LifetimeDominionPoints != 10 {
		t.Errorf("LifetimeDominionPoints: want 10 (3+7), got %d", p.LifetimeDominionPoints)
	}
	if len(p.CreditedMatchIDs) != 2 {
		t.Errorf("CreditedMatchIDs: want 2 entries, got %d: %v", len(p.CreditedMatchIDs), p.CreditedMatchIDs)
	}
}

// TestAwardDominionPoints_NegativeAmount_Returns400 verifies that a negative
// amount is rejected with error code "invalid_amount".
func TestAwardDominionPoints_NegativeAmount_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"matchId": "match-neg",
		"amount":  -1,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid_amount" {
		t.Errorf("error: want %q, got %q", "invalid_amount", body["error"])
	}
}

// TestAwardDominionPoints_LedgerTrimsBeyond50_OldestEvicted verifies that the
// CreditedMatchIDs ledger is capped at 50 entries. After posting 51 distinct
// match IDs the slice must contain exactly 50 entries, the oldest ("match-0")
// must have been evicted, the newest ("match-50") must be present, and
// DominionPoints must reflect all 51 credited amounts (eviction from the dedup
// ledger must not reduce the balance).
func TestAwardDominionPoints_LedgerTrimsBeyond50_OldestEvicted(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	const perMatchAmount = 2
	const total = 51

	for i := range total {
		matchID := fmt.Sprintf("match-%d", i)
		rec := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
			"matchId": matchID,
			"amount":  perMatchAmount,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("POST match-%d: want 200, got %d — body: %s", i, rec.Code, rec.Body.String())
		}
	}

	var p *profile.PlayerProfile
	err := pm.WithLocked(testPlayerID, func(prof *profile.PlayerProfile) error {
		p = prof
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked: %v", err)
	}

	if len(p.CreditedMatchIDs) != 50 {
		t.Errorf("CreditedMatchIDs length: want 50, got %d", len(p.CreditedMatchIDs))
	}

	oldestEvicted := true
	newestPresent := false
	for _, id := range p.CreditedMatchIDs {
		if id == "match-0" {
			oldestEvicted = false
		}
		if id == "match-50" {
			newestPresent = true
		}
	}
	if !oldestEvicted {
		t.Errorf("CreditedMatchIDs: expected %q to be evicted, but it is still present: %v", "match-0", p.CreditedMatchIDs)
	}
	if !newestPresent {
		t.Errorf("CreditedMatchIDs: expected %q to be present, but it is missing: %v", "match-50", p.CreditedMatchIDs)
	}

	wantDP := total * perMatchAmount
	if p.DominionPoints != wantDP {
		t.Errorf("DominionPoints: want %d (%d*%d), got %d", wantDP, total, perMatchAmount, p.DominionPoints)
	}
	if p.LifetimeDominionPoints != wantDP {
		t.Errorf("LifetimeDominionPoints: want %d (%d*%d), got %d", wantDP, total, perMatchAmount, p.LifetimeDominionPoints)
	}
}

// TestAwardDominionPoints_ZeroAmount_RecordsMatchButDoesNotCredit verifies
// that amount=0 is accepted (200), registers the matchId in CreditedMatchIDs
// for idempotency, but does not change DominionPoints or LifetimeDominionPoints.
func TestAwardDominionPoints_ZeroAmount_RecordsMatchButDoesNotCredit(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/match/award-dominion-points", testPlayerID, map[string]any{
		"matchId": "match-zero",
		"amount":  0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var p *profile.PlayerProfile
	_ = pm.WithLocked(testPlayerID, func(prof *profile.PlayerProfile) error {
		p = prof
		return nil
	})
	if p.DominionPoints != 0 {
		t.Errorf("DominionPoints: want 0, got %d", p.DominionPoints)
	}
	if p.LifetimeDominionPoints != 0 {
		t.Errorf("LifetimeDominionPoints: want 0, got %d", p.LifetimeDominionPoints)
	}
	// The match should still be recorded so a retry with amount>0 for the
	// same matchId is also a no-op.
	found := false
	for _, id := range p.CreditedMatchIDs {
		if id == "match-zero" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CreditedMatchIDs: want %q present, got %v", "match-zero", p.CreditedMatchIDs)
	}
}
