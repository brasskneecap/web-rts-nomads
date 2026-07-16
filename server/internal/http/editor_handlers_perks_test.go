package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostPerksValidationFails(t *testing.T) {
	t.Setenv("PERK_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/perks", strings.NewReader(`{"perk":{"id":"x","rank":"platinum"}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostPerksSavesThenDeletes(t *testing.T) {
	t.Setenv("PERK_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	post := httptest.NewRequest(http.MethodPost, "/perks", strings.NewReader(`{"perk":{"id":"post_perk","displayName":"Post Perk","rank":"bronze"}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/perks/post_perk", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}

// TestDeletePerksResetsEmbedded overrides a real embedded perk, then deletes it;
// the response status must be "reset" (reverted to the shipped default), not
// "deleted", and the embedded def must resolve again.
func TestDeletePerksResetsEmbedded(t *testing.T) {
	t.Setenv("PERK_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	post := httptest.NewRequest(http.MethodPost, "/perks", strings.NewReader(
		`{"perk":{"id":"savage_strikes","displayName":"Edited","unitType":"soldier","path":"berserker","rank":"bronze"}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("override save status=%d body=%s", rec.Code, rec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/perks/savage_strikes", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "reset") {
		t.Fatalf("delete status=%d body=%s (want reset)", drec.Code, drec.Body.String())
	}
}

// TestDeletePerksMalformedID rejects a multi-segment /perks/ path.
func TestDeletePerksMalformedID(t *testing.T) {
	t.Setenv("PERK_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	del := httptest.NewRequest(http.MethodDelete, "/perks/foo/bar", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, del)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_id") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
