package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostEffectsValidationFails(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/effects", strings.NewReader(`{"effect":{"id":"x","duration":-1}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostEffectsSavesThenDeletes(t *testing.T) {
	t.Setenv("EFFECT_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	post := httptest.NewRequest(http.MethodPost, "/effects", strings.NewReader(`{"effect":{"id":"post_fx","duration":1}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/effects/post_fx", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}
