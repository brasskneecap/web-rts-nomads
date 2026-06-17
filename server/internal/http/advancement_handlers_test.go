package httpserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"webrts/server/internal/profile"
)

// newAdvancementTestMux creates a ServeMux with advancement purchase routes
// registered and a fresh in-memory profile manager.
func newAdvancementTestMux(t *testing.T) (*http.ServeMux, *profile.Manager) {
	t.Helper()
	dir := t.TempDir()
	pm := profile.NewManager(dir)
	mux := http.NewServeMux()
	registerAdvancementRoutes(mux, pm, nil /* no match manager for unit tests */)
	return mux, pm
}

// seedAdvancementPlayer seeds a profile with dominionPoints and the given
// acquired advancement IDs at their catalog cost.
func seedAdvancementPlayer(t *testing.T, pm *profile.Manager, dominionPoints int, acquiredIDs []string) {
	t.Helper()
	_, err := pm.GetOrCreate(testPlayerID, profile.DefaultCommanderID)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	err = pm.WithLocked(testPlayerID, func(p *profile.PlayerProfile) error {
		p.DominionPoints = dominionPoints
		for _, id := range acquiredIDs {
			p.AcquiredAdvancements = append(p.AcquiredAdvancements, profile.AcquiredAdvancement{
				ID:       id,
				CostPaid: 50, // known cost for soldier_hp_1 in test catalog
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLocked seed: %v", err)
	}
}

// ─── POST /api/profile/advancements/purchase ─────────────────────────────────

// TestAdvancementPurchase_Success_DebitsPointsAndReturnsSlimResponse verifies
// a successful purchase debits dominion points and returns only dominionPoints and
// acquiredAdvancements (not the full profile).
func TestAdvancementPurchase_Success_DebitsPointsAndReturnsSlimResponse(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 100, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "soldier_hp_1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		DominionPoints       int                           `json:"dominionPoints"`
		AcquiredAdvancements []profile.AcquiredAdvancement `json:"acquiredAdvancements"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DominionPoints != 50 {
		t.Errorf("dominionPoints: want 50 (100 - 50 cost), got %d", resp.DominionPoints)
	}
	if len(resp.AcquiredAdvancements) != 1 {
		t.Fatalf("acquiredAdvancements: want 1, got %d", len(resp.AcquiredAdvancements))
	}
	if resp.AcquiredAdvancements[0].ID != "soldier_hp_1" {
		t.Errorf("acquiredAdvancements[0].id: want %q, got %q", "soldier_hp_1", resp.AcquiredAdvancements[0].ID)
	}
}

// TestAdvancementPurchase_ResponseHasNoProfileKey verifies the purchase response
// does NOT include a top-level "playerId" or "version" field (i.e. it is not
// the full PlayerProfile struct).
func TestAdvancementPurchase_ResponseHasNoProfileKey(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 100, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "soldier_hp_1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, forbidden := range []string{"playerId", "version", "stats", "ownedUpgradeRanks"} {
		if _, present := raw[forbidden]; present {
			t.Errorf("response must not contain %q key (full profile leak)", forbidden)
		}
	}
	for _, required := range []string{"dominionPoints", "acquiredAdvancements"} {
		if _, present := raw[required]; !present {
			t.Errorf("response must contain %q key", required)
		}
	}
}

// TestAdvancementPurchase_AlreadyAcquired_Returns400WithAlreadyAcquiredCode
// verifies that purchasing a node twice returns 400 with code "already_acquired".
func TestAdvancementPurchase_AlreadyAcquired_Returns400WithAlreadyAcquiredCode(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 1000, []string{"soldier_hp_1"})

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "soldier_hp_1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "already_acquired" {
		t.Errorf("error code: want %q, got %q", "already_acquired", body["error"])
	}
}

// TestAdvancementPurchase_InsufficientPoints_Returns400 verifies that a
// purchase with insufficient DP returns 400 with "insufficient_dominion_points".
func TestAdvancementPurchase_InsufficientPoints_Returns400(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 10 /* < 50 cost */, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "soldier_hp_1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "insufficient_dominion_points" {
		t.Errorf("error code: want %q, got %q", "insufficient_dominion_points", body["error"])
	}
	// Profile must be unchanged.
	p, _ := pm.Get(testPlayerID)
	if p.DominionPoints != 10 {
		t.Errorf("dominion points should be unchanged (10), got %d", p.DominionPoints)
	}
}

// TestAdvancementPurchase_UnknownAdvancement_Returns400 verifies that an
// unknown advancement ID returns 400 with "unknown_advancement".
func TestAdvancementPurchase_UnknownAdvancement_Returns400(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 1000, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "does_not_exist",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unknown_advancement" {
		t.Errorf("error code: want %q, got %q", "unknown_advancement", body["error"])
	}
}

// TestAdvancementPurchase_MissingID_Returns400 verifies that an empty
// advancementId returns 400 with "missing_advancement_id".
func TestAdvancementPurchase_MissingID_Returns400(t *testing.T) {
	mux, pm := newAdvancementTestMux(t)
	seedAdvancementPlayer(t, pm, 1000, nil)

	rec := postJSON(t, mux, "/api/profile/advancements/purchase", testPlayerID, map[string]string{
		"advancementId": "",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "missing_advancement_id" {
		t.Errorf("error code: want %q, got %q", "missing_advancement_id", body["error"])
	}
}

// ─── Advancement catalog shape via GET /api/profile ──────────────────────────

// TestGetProfile_AdvancementCatalogIsTracksNotFlatSlice verifies that the
// advancementCatalog field in GET /api/profile is []UnitAdvancementTrack (each
// with unitType and nodes), not a flat []UnitAdvancementNode.
func TestGetProfile_AdvancementCatalogIsTracksNotFlatSlice(t *testing.T) {
	mux, pm := newTestMux(t) // registerProfileRoutes, which includes advancementCatalog
	seedPlayer(t, pm, 0, nil)

	rec := getRequest(t, mux, "/api/profile", testPlayerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}

	var resp struct {
		AdvancementCatalog []struct {
			UnitType string `json:"unitType"`
			Nodes    []struct {
				ID      string `json:"id"`
				Kind    string `json:"kind"`
				Cost    int    `json:"cost"`
				Effects []struct {
					Kind string `json:"kind"`
				} `json:"effects"`
			} `json:"nodes"`
		} `json:"advancementCatalog"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.AdvancementCatalog) == 0 {
		t.Fatal("advancementCatalog must be non-empty (soldier track must appear)")
	}

	// Find the soldier track.
	var soldierTrack *struct {
		UnitType string `json:"unitType"`
		Nodes    []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Cost    int    `json:"cost"`
			Effects []struct {
				Kind string `json:"kind"`
			} `json:"effects"`
		} `json:"nodes"`
	}
	for i := range resp.AdvancementCatalog {
		if resp.AdvancementCatalog[i].UnitType == "soldier" {
			soldierTrack = &resp.AdvancementCatalog[i]
			break
		}
	}
	if soldierTrack == nil {
		t.Fatal("advancementCatalog: soldier track not found")
	}
	if len(soldierTrack.Nodes) == 0 {
		t.Fatal("soldier track has no nodes")
	}
	node := soldierTrack.Nodes[0]
	if node.ID != "soldier_hp_1" {
		t.Errorf("node[0].id: want %q, got %q", "soldier_hp_1", node.ID)
	}
	if node.Kind != "minor" {
		t.Errorf("node[0].kind: want %q, got %q", "minor", node.Kind)
	}
	if node.Cost != 50 {
		t.Errorf("node[0].cost: want 50, got %d", node.Cost)
	}
	if len(node.Effects) == 0 {
		t.Fatal("node[0].effects must be non-empty")
	}
	if node.Effects[0].Kind != "unitStatAdd" {
		t.Errorf("node[0].effects[0].kind: want %q, got %q", "unitStatAdd", node.Effects[0].Kind)
	}
}
