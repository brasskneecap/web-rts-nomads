package httpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestNewCatalogRoutes_ReturnNonEmptyExpectedKey exercises the five
// previously-unrouted catalog endpoints. A ListXDefs() unit test can't catch
// a JSON envelope key typo (e.g. "damageTypes" vs "damage_types") since the
// SPA parses that key directly and would just render an empty dropdown, so
// this asserts on the exact wire key and that it decodes to a non-empty
// array.
func TestNewCatalogRoutes_ReturnNonEmptyExpectedKey(t *testing.T) {
	srv := newTestRouter(t)

	cases := []struct {
		path string
		key  string
	}{
		{"/catalog/archetypes", "archetypes"},
		{"/catalog/projectiles", "projectiles"},
		{"/catalog/abilities", "abilities"},
		{"/catalog/effects", "effects"},
		{"/catalog/damage-types", "damageTypes"},
		{"/catalog/factions", "factions"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d", tc.path, resp.StatusCode)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("GET %s: decode: %v", tc.path, err)
			}
			raw, ok := body[tc.key]
			if !ok {
				t.Fatalf("GET %s: response missing key %q, got keys %v", tc.path, tc.key, keysOf(body))
			}
			var list []json.RawMessage
			if err := json.Unmarshal(raw, &list); err != nil {
				t.Fatalf("GET %s: key %q is not an array: %v", tc.path, tc.key, err)
			}
			if len(list) == 0 {
				t.Fatalf("GET %s: key %q decoded to an empty array", tc.path, tc.key)
			}
		})
	}
}

// getCatalogUnitArt performs the request/decode boilerplate shared by the two
// TestCatalogUnitArtRoute subtests below.
func getCatalogUnitArt(t *testing.T, srv *httptest.Server) []json.RawMessage {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/catalog/unit-art")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw, ok := body["art"]
	if !ok {
		t.Fatalf("response missing key %q, got keys %v", "art", keysOf(body))
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("key %q is not an array: %v", "art", err)
	}
	return list
}

