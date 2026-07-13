package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogAbilitiesRoutes(t *testing.T) {
	mux := http.NewServeMux()
	registerAbilityCatalogRoutes(mux)

	for _, path := range []string{
		"/catalog/abilities", "/catalog/projectiles", "/catalog/effects",
		"/catalog/autocast-selectors", "/catalog/ability-categories", "/catalog/damage-types",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: bad json: %v", path, err)
		}
	}
}
