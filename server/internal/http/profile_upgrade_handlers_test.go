package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"webrts/server/internal/profile"
)

const (
	testPlayerID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

// newTestMux creates an http.ServeMux with profile routes registered and a
// fresh in-memory profile manager.
func newTestMux(t *testing.T) (*http.ServeMux, *profile.Manager) {
	t.Helper()
	dir := t.TempDir()
	pm := profile.NewManager(dir)
	mux := http.NewServeMux()
	registerProfileRoutes(mux, pm)
	return mux, pm
}

// postJSON is a helper to send a POST with JSON body and X-Player-ID header.
func postJSON(t *testing.T, mux http.Handler, path, playerID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Player-ID", playerID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// getRequest sends a GET with X-Player-ID header.
func getRequest(t *testing.T, mux http.Handler, path, playerID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if playerID != "" {
		req.Header.Set("X-Player-ID", playerID)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// seedPlayer creates a profile with the given legend points and upgrade ranks
// via the manager, returning the player ID.
func seedPlayer(t *testing.T, pm *profile.Manager, legendPoints int, ranks map[string]int) {
	t.Helper()
	_, err := pm.GetOrCreate(testPlayerID, profile.DefaultCommanderID)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	err = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.LegendPoints = legendPoints
		if ranks != nil {
			for k, v := range ranks {
				p.OwnedUpgradeRanks[k] = v
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked to seed player: %v", err)
	}
}

// ─── GET /api/catalog/profile-upgrades ───────────────────────────────────────

// TestCatalogProfileUpgrades_ReturnsThreeUpgrades verifies the catalog endpoint
// returns the three initial upgrades.
func TestCatalogProfileUpgrades_ReturnsThreeUpgrades(t *testing.T) {
	mux, _ := newTestMux(t)
	rec := getRequest(t, mux, "/api/catalog/profile-upgrades", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var resp struct {
		Upgrades []struct {
			ID string `json:"id"`
		} `json:"upgrades"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Upgrades) < 3 {
		t.Fatalf("want at least 3 upgrades, got %d", len(resp.Upgrades))
	}
	ids := make(map[string]bool)
	for _, u := range resp.Upgrades {
		ids[u.ID] = true
	}
	for _, want := range []string{"additional_worker", "physical_power", "magic_power"} {
		if !ids[want] {
			t.Errorf("upgrade %q missing from catalog response", want)
		}
	}
}

// ─── GET /api/profile ─────────────────────────────────────────────────────────

// TestGetProfile_IncludesUpgradeCatalogAndOwnedRanks verifies the profile
// endpoint returns both profileUpgradeCatalog and profile.ownedUpgradeRanks.
func TestGetProfile_IncludesUpgradeCatalogAndOwnedRanks(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := getRequest(t, mux, "/api/profile", testPlayerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["profileUpgradeCatalog"]; !ok {
		t.Error("response missing 'profileUpgradeCatalog' key")
	}
	if _, ok := resp["profile"]; !ok {
		t.Fatal("response missing 'profile' key")
	}
	var p profile.PlayerProfile
	if err := json.Unmarshal(resp["profile"], &p); err != nil {
		t.Fatalf("unmarshal profile: %v", err)
	}
	if p.OwnedUpgradeRanks == nil {
		t.Error("profile.ownedUpgradeRanks must be non-nil")
	}
}

// ─── POST /api/profile/upgrades/purchase ─────────────────────────────────────

// TestPurchase_Success_DebitsExactCostAndIncrementsRank verifies a successful
// first rank purchase of additional_worker debits 25 LP and sets rank to 1.
func TestPurchase_Success_DebitsExactCostAndIncrementsRank(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 25, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/purchase", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if p.LegendPoints != 0 {
		t.Errorf("LegendPoints: want 0, got %d", p.LegendPoints)
	}
	if p.OwnedUpgradeRanks["additional_worker"] != 1 {
		t.Errorf("rank: want 1, got %d", p.OwnedUpgradeRanks["additional_worker"])
	}
}

// TestPurchase_InsufficientPoints_Returns400 verifies that a purchase with
// insufficient LP returns 400 with error "insufficient_legend_points" and does
// not mutate the profile.
func TestPurchase_InsufficientPoints_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	// Seed rank 1 (so rank 2 costs 100 LP), but only give 99 LP.
	seedPlayer(t, pm, 99, map[string]int{"additional_worker": 1})

	rec := postJSON(t, mux, "/api/profile/upgrades/purchase", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "insufficient_legend_points" {
		t.Errorf("error: want %q, got %q", "insufficient_legend_points", body["error"])
	}
	// Verify no mutation.
	p, _ := pm.Get(testPlayerID)
	if p.LegendPoints != 99 {
		t.Errorf("LegendPoints should be unchanged (99), got %d", p.LegendPoints)
	}
	if p.OwnedUpgradeRanks["additional_worker"] != 1 {
		t.Errorf("rank should be unchanged (1), got %d", p.OwnedUpgradeRanks["additional_worker"])
	}
}

// TestPurchase_MaxRankReached_Returns400 verifies that purchasing beyond
// maxRanks returns 400 with error "max_rank_reached".
func TestPurchase_MaxRankReached_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	// additional_worker maxRanks=2; seed at rank 2.
	seedPlayer(t, pm, 1000, map[string]int{"additional_worker": 2})

	rec := postJSON(t, mux, "/api/profile/upgrades/purchase", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "max_rank_reached" {
		t.Errorf("error: want %q, got %q", "max_rank_reached", body["error"])
	}
}

// TestPurchase_UnknownUpgrade_Returns400 verifies that a purchase with an
// unknown upgrade ID returns 400 with error "unknown_upgrade".
func TestPurchase_UnknownUpgrade_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 1000, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/purchase", testPlayerID, map[string]string{
		"upgradeId": "not_a_real_upgrade",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unknown_upgrade" {
		t.Errorf("error: want %q, got %q", "unknown_upgrade", body["error"])
	}
}

// ─── POST /api/profile/upgrades/refund ───────────────────────────────────────

// TestRefund_Success_RefundsExactCostOfLastRank verifies that refunding
// additional_worker at rank 2 returns 100 LP and decrements rank to 1.
func TestRefund_Success_RefundsExactCostOfLastRank(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, map[string]int{"additional_worker": 2})

	rec := postJSON(t, mux, "/api/profile/upgrades/refund", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if p.LegendPoints != 100 {
		t.Errorf("LegendPoints: want 100 (refund of rank-2 cost), got %d", p.LegendPoints)
	}
	if p.OwnedUpgradeRanks["additional_worker"] != 1 {
		t.Errorf("rank: want 1, got %d", p.OwnedUpgradeRanks["additional_worker"])
	}
}

// TestRefund_NotOwned_Returns400 verifies that refunding an upgrade with rank 0
// returns 400 with error "not_owned".
func TestRefund_NotOwned_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/refund", testPlayerID, map[string]string{
		"upgradeId": "physical_power",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "not_owned" {
		t.Errorf("error: want %q, got %q", "not_owned", body["error"])
	}
}

// TestRefund_DoesNotCreditLifetimeLegendPoints verifies that refunding a rank
// does not change LifetimeLegendPoints.
func TestRefund_DoesNotCreditLifetimeLegendPoints(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, map[string]int{"additional_worker": 1})

	// Set a known lifetime value.
	_ = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.LifetimeLegendPoints = 999
		return nil
	})

	rec := postJSON(t, mux, "/api/profile/upgrades/refund", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if p.LifetimeLegendPoints != 999 {
		t.Errorf("LifetimeLegendPoints should be unchanged (999), got %d", p.LifetimeLegendPoints)
	}
}

// TestRefund_UnknownUpgrade_Returns400 verifies refund of unknown upgrade ID
// returns 400 with error "unknown_upgrade".
func TestRefund_UnknownUpgrade_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/refund", testPlayerID, map[string]string{
		"upgradeId": "not_a_real_upgrade",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unknown_upgrade" {
		t.Errorf("error: want %q, got %q", "unknown_upgrade", body["error"])
	}
}

// ─── POST /api/profile/upgrades/toggle ───────────────────────────────────────

// TestToggle_ToggleOn_Success verifies toggling an owned upgrade to active
// adds it to ActiveUpgradeIDs and returns 200 with the updated profile.
func TestToggle_ToggleOn_Success(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, map[string]int{"additional_worker": 1})

	// First toggle off so we can toggle back on.
	_ = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.ActiveUpgradeIDs = []string{}
		return nil
	})

	rec := postJSON(t, mux, "/api/profile/upgrades/toggle", testPlayerID, map[string]any{
		"upgradeId": "additional_worker",
		"active":    true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	found := false
	for _, id := range p.ActiveUpgradeIDs {
		if id == "additional_worker" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ActiveUpgradeIDs: want additional_worker to be present, got %v", p.ActiveUpgradeIDs)
	}
}

// TestToggle_ToggleOff_Success verifies toggling an owned upgrade to inactive
// removes it from ActiveUpgradeIDs and returns 200 with the updated profile.
func TestToggle_ToggleOff_Success(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, map[string]int{"additional_worker": 1})

	// Ensure it's active first.
	_ = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.ActiveUpgradeIDs = []string{"additional_worker"}
		return nil
	})

	rec := postJSON(t, mux, "/api/profile/upgrades/toggle", testPlayerID, map[string]any{
		"upgradeId": "additional_worker",
		"active":    false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, id := range p.ActiveUpgradeIDs {
		if id == "additional_worker" {
			t.Errorf("ActiveUpgradeIDs: additional_worker should be absent after toggle off, got %v", p.ActiveUpgradeIDs)
		}
	}
}

// TestToggle_NotOwned_Returns400 verifies toggling an upgrade the player
// does not own returns 400 with error "not_owned".
func TestToggle_NotOwned_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/toggle", testPlayerID, map[string]any{
		"upgradeId": "physical_power",
		"active":    true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "not_owned" {
		t.Errorf("error: want %q, got %q", "not_owned", body["error"])
	}
}

// TestToggle_UnknownUpgrade_Returns400 verifies toggling an unknown upgrade ID
// returns 400 with error "unknown_upgrade".
func TestToggle_UnknownUpgrade_Returns400(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/toggle", testPlayerID, map[string]any{
		"upgradeId": "does_not_exist",
		"active":    true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unknown_upgrade" {
		t.Errorf("error: want %q, got %q", "unknown_upgrade", body["error"])
	}
}

// TestPurchase_AutoActivatesOnFirstRank verifies that purchasing the first rank
// of an upgrade adds it to ActiveUpgradeIDs automatically.
func TestPurchase_AutoActivatesOnFirstRank(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 25, nil)

	rec := postJSON(t, mux, "/api/profile/upgrades/purchase", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	found := false
	for _, id := range p.ActiveUpgradeIDs {
		if id == "additional_worker" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ActiveUpgradeIDs: want additional_worker auto-activated on first purchase, got %v", p.ActiveUpgradeIDs)
	}
}

// TestRefund_AutoDeactivatesOnFullRefund verifies that refunding to rank 0
// removes the upgrade from ActiveUpgradeIDs automatically.
func TestRefund_AutoDeactivatesOnFullRefund(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, map[string]int{"additional_worker": 1})

	// Ensure it's active.
	_ = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.ActiveUpgradeIDs = []string{"additional_worker"}
		return nil
	})

	rec := postJSON(t, mux, "/api/profile/upgrades/refund", testPlayerID, map[string]string{
		"upgradeId": "additional_worker",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var p profile.PlayerProfile
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, id := range p.ActiveUpgradeIDs {
		if id == "additional_worker" {
			t.Errorf("ActiveUpgradeIDs: additional_worker should be removed after full refund, got %v", p.ActiveUpgradeIDs)
		}
	}
}
