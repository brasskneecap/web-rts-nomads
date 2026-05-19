package embedded

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

//go:embed testdata/dist
var testDistFS embed.FS

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	sub, err := fs.Sub(testDistFS, "testdata/dist")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	h, err := Handler(sub)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRootServesIndexWithNoCache(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := rec.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), `id="app"`) {
		t.Errorf("body does not contain SPA marker; got: %s", rec.Body.String())
	}
}

func TestFingerprintedAssetGetsImmutableCache(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/assets/index-abc12345.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := "public, max-age=31536000, immutable"
	if got := rec.Header().Get("Cache-Control"); got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), "nomads spa test fixture") {
		t.Errorf("body does not contain asset contents; got: %s", rec.Body.String())
	}
}

func TestTopLevelEmbeddedAssetGetsHourCache(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/favicon.ico")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := "public, max-age=3600"
	if got := rec.Header().Get("Cache-Control"); got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
}

func TestSPAFallthroughReturnsIndexWithNoCache(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/main-menu/profile")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := rec.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), `id="app"`) {
		t.Errorf("fallthrough body is not index.html; got: %s", rec.Body.String())
	}
}

func TestAPIPrefixesNotShadowed(t *testing.T) {
	h := newTestHandler(t)
	cases := []string{
		"/ws",
		"/health",
		"/api/profile",
		"/catalog/units",
		"/maps",
		"/maps/forest",
		"/matches/abc/status",
		"/lobbies",
		"/lobbies/abc/join",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, path)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status for %q = %d, want 404 (SPA must not shadow API)", path, rec.Code)
			}
		})
	}
}

func TestNonGETMethodRejected(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandlerRejectsMissingIndex(t *testing.T) {
	emptyFS := fstest{} // a single-file FS that has only "other.txt", no index.html
	_, err := Handler(emptyFS)
	if err == nil {
		t.Fatal("expected error when index.html is missing, got nil")
	}
}

// fstest is a minimal fs.FS used to assert Handler rejects a dist missing index.html.
type fstest struct{}

func (fstest) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
