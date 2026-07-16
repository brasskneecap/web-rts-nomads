package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newPathPerkTestMux builds a bare mux with the editor routes + the
// /catalog/paths GET, on an isolated writable catalog dir so these tests
// never touch the real source catalog on disk. The env var only isolates
// ON-DISK writes — the game package's in-process overlay maps are still
// process-global for the lifetime of this test binary, so every test below
// creates its OWN uniquely-named synthetic unit/path/perk ids rather than
// touching a real catalog entry (e.g. "acolyte"/"cleric"), so nothing here
// can leak into another test in this package.
func newPathPerkTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	registerPathCatalogRoutes(mux)
	return mux
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// saveSyntheticUnit POSTs a minimal valid unit def (no attack fields — damage
// is 0/omitted, so validateUnitDef's attackRange/attackSpeed floor doesn't
// apply). pathChances is optional (nil/empty omits the field entirely).
func saveSyntheticUnit(t *testing.T, mux *http.ServeMux, unitType string, pathChances map[string]float64) {
	t.Helper()
	req := struct {
		Unit struct {
			Type        string             `json:"type"`
			Faction     string             `json:"faction"`
			HP          int                `json:"hp"`
			MoveSpeed   float64            `json:"moveSpeed"`
			PathChances map[string]float64 `json:"pathChances,omitempty"`
		} `json:"unit"`
	}{}
	req.Unit.Type = unitType
	req.Unit.Faction = "human"
	req.Unit.HP = 100
	req.Unit.MoveSpeed = 50
	req.Unit.PathChances = pathChances
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal synthetic unit: %v", err)
	}
	rec := doJSON(t, mux, http.MethodPost, "/units", string(raw))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup POST /units(%s) status=%d body=%s", unitType, rec.Code, rec.Body.String())
	}
}

func TestPathsRoute_PostValid_ThenListedInCatalogWithFullRanks(t *testing.T) {
	mux := newPathPerkTestMux(t)
	saveSyntheticUnit(t, mux, "route_test_unit_a", nil)

	pathBody := `{"unit":"route_test_unit_a","path":{"path":"route_test_path_a","ranks":{"bronze":{"maxHPMultiplier":1.1,"damageMultiplier":1.1,"attackSpeedMultiplier":1.0,"moveSpeedMultiplier":1.0,"attackRangeMultiplier":1.0}}}}`
	rec := doJSON(t, mux, http.MethodPost, "/paths", pathBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /paths status=%d body=%s", rec.Code, rec.Body.String())
	}
	t.Cleanup(func() { doJSON(t, mux, http.MethodDelete, "/paths/route_test_path_a", "") })

	var saveResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saveResp); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if saveResp.ID != "route_test_path_a" {
		t.Errorf("save response id = %q, want route_test_path_a", saveResp.ID)
	}

	getRec := doJSON(t, mux, http.MethodGet, "/catalog/paths", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /catalog/paths status=%d", getRec.Code)
	}
	var body struct {
		Paths []struct {
			Unit string          `json:"unit"`
			Path string          `json:"path"`
			Def  json.RawMessage `json:"def"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /catalog/paths: %v", err)
	}

	var found bool
	for _, e := range body.Paths {
		if e.Path != "route_test_path_a" {
			continue
		}
		found = true
		if e.Unit != "route_test_unit_a" {
			t.Errorf("entry.Unit = %q, want route_test_unit_a", e.Unit)
		}
		var def struct {
			Ranks map[string]json.RawMessage `json:"ranks"`
		}
		if err := json.Unmarshal(e.Def, &def); err != nil {
			t.Fatalf("decode entry.Def: %v", err)
		}
		if _, ok := def.Ranks["bronze"]; !ok {
			t.Errorf("entry.Def.ranks missing bronze: %s", e.Def)
		}
	}
	if !found {
		t.Fatalf("route_test_path_a not present in /catalog/paths response: %s", getRec.Body.String())
	}
}

// TestPathsRoute_DeleteReferencedByUnitPathChances_Returns400NamingUnit is
// the HTTP-level proof that DeleteEditorPath's reference guard (which
// prevents the init() pathChances boot panic) is actually wired through the
// /paths/ route — not bypassed by calling game.DeletePathOverride directly.
func TestPathsRoute_DeleteReferencedByUnitPathChances_Returns400NamingUnit(t *testing.T) {
	mux := newPathPerkTestMux(t)

	// 1. Unit first, with no pathChances yet — the path doesn't exist.
	saveSyntheticUnit(t, mux, "route_test_unit_e", nil)

	// 2. Path second — this is the documented ordering invariant (path
	// before pathChances row).
	pathBody := `{"unit":"route_test_unit_e","path":{"path":"route_test_path_e","ranks":{"bronze":{"maxHPMultiplier":1.1,"damageMultiplier":1.1,"attackSpeedMultiplier":1.0,"moveSpeedMultiplier":1.0,"attackRangeMultiplier":1.0}}}}`
	rec := doJSON(t, mux, http.MethodPost, "/paths", pathBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup POST /paths status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 3. Re-save the unit, now referencing the path.
	saveSyntheticUnit(t, mux, "route_test_unit_e", map[string]float64{"route_test_path_e": 1})

	// 4. Attempt to delete the still-referenced path — must be rejected.
	del := doJSON(t, mux, http.MethodDelete, "/paths/route_test_path_e", "")
	if del.Code != http.StatusBadRequest {
		t.Fatalf("DELETE /paths/route_test_path_e status=%d, want 400; body=%s", del.Code, del.Body.String())
	}
	if !strings.Contains(del.Body.String(), "validation_failed") {
		t.Fatalf("body missing validation_failed: %s", del.Body.String())
	}
	if !strings.Contains(del.Body.String(), "route_test_unit_e") {
		t.Fatalf("body missing the referencing unit name: %s", del.Body.String())
	}

	// Cleanup: drop the reference, then the path deletes cleanly — proves
	// the guard is precise (blocks ONLY while referenced) and leaves this
	// test binary's process-global overlay clean for any http test that
	// runs afterward.
	saveSyntheticUnit(t, mux, "route_test_unit_e", nil)
	del = doJSON(t, mux, http.MethodDelete, "/paths/route_test_path_e", "")
	if del.Code != http.StatusOK {
		t.Fatalf("cleanup DELETE /paths/route_test_path_e status=%d body=%s", del.Code, del.Body.String())
	}
}
