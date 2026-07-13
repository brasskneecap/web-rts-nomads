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

	for _, tc := range []struct {
		path string
		key  string
	}{
		{"/catalog/abilities", "abilities"},
		{"/catalog/projectiles", "projectiles"},
		{"/catalog/effects", "effects"},
		{"/catalog/autocast-selectors", "autoCastSelectors"},
		{"/catalog/ability-categories", "abilityCategories"},
		{"/catalog/damage-types", "damageTypes"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: bad json: %v", tc.path, err)
		}
		if _, ok := body[tc.key]; !ok {
			t.Fatalf("%s: response missing expected top-level key %q; got keys %v", tc.path, tc.key, keysOf(body))
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
