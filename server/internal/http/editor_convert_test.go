package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConvertEndpoint(t *testing.T) {
	// The real catalog "greater_heal" is schemaVersion:2 as of the
	// composable-abilities migration (this endpoint is literally what
	// produced that conversion), so it can no longer serve as "a legacy
	// heal ability that converts cleanly" — POSTing a scratch legacy
	// heal-shaped ability first (same pattern as
	// TestPostAbilitiesSavesThenDeletes) preserves that original intent
	// without touching the real catalog.
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	t.Run("greater_heal converts with schemaVersion 2", func(t *testing.T) {
		seedBody := `{"ability":{"id":"convert_test_heal","type":"spell","canTargetAllies":true,"castRange":220,"manaCost":10,"healAmount":15,"targetCount":3}}`
		seed := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(seedBody))
		seedRec := httptest.NewRecorder()
		mux.ServeHTTP(seedRec, seed)
		if seedRec.Code != http.StatusCreated {
			t.Fatalf("seed status: %d %s", seedRec.Code, seedRec.Body.String())
		}

		req := httptest.NewRequest(http.MethodPost, "/abilities/convert_test_heal/convert", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Ability struct {
				SchemaVersion int             `json:"schemaVersion"`
				Program       json.RawMessage `json:"program"`
			} `json:"ability"`
			Warnings []string `json:"warnings"`
			Runnable bool     `json:"runnable"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
		}
		if resp.Ability.SchemaVersion != 2 || len(resp.Ability.Program) == 0 {
			t.Fatalf("expected schemaVersion 2 + non-empty program, got %+v", resp.Ability)
		}
		if resp.Warnings == nil {
			t.Fatalf("warnings must be [] not null")
		}
	})

	t.Run("greater_heal is already composable and cannot be re-converted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abilities/greater_heal/convert", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("expected an error status converting the already-composable catalog greater_heal, got 200: %s", rec.Body.String())
		}
	})

	// siphon_life gained a registered channel_beam ActionDescriptor (the
	// siphon_life composable migration, which finished the catalog — every
	// ability is now schemaVersion:2), so it moves from "this endpoint's
	// still-deferred fixture" to the same "already composable" bucket as
	// greater_heal/arcane_missiles below. There is no remaining catalog
	// ability left to exercise the warnings/runnable=false contract through
	// this HTTP endpoint — see TestDegradationWarnings_UnrunnableActionWarns
	// (server/internal/game) for that coverage against a synthetic Program
	// instead, and TestConvertLegacyAbility_SiphonLife_NowFullyRunnable
	// (server/internal/game) for the flip-side proof at the ConvertLegacyAbility
	// layer.
	t.Run("siphon_life is already composable and cannot be re-converted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abilities/siphon_life/convert", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("expected an error status converting the already-composable catalog siphon_life, got 200: %s", rec.Body.String())
		}
	})

	t.Run("arcane_missiles is already composable and cannot be re-converted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abilities/arcane_missiles/convert", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("expected an error status converting the already-composable catalog arcane_missiles, got 200: %s", rec.Body.String())
		}
	})

	// TestConvertLegacyAbility_Fireball_NowFullyRunnable and
	// TestConvertLegacyAbility_ArcaneMissiles_NowFullyRunnable
	// (server/internal/game) cover the flip side (fireball/arcane_missiles
	// now convert fully runnable) directly against ConvertLegacyAbility; not
	// duplicated here as an HTTP round-trip.

	t.Run("unknown ability 404s", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/abilities/does_not_exist/convert", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET is not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/abilities/greater_heal/convert", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
		}
	})
}
