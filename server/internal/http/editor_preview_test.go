package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webrts/server/internal/game"
)

// previewTraceHasType mirrors game's own previewTraceHasType helper
// (unexported to package game, so it can't be reused here directly): reports
// whether evs contains at least one event of the given type.
func previewTraceHasType(evs []game.AbilityExecutionTraceEvent, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// TestPreviewEndpoint exercises POST /abilities/preview end to end with a
// minimal inline v2 ability def (select_targets(initial_target) ->
// restore_health) rather than fetching the catalog "greater_heal" def:
// package http has no access to the unexported getAbilityDef the game
// package's own RunAbilityPreview tests use, and a hand-built minimal
// program is the cleanest thing constructible from here while still
// exercising the same select_targets/restore_health action pair
// greater_heal compiles to.
//
// Scene: two injured allies, Target -1 (no explicit pick). With no
// CanTargetSelf and no enemy scene units, RunAbilityPreview's target
// fallback (ability_preview.go) resolves to the first scene unit — so
// exactly the first ally is healed, and the response must show a
// healing_applied trace event plus that unit's HP increasing.
func TestPreviewEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	body := `{
		"ability": {
			"id": "preview_heal_test",
			"displayName": "Preview Heal Test",
			"schemaVersion": 2,
			"canTargetAllies": true,
			"castRange": "match_attack_range",
			"program": {
				"triggers": [
					{"id": "t", "type": "on_cast_complete", "actions": [
						{"id": "sel", "type": "select_targets", "outputs": {"targets": "healTargets"},
							"target": {"source": "initial_target"}},
						{"id": "heal", "type": "restore_health", "input": {"targets": {"key": "healTargets"}},
							"config": {"amount": 50}}
					]}
				]
			}
		},
		"seed": 1,
		"casterX": 0,
		"casterY": 0,
		"target": -1,
		"durationSeconds": 2,
		"units": [
			{"team": "ally", "x": 40, "y": 0, "hp": 20, "maxHp": 100},
			{"team": "ally", "x": 80, "y": 0, "hp": 60, "maxHp": 100}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/abilities/preview", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var res game.PreviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q; body=%s", res.Error, rec.Body.String())
	}
	if len(res.Trace) == 0 {
		t.Fatal("expected non-empty trace")
	}
	if len(res.Units) == 0 {
		t.Fatal("expected non-empty units")
	}
	if !previewTraceHasType(res.Trace, "healing_applied") {
		t.Fatalf("no healing_applied event recorded: %+v", res.Trace)
	}
	healed := false
	for _, u := range res.Units {
		if u.HPAfter > u.HPBefore {
			healed = true
		}
	}
	if !healed {
		t.Fatalf("no unit was healed: %+v", res.Units)
	}
}

// TestPreviewEndpoint_MethodNotAllowed proves GET is refused (POST only,
// matching every other editor route).
func TestPreviewEndpoint_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/abilities/preview", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
}

// TestPreviewEndpoint_MalformedJSON proves a decode failure is a 400
// invalid_json, matching every other editor route's malformed-body handling.
func TestPreviewEndpoint_MalformedJSON(t *testing.T) {
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/abilities/preview", strings.NewReader(`{not json`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_json") {
		t.Fatalf("body missing invalid_json: %s", rec.Body.String())
	}
}

// TestPreviewEndpoint_BareRequestGetsDefaultScene proves the headline
// "one-click preview works out of the box" contract: a request that
// supplies ONLY an ability (no units, no durationSeconds) still previews
// something instead of failing or coming back empty. The handler's
// default-scene injection (editor_handlers.go) supplies one enemy scene
// unit near the caster and a default 2s duration; this asserts the
// response actually reflects that injected scene: a non-empty units list
// and a damage_applied trace event proving the ability ran against it.
func TestPreviewEndpoint_BareRequestGetsDefaultScene(t *testing.T) {
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	body := `{
		"ability": {
			"id": "preview_bare_damage_test",
			"displayName": "Preview Bare Damage Test",
			"schemaVersion": 2,
			"canTargetEnemies": true,
			"castRange": "match_attack_range",
			"program": {
				"triggers": [
					{"id": "t", "type": "on_cast_complete", "actions": [
						{"id": "sel", "type": "select_targets", "outputs": {"targets": "hits"},
							"target": {"source": "all_in_scene", "origin": "caster", "relations": ["enemy"], "radius": 200}},
						{"id": "dmg", "type": "deal_damage", "input": {"targets": {"key": "hits"}},
							"config": {"amount": 40, "type": "fire"}}
					]}
				]
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/abilities/preview", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var res game.PreviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if res.Error != "" {
		t.Fatalf("unexpected cast failure: %q; body=%s", res.Error, rec.Body.String())
	}
	if len(res.Units) == 0 {
		t.Fatal("expected the default-scene injection to produce at least one unit")
	}
	if !previewTraceHasType(res.Trace, "damage_applied") {
		t.Fatalf("no damage_applied event recorded — default scene did not run: %+v", res.Trace)
	}
	if res.Units[0].HPAfter >= res.Units[0].HPBefore {
		t.Fatalf("injected enemy not damaged: %+v", res.Units[0])
	}
}

// TestPreviewEndpoint_DoesNotShadowExistingRoutes proves registering the
// exact "/abilities/preview" pattern alongside "/abilities/validate" and the
// "/abilities/" catch-all does not break either of them: ServeMux prefers
// the longest exact match for "/abilities/preview", while
// "/abilities/validate" and every "/abilities/{id}..." request still
// resolve exactly as before.
func TestPreviewEndpoint_DoesNotShadowExistingRoutes(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)

	// /abilities/validate still resolves (not swallowed by /abilities/preview
	// or the /abilities/ catch-all).
	valReq := httptest.NewRequest(http.MethodPost, "/abilities/validate", strings.NewReader(`{"ability":{"id":"heal","displayName":"Heal"}}`))
	vrec := httptest.NewRecorder()
	mux.ServeHTTP(vrec, valReq)
	if vrec.Code != http.StatusOK {
		t.Fatalf("validate status = %d, want 200; body=%s", vrec.Code, vrec.Body.String())
	}

	// POST /abilities (save) then DELETE /abilities/{id} still resolve through
	// the "/abilities/" catch-all, unaffected by the new exact pattern.
	post := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(`{"ability":{"id":"preview_precedence_bolt","damageAmount":5}}`))
	prec := httptest.NewRecorder()
	mux.ServeHTTP(prec, post)
	if prec.Code != http.StatusCreated {
		t.Fatalf("save status = %d, want 201; body=%s", prec.Code, prec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/abilities/preview_precedence_bolt", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "deleted") {
		t.Fatalf("delete status=%d body=%s", drec.Code, drec.Body.String())
	}
}
