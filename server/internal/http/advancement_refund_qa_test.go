package httpserver

// QA tests for the Unit Advancements refund-on-cost-change feature.
//
// The implementer called out that refundStaleAdvancementCosts runs inside
// every GET /api/profile call. These tests verify:
//
//   A. Delta refund when catalog cost shrinks (costPaid > current cost).
//   B. No refund when costPaid == current cost (idempotent).
//   C. Full refund + removal when advancement no longer exists in catalog.
//   D. GET /api/profile triggers the refund automatically (integration).
//
// Because we cannot override the embed-FS-backed catalog in tests, tests for
// "cost decreased" and "advancement removed" are exercised directly against
// refundStaleAdvancementCosts by crafting profile.AcquiredAdvancement entries
// with CostPaid values that differ from the real catalog cost.

import (
	"encoding/json"
	"net/http"
	"testing"

	"webrts/server/internal/profile"
)

// ─── A. Delta refund on cost decrease ────────────────────────────────────────

// TestRefundStaleAdvancementCosts_DeltaRefund verifies that when CostPaid
// exceeds the current catalog cost, the player receives the delta as a refund
// and CostPaid is updated to the current cost.
//
// We simulate a "cost decreased from 80 to 50" scenario by seeding CostPaid=80
// for soldier_hp_1 (which has catalog cost 50). The refund should be 30 LP.
func TestRefundStaleAdvancementCosts_DeltaRefund(t *testing.T) {
	const originalCostPaid = 80  // inflated to simulate "past purchase at higher cost"
	const catalogCost = 50       // real cost from catalog
	const expectedRefund = originalCostPaid - catalogCost

	p := &profile.PlayerProfile{
		LegendPoints: 0,
		AcquiredAdvancements: []profile.AcquiredAdvancement{
			{ID: "soldier_hp_1", CostPaid: originalCostPaid},
		},
	}

	modified := refundStaleAdvancementCosts(p)

	if !modified {
		t.Error("refundStaleAdvancementCosts: want modified=true for cost-decreased advancement")
	}
	if p.LegendPoints != expectedRefund {
		t.Errorf("LegendPoints after delta refund: want %d, got %d", expectedRefund, p.LegendPoints)
	}
	if len(p.AcquiredAdvancements) != 1 {
		t.Fatalf("AcquiredAdvancements: want 1 entry retained, got %d", len(p.AcquiredAdvancements))
	}
	if p.AcquiredAdvancements[0].CostPaid != catalogCost {
		t.Errorf("AcquiredAdvancements[0].CostPaid after refund: want %d (catalog cost), got %d",
			catalogCost, p.AcquiredAdvancements[0].CostPaid)
	}
}

// ─── B. No refund when cost is unchanged ─────────────────────────────────────

// TestRefundStaleAdvancementCosts_NoRefundWhenCostUnchanged verifies that when
// CostPaid equals the current catalog cost, no modification occurs.
func TestRefundStaleAdvancementCosts_NoRefundWhenCostUnchanged(t *testing.T) {
	const catalogCost = 50

	p := &profile.PlayerProfile{
		LegendPoints: 100,
		AcquiredAdvancements: []profile.AcquiredAdvancement{
			{ID: "soldier_hp_1", CostPaid: catalogCost},
		},
	}

	modified := refundStaleAdvancementCosts(p)

	if modified {
		t.Error("refundStaleAdvancementCosts: want modified=false when cost is unchanged")
	}
	if p.LegendPoints != 100 {
		t.Errorf("LegendPoints: should be unchanged at 100, got %d", p.LegendPoints)
	}
	if p.AcquiredAdvancements[0].CostPaid != catalogCost {
		t.Errorf("CostPaid: should be unchanged at %d, got %d", catalogCost, p.AcquiredAdvancements[0].CostPaid)
	}
}

// ─── C. Full refund + removal when advancement removed from catalog ───────────

