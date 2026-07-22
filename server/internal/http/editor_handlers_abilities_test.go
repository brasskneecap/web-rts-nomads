package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webrts/server/internal/game"
)

func TestPostAbilitiesValidationFails(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	body := `{"ability":{"id":"x","category":"not_real"}}`
	req := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body missing validation_failed: %s", rec.Body.String())
	}
}

func TestPostAbilitiesSavesThenDeletes(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	post := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(`{"ability":{"id":"post_bolt","damageAmount":5}}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/abilities/post_bolt", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}

// TestDeleteAbilitiesRoute_ShippedAbilityRevertsThenResets exercises the
// three-way status over HTTP: a SHIPPED ability's first DELETE undoes the
// author's last save ("reverted"); a second DELETE (no undo step left) falls
// back to the catalog default ("reset"). Mirrors TestPostAbilitiesSavesThenDeletes
// but against an embedded id instead of an author-created one, and the
// game-package DeleteEditorAbility 3-way tests (ability_editor_test.go)
// against the actual HTTP route/response shape.
func TestDeleteAbilitiesRoute_ShippedAbilityRevertsThenResets(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	var id string
	var shippedName string
	for _, def := range game.ListAbilityDefs() {
		if game.AbilityIsEmbedded(def.ID) {
			id, shippedName = def.ID, def.DisplayName
			break
		}
	}
	if id == "" {
		t.Skip("no embedded abilities to test against")
	}

	saveBody := func(name string) string {
		b, err := json.Marshal(map[string]any{"ability": map[string]any{"id": id, "displayName": name}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}

	post := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(saveBody("Keeper")))
	prec := httptest.NewRecorder()
	mux.ServeHTTP(prec, post)
	if prec.Code != http.StatusCreated {
		t.Fatalf("first save status=%d body=%s", prec.Code, prec.Body.String())
	}
	post2 := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(saveBody("Oops")))
	prec2 := httptest.NewRecorder()
	mux.ServeHTTP(prec2, post2)
	if prec2.Code != http.StatusCreated {
		t.Fatalf("second save status=%d body=%s", prec2.Code, prec2.Body.String())
	}

	del1 := httptest.NewRequest(http.MethodDelete, "/abilities/"+id, nil)
	drec1 := httptest.NewRecorder()
	mux.ServeHTTP(drec1, del1)
	if drec1.Code != http.StatusOK || !strings.Contains(drec1.Body.String(), `"reverted"`) {
		t.Fatalf("first delete status=%d body=%s, want 200 status=reverted", drec1.Code, drec1.Body.String())
	}

	del2 := httptest.NewRequest(http.MethodDelete, "/abilities/"+id, nil)
	drec2 := httptest.NewRecorder()
	mux.ServeHTTP(drec2, del2)
	if drec2.Code != http.StatusOK || !strings.Contains(drec2.Body.String(), `"reset"`) {
		t.Fatalf("second delete status=%d body=%s, want 200 status=reset", drec2.Code, drec2.Body.String())
	}

	found := false
	for _, def := range game.ListAbilityDefs() {
		if def.ID == id {
			found = true
			if def.DisplayName != shippedName {
				t.Errorf("after second delete DisplayName = %q, want shipped default %q", def.DisplayName, shippedName)
			}
		}
	}
	if !found {
		t.Fatalf("ability %q not resolvable after second delete", id)
	}
}

// TestDeleteAbilitiesRoute_NotFound: DELETE on an id naming nothing at all is
// a 404, matching DELETE /items/{id}.
func TestDeleteAbilitiesRoute_NotFound(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	del := httptest.NewRequest(http.MethodDelete, "/abilities/no_such_ability_id_at_all", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", drec.Code, drec.Body.String())
	}
}

// TestAbilityStatsEndpoint_ServesDerivedRows covers the reason this is an
// ENDPOINT rather than a client-side mirror: the scoped ids are derived from the
// action registry, so a hand-maintained copy would rot the moment an action
// gained or lost a kinded field. The client must be able to ask.
func TestAbilityStatsEndpoint_ServesDerivedRows(t *testing.T) {
	mux := http.NewServeMux()
	registerAbilityCatalogRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/catalog/ability-stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Stats []struct {
			ID       string `json:"id"`
			Label    string `json:"label"`
			Kind     string `json:"kind"`
			FlatOnly bool   `json:"flatOnly"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response did not decode: %v", err)
	}
	if len(body.Stats) == 0 {
		t.Fatal("no ability stats served")
	}

	byID := map[string]bool{}
	flatOnly := map[string]bool{}
	for _, s := range body.Stats {
		byID[s.ID] = true
		flatOnly[s.ID] = s.FlatOnly
	}
	// Both addressing levels must reach the client, or the editor could only
	// offer broad rows and the scoped ones would be unauthorable.
	for _, want := range []string{"duration", "radius", "create_zone.duration", "apply_status_duration.duration"} {
		if !byID[want] {
			t.Errorf("stat %q not served", want)
		}
	}
	// flatOnly has to travel too — it is what tells the editor to hide the
	// percentage input for a whole quantity the server would reject.
	if !flatOnly["count"] {
		t.Error("count should be served as flatOnly")
	}
	if flatOnly["duration"] {
		t.Error("duration is continuous and must not be flatOnly")
	}
}