// TestCatalogUnitArtRoute_ServesFixtureManifest exercises the route end to
// end with a real manifest on disk. This is the regression net for the
// Windows backslash-in-BaseURL trap and for double-encoding of the raw
// Manifest field — both of which a request against an empty catalog cannot
// catch, since json.Unmarshal("null", &list) succeeds trivially.
func TestCatalogUnitArtRoute_ServesFixtureManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	rel := filepath.Join("human", "archer", "sprites.json")
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(`{"key":"archer","size":{"width":104,"height":104}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestRouter(t)
	list := getCatalogUnitArt(t, srv)
	if len(list) != 1 {
		t.Fatalf("want 1 entry, got %d: %s", len(list), list)
	}

	var entry struct {
		Key      string          `json:"key"`
		Faction  string          `json:"faction"`
		Unit     string          `json:"unit"`
		BaseURL  string          `json:"baseUrl"`
		Manifest json.RawMessage `json:"manifest"`
	}
	if err := json.Unmarshal(list[0], &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if entry.Key != "archer" || entry.Faction != "human" || entry.Unit != "archer" {
		t.Fatalf("entry wrong: %+v", entry)
	}
	// Regression net: on Windows, filepath.Join instead of path.Join for
	// BaseURL would emit backslashes here.
	if entry.BaseURL != "/assets/units/human/archer" {
		t.Fatalf("baseUrl = %q, want /assets/units/human/archer", entry.BaseURL)
	}
	// Regression net: if Manifest were double-encoded (a JSON string
	// containing JSON, rather than a raw JSON object), this Unmarshal target
	// would fail to decode as a map.
	var manifest map[string]any
	if err := json.Unmarshal(entry.Manifest, &manifest); err != nil {
		t.Fatalf("manifest did not decode as a JSON object (possible double-encoding): %v", err)
	}
	if manifest["key"] != "archer" {
		t.Fatalf("manifest did not round-trip: %v", manifest)
	}
}

// TestCatalogUnitArtRoute_EmptyCatalogServesEmptyArray asserts the no-art-dir
// case renders as JSON "[]", not "null" — a client doing data.art.map(...)
// would throw on the latter, and that is the DEFAULT response on any
// checkout/CI run with no writable art dir.
func TestCatalogUnitArtRoute_EmptyCatalogServesEmptyArray(t *testing.T) {
	t.Setenv("UNIT_ASSETS_DIR", filepath.Join(t.TempDir(), "does_not_exist"))
	srv := newTestRouter(t)

	resp, err := srv.Client().Get(srv.URL + "/catalog/unit-art")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(raw), `"art":[]`) {
		t.Fatalf("expected literal empty array in body, got: %s", raw)
	}

	list := getCatalogUnitArt(t, srv)
	if len(list) != 0 {
		t.Fatalf("want 0 entries, got %d", len(list))
	}
}

// TestAssetsUnitsRoute_ServesFixtureAndRejectsTraversal exercises the
// filesystem-backed /assets/units/ handler end to end through the real
// router. The exhaustive traversal-string coverage lives in
// game.ReadUnitArtFile's own unit tests (they call the function directly, so
// they still prove the containment logic even if http.ServeMux cleans the
// request path before it reaches this handler); this test just confirms the
// wiring: a real fixture serves with the right Content-Type, and a
// traversal attempt never yields 200.
func TestAssetsUnitsRoute_ServesFixtureAndRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	rel := filepath.Join("human", "archer", "packed", "walking.png")
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("\x89PNG\r\n\x1a\n fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A secret sitting just outside the art root.
	secret := filepath.Join(dir, "..", "secret.json")
	if err := os.WriteFile(secret, []byte(`{"password":"hunter2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	srv := newTestRouter(t)

	resp, err := srv.Client().Get(srv.URL + "/assets/units/human/archer/packed/walking.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("status %d type %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	badResp, err := srv.Client().Get(srv.URL + "/assets/units/../../secret.json")
	if err != nil {
		t.Fatalf("GET traversal: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode == http.StatusOK {
		t.Fatalf("traversal request was SERVED (status 200) — must be refused")
	}
}

// TestUnitArtRoute_SavesFileAndRejectsTraversal exercises POST /unit-art end
// to end through the real router: a valid one-file save must land on disk
// under the art root, and a request whose file name attempts to escape the
// root must be refused with 400 and write nothing.
func TestUnitArtRoute_SavesFileAndRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	srv := newTestRouter(t)

	body := `{"faction":"human","unit":"moon_dancer","files":[{"name":"sprites.json","contentBase64":"` +
		base64.StdEncoding.EncodeToString([]byte(`{"key":"moon_dancer"}`)) + `"}]}`
	resp, err := srv.Client().Post(srv.URL+"/unit-art", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "human", "moon_dancer", "sprites.json")); err != nil {
		t.Fatalf("expected file written: %v", err)
	}

	badBody := `{"faction":"human","unit":"u","files":[{"name":"../../secret.png","contentBase64":"` +
		base64.StdEncoding.EncodeToString([]byte("x")) + `"}]}`
	badResp, err := srv.Client().Post(srv.URL+"/unit-art", "application/json", strings.NewReader(badBody))
	if err != nil {
		t.Fatalf("POST traversal: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal request expected 400, got %d", badResp.StatusCode)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestItemsRoute_SaveAndDelete(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)

	body := `{"item":{"id":"route_test_item","displayName":"Route Test","iconKey":"route_test_item","kind":"equipment","tier":"common","slotKind":"any","costGold":10},"inputs":[]}`
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
	bad := `{"item":{"id":"NOT VALID","displayName":"x","iconKey":"x"}}`
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

func TestItemImageRoutes(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)

	// Create the item first.
	body := `{"item":{"id":"img_item","displayName":"Img","iconKey":"img_item","kind":"equipment","tier":"common","slotKind":"any","costGold":1},"inputs":[]}`
	resp, _ := srv.Client().Post(srv.URL+"/items", "application/json", strings.NewReader(body))
	resp.Body.Close()

	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	up, err := srv.Client().Post(srv.URL+"/items/img_item/image", "image/png", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer up.Body.Close()
	if up.StatusCode != 201 {
		raw, _ := io.ReadAll(up.Body)
		t.Fatalf("upload status %d: %s", up.StatusCode, raw)
	}
	got, err := srv.Client().Get(srv.URL + "/catalog/items/img_item/image")
	if err != nil {
		t.Fatalf("GET image: %v", err)
	}
	defer got.Body.Close()
	if got.StatusCode != 200 || got.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("serve: status %d type %s", got.StatusCode, got.Header.Get("Content-Type"))
	}
	// Missing icon → 404.
	miss, err := srv.Client().Get(srv.URL + "/catalog/items/never_uploaded/image")
	if err != nil {
		t.Fatalf("GET missing image: %v", err)
	}
	defer miss.Body.Close()
	if miss.StatusCode != 404 {
		t.Fatalf("missing icon expected 404, got %d", miss.StatusCode)
	}
}

func TestItemAvailabilityRoute(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	t.Setenv("RECIPE_CATALOG_DIR", t.TempDir())
	t.Setenv("NEUTRAL_GROUPS_DIR", t.TempDir())
	srv := newTestRouter(t)
	resp, err := srv.Client().Get(srv.URL + "/items/frost_sword/availability")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var av struct {
		Marketplace bool `json:"marketplace"`
		LootTable   struct {
			Enabled bool `json:"enabled"`
			Weight  int  `json:"weight"`
		} `json:"lootTable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&av); err != nil {
		t.Fatalf("decode: %v", err)
	}
	miss, err := srv.Client().Get(srv.URL + "/items/no_such_item/availability")
	if err != nil {
		t.Fatalf("GET miss: %v", err)
	}
	defer miss.Body.Close()
	if miss.StatusCode != 404 {
		t.Fatalf("unknown item expected 404, got %d", miss.StatusCode)
	}
}
