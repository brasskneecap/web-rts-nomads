package httpserver

// Tests for Conquest Badge spend mechanics on advancement purchase and reset.
//
// The catalog is the live embed-FS catalog, so we discover major/minor node IDs
// and their DP costs at runtime via game.GetAdvancementDef and
// game.ListUnitAdvancementTracks rather than hardcoding them.
//
// Strategy for prereqs: seed the immediate prerequisite directly into the
// profile's AcquiredAdvancements via pm.WithLocked (same pattern used in
// advancement_refund_qa_test.go).

import (
	"encoding/json"
	"net/http"
	"testing"

	"webrts/server/internal/game"
	"webrts/server/internal/profile"
)

// findFirstMajorNodeWithPrereq returns (majorID, prereqID, majorCost).
// It looks for the first "major" node in any track whose immediate prerequisite
// is a "minor" node that has no prerequisite of its own (so we only need to
// seed one prereq entry). Falls back to the first major node found, seeding
// whatever prereq is needed.
func findFirstMajorNode(t *testing.T) (majorID, prereqID string, majorCost int) {
	t.Helper()
	for _, track := range game.ListUnitAdvancementTracks() {
		for i, node := range track.Nodes {
			if node.Kind == "major" {
				major := node
				prereq := ""
				if i > 0 {
					prereq = track.Nodes[i-1].ID
				}
				return major.ID, prereq, major.Cost
			}
		}
	}
	t.Fatal("no major advancement node found in catalog — tests cannot run")
	return "", "", 0
}

// findFirstMinorNodeWithNoPrereq returns the first minor node that has no
// prerequisite (index 0 in some track), so we can test minor purchase without
// any seeding.
func findFirstMinorNodeWithNoPrereq(t *testing.T) (minorID string, minorCost int) {
	t.Helper()
	for _, track := range game.ListUnitAdvancementTracks() {
		if len(track.Nodes) > 0 && track.Nodes[0].Kind == "minor" {
			n := track.Nodes[0]
			return n.ID, n.Cost
		}
	}
	t.Fatal("no minor advancement node at index 0 found in catalog")
	return "", 0
}

// seedAdvancementPlayerWithBadges seeds a profile with dominionPoints, an
// initial ConquestBadges balance, and any pre-owned advancement IDs (seeded at
// their real catalog cost so the refund logic is consistent).
func seedAdvancementPlayerWithBadges(t *testing.T, pm *profile.Manager, dominionPoints, badges int, acquiredIDs []string) {
	t.Helper()
	_, err := pm.GetOrCreate(testPlayerID, profile.DefaultCommanderID)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	err = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.DominionPoints = dominionPoints
		p.ConquestBadges = badges
		for _, id := range acquiredIDs {
			node, ok := game.GetAdvancementDef(id)
			if !ok {
				t.Errorf("seed: advancement %q not found in catalog", id)
				return nil
			}
			p.AcquiredAdvancements = append(p.AcquiredAdvancements, profile.AcquiredAdvancement{
				ID:       id,
				CostPaid: node.Cost,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked seed: %v", err)
	}
}

// purchaseAdvancementResponse is the decoded shape of the purchase endpoint's
// success JSON, extended with the new conquestBadges field.
type purchaseAdvancementResponse struct {
	DominionPoints       int                           `json:"dominionPoints"`
	ConquestBadges       int                           `json:"conquestBadges"`
	AcquiredAdvancements []profile.AcquiredAdvancement `json:"acquiredAdvancements"`
}

// resetAdvancementResponse is the decoded shape of the reset endpoint's
// success JSON, extended with the new conquestBadges field.
type resetAdvancementResponse struct {
	DominionPoints       int                           `json:"dominionPoints"`
	ConquestBadges       int                           `json:"conquestBadges"`
	AcquiredAdvancements []profile.AcquiredAdvancement `json:"acquiredAdvancements"`
}

// ─── Test 1: major node with 0 badges → 400 insufficient_conquest_badges ─────

// TestAdvancementBadge_MajorNode_ZeroBadges_Returns400 verifies that a player
// with enough DP but zero Conquest Badges cannot purchase a "major" advancement.
// Profile state (DP and badges) must be unchanged; the advancement must not be
// recorded as acquired.
func TestAdvancementBadge_MajorNode_ZeroBadges_Returns400(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)

	majorID, prereqID, majorCost := findFirstMajorNode(t)

	var prereqs []string
	if prereqID != "" {
		prereqs = []string{prereqID}
	}
	// Give the player plenty of DP but explicitly 0 badges.
	seedAdvancementPlayerWithBadges(t, pm, majorCost*10, 0, prereqs)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": majorID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 (insufficient badges), got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "insufficient_conquest_badges" {
		t.Errorf("error code: want %q, got %q", "insufficient_conquest_badges", body["error"])
	}

	// Verify profile is completely unchanged.
	p, err := pm.Get(testPlayerID)
	if err != nil {
		t.Fatalf("pm.Get: %v", err)
	}
	if p.ConquestBadges != 0 {
		t.Errorf("ConquestBadges must be unchanged (0), got %d", p.ConquestBadges)
	}
	if p.DominionPoints != majorCost*10 {
		t.Errorf("DominionPoints must be unchanged (%d), got %d", majorCost*10, p.DominionPoints)
	}
	for _, aa := range p.AcquiredAdvancements {
		if aa.ID == majorID {
			t.Errorf("advancement %q must NOT be in AcquiredAdvancements after rejection", majorID)
		}
	}
}

