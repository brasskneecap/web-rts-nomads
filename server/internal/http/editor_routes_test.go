package httpserver

import (
	"encoding/json"
	"net/http/httptest"
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
