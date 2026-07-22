package httpserver

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tileTestPNG encodes a tiny valid in-memory PNG of the given size for
// image-upload tests, mirroring tilesetTestPNG in tileset_handlers_test.go.
func tileTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func newTileLibraryTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	t.Setenv("TILESET_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerTileLibraryRoutes(mux)
	return mux
}

func TestTileRoutes_SaveThenListedInCatalogWithDimensions(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	up := httptest.NewRequest(http.MethodPost, "/tiles/grass_01", bytes.NewReader(tileTestPNG(t, 2, 3)))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusCreated {
		t.Fatalf("POST /tiles/grass_01: status %d body %s", urec.Code, urec.Body.String())
	}
	var upResp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Image  string `json:"image"`
	}
	if err := json.Unmarshal(urec.Body.Bytes(), &upResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if upResp.ID != "grass_01" || upResp.Status != "saved" || upResp.Image != "grass_01.png" {
		t.Fatalf("upload response = %+v, want id=grass_01 status=saved image=grass_01.png", upResp)
	}

	get := httptest.NewRequest(http.MethodGet, "/catalog/tiles", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("GET /catalog/tiles: status %d", grec.Code)
	}
	var listResp struct {
		Tiles []struct {
			ID     string `json:"id"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"tiles"`
	}
	if err := json.Unmarshal(grec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if len(listResp.Tiles) != 1 {
		t.Fatalf("catalog tiles = %+v, want 1 entry", listResp.Tiles)
	}
	got := listResp.Tiles[0]
	if got.ID != "grass_01" || got.Width != 2 || got.Height != 3 {
		t.Fatalf("catalog entry = %+v, want id=grass_01 width=2 height=3", got)
	}
}

func TestTileRoutes_CatalogGetMethodNotAllowed(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	req := httptest.NewRequest(http.MethodPost, "/catalog/tiles", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /catalog/tiles: status = %d, want 405", rec.Code)
	}
}

func TestTileRoutes_ImageUploadThenServe(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	pngBytes := tileTestPNG(t, 1, 1)
	up := httptest.NewRequest(http.MethodPost, "/tiles/stone_01", bytes.NewReader(pngBytes))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", urec.Code, urec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/tiles/images/stone_01.png", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("GET image: status %d", grec.Code)
	}
	if ct := grec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if got := grec.Body.Bytes(); !bytes.Equal(got, pngBytes) {
		t.Fatalf("served image bytes do not match uploaded bytes (got %d bytes)", len(got))
	}

	// 404 for an unknown image key.
	miss := httptest.NewRequest(http.MethodGet, "/tiles/images/does_not_exist.png", nil)
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, miss)
	if mrec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing image, got %d", mrec.Code)
	}
}

func TestTileRoutes_ImagesPrefixNotMisroutedAsID(t *testing.T) {
	// Regression net: /tiles/images/{key} must never be parsed by the
	// POST/DELETE {id} branch as an id of "images/foo.png".
	mux := newTileLibraryTestMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/tiles/images/foo.png", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /tiles/images/foo.png: status = %d, want 405", rec.Code)
	}
}

func TestTileRoutes_SaveRejectsNonPNGBody(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	up := httptest.NewRequest(http.MethodPost, "/tiles/not_a_png", strings.NewReader("this is not a png"))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", urec.Code, urec.Body.String())
	}
	if !strings.Contains(urec.Body.String(), "image_rejected") {
		t.Fatalf("expected image_rejected error code, got %s", urec.Body.String())
	}
}

func TestTileRoutes_SaveRejectsOversizedBody(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	oversized := bytes.Repeat([]byte{0}, 2*1024*1024+2)
	up := httptest.NewRequest(http.MethodPost, "/tiles/too_big", bytes.NewReader(oversized))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", urec.Code, urec.Body.String())
	}
}

func TestTileRoutes_DeleteRemovesImageAndEmptiesCatalog(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	up := httptest.NewRequest(http.MethodPost, "/tiles/delete_me", bytes.NewReader(tileTestPNG(t, 1, 1)))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", urec.Code, urec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/tiles/delete_me", nil)
	drec := httptest.NewRecorder()
	mux.ServeHTTP(drec, del)
	if drec.Code != http.StatusOK {
		t.Fatalf("DELETE: status %d body %s", drec.Code, drec.Body.String())
	}
	if !strings.Contains(drec.Body.String(), `"deleted"`) {
		t.Fatalf("delete response missing deleted status: %s", drec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/catalog/tiles", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	var listResp struct {
		Tiles []json.RawMessage `json:"tiles"`
	}
	if err := json.Unmarshal(grec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if len(listResp.Tiles) != 0 {
		t.Fatalf("catalog tiles = %+v, want empty after delete", listResp.Tiles)
	}
}

func TestTileRoutes_IDWithSlashRejected(t *testing.T) {
	mux := newTileLibraryTestMux(t)

	req := httptest.NewRequest(http.MethodDelete, "/tiles/foo/bar", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