// ─── Test 2: major node with 1 badge + enough DP → 200, badge consumed ───────

// TestAdvancementBadge_MajorNode_OneBadge_Succeeds verifies that a player with
// exactly 1 Conquest Badge and enough DP can purchase a "major" advancement.
// The response must carry conquestBadges == 0 (badge spent), DP debited by
// the node cost, and the AcquiredAdvancement record must have BadgesPaid == 1.
func TestAdvancementBadge_MajorNode_OneBadge_Succeeds(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)

	majorID, prereqID, majorCost := findFirstMajorNode(t)

	var prereqs []string
	if prereqID != "" {
		prereqs = []string{prereqID}
	}
	const startingBadges = 1
	// Give generous DP so cost is never the limiting factor here.
	startingDP := majorCost * 3
	seedAdvancementPlayerWithBadges(t, pm, startingDP, startingBadges, prereqs)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": majorID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp purchaseAdvancementResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Badge must be consumed.
	if resp.ConquestBadges != 0 {
		t.Errorf("conquestBadges in response: want 0 (badge spent), got %d", resp.ConquestBadges)
	}

	// DP must be debited by the node cost.
	wantDP := startingDP - majorCost
	if resp.DominionPoints != wantDP {
		t.Errorf("dominionPoints: want %d (start %d - cost %d), got %d",
			wantDP, startingDP, majorCost, resp.DominionPoints)
	}

	// Find the acquired record for this node.
	var found *profile.AcquiredAdvancement
	for i := range resp.AcquiredAdvancements {
		if resp.AcquiredAdvancements[i].ID == majorID {
			found = &resp.AcquiredAdvancements[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("advancement %q not found in acquiredAdvancements", majorID)
	}
	if found.BadgesPaid != 1 {
		t.Errorf("BadgesPaid on acquired record: want 1, got %d", found.BadgesPaid)
	}
	if found.CostPaid != majorCost {
		t.Errorf("CostPaid on acquired record: want %d, got %d", majorCost, found.CostPaid)
	}
}

// ─── Test 3: minor node with 0 badges → 200 (no badge required) ──────────────

// TestAdvancementBadge_MinorNode_ZeroBadges_Succeeds verifies that "minor"
// advancement nodes can be purchased without any Conquest Badges.
// The acquired record must have BadgesPaid == 0.
func TestAdvancementBadge_MinorNode_ZeroBadges_Succeeds(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)

	minorID, minorCost := findFirstMinorNodeWithNoPrereq(t)

	// Seed with enough DP and explicitly 0 badges.
	startingDP := minorCost * 3
	seedAdvancementPlayerWithBadges(t, pm, startingDP, 0, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": minorID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200 (minor needs no badge), got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp purchaseAdvancementResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Badges must be unchanged (0).
	if resp.ConquestBadges != 0 {
		t.Errorf("conquestBadges: want 0 (unchanged for minor node), got %d", resp.ConquestBadges)
	}

	// DP debited correctly.
	wantDP := startingDP - minorCost
	if resp.DominionPoints != wantDP {
		t.Errorf("dominionPoints: want %d, got %d", wantDP, resp.DominionPoints)
	}

	// Find the acquired record.
	var found *profile.AcquiredAdvancement
	for i := range resp.AcquiredAdvancements {
		if resp.AcquiredAdvancements[i].ID == minorID {
			found = &resp.AcquiredAdvancements[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("advancement %q not found in acquiredAdvancements", minorID)
	}
	if found.BadgesPaid != 0 {
		t.Errorf("BadgesPaid on minor acquired record: want 0, got %d", found.BadgesPaid)
	}
}

// ─── Test 4: reset refunds the consumed badge ─────────────────────────────────

// TestAdvancementBadge_Reset_RefundsBadge verifies that resetting after buying
// a major advancement refunds the Conquest Badge that was consumed.
// Response conquestBadges must return to the pre-purchase balance.
func TestAdvancementBadge_Reset_RefundsBadge(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)

	majorID, prereqID, majorCost := findFirstMajorNode(t)

	var prereqs []string
	if prereqID != "" {
		prereqs = []string{prereqID}
	}
	const startingBadges = 3
	startingDP := majorCost * 5
	seedAdvancementPlayerWithBadges(t, pm, startingDP, startingBadges, prereqs)

	// Purchase the major node (costs 1 badge).
	buyRec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": majorID,
	})
	if buyRec.Code != http.StatusOK {
		t.Fatalf("purchase status: want 200, got %d — body: %s", buyRec.Code, buyRec.Body.String())
	}
	var buyResp purchaseAdvancementResponse
	if err := json.NewDecoder(buyRec.Body).Decode(&buyResp); err != nil {
		t.Fatalf("decode purchase response: %v", err)
	}
	if buyResp.ConquestBadges != startingBadges-1 {
		t.Fatalf("badges after purchase: want %d, got %d", startingBadges-1, buyResp.ConquestBadges)
	}

	// Now reset.
	resetRec := postJSON(t, mux, "/api/profile/advancements/reset", testPlayerID, map[string]string{})
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset status: want 200, got %d — body: %s", resetRec.Code, resetRec.Body.String())
	}

	var resetResp resetAdvancementResponse
	if err := json.NewDecoder(resetRec.Body).Decode(&resetResp); err != nil {
		t.Fatalf("decode reset response: %v", err)
	}

	// Badge must be refunded back to the original balance.
	if resetResp.ConquestBadges != startingBadges {
		t.Errorf("conquestBadges after reset: want %d (refunded), got %d",
			startingBadges, resetResp.ConquestBadges)
	}

	// Acquired list must be empty.
	if len(resetResp.AcquiredAdvancements) != 0 {
		t.Errorf("acquiredAdvancements after reset: want empty, got %d", len(resetResp.AcquiredAdvancements))
	}
}