// TestRefundStaleAdvancementCosts_FullRefundWhenRemoved verifies that when an
// advancement ID is no longer in the catalog, the player receives a full refund
// of CostPaid and the entry is removed from AcquiredAdvancements.
func TestRefundStaleAdvancementCosts_FullRefundWhenRemoved(t *testing.T) {
	const paidForRemovedNode = 75

	p := &profile.PlayerProfile{
		LegendPoints: 10,
		AcquiredAdvancements: []profile.AcquiredAdvancement{
			// This ID is deliberately not in any real catalog.
			{ID: "advancement_that_was_removed_from_catalog", CostPaid: paidForRemovedNode},
		},
	}

	modified := refundStaleAdvancementCosts(p)

	if !modified {
		t.Error("refundStaleAdvancementCosts: want modified=true when advancement removed from catalog")
	}
	wantPoints := 10 + paidForRemovedNode
	if p.LegendPoints != wantPoints {
		t.Errorf("LegendPoints after full refund: want %d (10 + %d), got %d",
			wantPoints, paidForRemovedNode, p.LegendPoints)
	}
	if len(p.AcquiredAdvancements) != 0 {
		t.Errorf("AcquiredAdvancements: want 0 entries after removal, got %d", len(p.AcquiredAdvancements))
	}
}

// TestRefundStaleAdvancementCosts_MixedPortfolio verifies that when a player
// owns multiple advancements — one valid, one removed — only the removed one
// is refunded and dropped, while the valid one is retained.
func TestRefundStaleAdvancementCosts_MixedPortfolio(t *testing.T) {
	p := &profile.PlayerProfile{
		LegendPoints: 0,
		AcquiredAdvancements: []profile.AcquiredAdvancement{
			{ID: "soldier_hp_1", CostPaid: 50},                             // valid, no refund
			{ID: "ghost_advancement_not_in_catalog", CostPaid: 100},        // removed, full refund
		},
	}

	modified := refundStaleAdvancementCosts(p)

	if !modified {
		t.Error("refundStaleAdvancementCosts: want modified=true when one of two advancements was removed")
	}
	if p.LegendPoints != 100 {
		t.Errorf("LegendPoints: want 100 (refund of removed node), got %d", p.LegendPoints)
	}
	if len(p.AcquiredAdvancements) != 1 {
		t.Fatalf("AcquiredAdvancements: want 1 retained entry, got %d", len(p.AcquiredAdvancements))
	}
	if p.AcquiredAdvancements[0].ID != "soldier_hp_1" {
		t.Errorf("AcquiredAdvancements[0].ID: want %q retained, got %q", "soldier_hp_1", p.AcquiredAdvancements[0].ID)
	}
}

// ─── D. GET /api/profile triggers refund automatically ───────────────────────

