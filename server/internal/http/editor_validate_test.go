package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestValidateEndpoint proves POST /abilities/validate is a dry-run: it
// always returns 200 with structured issues (never a 400 the way POST
// /abilities does), and that route precedence resolves the exact
// "/abilities/validate" pattern instead of falling into the "/abilities/"
// catch-all (which would otherwise try to treat "validate" as an ability id
// and reject the request with method_not_allowed / invalid_id).
func TestValidateEndpoint(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	body := `{"ability":{"id":"x","displayName":"X","schemaVersion":2,"program":{"triggers":[{"id":"t","type":"on_cast_complete","actions":[{"id":"a","type":"deal_damage","config":{"amount":0}}]}]}}}`
	req := httptest.NewRequest(http.MethodPost, "/abilities/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Issues []struct {
			Path     string `json:"path"`
			Code     string `json:"code"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	found := false
	for _, i := range resp.Issues {
		if i.Code == "empty_required_property" && i.Severity == "error" && i.Path != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected deal_damage amount issue with path, got %+v", resp.Issues)
	}
}

// TestValidateEndpoint_CleanLegacy proves a clean legacy (no-program) def
// round-trips through the endpoint with an empty issues list.
func TestValidateEndpoint_CleanLegacy(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	body := `{"ability":{"id":"heal","displayName":"Heal","healAmount":10}}`
	req := httptest.NewRequest(http.MethodPost, "/abilities/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Issues []any `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected no issues for clean legacy def, got %+v", resp.Issues)
	}
}

// TestValidateEndpoint_MalformedJSON proves a decode failure is the one case
// where /abilities/validate returns non-200 (400 invalid_json), matching
// every other editor save route's malformed-body handling.
func TestValidateEndpoint_MalformedJSON(t *testing.T) {
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/abilities/validate", strings.NewReader(`{not json`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_json") {
		t.Fatalf("body missing invalid_json: %s", rec.Body.String())
	}
}

// TestValidateEndpoint_DoesNotShadowAbilitiesSubtreeRoutes proves
// registering the exact "/abilities/validate" pattern alongside the
// "/abilities/" catch-all does not break the catch-all's own routes (delete
// / image) — ServeMux prefers the longer exact match for "/abilities/validate"
// while everything else under "/abilities/" still falls through to the
// catch-all as before.
func TestValidateEndpoint_DoesNotShadowAbilitiesSubtreeRoutes(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	post := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(`{"ability":{"id":"validate_precedence_bolt","damageAmount":5}}`))
	prec := httptest.NewRecorder()
	mux.ServeHTTP(prec, post)
	if prec.Code != http.StatusCreated {
		t.Fatalf("save status = %d, want 201; body=%s", prec.Code, prec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/abilities/validate_precedence_bolt", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}

	valReq := httptest.NewRequest(http.MethodPost, "/abilities/validate", strings.NewReader(`{"ability":{"id":"heal","displayName":"Heal"}}`))
	vrec := httptest.NewRecorder()
	mux.ServeHTTP(vrec, valReq)
	if vrec.Code != http.StatusOK {
		t.Fatalf("validate status = %d, want 200; body=%s", vrec.Code, vrec.Body.String())
	}
}