// ─── S5: POST /api/profile/dev/grant-conquest-badges ─────────────────────────

// TestDevGrantConquestBadges_Succeeds verifies that the dev grant endpoint
// credits the specified number of badges to the profile and returns the
// updated profile.
func TestDevGrantConquestBadges_Succeeds(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/dev/grant-conquest-badges", testPlayerID, map[string]int{
		"amount": 5,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	p := readProfileBody(t, rec)
	if p.ConquestBadges != 5 {
		t.Errorf("ConquestBadges: want 5, got %d", p.ConquestBadges)
	}
}

// TestDevGrantConquestBadges_Accumulates verifies that multiple grant calls
// accumulate (additive), not replace.
func TestDevGrantConquestBadges_Accumulates(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	postJSON(t, mux, "/api/profile/dev/grant-conquest-badges", testPlayerID, map[string]int{"amount": 3})
	rec := postJSON(t, mux, "/api/profile/dev/grant-conquest-badges", testPlayerID, map[string]int{"amount": 2})

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	p := readProfileBody(t, rec)
	if p.ConquestBadges != 5 {
		t.Errorf("ConquestBadges: want 5 (3+2), got %d", p.ConquestBadges)
	}
}

// TestDevGrantConquestBadges_ZeroAmountRejected verifies that amount <= 0
// returns 400 "invalid_amount".
func TestDevGrantConquestBadges_ZeroAmountRejected(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/dev/grant-conquest-badges", testPlayerID, map[string]int{"amount": 0})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid_amount" {
		t.Errorf("error code: want %q, got %q", "invalid_amount", body["error"])
	}
}

// TestDevGrantConquestBadges_NegativeAmountRejected verifies that negative
// amounts return 400 "invalid_amount".
func TestDevGrantConquestBadges_NegativeAmountRejected(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/dev/grant-conquest-badges", testPlayerID, map[string]int{"amount": -10})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "invalid_amount" {
		t.Errorf("error code: want %q, got %q", "invalid_amount", body["error"])
	}
}

// TestDevGrantConquestBadges_WrongMethod verifies GET is rejected.
func TestDevGrantConquestBadges_WrongMethod(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := getRequest(t, mux, "/api/profile/dev/grant-conquest-badges", testPlayerID)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status: want 405, got %d", rec.Code)
	}
}