// TestGetProfile_RefundOnCostChange_TriggersOnGetProfile verifies that calling
// GET /api/profile when the player has a CostPaid value exceeding the current
// catalog cost causes the refund to be applied and the updated balance to be
// returned in the response body. This exercises the integration path:
// profile_handlers.go calls refundStaleAdvancementCosts inside WithLocked
// before serialising the response.
func TestGetProfile_RefundOnCostChange_TriggersOnGetProfile(t *testing.T) {
	mux, pm := newTestMux(t)

	// Seed a player with soldier_hp_1 purchased at inflated cost (80 instead of 50).
	// This simulates a player who bought before a cost decrease.
	const inflatedCost = 80
	const catalogCost = 50
	const initialLP = 20
	const expectedLP = initialLP + (inflatedCost - catalogCost) // 20 + 30 = 50

	_, err := pm.GetOrCreate(testPlayerID, "nomad_commander_default")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	err = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.LegendPoints = initialLP
		p.AcquiredAdvancements = []profile.AcquiredAdvancement{
			{ID: "soldier_hp_1", CostPaid: inflatedCost},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed WithLocked: %v", err)
	}

	// GET /api/profile must trigger refund-on-cost-change.
	rec := getRequest(t, mux, "/api/profile", testPlayerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/profile status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Profile struct {
			LegendPoints         int `json:"legendPoints"`
			AcquiredAdvancements []struct {
				ID       string `json:"id"`
				CostPaid int    `json:"costPaid"`
			} `json:"acquiredAdvancements"`
		} `json:"profile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Profile.LegendPoints != expectedLP {
		t.Errorf("profile.legendPoints after GET-triggered refund: want %d, got %d",
			expectedLP, resp.Profile.LegendPoints)
	}
	if len(resp.Profile.AcquiredAdvancements) != 1 {
		t.Fatalf("profile.acquiredAdvancements: want 1, got %d", len(resp.Profile.AcquiredAdvancements))
	}
	if resp.Profile.AcquiredAdvancements[0].CostPaid != catalogCost {
		t.Errorf("acquiredAdvancements[0].costPaid after refund: want %d (new catalog cost), got %d",
			catalogCost, resp.Profile.AcquiredAdvancements[0].CostPaid)
	}

	// Verify persistence: second GET must show the same refunded state, not
	// double-refund. The refund must be written back by WithLocked.
	rec2 := getRequest(t, mux, "/api/profile", testPlayerID)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second GET /api/profile status: want 200, got %d", rec2.Code)
	}
	var resp2 struct {
		Profile struct {
			LegendPoints int `json:"legendPoints"`
		} `json:"profile"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.Profile.LegendPoints != expectedLP {
		t.Errorf("legendPoints on second GET (double-refund check): want %d, got %d — possible double-refund",
			expectedLP, resp2.Profile.LegendPoints)
	}
}

// TestGetProfile_FullRefundWhenRemoved_TriggersOnGetProfile verifies that an
// advancement whose ID was removed from the catalog is dropped from
// AcquiredAdvancements and fully refunded when GET /api/profile is called.
func TestGetProfile_FullRefundWhenRemoved_TriggersOnGetProfile(t *testing.T) {
	mux, pm := newTestMux(t)

	const paidForGhost = 60
	const initialLP = 5

	_, err := pm.GetOrCreate(testPlayerID, "nomad_commander_default")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	err = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.LegendPoints = initialLP
		p.AcquiredAdvancements = []profile.AcquiredAdvancement{
			{ID: "advancement_deleted_from_catalog", CostPaid: paidForGhost},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed WithLocked: %v", err)
	}

	rec := getRequest(t, mux, "/api/profile", testPlayerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/profile status: want 200, got %d", rec.Code)
	}

	var resp struct {
		Profile struct {
			LegendPoints         int `json:"legendPoints"`
			AcquiredAdvancements []struct {
				ID string `json:"id"`
			} `json:"acquiredAdvancements"`
		} `json:"profile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wantLP := initialLP + paidForGhost
	if resp.Profile.LegendPoints != wantLP {
		t.Errorf("legendPoints after full refund of removed advancement: want %d, got %d",
			wantLP, resp.Profile.LegendPoints)
	}
	if len(resp.Profile.AcquiredAdvancements) != 0 {
		t.Errorf("acquiredAdvancements: want 0 after removal of ghost entry, got %d",
			len(resp.Profile.AcquiredAdvancements))
	}
}

// ─── E. "In active match" gate ────────────────────────────────────────────────

// TestAdvancementPurchase_PlayerInActiveMatch_Returns409 verifies that the
// purchase endpoint returns 409 "player_in_match" when the match manager
// reports the player is active. Uses a stub that always returns true.
func TestAdvancementPurchase_PlayerInActiveMatch_Returns409(t *testing.T) {
	dir := t.TempDir()
	pm := profile.NewManager(dir)

	mux := http.NewServeMux()
	registerAdvancementRoutes(mux, pm, alwaysInMatchStub{})

	seedAdvancementPlayer(t, pm, 1000, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "soldier_hp_1",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status: want 409 Conflict, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "player_in_match" {
		t.Errorf("error code: want %q, got %q", "player_in_match", body["error"])
	}
}

// alwaysInMatchStub implements matchInActiveChecker and always returns true.
type alwaysInMatchStub struct{}

func (alwaysInMatchStub) IsPlayerInActiveMatch(string) bool { return true }

// TestAdvancementPurchase_PlayerNotInMatch_Succeeds verifies that the purchase
// succeeds when the match manager reports the player is NOT active.
func TestAdvancementPurchase_PlayerNotInMatch_Succeeds(t *testing.T) {
	dir := t.TempDir()
	pm := profile.NewManager(dir)

	mux := http.NewServeMux()
	registerAdvancementRoutes(mux, pm, neverInMatchStub{})

	seedAdvancementPlayer(t, pm, 1000, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "soldier_hp_1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
}

// neverInMatchStub implements matchInActiveChecker and always returns false.
type neverInMatchStub struct{}

func (neverInMatchStub) IsPlayerInActiveMatch(string) bool { return false }
