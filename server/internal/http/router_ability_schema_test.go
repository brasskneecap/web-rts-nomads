package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestActionSchemaEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	registerAbilityCatalogRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/catalog/action-schema", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Actions []struct {
			Type     string          `json:"type"`
			Fields   json.RawMessage `json:"fields"`
			Runnable bool            `json:"runnable"`
		} `json:"actions"`
		Enums map[string][]string `json:"enums"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}

	if len(body.Actions) == 0 {
		t.Fatalf("expected non-empty actions array")
	}
	for _, a := range body.Actions {
		if a.Type == "" {
			t.Errorf("action entry missing type: %+v", a)
		}
	}
	if len(body.Enums) == 0 {
		t.Fatalf("expected non-empty enums object")
	}
}
