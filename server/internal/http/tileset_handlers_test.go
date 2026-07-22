package httpserver

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tilesetTestPNG encodes a tiny valid in-memory 1x1 PNG for image-upload
// tests, mirroring abIconPNG in editor_handlers_ability_icon_test.go.
func tilesetTestPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func newTilesetTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	t.Setenv("TILESET_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerTilesetRoutes(mux)
	return mux
}

func TestTilesetRoutes_SaveThenListedInCatalog(t *testing.T) {
	mux := newTilesetTestMux(t)

	body := `{"id":"grasslands_test","name":"Grasslands Test","image":"grasslands_test.png","cols":4,"rows":4,"tileWidth":32,"tileHeight":32}`
	save := httptest.NewRequest(http.MethodPost, "/tilesets", strings.NewReader(body))
	srec := httptest.NewRecorder()
	mux.ServeHTTP(srec, save)
	if srec.Code != http.StatusCreated {
		t.Fatalf("POST /tilesets: status %d body %s", srec.Code, srec.Body.String())
	}
	var saveResp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(srec.Body.Bytes(), &saveResp); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if saveResp.ID != "grasslands_test" || saveResp.Status != "saved" {
		t.Fatalf("save response = %+v, want id=grasslands_test status=saved", saveResp)
	}

	get := httptest.NewRequest(http.MethodGet, "/catalog/tilesets", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("GET /catalog/tilesets: status %d", grec.Code)
	}
	if !strings.Contains(grec.Body.String(), `"grasslands_test"`) {
		t.Fatalf("catalog listing missing saved tileset: %s", grec.Body.String())
	}
}

func TestTilesetRoutes_SaveInvalidDefReturns400(t *testing.T) {
	mux := newTilesetTestMux(t)

	// Missing required fields (cols/rows/tileWidth/tileHeight) -> validation error.
	body := `{"id":"bad_def","name":"Bad"}`
	req := httptest.NewRequest(http.MethodPost, "/tilesets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("expected validation_failed error code, got %s", rec.Body.String())
	}
}

func TestTilesetRoutes_GetMethodNotAllowed(t *testing.T) {
	mux := newTilesetTestMux(t)

	req := httptest.NewRequest(http.MethodPost, "/catalog/tilesets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /catalog/tilesets: status = %d, want 405", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/tilesets", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /tilesets: status = %d, want 405", rec2.Code)
	}
}

func TestTilesetRoutes_ImageUploadThenServe(t *testing.T) {
	mux := newTilesetTestMux(t)

	// Save the def first (image upload doesn't require it, but exercises the
	// full author flow).
	body := `{"id":"img_tileset","name":"Img Tileset","image":"img_tileset.png","cols":2,"rows":2,"tileWidth":16,"tileHeight":16}`
	save := httptest.NewRequest(http.MethodPost, "/tilesets", strings.NewReader(body))
	srec := httptest.NewRecorder()
	mux.ServeHTTP(srec, save)
	if srec.Code != http.StatusCreated {
		t.Fatalf("save def: %d %s", srec.Code, srec.Body.String())
	}

	up := httptest.NewRequest(http.MethodPost, "/tilesets/img_tileset/image", bytes.NewReader(tilesetTestPNG(t)))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", urec.Code, urec.Body.String())
	}
	var upResp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Image  string `json:"image"`
	}
	if err := json.Unmarshal(urec.Body.Bytes(), &upResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if upResp.Status != "image_saved" || upResp.Image != "img_tileset.png" {
		t.Fatalf("upload response = %+v, want status=image_saved image=img_tileset.png", upResp)
	}

	get := httptest.NewRequest(http.MethodGet, "/tilesets/images/img_tileset.png", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("GET image: status %d", grec.Code)
	}
	if ct := grec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if got := grec.Body.Bytes(); !bytes.Equal(got, tilesetTestPNG(t)) {
		t.Fatalf("served image bytes do not match uploaded bytes (got %d bytes)", len(got))
	}

	// 404 for an unknown image key.
	miss := httptest.NewRequest(http.MethodGet, "/tilesets/images/does_not_exist.png", nil)
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, miss)
	if mrec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing image, got %d", mrec.Code)
	}
}

func TestTilesetRoutes_ImagesPrefixNotMisroutedAsDeleteID(t *testing.T) {
	// Regression net: /tilesets/images/{key} must never be parsed by the
	// DELETE/{id}/image branches as a delete of id "images/foo.png".
	mux := newTilesetTestMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/tilesets/images/foo.png", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// A DELETE against the images/ subtree must be rejected as method-not-
	// allowed (the images branch only accepts GET), never treated as a
	// tileset-def delete.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /tilesets/images/foo.png: status = %d, want 405", rec.Code)
	}
}

func TestTilesetRoutes_DeleteRemovesDef(t *testing.T) {
	mux := newTilesetTestMux(t)

	body := `{"id":"delete_me","name":"Delete Me","image":"delete_me.png","cols":1,"rows":1,"tileWidth":32,"tileHeight":32}`
	save := httptest.NewRequest(http.MethodPost, "/tilesets", strings.NewReader(body))
	srec := httptest.NewRecorder()
	mux.ServeHTTP(srec, save)
	if srec.Code != http.StatusCreated {
		t.Fatalf("save def: %d %s", srec.Code, srec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/tilesets/delete_me", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK {
		t.Fatalf("DELETE: status %d body %s", drec.Code, drec.Body.String())
	}
	if !strings.Contains(drec.Body.String(), `"deleted"`) {
		t.Fatalf("delete response missing deleted status: %s", drec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/catalog/tilesets", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if strings.Contains(grec.Body.String(), `"delete_me"`) {
		t.Fatalf("catalog still lists deleted tileset: %s", grec.Body.String())
	}
}

func TestTilesetRoutes_DeleteRejectsIDWithSlash(t *testing.T) {
	mux := newTilesetTestMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/tilesets/foo/bar", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestTilesetRoutes_ImageUploadTooLargeRejected(t *testing.T) {
	mux := newTilesetTestMux(t)

	oversized := bytes.Repeat([]byte{0}, 4*1024*1024+2)
	up := httptest.NewRequest(http.MethodPost, "/tilesets/too_big/image", bytes.NewReader(oversized))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusBadRequest {
		body, _ := io.ReadAll(urec.Body)
		t.Fatalf("status = %d, want 400: %s", urec.Code, body)
	}
}
