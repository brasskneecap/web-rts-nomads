package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webrts/server/internal/game"
	"webrts/server/internal/profile"
	"webrts/server/internal/ws"
)

// newTestProfileManager builds a fresh in-memory-backed profile manager
// rooted at a t.TempDir(), matching the construction used by every other
// httpserver test that needs a *profile.Manager (see
// advancement_handlers_test.go's newAdvancementTestMux).
func newTestProfileManager(t *testing.T) *profile.Manager {
	t.Helper()
	return profile.NewManager(t.TempDir())
}

// newTestRouter builds the real router with minimal deps for editor-route
// tests. Mirrors production wiring (see cmd/api/main.go): a Hub needs a
// MatchManager + LobbyManager, and NewRouter takes the hub, CORS origin,
// profile manager, and an optional SPA handler (nil here — API-only).
func newTestRouter(t *testing.T) *httptest.Server {
	t.Helper()
	hub := ws.NewHub(game.NewMatchManager(), game.NewLobbyManager())
	srv := httptest.NewServer(NewRouter(hub, "*", newTestProfileManager(t), nil))
	t.Cleanup(srv.Close)
	return srv
}

func TestCatalogProcsRoute(t *testing.T) {
	srv := newTestRouter(t)
	resp, err := srv.Client().Get(srv.URL + "/catalog/procs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Procs []map[string]any `json:"procs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Procs) < 3 {
		t.Fatalf("expected >=3 procs, got %d", len(body.Procs))
	}
	if body.Procs[0]["id"] == "" {
		t.Error("proc entries need ids")
	}
}

func TestItemsRoute_SaveAndDelete(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)

	body := `{"item":{"id":"route_test_item","displayName":"Route Test","iconKey":"route_test_item","kind":"equipment","tier":"common","slotKind":"any","costGold":10},"recipe":null,"availability":{"marketplace":true,"wanderingMerchant":false,"lootTable":{"enabled":false,"weight":0},"recipeList":false}}`
	resp, err := srv.Client().Post(srv.URL+"/items", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}

	// Invalid body → 400 with the validation envelope.
	bad := `{"item":{"id":"NOT VALID","displayName":"x","iconKey":"x"},"availability":{}}`
	resp2, err := srv.Client().Post(srv.URL+"/items", "application/json", strings.NewReader(bad))
	if err != nil {
		t.Fatalf("POST invalid: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp2.StatusCode)
	}

	// DELETE removes it.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/items/route_test_item", nil)
	resp3, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("delete status %d", resp3.StatusCode)
	}
	// Second delete → 404.
	resp4, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("re-DELETE: %v", err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != 404 {
		t.Fatalf("re-delete expected 404, got %d", resp4.StatusCode)
	}
}
