package httpserver

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func abIconPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestAbilityIconUploadThenServe(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	registerAbilityCatalogRoutes(mux)

	// create the ability def first
	save := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(`{"ability":{"id":"pic_bolt","damageAmount":3}}`))
	srec := httptest.NewRecorder()
	mux.ServeHTTP(srec, save)
	if srec.Code != http.StatusCreated {
		t.Fatalf("save def: %d %s", srec.Code, srec.Body.String())
	}

	// upload the icon (raw PNG body)
	up := httptest.NewRequest(http.MethodPost, "/abilities/pic_bolt/image", bytes.NewReader(abIconPNG(t)))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusCreated || !strings.Contains(urec.Body.String(), "icon_saved") {
		t.Fatalf("upload: %d %s", urec.Code, urec.Body.String())
	}

	// serve it back
	get := httptest.NewRequest(http.MethodGet, "/catalog/abilities/pic_bolt/image", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK || grec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("serve: %d ct=%q", grec.Code, grec.Header().Get("Content-Type"))
	}

	// 404 for an unknown icon
	miss := httptest.NewRequest(http.MethodGet, "/catalog/abilities/unknown_x/image", nil)
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, miss)
	if mrec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing icon, got %d", mrec.Code)
	}
}
